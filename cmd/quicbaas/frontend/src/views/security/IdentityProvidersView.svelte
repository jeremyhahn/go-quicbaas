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
  IdentityProvidersView is the top-level Security > Identity Providers route
  component.

  It manages a tenant's federation providers (OIDC, SAML, OAuth2, LDAP)
  through `client.iam`: the operator selects a tenant, then lists its
  providers with a type filter, registers one, edits one (only the changed
  fields are sent), toggles one on or off, and deletes one after a
  confirmation dialog.

  The server never returns a provider's client secret (it comes back empty),
  so the edit dialog never shows or prefills it: the secret field starts empty
  and is sent only when the operator types a new one.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import type {
    IAMExternalProvider,
    IAMProviderType,
    IAMTenant,
    RegisterIAMProviderRequest,
    UpdateIAMProviderRequest,
  } from '@quicbaas/sdk';
  import { Fingerprint } from 'lucide-svelte';

  type TypeFilter = '' | IAMProviderType;

  interface ProviderForm {
    name: string;
    enabled: boolean;
    clientID: string;
    clientSecret: string;
    issuer: string;
    authURL: string;
    tokenURL: string;
    userinfoURL: string;
    jwksURL: string;
    scopes: string;
  }

  const PROVIDER_TYPES: IAMProviderType[] = ['oidc', 'saml', 'oauth2', 'ldap'];

  function emptyForm(): ProviderForm {
    return {
      name: '',
      enabled: true,
      clientID: '',
      clientSecret: '',
      issuer: '',
      authURL: '',
      tokenURL: '',
      userinfoURL: '',
      jwksURL: '',
      scopes: '',
    };
  }

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let tenants: IAMTenant[] = $state([]);
  let selectedTenantID = $state('');
  let tenantsLoading = $state(false);
  let tenantsError: string | null = $state(null);

  let providers: IAMExternalProvider[] = $state([]);
  let providersLoading = $state(false);
  let providersError: string | null = $state(null);
  let typeFilter: TypeFilter = $state('');

  let registerType: IAMProviderType = $state('oidc');
  let registerForm: ProviderForm = $state(emptyForm());
  let registering = $state(false);

  let editTarget: IAMExternalProvider | null = $state(null);
  let editForm: ProviderForm = $state(emptyForm());
  let saving = $state(false);

  let toggling: string | null = $state(null);

  let pendingDelete: IAMExternalProvider | null = $state(null);
  let deleting = $state(false);

  const COLUMNS: Column<IAMExternalProvider>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'type', label: 'Type', sortable: true, searchable: true },
    { key: 'enabled', label: 'Enabled' },
    { key: 'issuer_url', label: 'Issuer URL', searchable: true },
    { key: 'client_id', label: 'Client ID', searchable: true },
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
    void loadProviders();
  }

  async function loadProviders(): Promise<void> {
    if (selectedTenantID === '') {
      providers = [];
      return;
    }
    providersLoading = true;
    providersError = null;
    try {
      const client = await getSDKClient();
      const resp = await client.iam.listProviders(selectedTenantID, { type: typeFilter || undefined });
      providers = resp.providers ?? [];
    } catch (e) {
      providersError = e instanceof Error ? e.message : 'Failed to load identity providers';
    } finally {
      providersLoading = false;
    }
  }

  $effect(() => {
    void loadTenants();
  });

  // ---------------------------------------------------------------------------
  // Register
  // ---------------------------------------------------------------------------

  function splitScopes(s: string): string[] {
    return s.split(/\s+/).filter((x) => x !== '');
  }

  async function registerProvider(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const f = registerForm;
    const name = f.name.trim();
    const clientID = f.clientID.trim();
    if (!name || !clientID || selectedTenantID === '') return;
    const req: RegisterIAMProviderRequest = {
      name,
      type: registerType,
      enabled: f.enabled,
      client_id: clientID,
      client_secret: f.clientSecret,
    };
    if (f.issuer.trim()) req.issuer = f.issuer.trim();
    if (f.authURL.trim()) req.auth_url = f.authURL.trim();
    if (f.tokenURL.trim()) req.token_url = f.tokenURL.trim();
    if (f.userinfoURL.trim()) req.userinfo_url = f.userinfoURL.trim();
    if (f.jwksURL.trim()) req.jwks_url = f.jwksURL.trim();
    const scopes = splitScopes(f.scopes);
    if (scopes.length > 0) req.scopes = scopes;
    registering = true;
    try {
      const client = await getSDKClient();
      await client.iam.registerProvider(selectedTenantID, req);
      showToast('success', `Identity provider ${name} registered`);
      registerForm = emptyForm();
      void loadProviders();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Registering the identity provider failed');
    } finally {
      registering = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Edit
  // ---------------------------------------------------------------------------

  function openEdit(p: IAMExternalProvider): void {
    editTarget = p;
    editForm = {
      name: p.name,
      enabled: p.enabled,
      clientID: p.client_id,
      // The server returns the secret redacted: never prefill it.
      clientSecret: '',
      issuer: p.issuer_url ?? '',
      authURL: p.auth_url ?? '',
      tokenURL: p.token_url ?? '',
      userinfoURL: p.userinfo_url ?? '',
      jwksURL: p.jwks_url ?? '',
      scopes: (p.scopes ?? []).join(' '),
    };
  }

  function changedFields(p: IAMExternalProvider, f: ProviderForm): UpdateIAMProviderRequest {
    const req: UpdateIAMProviderRequest = {};
    if (f.name.trim() !== p.name) req.name = f.name.trim();
    if (f.enabled !== p.enabled) req.enabled = f.enabled;
    if (f.clientID.trim() !== p.client_id) req.client_id = f.clientID.trim();
    if (f.clientSecret !== '') req.client_secret = f.clientSecret;
    if (f.issuer.trim() !== (p.issuer_url ?? '')) req.issuer = f.issuer.trim();
    if (f.authURL.trim() !== (p.auth_url ?? '')) req.auth_url = f.authURL.trim();
    if (f.tokenURL.trim() !== (p.token_url ?? '')) req.token_url = f.tokenURL.trim();
    if (f.userinfoURL.trim() !== (p.userinfo_url ?? '')) req.userinfo_url = f.userinfoURL.trim();
    if (f.jwksURL.trim() !== (p.jwks_url ?? '')) req.jwks_url = f.jwksURL.trim();
    const scopes = splitScopes(f.scopes);
    if (scopes.join(' ') !== (p.scopes ?? []).join(' ')) req.scopes = scopes;
    return req;
  }

  async function saveEdit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    const target = editTarget;
    if (!target) return;
    const req = changedFields(target, editForm);
    if (Object.keys(req).length === 0) {
      editTarget = null;
      return;
    }
    saving = true;
    try {
      const client = await getSDKClient();
      await client.iam.updateProvider(selectedTenantID, target.id, req);
      showToast('success', `Identity provider ${req.name ?? target.name} updated`);
      editTarget = null;
      void loadProviders();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Updating the identity provider failed');
    } finally {
      saving = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Toggle / delete
  // ---------------------------------------------------------------------------

  async function toggleEnabled(p: IAMExternalProvider): Promise<void> {
    const enabled = !p.enabled;
    toggling = p.id;
    try {
      const client = await getSDKClient();
      await client.iam.updateProvider(selectedTenantID, p.id, { enabled });
      showToast('success', `Identity provider ${p.name} ${enabled ? 'enabled' : 'disabled'}`);
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Updating the identity provider failed');
    } finally {
      toggling = null;
      void loadProviders();
    }
  }

  async function confirmDelete(): Promise<void> {
    const target = pendingDelete;
    if (!target) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.iam.deleteProvider(selectedTenantID, target.id);
      showToast('success', `Identity provider ${target.name} deleted`);
      pendingDelete = null;
      void loadProviders();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Delete failed');
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function formatDateTime(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleString();
  }
</script>

{#snippet urlFields(f: ProviderForm, prefix: string)}
  <div class="grid grid-cols-1 md:grid-cols-2 gap-2">
    <input class="input input-bordered input-sm" placeholder="Issuer URL" aria-label={`${prefix} issuer URL`} bind:value={f.issuer} />
    <input class="input input-bordered input-sm" placeholder="Auth URL (optional)" aria-label={`${prefix} auth URL`} bind:value={f.authURL} />
    <input class="input input-bordered input-sm" placeholder="Token URL (optional)" aria-label={`${prefix} token URL`} bind:value={f.tokenURL} />
    <input class="input input-bordered input-sm" placeholder="Userinfo URL (optional)" aria-label={`${prefix} userinfo URL`} bind:value={f.userinfoURL} />
    <input class="input input-bordered input-sm" placeholder="JWKS URL (optional)" aria-label={`${prefix} JWKS URL`} bind:value={f.jwksURL} />
    <input class="input input-bordered input-sm" placeholder="Scopes (space-separated)" aria-label={`${prefix} scopes`} bind:value={f.scopes} />
  </div>
{/snippet}

<div class="p-6 space-y-6">
  <PageHeader title="Identity Providers" subtitle="A tenant's federation providers: OIDC, SAML, OAuth2 and LDAP" />

  <div class="flex flex-wrap items-center gap-3">
    <label class="flex items-center gap-2 text-sm">
      <span>Tenant</span>
      <select
        class="select select-bordered select-sm"
        aria-label="Tenant"
        bind:value={selectedTenantID}
        disabled={tenantsLoading}
        onchange={() => { void loadProviders(); }}
      >
        {#each tenants as t (t.id)}
          <option value={t.id}>{t.name || t.id}</option>
        {/each}
      </select>
    </label>
    <label class="flex items-center gap-2 text-sm">
      <span>Type</span>
      <select
        class="select select-bordered select-sm"
        aria-label="Provider type filter"
        bind:value={typeFilter}
        onchange={() => { void loadProviders(); }}
      >
        <option value="">All</option>
        {#each PROVIDER_TYPES as t (t)}
          <option value={t}>{t}</option>
        {/each}
      </select>
    </label>
  </div>

  {#if tenantsError}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{tenantsError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadTenants(); }}>Retry</button>
    </div>
  {/if}

  <form class="card bg-base-200 p-4 space-y-2" aria-label="Register identity provider" onsubmit={registerProvider}>
    <h2 class="text-lg font-semibold">Register</h2>
    <div class="flex flex-wrap gap-2 items-center">
      <input class="input input-bordered input-sm" placeholder="Name" aria-label="Provider name" bind:value={registerForm.name} />
      <select class="select select-bordered select-sm" aria-label="Provider type" bind:value={registerType}>
        {#each PROVIDER_TYPES as t (t)}
          <option value={t}>{t}</option>
        {/each}
      </select>
      <label class="flex items-center gap-1 text-sm">
        <input type="checkbox" class="toggle toggle-sm" aria-label="Provider enabled" bind:checked={registerForm.enabled} />
        Enabled
      </label>
      <input class="input input-bordered input-sm" placeholder="Client ID" aria-label="Provider client ID" bind:value={registerForm.clientID} />
      <input
        type="password"
        class="input input-bordered input-sm"
        placeholder="Client secret"
        aria-label="Provider client secret"
        autocomplete="new-password"
        bind:value={registerForm.clientSecret}
      />
    </div>
    {@render urlFields(registerForm, 'Provider')}
    <button
      type="submit"
      class="btn btn-primary btn-sm"
      disabled={registering || selectedTenantID === '' || !registerForm.name.trim() || !registerForm.clientID.trim()}
    >
      {registering ? 'Registering…' : 'Register Provider'}
    </button>
  </form>

  <section class="space-y-3" aria-label="Identity providers">
    {#if providersError}
      <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
        <span>{providersError}</span>
        <button class="btn btn-xs btn-ghost" onclick={() => { void loadProviders(); }}>Retry</button>
      </div>
    {/if}

    {#if providersLoading}
      <div class="overflow-x-auto rounded-box">
        <table class="table table-sm" aria-label="Identity providers loading">
          <thead>
            <tr><th>Name</th><th>Type</th><th>Enabled</th><th>Issuer URL</th><th>Client ID</th><th>Updated</th><th></th></tr>
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
    {:else if !providersError}
      <DataTable
        columns={COLUMNS}
        rows={providers}
        rowKey={(p: IAMExternalProvider) => p.id}
        urlKey="identityproviders"
        ariaLabel="Identity providers"
        searchPlaceholder="Search identity providers..."
        searchLabel="Search identity providers"
        noMatchText="No identity providers match the search."
      >
        {#snippet empty()}
          <EmptyState
            icon={Fingerprint}
            title="No identity providers"
            description={selectedTenantID === '' ? 'Select a tenant.' : 'Register an OIDC, SAML, OAuth2 or LDAP provider.'}
          />
        {/snippet}

        {#snippet row(p: IAMExternalProvider)}
          <tr class="hover">
            <td class="text-sm">{p.name}</td>
            <td><span class="badge badge-ghost badge-sm">{p.type}</span></td>
            <td>
              <input
                type="checkbox"
                class="toggle toggle-sm"
                checked={p.enabled}
                disabled={toggling === p.id}
                aria-label={`Enabled ${p.name}`}
                onchange={() => { void toggleEnabled(p); }}
              />
            </td>
            <td class="font-mono text-xs">{p.issuer_url || '—'}</td>
            <td class="font-mono text-xs">{p.client_id || '—'}</td>
            <td class="text-xs text-base-content/50">{formatDateTime(p.updated)}</td>
            <td class="flex gap-1">
              <button class="btn btn-ghost btn-xs" onclick={() => { openEdit(p); }} aria-label={`Edit provider ${p.name}`}>
                Edit
              </button>
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={() => { pendingDelete = p; }}
                aria-label={`Delete provider ${p.name}`}
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
{#if editTarget !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Edit identity provider">
    <form class="modal-box max-w-2xl space-y-2" onsubmit={saveEdit}>
      <h3 class="text-base font-semibold mb-2">Edit {editTarget.name} ({editTarget.type})</h3>
      <div class="flex flex-wrap gap-2 items-center">
        <input class="input input-bordered input-sm" placeholder="Name" aria-label="Edit name" bind:value={editForm.name} />
        <label class="flex items-center gap-1 text-sm">
          <input type="checkbox" class="toggle toggle-sm" aria-label="Edit enabled" bind:checked={editForm.enabled} />
          Enabled
        </label>
        <input class="input input-bordered input-sm" placeholder="Client ID" aria-label="Edit client ID" bind:value={editForm.clientID} />
        <input
          type="password"
          class="input input-bordered input-sm"
          placeholder="New client secret (leave empty to keep)"
          aria-label="Edit client secret"
          autocomplete="new-password"
          bind:value={editForm.clientSecret}
        />
      </div>
      {@render urlFields(editForm, 'Edit')}
      <div class="modal-action">
        <button type="button" class="btn btn-ghost btn-sm" onclick={() => { editTarget = null; }}>Cancel</button>
        <button type="submit" class="btn btn-primary btn-sm" disabled={saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </form>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { editTarget = null; }}
      onkeydown={() => { editTarget = null; }}
    ></div>
  </div>
{/if}

<!-- Delete confirmation dialog -->
{#if pendingDelete !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Identity Provider</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-semibold">{pendingDelete.name}</span>? Users stop signing in through it.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { pendingDelete = null; }}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deleting} onclick={confirmDelete}>
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { pendingDelete = null; }}
      onkeydown={() => { pendingDelete = null; }}
    ></div>
  </div>
{/if}
