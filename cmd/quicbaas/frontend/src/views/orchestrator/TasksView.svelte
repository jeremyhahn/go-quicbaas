<!--
  TasksView renders orchestration tasks with status badges and an optional
  kanban layout toggle.
-->
<script lang="ts">
  import { orchestratorStore, type TaskStatus } from '../../stores/orchestrator.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { LayoutList, Columns3 } from 'lucide-svelte';

  type Layout = 'table' | 'kanban';
  let layout: Layout = $state('table');

  const COLUMNS: TaskStatus[] = ['pending', 'claimed', 'in_progress', 'completed', 'failed'];

  function tasksByStatus(status: TaskStatus) {
    return orchestratorStore.tasks.filter((t) => t.status === status);
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

  function formatTime(ts: number): string {
    if (!ts) return '—';
    return new Date(ts * 1000).toLocaleString();
  }
</script>

<div class="p-6 space-y-4">
  <PageHeader title="Tasks" subtitle="{orchestratorStore.tasks.length} total tasks">
    {#snippet children()}
      <div class="join">
        <button
          class="btn btn-sm join-item"
          class:btn-primary={layout === 'table'}
          onclick={() => (layout = 'table')}
        >
          <LayoutList class="h-4 w-4" />
        </button>
        <button
          class="btn btn-sm join-item"
          class:btn-primary={layout === 'kanban'}
          onclick={() => (layout = 'kanban')}
        >
          <Columns3 class="h-4 w-4" />
        </button>
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
          <tr class="bg-base-200 text-base-content/60">
            <th>Subject</th>
            <th>Team</th>
            <th>Status</th>
            <th>Assigned To</th>
            <th>Created</th>
            <th>Updated</th>
          </tr>
        </thead>
        <tbody>
          {#if orchestratorStore.tasks.length === 0}
            <tr>
              <td colspan="6" class="text-center py-8 text-base-content/30">No tasks</td>
            </tr>
          {:else}
            {#each orchestratorStore.tasks as task (task.id)}
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
                <td class="text-xs text-base-content/50">{formatTime(task.created_at)}</td>
                <td class="text-xs text-base-content/50">{formatTime(task.updated_at)}</td>
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
