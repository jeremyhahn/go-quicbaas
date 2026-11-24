<!--
  Copyright (c) 2026 Jeremy Hahn
  Copyright (c) 2026 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  AgentsView manages the tenant's Agent catalog: the Agents a job's slots
  are served by, and the agent pools that chain Agents in failover order.
  Each can be created, edited, deleted and asked what uses it. Edit reads
  the stored record and sends it back with the form's fields merged on, so
  the fields the form does not show are kept. The server's refusals (a
  record still in use, a role a slot still needs) are shown as returned.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { formatRelative } from '../../lib/time.ts';
  import type { Agent, AgentPool, AgentUsage, AgentsClient } from '@quicbaas/sdk';
  import { Bot, Plus, RefreshCw, Pencil, Trash2, Link } from 'lucide-svelte';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import AgentEditDialog from '../../components/orchestrator/AgentEditDialog.svelte';
  import AgentPoolEditDialog from '../../components/orchestrator/AgentPoolEditDialog.svelte';

  type Kind = 'agent' | 'pool';

  let agents = $state<Agent[]>([]);
  let pools = $state<AgentPool[]>([]);
  let loading = $state(false);
  let loaded = $state(false);
  let loadError = $state('');
  let unavailable = $state(false);

  // The action error of each section: a failed get before edit or used-by.
  let agentActionError = $state('');
  let poolActionError = $state('');
  let busyRow = $state('');

  let agentDialog = $state<{ mode: 'create' | 'edit'; stored: Agent | null } | null>(null);
  let poolDialog = $state<{ mode: 'create' | 'edit'; stored: AgentPool | null } | null>(null);
  let usage = $state<{ kind: Kind; id: string; rows: AgentUsage[] } | null>(null);
  let deleteTarget = $state<{ kind: Kind; id: string } | null>(null);
  let deleting = $state(false);
  let deleteError = $state('');

  function message(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
  }

  /** catalog returns the connection's Agent catalog, or null when it has none. */
  async function catalog(): Promise<AgentsClient | null> {
    const client = await getSDKClient();
    return client.agents ?? null;
  }

  /**
   * fetchAll reads the Agents and pools of ONE tenant, named by the caller.
   * An answer for a tenant the operator has since left is dropped.
   */
  async function fetchAll(tenantID: string): Promise<void> {
    loading = true;
    loadError = '';
    try {
      const api = await catalog();
      if (tenantID !== orchestratorStore.activeTenantID) return;
      if (!api) {
        unavailable = true;
        return;
      }
      unavailable = false;
      const [agentList, poolList] = await Promise.all([api.listAgents(tenantID), api.listPools(tenantID)]);
      if (tenantID !== orchestratorStore.activeTenantID) return;
      agents = agentList ?? [];
      pools = poolList ?? [];
      loaded = true;
    } catch (err: unknown) {
      if (tenantID !== orchestratorStore.activeTenantID) return;
      loadError = `Failed to load agents: ${message(err)}`;
    } finally {
      if (tenantID === orchestratorStore.activeTenantID) loading = false;
    }
  }

  $effect(() => {
    const tenantID = orchestratorStore.activeTenantID;
    void fetchAll(tenantID);
  });

  function reload(): Promise<void> {
    return fetchAll(orchestratorStore.activeTenantID);
  }

  function setActionError(kind: Kind, text: string): void {
    if (kind === 'agent') agentActionError = text;
    else poolActionError = text;
  }

  // ---------------------------------------------------------------------------
  // Agents
  // ---------------------------------------------------------------------------

  async function openEditAgent(id: string): Promise<void> {
    agentActionError = '';
    busyRow = `agent:${id}`;
    try {
      const api = await catalog();
      if (!api) return;
      const stored = await api.getAgent(id, orchestratorStore.activeTenantID);
      agentDialog = { mode: 'edit', stored };
    } catch (err: unknown) {
      agentActionError = `Edit agent ${id} failed: ${message(err)}`;
    } finally {
      busyRow = '';
    }
  }

  async function saveAgent(fields: Agent): Promise<void> {
    const api = await catalog();
    if (!api || !agentDialog) return;
    const tenant = orchestratorStore.activeTenantID;
    if (agentDialog.mode === 'create') {
      await api.createAgent(fields, tenant);
      showToast('success', `Agent ${fields.id} created`);
    } else {
      const stored = agentDialog.stored;
      await api.updateAgent(fields.id, { ...stored, ...fields }, tenant);
      showToast('success', `Agent ${fields.id} saved`);
    }
    agentDialog = null;
    await reload();
  }

  // ---------------------------------------------------------------------------
  // Pools
  // ---------------------------------------------------------------------------

  async function openEditPool(id: string): Promise<void> {
    poolActionError = '';
    busyRow = `pool:${id}`;
    try {
      const api = await catalog();
      if (!api) return;
      const stored = await api.getPool(id, orchestratorStore.activeTenantID);
      poolDialog = { mode: 'edit', stored };
    } catch (err: unknown) {
      poolActionError = `Edit pool ${id} failed: ${message(err)}`;
    } finally {
      busyRow = '';
    }
  }

  async function savePool(fields: AgentPool): Promise<void> {
    const api = await catalog();
    if (!api || !poolDialog) return;
    const tenant = orchestratorStore.activeTenantID;
    if (poolDialog.mode === 'create') {
      await api.createPool(fields, tenant);
      showToast('success', `Pool ${fields.id} created`);
    } else {
      const stored = poolDialog.stored;
      await api.updatePool(fields.id, { ...stored, ...fields }, tenant);
      showToast('success', `Pool ${fields.id} saved`);
    }
    poolDialog = null;
    await reload();
  }

  // ---------------------------------------------------------------------------
  // Used by and delete, for both kinds
  // ---------------------------------------------------------------------------

  async function openUsage(kind: Kind, id: string): Promise<void> {
    setActionError(kind, '');
    busyRow = `${kind}:${id}`;
    try {
      const api = await catalog();
      if (!api) return;
      const tenant = orchestratorStore.activeTenantID;
      const rows = kind === 'agent' ? await api.agentUsedBy(id, tenant) : await api.poolUsedBy(id, tenant);
      usage = { kind, id, rows: rows ?? [] };
    } catch (err: unknown) {
      setActionError(kind, `Used by ${kind} ${id} failed: ${message(err)}`);
    } finally {
      busyRow = '';
    }
  }

  function askDelete(kind: Kind, id: string): void {
    deleteError = '';
    deleteTarget = { kind, id };
  }

  async function confirmDelete(): Promise<void> {
    if (!deleteTarget) return;
    const { kind, id } = deleteTarget;
    deleting = true;
    deleteError = '';
    try {
      const api = await catalog();
      if (!api) return;
      const tenant = orchestratorStore.activeTenantID;
      if (kind === 'agent') await api.deleteAgent(id, tenant);
      else await api.deletePool(id, tenant);
      showToast('success', `${kind === 'agent' ? 'Agent' : 'Pool'} ${id} deleted`);
      deleteTarget = null;
      await reload();
    } catch (err: unknown) {
      deleteError = message(err);
    } finally {
      deleting = false;
    }
  }

  function updated(at: string | undefined): string {
    return at ? formatRelative(at) : '—';
  }
</script>

{#if agentDialog}
  <AgentEditDialog
    mode={agentDialog.mode}
    initial={agentDialog.stored}
    onsubmit={saveAgent}
    oncancel={() => { agentDialog = null; }}
  />
{/if}

{#if poolDialog}
  <AgentPoolEditDialog
    mode={poolDialog.mode}
    initial={poolDialog.stored}
    {agents}
    onsubmit={savePool}
    oncancel={() => { poolDialog = null; }}
  />
{/if}

{#if usage}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Used by {usage.kind} {usage.id}"
    data-testid="usage-dialog"
    onkeydown={(e) => { if (e.key === 'Escape') { usage = null; } }}
  >
    <div class="modal-box max-w-md">
      <h3 class="font-bold text-base">What uses {usage.kind} {usage.id}</h3>
      {#if usage.rows.length === 0}
        <p class="text-sm text-base-content/60 mt-2">Not used</p>
      {:else}
        <table class="table table-sm mt-2">
          <thead><tr><th>Kind</th><th>ID</th><th>Slot</th></tr></thead>
          <tbody>
            {#each usage.rows as row, i (i)}
              <tr><td>{row.kind}</td><td class="font-mono">{row.id}</td><td>{row.slot}</td></tr>
            {/each}
          </tbody>
        </table>
      {/if}
      <div class="modal-action">
        <button class="btn btn-sm" onclick={() => { usage = null; }}>Close</button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { usage = null; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

{#if deleteTarget}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Confirm delete {deleteTarget.kind}"
    onkeydown={(e) => { if (e.key === 'Escape' && !deleting) { deleteTarget = null; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Delete {deleteTarget.kind === 'agent' ? 'Agent' : 'Pool'}?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        {deleteTarget.kind === 'agent' ? 'Agent' : 'Pool'}
        <span class="font-semibold font-mono">{deleteTarget.id}</span>
        will be removed. The server refuses while anything still uses it.
      </p>
      {#if deleteError}
        <div class="alert alert-error text-sm py-2 mt-3" data-testid="delete-error">{deleteError}</div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={() => { deleteTarget = null; }} disabled={deleting}>Cancel</button>
        <button class="btn btn-sm btn-error" onclick={() => { void confirmDelete(); }} disabled={deleting}>
          {#if deleting}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Delete
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { if (!deleting) deleteTarget = null; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<div class="page">
  <PageHeader title="Agents" subtitle="The Agents that serve a job's slots, and the pools that chain them" icon={Bot}>
    {#snippet children()}
      <button
        class="btn btn-sm btn-ghost gap-1"
        onclick={() => { void reload(); }}
        disabled={loading || unavailable}
        aria-label="Refresh agents"
      >
        <RefreshCw class="h-4 w-4 {loading ? 'animate-spin' : ''}" />
        Refresh
      </button>
    {/snippet}
  </PageHeader>

  {#if unavailable}
    <EmptyState
      icon={Bot}
      title="Agents are not available on this connection."
      description="The Agent catalog is reached over the REST API."
    />
  {:else}
    {#if loadError}
      <div class="alert alert-error text-sm py-2">
        <span>{loadError}</span>
      </div>
    {/if}

    <section class="space-y-2">
      <div class="flex items-center justify-between">
        <h2 class="font-semibold">Agents</h2>
        <button class="btn btn-sm btn-primary gap-1" onclick={() => { agentDialog = { mode: 'create', stored: null }; }} aria-label="New agent">
          <Plus class="h-4 w-4" />
          New agent
        </button>
      </div>
      {#if agentActionError}
        <div class="alert alert-error text-sm py-2" data-testid="agent-action-error">{agentActionError}</div>
      {/if}
      {#if loading && !loaded}
        <div class="skeleton h-16 w-full rounded-box"></div>
      {:else if agents.length === 0}
        <p class="text-sm text-base-content/60">No agents yet.</p>
      {:else}
        <div class="overflow-x-auto">
          <table class="table table-sm" data-testid="agents-table">
            <thead>
              <tr><th>ID</th><th>Name</th><th>Roles</th><th>Backend</th><th>Model</th><th>Updated</th><th></th></tr>
            </thead>
            <tbody>
              {#each agents as a (a.id)}
                <tr>
                  <td class="font-mono">{a.id}</td>
                  <td>{a.name}</td>
                  <td>{(a.roles ?? []).join(', ')}</td>
                  <td>{a.backend}</td>
                  <td>{a.model}</td>
                  <td>{updated(a.updated_at)}</td>
                  <td class="flex gap-1 justify-end">
                    <button class="btn btn-xs btn-ghost" aria-label="Edit agent {a.id}" title="Edit" disabled={busyRow === `agent:${a.id}`} onclick={() => { void openEditAgent(a.id); }}>
                      <Pencil class="h-3 w-3" />
                    </button>
                    <button class="btn btn-xs btn-ghost" aria-label="Used by agent {a.id}" title="Used by" disabled={busyRow === `agent:${a.id}`} onclick={() => { void openUsage('agent', a.id); }}>
                      <Link class="h-3 w-3" />
                    </button>
                    <button class="btn btn-xs btn-ghost text-error" aria-label="Delete agent {a.id}" title="Delete" onclick={() => askDelete('agent', a.id)}>
                      <Trash2 class="h-3 w-3" />
                    </button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>

    <section class="space-y-2 mt-6">
      <div class="flex items-center justify-between">
        <h2 class="font-semibold">Agent pools</h2>
        <button class="btn btn-sm btn-primary gap-1" onclick={() => { poolDialog = { mode: 'create', stored: null }; }} aria-label="New pool">
          <Plus class="h-4 w-4" />
          New pool
        </button>
      </div>
      {#if poolActionError}
        <div class="alert alert-error text-sm py-2" data-testid="pool-action-error">{poolActionError}</div>
      {/if}
      {#if loading && !loaded}
        <div class="skeleton h-16 w-full rounded-box"></div>
      {:else if pools.length === 0}
        <p class="text-sm text-base-content/60">No pools yet.</p>
      {:else}
        <div class="overflow-x-auto">
          <table class="table table-sm" data-testid="pools-table">
            <thead>
              <tr><th>ID</th><th>Name</th><th>Members</th><th>Updated</th><th></th></tr>
            </thead>
            <tbody>
              {#each pools as p (p.id)}
                <tr>
                  <td class="font-mono">{p.id}</td>
                  <td>{p.name}</td>
                  <td class="font-mono">{(p.agents ?? []).join(' → ')}</td>
                  <td>{updated(p.updated_at)}</td>
                  <td class="flex gap-1 justify-end">
                    <button class="btn btn-xs btn-ghost" aria-label="Edit pool {p.id}" title="Edit" disabled={busyRow === `pool:${p.id}`} onclick={() => { void openEditPool(p.id); }}>
                      <Pencil class="h-3 w-3" />
                    </button>
                    <button class="btn btn-xs btn-ghost" aria-label="Used by pool {p.id}" title="Used by" disabled={busyRow === `pool:${p.id}`} onclick={() => { void openUsage('pool', p.id); }}>
                      <Link class="h-3 w-3" />
                    </button>
                    <button class="btn btn-xs btn-ghost text-error" aria-label="Delete pool {p.id}" title="Delete" onclick={() => askDelete('pool', p.id)}>
                      <Trash2 class="h-3 w-3" />
                    </button>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </section>
  {/if}
</div>
