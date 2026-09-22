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
  KMSView renders an operator table of cryptographic keys managed by the KMS.

  Actions:
    - List keys (algorithm, status, created)
    - Create Key dialog (algorithm selector, usage)
    - Rotate key per row
    - Delete key per row (confirm dialog)
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { statusBadgeClass } from '../../components/core/statusVariant.ts';
  import type { KMSKey, CreateKeyRequest } from '@quicbaas/sdk';
  import type { KMSKeyVersion } from '@quicbaas/sdk';
  import { Key, Pencil, Trash2, RotateCw, Eye, ToggleLeft, ToggleRight, Star } from 'lucide-svelte';
  import { formatDateTime } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Key list state
  // ---------------------------------------------------------------------------

  let keys: KMSKey[] = $state([]);

  const KEY_COLUMNS: Column<KMSKey>[] = [
    { key: 'name', label: 'Name / ID', sortable: true, searchable: true, value: (k) => k.name || k.id },
    { key: 'algorithm', label: 'Algorithm', sortable: true, searchable: true },
    { key: 'status', label: 'Status', sortable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'actions', label: 'Actions' },
  ];

  const KEY_VERSION_COLUMNS: Column<KMSKeyVersion>[] = [
    { key: 'version', label: 'Version', sortable: true },
    { key: 'status', label: 'Status', sortable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'destroyed_at', label: 'Destroyed', sortable: true },
  ];
  let loading = $state(false);
  let error: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Create Key dialog state
  // ---------------------------------------------------------------------------

  let showCreate = $state(false);
  let newKeyName = $state('');
  let newKeyAlgorithm = $state('AES-256-GCM');
  let newKeyUsage: string[] = $state(['encrypt', 'decrypt']);
  let createError: string | null = $state(null);
  let creating = $state(false);

  const algorithms = [
    'AES-256-GCM',
    'AES-128-GCM',
    'RSA-2048',
    'RSA-4096',
    'ECDSA-P256',
    'ECDSA-P384',
    'ED25519',
  ] as const;

  // ---------------------------------------------------------------------------
  // Delete confirm state
  // ---------------------------------------------------------------------------

  let keyToDelete: KMSKey | null = $state(null);
  let deleting = $state(false);

  // ---------------------------------------------------------------------------
  // Set default key tracking
  // ---------------------------------------------------------------------------

  let settingDefaultKey: string | null = $state(null);

  async function setDefaultKey(key: KMSKey): Promise<void> {
    settingDefaultKey = key.id;
    try {
      const client = await getSDKClient();
      await client.kms.setDefaultKey(key.name || key.id);
      showToast({ message: `Key "${key.name || key.id}" set as default`, type: 'success' });
      void loadKeys();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Set default failed', type: 'error' });
    } finally {
      settingDefaultKey = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotate in-progress tracking
  // ---------------------------------------------------------------------------

  let rotatingKey: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Key versions modal
  // ---------------------------------------------------------------------------

  let versionsKey: KMSKey | null = $state(null);
  let versions: KMSKeyVersion[] = $state([]);
  let versionsLoading = $state(false);
  let versionsError: string | null = $state(null);

  async function viewVersions(key: KMSKey): Promise<void> {
    versionsKey = key;
    versions = [];
    versionsError = null;
    versionsLoading = true;
    try {
      const client = await getSDKClient();
      versions = await client.kms.listKeyVersions(key.name || key.id);
    } catch (e) {
      versionsError = e instanceof Error ? e.message : 'Failed to load versions';
    } finally {
      versionsLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Disable / Enable key
  // ---------------------------------------------------------------------------

  let togglingKey: string | null = $state(null);

  async function toggleKeyStatus(key: KMSKey): Promise<void> {
    const nextStatus = (key.status === 'active' || key.status === 'enabled') ? 'disabled' : 'enabled';
    togglingKey = key.id;
    try {
      const client = await getSDKClient();
      await client.kms.updateKeyStatus(key.name || key.id, nextStatus);
      showToast({ message: `Key "${key.name || key.id}" ${nextStatus}`, type: 'success' });
      void loadKeys();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Status update failed', type: 'error' });
    } finally {
      togglingKey = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  // isSystemKey returns true for internal CA / TLS / agent-identity keys that
  // are not tenant-managed encryption keys. These have an empty algorithm and
  // a zero created_at timestamp (Go zero-time, mapped to '' by the SDK).
  function isSystemKey(k: KMSKey): boolean {
    return k.algorithm === '' && k.created_at === '';
  }

  // LOAD_TIMEOUT_MS caps how long loadKeys() can hold the loading state.
  // If the API call takes longer than this the view shows an error rather than
  // spinning indefinitely. This also prevents the Chrome DevTools Protocol
  // (CDP) Page.captureScreenshot command from timing out when the MCP
  // extension opens a fresh tab to this route.
  const LOAD_TIMEOUT_MS = 10_000;

  async function loadKeys(): Promise<void> {
    loading = true;
    error = null;
    const timeout = setTimeout(() => {
      if (loading) {
        loading = false;
        error = 'Key list timed out. Retry to reload.';
      }
    }, LOAD_TIMEOUT_MS);
    try {
      const client = await getSDKClient();
      const resp = await client.kms.listKeys(200);
      const seen = new Set<string>();
      keys = resp.keys.filter((k) => {
        if (isSystemKey(k)) return false;
        if (seen.has(k.id)) return false;
        seen.add(k.id);
        return true;
      });
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load keys';
    } finally {
      clearTimeout(timeout);
      loading = false;
    }
  }

  $effect(() => { void loadKeys(); });

  // ---------------------------------------------------------------------------
  // Create key
  // ---------------------------------------------------------------------------

  async function submitCreateKey(): Promise<void> {
    createError = null;
    creating = true;
    try {
      const client = await getSDKClient();
      const req: CreateKeyRequest = {
        name: newKeyName.trim() || undefined,
        key_attributes: {
          algorithm: newKeyAlgorithm,
          key_usage: newKeyUsage,
        },
      };
      await client.kms.createKey(req);
      showToast({ message: 'Key created', type: 'success' });
      showCreate = false;
      newKeyName = '';
      newKeyAlgorithm = 'AES-256-GCM';
      newKeyUsage = ['encrypt', 'decrypt'];
      void loadKeys();
    } catch (e) {
      createError = e instanceof Error ? e.message : 'Failed to create key';
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotate key
  // ---------------------------------------------------------------------------

  async function rotateKey(key: KMSKey): Promise<void> {
    rotatingKey = key.id;
    try {
      const client = await getSDKClient();
      await client.kms.rotateKey(key.name || key.id);
      showToast({ message: `Key "${key.name || key.id}" rotated`, type: 'success' });
      void loadKeys();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Rotate failed', type: 'error' });
    } finally {
      rotatingKey = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete key
  // ---------------------------------------------------------------------------

  async function confirmDeleteKey(): Promise<void> {
    if (!keyToDelete) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.kms.deleteKey(keyToDelete.name || keyToDelete.id);
      showToast({ message: `Key "${keyToDelete.name || keyToDelete.id}" deleted`, type: 'success' });
      keyToDelete = null;
      void loadKeys();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Delete failed', type: 'error' });
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Key usage toggle helper
  // ---------------------------------------------------------------------------

  function toggleUsage(usage: string): void {
    if (newKeyUsage.includes(usage)) {
      newKeyUsage = newKeyUsage.filter((u) => u !== usage);
    } else {
      newKeyUsage = [...newKeyUsage, usage];
    }
  }

  function statusVariant(status: string): string {
    return statusBadgeClass(status);
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="KMS" subtitle="Cryptographic Key Management" icon={Key}>
    {#snippet children()}
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { showCreate = true; }}
        aria-label="Create key"
      >
        + Create Key
      </button>
    {/snippet}
  </PageHeader>

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
      rowKey={(key: KMSKey) => key.id}
      urlKey="kms"
      ariaLabel="KMS keys"
      searchPlaceholder="Search keys..."
      searchLabel="Search keys"
      noMatchText="No keys match the search."
      emptyTitle="No keys found."
      emptyDescription="Create your first key to get started."
    >
      {#snippet row(key: KMSKey)}
        <tr class="hover">
          <td>
            <div class="font-mono text-xs font-medium">{key.name || key.id}</div>
            {#if key.description}
              <div class="text-xs text-base-content/40">{key.description}</div>
            {/if}
          </td>
          <td class="font-mono text-xs">{key.algorithm}</td>
          <td>
            <span class="badge badge-xs {statusVariant(key.status)}">{key.status}</span>
          </td>
          <td class="text-xs text-base-content/50">
            {formatDateTime(key.created_at)}
          </td>
          <td>
            <!--
              Per-row actions are rendered as compact icon buttons wrapped
              in daisyUI `.tooltip` so the action label is visible on
              hover and keyboard focus. Each button additionally carries
              a `title` attribute for the native browser tooltip and an
              `aria-label` for screen readers.
            -->
            <div class="flex gap-1">
              <div class="tooltip tooltip-left" data-tip="Set as default">
                <button
                  class="btn btn-ghost btn-xs"
                  disabled={settingDefaultKey === key.id}
                  onclick={() => void setDefaultKey(key)}
                  title="Set as default key"
                  aria-label={`Set "${key.name || key.id}" as default key`}
                >
                  <Star class="h-3 w-3" />
                </button>
              </div>
              <div class="tooltip tooltip-left" data-tip="View versions">
                <button
                  class="btn btn-ghost btn-xs"
                  onclick={() => void viewVersions(key)}
                  title="View versions"
                  aria-label={`View versions of ${key.name || key.id}`}
                >
                  <Eye class="h-3 w-3" />
                </button>
              </div>
              <div class="tooltip tooltip-left" data-tip="Rotate key">
                <button
                  class="btn btn-ghost btn-xs"
                  disabled={rotatingKey === key.id}
                  onclick={() => void rotateKey(key)}
                  title="Rotate key"
                  aria-label={`Rotate key ${key.name || key.id}`}
                >
                  <RotateCw class="h-3 w-3" />
                </button>
              </div>
              <div
                class="tooltip tooltip-left"
                data-tip={key.status === 'active' || key.status === 'enabled' ? 'Disable key' : 'Enable key'}
              >
                <button
                  class="btn btn-ghost btn-xs"
                  disabled={togglingKey === key.id}
                  onclick={() => void toggleKeyStatus(key)}
                  title={key.status === 'active' || key.status === 'enabled' ? 'Disable key' : 'Enable key'}
                  aria-label={key.status === 'active' || key.status === 'enabled' ? `Disable key ${key.name || key.id}` : `Enable key ${key.name || key.id}`}
                >
                  {#if key.status === 'active' || key.status === 'enabled'}
                    <ToggleRight class="h-3 w-3 text-success" />
                  {:else}
                    <ToggleLeft class="h-3 w-3 text-warning" />
                  {/if}
                </button>
              </div>
              <div class="tooltip tooltip-left" data-tip="Delete key">
                <button
                  class="btn btn-ghost btn-xs text-error"
                  onclick={() => { keyToDelete = key; }}
                  title="Delete key"
                  aria-label={`Delete key ${key.name || key.id}`}
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </div>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>

<!-- =========================================================================
     CREATE KEY DIALOG
     ========================================================================= -->
{#if showCreate}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create key dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Create Key</h3>

      {#if createError}
        <div class="alert alert-error text-sm mb-3" role="alert">{createError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="key-name">Name (optional)</label>
          <input
            id="key-name"
            class="input input-bordered input-sm"
            type="text"
            placeholder="my-app-encryption-key"
            bind:value={newKeyName}
          />
        </div>

        <div class="form-control">
          <label class="label label-text" for="key-algorithm">Algorithm</label>
          <select
            id="key-algorithm"
            class="select select-bordered select-sm"
            bind:value={newKeyAlgorithm}
          >
            {#each algorithms as algo}
              <option value={algo}>{algo}</option>
            {/each}
          </select>
        </div>

        <div class="form-control">
          <span class="label label-text">Key Usage</span>
          <div class="flex flex-wrap gap-2 mt-1">
            {#each ['encrypt', 'decrypt', 'sign', 'verify'] as usage}
              <label class="flex items-center gap-1 cursor-pointer text-sm">
                <input
                  type="checkbox"
                  class="checkbox checkbox-xs"
                  checked={newKeyUsage.includes(usage)}
                  onchange={() => toggleUsage(usage)}
                />
                {usage}
              </label>
            {/each}
          </div>
        </div>
      </div>

      <div class="modal-action">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { showCreate = false; createError = null; }}
        >
          Cancel
        </button>
        <button
          class="btn btn-primary btn-sm"
          disabled={creating}
          onclick={submitCreateKey}
        >
          {creating ? 'Creating…' : 'Create'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCreate = false; }} onkeydown={() => { showCreate = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE KEY CONFIRM DIALOG
     ========================================================================= -->
{#if keyToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete key confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Key?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{keyToDelete.name || keyToDelete.id}</span>? This cannot be undone.
        Data encrypted with this key will no longer be recoverable.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { keyToDelete = null; }}>Cancel</button>
        <button
          class="btn btn-error btn-sm"
          disabled={deleting}
          onclick={confirmDeleteKey}
        >
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { keyToDelete = null; }} onkeydown={() => { keyToDelete = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     KEY VERSIONS MODAL
     ========================================================================= -->
{#if versionsKey}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Key versions modal">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-1">Key Versions</h3>
      <p class="text-xs font-mono text-base-content/50 mb-4">{versionsKey.name || versionsKey.id}</p>

      {#if versionsLoading}
        <div class="flex justify-center py-6"><span class="loading loading-spinner loading-md"></span></div>
      {:else if versionsError}
        <div class="alert alert-error text-sm">{versionsError}</div>
      {:else}
        <DataTable
          columns={KEY_VERSION_COLUMNS}
          rows={versions}
          rowKey={(v: KMSKeyVersion) => v.version}
          ariaLabel="Key versions"
          emptyTitle="No versions found."
        >
          {#snippet row(v: KMSKeyVersion)}
            <tr>
              <td class="font-mono text-xs">v{v.version}</td>
              <td><span class="badge badge-xs {statusVariant(v.status)}">{v.status}</span></td>
              <td class="text-xs text-base-content/50">{formatDateTime(v.created_at)}</td>
              <td class="text-xs text-base-content/50">{v.destroyed_at ? new Date(v.destroyed_at).toLocaleString() : '—'}</td>
            </tr>
          {/snippet}
        </DataTable>
      {/if}

      <div class="modal-action">
        <button class="btn btn-primary btn-sm" onclick={() => { versionsKey = null; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { versionsKey = null; }} onkeydown={() => { versionsKey = null; }}></div>
  </div>
{/if}
