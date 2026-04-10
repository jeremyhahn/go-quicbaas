<!--
  AgentsView renders the registered agent fleet as a card grid with status
  badges, backend/model labels, capabilities tags, and last-seen timestamps.
-->
<script lang="ts">
  import { orchestratorStore, type AgentStatus } from '../../stores/orchestrator.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import StatusBadge from '../../components/core/StatusBadge.svelte';

  function statusVariant(status: AgentStatus): 'success' | 'error' | 'warning' | 'info' | 'ghost' {
    if (status === 'running') return 'success';
    if (status === 'error') return 'error';
    if (status === 'offline') return 'ghost';
    if (status === 'idle') return 'info';
    return 'ghost';
  }

  function formatLastSeen(ts: number): string {
    if (!ts) return 'never';
    const diff = Date.now() / 1000 - ts;
    if (diff < 60) return 'just now';
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
    return new Date(ts * 1000).toLocaleString();
  }
</script>

<div class="p-6 space-y-4">
  <PageHeader
    title="Agent Fleet"
    subtitle="{orchestratorStore.agents.length} registered agents"
  />

  {#if orchestratorStore.agentsLoading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md text-primary"></span>
    </div>
  {:else if orchestratorStore.agents.length === 0}
    <div class="text-center py-16 text-base-content/30">
      No agents registered
    </div>
  {:else}
    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      {#each orchestratorStore.agents as agent (agent.id)}
        <div class="card bg-base-200 rounded-box hover:bg-base-300/50 transition-colors">
          <div class="card-body p-4 gap-2">
            <!-- Header row -->
            <div class="flex items-start justify-between gap-2">
              <div>
                <div class="font-semibold text-sm">{agent.name || agent.id}</div>
                <div class="text-xs text-base-content/40 font-mono">{agent.id.slice(0, 16)}...</div>
              </div>
              <StatusBadge
                text={agent.status}
                variant={statusVariant(agent.status)}
                pulse={agent.status === 'running'}
              />
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
