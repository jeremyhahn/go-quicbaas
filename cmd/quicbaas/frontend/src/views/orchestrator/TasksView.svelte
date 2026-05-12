<!--
  TasksView renders orchestration tasks with status badges and an optional
  kanban layout toggle.
  Header provides a "+ New Task" button that opens CreateTaskDialog.
  Each row has a "Decompose" action that calls decomposeTask and shows a toast.
-->
<script lang="ts">
  import { orchestratorStore, refresh, type TaskStatus } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import CreateTaskDialog from '../../components/orchestrator/CreateTaskDialog.svelte';
  import { LayoutList, Columns3, Plus, ClipboardList, UserCheck, CheckCircle, Zap } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type { DecomposeTaskRequest, ClaimTaskRequest, CompleteTaskRequest, Task } from '@quicbaas/sdk';
  import { formatDateTime } from '../../lib/time.ts';

  type Layout = 'table' | 'kanban';
  type SpawnFilter = 'all' | 'auto_spawned' | 'queued';

  let layout: Layout = $state('table');
  let spawnFilter: SpawnFilter = $state('all');

  let createTaskOpen = $state(false);
  let decomposingID = $state('');
  let claimingID = $state('');
  let completingID = $state('');

  function filteredTasks(): Task[] {
    const all = Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : [];
    if (spawnFilter === 'auto_spawned') return all.filter((t) => t.auto_spawn);
    if (spawnFilter === 'queued') return all.filter((t) => !t.auto_spawn);
    return all;
  }

  const COLUMNS: TaskStatus[] = ['pending', 'claimed', 'in_progress', 'completed', 'failed'];

  function tasksByStatus(status: TaskStatus) {
    return (Array.isArray(orchestratorStore.tasks) ? orchestratorStore.tasks : []).filter((t) => t.status === status);
  }

  function statusVariant(status: TaskStatus): 'info' | 'warning' | 'primary' | 'success' | 'error' | 'ghost' {
    if (status === 'completed') return 'success';
    if (status === 'failed') return 'error';
    if (status === 'in_progress') return 'primary';
    if (status === 'claimed') return 'warning';
    if (status === 'cancelled') return 'ghost';
    return 'info';
  }

  function statusLabel(status: TaskStatus): string {
    return status.replace('_', ' ');
  }

  const formatTime = (ts: number): string => formatDateTime(ts);

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

  {#if orchestratorStore.tasksLoading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md text-primary"></span>
    </div>

  {:else if layout === 'table'}
    <div class="overflow-x-auto rounded-box border border-base-300/50">
      <table class="table table-sm table-zebra w-full">
        <thead>
          <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
            <th>Subject</th>
            <th>Team</th>
            <th>Status</th>
            <th>Assigned To</th>
            <th>Spawned Job</th>
            <th>Created</th>
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
                  title="No tasks tracked — New Task is how you start."
                  description="Tasks represent units of work dispatched to your agent fleet."
                  actionLabel="New Task"
                  onaction={() => { createTaskOpen = true; }}
                />
              </td>
            </tr>
          {:else}
            {#each filteredTasks() as task (task.id)}
              <tr class="hover:bg-base-300/40">
                <td class="max-w-xs">
                  <div class="font-medium text-sm">{task.subject}</div>
                  {#if task.description}
                    <div class="text-xs text-base-content/40 truncate">{task.description}</div>
                  {/if}
                </td>
                <td class="text-xs">{task.team_name || '—'}</td>
                <td>
                  <StatusBadge text={statusLabel(task.status)} variant={statusVariant(task.status)} />
                </td>
                <td class="text-xs text-base-content/60">{task.assigned_to || '—'}</td>
                <td class="text-xs">
                  {#if task.spawned_job_id}
                    <a
                      class="link link-primary font-mono"
                      href={`#/orchestrator/jobs/${task.spawned_job_id}`}
                      title="View spawned job {task.spawned_job_id}"
                    >{task.spawned_job_id.slice(0, 8)}…</a>
                  {:else if task.auto_spawn}
                    <span class="text-base-content/30 italic">no agent matched</span>
                  {:else}
                    <span class="text-base-content/25">—</span>
                  {/if}
                </td>
                <td class="text-xs text-base-content/50">{formatTime(task.created_at)}</td>
                <td class="text-xs text-base-content/50">{formatTime(task.updated_at)}</td>
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
      {#each COLUMNS as col}
        {@const colTasks = tasksByStatus(col)}
        <div class="flex-none w-64">
          <div class="flex items-center gap-2 mb-3">
            <StatusBadge text={statusLabel(col)} variant={statusVariant(col)} />
            <span class="text-xs text-base-content/40">({colTasks.length})</span>
          </div>
          <div class="space-y-2">
            {#each colTasks as task (task.id)}
              <div class="card bg-base-200 rounded-box p-3 shadow-sm">
                <div class="font-medium text-sm">{task.subject}</div>
                {#if task.team_name}
                  <div class="text-xs text-base-content/40 mt-1">{task.team_name}</div>
                {/if}
                {#if task.assigned_to}
                  <div class="text-xs text-primary mt-1">{task.assigned_to}</div>
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
