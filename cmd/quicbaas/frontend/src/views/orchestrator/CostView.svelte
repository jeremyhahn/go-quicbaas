<!--
  CostView renders a budget gauge and per-job iteration usage breakdown.
-->
<script lang="ts">
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import BudgetGauge from '../../components/orchestrator/BudgetGauge.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';

  const totalIterations = $derived(
    orchestratorStore.jobs.reduce((sum, j) => sum + j.iteration, 0)
  );

  const activeJobs = $derived(
    orchestratorStore.jobs.filter((j) => j.state !== 'completed' && j.state !== 'failed')
  );

  const maxPossibleIterations = $derived(
    orchestratorStore.jobs.reduce((sum, j) => sum + (j.config?.max_iterations ?? 10), 0) || 1
  );

  const overallPercent = $derived(
    Math.min(100, (totalIterations / maxPossibleIterations) * 100)
  );
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Cost Tracking" subtitle="Iteration usage and resource consumption" />

  <div class="grid grid-cols-1 lg:grid-cols-3 gap-6">
    <!-- Overall gauge -->
    <div class="card bg-base-200 rounded-box lg:col-span-1">
      <div class="card-body p-4 items-center">
        <h3 class="text-sm font-semibold text-base-content/70 mb-2">Overall Usage</h3>
        <BudgetGauge percent={overallPercent} label="Iterations" />
        <div class="text-xs text-base-content/40 mt-2">
          {totalIterations} iterations across {orchestratorStore.jobs.length} jobs
        </div>
      </div>
    </div>

    <!-- Per-job breakdown -->
    <div class="card bg-base-200 rounded-box lg:col-span-2">
      <div class="card-body p-4">
        <h3 class="text-sm font-semibold text-base-content/70 mb-3">Per-Job Breakdown</h3>

        {#if orchestratorStore.jobs.length === 0}
          <div class="text-center py-8 text-base-content/30 text-sm">No jobs</div>
        {:else}
          <div class="space-y-3 max-h-96 overflow-y-auto">
            {#each orchestratorStore.jobs as job (job.job_id)}
              {@const max = job.config?.max_iterations ?? 10}
              {@const pct = Math.min(100, (job.iteration / max) * 100)}
              <div class="flex items-center gap-3">
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-2 mb-1">
                    <span class="text-xs font-medium truncate">{job.description || job.job_id}</span>
                    <StatusBadge
                      text={job.state}
                      variant={job.state === 'completed' ? 'success' : job.state === 'failed' ? 'error' : 'info'}
                    />
                  </div>
                  <div class="flex items-center gap-2">
                    <progress
                      class="progress progress-primary flex-1"
                      class:progress-error={pct > 80}
                      class:progress-warning={pct > 60 && pct <= 80}
                      value={job.iteration}
                      max={max}
                    ></progress>
                    <span class="text-xs text-base-content/50 shrink-0 w-16 text-right">
                      {job.iteration}/{max}
                    </span>
                  </div>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>

  <!-- Active jobs summary -->
  {#if activeJobs.length > 0}
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-4">
        <h3 class="text-sm font-semibold text-base-content/70 mb-2">
          Active Jobs ({activeJobs.length})
        </h3>
        <div class="grid grid-cols-2 lg:grid-cols-4 gap-3">
          {#each activeJobs as job (job.job_id)}
            <div class="stat bg-base-300 rounded-box p-3">
              <div class="stat-title text-xs truncate">{job.description || job.job_id.slice(0, 8)}</div>
              <div class="stat-value text-xl">{job.iteration}</div>
              <div class="stat-desc">iterations</div>
            </div>
          {/each}
        </div>
      </div>
    </div>
  {/if}
</div>
