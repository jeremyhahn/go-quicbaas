<!--
  JobDetailView renders a premium ops detail page for a single orchestration job.
  Accessible via: navigating from JobsView row click → #/jobs/{id}.

  Layout:
  - Header: job ID + status badge + state-aware action buttons + ChevronLeft back
  - 3-column summary card: Workflow / Agent / Started + Completed + Duration
  - Iteration timeline: visual progress bar + per-iteration audit list
  - Cost panel: token usage if cost metadata present, otherwise unavailable notice
  - Live event stream: EventLog filtered to this job_id with auto-scroll
  - Raw JSON drawer: collapsible debug panel
-->
<script lang="ts">
  import {
    orchestratorStore,
    selectJob,
    type JobAuditEvent,
    type JobState,
  } from '../../stores/orchestrator.svelte.ts';
  import { navigate } from '../../stores/navigation.svelte.ts';
  import { routeJobs } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import JobStateMachine from '../../components/orchestrator/JobStateMachine.svelte';
  import DAGGraph from '../../components/orchestrator/DAGGraph.svelte';
  import EventLog from '../../components/orchestrator/EventLog.svelte';
  import BudgetGauge from '../../components/orchestrator/BudgetGauge.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { ChevronLeft, Play, X, RotateCcw, Radio } from 'lucide-svelte';
  import { formatDateTime, formatTimeOnly, toMillis } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  type Tab = 'overview' | 'dag' | 'audit' | 'cost' | 'events';
  let activeTab: Tab = $state('overview');
  let rawJsonOpen = $state(false);
  let cancelling = $state(false);

  // ---------------------------------------------------------------------------
  // Derived
  // ---------------------------------------------------------------------------

  const job = $derived(orchestratorStore.selectedJob);

  const progressPercent = $derived(
    job && job.task_count > 0
      ? Math.round((job.completed / job.task_count) * 100)
      : 0,
  );

  // Duration uses toMillis to normalize started_at/updated_at, which arrive
  // in either Unix seconds (some surfaces) or Go UnixNano (orchestrator API).
  // Subtracting raw values without unit normalization produced impossible
  // durations like "613683h 24m" when the wire format was nanoseconds.
  const duration = $derived.by((): string => {
    if (!job) return '—';
    const startMs = toMillis(job.started_at);
    if (startMs === null) return '—';
    const endMs = toMillis(job.updated_at) ?? Date.now();
    const secs = Math.max(0, Math.floor((endMs - startMs) / 1000));
    if (secs < 60) return `${secs}s`;
    if (secs < 3600) return `${Math.floor(secs / 60)}m ${secs % 60}s`;
    return `${Math.floor(secs / 3600)}h ${Math.floor((secs % 3600) / 60)}m`;
  });

  const jobEvents = $derived(
    job
      ? (Array.isArray(orchestratorStore.events) ? orchestratorStore.events : []).filter(
          (e) => e.job_id === job.job_id,
        )
      : [],
  );

  // Cost metadata: look for token_usage key in job memory.
  const tokenUsage = $derived.by((): { input: number; output: number } | null => {
    if (!job?.memory) return null;
    const u = job.memory['token_usage'];
    if (u && typeof u === 'object' && u !== null) {
      const obj = u as Record<string, unknown>;
      const input = typeof obj['input'] === 'number' ? obj['input'] : 0;
      const output = typeof obj['output'] === 'number' ? obj['output'] : 0;
      return { input, output };
    }
    return null;
  });

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

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

  function isRetryable(state: JobState): boolean {
    return state === 'failed';
  }

  const formatTime = (ts: number): string => formatDateTime(ts);

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  function handleBack(): void {
    selectJob(null);
    routeJobs();
  }

  async function handleCancel(): Promise<void> {
    if (!job || cancelling) return;
    cancelling = true;
    try {
      const client = await getSDKClient();
      await client.orchestrator.cancelJob(orchestratorStore.activeTenantID, job.job_id);
      showToast('success', `Job ${job.job_id.slice(0, 8)} cancelled`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Cancel failed: ${msg}`);
    } finally {
      cancelling = false;
    }
  }
</script>

{#if !job}
  <div class="flex items-center justify-center h-full">
    <div class="text-base-content/30">No job selected</div>
  </div>
{:else}
  <div class="p-6 space-y-5 max-w-5xl mx-auto">

    <!-- Header -->
    <div class="flex items-start gap-3 flex-wrap">
      <button
        class="btn btn-ghost btn-sm gap-1 shrink-0"
        onclick={handleBack}
        aria-label="Back to jobs list"
      >
        <ChevronLeft class="h-4 w-4" />
        Back
      </button>

      <div class="flex-1 min-w-0">
        <h1 class="text-xl font-bold truncate">{job.description || job.job_id}</h1>
        <div class="flex items-center gap-2 mt-1 flex-wrap">
          <StatusBadge
            text={job.state}
            variant={stateVariant(job.state)}
            pulse={job.state === 'executing'}
          />
          <span class="text-xs text-base-content/40 font-mono">{job.job_id}</span>
        </div>
      </div>

      <!-- State-aware action buttons -->
      <div class="flex items-center gap-2 shrink-0">
        <button
          class="btn btn-xs btn-ghost gap-1"
          onclick={() => { activeTab = 'events'; }}
          aria-label="Open event stream"
        >
          <Radio class="h-3 w-3" />
          Events
        </button>

        {#if isCancellable(job.state)}
          <button
            class="btn btn-xs btn-error gap-1"
            onclick={() => { void handleCancel(); }}
            disabled={cancelling}
            aria-label="Cancel job"
          >
            {#if cancelling}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <X class="h-3 w-3" />
            {/if}
            Cancel
          </button>
        {/if}

        {#if isRetryable(job.state)}
          <button
            class="btn btn-xs btn-primary gap-1"
            aria-label="Retry job"
            title="Retry is not yet available in this version"
            disabled
          >
            <RotateCcw class="h-3 w-3" />
            Retry
          </button>
        {/if}
      </div>
    </div>

    <!-- State machine stepper -->
    <div class="card bg-base-200 rounded-box p-4">
      <JobStateMachine state={job.state} />
    </div>

    <!-- 3-column summary card -->
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
      <div class="card bg-base-200 rounded-box p-4 space-y-1">
        <div class="text-xs text-base-content/40 uppercase tracking-wide">Workflow</div>
        <div class="font-semibold text-sm">{job.workflow_name || job.config?.workflow_name || '—'}</div>
        <div class="text-xs text-base-content/40">Lead backend: {job.config?.lead_backend || '—'}</div>
      </div>

      <div class="card bg-base-200 rounded-box p-4 space-y-1">
        <div class="text-xs text-base-content/40 uppercase tracking-wide">Agent</div>
        <div class="font-semibold text-sm">{job.config?.lead_model || '—'}</div>
        <div class="text-xs text-base-content/40">Tenant: {job.tenant_id}</div>
      </div>

      <div class="card bg-base-200 rounded-box p-4 space-y-1">
        <div class="text-xs text-base-content/40 uppercase tracking-wide">Timing</div>
        <div class="text-sm">Started: <span class="font-mono text-xs">{formatTime(job.started_at)}</span></div>
        {#if job.state === 'completed' || job.state === 'failed'}
          <div class="text-sm">Completed: <span class="font-mono text-xs">{formatTime(job.updated_at)}</span></div>
        {/if}
        <div class="text-xs text-base-content/60">Duration: <strong>{duration}</strong></div>
      </div>
    </div>

    <!-- Iteration timeline -->
    <div class="card bg-base-200 rounded-box p-4 space-y-3">
      <div class="flex items-center justify-between">
        <div class="text-sm font-semibold">Iteration Progress</div>
        <div class="text-xs text-base-content/50">
          {job.iteration} / {job.config?.max_iterations ?? '∞'} max
        </div>
      </div>

      <!-- Visual progress bar: current / max iterations -->
      <div class="space-y-1">
        <div class="flex justify-between text-xs text-base-content/50">
          <span>Tasks</span>
          <span>{progressPercent}% — {job.completed}/{job.task_count}</span>
        </div>
        <progress
          class="progress progress-primary w-full"
          value={job.completed}
          max={job.task_count || 1}
          aria-label="Task completion progress"
        ></progress>
      </div>

      <!-- Per-iteration audit list -->
      {#if job.events && job.events.length > 0}
        <div class="space-y-0.5 max-h-48 overflow-y-auto">
          {#each job.events as evt, i (i)}
            <div class="flex gap-3 py-1 border-b border-base-300/30 text-xs font-mono items-start">
              <span class="text-base-content/40 shrink-0 w-32">
                {formatTimeOnly(evt.timestamp)}
              </span>
              <span class="badge badge-xs badge-ghost shrink-0">{evt.type}</span>
              <span class="text-base-content/60 break-all">{evt.details}</span>
            </div>
          {/each}
        </div>
      {:else}
        <div class="text-xs text-base-content/30 text-center py-2">No audit events recorded yet</div>
      {/if}
    </div>

    <!-- Tabs -->
    <div role="tablist" class="tabs tabs-bordered">
      {#each (['overview', 'dag', 'audit', 'cost', 'events'] as Tab[]) as tab}
        <button
          role="tab"
          class="tab"
          class:tab-active={activeTab === tab}
          onclick={() => { activeTab = tab; }}
        >
          {tab.charAt(0).toUpperCase() + tab.slice(1)}
        </button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="card bg-base-200 rounded-box">
      <div class="card-body p-4">

        {#if activeTab === 'overview'}
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
              <div class="text-xs text-base-content/40">Failed Tasks</div>
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
                    {formatDateTime(evt.timestamp)}
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
          <!-- Cost panel: token usage if present, otherwise show unavailable notice -->
          <div class="flex flex-col items-center gap-4">
            <BudgetGauge
              percent={job.config?.max_iterations
                ? Math.min(100, (job.iteration / job.config.max_iterations) * 100)
                : 0}
              label="Iterations Used"
            />
            <div class="grid grid-cols-2 gap-4 w-full max-w-sm text-sm">
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

            {#if tokenUsage !== null}
              <div class="w-full max-w-sm space-y-1">
                <div class="text-xs font-semibold text-base-content/50 uppercase tracking-wide">Token Usage</div>
                <div class="grid grid-cols-2 gap-2">
                  <div class="stat bg-base-300 rounded-box p-3">
                    <div class="stat-title text-xs">Input tokens</div>
                    <div class="stat-value text-xl">{tokenUsage.input.toLocaleString()}</div>
                  </div>
                  <div class="stat bg-base-300 rounded-box p-3">
                    <div class="stat-title text-xs">Output tokens</div>
                    <div class="stat-value text-xl">{tokenUsage.output.toLocaleString()}</div>
                  </div>
                </div>
              </div>
            {:else}
              <div class="text-sm text-base-content/40 mt-2">No cost data available for this job</div>
            {/if}
          </div>

        {:else if activeTab === 'events'}
          <!-- Live event stream filtered to this job -->
          <EventLog events={jobEvents} maxHeight="400px" autoScroll={true} />
        {/if}

      </div>
    </div>

    <!-- Raw JSON drawer -->
    <div class="rounded-box border border-base-300/50 overflow-hidden">
      <button
        class="w-full flex items-center justify-between px-4 py-2 bg-base-200/80 text-xs font-semibold text-base-content/60 hover:bg-base-300/60 transition-colors"
        onclick={() => { rawJsonOpen = !rawJsonOpen; }}
        aria-expanded={rawJsonOpen}
        aria-controls="raw-json-panel"
      >
        <span>Raw Job State (debug)</span>
        <span>{rawJsonOpen ? '▲' : '▼'}</span>
      </button>
      {#if rawJsonOpen}
        <div id="raw-json-panel">
          <pre class="bg-base-300 p-4 text-xs overflow-auto max-h-80 whitespace-pre-wrap break-all font-mono">{JSON.stringify(job, null, 2)}</pre>
        </div>
      {/if}
    </div>

  </div>
{/if}
