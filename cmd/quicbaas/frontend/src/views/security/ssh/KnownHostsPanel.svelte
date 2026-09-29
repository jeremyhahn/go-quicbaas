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
  KnownHostsPanel renders a collapsible list of host-key pins (known_hosts)
  for the active SSH scope/owner combination.

  Features:
    - List pinned hosts: Host · Fingerprint (truncated + copy) · Algorithm · Source · Added · Actions
    - "Add Pin" button opens an inline modal with host + host-key fields; the
      server derives the fingerprint and algorithm from the key
    - Remove pin with a confirmation dialog

  Props:
    scope — SSH scope to filter pins by (user | tenant | service).
    owner — Owner namespace string (empty means server default).
-->
<script lang="ts">
  import { getSDKClient } from '../../../stores/sdkClient.ts';
  import { showToast } from '../../../stores/toast.svelte.ts';
  import type { SSHPin, SSHScope } from '@quicbaas/sdk';
  import { SSHUnavailableError } from '@quicbaas/sdk';
  import DataTable from '../../../components/core/DataTable.svelte';
  import type { Column } from '../../../lib/dataTable.ts';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let {
    scope,
    owner,
  }: {
    /** SSH scope to list known hosts for. */
    scope: SSHScope;
    /** Owner namespace string. */
    owner: string;
  } = $props();

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let open = $state(false);
  let pins: SSHPin[] = $state([]);

  const PIN_COLUMNS: Column<SSHPin>[] = [
    { key: 'host', label: 'Host', sortable: true, searchable: true },
    { key: 'fingerprint', label: 'Fingerprint', searchable: true },
    { key: 'algorithm', label: 'Algorithm', sortable: true },
    { key: 'source', label: 'Source', sortable: true },
    { key: 'added_at', label: 'Added', sortable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];
  let loading = $state(false);
  let error: string | null = $state(null);

  // Add pin modal
  let showAdd = $state(false);
  let addHost = $state('');
  let addPublicKey = $state('');
  let addError: string | null = $state(null);
  let adding = $state(false);

  // Delete confirmation
  let pinToRemove: SSHPin | null = $state(null);
  let removing = $state(false);

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadPins(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      const resp = await client.ssh.knownHosts.list({ scope, owner: owner || undefined });
      const raw = resp.items ?? [];
      const seen = new Set<string>();
      pins = raw.filter((p) => {
        const composite = p.host + p.scope;
        if (seen.has(composite)) return false;
        seen.add(composite);
        return true;
      });
    } catch (e) {
      if (!(e instanceof SSHUnavailableError)) {
        error = e instanceof Error ? e.message : 'Failed to load known hosts';
      }
    } finally {
      loading = false;
    }
  }

  // Reload when the panel is opened or scope/owner changes.
  $effect(() => {
    if (open) {
      void scope;
      void owner;
      void loadPins();
    }
  });

  // ---------------------------------------------------------------------------
  // Add pin
  // ---------------------------------------------------------------------------

  async function submitAdd(): Promise<void> {
    addError = null;
    if (!addHost.trim()) {
      addError = 'Host is required';
      return;
    }
    if (!addPublicKey.trim()) {
      addError = 'Host key is required';
      return;
    }
    adding = true;
    try {
      const client = await getSDKClient();
      await client.ssh.knownHosts.add({
        host: addHost.trim(),
        public_key: addPublicKey.trim(),
        scope,
        owner: owner || undefined,
      });
      showToast('success', `Host "${addHost.trim()}" pinned`);
      showAdd = false;
      addHost = '';
      addPublicKey = '';
      void loadPins();
    } catch (e) {
      addError = e instanceof Error ? e.message : 'Failed to add host pin';
    } finally {
      adding = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Remove pin
  // ---------------------------------------------------------------------------

  async function confirmRemove(): Promise<void> {
    if (!pinToRemove) return;
    removing = true;
    try {
      const client = await getSDKClient();
      await client.ssh.knownHosts.remove(pinToRemove.host, { scope, owner: owner || undefined });
      showToast('success', `Host "${pinToRemove.host}" removed`);
      pinToRemove = null;
      void loadPins();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Remove failed');
    } finally {
      removing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function truncate(s: string, max = 20): string {
    return s.length > max ? `${s.slice(0, max)}…` : s;
  }

  function formatDate(iso: string | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleDateString();
  }

  async function copyText(text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', 'Copied');
    } catch {
      showToast('error', 'Copy failed');
    }
  }
</script>

<!-- Collapsible section -->
<!-- collapse-open must track `open`, otherwise daisyUI keeps .collapse-content
     visually hidden (max-height:0) even when {#if open} adds it to the DOM. -->
<div class="collapse collapse-arrow border border-base-300 rounded-box bg-base-100" class:collapse-open={open}>
  <button
    class="collapse-title text-sm font-medium flex items-center gap-2"
    aria-expanded={open}
    onclick={() => { open = !open; }}
  >
    Known Hosts
    {#if pins.length > 0}
      <span class="badge badge-sm badge-outline">{pins.length}</span>
    {/if}
  </button>

  {#if open}
    <div class="collapse-content pt-0">

      <!-- Panel actions -->
      <div class="flex justify-end mb-3">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { showAdd = true; }}
          aria-label="Add host pin"
        >
          + Add Pin
        </button>
      </div>

      <!-- Error -->
      {#if error}
        <div class="alert alert-error text-sm mb-3" role="alert">{error}</div>
      {/if}

      <!-- Loading -->
      {#if loading}
        <div class="flex justify-center py-4">
          <span class="loading loading-spinner loading-sm"></span>
        </div>

      <!-- Pins table -->
      {:else}
        <DataTable
          columns={PIN_COLUMNS}
          rows={pins}
          rowKey={(pin: SSHPin) => pin.host + pin.scope}
          ariaLabel="Known host pins"
          searchPlaceholder="Search pins..."
          searchLabel="Search host pins"
          noMatchText="No pins match the search."
        >
          {#snippet empty()}
            <p class="text-sm text-base-content/40 text-center py-4">
              No host-key pins found. Add a pin to protect against MITM attacks.
            </p>
          {/snippet}

          {#snippet row(pin: SSHPin)}
            <tr class="hover">
              <td class="font-mono text-xs">{pin.host}</td>
              <td class="font-mono text-xs">
                <div class="flex items-center gap-1">
                  <span title={pin.fingerprint}>{truncate(pin.fingerprint, 22)}</span>
                  <button
                    class="btn btn-ghost btn-xs px-1"
                    onclick={() => void copyText(pin.fingerprint)}
                    aria-label={`Copy fingerprint for ${pin.host}`}
                  >
                    <svg xmlns="http://www.w3.org/2000/svg" class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                  </button>
                </div>
              </td>
              <td class="text-xs">{pin.algorithm ?? '—'}</td>
              <td class="text-xs text-base-content/50">{pin.source ?? '—'}</td>
              <td class="text-xs text-base-content/50">{formatDate(pin.added_at)}</td>
              <td>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  onclick={() => { pinToRemove = pin; }}
                  aria-label={`Remove host pin for ${pin.host}`}
                >
                  Remove
                </button>
              </td>
            </tr>
          {/snippet}
        </DataTable>
      {/if}

    </div>
  {/if}
</div>

<!-- Add pin modal -->
{#if showAdd}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Add host pin dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Add Host Pin</h3>

      {#if addError}
        <div class="alert alert-error text-sm mb-3" role="alert">{addError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="add-pin-host">
            Host <span class="text-error ml-0.5" aria-hidden="true">*</span>
          </label>
          <input
            id="add-pin-host"
            class="input input-bordered input-sm"
            type="text"
            placeholder="github.com"
            bind:value={addHost}
            aria-required="true"
          />
        </div>

        <div class="form-control">
          <label class="label label-text" for="add-pin-key">
            Host key <span class="text-error ml-0.5" aria-hidden="true">*</span>
          </label>
          <textarea
            id="add-pin-key"
            class="textarea textarea-bordered textarea-sm font-mono text-xs"
            rows="3"
            placeholder="ssh-ed25519 AAAAC3Nza…"
            bind:value={addPublicKey}
            aria-required="true"
          ></textarea>
          <div class="label">
            <span class="label-text-alt text-base-content/40">
              Obtain with: <code class="font-mono">ssh-keyscan -t ed25519 hostname | cut -d' ' -f2-</code>
            </span>
          </div>
        </div>
      </div>

      <div class="modal-action">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { showAdd = false; addError = null; addHost = ''; addPublicKey = ''; }}
          aria-label="Cancel adding host pin"
        >
          Cancel
        </button>
        <button
          class="btn btn-primary btn-sm"
          disabled={adding}
          onclick={submitAdd}
          aria-label="Add host pin"
        >
          {adding ? 'Adding…' : 'Add Pin'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { showAdd = false; addError = null; }}
      onkeydown={() => { showAdd = false; addError = null; }}
    ></div>
  </div>
{/if}

<!-- Remove confirmation dialog -->
{#if pinToRemove !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Remove host pin confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Remove Host Pin</h3>
      <p class="text-sm text-base-content/70">
        Are you sure you want to remove the pin for
        <span class="font-mono font-semibold">{pinToRemove.host}</span>?
        Future SSH connections to this host will not be verified against a stored fingerprint.
      </p>
      <div class="modal-action">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { pinToRemove = null; }}
        >
          Cancel
        </button>
        <button
          class="btn btn-error btn-sm"
          disabled={removing}
          onclick={confirmRemove}
        >
          {removing ? 'Removing…' : 'Remove'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { pinToRemove = null; }}
      onkeydown={() => { pinToRemove = null; }}
    ></div>
  </div>
{/if}
