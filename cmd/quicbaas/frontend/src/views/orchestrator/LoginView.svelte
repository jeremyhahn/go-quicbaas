<!--
  LoginView renders the hardware-only authentication chooser.

  Two methods are offered:
    1. Passkey (FIDO2 / WebAuthn) — tenant-select then email-then-assertion flow.
    2. Smartcard (PIV) — TLS client-cert POST to /api/v1/auth/login/piv.

  Organization (tenant) entry:
    The organization id is typed into a free-text field. It is deliberately NOT
    a dropdown: populating one required an unauthenticated tenant-listing
    endpoint, which handed every tenant's id and name to anyone who loaded this
    page — a cross-tenant namespace disclosure. That endpoint has been deleted;
    the client asks the operator for the id instead of enumerating them.

    To keep repeat sign-ins one click away, the last SUCCESSFULLY used
    organization id is remembered in localStorage and prefilled on the next
    visit. Only a value that actually completed a login is stored, so a typo
    never becomes a sticky default. A first-time visitor gets 'system', qrdb's
    canonical control-plane tenant, which is the right answer for single-tenant
    and first-boot deployments.

    PIV login is tenant-agnostic (the server resolves the tenant from the cert
    thumbprint), so the field does not participate in the smartcard flow.

  SDK usage:
    All network calls go through the SDK client (client.auth.*). The view does
    NOT use raw fetch for transport; raw fetch is gone.

  Token storage:
    On success the server returns { access_token, refresh_token }.
    The tokens are handed to the shared auth store (setAuthenticated), which
    persists access_token to localStorage under "quicbaas_token" AND flips the
    in-tab auth status to 'authenticated'. The status flip is essential: the
    MainApp route guard bounces protected routes to /login whenever the status
    is 'unauthenticated', so persisting the token alone is not enough — the
    status must change in this tab before routeDashboard() runs.
-->
<script lang="ts">
  import { KeyRound, CreditCard } from 'lucide-svelte';
  import { routeDashboard } from '../../stores/router.svelte.ts';
  import { setConnected } from '../../stores/connection.svelte.ts';
  import { setAuthenticated } from '../../stores/auth.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { parseRequestOptions, encodeAssertionResponse } from '../../lib/webauthn.ts';
  import type { AuthTokenResponse } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  type Mode = 'chooser' | 'passkey' | 'error';

  // GENERIC_AUTH_FAILURE is the single message shown for every rejected
  // passkey sign-in. It names neither the organization nor the email, so the
  // UI cannot be used to probe which of the two exists.
  const GENERIC_AUTH_FAILURE =
    "Sign-in failed. Check your organization and email address, then try again.";

  let mode: Mode = $state('chooser');
  let email = $state('');
  let busy = $state(false);
  let errorMessage = $state('');

  // Organization (tenant) id — used for the passkey (FIDO2) path only.
  // 'system' is qrdb's canonical control-plane tenant and the correct default
  // for a single-tenant / first-boot deployment. It is a CLIENT-side default:
  // the server never tells this page which tenants exist.
  const DEFAULT_TENANT_ID = 'system';
  const LAST_TENANT_KEY = 'quicbaas_login_tenant';

  let tenantID = $state(
    window.localStorage.getItem(LAST_TENANT_KEY) ?? DEFAULT_TENANT_ID,
  );

  // Passkey button ref for auto-focus on mount.
  let passkeyBtnEl: HTMLButtonElement | null = $state(null);

  // Idle timeout handle.
  let idleTimer: ReturnType<typeof setTimeout> | null = null;

  // ---------------------------------------------------------------------------
  // Idle timeout — blur email field after 60 s of inactivity on this screen.
  // ---------------------------------------------------------------------------

  function resetIdleTimer(): void {
    if (idleTimer !== null) clearTimeout(idleTimer);
    idleTimer = setTimeout(() => {
      const emailEl = document.getElementById('login-email') as HTMLInputElement | null;
      emailEl?.blur();
    }, 60_000);
  }

  $effect(() => {
    // Auto-focus the passkey button when the chooser is visible.
    if (mode === 'chooser' && passkeyBtnEl !== null) {
      passkeyBtnEl.focus();
    }

    // Start idle timer on mount; listen for any user interaction to reset it.
    resetIdleTimer();
    const handler = () => resetIdleTimer();
    window.addEventListener('pointerdown', handler, { passive: true });
    window.addEventListener('keydown', handler, { passive: true });
    window.addEventListener('input', handler, { passive: true });
    return () => {
      if (idleTimer !== null) clearTimeout(idleTimer);
      window.removeEventListener('pointerdown', handler);
      window.removeEventListener('keydown', handler);
      window.removeEventListener('input', handler);
    };
  });

  // ---------------------------------------------------------------------------
  // Token persistence helpers
  // ---------------------------------------------------------------------------

  // rememberTenant stores an organization id that has just completed a login.
  // Persisting only on success is what makes the prefill trustworthy: a typo
  // or a probe of someone else's namespace never becomes a sticky default.
  function rememberTenant(id: string): void {
    window.localStorage.setItem(LAST_TENANT_KEY, id);
  }

  function handleAuthSuccess(tokens: AuthTokenResponse): void {
    if (!tokens || !tokens.access_token) {
      // A 200 with no usable token is a server contract violation, not a
      // success — surface it instead of silently routing to a dead session.
      errorMessage = 'Sign-in did not return a session token. Please try again.';
      return;
    }
    // setAuthenticated persists the tokens AND flips the shared auth status to
    // 'authenticated' in THIS tab. That second step is the bug fix: without it
    // MainApp's route guard still saw 'unauthenticated' and bounced the
    // freshly-signed-in user straight back to the login chooser.
    setAuthenticated({
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
    });
    setConnected(window.location.origin, 'rest');
    routeDashboard();
  }

  // ---------------------------------------------------------------------------
  // FIDO2 / WebAuthn login flow — uses SDK client.auth.webAuthnBeginLogin /
  // webAuthnFinishLogin. The base64url encode/decode for the WebAuthn challenge
  // bytes stays in the view (serialization concern, not transport concern).
  // ---------------------------------------------------------------------------

  async function startPasskeyFlow(): Promise<void> {
    mode = 'passkey';
    errorMessage = '';
    // Focus the email field after the mode transition settles.
    await Promise.resolve();
    (document.getElementById('login-email') as HTMLInputElement | null)?.focus();
    resetIdleTimer();
  }

  async function submitPasskey(): Promise<void> {
    if (busy) return;
    const trimmedEmail = email.trim();
    if (!trimmedEmail) {
      errorMessage = 'Please enter your email address.';
      return;
    }

    const trimmedTenant = tenantID.trim();
    if (!trimmedTenant) {
      errorMessage = 'Please enter your organization.';
      return;
    }

    busy = true;
    errorMessage = '';

    try {
      const client = await getSDKClient();

      // Step 1: Begin login — fetch WebAuthn challenge from server via SDK.
      //
      // The server answers every identity-resolution failure here with the SAME
      // 401, so this branch cannot tell an unknown organization from an unknown
      // email — and neither can the message shown to the user. Reporting them
      // separately would rebuild the tenant/user enumeration oracle the
      // deleted tenant-listing endpoint used to be.
      let challenge;
      try {
        challenge = await client.auth.webAuthnBeginLogin(trimmedTenant, trimmedEmail);
      } catch (e: unknown) {
        errorMessage = e instanceof TypeError
          ? "Can't reach the server right now."
          : GENERIC_AUTH_FAILURE;
        busy = false;
        return;
      }

      // Step 2: Present the challenge to the browser / authenticator. The
      // server emits the go-webauthn `{ publicKey: { challenge, ... } }`
      // envelope with base64url binary members; the helper unwraps it and
      // decodes challenge / allowCredentials into ArrayBuffers.
      const publicKey = parseRequestOptions(challenge.options);
      const assertion = await navigator.credentials.get({ publicKey });

      if (assertion === null) {
        errorMessage = 'Authentication declined or canceled. Try again with your hardware key.';
        busy = false;
        return;
      }

      // Step 3: Serialize the assertion into the base64url JSON go-webauthn parses.
      const credential = encodeAssertionResponse(assertion as PublicKeyCredential);

      // Step 4: Finish login via SDK — the unified ceremony verifies the
      // assertion and returns the bearer + rotating refresh credential.
      const tokens = await client.auth.webAuthnFinishLogin(challenge, credential);
      // Only a completed sign-in earns the prefill slot.
      rememberTenant(trimmedTenant);
      handleAuthSuccess(tokens);
    } catch (e) {
      if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) {
        errorMessage = 'Authentication declined or canceled. Try again with your hardware key.';
      } else if (e instanceof TypeError) {
        errorMessage = "Can't reach the server right now.";
      } else {
        errorMessage = GENERIC_AUTH_FAILURE;
      }
    } finally {
      busy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // PIV smartcard flow — POST to TLS client-cert endpoint via SDK.
  // The tenant selector is hidden on the PIV path; the server resolves the
  // tenant from the cert thumbprint via the user record.
  // ---------------------------------------------------------------------------

  async function startPIVFlow(): Promise<void> {
    busy = true;
    errorMessage = '';
    try {
      const client = await getSDKClient();
      const tokens = await client.auth.loginPIV();
      handleAuthSuccess(tokens);
    } catch (e: unknown) {
      const status = (e as { status?: number }).status;
      if (status === 401 || status === 403) {
        const msg = (e as { message?: string }).message ?? '';
        if (msg.includes('piv_login_disabled')) {
          errorMessage =
            'PIV smartcard login is not enabled on this server. Contact your admin.';
        } else {
          errorMessage =
            "Your smartcard isn't recognized. Confirm the cert is enrolled with your QuicBaaS admin.";
        }
      } else if (status === 400) {
        errorMessage =
          'No matching PIV identity. Make sure your smartcard is inserted and your browser is configured to send client certificates.';
      } else if (e instanceof TypeError) {
        errorMessage = "Can't reach the server right now.";
      } else {
        errorMessage = e instanceof Error ? e.message : 'Smartcard authentication failed.';
      }
    } finally {
      busy = false;
    }
  }

  function backToChooser(): void {
    mode = 'chooser';
    errorMessage = '';
    email = '';
    resetIdleTimer();
  }
</script>

<main class="flex min-h-full items-center justify-center p-4">
  <div class="card bg-base-200 w-full max-w-sm shadow-xl hover:-translate-y-0.5 transition-transform">
    <div class="card-body gap-5">

      <!-- Header -->
      <div class="text-center">
        <div class="text-2xl font-bold gradient-text">QuicBaaS</div>
        <div class="text-sm text-base-content/50 mt-1">Hardware-Backed Sign-In</div>
      </div>

      <!-- Error alert -->
      {#if errorMessage}
        <div class="alert alert-error py-2 px-3 text-sm" role="alert">
          <span>{errorMessage}</span>
          <button
            class="btn btn-xs btn-ghost ml-auto"
            onclick={backToChooser}
            aria-label="Try again"
          >
            Try again
          </button>
        </div>
      {/if}

      {#if mode === 'chooser'}
        <!-- ----------------------------------------------------------------
             Chooser: tenant selector + two hardware login options
             ---------------------------------------------------------------- -->

        <!-- Organization id (passkey path only; PIV resolves it from the cert).
             Free text by design — see the component header. -->
        <div class="form-control">
          <label class="label pb-1" for="login-tenant">
            <span class="label-text text-sm">Organization</span>
          </label>
          <input
            id="login-tenant"
            type="text"
            class="input input-bordered input-sm w-full"
            bind:value={tenantID}
            autocomplete="organization"
            autocapitalize="none"
            autocorrect="off"
            spellcheck="false"
            aria-label="Organization"
            aria-describedby="login-tenant-help"
            placeholder={DEFAULT_TENANT_ID}
            oninput={resetIdleTimer}
          />
          <p id="login-tenant-help" class="text-xs text-base-content/40 pt-1">
            Your organization id, provided by your administrator.
          </p>
        </div>

        <!-- Passkey option -->
        <div class="flex flex-col gap-1">
          <button
            bind:this={passkeyBtnEl}
            class="btn btn-primary btn-lg w-full flex flex-col items-center gap-1 h-auto py-4"
            onclick={startPasskeyFlow}
            aria-label="Sign in with passkey"
          >
            <KeyRound class="h-6 w-6" />
            <span class="font-semibold">Sign in with passkey</span>
            <span class="text-xs font-normal opacity-80">FIDO2 / WebAuthn</span>
          </button>
          <p class="text-xs text-base-content/40 text-center px-2">
            Use a hardware security key (YubiKey, etc.) or a device-bound passkey.
          </p>
        </div>

        <!-- PIV smartcard option -->
        <div class="flex flex-col gap-1">
          <button
            class="btn btn-ghost btn-lg w-full flex flex-col items-center gap-1 h-auto py-4 border border-base-content/10"
            onclick={startPIVFlow}
            disabled={busy}
            aria-label="Sign in with smartcard"
          >
            <CreditCard class="h-6 w-6" />
            <span class="font-semibold">Sign in with smartcard</span>
            <span class="text-xs font-normal opacity-70">PIV X.509 client cert</span>
          </button>
          <p class="text-xs text-base-content/40 text-center px-2">
            Use a PIV-compatible smartcard (CAC, YubiKey PIV, etc.). Your browser will prompt for the certificate.
          </p>
        </div>

        <!-- Footer -->
        <p class="text-center text-xs text-base-content/30 pt-1">
          No account yet? Ask your admin for an invite.
        </p>

      {:else if mode === 'passkey'}
        <!-- ----------------------------------------------------------------
             Passkey: inline email + submit
             The tenant was selected on the chooser screen.
             ---------------------------------------------------------------- -->
        <div class="flex flex-col gap-3">
          <label class="form-control">
            <div class="label">
              <span class="label-text text-sm">Email address</span>
            </div>
            <input
              id="login-email"
              type="email"
              class="input input-bordered input-sm"
              bind:value={email}
              autocomplete="email"
              required
              aria-label="Email address"
              placeholder="you@example.com"
              oninput={resetIdleTimer}
              onkeydown={(e) => { if (e.key === 'Enter') void submitPasskey(); }}
            />
          </label>

          <button
            class="btn btn-primary w-full"
            onclick={() => void submitPasskey()}
            disabled={busy || email.trim() === ''}
            aria-label="Authenticate with passkey"
          >
            {#if busy}
              <span class="loading loading-spinner loading-sm"></span>
            {:else}
              <KeyRound class="h-4 w-4" />
              Authenticate
            {/if}
          </button>

          <button
            class="btn btn-ghost btn-sm w-full"
            onclick={backToChooser}
            aria-label="Back to login options"
          >
            Back
          </button>
        </div>
      {/if}

    </div>
  </div>
</main>
