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
  ClusterOperationsPanel runs the cluster's long operations: it shows the
  rebalance plan, starts a rebalance and follows its progress, and starts a
  data migration between two shards, follows its progress and cancels it
  after a confirmation. Progress is read at a fixed interval until a terminal
  status, an error, a session change or unmount. A session change also drops
  any answer still in flight and clears what the previous principal saw.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { authStore } from '../../stores/auth.svelte.ts';
  import type {
    ClusterClient,
    RebalancePlanResponse,
    RebalanceRequest,
    RebalanceStatusResponse,
    MigrationRequest,
    MigrationStatusResponse,
  } from '@quicbaas/sdk';
  import { Shuffle, ArrowRightLeft, X } from 'lucide-svelte';

  /** Milliseconds between two progress reads. */
  const POLL_INTERVAL_MS = 2000;
  const TERMINAL = new Set([
    'completed', 'complete', 'succeeded', 'success', 'done',
    'failed', 'error', 'cancelled', 'canceled', 'not_running', 'not_found',
  ]);
  const UNAVAILABLE = 'Cluster operations are not available over this connection';

  function errText(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  function isTerminal(status: string | undefined): boolean {
    return TERMINAL.has((status ?? '').toLowerCase());
  }

  /**
   * parseCount reads a non-negative whole number from a form field. It
   * returns undefined for an empty optional field and an error text for a
   * missing required field or a value that is not a number.
   */
  function parseCount(raw: string, label: string, required: boolean): number | undefined | string {
    const v = raw.trim();
    if (v === '') return required ? `${label} is required` : undefined;
    if (!/^\d+$/.test(v)) return `${label} must be a whole number`;
    return Number(v);
  }

  /** clusterFor returns the cluster client, or null when the transport has none. */
  async function clusterFor(): Promise<ClusterClient | null> {
    const client = await getSDKClient();
    return client.cluster ?? null;
  }

  // ---------------------------------------------------------------------------
  // Polling: read progress now, then every POLL_INTERVAL_MS until a terminal
  // status, an error, a session change or unmount.
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
  // Rebalance
  // ---------------------------------------------------------------------------

  let plan = $state<RebalancePlanResponse | null>(null);
  let planLoading = $state(false);
  let rebalanceStrategy = $state('');
  let rebalanceTarget = $state('');
  let rebalanceStarting = $state(false);
  let rebalanceOpId = $state('');
  let rebalanceProgress = $state<RebalanceStatusResponse | null>(null);
  let rebalanceError = $state('');
  let stopRebalance: (() => void) | null = null;

  function stopRebalancePoll(): void {
    stopRebalance?.();
    stopRebalance = null;
  }

  async function showPlan(): Promise<void> {
    const forSession = authStore.sessionKey;
    planLoading = true;
    rebalanceError = '';
    try {
      const cluster = await clusterFor();
      if (forSession !== authStore.sessionKey) return;
      if (!cluster) { rebalanceError = UNAVAILABLE; return; }
      const resp = await cluster.rebalancePlan();
      if (forSession !== authStore.sessionKey) return;
      plan = resp;
    } catch (err: unknown) {
      if (forSession !== authStore.sessionKey) return;
      rebalanceError = errText(err);
    } finally {
      if (forSession === authStore.sessionKey) planLoading = false;
    }
  }

  async function startRebalance(): Promise<void> {
    rebalanceError = '';
    const target = parseCount(rebalanceTarget, 'Target shard count', false);
    if (typeof target === 'string') { rebalanceError = target; return; }
    const req: RebalanceRequest = {};
    if (rebalanceStrategy.trim() !== '') req.strategy = rebalanceStrategy.trim();
    if (target !== undefined) req.target_shard_count = target;

    const forSession = authStore.sessionKey;
    rebalanceStarting = true;
    stopRebalancePoll();
    try {
      const cluster = await clusterFor();
      if (forSession !== authStore.sessionKey) return;
      if (!cluster) { rebalanceError = UNAVAILABLE; return; }
      const resp = await cluster.startRebalance(req);
      if (forSession !== authStore.sessionKey) return;
      rebalanceOpId = resp.operation_id;
      rebalanceProgress = null;
      showToast({ message: `Rebalance ${resp.operation_id} started.`, type: 'success' });
      const opId = resp.operation_id;
      stopRebalance = poll(
        () => cluster.rebalanceStatus(opId),
        (v) => { rebalanceProgress = v; },
        (msg) => { rebalanceError = msg; },
        () => { stopRebalance = null; },
      );
    } catch (err: unknown) {
      if (forSession !== authStore.sessionKey) return;
      rebalanceError = errText(err);
    } finally {
      if (forSession === authStore.sessionKey) rebalanceStarting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Migration
  // ---------------------------------------------------------------------------

  let migrationSource = $state('');
  let migrationTarget = $state('');
  let migrationEntityTypes = $state('');
  let migrationStarting = $state(false);
  let migrationOpId = $state('');
  let migrationStartStatus = $state('');
  let migrationProgress = $state<MigrationStatusResponse | null>(null);
  let migrationError = $state('');
  let confirmCancel = $state(false);
  let cancelling = $state(false);
  let migrationCluster: ClusterClient | null = null;
  let stopMigration: (() => void) | null = null;

  function stopMigrationPoll(): void {
    stopMigration?.();
    stopMigration = null;
  }

  const migrationStatusText = $derived(migrationProgress?.status ?? migrationStartStatus);
  const migrationRunning = $derived(migrationOpId !== '' && !isTerminal(migrationStatusText));

  async function startMigration(): Promise<void> {
    migrationError = '';
    const source = parseCount(migrationSource, 'Source shard', true);
    if (typeof source !== 'number') { migrationError = source ?? 'Source shard is required'; return; }
    const target = parseCount(migrationTarget, 'Target shard', true);
    if (typeof target !== 'number') { migrationError = target ?? 'Target shard is required'; return; }
    const req: MigrationRequest = { source_shard_id: source, target_shard_id: target };
    const types = migrationEntityTypes.split(',').map((t) => t.trim()).filter((t) => t !== '');
    if (types.length > 0) req.entity_types = types;

    const forSession = authStore.sessionKey;
    migrationStarting = true;
    stopMigrationPoll();
    try {
      const cluster = await clusterFor();
      if (forSession !== authStore.sessionKey) return;
      if (!cluster) { migrationError = UNAVAILABLE; return; }
      const resp = await cluster.startMigration(req);
      if (forSession !== authStore.sessionKey) return;
      migrationCluster = cluster;
      migrationOpId = resp.operation_id;
      migrationStartStatus = resp.status;
      migrationProgress = null;
      showToast({ message: `Migration ${resp.operation_id} started.`, type: 'success' });
      const opId = resp.operation_id;
      stopMigration = poll(
        () => cluster.migrationStatus(opId),
        (v) => { migrationProgress = v; },
        (msg) => { migrationError = msg; },
        () => { stopMigration = null; },
      );
    } catch (err: unknown) {
      if (forSession !== authStore.sessionKey) return;
      migrationError = errText(err);
    } finally {
      if (forSession === authStore.sessionKey) migrationStarting = false;
    }
  }

  async function cancelMigration(): Promise<void> {
    const cluster = migrationCluster;
    const opId = migrationOpId;
    if (!cluster || opId === '') return;
    const forSession = authStore.sessionKey;
    cancelling = true;
    migrationError = '';
    stopMigrationPoll();
    try {
      const resp = await cluster.cancelMigration(opId);
      if (forSession !== authStore.sessionKey) return;
      if (resp.cancelled) {
        migrationStartStatus = 'cancelled';
        if (migrationProgress) migrationProgress = { ...migrationProgress, status: 'cancelled' };
        showToast({ message: `Migration ${opId} cancelled.`, type: 'success' });
      } else {
        migrationError = resp.message || `Migration ${opId} was not cancelled`;
      }
    } catch (err: unknown) {
      if (forSession !== authStore.sessionKey) return;
      migrationError = errText(err);
    } finally {
      if (forSession === authStore.sessionKey) {
        cancelling = false;
        confirmCancel = false;
      }
    }
  }

  // ---------------------------------------------------------------------------
  // Session guard: a new principal never sees the previous one's operations,
  // and no poll keeps reading for a session that has gone.
  // ---------------------------------------------------------------------------

  $effect(() => {
    void authStore.sessionKey;
    stopRebalancePoll();
    stopMigrationPoll();
    plan = null;
    planLoading = false;
    rebalanceStarting = false;
    rebalanceOpId = '';
    rebalanceProgress = null;
    rebalanceError = '';
    migrationStarting = false;
    migrationCluster = null;
    migrationOpId = '';
    migrationStartStatus = '';
    migrationProgress = null;
    migrationError = '';
    confirmCancel = false;
    cancelling = false;
    return () => {
      stopRebalancePoll();
      stopMigrationPoll();
    };
  });

  function percent(v: number | undefined): string {
    return `${Math.round(v ?? 0)}%`;
  }
</script>

<div class="grid grid-cols-1 lg:grid-cols-2 gap-4" data-testid="cluster-operations">
  <section class="card bg-base-200 shadow-sm" aria-label="Rebalance">
    <div class="card-body gap-3">
      <h2 class="card-title text-base"><Shuffle class="h-4 w-4" /> Rebalance</h2>

      <div class="flex items-center gap-2">
        <button class="btn btn-sm btn-ghost" onclick={() => { void showPlan(); }} disabled={planLoading}>
          {#if planLoading}<span class="loading loading-spinner loading-xs"></span>{/if}
          Show plan
        </button>
      </div>
      {#if plan}
        <p class="text-sm" data-testid="rebalance-plan">
          Current shards {plan.current_shards} &rarr; target {plan.target_shards}
          &nbsp;&bull;&nbsp; strategy <span class="font-mono">{plan.strategy || 'default'}</span>
          &nbsp;&bull;&nbsp; estimated {plan.estimated_time} s
        </p>
      {/if}

      <div class="grid grid-cols-1 sm:grid-cols-2 gap-2">
        <label class="form-control">
          <span class="label-text text-xs">Strategy</span>
          <input class="input input-sm input-bordered" type="text" placeholder="server default" bind:value={rebalanceStrategy} />
        </label>
        <label class="form-control">
          <span class="label-text text-xs">Target shard count</span>
          <input class="input input-sm input-bordered" type="text" inputmode="numeric" placeholder="server default" bind:value={rebalanceTarget} />
        </label>
      </div>
      <div>
        <button class="btn btn-sm btn-primary" onclick={() => { void startRebalance(); }} disabled={rebalanceStarting}>
          {#if rebalanceStarting}<span class="loading loading-spinner loading-xs"></span>{/if}
          Start rebalance
        </button>
      </div>

      {#if rebalanceOpId}
        <div class="text-sm space-y-1" data-testid="rebalance-progress">
          <div>
            Operation <span class="font-mono">{rebalanceOpId}</span>:
            <span class="badge badge-sm">{rebalanceProgress?.status ?? 'started'}</span>
            {percent(rebalanceProgress?.percent_complete)}
            {#if rebalanceProgress?.total_shards}
              &nbsp;&bull;&nbsp; {rebalanceProgress.shards_moved ?? 0} of {rebalanceProgress.total_shards} shards moved
            {/if}
          </div>
          <progress class="progress progress-primary w-full" value={rebalanceProgress?.percent_complete ?? 0} max="100"></progress>
        </div>
      {/if}

      {#if rebalanceError}
        <div class="alert alert-error text-sm" role="alert" data-testid="rebalance-error">{rebalanceError}</div>
      {/if}
    </div>
  </section>

  <section class="card bg-base-200 shadow-sm" aria-label="Migration">
    <div class="card-body gap-3">
      <h2 class="card-title text-base"><ArrowRightLeft class="h-4 w-4" /> Migration</h2>

      <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
        <label class="form-control">
          <span class="label-text text-xs">Source shard</span>
          <input class="input input-sm input-bordered" type="text" inputmode="numeric" bind:value={migrationSource} />
        </label>
        <label class="form-control">
          <span class="label-text text-xs">Target shard</span>
          <input class="input input-sm input-bordered" type="text" inputmode="numeric" bind:value={migrationTarget} />
        </label>
        <label class="form-control">
          <span class="label-text text-xs">Entity types</span>
          <input class="input input-sm input-bordered" type="text" placeholder="all, or comma separated" bind:value={migrationEntityTypes} />
        </label>
      </div>
      <div class="flex items-center gap-2">
        <button class="btn btn-sm btn-primary" onclick={() => { void startMigration(); }} disabled={migrationStarting}>
          {#if migrationStarting}<span class="loading loading-spinner loading-xs"></span>{/if}
          Start migration
        </button>
        {#if migrationRunning}
          <button class="btn btn-sm btn-error btn-outline" onclick={() => { confirmCancel = true; }}>
            <X class="h-4 w-4" /> Cancel migration
          </button>
        {/if}
      </div>

      {#if migrationOpId}
        <div class="text-sm space-y-1" data-testid="migration-progress">
          <div>
            Operation <span class="font-mono">{migrationOpId}</span>:
            <span class="badge badge-sm">{migrationStatusText}</span>
            {percent(migrationProgress?.percent_complete)}
            {#if migrationProgress}
              &nbsp;&bull;&nbsp; {migrationProgress.keys_processed} keys, {migrationProgress.bytes_processed} bytes
            {/if}
          </div>
          <progress class="progress progress-primary w-full" value={migrationProgress?.percent_complete ?? 0} max="100"></progress>
        </div>
      {/if}

      {#if migrationError}
        <div class="alert alert-error text-sm" role="alert" data-testid="migration-error">{migrationError}</div>
      {/if}
    </div>
  </section>
</div>

{#if confirmCancel}
  <dialog class="modal modal-open" open aria-label="Cancel migration confirmation">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-2">Cancel migration?</h3>
      <p class="text-sm text-base-content/70">
        Stop migration <span class="font-mono font-semibold">{migrationOpId}</span>? The data moved so far stays where it is.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { confirmCancel = false; }}>Keep running</button>
        <button class="btn btn-error" onclick={() => { void cancelMigration(); }} disabled={cancelling}>
          {#if cancelling}<span class="loading loading-spinner loading-xs"></span>{/if}
          Confirm cancel
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { confirmCancel = false; }} aria-label="Close cancel dialog"></button>
  </dialog>
{/if}
