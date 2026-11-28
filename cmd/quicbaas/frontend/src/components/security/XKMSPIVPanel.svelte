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
  XKMSPIVPanel is the Key Store page's PIV slots section.

  Actions:
    - List the slots (slot, name, certificate and key flags, subject,
      algorithm, expiry), narrowed by the page's backend filter
    - View a slot certificate's PEM
    - Store a PEM certificate in a slot
    - Delete a slot certificate, after a confirmation that names the slot
    - Generate a key in a slot; on a slot that already holds a key, only after
      a confirmation that names the slot
    - Generate a CSR from a slot's key

  The backend is sent on every call only when one is chosen. Each load is
  bound to the session and the backend that started it: a response that
  lands after a sign-out, another login or a backend change is dropped.
  Toasts name the slot only, never PEM text.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import DataTable from '../core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type {
    GeneratePIVCSRRequest,
    GeneratePIVKeyRequest,
    GeneratePIVKeyResponse,
    PIVClient,
    PIVKeyAlgorithm,
    PIVSlotStatus,
  } from '@quicbaas/sdk';
  import { Eye, Upload, Trash2, KeyRound, FileSignature } from 'lucide-svelte';

  let { backend = '' }: { backend?: string } = $props();

  const NOT_AVAILABLE = 'PIV slots are not available on this connection.';
  const PEM_HEADER = '-----BEGIN CERTIFICATE-----';
  const ALGORITHMS: PIVKeyAlgorithm[] = ['rsa2048', 'rsa4096', 'ecdsap256', 'ecdsap384', 'ed25519'];

  const COLUMNS: Column<PIVSlotStatus>[] = [
    { key: 'slot', label: 'Slot', sortable: true, searchable: true },
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'has_cert', label: 'Certificate' },
    { key: 'has_key', label: 'Key' },
    { key: 'subject', label: 'Subject', sortable: true, searchable: true },
    { key: 'algorithm', label: 'Algorithm', sortable: true },
    { key: 'not_after', label: 'Expires', sortable: true },
    { key: 'actions', label: 'Actions' },
  ];

  function errText(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  let unavailable = $state(false);

  /** Resolves the PIV client, or null when this connection has none. */
  async function pivClient(): Promise<PIVClient | null> {
    const client = await getSDKClient();
    return client.piv ?? null;
  }

  // ---------------------------------------------------------------------------
  // List
  // ---------------------------------------------------------------------------

  let slots = $state<PIVSlotStatus[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let listSeq = 0;

  async function loadSlots(forBackend: string): Promise<void> {
    const forSession = authStore.sessionKey;
    const seq = ++listSeq;
    loading = true;
    error = null;
    try {
      const piv = await pivClient();
      if (seq !== listSeq || forSession !== authStore.sessionKey) return;
      if (!piv) { unavailable = true; return; }
      unavailable = false;
      const res = forBackend === '' ? await piv.listSlots() : await piv.listSlots(forBackend);
      if (seq !== listSeq || forSession !== authStore.sessionKey) return;
      slots = res.slots ?? [];
    } catch (e) {
      if (seq !== listSeq || forSession !== authStore.sessionKey) return;
      slots = [];
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else error = errText(e, 'Failed to load the PIV slots');
    } finally {
      if (seq === listSeq && forSession === authStore.sessionKey) loading = false;
    }
  }

  function reload(): void {
    void loadSlots(backend);
  }

  function resetAll(): void {
    slots = []; error = null;
    viewing = null; actionError = null;
    confirmDelete = null;
    storeSlot = null; storePem = ''; storeError = null;
    keySlot = null; keyError = null; confirmReplace = false; keyResult = null;
    csrSlot = null; csrError = null; csrResult = null;
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
      void loadSlots(b);
    });
  });

  /** The backend for a request body, only when one is chosen. */
  function withBackend<T extends object>(req: T): T & { backend?: string } {
    return backend === '' ? req : { ...req, backend };
  }

  // ---------------------------------------------------------------------------
  // View and delete
  // ---------------------------------------------------------------------------

  let viewing = $state<{ slot: string; pem: string } | null>(null);
  let actionError = $state<string | null>(null);
  let confirmDelete = $state<string | null>(null);
  let deleting = $state(false);

  async function view(slot: string): Promise<void> {
    const forSession = authStore.sessionKey;
    actionError = null;
    try {
      const piv = await pivClient();
      if (forSession !== authStore.sessionKey) return;
      if (!piv) { unavailable = true; return; }
      const res = await piv.getCertificate(slot, withBackend({ format: 'pem' as const }));
      if (forSession !== authStore.sessionKey) return;
      viewing = { slot, pem: res.certificate };
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else actionError = errText(e, 'Failed to load the slot certificate');
    }
  }

  async function runDelete(): Promise<void> {
    const slot = confirmDelete;
    if (slot === null) return;
    const forSession = authStore.sessionKey;
    actionError = null;
    deleting = true;
    try {
      const piv = await pivClient();
      if (forSession !== authStore.sessionKey) return;
      if (!piv) { unavailable = true; return; }
      if (backend === '') await piv.deleteCertificate(slot);
      else await piv.deleteCertificate(slot, backend);
      if (forSession !== authStore.sessionKey) return;
      confirmDelete = null;
      showToast({ message: `Certificate in slot ${slot} deleted`, type: 'success' });
      reload();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      confirmDelete = null;
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else actionError = errText(e, 'Failed to delete the slot certificate');
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Store
  // ---------------------------------------------------------------------------

  let storeSlot = $state<string | null>(null);
  let storePem = $state('');
  let storeError = $state<string | null>(null);
  let storing = $state(false);

  function openStore(slot: string): void {
    storeSlot = slot;
    storePem = '';
    storeError = null;
  }

  function closeStore(): void {
    storeSlot = null;
    storePem = '';
    storeError = null;
  }

  async function submitStore(): Promise<void> {
    const slot = storeSlot;
    if (slot === null) return;
    const pem = storePem.trim();
    if (!pem.includes(PEM_HEADER)) { storeError = `Enter a PEM certificate (${PEM_HEADER}).`; return; }
    const forSession = authStore.sessionKey;
    storeError = null;
    storing = true;
    try {
      const piv = await pivClient();
      if (forSession !== authStore.sessionKey) return;
      if (!piv) { storeError = NOT_AVAILABLE; return; }
      await piv.storeCertificate(slot, withBackend({ certificate: pem, format: 'pem' as const }));
      if (forSession !== authStore.sessionKey) return;
      showToast({ message: `Certificate stored in slot ${slot}`, type: 'success' });
      closeStore();
      reload();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      storeError = errText(e, 'Failed to store the certificate');
    } finally {
      storing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Generate key
  // ---------------------------------------------------------------------------

  let keySlot = $state<{ slot: string; hasKey: boolean } | null>(null);
  let keyAlgorithm = $state<PIVKeyAlgorithm>('ecdsap256');
  let keySubject = $state('');
  let keyOrganization = $state('');
  let keyCountry = $state('');
  let keyValidity = $state('');
  let keyError = $state<string | null>(null);
  let confirmReplace = $state(false);
  let generatingKey = $state(false);
  let keyResult = $state<GeneratePIVKeyResponse | null>(null);

  function openKey(s: PIVSlotStatus): void {
    keySlot = { slot: s.slot, hasKey: s.has_key };
    keyAlgorithm = 'ecdsap256';
    keySubject = '';
    keyOrganization = '';
    keyCountry = '';
    keyValidity = '';
    keyError = null;
    confirmReplace = false;
  }

  function closeKey(): void {
    keySlot = null;
    keyError = null;
    confirmReplace = false;
  }

  function buildKeyRequest(): GeneratePIVKeyRequest | null {
    const subject = keySubject.trim();
    if (subject === '') { keyError = 'Subject is required.'; return null; }
    const req: GeneratePIVKeyRequest = { algorithm: keyAlgorithm, subject };
    if (keyOrganization.trim() !== '') req.organization = keyOrganization.trim();
    if (keyCountry.trim() !== '') req.country = keyCountry.trim();
    const days = String(keyValidity).trim();
    if (days !== '') {
      const n = Number(days);
      if (!Number.isInteger(n) || n <= 0) { keyError = 'Validity days must be a positive whole number.'; return null; }
      req.validity_days = n;
    }
    return withBackend(req);
  }

  function requestKey(): void {
    keyError = null;
    if (keySlot === null || buildKeyRequest() === null) return;
    if (keySlot.hasKey) { confirmReplace = true; return; }
    void runKey();
  }

  async function runKey(): Promise<void> {
    confirmReplace = false;
    if (keySlot === null) return;
    const slot = keySlot.slot;
    const req = buildKeyRequest();
    if (req === null) return;
    const forSession = authStore.sessionKey;
    generatingKey = true;
    try {
      const piv = await pivClient();
      if (forSession !== authStore.sessionKey) return;
      if (!piv) { keyError = NOT_AVAILABLE; return; }
      const res = await piv.generateKey(slot, req);
      if (forSession !== authStore.sessionKey) return;
      keyResult = res;
      closeKey();
      showToast({ message: `Key generated in slot ${slot}`, type: 'success' });
      reload();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      keyError = errText(e, 'Failed to generate the key');
    } finally {
      generatingKey = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Generate CSR
  // ---------------------------------------------------------------------------

  let csrSlot = $state<string | null>(null);
  let csrSubject = $state('');
  let csrOrganization = $state('');
  let csrUnit = $state('');
  let csrCountry = $state('');
  let csrState = $state('');
  let csrLocality = $state('');
  let csrError = $state<string | null>(null);
  let generatingCSR = $state(false);
  let csrResult = $state<{ slot: string; csr: string } | null>(null);

  function openCSR(slot: string): void {
    csrSlot = slot;
    csrSubject = ''; csrOrganization = ''; csrUnit = '';
    csrCountry = ''; csrState = ''; csrLocality = '';
    csrError = null;
  }

  function closeCSR(): void {
    csrSlot = null;
    csrError = null;
  }

  async function submitCSR(): Promise<void> {
    const slot = csrSlot;
    if (slot === null) return;
    const subject = csrSubject.trim();
    if (subject === '') { csrError = 'Subject is required.'; return; }
    const req: GeneratePIVCSRRequest = { subject };
    const optional: [keyof GeneratePIVCSRRequest, string][] = [
      ['organization', csrOrganization], ['organizational_unit', csrUnit], ['country', csrCountry],
      ['state', csrState], ['locality', csrLocality],
    ];
    for (const [k, v] of optional) if (v.trim() !== '') req[k] = v.trim();
    const forSession = authStore.sessionKey;
    csrError = null;
    generatingCSR = true;
    try {
      const piv = await pivClient();
      if (forSession !== authStore.sessionKey) return;
      if (!piv) { csrError = NOT_AVAILABLE; return; }
      const res = await piv.generateCSR(slot, withBackend(req));
      if (forSession !== authStore.sessionKey) return;
      csrResult = { slot, csr: res.csr };
      closeCSR();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      csrError = errText(e, 'Failed to generate the CSR');
    } finally {
      generatingCSR = false;
    }
  }
</script>

<div class="space-y-3">
  <h2 class="text-base font-semibold">PIV Slots</h2>

  {#if unavailable}
    <div class="alert alert-info text-sm" data-testid="xkms-piv-not-available">{NOT_AVAILABLE}</div>
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
        rows={slots}
        rowKey={(s: PIVSlotStatus) => s.slot}
        ariaLabel="PIV slots"
        searchPlaceholder="Search slots..."
        searchLabel="Search slots"
        noMatchText="No slots match the search."
        emptyTitle="No PIV slots found."
        emptyDescription="The backend reports no PIV slots."
      >
        {#snippet row(s: PIVSlotStatus)}
          <tr class="hover">
            <td class="font-mono text-xs font-medium">{s.slot}</td>
            <td class="text-xs">{s.name}</td>
            <td class="text-xs" data-testid={`piv-has-cert-${s.slot}`}>{s.has_cert ? 'Yes' : 'No'}</td>
            <td class="text-xs" data-testid={`piv-has-key-${s.slot}`}>{s.has_key ? 'Yes' : 'No'}</td>
            <td class="font-mono text-xs">{s.subject || '—'}</td>
            <td class="font-mono text-xs">{s.algorithm || '—'}</td>
            <td class="font-mono text-xs">{s.not_after || '—'}</td>
            <td>
              <div class="flex gap-1">
                <button class="btn btn-ghost btn-xs" onclick={() => { void view(s.slot); }}
                  title="View certificate" aria-label={`View certificate ${s.slot}`}>
                  <Eye class="h-3 w-3" />
                </button>
                <button class="btn btn-ghost btn-xs" onclick={() => openStore(s.slot)}
                  title="Store certificate" aria-label={`Store certificate ${s.slot}`}>
                  <Upload class="h-3 w-3" />
                </button>
                <button class="btn btn-ghost btn-xs text-error" onclick={() => { actionError = null; confirmDelete = s.slot; }}
                  title="Delete certificate" aria-label={`Delete certificate ${s.slot}`}>
                  <Trash2 class="h-3 w-3" />
                </button>
                <button class="btn btn-ghost btn-xs" onclick={() => openKey(s)}
                  title="Generate key" aria-label={`Generate key ${s.slot}`}>
                  <KeyRound class="h-3 w-3" />
                </button>
                <button class="btn btn-ghost btn-xs" onclick={() => openCSR(s.slot)}
                  title="Generate CSR" aria-label={`Generate CSR ${s.slot}`}>
                  <FileSignature class="h-3 w-3" />
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
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Slot ${viewing.slot} certificate`}>
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-4">Slot <span class="font-mono">{viewing.slot}</span> certificate</h3>
      <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="piv-cert-pem">{viewing.pem}</pre>
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
      <p class="text-sm">Delete the certificate in PIV slot <span class="font-mono">{confirmDelete}</span>? The slot's key stays.</p>
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

{#if storeSlot !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Store certificate dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Store certificate in slot <span class="font-mono">{storeSlot}</span></h3>
      {#if storeError}
        <div class="alert alert-error text-sm mb-3" role="alert">{storeError}</div>
      {/if}
      <div class="form-control">
        <label class="label label-text" for="xkms-piv-store-pem">Certificate PEM</label>
        <textarea id="xkms-piv-store-pem" class="textarea textarea-bordered font-mono text-xs h-40"
          bind:value={storePem} placeholder={PEM_HEADER}></textarea>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeStore}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={storing} onclick={() => void submitStore()}>
          {storing ? 'Storing…' : 'Store'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeStore} onkeydown={closeStore}></div>
  </div>
{/if}

{#if keySlot !== null && !confirmReplace}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Generate key dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Generate key in slot <span class="font-mono">{keySlot.slot}</span></h3>
      {#if keyError}
        <div class="alert alert-error text-sm mb-3" role="alert">{keyError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-key-algo">Algorithm</label>
          <select id="xkms-piv-key-algo" class="select select-bordered select-sm" bind:value={keyAlgorithm}>
            {#each ALGORITHMS as a (a)}
              <option value={a}>{a}</option>
            {/each}
          </select>
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-key-subject">Subject</label>
          <input id="xkms-piv-key-subject" class="input input-bordered input-sm" type="text" bind:value={keySubject} placeholder="CN=..." />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-key-org">Organization</label>
          <input id="xkms-piv-key-org" class="input input-bordered input-sm" type="text" bind:value={keyOrganization} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-key-country">Country</label>
          <input id="xkms-piv-key-country" class="input input-bordered input-sm" type="text" bind:value={keyCountry} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-key-days">Validity days</label>
          <input id="xkms-piv-key-days" class="input input-bordered input-sm" type="text" inputmode="numeric" bind:value={keyValidity} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeKey}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={generatingKey} onclick={requestKey}>
          {generatingKey ? 'Generating…' : 'Generate'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeKey} onkeydown={closeKey}></div>
  </div>
{/if}

{#if keySlot !== null && confirmReplace}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Replace key dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Replace key</h3>
      <p class="text-sm">PIV slot <span class="font-mono">{keySlot.slot}</span> already holds a key. Generating a new one replaces it for good.</p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { confirmReplace = false; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={generatingKey} onclick={() => void runKey()}>Replace key</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { confirmReplace = false; }} onkeydown={() => { confirmReplace = false; }}></div>
  </div>
{/if}

{#if keyResult}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Slot ${keyResult.slot} key`}>
    <div class="modal-box max-w-2xl space-y-3">
      <h3 class="text-base font-semibold">Key generated in slot <span class="font-mono">{keyResult.slot}</span></h3>
      <div>
        <div class="text-xs font-semibold mb-1">Certificate</div>
        <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="piv-key-cert">{keyResult.certificate}</pre>
      </div>
      <div>
        <div class="text-xs font-semibold mb-1">Public key</div>
        <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="piv-key-public">{keyResult.public_key}</pre>
      </div>
      {#if keyResult.csr}
        <div>
          <div class="text-xs font-semibold mb-1">CSR</div>
          <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="piv-key-csr">{keyResult.csr}</pre>
        </div>
      {/if}
      {#if keyResult.chain_pem}
        <div>
          <div class="text-xs font-semibold mb-1">Chain</div>
          <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="piv-key-chain">{keyResult.chain_pem}</pre>
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { keyResult = null; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { keyResult = null; }} onkeydown={() => { keyResult = null; }}></div>
  </div>
{/if}

{#if csrSlot !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Generate CSR dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Generate CSR for slot <span class="font-mono">{csrSlot}</span></h3>
      {#if csrError}
        <div class="alert alert-error text-sm mb-3" role="alert">{csrError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-csr-subject">Subject</label>
          <input id="xkms-piv-csr-subject" class="input input-bordered input-sm" type="text" bind:value={csrSubject} placeholder="CN=..." />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-csr-org">Organization</label>
          <input id="xkms-piv-csr-org" class="input input-bordered input-sm" type="text" bind:value={csrOrganization} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-csr-ou">Organizational unit</label>
          <input id="xkms-piv-csr-ou" class="input input-bordered input-sm" type="text" bind:value={csrUnit} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-csr-country">Country</label>
          <input id="xkms-piv-csr-country" class="input input-bordered input-sm" type="text" bind:value={csrCountry} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-csr-state">State</label>
          <input id="xkms-piv-csr-state" class="input input-bordered input-sm" type="text" bind:value={csrState} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-piv-csr-locality">Locality</label>
          <input id="xkms-piv-csr-locality" class="input input-bordered input-sm" type="text" bind:value={csrLocality} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeCSR}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={generatingCSR} onclick={() => void submitCSR()}>
          {generatingCSR ? 'Generating…' : 'Generate'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeCSR} onkeydown={closeCSR}></div>
  </div>
{/if}

{#if csrResult}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Slot ${csrResult.slot} CSR`}>
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-4">CSR for slot <span class="font-mono">{csrResult.slot}</span></h3>
      <pre class="bg-base-200 p-3 rounded text-xs overflow-x-auto whitespace-pre-wrap break-all" data-testid="piv-csr">{csrResult.csr}</pre>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { csrResult = null; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { csrResult = null; }} onkeydown={() => { csrResult = null; }}></div>
  </div>
{/if}
