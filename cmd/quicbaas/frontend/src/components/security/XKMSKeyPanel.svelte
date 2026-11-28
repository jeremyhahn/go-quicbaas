<!--
  Copyright (c) 2026 Jeremy Hahn
  Copyright (c) 2026 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  XKMSKeyPanel manages one xkms key: its details and public key, its
  versions (enable or disable one or all), rotation, deletion, and the
  encrypt, decrypt, sign and verify operations on UTF-8 text. Each tab loads
  on first open and keeps its own loading and error state; a response that
  arrives after the key changed is dropped. Key material and plaintext are
  shown in the panel only, never logged or toasted.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { formatDateTime } from '../../lib/time.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type {
    XKMSKeysClient,
    XKMSKeyInfo,
    XKMSKeyVersion,
    XKMSDecryptRequest,
  } from '@quicbaas/sdk';
  import { X, RotateCw, Trash2, Lock, Unlock, PenLine, ShieldCheck } from 'lucide-svelte';

  interface Props {
    keyId: string;
    backend?: string;
    onclose: () => void;
    ondeleted?: (keyId: string) => void;
  }

  let { keyId, backend = '', onclose, ondeleted }: Props = $props();

  type Tab = 'details' | 'versions' | 'operations';
  const TABS: { id: Tab; label: string }[] = [
    { id: 'details', label: 'Details' },
    { id: 'versions', label: 'Versions' },
    { id: 'operations', label: 'Operations' },
  ];
  const NOT_AVAILABLE = 'Key Store is not available on this connection.';

  let tab = $state<Tab>('details');
  let unavailable = $state(false);

  function errText(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  function utf8ToBase64(text: string): string {
    let bin = '';
    for (const b of new TextEncoder().encode(text)) bin += String.fromCharCode(b);
    return btoa(bin);
  }

  /** Decodes base64 to UTF-8 text; bytes that are not UTF-8 stay base64. */
  function base64ToText(b64: string): { text: string; utf8: boolean } {
    try {
      const bin = atob(b64);
      const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
      return { text: new TextDecoder('utf-8', { fatal: true }).decode(bytes), utf8: true };
    } catch {
      return { text: b64, utf8: false };
    }
  }

  function isEnabled(status: string): boolean {
    const s = status.toLowerCase();
    return s === 'enabled' || s === 'active';
  }

  // ---------------------------------------------------------------------------
  // Generation guard: a key change bumps the generation, so a response still
  // in flight for the old key is dropped.
  // ---------------------------------------------------------------------------

  let generation = 0;
  let loadedTabs = new Set<Tab>();

  /** Resolves the key client, or null when this connection has none. */
  async function keysClient(): Promise<XKMSKeysClient | null> {
    const client = await getSDKClient();
    const keys: XKMSKeysClient | null = client.xkmsKeys ?? null;
    return keys;
  }

  /** Records an error; an unsupported transport becomes the not-available state. */
  function failed(err: unknown, set: (msg: string) => void): void {
    if (err instanceof TransportNotSupportedError) unavailable = true;
    else set(errText(err));
  }

  // ---------------------------------------------------------------------------
  // Details
  // ---------------------------------------------------------------------------

  let info = $state<XKMSKeyInfo | null>(null);
  let detailsLoading = $state(false);
  let detailsError = $state('');

  async function loadDetails(id: string, gen: number): Promise<void> {
    detailsLoading = true;
    detailsError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      const res = backend !== '' ? await keys.getKey(id, backend) : await keys.getKey(id);
      if (gen !== generation) return;
      info = res;
    } catch (err: unknown) {
      if (gen !== generation) return;
      failed(err, (m) => { detailsError = m; });
    } finally {
      if (gen === generation) detailsLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Versions
  // ---------------------------------------------------------------------------

  let versions = $state<XKMSKeyVersion[]>([]);
  let versionsLoading = $state(false);
  let versionsError = $state('');
  let versionBusy = $state(false);
  let bulkConfirm = $state<'enable' | 'disable' | null>(null);

  async function loadVersions(id: string, gen: number): Promise<void> {
    versionsLoading = true;
    versionsError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      const res = await keys.listKeyVersions(id);
      if (gen !== generation) return;
      versions = res.versions ?? [];
    } catch (err: unknown) {
      if (gen !== generation) return;
      failed(err, (m) => { versionsError = m; });
    } finally {
      if (gen === generation) versionsLoading = false;
    }
  }

  async function toggleVersion(v: XKMSKeyVersion): Promise<void> {
    const id = keyId;
    const gen = generation;
    versionBusy = true;
    versionsError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      if (isEnabled(v.status)) await keys.disableKeyVersion(id, v.version);
      else await keys.enableKeyVersion(id, v.version);
      if (gen !== generation) return;
      await loadVersions(id, gen);
    } catch (err: unknown) {
      if (gen !== generation) return;
      failed(err, (m) => { versionsError = m; });
    } finally {
      if (gen === generation) versionBusy = false;
    }
  }

  async function confirmBulk(): Promise<void> {
    const action = bulkConfirm;
    if (!action) return;
    const id = keyId;
    const gen = generation;
    versionBusy = true;
    versionsError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      const res = action === 'enable' ? await keys.enableAllKeyVersions(id) : await keys.disableAllKeyVersions(id);
      if (gen !== generation) return;
      bulkConfirm = null;
      showToast({ message: res.message || `${res.count} versions ${action}d`, type: 'success' });
      await loadVersions(id, gen);
    } catch (err: unknown) {
      if (gen !== generation) return;
      bulkConfirm = null;
      failed(err, (m) => { versionsError = m; });
    } finally {
      if (gen === generation) versionBusy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotate and delete
  // ---------------------------------------------------------------------------

  let confirmAction = $state<'rotate' | 'delete' | null>(null);
  let actionBusy = $state(false);
  let actionError = $state('');
  let rotatedVersion = $state<number | null>(null);

  async function runConfirmed(): Promise<void> {
    const action = confirmAction;
    if (!action) return;
    const id = keyId;
    const gen = generation;
    actionBusy = true;
    actionError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      if (action === 'rotate') {
        const res = await keys.rotateKey(id);
        if (gen !== generation) return;
        confirmAction = null;
        rotatedVersion = res.key_version ?? null;
        showToast({ message: `Key "${id}" rotated`, type: 'success' });
        void loadDetails(id, gen);
        loadedTabs.add('versions');
        void loadVersions(id, gen);
      } else {
        await keys.deleteKey(id);
        if (gen !== generation) return;
        confirmAction = null;
        ondeleted?.(id);
      }
    } catch (err: unknown) {
      if (gen !== generation) return;
      confirmAction = null;
      failed(err, (m) => { actionError = m; });
    } finally {
      if (gen === generation) actionBusy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Operations: encrypt, decrypt, sign, verify
  // ---------------------------------------------------------------------------

  let plainText = $state('');
  let cipherText = $state('');
  let nonce = $state('');
  let tag = $state('');
  let decrypted = $state<{ text: string; utf8: boolean } | null>(null);
  let cryptoError = $state('');
  let cryptoBusy = $state(false);

  async function encrypt(): Promise<void> {
    if (plainText === '') { cryptoError = 'Enter the text to encrypt.'; return; }
    const id = keyId;
    const gen = generation;
    cryptoBusy = true;
    cryptoError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      const res = await keys.encrypt(id, { plaintext: utf8ToBase64(plainText) });
      if (gen !== generation) return;
      cipherText = res.ciphertext;
      nonce = res.nonce ?? '';
      tag = res.tag ?? '';
      decrypted = null;
    } catch (err: unknown) {
      if (gen !== generation) return;
      failed(err, (m) => { cryptoError = m; });
    } finally {
      if (gen === generation) cryptoBusy = false;
    }
  }

  async function decrypt(): Promise<void> {
    const ct = cipherText.trim();
    if (ct === '') { cryptoError = 'Enter the ciphertext to decrypt.'; return; }
    const id = keyId;
    const gen = generation;
    cryptoBusy = true;
    cryptoError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      const req: XKMSDecryptRequest = { ciphertext: ct };
      if (nonce.trim() !== '') req.nonce = nonce.trim();
      if (tag.trim() !== '') req.tag = tag.trim();
      const res = await keys.decrypt(id, req);
      if (gen !== generation) return;
      decrypted = base64ToText(res.plaintext);
    } catch (err: unknown) {
      if (gen !== generation) return;
      failed(err, (m) => { cryptoError = m; });
    } finally {
      if (gen === generation) cryptoBusy = false;
    }
  }

  let signText = $state('');
  let signature = $state('');
  let verified = $state<boolean | null>(null);
  let signError = $state('');
  let signBusy = $state(false);

  async function sign(): Promise<void> {
    if (signText === '') { signError = 'Enter the text to sign.'; return; }
    const id = keyId;
    const gen = generation;
    signBusy = true;
    signError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      const res = await keys.sign(id, { data: utf8ToBase64(signText) });
      if (gen !== generation) return;
      signature = res.signature;
      verified = null;
    } catch (err: unknown) {
      if (gen !== generation) return;
      failed(err, (m) => { signError = m; });
    } finally {
      if (gen === generation) signBusy = false;
    }
  }

  async function verify(): Promise<void> {
    const sig = signature.trim();
    if (signText === '' || sig === '') { signError = 'Enter the text and the signature to verify.'; return; }
    const id = keyId;
    const gen = generation;
    signBusy = true;
    signError = '';
    try {
      const keys = await keysClient();
      if (gen !== generation) return;
      if (!keys) { unavailable = true; return; }
      const res = await keys.verify(id, { data: utf8ToBase64(signText), signature: sig });
      if (gen !== generation) return;
      verified = res.valid;
    } catch (err: unknown) {
      if (gen !== generation) return;
      failed(err, (m) => { signError = m; });
    } finally {
      if (gen === generation) signBusy = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Tabs and key changes
  // ---------------------------------------------------------------------------

  function ensureLoaded(t: Tab): void {
    if (loadedTabs.has(t)) return;
    loadedTabs.add(t);
    const id = keyId;
    const gen = generation;
    if (t === 'details') void loadDetails(id, gen);
    else if (t === 'versions') void loadVersions(id, gen);
  }

  function selectTab(t: Tab): void {
    tab = t;
    ensureLoaded(t);
  }

  function resetAll(): void {
    unavailable = false;
    info = null; detailsLoading = false; detailsError = '';
    versions = []; versionsLoading = false; versionsError = ''; versionBusy = false; bulkConfirm = null;
    confirmAction = null; actionBusy = false; actionError = ''; rotatedVersion = null;
    plainText = ''; cipherText = ''; nonce = ''; tag = ''; decrypted = null; cryptoError = ''; cryptoBusy = false;
    signText = ''; signature = ''; verified = null; signError = ''; signBusy = false;
  }

  $effect(() => {
    void keyId;
    void backend;
    untrack(() => {
      generation += 1;
      loadedTabs = new Set<Tab>();
      resetAll();
      ensureLoaded(tab);
    });
  });
</script>

<div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Manage key ${keyId}`}>
  <div class="modal-box max-w-2xl">
    <div class="flex items-start justify-between mb-3">
      <div>
        <h3 class="text-base font-semibold">Manage key</h3>
        <p class="text-xs font-mono text-base-content/50">{keyId}{backend ? ` on ${backend}` : ''}</p>
      </div>
      <div class="flex gap-1">
        {#if !unavailable}
          <button
            class="btn btn-ghost btn-sm gap-1"
            disabled={actionBusy}
            onclick={() => { confirmAction = 'rotate'; }}
            aria-label="Rotate key"
          ><RotateCw class="h-4 w-4" /> Rotate</button>
          <button
            class="btn btn-ghost btn-sm gap-1 text-error"
            disabled={actionBusy}
            onclick={() => { confirmAction = 'delete'; }}
            aria-label="Delete key"
          ><Trash2 class="h-4 w-4" /> Delete</button>
        {/if}
        <button class="btn btn-ghost btn-sm btn-square" onclick={onclose} aria-label="Close key panel" title="Close">
          <X class="h-4 w-4" />
        </button>
      </div>
    </div>

    {#if confirmAction}
      <div class="alert alert-warning text-sm mb-3 flex items-center justify-between" role="alertdialog" aria-label="Confirm key action">
        <span>
          {#if confirmAction === 'rotate'}
            Rotate <span class="font-mono">{keyId}</span>? A new key version becomes current.
          {:else}
            Delete <span class="font-mono">{keyId}</span>? This cannot be undone, and data encrypted with it is lost.
          {/if}
        </span>
        <span class="flex gap-1">
          <button class="btn btn-ghost btn-xs" onclick={() => { confirmAction = null; }} aria-label="Cancel key action">Cancel</button>
          <button
            class="btn btn-xs {confirmAction === 'delete' ? 'btn-error' : 'btn-primary'}"
            disabled={actionBusy}
            onclick={() => void runConfirmed()}
            aria-label={confirmAction === 'rotate' ? 'Confirm rotate' : 'Confirm delete'}
          >{confirmAction === 'rotate' ? 'Rotate' : 'Delete'}</button>
        </span>
      </div>
    {/if}
    {#if actionError}
      <div class="alert alert-error text-sm mb-3" role="alert">{actionError}</div>
    {/if}
    {#if rotatedVersion !== null}
      <div class="alert alert-success text-sm mb-3" data-testid="rotated-version">Rotated to version {rotatedVersion}</div>
    {/if}

    <div role="tablist" class="tabs tabs-bordered mb-4">
      {#each TABS as t (t.id)}
        <button
          role="tab"
          class="tab {tab === t.id ? 'tab-active' : ''}"
          aria-selected={tab === t.id}
          onclick={() => selectTab(t.id)}
        >{t.label}</button>
      {/each}
    </div>

    {#if unavailable}
      <div class="alert alert-info text-sm">{NOT_AVAILABLE}</div>
    {:else if tab === 'details'}
      <section aria-label="Key details">
        {#if detailsLoading}
          <div class="flex justify-center py-6"><span class="loading loading-spinner loading-md"></span></div>
        {:else}
          {#if detailsError}
            <div class="alert alert-error text-sm mb-3" role="alert">{detailsError}</div>
          {/if}
          {#if info}
            <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
              <dt class="text-base-content/60">Key ID</dt><dd class="font-mono text-xs">{info.key_id}</dd>
              <dt class="text-base-content/60">Type</dt><dd class="font-mono text-xs">{info.key_type}</dd>
              <dt class="text-base-content/60">Algorithm</dt><dd class="font-mono text-xs">{info.algorithm || '—'}</dd>
              <dt class="text-base-content/60">Backend</dt><dd class="font-mono text-xs">{info.backend}</dd>
              {#if info.key_size}
                <dt class="text-base-content/60">Key size</dt><dd class="font-mono text-xs">{info.key_size}</dd>
              {/if}
              {#if info.curve}
                <dt class="text-base-content/60">Curve</dt><dd class="font-mono text-xs">{info.curve}</dd>
              {/if}
              {#if info.partition}
                <dt class="text-base-content/60">Partition</dt><dd class="font-mono text-xs">{info.partition}</dd>
              {/if}
              {#if info.key_version !== undefined}
                <dt class="text-base-content/60">Version</dt><dd class="font-mono text-xs" data-testid="key-version">{info.key_version}</dd>
              {/if}
            </dl>
            {#if info.public_key_pem}
              <div class="mt-3">
                <div class="text-xs text-base-content/60 mb-1">Public key</div>
                <pre class="bg-base-200 rounded p-2 text-xs overflow-x-auto" data-testid="public-key-pem">{info.public_key_pem}</pre>
              </div>
            {/if}
          {/if}
        {/if}
      </section>
    {:else if tab === 'versions'}
      <section aria-label="Key versions">
        {#if versionsError}
          <div class="alert alert-error text-sm mb-3" role="alert">{versionsError}</div>
        {/if}
        <div class="flex gap-2 mb-3">
          <button class="btn btn-sm btn-outline gap-1" disabled={versionBusy} onclick={() => { bulkConfirm = 'enable'; }} aria-label="Enable all versions">
            <Unlock class="h-4 w-4" /> Enable all
          </button>
          <button class="btn btn-sm btn-outline gap-1" disabled={versionBusy} onclick={() => { bulkConfirm = 'disable'; }} aria-label="Disable all versions">
            <Lock class="h-4 w-4" /> Disable all
          </button>
        </div>
        {#if bulkConfirm}
          <div class="alert alert-warning text-sm mb-3 flex items-center justify-between" role="alertdialog" aria-label="Confirm all versions">
            <span>{bulkConfirm === 'enable' ? 'Enable' : 'Disable'} every version of <span class="font-mono">{keyId}</span>?</span>
            <span class="flex gap-1">
              <button class="btn btn-ghost btn-xs" onclick={() => { bulkConfirm = null; }} aria-label="Cancel all versions">Cancel</button>
              <button
                class="btn btn-primary btn-xs"
                disabled={versionBusy}
                onclick={() => void confirmBulk()}
                aria-label={bulkConfirm === 'enable' ? 'Confirm enable all' : 'Confirm disable all'}
              >{bulkConfirm === 'enable' ? 'Enable all' : 'Disable all'}</button>
            </span>
          </div>
        {/if}
        {#if versionsLoading}
          <div class="flex justify-center py-6"><span class="loading loading-spinner loading-md"></span></div>
        {:else if versions.length === 0}
          <p class="text-sm text-base-content/60">No versions found.</p>
        {:else}
          <table class="table table-sm" aria-label="Versions">
            <thead><tr><th>Version</th><th>Status</th><th>Created</th><th></th></tr></thead>
            <tbody>
              {#each versions as v (v.version)}
                <tr>
                  <td class="font-mono text-xs">v{v.version}</td>
                  <td><span class="badge badge-xs {isEnabled(v.status) ? 'badge-success' : 'badge-ghost'}">{v.status}</span></td>
                  <td class="text-xs text-base-content/50">{v.created_at ? formatDateTime(v.created_at) : '—'}</td>
                  <td>
                    <button
                      class="btn btn-ghost btn-xs"
                      disabled={versionBusy}
                      onclick={() => void toggleVersion(v)}
                      aria-label={`${isEnabled(v.status) ? 'Disable' : 'Enable'} version ${v.version}`}
                    >{isEnabled(v.status) ? 'Disable' : 'Enable'}</button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>
    {:else}
      <section aria-label="Encrypt and decrypt with the key" class="space-y-2 mb-6">
        <h4 class="text-sm font-semibold">Encrypt and decrypt</h4>
        {#if cryptoError}
          <div class="alert alert-error text-sm" role="alert">{cryptoError}</div>
        {/if}
        <label class="label label-text" for="xkms-plaintext">Plaintext</label>
        <textarea id="xkms-plaintext" class="textarea textarea-bordered w-full text-sm" rows="2" bind:value={plainText}></textarea>
        <button class="btn btn-sm btn-primary gap-1" disabled={cryptoBusy} onclick={() => void encrypt()} aria-label="Encrypt">
          <Lock class="h-4 w-4" /> Encrypt
        </button>
        <label class="label label-text" for="xkms-ciphertext">Ciphertext</label>
        <textarea id="xkms-ciphertext" class="textarea textarea-bordered w-full font-mono text-xs" rows="2" bind:value={cipherText}></textarea>
        <div class="grid grid-cols-2 gap-2">
          <div>
            <label class="label label-text" for="xkms-nonce">Nonce</label>
            <input id="xkms-nonce" class="input input-bordered input-sm w-full font-mono text-xs" bind:value={nonce} />
          </div>
          <div>
            <label class="label label-text" for="xkms-tag">Tag</label>
            <input id="xkms-tag" class="input input-bordered input-sm w-full font-mono text-xs" bind:value={tag} />
          </div>
        </div>
        <button class="btn btn-sm btn-outline gap-1" disabled={cryptoBusy} onclick={() => void decrypt()} aria-label="Decrypt">
          <Unlock class="h-4 w-4" /> Decrypt
        </button>
        {#if decrypted}
          <div class="text-sm">
            <span class="text-base-content/60">Decrypted{decrypted.utf8 ? '' : ' (base64)'}:</span>
            <span class="font-mono" data-testid="decrypted-text">{decrypted.text}</span>
          </div>
        {/if}
      </section>

      <section aria-label="Sign and verify with the key" class="space-y-2">
        <h4 class="text-sm font-semibold">Sign and verify</h4>
        {#if signError}
          <div class="alert alert-error text-sm" role="alert">{signError}</div>
        {/if}
        <label class="label label-text" for="xkms-sign-data">Data</label>
        <textarea id="xkms-sign-data" class="textarea textarea-bordered w-full text-sm" rows="2" bind:value={signText}></textarea>
        <button class="btn btn-sm btn-primary gap-1" disabled={signBusy} onclick={() => void sign()} aria-label="Sign">
          <PenLine class="h-4 w-4" /> Sign
        </button>
        <label class="label label-text" for="xkms-signature">Signature</label>
        <textarea id="xkms-signature" class="textarea textarea-bordered w-full font-mono text-xs" rows="2" bind:value={signature}></textarea>
        <button class="btn btn-sm btn-outline gap-1" disabled={signBusy} onclick={() => void verify()} aria-label="Verify">
          <ShieldCheck class="h-4 w-4" /> Verify
        </button>
        {#if verified !== null}
          <div class="text-sm" data-testid="verify-result">
            {#if verified}<span class="badge badge-success">Valid</span>{:else}<span class="badge badge-error">Invalid</span>{/if}
          </div>
        {/if}
      </section>
    {/if}
  </div>
  <div class="modal-backdrop" role="none" onclick={onclose} onkeydown={onclose}></div>
</div>
