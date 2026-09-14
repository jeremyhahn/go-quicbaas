<!--
  TasksView renders the tenant's tasks — hand-created and adopted from the
  forge alike — with the generic Task model's provenance, attempt accounting
  and parked state, plus an optional kanban layout toggle.

  Columns: Subject (with the forge issue `#N` when the task came from one) ·
  Project · Status (a blocked task carries its reason) · Attempts
  (`charged / budget`, with refunds called out) · Assigned (the claimant, or
  the fleet agent the latest attempt ran on) · Job (the latest attempt,
  linked) · Updated. A row opens the task's detail page.

  `#/tasks?project=<id>` narrows the list to one project.
-->
<script lang="ts">
  import { orchestratorStore, loadJobs, refresh, type Task, type TaskStatus } from '../../stores/orchestrator.svelte.ts';
  import { routerStore, routeTaskDetail, routeTasks } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { statusToVariant } from '../../components/core/statusVariant.ts';
  import CreateTaskDialog from '../../components/orchestrator/CreateTaskDialog.svelte';
  import { LayoutList, Columns3, Plus, ClipboardList, UserCheck, CheckCircle, Zap, AlertTriangle, X } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type { DecomposeTaskRequest, ClaimTaskRequest, CompleteTaskRequest } from '@quicbaas/sdk';
  import { formatDateTime, formatRelative } from '../../lib/time.ts';
  import {
    budgetLabel,
    blockedReason,
    issueRef,
    latestTaskJobID,
    loadErrorMessage,
    taskAgentID,
    taskHasDispatched,
    NO_REASON_RECORDED,
  } from '../../lib/tasks.ts';

  type Layout = 'table' | 'kanban';
  type SpawnFilter = 'all' | 'auto_spawned' | 'queued';

  /** The permission the tasks read routes are guarded by (routes_permissions_generated.go). */
  const TASKS_READ_PERMISSION = 'orchestrator:status';

  let layout: Layout = $state('table');
  let spawnFilter: SpawnFilter = $state('all');

  let createTaskOpen = $state(false);
  let decomposingID = $state('');
  let claimingID = $state('');
  let completingID = $state('');

  // Project names, so the Project column reads as a name rather than an id.
  // Loaded once; a task whose project is unknown to the list shows its id.
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
      // The column falls back to the project id; the tasks themselves still load.
      projectNames = {};
    }
  }

  // THE AGENT IS ON THE JOB, NOT ON THE TASK. `assigned_to` is the claimant,
  // and the dispatcher's own path never writes one, so it is empty for the
  // whole life of every auto-spawned task (see taskAgentID in lib/tasks.ts).
  // The tenant's job list is read ONCE for the page — not once per row — and
  // the Assigned column names the agent the task's latest attempt ran on.
  $effect(() => {
    void orchestratorStore.activeTenantID;
    void loadJobs();
  });

  /** The job records the page holds, in the shape taskAgentID reads. */
  const knownJobs = $derived(
    Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : [],
  );

  const projectFilter = $derived(routerStore.params['project'] ?? '');

  function projectLabel(projectID: string | undefined): string {
    if (!projectID) return '';
    return projectNames[projectID] ?? projectID;
  }

  function filteredTasks(): Task[] {
    const all = Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : [];
    const inProject = projectFilter === '' ? all : all.filter((t) => t.project_id === projectFilter);
    if (spawnFilter === 'auto_spawned') return inProject.filter((t) => t.auto_spawn);
    if (spawnFilter === 'queued') return inProject.filter((t) => !t.auto_spawn);
    return inProject;
  }

  const COLUMNS: TaskStatus[] = ['pending', 'claimed', 'in_progress', 'blocked', 'completed', 'failed'];

  function tasksByStatus(status: TaskStatus) {
    return filteredTasks().filter((t) => t.status === status);
  }

  function statusLabel(status: TaskStatus): string {
    return status.replace('_', ' ');
  }

  const loadError = $derived(
    orchestratorStore.tasksError === null || orchestratorStore.tasksError === undefined
      ? ''
      : loadErrorMessage(orchestratorStore.tasksError, 'tasks', TASKS_READ_PERMISSION),
  );

  function emptyTitle(): string {
    if (projectFilter !== '') return `No tasks in ${projectLabel(projectFilter)}.`;
    return 'No tasks tracked — New Task is how you start.';
  }

  function openTask(task: Task): void {
    routeTaskDetail(task.id);
  }

  async function handleDecompose(taskID: string, e: Event): Promise<void> {
    e.stopPropagation();
    decomposingID = taskID;
    const req: DecomposeTaskRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      task_id: taskID,
    };
    try {
      const client = await getSDKClient();
      const resp = await client.orchestrator.decomposeTask(req);
      const count = resp.tasks?.length ?? 0;
      showToast('success', `Task decomposed into ${count} sub-task${count === 1 ? '' : 's'}`);
      void refresh('tasks');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Decompose failed: ${msg}`);
    } finally {
      decomposingID = '';
    }
  }

  async function handleClaim(taskID: string, e: Event): Promise<void> {
    e.stopPropagation();
    claimingID = taskID;
    const req: ClaimTaskRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      task_id: taskID,
      agent_id: 'manual',
    };
    try {
      const client = await getSDKClient();
      await client.orchestrator.claimTask(req);
      showToast('success', 'Task claimed');
      void refresh('tasks');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Claim failed: ${msg}`);
    } finally {
      claimingID = '';
    }
  }

  async function handleComplete(taskID: string, e: Event): Promise<void> {
    e.stopPropagation();
    completingID = taskID;
    const req: CompleteTaskRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      task_id: taskID,
    };
    try {
      const client = await getSDKClient();
      await client.orchestrator.completeTask(req);
      showToast('success', 'Task marked complete');
      void refresh('tasks');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Complete failed: ${msg}`);
    } finally {
      completingID = '';
    }
  }
</script>

<!-- Create Task dialog -->
<CreateTaskDialog bind:open={createTaskOpen} onCreated={() => { void refresh('tasks'); }} />

<div class="p-6 space-y-6">
  <PageHeader title="Tasks" subtitle="{filteredTasks().length} tasks" icon={ClipboardList}>
    {#snippet children()}
      <div class="flex items-center gap-2">
        {#if projectFilter !== ''}
          <span class="badge badge-outline gap-1" data-testid="project-filter">
            Project: {projectLabel(projectFilter)}
            <button
              class="btn btn-ghost btn-xs btn-circle"
              onclick={() => routeTasks()}
              aria-label="Show tasks from every project"
              title="Clear project filter"
            >
              <X class="h-3 w-3" />
            </button>
          </span>
        {/if}
        <!-- Spawn filter -->
        <div class="join">
          <button
            class="btn btn-xs join-item"
            class:btn-primary={spawnFilter === 'all'}
            onclick={() => (spawnFilter = 'all')}
            aria-label="Show all tasks"
          >All</button>
          <button
            class="btn btn-xs join-item gap-1"
            class:btn-secondary={spawnFilter === 'auto_spawned'}
            onclick={() => (spawnFilter = 'auto_spawned')}
            aria-label="Show auto-spawned tasks"
          >
            <Zap class="h-3 w-3" />
            Auto-spawned
          </button>
          <button
            class="btn btn-xs join-item"
            class:btn-ghost={spawnFilter === 'queued'}
            onclick={() => (spawnFilter = 'queued')}
            aria-label="Show queued tasks"
          >Queued</button>
        </div>
        <button
          class="btn btn-sm btn-primary gap-1"
          onclick={() => { createTaskOpen = true; }}
          aria-label="Create new task"
        >
          <Plus class="h-4 w-4" />
          New Task
        </button>
        <div class="join">
          <button
            class="btn btn-sm join-item"
            class:btn-primary={layout === 'table'}
            onclick={() => (layout = 'table')}
            aria-label="Table layout"
          >
            <LayoutList class="h-4 w-4" />
          </button>
          <button
            class="btn btn-sm join-item"
            class:btn-primary={layout === 'kanban'}
            onclick={() => (layout = 'kanban')}
            aria-label="Kanban layout"
          >
            <Columns3 class="h-4 w-4" />
          </button>
        </div>
      </div>
    {/snippet}
  </PageHeader>

  {#if loadError !== '' && filteredTasks().length === 0}
    <div class="alert alert-error text-sm" role="alert">
      <AlertTriangle class="h-4 w-4" />
      <span>{loadError}</span>
    </div>
  {:else if orchestratorStore.tasksLoading && filteredTasks().length === 0}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md text-primary"></span>
    </div>

  {:else if layout === 'table'}
    <div class="overflow-x-auto rounded-box border border-base-300/50">
      <table class="table table-sm table-zebra w-full">
        <thead>
          <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
            <th>Subject</th>
            <th>Project</th>
            <th>Status</th>
            <th>Attempts</th>
            <th>Assigned</th>
            <th>Job</th>
            <th>Updated</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {#if filteredTasks().length === 0}
            <tr>
              <td colspan="8">
                <EmptyState
                  icon={ClipboardList}
                  title={emptyTitle()}
                  description="Tasks represent units of work dispatched to your agent fleet."
                  actionLabel="New Task"
                  onaction={() => { createTaskOpen = true; }}
                />
              </td>
            </tr>
          {:else}
            {#each filteredTasks() as task (task.id)}
              {@const ref = issueRef(task)}
              {@const reason = blockedReason(task)}
              {@const jobID = latestTaskJobID(task)}
              {@const agentID = taskAgentID(task, knownJobs)}
              <tr
                class="cursor-pointer hover:bg-base-300/40 transition-colors"
                onclick={() => openTask(task)}
                onkeydown={(e) => { if (e.key === 'Enter') openTask(task); }}
                tabindex="0"
                role="button"
                aria-label="Open task {task.subject}"
              >
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
                    <span class="font-medium text-sm truncate">{task.subject}</span>
                  </div>
                  {#if task.description}
                    <div class="text-xs text-base-content/40 truncate">{task.description}</div>
                  {/if}
                </td>
                <td class="text-xs">{projectLabel(task.project_id) || '—'}</td>
                <td>
                  <StatusBadge text={statusLabel(task.status)} variant={statusToVariant(task.status)} />
                  {#if task.status === 'blocked'}
                    <div
                      class="flex items-center gap-1 mt-1 text-xs text-warning max-w-xs"
                      title={reason || NO_REASON_RECORDED}
                      data-testid="blocked-marker"
                    >
                      <AlertTriangle class="h-3 w-3 shrink-0" />
                      <span class="truncate">{reason || NO_REASON_RECORDED}</span>
                    </div>
                  {/if}
                </td>
                <td class="text-xs font-mono whitespace-nowrap">
                  {budgetLabel(task)}
                  {#if (task.environment_faults ?? 0) > 0}
                    <span class="text-base-content/40" title="Attempts refunded because the environment failed, not the work">
                      · {task.environment_faults} refunded
                    </span>
                  {/if}
                </td>
                <td class="text-xs text-base-content/60" data-testid="task-assigned">
                  {agentID || '—'}
                </td>
                <td class="text-xs" data-testid="task-job">
                  {#if jobID !== ''}
                    <a
                      class="link link-primary font-mono"
                      href={`#/jobs/${encodeURIComponent(jobID)}`}
                      onclick={(e) => e.stopPropagation()}
                      title="View job {jobID}"
                    >{jobID.slice(0, 8)}…</a>
                  {:else if task.auto_spawn && !taskHasDispatched(task)}
                    <span class="text-base-content/30 italic">no agent matched</span>
                  {:else}
                    <span class="text-base-content/25">—</span>
                  {/if}
                </td>
                <td class="text-xs text-base-content/50" title={formatDateTime(task.updated_at)}>{formatRelative(task.updated_at)}</td>
                <td>
                  <div class="flex items-center gap-1">
                    {#if task.status === 'pending'}
                      <button
                        class="btn btn-ghost btn-xs gap-1"
                        onclick={(e) => { void handleClaim(task.id, e); }}
                        disabled={claimingID === task.id}
                        aria-label="Claim task {task.subject}"
                        title="Claim"
                      >
                        {#if claimingID === task.id}
                          <span class="loading loading-spinner loading-xs"></span>
                        {:else}
                          <UserCheck class="h-3 w-3" />
                        {/if}
                      </button>
                    {/if}
                    {#if task.status === 'claimed' || task.status === 'in_progress'}
                      <button
                        class="btn btn-ghost btn-xs gap-1 text-success"
                        onclick={(e) => { void handleComplete(task.id, e); }}
                        disabled={completingID === task.id}
                        aria-label="Complete task {task.subject}"
                        title="Complete"
                      >
                        {#if completingID === task.id}
                          <span class="loading loading-spinner loading-xs"></span>
                        {:else}
                          <CheckCircle class="h-3 w-3" />
                        {/if}
                      </button>
                    {/if}
                    <button
                      class="btn btn-ghost btn-xs"
                      onclick={(e) => { void handleDecompose(task.id, e); }}
                      disabled={decomposingID === task.id}
                      aria-label="Decompose task {task.subject}"
                      title="Decompose"
                    >
                      {#if decomposingID === task.id}
                        <span class="loading loading-spinner loading-xs"></span>
                      {:else}
                        Decompose
                      {/if}
                    </button>
                  </div>
                </td>
              </tr>
            {/each}
          {/if}
        </tbody>
      </table>
    </div>

  {:else}
    <!-- Kanban board -->
    <div class="flex gap-4 overflow-x-auto pb-4">
      {#each COLUMNS as col (col)}
        {@const colTasks = tasksByStatus(col)}
        <div class="flex-none w-64">
          <div class="flex items-center gap-2 mb-3">
            <StatusBadge text={statusLabel(col)} variant={statusToVariant(col)} />
            <span class="text-xs text-base-content/40">({colTasks.length})</span>
          </div>
          <div class="space-y-2">
            {#each colTasks as task (task.id)}
              {@const agentID = taskAgentID(task, knownJobs)}
              <div
                class="card bg-base-200 rounded-box p-3 shadow-sm cursor-pointer hover:bg-base-300/40"
                onclick={() => openTask(task)}
                onkeydown={(e) => { if (e.key === 'Enter') openTask(task); }}
                tabindex="0"
                role="button"
                aria-label="Open task {task.subject}"
              >
                <div class="font-medium text-sm">{task.subject}</div>
                {#if task.project_id}
                  <div class="text-xs text-base-content/40 mt-1">{projectLabel(task.project_id)}</div>
                {/if}
                {#if agentID !== ''}
                  <div class="text-xs text-primary mt-1">{agentID}</div>
                {/if}
                {#if task.status === 'blocked'}
                  <div class="text-xs text-warning mt-1 truncate" title={blockedReason(task) || NO_REASON_RECORDED}>
                    {blockedReason(task) || NO_REASON_RECORDED}
                  </div>
                {/if}
                <div class="mt-2">
                  <button
                    class="btn btn-ghost btn-xs w-full"
                    onclick={(e) => { void handleDecompose(task.id, e); }}
                    disabled={decomposingID === task.id}
                  >
                    {#if decomposingID === task.id}
                      <span class="loading loading-spinner loading-xs"></span>
                    {:else}
                      Decompose
                    {/if}
                  </button>
                </div>
              </div>
            {:else}
              <div class="text-xs text-base-content/25 text-center py-4 border border-dashed border-base-300/30 rounded-box">
                Empty
              </div>
            {/each}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
