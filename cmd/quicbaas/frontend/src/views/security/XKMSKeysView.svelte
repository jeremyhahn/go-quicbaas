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
  XKMSKeysView is the Key Store page: the keys held by the xkms backends.

  Actions:
    - Backend status: the xkms health and the backends list; picking a
      backend filters the keys and shows that backend's capabilities
    - List keys (key id, type, algorithm, backend)
    - Generate Key dialog (key id, backend, type, size or curve, algorithm)
    - Manage a key in XKMSKeyPanel (details, versions, rotate, delete,
      encrypt, decrypt, sign, verify)

  Each load is bound to the session that started it: a response that lands
  after a sign-out or another login is dropped.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { TransportNotSupportedError } from '@quicbaas/sdk';
  import type {
    XKMSKeyInfo,
    XKMSBackendInfo,
    XKMSBackendCapabilities,
    XKMSHealthResponse,
    XKMSGenerateKeyRequest,
  } from '@quicbaas/sdk';
  import { KeySquare, Settings2 } from 'lucide-svelte';
  import XKMSKeyPanel from '../../components/security/XKMSKeyPanel.svelte';

  const NOT_AVAILABLE = 'Key Store is not available on this connection.';

  const KEY_COLUMNS: Column<XKMSKeyInfo>[] = [
    { key: 'key_id', label: 'Key ID', sortable: true, searchable: true },
    { key: 'key_type', label: 'Type', sortable: true, searchable: true },
    { key: 'algorithm', label: 'Algorithm', sortable: true, searchable: true },
    { key: 'backend', label: 'Backend', sortable: true, searchable: true },
    { key: 'actions', label: 'Actions' },
  ];

  const KEY_TYPES = ['rsa', 'ecdsa', 'ed25519', 'aes'] as const;
  const RSA_SIZES = [2048, 3072, 4096] as const;
  const AES_SIZES = [128, 256] as const;
  const CURVES = ['P-256', 'P-384', 'P-521'] as const;

  function errText(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  let unavailable = $state(false);

  // ---------------------------------------------------------------------------
  // Backend status
  // ---------------------------------------------------------------------------

  let health = $state<XKMSHealthResponse | null>(null);
  let healthError = $state<string | null>(null);
  let backends = $state<XKMSBackendInfo[]>([]);
  let backendsError = $state<string | null>(null);
  let backendFilter = $state('');
  let backendDetail = $state<XKMSBackendInfo | null>(null);
  let backendDetailError = $state<string | null>(null);

  function capabilityNames(c: XKMSBackendCapabilities | undefined): string[] {
    if (!c) return [];
    return (Object.keys(c) as (keyof XKMSBackendCapabilities)[]).filter((k) => c[k]);
  }

  function healthBadge(status: string | undefined): string {
    const s = (status ?? '').toLowerCase();
    if (s === 'ok' || s === 'healthy' || s === 'up') return 'badge-success';
    if (s === '') return 'badge-ghost';
    return 'badge-warning';
  }

  async function loadHealth(): Promise<void> {
    const forSession = authStore.sessionKey;
    healthError = null;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      if (!client.xkms) { healthError = 'Backend status is not available on this connection.'; return; }
      const res = await client.xkms.health();
      if (forSession !== authStore.sessionKey) return;
      health = res;
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      health = null;
      healthError = errText(e, 'Failed to load the health status');
    }
  }

  async function loadBackends(): Promise<void> {
    const forSession = authStore.sessionKey;
    backendsError = null;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      if (!client.xkms) return;
      const res = await client.xkms.listBackends();
      if (forSession !== authStore.sessionKey) return;
      backends = res.backends ?? [];
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      backendsError = errText(e, 'Failed to load backends');
    }
  }

  let backendSeq = 0;

  async function loadBackendDetail(id: string): Promise<void> {
    const forSession = authStore.sessionKey;
    const seq = ++backendSeq;
    backendDetail = null;
    backendDetailError = null;
    if (id === '') return;
    try {
      const client = await getSDKClient();
      if (seq !== backendSeq || forSession !== authStore.sessionKey) return;
      if (!client.xkms) return;
      const res = await client.xkms.getBackend(id);
      if (seq !== backendSeq || forSession !== authStore.sessionKey) return;
      backendDetail = res;
    } catch (e) {
      if (seq !== backendSeq || forSession !== authStore.sessionKey) return;
      backendDetailError = errText(e, 'Failed to load the backend');
    }
  }

  function onBackendChange(): void {
    void loadKeys();
    void loadBackendDetail(backendFilter);
  }

  // ---------------------------------------------------------------------------
  // Key list
  // ---------------------------------------------------------------------------

  let keys = $state<XKMSKeyInfo[]>([]);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let managedKey = $state<XKMSKeyInfo | null>(null);
  let keysSeq = 0;

  async function loadKeys(): Promise<void> {
    const forSession = authStore.sessionKey;
    const seq = ++keysSeq;
    const backend = backendFilter;
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      if (seq !== keysSeq || forSession !== authStore.sessionKey) return;
      if (!client.xkmsKeys) { unavailable = true; return; }
      unavailable = false;
      const res = backend === '' ? await client.xkmsKeys.listKeys() : await client.xkmsKeys.listKeys(backend);
      if (seq !== keysSeq || forSession !== authStore.sessionKey) return;
      keys = res.keys ?? [];
    } catch (e) {
      if (seq !== keysSeq || forSession !== authStore.sessionKey) return;
      keys = [];
      if (e instanceof TransportNotSupportedError) unavailable = true;
      else error = errText(e, 'Failed to load keys');
    } finally {
      if (seq === keysSeq && forSession === authStore.sessionKey) loading = false;
    }
  }

  /** Loads the page: the keys first, then the backend status when keys exist. */
  async function loadAll(): Promise<void> {
    await loadKeys();
    if (unavailable) return;
    void loadHealth();
    void loadBackends();
  }

  function resetAll(): void {
    health = null; healthError = null;
    backends = []; backendsError = null;
    backendFilter = ''; backendDetail = null; backendDetailError = null;
    keys = []; error = null; managedKey = null;
    showGenerate = false; genError = null;
  }

  $effect(() => { untrack(() => { void loadAll(); }); });

  // A change of session identity drops what the previous session saw and
  // loads the page again for the new one.
  let lastSession = authStore.sessionKey;
  $effect(() => {
    const key = authStore.sessionKey;
    if (key === lastSession) return;
    lastSession = key;
    untrack(() => {
      resetAll();
      void loadAll();
    });
  });

  // ---------------------------------------------------------------------------
  // Generate key
  // ---------------------------------------------------------------------------

  let showGenerate = $state(false);
  let genKeyId = $state('');
  let genBackend = $state('');
  let genKeyType = $state<string>('rsa');
  let genKeySize = $state<number>(2048);
  let genCurve = $state<string>('P-256');
  let genAlgorithm = $state('');
  let genError = $state<string | null>(null);
  let generating = $state(false);

  function openGenerate(): void {
    genKeyId = '';
    genBackend = backendFilter || (backends[0]?.id ?? '');
    genKeyType = 'rsa';
    genKeySize = 2048;
    genCurve = 'P-256';
    genAlgorithm = '';
    genError = null;
    showGenerate = true;
  }

  function onKeyTypeChange(): void {
    if (genKeyType === 'rsa') genKeySize = 2048;
    else if (genKeyType === 'aes') genKeySize = 256;
  }

  async function submitGenerate(): Promise<void> {
    const keyId = genKeyId.trim();
    const backend = genBackend.trim();
    if (keyId === '') { genError = 'Key ID is required.'; return; }
    if (backend === '') { genError = 'Backend is required.'; return; }
    const req: XKMSGenerateKeyRequest = { key_id: keyId, backend, key_type: genKeyType };
    if (genKeyType === 'rsa' || genKeyType === 'aes') req.key_size = Number(genKeySize);
    if (genKeyType === 'ecdsa') req.curve = genCurve;
    const algorithm = genAlgorithm.trim();
    if (algorithm !== '') req.algorithm = algorithm;
    const forSession = authStore.sessionKey;
    genError = null;
    generating = true;
    try {
      const client = await getSDKClient();
      if (forSession !== authStore.sessionKey) return;
      if (!client.xkmsKeys) { genError = NOT_AVAILABLE; return; }
      await client.xkmsKeys.generateKey(req);
      if (forSession !== authStore.sessionKey) return;
      showToast({ message: `Key "${keyId}" generated`, type: 'success' });
      showGenerate = false;
      void loadKeys();
    } catch (e) {
      if (forSession !== authStore.sessionKey) return;
      genError = errText(e, 'Failed to generate the key');
    } finally {
      generating = false;
    }
  }

  function onKeyDeleted(keyId: string): void {
    managedKey = null;
    showToast({ message: `Key "${keyId}" deleted`, type: 'success' });
    void loadKeys();
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Key Store" subtitle="Keys held by the xkms backends" icon={KeySquare}>
    {#snippet children()}
      {#if !unavailable}
        <button class="btn btn-primary btn-sm" onclick={openGenerate} aria-label="Generate key">
          + Generate Key
        </button>
      {/if}
    {/snippet}
  </PageHeader>

  {#if unavailable}
    <div class="alert alert-info text-sm" data-testid="xkms-not-available">{NOT_AVAILABLE}</div>
  {:else}
    <section class="card bg-base-200 p-4 space-y-3" aria-label="Backend status">
      <div class="flex flex-wrap items-center gap-3">
        <span class="text-sm font-medium">Health</span>
        {#if healthError}
          <span class="text-xs text-error" data-testid="health-error">{healthError}</span>
        {:else if health}
          <span class="badge badge-sm {healthBadge(health.status)}" data-testid="health-badge">{health.status}</span>
          {#if health.version}<span class="text-xs text-base-content/50">v{health.version}</span>{/if}
          {#if health.message}<span class="text-xs text-base-content/50">{health.message}</span>{/if}
        {:else}
          <span class="loading loading-spinner loading-xs"></span>
        {/if}

        <label class="label label-text ml-auto" for="xkms-backend-filter">Backend</label>
        <select
          id="xkms-backend-filter"
          class="select select-bordered select-sm"
          bind:value={backendFilter}
          onchange={onBackendChange}
        >
          <option value="">All backends</option>
          {#each backends as b (b.id)}
            <option value={b.id}>{b.id}</option>
          {/each}
        </select>
      </div>

      {#if backendsError}
        <div class="alert alert-error text-sm" role="alert">{backendsError}</div>
      {:else if backends.length > 0}
        <ul class="flex flex-wrap gap-2 text-xs" aria-label="Backends">
          {#each backends as b (b.id)}
            <li class="badge badge-outline gap-1">
              <span class="font-mono">{b.id}</span>
              <span class="text-base-content/50">{b.type}{b.hardware_backed ? ', hardware' : ''}</span>
            </li>
          {/each}
        </ul>
      {/if}

      {#if backendDetailError}
        <div class="alert alert-error text-sm" role="alert">{backendDetailError}</div>
      {:else if backendDetail}
        <div class="text-xs" data-testid="backend-capabilities">
          <span class="font-medium">{backendDetail.id}</span> capabilities:
          {capabilityNames(backendDetail.capabilities).join(', ') || 'none'}
        </div>
      {/if}
    </section>

    {#if error}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{error}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => { void loadKeys(); }}>Retry</button>
      </div>
    {/if}

    {#if loading}
      <div class="flex justify-center py-8">
        <span class="loading loading-spinner loading-md"></span>
      </div>
    {:else}
      <DataTable
        columns={KEY_COLUMNS}
        rows={keys}
        rowKey={(k: XKMSKeyInfo) => `${k.backend}/${k.key_id}`}
        ariaLabel="Key Store keys"
        searchPlaceholder="Search keys..."
        searchLabel="Search keys"
        noMatchText="No keys match the search."
        emptyTitle="No keys found."
        emptyDescription="Generate a key to get started."
      >
        {#snippet row(k: XKMSKeyInfo)}
          <tr class="hover">
            <td class="font-mono text-xs font-medium">{k.key_id}</td>
            <td class="font-mono text-xs">{k.key_type}</td>
            <td class="font-mono text-xs">{k.algorithm || '—'}</td>
            <td class="font-mono text-xs">{k.backend}</td>
            <td>
              <div class="tooltip tooltip-left" data-tip="Manage key">
                <button
                  class="btn btn-ghost btn-xs"
                  onclick={() => { managedKey = k; }}
                  title="Manage key"
                  aria-label={`Manage key ${k.key_id}`}
                >
                  <Settings2 class="h-3 w-3" />
                </button>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}
  {/if}
</div>

{#if showGenerate}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Generate key dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Generate Key</h3>

      {#if genError}
        <div class="alert alert-error text-sm mb-3" role="alert">{genError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="xkms-gen-id">Key ID</label>
          <input id="xkms-gen-id" class="input input-bordered input-sm" type="text" bind:value={genKeyId} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-gen-backend">Backend</label>
          <input
            id="xkms-gen-backend"
            class="input input-bordered input-sm"
            type="text"
            list="xkms-gen-backends"
            bind:value={genBackend}
          />
          <datalist id="xkms-gen-backends">
            {#each backends as b (b.id)}<option value={b.id}></option>{/each}
          </datalist>
        </div>
        <div class="form-control">
          <label class="label label-text" for="xkms-gen-type">Key type</label>
          <select id="xkms-gen-type" class="select select-bordered select-sm" bind:value={genKeyType} onchange={onKeyTypeChange}>
            {#each KEY_TYPES as t (t)}<option value={t}>{t}</option>{/each}
          </select>
        </div>
        {#if genKeyType === 'rsa' || genKeyType === 'aes'}
          <div class="form-control">
            <label class="label label-text" for="xkms-gen-size">Key size</label>
            <select id="xkms-gen-size" class="select select-bordered select-sm" bind:value={genKeySize}>
              {#each genKeyType === 'rsa' ? RSA_SIZES : AES_SIZES as s (s)}<option value={s}>{s}</option>{/each}
            </select>
          </div>
        {:else if genKeyType === 'ecdsa'}
          <div class="form-control">
            <label class="label label-text" for="xkms-gen-curve">Curve</label>
            <select id="xkms-gen-curve" class="select select-bordered select-sm" bind:value={genCurve}>
              {#each CURVES as c (c)}<option value={c}>{c}</option>{/each}
            </select>
          </div>
        {/if}
        <div class="form-control">
          <label class="label label-text" for="xkms-gen-alg">Algorithm (optional)</label>
          <input id="xkms-gen-alg" class="input input-bordered input-sm" type="text" bind:value={genAlgorithm} />
        </div>
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showGenerate = false; genError = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={generating} onclick={() => void submitGenerate()}>
          {generating ? 'Generating…' : 'Generate'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showGenerate = false; }} onkeydown={() => { showGenerate = false; }}></div>
  </div>
{/if}

{#if managedKey}
  <XKMSKeyPanel
    keyId={managedKey.key_id}
    backend={managedKey.backend}
    onclose={() => { managedKey = null; }}
    ondeleted={onKeyDeleted}
  />
{/if}
