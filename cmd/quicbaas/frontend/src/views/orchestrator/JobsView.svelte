<!--
  JobsView renders a filterable, searchable table of orchestration jobs.
  Clicking a row selects the job and navigates to JobDetailView.
-->
<script lang="ts">
  import {
    orchestratorStore,
    selectJob,
    type JobStatus,
    type JobState,
  } from '../../stores/orchestrator.svelte.ts';
  import { navigate } from '../../stores/navigation.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';

  type StateFilter = 'all' | 'active' | 'completed' | 'failed';

  let stateFilter: StateFilter = $state('all');
  let searchQuery = $state('');

  const filteredJobs = $derived.by((): JobStatus[] => {
    let result = orchestratorStore.jobs;

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

  function formatTime(ts: number): string {
    if (!ts) return '—';
    return new Date(ts * 1000).toLocaleString();
  }

  function truncateID(id: string): string {
    return id.length > 12 ? id.slice(0, 8) + '…' + id.slice(-4) : id;
  }

  function handleRowClick(job: JobStatus): void {
    selectJob(job);
    navigate('jobs');
  }
</script>

<div class="p-6 space-y-4">
  <PageHeader title="Jobs" subtitle="Orchestration job queue and history" />

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
        <tr class="bg-base-200 text-base-content/60">
          <th>Job ID</th>
          <th>Description</th>
          <th>State</th>
          <th>Iteration</th>
          <th>Progress</th>
          <th>Started</th>
          <th></th>
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
            <td colspan="7" class="text-center py-8 text-base-content/30">
              No jobs found
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
              <td>
                <button
                  class="btn btn-ghost btn-xs"
                  onclick={(e) => { e.stopPropagation(); handleRowClick(job); }}
                >
                  View
                </button>
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
