<!--
  Copyright (c) 2025 Jeremy Hahn
  Copyright (c) 2025 Automate The Things, LLC

  This file is part of go-quicbaas.

  go-quicbaas is dual-licensed:

  1. GNU Affero General Public License v3.0 (AGPL-3.0)
     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html

  2. Commercial License
     Contact licensing@automatethethings.com for commercial licensing options.
-->

<!--
  MySessionsView is the top-level Security > My Sessions route component.

  It lists the authenticated caller's IAM sessions (GET /api/v1/iam/sessions),
  revokes one session (DELETE /api/v1/iam/sessions/{id}) and revokes every
  other session (POST /api/v1/iam/sessions/revoke-others), or signs out of
  every session, this one included (DELETE /api/v1/iam/sessions), each after
  a confirmation dialog. "Details" shows one session as GET
  /api/v1/iam/sessions/{id} returns it.

  IAMSession carries no "current" flag today; a session the response marks
  with `current` or `is_current` is shown as the current one, and "Revoke
  other sessions" is enabled only when such a session is present.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type { IAMSession } from '@quicbaas/sdk';
  import { Timer } from 'lucide-svelte';

  // A session as the server may send it: the SDK type plus the optional
  // markers a server can use to say which session made the request.
  type SessionRow = IAMSession & { current?: boolean; is_current?: boolean };

  type Pending = { kind: 'one'; session: SessionRow } | { kind: 'others' } | { kind: 'all' };

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let sessions: SessionRow[] = $state([]);
  let loading = $state(false);
  let error: string | null = $state(null);
  let pending: Pending | null = $state(null);
  let revoking = $state(false);

  // Session details dialog: the row it was opened for, and what getSession returned.
  let detailsFor: SessionRow | null = $state(null);
  let details: IAMSession | null = $state(null);
  let detailsLoading = $state(false);
  let detailsError: string | null = $state(null);

  const SESSION_COLUMNS: Column<SessionRow>[] = [
    { key: 'device', label: 'Device', searchable: true },
    { key: 'ip_address', label: 'IP address', sortable: true, searchable: true },
    { key: 'created', label: 'Created', sortable: true },
    { key: 'last_active', label: 'Last active', sortable: true },
    { key: 'expires_at', label: 'Expires', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  const currentSession = $derived(sessions.find((s) => isCurrent(s)) ?? null);

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadSessions(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listSessions();
      sessions = resp.sessions ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load sessions';
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    void loadSessions();
  });

  // ---------------------------------------------------------------------------
  // Revoke
  // ---------------------------------------------------------------------------

  async function confirmRevoke(): Promise<void> {
    if (!pending) return;
    revoking = true;
    try {
      const client = await getSDKClient();
      if (pending.kind === 'one') {
        await client.iam.revokeSession(pending.session.id);
        showToast('success', 'Session revoked');
      } else if (pending.kind === 'all') {
        await client.iam.revokeAllSessions();
        showToast('success', 'Signed out of every session');
      } else {
        if (!currentSession) throw new Error('The current session is unknown');
        await client.iam.revokeOtherSessions(currentSession.id);
        showToast('success', 'Other sessions revoked');
      }
      pending = null;
      void loadSessions();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Revoke failed');
    } finally {
      revoking = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Details
  // ---------------------------------------------------------------------------

  async function openDetails(s: SessionRow): Promise<void> {
    detailsFor = s;
    details = null;
    detailsError = null;
    detailsLoading = true;
    try {
      const client = await getSDKClient();
      const got = await client.iam.getSession(s.id);
      if (detailsFor === s) details = got;
    } catch (e) {
      if (detailsFor === s) detailsError = e instanceof Error ? e.message : 'Failed to load the session';
    } finally {
      if (detailsFor === s) detailsLoading = false;
    }
  }

  function closeDetails(): void {
    detailsFor = null;
    details = null;
    detailsError = null;
    detailsLoading = false;
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function isCurrent(s: SessionRow): boolean {
    return s.current === true || s.is_current === true;
  }

  function deviceLabel(s: SessionRow): string {
    return s.device_info?.name || s.user_agent || 'Unknown device';
  }

  function formatDateTime(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="My Sessions" subtitle="Devices signed in to your account">
    {#snippet children()}
      <button
        class="btn btn-error btn-sm"
        disabled={!currentSession || sessions.length < 2}
        title={currentSession ? undefined : 'The current session is not known'}
        onclick={() => { pending = { kind: 'others' }; }}
        aria-label="Revoke other sessions"
      >
        Revoke Other Sessions
      </button>
      <button
        class="btn btn-error btn-outline btn-sm"
        disabled={sessions.length === 0}
        onclick={() => { pending = { kind: 'all' }; }}
        aria-label="Sign out of every session"
      >
        Sign out everywhere
      </button>
    {/snippet}
  </PageHeader>

  <!-- Load error -->
  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadSessions(); }}>Retry</button>
    </div>
  {/if}

  <!-- Loading skeleton -->
  {#if loading}
    <div class="overflow-x-auto rounded-box">
      <table class="table table-sm" aria-label="Sessions loading">
        <thead>
          <tr>
            <th>Device</th><th>IP address</th><th>Created</th><th>Last active</th><th>Expires</th><th></th>
          </tr>
        </thead>
        <tbody>
          {#each [1, 2, 3] as i (i)}
            <tr>
              <td><div class="skeleton h-4 w-32"></div></td>
              <td><div class="skeleton h-4 w-24"></div></td>
              <td><div class="skeleton h-4 w-24"></div></td>
              <td><div class="skeleton h-4 w-24"></div></td>
              <td><div class="skeleton h-4 w-24"></div></td>
              <td><div class="skeleton h-4 w-16"></div></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

  <!-- Sessions table -->
  {:else if !error}
    <DataTable
      columns={SESSION_COLUMNS}
      rows={sessions}
      rowKey={(s: SessionRow) => s.id}
      urlKey="mysessions"
      ariaLabel="Sessions"
      searchPlaceholder="Search sessions..."
      searchLabel="Search sessions"
      noMatchText="No sessions match the search."
    >
      {#snippet empty()}
        <EmptyState
          icon={Timer}
          title="No sessions found"
          description="Sessions appear here when you sign in from a device."
        />
      {/snippet}

      {#snippet row(s: SessionRow)}
        <tr class="hover">
          <td class="text-sm">
            <span title={s.user_agent}>{deviceLabel(s)}</span>
            {#if isCurrent(s)}
              <span class="badge badge-primary badge-sm ml-1">Current</span>
            {/if}
          </td>
          <td class="font-mono text-xs">{s.ip_address || '—'}</td>
          <td class="text-xs text-base-content/50">{formatDateTime(s.created)}</td>
          <td class="text-xs text-base-content/50">{formatDateTime(s.last_active)}</td>
          <td class="text-xs text-base-content/50">{formatDateTime(s.expires_at)}</td>
          <td>
            <button
              class="btn btn-ghost btn-xs"
              onclick={() => { void openDetails(s); }}
              aria-label={`Details for session ${deviceLabel(s)}`}
            >
              Details
            </button>
            {#if !isCurrent(s)}
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={() => { pending = { kind: 'one', session: s }; }}
                aria-label={`Revoke session ${deviceLabel(s)}`}
              >
                Revoke
              </button>
            {/if}
          </td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>

<!-- Revoke confirmation dialog -->
{#if pending !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Revoke session confirmation">
    <div class="modal-box max-w-sm">
      {#if pending.kind === 'one'}
        <h3 class="text-base font-semibold mb-2">Revoke Session</h3>
        <p class="text-sm text-base-content/70">
          Revoke the session on <span class="font-semibold">{deviceLabel(pending.session)}</span>?
          That device will be signed out.
        </p>
      {:else if pending.kind === 'all'}
        <h3 class="text-base font-semibold mb-2">Sign Out Everywhere</h3>
        <p class="text-sm text-base-content/70">
          Sign out of every session, this one included? Every device, this one too, will be signed out.
        </p>
      {:else}
        <h3 class="text-base font-semibold mb-2">Revoke Other Sessions</h3>
        <p class="text-sm text-base-content/70">
          Sign out every other device? Only this session stays signed in.
        </p>
      {/if}
      <div class="modal-action">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { pending = null; }}
        >
          Cancel
        </button>
        <button
          class="btn btn-error btn-sm"
          disabled={revoking}
          onclick={confirmRevoke}
        >
          {revoking ? 'Revoking…' : 'Revoke'}
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

<!-- Session details dialog -->
{#if detailsFor !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Session details">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-2">Session Details</h3>
      {#if detailsLoading}
        <div class="space-y-2" aria-label="Session details loading">
          <div class="skeleton h-4 w-full"></div>
          <div class="skeleton h-4 w-3/4"></div>
          <div class="skeleton h-4 w-1/2"></div>
        </div>
      {:else if detailsError}
        <div class="alert alert-error text-sm" role="alert">
          <span>{detailsError}</span>
        </div>
      {:else if details}
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
          <dt class="text-base-content/60">Session ID</dt>
          <dd class="font-mono text-xs break-all">{details.id}</dd>
          <dt class="text-base-content/60">Created</dt>
          <dd>{formatDateTime(details.created)}</dd>
          <dt class="text-base-content/60">Last active</dt>
          <dd>{formatDateTime(details.last_active)}</dd>
          <dt class="text-base-content/60">Expires</dt>
          <dd>{formatDateTime(details.expires_at)}</dd>
          <dt class="text-base-content/60">MFA verified</dt>
          <dd>{details.mfa_verified ? 'Yes' : 'No'}</dd>
          <dt class="text-base-content/60">IP address</dt>
          <dd class="font-mono text-xs">{details.ip_address || '—'}</dd>
          <dt class="text-base-content/60">User agent</dt>
          <dd class="break-all">{details.user_agent || '—'}</dd>
          {#if details.device_info}
            <dt class="text-base-content/60">Device ID</dt>
            <dd class="font-mono text-xs break-all">{details.device_info.id || '—'}</dd>
            <dt class="text-base-content/60">Device name</dt>
            <dd>{details.device_info.name || '—'}</dd>
            <dt class="text-base-content/60">Device type</dt>
            <dd>{details.device_info.type || '—'}</dd>
            <dt class="text-base-content/60">Fingerprint</dt>
            <dd class="font-mono text-xs break-all">{details.device_info.fingerprint || '—'}</dd>
            <dt class="text-base-content/60">Trusted device</dt>
            <dd>{details.device_info.trusted ? 'Yes' : 'No'}</dd>
          {/if}
        </dl>
      {/if}
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeDetails}>Close</button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={closeDetails}
      onkeydown={closeDetails}
    ></div>
  </div>
{/if}
