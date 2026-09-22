<!--
  TaskDetailView renders one task. Accessible via: #/tasks/{id}

  Layout:
  - Header: subject, status pill, forge issue link, project, Back + Reload
  - State card: status, block kind, priority, assignee, provenance, timestamps
  - Attempts card: charged / budget, dispatched, refunds, deferral
  - "Waiting on a person" box when the task is blocked or failed, with the
    recorded reason — or an explicit statement that none was recorded
  - Jobs card: every attempt at this task, the open one first
  - Pull requests card: delivered pull requests from the output artifacts
  - Issue body (the task description) in a collapsible block
  - Raw JSON drawer

  The task is resolved from the orchestrator store by route param, the same
  way AgentDetailView resolves an agent. Its ATTEMPTS are read separately and
  narrowed to this task by the server (loadTaskJobs); the tenant-wide job
  list holds what an operator has recently browsed, not one task's history.
  A fresh page load asks the store to load tasks once and reads this task's
  attempts; Reload refetches both unconditionally.

  An empty Jobs or Pull requests panel says WHICH emptiness it means — see
  taskJobsEmptyMessage / taskPullRequestEmptyMessage in lib/tasks.ts.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { orchestratorStore, loadTasks, loadTaskJobs, refresh, type JobStatus } from '../../stores/orchestrator.svelte.ts';
  import { routerStore, routeTasks, routeJobDetail } from '../../stores/router.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { statusToVariant } from '../../components/core/statusVariant.ts';
  import { ChevronLeft, ClipboardList, RefreshCw, AlertTriangle, ExternalLink, GitPullRequest } from 'lucide-svelte';
  import { formatDateTime, formatRelative, hasInstant } from '../../lib/time.ts';
  import {
    artifactUrl,
    blockedReason,
    budgetLabel,
    chargedAttempts,
    dispatchedAttempts,
    isWaitingOnPerson,
    issueRef,
    latestTaskJobID,
    loadErrorMessage,
    NO_REASON_RECORDED,
    pullRequestArtifacts,
    taskAgentID,
    taskJobsEmptyMessage,
    taskPullRequestEmptyMessage,
  } from '../../lib/tasks.ts';

  /** The permission the tasks read routes are guarded by (routes_permissions_generated.go). */
  const TASKS_READ_PERMISSION = 'orchestrator:status';

  // ---------------------------------------------------------------------------
  // Derived: resolve the task and its jobs from the store by route param
  // ---------------------------------------------------------------------------

  const taskId = $derived(routerStore.params['id'] ?? '');

  const task = $derived(orchestratorStore.tasks.find((t) => t.id === taskId) ?? null);

  // The task's attempts come from the SERVER, narrowed to this task, not
  // sieved out of the tenant-wide job list. The tenant list holds the jobs an
  // operator has recently browsed; a task's history is every attempt at it,
  // concluded ones included. go-quicbaas#822.
  const attemptJobs = $derived(
    orchestratorStore.taskJobsTaskID === taskId ? orchestratorStore.taskJobs : [],
  );

  // THE LATEST ATTEMPT, WHICH IS NOT ALWAYS THE OPEN ONE. `spawned_job_id`
  // names the attempt still open and is cleared the moment one concludes
  // (recordAttemptHistory, pkg/orchestration/task_attempt.go), so a settled
  // task named no job at all and this page could not link the run that
  // settled it — go-quicbaas#856. latestTaskJobID reads the concluded
  // attempts out of the task's own history when there is no open one.
  const latestJobID = $derived(task ? latestTaskJobID(task) : '');

  /** True while an attempt is still open, which is what "Current" means. */
  const attemptIsOpen = $derived((task?.spawned_job_id ?? '') !== '');

  const latestJob = $derived(
    latestJobID === ''
      ? null
      : attemptJobs.find((j) => j.job_id === latestJobID) ?? null,
  );

  // Every attempt at this task other than the latest, which is shown above.
  const priorJobs = $derived(attemptJobs.filter((j) => j.job_id !== latestJobID));

  const PRIOR_JOB_COLUMNS: Column<JobStatus>[] = [
    { key: 'attempt', label: 'Attempt', sortable: true },
    { key: 'job_id', label: 'Job ID', sortable: true, searchable: true },
    { key: 'state', label: 'State', sortable: true },
    { key: 'started_at', label: 'Started', sortable: true },
  ];

  // The agent working the task: its claimant when it has one, otherwise the
  // fleet agent the latest attempt ran on. The dispatcher never writes
  // `assigned_to`, so for an auto-spawned task the job record is the only
  // place the agent is recorded (see taskAgentID in lib/tasks.ts).
  const agentID = $derived(task ? taskAgentID(task, attemptJobs) : '');

  // An attempt's ordinal is on the record itself; older records carry it only
  // on the config they were launched from.
  function attemptOrdinal(job: { attempt?: number; config?: { attempt?: number } }): string {
    const ordinal = job.attempt ?? job.config?.attempt ?? 0;
    return ordinal > 0 ? String(ordinal) : '—';
  }

  const ref = $derived(task ? issueRef(task) : null);
  const pullRequests = $derived(task ? pullRequestArtifacts(task) : []);
  const reason = $derived(task ? blockedReason(task) : '');
  const waiting = $derived(task ? isWaitingOnPerson(task) : false);

  const loadError = $derived(
    orchestratorStore.tasksError === null || orchestratorStore.tasksError === undefined
      ? ''
      : loadErrorMessage(orchestratorStore.tasksError, 'this task', TASKS_READ_PERMISSION),
  );

  // On a fresh page load at #/tasks/:id the store may not hold tasks yet.
  // Ask it to load once; the derived `task` resolves when the list arrives.
  $effect(() => {
    if (taskId !== '' && task === null) {
      untrack(() => { void loadTasks(); });
    }
  });

  // The attempt history is read per task, so it is re-read whenever the route
  // names a different one.
  $effect(() => {
    const id = taskId;
    if (id === '') return;
    untrack(() => { void loadTaskJobs(id); });
  });

  // Project names, so the header reads as a name rather than an id. Loaded
  // once per tenant; a project the list does not know shows its id.
  let projectNames = $state<Record<string, string>>({});

  $effect(() => {
    const tenantID = orchestratorStore.activeTenantID;
    void loadProjectNames(tenantID);
  });

  async function loadProjectNames(tenantID: string): Promise<void> {
    try {
      const client = await getSDKClient();
      const resp = await client.projects.listProjects(tenantID);
      const names: Record<string, string> = {};
      for (const p of resp.projects ?? []) names[p.id] = p.name;
      projectNames = names;
    } catch {
      // The header falls back to the project id.
      projectNames = {};
    }
  }

  const projectName = $derived(task?.project_id ? projectNames[task.project_id] ?? '' : '');

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let reloading = $state(false);
  let rawJsonOpen = $state(false);

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  function handleBack(): void {
    routeTasks();
  }

  async function handleReload(): Promise<void> {
    if (reloading) return;
    reloading = true;
    try {
      await Promise.allSettled([refresh('tasks'), loadTaskJobs(taskId)]);
    } finally {
      reloading = false;
    }
  }

  function sourceLabel(kind: string): string {
    return kind.replace('_', ' ');
  }
</script>

{#if !task}
  <div class="flex flex-col items-center justify-center h-full gap-3 text-base-content/30 p-6">
    <ClipboardList class="h-10 w-10" />
    {#if orchestratorStore.tasksLoading}
      <span class="loading loading-spinner loading-md text-primary"></span>
      <div class="text-sm">Loading task…</div>
    {:else if loadError !== ''}
      <div class="alert alert-error text-sm max-w-xl" role="alert">
        <AlertTriangle class="h-4 w-4" />
        <span>{loadError}</span>
      </div>
    {:else}
      <div class="text-sm">Task not found or not yet loaded.</div>
    {/if}
    <div class="flex items-center gap-2">
      <button class="btn btn-ghost btn-sm gap-1" onclick={handleBack}>
        <ChevronLeft class="h-4 w-4" />
        Back to Tasks
      </button>
      <button
        class="btn btn-ghost btn-sm gap-1"
        onclick={() => { void handleReload(); }}
        disabled={reloading}
        aria-label="Reload task"
      >
        <RefreshCw class="h-4 w-4" />
        Reload
      </button>
    </div>
  </div>
{:else}
  <div class="p-6 space-y-5 max-w-5xl mx-auto">

    <PageHeader title="Task" subtitle={task.subject} icon={ClipboardList}>
      {#snippet children()}
        <div class="flex items-center gap-2 flex-wrap">
          <button
            class="btn btn-ghost btn-sm gap-1"
            onclick={handleBack}
            aria-label="Back to tasks"
          >
            <ChevronLeft class="h-4 w-4" />
            Back
          </button>
          <button
            class="btn btn-ghost btn-sm gap-1"
            onclick={() => { void handleReload(); }}
            disabled={reloading}
            aria-label="Reload task"
          >
            {#if reloading}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              <RefreshCw class="h-4 w-4" />
            {/if}
            Reload
          </button>
        </div>
      {/snippet}
    </PageHeader>

    <!-- Identity line: status, issue, project, id -->
    <div class="flex items-center gap-2 flex-wrap">
      <StatusBadge
        text={task.status.replace('_', ' ')}
        variant={statusToVariant(task.status)}
        pulse={task.status === 'in_progress'}
      />
      {#if ref !== null}
        {#if ref.href !== ''}
          <a
            class="badge badge-outline font-mono gap-1"
            href={ref.href}
            target="_blank"
            rel="noopener noreferrer"
            title="Open issue {ref.label} on the forge"
          >
            {ref.label}
            <ExternalLink class="h-3 w-3" />
          </a>
        {:else}
          <span class="badge badge-outline font-mono" title="Forge issue {ref.label}">{ref.label}</span>
        {/if}
      {/if}
      {#if task.project_id}
        <a
          class="badge badge-ghost gap-1"
          href={`#/tasks?project=${encodeURIComponent(task.project_id)}`}
          title="Tasks in this project"
        >
          Project: {projectName || task.project_id}
        </a>
      {/if}
      <span class="text-xs text-base-content/40 font-mono">{task.id}</span>
    </div>

    <!-- Waiting on a person -->
    {#if waiting}
      <div class="alert alert-warning items-start" role="status" data-testid="waiting-on-person">
        <AlertTriangle class="h-5 w-5 shrink-0 mt-0.5" />
        <div class="min-w-0">
          <div class="font-semibold text-sm">Waiting on a person</div>
          <div class="text-sm mt-1 whitespace-pre-wrap break-words">
            {reason || NO_REASON_RECORDED}
          </div>
          <div class="text-xs mt-1 opacity-70">
            {#if task.status === 'blocked'}
              Parked{task.block_kind ? ` (${task.block_kind.replace('_', ' ')})` : ''}; no job will move this task until it is readmitted.
            {:else}
              The last attempt failed and the task will not be retried on its own.
            {/if}
          </div>
        </div>
      </div>
    {/if}

    <!-- State card -->
    <div class="card bg-base-200 rounded-box p-5">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">State</div>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
        <div>
          <div class="text-xs text-base-content/40">Status</div>
          <div class="mt-0.5">
            <StatusBadge text={task.status.replace('_', ' ')} variant={statusToVariant(task.status)} />
          </div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Status changed</div>
          <div class="text-xs mt-0.5" title={formatDateTime(task.status_changed_at)}>
            {hasInstant(task.status_changed_at) ? formatRelative(task.status_changed_at) : '—'}
          </div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Priority</div>
          <div class="font-mono text-xs mt-0.5">{task.priority ?? '—'}</div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Assigned to</div>
          <div class="text-xs mt-0.5" data-testid="task-assigned">{agentID || '—'}</div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Source</div>
          <div class="text-xs mt-0.5">
            {#if task.source}
              <span class="badge badge-sm badge-neutral">{sourceLabel(task.source.kind)}</span>
              <span class="font-mono ml-1">{task.source.ref}</span>
            {:else}
              —
            {/if}
          </div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Created by</div>
          <div class="text-xs mt-0.5">{task.created_by || '—'}</div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Created</div>
          <div class="text-xs mt-0.5">{formatDateTime(task.created_at)}</div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Updated</div>
          <div class="text-xs mt-0.5">{formatDateTime(task.updated_at)}</div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Tenant</div>
          <div class="font-mono text-xs mt-0.5">{task.tenant_id}</div>
        </div>
      </div>
    </div>

    <!-- Attempts card -->
    <div class="card bg-base-200 rounded-box p-5">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">Attempts</div>
      <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
        <div class="stat bg-base-300 rounded-box p-3">
          <div class="stat-title text-xs">Charged / budget</div>
          <div class="stat-value text-xl font-mono" data-testid="budget-label">{budgetLabel(task)}</div>
        </div>
        <div class="stat bg-base-300 rounded-box p-3">
          <div class="stat-title text-xs">Dispatched</div>
          <div class="stat-value text-xl font-mono">{task.attempts === undefined ? '—' : dispatchedAttempts(task)}</div>
        </div>
        <div class="stat bg-base-300 rounded-box p-3">
          <div class="stat-title text-xs">Refunded</div>
          <div class="stat-value text-xl font-mono" data-testid="refunded">
            {task.environment_faults === undefined ? '—' : task.environment_faults}
          </div>
          {#if (task.consecutive_environment_faults ?? 0) > 0}
            <div class="stat-desc text-xs">{task.consecutive_environment_faults} in a row</div>
          {/if}
        </div>
        <div class="stat bg-base-300 rounded-box p-3">
          <div class="stat-title text-xs">Retry not before</div>
          <div class="stat-value text-sm" data-testid="retry-not-before">
            {#if hasInstant(task.retry_not_before)}
              <span title={formatDateTime(task.retry_not_before)}>{formatRelative(task.retry_not_before)}</span>
            {:else}
              <span class="text-base-content/40">not deferred</span>
            {/if}
          </div>
        </div>
      </div>
      {#if task.attempts !== undefined && chargedAttempts(task) !== dispatchedAttempts(task)}
        <div class="text-xs text-base-content/50 mt-3">
          {dispatchedAttempts(task) - chargedAttempts(task)} of {dispatchedAttempts(task)} dispatched attempts were refunded because the environment failed, not the work.
        </div>
      {/if}
    </div>

    <!-- Jobs card -->
    <div class="card bg-base-200 rounded-box p-4">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">
        Jobs ({attemptJobs.length})
      </div>
      {#if latestJobID !== ''}
        <div class="flex items-center gap-2 flex-wrap mb-3" data-testid="task-latest-job">
          <span class="text-xs text-base-content/40">{attemptIsOpen ? 'Current' : 'Latest'}</span>
          <button
            class="link link-primary font-mono text-xs"
            onclick={() => routeJobDetail(latestJobID)}
            aria-label="Open job {latestJobID}"
          >{latestJobID}</button>
          {#if latestJob}
            <StatusBadge
              text={latestJob.state}
              variant={statusToVariant(latestJob.state)}
              pulse={latestJob.state === 'executing'}
            />
            <span class="text-xs text-base-content/40">iteration {latestJob.iteration}</span>
          {:else}
            <span class="text-xs text-base-content/30 italic">no record read for this job</span>
          {/if}
        </div>
      {:else if attemptJobs.length === 0}
        <!--
          An empty panel is a claim about the task, so it says which claim.
          "No agent has matched this task yet" was rendered for every empty
          panel, including a task that had dispatched three times and ended
          failed — go-quicbaas#822.
        -->
        <div class="text-sm text-base-content/30 mb-3" data-testid="jobs-empty">
          {taskJobsEmptyMessage(task, attemptJobs.length, orchestratorStore.taskJobsLoading)}
        </div>
      {/if}
      {#if priorJobs.length > 0}
        <DataTable
          columns={PRIOR_JOB_COLUMNS}
          rows={priorJobs}
          rowKey={(job: JobStatus) => job.job_id}
          ariaLabel="Prior attempts"
          emptyTitle="No prior attempts."
        >
          {#snippet row(job: JobStatus)}
            <tr
              class="cursor-pointer hover:bg-base-300/40 transition-colors"
              onclick={() => routeJobDetail(job.job_id)}
              onkeydown={(e) => { if (e.key === 'Enter') routeJobDetail(job.job_id); }}
              tabindex="0"
              role="button"
              aria-label="Open job {job.job_id.slice(0, 8)}"
            >
              <td class="font-mono text-xs">{attemptOrdinal(job)}</td>
              <td class="font-mono text-xs">{job.job_id.slice(0, 12)}…</td>
              <td>
                <StatusBadge text={job.state} variant={statusToVariant(job.state)} />
              </td>
              <td class="text-xs text-base-content/50">{formatDateTime(job.started_at)}</td>
            </tr>
          {/snippet}
        </DataTable>
      {/if}
    </div>

    <!-- Pull requests card -->
    <div class="card bg-base-200 rounded-box p-4">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">
        Pull requests ({pullRequests.length})
      </div>
      {#if pullRequests.length === 0}
        <div class="text-sm text-base-content/30" data-testid="pull-requests-empty">
          {taskPullRequestEmptyMessage(task, pullRequests.length)}
        </div>
      {:else}
        <ul class="space-y-1" aria-label="Pull requests">
          {#each pullRequests as pr (pr.name)}
            {@const href = artifactUrl(pr)}
            <li class="flex items-center gap-2 text-sm">
              <GitPullRequest class="h-4 w-4 text-base-content/50 shrink-0" />
              {#if href !== ''}
                <a class="link link-primary" {href} target="_blank" rel="noopener noreferrer">{pr.name}</a>
                <ExternalLink class="h-3 w-3 text-base-content/40" />
              {:else}
                <span>{pr.name}</span>
              {/if}
              {#if pr.ref?.branch}
                <span class="font-mono text-xs text-base-content/40">{pr.ref.branch}</span>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}
    </div>

    <!-- Issue body -->
    <details class="rounded-box border border-base-300/50 overflow-hidden">
      <summary class="px-4 py-2 bg-base-200/80 text-xs font-semibold text-base-content/60 cursor-pointer">
        {ref !== null ? 'Issue body' : 'Description'}
      </summary>
      {#if task.description}
        <pre class="bg-base-300 p-4 text-xs overflow-auto max-h-96 whitespace-pre-wrap break-words">{task.description}</pre>
      {:else}
        <div class="p-4 text-sm text-base-content/30">No description recorded.</div>
      {/if}
    </details>

    <!-- Raw JSON drawer -->
    <div class="rounded-box border border-base-300/50 overflow-hidden">
      <button
        class="w-full flex items-center justify-between px-4 py-2 bg-base-200/80 text-xs font-semibold text-base-content/60 hover:bg-base-300/60 transition-colors"
        onclick={() => { rawJsonOpen = !rawJsonOpen; }}
        aria-expanded={rawJsonOpen}
        aria-controls="task-raw-json-panel"
      >
        <span>Raw Task State (debug)</span>
        <span>{rawJsonOpen ? '▲' : '▼'}</span>
      </button>
      {#if rawJsonOpen}
        <div id="task-raw-json-panel">
          <pre class="bg-base-300 p-4 text-xs overflow-auto max-h-80 whitespace-pre-wrap break-all font-mono">{JSON.stringify(task, null, 2)}</pre>
        </div>
      {/if}
    </div>

  </div>
{/if}
