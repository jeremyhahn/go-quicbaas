<!--
  FleetList renders a grid of fleet nodes with live status.
  Header provides:
    - "+ Register node" button (left) — opens RegisterFleetNodeDialog
    - "Deploy node" area (right) — owned by the other node, rendered
      via a slot so that node can inject its button without touching this file.
  Each card has a "Remove" action that confirms before deregistering the node.
-->
<script lang="ts">
  import { fleetStore, refreshFleet } from '../../stores/fleetStore.svelte.ts';
  import { routeFleetNode } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import RegisterFleetNodeDialog from './RegisterFleetNodeDialog.svelte';
  import DeployFleetNodeDialog from './DeployFleetNodeDialog.svelte';
  import WorkerImagePanel from './WorkerImagePanel.svelte';
  import type { FleetNode } from '@quicbaas/sdk';
  import { UserPlus, Cloud, ServerCog, RefreshCw } from 'lucide-svelte';
  import PageHeader from '../core/PageHeader.svelte';
  import FilterBar from '../core/FilterBar.svelte';
  import StatusBadge from '../core/StatusBadge.svelte';
  import { statusToVariant } from '../core/statusVariant.ts';
  import { computeFacets, filterByFacets } from '../../lib/filters.ts';
  import { FilterState } from '../../lib/filterState.svelte.ts';
  import { fleetNodeFilters } from '../../lib/listFilters.ts';
  import type { RemoveFleetNodeRequest } from '@quicbaas/sdk';

  let {
    // deploySlot allows the Deploy node button (other node's work) to
    // be injected in the header without modifying this component's internals.
    deploySlot,
  }: {
    deploySlot?: import('svelte').Snippet;
  } = $props();

  // ---------------------------------------------------------------------------
  // Relative time helper
  // ---------------------------------------------------------------------------

  // The fleet API returns `last_seen` as Unix nanoseconds
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

  function cardBorder(status: string): string {
    if (status === 'running' || status === 'idle') return 'border-success/20';
    if (status === 'error') return 'border-error/20';
    return 'border-base-300/50';
  }

  // Unique backends across all nodes.
  const uniqueBackends = $derived(
    (a: FleetNode): string => a.backend,
  );

  // ---------------------------------------------------------------------------
  // Finding one node in a fleet
  //
  // The fleet is a card GRID and not a table, which is the right shape for
  // what a card shows and the wrong shape for "which three are offline". The
  // search and the facets are therefore the same controls every list in this
  // app has — FilterBar, the same URL grammar (`#/fleet?fleet_status=offline`)
  // — over the collection the store holds rather than over a server page: the
  // fleet listing is read whole, so the counts are exact and nothing is
  // hiding on a page two that does not exist.
  // ---------------------------------------------------------------------------

  /** The facet fields, and how each reads its value off a node. */
  const FLEET_FILTERS = fleetNodeFilters<FleetNode>(
    (node) => node.status,
    (node) => node.backend,
    [],
  );

  const fleetFilters = new FilterState('fleet', FLEET_FILTERS);

  let fleetSearch = $state('');

  /** searchedNodes is the fleet after the search and BEFORE the facets —
   *  which is the set the counts are taken over. */
  const searchedNodes = $derived.by(() => {
    const needle = fleetSearch.trim().toLowerCase();
    const all = fleetStore.nodes;
    if (needle === '') return all;
    return all.filter((node) =>
      [node.name, node.host, node.backend, node.model, node.status]
        .some((field) => (field ?? '').toLowerCase().includes(needle)),
    );
  });

  const fleetFacets = $derived(computeFacets(searchedNodes, FLEET_FILTERS));

  const visibleNodes = $derived(
    filterByFacets(searchedNodes, FLEET_FILTERS, fleetFilters.values),
  );

  /** narrowed says the emptiness is a control's doing, not the tenant's. */
  const fleetNarrowed = $derived(
    fleetSearch.trim() !== '' || Object.keys(fleetFilters.values).length > 0,
  );

  // Whether the Wails desktop transport is active — the Deploy
  // Remote Fleet node button is only shown when this is true because
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
    await refreshFleet();
    refreshing = false;
  }

  // Fleet node detail: navigate to dedicated detail view.
  let selectedNode: FleetNode | null = $state(null);

  function openDetail(node: FleetNode): void {
    selectedNode = node;
    routeFleetNode(node.id);
  }

  function closeDetail(): void {
    selectedNode = null;
  }

  // ---------------------------------------------------------------------------
  // Register node dialog
  // ---------------------------------------------------------------------------

  let registerOpen = $state(false);

  async function onRegistered(): Promise<void> {
    await refreshFleet();
  }

  // ---------------------------------------------------------------------------
  // Remove node
  // ---------------------------------------------------------------------------

  let removeTarget: FleetNode | null = $state(null);
  let removing = $state(false);

  function requestRemove(node: FleetNode, e: Event): void {
    e.stopPropagation();
    removeTarget = node;
  }

  function dismissRemove(): void {
    removeTarget = null;
  }

  async function confirmRemove(): Promise<void> {
    if (!removeTarget) return;
    removing = true;
    const node = removeTarget;
    const req: RemoveFleetNodeRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      node_id: node.id,
    };
    try {
      const client = await getSDKClient();
      await client.orchestrator.removeFleetNode(req);
      showToast('success', `Fleet node "${node.name}" removed`);
      removeTarget = null;
      await refreshFleet();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Failed to remove node: ${msg}`);
    } finally {
      removing = false;
    }
  }
</script>

<!-- RegisterFleetNodeDialog -->
<RegisterFleetNodeDialog bind:open={registerOpen} onRegistered={onRegistered} />

<!-- DeployFleetNodeDialog: only reachable via the Wails desktop transport. -->
<DeployFleetNodeDialog
  open={deployDialogOpen}
  onClose={() => {
    deployDialogOpen = false;
    void refreshFleet();
  }}
/>

<!-- Fleet node detail modal -->
{#if selectedNode !== null}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Fleet node details"
    onkeydown={(e) => { if (e.key === 'Escape') closeDetail(); }}
  >
    <div class="modal-box w-full max-w-lg">
      <h3 class="text-base font-semibold mb-1">{selectedNode.name}</h3>
      <p class="text-xs text-base-content/40 font-mono mb-4">{selectedNode.id}</p>
      <pre class="bg-base-300 rounded-btn p-3 text-xs overflow-auto max-h-96 whitespace-pre-wrap break-all">{JSON.stringify(selectedNode, null, 2)}</pre>
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
      <h3 class="font-bold text-base">Remove fleet node?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Fleet node <span class="font-semibold">{removeTarget.name}</span> will be deregistered.
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

<div class="page">
  <PageHeader
    title="Fleet"
    subtitle={`${fleetStore.nodes.length} fleet node${fleetStore.nodes.length === 1 ? '' : 's'} registered`}
    icon={ServerCog}
  >
    <button
      class="btn btn-sm btn-primary gap-1"
      onclick={() => { registerOpen = true; }}
      aria-label="Register a fleet node"
    >
      <UserPlus class="h-4 w-4" />
      Register node
    </button>

    {#if isWailsTransport}
      <button
        class="btn btn-sm btn-primary"
        onclick={() => { deployDialogOpen = true; }}
        aria-label="Deploy node"
      >
        Deploy node
      </button>
    {/if}

    {#if deploySlot}
      {@render deploySlot()}
    {/if}

    <button
      class="btn btn-sm btn-ghost btn-square"
      onclick={() => { void handleRefresh(); }}
      disabled={refreshing || fleetStore.nodesLoading}
      aria-label="Refresh fleet"
      title="Refresh"
    >
      {#if refreshing || fleetStore.nodesLoading}
        <span class="loading loading-spinner loading-xs"></span>
      {:else}
        <RefreshCw class="h-4 w-4" />
      {/if}
    </button>
  </PageHeader>

  <!-- The image every sandboxed worker runs in, and whether it is current. -->
  <WorkerImagePanel />

  <!-- Search and facets — the same controls the tables in this app carry. -->
  {#if fleetStore.nodes.length > 0}
    <div class="space-y-2">
      <input
        type="search"
        class="input input-sm w-full bg-base-100 sm:max-w-xs"
        placeholder="Search fleet nodes..."
        aria-label="Search fleet nodes"
        data-testid="fleet-search"
        bind:value={fleetSearch}
      />
      <FilterBar
        fields={FLEET_FILTERS}
        values={fleetFilters.values}
        facets={fleetFacets}
        onchange={(next) => fleetFilters.apply(next)}
      />
      <div class="text-xs tabular-nums text-base-content/60" data-testid="fleet-count">
        {visibleNodes.length} of {fleetStore.nodes.length} nodes
      </div>
    </div>
  {/if}

  <!-- Grid -->
  {#if fleetStore.nodesLoading && fleetStore.nodes.length === 0}
    <div class="flex items-center justify-center gap-2 py-14 text-sm text-base-content/60">
      <span class="loading loading-spinner loading-sm text-primary"></span>
      Loading nodes…
    </div>
  {:else if fleetStore.nodes.length === 0}
    <div class="rounded-box border border-dashed border-base-300 px-6 py-14 text-center text-sm text-base-content/60">
      No nodes registered.
    </div>
  {:else if visibleNodes.length === 0}
    <!--
      A FLEET NARROWED TO NOTHING IS NOT AN EMPTY FLEET. "No nodes
      registered" over a tenant with forty of them, because the operator
      asked for the offline ones and there are none, is a falsehood.
    -->
    <div
      class="rounded-box border border-dashed border-base-300 px-6 py-14 text-center text-sm text-base-content/60"
      data-testid="fleet-no-match"
    >
      {fleetNarrowed ? 'No nodes match the current filter.' : 'No nodes registered.'}
    </div>
  {:else}
    <div class="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
      {#each visibleNodes as node (node.id)}
        <div
          class="card cursor-pointer border bg-base-200 transition-colors hover:border-primary/50 {cardBorder(node.status)}"
          onclick={() => openDetail(node)}
          onkeydown={(e) => { if (e.key === 'Enter') openDetail(node); }}
          tabindex="0"
          role="button"
          aria-label="View details for fleet node {node.name}"
        >
          <div class="card-body gap-3 p-4">
            <!-- Name, host, status -->
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0">
                <div class="truncate text-sm font-semibold" title={node.name}>{node.name}</div>
                <div class="truncate font-mono text-xs text-base-content/60" title={node.host}>{node.host}</div>
              </div>
              <StatusBadge
                text={node.status}
                variant={statusToVariant(node.status)}
                pulse={node.status === 'running'}
              />
            </div>

            <!--
              Backend / Model / Runtime as a two-column definition list, so
              when two fields share a value (backend=runtime=ollama) the label
              column still says which is which, and every value starts on the
              same edge.
            -->
            <dl class="grid grid-cols-[4.5rem_minmax(0,1fr)] items-center gap-x-3 gap-y-1.5 text-xs">
              <dt class="text-base-content/60" data-testid="agent-backend-label">Backend</dt>
              <dd>
                <span class="badge badge-sm badge-ghost" data-testid="agent-runtime-badge">
                  {uniqueBackends(node)}
                </span>
              </dd>
              <dt class="text-base-content/60" data-testid="agent-model-dt-label">Model</dt>
              <dd class="truncate font-mono" data-testid="agent-model-label" title={node.model || undefined}>
                {node.model || '—'}
              </dd>
              <dt class="text-base-content/60" data-testid="agent-runtime-dt-label">Runtime</dt>
              <dd>
                <span class="badge badge-sm badge-ghost" data-testid="agent-runtime-pill">
                  {node.node_type || uniqueBackends(node)}
                </span>
              </dd>
            </dl>

            <!-- Capabilities summary -->
            {#if node.capabilities && node.capabilities.length > 0}
              <div class="flex flex-wrap gap-1">
                {#each node.capabilities.slice(0, 3) as cap}
                  <span class="badge badge-xs badge-outline">{cap}</span>
                {/each}
                {#if node.capabilities.length > 3}
                  <span class="badge badge-xs badge-outline">+{node.capabilities.length - 3}</span>
                {/if}
              </div>
            {/if}

            <!-- Footer: heartbeat + Remove action -->
            <div class="mt-auto flex items-center justify-between border-t border-base-300/80 pt-2.5">
              <span class="text-xs text-base-content/60">
                Last seen {relativeTime(node.last_seen)}
              </span>
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={(e) => requestRemove(node, e)}
                aria-label="Remove fleet node {node.name}"
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
