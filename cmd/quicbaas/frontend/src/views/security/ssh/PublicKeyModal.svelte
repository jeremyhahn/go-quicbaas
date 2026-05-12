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
  PublicKeyModal displays the OpenSSH public key for a given SSHKey entry.

  Props:
    key     — The SSHKey to display. public_key field is rendered in a <pre>.
    onClose — Called when the modal is dismissed.
-->
<script lang="ts">
  import { showToast } from '../../../stores/toast.svelte.ts';
  import type { SSHKey } from '@quicbaas/sdk';

  let {
    key,
    onClose,
  }: {
    /** The SSH key whose public key to display. */
    key: SSHKey;
    /** Called when the modal should close. */
    onClose: () => void;
  } = $props();

  const publicKey = $derived(key.public_key ?? key.public_key_openssh ?? '');

  async function copyPublicKey(): Promise<void> {
    try {
      await navigator.clipboard.writeText(publicKey);
      showToast('success', 'Public key copied to clipboard');
    } catch {
      showToast('error', 'Copy failed — check clipboard permissions');
    }
  }
</script>

<div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Public key for ${key.name}`}>
  <div class="modal-box max-w-2xl">
    <h3 class="text-base font-semibold mb-1">Public Key</h3>
    <p class="text-sm text-base-content/50 mb-4">
      <span class="font-mono">{key.name}</span>
      {#if key.algorithm}
        &nbsp;·&nbsp; {key.algorithm}
      {/if}
    </p>

    {#if publicKey}
      <pre
        class="bg-base-200 rounded-lg p-4 text-xs font-mono overflow-x-auto whitespace-pre-wrap break-all leading-relaxed"
        aria-label="Public key text"
      >{publicKey}</pre>
    {:else}
      <div class="alert alert-warning text-sm">Public key data is not available for this entry.</div>
    {/if}

    <div class="modal-action">
      <button
        class="btn btn-ghost btn-sm"
        onclick={onClose}
        aria-label="Close public key dialog"
      >
        Close
      </button>
      {#if publicKey}
        <button
          class="btn btn-primary btn-sm"
          onclick={copyPublicKey}
          aria-label="Copy public key to clipboard"
        >
          Copy
        </button>
      {/if}
    </div>
  </div>
  <div
    class="modal-backdrop"
    role="none"
    onclick={onClose}
    onkeydown={onClose}
  ></div>
</div>
