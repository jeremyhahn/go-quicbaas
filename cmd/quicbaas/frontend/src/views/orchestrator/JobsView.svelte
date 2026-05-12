<!--
  JobsView renders a filterable, searchable table of orchestration jobs.
  Clicking a row selects the job and navigates to JobDetailView.
  Header provides a "+ Start Job" button that opens StartJobDialog.
  Each row has a "Cancel" action that confirms before cancelling the job.
-->
<script lang="ts">
  import {
    orchestratorStore,
    selectJob,
    refresh,
    type JobStatus,
    type JobState,
  } from '../../stores/orchestrator.svelte.ts';
  import { navigate } from '../../stores/navigation.svelte.ts';
  import { routeJobDetail } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import StartJobDialog from '../../components/orchestrator/StartJobDialog.svelte';
  import { Plus, Briefcase } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { formatDateTime } from '../../lib/time.ts';

  type StateFilter = 'all' | 'active' | 'completed' | 'failed';

  let stateFilter: StateFilter = $state('all');
  let searchQuery = $state('');
  let startJobOpen = $state(false);

  // Job being cancelled (for confirm dialog state).
  let cancelTarget = $state<JobStatus | null>(null);
  let cancelling = $state(false);

  const filteredJobs = $derived.by((): JobStatus[] => {
    let result = Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : [];

    if (stateFilter === 'active') {
      result = result.filter((j) => j.state !== 'completed' && j.state !== 'failed');
    } else if (stateFilter === 'completed') {
      result = result.filter((j) => j.state === 'completed');
    } else if (stateFilter === 'failed') {
      result = result.filter((j) => j.state === 'failed');
    }

    if (searchQuery.trim() !== '') {
      const q = searchQuery.toLowerCase();
      result = result.filter(
        (j) =>
          j.description.toLowerCase().includes(q) ||
          j.job_id.toLowerCase().includes(q)
      );
    }

    return result;
  });

  function stateVariant(state: JobState): 'success' | 'error' | 'warning' | 'info' | 'primary' | 'ghost' {
    if (state === 'completed') return 'success';
    if (state === 'failed') return 'error';
    if (state === 'escalating' || state === 're-delegating') return 'warning';
    if (state === 'executing' || state === 'assigning') return 'primary';
    return 'info';
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
      // Force immediate refresh so the row reflects the new terminal state
      // without waiting up to 30 s for the next poll. Without this the user
      // sees the row still labelled "executing" and has no feedback that
      // their action took effect.
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
  <PageHeader title="Jobs" subtitle="Orchestration job queue and history" icon={Briefcase}>
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

  <!-- Filters -->
  <div class="flex gap-3 flex-wrap">
    <select
      class="select select-bordered select-sm"
      bind:value={stateFilter}
    >
      <option value="all">All States</option>
      <option value="active">Active</option>
      <option value="completed">Completed</option>
      <option value="failed">Failed</option>
    </select>

    <input
      type="text"
      class="input input-bordered input-sm flex-1 min-w-48"
      placeholder="Search jobs..."
      bind:value={searchQuery}
    />
  </div>

  <!-- Table -->
  <div class="overflow-x-auto rounded-box border border-base-300/50">
    <table class="table table-sm table-zebra w-full">
      <thead>
        <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
          <th>Job ID</th>
          <th>Description</th>
          <th>State</th>
          <th>Iteration</th>
          <th>Progress</th>
          <th>Started</th>
          <th><span class="sr-only">Actions</span></th>
        </tr>
      </thead>
      <tbody>
        {#if orchestratorStore.jobsLoading}
          <tr>
            <td colspan="7" class="text-center py-8">
              <span class="loading loading-spinner loading-sm text-primary"></span>
            </td>
          </tr>
        {:else if filteredJobs.length === 0}
          <tr>
            <td colspan="7">
              {#if orchestratorStore.jobs.length === 0}
                <EmptyState
                  icon={Briefcase}
                  title="No jobs yet — Start Job is how you begin."
                  description="Kick off an orchestration job and watch it execute across your agent fleet."
                  actionLabel="+ Start Job"
                  onaction={() => { startJobOpen = true; }}
                />
              {:else}
                <div class="text-center py-8 text-sm text-base-content/40">
                  No jobs match the current filter.
                </div>
              {/if}
            </td>
          </tr>
        {:else}
          {#each filteredJobs as job (job.job_id)}
            <tr
              class="cursor-pointer hover:bg-base-300/40 transition-colors"
              onclick={() => handleRowClick(job)}
            >
              <td class="font-mono text-xs">
                <span class="tooltip" data-tip={job.job_id}>
                  {truncateID(job.job_id)}
                </span>
              </td>
              <td class="max-w-xs truncate">{job.description || '—'}</td>
              <td>
                <StatusBadge text={job.state} variant={stateVariant(job.state)} pulse={job.state === 'executing'} />
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
          {/each}
        {/if}
      </tbody>
    </table>
  </div>

  <div class="text-xs text-base-content/40">
    Showing {filteredJobs.length} of {orchestratorStore.jobs.length} jobs
  </div>
</div>
