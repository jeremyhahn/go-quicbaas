<!--
  AgentFleet renders a grid of agent daemons with live status.
  Header provides:
    - "+ Register Agent" button (left) — opens RegisterAgentDialog
    - "Deploy Remote Agent" area (right) — owned by the other agent, rendered
      via a slot so that agent can inject its button without touching this file.
  Each card has a "Remove" action that confirms before deregistering the agent.
-->
<script lang="ts">
  import { agentStore, refreshAgents } from '../../stores/agentStore.svelte.ts';
  import { routeAgentDetail } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import RegisterAgentDialog from './RegisterAgentDialog.svelte';
  import DeployRemoteAgentDialog from './DeployRemoteAgentDialog.svelte';
  import type { AgentRegistration } from '@quicbaas/sdk';
  import { UserPlus, Cloud, Server } from 'lucide-svelte';
  import PageHeader from '../core/PageHeader.svelte';
  import FilterBar from '../core/FilterBar.svelte';
  import { computeFacets, filterByFacets } from '../../lib/filters.ts';
  import { FilterState } from '../../lib/filterState.svelte.ts';
  import { agentFilters } from '../../lib/listFilters.ts';
  import type { RemoveAgentRequest } from '@quicbaas/sdk';

  let {
    // deploySlot allows the Deploy Remote Agent button (other agent's work) to
    // be injected in the header without modifying this component's internals.
    deploySlot,
  }: {
    deploySlot?: import('svelte').Snippet;
  } = $props();

  // ---------------------------------------------------------------------------
  // Relative time helper
  // ---------------------------------------------------------------------------

  // The orchestrator/agents API returns `last_seen` as Unix nanoseconds
  // (Go time.Now().UnixNano()), not seconds. Detect the magnitude so the
  // helper is robust to either shape — values > 1e12 are ns or ms.
  function relativeTime(ts: number): string {
    if (!ts) return '—';
    let secs = ts;
    if (ts > 1e15) secs = ts / 1e9;       // nanoseconds
    else if (ts > 1e12) secs = ts / 1e3;  // milliseconds
    const diff = Math.max(0, Date.now() / 1000 - secs);
    if (diff < 60) return `${Math.floor(diff)}s ago`;
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
    return `${Math.floor(diff / 86400)}d ago`;
  }

  function statusVariant(status: string): string {
    if (status === 'running' || status === 'idle') return 'badge-success';
    if (status === 'error') return 'badge-error';
    if (status === 'offline') return 'badge-ghost';
    return 'badge-info';
  }

  function cardBorder(status: string): string {
    if (status === 'running' || status === 'idle') return 'border-success/20';
    if (status === 'error') return 'border-error/20';
    return 'border-base-300/50';
  }

  // Unique backends across all agents.
  const uniqueBackends = $derived(
    (a: AgentRegistration): string => a.backend,
  );

  // ---------------------------------------------------------------------------
  // Finding one agent in a fleet
  //
  // The fleet is a card GRID and not a table, which is the right shape for
  // what a card shows and the wrong shape for "which three are offline". The
  // search and the facets are therefore the same controls every list in this
  // app has — FilterBar, the same URL grammar (`#/fleet?fleet_status=offline`)
  // — over the collection the store holds rather than over a server page: the
  // fleet listing is read whole, so the counts are exact and nothing is
  // hiding on a page two that does not exist.
  // ---------------------------------------------------------------------------

  /** The facet fields, and how each reads its value off an agent. */
  const FLEET_FILTERS = agentFilters<AgentRegistration>(
    (agent) => agent.status,
    (agent) => agent.backend,
    [],
  );

  const fleetFilters = new FilterState('fleet', FLEET_FILTERS);

  let fleetSearch = $state('');

  /** searchedAgents is the fleet after the search and BEFORE the facets —
   *  which is the set the counts are taken over. */
  const searchedAgents = $derived.by(() => {
    const needle = fleetSearch.trim().toLowerCase();
    const all = agentStore.agents;
    if (needle === '') return all;
    return all.filter((agent) =>
      [agent.name, agent.host, agent.backend, agent.model, agent.status]
        .some((field) => (field ?? '').toLowerCase().includes(needle)),
    );
  });

  const fleetFacets = $derived(computeFacets(searchedAgents, FLEET_FILTERS));

  const visibleAgents = $derived(
    filterByFacets(searchedAgents, FLEET_FILTERS, fleetFilters.values),
  );

  /** narrowed says the emptiness is a control's doing, not the tenant's. */
  const fleetNarrowed = $derived(
    fleetSearch.trim() !== '' || Object.keys(fleetFilters.values).length > 0,
  );

  // Whether the Wails desktop transport is active — the Deploy
  // Remote Agent button is only shown when this is true because
  // other transports reject deploy with DeployNotSupportedError.
  let isWailsTransport = $state(false);
  let deployDialogOpen = $state(false);

  $effect(() => {
    void (async () => {
      try {
        const client = await getSDKClient();
        isWailsTransport = client.connection.getTransport() === 'wails';
      } catch {
        isWailsTransport = false;
      }
    })();
  });

  let refreshing = $state(false);
  async function handleRefresh(): Promise<void> {
    refreshing = true;
    await refreshAgents();
    refreshing = false;
  }

  // Agent detail: navigate to dedicated detail view.
  let selectedAgent: AgentRegistration | null = $state(null);

  function openDetail(agent: AgentRegistration): void {
    selectedAgent = agent;
    routeAgentDetail(agent.id);
  }

  function closeDetail(): void {
    selectedAgent = null;
  }

  // ---------------------------------------------------------------------------
  // Register agent dialog
  // ---------------------------------------------------------------------------

  let registerOpen = $state(false);

  async function onRegistered(): Promise<void> {
    await refreshAgents();
  }

  // ---------------------------------------------------------------------------
  // Remove agent
  // ---------------------------------------------------------------------------

  let removeTarget: AgentRegistration | null = $state(null);
  let removing = $state(false);

  function requestRemove(agent: AgentRegistration, e: Event): void {
    e.stopPropagation();
    removeTarget = agent;
  }

  function dismissRemove(): void {
    removeTarget = null;
  }

  async function confirmRemove(): Promise<void> {
    if (!removeTarget) return;
    removing = true;
    const agent = removeTarget;
    const req: RemoveAgentRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      agent_id: agent.id,
    };
    try {
      const client = await getSDKClient();
      await client.orchestrator.removeAgent(req);
      showToast('success', `Agent "${agent.name}" removed`);
      removeTarget = null;
      await refreshAgents();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Failed to remove agent: ${msg}`);
    } finally {
      removing = false;
    }
  }
</script>

<!-- RegisterAgentDialog -->
<RegisterAgentDialog bind:open={registerOpen} onRegistered={onRegistered} />

<!-- DeployRemoteAgentDialog: only reachable via the Wails desktop transport. -->
<DeployRemoteAgentDialog
  open={deployDialogOpen}
  onClose={() => {
    deployDialogOpen = false;
    void refreshAgents();
  }}
/>

<!-- Agent detail modal -->
{#if selectedAgent !== null}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Agent details"
    onkeydown={(e) => { if (e.key === 'Escape') closeDetail(); }}
  >
    <div class="modal-box w-full max-w-lg">
      <h3 class="text-base font-semibold mb-1">{selectedAgent.name}</h3>
      <p class="text-xs text-base-content/40 font-mono mb-4">{selectedAgent.id}</p>
      <pre class="bg-base-300 rounded-btn p-3 text-xs overflow-auto max-h-96 whitespace-pre-wrap break-all">{JSON.stringify(selectedAgent, null, 2)}</pre>
      <div class="modal-action mt-4">
        <button class="btn btn-sm btn-ghost" onclick={closeDetail}>Close</button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={closeDetail} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Remove confirm dialog -->
{#if removeTarget !== null}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    onkeydown={(e) => { if (e.key === 'Escape') dismissRemove(); }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Remove Agent?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Agent <span class="font-semibold">{removeTarget.name}</span> will be deregistered.
      </p>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={dismissRemove} disabled={removing}>
          Keep
        </button>
        <button
          class="btn btn-sm btn-error"
          onclick={() => { void confirmRemove(); }}
          disabled={removing}
        >
          {#if removing}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Remove
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={dismissRemove} onkeydown={() => {}}></div>
  </dialog>
{/if}

<div class="flex flex-col gap-4 p-4 h-full">
  <PageHeader
    title="Agent Fleet"
    subtitle={`${agentStore.agents.length} agent daemon${agentStore.agents.length === 1 ? '' : 's'} registered`}
    icon={Server}
  >
    <button
      class="btn btn-sm btn-primary gap-1"
      onclick={() => { registerOpen = true; }}
      aria-label="Register new agent"
    >
      <UserPlus class="h-4 w-4" />
      Register Agent
    </button>

    {#if isWailsTransport}
      <button
        class="btn btn-sm btn-primary"
        onclick={() => { deployDialogOpen = true; }}
        aria-label="Deploy Remote Agent"
      >
        Deploy Remote Agent
      </button>
    {/if}

    {#if deploySlot}
      {@render deploySlot()}
    {/if}

    <button
      class="btn btn-sm btn-ghost"
      onclick={() => { void handleRefresh(); }}
      disabled={refreshing || agentStore.agentsLoading}
      aria-label="Refresh agent fleet"
    >
      {#if refreshing || agentStore.agentsLoading}
        <span class="loading loading-spinner loading-xs"></span>
      {:else}
        &#x21BB;
      {/if}
    </button>
  </PageHeader>

  <!-- Search and facets — the same controls the tables in this app carry. -->
  {#if agentStore.agents.length > 0}
    <div class="space-y-2">
      <input
        type="search"
        class="input input-bordered input-sm w-full max-w-xs"
        placeholder="Search agents..."
        aria-label="Search agents"
        data-testid="fleet-search"
        bind:value={fleetSearch}
      />
      <FilterBar
        fields={FLEET_FILTERS}
        values={fleetFilters.values}
        facets={fleetFacets}
        onchange={(next) => fleetFilters.apply(next)}
      />
      <div class="text-xs text-base-content/50" data-testid="fleet-count">
        {visibleAgents.length} of {agentStore.agents.length} agents
      </div>
    </div>
  {/if}

  <!-- Grid -->
  {#if agentStore.agentsLoading && agentStore.agents.length === 0}
    <div class="flex items-center justify-center flex-1">
      <span class="loading loading-ring loading-md text-primary"></span>
    </div>
  {:else if agentStore.agents.length === 0}
    <div class="flex items-center justify-center flex-1 text-base-content/30 text-sm">
      No agents registered.
    </div>
  {:else if visibleAgents.length === 0}
    <!--
      A FLEET NARROWED TO NOTHING IS NOT AN EMPTY FLEET. "No agents
      registered" over a tenant with forty of them, because the operator
      asked for the offline ones and there are none, is a falsehood.
    -->
    <div
      class="flex items-center justify-center flex-1 text-base-content/30 text-sm"
      data-testid="fleet-no-match"
    >
      {fleetNarrowed ? 'No agents match the current filter.' : 'No agents registered.'}
    </div>
  {:else}
    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3 overflow-y-auto">
      {#each visibleAgents as agent (agent.id)}
        <div
          class="card bg-base-200 border {cardBorder(agent.status)} hover:shadow-md transition-shadow cursor-pointer"
          onclick={() => openDetail(agent)}
          onkeydown={(e) => { if (e.key === 'Enter') openDetail(agent); }}
          tabindex="0"
          role="button"
          aria-label="View details for agent {agent.name}"
        >
          <div class="card-body p-3 gap-2">
            <!-- Name + status -->
            <div class="flex items-center justify-between gap-2">
              <span class="font-semibold text-sm truncate" title={agent.name}>
                {agent.name}
              </span>
              <span class="badge badge-xs {statusVariant(agent.status)}">
                {agent.status}
              </span>
            </div>

            <!-- Host -->
            <div class="text-xs text-base-content/50 font-mono truncate" title={agent.host}>
              {agent.host}
            </div>

            <!-- Backend / Model / Runtime labeled fields -->
            <!--
              Each field is prefixed with a small uppercase label so that when
              two fields share the same value (e.g. backend=runtime=ollama)
              users can distinguish them at a glance.
            -->
            <dl class="flex flex-col gap-0.5">
              <div class="flex items-center gap-1.5">
                <dt
                  class="text-xs uppercase tracking-wider text-base-content/40 shrink-0 w-14"
                  data-testid="agent-backend-label"
                >
                  Backend:
                </dt>
                <dd>
                  <span
                    class="badge badge-xs badge-ghost"
                    data-testid="agent-runtime-badge"
                  >
                    {uniqueBackends(agent)}
                  </span>
                </dd>
              </div>
              <div class="flex items-center gap-1.5">
                <dt
                  class="text-xs uppercase tracking-wider text-base-content/40 shrink-0 w-14"
                  data-testid="agent-model-dt-label"
                >
                  Model:
                </dt>
                <dd
                  class="text-xs text-base-content/40 truncate"
                  data-testid="agent-model-label"
                >
                  {agent.model || '—'}
                </dd>
              </div>
              <div class="flex items-center gap-1.5">
                <dt
                  class="text-xs uppercase tracking-wider text-base-content/40 shrink-0 w-14"
                  data-testid="agent-runtime-dt-label"
                >
                  Runtime:
                </dt>
                <dd>
                  <span class="badge badge-xs badge-ghost" data-testid="agent-runtime-pill">
                    {agent.agent_type || uniqueBackends(agent)}
                  </span>
                </dd>
              </div>
            </dl>

            <!-- Capabilities summary -->
            {#if agent.capabilities && agent.capabilities.length > 0}
              <div class="flex flex-wrap gap-1">
                {#each agent.capabilities.slice(0, 3) as cap}
                  <span class="badge badge-xs badge-ghost">{cap}</span>
                {/each}
                {#if agent.capabilities.length > 3}
                  <span class="badge badge-xs badge-ghost">+{agent.capabilities.length - 3}</span>
                {/if}
              </div>
            {/if}

            <!-- Footer: heartbeat + Remove action -->
            <div class="flex items-center justify-between mt-1">
              <span class="text-xs text-base-content/30">
                Last seen: {relativeTime(agent.last_seen)}
              </span>
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={(e) => requestRemove(agent, e)}
                aria-label="Remove agent {agent.name}"
              >
                Remove
              </button>
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
