<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.
-->

<!--
  CeremonyClaimChallenge handles the challenge-response flow for an officer
  claiming their ceremony slot. The operator pastes the daemon's challenge
  string and a base64 signature produced offline by the CSR private key.
-->
<script lang="ts">
  import type { CeremonyClient, ClaimResponse } from '../types.ts';

  interface Props {
    client: CeremonyClient;
    disabled?: boolean;
    onclaimed?: (resp: ClaimResponse) => void;
    onerror?: (err: Error) => void;
  }

  const { client, disabled = false, onclaimed, onerror }: Props = $props();

  let username = $state('');
  let challenge = $state('');
  let signatureB64 = $state('');
  let submitting = $state(false);
  let errorMsg: string | null = $state(null);

  async function submit(): Promise<void> {
    errorMsg = null;
    if (!username.trim() || !challenge.trim() || !signatureB64.trim()) {
      errorMsg = 'username, challenge, and signature are required';
      return;
    }
    submitting = true;
    try {
      const resp = await client.claimCertificate({
        username: username.trim(),
        challenge: challenge.trim(),
        signature_b64: signatureB64.trim(),
      });
      onclaimed?.(resp);
      challenge = '';
      signatureB64 = '';
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      errorMsg = err.message;
      onerror?.(err);
    } finally {
      submitting = false;
    }
  }
</script>

<form
  class="space-y-3"
  data-testid="ceremony-claim-challenge"
  onsubmit={(e) => { e.preventDefault(); void submit(); }}
>
  <h3 class="text-sm font-semibold">Claim Certificate</h3>

  {#if errorMsg !== null}
    <div class="alert alert-error text-sm" role="alert">{errorMsg}</div>
  {/if}

  <div class="form-control">
    <label class="label label-text" for="ceremony-claim-username">Username</label>
    <input
      id="ceremony-claim-username"
      class="input input-bordered input-sm font-mono"
      type="text"
      bind:value={username}
    />
  </div>

  <div class="form-control">
    <label class="label label-text" for="ceremony-claim-challenge">Challenge</label>
    <textarea
      id="ceremony-claim-challenge"
      class="textarea textarea-bordered font-mono text-xs"
      rows="2"
      placeholder="Paste daemon-issued challenge..."
      bind:value={challenge}
    ></textarea>
  </div>

  <div class="form-control">
    <label class="label label-text" for="ceremony-claim-signature">Signature (base64)</label>
    <textarea
      id="ceremony-claim-signature"
      class="textarea textarea-bordered font-mono text-xs"
      rows="3"
      placeholder="Paste base64 signature..."
      bind:value={signatureB64}
    ></textarea>
  </div>

  <button
    class="btn btn-primary btn-sm"
    type="submit"
    disabled={submitting || disabled}
    title={disabled ? 'Ceremony service is not active. Run `quicbaasd init` first.' : undefined}
  >
    {submitting ? 'Submitting...' : 'Submit Claim'}
  </button>
</form>
