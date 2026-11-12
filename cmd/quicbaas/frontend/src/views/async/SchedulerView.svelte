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
  SchedulerView lists all registered cron/one-time jobs and allows creating,
  deleting, and immediately triggering them.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import type { SchedulerJob, CreateJobRequest, SchedulerStatus, PlacementNodeInfo, Placement } from '@quicbaas/sdk';
  import { CalendarClock, Plus, RefreshCw, Trash2, Play, Pause, Square, Pencil, X, Server } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';

  // ---------------------------------------------------------------------------
  // Job list state
  // ---------------------------------------------------------------------------

  let jobs = $state<SchedulerJob[]>([]);

  const JOB_COLUMNS: Column<SchedulerJob>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'type', label: 'Type', sortable: true, searchable: true },
    { key: 'schedule', label: 'Schedule', sortable: true, searchable: true },
    { key: 'handler', label: 'Handler', sortable: true, searchable: true },
    { key: 'status', label: 'Status', sortable: true },
    { key: 'next_run', label: 'Next Run', sortable: true },
    { key: 'run_count', label: 'Runs', sortable: true },
    { key: 'actions', label: 'Actions', headerClass: 'text-right' },
  ];
  let loading = $state(false);
  let loadError = $state('');

  async function fetchJobs(): Promise<void> {
    loading = true;
    loadError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.scheduler.listJobs({ limit: 100 });
      const raw = resp.jobs ?? [];
      const seen = new Set<SchedulerJob['id']>();
      jobs = raw.filter((j) => {
        if (seen.has(j.id)) return false;
        seen.add(j.id);
        return true;
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      loadError = `Failed to load jobs: ${msg}`;
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchJobs(); });

  // ---------------------------------------------------------------------------
  // Create job dialog
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let createName = $state('');
  let createSchedule = $state('');
  let createHandler = $state('');
  let createPayload = $state('');
  let createType = $state('cron');
  let createError = $state('');
  let creating = $state(false);

  function openCreateDialog(): void {
    showCreateDialog = true;
    createName = '';
    createSchedule = '';
    createHandler = '';
    createPayload = '';
    createType = 'cron';
    createError = '';
  }

  function closeCreateDialog(): void {
    showCreateDialog = false;
  }

  async function handleCreate(): Promise<void> {
    if (!createName.trim()) {
      createError = 'Job name is required.';
      return;
    }
    if (!createHandler.trim()) {
      createError = 'Handler is required.';
      return;
    }
    if (createType === 'cron' && !createSchedule.trim()) {
      createError = 'Cron schedule is required for cron jobs.';
      return;
    }

    let parsedPayload: unknown = undefined;
    if (createPayload.trim()) {
      try {
        parsedPayload = JSON.parse(createPayload.trim()) as unknown;
      } catch {
        createError = 'Payload must be valid JSON.';
        return;
      }
    }

    creating = true;
    createError = '';
    try {
      const client = await getSDKClient();
      await client.scheduler.createJob({
        name: createName.trim(),
        type: createType,
        schedule: createSchedule.trim() || undefined,
        handler: createHandler.trim(),
        payload: parsedPayload,
      });
      showToast({ type: 'success', message: `Job "${createName}" created.` });
      closeCreateDialog();
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      createError = `Failed to create job: ${msg}`;
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete job (with confirm modal)
  // ---------------------------------------------------------------------------

  let jobToDelete: SchedulerJob | null = $state(null);
  let deletingJob = $state(false);

  async function confirmDeleteJob(): Promise<void> {
    if (!jobToDelete) return;
    deletingJob = true;
    try {
      const client = await getSDKClient();
      await client.scheduler.deleteJob(jobToDelete.id);
      showToast({ type: 'success', message: `Job "${jobToDelete.name}" deleted.` });
      jobToDelete = null;
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Delete failed: ${msg}` });
    } finally {
      deletingJob = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Trigger job (Run Now)
  // ---------------------------------------------------------------------------

  let triggeringJob: number | null = $state(null);

  async function handleTrigger(job: SchedulerJob): Promise<void> {
    triggeringJob = job.id;
    try {
      const client = await getSDKClient();
      await client.scheduler.triggerJob(job.id);
      showToast({ type: 'success', message: `Job "${job.name}" triggered.` });
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Trigger failed: ${msg}` });
    } finally {
      triggeringJob = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Pause / Resume job
  // ---------------------------------------------------------------------------

  let togglingJob: number | null = $state(null);

  async function handlePauseResume(job: SchedulerJob): Promise<void> {
    togglingJob = job.id;
    try {
      const client = await getSDKClient();
      if (job.status === 'paused') {
        await client.scheduler.resumeJob(job.id);
        showToast({ type: 'success', message: `Job "${job.name}" resumed.` });
      } else {
        await client.scheduler.pauseJob(job.id);
        showToast({ type: 'success', message: `Job "${job.name}" paused.` });
      }
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Failed: ${msg}` });
    } finally {
      togglingJob = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Edit job dialog
  // ---------------------------------------------------------------------------

  let jobToEdit: SchedulerJob | null = $state(null);
  let editName = $state('');
  let editSchedule = $state('');
  let editHandler = $state('');
  let editPayload = $state('');
  let editError = $state('');
  let editingJob = $state(false);

  function openEditJob(job: SchedulerJob): void {
    jobToEdit = job;
    editName = job.name;
    editSchedule = job.schedule ?? '';
    editHandler = job.handler;
    editPayload = job.payload ? JSON.stringify(job.payload, null, 2) : '';
    editError = '';
  }

  async function submitEditJob(): Promise<void> {
    if (!jobToEdit) return;
    editError = '';
    let parsedPayload: unknown = undefined;
    if (editPayload.trim()) {
      try {
        parsedPayload = JSON.parse(editPayload.trim()) as unknown;
      } catch {
        editError = 'Payload must be valid JSON.';
        return;
      }
    }
    editingJob = true;
    try {
      const client = await getSDKClient();
      const req: Partial<CreateJobRequest> = {
        name: editName.trim() || undefined,
        schedule: editSchedule.trim() || undefined,
        handler: editHandler.trim() || undefined,
        payload: parsedPayload,
      };
      await client.scheduler.updateJob(jobToEdit.id, req);
      showToast({ type: 'success', message: `Job "${editName || jobToEdit.name}" updated.` });
      jobToEdit = null;
      await fetchJobs();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      editError = `Update failed: ${msg}`;
    } finally {
      editingJob = false;
    }
  }

  function formatNextRun(nextRun?: string): string {
    if (!nextRun) return '—';
    const d = new Date(nextRun);
    if (isNaN(d.getTime())) return '—';
    return d.toLocaleString();
  }

  function statusVariant(status: string): string {
    if (status === 'active' || status === 'running') return 'badge-success';
    if (status === 'paused') return 'badge-warning';
    if (status === 'failed' || status === 'cancelled') return 'badge-error';
    return 'badge-ghost';
  }

  // ---------------------------------------------------------------------------
  // Placement: scheduler status, nodes and placements
  // ---------------------------------------------------------------------------

  let schedStatus = $state<SchedulerStatus | null>(null);
  let nodes = $state<PlacementNodeInfo[]>([]);
  let placements = $state<Placement[]>([]);
  let placementLoading = $state(false);
  let placementError = $state('');

  function errMsg(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  async function fetchPlacement(): Promise<void> {
    placementLoading = true;
    placementError = '';
    try {
      const client = await getSDKClient();
      const [st, ns, ps] = await Promise.all([
        client.scheduler.getStatus(),
        client.scheduler.listNodes(),
        client.scheduler.listPlacements(),
      ]);
      schedStatus = st;
      nodes = ns ?? [];
      placements = ps ?? [];
    } catch (err: unknown) {
      const msg = errMsg(err);
      placementError = `Failed to load placement: ${msg}`;
      showToast({ type: 'error', message: placementError });
    } finally {
      placementLoading = false;
    }
  }

  $effect(() => { void fetchPlacement(); });

  function formatResources(rec?: Record<string, number>): string {
    if (!rec) return '—';
    const parts = Object.entries(rec).map(([k, v]) => `${k}=${v}`);
    return parts.length ? parts.join(', ') : '—';
  }

  function nodeState(node: PlacementNodeInfo): string {
    if (node.status?.draining) return 'draining';
    if (node.status?.unschedulable) return 'cordoned';
    return 'ready';
  }

  function nodeStateVariant(node: PlacementNodeInfo): string {
    const s = nodeState(node);
    if (s === 'ready') return 'badge-success';
    if (s === 'draining') return 'badge-error';
    return 'badge-warning';
  }

  let nodeDetail = $state<PlacementNodeInfo | null>(null);
  let placementDetail = $state<Placement | null>(null);
  let busyNode: string | null = $state(null);

  async function showNodeDetails(nodeId: string): Promise<void> {
    try {
      const client = await getSDKClient();
      nodeDetail = await client.scheduler.getNode(nodeId);
    } catch (err: unknown) {
      showToast({ type: 'error', message: `Failed to load node: ${errMsg(err)}` });
    }
  }

  async function showPlacementDetails(workloadId: string): Promise<void> {
    try {
      const client = await getSDKClient();
      placementDetail = await client.scheduler.getPlacement(workloadId);
    } catch (err: unknown) {
      showToast({ type: 'error', message: `Failed to load placement: ${errMsg(err)}` });
    }
  }

  async function toggleCordon(node: PlacementNodeInfo): Promise<void> {
    const id = node.facts.node_id;
    busyNode = id;
    try {
      const client = await getSDKClient();
      if (node.status?.unschedulable) {
        await client.scheduler.uncordonNode(id);
        showToast({ type: 'success', message: `Node "${id}" uncordoned.` });
      } else {
        await client.scheduler.cordonNode(id);
        showToast({ type: 'success', message: `Node "${id}" cordoned.` });
      }
      await fetchPlacement();
    } catch (err: unknown) {
      showToast({ type: 'error', message: `Failed: ${errMsg(err)}` });
    } finally {
      busyNode = null;
    }
  }

  type PendingAction = { kind: 'drain' | 'deregister' | 'release'; id: string };
  let pendingAction = $state<PendingAction | null>(null);
  let confirming = $state(false);

  const ACTION_LABEL: Record<PendingAction['kind'], string> = {
    drain: 'Drain',
    deregister: 'Deregister',
    release: 'Release',
  };

  async function confirmPendingAction(): Promise<void> {
    if (!pendingAction) return;
    const { kind, id } = pendingAction;
    confirming = true;
    try {
      const client = await getSDKClient();
      if (kind === 'drain') {
        const moved = await client.scheduler.drainNode(id);
        const n = moved?.length ?? 0;
        showToast({ type: 'success', message: `Node "${id}" drained: ${n} placement${n === 1 ? '' : 's'} to move.` });
      } else if (kind === 'deregister') {
        await client.scheduler.deregisterNode(id);
        showToast({ type: 'success', message: `Node "${id}" deregistered.` });
      } else {
        await client.scheduler.release(id);
        showToast({ type: 'success', message: `Placement "${id}" released.` });
      }
      pendingAction = null;
      await fetchPlacement();
    } catch (err: unknown) {
      showToast({ type: 'error', message: `${ACTION_LABEL[kind]} failed: ${errMsg(err)}` });
    } finally {
      confirming = false;
    }
  }
</script>

<div class="flex h-full flex-col">
  <div class="p-6 pb-0">
    <PageHeader title="Scheduler" subtitle="Cron and one-time scheduled jobs" icon={CalendarClock}>
      {#snippet children()}
        <button
          class="btn btn-ghost btn-sm gap-2"
          onclick={fetchJobs}
          disabled={loading}
          aria-label="Refresh jobs"
        >
          <RefreshCw class={`h-4 w-4${loading ? ' animate-spin' : ''}`} />
          Refresh
        </button>
        <button
          class="btn btn-primary btn-sm gap-2"
          onclick={openCreateDialog}
          aria-label="Create Job"
        >
          <Plus class="h-4 w-4" />
          Create Job
        </button>
      {/snippet}
    </PageHeader>
  </div>

  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-6">

    <!-- Job Table -->
    <section class="card bg-base-200 shadow-sm">
      <div class="card-body">

        {#if loadError}
          <div class="alert alert-error text-sm flex items-center justify-between">
            <span>{loadError}</span>
            <button class="btn btn-xs btn-ghost" onclick={() => { void fetchJobs(); }}>Retry</button>
          </div>
        {:else if loading}
          <div class="flex justify-center py-10">
            <span class="loading loading-spinner loading-md"></span>
          </div>
        {:else}
          <DataTable
            columns={JOB_COLUMNS}
            rows={jobs}
            rowKey={(job: SchedulerJob) => job.id}
            urlKey="schedjobs"
            ariaLabel="Scheduled jobs"
            searchPlaceholder="Search jobs..."
            searchLabel="Search scheduled jobs"
            noMatchText="No jobs match the search."
          >
            {#snippet empty()}
              <EmptyState
                icon={CalendarClock}
                title="No scheduled jobs — Create Job is how you start."
                description="Schedule recurring cron jobs or one-time delayed tasks to automate your workflows."
                actionLabel="Create Job"
                onaction={openCreateDialog}
              />
            {/snippet}

            {#snippet row(job: SchedulerJob)}
              <tr>
                <td class="font-semibold">{job.name}</td>
                <td>
                  <span class="badge badge-outline badge-xs">{job.type}</span>
                </td>
                <td class="font-mono text-xs">{job.schedule ?? '—'}</td>
                <td class="font-mono text-xs">{job.handler}</td>
                <td>
                  <span class="badge badge-sm {statusVariant(job.status)}">{job.status}</span>
                </td>
                <td class="text-xs text-base-content/60">{formatNextRun(job.next_run)}</td>
                <td class="text-xs">
                  {job.run_count}
                  {#if job.fail_count > 0}
                    <span class="text-error ml-1">({job.fail_count} failed)</span>
                  {/if}
                </td>
                <td class="text-right">
                  <div class="flex justify-end gap-1">
                    <button
                      class="btn btn-ghost btn-xs"
                      disabled={triggeringJob === job.id}
                      onclick={() => void handleTrigger(job)}
                      title={`Run job ${job.name} now`}
                      aria-label={`Run job ${job.name} now`}
                    >
                      <Play class="h-3 w-3" />
                    </button>
                    <button
                      class="btn btn-ghost btn-xs"
                      disabled={togglingJob === job.id}
                      onclick={() => void handlePauseResume(job)}
                      title={job.status === 'paused' ? `Resume job ${job.name}` : `Pause job ${job.name}`}
                      aria-label={job.status === 'paused' ? `Resume job ${job.name}` : `Pause job ${job.name}`}
                    >
                      {#if job.status === 'paused'}
                        <Square class="h-3 w-3" />
                      {:else}
                        <Pause class="h-3 w-3" />
                      {/if}
                    </button>
                    <button
                      class="btn btn-ghost btn-xs"
                      onclick={() => openEditJob(job)}
                      title={`Edit job ${job.name}`}
                      aria-label={`Edit job ${job.name}`}
                    >
                      <Pencil class="h-3 w-3" />
                    </button>
                    <button
                      class="btn btn-ghost btn-xs text-error"
                      onclick={() => { jobToDelete = job; }}
                      title={`Delete job ${job.name}`}
                      aria-label={`Delete job ${job.name}`}
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
    </section>

    <!-- Placement -->
    <section class="card bg-base-200 shadow-sm" aria-label="Placement">
      <div class="card-body gap-4">
        <div class="flex items-center justify-between">
          <h2 class="card-title text-base gap-2"><Server class="h-4 w-4" /> Placement</h2>
          <button
            class="btn btn-ghost btn-sm gap-2"
            onclick={() => { void fetchPlacement(); }}
            disabled={placementLoading}
            aria-label="Refresh placement"
          >
            <RefreshCw class={`h-4 w-4${placementLoading ? ' animate-spin' : ''}`} />
            Refresh
          </button>
        </div>

        {#if placementError}
          <div class="alert alert-error text-sm flex items-center justify-between">
            <span>{placementError}</span>
            <button class="btn btn-xs btn-ghost" onclick={() => { void fetchPlacement(); }}>Retry</button>
          </div>
        {/if}

        {#if schedStatus}
          <dl class="grid grid-cols-2 gap-2 text-sm md:grid-cols-5" aria-label="Scheduler status">
            <div><dt class="text-base-content/60 text-xs">Running</dt><dd>{schedStatus.running ? 'yes' : 'no'}</dd></div>
            <div><dt class="text-base-content/60 text-xs">Jobs</dt><dd>{schedStatus.stats?.job_count ?? '—'}</dd></div>
            <div><dt class="text-base-content/60 text-xs">Running jobs</dt><dd>{schedStatus.stats?.running_jobs ?? '—'}</dd></div>
            <div><dt class="text-base-content/60 text-xs">Successes</dt><dd>{schedStatus.stats?.success_count ?? '—'}</dd></div>
            <div><dt class="text-base-content/60 text-xs">Failures</dt><dd>{schedStatus.stats?.failure_count ?? '—'}</dd></div>
          </dl>
        {/if}

        <h3 class="text-sm font-semibold">Nodes</h3>
        {#if nodes.length === 0}
          <p class="text-sm text-base-content/60">No placement nodes registered.</p>
        {:else}
          <div class="overflow-x-auto">
            <table class="table table-sm" aria-label="Placement nodes">
              <thead>
                <tr>
                  <th>Node</th><th>State</th><th>Capacity</th><th>Allocated</th>
                  <th>Placements</th><th>TTL (s)</th><th>Registered</th><th class="text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {#each nodes as node (node.facts.node_id)}
                  <tr>
                    <td class="font-mono text-xs">{node.facts.node_id}</td>
                    <td><span class="badge badge-sm {nodeStateVariant(node)}">{nodeState(node)}</span></td>
                    <td class="font-mono text-xs">{formatResources(node.facts.capacity)}</td>
                    <td class="font-mono text-xs">{formatResources(node.allocated)}</td>
                    <td>{node.placements}</td>
                    <td>{node.ttl_seconds}</td>
                    <td class="text-xs text-base-content/60">{formatNextRun(node.registered_at)}</td>
                    <td class="text-right">
                      <div class="flex justify-end gap-1">
                        <button class="btn btn-ghost btn-xs" onclick={() => void showNodeDetails(node.facts.node_id)} aria-label={`Details for node ${node.facts.node_id}`}>Details</button>
                        <button
                          class="btn btn-ghost btn-xs"
                          disabled={busyNode === node.facts.node_id}
                          onclick={() => void toggleCordon(node)}
                          aria-label={node.status?.unschedulable ? `Uncordon node ${node.facts.node_id}` : `Cordon node ${node.facts.node_id}`}
                        >{node.status?.unschedulable ? 'Uncordon' : 'Cordon'}</button>
                        <button class="btn btn-ghost btn-xs" onclick={() => { pendingAction = { kind: 'drain', id: node.facts.node_id }; }} aria-label={`Drain node ${node.facts.node_id}`}>Drain</button>
                        <button class="btn btn-ghost btn-xs text-error" onclick={() => { pendingAction = { kind: 'deregister', id: node.facts.node_id }; }} aria-label={`Deregister node ${node.facts.node_id}`}>Deregister</button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}

        <h3 class="text-sm font-semibold">Placements</h3>
        {#if placements.length === 0}
          <p class="text-sm text-base-content/60">No workload placements.</p>
        {:else}
          <div class="overflow-x-auto">
            <table class="table table-sm" aria-label="Workload placements">
              <thead>
                <tr>
                  <th>Workload</th><th>Node</th><th>Group</th><th>Resources</th>
                  <th>Score</th><th>Created</th><th class="text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {#each placements as p (p.workload_id)}
                  <tr>
                    <td class="font-mono text-xs">
                      {p.workload_id}
                      {#if p.orphaned}<span class="badge badge-warning badge-xs ml-1">orphaned</span>{/if}
                    </td>
                    <td class="font-mono text-xs">{p.node_id}</td>
                    <td class="text-xs">{p.group ?? '—'}</td>
                    <td class="font-mono text-xs">{formatResources(p.resources)}</td>
                    <td>{p.score}</td>
                    <td class="text-xs text-base-content/60">{formatNextRun(p.created_at)}</td>
                    <td class="text-right">
                      <div class="flex justify-end gap-1">
                        <button class="btn btn-ghost btn-xs" onclick={() => void showPlacementDetails(p.workload_id)} aria-label={`Details for placement ${p.workload_id}`}>Details</button>
                        <button class="btn btn-ghost btn-xs text-error" onclick={() => { pendingAction = { kind: 'release', id: p.workload_id }; }} aria-label={`Release placement ${p.workload_id}`}>Release</button>
                      </div>
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        {/if}
      </div>
    </section>

  </div>
</div>

<!-- Placement confirm dialog (drain / deregister / release) -->
{#if pendingAction}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label={`Confirm ${pendingAction.kind}`}>
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">{ACTION_LABEL[pendingAction.kind]}?</h3>
      <p class="text-sm text-base-content/70">
        {#if pendingAction.kind === 'release'}
          Release the placement of workload <span class="font-mono font-semibold">{pendingAction.id}</span>?
        {:else if pendingAction.kind === 'drain'}
          Drain node <span class="font-mono font-semibold">{pendingAction.id}</span>? Its placements will be moved.
        {:else}
          Deregister node <span class="font-mono font-semibold">{pendingAction.id}</span>? Its lease is revoked.
        {/if}
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { pendingAction = null; }}>Cancel</button>
        <button class="btn btn-error gap-2" onclick={confirmPendingAction} disabled={confirming}>
          {#if confirming}<span class="loading loading-spinner loading-xs"></span>{/if}
          {ACTION_LABEL[pendingAction.kind]}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" onclick={() => { pendingAction = null; }}></div>
  </div>
{/if}

<!-- Node details dialog -->
{#if nodeDetail}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Node details">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Node {nodeDetail.facts.node_id}</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={() => { nodeDetail = null; }} aria-label="Close"><X class="h-4 w-4" /></button>
      </div>
      <pre class="bg-base-300 rounded p-3 text-xs overflow-x-auto">{JSON.stringify(nodeDetail, null, 2)}</pre>
    </div>
    <div class="modal-backdrop" onclick={() => { nodeDetail = null; }}></div>
  </div>
{/if}

<!-- Placement details dialog -->
{#if placementDetail}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Placement details">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Placement {placementDetail.workload_id}</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={() => { placementDetail = null; }} aria-label="Close"><X class="h-4 w-4" /></button>
      </div>
      <pre class="bg-base-300 rounded p-3 text-xs overflow-x-auto">{JSON.stringify(placementDetail, null, 2)}</pre>
    </div>
    <div class="modal-backdrop" onclick={() => { placementDetail = null; }}></div>
  </div>
{/if}

<!-- Delete Job Confirm Dialog -->
{#if jobToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete job confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Job?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{jobToDelete.name}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { jobToDelete = null; }}>Cancel</button>
        <button class="btn btn-error gap-2" onclick={confirmDeleteJob} disabled={deletingJob}>
          {#if deletingJob}<span class="loading loading-spinner loading-xs"></span>{/if}
          Delete
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="presentation" onclick={() => { jobToDelete = null; }}></div>
  </div>
{/if}

<!-- Edit Job Dialog -->
{#if jobToEdit}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit Job">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Edit Job</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={() => { jobToEdit = null; }} aria-label="Close"><X class="h-4 w-4" /></button>
      </div>
      <div class="flex flex-col gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Job Name</span></div>
          <input class="input input-bordered w-full" type="text" bind:value={editName} />
        </label>
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Cron Schedule</span></div>
          <input class="input input-bordered w-full font-mono" type="text" bind:value={editSchedule} placeholder="0 0 * * *" />
        </label>
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Handler</span></div>
          <input class="input input-bordered w-full font-mono" type="text" bind:value={editHandler} />
        </label>
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Payload (JSON, optional)</span></div>
          <textarea class="textarea textarea-bordered w-full font-mono text-xs" rows="4" bind:value={editPayload}></textarea>
        </label>
        {#if editError}
          <div class="alert alert-error text-sm">{editError}</div>
        {/if}
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { jobToEdit = null; }}>Cancel</button>
        <button class="btn btn-primary gap-2" onclick={submitEditJob} disabled={editingJob}>
          {#if editingJob}<span class="loading loading-spinner loading-xs"></span>{/if}
          Save
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="presentation" onclick={() => { jobToEdit = null; }}></div>
  </div>
{/if}

<!-- Create Job Dialog -->
{#if showCreateDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create Job">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Create Scheduled Job</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={closeCreateDialog} aria-label="Close">
          <X class="h-4 w-4" />
        </button>
      </div>

      <div class="flex flex-col gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Job Name</span></div>
          <input
            class="input input-bordered w-full"
            type="text"
            placeholder="e.g. nightly-cleanup"
            bind:value={createName}
          />
        </label>

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Job Type</span></div>
          <select class="select select-bordered w-full" bind:value={createType}>
            <option value="cron">Cron (recurring)</option>
            <option value="one_time">One-time</option>
            <option value="delayed">Delayed</option>
          </select>
        </label>

        {#if createType === 'cron'}
          <label class="form-control w-full">
            <div class="label">
              <span class="label-text">Cron Expression</span>
              <span class="label-text-alt text-base-content/40">e.g. 0 0 * * *</span>
            </div>
            <input
              class="input input-bordered w-full font-mono"
              type="text"
              placeholder="0 0 * * *"
              bind:value={createSchedule}
            />
          </label>
        {/if}

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Handler</span></div>
          <input
            class="input input-bordered w-full font-mono"
            type="text"
            placeholder="e.g. cleanup.run"
            bind:value={createHandler}
          />
        </label>

        <label class="form-control w-full">
          <div class="label">
            <span class="label-text">Action Payload (JSON, optional)</span>
          </div>
          <textarea
            class="textarea textarea-bordered w-full font-mono text-xs"
            rows="4"
            placeholder='&#123;"target": "logs", "older_than_days": 30&#125;'
            bind:value={createPayload}
          ></textarea>
        </label>

        {#if createError}
          <div class="alert alert-error text-sm">{createError}</div>
        {/if}
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={closeCreateDialog}>Cancel</button>
        <button
          class="btn btn-primary gap-2"
          onclick={handleCreate}
          disabled={creating}
          aria-label="Create"
        >
          {#if creating}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Create
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="presentation" onclick={closeCreateDialog}></div>
  </div>
{/if}
