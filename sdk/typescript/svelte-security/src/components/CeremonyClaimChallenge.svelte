<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.
-->

<!--
  CeremonyClaimChallenge handles the challenge-response flow for an officer
  claiming their ceremony slot. The component shows the message qrdb verifies
  the claim over; the officer signs it offline with the certificate's private
  key and pastes the base64 signature.
-->
<script lang="ts">
  import type { ClaimedShare, ShareClaimClient } from '../types.ts';
  import { shareClaimMessage, SYSTEM_CLAIM_LABEL } from '../challengeMessage.ts';

  interface Props {
    client: ShareClaimClient;
    /** claimLabel is the barrier scope's qrdb claim label (system by default). */
    claimLabel?: string;
    disabled?: boolean;
    onclaimed?: (share: ClaimedShare) => void;
    onerror?: (err: Error) => void;
  }

  const { client, claimLabel = SYSTEM_CLAIM_LABEL, disabled = false, onclaimed, onerror }: Props = $props();

  // An officer claims its share in two steps: it requests the claim nonce,
  // signs qrdb's ceremony.ShareClaimMessage (never the bare nonce) offline
  // with its key, then submits the signature.
  let username = $state('');
  let challenge = $state('');
  let signingMessage = $state('');
  let signatureB64 = $state('');
  let claimed: ClaimedShare | null = $state(null);
  let submitting = $state(false);
  let errorMsg: string | null = $state(null);

  function fail(e: unknown): void {
    const err = e instanceof Error ? e : new Error(String(e));
    errorMsg = err.message;
    onerror?.(err);
  }

  async function requestChallenge(): Promise<void> {
    errorMsg = null;
    if (!username.trim()) {
      errorMsg = 'username is required';
      return;
    }
    submitting = true;
    try {
      const begin = await client.beginClaim(username.trim());
      signingMessage = shareClaimMessage(claimLabel, username.trim(), begin.nonce_hex);
      challenge = begin.nonce_hex;
    } catch (e) {
      fail(e);
    } finally {
      submitting = false;
    }
  }

  async function submit(): Promise<void> {
    errorMsg = null;
    if (!username.trim() || !challenge.trim() || !signatureB64.trim()) {
      errorMsg = 'username, challenge, and signature are required';
      return;
    }
    submitting = true;
    try {
      claimed = await client.claimShare({
        username: username.trim(),
        nonce_hex: challenge.trim(),
        signature_b64: signatureB64.trim(),
      });
      onclaimed?.(claimed);
      challenge = '';
      signingMessage = '';
      signatureB64 = '';
    } catch (e) {
      fail(e);
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
  <h3 class="text-sm font-semibold">Claim Share</h3>

  {#if errorMsg !== null}
    <div class="alert alert-error text-sm" role="alert">{errorMsg}</div>
  {/if}

  <div class="form-control">
    <label class="label label-text" for="ceremony-claim-username">Username</label>
    <input
      id="ceremony-claim-username"
      class="input input-bordered input-sm font-mono"
      type="text"
      disabled={challenge !== ''}
      bind:value={username}
    />
  </div>

  <button
    class="btn btn-outline btn-sm"
    type="button"
    disabled={submitting || disabled}
    onclick={requestChallenge}
  >
    Request Challenge
  </button>

  <div class="form-control">
    <label class="label label-text" for="ceremony-claim-message">Message to sign (base64)</label>
    <textarea
      id="ceremony-claim-message"
      class="textarea textarea-bordered font-mono text-xs"
      rows="3"
      readonly
      placeholder="Request a challenge to get the message to sign..."
      data-testid="ceremony-claim-message"
      value={signingMessage}
    ></textarea>
    <p class="text-xs text-base-content/60">
      Base64-decode this message and sign the decoded bytes with your certificate's private
      key. It binds the barrier, your username and the nonce; a signature over the nonce
      alone is refused.
    </p>
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

  {#if claimed !== null}
    <div class="text-xs space-y-1">
      <div>Share {claimed.share_index}, sealed to your key:</div>
      <pre class="font-mono break-all whitespace-pre-wrap" data-testid="ceremony-claim-envelope">{claimed.envelope}</pre>
    </div>
  {/if}
</form>
