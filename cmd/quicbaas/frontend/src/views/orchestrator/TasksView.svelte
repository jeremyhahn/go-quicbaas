<!--
  TasksView renders the tenant's tasks — hand-created and adopted from the
  forge alike — with the generic Task model's provenance, attempt accounting
  and parked state, plus an optional kanban layout toggle.

  Columns: Subject (with the forge issue `#N` when the task came from one) ·
  Project · Status (a blocked task carries its reason) · Attempts
  (`charged / budget`, with refunds called out) · Assigned (the claimant, or
  the fleet node the latest attempt ran on) · Job (the latest attempt,
  linked) · Updated. A row opens the task's detail page.

  `#/tasks?tasks_status=pending,blocked&tasks_project=<id>` narrows the list,
  and is the link somebody pastes into an issue: every facet the page offers
  lives in the hash, so a narrowed view is a shareable address rather than a
  sequence of clicks to describe in prose.

  THE TABLE IS IN SERVER MODE and the kanban board is not. The table hands its
  loader {limit, offset, sort, q, filters} and the daemon answers one page with
  the count of everything that matched and the counts behind each facet; the
  status, dispatch, project and agent filters go with it as server parameters
  (`statuses`, `auto_spawn`, `project_id`, `agent_id`), because a filter
  applied to a page of twenty-five reports a total it cannot know. The board
  still renders the collection the store holds — a board has no pager to put a
  second page behind, and it shows whatever the store last read.

  The sortable columns are the ones the endpoint orders by (subject, project,
  status, updated). Attempts, Assigned and Job are not in that allow-list and
  do not claim to sort.
-->
<script lang="ts">
  import { orchestratorStore, refresh, type Task, type TaskStatus } from '../../stores/orchestrator.svelte.ts';
  import { routeTaskDetail } from '../../stores/router.svelte.ts';
  import {
    loadReferenceOptions,
    referenceOptions,
  } from '../../stores/referenceOptions.svelte.ts';
  import { taskFilters } from '../../lib/listFilters.ts';
  import { readFilters, selected, type FilterValues } from '../../lib/filters.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { statusToVariant } from '../../components/core/statusVariant.ts';
  import CreateTaskDialog from '../../components/orchestrator/CreateTaskDialog.svelte';
  import { LayoutList, Columns3, Plus, ClipboardList, UserCheck, CheckCircle, AlertTriangle } from 'lucide-svelte';
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
    taskStopPresentation,
    NO_REASON_RECORDED,
  } from '../../lib/tasks.ts';

  type Layout = 'table' | 'kanban';

  /** The permission the tasks read routes are guarded by (routes_permissions_generated.go). */
  const TASKS_READ_PERMISSION = 'orchestrator:status';

  let layout: Layout = $state('table');

  let createTaskOpen = $state(false);
  let decomposingID = $state('');
  let claimingID = $state('');
  let completingID = $state('');

  // The project column reads as a NAME rather than an id, and the project
  // picker offers the same list — one read, shared with every other page that
  // narrows by project. A project the read missed shows its id.
  $effect(() => {
    void loadReferenceOptions(orchestratorStore.activeTenantID);
  });

  const projectNames = $derived(
    Object.fromEntries(referenceOptions.projects.map((p) => [p.value, p.label])),
  );

  const FILTERS = $derived(
    taskFilters<Task>(referenceOptions.projects, referenceOptions.agents),
  );

  /**
   * chosen is the selection the table is asking with, read from the link this
   * page opened on and replaced on every load. The view needs it for two
   * answers the table cannot give: the empty state that names the project
   * nobody has tasks in, and the board, which narrows the store's collection
   * the same way the table narrows the server's.
   */
  let chosen = $state<FilterValues>(
    readFilters(
      typeof window === 'undefined' ? '' : window.location.hash,
      'tasks',
      taskFilters<Task>([], []),
    ),
  );

  // THE AGENT IS READ OFF THE TASK ROW. `assigned_to` is the claimant, and the
  // dispatcher's own path never writes one, so the fleet node an attempt ran
  // on is resolved by the SERVER from the task's latest attempt and carried on
  // every listed task as `agent_id` (see taskAgentID in lib/tasks.ts). Neither
  // the table nor the board needs a job listing of its own to name it.

  /** The project the operator narrowed to, or '' for every project. */
  const projectFilter = $derived(selected(chosen, 'project')[0] ?? '');

  function projectLabel(projectID: string | undefined): string {
    if (!projectID) return '';
    return projectNames[projectID] ?? projectID;
  }

  /**
   * filteredTasks is the BOARD's collection: the store's tasks, narrowed the
   * same way the table narrowed the server's page. The board cannot ask the
   * server — it has no pager to put a second page behind — so the same
   * selection is applied here to what the store holds.
   */
  function filteredTasks(): Task[] {
    const all = Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : [];
    const statuses = selected(chosen, 'status');
    const dispatch = selected(chosen, 'dispatch')[0] ?? '';
    const agentID = selected(chosen, 'node')[0] ?? '';
    return all.filter((task) => {
      if (projectFilter !== '' && task.project_id !== projectFilter) return false;
      if (statuses.length > 0 && !statuses.includes(task.status)) return false;
      if (dispatch !== '' && String(task.auto_spawn === true) !== dispatch) return false;
      if (agentID !== '' && taskAgentID(task) !== agentID) return false;
      return true;
    });
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
      value: (t) => taskAgentID(t),
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

  const reloadKey = $derived(`${orchestratorStore.activeTenantID}|${refreshTick}`);

  /**
   * loadTasksPage turns the table's faceted selection into the endpoint's own
   * parameters, and records it for the board and the empty state. Dispatch
   * travels as a string in the selection ('true'/'false') because a link
   * carries text; the API takes the boolean.
   */
  async function loadTasksPage(req: PageRequest): Promise<unknown> {
    chosen = req.filters ?? {};
    const statuses = selected(chosen, 'status') as TaskStatus[];
    const dispatch = selected(chosen, 'dispatch')[0] ?? '';
    const projectID = selected(chosen, 'project')[0] ?? '';
    const agentID = selected(chosen, 'node')[0] ?? '';
    try {
      const client = await getSDKClient();
      const page = await client.orchestrator.listTasksPage(orchestratorStore.activeTenantID, {
        limit: req.limit,
        offset: req.offset,
        sort: req.sort,
        q: req.q,
        ...(statuses.length === 0 ? {} : { statuses }),
        ...(dispatch === '' ? {} : { auto_spawn: dispatch === 'true' }),
        ...(projectID === '' ? {} : { project_id: projectID }),
        ...(agentID === '' ? {} : { agent_id: agentID }),
      });
      pageTotal = page.total;
      return page;
    } catch (err: unknown) {
      // A 403 names the permission the account lacks rather than reading as a
      // transport failure — the table renders whatever message it is given.
      throw new Error(loadErrorMessage(err, 'tasks', TASKS_READ_PERMISSION));
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

  /**
   * noMatchText names the project when one is selected, because "no tasks
   * match" over a project-narrowed list leaves the operator wondering which
   * of their four filters emptied it.
   *
   * It is the NO-MATCH slot and not the empty state: a tenant with tasks and
   * a filter that excludes all of them has not got "no tasks yet", and
   * inviting them to create their first one would be a claim about data the
   * filter is hiding.
   */
  const noMatchText = $derived(
    projectFilter === ''
      ? 'No tasks match the current filter.'
      : `No tasks in ${projectLabel(projectFilter)} match the current filter.`,
  );

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

<div class="page">
  <PageHeader title="Tasks" subtitle="{taskCount()} tasks" icon={ClipboardList}>
    {#snippet children()}
      <div class="flex items-center gap-2">
        <!--
          The narrowing lives in the table's own FilterBar now — status,
          dispatch, project and agent, each with the count behind it — rather
          than in three chips in the page header that could only ask two of
          those questions and said nothing about how many.
        -->
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
      filters={FILTERS}
      rowKey={(t: Task) => t.id}
      urlKey="tasks"
      ariaLabel="Tasks"
      searchPlaceholder="Search tasks..."
      searchLabel="Search tasks"
      {noMatchText}
    >
      {#snippet empty()}
        <EmptyState
          icon={ClipboardList}
          title="No tasks tracked — New Task is how you start."
          description="Tasks represent units of work dispatched to your fleet."
          actionLabel="New Task"
          onaction={() => { createTaskOpen = true; }}
        />
      {/snippet}

      {#snippet row(task: Task)}
        {@const ref = issueRef(task)}
        {@const reason = blockedReason(task)}
        {@const stop = taskStopPresentation(task)}
        {@const jobID = latestTaskJobID(task)}
        {@const agentID = taskAgentID(task)}
        <tr
          class="cursor-pointer transition-colors"
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
                    class="ref-chip"
                    href={ref.href}
                    target="_blank"
                    rel="noopener noreferrer"
                    onclick={(e) => e.stopPropagation()}
                    title="Open issue {ref.label} on the forge"
                  >{ref.label}</a>
                {:else}
                  <span class="ref-chip" title="Forge issue {ref.label}">{ref.label}</span>
                {/if}
              {/if}
              <span class="truncate font-medium">{task.subject}</span>
            </div>
            {#if task.description}
              <div class="mt-0.5 truncate text-xs text-base-content/60">{task.description}</div>
            {/if}
          </td>
          <!--
            Every id-like cell is a LINK to the thing it names: the project to
            its own tasks, the agent to its detail, the job to the attempt.
            Reading an id off a row and typing it into a search box is the
            drill-down this page used to make people do by hand.
          -->
          <td class="text-xs">
            {#if task.project_id}
              <a
                class="link link-hover"
                href={`#/tasks?tasks_project=${encodeURIComponent(task.project_id)}`}
                onclick={(e) => e.stopPropagation()}
                title="Show this project's tasks"
              >{projectLabel(task.project_id)}</a>
            {:else}
              <span class="text-base-content/25">—</span>
            {/if}
          </td>
          <td>
            <StatusBadge text={statusLabel(task.status)} variant={statusToVariant(task.status)} />
            <!--
              WHY IT STOPPED, from the server's typed fields: an environment
              fault, a failure of the work, a lost node, and a park on the
              owner each read differently without opening the task
              (go-quicbaas#947).
            -->
            {#if stop}
              <div class="mt-1" title={stop.detail} data-testid="task-stop">
                <StatusBadge text={stop.label} variant={stop.variant} />
              </div>
            {/if}
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
          <td class="whitespace-nowrap tabular-nums">
            {budgetLabel(task)}
            {#if (task.environment_faults ?? 0) > 0}
              <span class="text-base-content/40" title="Attempts refunded because the environment failed, not the work">
                · {task.environment_faults} refunded
              </span>
            {/if}
          </td>
          <td class="text-xs text-base-content/60" data-testid="task-assigned">
            {#if agentID !== ''}
              <a
                class="link link-hover"
                href={`#/agents/${encodeURIComponent(agentID)}`}
                onclick={(e) => e.stopPropagation()}
                title="Open agent {agentID}"
              >{agentID}</a>
            {:else}
              —
            {/if}
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
              <span class="italic text-base-content/60">no agent matched</span>
            {:else}
              <span class="text-base-content/25">—</span>
            {/if}
          </td>
          <td class="whitespace-nowrap text-xs text-base-content/60" title={formatDateTime(task.updated_at)}>{formatRelative(task.updated_at)}</td>
          <td>
            <div class="flex items-center justify-end gap-1">
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
                    <UserCheck class="h-3.5 w-3.5" />
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
                    <CheckCircle class="h-3.5 w-3.5" />
                  {/if}
                </button>
              {/if}
              <button
                class="btn btn-ghost btn-xs font-medium text-base-content/80"
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
    <div class="flex items-center justify-center gap-2 py-14 text-sm text-base-content/60">
      <span class="loading loading-spinner loading-sm text-primary"></span>
      Loading tasks…
    </div>

  {:else}
    <!-- Kanban board -->
    <div class="flex gap-4 overflow-x-auto pb-4">
      {#each COLUMNS as col (col)}
        {@const colTasks = tasksByStatus(col)}
        <div class="w-72 flex-none rounded-box border border-base-300/80 bg-base-200/60 p-2.5">
          <div class="mb-2.5 flex items-center justify-between gap-2 px-0.5">
            <StatusBadge text={statusLabel(col)} variant={statusToVariant(col)} />
            <span class="text-xs tabular-nums text-base-content/60">{colTasks.length}</span>
          </div>
          <div class="space-y-2">
            {#each colTasks as task (task.id)}
              {@const agentID = taskAgentID(task)}
              {@const stop = taskStopPresentation(task)}
              <div
                class="card cursor-pointer rounded-field border border-base-300/80 bg-base-100 p-3 transition-colors hover:border-primary/50"
                onclick={() => openTask(task)}
                onkeydown={(e) => { if (e.key === 'Enter') openTask(task); }}
                tabindex="0"
                role="button"
                aria-label="Open task {task.subject}"
              >
                <div class="font-medium text-sm">{task.subject}</div>
                {#if task.project_id}
                  <div class="mt-1 text-xs text-base-content/60">{projectLabel(task.project_id)}</div>
                {/if}
                {#if agentID !== ''}
                  <div class="mt-1 font-mono text-xs text-primary">{agentID}</div>
                {/if}
                {#if stop}
                  <div class="mt-1" title={stop.detail} data-testid="task-stop">
                    <StatusBadge text={stop.label} variant={stop.variant} />
                  </div>
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
              <div class="rounded-field border border-dashed border-base-300 py-5 text-center text-xs text-base-content/60">
                No tasks
              </div>
            {/each}
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
