<!--
  SessionList renders the tenant's sessions in the shared DataTable: paged,
  sorted and searched there rather than here.

  Each row carries the ROLE the session played — worker, reviewer, triage —
  beside the backend and model running it. A session record names no role, and
  a worker's transcript discussing "the review's findings" reads exactly like a
  reviewer's; the role comes from the runner of the job the session names (see
  sessionRole in lib/sessions.ts).

  Clicking a row navigates to the session detail via hash routing.
-->
<script lang="ts">
  import type { SessionStatusResponse } from '@quicbaas/sdk';
  import { sessionStore, refreshSessions } from '../../stores/sessionStore.svelte.ts';
  import { orchestratorStore, loadJobs } from '../../stores/orchestrator.svelte.ts';
  import { routeSessionDetail } from '../../stores/router.svelte.ts';
  import StatusBadge from '../core/StatusBadge.svelte';
  import PageHeader from '../core/PageHeader.svelte';
  import EmptyState from '../core/EmptyState.svelte';
  import DataTable from '../core/DataTable.svelte';
  import { statusToVariant } from '../core/statusVariant.ts';
  import { formatDateTime } from '../../lib/time.ts';
  import { sessionModel, sessionRole } from '../../lib/sessions.ts';
  import type { Column } from '../../lib/dataTable.ts';
  import { MessageSquare } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // The job records behind the Role column
  // ---------------------------------------------------------------------------

  // Read ONCE for the page rather than once per row, the way TasksView reads
  // them for its Assigned column. The store is stale-while-revalidate, so a
  // page that already has jobs pays nothing.
  $effect(() => {
    void orchestratorStore.activeTenantID;
    void loadJobs();
  });

  const knownJobs = $derived(
    Array.isArray(orchestratorStore.jobs) ? orchestratorStore.jobs : [],
  );

  function roleOf(session: SessionStatusResponse): string {
    return sessionRole(session, knownJobs);
  }

  // ---------------------------------------------------------------------------
  // Filters this page owns — the search, the sort and the paging are the
  // table's, and the hand-written versions of all three were deleted.
  // ---------------------------------------------------------------------------

  let filterBackend = $state('');
  let filterStatus = $state('');

  const filteredSessions = $derived.by((): SessionStatusResponse[] => {
    let list = sessionStore.sessions;
    if (filterBackend !== '') {
      list = list.filter((s) => s.backend.toLowerCase().includes(filterBackend.toLowerCase()));
    }
    if (filterStatus !== '') {
      list = list.filter((s) => s.status === filterStatus);
    }
    return list;
  });

  // Unique backends and statuses for filter dropdowns.
  const uniqueBackends = $derived([
    ...new Set(sessionStore.sessions.map((s) => s.backend)),
  ].sort());

  const uniqueStatuses = $derived([
    ...new Set(sessionStore.sessions.map((s) => s.status)),
  ].sort());

  const COLUMNS: Column<SessionStatusResponse>[] = [
    { key: 'session_id', label: 'Session ID', sortable: true, searchable: true },
    { key: 'role', label: 'Role', sortable: true, searchable: true, value: (s) => roleOf(s) },
    { key: 'backend', label: 'Backend', sortable: true, searchable: true },
    { key: 'status', label: 'Status', sortable: true },
    { key: 'container_id', label: 'Container' },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'last_active', label: 'Last Active', sortable: true },
  ];

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

  <DataTable
    columns={COLUMNS}
    rows={filteredSessions}
    rowKey={(s: SessionStatusResponse) => s.session_id}
    loading={sessionStore.sessionsLoading && sessionStore.sessions.length === 0}
    narrowed={filterBackend !== '' || filterStatus !== ''}
    urlKey="sessions"
    defaultSort="created_at:desc"
    ariaLabel="Sessions"
    searchPlaceholder="Search sessions..."
    searchLabel="Search sessions"
    noMatchText="No sessions match the current filter."
  >
    {#snippet toolbar()}
      <select
        class="select select-bordered select-sm"
        bind:value={filterBackend}
        aria-label="Filter by backend"
      >
        <option value="">All backends</option>
        {#each uniqueBackends as backend (backend)}
          <option value={backend}>{backend}</option>
        {/each}
      </select>

      <select
        class="select select-bordered select-sm"
        bind:value={filterStatus}
        aria-label="Filter by status"
      >
        <option value="">All statuses</option>
        {#each uniqueStatuses as status (status)}
          <option value={status}>{status}</option>
        {/each}
      </select>
    {/snippet}

    {#snippet empty()}
      <EmptyState
        icon={MessageSquare}
        title="No sessions yet — sessions are how you converse with persistent backends."
        description="Spawn a session against an ACP backend like Claude or Codex and chat across many turns. Stateless prompts run from Inference instead."
        actionLabel="+ Spawn"
        onaction={() => { spawnOpen = true; }}
      />
    {/snippet}

    {#snippet row(session: SessionStatusResponse)}
      {@const role = roleOf(session)}
      {@const model = sessionModel(session)}
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
          {#if role !== ''}
            <span
              class="badge badge-sm badge-outline"
              data-testid="session-role"
              title="The runner of this session's job decides its role"
            >{role}</span>
          {:else}
            <span class="text-base-content/25">—</span>
          {/if}
        </td>
        <td>
          <div class="flex items-center gap-1 flex-wrap">
            <span class="badge badge-sm badge-primary">{session.backend}</span>
            {#if model !== ''}
              <span class="font-mono text-xs text-base-content/50" data-testid="session-model">{model}</span>
            {/if}
          </div>
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
    {/snippet}
  </DataTable>
</div>
