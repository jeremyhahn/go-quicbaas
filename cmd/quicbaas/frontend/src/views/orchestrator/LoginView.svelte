<!--
  LoginView renders the hardware-only authentication chooser.

  Two methods are offered:
    1. Passkey (FIDO2 / WebAuthn) — tenant-select then email-then-assertion flow.
    2. Smartcard (PIV) — TLS client-cert POST to /api/v1/auth/login/piv.

  Tenant selection:
    Before the passkey email field appears, a tenant <select> is populated from
    GET /api/v1/iam/tenants/public via client.auth.listPublicTenants(). The
    selection is persisted in localStorage so repeat visitors skip re-picking.
    PIV login is tenant-agnostic (the server resolves the tenant from the cert
    thumbprint), so the tenant selector is hidden in PIV mode.

  SDK usage:
    All network calls go through the SDK client (client.auth.*). The view does
    NOT use raw fetch for transport; raw fetch is gone.

  Token storage:
    On success the server returns { access_token, refresh_token, id_token }.
    access_token is persisted to localStorage under "quicbaas_token" so that
    subsequent API calls can attach "Authorization: Bearer <token>".
-->
<script lang="ts">
  import { KeyRound, CreditCard } from 'lucide-svelte';
  import { routeDashboard } from '../../stores/router.svelte.ts';
  import { setConnected } from '../../stores/connection.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { PublicTenant, PublicKeyCredentialJSON, AuthTokenResponse } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  type Mode = 'chooser' | 'passkey' | 'error';

  let mode: Mode = $state('chooser');
  let email = $state('');
  let busy = $state(false);
  let errorMessage = $state('');

  // Tenant selector state — used for the passkey (FIDO2) path only.
  let tenants: PublicTenant[] = $state([]);
  let selectedTenantID = $state(
    window.localStorage.getItem('quicbaas_login_tenant') ?? 'default',
  );
  let tenantsLoading = $state(true);

  // Passkey button ref for auto-focus on mount.
  let passkeyBtnEl: HTMLButtonElement | null = $state(null);

  // Idle timeout handle.
  let idleTimer: ReturnType<typeof setTimeout> | null = null;

  // ---------------------------------------------------------------------------
  // Load public tenants on mount
  // ---------------------------------------------------------------------------

  $effect(() => {
    void (async () => {
      try {
        const client = await getSDKClient();
        const list = await client.auth.listPublicTenants();
        tenants = list;

        // If the stored selection is no longer valid, fall back to the first tenant.
        if (list.length > 0 && !list.some((t) => t.id === selectedTenantID)) {
          selectedTenantID = list[0].id;
        }
      } catch {
        // Non-fatal: fall back to 'default' tenant if the public list fails.
        tenants = [{ id: 'default', name: 'default', display_name: 'Default' }];
      } finally {
        tenantsLoading = false;
      }
    })();
  });

  // Persist tenant selection to localStorage whenever it changes.
  $effect(() => {
    window.localStorage.setItem('quicbaas_login_tenant', selectedTenantID);
  });

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

  function persistTokens(tokens: AuthTokenResponse): void {
    window.localStorage.setItem('quicbaas_token', tokens.access_token);
    if (tokens.refresh_token) {
      window.localStorage.setItem('quicbaas_refresh_token', tokens.refresh_token);
    }
  }

  function handleAuthSuccess(tokens: AuthTokenResponse): void {
    persistTokens(tokens);
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

    busy = true;
    errorMessage = '';

    try {
      const client = await getSDKClient();
      const tenantID = selectedTenantID || 'default';

      // Step 1: Begin login — fetch WebAuthn challenge from server via SDK.
      let challengeOptions;
      try {
        challengeOptions = await client.auth.webAuthnBeginLogin(tenantID, trimmedEmail);
      } catch (e: unknown) {
        const status = (e as { status?: number }).status;
        if (status === 404) {
          errorMessage = "We couldn't find that email. Check with your admin or try a different identity.";
        } else {
          errorMessage = e instanceof Error ? e.message : "Can't reach the server right now.";
        }
        busy = false;
        return;
      }

      // Step 2: Present the challenge to the browser / authenticator.
      const assertion = await navigator.credentials.get({
        publicKey: challengeOptions as unknown as PublicKeyCredentialRequestOptions,
      });

      if (assertion === null) {
        errorMessage = 'Authentication declined or canceled. Try again with your hardware key.';
        busy = false;
        return;
      }

      // Step 3: Serialize assertion bytes into JSON-safe arrays.
      const pkc = assertion as PublicKeyCredential;
      const assertionResponse = pkc.response as AuthenticatorAssertionResponse;
      const credential: PublicKeyCredentialJSON = {
        id: pkc.id,
        rawId: Array.from(new Uint8Array(pkc.rawId)),
        type: pkc.type,
        response: {
          authenticatorData: Array.from(new Uint8Array(assertionResponse.authenticatorData)),
          clientDataJSON: Array.from(new Uint8Array(assertionResponse.clientDataJSON)),
          signature: Array.from(new Uint8Array(assertionResponse.signature)),
          userHandle: assertionResponse.userHandle
            ? Array.from(new Uint8Array(assertionResponse.userHandle))
            : null,
        },
      };

      // Step 4: Finish login via SDK — server verifies and returns tokens.
      const tokens = await client.auth.webAuthnFinishLogin(tenantID, trimmedEmail, credential);
      handleAuthSuccess(tokens);
    } catch (e) {
      if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) {
        errorMessage = 'Authentication declined or canceled. Try again with your hardware key.';
      } else if (e instanceof TypeError) {
        errorMessage = "Can't reach the server right now.";
      } else {
        errorMessage = e instanceof Error ? e.message : 'Authentication failed.';
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

        <!-- Tenant selector (passkey path only; hidden when in PIV) -->
        <div class="form-control">
          <label class="label pb-1" for="login-tenant">
            <span class="label-text text-sm">Organization</span>
          </label>
          {#if tenantsLoading}
            <div class="skeleton h-8 w-full rounded"></div>
          {:else}
            <select
              id="login-tenant"
              class="select select-bordered select-sm w-full"
              aria-label="Select tenant"
              bind:value={selectedTenantID}
            >
              {#each tenants as t (t.id)}
                <option value={t.id}>{t.display_name}</option>
              {/each}
              {#if tenants.length === 0}
                <option value="default">Default</option>
              {/if}
            </select>
          {/if}
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
