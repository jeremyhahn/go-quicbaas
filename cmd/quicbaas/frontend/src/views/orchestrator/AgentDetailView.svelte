<!--
  AgentDetailView renders a premium detail page for a single registered agent.
  Accessible via: #/agents/{id}

  Layout:
  - Header: agent name + status pill + ChevronLeft back + Remove confirm action
  - Identity card: backend, model, host, registered_at, last_seen
  - Capability chips: all capabilities, gpu highlighted
  - Metrics card: metrics if available, otherwise unavailable notice
  - Recent sessions table: sessions on this agent (filtered by backend/model match)
  - Recent jobs table: jobs that reference this agent's backend
  - Raw JSON drawer: collapsible debug panel
-->
<script lang="ts">
  import { agentStore, refreshAgents } from '../../stores/agentStore.svelte.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { sessionStore } from '../../stores/sessionStore.svelte.ts';
  import { routeFleet, routeSessionDetail, routerStore } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import { ChevronLeft, Server, Trash2, Zap } from 'lucide-svelte';
  import type { AgentRegistration } from '@quicbaas/sdk';
  import { formatDateTime, formatRelative } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Derived: resolve agent from the store by route param
  // ---------------------------------------------------------------------------

  const agentId = $derived(routerStore.params['id'] ?? '');

  const agent = $derived(
    agentStore.agents.find((a) => a.id === agentId) ?? null,
  );

  // Recent sessions: match by backend name (best available correlation).
  const recentSessions = $derived(
    agent
      ? sessionStore.sessions
          .filter((s) => s.backend === agent.backend)
          .slice(0, 10)
      : [],
  );

  // Recent jobs: match by lead_backend config field.
  const recentJobs = $derived(
    agent
      ? orchestratorStore.jobs
          .filter((j) => j.config?.lead_backend === agent.backend)
          .slice(0, 10)
      : [],
  );

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let removeConfirmOpen = $state(false);
  let removing = $state(false);
  let rawJsonOpen = $state(false);
  let disableConfirmOpen = $state(false);
  let disabling = $state(false);

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function statusVariant(status: string): 'success' | 'error' | 'warning' | 'info' | 'ghost' {
    if (status === 'running' || status === 'idle') return 'success';
    if (status === 'error') return 'error';
    if (status === 'offline') return 'ghost';
    if (status === 'disabled') return 'error';
    return 'info';
  }

  const isDisabled = $derived(agent?.status === 'disabled');

  function sessionStatusVariant(status: string): 'success' | 'error' | 'warning' | 'info' | 'ghost' {
    if (status === 'active' || status === 'running') return 'success';
    if (status === 'closed' || status === 'terminated') return 'ghost';
    if (status === 'error') return 'error';
    return 'info';
  }

  function jobStateVariant(state: string): 'success' | 'error' | 'warning' | 'info' | 'primary' | 'ghost' {
    if (state === 'completed') return 'success';
    if (state === 'failed') return 'error';
    if (state === 'escalating') return 'warning';
    if (state === 'executing') return 'primary';
    return 'info';
  }

  const formatTime = (ts: number): string => formatDateTime(ts);
  const relativeTime = (ts: number): string => formatRelative(ts);

  function hasGpu(capabilities: string[] | undefined): boolean {
    return (capabilities ?? []).some((c) => c.toLowerCase().includes('gpu'));
  }

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  function handleBack(): void {
    routeFleet();
  }

  async function confirmRemove(): Promise<void> {
    if (!agent || removing) return;
    removing = true;
    try {
      const client = await getSDKClient();
      await client.orchestrator.removeAgent({
        tenant_id: orchestratorStore.activeTenantID,
        agent_id: agent.id,
      });
      showToast('success', `Agent "${agent.name}" removed`);
      removeConfirmOpen = false;
      routeFleet();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Remove failed: ${msg}`);
    } finally {
      removing = false;
    }
  }

  async function confirmDisable(): Promise<void> {
    if (!agent || disabling) return;
    disabling = true;
    try {
      const client = await getSDKClient();
      if (isDisabled) {
        await client.orchestrator.enableAgent({
          tenant_id: orchestratorStore.activeTenantID,
          agent_id: agent.id,
        });
        showToast('success', `Agent "${agent.name}" re-enabled`);
      } else {
        await client.orchestrator.disableAgent({
          tenant_id: orchestratorStore.activeTenantID,
          agent_id: agent.id,
        });
        showToast('success', `Agent "${agent.name}" disabled`);
      }
      disableConfirmOpen = false;
      void refreshAgents();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `${isDisabled ? 'Enable' : 'Disable'} failed: ${msg}`);
    } finally {
      disabling = false;
    }
  }
</script>

<!-- Remove confirm dialog -->
{#if removeConfirmOpen && agent}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Confirm remove agent"
    onkeydown={(e) => { if (e.key === 'Escape') { removeConfirmOpen = false; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Remove Agent?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Agent <span class="font-semibold">{agent.name || agent.id}</span> will be deregistered.
        Running jobs may be affected.
      </p>
      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { removeConfirmOpen = false; }}
          disabled={removing}
        >
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
    <div class="modal-backdrop" onclick={() => { removeConfirmOpen = false; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

{#if disableConfirmOpen && agent}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label={isDisabled ? 'Confirm enable agent' : 'Confirm disable agent'}
    onkeydown={(e) => { if (e.key === 'Escape') { disableConfirmOpen = false; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">{isDisabled ? 'Re-enable Agent?' : 'Disable Agent?'}</h3>
      {#if isDisabled}
        <p class="text-sm text-base-content/60 mt-2">
          Agent <span class="font-semibold">{agent.name || agent.id}</span> will be re-enabled and
          allowed to re-enroll. Existing OAuth2 tokens and mTLS pins must be re-issued.
        </p>
      {:else}
        <p class="text-sm text-base-content/60 mt-2">
          This will immediately revoke all OAuth2 tokens, remove mTLS pins, and close active streams
          for <span class="font-semibold">{agent.name || agent.id}</span>. The agent will need to
          re-enroll to come back online.
        </p>
      {/if}
      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { disableConfirmOpen = false; }}
          disabled={disabling}
        >
          Cancel
        </button>
        <button
          class={isDisabled ? 'btn btn-sm btn-success' : 'btn btn-sm btn-warning'}
          onclick={() => { void confirmDisable(); }}
          disabled={disabling}
        >
          {#if disabling}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          {isDisabled ? 'Enable' : 'Disable'}
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { disableConfirmOpen = false; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

{#if !agent}
  <div class="flex flex-col items-center justify-center h-full gap-3 text-base-content/30">
    <Server class="h-10 w-10" />
    <div class="text-sm">Agent not found or not yet loaded.</div>
    <button class="btn btn-ghost btn-sm gap-1" onclick={handleBack}>
      <ChevronLeft class="h-4 w-4" />
      Back to Fleet
    </button>
  </div>
{:else}
  <div class="p-6 space-y-5 max-w-5xl mx-auto">

    <!-- Header -->
    <div class="flex items-start gap-3 flex-wrap">
      <button
        class="btn btn-ghost btn-sm gap-1 shrink-0"
        onclick={handleBack}
        aria-label="Back to agent fleet"
      >
        <ChevronLeft class="h-4 w-4" />
        Back
      </button>

      <div class="flex-1 min-w-0">
        <h1 class="text-xl font-bold truncate">{agent.name || agent.id}</h1>
        <div class="flex items-center gap-2 mt-1 flex-wrap">
          <StatusBadge
            text={agent.status}
            variant={statusVariant(agent.status)}
            pulse={agent.status === 'running'}
          />
          <span class="text-xs text-base-content/40 font-mono">{agent.id}</span>
        </div>
      </div>

      <div class="flex items-center gap-2 shrink-0">
        <button
          class={isDisabled ? 'btn btn-xs btn-success gap-1' : 'btn btn-xs btn-warning gap-1'}
          onclick={() => { disableConfirmOpen = true; }}
          aria-label={isDisabled ? 'Enable agent' : 'Disable agent'}
        >
          {isDisabled ? 'Enable' : 'Disable'}
        </button>
        <button
          class="btn btn-xs btn-error gap-1"
          onclick={() => { removeConfirmOpen = true; }}
          aria-label="Remove agent"
        >
          <Trash2 class="h-3 w-3" />
          Remove
        </button>
      </div>
    </div>

    <!-- Identity card -->
    <div class="card bg-base-200 rounded-box p-5">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">Identity</div>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
        <div>
          <div class="text-xs text-base-content/40">Backend</div>
          <div class="badge badge-sm badge-primary mt-0.5">{agent.backend}</div>
        </div>
        {#if agent.model}
          <div>
            <div class="text-xs text-base-content/40">Model</div>
            <div class="font-mono text-xs mt-0.5">{agent.model}</div>
          </div>
        {/if}
        {#if agent.agent_type}
          <div>
            <div class="text-xs text-base-content/40">Agent Type</div>
            <div class="badge badge-sm badge-neutral mt-0.5">{agent.agent_type}</div>
          </div>
        {/if}
        <div>
          <div class="text-xs text-base-content/40">Host</div>
          <div class="font-mono text-xs mt-0.5">{agent.host}</div>
        </div>
        {#if agent.container_id}
          <div>
            <div class="text-xs text-base-content/40">Container</div>
            <div class="font-mono text-xs mt-0.5">{agent.container_id.slice(0, 12)}</div>
          </div>
        {/if}
        {#if agent.work_dir}
          <div>
            <div class="text-xs text-base-content/40">Work Dir</div>
            <div class="font-mono text-xs mt-0.5 break-all">{agent.work_dir}</div>
          </div>
        {/if}
        <div>
          <div class="text-xs text-base-content/40">Registered</div>
          <div class="text-xs mt-0.5">{formatTime(agent.registered_at)}</div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Last Seen</div>
          <div class="text-xs mt-0.5">{relativeTime(agent.last_seen)}</div>
        </div>
        <div>
          <div class="text-xs text-base-content/40">Tenant</div>
          <div class="font-mono text-xs mt-0.5">{agent.tenant_id}</div>
        </div>
      </div>
    </div>

    <!-- Capability chips -->
    {#if agent.capabilities && agent.capabilities.length > 0}
      <div class="card bg-base-200 rounded-box p-4">
        <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">Capabilities</div>
        <div class="flex flex-wrap gap-2" role="list" aria-label="Agent capabilities">
          {#each agent.capabilities as cap}
            <span
              class="badge badge-sm {cap.toLowerCase().includes('gpu') ? 'badge-warning' : 'badge-outline'}"
              role="listitem"
              aria-label="Capability: {cap}"
            >
              {#if cap.toLowerCase().includes('gpu')}
                <Zap class="h-2.5 w-2.5 mr-0.5" />
              {/if}
              {cap}
            </span>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Metrics card -->
    <div class="card bg-base-200 rounded-box p-4">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">Metrics</div>
      {#if agent.metadata && (agent.metadata['total_prompts'] || agent.metadata['total_calls'] || agent.metadata['avg_latency_ms'])}
        <div class="grid grid-cols-3 gap-4">
          <div class="stat bg-base-300 rounded-box p-3">
            <div class="stat-title text-xs">Total Prompts</div>
            <div class="stat-value text-xl">{agent.metadata['total_prompts'] ?? '—'}</div>
          </div>
          <div class="stat bg-base-300 rounded-box p-3">
            <div class="stat-title text-xs">Inference Calls</div>
            <div class="stat-value text-xl">{agent.metadata['total_calls'] ?? '—'}</div>
          </div>
          <div class="stat bg-base-300 rounded-box p-3">
            <div class="stat-title text-xs">Avg Latency</div>
            <div class="stat-value text-xl">{agent.metadata['avg_latency_ms'] ? `${agent.metadata['avg_latency_ms']}ms` : '—'}</div>
          </div>
        </div>
      {:else}
        <div class="text-sm text-base-content/40">Metrics unavailable — no telemetry reported by this agent yet.</div>
      {/if}
    </div>

    <!-- Recent sessions -->
    <div class="card bg-base-200 rounded-box p-4">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">
        Recent Sessions ({recentSessions.length})
      </div>
      {#if recentSessions.length === 0}
        <div class="text-sm text-base-content/30">No sessions found for this agent's backend</div>
      {:else}
        <div class="overflow-x-auto rounded-box border border-base-300/40">
          <table class="table table-xs w-full">
            <thead>
              <tr class="text-base-content/40 text-xs">
                <th>Session ID</th>
                <th>Status</th>
                <th>Created</th>
                <th>Last Active</th>
              </tr>
            </thead>
            <tbody>
              {#each recentSessions as session (session.session_id)}
                <tr
                  class="cursor-pointer hover:bg-base-300/40 transition-colors"
                  onclick={() => routeSessionDetail(session.session_id)}
                  onkeydown={(e) => { if (e.key === 'Enter') routeSessionDetail(session.session_id); }}
                  tabindex="0"
                  role="button"
                  aria-label="Open session {session.session_id.slice(0, 8)}"
                >
                  <td class="font-mono text-xs">{session.session_id.slice(0, 16)}…</td>
                  <td>
                    <StatusBadge
                      text={session.status}
                      variant={sessionStatusVariant(session.status)}
                      pulse={session.status === 'active'}
                    />
                  </td>
                  <td class="text-xs text-base-content/50">{formatTime(session.created_at)}</td>
                  <td class="text-xs text-base-content/50">{formatTime(session.last_active)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

    <!-- Recent jobs -->
    <div class="card bg-base-200 rounded-box p-4">
      <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-3">
        Recent Jobs ({recentJobs.length})
      </div>
      {#if recentJobs.length === 0}
        <div class="text-sm text-base-content/30">No jobs found for this agent's backend</div>
      {:else}
        <div class="overflow-x-auto rounded-box border border-base-300/40">
          <table class="table table-xs w-full">
            <thead>
              <tr class="text-base-content/40 text-xs">
                <th>Job ID</th>
                <th>Description</th>
                <th>State</th>
                <th>Started</th>
              </tr>
            </thead>
            <tbody>
              {#each recentJobs as job (job.job_id)}
                <tr class="hover:bg-base-300/40 transition-colors">
                  <td class="font-mono text-xs">{job.job_id.slice(0, 12)}…</td>
                  <td class="text-xs max-w-xs truncate">{job.description || '—'}</td>
                  <td>
                    <StatusBadge
                      text={job.state}
                      variant={jobStateVariant(job.state)}
                      pulse={job.state === 'executing'}
                    />
                  </td>
                  <td class="text-xs text-base-content/50">{formatTime(job.started_at)}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    </div>

    <!-- Raw JSON drawer -->
    <div class="rounded-box border border-base-300/50 overflow-hidden">
      <button
        class="w-full flex items-center justify-between px-4 py-2 bg-base-200/80 text-xs font-semibold text-base-content/60 hover:bg-base-300/60 transition-colors"
        onclick={() => { rawJsonOpen = !rawJsonOpen; }}
        aria-expanded={rawJsonOpen}
        aria-controls="agent-raw-json-panel"
      >
        <span>Raw Agent State (debug)</span>
        <span>{rawJsonOpen ? '▲' : '▼'}</span>
      </button>
      {#if rawJsonOpen}
        <div id="agent-raw-json-panel">
          <pre class="bg-base-300 p-4 text-xs overflow-auto max-h-80 whitespace-pre-wrap break-all font-mono">{JSON.stringify(agent, null, 2)}</pre>
        </div>
      {/if}
    </div>

  </div>
{/if}
