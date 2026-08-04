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
    - "Add Pin" button opens an inline modal with host + fingerprint + algorithm fields
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
  let loading = $state(false);
  let error: string | null = $state(null);

  // Add pin modal
  let showAdd = $state(false);
  let addHost = $state('');
  let addFingerprint = $state('');
  let addAlgorithm = $state('ecdsa-sha2-nistp256');
  let addError: string | null = $state(null);
  let adding = $state(false);

  // Delete confirmation
  let pinToRemove: SSHPin | null = $state(null);
  let removing = $state(false);

  const algorithms = [
    'ecdsa-sha2-nistp256',
    'ecdsa-sha2-nistp384',
    'ecdsa-sha2-nistp521',
    'ssh-ed25519',
    'ssh-rsa',
  ] as const;

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
    if (!addFingerprint.trim()) {
      addError = 'Fingerprint is required';
      return;
    }
    adding = true;
    try {
      const client = await getSDKClient();
      await client.ssh.knownHosts.add({
        host: addHost.trim(),
        fingerprint: addFingerprint.trim(),
        algorithm: addAlgorithm,
        scope,
        owner: owner || undefined,
      });
      showToast('success', `Host "${addHost.trim()}" pinned`);
      showAdd = false;
      addHost = '';
      addFingerprint = '';
      addAlgorithm = 'ecdsa-sha2-nistp256';
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

      <!-- Empty -->
      {:else if pins.length === 0}
        <p class="text-sm text-base-content/40 text-center py-4">
          No host-key pins found. Add a pin to protect against MITM attacks.
        </p>

      <!-- Pins table -->
      {:else}
        <div class="overflow-x-auto">
          <table class="table table-xs" aria-label="Known host pins">
            <thead>
              <tr>
                <th scope="col">Host</th>
                <th scope="col">Fingerprint</th>
                <th scope="col">Algorithm</th>
                <th scope="col">Source</th>
                <th scope="col">Added</th>
                <th scope="col"><span class="sr-only">Actions</span></th>
              </tr>
            </thead>
            <tbody>
              {#each pins as pin (pin.host + pin.scope)}
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
              {/each}
            </tbody>
          </table>
        </div>
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
          <label class="label label-text" for="add-pin-fingerprint">
            Fingerprint <span class="text-error ml-0.5" aria-hidden="true">*</span>
          </label>
          <input
            id="add-pin-fingerprint"
            class="input input-bordered input-sm font-mono text-xs"
            type="text"
            placeholder="SHA256:xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
            bind:value={addFingerprint}
            aria-required="true"
          />
          <div class="label">
            <span class="label-text-alt text-base-content/40">
              Obtain with: <code class="font-mono">ssh-keyscan -t ecdsa hostname | ssh-keygen -lf -</code>
            </span>
          </div>
        </div>

        <div class="form-control">
          <label class="label label-text" for="add-pin-algo">Algorithm</label>
          <select
            id="add-pin-algo"
            class="select select-bordered select-sm"
            bind:value={addAlgorithm}
          >
            {#each algorithms as algo}
              <option value={algo}>{algo}</option>
            {/each}
          </select>
        </div>
      </div>

      <div class="modal-action">
        <button
          class="btn btn-ghost btn-sm"
          onclick={() => { showAdd = false; addError = null; addHost = ''; addFingerprint = ''; }}
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
