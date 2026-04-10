<!--
  JobDetailView renders a tabbed detail panel for the currently selected job.
  Tabs: Overview, DAG, Audit, Cost, Events.
-->
<script lang="ts">
  import { orchestratorStore, selectJob, type JobAuditEvent } from '../../stores/orchestrator.svelte.ts';
  import { navigate } from '../../stores/navigation.svelte.ts';
  import JobStateMachine from '../../components/orchestrator/JobStateMachine.svelte';
  import DAGGraph from '../../components/orchestrator/DAGGraph.svelte';
  import EventLog from '../../components/orchestrator/EventLog.svelte';
  import BudgetGauge from '../../components/orchestrator/BudgetGauge.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import type { JobState } from '../../stores/orchestrator.svelte.ts';

  type Tab = 'overview' | 'dag' | 'audit' | 'cost' | 'events';
  let activeTab: Tab = $state('overview');

  const job = $derived(orchestratorStore.selectedJob);

  function formatTime(ts: number): string {
    if (!ts) return '—';
    return new Date(ts * 1000).toLocaleString();
  }

  function stateVariant(state: JobState): 'success' | 'error' | 'warning' | 'info' | 'primary' | 'ghost' {
    if (state === 'completed') return 'success';
    if (state === 'failed') return 'error';
    if (state === 'escalating' || state === 're-delegating') return 'warning';
    if (state === 'executing' || state === 'assigning') return 'primary';
    return 'info';
  }

  function handleBack(): void {
    selectJob(null);
    navigate('jobs');
  }

  const jobEvents = $derived(
    job
      ? orchestratorStore.events.filter((e) => e.job_id === job.job_id)
      : []
  );

  const progressPercent = $derived(
    job && job.task_count > 0
      ? Math.round((job.completed / job.task_count) * 100)
      : 0
  );
</script>

{#if !job}
  <div class="flex items-center justify-center h-full">
    <div class="text-base-content/30">No job selected</div>
  </div>
{:else}
  <div class="p-6 space-y-4">
    <!-- Back button + title -->
    <div class="flex items-center gap-3">
      <button class="btn btn-ghost btn-sm" onclick={handleBack}>
        ← Back
      </button>
      <div>
        <h1 class="text-xl font-bold">{job.description || job.job_id}</h1>
        <div class="flex items-center gap-2 mt-1">
          <StatusBadge text={job.state} variant={stateVariant(job.state)} pulse={job.state === 'executing'} />
          <span class="text-xs text-base-content/40 font-mono">{job.job_id}</span>
        </div>
      </div>
    </div>

    <!-- State machine stepper -->
    <div class="card bg-base-200 rounded-box p-4">
      <JobStateMachine state={job.state} />
    </div>

    <!-- Tabs -->
    <div role="tablist" class="tabs tabs-bordered">
      {#each (['overview', 'dag', 'audit', 'cost', 'events'] as Tab[]) as tab}
        <button
          role="tab"
          class="tab"
          class:tab-active={activeTab === tab}
          onclick={() => (activeTab = tab)}
        >
          {tab.charAt(0).toUpperCase() + tab.slice(1)}
        </button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-4">

        {#if activeTab === 'overview'}
          <!-- Metadata -->
          <div class="grid grid-cols-2 lg:grid-cols-3 gap-4 mb-4">
            <div>
              <div class="text-xs text-base-content/40">Tenant</div>
              <div class="font-mono text-sm">{job.tenant_id}</div>
            </div>
            <div>
              <div class="text-xs text-base-content/40">Iteration</div>
              <div class="text-sm">{job.iteration}</div>
            </div>
            <div>
              <div class="text-xs text-base-content/40">Task Count</div>
              <div class="text-sm">{job.task_count}</div>
            </div>
            <div>
              <div class="text-xs text-base-content/40">Completed</div>
              <div class="text-sm text-success">{job.completed}</div>
            </div>
            <div>
              <div class="text-xs text-base-content/40">Failed</div>
              <div class="text-sm text-error">{job.failed}</div>
            </div>
            <div>
              <div class="text-xs text-base-content/40">Started</div>
              <div class="text-sm">{formatTime(job.started_at)}</div>
            </div>
            <div>
              <div class="text-xs text-base-content/40">Updated</div>
              <div class="text-sm">{formatTime(job.updated_at)}</div>
            </div>
            {#if job.config?.lead_backend}
              <div>
                <div class="text-xs text-base-content/40">Lead Backend</div>
                <div class="badge badge-sm">{job.config.lead_backend}</div>
              </div>
            {/if}
            {#if job.workflow_name}
              <div>
                <div class="text-xs text-base-content/40">Workflow</div>
                <div class="text-sm">{job.workflow_name}</div>
              </div>
            {/if}
          </div>

          <!-- Progress bar -->
          <div class="space-y-1">
            <div class="flex justify-between text-xs text-base-content/50">
              <span>Progress</span>
              <span>{progressPercent}% ({job.completed}/{job.task_count})</span>
            </div>
            <progress
              class="progress progress-primary w-full"
              value={job.completed}
              max={job.task_count || 1}
            ></progress>
          </div>

          {#if job.error}
            <div role="alert" class="alert alert-error mt-4 text-sm">
              <span>{job.error}</span>
            </div>
          {/if}

        {:else if activeTab === 'dag'}
          {#if job.work_items && job.work_items.length > 0}
            <DAGGraph workItems={job.work_items} />
          {:else}
            <div class="text-center py-8 text-base-content/30 text-sm">
              No work items to visualize yet
            </div>
          {/if}

        {:else if activeTab === 'audit'}
          <div class="space-y-1 max-h-96 overflow-y-auto font-mono text-xs">
            {#if job.events && job.events.length > 0}
              {#each job.events as evt, i (i)}
                <div class="flex gap-3 py-1 border-b border-base-300/30">
                  <span class="text-base-content/40 shrink-0 w-36">
                    {new Date(evt.timestamp * 1000).toLocaleString()}
                  </span>
                  <span class="badge badge-xs badge-ghost shrink-0">{evt.type}</span>
                  <span class="text-base-content/70 break-all">{evt.details}</span>
                </div>
              {/each}
            {:else}
              <div class="text-center py-8 text-base-content/30">No audit events</div>
            {/if}
          </div>

        {:else if activeTab === 'cost'}
          <div class="flex flex-col items-center">
            <BudgetGauge
              percent={job.config?.max_iterations
                ? Math.min(100, (job.iteration / job.config.max_iterations) * 100)
                : 0}
              label="Iterations Used"
            />
            <div class="mt-4 grid grid-cols-2 gap-4 w-full max-w-sm text-sm">
              <div class="stat bg-base-300 rounded-box p-3">
                <div class="stat-title text-xs">Iteration</div>
                <div class="stat-value text-2xl">{job.iteration}</div>
                <div class="stat-desc">of {job.config?.max_iterations ?? '∞'} max</div>
              </div>
              <div class="stat bg-base-300 rounded-box p-3">
                <div class="stat-title text-xs">Failures</div>
                <div class="stat-value text-2xl text-error">{job.failed}</div>
                <div class="stat-desc">tasks failed</div>
              </div>
            </div>
          </div>

        {:else if activeTab === 'events'}
          <EventLog events={jobEvents} maxHeight="400px" autoScroll={true} />
        {/if}

      </div>
    </div>
  </div>
{/if}
