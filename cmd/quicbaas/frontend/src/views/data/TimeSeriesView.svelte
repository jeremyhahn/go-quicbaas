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
  TimeSeriesView lists known metrics, allows querying a metric over a time
  range, and provides a write dialog for new data points.
  Mirrors the `quicbaas timeseries write/query` CLI surface.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { TSPoint } from '@quicbaas/sdk';
  import { APIError } from '@quicbaas/sdk';
  import { Plus, Search, BarChart2, Trash2, LineChart } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Metrics list
  // ---------------------------------------------------------------------------

  let metrics = $state<string[]>([]);
  let metricsLoading = $state(false);
  let metricsError = $state('');
  let subsystemDisabled = $state(false);

  function isDisabledError(err: unknown): boolean {
    if (err instanceof APIError && (err.status === 408 || err.status === 503 || err.status === 500 || err.status === 501)) {
      const msg = err.message.toLowerCase();
      return msg.includes('not available') || msg.includes('not configured') || msg.includes('not compiled');
    }
    return false;
  }

  async function fetchMetrics(): Promise<void> {
    metricsLoading = true;
    metricsError = '';
    subsystemDisabled = false;
    try {
      const client = await getSDKClient();
      // Deduplicate metric names: {#each metrics as m (m)} uses the string
      // itself as the key, so server-side duplicates would fire each_key_duplicate.
      metrics = [...new Set(await client.timeseries.listMetrics())];
    } catch (err: unknown) {
      if (isDisabledError(err)) {
        subsystemDisabled = true;
      } else {
        metricsError = err instanceof Error ? err.message : String(err);
      }
    } finally {
      metricsLoading = false;
    }
  }

  $effect(() => { void fetchMetrics(); });

  // ---------------------------------------------------------------------------
  // Query panel
  // ---------------------------------------------------------------------------

  let queryMetric = $state('');
  let queryStart = $state('');
  let queryEnd = $state('');
  let queryResults = $state<TSPoint[]>([]);

  // No rowKey: two points of one metric may share a timestamp, so the row's
  // position in the answer is its only identity — which is what the table
  // falls back to when a view declares none.
  const POINT_COLUMNS: Column<TSPoint>[] = [
    { key: 'timestamp', label: 'Timestamp', sortable: true, searchable: true },
    { key: 'value', label: 'Value', sortable: true },
    { key: 'tags', label: 'Tags', searchable: true, value: (pt) => (pt.tags ? JSON.stringify(pt.tags) : '') },
  ];
  let querying = $state(false);
  let queryError = $state('');

  async function handleQuery(): Promise<void> {
    if (!queryMetric.trim()) {
      queryError = 'Metric name is required.';
      return;
    }
    querying = true;
    queryError = '';
    queryResults = [];
    try {
      const client = await getSDKClient();
      const resp = await client.timeseries.query({
        metric: queryMetric.trim(),
        start: queryStart || undefined,
        end: queryEnd || undefined,
      });
      queryResults = resp.points;
      if (resp.points.length === 0) {
        queryError = 'No data points found for the given criteria.';
      }
    } catch (err: unknown) {
      queryError = err instanceof Error ? err.message : String(err);
    } finally {
      querying = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Write dialog
  // ---------------------------------------------------------------------------

  let showWriteDialog = $state(false);
  let writeMetric = $state('');
  let writeTimestamp = $state('');
  let writeValue = $state('');
  let writeTags = $state('');
  let writeError = $state('');
  let writing = $state(false);

  function openWriteDialog(): void {
    writeMetric = '';
    writeTimestamp = new Date().toISOString();
    writeValue = '';
    writeTags = '';
    writeError = '';
    showWriteDialog = true;
  }

  async function handleWrite(): Promise<void> {
    if (!writeMetric.trim()) {
      writeError = 'Metric name is required.';
      return;
    }
    const val = parseFloat(writeValue);
    if (Number.isNaN(val)) {
      writeError = 'Value must be a number.';
      return;
    }

    let tags: Record<string, string> = {};
    if (writeTags.trim()) {
      try {
        tags = JSON.parse(writeTags) as Record<string, string>;
      } catch {
        writeError = 'Tags must be valid JSON object, e.g. {"host":"web-01"}';
        return;
      }
    }

    writing = true;
    writeError = '';
    try {
      const client = await getSDKClient();
      await client.timeseries.write({
        metric: writeMetric.trim(),
        timestamp: writeTimestamp || new Date().toISOString(),
        value: val,
        tags,
      });
      showToast({ message: 'Data point written.', type: 'success' });
      showWriteDialog = false;
      await fetchMetrics();
    } catch (err: unknown) {
      writeError = err instanceof Error ? err.message : String(err);
    } finally {
      writing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete series (confirm modal)
  // ---------------------------------------------------------------------------

  let metricToDelete = $state<string | null>(null);
  let deletingSeries = $state(false);

  async function confirmDeleteSeries(): Promise<void> {
    if (!metricToDelete) return;
    deletingSeries = true;
    try {
      const client = await getSDKClient();
      await client.timeseries.deleteSeries(metricToDelete);
      showToast({ message: `Series "${metricToDelete}" deleted.`, type: 'success' });
      metricToDelete = null;
      await fetchMetrics();
    } catch (err: unknown) {
      showToast({ message: err instanceof Error ? err.message : String(err), type: 'error' });
    } finally {
      deletingSeries = false;
    }
  }
</script>

<div class="p-6">
  <PageHeader title="Time-Series" subtitle="Write and query time-series metrics" icon={LineChart}>
    {#snippet children()}
      <button
        class="btn btn-primary btn-sm"
        onclick={openWriteDialog}
        disabled={subsystemDisabled}
        title={subsystemDisabled ? 'Time series subsystem is not enabled on this node' : undefined}
      >
        <Plus class="h-4 w-4" />
        Write Point
      </button>
    {/snippet}
  </PageHeader>

  {#if subsystemDisabled}
    <div class="alert alert-info text-sm mb-4" role="alert">
      <span>
        Time series storage is not enabled on this node. Start the daemon with
        <code class="font-mono">QUICBAAS_TIMESERIES_ENABLED=true</code>
        or set <code class="font-mono">timeseries.enabled: true</code>
        in the config file.
      </span>
    </div>
  {/if}

  <!-- Top layout: metric list + query panel -->
  <div class="flex gap-6 flex-wrap lg:flex-nowrap mb-6">

    <!-- Metrics list -->
    <div class="flex-1 min-w-0">
      <h2 class="text-sm font-semibold text-base-content/50 uppercase tracking-wider mb-3">
        Metrics
        {#if metrics.length > 0}
          <span class="badge badge-sm badge-ghost ml-1">{metrics.length}</span>
        {/if}
      </h2>

      {#if metricsLoading}
        <div class="flex justify-center py-8"><span class="loading loading-spinner loading-sm"></span></div>
      {:else if subsystemDisabled}
        <div class="text-center text-base-content/30 py-8 text-sm">Subsystem disabled.</div>
      {:else if metricsError}
        <div class="alert alert-error text-xs flex items-center justify-between">
          <span>{metricsError}</span>
          <button class="btn btn-xs btn-ghost" onclick={() => { void fetchMetrics(); }}>Retry</button>
        </div>
      {:else if metrics.length === 0}
        <div class="text-center text-base-content/30 py-8 text-sm">No metrics found.</div>
      {:else}
        <div class="space-y-1 max-h-64 overflow-y-auto">
          {#each metrics as m (m)}
            <div
              class="flex items-center justify-between gap-2 px-3 py-2 rounded hover:bg-base-200 cursor-pointer"
              class:bg-primary={queryMetric === m}
              class:text-primary-content={queryMetric === m}
              role="button"
              tabindex="0"
              onclick={() => { queryMetric = m; }}
              onkeydown={(e) => { if (e.key === 'Enter') queryMetric = m; }}
            >
              <span class="font-mono text-xs truncate">{m}</span>
              <div class="flex gap-1 shrink-0">
                <button
                  class="btn btn-ghost btn-xs"
                  onclick={(e) => { e.stopPropagation(); queryMetric = m; void handleQuery(); }}
                  aria-label={`Query ${m}`}
                  title="Query metric"
                >
                  <BarChart2 class="h-3 w-3" />
                </button>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  onclick={(e) => { e.stopPropagation(); metricToDelete = m; }}
                  aria-label={`Delete series ${m}`}
                  title={`Delete series ${m}`}
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- Query panel -->
    <div class="w-full lg:w-80 shrink-0">
      <h2 class="text-sm font-semibold text-base-content/50 uppercase tracking-wider mb-3">Query</h2>
      <div class="card bg-base-200">
        <div class="card-body gap-3">
          <label class="form-control">
            <div class="label py-1"><span class="label-text text-xs">Metric</span></div>
            <input
              type="text"
              class="input input-bordered input-sm font-mono"
              bind:value={queryMetric}
              placeholder="cpu.usage"
            />
          </label>

          <label class="form-control">
            <div class="label py-1"><span class="label-text text-xs">Start (ISO 8601)</span></div>
            <input
              type="datetime-local"
              class="input input-bordered input-sm"
              bind:value={queryStart}
            />
          </label>

          <label class="form-control">
            <div class="label py-1"><span class="label-text text-xs">End (ISO 8601)</span></div>
            <input
              type="datetime-local"
              class="input input-bordered input-sm"
              bind:value={queryEnd}
            />
          </label>

          {#if queryError}
            <div class="alert alert-error text-xs">{queryError}</div>
          {/if}

          <button
            class="btn btn-primary btn-sm w-full"
            onclick={() => { void handleQuery(); }}
            disabled={querying || subsystemDisabled}
            title={subsystemDisabled ? 'Time series subsystem is not enabled on this node' : undefined}
          >
            {#if querying}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <Search class="h-4 w-4" />
            {/if}
            Query
          </button>
        </div>
      </div>
    </div>
  </div>

  <!-- Results table -->
  {#if queryResults.length > 0}
    <div>
      <h2 class="text-sm font-semibold text-base-content/50 uppercase tracking-wider mb-3">
        Results for "{queryMetric}"
        <span class="badge badge-sm badge-ghost ml-1">{queryResults.length} points</span>
      </h2>
      <DataTable
        columns={POINT_COLUMNS}
        rows={queryResults}
        urlKey="points"
        ariaLabel="Query results"
        searchPlaceholder="Search points..."
        searchLabel="Search points"
        noMatchText="No points match the search."
      >
        {#snippet row(pt: TSPoint)}
          <tr class="hover:bg-base-200/50">
            <td class="font-mono text-xs">{pt.timestamp}</td>
            <td class="font-mono text-xs">{pt.value}</td>
            <td class="text-xs text-base-content/50">
              {pt.tags ? JSON.stringify(pt.tags) : '—'}
            </td>
          </tr>
        {/snippet}
      </DataTable>
    </div>
  {/if}
</div>

<!-- Delete series confirm dialog -->
{#if metricToDelete}
  <dialog class="modal modal-open" aria-label="Delete series confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Series?</h3>
      <p class="text-sm text-base-content/70">
        Delete all data for metric <span class="font-mono font-semibold">{metricToDelete}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { metricToDelete = null; }}>Cancel</button>
        <button class="btn btn-error" onclick={() => { void confirmDeleteSeries(); }} disabled={deletingSeries}>
          {#if deletingSeries}<span class="loading loading-spinner loading-xs"></span>{/if}
          Delete
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { metricToDelete = null; }} aria-label="Close delete dialog"></button>
  </dialog>
{/if}

<!-- Write dialog -->
{#if showWriteDialog}
  <dialog class="modal modal-open" aria-label="Write data point dialog">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-4">Write Data Point</h3>

      {#if writeError}
        <div class="alert alert-error text-sm mb-3">{writeError}</div>
      {/if}

      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Metric Name</span></div>
        <input
          type="text"
          class="input input-bordered font-mono"
          bind:value={writeMetric}
          placeholder="cpu.usage"
        />
      </label>

      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Timestamp (ISO 8601)</span></div>
        <input
          type="datetime-local"
          class="input input-bordered"
          bind:value={writeTimestamp}
        />
      </label>

      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Value</span></div>
        <input
          type="number"
          class="input input-bordered"
          bind:value={writeValue}
          placeholder="42.5"
          step="any"
        />
      </label>

      <label class="form-control mb-4">
        <div class="label"><span class="label-text">Tags (JSON object, optional)</span></div>
        <input
          type="text"
          class="input input-bordered font-mono text-sm"
          bind:value={writeTags}
          placeholder={"e.g. {\"host\":\"web-01\"}"}
        />
      </label>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { showWriteDialog = false; }}>Cancel</button>
        <button class="btn btn-primary" onclick={() => { void handleWrite(); }} disabled={writing}>
          {#if writing}<span class="loading loading-spinner loading-xs"></span>{/if}
          Write
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { showWriteDialog = false; }} aria-label="Close dialog"></button>
  </dialog>
{/if}
