<!--
  SessionsView renders a table of active agent sessions with state badges.
-->
<script lang="ts">
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';

  function stateVariant(state: string): 'success' | 'error' | 'warning' | 'info' | 'ghost' {
    if (state === 'active' || state === 'running') return 'success';
    if (state === 'closed' || state === 'terminated') return 'ghost';
    if (state === 'error') return 'error';
    return 'info';
  }

  function formatTime(ts: number): string {
    if (!ts) return '—';
    return new Date(ts * 1000).toLocaleString();
  }
</script>

<div class="p-6 space-y-4">
  <PageHeader
    title="Sessions"
    subtitle="{orchestratorStore.sessions.length} active sessions"
  />

  <div class="overflow-x-auto rounded-box border border-base-300/50">
    <table class="table table-sm table-zebra w-full">
      <thead>
        <tr class="bg-base-200 text-base-content/60">
          <th>Session ID</th>
          <th>Project</th>
          <th>Backend</th>
          <th>Model</th>
          <th>State</th>
          <th>Created</th>
          <th>Last Activity</th>
        </tr>
      </thead>
      <tbody>
        {#if orchestratorStore.sessionsLoading}
          <tr>
            <td colspan="7" class="text-center py-8">
              <span class="loading loading-spinner loading-sm text-primary"></span>
            </td>
          </tr>
        {:else if orchestratorStore.sessions.length === 0}
          <tr>
            <td colspan="7" class="text-center py-8 text-base-content/30">
              No active sessions
            </td>
          </tr>
        {:else}
          {#each orchestratorStore.sessions as session (session.session_id)}
            <tr class="hover:bg-base-300/40">
              <td class="font-mono text-xs">{session.session_id.slice(0, 12)}...</td>
              <td class="font-mono text-xs text-base-content/60">{session.project_id || '—'}</td>
              <td><span class="badge badge-sm badge-primary">{session.backend}</span></td>
              <td class="text-xs">{session.model || '—'}</td>
              <td>
                <StatusBadge
                  text={session.state}
                  variant={stateVariant(session.state)}
                  pulse={session.state === 'active'}
                />
              </td>
              <td class="text-xs text-base-content/50">{formatTime(session.created_at)}</td>
              <td class="text-xs text-base-content/50">{formatTime(session.last_activity)}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>
