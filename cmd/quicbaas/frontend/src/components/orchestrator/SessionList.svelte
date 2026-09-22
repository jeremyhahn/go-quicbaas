<!--
  SessionList renders the tenant's sessions in the shared DataTable, in SERVER
  MODE: the table hands its loader {limit, offset, sort, q} and the daemon
  answers one page with the count of everything that matched. The backend and
  status selectors are server parameters too — a filter applied to a page of
  twenty-five would report a total it cannot know.

  The sort column names are the server's allow-list, which is why Status sorts
  on `state` and Last Active on `last_activity`: those are the fields the
  endpoint orders by (docs/software-factory/api.md). Container has no server
  sort and is therefore not sortable, rather than sorting one page locally and
  looking like it sorted the collection.

  Each row carries the ROLE the session played — worker, reviewer, triage —
  beside the backend and model running it. The row says so itself: the listing
  resolves the runner of the session's job server-side, per page.

  Clicking a row navigates to the session detail via hash routing.
-->
<script lang="ts">
  import type { SessionState, SessionStatusResponse } from '@quicbaas/sdk';
  import { sessionStore, refreshSessions } from '../../stores/sessionStore.svelte.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { routeSessionDetail } from '../../stores/router.svelte.ts';
  import StatusBadge from '../core/StatusBadge.svelte';
  import PageHeader from '../core/PageHeader.svelte';
  import EmptyState from '../core/EmptyState.svelte';
  import DataTable from '../core/DataTable.svelte';
  import { statusToVariant } from '../core/statusVariant.ts';
  import { formatDateTime } from '../../lib/time.ts';
  import { sessionModel, sessionRole } from '../../lib/sessions.ts';
  import { sessionFilters } from '../../lib/listFilters.ts';
  import { selected, type FilterValues } from '../../lib/filters.ts';
  import {
    loadReferenceOptions,
    referenceOptions,
  } from '../../stores/referenceOptions.svelte.ts';
  import type { Column, PageRequest } from '../../lib/dataTable.ts';
  import { MessageSquare } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // The role a row played
  //
  // The listing resolves it: each session names its job and the daemon reads
  // that job's runner for the page it is answering. Nothing to look up here,
  // and nothing that could go blank because the job fell off a page.
  // ---------------------------------------------------------------------------

  function roleOf(session: SessionStatusResponse): string {
    return sessionRole(session);
  }

  // ---------------------------------------------------------------------------
  // Filters this page owns — sent to the server, not applied to a page
  //
  // The vocabulary is FIXED rather than derived from the rows: a control built
  // from the loaded records could only ever offer what is on the current page,
  // so filtering to "codex" would be impossible on a page with no codex
  // session. The backends come from the daemon's registry, the states from the
  // lifecycle the service defines (pkg/orchestration/backend.go), and the
  // COUNTS beside each value come from the answer's own facets.
  // ---------------------------------------------------------------------------

  $effect(() => {
    void loadReferenceOptions(orchestratorStore.activeTenantID);
  });

  const FILTERS = $derived(
    sessionFilters<SessionStatusResponse>(
      referenceOptions.backends,
      referenceOptions.projects,
      referenceOptions.agents,
    ),
  );

  // ---------------------------------------------------------------------------
  // The page loader
  //
  // `reloadKey` carries the tenant and the two filters, so a change to any of
  // them re-asks the server instead of re-cutting rows the table is holding.
  // ---------------------------------------------------------------------------

  // refreshTick is how the Refresh button re-asks: the table re-reads when the
  // reload key changes, so a manual refresh is a change to it.
  let refreshTick = $state(0);

  const reloadKey = $derived(`${orchestratorStore.activeTenantID}|${refreshTick}`);

  /** loadSessions turns the faceted selection into the endpoint's parameters. */
  async function loadSessions(req: PageRequest): Promise<unknown> {
    const client = await getSDKClient();
    const chosen: FilterValues = req.filters ?? {};
    const status = (selected(chosen, 'status')[0] ?? '') as SessionState | '';
    const backend = selected(chosen, 'backend')[0] ?? '';
    const runner = selected(chosen, 'runner')[0] ?? '';
    const projectID = selected(chosen, 'project')[0] ?? '';
    const agentID = selected(chosen, 'agent')[0] ?? '';
    return client.orchestrator.listSessionsPage(orchestratorStore.activeTenantID, {
      limit: req.limit,
      offset: req.offset,
      sort: req.sort,
      q: req.q,
      ...(backend === '' ? {} : { backend }),
      ...(status === '' ? {} : { status }),
      ...(runner === '' ? {} : { runner }),
      ...(projectID === '' ? {} : { project_id: projectID }),
      ...(agentID === '' ? {} : { agent_id: agentID }),
    });
  }

  // The keys are the SERVER'S sort fields, not the cell labels: `state` and
  // `last_activity` are what the sessions endpoint orders by. Role and
  // Container are not in that allow-list, so they do not claim to sort.
  const COLUMNS: Column<SessionStatusResponse>[] = [
    { key: 'session_id', label: 'Session ID', sortable: true, searchable: true },
    { key: 'role', label: 'Role', value: (s) => roleOf(s) },
    { key: 'backend', label: 'Backend', sortable: true, searchable: true },
    { key: 'state', label: 'Status', sortable: true, value: (s) => s.status },
    { key: 'container_id', label: 'Container' },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'last_activity', label: 'Last Active', sortable: true, value: (s) => s.last_active },
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
    // The table re-reads its page; the store is refreshed too, because the
    // dashboard's live-session count and the session detail route still read
    // from it.
    refreshTick += 1;
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
    load={loadSessions}
    itemsKey="items"
    {reloadKey}
    filters={FILTERS}
    rowKey={(s: SessionStatusResponse) => s.session_id}
    urlKey="sessions"
    defaultSort="created_at:desc"
    ariaLabel="Sessions"
    searchPlaceholder="Search sessions..."
    searchLabel="Search sessions"
    noMatchText="No sessions match the current filter."
  >
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
        <!--
          The id is a LINK as well as a clickable row: an operator pasting a
          session into an issue, or opening three of them in tabs, cannot do
          either with a row that only answers to a click.
        -->
        <td class="font-mono text-xs">
          <a
            class="link link-hover"
            href={`#/sessions/${encodeURIComponent(session.session_id)}`}
            onclick={(e) => e.stopPropagation()}
            title={session.session_id}
          >{session.session_id.slice(0, 14)}…</a>
        </td>
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
