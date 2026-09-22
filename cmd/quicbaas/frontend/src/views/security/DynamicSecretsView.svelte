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
  DynamicSecretsView renders an operator console for Dynamic Secrets:
  credential-generation role CRUD, on-demand credential generation, and
  lease renewal/revocation. Equivalent to the SDK's dynamicSecrets client
  verb surface: create-role, get-role, list-roles, delete-role, generate,
  list-leases, renew-lease, revoke-lease.

  SECURITY: generateCredential's response is a mint-once secret. The result
  is rendered exactly once in a dismissible dialog and is discarded (set to
  null) the moment that dialog closes — it is never re-fetched or persisted
  elsewhere in this component.

  Actions:
    - Role list with create / delete
    - Generate Credential dialog (per role, optional TTL override)
    - Lease list with renew / revoke
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { formatDateTime } from '../../lib/time.ts';
  import type {
    DynamicSecretRole,
    LeaseInfo,
    GenerateCredentialResponse,
  } from '@quicbaas/sdk';
  import {
    Timer,
    Plus,
    RefreshCw,
    Trash2,
    KeyRound,
    RotateCw,
    Ban,
    Copy,
  } from 'lucide-svelte';

  async function copyToClipboard(text: string, label: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', `${label} copied to clipboard`);
    } catch {
      showToast('error', `Failed to copy ${label}`);
    }
  }

  /** parseConfigLines turns "KEY=VALUE" lines into a Record, skipping blank/malformed lines. */
  function parseConfigLines(text: string): Record<string, string> | undefined {
    const out: Record<string, string> = {};
    for (const line of text.split('\n')) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      const idx = trimmed.indexOf('=');
      if (idx <= 0) continue;
      out[trimmed.slice(0, idx).trim()] = trimmed.slice(idx + 1).trim();
    }
    return Object.keys(out).length > 0 ? out : undefined;
  }

  // ---------------------------------------------------------------------------
  // Roles list
  // ---------------------------------------------------------------------------

  let roles = $state<DynamicSecretRole[]>([]);

  const ROLE_COLUMNS: Column<DynamicSecretRole>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'generator_type', label: 'Generator', sortable: true, searchable: true },
    { key: 'default_ttl', label: 'Default TTL', sortable: true },
    { key: 'max_ttl', label: 'Max TTL', sortable: true },
    { key: 'actions', label: 'Actions' },
  ];
  let rolesLoading = $state(false);
  let rolesError = $state('');
  let filterPrefix = $state('');

  async function loadRoles(): Promise<void> {
    rolesLoading = true;
    rolesError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.dynamicSecrets.listRoles(filterPrefix.trim() || undefined);
      roles = resp.roles;
    } catch (err: unknown) {
      rolesError = err instanceof Error ? err.message : String(err);
    } finally {
      rolesLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Leases list
  // ---------------------------------------------------------------------------

  let leases = $state<LeaseInfo[]>([]);

  const LEASE_COLUMNS: Column<LeaseInfo>[] = [
    { key: 'lease_id', label: 'Lease ID', sortable: true, searchable: true },
    { key: 'path', label: 'Path', sortable: true, searchable: true },
    { key: 'ttl', label: 'TTL', sortable: true },
    { key: 'expiry', label: 'Expiry', sortable: true },
    { key: 'renew_count', label: 'Renewals', sortable: true },
    { key: 'actions', label: 'Actions' },
  ];
  let leasesLoading = $state(false);
  let leasesError = $state('');

  async function loadLeases(): Promise<void> {
    leasesLoading = true;
    leasesError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.dynamicSecrets.listLeases();
      leases = resp.leases;
    } catch (err: unknown) {
      leasesError = err instanceof Error ? err.message : String(err);
    } finally {
      leasesLoading = false;
    }
  }

  async function loadAll(): Promise<void> {
    await Promise.all([loadRoles(), loadLeases()]);
  }

  $effect(() => { void loadAll(); });

  // ---------------------------------------------------------------------------
  // Create role dialog
  // ---------------------------------------------------------------------------

  let showCreateRoleDialog = $state(false);
  let creatingRole = $state(false);
  let createRoleError = $state('');
  let createRoleName = $state('');
  let createRoleGeneratorType = $state('');
  let createRoleConfigText = $state('');
  let createRoleDefaultTTL = $state('1h');
  let createRoleMaxTTL = $state('24h');

  function openCreateRoleDialog(): void {
    showCreateRoleDialog = true;
    createRoleError = '';
    createRoleName = '';
    createRoleGeneratorType = '';
    createRoleConfigText = '';
    createRoleDefaultTTL = '1h';
    createRoleMaxTTL = '24h';
  }

  async function submitCreateRole(): Promise<void> {
    if (!createRoleName.trim()) {
      createRoleError = 'Role name is required.';
      return;
    }
    if (!createRoleGeneratorType.trim()) {
      createRoleError = 'Generator type is required.';
      return;
    }
    creatingRole = true;
    createRoleError = '';
    try {
      const client = await getSDKClient();
      await client.dynamicSecrets.createRole({
        name: createRoleName.trim(),
        generator_type: createRoleGeneratorType.trim(),
        config: parseConfigLines(createRoleConfigText),
        default_ttl: createRoleDefaultTTL.trim(),
        max_ttl: createRoleMaxTTL.trim(),
      });
      showCreateRoleDialog = false;
      showToast('success', `Role "${createRoleName.trim()}" created`);
      await loadRoles();
    } catch (err: unknown) {
      createRoleError = err instanceof Error ? err.message : String(err);
    } finally {
      creatingRole = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete role
  // ---------------------------------------------------------------------------

  let deleteRoleTarget = $state<DynamicSecretRole | null>(null);
  let deletingRole = $state(false);

  async function confirmDeleteRole(): Promise<void> {
    if (!deleteRoleTarget) return;
    deletingRole = true;
    try {
      const client = await getSDKClient();
      await client.dynamicSecrets.deleteRole(deleteRoleTarget.name);
      showToast('success', `Role "${deleteRoleTarget.name}" deleted`);
      deleteRoleTarget = null;
      await loadRoles();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      deletingRole = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Generate credential — mint-once result, never persisted beyond dialog.
  // ---------------------------------------------------------------------------

  let generateTarget = $state<DynamicSecretRole | null>(null);
  let generateTTL = $state('');
  let generating = $state(false);
  let generateError = $state('');
  let generateResult = $state<GenerateCredentialResponse | null>(null);

  function openGenerateDialog(role: DynamicSecretRole): void {
    generateTarget = role;
    generateTTL = '';
    generateError = '';
    generateResult = null;
  }

  async function submitGenerate(): Promise<void> {
    if (!generateTarget) return;
    generating = true;
    generateError = '';
    try {
      const client = await getSDKClient();
      generateResult = await client.dynamicSecrets.generateCredential(generateTarget.name, {
        ttl: generateTTL.trim() || undefined,
      });
      await loadLeases();
    } catch (err: unknown) {
      generateError = err instanceof Error ? err.message : String(err);
    } finally {
      generating = false;
    }
  }

  /** closeGenerateDialog discards the one-time credential result. It must never be re-shown. */
  function closeGenerateDialog(): void {
    generateTarget = null;
    generateResult = null;
    generateError = '';
  }

  // ---------------------------------------------------------------------------
  // Renew lease
  // ---------------------------------------------------------------------------

  let renewTarget = $state<LeaseInfo | null>(null);
  let renewIncrement = $state('');
  let renewing = $state(false);

  function openRenewDialog(lease: LeaseInfo): void {
    renewTarget = lease;
    renewIncrement = '';
  }

  async function confirmRenew(): Promise<void> {
    if (!renewTarget) return;
    renewing = true;
    try {
      const client = await getSDKClient();
      await client.dynamicSecrets.renewLease(renewTarget.lease_id, {
        increment: renewIncrement.trim() || undefined,
      });
      showToast('success', `Lease "${renewTarget.lease_id}" renewed`);
      renewTarget = null;
      await loadLeases();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      renewing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Revoke lease
  // ---------------------------------------------------------------------------

  let revokeTarget = $state<LeaseInfo | null>(null);
  let revoking = $state(false);

  async function confirmRevoke(): Promise<void> {
    if (!revokeTarget) return;
    revoking = true;
    try {
      const client = await getSDKClient();
      await client.dynamicSecrets.revokeLease(revokeTarget.lease_id);
      showToast('success', `Lease "${revokeTarget.lease_id}" revoked`);
      revokeTarget = null;
      await loadLeases();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      revoking = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Dynamic Secrets" subtitle="Credential-generation roles and short-lived leased credentials" icon={Timer}>
    {#snippet children()}
      <button class="btn btn-ghost btn-sm gap-1" onclick={() => void loadAll()} disabled={rolesLoading || leasesLoading} aria-label="Refresh">
        <RefreshCw class="h-3.5 w-3.5 {(rolesLoading || leasesLoading) ? 'animate-spin' : ''}" />
        Refresh
      </button>
      <button class="btn btn-primary btn-sm gap-1" onclick={openCreateRoleDialog} aria-label="Create role">
        <Plus class="h-3.5 w-3.5" />
        Create Role
      </button>
    {/snippet}
  </PageHeader>

  <!-- =========================================================================
       ROLES
       ========================================================================= -->
  <section class="space-y-3">
    <div class="flex items-center justify-between">
      <h2 class="text-sm font-semibold">Roles</h2>
      <div class="flex items-end gap-2">
        <input
          class="input input-bordered input-sm"
          type="text"
          placeholder="Filter by name prefix"
          bind:value={filterPrefix}
          onkeydown={(e) => { if (e.key === 'Enter') void loadRoles(); }}
        />
        <button class="btn btn-sm btn-outline" onclick={() => void loadRoles()}>Apply</button>
      </div>
    </div>

    {#if rolesError}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{rolesError}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => void loadRoles()}>Retry</button>
      </div>
    {/if}

    {#if rolesLoading && roles.length === 0}
      <div class="flex justify-center py-8">
        <span class="loading loading-spinner loading-md"></span>
      </div>
    {:else}
      <DataTable
        columns={ROLE_COLUMNS}
        rows={roles}
        rowKey={(role: DynamicSecretRole) => role.name}
        urlKey="roles"
        ariaLabel="Credential roles"
        searchPlaceholder="Search roles..."
        searchLabel="Search roles"
        noMatchText="No roles match the search."
      >
        {#snippet empty()}
          <EmptyState
            icon={Timer}
            title="No roles found"
            description="Create a credential-generation role to start minting dynamic secrets."
            actionLabel="Create Role"
            onaction={openCreateRoleDialog}
          />
        {/snippet}

        {#snippet row(role: DynamicSecretRole)}
          <tr class="hover">
            <td class="font-mono text-xs">{role.name}</td>
            <td class="text-xs">{role.generator_type}</td>
            <td class="text-xs text-base-content/50">{role.default_ttl}</td>
            <td class="text-xs text-base-content/50">{role.max_ttl}</td>
            <td>
              <div class="flex gap-1">
                <div class="tooltip tooltip-left" data-tip="Generate Credential">
                  <button
                    class="btn btn-ghost btn-xs"
                    onclick={() => openGenerateDialog(role)}
                    aria-label={`Generate credential for ${role.name}`}
                  >
                    <KeyRound class="h-3 w-3" />
                  </button>
                </div>
                <div class="tooltip tooltip-left" data-tip="Delete">
                  <button
                    class="btn btn-ghost btn-xs text-error"
                    onclick={() => { deleteRoleTarget = role; }}
                    aria-label={`Delete role ${role.name}`}
                  >
                    <Trash2 class="h-3 w-3" />
                  </button>
                </div>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}
  </section>

  <!-- =========================================================================
       LEASES
       ========================================================================= -->
  <section class="space-y-3">
    <h2 class="text-sm font-semibold">Active Leases</h2>

    {#if leasesError}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{leasesError}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => void loadLeases()}>Retry</button>
      </div>
    {/if}

    {#if leasesLoading && leases.length === 0}
      <div class="flex justify-center py-8">
        <span class="loading loading-spinner loading-md"></span>
      </div>
    {:else}
      <DataTable
        columns={LEASE_COLUMNS}
        rows={leases}
        rowKey={(lease: LeaseInfo) => lease.lease_id}
        urlKey="leases"
        ariaLabel="Active leases"
        searchPlaceholder="Search leases..."
        searchLabel="Search leases"
        noMatchText="No leases match the search."
      >
        {#snippet empty()}
          <EmptyState icon={Timer} title="No active leases" description="Generate a credential above to create a lease." />
        {/snippet}

        {#snippet row(lease: LeaseInfo)}
          <tr class="hover">
            <td class="font-mono text-xs truncate max-w-[10rem]" title={lease.lease_id}>{lease.lease_id}</td>
            <td class="font-mono text-xs">{lease.path}</td>
            <td class="text-xs text-base-content/50">{lease.ttl}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(lease.expiry)}</td>
            <td class="text-xs">{lease.renew_count}</td>
            <td>
              <div class="flex gap-1">
                <div class="tooltip tooltip-left" data-tip="Renew">
                  <button
                    class="btn btn-ghost btn-xs"
                    disabled={!lease.renewable}
                    onclick={() => openRenewDialog(lease)}
                    aria-label={`Renew lease ${lease.lease_id}`}
                  >
                    <RotateCw class="h-3 w-3" />
                  </button>
                </div>
                <div class="tooltip tooltip-left" data-tip="Revoke">
                  <button
                    class="btn btn-ghost btn-xs text-error"
                    onclick={() => { revokeTarget = lease; }}
                    aria-label={`Revoke lease ${lease.lease_id}`}
                  >
                    <Ban class="h-3 w-3" />
                  </button>
                </div>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}
  </section>
</div>

<!-- =========================================================================
     CREATE ROLE DIALOG
     ========================================================================= -->
{#if showCreateRoleDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create role dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Create Role</h3>
      {#if createRoleError}
        <div class="alert alert-error text-sm mb-3" role="alert">{createRoleError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="role-name">Name <span class="text-error">*</span></label>
          <input id="role-name" class="input input-bordered input-sm font-mono" type="text" bind:value={createRoleName} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="role-generator">Generator Type <span class="text-error">*</span></label>
          <input id="role-generator" class="input input-bordered input-sm font-mono" type="text" placeholder="postgres, api-key, …" bind:value={createRoleGeneratorType} />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="form-control">
            <label class="label label-text" for="role-default-ttl">Default TTL</label>
            <input id="role-default-ttl" class="input input-bordered input-sm font-mono" type="text" placeholder="1h" bind:value={createRoleDefaultTTL} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="role-max-ttl">Max TTL</label>
            <input id="role-max-ttl" class="input input-bordered input-sm font-mono" type="text" placeholder="24h" bind:value={createRoleMaxTTL} />
          </div>
        </div>
        <div class="form-control">
          <label class="label label-text" for="role-config">Config (KEY=VALUE per line)</label>
          <textarea id="role-config" class="textarea textarea-bordered font-mono text-xs" rows="4" bind:value={createRoleConfigText}></textarea>
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showCreateRoleDialog = false; }} disabled={creatingRole}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={creatingRole} onclick={() => void submitCreateRole()}>
          {creatingRole ? 'Creating…' : 'Create'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCreateRoleDialog = false; }} onkeydown={() => { showCreateRoleDialog = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE ROLE CONFIRM DIALOG
     ========================================================================= -->
{#if deleteRoleTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete role confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Role?</h3>
      <p class="text-sm text-base-content/70">
        Delete role <span class="font-mono font-semibold">{deleteRoleTarget.name}</span>? Existing leases are
        unaffected, but no new credentials can be generated from this role.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { deleteRoleTarget = null; }} disabled={deletingRole}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deletingRole} onclick={() => void confirmDeleteRole()}>
          {deletingRole ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { deleteRoleTarget = null; }} onkeydown={() => { deleteRoleTarget = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     GENERATE CREDENTIAL DIALOG — ONE-TIME SECRET DISPLAY
     ========================================================================= -->
{#if generateTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Generate credential dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Generate Credential — <span class="font-mono">{generateTarget.name}</span></h3>

      {#if generateError}
        <div class="alert alert-error text-sm mb-3" role="alert">{generateError}</div>
      {/if}

      {#if !generateResult}
        <div class="form-control">
          <label class="label label-text" for="generate-ttl">TTL Override (optional, e.g. "30m")</label>
          <input id="generate-ttl" class="input input-bordered input-sm font-mono" type="text" bind:value={generateTTL} />
        </div>
        <div class="modal-action">
          <button class="btn btn-ghost btn-sm" onclick={closeGenerateDialog} disabled={generating}>Cancel</button>
          <button class="btn btn-primary btn-sm" disabled={generating} onclick={() => void submitGenerate()}>
            {generating ? 'Generating…' : 'Generate'}
          </button>
        </div>
      {:else}
        <p class="text-xs text-warning mb-3">
          This credential is shown only once. Save it now — it cannot be retrieved again.
        </p>
        <div class="space-y-3">
          {#if generateResult.username}
            <div>
              <div class="flex items-center justify-between mb-1">
                <span class="text-xs font-semibold text-base-content/50 uppercase tracking-wide">Username</span>
                <button class="btn btn-ghost btn-xs gap-1" onclick={() => void copyToClipboard(generateResult?.username ?? '', 'Username')}>
                  <Copy class="h-3 w-3" /> Copy
                </button>
              </div>
              <pre class="bg-base-300 rounded-box p-2 text-xs overflow-x-auto">{generateResult.username}</pre>
            </div>
          {/if}
          {#if generateResult.password}
            <div>
              <div class="flex items-center justify-between mb-1">
                <span class="text-xs font-semibold text-warning uppercase tracking-wide">Password (shown once)</span>
                <button class="btn btn-ghost btn-xs gap-1" onclick={() => void copyToClipboard(generateResult?.password ?? '', 'Password')}>
                  <Copy class="h-3 w-3" /> Copy
                </button>
              </div>
              <pre class="bg-base-300 rounded-box p-2 text-xs overflow-x-auto">{generateResult.password}</pre>
            </div>
          {/if}
          {#if generateResult.secret && Object.keys(generateResult.secret).length > 0}
            <div>
              <span class="text-xs font-semibold text-base-content/50 uppercase tracking-wide">Secret</span>
              <pre class="bg-base-300 rounded-box p-2 text-xs overflow-x-auto">{JSON.stringify(generateResult.secret, null, 2)}</pre>
            </div>
          {/if}
          <div class="text-xs text-base-content/50">
            Lease: <span class="font-mono">{generateResult.lease_id}</span>
            (expires {formatDateTime(generateResult.lease_expiry)}, renewable: {generateResult.renewable ? 'yes' : 'no'})
          </div>
        </div>
        <div class="modal-action">
          <button class="btn btn-primary btn-sm" onclick={closeGenerateDialog}>Done</button>
        </div>
      {/if}
    </div>
    <div class="modal-backdrop" role="none" onclick={closeGenerateDialog} onkeydown={closeGenerateDialog}></div>
  </div>
{/if}

<!-- =========================================================================
     RENEW LEASE DIALOG
     ========================================================================= -->
{#if renewTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Renew lease dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-4">Renew Lease</h3>
      <p class="text-sm text-base-content/70 mb-3">
        Renew <span class="font-mono">{renewTarget.lease_id}</span>.
      </p>
      <div class="form-control">
        <label class="label label-text" for="renew-increment">Increment (optional, e.g. "30m")</label>
        <input id="renew-increment" class="input input-bordered input-sm font-mono" type="text" bind:value={renewIncrement} />
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { renewTarget = null; }} disabled={renewing}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={renewing} onclick={() => void confirmRenew()}>
          {renewing ? 'Renewing…' : 'Renew'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { renewTarget = null; }} onkeydown={() => { renewTarget = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     REVOKE LEASE CONFIRM DIALOG
     ========================================================================= -->
{#if revokeTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Revoke lease confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Revoke Lease?</h3>
      <p class="text-sm text-base-content/70">
        Revoke <span class="font-mono font-semibold">{revokeTarget.lease_id}</span> immediately? The underlying
        credential will stop working right away. This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { revokeTarget = null; }} disabled={revoking}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={revoking} onclick={() => void confirmRevoke()}>
          {revoking ? 'Revoking…' : 'Revoke'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { revokeTarget = null; }} onkeydown={() => { revokeTarget = null; }}></div>
  </div>
{/if}
