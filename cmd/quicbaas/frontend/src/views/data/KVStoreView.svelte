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
  KVStoreView provides a paginated key listing for a chosen entity type,
  with GET / PUT / DELETE operations and a JSON-pretty detail modal.
  Mirrors the `quicbaas data put/get/delete/list` CLI surface.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { KVEntry } from '@quicbaas/sdk';
  import { Plus, Trash2, Eye, Pencil, Database } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let entityType = $state('default');
  let entries = $state<KVEntry[]>([]);
  let loading = $state(false);
  let loadError = $state('');

  // PUT dialog
  let showPutDialog = $state(false);
  let putKey = $state('');
  let putValue = $state('');
  let putError = $state('');
  let putting = $state(false);

  // Detail modal
  let detailEntry = $state<KVEntry | null>(null);

  // Edit modal (same as put but pre-filled with existing key/value)
  let editEntry = $state<KVEntry | null>(null);
  let editKey = $state('');
  let editValue = $state('');
  let editError = $state('');
  let editSaving = $state(false);

  // Delete confirm modal
  let entryToDelete = $state<KVEntry | null>(null);
  let deletingEntry = $state(false);

  // ---------------------------------------------------------------------------
  // Fetch
  // ---------------------------------------------------------------------------

  async function fetchEntries(): Promise<void> {
    loading = true;
    loadError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.data.kvList(entityType || 'default');
      const seen = new Set<string>();
      entries = resp.entries.filter((e) => {
        if (seen.has(e.key)) return false;
        seen.add(e.key);
        return true;
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      loadError = `Failed to load keys: ${msg}`;
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchEntries(); });

  // ---------------------------------------------------------------------------
  // Filtered view
  // ---------------------------------------------------------------------------

  const TABLE_COLUMNS: Column<KVEntry>[] = [
    { key: 'key', label: 'Key', sortable: true, searchable: true },
    {
      key: 'value',
      label: 'Preview',
      searchable: true,
      value: (e) => JSON.stringify(e.value),
    },
    { key: 'actions', label: 'Actions', headerClass: 'w-24 text-right', srLabel: true },
  ];

  // ---------------------------------------------------------------------------
  // PUT
  // ---------------------------------------------------------------------------

  async function handlePut(): Promise<void> {
    if (!putKey.trim()) {
      putError = 'Key is required.';
      return;
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(putValue);
    } catch {
      putError = 'Value must be valid JSON.';
      return;
    }
    putting = true;
    putError = '';
    try {
      const client = await getSDKClient();
      await client.data.kvPut(entityType || 'default', putKey.trim(), parsed);
      showToast({ message: `Key "${putKey}" stored.`, type: 'success' });
      showPutDialog = false;
      putKey = '';
      putValue = '';
      await fetchEntries();
    } catch (err: unknown) {
      putError = err instanceof Error ? err.message : String(err);
    } finally {
      putting = false;
    }
  }

  function openPutDialog(): void {
    putKey = '';
    putValue = '';
    putError = '';
    showPutDialog = true;
  }

  // ---------------------------------------------------------------------------
  // EDIT
  // ---------------------------------------------------------------------------

  function openEditDialog(entry: KVEntry): void {
    editEntry = entry;
    editKey = entry.key;
    editValue = JSON.stringify(entry.value, null, 2);
    editError = '';
  }

  async function handleEdit(): Promise<void> {
    if (!editEntry) return;
    let parsed: unknown;
    try {
      parsed = JSON.parse(editValue);
    } catch {
      editError = 'Value must be valid JSON.';
      return;
    }
    editSaving = true;
    editError = '';
    try {
      const client = await getSDKClient();
      await client.data.kvPut(entityType || 'default', editKey.trim(), parsed);
      showToast({ message: `Key "${editKey}" updated.`, type: 'success' });
      editEntry = null;
      await fetchEntries();
    } catch (err: unknown) {
      editError = err instanceof Error ? err.message : String(err);
    } finally {
      editSaving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // DELETE (confirm modal)
  // ---------------------------------------------------------------------------

  async function confirmDelete(): Promise<void> {
    if (!entryToDelete) return;
    deletingEntry = true;
    try {
      const client = await getSDKClient();
      await client.data.kvDelete(entityType || 'default', entryToDelete.key);
      showToast({ message: `Key "${entryToDelete.key}" deleted.`, type: 'success' });
      entryToDelete = null;
      await fetchEntries();
    } catch (err: unknown) {
      showToast({ message: err instanceof Error ? err.message : String(err), type: 'error' });
    } finally {
      deletingEntry = false;
    }
  }
</script>

<div class="p-6">
  <PageHeader title="KV Store" subtitle="Manage key-value data by entity type" icon={Database}>
    {#snippet children()}
      <button class="btn btn-primary btn-sm" onclick={openPutDialog}>
        <Plus class="h-4 w-4" />
        Put Key
      </button>
    {/snippet}
  </PageHeader>

  <!-- Controls -->
  <div class="flex flex-wrap gap-3 mb-4">
    <label class="flex items-center gap-2 text-sm text-base-content/60">
      Entity type:
      <input
        type="text"
        class="input input-bordered input-sm w-36 font-mono"
        bind:value={entityType}
        placeholder="default"
        onchange={() => { void fetchEntries(); }}
      />
    </label>

  </div>

  <!-- Error -->
  {#if loadError}
    <div class="alert alert-error text-sm mb-4 flex items-center justify-between">
      <span>{loadError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void fetchEntries(); }}>Retry</button>
    </div>
  {/if}

  <!-- Table -->
  {#if loading}
    <div class="flex justify-center py-12">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else}
    <DataTable
      columns={TABLE_COLUMNS}
      rows={entries}
      rowKey={(e: KVEntry) => e.key}
      urlKey="kv"
      ariaLabel="Key-value entries"
      searchPlaceholder="Filter by key…"
      searchLabel="Filter keys"
      noMatchText="No keys match the filter."
    >
      {#snippet empty()}
        <EmptyState
          icon={Database}
          title="No keys for entity type '{entityType || 'default'}' — Put Key is how you start."
          description="Store structured JSON values under any key. Retrieve them instantly by key or list all."
          actionLabel="Put Key"
          onaction={openPutDialog}
        />
      {/snippet}

      {#snippet row(entry: KVEntry)}
        <tr class="hover:bg-base-200/50">
          <td class="font-mono text-xs">{entry.key}</td>
          <td class="text-xs text-base-content/50 truncate max-w-xs">
            {JSON.stringify(entry.value).slice(0, 80)}
          </td>
          <td class="text-right">
            <div class="flex justify-end gap-1">
              <button
                class="btn btn-ghost btn-xs"
                title={`View ${entry.key}`}
                onclick={() => { detailEntry = entry; }}
                aria-label={`View ${entry.key}`}
              >
                <Eye class="h-3 w-3" />
              </button>
              <button
                class="btn btn-ghost btn-xs"
                title={`Edit ${entry.key}`}
                onclick={() => openEditDialog(entry)}
                aria-label={`Edit ${entry.key}`}
              >
                <Pencil class="h-3 w-3" />
              </button>
              <button
                class="btn btn-ghost btn-xs text-error"
                title={`Delete ${entry.key}`}
                onclick={() => { entryToDelete = entry; }}
                aria-label={`Delete ${entry.key}`}
              >
                <Trash2 class="h-3 w-3" />
              </button>
            </div>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>

<!-- PUT dialog -->
{#if showPutDialog}
  <dialog class="modal modal-open" aria-label="Put key-value dialog">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-4">Put Key-Value</h3>

      {#if putError}
        <div class="alert alert-error text-sm mb-3">{putError}</div>
      {/if}

      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Key</span></div>
        <input
          type="text"
          class="input input-bordered font-mono"
          bind:value={putKey}
          placeholder="my-key"
        />
      </label>

      <label class="form-control mb-4">
        <div class="label"><span class="label-text">Value (JSON)</span></div>
        <textarea
          class="textarea textarea-bordered font-mono text-sm h-32"
          bind:value={putValue}
          placeholder={"JSON value, e.g. {\"hello\": \"world\"}"}
        ></textarea>
      </label>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showPutDialog = false; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" onclick={() => { void handlePut(); }} disabled={putting}>
          {#if putting}<span class="loading loading-spinner loading-xs"></span>{/if}
          Store
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { showPutDialog = false; }} aria-label="Close dialog"></button>
  </dialog>
{/if}

<!-- Edit modal -->
{#if editEntry !== null}
  <dialog class="modal modal-open" aria-label="Edit key-value dialog">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-4">Edit Key-Value</h3>
      {#if editError}
        <div class="alert alert-error text-sm mb-3">{editError}</div>
      {/if}
      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Key</span></div>
        <input type="text" class="input input-bordered font-mono" bind:value={editKey} readonly />
      </label>
      <label class="form-control mb-4">
        <div class="label"><span class="label-text">Value (JSON)</span></div>
        <textarea class="textarea textarea-bordered font-mono text-sm h-32" bind:value={editValue}></textarea>
      </label>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { editEntry = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" onclick={() => { void handleEdit(); }} disabled={editSaving}>
          {#if editSaving}<span class="loading loading-spinner loading-xs"></span>{/if}
          Save
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { editEntry = null; }} aria-label="Close edit dialog"></button>
  </dialog>
{/if}

<!-- Delete confirm modal -->
{#if entryToDelete !== null}
  <dialog class="modal modal-open" aria-label="Delete key confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Key?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{entryToDelete.key}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { entryToDelete = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" onclick={() => { void confirmDelete(); }} disabled={deletingEntry}>
          {#if deletingEntry}<span class="loading loading-spinner loading-xs"></span>{/if}
          Delete
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { entryToDelete = null; }} aria-label="Close delete dialog"></button>
  </dialog>
{/if}

<!-- Detail modal -->
{#if detailEntry !== null}
  <dialog class="modal modal-open" aria-label="Key detail modal">
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-1">Key Detail</h3>
      <p class="text-xs font-mono text-base-content/50 mb-4">{detailEntry.key}</p>
      <pre class="bg-base-300 rounded-lg p-4 text-xs overflow-x-auto">{JSON.stringify(detailEntry.value, null, 2)}</pre>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { detailEntry = null; }}>Close</button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { detailEntry = null; }} aria-label="Close detail modal"></button>
  </dialog>
{/if}
