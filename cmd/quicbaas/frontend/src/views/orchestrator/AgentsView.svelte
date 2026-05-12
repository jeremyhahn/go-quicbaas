<!--
  AgentsView renders the registered agent fleet as a card grid with status
  badges, backend/model labels, capabilities tags, last-seen timestamps, and
  a Remove confirm flow.
-->
<script lang="ts">
  import { orchestratorStore, refresh, type AgentStatus, type AgentRegistration } from '../../stores/orchestrator.svelte.ts';
  import { routeAgentDetail } from '../../stores/router.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { Server, Trash2 } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import RegisterAgentDialog from '../../components/orchestrator/RegisterAgentDialog.svelte';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { toMillis, formatDateTime } from '../../lib/time.ts';
  import type { RemoveAgentRequest } from '@quicbaas/sdk';

  let showRegister = $state(false);

  // ---------------------------------------------------------------------------
  // Remove confirm modal state
  // ---------------------------------------------------------------------------

  let removeTarget = $state<AgentRegistration | null>(null);
  let removing = $state(false);

  function requestRemove(agent: AgentRegistration): void {
    removeTarget = agent;
  }

  function dismissRemove(): void {
    removeTarget = null;
  }

  async function confirmRemove(): Promise<void> {
    if (!removeTarget) return;
    removing = true;
    const req: RemoveAgentRequest = {
      tenant_id: orchestratorStore.activeTenantID,
      agent_id: removeTarget.id,
    };
    const name = removeTarget.name || removeTarget.id;
    try {
      const client = await getSDKClient();
      await client.orchestrator.removeAgent(req);
      showToast('success', `Agent "${name}" removed`);
      removeTarget = null;
      void refresh('agents');
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Remove failed: ${msg}`);
    } finally {
      removing = false;
    }
  }

  function statusVariant(status: AgentStatus): 'success' | 'error' | 'warning' | 'info' | 'ghost' {
    if (status === 'running') return 'success';
    if (status === 'error') return 'error';
    if (status === 'offline') return 'ghost';
    if (status === 'idle') return 'info';
    return 'ghost';
  }

  function formatLastSeen(ts: number): string {
    const ms = toMillis(ts);
    if (ms === null) return 'never';
    const diff = Date.now() - ms;
    if (diff < 60_000) return 'just now';
    if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
    return formatDateTime(ts);
  }
</script>

<!-- Remove confirm modal -->
{#if removeTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Confirm remove agent">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Remove agent?</h3>
      <p class="text-sm text-base-content/70 mb-4">
        Remove <span class="font-semibold">{removeTarget.name || removeTarget.id}</span> from the fleet?
        Running jobs may be affected.
      </p>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={dismissRemove} disabled={removing}>Cancel</button>
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
    <button class="modal-backdrop" onclick={dismissRemove} aria-label="Close"></button>
  </div>
{/if}

<div class="p-6 space-y-6">
  <RegisterAgentDialog bind:open={showRegister} />

  <PageHeader
    title="Agent Fleet"
    subtitle="{orchestratorStore.agents.length} registered agents"
    icon={Server}
  >
    {#snippet children()}
      <button class="btn btn-primary btn-sm" onclick={() => { showRegister = true; }}>
        Register Agent
      </button>
    {/snippet}
  </PageHeader>

  {#if orchestratorStore.agentsLoading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md text-primary"></span>
    </div>
  {:else if orchestratorStore.agents.length === 0}
    <EmptyState
      icon={Server}
      title="No agents registered yet — deploy your first agent over SSH."
      description="Agents execute your jobs. Register one via the button above or deploy directly from the CLI."
      actionLabel="Register Agent"
      onaction={() => { showRegister = true; }}
      learnMoreHref="https://docs.quicbaas.io/agents/deploy"
      learnMoreLabel="Deploy via CLI"
    />
  {:else}
    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {#each orchestratorStore.agents as agent (agent.id)}
        <div
          class="card bg-base-200 rounded-box hover:bg-base-300/50 transition-colors cursor-pointer"
          onclick={() => routeAgentDetail(agent.id)}
          onkeydown={(e) => { if (e.key === 'Enter') routeAgentDetail(agent.id); }}
          tabindex="0"
          role="button"
          aria-label="View details for agent {agent.name || agent.id}"
        >
          <div class="card-body p-4 gap-2">
            <!-- Header row -->
            <div class="flex items-start justify-between gap-2">
              <div>
                <div class="font-semibold text-sm">{agent.name || agent.id}</div>
                <div class="text-xs text-base-content/40 font-mono">{agent.id.slice(0, 16)}...</div>
              </div>
              <div class="flex items-center gap-1">
                <StatusBadge
                  text={agent.status}
                  variant={statusVariant(agent.status)}
                  pulse={agent.status === 'running'}
                />
                <button
                  class="btn btn-xs btn-ghost text-error"
                  onclick={() => requestRemove(agent)}
                  aria-label="Remove agent {agent.name || agent.id}"
                  title="Remove agent"
                >
                  <Trash2 class="h-3 w-3" />
                </button>
              </div>
            </div>

            <!-- Backend + model -->
            <div class="flex gap-2 flex-wrap">
              <span class="badge badge-sm badge-primary">{agent.backend}</span>
              {#if agent.model}
                <span class="badge badge-sm badge-ghost">{agent.model}</span>
              {/if}
              {#if agent.agent_type}
                <span class="badge badge-sm badge-neutral">{agent.agent_type}</span>
              {/if}
              {#if agent.kind === 'embedded' || agent.id?.startsWith('embedded-')}
                <span class="badge badge-sm badge-accent" title="Co-located in-process agent (bufconn loopback)">Embedded</span>
              {/if}
            </div>

            <!-- Capabilities -->
            {#if agent.capabilities && agent.capabilities.length > 0}
              <div class="flex flex-wrap gap-1">
                {#each agent.capabilities.slice(0, 4) as cap}
                  <span class="badge badge-xs badge-outline text-base-content/60">{cap}</span>
                {/each}
                {#if agent.capabilities.length > 4}
                  <span class="badge badge-xs badge-outline text-base-content/40">
                    +{agent.capabilities.length - 4}
                  </span>
                {/if}
              </div>
            {/if}

            <!-- Footer -->
            <div class="flex items-center justify-between text-xs text-base-content/40 pt-1 border-t border-base-300/40">
              <span>{agent.host}</span>
              <span>seen {formatLastSeen(agent.last_seen)}</span>
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
