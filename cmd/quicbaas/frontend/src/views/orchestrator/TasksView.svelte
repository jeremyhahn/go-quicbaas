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

  THE TABLE IS IN SERVER MODE and the kanban board is not. The table hands its
  loader {limit, offset, sort, q} and the daemon answers one page with the
  count of everything that matched; the project and dispatch-mode filters go
  with it as server parameters (`project_id`, `auto_spawn`), because a filter
  applied to a page of twenty-five reports a total it cannot know. The board
  still renders the collection the store holds — a board has no pager to put a
  second page behind, and it shows whatever the store last read.

  The sortable columns are the ones the endpoint orders by (subject, project,
  status, updated). Attempts, Assigned and Job are not in that allow-list and
  do not claim to sort.
-->
<script lang="ts">
  import { orchestratorStore, loadJobs, refresh, type JobStatus, type Task, type TaskStatus } from '../../stores/orchestrator.svelte.ts';
  import { routerStore, routeTaskDetail, routeTasks } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { statusToVariant } from '../../components/core/statusVariant.ts';
  import CreateTaskDialog from '../../components/orchestrator/CreateTaskDialog.svelte';
  import { LayoutList, Columns3, Plus, ClipboardList, UserCheck, CheckCircle, Zap, AlertTriangle, X } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column, PageRequest } from '../../lib/dataTable.ts';
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
  //
  // The jobs are read ONCE PER PAGE, for exactly the jobs this page's rows
  // name (`job_ids`), rather than once per row or by listing the tenant. The
  // tenant's job list used to serve this, and cannot any more: it is a page
  // itself now, so the Assigned column would blank for every task whose
  // attempt is not among the twenty-five jobs the store happens to hold.
  let pageJobs = $state<JobStatus[]>([]);

  // The BOARD is a different question. It renders the collection the store
  // holds, not a page, so it names its agents from the store's jobs — read
  // only while the board is the layout in front of the operator.
  $effect(() => {
    if (layout !== 'kanban') return;
    void orchestratorStore.activeTenantID;
    void loadJobs();
  });

  const boardJobs = $derived(Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : []);

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

  /**
   * What the table's headers say and which of them sort. The keys are the
   * SERVER's sort fields (docs/software-factory/api.md); a column the endpoint
   * does not order by is not marked sortable, because sorting the twenty-five
   * rows in hand would look exactly like sorting the collection.
   *
   * The kanban board below uses the store's collection and none of this: a
   * board has no columns to sort.
   */
  const TABLE_COLUMNS: Column<Task>[] = [
    { key: 'subject', label: 'Subject', sortable: true, searchable: true },
    {
      key: 'project_id',
      label: 'Project',
      sortable: true,
      searchable: true,
      value: (t) => projectLabel(t.project_id),
    },
    { key: 'status', label: 'Status', sortable: true },
    { key: 'attempts', label: 'Attempts', value: (t) => t.attempts ?? 0 },
    {
      key: 'assigned_to',
      label: 'Assigned',
      value: (t) => taskAgentID(t, pageJobs),
    },
    { key: 'job', label: 'Job', value: (t) => latestTaskJobID(t) },
    { key: 'updated_at', label: 'Updated', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  // ---------------------------------------------------------------------------
  // The page loader
  // ---------------------------------------------------------------------------

  /** How many tasks the server said match, for the header count. */
  let pageTotal = $state(0);

  /** refreshTick re-asks the server after an action changes a task. */
  let refreshTick = $state(0);

  const reloadKey = $derived(
    `${orchestratorStore.activeTenantID}|${projectFilter}|${spawnFilter}|${refreshTick}`,
  );

  /** dispatchMode is the auto_spawn value the SERVER narrows on. */
  function dispatchMode(): boolean | undefined {
    if (spawnFilter === 'auto_spawned') return true;
    if (spawnFilter === 'queued') return false;
    return undefined;
  }

  async function loadTasksPage(req: PageRequest): Promise<unknown> {
    const mode = dispatchMode();
    try {
      const client = await getSDKClient();
      const page = await client.orchestrator.listTasksPage(orchestratorStore.activeTenantID, {
        limit: req.limit,
        offset: req.offset,
        sort: req.sort,
        q: req.q,
        ...(projectFilter === '' ? {} : { project_id: projectFilter }),
        ...(mode === undefined ? {} : { auto_spawn: mode }),
      });
      pageTotal = page.total;
      await loadPageJobs(page.items as unknown as Task[]);
      return page;
    } catch (err: unknown) {
      // A 403 names the permission the account lacks rather than reading as a
      // transport failure — the table renders whatever message it is given.
      throw new Error(loadErrorMessage(err, 'tasks', TASKS_READ_PERMISSION));
    }
  }

  /**
   * loadPageJobs reads the jobs this page's rows name — ONE request for the
   * whole page, narrowed to those ids. A page whose rows have never dispatched
   * asks for nothing at all.
   */
  async function loadPageJobs(tasks: readonly Task[]): Promise<void> {
    const ids = [...new Set(tasks.map((t) => latestTaskJobID(t)).filter((id) => id !== ''))];
    if (ids.length === 0) {
      pageJobs = [];
      return;
    }
    try {
      const client = await getSDKClient();
      const page = await client.orchestrator.listJobsPage(orchestratorStore.activeTenantID, {
        job_ids: ids,
        limit: ids.length,
      });
      pageJobs = page.items as unknown as JobStatus[];
    } catch {
      // The Assigned column falls back to the claimant; the tasks still render.
      pageJobs = [];
    }
  }

  const COLUMNS: TaskStatus[] = ['pending', 'claimed', 'in_progress', 'blocked', 'completed', 'failed'];

  function tasksByStatus(status: TaskStatus) {
    return filteredTasks().filter((t) => t.status === status);
  }

  function statusLabel(status: TaskStatus): string {
    return status.replace('_', ' ');
  }

  /**
   * refreshTasks re-reads BOTH halves after an action: the table's page,
   * because it is the server's answer and not this store's collection any
   * more, and the store, because the kanban board and the task detail read it.
   */
  function refreshTasks(): void {
    refreshTick += 1;
    void refresh('tasks');
  }

  /**
   * taskCount is how many tasks the header claims. In table mode that is the
   * SERVER's total — the count of everything that matched, not the size of the
   * page; the board, which shows what the store holds, counts that.
   */
  function taskCount(): number {
    return layout === 'table' ? pageTotal : filteredTasks().length;
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
      refreshTasks();
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
      refreshTasks();
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
      refreshTasks();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Complete failed: ${msg}`);
    } finally {
      completingID = '';
    }
  }
</script>

<!-- Create Task dialog -->
<CreateTaskDialog bind:open={createTaskOpen} onCreated={() => { refreshTasks(); }} />

<div class="p-6 space-y-6">
  <PageHeader title="Tasks" subtitle="{taskCount()} tasks" icon={ClipboardList}>
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

  <!--
    THE TWO LAYOUTS ANSWER FOR THEMSELVES. The table is server-paged: it owns
    its own spinner and its own error, and a failure of ITS read is the one it
    reports (the loader formats a 403 into the permission it names). The board
    reads the store, so the store's loading and error states are the board's.
  -->
  {#if layout === 'table'}
    <DataTable
      columns={TABLE_COLUMNS}
      load={loadTasksPage}
      itemsKey="items"
      {reloadKey}
      rowKey={(t: Task) => t.id}
      narrowed={spawnFilter !== 'all'}
      urlKey="tasks"
      ariaLabel="Tasks"
      searchPlaceholder="Search tasks..."
      searchLabel="Search tasks"
      noMatchText="No tasks match the current filter."
    >
      {#snippet empty()}
        <EmptyState
          icon={ClipboardList}
          title={emptyTitle()}
          description="Tasks represent units of work dispatched to your agent fleet."
          actionLabel="New Task"
          onaction={() => { createTaskOpen = true; }}
        />
      {/snippet}

      {#snippet row(task: Task)}
        {@const ref = issueRef(task)}
        {@const reason = blockedReason(task)}
        {@const jobID = latestTaskJobID(task)}
        {@const agentID = taskAgentID(task, pageJobs)}
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
      {/snippet}
    </DataTable>

  {:else if loadError !== '' && filteredTasks().length === 0}
    <div class="alert alert-error text-sm" role="alert">
      <AlertTriangle class="h-4 w-4" />
      <span>{loadError}</span>
    </div>
  {:else if orchestratorStore.tasksLoading && filteredTasks().length === 0}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md text-primary"></span>
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
              {@const agentID = taskAgentID(task, boardJobs)}
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
