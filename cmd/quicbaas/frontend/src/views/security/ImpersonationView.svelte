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
  ImpersonationView is the top-level Security > Impersonation route component.

  It records and ends delegated-admin impersonation sessions through
  `client.iam`:
    - Sessions: list with an "Active only" toggle and an admin user filter.
    - Start: a tenant, a target user, a required reason and a duration,
      confirmed before the session is recorded.
    - End: each active session can be ended after a confirmation dialog.
    - Sessions for user: the sessions that targeted one user.

  The page never switches the console's own identity and stores no token.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type { IAMTenant, IAMUser, ImpersonationSession } from '@quicbaas/sdk';
  import { UserCog } from 'lucide-svelte';

  type StartRequest = { target_user_id: string; tenant_id: string; reason: string; duration: string };
  type Pending = { kind: 'start'; req: StartRequest } | { kind: 'end'; session: ImpersonationSession };

  const DURATIONS = ['15m', '30m', '1h', '4h'];

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let sessions: ImpersonationSession[] = $state([]);
  let total = $state(0);
  let loading = $state(false);
  let error: string | null = $state(null);
  let activeOnly = $state(true);
  let adminFilter = $state('');

  let tenants: IAMTenant[] = $state([]);
  let selectedTenantID = $state('');
  let tenantsError: string | null = $state(null);
  let users: IAMUser[] = $state([]);
  let targetUserID = $state('');
  let reason = $state('');
  let duration = $state('30m');

  let lookupUserID = $state('');
  let lookupSessions: ImpersonationSession[] | null = $state(null);
  let lookupLoading = $state(false);
  let lookupError: string | null = $state(null);

  let pending: Pending | null = $state(null);
  let working = $state(false);

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadSessions(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const admin = adminFilter.trim();
      const resp = await client.iam.listImpersonations({
        activeOnly,
        adminUserID: admin === '' ? undefined : admin,
      });
      sessions = resp.sessions ?? [];
      total = resp.total ?? sessions.length;
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load impersonation sessions';
    } finally {
      loading = false;
    }
  }

  async function loadTenants(): Promise<void> {
    tenantsError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listTenants();
      tenants = resp.tenants ?? [];
      if (selectedTenantID === '' && tenants.length > 0) {
        selectedTenantID = tenants[0]?.id ?? '';
      }
    } catch (e) {
      tenantsError = e instanceof Error ? e.message : 'Failed to load tenants';
    }
  }

  async function loadUsers(tenantID: string): Promise<void> {
    users = [];
    targetUserID = '';
    if (tenantID === '') return;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listUsers(tenantID);
      users = resp.users ?? [];
      targetUserID = users[0]?.id ?? '';
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to load users');
    }
  }

  // Load once on mount; the filters reload on their own events, not per keystroke.
  $effect(() => {
    untrack(() => {
      void loadSessions();
      void loadTenants();
    });
  });

  $effect(() => {
    void loadUsers(selectedTenantID);
  });

  // ---------------------------------------------------------------------------
  // Start / end
  // ---------------------------------------------------------------------------

  function requestStart(event: SubmitEvent): void {
    event.preventDefault();
    const r = reason.trim();
    if (!r || selectedTenantID === '' || targetUserID === '') return;
    pending = {
      kind: 'start',
      req: { target_user_id: targetUserID, tenant_id: selectedTenantID, reason: r, duration },
    };
  }

  async function confirmPending(): Promise<void> {
    if (!pending) return;
    working = true;
    try {
      const client = await getSDKClient();
      if (pending.kind === 'start') {
        const session = await client.iam.startImpersonation(pending.req);
        showToast('success', `Impersonation session ${session.id} started`);
        reason = '';
      } else {
        const id = pending.session.id;
        const result = await client.iam.endImpersonation(id);
        const endedID = 'id' in result ? result.id : id;
        showToast('success', `Impersonation session ${endedID} ended`);
      }
      pending = null;
      void loadSessions();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'The impersonation request failed');
    } finally {
      working = false;
    }
  }

  // ---------------------------------------------------------------------------
  // User lookup
  // ---------------------------------------------------------------------------

  async function lookupUser(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const id = lookupUserID.trim();
    if (!id) return;
    lookupLoading = true;
    lookupError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listUserImpersonations(id);
      lookupSessions = resp.sessions ?? [];
    } catch (e) {
      lookupError = e instanceof Error ? e.message : 'Failed to load the user\'s sessions';
      lookupSessions = null;
    } finally {
      lookupLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function formatDateTime(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  function who(id: string, name: string | undefined): string {
    return name ? `${name} (${id})` : id;
  }
</script>

{#snippet sessionTable(rows: ImpersonationSession[], label: string, withEnd: boolean)}
  <div class="overflow-x-auto rounded-box">
    <table class="table table-sm" aria-label={label}>
      <thead>
        <tr>
          <th>Admin</th><th>Target</th><th>Tenant</th><th>Reason</th>
          <th>Started</th><th>Expires</th><th>Ended</th><th>Operations</th>
          {#if withEnd}<th><span class="sr-only">Actions</span></th>{/if}
        </tr>
      </thead>
      <tbody>
        {#each rows as s (s.id)}
          <tr class="hover">
            <td class="text-sm">{who(s.admin_user_id, s.admin_username)}</td>
            <td class="text-sm">{who(s.target_user_id, s.target_username)}</td>
            <td class="font-mono text-xs">{s.tenant_id}</td>
            <td class="text-sm">{s.reason}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(s.started_at)}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(s.expires_at)}</td>
            <td class="text-xs">
              {#if s.ended_at}
                <span class="text-base-content/50">{formatDateTime(s.ended_at)}</span>
              {:else}
                <span class="badge badge-success badge-sm">Active</span>
              {/if}
            </td>
            <td class="text-xs">{s.operation_count}</td>
            {#if withEnd}
              <td>
                {#if !s.ended_at}
                  <button
                    class="btn btn-ghost btn-xs text-error"
                    onclick={() => { pending = { kind: 'end', session: s }; }}
                    aria-label={`End session ${s.id}`}
                  >
                    End
                  </button>
                {/if}
              </td>
            {/if}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/snippet}

<div class="p-6 space-y-6">
  <PageHeader
    title="Impersonation"
    subtitle="Delegated-admin impersonation sessions: start, list and end them"
  />

  <!-- ===================================================================== -->
  <!-- Start                                                                 -->
  <!-- ===================================================================== -->
  <section class="space-y-3" aria-label="Start impersonation">
    <h2 class="text-lg font-semibold">Start a Session</h2>
    {#if tenantsError}
      <div class="alert alert-warning text-sm" role="alert">{tenantsError}</div>
    {/if}
    <form class="card bg-base-200 p-4 space-y-2" aria-label="Start impersonation form" onsubmit={requestStart}>
      <div class="flex flex-wrap gap-2 items-center">
        <select
          class="select select-bordered select-sm"
          aria-label="Tenant"
          bind:value={selectedTenantID}
          disabled={tenants.length === 0}
        >
          {#each tenants as tenant (tenant.id)}
            <option value={tenant.id}>{tenant.name || tenant.id}</option>
          {/each}
        </select>
        <select
          class="select select-bordered select-sm"
          aria-label="Target user"
          bind:value={targetUserID}
          disabled={users.length === 0}
        >
          {#each users as user (user.id)}
            <option value={user.id}>{user.username || user.email || user.id}</option>
          {/each}
        </select>
        <select class="select select-bordered select-sm" aria-label="Duration" bind:value={duration}>
          {#each DURATIONS as d (d)}
            <option value={d}>{d}</option>
          {/each}
        </select>
      </div>
      <input
        class="input input-bordered input-sm w-full"
        placeholder="Reason (required)"
        aria-label="Reason"
        bind:value={reason}
      />
      <button
        type="submit"
        class="btn btn-primary btn-sm"
        disabled={working || !reason.trim() || !selectedTenantID || !targetUserID}
      >
        Start Impersonation
      </button>
    </form>
  </section>

  <!-- ===================================================================== -->
  <!-- Sessions                                                              -->
  <!-- ===================================================================== -->
  <section class="space-y-3" aria-label="Sessions">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h2 class="text-lg font-semibold">Sessions <span class="text-sm text-base-content/50">({total})</span></h2>
      <div class="flex flex-wrap items-center gap-2">
        <label class="flex items-center gap-1 text-sm">
          <input
            type="checkbox"
            class="checkbox checkbox-sm"
            bind:checked={activeOnly}
            onchange={() => { void loadSessions(); }}
          />
          Active only
        </label>
        <form
          class="flex items-center gap-1"
          onsubmit={(e) => { e.preventDefault(); void loadSessions(); }}
        >
          <input
            class="input input-bordered input-sm"
            placeholder="Admin user ID"
            aria-label="Admin user ID filter"
            bind:value={adminFilter}
          />
          <button type="submit" class="btn btn-sm">Filter</button>
        </form>
      </div>
    </div>

    {#if error}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{error}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => { void loadSessions(); }}>Retry</button>
      </div>
    {/if}

    {#if loading}
      <div class="overflow-x-auto rounded-box">
        <table class="table table-sm" aria-label="Sessions loading">
          <tbody>
            {#each [1, 2, 3] as i (i)}
              <tr>
                {#each [1, 2, 3, 4, 5, 6, 7, 8] as j (j)}
                  <td><div class="skeleton h-4 w-16"></div></td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else if !error}
      {#if sessions.length === 0}
        <EmptyState
          icon={UserCog}
          title="No impersonation sessions"
          description={activeOnly ? 'No session is active.' : 'No session has been recorded.'}
        />
      {:else}
        {@render sessionTable(sessions, 'Impersonation sessions', true)}
      {/if}
    {/if}
  </section>

  <!-- ===================================================================== -->
  <!-- Sessions for user                                                     -->
  <!-- ===================================================================== -->
  <section class="space-y-3" aria-label="Sessions for user">
    <h2 class="text-lg font-semibold">Sessions for User</h2>
    <form class="flex items-center gap-2" aria-label="Sessions for user lookup" onsubmit={lookupUser}>
      <input
        class="input input-bordered input-sm"
        placeholder="User ID"
        aria-label="Lookup user ID"
        bind:value={lookupUserID}
      />
      <button type="submit" class="btn btn-sm" disabled={lookupLoading || !lookupUserID.trim()}>
        {lookupLoading ? 'Looking up…' : 'Look up'}
      </button>
    </form>
    {#if lookupError}
      <div class="alert alert-error text-sm" role="alert">{lookupError}</div>
    {:else if lookupSessions !== null}
      {#if lookupSessions.length === 0}
        <p class="text-sm text-base-content/60">No session targeted this user.</p>
      {:else}
        {@render sessionTable(lookupSessions, 'User impersonation sessions', false)}
      {/if}
    {/if}
  </section>
</div>

<!-- Confirmation dialog -->
{#if pending !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Impersonation confirmation">
    <div class="modal-box max-w-sm">
      {#if pending.kind === 'start'}
        <h3 class="text-base font-semibold mb-2">Start Impersonation</h3>
        <p class="text-sm text-base-content/70">
          Record a {pending.req.duration} session acting as user
          <span class="font-semibold">{pending.req.target_user_id}</span>
          in tenant <span class="font-semibold">{pending.req.tenant_id}</span>?
        </p>
      {:else}
        <h3 class="text-base font-semibold mb-2">End Impersonation</h3>
        <p class="text-sm text-base-content/70">
          End session <span class="font-semibold">{pending.session.id}</span>?
        </p>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { pending = null; }}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={working} onclick={confirmPending}>
          {working ? 'Working…' : 'Confirm'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { pending = null; }}
      onkeydown={() => { pending = null; }}
    ></div>
  </div>
{/if}
