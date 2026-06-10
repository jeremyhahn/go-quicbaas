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
  ShamirSharesReveal displays freshly minted Shamir shares exactly once after a
  barrier is initialized. The shares are the ONLY copy of the unseal material:
  if the operator dismisses this panel without saving them, the barrier can
  never be unsealed after a restart. A save-acknowledgement checkbox therefore
  gates the dismiss action — the parent's ondismiss handler only fires once the
  operator confirms they have securely stored every share.

  Each share offers a one-click copy with a transient per-item confirmation.
  This mirrors the share-reveal step of qrdb's ServerInitWizard so both
  frontends present the same custody-critical UX.
-->
<script lang="ts">
  interface Props {
    /** The freshly minted shares, shown exactly once. */
    shares: string[];
    /** Threshold (M) required to reconstruct the master key. */
    threshold: number;
    /** Emitted when the operator acknowledges and dismisses the panel. */
    ondismiss?: () => void;
  }

  const { shares, threshold, ondismiss }: Props = $props();

  let acknowledged = $state(false);
  let copiedIndex: number | null = $state(null);

  // copyShare copies a single share to the clipboard and flashes a transient
  // per-item confirmation. Clipboard failures are swallowed: the operator can
  // always select the share text manually.
  async function copyShare(text: string, index: number): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      copiedIndex = index;
      setTimeout(() => {
        if (copiedIndex === index) copiedIndex = null;
      }, 1500);
    } catch {
      // Clipboard unavailable (e.g. insecure context) — manual copy still works.
    }
  }

  function dismiss(): void {
    if (!acknowledged) return;
    ondismiss?.();
  }
</script>

<div class="space-y-4" data-testid="shamir-shares-reveal">
  <div class="alert alert-warning text-sm" role="alert">
    <span>
      These {shares.length} Shamir key shares are displayed
      <strong>only once</strong>. Any {threshold} of them are required to
      unseal this barrier after a restart. Store them securely and distribute
      them to separate custodians — if lost, the encrypted data cannot be
      recovered.
    </span>
  </div>

  <div class="form-control">
    <span class="label-text font-semibold">Key shares ({shares.length})</span>
    <div class="mt-1 flex flex-col gap-2">
      {#each shares as share, i (i)}
        <div class="bg-base-200 flex items-center gap-2 rounded-lg p-3">
          <span class="text-base-content/50 w-6 shrink-0 text-xs">#{i + 1}</span>
          <span
            class="flex-1 break-all font-mono text-xs"
            data-testid="shamir-share-reveal-item"
          >{share}</span>
          <button
            class="btn btn-ghost btn-xs"
            title="Copy share {i + 1}"
            onclick={() => { void copyShare(share, i); }}
            data-testid="shamir-share-copy-{i}"
          >
            {copiedIndex === i ? 'Copied' : 'Copy'}
          </button>
        </div>
      {/each}
    </div>
  </div>

  <label class="label cursor-pointer justify-start gap-3">
    <input
      type="checkbox"
      class="checkbox checkbox-primary"
      bind:checked={acknowledged}
      data-testid="shamir-shares-acknowledge"
    />
    <span class="label-text">
      I have securely saved all {shares.length} key shares.
    </span>
  </label>

  <button
    class="btn btn-primary btn-sm"
    disabled={!acknowledged}
    onclick={dismiss}
    data-testid="shamir-shares-dismiss"
  >
    Done
  </button>
</div>
