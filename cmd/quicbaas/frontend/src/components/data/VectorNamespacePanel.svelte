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
  VectorNamespacePanel shows one vector namespace: its details and stats, an
  index rebuild with progress, a compaction with progress, and export and
  import of its vectors as JSON Lines.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { downloadText } from '../../lib/download.ts';
  import type {
    NamespaceInfo,
    NamespaceStats,
    IndexProgress,
    CompactionProgress,
    RebuildIndexRequest,
    VectorExportEntry,
    ImportVectorsResponse,
  } from '@quicbaas/sdk';
  import { X, RefreshCw, Archive, Download, Upload } from 'lucide-svelte';

  interface Props {
    namespace: string;
    onclose: () => void;
  }

  let { namespace, onclose }: Props = $props();

  /** Milliseconds between two progress reads. */
  const POLL_INTERVAL_MS = 2000;
  const TERMINAL = new Set(['completed', 'complete', 'succeeded', 'success', 'done', 'failed', 'error', 'cancelled', 'canceled']);

  function errText(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  function isTerminal(status: string | undefined): boolean {
    return TERMINAL.has((status ?? '').toLowerCase());
  }

  // ---------------------------------------------------------------------------
  // Details and stats. A namespace change bumps the generation, so a response
  // still in flight for the old name is dropped.
  // ---------------------------------------------------------------------------

  let generation = 0;
  let details = $state<NamespaceInfo | null>(null);
  let stats = $state<NamespaceStats | null>(null);
  let detailsError = $state('');
  let statsError = $state('');
  let loading = $state(false);

  async function load(name: string, gen: number): Promise<void> {
    loading = true;
    details = null;
    stats = null;
    detailsError = '';
    statsError = '';
    let client;
    try {
      client = await getSDKClient();
    } catch (err: unknown) {
      if (gen === generation) { detailsError = errText(err); loading = false; }
      return;
    }
    const infoP = client.vectors.getNamespace(name).then(
      (v) => { if (gen === generation) details = v; },
      (err: unknown) => { if (gen === generation) detailsError = errText(err); },
    );
    const statsP = client.vectors.getNamespaceStats(name).then(
      (v) => { if (gen === generation) stats = v; },
      (err: unknown) => { if (gen === generation) statsError = errText(err); },
    );
    await Promise.all([infoP, statsP]);
    if (gen === generation) loading = false;
  }

  $effect(() => {
    const name = namespace;
    generation += 1;
    const gen = generation;
    stopIndexPoll();
    stopCompactPoll();
    indexProgress = null;
    indexError = '';
    compactProgress = null;
    compactError = '';
    importResult = null;
    importError = '';
    void load(name, gen);
    return () => {
      stopIndexPoll();
      stopCompactPoll();
    };
  });

  // ---------------------------------------------------------------------------
  // Polling: read progress now, then every POLL_INTERVAL_MS until a terminal
  // status, an error, a namespace change or unmount.
  // ---------------------------------------------------------------------------

  function poll<T extends { status: string }>(
    read: () => Promise<T>,
    onUpdate: (v: T) => void,
    onError: (msg: string) => void,
    onDone: () => void,
  ): () => void {
    let stopped = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const tick = async (): Promise<void> => {
      try {
        const v = await read();
        if (stopped) return;
        onUpdate(v);
        if (isTerminal(v.status)) { onDone(); return; }
        timer = setTimeout(() => { void tick(); }, POLL_INTERVAL_MS);
      } catch (err: unknown) {
        if (stopped) return;
        onError(errText(err));
        onDone();
      }
    };
    void tick();
    return () => {
      stopped = true;
      if (timer !== undefined) clearTimeout(timer);
    };
  }

  // ---------------------------------------------------------------------------
  // Index rebuild
  // ---------------------------------------------------------------------------

  let rebuildAsync = $state(true);
  let rebuildTimeoutMs = $state<number | null>(null);
  let rebuildParams = $state('');
  let indexProgress = $state<IndexProgress | null>(null);
  let indexError = $state('');
  let indexRunning = $state(false);
  let stopIndex: (() => void) | null = null;

  function stopIndexPoll(): void {
    stopIndex?.();
    stopIndex = null;
    indexRunning = false;
  }

  async function handleRebuild(): Promise<void> {
    indexError = '';
    const req: RebuildIndexRequest = { async: rebuildAsync };
    if (rebuildParams.trim()) {
      try {
        const parsed: unknown = JSON.parse(rebuildParams);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('not an object');
        req.index_params = Object.fromEntries(
          Object.entries(parsed as Record<string, unknown>).map(([k, v]) => [k, String(v)]),
        );
      } catch {
        indexError = 'Index parameters must be a JSON object.';
        return;
      }
    }
    if (rebuildTimeoutMs && rebuildTimeoutMs > 0) req.timeout_ms = rebuildTimeoutMs;
    const name = namespace;
    const gen = generation;
    stopIndexPoll();
    indexRunning = true;
    indexProgress = null;
    try {
      const client = await getSDKClient();
      const res = await client.vectors.rebuildIndex(name, req);
      if (gen !== generation) return;
      stopIndex = poll(
        () => client.vectors.getIndexProgress(name, res.operation_id),
        (v) => { indexProgress = v; },
        (msg) => { indexError = msg; },
        () => { indexRunning = false; stopIndex = null; },
      );
    } catch (err: unknown) {
      if (gen !== generation) return;
      indexError = errText(err);
      indexRunning = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Compaction
  // ---------------------------------------------------------------------------

  let compactProgress = $state<CompactionProgress | null>(null);
  let compactError = $state('');
  let compactRunning = $state(false);
  let stopCompact: (() => void) | null = null;

  function stopCompactPoll(): void {
    stopCompact?.();
    stopCompact = null;
    compactRunning = false;
  }

  async function handleCompact(): Promise<void> {
    compactError = '';
    const name = namespace;
    const gen = generation;
    stopCompactPoll();
    compactRunning = true;
    compactProgress = null;
    try {
      const client = await getSDKClient();
      const res = await client.vectorAdmin.compactNamespace(name);
      if (gen !== generation) return;
      stopCompact = poll(
        () => client.vectorAdmin.getCompactionProgress(res.operation_id),
        (v) => { compactProgress = v; },
        (msg) => { compactError = msg; },
        () => { compactRunning = false; stopCompact = null; },
      );
    } catch (err: unknown) {
      if (gen !== generation) return;
      compactError = errText(err);
      compactRunning = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Export and import (JSON Lines: one VectorExportEntry per line)
  // ---------------------------------------------------------------------------

  let exporting = $state(false);
  let importing = $state(false);
  let importResult = $state<ImportVectorsResponse | null>(null);
  let importError = $state('');

  async function handleExport(): Promise<void> {
    exporting = true;
    const name = namespace;
    try {
      const client = await getSDKClient();
      const entries = await client.vectorAdmin.exportVectors(name);
      const body = entries.map((e) => JSON.stringify(e) + '\n').join('');
      downloadText(`${name}.jsonl`, body, 'application/x-ndjson');
      showToast({ message: `Exported ${entries.length} vectors from "${name}".`, type: 'success' });
    } catch (err: unknown) {
      showToast({ message: errText(err), type: 'error' });
    } finally {
      exporting = false;
    }
  }

  function readFile(file: File): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result ?? ''));
      reader.onerror = () => reject(reader.error ?? new Error('could not read the file'));
      reader.readAsText(file);
    });
  }

  function parseJSONLines(text: string): VectorExportEntry[] {
    const entries: VectorExportEntry[] = [];
    for (const [i, raw] of text.split('\n').entries()) {
      const line = raw.trim();
      if (!line) continue;
      let v: unknown;
      try {
        v = JSON.parse(line);
      } catch {
        throw new Error(`Line ${i + 1} is not valid JSON.`);
      }
      const entry = v as Partial<VectorExportEntry> | null;
      if (!entry || typeof entry !== 'object' || typeof entry.id !== 'string' || !Array.isArray(entry.vector)) {
        throw new Error(`Line ${i + 1} needs an "id" string and a "vector" array.`);
      }
      entries.push(entry as VectorExportEntry);
    }
    return entries;
  }

  async function handleImportFile(e: Event): Promise<void> {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;
    importError = '';
    importResult = null;
    const name = namespace;
    const gen = generation;
    let entries: VectorExportEntry[];
    try {
      entries = parseJSONLines(await readFile(file));
    } catch (err: unknown) {
      importError = errText(err);
      return;
    }
    if (entries.length === 0) {
      importError = 'The file has no vectors.';
      return;
    }
    importing = true;
    try {
      const client = await getSDKClient();
      const res = await client.vectorAdmin.importVectors(name, entries);
      if (gen === generation) importResult = res;
    } catch (err: unknown) {
      if (gen === generation) importError = errText(err);
    } finally {
      importing = false;
      input.value = '';
    }
  }
</script>

<div class="card bg-base-200" aria-label={`Namespace ${namespace}`}>
  <div class="card-body gap-4">
    <div class="flex items-center justify-between">
      <h2 class="text-base font-semibold font-mono truncate">{namespace}</h2>
      <button class="btn btn-ghost btn-xs" onclick={onclose} aria-label="Close namespace panel">
        <X class="h-4 w-4" />
      </button>
    </div>

    <!-- Details -->
    <section>
      <h3 class="text-xs font-semibold text-base-content/50 uppercase tracking-wider mb-2">Details</h3>
      {#if detailsError}
        <div class="alert alert-error text-xs" data-testid="ns-error">{detailsError}</div>
      {:else if loading && !details}
        <span class="loading loading-spinner loading-sm"></span>
      {:else if details}
        <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
          <dt class="text-base-content/50">Dimension</dt><dd data-testid="ns-dimension">{details.dimensions}</dd>
          <dt class="text-base-content/50">Metric</dt><dd data-testid="ns-metric">{details.distance_metric}</dd>
          <dt class="text-base-content/50">Index type</dt><dd>{details.index_type}</dd>
          <dt class="text-base-content/50">Vectors</dt><dd data-testid="ns-vector-count">{details.vector_count}</dd>
          <dt class="text-base-content/50">Indexed</dt><dd>{details.indexed_count}</dd>
          {#if details.pending_count !== undefined}
            <dt class="text-base-content/50">Pending</dt><dd>{details.pending_count}</dd>
          {/if}
          <dt class="text-base-content/50">Storage</dt><dd>{details.storage_bytes} B</dd>
          <dt class="text-base-content/50">Index size</dt><dd>{details.index_bytes} B</dd>
          {#if details.last_index_build}
            <dt class="text-base-content/50">Last index build</dt><dd>{details.last_index_build}</dd>
          {/if}
        </dl>
      {/if}
    </section>

    <!-- Stats -->
    <section>
      <h3 class="text-xs font-semibold text-base-content/50 uppercase tracking-wider mb-2">Stats</h3>
      {#if statsError}
        <div class="alert alert-error text-xs" data-testid="ns-stats-error">{statsError}</div>
      {:else if stats}
        <dl class="grid grid-cols-2 gap-x-4 gap-y-1 text-xs">
          <dt class="text-base-content/50">Searches</dt><dd data-testid="ns-search-count">{stats.search_count}</dd>
          <dt class="text-base-content/50">Avg latency</dt><dd>{stats.avg_search_latency_ms} ms</dd>
          <dt class="text-base-content/50">Indexed</dt><dd>{stats.indexed_count} / {stats.vector_count}</dd>
        </dl>
      {/if}
    </section>

    <!-- Index -->
    <section class="space-y-2">
      <h3 class="text-xs font-semibold text-base-content/50 uppercase tracking-wider">Index</h3>
      <label class="label cursor-pointer justify-start gap-2 py-0">
        <input type="checkbox" class="checkbox checkbox-xs" bind:checked={rebuildAsync} />
        <span class="label-text text-xs">Run asynchronously</span>
      </label>
      <label class="form-control">
        <div class="label py-1"><span class="label-text text-xs">Index parameters (JSON object, optional)</span></div>
        <input type="text" class="input input-bordered input-xs font-mono" bind:value={rebuildParams} placeholder={'{"m":"16"}'} />
      </label>
      <label class="form-control">
        <div class="label py-1"><span class="label-text text-xs">Timeout (ms, optional)</span></div>
        <input type="number" class="input input-bordered input-xs w-32" bind:value={rebuildTimeoutMs} min="0" />
      </label>
      <button class="btn btn-sm gap-2" onclick={() => { void handleRebuild(); }} disabled={indexRunning}>
        {#if indexRunning}<span class="loading loading-spinner loading-xs"></span>{:else}<RefreshCw class="h-4 w-4" />{/if}
        Rebuild index
      </button>
      {#if indexError}
        <div class="alert alert-error text-xs">{indexError}</div>
      {/if}
      {#if indexProgress}
        <div class="text-xs" data-testid="index-progress">
          <progress class="progress progress-primary w-full" value={indexProgress.percent_complete} max="100"></progress>
          <span>{Math.round(indexProgress.percent_complete)}% · {indexProgress.status}</span>
          {#if indexProgress.error_message}<span class="text-error"> · {indexProgress.error_message}</span>{/if}
        </div>
      {/if}
    </section>

    <!-- Maintenance -->
    <section class="space-y-2">
      <h3 class="text-xs font-semibold text-base-content/50 uppercase tracking-wider">Maintenance</h3>
      <div class="flex flex-wrap gap-2">
        <button class="btn btn-sm gap-2" onclick={() => { void handleCompact(); }} disabled={compactRunning}>
          {#if compactRunning}<span class="loading loading-spinner loading-xs"></span>{:else}<Archive class="h-4 w-4" />{/if}
          Compact
        </button>
        <button class="btn btn-sm gap-2" onclick={() => { void handleExport(); }} disabled={exporting}>
          {#if exporting}<span class="loading loading-spinner loading-xs"></span>{:else}<Download class="h-4 w-4" />{/if}
          Export
        </button>
      </div>
      {#if compactError}
        <div class="alert alert-error text-xs">{compactError}</div>
      {/if}
      {#if compactProgress}
        <div class="text-xs" data-testid="compact-progress">
          <progress class="progress progress-primary w-full" value={compactProgress.percent_complete} max="100"></progress>
          <span>{Math.round(compactProgress.percent_complete)}% · {compactProgress.status}</span>
          {#if compactProgress.space_reclaimed_bytes}<span> · {compactProgress.space_reclaimed_bytes} B reclaimed</span>{/if}
        </div>
      {/if}
      <label class="form-control">
        <div class="label py-1">
          <span class="label-text text-xs flex items-center gap-1"><Upload class="h-3 w-3" /> Import file (JSON Lines)</span>
        </div>
        <input
          type="file"
          accept=".jsonl,.ndjson,application/x-ndjson"
          class="file-input file-input-bordered file-input-xs"
          aria-label="Import file"
          disabled={importing}
          onchange={(e) => { void handleImportFile(e); }}
        />
      </label>
      {#if importError}
        <div class="alert alert-error text-xs" data-testid="import-error">{importError}</div>
      {/if}
      {#if importResult}
        <p class="text-xs" data-testid="import-result">
          {importResult.vectors_imported} imported · {importResult.vectors_skipped} skipped · {importResult.vectors_failed} failed
        </p>
      {/if}
    </section>
  </div>
</div>
