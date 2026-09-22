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
  SecretsView renders an operator console for the secrets engine.

  Actions:
    - List secrets (path, version, updated)
    - Store Secret dialog (path + JSON data editor)
    - Fetch secret: modal showing decoded value
    - Rotate secret per row
    - Delete secret per row (confirm dialog)
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import type { SecretEntry, GetSecretResponse, SecretVersionEntry } from '@quicbaas/sdk';
  import { Lock } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { formatDateTime } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // List state
  // ---------------------------------------------------------------------------

  let secrets: SecretEntry[] = $state([]);

  const SECRET_COLUMNS: Column<SecretEntry>[] = [
    { key: 'path', label: 'Path', sortable: true, searchable: true },
    { key: 'version', label: 'Version', sortable: true },
    { key: 'updated_at', label: 'Updated', sortable: true },
    { key: 'actions', label: 'Actions' },
  ];

  const VERSION_COLUMNS: Column<SecretVersionEntry>[] = [
    { key: 'version', label: 'Version', sortable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'created_by', label: 'Created By', sortable: true },
    { key: 'expires_at', label: 'Expires', sortable: true },
  ];
  let loading = $state(false);
  let error: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Store Secret dialog state
  // ---------------------------------------------------------------------------

  let showStore = $state(false);
  let newPath = $state('');
  let newDataJSON = $state('{\n  "value": ""\n}');
  let storeError: string | null = $state(null);
  let storing = $state(false);

  // ---------------------------------------------------------------------------
  // Fetch secret modal
  // ---------------------------------------------------------------------------

  let fetchedSecret: GetSecretResponse | null = $state(null);
  let fetching: string | null = $state(null);
  let fetchError: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Rotate in-progress tracking
  // ---------------------------------------------------------------------------

  let rotatingPath: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Delete confirm state
  // ---------------------------------------------------------------------------

  let secretToDelete: SecretEntry | null = $state(null);
  let deleting = $state(false);

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadSecrets(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.secrets.listSecrets(200);
      const seen = new Set<string>();
      secrets = resp.secrets.filter((s) => {
        if (seen.has(s.path)) return false;
        seen.add(s.path);
        return true;
      });
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load secrets';
    } finally {
      loading = false;
    }
  }

  $effect(() => { void loadSecrets(); });

  // ---------------------------------------------------------------------------
  // Store secret
  // ---------------------------------------------------------------------------

  async function submitStoreSecret(): Promise<void> {
    storeError = null;
    if (!newPath.trim()) {
      storeError = 'Path is required';
      return;
    }
    let data: Record<string, unknown>;
    try {
      data = JSON.parse(newDataJSON) as Record<string, unknown>;
    } catch {
      storeError = 'Invalid JSON — please fix the data field';
      return;
    }
    storing = true;
    try {
      const client = await getSDKClient();
      await client.secrets.storeSecret({ path: newPath.trim(), data });
      showToast({ message: `Secret stored at "${newPath}"`, type: 'success' });
      showStore = false;
      newPath = '';
      newDataJSON = '{\n  "value": ""\n}';
      void loadSecrets();
    } catch (e) {
      storeError = e instanceof Error ? e.message : 'Failed to store secret';
    } finally {
      storing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Fetch secret
  // ---------------------------------------------------------------------------

  async function fetchSecret(path: string): Promise<void> {
    fetching = path;
    fetchError = null;
    fetchedSecret = null;
    try {
      const client = await getSDKClient();
      fetchedSecret = await client.secrets.getSecret(path);
    } catch (e) {
      fetchError = e instanceof Error ? e.message : 'Failed to fetch secret';
      fetchedSecret = { path, data: {}, version: 0, created_at: '', updated_at: '' };
    } finally {
      fetching = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotate secret (confirm modal)
  // ---------------------------------------------------------------------------

  let secretToRotate: SecretEntry | null = $state(null);

  async function confirmRotateSecret(): Promise<void> {
    if (!secretToRotate) return;
    const path = secretToRotate.path;
    rotatingPath = path;
    try {
      const client = await getSDKClient();
      await client.secrets.rotateSecret(path);
      showToast({ message: `Secret "${path}" rotated`, type: 'success' });
      secretToRotate = null;
      void loadSecrets();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Rotate failed', type: 'error' });
    } finally {
      rotatingPath = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Versions modal
  // ---------------------------------------------------------------------------

  let versionsSecret: SecretEntry | null = $state(null);
  let versions: SecretVersionEntry[] = $state([]);
  let versionsLoading = $state(false);
  let versionsError: string | null = $state(null);

  async function viewVersions(secret: SecretEntry): Promise<void> {
    versionsSecret = secret;
    versions = [];
    versionsError = null;
    versionsLoading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.secrets.listVersions(secret.path);
      versions = resp.versions;
    } catch (e) {
      versionsError = e instanceof Error ? e.message : 'Failed to load versions';
    } finally {
      versionsLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete secret
  // ---------------------------------------------------------------------------

  async function confirmDeleteSecret(): Promise<void> {
    if (!secretToDelete) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.secrets.deleteSecret(secretToDelete.path);
      showToast({ message: `Secret "${secretToDelete.path}" deleted`, type: 'success' });
      secretToDelete = null;
      void loadSecrets();
    } catch (e) {
      showToast({ message: e instanceof Error ? e.message : 'Delete failed', type: 'error' });
    } finally {
      deleting = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Secrets" subtitle="Secrets Engine — Encrypted Secret Storage" icon={Lock}>
    {#snippet children()}
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { showStore = true; }}
        aria-label="Store secret"
      >
        + Store Secret
      </button>
    {/snippet}
  </PageHeader>

  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadSecrets(); }}>Retry</button>
    </div>
  {/if}

  {#if loading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else}
    <DataTable
      columns={SECRET_COLUMNS}
      rows={secrets}
      rowKey={(sec: SecretEntry) => sec.path}
      urlKey="secrets"
      ariaLabel="Secrets"
      searchPlaceholder="Search secrets..."
      searchLabel="Search secrets"
      noMatchText="No secrets match the search."
    >
      {#snippet empty()}
        <EmptyState
          icon={Lock}
          title="No secrets stored — + Store Secret is how you start."
          description="Secrets are encrypted at rest and versioned. Access them securely via the API or CLI."
          actionLabel="+ Store Secret"
          onaction={() => { showStore = true; }}
        />
      {/snippet}

      {#snippet row(secret: SecretEntry)}
        <tr class="hover">
          <td class="font-mono text-xs">{secret.path}</td>
          <td class="text-sm">{secret.version}</td>
          <td class="text-xs text-base-content/50">
            {secret.updated_at ? new Date(secret.updated_at).toLocaleString() : '—'}
          </td>
          <td>
            <div class="flex gap-1">
              <button
                class="btn btn-ghost btn-xs"
                disabled={fetching === secret.path}
                onclick={() => void fetchSecret(secret.path)}
                aria-label={`Fetch secret ${secret.path}`}
              >
                {fetching === secret.path ? '…' : 'Fetch'}
              </button>
              <button
                class="btn btn-ghost btn-xs"
                onclick={() => void viewVersions(secret)}
                aria-label={`View versions of secret ${secret.path}`}
              >
                Versions
              </button>
              <button
                class="btn btn-ghost btn-xs"
                disabled={rotatingPath === secret.path}
                onclick={() => { secretToRotate = secret; }}
                aria-label={`Rotate secret ${secret.path}`}
              >
                {rotatingPath === secret.path ? '…' : 'Rotate'}
              </button>
              <button
                class="btn btn-ghost btn-xs text-error"
                onclick={() => { secretToDelete = secret; }}
                aria-label={`Delete secret ${secret.path}`}
              >
                Delete
              </button>
            </div>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>

<!-- =========================================================================
     STORE SECRET DIALOG
     ========================================================================= -->
{#if showStore}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Store secret dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Store Secret</h3>

      {#if storeError}
        <div class="alert alert-error text-sm mb-3" role="alert">{storeError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="secret-path">Path</label>
          <input
            id="secret-path"
            class="input input-bordered input-sm font-mono"
            type="text"
            placeholder="app/database/password"
            bind:value={newPath}
          />
        </div>
        <div class="form-control">
          <label class="label label-text" for="secret-data">Data (JSON)</label>
          <textarea
            id="secret-data"
            class="textarea textarea-bordered font-mono text-xs"
            rows="6"
            bind:value={newDataJSON}
          ></textarea>
        </div>
      </div>

      <div class="modal-action">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { showStore = false; storeError = null; }}
        >
          Cancel
        </button>
        <button
          class="btn btn-primary btn-sm"
          disabled={storing}
          onclick={submitStoreSecret}
        >
          {storing ? 'Storing…' : 'Store'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showStore = false; }} onkeydown={() => { showStore = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     FETCH SECRET MODAL
     ========================================================================= -->
{#if fetchedSecret !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Secret value modal">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-1">Secret Value</h3>
      <p class="text-xs text-base-content/50 font-mono mb-4">{fetchedSecret.path} <span class="ml-2">v{fetchedSecret.version}</span></p>

      {#if fetchError}
        <div class="alert alert-error text-sm mb-3" role="alert">{fetchError}</div>
      {:else}
        <pre class="bg-base-300 rounded-box p-3 text-xs overflow-x-auto">{JSON.stringify(fetchedSecret.data, null, 2)}</pre>
      {/if}

      <div class="modal-action">
        <button class="btn btn-primary btn-sm" onclick={() => { fetchedSecret = null; fetchError = null; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { fetchedSecret = null; fetchError = null; }} onkeydown={() => { fetchedSecret = null; fetchError = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     SECRET VERSIONS MODAL
     ========================================================================= -->
{#if versionsSecret}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Secret versions modal">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-1">Secret Versions</h3>
      <p class="text-xs font-mono text-base-content/50 mb-4">{versionsSecret.path}</p>

      {#if versionsLoading}
        <div class="flex justify-center py-6"><span class="loading loading-spinner loading-md"></span></div>
      {:else if versionsError}
        <div class="alert alert-error text-sm">{versionsError}</div>
      {:else}
        <DataTable
          columns={VERSION_COLUMNS}
          rows={versions}
          rowKey={(v: SecretVersionEntry) => v.version}
          ariaLabel="Secret versions"
          emptyTitle="No versions found."
        >
          {#snippet row(v: SecretVersionEntry)}
            <tr>
              <td class="font-mono text-xs">v{v.version}</td>
              <td class="text-xs text-base-content/50">{formatDateTime(v.created_at)}</td>
              <td class="text-xs text-base-content/50">{v.created_by || '—'}</td>
              <td class="text-xs text-base-content/50">{v.expires_at ? new Date(v.expires_at).toLocaleString() : '—'}</td>
            </tr>
          {/snippet}
        </DataTable>
      {/if}

      <div class="modal-action">
        <button class="btn btn-primary btn-sm" onclick={() => { versionsSecret = null; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { versionsSecret = null; }} onkeydown={() => { versionsSecret = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     ROTATE CONFIRM DIALOG
     ========================================================================= -->
{#if secretToRotate}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Rotate secret confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Rotate Secret?</h3>
      <p class="text-sm text-base-content/70">
        Rotate <span class="font-mono font-semibold">{secretToRotate.path}</span>? A new version will be created.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { secretToRotate = null; }}>Cancel</button>
        <button
          class="btn btn-warning btn-sm"
          disabled={rotatingPath !== null}
          onclick={confirmRotateSecret}
        >
          {rotatingPath !== null ? 'Rotating…' : 'Rotate'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { secretToRotate = null; }} onkeydown={() => { secretToRotate = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE CONFIRM DIALOG
     ========================================================================= -->
{#if secretToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete secret confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Secret</h3>
      <p class="text-sm text-base-content/70">
        Permanently delete <span class="font-mono font-semibold">{secretToDelete.path}</span>? All versions will be removed.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { secretToDelete = null; }}>Cancel</button>
        <button
          class="btn btn-error btn-sm"
          disabled={deleting}
          onclick={confirmDeleteSecret}
        >
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { secretToDelete = null; }} onkeydown={() => { secretToDelete = null; }}></div>
  </div>
{/if}
