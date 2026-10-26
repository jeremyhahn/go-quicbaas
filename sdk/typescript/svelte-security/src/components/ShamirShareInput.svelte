<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.
-->

<!--
  ShamirShareInput drives one Shamir quorum (unseal by default; label names
  the operation the client's quorum performs, e.g. "Seal"). Each key holder
  requests a challenge, then submits its signed response with its share, and
  the progress meter advances toward the threshold reported by status().
-->
<script lang="ts">
  import type { BarrierClient, BarrierQuorumChallenge, BarrierStatus } from '../types.ts';
  import { quorumChallengeMessage, quorumChallengePurpose } from '../challengeMessage.ts';

  interface Props {
    client: Pick<BarrierClient, 'quorumBegin' | 'quorumSubmit'>;
    status: BarrierStatus;
    label?: string;
    onunseal?: (status: BarrierStatus) => void;
    onerror?: (err: Error) => void;
  }

  const { client, status, label = 'Unseal', onunseal, onerror }: Props = $props();

  // Each key holder unseals in two steps: it requests a challenge, signs the
  // challenge's message offline, then submits the signed response with its
  // share. The message is qrdb's mtls.ChallengeMessage, never the bare nonce.
  let username = $state('');
  let challenge = $state<BarrierQuorumChallenge | null>(null);
  let purpose = $state('');
  let signingMessage = $state('');
  let response = $state('');
  let share = $state('');
  let submitting = $state(false);
  let errorMsg: string | null = $state(null);

  // progress overrides the prop's counters once a share has been submitted
  // from this form. Until then the meter mirrors the daemon's status.
  let progress = $state<{ submitted: number; required: number } | null>(null);

  const submitted = $derived(progress?.submitted ?? status.shares_provided);
  const required = $derived(progress?.required ?? status.threshold);

  const percent = $derived(
    required > 0 ? Math.min(100, Math.round((submitted / required) * 100)) : 0,
  );

  function fail(e: unknown): void {
    const err = e instanceof Error ? e : new Error(String(e));
    errorMsg = err.message;
    onerror?.(err);
  }

  async function requestChallenge(): Promise<void> {
    errorMsg = null;
    if (!username.trim()) {
      errorMsg = 'Officer username is required';
      return;
    }
    submitting = true;
    try {
      const begun = await client.quorumBegin({ username: username.trim() });
      purpose = quorumChallengePurpose(begun);
      signingMessage = quorumChallengeMessage(begun);
      challenge = begun;
    } catch (e) {
      fail(e);
    } finally {
      submitting = false;
    }
  }

  async function submitShare(): Promise<void> {
    errorMsg = null;
    if (challenge === null) {
      return;
    }
    if (!share.trim() || !response.trim()) {
      errorMsg = 'Share and signed response are required';
      return;
    }
    submitting = true;
    try {
      const outcome = await client.quorumSubmit({
        username: username.trim(),
        challenge_id: challenge.challenge_id,
        factor: challenge.factor,
        response: response.trim(),
        share: share.trim(),
      });
      progress = { submitted: outcome.submitted, required: outcome.required };
      challenge = null;
      username = '';
      response = '';
      share = '';
      if (outcome.complete) {
        onunseal?.({
          ...status,
          sealed: false,
          shares_provided: outcome.submitted,
          threshold: outcome.required,
        });
      }
    } catch (e) {
      // qrdb consumes the challenge on every submission, a refused one
      // included, so drop it: the holder requests a fresh challenge and signs
      // that one. The share and username stay for the retry.
      challenge = null;
      response = '';
      fail(e);
    } finally {
      submitting = false;
    }
  }
</script>

<div class="space-y-3" data-testid="shamir-share-input">
  {#if errorMsg !== null}
    <div class="alert alert-error text-sm" role="alert">{errorMsg}</div>
  {/if}

  <div>
    <div class="flex justify-between text-xs mb-1">
      <span class="text-base-content/60">{label} progress</span>
      <span class="font-mono" data-testid="shamir-progress-text">
        {submitted} / {required}
      </span>
    </div>
    <progress
      class="progress progress-warning w-full"
      value={percent}
      max="100"
      aria-label={`Shamir ${label.toLowerCase()} progress`}
      data-testid="shamir-progress-bar"
    ></progress>
  </div>

  <div class="form-control">
    <label class="label label-text" for="shamir-officer-username">Officer username</label>
    <input
      id="shamir-officer-username"
      class="input input-bordered input-sm font-mono"
      type="text"
      autocomplete="off"
      disabled={challenge !== null}
      bind:value={username}
    />
  </div>

  {#if challenge === null}
    <button class="btn btn-primary btn-sm" disabled={submitting} onclick={requestChallenge}>
      {submitting ? 'Requesting...' : 'Request challenge'}
    </button>
  {:else}
    <div class="text-xs font-mono break-all space-y-1" data-testid="shamir-challenge">
      <div>Challenge: {challenge.challenge_id}</div>
      <div>Factor: {challenge.factor}</div>
      <div>Purpose: {purpose}</div>
      <div>Expires: {challenge.expires_at}</div>
    </div>

    <div class="form-control">
      <label class="label label-text" for="shamir-signing-message">Message to sign (base64)</label>
      <textarea
        id="shamir-signing-message"
        class="textarea textarea-bordered font-mono text-xs"
        rows="3"
        readonly
        data-testid="shamir-signing-message"
        value={signingMessage}
      ></textarea>
      <p class="text-xs text-base-content/60">
        Base64-decode this message and sign the decoded bytes with your enrolled certificate's
        private key. It binds the challenge, its purpose and the nonce; a signature over the
        nonce alone is refused.
      </p>
    </div>

    <div class="form-control">
      <label class="label label-text" for="shamir-signed-response">Signed response</label>
      <textarea
        id="shamir-signed-response"
        class="textarea textarea-bordered font-mono text-xs"
        rows="2"
        placeholder="Paste the base64 signature over the message..."
        bind:value={response}
      ></textarea>
    </div>

    <div class="form-control">
      <label class="label label-text" for="shamir-share-textarea">Share</label>
      <textarea
        id="shamir-share-textarea"
        class="textarea textarea-bordered font-mono text-xs"
        rows="2"
        placeholder="Paste your base64 share..."
        bind:value={share}
      ></textarea>
    </div>

    <button class="btn btn-primary btn-sm" disabled={submitting} onclick={submitShare}>
      {submitting ? 'Submitting...' : 'Submit share'}
    </button>
  {/if}
</div>
