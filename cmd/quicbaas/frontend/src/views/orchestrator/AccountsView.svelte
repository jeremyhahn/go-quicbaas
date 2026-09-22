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
  AccountsView manages the tenant's pooled provider account credentials —
  Anthropic/Claude, OpenAI/Codex, and OpenCode logins an agent session can be
  spawned against. Secret material is write-only: create and rotate submit a
  credential envelope, but no response from any transport ever contains one,
  so this view never has anything to display or leak.
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { formatRelative } from '../../lib/time.ts';
  import { providerLabel, refreshPolicyLabel, KNOWN_ACCOUNT_PROVIDERS } from '../../lib/accountLabels.ts';
  import type {
    Account,
    AccountProvider,
    AccountRefreshPolicy,
    AccountCredential,
    CreateAccountRequest,
    RotateAccountCredentialRequest,
    AccountUsage,
    Project,
  } from '@quicbaas/sdk';
  import {
    UserCog,
    Plus,
    RefreshCw,
    Trash2,
    Star,
    RotateCw,
    Ban,
    Activity,
    CircleAlert,
  } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import FilterBar from '../../components/core/FilterBar.svelte';
  import { computeFacets, filterByFacets } from '../../lib/filters.ts';
  import { FilterState } from '../../lib/filterState.svelte.ts';
  import { accountFilters } from '../../lib/listFilters.ts';

  // ---------------------------------------------------------------------------
  // Vocabulary — provider / status / refresh-policy labels
  // ---------------------------------------------------------------------------

  const PROVIDERS: readonly AccountProvider[] = KNOWN_ACCOUNT_PROVIDERS;
  const REFRESH_POLICIES: AccountRefreshPolicy[] = ['oauth_refresh', 'static_token'];

  function statusVariant(status: string): string {
    switch (status) {
      case 'active':  return 'badge-success';
      case 'revoked': return 'badge-ghost';
      case 'error':   return 'badge-error';
      default:        return 'badge-ghost';
    }
  }

  /** projectName resolves a project_id to its display name, or "Tenant-wide" when unset. */
  function projectName(projectId: string | undefined): string {
    if (!projectId) return 'Tenant-wide';
    const p = projects.find((x) => x.id === projectId);
    return p?.name ?? projectId.slice(0, 8);
  }

  // ---------------------------------------------------------------------------
  // List state
  // ---------------------------------------------------------------------------

  let accounts = $state<Account[]>([]);
  let projects = $state<Project[]>([]);
  let loading = $state(false);
  let loadError = $state('');

  // ---------------------------------------------------------------------------
  // Finding one account
  //
  // "Which accounts are revoked" and "which are Anthropic" are the two
  // questions this page exists to answer and could not, so the same FilterBar
  // and the same URL grammar (`#/accounts?accounts_status=revoked`) every
  // other list carries sit over the collection this view holds.
  // ---------------------------------------------------------------------------

  const ACCOUNT_FILTERS = accountFilters<Account>(
    PROVIDERS.map((provider) => ({ value: provider, label: providerLabel(provider) })),
    (account) => account.provider,
    (account) => account.status,
  );

  const accountNarrowing = new FilterState('accounts', ACCOUNT_FILTERS);

  let accountSearch = $state('');

  /** The searched set — what the facet counts are taken over. */
  const searchedAccounts = $derived.by(() => {
    const needle = accountSearch.trim().toLowerCase();
    if (needle === '') return accounts;
    return accounts.filter((account) =>
      [account.display_name, account.id, account.provider, account.status]
        .some((field) => (field ?? '').toLowerCase().includes(needle)),
    );
  });

  const accountFacets = $derived(computeFacets(searchedAccounts, ACCOUNT_FILTERS));

  const visibleAccounts = $derived(
    filterByFacets(searchedAccounts, ACCOUNT_FILTERS, accountNarrowing.values),
  );

  /** narrowed says the emptiness is a control's doing, not the tenant's. */
  const accountsNarrowed = $derived(
    accountSearch.trim() !== '' || Object.keys(accountNarrowing.values).length > 0,
  );

  async function fetchAll(): Promise<void> {
    loading = true;
    loadError = '';
    try {
      const client = await getSDKClient();
      const [acctResp, projResp] = await Promise.all([
        client.accounts.listAccounts({ tenant_id: orchestratorStore.activeTenantID }),
        client.projects
          .listProjects(orchestratorStore.activeTenantID)
          .catch(() => ({ projects: [], total: 0 })),
      ]);
      accounts = acctResp.accounts ?? [];
      projects = projResp.projects ?? [];
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      loadError = `Failed to load accounts: ${msg}`;
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchAll(); });

  // ---------------------------------------------------------------------------
  // Create account dialog
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let createProvider = $state<AccountProvider>('claude');
  let createDisplayName = $state('');
  let createProjectId = $state('');
  let createDefault = $state(false);
  let createRefreshPolicy = $state<AccountRefreshPolicy>('oauth_refresh');

  // Credential fields — the set that is actually sent depends on
  // createRefreshPolicy (see buildCredential). refreshToken/tokenEndpoint are
  // never read for "static_token", so they can never reach the request body
  // even if a value happens to be sitting in these fields from a prior policy
  // selection.
  let credAccessToken = $state('');
  let credRefreshToken = $state('');
  let credTokenEndpoint = $state('');
  let credClientId = $state('');
  let credScopes = $state('');
  let credAccessTokenEnvKey = $state('');
  let credEnvLines = $state('');
  let credExpiresAt = $state('');

  let createError = $state('');
  let creating = $state(false);

  function openCreateDialog(): void {
    showCreateDialog = true;
    createProvider = 'claude';
    createDisplayName = '';
    createProjectId = '';
    createDefault = false;
    createRefreshPolicy = 'oauth_refresh';
    credAccessToken = '';
    credRefreshToken = '';
    credTokenEndpoint = '';
    credClientId = '';
    credScopes = '';
    credAccessTokenEnvKey = '';
    credEnvLines = '';
    credExpiresAt = '';
    createError = '';
  }

  /** parseEnvLines turns "KEY=VALUE" lines into a Record, skipping blank/malformed lines. */
  function parseEnvLines(text: string): Record<string, string> | undefined {
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

  /**
   * buildCredential assembles the envelope for the current refresh policy.
   * refresh_token / token_endpoint are only ever populated for oauth_refresh
   * — the server rejects a static_token account carrying a refresh_token, so
   * this function must never emit one for that policy.
   */
  function buildCredential(policy: AccountRefreshPolicy): AccountCredential {
    const cred: AccountCredential = {
      access_token: credAccessToken.trim() || undefined,
      access_token_env_key: credAccessTokenEnvKey.trim() || undefined,
      env: parseEnvLines(credEnvLines),
      expires_at: credExpiresAt.trim() ? new Date(credExpiresAt.trim()).toISOString() : undefined,
    };
    if (policy === 'oauth_refresh') {
      cred.refresh_token = credRefreshToken.trim() || undefined;
      cred.token_endpoint = credTokenEndpoint.trim() || undefined;
      cred.client_id = credClientId.trim() || undefined;
      const scopes = credScopes.split(',').map((s) => s.trim()).filter(Boolean);
      cred.scopes = scopes.length > 0 ? scopes : undefined;
    }
    return cred;
  }

  async function submitCreate(): Promise<void> {
    if (!createDisplayName.trim()) {
      createError = 'Display name is required.';
      return;
    }
    if (createRefreshPolicy === 'oauth_refresh' && !credRefreshToken.trim()) {
      createError = 'Refresh token is required for the OAuth Refresh policy.';
      return;
    }
    if (createRefreshPolicy === 'oauth_refresh' && !credTokenEndpoint.trim()) {
      createError = 'Token endpoint is required for the OAuth Refresh policy.';
      return;
    }
    if (createRefreshPolicy === 'static_token' && !credAccessToken.trim()) {
      createError = 'Access token is required for the Static Token policy.';
      return;
    }
    creating = true;
    createError = '';
    try {
      const client = await getSDKClient();
      const req: CreateAccountRequest = {
        tenant_id: orchestratorStore.activeTenantID,
        project_id: createProjectId || undefined,
        provider: createProvider,
        display_name: createDisplayName.trim(),
        default: createDefault,
        refresh_policy: createRefreshPolicy,
        credential: buildCredential(createRefreshPolicy),
      };
      await client.accounts.createAccount(req);
      showToast('success', `Account "${createDisplayName.trim()}" created`);
      showCreateDialog = false;
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      createError = `Create failed: ${msg}`;
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotate credential dialog
  // ---------------------------------------------------------------------------

  let rotateTarget = $state<Account | null>(null);
  let rotateAccessToken = $state('');
  let rotateRefreshToken = $state('');
  let rotateTokenEndpoint = $state('');
  let rotateClientId = $state('');
  let rotateScopes = $state('');
  let rotateAccessTokenEnvKey = $state('');
  let rotateEnvLines = $state('');
  let rotateExpiresAt = $state('');
  let rotateError = $state('');
  let rotating = $state(false);

  function openRotateDialog(account: Account): void {
    rotateTarget = account;
    rotateAccessToken = '';
    rotateRefreshToken = '';
    rotateTokenEndpoint = '';
    rotateClientId = '';
    rotateScopes = '';
    rotateAccessTokenEnvKey = '';
    rotateEnvLines = '';
    rotateExpiresAt = '';
    rotateError = '';
  }

  async function submitRotate(): Promise<void> {
    if (!rotateTarget) return;
    const policy = rotateTarget.refresh_policy;
    if (policy === 'oauth_refresh' && !rotateRefreshToken.trim()) {
      rotateError = 'Refresh token is required for the OAuth Refresh policy.';
      return;
    }
    if (policy === 'oauth_refresh' && !rotateTokenEndpoint.trim()) {
      rotateError = 'Token endpoint is required for the OAuth Refresh policy.';
      return;
    }
    if (policy === 'static_token' && !rotateAccessToken.trim()) {
      rotateError = 'Access token is required for the Static Token policy.';
      return;
    }
    rotating = true;
    rotateError = '';
    try {
      const client = await getSDKClient();
      const cred: AccountCredential = {
        access_token: rotateAccessToken.trim() || undefined,
        access_token_env_key: rotateAccessTokenEnvKey.trim() || undefined,
        env: parseEnvLines(rotateEnvLines),
        expires_at: rotateExpiresAt.trim() ? new Date(rotateExpiresAt.trim()).toISOString() : undefined,
      };
      if (policy === 'oauth_refresh') {
        cred.refresh_token = rotateRefreshToken.trim() || undefined;
        cred.token_endpoint = rotateTokenEndpoint.trim() || undefined;
        cred.client_id = rotateClientId.trim() || undefined;
        const scopes = rotateScopes.split(',').map((s) => s.trim()).filter(Boolean);
        cred.scopes = scopes.length > 0 ? scopes : undefined;
      }
      const req: RotateAccountCredentialRequest = {
        tenant_id: orchestratorStore.activeTenantID,
        account_id: rotateTarget.id,
        credential: cred,
      };
      await client.accounts.rotateAccountCredential(req);
      showToast('success', `Credential rotated for "${rotateTarget.display_name || rotateTarget.id}"`);
      rotateTarget = null;
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      rotateError = `Rotate failed: ${msg}`;
    } finally {
      rotating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Set default / revoke / delete
  // ---------------------------------------------------------------------------

  let busyIds = $state<Set<string>>(new Set());

  function markBusy(id: string, busy: boolean): void {
    const next = new Set(busyIds);
    if (busy) next.add(id); else next.delete(id);
    busyIds = next;
  }

  async function setDefault(account: Account): Promise<void> {
    markBusy(account.id, true);
    try {
      const client = await getSDKClient();
      await client.accounts.setDefaultAccount(orchestratorStore.activeTenantID, account.id);
      showToast('success', `"${account.display_name || account.id}" is now the default`);
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Set default failed: ${msg}`);
    } finally {
      markBusy(account.id, false);
    }
  }

  async function revoke(account: Account): Promise<void> {
    markBusy(account.id, true);
    try {
      const client = await getSDKClient();
      await client.accounts.revokeAccount({
        tenant_id: orchestratorStore.activeTenantID,
        account_id: account.id,
      });
      showToast('success', `"${account.display_name || account.id}" revoked`);
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Revoke failed: ${msg}`);
    } finally {
      markBusy(account.id, false);
    }
  }

  let deleteTarget = $state<Account | null>(null);
  let deleting = $state(false);

  async function confirmDelete(): Promise<void> {
    if (!deleteTarget || deleting) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.accounts.deleteAccount(orchestratorStore.activeTenantID, deleteTarget.id);
      showToast('success', `Account "${deleteTarget.display_name || deleteTarget.id}" deleted`);
      deleteTarget = null;
      await fetchAll();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast('error', `Delete failed: ${msg}`);
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Usage dialog
  // ---------------------------------------------------------------------------

  let usageTarget = $state<Account | null>(null);
  let usageReport = $state<AccountUsage | null>(null);
  let usageError = $state('');
  let usageLoading = $state(false);

  async function openUsage(account: Account): Promise<void> {
    usageTarget = account;
    usageReport = null;
    usageError = '';
    usageLoading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.accounts.getAccountUsage(orchestratorStore.activeTenantID, account.id);
      usageReport = resp.usage;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      usageError = `Failed to load usage: ${msg}`;
    } finally {
      usageLoading = false;
    }
  }

  function usagePercent(u: AccountUsage): number {
    return Math.min(100, Math.max(0, u.percent_used));
  }
</script>

<!-- Create account dialog -->
{#if showCreateDialog}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Create account"
    onkeydown={(e) => { if (e.key === 'Escape') { showCreateDialog = false; } }}
  >
    <div class="modal-box max-w-md">
      <h3 class="font-bold text-base">New Account</h3>
      <div class="form-control mt-4 gap-3">
        <div>
          <label class="label py-1" for="acct-provider">
            <span class="label-text text-xs">Provider <span class="text-error">*</span></span>
          </label>
          <select
            id="acct-provider"
            class="select select-bordered select-sm w-full"
            bind:value={createProvider}
          >
            {#each PROVIDERS as p (p)}
              <option value={p}>{providerLabel(p)}</option>
            {/each}
          </select>
        </div>
        <div>
          <label class="label py-1" for="acct-display-name">
            <span class="label-text text-xs">Display Name <span class="text-error">*</span></span>
          </label>
          <input
            id="acct-display-name"
            type="text"
            class="input input-bordered input-sm w-full"
            placeholder="alice-work"
            bind:value={createDisplayName}
          />
        </div>
        <div>
          <label class="label py-1" for="acct-project">
            <span class="label-text text-xs">Project (optional)</span>
          </label>
          <select
            id="acct-project"
            class="select select-bordered select-sm w-full"
            bind:value={createProjectId}
          >
            <option value="">Tenant-wide</option>
            {#each projects as p (p.id)}
              <option value={p.id}>{p.name}</option>
            {/each}
          </select>
        </div>
        <div>
          <label class="label py-1" for="acct-refresh-policy">
            <span class="label-text text-xs">Refresh Policy <span class="text-error">*</span></span>
          </label>
          <select
            id="acct-refresh-policy"
            class="select select-bordered select-sm w-full"
            bind:value={createRefreshPolicy}
          >
            {#each REFRESH_POLICIES as p (p)}
              <option value={p}>{refreshPolicyLabel(p)}</option>
            {/each}
          </select>
        </div>
        <div class="flex items-center gap-2 py-1">
          <input
            id="acct-default"
            type="checkbox"
            class="checkbox checkbox-sm"
            bind:checked={createDefault}
          />
          <label class="label-text text-xs" for="acct-default">
            Make this the default account for its provider scope
          </label>
        </div>

        <div class="divider my-1 text-xs">Credential</div>

        {#if createRefreshPolicy === 'static_token'}
          <div>
            <label class="label py-1" for="acct-access-token">
              <span class="label-text text-xs">Access Token <span class="text-error">*</span></span>
            </label>
            <input
              id="acct-access-token"
              type="password"
              class="input input-bordered input-sm w-full font-mono"
              placeholder="sk-... / CLAUDE_CODE_OAUTH_TOKEN value"
              bind:value={credAccessToken}
            />
          </div>
        {:else}
          <div>
            <label class="label py-1" for="acct-refresh-token">
              <span class="label-text text-xs">Refresh Token <span class="text-error">*</span></span>
            </label>
            <input
              id="acct-refresh-token"
              type="password"
              class="input input-bordered input-sm w-full font-mono"
              bind:value={credRefreshToken}
            />
          </div>
          <div>
            <label class="label py-1" for="acct-token-endpoint">
              <span class="label-text text-xs">Token Endpoint <span class="text-error">*</span></span>
            </label>
            <input
              id="acct-token-endpoint"
              type="text"
              class="input input-bordered input-sm w-full"
              placeholder="https://provider.example.com/oauth/token"
              bind:value={credTokenEndpoint}
            />
          </div>
          <div>
            <label class="label py-1" for="acct-client-id">
              <span class="label-text text-xs">Client ID</span>
            </label>
            <input
              id="acct-client-id"
              type="text"
              class="input input-bordered input-sm w-full"
              bind:value={credClientId}
            />
          </div>
          <div>
            <label class="label py-1" for="acct-scopes">
              <span class="label-text text-xs">Scopes (comma-separated)</span>
            </label>
            <input
              id="acct-scopes"
              type="text"
              class="input input-bordered input-sm w-full"
              placeholder="read, write"
              bind:value={credScopes}
            />
          </div>
          <div>
            <label class="label py-1" for="acct-access-token-oauth">
              <span class="label-text text-xs">Access Token (optional)</span>
            </label>
            <input
              id="acct-access-token-oauth"
              type="password"
              class="input input-bordered input-sm w-full font-mono"
              bind:value={credAccessToken}
            />
          </div>
        {/if}

        <details class="collapse collapse-arrow bg-base-100 rounded-box">
          <summary class="collapse-title text-xs font-semibold py-2 min-h-0">Advanced</summary>
          <div class="collapse-content flex flex-col gap-3 pt-1">
            <div>
              <label class="label py-1" for="acct-access-token-env-key">
                <span class="label-text text-xs">Access Token Env Key</span>
              </label>
              <input
                id="acct-access-token-env-key"
                type="text"
                class="input input-bordered input-sm w-full font-mono"
                placeholder="CLAUDE_CODE_OAUTH_TOKEN"
                bind:value={credAccessTokenEnvKey}
              />
            </div>
            <div>
              <label class="label py-1" for="acct-expires-at">
                <span class="label-text text-xs">Credential Expires At</span>
              </label>
              <input
                id="acct-expires-at"
                type="datetime-local"
                class="input input-bordered input-sm w-full"
                bind:value={credExpiresAt}
              />
            </div>
            <div>
              <label class="label py-1" for="acct-env-lines">
                <span class="label-text text-xs">Env Vars (one KEY=VALUE per line)</span>
              </label>
              <textarea
                id="acct-env-lines"
                class="textarea textarea-bordered textarea-sm w-full font-mono resize-none"
                rows="3"
                placeholder="CODEX_API_KEY=sk-..."
                bind:value={credEnvLines}
              ></textarea>
            </div>
          </div>
        </details>

        {#if createError}
          <p class="text-xs text-error">{createError}</p>
        {/if}
      </div>
      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { showCreateDialog = false; }}
          disabled={creating}
        >
          Cancel
        </button>
        <button
          class="btn btn-sm btn-primary"
          onclick={() => { void submitCreate(); }}
          disabled={creating}
        >
          {#if creating}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Create
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { showCreateDialog = false; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Rotate credential dialog -->
{#if rotateTarget}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Rotate account credential"
    onkeydown={(e) => { if (e.key === 'Escape') { rotateTarget = null; } }}
  >
    <div class="modal-box max-w-md">
      <h3 class="font-bold text-base">Rotate Credential — {rotateTarget.display_name || rotateTarget.id}</h3>
      <p class="text-xs text-base-content/50 mt-1">
        Credential material is write-only. The current value is never shown or returned by the
        server — submit a replacement below.
      </p>
      <div class="form-control mt-4 gap-3">
        {#if rotateTarget.refresh_policy === 'static_token'}
          <div>
            <label class="label py-1" for="rotate-access-token">
              <span class="label-text text-xs">Access Token <span class="text-error">*</span></span>
            </label>
            <input
              id="rotate-access-token"
              type="password"
              class="input input-bordered input-sm w-full font-mono"
              bind:value={rotateAccessToken}
            />
          </div>
        {:else}
          <div>
            <label class="label py-1" for="rotate-refresh-token">
              <span class="label-text text-xs">Refresh Token <span class="text-error">*</span></span>
            </label>
            <input
              id="rotate-refresh-token"
              type="password"
              class="input input-bordered input-sm w-full font-mono"
              bind:value={rotateRefreshToken}
            />
          </div>
          <div>
            <label class="label py-1" for="rotate-token-endpoint">
              <span class="label-text text-xs">Token Endpoint <span class="text-error">*</span></span>
            </label>
            <input
              id="rotate-token-endpoint"
              type="text"
              class="input input-bordered input-sm w-full"
              bind:value={rotateTokenEndpoint}
            />
          </div>
          <div>
            <label class="label py-1" for="rotate-client-id">
              <span class="label-text text-xs">Client ID</span>
            </label>
            <input
              id="rotate-client-id"
              type="text"
              class="input input-bordered input-sm w-full"
              bind:value={rotateClientId}
            />
          </div>
          <div>
            <label class="label py-1" for="rotate-scopes">
              <span class="label-text text-xs">Scopes (comma-separated)</span>
            </label>
            <input
              id="rotate-scopes"
              type="text"
              class="input input-bordered input-sm w-full"
              bind:value={rotateScopes}
            />
          </div>
          <div>
            <label class="label py-1" for="rotate-access-token-oauth">
              <span class="label-text text-xs">Access Token (optional)</span>
            </label>
            <input
              id="rotate-access-token-oauth"
              type="password"
              class="input input-bordered input-sm w-full font-mono"
              bind:value={rotateAccessToken}
            />
          </div>
        {/if}

        <details class="collapse collapse-arrow bg-base-100 rounded-box">
          <summary class="collapse-title text-xs font-semibold py-2 min-h-0">Advanced</summary>
          <div class="collapse-content flex flex-col gap-3 pt-1">
            <div>
              <label class="label py-1" for="rotate-access-token-env-key">
                <span class="label-text text-xs">Access Token Env Key</span>
              </label>
              <input
                id="rotate-access-token-env-key"
                type="text"
                class="input input-bordered input-sm w-full font-mono"
                bind:value={rotateAccessTokenEnvKey}
              />
            </div>
            <div>
              <label class="label py-1" for="rotate-expires-at">
                <span class="label-text text-xs">Credential Expires At</span>
              </label>
              <input
                id="rotate-expires-at"
                type="datetime-local"
                class="input input-bordered input-sm w-full"
                bind:value={rotateExpiresAt}
              />
            </div>
            <div>
              <label class="label py-1" for="rotate-env-lines">
                <span class="label-text text-xs">Env Vars (one KEY=VALUE per line)</span>
              </label>
              <textarea
                id="rotate-env-lines"
                class="textarea textarea-bordered textarea-sm w-full font-mono resize-none"
                rows="3"
                bind:value={rotateEnvLines}
              ></textarea>
            </div>
          </div>
        </details>

        {#if rotateError}
          <p class="text-xs text-error">{rotateError}</p>
        {/if}
      </div>
      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { rotateTarget = null; }}
          disabled={rotating}
        >
          Cancel
        </button>
        <button
          class="btn btn-sm btn-primary"
          onclick={() => { void submitRotate(); }}
          disabled={rotating}
        >
          {#if rotating}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Rotate
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { rotateTarget = null; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Delete confirm dialog -->
{#if deleteTarget}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Confirm delete account"
    onkeydown={(e) => { if (e.key === 'Escape') { deleteTarget = null; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Delete Account?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Account <span class="font-semibold">{deleteTarget.display_name || deleteTarget.id}</span>
        and its stored credential will be permanently removed. Sessions already running with this
        account are not affected.
      </p>
      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { deleteTarget = null; }}
          disabled={deleting}
        >
          Cancel
        </button>
        <button
          class="btn btn-sm btn-error"
          onclick={() => { void confirmDelete(); }}
          disabled={deleting}
        >
          {#if deleting}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Delete
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { deleteTarget = null; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Usage dialog -->
{#if usageTarget}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Account usage"
    onkeydown={(e) => { if (e.key === 'Escape') { usageTarget = null; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Usage — {usageTarget.display_name || usageTarget.id}</h3>
      {#if usageLoading}
        <div class="skeleton h-16 w-full rounded-box mt-4"></div>
      {:else if usageError}
        <p class="text-xs text-error mt-3">{usageError}</p>
      {:else if usageReport}
        {@const pct = usagePercent(usageReport)}
        <div class="mt-4 space-y-3">
          <div>
            <div class="flex justify-between text-xs text-base-content/50 mb-1">
              <span>{usageReport.total_tokens.toLocaleString()} tokens</span>
              <span>{usageReport.budget_limit > 0 ? usageReport.budget_limit.toLocaleString() : 'unlimited'}</span>
            </div>
            <progress
              class="progress progress-primary w-full"
              class:progress-error={pct > 80}
              class:progress-warning={pct > 60 && pct <= 80}
              value={pct}
              max="100"
            ></progress>
          </div>
          {#if usageReport.by_agent && Object.keys(usageReport.by_agent).length > 0}
            <div>
              <div class="text-xs font-semibold text-base-content/40 uppercase tracking-wide mb-1">By Agent</div>
              <table class="table table-xs w-full">
                <tbody>
                  {#each Object.entries(usageReport.by_agent) as [agentId, tokens] (agentId)}
                    <tr>
                      <td class="font-mono text-xs">{agentId.slice(0, 12)}</td>
                      <td class="text-right">{tokens.toLocaleString()}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}
        </div>
      {/if}
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={() => { usageTarget = null; }}>Close</button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { usageTarget = null; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Main content -->
<div class="p-6 space-y-5 max-w-5xl mx-auto">

  <PageHeader
    title="Accounts"
    subtitle="Pooled provider credentials agent sessions can be spawned against"
    icon={UserCog}
  >
    {#snippet children()}
      {#if !loading}
        <span class="badge badge-sm badge-ghost" aria-label="{accounts.length} accounts">{accounts.length}</span>
      {/if}
      <button
        class="btn btn-sm btn-ghost gap-1"
        onclick={() => { void fetchAll(); }}
        disabled={loading}
        aria-label="Refresh accounts"
      >
        <RefreshCw class="h-3 w-3 {loading ? 'animate-spin' : ''}" />
        Refresh
      </button>
      <button
        class="btn btn-sm btn-primary gap-1"
        onclick={openCreateDialog}
        aria-label="Create account"
      >
        <Plus class="h-3 w-3" />
        New Account
      </button>
    {/snippet}
  </PageHeader>

  {#if loadError}
    <div class="alert alert-error text-sm py-2">
      <span>{loadError}</span>
    </div>
  {/if}

  {#if loading && accounts.length === 0}
    <div class="space-y-3">
      {#each Array(3) as _}
        <div class="skeleton h-16 w-full rounded-box"></div>
      {/each}
    </div>

  {:else if !loading && accounts.length === 0}
    <EmptyState
      icon={UserCog}
      title="No accounts yet"
      description="Add a pooled provider account so agent sessions can authenticate without sharing the host's own login."
      actionLabel="New Account"
      onaction={openCreateDialog}
    />

  {:else}
    <div class="space-y-2 mb-3">
      <input
        type="search"
        class="input input-bordered input-sm w-full max-w-xs"
        placeholder="Search accounts..."
        aria-label="Search accounts"
        data-testid="accounts-search"
        bind:value={accountSearch}
      />
      <FilterBar
        fields={ACCOUNT_FILTERS}
        values={accountNarrowing.values}
        facets={accountFacets}
        onchange={(next) => accountNarrowing.apply(next)}
      />
      <div class="text-xs text-base-content/50" data-testid="accounts-count">
        {visibleAccounts.length} of {accounts.length} accounts
      </div>
    </div>

    {#if visibleAccounts.length === 0}
      <!-- Narrowed to nothing is not "no accounts yet" — see AgentFleet. -->
      <div class="text-center py-8 text-sm text-base-content/40" data-testid="accounts-no-match">
        {accountsNarrowed ? 'No accounts match the current filter.' : 'No accounts yet.'}
      </div>
    {/if}

    <div class="space-y-3">
      {#each visibleAccounts as account (account.id)}
        {@const busy = busyIds.has(account.id)}
        <div class="card bg-base-200 rounded-box overflow-hidden">
          <div class="flex items-center gap-3 px-4 py-3">
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="font-semibold text-sm truncate">{account.display_name || account.id}</span>
                <span class="badge badge-xs badge-outline">{providerLabel(account.provider)}</span>
                <span class="badge badge-xs {statusVariant(account.status)}">{account.status}</span>
                {#if account.default}
                  <span class="badge badge-xs badge-primary flex items-center gap-0.5">
                    <Star class="h-2.5 w-2.5" />
                    default
                  </span>
                {/if}
              </div>
              <div class="flex items-center gap-3 text-xs text-base-content/40 mt-0.5">
                <span>{projectName(account.project_id)}</span>
                <span>{refreshPolicyLabel(account.refresh_policy)}</span>
                <span>Last used: {account.last_used_at ? formatRelative(account.last_used_at) : 'never'}</span>
              </div>
              {#if account.status === 'error' && account.last_error}
                <div class="flex items-center gap-1 text-xs text-error mt-1">
                  <CircleAlert class="h-3 w-3 shrink-0" />
                  <span class="truncate" title={account.last_error}>{account.last_error}</span>
                </div>
              {/if}
            </div>

            <div class="flex items-center gap-1 shrink-0">
              <button
                class="btn btn-ghost btn-xs btn-square"
                onclick={() => { void setDefault(account); }}
                disabled={busy || account.default || account.status !== 'active'}
                title="Set as default"
                aria-label="Set {account.display_name || account.id} as default"
              >
                <Star class="h-3.5 w-3.5" />
              </button>
              <button
                class="btn btn-ghost btn-xs btn-square"
                onclick={() => { openRotateDialog(account); }}
                title="Rotate credential"
                aria-label="Rotate credential for {account.display_name || account.id}"
              >
                <RotateCw class="h-3.5 w-3.5" />
              </button>
              <button
                class="btn btn-ghost btn-xs btn-square"
                onclick={() => { void openUsage(account); }}
                title="View usage"
                aria-label="View usage for {account.display_name || account.id}"
              >
                <Activity class="h-3.5 w-3.5" />
              </button>
              <button
                class="btn btn-ghost btn-xs btn-square text-warning hover:bg-warning/10"
                onclick={() => { void revoke(account); }}
                disabled={busy || account.status === 'revoked'}
                title={account.status === 'revoked' ? 'Already revoked' : 'Revoke'}
                aria-label="Revoke {account.display_name || account.id}"
              >
                <Ban class="h-3.5 w-3.5" />
              </button>
              <button
                class="btn btn-ghost btn-xs btn-square text-error hover:bg-error/10"
                onclick={() => { deleteTarget = account; }}
                title="Delete"
                aria-label="Delete {account.display_name || account.id}"
              >
                <Trash2 class="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
