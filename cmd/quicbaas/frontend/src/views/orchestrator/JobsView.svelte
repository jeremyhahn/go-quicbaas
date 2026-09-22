<!--
  JobsView renders the tenant's orchestration jobs in the shared DataTable, in
  SERVER MODE: the table hands its loader {limit, offset, sort, q} and the
  daemon answers one page with the count of everything that matched. The burn-in
  tenant has ~2,000 jobs; this page never holds more than a hundred.

  The state filter is a SERVER parameter (`statuses`), not a narrowing of the
  rows in hand — "Active" asks for the eleven non-terminal states by name. A
  filter applied to a page of twenty-five would report a total it cannot know
  and hide matches sitting on page two.

  Sorting names the server's allow-listed fields (job_id, description, state,
  started_at). Iteration and Progress are NOT in that list, so their headers do
  not sort rather than ordering one page and looking like they ordered the
  collection.

  LIVE UPDATES ARE A RE-READ, NOT A PATCH. When the job event stream reports a
  state change, the page is fetched again (debounced, late answers discarded by
  the table) — because the event may be about a job this page does not hold,
  and because the order and membership of the page are the server's answer. The
  store keeps patching its own records for the detail views that read them.

  Clicking a row selects the job and navigates to JobDetailView.
  Header provides a "+ Start Job" button that opens StartJobDialog.
  Each row has a "Cancel" action that confirms before cancelling the job.
-->
<script lang="ts">
  import {
    ACTIVE_JOB_STATES,
    orchestratorStore,
    selectJob,
    refresh,
    type JobStatus,
    type JobState,
  } from '../../stores/orchestrator.svelte.ts';
  import { routeJobDetail } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { statusToVariant } from '../../components/core/statusVariant.ts';
  import StartJobDialog from '../../components/orchestrator/StartJobDialog.svelte';
  import { Plus, Briefcase } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column, PageRequest } from '../../lib/dataTable.ts';
  import { formatDateTime } from '../../lib/time.ts';
  import { issueRef, type IssueRef } from '../../lib/tasks.ts';

  type StateFilter = 'all' | 'active' | 'completed' | 'failed';

  /** How long the page waits after a job event before re-reading. */
  const LIVE_REFRESH_DEBOUNCE_MS = 400;

  let stateFilter: StateFilter = $state('all');
  let startJobOpen = $state(false);

  // Job being cancelled (for confirm dialog state).
  let cancelTarget = $state<JobStatus | null>(null);
  let cancelling = $state(false);

  /**
   * statesFor turns this page's filter into the states the SERVER narrows on.
   * "Active" is every state a job can still leave, named explicitly, because
   * "not terminal" is not a question the endpoint can be asked.
   */
  function statesFor(filter: StateFilter): JobState[] {
    if (filter === 'active') return [...ACTIVE_JOB_STATES];
    if (filter === 'completed') return ['completed'];
    if (filter === 'failed') return ['failed'];
    return [];
  }

  /**
   * liveTick advances once the job event stream has been quiet for
   * LIVE_REFRESH_DEBOUNCE_MS. It is part of the reload key, so the table
   * re-reads the page it is on; a burst of events from five running jobs
   * costs ONE fetch, and DataTable discards any answer a newer read overtakes.
   */
  let liveTick = $state(0);

  $effect(() => {
    const revision = orchestratorStore.jobEventRevision;
    if (revision === 0) return;
    const timer = setTimeout(() => { liveTick += 1; }, LIVE_REFRESH_DEBOUNCE_MS);
    return () => { clearTimeout(timer); };
  });

  const reloadKey = $derived(`${orchestratorStore.activeTenantID}|${stateFilter}|${liveTick}`);

  async function loadJobsPage(req: PageRequest): Promise<unknown> {
    const client = await getSDKClient();
    const statuses = statesFor(stateFilter);
    return client.orchestrator.listJobsPage(orchestratorStore.activeTenantID, {
      limit: req.limit,
      offset: req.offset,
      sort: req.sort,
      q: req.q,
      ...(statuses.length === 0 ? {} : { statuses }),
    });
  }

  // The keys are the SERVER'S sort fields (docs/software-factory/api.md).
  // Iteration and Progress are not among them and therefore do not sort.
  const COLUMNS: Column<JobStatus>[] = [
    { key: 'job_id', label: 'Job ID', sortable: true, searchable: true },
    { key: 'description', label: 'Description', sortable: true, searchable: true },
    { key: 'state', label: 'State', sortable: true },
    { key: 'iteration', label: 'Iteration' },
    { key: 'completed', label: 'Progress' },
    { key: 'started_at', label: 'Started', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  /**
   * jobIssueRef is the forge issue behind a job, read from the Task the job is
   * an attempt at — by the job's own binding, or from the other end when the
   * server does not yet carry one. A lead-driven job has neither, and no chip.
   */
  function jobIssueRef(job: JobStatus): IssueRef | null {
    const tasks = Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : [];
    const task = job.task_id
      ? tasks.find((t) => t.id === job.task_id)
      : tasks.find((t) => t.spawned_job_id === job.job_id);
    return task ? issueRef(task) : null;
  }

  function isCancellable(state: JobState): boolean {
    return state !== 'completed' && state !== 'failed' && state !== 'cleanup';
  }

  const formatTime = (ts: number): string => formatDateTime(ts);

  function truncateID(id: string): string {
    return id.length > 12 ? id.slice(0, 8) + '…' + id.slice(-4) : id;
  }

  function handleRowClick(job: JobStatus): void {
    selectJob(job);
    routeJobDetail(job.job_id);
  }

  function requestCancel(job: JobStatus, e: Event): void {
    e.stopPropagation();
    cancelTarget = job;
  }

  function dismissCancel(): void {
    cancelTarget = null;
  }

  async function confirmCancel(): Promise<void> {
    if (!cancelTarget) return;
    cancelling = true;
    const job = cancelTarget;
    try {
      const client = await getSDKClient();
      await client.orchestrator.cancelJob(orchestratorStore.activeTenantID, job.job_id);
      showToast('success', `Job ${job.job_id.slice(0, 8)} cancelled`);
      cancelTarget = null;
      // Re-read THIS PAGE at once so the row reflects the new terminal state
      // rather than sitting on "executing" until something else asks. The
      // store is refreshed too, for the detail views that read it.
      liveTick += 1;
      void refresh('jobs');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Failed to cancel job: ${msg}`);
    } finally {
      cancelling = false;
    }
  }
</script>

<!-- Start Job dialog -->
<StartJobDialog bind:open={startJobOpen} />

<!-- Cancel confirm dialog -->
{#if cancelTarget !== null}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    onkeydown={(e) => { if (e.key === 'Escape') dismissCancel(); }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Cancel Job?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Job <span class="font-mono">{cancelTarget.job_id.slice(0, 8)}</span> will be terminated.
        This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={dismissCancel} disabled={cancelling}>
          Keep Running
        </button>
        <button
          class="btn btn-sm btn-error"
          onclick={() => { void confirmCancel(); }}
          disabled={cancelling}
        >
          {#if cancelling}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Cancel Job
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={dismissCancel} onkeydown={() => {}}></div>
  </dialog>
{/if}

<div class="p-6 space-y-6">
  <PageHeader title="Jobs" subtitle="Software Factory job queue and history" icon={Briefcase}>
    {#snippet children()}
      <button
        class="btn btn-sm btn-primary gap-1"
        onclick={() => { startJobOpen = true; }}
        aria-label="Start new job"
      >
        <Plus class="h-4 w-4" />
        Start Job
      </button>
    {/snippet}
  </PageHeader>

  <DataTable
    columns={COLUMNS}
    load={loadJobsPage}
    itemsKey="items"
    {reloadKey}
    rowKey={(j: JobStatus) => j.job_id}
    narrowed={stateFilter !== 'all'}
    urlKey="jobs"
    ariaLabel="Jobs"
    searchPlaceholder="Search jobs..."
    searchLabel="Search jobs"
    noMatchText="No jobs match the current filter."
  >
    {#snippet toolbar()}
      <select
        class="select select-bordered select-sm"
        aria-label="Filter by state"
        bind:value={stateFilter}
      >
        <option value="all">All States</option>
        <option value="active">Active</option>
        <option value="completed">Completed</option>
        <option value="failed">Failed</option>
      </select>
    {/snippet}

    {#snippet empty()}
      <EmptyState
        icon={Briefcase}
        title="No jobs yet — Start Job is how you begin."
        description="Kick off an orchestration job and watch it execute across your agent fleet."
        actionLabel="+ Start Job"
        onaction={() => { startJobOpen = true; }}
      />
    {/snippet}

    {#snippet row(job: JobStatus)}
      {@const ref = jobIssueRef(job)}
      <tr
        class="cursor-pointer hover:bg-base-300/40 transition-colors"
        onclick={() => handleRowClick(job)}
      >
        <td class="font-mono text-xs">
          <span class="tooltip" data-tip={job.job_id}>
            {truncateID(job.job_id)}
          </span>
        </td>
        <td class="max-w-xs">
          <div class="flex items-center gap-2 min-w-0">
            {#if ref !== null}
              {#if ref.href !== ''}
                <a
                  class="badge badge-sm badge-outline font-mono shrink-0"
                  href={ref.href}
                  target="_blank"
                  rel="noopener noreferrer"
                  onclick={(e) => e.stopPropagation()}
                  title="Open issue {ref.label} on the forge"
                >{ref.label}</a>
              {:else}
                <span class="badge badge-sm badge-outline font-mono shrink-0" title="Forge issue {ref.label}">{ref.label}</span>
              {/if}
            {/if}
            <span class="truncate">{job.description || '—'}</span>
          </div>
        </td>
        <td>
          <StatusBadge text={job.state} variant={statusToVariant(job.state)} pulse={job.state === 'executing'} />
        </td>
        <td class="text-center">{job.iteration}</td>
        <td>
          <div class="flex items-center gap-2">
            <progress
              class="progress progress-primary w-20"
              value={job.completed}
              max={job.task_count || 1}
            ></progress>
            <span class="text-xs text-base-content/50">
              {job.completed}/{job.task_count}
            </span>
          </div>
        </td>
        <td class="text-xs text-base-content/50">{formatTime(job.started_at)}</td>
        <!-- svelte-ignore a11y_no_static_element_interactions a11y_click_events_have_key_events -->
        <td>
          <div class="flex gap-1" onclick={(e) => e.stopPropagation()} onkeydown={() => {}}>
            <button
              class="btn btn-ghost btn-xs"
              onclick={(e) => { e.stopPropagation(); handleRowClick(job); }}
            >
              View
            </button>
            {#if isCancellable(job.state)}
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={(e) => requestCancel(job, e)}
                aria-label="Cancel job {job.job_id}"
              >
                Cancel
              </button>
            {/if}
          </div>
        </td>
      </tr>
    {/snippet}
  </DataTable>
</div>
