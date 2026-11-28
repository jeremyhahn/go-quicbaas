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
  XKMSCertificatesPanel is the Key Store page's certificate store: the
  certificates xkms holds against its keys.

  Actions:
    - List the stored certificates (key id, subject, issuer, serial, expiry),
      narrowed by the page's backend filter
    - View a certificate's PEM
    - Save a PEM certificate for a key id
    - Delete a certificate, after a confirmation that names the key id

  Each load is bound to the session and the backend that started it: a
  response that lands after a sign-out, another login or a backend change
  is dropped. Toasts name the key id only, never the PEM.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import DataTable from '../core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type { XKMSCertificateInfo, XKMSClient } from '@quicbaas/sdk';
  import { Eye, Trash2 } from 'lucide-svelte';

  let { backend = '' }: { backend?: string } = $props();

  const NOT_AVAILABLE = 'The certificate store is not available on this connection.';
  const PEM_HEADER = '-----BEGIN CERTIFICATE-----';

  const COLUMNS: Column<XKMSCertificateInfo>[] = [
    { key: 'key_id', label: 'Key ID', sortable: true, searchable: true },
    { key: 'subject', label: 'Subject', sortable: true, searchable: true },
    { key: 'issuer', label: 'Issuer', sortable: true, searchable: true },
    { key: 'serial_number', label: 'Serial', sortable: true, searchable: true },
    { key: 'not_after', label: 'Expires', sortable: true },
    { key: 'actions', label: 'Actions' },
  ];

  function errText(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  let unavailable = $state(false);

  /** Resolves the xkms client, or null when this connection has none. */
  async function xkmsClient(): Promise<XKMSClient | null> {
    const client = await getSDKClient();
    return client.xkms ?? null;
  }

  // ---------------------------------------------------------------------------
  // List
  // ---------------------------------------------------------------------------

  let certs = $state<XKMSCertificateInfo[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let listSeq = 0;

  async function loadCerts(forBackend: string): Promise<void> {
    const forSession = authStore.sessionKey;
    const seq = ++listSeq;
    loading = true;
    error = null;
    try {
      const xkms = await xkmsClient();
      if (seq !== listSeq || forSession !== authStore.sessionKey) return;
      if (!xkms) { unavailable = true; return; }
      unavailable = false;
      const res = forBackend === '' ? await xkms.listCertificates() : await xkms.listCertificates(forBackend);
      if (seq !== listSeq || forSession !== authStore.sessionKey) return;
      certs = res.certificates ?? [];
    } catch (e) {
      if (seq !== listSeq || forSession !== authStore.sessionKey) return;
      certs = [];
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else error = errText(e, 'Failed to load certificates');
    } finally {
      if (seq === listSeq && forSession === authStore.sessionKey) loading = false;
    }
  }

  function reload(): void {
    void loadCerts(backend);
  }

  function resetAll(): void {
    certs = []; error = null;
    viewing = null; actionError = null;
    showSave = false; saveError = null; savePem = '';
    confirmDelete = null;
  }

  // The list follows the backend prop and the session: a new session drops
  // what the previous one saw before it loads again.
  let lastSession = authStore.sessionKey;
  $effect(() => {
    const b = backend;
    const key = authStore.sessionKey;
    untrack(() => {
      if (key !== lastSession) {
        lastSession = key;
        resetAll();
      }
      void loadCerts(b);
    });
  });

  // ---------------------------------------------------------------------------
  // View and delete
  // ---------------------------------------------------------------------------

  let viewing = $state<{ keyId: string; pem: string } | null>(null);
  let actionError = $state<string | null>(null);
  let confirmDelete = $state<string | null>(null);
  let deleting = $state(false);

  async function view(keyId: string): Promise<void> {
    const forSession = authStore.sessionKey;
    actionError = null;
    try {
      const xkms = await xkmsClient();
      if (forSession !== authStore.sessionKey) return;
      if (!xkms) { unavailable = true; return; }
      const res = await xkms.getCertificate(keyId);
      if (forSession !== authStore.sessionKey) return;
      viewing = { keyId, pem: res.certificate_pem };
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else actionError = errText(e, 'Failed to load the certificate');
    }
  }

  async function runDelete(): Promise<void> {
    const keyId = confirmDelete;
    if (keyId === null) return;
    const forSession = authStore.sessionKey;
    actionError = null;
    deleting = true;
    try {
      const xkms = await xkmsClient();
      if (forSession !== authStore.sessionKey) return;
      if (!xkms) { unavailable = true; return; }
      await xkms.deleteCertificate(keyId);
      if (forSession !== authStore.sessionKey) return;
      confirmDelete = null;
      showToast({ message: `Certificate for "${keyId}" deleted`, type: 'success' });
      reload();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      confirmDelete = null;
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else actionError = errText(e, 'Failed to delete the certificate');
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Save
  // ---------------------------------------------------------------------------

  let showSave = $state(false);
  let saveKeyId = $state('');
  let savePem = $state('');
  let saveError = $state<string | null>(null);
  let saving = $state(false);

  function openSave(): void {
    saveKeyId = '';
    savePem = '';
    saveError = null;
    showSave = true;
  }

  function closeSave(): void {
    showSave = false;
    savePem = '';
    saveError = null;
  }

  async function submitSave(): Promise<void> {
    const keyId = saveKeyId.trim();
    const pem = savePem.trim();
    if (keyId === '') { saveError = 'Key ID is required.'; return; }
    if (!pem.includes(PEM_HEADER)) { saveError = `Enter a PEM certificate (${PEM_HEADER}).`; return; }
    const forSession = authStore.sessionKey;
    saveError = null;
    saving = true;
    try {
      const xkms = await xkmsClient();
      if (forSession !== authStore.sessionKey) return;
      if (!xkms) { saveError = NOT_AVAILABLE; return; }
      await xkms.saveCertificate(keyId, pem);
      if (forSession !== authStore.sessionKey) return;
      showToast({ message: `Certificate for "${keyId}" saved`, type: 'success' });
      closeSave();
      reload();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      saveError = errText(e, 'Failed to save the certificate');
    } finally {
      saving = false;
    }
  }
</script>

<div class="space-y-3">
  <div class="flex items-center justify-between">
    <h2 class="text-base font-semibold">Certificates</h2>
    {#if !unavailable}
      <button class="btn btn-primary btn-sm" onclick={openSave} aria-label="Save certificate">+ Save Certificate</button>
    {/if}
  </div>

  {#if unavailable}
    <div class="alert alert-info text-sm" data-testid="xkms-certs-not-available">{NOT_AVAILABLE}</div>
  {:else}
    {#if error}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{error}</span>
        <button class="btn btn-xs btn-ghost" onclick={reload}>Retry</button>
      </div>
    {/if}
    {#if actionError}
      <div class="alert alert-error text-sm" role="alert">{actionError}</div>
    {/if}

    {#if loading}
      <div class="flex justify-center py-4">
        <span class="loading loading-spinner loading-md"></span>
      </div>
    {:else}
      <DataTable
        columns={COLUMNS}
        rows={certs}
        rowKey={(c: XKMSCertificateInfo) => c.key_id}
        ariaLabel="Key Store certificates"
        searchPlaceholder="Search certificates..."
        searchLabel="Search certificates"
        noMatchText="No certificates match the search."
        emptyTitle="No certificates found."
        emptyDescription="Save a certificate for a key to get started."
      >
        {#snippet row(c: XKMSCertificateInfo)}
          <tr class="hover">
            <td class="font-mono text-xs font-medium">{c.key_id}</td>
            <td class="font-mono text-xs">{c.subject || '—'}</td>
            <td class="font-mono text-xs">{c.issuer || '—'}</td>
            <td class="font-mono text-xs">{c.serial_number || '—'}</td>
            <td class="font-mono text-xs">{c.not_after || '—'}</td>
            <td>
              <div class="flex gap-1">
                <button
                  class="btn btn-ghost btn-xs"
                  onclick={() => { void view(c.key_id); }}
                  title="View certificate"
                  aria-label={`View certificate ${c.key_id}`}
                >
                  <Eye class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  onclick={() => { actionError = null; confirmDelete = c.key_id; }}
                  title="Delete certificate"
                  aria-label={`Delete certificate ${c.key_id}`}
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}
  {/if}
</div>

{#if viewing}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Certificate ${viewing.keyId}`}>
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-4">Certificate <span class="font-mono">{viewing.keyId}</span></h3>
      <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="cert-pem">{viewing.pem}</pre>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { viewing = null; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { viewing = null; }} onkeydown={() => { viewing = null; }}></div>
  </div>
{/if}

{#if confirmDelete !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete certificate dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete certificate</h3>
      <p class="text-sm">Delete the certificate stored for <span class="font-mono">{confirmDelete}</span>? The key itself stays.</p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { confirmDelete = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deleting} onclick={() => void runDelete()}>
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { confirmDelete = null; }} onkeydown={() => { confirmDelete = null; }}></div>
  </div>
{/if}

{#if showSave}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Save certificate dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Save Certificate</h3>

      {#if saveError}
        <div class="alert alert-error text-sm mb-3" role="alert">{saveError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="xkms-cert-save-id">Key ID</label>
          <input id="xkms-cert-save-id" class="input input-bordered input-sm" type="text" bind:value={saveKeyId} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-cert-save-pem">Certificate PEM</label>
          <textarea
            id="xkms-cert-save-pem"
            class="textarea textarea-bordered font-mono text-xs h-40"
            bind:value={savePem}
            placeholder={PEM_HEADER}
          ></textarea>
        </div>
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeSave}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={saving} onclick={() => void submitSave()}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeSave} onkeydown={closeSave}></div>
  </div>
{/if}
