<!--
  JobDetailView renders the detail page for a single orchestration job.
  Accessible via: navigating from JobsView row click → #/jobs/{id}.

  THE PAGE IS BUILT BY RUNNER. The engine's own `workflow` runner decomposes a
  description into a DAG and iterates over it, so its page is iterations,
  tasks and a graph. The `sdlc` runner makes ONE attempt at ONE issue in ONE
  container, so its page is the attempt, the agent that ran it, the stage
  timeline, the pull request and its review rounds, the verification output,
  and the typed reason it stopped. Rendering the first layout for both is what
  go-quicbaas#796 reported: an sdlc job showed `Workflow —`, `Agent —`,
  `Iteration Progress 0/10` and `Tasks 0%`, none of which it has.

  The panel decisions are pure functions in src/lib/jobDetail.ts; this file is
  their rendering.

  Layout:
  - Header: description + status badge + runner-aware actions + back
  - Summary: attempt/agent/timing (sdlc) or workflow/lead/timing (workflow)
  - sdlc: stage timeline, delivery + review rounds, verification, terminal
    reason, the worker's last reply
  - workflow: iteration progress, task completion, DAG tab
  - Cost: THIS job's consumption-ledger entries, for BOTH runners. Spend is
    not the workflow runner's — every runner burns provider tokens — so the
    tab is offered to both and only its ITERATION gauge is runner-gated
    (go-quicbaas#794). Before that the tab held nothing but the gauge and a
    job-memory token panel no production writer populates, which is why #796
    hid it for the sdlc runner.
  - Live event stream, audit trail and a raw JSON drawer for both
-->
<script lang="ts">
  import {
    orchestratorStore,
    selectJob,
    isTerminalJobState,
    type JobAuditEvent,
    type JobState,
    type Task,
  } from '../../stores/orchestrator.svelte.ts';
  import {
    listReturnHash,
    navigateTo,
    routeFleetNode,
    routeSessionDetail,
    routeTaskDetail,
  } from '../../stores/router.svelte.ts';
  import {
    eventSubStore,
    retryEventStream,
    startEventStream,
    stopEventStream,
  } from '../../stores/job-events.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import SdlcReviewerPicker from '../../components/orchestrator/SdlcReviewerPicker.svelte';
  import type { JobRunnerInfo } from '@quicbaas/sdk';
  import JobStateMachine from '../../components/orchestrator/JobStateMachine.svelte';
  import DAGGraph from '../../components/orchestrator/DAGGraph.svelte';
  import EventLog from '../../components/orchestrator/EventLog.svelte';
  import BudgetGauge from '../../components/orchestrator/BudgetGauge.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { statusToVariant } from '../../components/core/statusVariant.ts';
  import { ChevronLeft, X, RotateCcw, Radio, RadioTower, ExternalLink, GitPullRequest, ClipboardList, Terminal, Bot, GitBranch, CheckCircle2, XCircle } from 'lucide-svelte';
  import { formatDateTime, formatTimeOnly, toMillis } from '../../lib/time.ts';
  import { issueRef } from '../../lib/tasks.ts';
  import type { GetSpendReportResponse } from '@quicbaas/sdk';
  import {
    SPEND_UNATTRIBUTED_LEVEL,
    billedNote,
    equivalentNote,
    formatBilled,
    formatEquivalent,
    formatTokens,
    noIterationBudgetNotice,
    policyLabel,
    policyVariant,
    spendErrorMessage,
  } from '../../lib/cost.ts';
  import {
    attemptLabel,
    formatDuration,
    isWorkflowJob,
    lastAgentTurn,
    readmitAllowed,
    reviewVerdictTone,
    runnerOf,
    stageTimeline,
    terminalReason,
  } from '../../lib/jobDetail.ts';

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  type Tab = 'overview' | 'dag' | 'audit' | 'cost' | 'events';
  let activeTab: Tab = $state('overview');
  let rawJsonOpen = $state(false);
  let cancelling = $state(false);
  let readmitting = $state(false);

  // ---------------------------------------------------------------------------
  // Derived
  // ---------------------------------------------------------------------------

  const job = $derived(orchestratorStore.selectedJob);

  // The runner decides the whole layout. A record written before runners
  // existed names none, which the engine reads as the workflow runner.
  const runner = $derived(job ? runnerOf(job) : '');
  const workflowJob = $derived(job !== null && isWorkflowJob(job));

  // sdlc provenance, all derived so the markup stays a rendering.
  const attempt = $derived(job ? attemptLabel(job) : '');
  const stages = $derived(job ? stageTimeline(job) : []);
  const delivery = $derived(job?.delivery ?? null);
  const reviewRounds = $derived(delivery?.reviews ?? []);
  const verification = $derived(job?.verification ?? []);
  const conclusion = $derived(job ? terminalReason(job) : null);
  const canReadmit = $derived(job !== null && readmitAllowed(job));
  const workerTurn = $derived(job ? lastAgentTurn(job) : null);

  // The DAG is the workflow runner's: an sdlc job has no work items to graph.
  //
  // COST IS NOT. Every runner burns provider tokens, and the consumption
  // ledger records them under the job whichever runner drove it, so the tab
  // is offered to both and only its ITERATION gauge is runner-gated. It was
  // hidden for the sdlc runner by go-quicbaas#796 because it then contained
  // nothing but that gauge and a job-memory token panel no production writer
  // populates — which left an sdlc job's real spend unreachable on its own
  // page (go-quicbaas#794).
  const visibleTabs = $derived.by((): Tab[] =>
    workflowJob
      ? (['overview', 'dag', 'audit', 'cost', 'events'] as Tab[])
      : (['overview', 'audit', 'cost', 'events'] as Tab[]),
  );

  // A tab the runner does not offer must not stay selected when the record
  // arrives; otherwise the panel area renders nothing at all.
  $effect(() => {
    if (!visibleTabs.includes(activeTab)) activeTab = 'overview';
  });

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

  // The Task this job is an attempt at. The job says so directly once the
  // server carries task_id; until then the binding is only readable from the
  // other end, where the task names the job it spawned.
  const sourceTask = $derived.by((): Task | null => {
    if (!job) return null;
    const tasks = Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : [];
    if (job.task_id) return tasks.find((t) => t.id === job.task_id) ?? null;
    return tasks.find((t) => t.spawned_job_id === job.job_id) ?? null;
  });

  const ref = $derived(sourceTask ? issueRef(sourceTask) : null);

  // THE JOB'S PULL REQUEST IS THE ONE ITS OWN ATTEMPT DELIVERED.
  //
  // A retried task accumulates one delivery per attempt, and the task-level
  // `pullRequestArtifacts` deliberately returns every one of them — right for
  // the task page, wrong here. Reading that list on a per-attempt page sent
  // the operator to another attempt's review: attempt 3's page listed and
  // opened attempt 1's pull request. The server already answers the scoped
  // question — see pkg/orchestration's JobDeliveryFromArtifacts — so the page
  // reads the job's own delivery and nothing else.
  const primaryPullRequestUrl = $derived(delivery?.pull_request_url ?? '');
  const pullRequestState = $derived(delivery?.pull_request_state ?? '');
  // A delivery can carry a URL without a number when the runner recorded only
  // the link, so the label falls back to a word rather than rendering "#0".
  const pullRequestLabel = $derived.by((): string => {
    const number = delivery?.pull_request_number ?? 0;
    if (number > 0) return `#${number}`;
    return primaryPullRequestUrl === '' ? '' : 'Pull request';
  });

  // A review round is only meaningful while the job is reviewing or sending
  // work back: elsewhere `iteration` counts execution passes, not rounds.
  const inReviewRound = $derived(
    job !== null && (job.state === 'reviewing' || job.state === 're-delegating'),
  );
  const reviewRound = $derived(
    job ? `${job.iteration} / ${job.config?.max_iterations ?? '∞'}` : '—',
  );

  // Every session the job holds, lead first, without repeating the lead when
  // it also appears in session_ids.
  const sessionIDs = $derived.by((): string[] => {
    if (!job) return [];
    const ordered = [job.lead_session_id ?? '', ...(job.session_ids ?? [])];
    return [...new Set(ordered.filter((id) => id !== ''))];
  });

  // The job to hold an event stream open for: this one, while it is still
  // running. It is derived to a plain id on purpose — the effect below must
  // depend on WHICH job to stream, not on the job record, or every event it
  // delivers would patch the record, re-run the effect and reopen the stream.
  const streamJobID = $derived(job !== null && !isTerminalJobState(job.state) ? job.job_id : '');

  // A job that has not stopped still emits events. Subscribe while the view is
  // open so the state, the iteration and the event log move on their own, and
  // release the stream when the operator leaves or the job finishes.
  $effect(() => {
    const jobID = streamJobID;
    const tenantID = orchestratorStore.activeTenantID;
    if (jobID === '') {
      stopEventStream();
      return;
    }
    startEventStream(tenantID, jobID);
    return () => { stopEventStream(); };
  });

  // The registered runners and the states each declares. A job's stepper is
  // built from the runner that drove it, so the view reads the registry once
  // rather than keeping its own copy of the engine's states.
  let runners = $state<JobRunnerInfo[]>([]);

  $effect(() => {
    void (async () => {
      try {
        const client = await getSDKClient();
        runners = await client.orchestrator.listRunners();
      } catch (err: unknown) {
        // A stepper with no runner says so in place; it is not worth a toast
        // over the job the operator actually came here to read.
        console.warn('failed to load job runners', err);
      }
    })();
  });

  // ---------------------------------------------------------------------------
  // Spend — read from the consumption ledger, not from job memory
  // ---------------------------------------------------------------------------
  //
  // IT USED TO READ job.memory['token_usage'], which no production writer
  // sets: the turn recorder writes the LEDGER (pkg/orchestration/
  // turn_usage_recorder.go), so the Cost tab said "No cost data available for
  // this job" for every job that had ever run. This reads the same record the
  // spend cap is enforced from, scoped to this job.
  let spend = $state<GetSpendReportResponse | null>(null);
  let spendError = $state('');

  // Derived to plain ids on purpose: the effect must depend on WHICH job to
  // read, not on the job record, or every event that patches the record would
  // re-issue the read.
  const spendJobID = $derived(job?.job_id ?? '');
  const spendTaskID = $derived(job?.task_id ?? '');
  const spendProjectID = $derived(job?.config?.project_id ?? '');

  $effect(() => {
    const tenantID = orchestratorStore.activeTenantID;
    const jobID = spendJobID;
    const taskID = spendTaskID;
    const projectID = spendProjectID;
    // THE SPEND ON SCREEN BELONGS TO THE JOB NAMED ABOVE IT. This component
    // stays mounted across a navigation from one job to the next, so leaving
    // the previous job's figures rendered while the new read is in flight
    // attributes A's tokens to B. The same reasoning as CostView's.
    spend = null;
    spendError = '';
    if (jobID === '') return;
    void loadSpend(tenantID, projectID, taskID, jobID);
  });

  async function loadSpend(
    tenantID: string,
    projectID: string,
    taskID: string,
    jobID: string,
  ): Promise<void> {
    try {
      const client = await getSDKClient();
      // The ledger's chain is a PATH, so a job is addressed through its
      // project and task. A job that has neither — started directly, outside
      // any task — is filed under the ledger's no-value sentinel, which is
      // what addresses that subtree; see lib/cost.ts.
      const resp = await client.orchestrator.getSpendReport({
        tenant_id: tenantID,
        project_id: projectID || SPEND_UNATTRIBUTED_LEVEL,
        task_id: taskID || SPEND_UNATTRIBUTED_LEVEL,
        job_id: jobID,
      });
      if (!isCurrentSpendRequest(tenantID, jobID)) return;
      spend = resp;
      spendError = '';
    } catch (err: unknown) {
      if (!isCurrentSpendRequest(tenantID, jobID)) return;
      // REPORTED, NOT SWALLOWED: a read that failed and a job that cost
      // nothing must never render the same.
      spend = null;
      spendError = spendErrorMessage(err, "this job's spend");
    }
  }

  /**
   * isCurrentSpendRequest says whether the reply to (tenantID, jobID) is still
   * the one this page is waiting for. The tenant is part of the request's
   * identity: switching tenant leaves the job id untouched, so a job-only
   * check accepts the previous tenant's report.
   */
  function isCurrentSpendRequest(tenantID: string, jobID: string): boolean {
    return jobID === spendJobID && tenantID === orchestratorStore.activeTenantID;
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function isCancellable(state: JobState): boolean {
    return state !== 'completed' && state !== 'failed' && state !== 'cleanup';
  }

  const formatTime = (ts: number): string => formatDateTime(ts);

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  /** handleBack returns to the list as the operator left it — see TaskDetailView. */
  function handleBack(): void {
    selectJob(null);
    navigateTo(listReturnHash('/jobs'));
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

  /**
   * handleReadmit returns the job's task to the queue. It is the TASK verb,
   * not a job one: a concluded job is over, and what a person retries is the
   * work, on a fresh attempt.
   */
  async function handleReadmit(): Promise<void> {
    if (!job || !job.task_id || readmitting) return;
    readmitting = true;
    try {
      const client = await getSDKClient();
      await client.orchestrator.readmitTask(orchestratorStore.activeTenantID, job.task_id);
      showToast('success', `Task ${job.task_id} readmitted`);
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Readmit failed: ${msg}`);
    } finally {
      readmitting = false;
    }
  }
</script>

{#if !job}
  <div class="flex items-center justify-center h-full">
    <div class="text-base-content/30">No job selected</div>
  </div>
{:else}
  <div class="page page-narrow">

    <!-- Header -->
    <div class="flex flex-wrap items-start gap-x-6 gap-y-3">
      <div class="min-w-0 flex-1">
        <button
          class="-ml-2 mb-1 inline-flex items-center gap-1 rounded-field px-2 py-0.5 text-sm text-primary hover:bg-base-200"
          onclick={handleBack}
          aria-label="Back to jobs list"
        >
          <ChevronLeft class="h-4 w-4" />
          Jobs
        </button>
        <h1 class="display text-[1.75rem] text-base-content">{job.description || job.job_id}</h1>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <StatusBadge
            text={job.state}
            variant={statusToVariant(job.state)}
            pulse={job.state === 'executing'}
          />
          <span class="font-mono text-xs text-base-content/60">{job.job_id}</span>
        </div>
        {#if streamJobID !== '' && eventSubStore.liveUpdatesUnavailable(streamJobID)}
          <!--
            The job's event stream ended in error (refused, or not an event
            stream). It is not retried on its own, so say plainly that the page
            has stopped moving and offer the one action that restarts it.
          -->
          <div
            class="mt-3 inline-flex flex-wrap items-center gap-x-3 gap-y-1 rounded-field border px-3 py-1.5 text-sm"
            style:background-color="var(--tone-warning-bg)"
            style:color="var(--tone-warning-fg)"
            style:border-color="color-mix(in oklch, var(--tone-warning-fg) 30%, transparent)"
            role="status"
            data-testid="live-updates-unavailable"
          >
            <RadioTower class="h-4 w-4 shrink-0" />
            <span>Live updates unavailable. This page no longer refreshes on its own.</span>
            <button
              class="font-medium underline underline-offset-2 hover:no-underline"
              onclick={() => { retryEventStream(orchestratorStore.activeTenantID, streamJobID); }}
            >Reconnect</button>
          </div>
        {/if}
      </div>

      <!-- State-aware action buttons -->
      <div class="flex shrink-0 items-center gap-2">
        <button
          class="btn btn-sm btn-ghost gap-1"
          onclick={() => { activeTab = 'events'; }}
          aria-label="Open event stream"
        >
          <Radio class="h-4 w-4" />
          Events
        </button>

        {#if isCancellable(job.state)}
          <button
            class="btn btn-sm btn-outline btn-error gap-1"
            onclick={() => { void handleCancel(); }}
            disabled={cancelling}
            aria-label="Cancel job"
          >
            {#if cancelling}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <X class="h-4 w-4" />
            {/if}
            Cancel
          </button>
        {/if}

        {#if job.state === 'failed' || conclusion !== null}
          <button
            class="btn btn-sm btn-primary gap-1"
            aria-label="Readmit task"
            title={canReadmit
              ? 'Return the task to the queue for another attempt'
              : 'The task concluded terminally; it cannot be readmitted'}
            disabled={!canReadmit || readmitting}
            onclick={() => { void handleReadmit(); }}
          >
            {#if readmitting}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <RotateCcw class="h-4 w-4" />
            {/if}
            Readmit
          </button>
        {/if}
      </div>
    </div>

    <!-- State machine stepper -->
    <div class="card bg-base-200 rounded-box p-4">
      <JobStateMachine state={job.state} runner={job.runner ?? ''} {runners} />
    </div>

    <!-- Summary: the first two cards are the runner's, the third is common. -->
    <div class="grid grid-cols-1 sm:grid-cols-3 gap-4">
      {#if workflowJob}
        <div class="card bg-base-200 rounded-box p-4 space-y-1" data-testid="summary-workflow">
          <div class="text-xs font-medium text-base-content/60">Workflow</div>
          <div class="font-semibold text-sm">{job.workflow_name || job.config?.workflow_name || '—'}</div>
          <div class="text-xs text-base-content/60">Lead backend: {job.config?.lead_backend || '—'}</div>
        </div>

        <div class="card bg-base-200 rounded-box p-4 space-y-1" data-testid="summary-lead">
          <div class="text-xs font-medium text-base-content/60">Lead model</div>
          <div class="font-semibold text-sm">{job.config?.lead_model || '—'}</div>
          <div class="text-xs text-base-content/60">Tenant: {job.tenant_id}</div>
        </div>
      {:else}
        <div class="card bg-base-200 rounded-box p-4 space-y-1" data-testid="summary-attempt">
          <div class="text-xs font-medium text-base-content/60">Attempt</div>
          <div class="font-semibold text-sm">{attempt === '' ? '—' : attempt}</div>
          <div class="text-xs text-base-content/60">
            Budget: {job.attempt_budget && job.attempt_budget > 0 ? job.attempt_budget : '—'}
            · Runner: {runner}
          </div>
        </div>

        <div class="card bg-base-200 rounded-box p-4 space-y-1" data-testid="summary-agent">
          <div class="text-xs font-medium text-base-content/60">Agent</div>
          {#if job.agent_id}
            <a
              class="link link-primary font-semibold text-sm inline-flex items-center gap-1"
              href="#/agents/{job.agent_id}"
              onclick={(e) => { e.preventDefault(); routeFleetNode(job.agent_id ?? ''); }}
            >
              <Bot class="h-3.5 w-3.5" />
              {job.agent_id}
            </a>
          {:else}
            <div class="font-semibold text-sm" title="The work ran on this daemon's own backend">
              {job.node || '—'}
            </div>
          {/if}
          <div class="text-xs text-base-content/60">Daemon: {job.node || '—'}</div>
        </div>
      {/if}

      <div class="card bg-base-200 rounded-box p-4 space-y-1">
        <div class="text-xs font-medium text-base-content/60">Timing</div>
        <div class="text-sm"><span class="text-base-content/60">Started</span> <span class="tabular-nums">{formatTime(job.started_at)}</span></div>
        {#if job.state === 'completed' || job.state === 'failed'}
          <div class="text-sm"><span class="text-base-content/60">Completed</span> <span class="tabular-nums">{formatTime(job.updated_at)}</span></div>
        {/if}
        <div class="text-xs text-base-content/60">Duration: <strong>{duration}</strong></div>
      </div>
    </div>

    <!-- Why the job stopped, in the engine's own closed vocabulary. -->
    {#if conclusion !== null}
      <div class="card bg-base-200 rounded-box p-4 space-y-1" data-testid="terminal-reason">
        <div class="flex items-center gap-2">
          <div class="text-xs font-medium text-base-content/60">Outcome</div>
          <span class="badge badge-sm badge-{conclusion.tone}">{conclusion.label}</span>
        </div>
        <div class="text-sm text-base-content/70">{conclusion.detail}</div>
      </div>
    {/if}

    <!--
      Iteration progress is the WORKFLOW runner's: it counts review rounds
      over a DAG of work items. An sdlc job has one work item and no DAG, so
      the same panel rendered `0/10` and `Tasks 0%` for it — go-quicbaas#796.
    -->
    {#if workflowJob}
      <div class="card bg-base-200 rounded-box p-4 space-y-3" data-testid="iteration-progress">
        <div class="flex items-center justify-between">
          <div class="text-sm font-semibold">Iteration Progress</div>
          <div class="text-xs text-base-content/60">
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
    {:else}
      <!-- The attempt's lifecycle moments, with how long each one took. -->
      <div class="card bg-base-200 rounded-box p-4 space-y-2" data-testid="stage-timeline">
        <div class="text-sm font-semibold">Stage timeline</div>
        {#if stages.length === 0}
          <div class="text-xs text-base-content/30 py-2">
            No lifecycle moment has been entered yet.
          </div>
        {:else}
          <ul class="space-y-1" aria-label="Stage timeline">
            {#each stages as span, i (i)}
              <li class="flex items-center gap-3 text-xs border-b border-base-300/30 py-1">
                <span class="badge badge-xs {span.open ? 'badge-primary' : 'badge-ghost'} shrink-0">
                  {span.stage}
                </span>
                <span class="font-mono text-base-content/40 shrink-0 w-32">
                  {formatTimeOnly(span.enteredAt)}
                </span>
                <span class="text-base-content/60">{formatDuration(span.durationMs)}</span>
                {#if span.open}
                  <span class="text-base-content/30">(still open)</span>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}
      </div>

      <!-- What the attempt published: the branch, the head, the pull request. -->
      <div class="card bg-base-200 rounded-box p-4 space-y-2" data-testid="delivery">
        <div class="text-sm font-semibold">Delivery</div>
        {#if delivery === null}
          <div class="text-xs text-base-content/30 py-2">
            This attempt has published nothing yet.
          </div>
        {:else}
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 text-sm">
            <div>
              <div class="text-xs font-medium text-base-content/60">Branch</div>
              <div class="font-mono text-xs inline-flex items-center gap-1">
                <GitBranch class="h-3 w-3 text-base-content/50" />
                {delivery.branch || '—'}
              </div>
            </div>
            <div>
              <div class="text-xs font-medium text-base-content/60">Head</div>
              <div class="font-mono text-xs">{delivery.head_sha ? delivery.head_sha.slice(0, 12) : '—'}</div>
            </div>
            <div>
              <div class="text-xs font-medium text-base-content/60">Pull request</div>
              {#if delivery.pull_request_url}
                <a
                  class="link link-primary text-sm inline-flex items-center gap-1"
                  href={delivery.pull_request_url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <GitPullRequest class="h-3.5 w-3.5" />
                  <!--
                    The same label the summary row uses: the server accepts a
                    pull-request artifact whose metadata carries no number, and
                    "#0" is a pull request nobody can look up.
                  -->
                  {pullRequestLabel}
                  {#if delivery.pull_request_state}
                    <span class="badge badge-xs badge-ghost">{delivery.pull_request_state}</span>
                  {/if}
                  <ExternalLink class="h-3 w-3" />
                </a>
              {:else}
                <div class="text-sm" title="A carve-out change lands without a pull request">—</div>
              {/if}
            </div>
          </div>

          {#if (delivery.pull_request_number ?? 0) > 0 && job.config?.project_id}
            <!-- Which of the project's reviewer profiles reviews this pull request. -->
            <SdlcReviewerPicker
              tenantId={orchestratorStore.activeTenantID}
              projectId={job.config.project_id}
              pullRequest={delivery.pull_request_number ?? 0}
            />
          {/if}

          {#if reviewRounds.length > 0}
            <div class="space-y-1 pt-2" aria-label="Review rounds">
              <div class="text-xs font-medium text-base-content/60">Review rounds</div>
              {#each reviewRounds as round, i (i)}
                <div class="flex items-start gap-2 text-xs border-b border-base-300/30 py-1">
                  <span class="text-base-content/40 shrink-0">Round {round.round}</span>
                  <span class="badge badge-xs {reviewVerdictTone(round.verdict)} shrink-0">
                    {round.verdict}
                  </span>
                  <span class="font-mono text-base-content/40 shrink-0">{round.reviewer}</span>
                  {#if round.findings}
                    <span class="text-base-content/60 break-all">{round.findings}</span>
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        {/if}
      </div>

      <!-- What the project's own verification commands said. -->
      {#if verification.length > 0}
        <div class="card bg-base-200 rounded-box p-4 space-y-2" data-testid="verification">
          <div class="text-sm font-semibold">Verification</div>
          <ul class="space-y-1" aria-label="Verification results">
            {#each verification as result, i (i)}
              <li class="flex items-start gap-2 text-xs border-b border-base-300/30 py-1">
                {#if result.passed}
                  <CheckCircle2 class="h-3.5 w-3.5 text-success shrink-0" />
                {:else}
                  <XCircle class="h-3.5 w-3.5 text-error shrink-0" />
                {/if}
                <span class="font-mono shrink-0">{result.name}</span>
                <span class="text-base-content/60 break-all">{result.message ?? ''}</span>
              </li>
            {/each}
          </ul>
        </div>
      {/if}

      <!--
        The worker's own last turn, when the daemon recorded one. The header
        of counts is the DAEMON's measurement of the turn and is rendered
        apart from the reply, which is the worker's own words.
      -->
      {#if workerTurn !== null}
        <div class="card bg-base-200 rounded-box p-4 space-y-2" data-testid="worker-reply">
          <div class="flex items-center gap-2 flex-wrap">
            <div class="text-sm font-semibold">Worker's last turn</div>
            {#if workerTurn.silent}
              <span class="badge badge-sm badge-warning">answered with no text</span>
            {/if}
            {#if workerTurn.truncated}
              <span class="badge badge-sm badge-ghost">reply truncated</span>
            {/if}
          </div>

          {#if workerTurn.metrics.length > 0}
            <div class="flex flex-wrap gap-x-3 gap-y-1 text-xs" aria-label="Turn metrics">
              {#each workerTurn.metrics as metric (metric.key)}
                <span class="font-mono text-base-content/40">
                  {metric.key}=<span class="text-base-content/70">{metric.value}</span>
                </span>
              {/each}
            </div>
          {/if}

          {#if workerTurn.silent}
            <div class="text-xs text-base-content/60">
              The worker produced no text at all. A silent turn is evidence, not a
              failure of the work — see the outcome above for what the runner
              concluded from it.
            </div>
          {:else}
            <pre class="text-xs whitespace-pre-wrap break-all text-base-content/70">{workerTurn.reply}</pre>
          {/if}
        </div>
      {/if}
    {/if}

    <!-- Tabs -->
    <div role="tablist" class="tabs tabs-border">
      {#each visibleTabs as tab}
        <button
          role="tab"
          class="tab"
          class:tab-active={activeTab === tab}
          aria-selected={activeTab === tab}
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
              <div class="text-xs font-medium text-base-content/60">Tenant</div>
              <div class="font-mono text-sm">{job.tenant_id}</div>
            </div>
            <!--
              The work-item counters count a DAG. An sdlc job has one work
              item and no DAG, so it gets its attempt accounting instead.
            -->
            {#if workflowJob}
              <div>
                <div class="text-xs font-medium text-base-content/60">Iteration</div>
                <div class="text-sm">{job.iteration}</div>
              </div>
              <div>
                <div class="text-xs font-medium text-base-content/60">Task Count</div>
                <div class="text-sm">{job.task_count}</div>
              </div>
              <div>
                <div class="text-xs font-medium text-base-content/60">Completed</div>
                <div class="text-sm text-success">{job.completed}</div>
              </div>
              <div>
                <div class="text-xs font-medium text-base-content/60">Failed Tasks</div>
                <div class="text-sm text-error">{job.failed}</div>
              </div>
            {:else}
              <div>
                <div class="text-xs font-medium text-base-content/60">Attempt</div>
                <div class="text-sm">{attempt === '' ? '—' : attempt}</div>
              </div>
              <div>
                <div class="text-xs font-medium text-base-content/60">Runner</div>
                <div class="badge badge-sm">{runner}</div>
              </div>
            {/if}
            <div>
              <div class="text-xs font-medium text-base-content/60">Started</div>
              <div class="text-sm">{formatTime(job.started_at)}</div>
            </div>
            <div>
              <div class="text-xs font-medium text-base-content/60">Updated</div>
              <div class="text-sm">{formatTime(job.updated_at)}</div>
            </div>
            {#if workflowJob && job.config?.lead_backend}
              <div>
                <div class="text-xs font-medium text-base-content/60">Lead Backend</div>
                <div class="badge badge-sm">{job.config.lead_backend}</div>
              </div>
            {/if}
            {#if workflowJob && job.workflow_name}
              <div>
                <div class="text-xs font-medium text-base-content/60">Workflow</div>
                <div class="text-sm">{job.workflow_name}</div>
              </div>
            {/if}
            <div>
              <div class="text-xs font-medium text-base-content/60">Task</div>
              {#if sourceTask !== null}
                <a
                  class="link link-primary text-sm inline-flex items-center gap-1"
                  href="#/tasks/{sourceTask.id}"
                  onclick={(e) => { e.preventDefault(); routeTaskDetail(sourceTask.id); }}
                >
                  <ClipboardList class="h-3.5 w-3.5" />
                  {sourceTask.subject || sourceTask.id}
                </a>
              {:else}
                <div class="text-sm">—</div>
              {/if}
            </div>
            <div>
              <div class="text-xs font-medium text-base-content/60">Issue</div>
              {#if ref !== null && ref.href !== ''}
                <a
                  class="link link-primary text-sm font-mono inline-flex items-center gap-1"
                  href={ref.href}
                  target="_blank"
                  rel="noopener noreferrer"
                  title="Open issue {ref.label} on the forge"
                >
                  {ref.label}
                  <ExternalLink class="h-3 w-3" />
                </a>
              {:else if ref !== null}
                <div class="text-sm font-mono" title="Forge issue {ref.label}">{ref.label}</div>
              {:else}
                <div class="text-sm">—</div>
              {/if}
            </div>
            {#if workflowJob && inReviewRound}
              <div>
                <div class="text-xs font-medium text-base-content/60">Review round</div>
                <div class="text-sm">{reviewRound}</div>
              </div>
            {/if}
            <!--
              This attempt's pull request, never the task's whole list: see
              primaryPullRequestUrl above.
            -->
            <div data-testid="summary-pull-request">
              <div class="text-xs font-medium text-base-content/60">Pull request</div>
              {#if pullRequestLabel === ''}
                <div class="text-sm">—</div>
              {:else}
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="inline-flex items-center gap-1 text-sm">
                    <GitPullRequest class="h-3.5 w-3.5 text-base-content/50" />
                    {#if primaryPullRequestUrl !== ''}
                      <a
                        class="link link-primary"
                        href={primaryPullRequestUrl}
                        target="_blank"
                        rel="noopener noreferrer"
                      >{pullRequestLabel}</a>
                    {:else}
                      <span>{pullRequestLabel}</span>
                    {/if}
                    {#if pullRequestState !== ''}
                      <span class="badge badge-xs badge-ghost">{pullRequestState}</span>
                    {/if}
                  </span>
                  {#if primaryPullRequestUrl !== ''}
                    <a
                      class="btn btn-xs btn-outline gap-1"
                      href={primaryPullRequestUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      <ExternalLink class="h-3 w-3" />
                      Open PR
                    </a>
                  {/if}
                </div>
              {/if}
            </div>
          </div>

          <!--
            The run-time prompt material every session of this job was spawned
            with, after its agent's own prompt: the audit of what the job added.
          -->
          {#if job.prompt_append}
            <details class="mb-4" data-testid="job-prompt-append">
              <summary class="text-xs font-medium text-base-content/60 cursor-pointer">
                Appended to every session's prompt ({new TextEncoder().encode(job.prompt_append).length} bytes)
              </summary>
              <pre class="mt-2 whitespace-pre-wrap break-words font-mono text-xs bg-base-200 rounded p-2">{job.prompt_append}</pre>
            </details>
          {/if}

          <!-- Sessions the job holds: the lead first, then every worker. -->
          <div class="space-y-1">
            <div class="text-xs font-medium text-base-content/60">Sessions</div>
            {#if sessionIDs.length === 0}
              <div class="text-sm text-base-content/30">
                This job has no open session. A session appears here once the job spawns one.
              </div>
            {:else}
              <div class="flex flex-wrap gap-2" aria-label="Job sessions">
                {#each sessionIDs as sessionID (sessionID)}
                  <a
                    class="ref-chip"
                    href="#/sessions/{sessionID}"
                    onclick={(e) => { e.preventDefault(); routeSessionDetail(sessionID); }}
                    title={sessionID === job.lead_session_id ? 'Lead session ' + sessionID : 'Session ' + sessionID}
                  >
                    <Terminal class="h-3 w-3" />
                    {sessionID === job.lead_session_id ? 'lead' : ''}
                    {sessionID.length > 12 ? sessionID.slice(0, 8) + '…' : sessionID}
                  </a>
                {/each}
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
          <!-- This job's entries in the consumption ledger. The iteration
               gauge is the WORKFLOW runner's and renders only for it; the
               spend below is every runner's. go-quicbaas#794. -->
          <div class="space-y-4">
            {#if workflowJob}
              <div class="flex flex-col items-center gap-2">
                <BudgetGauge
                  percent={job.config?.max_iterations
                    ? Math.min(100, (job.iteration / job.config.max_iterations) * 100)
                    : 0}
                  label="Iterations Used"
                />
                <div class="text-xs text-base-content/60">
                  {job.iteration} of {job.config?.max_iterations ?? '∞'} max
                </div>
              </div>
            {:else}
              <div class="text-sm text-base-content/40" data-testid="cost-no-iteration-budget">
                {noIterationBudgetNotice(runner)}
              </div>
            {/if}

            {#if spendError !== ''}
              <div role="alert" class="alert alert-error">
                <span>{spendError}</span>
              </div>
            {:else if spend === null}
              <div class="text-sm text-base-content/40">Loading this job's spend…</div>
            {:else}
              <div class="grid grid-cols-2 lg:grid-cols-5 gap-3">
                <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
                  <div class="stat-title text-xs font-medium text-base-content/60">Total tokens</div>
                  <div class="stat-value text-xl" data-testid="job-total-tokens">
                    {formatTokens(spend.total_tokens)}
                  </div>
                  <div class="stat-desc">{spend.entries ?? 0} turns</div>
                </div>
                <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
                  <div class="stat-title text-xs font-medium text-base-content/60">Input</div>
                  <div class="stat-value text-xl">{formatTokens(spend.input_tokens)}</div>
                </div>
                <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
                  <div class="stat-title text-xs font-medium text-base-content/60">Output</div>
                  <div class="stat-value text-xl">{formatTokens(spend.output_tokens)}</div>
                </div>
                <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
                  <div class="stat-title text-xs font-medium text-base-content/60">List-price equivalent</div>
                  <div class="stat-value text-xl" data-testid="job-cost">{formatEquivalent(spend, spend.currency)}</div>
                  <div class="stat-desc">{equivalentNote(spend)}</div>
                </div>
                <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
                  <div class="stat-title text-xs font-medium text-base-content/60">Billed</div>
                  <div class="stat-value text-xl" data-testid="job-billed">{formatBilled(spend, spend.currency)}</div>
                  <div class="stat-desc">{billedNote(spend)}</div>
                </div>
              </div>

              {#if (spend.accounts ?? []).length === 0}
                <div class="text-sm text-base-content/40">
                  No pooled account has been charged for this job.
                </div>
              {:else}
                <div class="space-y-2">
                  <div class="section-title">
                    Accounts that paid
                  </div>
                  {#each spend.accounts ?? [] as account (account.account_id)}
                    <div class="flex items-center gap-2 flex-wrap" data-testid="job-spend-account">
                      <span class="text-xs font-mono">{account.display_name || account.account_id}</span>
                      <StatusBadge text={policyLabel(account)} variant={policyVariant(account)} />
                      <span class="text-xs text-base-content/50 ml-auto">
                        {formatTokens(account.total_tokens)} tokens
                      </span>
                    </div>
                  {/each}
                </div>
              {/if}
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
