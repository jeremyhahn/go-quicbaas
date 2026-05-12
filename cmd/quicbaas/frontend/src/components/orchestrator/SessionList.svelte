<!--
  SessionList renders a sortable, filterable table of sessions.
  Clicking a row navigates to the session detail via hash routing.
-->
<script lang="ts">
  import type { SessionStatusResponse } from '@quicbaas/sdk';
  import { sessionStore, refreshSessions } from '../../stores/sessionStore.svelte.ts';
  import { routeSessionDetail } from '../../stores/router.svelte.ts';
  import StatusBadge from '../core/StatusBadge.svelte';
  import PageHeader from '../core/PageHeader.svelte';
  import EmptyState from '../core/EmptyState.svelte';
  import { statusToVariant } from '../core/statusVariant.ts';
  import { formatDateTime } from '../../lib/time.ts';
  import { MessageSquare } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Sort and filter state
  // ---------------------------------------------------------------------------

  type SortField = 'created_at' | 'last_active' | 'status' | 'backend';
  type SortDir = 'asc' | 'desc';

  let sortField: SortField = $state('created_at');
  let sortDir: SortDir = $state('desc');
  let filterBackend = $state('');
  let filterStatus = $state('');

  // ---------------------------------------------------------------------------
  // Derived filtered + sorted list
  // ---------------------------------------------------------------------------

  const filteredSessions = $derived((): SessionStatusResponse[] => {
    let list = sessionStore.sessions;

    if (filterBackend !== '') {
      list = list.filter((s) =>
        s.backend.toLowerCase().includes(filterBackend.toLowerCase()),
      );
    }
    if (filterStatus !== '') {
      list = list.filter((s) => s.status === filterStatus);
    }

    return [...list].sort((a, b) => {
      let cmp = 0;
      if (sortField === 'created_at') {
        cmp = a.created_at - b.created_at;
      } else if (sortField === 'last_active') {
        cmp = a.last_active - b.last_active;
      } else if (sortField === 'status') {
        cmp = a.status.localeCompare(b.status);
      } else if (sortField === 'backend') {
        cmp = a.backend.localeCompare(b.backend);
      }
      return sortDir === 'asc' ? cmp : -cmp;
    });
  });

  // Unique backends and statuses for filter dropdowns.
  const uniqueBackends = $derived([
    ...new Set(sessionStore.sessions.map((s) => s.backend)),
  ].sort());

  const uniqueStatuses = $derived([
    ...new Set(sessionStore.sessions.map((s) => s.status)),
  ].sort());

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function toggleSort(field: SortField): void {
    if (sortField === field) {
      sortDir = sortDir === 'asc' ? 'desc' : 'asc';
    } else {
      sortField = field;
      sortDir = 'desc';
    }
  }

  function sortIndicator(field: SortField): string {
    if (sortField !== field) return '';
    return sortDir === 'asc' ? ' ↑' : ' ↓';
  }

  function statusVariant(s: string): 'success' | 'error' | 'warning' | 'info' | 'ghost' | 'neutral' | 'primary' {
    return statusToVariant(s);
  }

  // Daemon emits created_at/last_active in mixed units (seconds | ms | ns)
  // depending on the surface — the shared helper auto-detects.
  const formatTime = (ts: number): string => formatDateTime(ts);

  let refreshing = $state(false);
  async function handleRefresh(): Promise<void> {
    refreshing = true;
    await refreshSessions();
    refreshing = false;
  }

  // Spawn dialog visibility — hoisted so the toolbar button can open it.
  let spawnOpen = $state(false);
</script>

<!-- SpawnDialog rendered at the top level so it portals above the table. -->
{#await import('../orchestrator/SpawnDialog.svelte') then { default: SpawnDialogCmp }}
  <SpawnDialogCmp open={spawnOpen} onClose={() => { spawnOpen = false; }} />
{/await}

<div class="flex flex-col gap-3 p-6 h-full">
  <!-- Page header — matches the standard pattern used across every other view. -->
  <PageHeader
    title="Sessions"
    subtitle="Conversational sessions across persistent backends"
    icon={MessageSquare}
  >
    {#snippet children()}
      <button
        class="btn btn-ghost btn-sm"
        onclick={() => { void handleRefresh(); }}
        disabled={refreshing || sessionStore.sessionsLoading}
        aria-label="Refresh sessions"
      >
        {#if refreshing || sessionStore.sessionsLoading}
          <span class="loading loading-spinner loading-xs"></span>
        {:else}
          &#x21BB;
        {/if}
      </button>
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { spawnOpen = true; }}
        aria-label="Spawn new session"
      >
        + Spawn
      </button>
    {/snippet}
  </PageHeader>

  <!-- Toolbar -->
  <div class="flex items-center gap-2 flex-wrap">
    <div class="flex-1 text-base-content/60 text-sm">
      {filteredSessions().length} of {sessionStore.sessions.length} sessions
    </div>

    <!-- Backend filter -->
    <select
      class="select select-bordered select-xs"
      bind:value={filterBackend}
      aria-label="Filter by backend"
    >
      <option value="">All backends</option>
      {#each uniqueBackends as backend}
        <option value={backend}>{backend}</option>
      {/each}
    </select>

    <!-- Status filter -->
    <select
      class="select select-bordered select-xs"
      bind:value={filterStatus}
      aria-label="Filter by status"
    >
      <option value="">All statuses</option>
      {#each uniqueStatuses as status}
        <option value={status}>{status}</option>
      {/each}
    </select>
  </div>

  <!-- Table -->
  <div class="overflow-x-auto rounded-box border border-base-300/50 flex-1">
    <table class="table table-sm table-zebra w-full">
      <thead>
        <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
          <th>Session ID</th>
          <th>
            <button
              class="btn btn-xs btn-ghost font-semibold uppercase tracking-wider"
              onclick={() => toggleSort('backend')}
            >
              Backend{sortIndicator('backend')}
            </button>
          </th>
          <th>
            <button
              class="btn btn-xs btn-ghost font-semibold uppercase tracking-wider"
              onclick={() => toggleSort('status')}
            >
              Status{sortIndicator('status')}
            </button>
          </th>
          <th>Container</th>
          <th>
            <button
              class="btn btn-xs btn-ghost font-semibold uppercase tracking-wider"
              onclick={() => toggleSort('created_at')}
            >
              Created{sortIndicator('created_at')}
            </button>
          </th>
          <th>
            <button
              class="btn btn-xs btn-ghost font-semibold uppercase tracking-wider"
              onclick={() => toggleSort('last_active')}
            >
              Last Active{sortIndicator('last_active')}
            </button>
          </th>
        </tr>
      </thead>
      <tbody>
        {#if sessionStore.sessionsLoading && sessionStore.sessions.length === 0}
          <tr>
            <td colspan="6" class="text-center py-8">
              <span class="loading loading-spinner loading-sm text-primary"></span>
            </td>
          </tr>
        {:else if filteredSessions().length === 0}
          <tr>
            <td colspan="6">
              {#if sessionStore.sessions.length === 0}
                <EmptyState
                  icon={MessageSquare}
                  title="No sessions yet — sessions are how you converse with persistent backends."
                  description="Spawn a session against an ACP backend like Claude or Codex and chat across many turns. Stateless prompts run from Inference instead."
                  actionLabel="+ Spawn"
                  onaction={() => { spawnOpen = true; }}
                />
              {:else}
                <div class="text-center py-8 text-base-content/30">
                  No sessions match the current filter.
                </div>
              {/if}
            </td>
          </tr>
        {:else}
          {#each filteredSessions() as session (session.session_id)}
            <tr
              class="hover:bg-primary/10 cursor-pointer transition-colors"
              onclick={() => routeSessionDetail(session.session_id)}
              onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') routeSessionDetail(session.session_id); }}
              tabindex="0"
              role="button"
              aria-label="Open session {session.session_id}"
            >
              <td class="font-mono text-xs">{session.session_id.slice(0, 14)}…</td>
              <td>
                <span class="badge badge-sm badge-primary">{session.backend}</span>
              </td>
              <td>
                <StatusBadge
                  text={session.status}
                  variant={statusVariant(session.status)}
                  pulse={session.status === 'active'}
                />
              </td>
              <td class="font-mono text-xs text-base-content/50">
                {session.container_id ? session.container_id.slice(0, 12) : '—'}
              </td>
              <td class="text-xs text-base-content/50">{formatTime(session.created_at)}</td>
              <td class="text-xs text-base-content/50">{formatTime(session.last_active)}</td>
            </tr>
          {/each}
        {/if}
      </tbody>
    </table>
  </div>
</div>
