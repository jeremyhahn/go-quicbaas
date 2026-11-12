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
  IAMAuditView is the top-level Security > IAM Audit route component.

  It reads the selected tenant's IAM audit trail through `client.iam`:
    - a server-paged table (50 a page) from listAuditEvents, with filters for
      event type, category, outcome, actor, resource and a time range;
    - a detail dialog from getAuditEvent for the clicked row;
    - a confirmed "Delete events before" that prunes through
      deleteAuditEventsBefore and reloads.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type { IAMAuditEvent, IAMTenant, ListIAMAuditEventsOptions } from '@quicbaas/sdk';
  import { ScrollText } from 'lucide-svelte';

  const PAGE_SIZE = 50;

  type Filters = {
    eventType: string;
    category: string;
    outcome: string;
    actorID: string;
    resourceID: string;
    from: string;
    to: string;
  };

  const EMPTY_FILTERS: Filters = {
    eventType: '',
    category: '',
    outcome: '',
    actorID: '',
    resourceID: '',
    from: '',
    to: '',
  };

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let tenants: IAMTenant[] = $state([]);
  let selectedTenantID = $state('');
  let tenantsLoading = $state(false);
  let tenantsError: string | null = $state(null);

  let events: IAMAuditEvent[] = $state([]);
  let total = $state(0);
  let offset = $state(0);
  let loading = $state(false);
  let error: string | null = $state(null);

  // The form's draft and the filters the table was last loaded with.
  let draft: Filters = $state({ ...EMPTY_FILTERS });
  let applied: Filters = $state({ ...EMPTY_FILTERS });

  let detail: IAMAuditEvent | null = $state(null);
  let detailOpen = $state(false);
  let detailLoading = $state(false);
  let detailError: string | null = $state(null);

  let pruneDate = $state('');
  let pendingPrune: string | null = $state(null);
  let pruning = $state(false);

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  /** Converts a datetime-local value (local time) to RFC 3339 UTC. */
  function localToRFC3339(value: string): string | undefined {
    if (!value) return undefined;
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return undefined;
    return d.toISOString();
  }

  /** Converts a date input value (YYYY-MM-DD) to RFC 3339 UTC midnight. */
  function dateToRFC3339(value: string): string {
    return `${value}T00:00:00Z`;
  }

  function trimmed(value: string): string | undefined {
    const v = value.trim();
    return v === '' ? undefined : v;
  }

  function buildOptions(tenantID: string, f: Filters, off: number): ListIAMAuditEventsOptions {
    const opts: ListIAMAuditEventsOptions = { tenantID, limit: PAGE_SIZE, offset: off };
    const eventType = trimmed(f.eventType);
    if (eventType) opts.eventType = eventType;
    const category = trimmed(f.category);
    if (category) opts.category = category;
    if (f.outcome) opts.outcome = f.outcome;
    const actorID = trimmed(f.actorID);
    if (actorID) opts.actorID = actorID;
    const resourceID = trimmed(f.resourceID);
    if (resourceID) opts.resourceID = resourceID;
    const startTime = localToRFC3339(f.from);
    if (startTime) opts.startTime = startTime;
    const endTime = localToRFC3339(f.to);
    if (endTime) opts.endTime = endTime;
    return opts;
  }

  function formatDateTime(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  function actorLabel(e: IAMAuditEvent): string {
    return e.actor?.username || e.actor?.user_id || e.actor?.service_identity || '—';
  }

  function resourceLabel(e: IAMAuditEvent): string {
    if (!e.resource) return '—';
    return `${e.resource.type}/${e.resource.id}`;
  }

  function outcomeBadge(outcome: string): string {
    if (outcome === 'success') return 'badge-success';
    if (outcome === 'failure') return 'badge-error';
    if (outcome === 'denied') return 'badge-warning';
    return 'badge-ghost';
  }

  function tenantLabel(id: string): string {
    const t = tenants.find((x) => x.id === id);
    return t ? t.name || t.id : id;
  }

  function formatJSON(value: unknown): string {
    return JSON.stringify(value, null, 2);
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadTenants(): Promise<void> {
    tenantsLoading = true;
    tenantsError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listTenants();
      const seen = new Set<string>();
      tenants = resp.tenants.filter((t) => {
        if (seen.has(t.id)) return false;
        seen.add(t.id);
        return true;
      });
      if (selectedTenantID === '' && tenants.length > 0) {
        selectedTenantID = tenants[0]?.id ?? '';
      }
    } catch (e) {
      tenantsError = e instanceof Error ? e.message : 'Failed to load tenants';
    } finally {
      tenantsLoading = false;
    }
  }

  async function loadEvents(): Promise<void> {
    const tenantID = selectedTenantID;
    if (tenantID === '') {
      events = [];
      total = 0;
      return;
    }
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listAuditEvents(buildOptions(tenantID, applied, offset));
      events = resp.events ?? [];
      total = resp.total ?? 0;
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load audit events';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    untrack(() => { void loadTenants(); });
  });

  // Changing the tenant reloads from the first page.
  $effect(() => {
    void selectedTenantID;
    untrack(() => {
      offset = 0;
      void loadEvents();
    });
  });

  function applyFilters(event: SubmitEvent): void {
    event.preventDefault();
    applied = { ...draft };
    offset = 0;
    void loadEvents();
  }

  function resetFilters(): void {
    draft = { ...EMPTY_FILTERS };
    applied = { ...EMPTY_FILTERS };
    offset = 0;
    void loadEvents();
  }

  function previousPage(): void {
    offset = Math.max(0, offset - PAGE_SIZE);
    void loadEvents();
  }

  function nextPage(): void {
    offset = offset + PAGE_SIZE;
    void loadEvents();
  }

  // ---------------------------------------------------------------------------
  // Detail
  // ---------------------------------------------------------------------------

  async function openDetail(e: IAMAuditEvent): Promise<void> {
    detailOpen = true;
    detail = null;
    detailError = null;
    detailLoading = true;
    try {
      const client = await getSDKClient();
      detail = await client.iam.getAuditEvent(e.id, selectedTenantID || undefined);
    } catch (err) {
      detailError = err instanceof Error ? err.message : 'Failed to load the audit event';
    } finally {
      detailLoading = false;
    }
  }

  function closeDetail(): void {
    detailOpen = false;
    detail = null;
    detailError = null;
  }

  // ---------------------------------------------------------------------------
  // Prune
  // ---------------------------------------------------------------------------

  function requestPrune(event: SubmitEvent): void {
    event.preventDefault();
    if (!pruneDate || selectedTenantID === '') return;
    pendingPrune = pruneDate;
  }

  async function confirmPrune(): Promise<void> {
    if (!pendingPrune) return;
    pruning = true;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.deleteAuditEventsBefore(
        dateToRFC3339(pendingPrune),
        selectedTenantID || undefined,
      );
      showToast('success', `Deleted ${resp.deleted_count} audit events`);
      pendingPrune = null;
      offset = 0;
      void loadEvents();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Deleting audit events failed');
    } finally {
      pruning = false;
    }
  }

  const pageStart = $derived(total === 0 ? 0 : offset + 1);
  const pageEnd = $derived(Math.min(offset + events.length, total));
</script>

<div class="p-6 space-y-6">
  <PageHeader title="IAM Audit" subtitle="Audit events recorded by the IAM service for a tenant" />

  {#if tenantsError}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{tenantsError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadTenants(); }}>Retry</button>
    </div>
  {/if}

  <div class="form-control max-w-sm">
    <label class="label label-text" for="iam-audit-tenant-select">Tenant</label>
    <select
      id="iam-audit-tenant-select"
      class="select select-bordered select-sm"
      bind:value={selectedTenantID}
      disabled={tenantsLoading || tenants.length === 0}
    >
      {#if tenants.length === 0}
        <option value="">No tenants available</option>
      {/if}
      {#each tenants as tenant (tenant.id)}
        <option value={tenant.id}>{tenant.name || tenant.id}</option>
      {/each}
    </select>
  </div>

  <!-- Filters -->
  <form class="card bg-base-200 p-4 space-y-2" aria-label="Audit filters" onsubmit={applyFilters}>
    <div class="flex flex-wrap gap-2 items-end">
      <input class="input input-bordered input-sm" placeholder="Event type" aria-label="Event type" bind:value={draft.eventType} />
      <input class="input input-bordered input-sm" placeholder="Category" aria-label="Category" bind:value={draft.category} />
      <select class="select select-bordered select-sm" aria-label="Outcome" bind:value={draft.outcome}>
        <option value="">Any outcome</option>
        <option value="success">success</option>
        <option value="failure">failure</option>
        <option value="denied">denied</option>
      </select>
      <input class="input input-bordered input-sm" placeholder="Actor ID" aria-label="Actor ID" bind:value={draft.actorID} />
      <input class="input input-bordered input-sm" placeholder="Resource ID" aria-label="Resource ID" bind:value={draft.resourceID} />
      <label class="flex items-center gap-1 text-sm">
        <span>From</span>
        <input type="datetime-local" class="input input-bordered input-sm" aria-label="From" bind:value={draft.from} />
      </label>
      <label class="flex items-center gap-1 text-sm">
        <span>To</span>
        <input type="datetime-local" class="input input-bordered input-sm" aria-label="To" bind:value={draft.to} />
      </label>
      <button type="submit" class="btn btn-primary btn-sm" disabled={selectedTenantID === ''}>Apply</button>
      <button type="button" class="btn btn-ghost btn-sm" onclick={resetFilters}>Reset</button>
    </div>
  </form>

  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadEvents(); }}>Retry</button>
    </div>
  {/if}

  {#if loading}
    <div class="overflow-x-auto rounded-box">
      <table class="table table-sm" aria-label="Audit events loading">
        <thead>
          <tr><th>Time</th><th>Event type</th><th>Category</th><th>Actor</th><th>Resource</th><th>Action</th><th>Outcome</th></tr>
        </thead>
        <tbody>
          {#each [1, 2, 3] as i (i)}
            <tr>
              {#each [1, 2, 3, 4, 5, 6, 7] as j (j)}
                <td><div class="skeleton h-4 w-16"></div></td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {:else if !error}
    {#if events.length === 0}
      <EmptyState
        icon={ScrollText}
        title="No audit events"
        description={selectedTenantID === '' ? 'Select a tenant to see its audit events.' : 'No audit events match the filters.'}
      />
    {:else}
      <div class="overflow-x-auto rounded-box">
        <table class="table table-sm" aria-label="Audit events">
          <thead>
            <tr><th>Time</th><th>Event type</th><th>Category</th><th>Actor</th><th>Resource</th><th>Action</th><th>Outcome</th></tr>
          </thead>
          <tbody>
            {#each events as e (e.id)}
              <tr class="hover cursor-pointer" onclick={() => { void openDetail(e); }}>
                <td class="text-xs">
                  <button
                    type="button"
                    class="link link-hover"
                    aria-label={`View audit event ${e.id}`}
                    onclick={(ev) => { ev.stopPropagation(); void openDetail(e); }}
                  >
                    {formatDateTime(e.timestamp)}
                  </button>
                </td>
                <td class="font-mono text-xs">{e.event_type}</td>
                <td class="text-xs">{e.category}</td>
                <td class="text-xs">{actorLabel(e)}</td>
                <td class="font-mono text-xs">{resourceLabel(e)}</td>
                <td class="text-xs">{e.action}</td>
                <td><span class={`badge badge-sm ${outcomeBadge(e.outcome)}`}>{e.outcome}</span></td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <div class="flex items-center justify-between text-sm">
      <span>{pageStart}–{pageEnd} of {total}</span>
      <div class="join">
        <button class="btn btn-sm join-item" disabled={offset === 0} onclick={previousPage}>Previous</button>
        <button class="btn btn-sm join-item" disabled={offset + PAGE_SIZE >= total} onclick={nextPage}>Next</button>
      </div>
    </div>
  {/if}

  <!-- Prune -->
  <form class="card bg-base-200 p-4 flex flex-row flex-wrap gap-2 items-center" aria-label="Delete events before" onsubmit={requestPrune}>
    <label class="flex items-center gap-1 text-sm">
      <span>Delete events before</span>
      <input type="date" class="input input-bordered input-sm" aria-label="Delete before date" bind:value={pruneDate} />
    </label>
    <button type="submit" class="btn btn-error btn-sm" disabled={!pruneDate || selectedTenantID === ''}>
      Delete events before
    </button>
  </form>
</div>

<!-- Detail dialog -->
{#if detailOpen}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Audit event detail">
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-2">Audit Event</h3>
      {#if detailLoading}
        <div class="skeleton h-24 w-full"></div>
      {:else if detailError}
        <div class="alert alert-error text-sm" role="alert"><span>{detailError}</span></div>
      {:else if detail}
        <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
          <dt class="font-semibold">ID</dt><dd class="font-mono text-xs">{detail.id}</dd>
          <dt class="font-semibold">Tenant</dt><dd class="font-mono text-xs">{detail.tenant_id}</dd>
          <dt class="font-semibold">Time</dt><dd>{formatDateTime(detail.timestamp)}</dd>
          <dt class="font-semibold">Event type</dt><dd class="font-mono text-xs">{detail.event_type}</dd>
          <dt class="font-semibold">Category</dt><dd>{detail.category}</dd>
          <dt class="font-semibold">Action</dt><dd>{detail.action}</dd>
          <dt class="font-semibold">Outcome</dt><dd>{detail.outcome}</dd>
          <dt class="font-semibold">IP address</dt><dd>{detail.ip_address || '—'}</dd>
          <dt class="font-semibold">User agent</dt><dd class="break-all">{detail.user_agent || '—'}</dd>
          <dt class="font-semibold">Impersonation session</dt><dd class="font-mono text-xs">{detail.impersonation_session_id || '—'}</dd>
          <dt class="font-semibold">Correlation ID</dt><dd class="font-mono text-xs">{detail.correlation_id || '—'}</dd>
        </dl>

        <h4 class="font-semibold mt-4 mb-1">Actor</h4>
        {#if detail.actor}
          {#if detail.actor.impersonating_user_id || detail.actor.impersonating_username}
            <div class="alert alert-warning text-sm py-2 mb-2">
              <span>Impersonated by {detail.actor.impersonating_username || detail.actor.impersonating_user_id}</span>
            </div>
          {/if}
          <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
            <dt class="font-semibold">User ID</dt><dd class="font-mono text-xs">{detail.actor.user_id || '—'}</dd>
            <dt class="font-semibold">Username</dt><dd>{detail.actor.username || '—'}</dd>
            <dt class="font-semibold">Email</dt><dd>{detail.actor.email || '—'}</dd>
            <dt class="font-semibold">Session ID</dt><dd class="font-mono text-xs">{detail.actor.session_id || '—'}</dd>
            <dt class="font-semibold">Client ID</dt><dd class="font-mono text-xs">{detail.actor.client_id || '—'}</dd>
            <dt class="font-semibold">Service identity</dt><dd class="font-mono text-xs">{detail.actor.service_identity || '—'}</dd>
            <dt class="font-semibold">Scopes</dt><dd>{detail.actor.scopes?.join(', ') || '—'}</dd>
            <dt class="font-semibold">Roles</dt><dd>{detail.actor.roles?.join(', ') || '—'}</dd>
          </dl>
        {:else}
          <p class="text-sm text-base-content/60">No actor recorded.</p>
        {/if}

        <h4 class="font-semibold mt-4 mb-1">Resource</h4>
        {#if detail.resource}
          <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
            <dt class="font-semibold">Type</dt><dd>{detail.resource.type}</dd>
            <dt class="font-semibold">ID</dt><dd class="font-mono text-xs">{detail.resource.id}</dd>
            <dt class="font-semibold">Name</dt><dd>{detail.resource.name || '—'}</dd>
          </dl>
        {:else}
          <p class="text-sm text-base-content/60">No resource recorded.</p>
        {/if}

        <h4 class="font-semibold mt-4 mb-1">Details</h4>
        {#if detail.details !== undefined && detail.details !== null}
          <pre class="bg-base-200 rounded p-2 text-xs overflow-x-auto" aria-label="Event details">{formatJSON(detail.details)}</pre>
        {:else}
          <p class="text-sm text-base-content/60">No details.</p>
        {/if}
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeDetail}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeDetail} onkeydown={closeDetail}></div>
  </div>
{/if}

<!-- Prune confirmation dialog -->
{#if pendingPrune !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Audit Events</h3>
      <p class="text-sm text-base-content/70">
        Delete every audit event before <span class="font-semibold">{pendingPrune}</span> for tenant
        <span class="font-semibold">{tenantLabel(selectedTenantID)}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { pendingPrune = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={pruning} onclick={confirmPrune}>
          {pruning ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { pendingPrune = null; }}
      onkeydown={() => { pendingPrune = null; }}
    ></div>
  </div>
{/if}
