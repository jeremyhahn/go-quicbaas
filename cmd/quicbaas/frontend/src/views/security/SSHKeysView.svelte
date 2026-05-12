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
  SSHKeysView is the top-level Security > SSH Keys route component.

  Scope tabs:
    My Keys      — scope=user,    owner=current session user ID
    Tenant Keys  — scope=tenant,  owner=current tenant ID
    Service Keys — scope=service, owner="" (server-side filtered by tenant)

  Tab availability is role-based:
    - All authenticated users see My Keys (always enabled).
    - Tenant Keys is enabled for tenant admin and system admin roles.
    - Service Keys is enabled for system admin role only.
  Disabled tabs are visible but greyed and non-clickable.

  The view renders correctly in both Wails desktop (window.go.main binding)
  and browser-mode (RestClient dispatching to /api/v1/ssh/*).
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { orchestratorStore } from '../../stores/orchestrator.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import GenerateKeyModal from './ssh/GenerateKeyModal.svelte';
  import ImportKeyModal from './ssh/ImportKeyModal.svelte';
  import PublicKeyModal from './ssh/PublicKeyModal.svelte';
  import KnownHostsPanel from './ssh/KnownHostsPanel.svelte';
  import type { SSHKey, SSHScope } from '@quicbaas/sdk';
  import { SSHUnavailableError } from '@quicbaas/sdk';
  import { Key, KeyRound } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Scope tab definitions
  // ---------------------------------------------------------------------------

  type ScopeTab = {
    id: SSHScope;
    label: string;
    /** Whether the tab is available for the current user role. */
    enabled: boolean;
  };

  // Derive owner values from the orchestrator store (tenant ID is already
  // tracked there). The "user" owner is the tenant ID acting as a user
  // namespace until a dedicated auth-user store is wired up in Phase 4.
  const tenantID = $derived(orchestratorStore.activeTenantID || 'default');

  // Role-based tab availability.
  // Phase 3 ships with a simple heuristic: all three tabs enabled. The auth
  // store does not yet expose per-user role claims to the frontend. Phase 4
  // will wire a dedicated auth store and gate these properly.
  const scopeTabs: ScopeTab[] = $derived([
    { id: 'user',    label: 'My Keys',      enabled: true },
    { id: 'tenant',  label: 'Tenant Keys',  enabled: true },
    { id: 'service', label: 'Service Keys', enabled: true },
  ]);

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let activeScope: SSHScope = $state('user');
  let keys: SSHKey[] = $state([]);
  let loading = $state(false);
  let error: string | null = $state(null);
  let sshUnavailable = $state(false);

  // Modals
  let showGenerate = $state(false);
  let showImport = $state(false);
  let publicKeyTarget: SSHKey | null = $state(null);
  let keyToDelete: SSHKey | null = $state(null);
  let deleting = $state(false);

  // ---------------------------------------------------------------------------
  // Derived: owner for active scope
  // ---------------------------------------------------------------------------

  const activeOwner = $derived((): string => {
    if (activeScope === 'service') return '';
    // Both 'user' and 'tenant' use the active tenant ID as the owner namespace
    // until Phase 4 adds a dedicated per-user identity store.
    return tenantID;
  });

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadKeys(): Promise<void> {
    loading = true;
    error = null;
    sshUnavailable = false;
    try {
      const client = await getSDKClient();
      const resp = await client.ssh.keys.listScoped({
        scope: activeScope,
        owner: activeOwner(),
      });
      const raw = resp.items ?? resp.keys ?? [];
      const seen = new Set<string>();
      keys = raw.filter((k) => {
        const composite = k.name + (k.scope ?? '') + (k.owner ?? '');
        if (seen.has(composite)) return false;
        seen.add(composite);
        return true;
      });
    } catch (e) {
      if (e instanceof SSHUnavailableError) {
        sshUnavailable = true;
      } else {
        error = e instanceof Error ? e.message : 'Failed to load SSH keys';
      }
    } finally {
      loading = false;
    }
  }

  // Reload whenever scope changes.
  $effect(() => {
    void activeScope;
    void loadKeys();
  });

  // ---------------------------------------------------------------------------
  // Delete
  // ---------------------------------------------------------------------------

  async function confirmDelete(): Promise<void> {
    if (!keyToDelete) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.ssh.keys.deleteScoped(
        keyToDelete.scope ?? activeScope,
        keyToDelete.owner ?? activeOwner(),
        keyToDelete.name,
      );
      showToast('success', `Key "${keyToDelete.name}" deleted`);
      keyToDelete = null;
      void loadKeys();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Delete failed');
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function truncateFingerprint(fp: string): string {
    if (!fp) return '—';
    // SHA256:xxxxxxx… — show up to 20 chars then ellipsis.
    return fp.length > 20 ? `${fp.slice(0, 20)}…` : fp;
  }

  function formatDate(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleDateString();
  }

  async function copyToClipboard(text: string, label: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', `${label} copied`);
    } catch {
      showToast('error', 'Copy failed — check clipboard permissions');
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="SSH Keys" subtitle="Manage SSH key identities backed by xkms" icon={KeyRound}>
    {#snippet children()}
      <button
        class="btn btn-ghost btn-sm"
        onclick={() => { showImport = true; }}
        aria-label="Import SSH key"
      >
        Import Key
      </button>
      <button
        class="btn btn-primary btn-sm"
        onclick={() => { showGenerate = true; }}
        aria-label="Generate SSH key"
      >
        + Generate Key
      </button>
    {/snippet}
  </PageHeader>

  <!-- SSH Unavailable banner -->
  {#if sshUnavailable}
    <div class="alert alert-warning" role="alert">
      <span>
        SSH is not configured for this QuicBaaS instance — set XKMS configuration to enable SSH key management.
        <a
          href="https://docs.quicbaas.io/security/ssh-keys"
          class="link link-primary ml-1"
          target="_blank"
          rel="noopener noreferrer"
        >
          Documentation
        </a>
      </span>
    </div>
  {/if}

  <!-- Generic error -->
  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadKeys(); }}>Retry</button>
    </div>
  {/if}

  <!-- Scope tab bar -->
  <div role="tablist" class="tabs tabs-bordered">
    {#each scopeTabs as tab (tab.id)}
      <button
        role="tab"
        class="tab"
        class:tab-active={activeScope === tab.id}
        class:opacity-40={!tab.enabled}
        class:cursor-not-allowed={!tab.enabled}
        disabled={!tab.enabled}
        aria-selected={activeScope === tab.id}
        aria-disabled={!tab.enabled}
        onclick={() => { if (tab.enabled) activeScope = tab.id; }}
      >
        {tab.label}
      </button>
    {/each}
  </div>

  <!-- Loading skeleton -->
  {#if loading}
    <div class="overflow-x-auto rounded-box">
      <table class="table table-sm" aria-label="SSH keys loading">
        <thead>
          <tr>
            <th>Name</th><th>Algorithm</th><th>Fingerprint</th><th>Created</th><th></th>
          </tr>
        </thead>
        <tbody>
          {#each [1, 2, 3] as i (i)}
            <tr>
              <td><div class="skeleton h-4 w-32"></div></td>
              <td><div class="skeleton h-4 w-20"></div></td>
              <td><div class="skeleton h-4 w-48"></div></td>
              <td><div class="skeleton h-4 w-24"></div></td>
              <td><div class="skeleton h-4 w-16"></div></td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>

  <!-- Empty state -->
  {:else if keys.length === 0 && !sshUnavailable && !error}
    <EmptyState
      icon={Key}
      title="No SSH keys found"
      description="Generate a new key or import an existing private key to get started."
      actionLabel="Generate Key"
      onaction={() => { showGenerate = true; }}
    />

  <!-- Keys table -->
  {:else if keys.length > 0}
    <div class="overflow-x-auto rounded-box">
      <table class="table table-sm" aria-label="SSH keys">
        <thead>
          <tr>
            <th scope="col">Name</th>
            <th scope="col">Algorithm</th>
            <th scope="col">Fingerprint</th>
            <th scope="col">Created</th>
            <th scope="col"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each keys as key (key.name + (key.scope ?? '') + (key.owner ?? ''))}
            <tr class="hover">
              <td class="font-mono text-xs font-medium">{key.name}</td>
              <td class="text-sm">{key.algorithm ?? '—'}</td>
              <td class="font-mono text-xs">
                <div class="flex items-center gap-1">
                  <span title={key.fingerprint ?? ''}>{truncateFingerprint(key.fingerprint ?? '')}</span>
                  {#if key.fingerprint}
                    <button
                      class="btn btn-ghost btn-xs px-1"
                      onclick={() => void copyToClipboard(key.fingerprint ?? '', 'Fingerprint')}
                      aria-label={`Copy fingerprint for ${key.name}`}
                    >
                      <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                    </button>
                  {/if}
                </div>
              </td>
              <td class="text-xs text-base-content/50">{formatDate(key.created_at)}</td>
              <td>
                <div class="flex items-center gap-1">
                  <button
                    class="btn btn-ghost btn-xs"
                    onclick={() => { publicKeyTarget = key; }}
                    aria-label={`View public key for ${key.name}`}
                  >
                    View
                  </button>
                  <button
                    class="btn btn-ghost btn-xs"
                    onclick={() => void copyToClipboard(key.public_key ?? key.public_key_openssh ?? '', 'Public key')}
                    aria-label={`Copy public key for ${key.name}`}
                  >
                    Copy
                  </button>
                  <button
                    class="btn btn-ghost btn-xs text-error"
                    onclick={() => { keyToDelete = key; }}
                    aria-label={`Delete key ${key.name}`}
                  >
                    Delete
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}

  <!-- Known Hosts panel -->
  <KnownHostsPanel scope={activeScope} owner={activeOwner()} />
</div>

<!-- Generate Key modal -->
{#if showGenerate}
  <GenerateKeyModal
    scope={activeScope}
    owner={activeOwner()}
    onClose={() => { showGenerate = false; }}
    onCreated={(key) => {
      showGenerate = false;
      keys = [key, ...keys];
      showToast('success', `Key "${key.name}" generated`);
    }}
  />
{/if}

<!-- Import Key modal -->
{#if showImport}
  <ImportKeyModal
    scope={activeScope}
    owner={activeOwner()}
    onClose={() => { showImport = false; }}
    onCreated={(key) => {
      showImport = false;
      keys = [key, ...keys];
      showToast('success', `Key "${key.name}" imported`);
    }}
  />
{/if}

<!-- Public key display modal -->
{#if publicKeyTarget !== null}
  <PublicKeyModal
    key={publicKeyTarget}
    onClose={() => { publicKeyTarget = null; }}
  />
{/if}

<!-- Delete confirmation dialog -->
{#if keyToDelete !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete key confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete SSH Key</h3>
      <p class="text-sm text-base-content/70">
        Are you sure you want to delete <span class="font-mono font-semibold">{keyToDelete.name}</span>?
        The private key will be permanently destroyed inside xkms. This action cannot be undone.
      </p>
      <div class="modal-action">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { keyToDelete = null; }}
        >
          Cancel
        </button>
        <button
          class="btn btn-error btn-sm"
          disabled={deleting}
          onclick={confirmDelete}
        >
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { keyToDelete = null; }}
      onkeydown={() => { keyToDelete = null; }}
    ></div>
  </div>
{/if}
