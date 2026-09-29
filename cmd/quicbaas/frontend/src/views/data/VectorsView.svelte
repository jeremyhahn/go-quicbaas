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
  VectorsView lists vector namespaces (collections), allows creating a new
  namespace, and provides a similarity search interface for any namespace.
  Mirrors the `quicbaas vector search/index/create` CLI surface.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { VectorNamespace, VectorSearchResult } from '@quicbaas/sdk';
  import { APIError } from '@quicbaas/sdk';
  import { Plus, Search, Trash2, ChevronRight, ScatterChart } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Namespace list state
  // ---------------------------------------------------------------------------

  let namespaces = $state<VectorNamespace[]>([]);
  let loading = $state(false);
  let loadError = $state('');
  let subsystemDisabled = $state(false);

  function isDisabledError(err: unknown): boolean {
    if (err instanceof APIError && (err.status === 408 || err.status === 503 || err.status === 500)) {
      const msg = err.message.toLowerCase();
      return msg.includes('not available') || msg.includes('not configured') || msg.includes('not compiled');
    }
    return false;
  }

  async function fetchNamespaces(): Promise<void> {
    loading = true;
    loadError = '';
    subsystemDisabled = false;
    try {
      const client = await getSDKClient();
      // Deduplicate by name: both {#each} blocks over this array use ns.name
      // as the key, so server-side duplicates would fire each_key_duplicate.
      const raw = await client.vectors.listNamespaces();
      const seen = new Set<string>();
      namespaces = raw.filter((ns) => {
        if (seen.has(ns.name)) return false;
        seen.add(ns.name);
        return true;
      });
    } catch (err: unknown) {
      if (isDisabledError(err)) {
        subsystemDisabled = true;
      } else {
        loadError = err instanceof Error ? err.message : String(err);
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchNamespaces(); });

  // ---------------------------------------------------------------------------
  // Create namespace
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let createName = $state('');
  let createDimension = $state(768);
  let createMetric = $state('cosine');
  let createError = $state('');
  let creating = $state(false);

  function openCreateDialog(): void {
    createName = '';
    createDimension = 768;
    createMetric = 'cosine';
    createError = '';
    showCreateDialog = true;
  }

  async function handleCreate(): Promise<void> {
    if (!createName.trim()) {
      createError = 'Namespace name is required.';
      return;
    }
    if (createDimension < 1) {
      createError = 'Dimension must be at least 1.';
      return;
    }
    creating = true;
    createError = '';
    try {
      const client = await getSDKClient();
      await client.vectors.createNamespace({
        name: createName.trim(),
        dimension: createDimension,
        metric: createMetric,
      });
      showToast({ message: `Namespace "${createName}" created.`, type: 'success' });
      showCreateDialog = false;
      await fetchNamespaces();
    } catch (err: unknown) {
      createError = err instanceof Error ? err.message : String(err);
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete namespace (confirm modal)
  // ---------------------------------------------------------------------------

  let namespaceToDelete = $state<VectorNamespace | null>(null);
  let deletingNamespace = $state(false);

  async function confirmDeleteNamespace(): Promise<void> {
    if (!namespaceToDelete) return;
    deletingNamespace = true;
    try {
      const client = await getSDKClient();
      await client.vectors.deleteNamespace(namespaceToDelete.name);
      showToast({ message: `Namespace "${namespaceToDelete.name}" deleted.`, type: 'success' });
      namespaceToDelete = null;
      await fetchNamespaces();
    } catch (err: unknown) {
      showToast({ message: err instanceof Error ? err.message : String(err), type: 'error' });
    } finally {
      deletingNamespace = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Search panel
  // ---------------------------------------------------------------------------

  let searchNamespace = $state('');
  let searchQuery = $state('');
  let searchTopK = $state(10);
  let searchResults = $state<VectorSearchResult[]>([]);
  let searching = $state(false);
  let searchError = $state('');

  async function handleSearch(): Promise<void> {
    if (!searchNamespace.trim()) {
      searchError = 'Select a namespace first.';
      return;
    }
    if (!searchQuery.trim()) {
      searchError = 'Enter a search query.';
      return;
    }
    searching = true;
    searchError = '';
    searchResults = [];
    try {
      const client = await getSDKClient();
      searchResults = await client.vectors.searchSimilar(searchNamespace, {
        text: searchQuery.trim(),
        top_k: searchTopK,
      });
    } catch (err: unknown) {
      searchError = err instanceof Error ? err.message : String(err);
    } finally {
      searching = false;
    }
  }
</script>

<div class="p-6">
  <PageHeader title="Vectors" subtitle="Manage vector namespaces and run similarity searches" icon={ScatterChart}>
    {#snippet children()}
      <button
        class="btn btn-primary btn-sm"
        onclick={openCreateDialog}
        disabled={subsystemDisabled}
        title={subsystemDisabled ? 'Vector database subsystem is not enabled on this node' : undefined}
      >
        <Plus class="h-4 w-4" />
        New Namespace
      </button>
    {/snippet}
  </PageHeader>

  {#if subsystemDisabled}
    <div class="alert alert-info text-sm mb-4" role="alert">
      <span>
        Vector database is not enabled on this node. Start the daemon with
        <code class="font-mono">QUICBAAS_VECTOR_ENABLED=true</code>
        or add <code class="font-mono">--with-vector</code> to the build.
      </span>
    </div>
  {:else if loadError}
    <div class="alert alert-error text-sm mb-4 flex items-center justify-between">
      <span>{loadError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void fetchNamespaces(); }}>Retry</button>
    </div>
  {/if}

  <!-- Two-panel layout: namespace list | search panel -->
  <div class="flex gap-6 flex-wrap lg:flex-nowrap">

    <!-- Namespace list -->
    <div class="flex-1 min-w-0">
      <h2 class="text-sm font-semibold text-base-content/50 uppercase tracking-wider mb-3">Namespaces</h2>
      {#if loading}
        <div class="flex justify-center py-8"><span class="loading loading-spinner loading-sm"></span></div>
      {:else if namespaces.length === 0}
        <div class="text-center text-base-content/30 py-8 text-sm">No namespaces found.</div>
      {:else}
        <div class="space-y-2">
          {#each namespaces as ns (ns.name)}
            <div
              class="card card-sm bg-base-200 cursor-pointer"
              class:ring-2={searchNamespace === ns.name}
              class:ring-primary={searchNamespace === ns.name}
              role="button"
              tabindex="0"
              onclick={() => { searchNamespace = ns.name; }}
              onkeydown={(e) => { if (e.key === 'Enter') searchNamespace = ns.name; }}
            >
              <div class="card-body flex-row items-center gap-4">
                <div class="flex-1 min-w-0">
                  <p class="font-semibold text-sm truncate">{ns.name}</p>
                  <p class="text-xs text-base-content/40">
                    {ns.vector_count ?? 0} vectors
                    {#if ns.dimension} · dim {ns.dimension}{/if}
                    {#if ns.metric} · {ns.metric}{/if}
                  </p>
                </div>
                <div class="flex items-center gap-1">
                  <button
                    class="btn btn-ghost btn-xs text-error"
                    onclick={(e) => { e.stopPropagation(); namespaceToDelete = ns; }}
                    aria-label={`Delete namespace ${ns.name}`}
                    title={`Delete namespace ${ns.name}`}
                  >
                    <Trash2 class="h-3 w-3" />
                  </button>
                  <ChevronRight class="h-4 w-4 text-base-content/30" />
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Search panel -->
    <div class="w-full lg:w-96 shrink-0">
      <h2 class="text-sm font-semibold text-base-content/50 uppercase tracking-wider mb-3">Similarity Search</h2>

      <div class="card bg-base-200">
        <div class="card-body gap-3">
          <label class="form-control">
            <div class="label py-1"><span class="label-text text-xs">Namespace</span></div>
            <select class="select select-bordered select-sm" bind:value={searchNamespace}>
              <option value="">Select namespace…</option>
              {#each namespaces as ns (ns.name)}
                <option value={ns.name}>{ns.name}</option>
              {/each}
            </select>
          </label>

          <label class="form-control">
            <div class="label py-1"><span class="label-text text-xs">Text query</span></div>
            <input
              type="text"
              class="input input-bordered input-sm"
              bind:value={searchQuery}
              placeholder="Enter search text…"
            />
          </label>

          <label class="form-control">
            <div class="label py-1"><span class="label-text text-xs">Top-K results</span></div>
            <input
              type="number"
              class="input input-bordered input-sm w-24"
              bind:value={searchTopK}
              min="1"
              max="100"
            />
          </label>

          {#if searchError}
            <div class="alert alert-error text-xs">{searchError}</div>
          {/if}

          <button
            class="btn btn-primary btn-block gap-2"
            onclick={() => { void handleSearch(); }}
            disabled={searching || subsystemDisabled}
            title={subsystemDisabled ? 'Vector database subsystem is not enabled on this node' : undefined}
            aria-label="Run similarity search"
          >
            {#if searching}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <Search class="h-4 w-4 shrink-0" />
            {/if}
            <span>Search</span>
          </button>

          {#if searchResults.length > 0}
            <div class="divider my-1 text-xs">Results ({searchResults.length})</div>
            <div class="space-y-1 max-h-64 overflow-y-auto">
              {#each searchResults as result (result.id)}
                <div class="bg-base-100 rounded p-2 text-xs">
                  <div class="flex items-center justify-between gap-2">
                    <span class="font-mono truncate">{result.id}</span>
                    <span class="badge badge-sm badge-primary shrink-0">
                      {result.score.toFixed(4)}
                    </span>
                  </div>
                  {#if result.metadata && Object.keys(result.metadata).length > 0}
                    <pre class="text-[10px] text-base-content/40 mt-1 overflow-hidden">{JSON.stringify(result.metadata)}</pre>
                  {/if}
                </div>
              {/each}
            </div>
          {:else if !searching && searchQuery}
            <p class="text-xs text-base-content/30 text-center">No results.</p>
          {/if}
        </div>
      </div>
    </div>

  </div>
</div>

<!-- Delete namespace confirm dialog -->
{#if namespaceToDelete}
  <dialog class="modal modal-open" aria-label="Delete namespace confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Namespace?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{namespaceToDelete.name}</span> and all its vectors? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { namespaceToDelete = null; }}>Cancel</button>
        <button class="btn btn-error" onclick={() => { void confirmDeleteNamespace(); }} disabled={deletingNamespace}>
          {#if deletingNamespace}<span class="loading loading-spinner loading-xs"></span>{/if}
          Delete
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { namespaceToDelete = null; }} aria-label="Close delete dialog"></button>
  </dialog>
{/if}

<!-- Create namespace dialog -->
{#if showCreateDialog}
  <dialog class="modal modal-open" aria-label="Create namespace dialog">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-4">New Vector Namespace</h3>

      {#if createError}
        <div class="alert alert-error text-sm mb-3">{createError}</div>
      {/if}

      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Namespace Name</span></div>
        <input
          type="text"
          class="input input-bordered"
          bind:value={createName}
          placeholder="my-collection"
        />
      </label>

      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Dimension</span></div>
        <input
          type="number"
          class="input input-bordered"
          bind:value={createDimension}
          min="1"
          max="65536"
        />
      </label>

      <label class="form-control mb-4">
        <div class="label"><span class="label-text">Distance Metric</span></div>
        <select class="select select-bordered" bind:value={createMetric}>
          <option value="cosine">cosine</option>
          <option value="euclidean">euclidean</option>
          <option value="dot_product">dot_product</option>
        </select>
      </label>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { showCreateDialog = false; }}>Cancel</button>
        <button class="btn btn-primary" onclick={() => { void handleCreate(); }} disabled={creating}>
          {#if creating}<span class="loading loading-spinner loading-xs"></span>{/if}
          Create
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { showCreateDialog = false; }} aria-label="Close dialog"></button>
  </dialog>
{/if}
