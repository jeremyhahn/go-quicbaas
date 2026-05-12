<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.
-->

<!--
  ShamirShareInput drives the Shamir unseal flow. Operators paste a single
  share or a batch (newline-separated). Each share is submitted in order and
  the progress meter advances toward the threshold reported by status().
-->
<script lang="ts">
  import type { BarrierClient, BarrierStatus } from '../types.ts';

  interface Props {
    client: BarrierClient;
    status: BarrierStatus;
    onunseal?: (status: BarrierStatus) => void;
    onerror?: (err: Error) => void;
  }

  const { client, status, onunseal, onerror }: Props = $props();

  let pasted = $state('');
  let submitting = $state(false);
  let errorMsg: string | null = $state(null);

  // localShares overrides the prop's shares_provided once the user starts
  // submitting shares from inside the form. Until then we read straight from
  // the parent-supplied status so the progress meter mirrors the daemon.
  let localShares: number | null = $state(null);

  const sharesProvided = $derived(localShares ?? status.shares_provided);

  const percent = $derived(
    status.threshold > 0
      ? Math.min(100, Math.round((sharesProvided / status.threshold) * 100))
      : 0,
  );

  async function submit(): Promise<void> {
    errorMsg = null;
    const lines = pasted
      .split(/\r?\n/)
      .map((s) => s.trim())
      .filter((s) => s.length > 0);
    if (lines.length === 0) {
      errorMsg = 'Paste at least one share';
      return;
    }
    submitting = true;
    try {
      let lastResp: BarrierStatus | null = null;
      for (const share of lines) {
        const resp = await client.unseal({ share });
        localShares = resp.shares_provided;
        lastResp = {
          ...status,
          sealed: resp.sealed,
          shares_provided: resp.shares_provided,
          threshold: resp.threshold,
        };
        if (!resp.sealed) {
          break;
        }
      }
      pasted = '';
      if (lastResp !== null) {
        onunseal?.(lastResp);
      }
    } catch (e) {
      const err = e instanceof Error ? e : new Error(String(e));
      errorMsg = err.message;
      onerror?.(err);
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
      <span class="text-base-content/60">Unseal progress</span>
      <span class="font-mono" data-testid="shamir-progress-text">
        {sharesProvided} / {status.threshold}
      </span>
    </div>
    <progress
      class="progress progress-warning w-full"
      value={percent}
      max="100"
      aria-label="Shamir unseal progress"
      data-testid="shamir-progress-bar"
    ></progress>
  </div>

  <div class="form-control">
    <label class="label label-text" for="shamir-share-textarea">
      Recovery Key Share(s) — one per line
    </label>
    <textarea
      id="shamir-share-textarea"
      class="textarea textarea-bordered font-mono text-xs"
      rows="4"
      placeholder="Paste one share per line..."
      bind:value={pasted}
    ></textarea>
  </div>

  <button
    class="btn btn-primary btn-sm"
    disabled={submitting}
    onclick={submit}
  >
    {submitting ? 'Submitting...' : 'Submit'}
  </button>
</div>
