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
  InviteActivateView renders the passwordless account activation page.

  The page is publicly accessible (no auth required) at #/invite/{token}.
  It reads the token from route params, fetches invite metadata, then lets
  the user activate their account via FIDO2/WebAuthn or a PIV certificate.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { navigateTo } from '../../stores/router.svelte.ts';
  import type { InviteMetadata, ActivationResponse } from '@quicbaas/sdk';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  interface Props {
    token: string;
  }
  const { token }: Props = $props();

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  type Phase = 'loading' | 'ready' | 'activating' | 'done' | 'error';

  let phase: Phase = $state('loading');
  let invite: InviteMetadata | null = $state(null);
  let loadError: string | null = $state(null);

  // Which method the user chose for this activation session.
  let selectedMethod: 'webauthn' | 'piv' = $state('webauthn');

  // PIV path state.
  let pivPEM = $state('');
  let pivError: string | null = $state(null);

  // General activation error.
  let activationError: string | null = $state(null);

  // Result message shown after successful activation.
  let successMessage = $state('');

  // ---------------------------------------------------------------------------
  // Load invite metadata on mount
  // ---------------------------------------------------------------------------

  async function loadInvite(): Promise<void> {
    if (!token) {
      loadError = 'Missing invite token';
      phase = 'error';
      return;
    }
    try {
      const client = await getSDKClient();
      invite = await client.iam.getInvite(token);
      // Default to first method offered by the server, if available.
      if (invite.auth_methods.length > 0) {
        const first = invite.auth_methods[0];
        if (first === 'webauthn' || first === 'fido2') {
          selectedMethod = 'webauthn';
        } else if (first === 'piv') {
          selectedMethod = 'piv';
        }
      }
      phase = 'ready';
    } catch (e) {
      loadError = e instanceof Error ? e.message : 'Failed to load invite';
      phase = 'error';
    }
  }

  // Run immediately on component mount.
  void loadInvite();

  // ---------------------------------------------------------------------------
  // FIDO2 activation
  // ---------------------------------------------------------------------------

  async function activateFIDO2(): Promise<void> {
    activationError = null;
    phase = 'activating';
    try {
      const client = await getSDKClient();

      // Begin: fetch credential creation options from server.
      const options = await client.iam.activateFIDO2Begin(token);

      // Ask the browser to create a new credential.
      // navigator.credentials.create() is standard Web Authentication API.
      const rawCredential = await navigator.credentials.create({
        publicKey: options as unknown as PublicKeyCredentialCreationOptions,
      });
      if (rawCredential === null) {
        throw new Error('Credential creation cancelled');
      }

      // Serialize the PublicKeyCredential for transport.
      const pkc = rawCredential as PublicKeyCredential;
      const response = pkc.response as AuthenticatorAttestationResponse;
      const credential: Record<string, unknown> = {
        id: pkc.id,
        rawId: Array.from(new Uint8Array(pkc.rawId)),
        type: pkc.type,
        response: {
          clientDataJSON: Array.from(new Uint8Array(response.clientDataJSON)),
          attestationObject: Array.from(new Uint8Array(response.attestationObject)),
        },
      };

      const result: ActivationResponse = await client.iam.activateFIDO2Finish(token, credential);
      successMessage = result.message || 'Account activated successfully';
      phase = 'done';
    } catch (e) {
      activationError = e instanceof Error ? e.message : 'FIDO2 activation failed';
      phase = 'ready';
    }
  }

  // ---------------------------------------------------------------------------
  // PIV certificate activation
  // ---------------------------------------------------------------------------

  async function activatePIV(): Promise<void> {
    pivError = null;
    activationError = null;
    if (!pivPEM.trim()) {
      pivError = 'Paste your PIV certificate PEM below';
      return;
    }
    phase = 'activating';
    try {
      const client = await getSDKClient();
      const result: ActivationResponse = await client.iam.activatePIV(token, pivPEM.trim());
      successMessage = result.message || 'Account activated successfully';
      phase = 'done';
    } catch (e) {
      activationError = e instanceof Error ? e.message : 'PIV activation failed';
      phase = 'ready';
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function supportsWebAuthn(): boolean {
    return typeof window !== 'undefined' && !!window.PublicKeyCredential;
  }

  function methodAllowed(method: 'webauthn' | 'piv'): boolean {
    if (!invite) return false;
    if (method === 'webauthn') {
      return invite.auth_methods.some(m => m === 'webauthn' || m === 'fido2');
    }
    return invite.auth_methods.includes('piv');
  }
</script>

<div class="min-h-screen flex items-center justify-center bg-base-200 px-4">
  <div class="card w-full max-w-md bg-base-100 shadow-xl">
    <div class="card-body">

      <!-- Header -->
      <h1 class="card-title text-xl justify-center mb-1">Activate Your Account</h1>

      <!-- Loading -->
      {#if phase === 'loading'}
        <div class="flex justify-center py-8">
          <span class="loading loading-spinner loading-lg"></span>
        </div>

      <!-- Error loading invite -->
      {:else if phase === 'error'}
        <div class="alert alert-error mt-4" role="alert">
          <span>{loadError}</span>
        </div>
        <p class="text-sm text-center opacity-60 mt-3">
          This invite link may have expired or already been used.
        </p>

      <!-- Success -->
      {:else if phase === 'done'}
        <div class="alert alert-success mt-4" role="alert">
          <span>{successMessage}</span>
        </div>
        <p class="text-sm text-center opacity-60 mt-2">You can now sign in to your account.</p>
        <div class="card-actions justify-center mt-4">
          <button class="btn btn-primary btn-sm" onclick={() => navigateTo('/login')}>Go to Login</button>
        </div>

      <!-- Ready / Activating -->
      {:else}
        <!-- Invite info -->
        {#if invite}
          <div class="text-sm opacity-70 text-center mb-4">
            {#if invite.name}
              <p class="font-medium">{invite.name}</p>
            {/if}
            {#if invite.email}
              <p>{invite.email}</p>
            {/if}
            <p class="text-xs mt-1">
              Expires: {new Date(invite.expires_at).toLocaleString()}
            </p>
          </div>
        {/if}

        <!-- Activation error -->
        {#if activationError}
          <div class="alert alert-error text-sm mb-3" role="alert">{activationError}</div>
        {/if}

        <!-- Method selector -->
        {#if methodAllowed('webauthn') && methodAllowed('piv')}
          <div class="flex gap-3 mb-4">
            <label class="flex items-center gap-2 cursor-pointer border rounded-lg px-3 py-2 flex-1 {selectedMethod === 'webauthn' ? 'border-primary bg-primary/10' : 'border-base-300'}">
              <input type="radio" class="radio radio-primary radio-sm" name="act-method" value="webauthn" bind:group={selectedMethod} />
              <span class="text-sm font-medium">FIDO2 / WebAuthn</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer border rounded-lg px-3 py-2 flex-1 {selectedMethod === 'piv' ? 'border-primary bg-primary/10' : 'border-base-300'}">
              <input type="radio" class="radio radio-primary radio-sm" name="act-method" value="piv" bind:group={selectedMethod} />
              <span class="text-sm font-medium">PIV Certificate</span>
            </label>
          </div>
        {/if}

        <!-- FIDO2 panel -->
        {#if selectedMethod === 'webauthn' && methodAllowed('webauthn')}
          <div class="space-y-3">
            {#if !supportsWebAuthn()}
              <div class="alert alert-warning text-sm" role="alert">
                Your browser does not support WebAuthn. Use a modern browser or switch to PIV activation.
              </div>
            {:else}
              <p class="text-sm opacity-70">
                Click the button below and follow your browser's prompts to register your security key or passkey.
              </p>
              <button
                class="btn btn-primary w-full"
                disabled={phase === 'activating'}
                onclick={activateFIDO2}
              >
                {phase === 'activating' ? 'Registering…' : 'Register Security Key / Passkey'}
              </button>
            {/if}
          </div>

        <!-- PIV panel -->
        {:else if selectedMethod === 'piv' && methodAllowed('piv')}
          <div class="space-y-3">
            <p class="text-sm opacity-70">
              Export your PIV certificate in PEM format and paste it below.
              The certificate chain must be trusted by the server.
            </p>
            {#if pivError}
              <div class="alert alert-error text-sm" role="alert">{pivError}</div>
            {/if}
            <div class="form-control">
              <label class="label label-text" for="piv-pem">PIV Certificate (PEM)</label>
              <textarea
                id="piv-pem"
                class="textarea textarea-bordered textarea-sm font-mono text-xs h-36"
                placeholder="-----BEGIN CERTIFICATE-----&#10;...&#10;-----END CERTIFICATE-----"
                bind:value={pivPEM}
              ></textarea>
            </div>
            <button
              class="btn btn-primary w-full"
              disabled={phase === 'activating'}
              onclick={activatePIV}
            >
              {phase === 'activating' ? 'Verifying…' : 'Activate with PIV Certificate'}
            </button>
          </div>

        {:else}
          <div class="alert alert-warning text-sm" role="alert">
            No supported authentication method available for this invite.
          </div>
        {/if}
      {/if}

    </div>
  </div>
</div>
