<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  RegisterView drives the self-service tenant registration ceremony.

  Flow:
    1. User fills in tenant name, display name, email, and (optionally) isolation mode.
    2. POST /api/v1/auth/register/begin via SDK.
    3. Browser calls navigator.credentials.create() with the returned challenge.
    4. POST /api/v1/auth/register/finish via SDK.
    5. On "active" — tokens stored, redirect to dashboard.
       On "pending" — show "awaiting admin approval" message.
       On "verification_required" — show "check your email" message.

  SDK usage:
    All network calls go through client.auth.registerBegin / registerFinish.
    Raw fetch is never used.
-->
<script lang="ts">
  import { KeyRound } from 'lucide-svelte';
  import { routeLogin, routeDashboard } from '../../stores/router.svelte.ts';
  import { setConnected } from '../../stores/connection.svelte.ts';
  import { setAuthenticated } from '../../stores/auth.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { parseCreationOptions, encodeAttestationResponse } from '../../lib/webauthn.ts';
  import type { SelfRegisterFinishResponse } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  type Stage = 'form' | 'ceremony' | 'done' | 'error';

  let stage: Stage = $state('form');
  let tenantName = $state('');
  let displayName = $state('');
  let email = $state('');
  let isolationMode = $state('keyspace');
  let busy = $state(false);
  let errorMessage = $state('');
  let doneResponse = $state<SelfRegisterFinishResponse | null>(null);

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  // persistTokens hands the issued tokens to the shared auth store, which both
  // persists them to localStorage AND flips the in-tab auth status to
  // 'authenticated'. The status flip is what lets the post-registration
  // routeDashboard() land — without it MainApp's route guard would bounce the
  // newly-registered user straight back to /login (same bug class as the
  // passkey login path).
  function persistTokens(resp: SelfRegisterFinishResponse): void {
    if (!resp.access_token) return;
    setAuthenticated({
      access_token: resp.access_token,
      refresh_token: resp.refresh_token,
    });
  }

  // ---------------------------------------------------------------------------
  // Registration flow
  // ---------------------------------------------------------------------------

  async function handleSubmit(): Promise<void> {
    if (busy) return;

    const trimName = tenantName.trim();
    const trimDisplay = displayName.trim();
    const trimEmail = email.trim();

    if (!trimName) {
      errorMessage = 'Organization name is required.';
      return;
    }
    if (!trimDisplay) {
      errorMessage = 'Display name is required.';
      return;
    }
    if (!trimEmail) {
      errorMessage = 'Email address is required.';
      return;
    }

    busy = true;
    errorMessage = '';
    stage = 'ceremony';

    try {
      const client = await getSDKClient();

      // Step 1: begin — server returns a WebAuthn challenge.
      const beginResp = await client.auth.registerBegin({
        tenant_name: trimName,
        display_name: trimDisplay,
        email: trimEmail,
        isolation_mode: isolationMode,
      });

      // Step 2: present the WebAuthn creation challenge to the browser. The
      // server emits the go-webauthn `{ publicKey: { challenge, ... } }`
      // envelope with base64url binary members; the helper unwraps it and
      // decodes challenge / user.id / excludeCredentials into ArrayBuffers.
      const creationOptions = beginResp.public_key_credential_creation_options;
      let credential: PublicKeyCredential;
      try {
        const publicKey = parseCreationOptions(creationOptions);
        const cred = await navigator.credentials.create({ publicKey });
        if (cred === null) {
          errorMessage = 'Credential creation was declined or cancelled. Try again with your hardware key.';
          stage = 'error';
          busy = false;
          return;
        }
        credential = cred as PublicKeyCredential;
      } catch (e) {
        if (e instanceof DOMException && (e.name === 'NotAllowedError' || e.name === 'AbortError')) {
          errorMessage = 'Credential creation was declined or cancelled. Try again with your hardware key.';
        } else if (e instanceof TypeError) {
          errorMessage = "Can't reach the authenticator right now. Make sure your security key is connected.";
        } else {
          errorMessage = e instanceof Error ? e.message : 'Authenticator interaction failed.';
        }
        stage = 'error';
        busy = false;
        return;
      }

      // Step 3: serialise the attestation into the base64url JSON go-webauthn
      // parses (camelCase keys: rawId / attestationObject / clientDataJSON).
      const credentialJSON = encodeAttestationResponse(credential);

      // Step 4: finish — server verifies attestation, creates tenant+user.
      const finishResp = await client.auth.registerFinish({
        session_id: beginResp.session_id,
        credential: credentialJSON as unknown as Record<string, unknown>,
      });

      doneResponse = finishResp;

      if (finishResp.status === 'active') {
        persistTokens(finishResp);
        setConnected(window.location.origin, 'rest');
        stage = 'done';
        // Redirect to dashboard after a short moment so the user sees the confirmation.
        setTimeout(() => { routeDashboard(); }, 1500);
      } else {
        // pending or verification_required — show the done screen.
        stage = 'done';
      }
    } catch (e: unknown) {
      const status = (e as { status?: number }).status;
      if (status === 403) {
        errorMessage = 'Self-service registration is not enabled on this server. Ask your admin for an invite.';
      } else if (status === 400) {
        errorMessage = (e as { message?: string }).message ?? 'Invalid registration request. Check your inputs and try again.';
      } else if (e instanceof TypeError) {
        errorMessage = "Can't reach the server right now.";
      } else {
        errorMessage = e instanceof Error ? e.message : 'Registration failed. Please try again.';
      }
      stage = 'error';
    } finally {
      busy = false;
    }
  }

  function backToForm(): void {
    stage = 'form';
    errorMessage = '';
  }
</script>

<main class="flex min-h-full items-center justify-center p-4">
  <div class="card bg-base-200 w-full max-w-sm shadow-xl hover:-translate-y-0.5 transition-transform">
    <div class="card-body gap-5">

      <!-- Header -->
      <div class="text-center">
        <div class="text-2xl font-bold gradient-text">QuicBaaS</div>
        <div class="text-sm text-base-content/50 mt-1">Create your organization</div>
      </div>

      <!-- Error alert -->
      {#if errorMessage}
        <div class="alert alert-error py-2 px-3 text-sm" role="alert">
          <span>{errorMessage}</span>
          <button
            class="btn btn-xs btn-ghost ml-auto"
            onclick={backToForm}
            aria-label="Try again"
          >
            Try again
          </button>
        </div>
      {/if}

      <!-- ===================================================================
           FORM STAGE
           =================================================================== -->
      {#if stage === 'form' || stage === 'error'}
        <div class="flex flex-col gap-3">

          <!-- Organization name -->
          <label class="form-control">
            <div class="label">
              <span class="label-text text-sm">Organization name</span>
            </div>
            <input
              type="text"
              class="input input-bordered input-sm"
              bind:value={tenantName}
              placeholder="Acme Corp"
              autocomplete="organization"
              aria-label="Organization name"
              onkeydown={(e) => { if (e.key === 'Enter') void handleSubmit(); }}
            />
          </label>

          <!-- Display name -->
          <label class="form-control">
            <div class="label">
              <span class="label-text text-sm">Your display name</span>
            </div>
            <input
              type="text"
              class="input input-bordered input-sm"
              bind:value={displayName}
              placeholder="Jane Smith"
              autocomplete="name"
              aria-label="Display name"
              onkeydown={(e) => { if (e.key === 'Enter') void handleSubmit(); }}
            />
          </label>

          <!-- Email -->
          <label class="form-control">
            <div class="label">
              <span class="label-text text-sm">Email address</span>
            </div>
            <input
              type="email"
              class="input input-bordered input-sm"
              bind:value={email}
              placeholder="you@example.com"
              autocomplete="email"
              aria-label="Email address"
              onkeydown={(e) => { if (e.key === 'Enter') void handleSubmit(); }}
            />
          </label>

          <!-- Isolation mode (only shown when barrier is relevant) -->
          <div class="form-control">
            <div class="label pb-1">
              <span class="label-text text-sm">Tenant isolation</span>
            </div>
            <div class="flex gap-4">
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  class="radio radio-sm radio-primary"
                  name="isolation"
                  value="keyspace"
                  bind:group={isolationMode}
                />
                <span class="text-sm">Keyspace</span>
              </label>
              <label class="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  class="radio radio-sm radio-primary"
                  name="isolation"
                  value="barrier"
                  bind:group={isolationMode}
                />
                <span class="text-sm">Barrier</span>
              </label>
            </div>
            <div class="label pt-0.5">
              <span class="label-text-alt text-base-content/40">
                {isolationMode === 'barrier'
                  ? 'Dedicated cryptographic barrier — stronger isolation, requires provisioning.'
                  : 'Shared keyspace with prefix scoping — fastest option.'}
              </span>
            </div>
          </div>

          <!-- Submit -->
          <button
            class="btn btn-primary w-full"
            onclick={() => void handleSubmit()}
            disabled={busy || !tenantName.trim() || !displayName.trim() || !email.trim()}
            aria-label="Create account with passkey"
          >
            {#if busy}
              <span class="loading loading-spinner loading-sm"></span>
            {:else}
              <KeyRound class="h-4 w-4" />
              Create account
            {/if}
          </button>

          <!-- Link back to login -->
          <p class="text-center text-xs text-base-content/40">
            Already have an account?
            <button
              class="link link-primary"
              onclick={() => routeLogin()}
              aria-label="Go to sign-in"
            >
              Sign in
            </button>
          </p>
        </div>

      <!-- ===================================================================
           CEREMONY STAGE — waiting for authenticator
           =================================================================== -->
      {:else if stage === 'ceremony'}
        <div class="flex flex-col items-center gap-4 py-4">
          <span class="loading loading-spinner loading-lg"></span>
          <p class="text-sm text-base-content/70 text-center">
            Touch your hardware security key when prompted by the browser…
          </p>
        </div>

      <!-- ===================================================================
           DONE STAGE — show result
           =================================================================== -->
      {:else if stage === 'done' && doneResponse !== null}
        {#if doneResponse.status === 'active'}
          <div class="flex flex-col items-center gap-3 py-2 text-center">
            <div class="text-success text-4xl">&#10003;</div>
            <p class="font-semibold">Account created!</p>
            <p class="text-sm text-base-content/60">
              Your organization <code class="font-mono text-xs">{doneResponse.tenant_id}</code> is ready.
              Redirecting to dashboard…
            </p>
          </div>
        {:else if doneResponse.status === 'pending'}
          <div class="flex flex-col items-center gap-3 py-2 text-center">
            <div class="text-warning text-4xl">&#9201;</div>
            <p class="font-semibold">Awaiting approval</p>
            <p class="text-sm text-base-content/60">
              {doneResponse.message ?? 'Your account is awaiting admin approval. Check back soon.'}
            </p>
            <button class="btn btn-ghost btn-sm mt-2" onclick={() => routeLogin()}>
              Back to sign-in
            </button>
          </div>
        {:else}
          <div class="flex flex-col items-center gap-3 py-2 text-center">
            <div class="text-info text-4xl">&#9993;</div>
            <p class="font-semibold">Check your email</p>
            <p class="text-sm text-base-content/60">
              {doneResponse.message ?? 'A verification link has been sent. Please check your inbox.'}
            </p>
            <button class="btn btn-ghost btn-sm mt-2" onclick={() => routeLogin()}>
              Back to sign-in
            </button>
          </div>
        {/if}
      {/if}

    </div>
  </div>
</main>
