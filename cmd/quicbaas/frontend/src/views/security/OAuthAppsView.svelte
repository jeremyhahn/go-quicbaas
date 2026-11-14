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
  OAuthAppsView is the top-level Security > OAuth Apps route component.

  It manages a tenant's registered OAuth applications through `client.iam`:
    - list the applications of the selected tenant;
    - register an application (the response carries its client secret);
    - edit the name, redirect URIs and grant types (only changed fields sent);
    - rotate the client secret and delete, each after a confirmation dialog.
  A client secret returned by register or rotate is shown once in a dialog and
  dropped from state when the dialog closes.
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type {
    IAMApplication,
    IAMTenant,
    RegisterIAMAppRequest,
    UpdateIAMAppRequest,
  } from '@quicbaas/sdk';
  import { AppWindow } from 'lucide-svelte';

  type Pending = { kind: 'rotate' | 'delete'; clientID: string; name: string };
  type Secret = { clientID: string; value: string };

  const GRANT_TYPES = ['authorization_code', 'client_credentials', 'refresh_token'];
  const AUTH_METHODS = [
    'client_secret_basic',
    'client_secret_post',
    'private_key_jwt',
    'tls_client_auth',
    'none',
  ];

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let tenants: IAMTenant[] = $state([]);
  let selectedTenantID = $state('');
  let tenantsLoading = $state(false);
  let tenantsError: string | null = $state(null);

  let apps: IAMApplication[] = $state([]);
  let appsLoading = $state(false);
  let appsError: string | null = $state(null);

  let regName = $state('');
  let regRedirects = $state('');
  let regGrants: string[] = $state(['authorization_code']);
  let regPKCE = $state(true);
  let regAuthMethod = $state('client_secret_basic');
  let registering = $state(false);

  let editing: IAMApplication | null = $state(null);
  let editName = $state('');
  let editRedirects = $state('');
  let editGrants: string[] = $state([]);
  let saving = $state(false);

  let pending: Pending | null = $state(null);
  let working = $state(false);

  let secret: Secret | null = $state(null);

  const COLUMNS: Column<IAMApplication>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'client_id', label: 'Client ID', searchable: true },
    { key: 'grant_types', label: 'Grant types' },
    { key: 'redirect_uris', label: 'Redirect URIs' },
    { key: 'updated', label: 'Updated', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

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

  async function loadApps(tenantID: string): Promise<void> {
    if (tenantID === '') {
      apps = [];
      return;
    }
    appsLoading = true;
    appsError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listApps(tenantID);
      if (tenantID !== selectedTenantID) return;
      apps = resp.applications ?? resp.apps ?? [];
    } catch (e) {
      appsError = e instanceof Error ? e.message : 'Failed to load OAuth applications';
    } finally {
      appsLoading = false;
    }
  }

  function reload(): void {
    void loadApps(selectedTenantID);
  }

  $effect(() => {
    void loadTenants();
  });

  $effect(() => {
    const tenantID = selectedTenantID;
    untrack(() => { void loadApps(tenantID); });
  });

  // ---------------------------------------------------------------------------
  // Register
  // ---------------------------------------------------------------------------

  function parseLines(text: string): string[] {
    return text.split('\n').map((l) => l.trim()).filter((l) => l !== '');
  }

  async function registerApp(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const name = regName.trim();
    if (!name || selectedTenantID === '') return;
    registering = true;
    try {
      const client = await getSDKClient();
      const req: RegisterIAMAppRequest = {
        name,
        redirect_uris: parseLines(regRedirects),
        grant_types: [...regGrants],
        pkce_required: regPKCE,
        token_endpoint_auth_method: regAuthMethod,
      };
      const app = await client.iam.registerApp(selectedTenantID, req);
      showToast('success', `Application ${name} registered`);
      regName = '';
      regRedirects = '';
      if (app.client_secret) {
        secret = { clientID: app.client_id, value: app.client_secret };
      }
      reload();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Registering the application failed');
    } finally {
      registering = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Edit
  // ---------------------------------------------------------------------------

  function openEdit(app: IAMApplication): void {
    editing = app;
    editName = app.name;
    editRedirects = (app.redirect_uris ?? []).join('\n');
    editGrants = [...(app.grant_types ?? [])];
  }

  function sameList(a: string[], b: string[]): boolean {
    return a.length === b.length && a.every((v, i) => v === b[i]);
  }

  async function saveEdit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!editing) return;
    const app = editing;
    const req: UpdateIAMAppRequest = {};
    const name = editName.trim();
    if (name !== '' && name !== app.name) req.name = name;
    const redirects = parseLines(editRedirects);
    if (!sameList(redirects, app.redirect_uris ?? [])) req.redirect_uris = redirects;
    const grants = GRANT_TYPES.filter((g) => editGrants.includes(g));
    const oldGrants = GRANT_TYPES.filter((g) => (app.grant_types ?? []).includes(g));
    if (!sameList(grants, oldGrants)) req.grant_types = grants;
    if (Object.keys(req).length === 0) {
      editing = null;
      return;
    }
    saving = true;
    try {
      const client = await getSDKClient();
      await client.iam.updateApp(selectedTenantID, app.client_id, req);
      showToast('success', `Application ${req.name ?? app.name} updated`);
      editing = null;
      reload();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Updating the application failed');
    } finally {
      saving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotate / delete
  // ---------------------------------------------------------------------------

  async function confirmPending(): Promise<void> {
    if (!pending) return;
    const p = pending;
    working = true;
    try {
      const client = await getSDKClient();
      if (p.kind === 'rotate') {
        const app = await client.iam.rotateAppSecret(selectedTenantID, p.clientID);
        showToast('success', `Secret of ${p.name} rotated`);
        pending = null;
        if (app.client_secret) {
          secret = { clientID: p.clientID, value: app.client_secret };
        }
      } else {
        await client.iam.deleteApp(selectedTenantID, p.clientID);
        showToast('success', `Application ${p.name} deleted`);
        pending = null;
      }
      reload();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'The operation failed');
    } finally {
      working = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Secret dialog
  // ---------------------------------------------------------------------------

  async function copySecret(): Promise<void> {
    if (!secret) return;
    try {
      await navigator.clipboard.writeText(secret.value);
      showToast('success', 'Client secret copied');
    } catch {
      showToast('error', 'Copying failed');
    }
  }

  function closeSecret(): void {
    secret = null;
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function formatDateTime(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }

  function formatList(list: string[] | undefined): string {
    return list && list.length > 0 ? list.join(', ') : '—';
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="OAuth Apps" subtitle="OAuth applications registered in a tenant" />

  {#if tenantsError}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{tenantsError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadTenants(); }}>Retry</button>
    </div>
  {/if}
  <div class="form-control max-w-sm">
    <label class="label label-text" for="oauth-apps-tenant-select">Tenant</label>
    <select
      id="oauth-apps-tenant-select"
      class="select select-bordered select-sm"
      bind:value={selectedTenantID}
      disabled={tenantsLoading || tenants.length === 0}
      data-testid="oauth-apps-tenant-select"
    >
      {#if tenants.length === 0}
        <option value="">No tenants available</option>
      {/if}
      {#each tenants as tenant (tenant.id)}
        <option value={tenant.id}>{tenant.name || tenant.id}</option>
      {/each}
    </select>
  </div>

  <form class="card bg-base-200 p-4 space-y-2" aria-label="Register application" onsubmit={registerApp}>
    <h2 class="text-lg font-semibold">Register</h2>
    <input
      class="input input-bordered input-sm w-full max-w-sm"
      placeholder="Name"
      aria-label="Application name"
      bind:value={regName}
    />
    <textarea
      class="textarea textarea-bordered w-full font-mono text-xs"
      rows="3"
      placeholder="https://app.example.com/callback (one per line)"
      aria-label="Redirect URIs"
      bind:value={regRedirects}
    ></textarea>
    <div class="flex flex-wrap gap-4 items-center text-sm">
      {#each GRANT_TYPES as g (g)}
        <label class="flex items-center gap-1">
          <input type="checkbox" class="checkbox checkbox-sm" value={g} bind:group={regGrants} />
          {g}
        </label>
      {/each}
      <label class="flex items-center gap-1">
        <input type="checkbox" class="checkbox checkbox-sm" aria-label="PKCE required" bind:checked={regPKCE} />
        PKCE required
      </label>
      <select
        class="select select-bordered select-sm"
        aria-label="Token endpoint auth method"
        bind:value={regAuthMethod}
      >
        {#each AUTH_METHODS as m (m)}
          <option value={m}>{m}</option>
        {/each}
      </select>
    </div>
    <button
      type="submit"
      class="btn btn-primary btn-sm"
      disabled={registering || !regName.trim() || selectedTenantID === ''}
    >
      {registering ? 'Registering…' : 'Register Application'}
    </button>
  </form>

  <section class="space-y-3" aria-label="Applications">
    {#if appsError}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{appsError}</span>
        <button class="btn btn-xs btn-ghost" onclick={reload}>Retry</button>
      </div>
    {/if}

    {#if appsLoading}
      <div class="overflow-x-auto rounded-box">
        <table class="table table-sm" aria-label="Applications loading">
          <thead>
            <tr><th>Name</th><th>Client ID</th><th>Grant types</th><th>Redirect URIs</th><th>Updated</th><th></th></tr>
          </thead>
          <tbody>
            {#each [1, 2, 3] as i (i)}
              <tr>
                {#each [1, 2, 3, 4, 5, 6] as j (j)}
                  <td><div class="skeleton h-4 w-20"></div></td>
                {/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {:else if !appsError}
      <DataTable
        columns={COLUMNS}
        rows={apps}
        rowKey={(a: IAMApplication) => a.client_id}
        urlKey="oauthapps"
        ariaLabel="Applications"
        searchPlaceholder="Search applications..."
        searchLabel="Search applications"
        noMatchText="No applications match the search."
      >
        {#snippet empty()}
          <EmptyState
            icon={AppWindow}
            title="No OAuth applications"
            description="Register an application for this tenant."
          />
        {/snippet}

        {#snippet row(a: IAMApplication)}
          <tr class="hover">
            <td class="text-sm">{a.name}</td>
            <td class="font-mono text-xs">{a.client_id}</td>
            <td class="text-xs">{formatList(a.grant_types)}</td>
            <td class="font-mono text-xs">{formatList(a.redirect_uris)}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(a.updated)}</td>
            <td class="flex gap-1">
              <button class="btn btn-ghost btn-xs" onclick={() => openEdit(a)} aria-label={`Edit ${a.name}`}>
                Edit
              </button>
              <button
                class="btn btn-ghost btn-xs"
                onclick={() => { pending = { kind: 'rotate', clientID: a.client_id, name: a.name }; }}
                aria-label={`Rotate secret of ${a.name}`}
              >
                Rotate secret
              </button>
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={() => { pending = { kind: 'delete', clientID: a.client_id, name: a.name }; }}
                aria-label={`Delete ${a.name}`}
              >
                Delete
              </button>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}
  </section>
</div>

<!-- Edit dialog -->
{#if editing !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit application">
    <form class="modal-box space-y-2" onsubmit={saveEdit}>
      <h3 class="text-base font-semibold">Edit {editing.name}</h3>
      <input class="input input-bordered input-sm w-full" aria-label="Edit name" bind:value={editName} />
      <textarea
        class="textarea textarea-bordered w-full font-mono text-xs"
        rows="3"
        aria-label="Edit redirect URIs"
        bind:value={editRedirects}
      ></textarea>
      <div class="flex flex-wrap gap-4 text-sm">
        {#each GRANT_TYPES as g (g)}
          <label class="flex items-center gap-1">
            <input type="checkbox" class="checkbox checkbox-sm" value={g} bind:group={editGrants} />
            {g}
          </label>
        {/each}
      </div>
      <div class="modal-action">
        <button type="button" class="btn btn-ghost btn-sm" onclick={() => { editing = null; }}>Cancel</button>
        <button type="submit" class="btn btn-primary btn-sm" disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </form>
  </div>
{/if}

<!-- Rotate / delete confirmation dialog -->
{#if pending !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">
        {pending.kind === 'rotate' ? 'Rotate Client Secret' : 'Delete Application'}
      </h3>
      <p class="text-sm text-base-content/70">
        {#if pending.kind === 'rotate'}
          Rotate the secret of <span class="font-semibold">{pending.name}</span>? The current secret stops working.
        {:else}
          Delete <span class="font-semibold">{pending.name}</span>? Its clients can no longer obtain tokens.
        {/if}
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { pending = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={working} onclick={confirmPending}>
          {pending.kind === 'rotate' ? 'Rotate' : 'Delete'}
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

<!-- One-time client secret dialog -->
{#if secret !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Client secret">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-2">Client secret for {secret.clientID}</h3>
      <p class="text-sm text-warning mb-2">Copy it now: this secret cannot be shown again.</p>
      <code class="block break-all bg-base-200 p-2 rounded font-mono text-xs" data-testid="client-secret">{secret.value}</code>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={copySecret}>Copy</button>
        <button class="btn btn-primary btn-sm" onclick={closeSecret}>Done</button>
      </div>
    </div>
  </div>
{/if}
