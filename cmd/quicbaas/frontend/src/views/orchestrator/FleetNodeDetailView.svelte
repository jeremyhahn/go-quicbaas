<!--
  FleetNodeDetailView renders a premium detail page for a single registered node.
  Accessible via: #/fleet/{id}

  Layout:
  - Header: node name + status pill + ChevronLeft back + Remove confirm action
  - Identity card: backend, model, host, registered_at, last_seen
  - Capability chips: all capabilities, gpu highlighted
  - Metrics card: metrics if available, otherwise unavailable notice
  - Sessions table: the sessions that ran on THIS node, read from the server
  - Jobs table: the jobs that ran on THIS node, read from the server
  - Raw JSON drawer: collapsible debug panel
-->
<script lang="ts">
  import { fleetStore, refreshFleet } from '../../stores/fleetStore.svelte.ts';
  import { orchestratorStore, type JobStatus } from '../../stores/orchestrator.svelte.ts';
  import { listReturnHash, navigateTo, routeFleet, routeSessionDetail, routerStore } from '../../stores/router.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import StatusBadge from '../../components/core/StatusBadge.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column, PageRequest } from '../../lib/dataTable.ts';
  import { ChevronLeft, Server, Trash2, Zap } from 'lucide-svelte';
  import type { FleetNode, SessionStatusResponse } from '@quicbaas/sdk';
  import { formatDateTime, formatRelative } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Derived: resolve node from the store by route param
  // ---------------------------------------------------------------------------

  const nodeId = $derived(routerStore.params['id'] ?? '');

  const node = $derived(
    fleetStore.nodes.find((a) => a.id === nodeId) ?? null,
  );

  // ---------------------------------------------------------------------------
  // What ran HERE
  //
  // Both panels used to sieve the store's collection by BACKEND — every
  // session on claude, whichever host it ran on — which is a different
  // question from "what ran on this node" and answered it wrongly whenever
  // two nodes shared a backend, which is the normal case. The listings take
  // `agent_id` now, so each panel asks the server for exactly its own node's
  // records, paged, and the store is left to the pages that own it.
  // ---------------------------------------------------------------------------

  async function loadNodeSessions(req: PageRequest): Promise<unknown> {
    if (nodeId === '') return { items: [], total: 0, limit: req.limit, offset: 0 };
    const client = await getSDKClient();
    return client.orchestrator.listSessionsPage(orchestratorStore.activeTenantID, {
      limit: req.limit,
      offset: req.offset,
      sort: req.sort,
      q: req.q,
      agent_id: nodeId,
    });
  }

  async function loadNodeJobs(req: PageRequest): Promise<unknown> {
    if (nodeId === '') return { items: [], total: 0, limit: req.limit, offset: 0 };
    const client = await getSDKClient();
    return client.orchestrator.listJobsPage(orchestratorStore.activeTenantID, {
      limit: req.limit,
      offset: req.offset,
      sort: req.sort,
      q: req.q,
      agent_id: nodeId,
    });
  }

  const NODE_SESSION_COLUMNS: Column<SessionStatusResponse>[] = [
    { key: 'session_id', label: 'Session ID', sortable: true, searchable: true },
    { key: 'status', label: 'Status', sortable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'last_active', label: 'Last Active', sortable: true },
  ];

  const NODE_JOB_COLUMNS: Column<JobStatus>[] = [
    { key: 'job_id', label: 'Job ID', sortable: true, searchable: true },
    { key: 'description', label: 'Description', sortable: true, searchable: true },
    { key: 'state', label: 'State', sortable: true },
    { key: 'started_at', label: 'Started', sortable: true },
  ];

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

  const isDisabled = $derived(node?.status === 'disabled');

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

  /** handleBack returns to the fleet as the operator left it, narrowed. */
  function handleBack(): void {
    navigateTo(listReturnHash('/fleet'));
  }

  async function confirmRemove(): Promise<void> {
    if (!node || removing) return;
    removing = true;
    try {
      const client = await getSDKClient();
      await client.orchestrator.removeFleetNode({
        tenant_id: orchestratorStore.activeTenantID,
        node_id: node.id,
      });
      showToast('success', `Fleet node "${node.name}" removed`);
      removeConfirmOpen = false;
      // Drop the removed node from the fleet store before navigating back, so
      // the grid doesn't keep showing its card until the next poll tick
      // (mirrors confirmDisable, which refreshes after disable/enable).
      void refreshFleet();
      routeFleet();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Remove failed: ${msg}`);
    } finally {
      removing = false;
    }
  }

  async function confirmDisable(): Promise<void> {
    if (!node || disabling) return;
    disabling = true;
    try {
      const client = await getSDKClient();
      if (isDisabled) {
        await client.orchestrator.enableFleetNode({
          tenant_id: orchestratorStore.activeTenantID,
          node_id: node.id,
        });
        showToast('success', `Fleet node "${node.name}" re-enabled`);
      } else {
        await client.orchestrator.disableFleetNode({
          tenant_id: orchestratorStore.activeTenantID,
          node_id: node.id,
        });
        showToast('success', `Fleet node "${node.name}" disabled`);
      }
      disableConfirmOpen = false;
      void refreshFleet();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `${isDisabled ? 'Enable' : 'Disable'} failed: ${msg}`);
    } finally {
      disabling = false;
    }
  }
</script>

<!-- Remove confirm dialog -->
{#if removeConfirmOpen && node}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Confirm remove fleet node"
    onkeydown={(e) => { if (e.key === 'Escape') { removeConfirmOpen = false; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Remove fleet node?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Fleet node <span class="font-semibold">{node.name || node.id}</span> will be deregistered.
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

{#if disableConfirmOpen && node}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label={isDisabled ? 'Confirm enable fleet node' : 'Confirm disable fleet node'}
    onkeydown={(e) => { if (e.key === 'Escape') { disableConfirmOpen = false; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">{isDisabled ? 'Re-enable fleet node?' : 'Disable fleet node?'}</h3>
      {#if isDisabled}
        <p class="text-sm text-base-content/60 mt-2">
          Fleet node <span class="font-semibold">{node.name || node.id}</span> will be re-enabled and
          allowed to re-enroll. Existing OAuth2 tokens and mTLS pins must be re-issued.
        </p>
      {:else}
        <p class="text-sm text-base-content/60 mt-2">
          This will immediately revoke all OAuth2 tokens, remove mTLS pins, and close active streams
          for <span class="font-semibold">{node.name || node.id}</span>. The node will need to
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

{#if !node}
  <div class="flex flex-col items-center justify-center h-full gap-3 text-base-content/30">
    <Server class="h-10 w-10" />
    <div class="text-sm">Fleet node not found or not yet loaded.</div>
    <button class="btn btn-ghost btn-sm gap-1" onclick={handleBack}>
      <ChevronLeft class="h-4 w-4" />
      Back to Fleet
    </button>
  </div>
{:else}
  <div class="page page-narrow">

    <!-- Header -->
    <div class="flex flex-wrap items-start gap-x-6 gap-y-3">
      <div class="min-w-0 flex-1">
        <button
          class="-ml-2 mb-1 inline-flex items-center gap-1 rounded-field px-2 py-0.5 text-sm text-primary hover:bg-base-200"
          onclick={handleBack}
          aria-label="Back to fleet"
        >
          <ChevronLeft class="h-4 w-4" />
          Fleet
        </button>
        <h1 class="display truncate text-[1.75rem] text-base-content">{node.name || node.id}</h1>
        <div class="mt-2 flex flex-wrap items-center gap-2">
          <StatusBadge
            text={node.status}
            variant={statusVariant(node.status)}
            pulse={node.status === 'running'}
          />
          <span class="font-mono text-xs text-base-content/60">{node.id}</span>
        </div>
      </div>

      <div class="flex shrink-0 items-center gap-2">
        <button
          class={isDisabled ? 'btn btn-sm btn-outline btn-success gap-1' : 'btn btn-sm btn-outline btn-warning gap-1'}
          onclick={() => { disableConfirmOpen = true; }}
          aria-label={isDisabled ? 'Enable fleet node' : 'Disable fleet node'}
        >
          {isDisabled ? 'Enable' : 'Disable'}
        </button>
        <button
          class="btn btn-sm btn-outline btn-error gap-1"
          onclick={() => { removeConfirmOpen = true; }}
          aria-label="Remove fleet node"
        >
          <Trash2 class="h-4 w-4" />
          Remove
        </button>
      </div>
    </div>

    <!-- Identity card -->
    <div class="card bg-base-200 rounded-box p-5">
      <div class="section-title mb-3">Identity</div>
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
        <div>
          <div class="text-xs font-medium text-base-content/60">Backend</div>
          <div class="badge badge-sm badge-primary mt-0.5">{node.backend}</div>
        </div>
        {#if node.model}
          <div>
            <div class="text-xs font-medium text-base-content/60">Model</div>
            <div class="mt-0.5 font-mono text-[0.8125rem]">{node.model}</div>
          </div>
        {/if}
        {#if node.node_type}
          <div>
            <div class="text-xs font-medium text-base-content/60">Node type</div>
            <div class="badge badge-sm badge-neutral mt-0.5">{node.node_type}</div>
          </div>
        {/if}
        <div>
          <div class="text-xs font-medium text-base-content/60">Host</div>
          <div class="mt-0.5 font-mono text-[0.8125rem]">{node.host}</div>
        </div>
        {#if node.work_dir}
          <div>
            <div class="text-xs font-medium text-base-content/60">Work Dir</div>
            <div class="font-mono text-xs mt-0.5 break-all">{node.work_dir}</div>
          </div>
        {/if}
        <div>
          <div class="text-xs font-medium text-base-content/60">Registered</div>
          <div class="mt-0.5 text-sm">{formatTime(node.registered_at)}</div>
        </div>
        <div>
          <div class="text-xs font-medium text-base-content/60">Last Seen</div>
          <div class="mt-0.5 text-sm">{relativeTime(node.last_seen)}</div>
        </div>
        <div>
          <div class="text-xs font-medium text-base-content/60">Tenant</div>
          <div class="mt-0.5 font-mono text-[0.8125rem]">{node.tenant_id}</div>
        </div>
      </div>
    </div>

    <!-- Capability chips -->
    {#if node.capabilities && node.capabilities.length > 0}
      <div class="card bg-base-200 rounded-box p-4">
        <div class="section-title mb-3">Capabilities</div>
        <div class="flex flex-wrap gap-2" role="list" aria-label="Fleet node capabilities">
          {#each node.capabilities as cap}
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
      <div class="section-title mb-3">Metrics</div>
      {#if node.metadata && (node.metadata['total_prompts'] || node.metadata['total_calls'] || node.metadata['avg_latency_ms'])}
        <div class="grid grid-cols-3 gap-4">
          <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
            <div class="stat-title text-xs font-medium text-base-content/60">Total Prompts</div>
            <div class="stat-value text-xl">{node.metadata['total_prompts'] ?? '—'}</div>
          </div>
          <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
            <div class="stat-title text-xs font-medium text-base-content/60">Inference Calls</div>
            <div class="stat-value text-xl">{node.metadata['total_calls'] ?? '—'}</div>
          </div>
          <div class="stat rounded-field border border-base-300/80 bg-base-100 px-4 py-3">
            <div class="stat-title text-xs font-medium text-base-content/60">Avg Latency</div>
            <div class="stat-value text-xl">{node.metadata['avg_latency_ms'] ? `${node.metadata['avg_latency_ms']}ms` : '—'}</div>
          </div>
        </div>
      {:else}
        <div class="text-sm text-base-content/40">Metrics unavailable — no telemetry reported by this node yet.</div>
      {/if}
    </div>

    <!-- Sessions that ran on this node -->
    <div class="card bg-base-200 rounded-box p-4">
      <div class="flex items-center justify-between gap-2 mb-3">
        <div class="section-title">
          Sessions on this node
        </div>
        <a
          class="link link-primary text-xs"
          href={`#/sessions?sessions_node=${encodeURIComponent(nodeId)}`}
        >Open in Sessions</a>
      </div>
      <DataTable
        columns={NODE_SESSION_COLUMNS}
        load={loadNodeSessions}
        itemsKey="items"
        reloadKey={nodeId}
        rowKey={(session: SessionStatusResponse) => session.session_id}
        ariaLabel="Sessions on this node"
        searchPlaceholder="Search sessions..."
        searchLabel="Search node sessions"
        noMatchText="No sessions match the search."
        emptyTitle="No sessions have run on this node"
      >
        {#snippet row(session: SessionStatusResponse)}
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
        {/snippet}
    </DataTable>
    </div>

    <!-- Jobs that ran on this node -->
    <div class="card bg-base-200 rounded-box p-4">
      <div class="flex items-center justify-between gap-2 mb-3">
        <div class="section-title">
          Jobs on this node
        </div>
        <a
          class="link link-primary text-xs"
          href={`#/jobs?jobs_node=${encodeURIComponent(nodeId)}`}
        >Open in Jobs</a>
      </div>
      <DataTable
        columns={NODE_JOB_COLUMNS}
        load={loadNodeJobs}
        itemsKey="items"
        reloadKey={nodeId}
        rowKey={(job: JobStatus) => job.job_id}
        ariaLabel="Jobs on this node"
        searchPlaceholder="Search jobs..."
        searchLabel="Search node jobs"
        noMatchText="No jobs match the search."
        emptyTitle="No jobs have run on this node"
      >
        {#snippet row(job: JobStatus)}
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
        {/snippet}
    </DataTable>
    </div>

    <!-- Raw JSON drawer -->
    <div class="rounded-box border border-base-300/50 overflow-hidden">
      <button
        class="w-full flex items-center justify-between px-4 py-2 bg-base-200/80 text-xs font-semibold text-base-content/60 hover:bg-base-300/60 transition-colors"
        onclick={() => { rawJsonOpen = !rawJsonOpen; }}
        aria-expanded={rawJsonOpen}
        aria-controls="agent-raw-json-panel"
      >
        <span>Raw node state (debug)</span>
        <span>{rawJsonOpen ? '▲' : '▼'}</span>
      </button>
      {#if rawJsonOpen}
        <div id="agent-raw-json-panel">
          <pre class="bg-base-300 p-4 text-xs overflow-auto max-h-80 whitespace-pre-wrap break-all font-mono">{JSON.stringify(node, null, 2)}</pre>
        </div>
      {/if}
    </div>

  </div>
{/if}
