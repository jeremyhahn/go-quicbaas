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
  PublicKeyModal displays the details and the exported OpenSSH public key of
  one SSH key.

  On open it calls keys.getScoped (GET /api/v1/ssh/keys/{scope}/{owner}/{name})
  for the details and keys.exportPublic (GET .../{name}/public) for the line
  and fingerprint. Until the export answers, or if it fails, the modal shows
  the public_key from the list answer; each failed call shows an inline error.

  Props:
    key     — The SSHKey row to display.
    scope   — The key's scope.
    owner   — The key's owner.
    onClose — Called when the modal is dismissed.
-->
<script lang="ts">
  import { getSDKClient } from '../../../stores/sdkClient.ts';
  import { showToast } from '../../../stores/toast.svelte.ts';
  import type { SSHKey, SSHPublicKey, SSHScope } from '@quicbaas/sdk';

  let {
    key,
    scope,
    owner,
    onClose,
  }: {
    /** The SSH key whose public key to display. */
    key: SSHKey;
    /** The key's scope. */
    scope: SSHScope;
    /** The key's owner. */
    owner: string;
    /** Called when the modal should close. */
    onClose: () => void;
  } = $props();

  let details = $state<SSHKey | null>(null);
  let detailsError: string | null = $state(null);
  let exported = $state<SSHPublicKey | null>(null);
  let exportError: string | null = $state(null);
  let loading = $state(false);

  const shown = $derived(details ?? key);
  const publicKey = $derived(exported?.line ?? key.public_key ?? key.public_key_openssh ?? '');

  async function load(): Promise<void> {
    loading = true;
    try {
      const client = await getSDKClient();
      const [d, x] = await Promise.allSettled([
        client.ssh.keys.getScoped(scope, owner, key.name),
        client.ssh.keys.exportPublic(scope, owner, key.name),
      ]);
      if (d.status === 'fulfilled') {
        details = d.value;
      } else {
        detailsError = d.reason instanceof Error ? d.reason.message : 'Failed to load key details';
      }
      if (x.status === 'fulfilled') {
        exported = x.value;
      } else {
        exportError = x.reason instanceof Error ? x.reason.message : 'Failed to export the public key';
      }
    } catch (e) {
      const msg = e instanceof Error ? e.message : 'Failed to load the key';
      detailsError = msg;
      exportError = msg;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void load();
  });

  function formatDate(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

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
      {#if shown.algorithm}
        &nbsp;·&nbsp; {shown.algorithm}
      {/if}
      {#if loading}
        <span class="loading loading-spinner loading-xs ml-2" aria-label="Loading key details"></span>
      {/if}
    </p>

    {#if detailsError}
      <div class="alert alert-error text-sm mb-3" role="alert">Key details: {detailsError}</div>
    {/if}
    {#if exportError}
      <div class="alert alert-error text-sm mb-3" role="alert">Public export: {exportError}</div>
    {/if}

    <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm mb-4" aria-label="Key details">
      <dt class="text-base-content/50">Name</dt><dd class="font-mono">{shown.name}</dd>
      <dt class="text-base-content/50">Scope</dt><dd>{shown.scope ?? scope}</dd>
      <dt class="text-base-content/50">Owner</dt><dd class="font-mono">{(shown.owner ?? owner) || '—'}</dd>
      <dt class="text-base-content/50">Algorithm</dt><dd>{shown.algorithm || '—'}</dd>
      <dt class="text-base-content/50">Fingerprint</dt><dd class="font-mono text-xs break-all">{shown.fingerprint || '—'}</dd>
      <dt class="text-base-content/50">Created</dt><dd>{formatDate(shown.created_at)}</dd>
      <dt class="text-base-content/50">Comment</dt><dd>{shown.comment || '—'}</dd>
      {#if exported}
        <dt class="text-base-content/50">Exported fingerprint</dt>
        <dd class="font-mono text-xs break-all">{exported.fingerprint}</dd>
      {/if}
    </dl>

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
