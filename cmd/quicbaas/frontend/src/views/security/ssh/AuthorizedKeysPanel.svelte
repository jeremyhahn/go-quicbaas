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
  AuthorizedKeysPanel renders the stored authorized_keys entries for the
  active SSH scope/owner combination.

  Features:
    - List entries: Name · Line (truncated) · Options · Comment · Actions
      (GET /api/v1/ssh/authorized/keys)
    - "Add entry" opens a modal with name, line, options and comment
      (POST /api/v1/ssh/authorized/keys)
    - Row "View" shows one entry (GET .../authorized/keys/{scope}/{owner}/{name})
    - Row "Remove" with a confirmation dialog (DELETE .../authorized/keys/...)
    - "Format a line" turns a public key into an authorized_keys line
      (POST /api/v1/ssh/authorized)
    - "Check a fingerprint" finds the entry a fingerprint matches
      (POST /api/v1/ssh/authorized/verify)

  A connection without an SSH client, or an SSHUnavailableError from the
  list call, shows a not-available state and makes no further calls.

  Props:
    scope — SSH scope to list entries for (user | tenant | service).
    owner — Owner namespace string (empty means server default).
-->
<script lang="ts">
  import { getSDKClient } from '../../../stores/sdkClient.ts';
  import { showToast } from '../../../stores/toast.svelte.ts';
  import type { SSHAuthorizedEntry, SSHScope } from '@quicbaas/sdk';
  import { SSHNotFoundError, SSHUnavailableError } from '@quicbaas/sdk';
  import DataTable from '../../../components/core/DataTable.svelte';
  import type { Column } from '../../../lib/dataTable.ts';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let {
    scope,
    owner,
  }: {
    /** SSH scope to list authorized entries for. */
    scope: SSHScope;
    /** Owner namespace string. */
    owner: string;
  } = $props();

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  const ENTRY_COLUMNS: Column<SSHAuthorizedEntry>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'line', label: 'Line', searchable: true },
    { key: 'options', label: 'Options' },
    { key: 'comment', label: 'Comment', searchable: true },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  let entries: SSHAuthorizedEntry[] = $state([]);
  let loading = $state(false);
  let error: string | null = $state(null);
  let unavailable = $state(false);
  // Sequence number of the newest list call; older answers are dropped.
  let listSeq = 0;

  // Add entry modal
  let showAdd = $state(false);
  let addName = $state('');
  let addLine = $state('');
  let addOptions = $state('');
  let addComment = $state('');
  let addError: string | null = $state(null);
  let adding = $state(false);

  // Entry detail modal
  let detail: SSHAuthorizedEntry | null = $state(null);
  let detailName: string | null = $state(null);
  let detailError: string | null = $state(null);
  let viewing = $state(false);

  // Remove confirmation
  let entryToRemove: SSHAuthorizedEntry | null = $state(null);
  let removing = $state(false);

  // Format a line
  let emitKey = $state('');
  let emitOptions = $state('');
  let emitComment = $state('');
  let emitLine: string | null = $state(null);
  let emitError: string | null = $state(null);
  let emitting = $state(false);

  // Check a fingerprint
  let verifyFingerprint = $state('');
  let verifyEntry: SSHAuthorizedEntry | null = $state(null);
  let verifyNoMatch = $state(false);
  let verifyError: string | null = $state(null);
  let verifying = $state(false);

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  /** The scope/owner filter; an empty owner is not sent. */
  function filter(): { scope: SSHScope; owner?: string } {
    return owner ? { scope, owner } : { scope };
  }

  /** Splits a comma-separated options field into trimmed, non-empty parts. */
  function splitOptions(raw: string): string[] {
    return raw.split(',').map((o) => o.trim()).filter((o) => o.length > 0);
  }

  function message(e: unknown, fallback: string): string {
    return e instanceof Error ? e.message : fallback;
  }

  function truncate(s: string, max = 40): string {
    return s.length > max ? `${s.slice(0, max)}…` : s;
  }

  async function copyText(text: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      showToast('success', 'Copied');
    } catch {
      showToast('error', 'Copy failed');
    }
  }

  // ---------------------------------------------------------------------------
  // Data loading
  // ---------------------------------------------------------------------------

  async function loadEntries(): Promise<void> {
    if (unavailable) return;
    const seq = ++listSeq;
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      if (!client.ssh) {
        if (seq === listSeq) unavailable = true;
        return;
      }
      const resp = await client.ssh.authorized.list(filter());
      if (seq !== listSeq) return;
      entries = resp ?? [];
    } catch (e) {
      if (seq !== listSeq) return;
      if (e instanceof SSHUnavailableError) {
        unavailable = true;
      } else {
        error = message(e, 'Failed to load authorized keys');
      }
    } finally {
      if (seq === listSeq) loading = false;
    }
  }

  // Reload whenever scope or owner changes.
  $effect(() => {
    void scope;
    void owner;
    void loadEntries();
  });

  // ---------------------------------------------------------------------------
  // Add entry
  // ---------------------------------------------------------------------------

  function closeAdd(): void {
    showAdd = false;
    addError = null;
    addName = '';
    addLine = '';
    addOptions = '';
    addComment = '';
  }

  async function submitAdd(): Promise<void> {
    addError = null;
    const name = addName.trim();
    const line = addLine.trim();
    if (!name) {
      addError = 'Name is required';
      return;
    }
    if (!line) {
      addError = 'Line is required';
      return;
    }
    const options = splitOptions(addOptions);
    const comment = addComment.trim();
    adding = true;
    try {
      const client = await getSDKClient();
      await client.ssh.authorized.add({
        scope,
        owner,
        name,
        line,
        ...(options.length > 0 ? { options } : {}),
        ...(comment ? { comment } : {}),
      });
      showToast('success', `Authorized entry "${name}" added`);
      closeAdd();
      void loadEntries();
    } catch (e) {
      addError = message(e, 'Failed to add authorized entry');
    } finally {
      adding = false;
    }
  }

  // ---------------------------------------------------------------------------
  // View entry
  // ---------------------------------------------------------------------------

  async function viewEntry(entry: SSHAuthorizedEntry): Promise<void> {
    detailName = entry.name;
    detail = null;
    detailError = null;
    viewing = true;
    try {
      const client = await getSDKClient();
      detail = await client.ssh.authorized.get(entry.scope ?? scope, entry.owner ?? owner, entry.name);
    } catch (e) {
      detailError = message(e, 'Failed to load authorized entry');
    } finally {
      viewing = false;
    }
  }

  function closeDetail(): void {
    detailName = null;
    detail = null;
    detailError = null;
  }

  // ---------------------------------------------------------------------------
  // Remove entry
  // ---------------------------------------------------------------------------

  async function confirmRemove(): Promise<void> {
    if (!entryToRemove) return;
    const target = entryToRemove;
    removing = true;
    try {
      const client = await getSDKClient();
      await client.ssh.authorized.remove(target.scope ?? scope, target.owner ?? owner, target.name);
      showToast('success', `Authorized entry "${target.name}" removed`);
      entryToRemove = null;
      void loadEntries();
    } catch (e) {
      showToast('error', message(e, 'Remove failed'));
    } finally {
      removing = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Format a line
  // ---------------------------------------------------------------------------

  async function submitEmit(): Promise<void> {
    emitError = null;
    emitLine = null;
    const publicKey = emitKey.trim();
    if (!publicKey) {
      emitError = 'Public key is required';
      return;
    }
    const options = splitOptions(emitOptions);
    const comment = emitComment.trim();
    emitting = true;
    try {
      const client = await getSDKClient();
      const resp = await client.ssh.authorized.emit({
        public_key: publicKey,
        ...(options.length > 0 ? { options } : {}),
        ...(comment ? { comment } : {}),
      });
      emitLine = resp.line;
    } catch (e) {
      emitError = message(e, 'Failed to format the line');
    } finally {
      emitting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Check a fingerprint
  // ---------------------------------------------------------------------------

  async function submitVerify(): Promise<void> {
    verifyError = null;
    verifyEntry = null;
    verifyNoMatch = false;
    const fingerprint = verifyFingerprint.trim();
    if (!fingerprint) {
      verifyError = 'Fingerprint is required';
      return;
    }
    verifying = true;
    try {
      const client = await getSDKClient();
      const resp = await client.ssh.authorized.verify({ ...filter(), fingerprint });
      verifyEntry = resp.entry;
    } catch (e) {
      if (e instanceof SSHNotFoundError) {
        verifyNoMatch = true;
      } else {
        verifyError = message(e, 'Failed to check the fingerprint');
      }
    } finally {
      verifying = false;
    }
  }
</script>

<section class="border border-base-300 rounded-box bg-base-100 p-4 space-y-4" aria-label="Authorized keys">
  <div class="flex items-center justify-between">
    <h2 class="text-sm font-medium flex items-center gap-2">
      Authorized Keys
      {#if entries.length > 0}
        <span class="badge badge-sm badge-outline">{entries.length}</span>
      {/if}
    </h2>
    {#if !unavailable}
      <button
        class="btn btn-sm btn-ghost"
        onclick={() => { showAdd = true; }}
        aria-label="Add authorized entry"
      >
        + Add entry
      </button>
    {/if}
  </div>

  {#if unavailable}
    <div class="alert alert-warning text-sm" role="status">
      Authorized keys are not available on this connection.
    </div>
  {:else}
    {#if error}
      <div class="alert alert-error text-sm" role="alert">{error}</div>
    {/if}

    {#if loading}
      <div class="flex justify-center py-4">
        <span class="loading loading-spinner loading-sm"></span>
      </div>
    {:else}
      <DataTable
        columns={ENTRY_COLUMNS}
        rows={entries}
        rowKey={(entry: SSHAuthorizedEntry) => entry.name + entry.scope + entry.owner}
        ariaLabel="Authorized key entries"
        searchPlaceholder="Search entries..."
        searchLabel="Search authorized entries"
        noMatchText="No entries match the search."
      >
        {#snippet empty()}
          <p class="text-sm text-base-content/40 text-center py-4">
            No authorized_keys entries found.
          </p>
        {/snippet}

        {#snippet row(entry: SSHAuthorizedEntry)}
          <tr class="hover">
            <td class="font-mono text-xs font-medium">{entry.name}</td>
            <td class="font-mono text-xs" title={entry.line}>{truncate(entry.line)}</td>
            <td class="text-xs">{entry.options && entry.options.length > 0 ? entry.options.join(', ') : '—'}</td>
            <td class="text-xs text-base-content/50">{entry.comment || '—'}</td>
            <td>
              <div class="flex items-center gap-1">
                <button
                  class="btn btn-ghost btn-xs"
                  disabled={viewing}
                  onclick={() => void viewEntry(entry)}
                  aria-label={`View authorized entry ${entry.name}`}
                >
                  View
                </button>
                <button
                  class="btn btn-ghost btn-xs text-error"
                  onclick={() => { entryToRemove = entry; }}
                  aria-label={`Remove authorized entry ${entry.name}`}
                >
                  Remove
                </button>
              </div>
            </td>
          </tr>
        {/snippet}
      </DataTable>
    {/if}

    <div class="grid gap-4 md:grid-cols-2">
      <!-- Format a line -->
      <div class="space-y-2">
        <h3 class="text-sm font-medium">Format a line</h3>
        {#if emitError}
          <div class="alert alert-error text-sm" role="alert">{emitError}</div>
        {/if}
        <div class="form-control">
          <label class="label label-text" for="auth-emit-key">
            Public key <span class="text-error ml-0.5" aria-hidden="true">*</span>
          </label>
          <textarea
            id="auth-emit-key"
            class="textarea textarea-bordered textarea-sm font-mono text-xs"
            rows="2"
            placeholder="ssh-ed25519 AAAAC3Nza…"
            bind:value={emitKey}
            aria-required="true"
          ></textarea>
        </div>
        <div class="form-control">
          <label class="label label-text" for="auth-emit-options">Line options</label>
          <input
            id="auth-emit-options"
            class="input input-bordered input-sm"
            type="text"
            placeholder="restrict, no-pty"
            bind:value={emitOptions}
          />
        </div>
        <div class="form-control">
          <label class="label label-text" for="auth-emit-comment">Line comment</label>
          <input
            id="auth-emit-comment"
            class="input input-bordered input-sm"
            type="text"
            bind:value={emitComment}
          />
        </div>
        <button
          class="btn btn-sm btn-primary"
          disabled={emitting}
          onclick={submitEmit}
          aria-label="Format line"
        >
          {emitting ? 'Formatting…' : 'Format line'}
        </button>
        {#if emitLine !== null}
          <div class="flex items-start gap-2">
            <pre
              class="bg-base-200 rounded-lg p-2 text-xs font-mono whitespace-pre-wrap break-all flex-1"
              aria-label="Formatted line"
            >{emitLine}</pre>
            <button
              class="btn btn-ghost btn-xs"
              onclick={() => void copyText(emitLine ?? '')}
              aria-label="Copy formatted line"
            >
              Copy
            </button>
          </div>
        {/if}
      </div>

      <!-- Check a fingerprint -->
      <div class="space-y-2">
        <h3 class="text-sm font-medium">Check a fingerprint</h3>
        {#if verifyError}
          <div class="alert alert-error text-sm" role="alert">{verifyError}</div>
        {/if}
        <div class="form-control">
          <label class="label label-text" for="auth-verify-fingerprint">
            Fingerprint <span class="text-error ml-0.5" aria-hidden="true">*</span>
          </label>
          <input
            id="auth-verify-fingerprint"
            class="input input-bordered input-sm font-mono"
            type="text"
            placeholder="SHA256:…"
            bind:value={verifyFingerprint}
            aria-required="true"
          />
        </div>
        <button
          class="btn btn-sm btn-primary"
          disabled={verifying}
          onclick={submitVerify}
          aria-label="Check fingerprint"
        >
          {verifying ? 'Checking…' : 'Check fingerprint'}
        </button>
        {#if verifyEntry !== null}
          <div class="alert alert-success text-sm flex-col items-start" role="status" aria-label="Fingerprint check result">
            <span>Matches {verifyEntry.name}</span>
            <code class="font-mono text-xs break-all">{verifyEntry.line}</code>
          </div>
        {:else if verifyNoMatch}
          <div class="alert alert-warning text-sm" role="status" aria-label="Fingerprint check result">
            No authorized entry matches
          </div>
        {/if}
      </div>
    </div>
  {/if}
</section>

<!-- Add entry modal -->
{#if showAdd}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Add authorized entry dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Add Authorized Entry</h3>

      {#if addError}
        <div class="alert alert-error text-sm mb-3" role="alert">{addError}</div>
      {/if}

      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="auth-add-name">
            Entry name <span class="text-error ml-0.5" aria-hidden="true">*</span>
          </label>
          <input
            id="auth-add-name"
            class="input input-bordered input-sm"
            type="text"
            bind:value={addName}
            aria-required="true"
          />
        </div>
        <div class="form-control">
          <label class="label label-text" for="auth-add-line">
            Entry line <span class="text-error ml-0.5" aria-hidden="true">*</span>
          </label>
          <textarea
            id="auth-add-line"
            class="textarea textarea-bordered textarea-sm font-mono text-xs"
            rows="3"
            placeholder="ssh-ed25519 AAAAC3Nza… user@host"
            bind:value={addLine}
            aria-required="true"
          ></textarea>
        </div>
        <div class="form-control">
          <label class="label label-text" for="auth-add-options">Entry options</label>
          <input
            id="auth-add-options"
            class="input input-bordered input-sm"
            type="text"
            placeholder="restrict, no-pty"
            bind:value={addOptions}
          />
        </div>
        <div class="form-control">
          <label class="label label-text" for="auth-add-comment">Entry comment</label>
          <input
            id="auth-add-comment"
            class="input input-bordered input-sm"
            type="text"
            bind:value={addComment}
          />
        </div>
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeAdd} aria-label="Cancel adding authorized entry">
          Cancel
        </button>
        <button
          class="btn btn-primary btn-sm"
          disabled={adding}
          onclick={submitAdd}
          aria-label="Save authorized entry"
        >
          {adding ? 'Adding…' : 'Add entry'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeAdd} onkeydown={closeAdd}></div>
  </div>
{/if}

<!-- Entry detail modal -->
{#if detailName !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Authorized entry detail">
    <div class="modal-box max-w-2xl">
      <h3 class="text-base font-semibold mb-1">Authorized Entry</h3>
      <p class="text-sm text-base-content/50 mb-4 font-mono">{detailName}</p>

      {#if detailError}
        <div class="alert alert-error text-sm mb-3" role="alert">{detailError}</div>
      {:else if viewing || detail === null}
        <div class="flex justify-center py-4">
          <span class="loading loading-spinner loading-sm"></span>
        </div>
      {:else}
        <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm mb-3">
          <dt class="text-base-content/50">Scope</dt><dd>{detail.scope}</dd>
          <dt class="text-base-content/50">Owner</dt><dd class="font-mono">{detail.owner || '—'}</dd>
          <dt class="text-base-content/50">Options</dt>
          <dd>{detail.options && detail.options.length > 0 ? detail.options.join(', ') : '—'}</dd>
          <dt class="text-base-content/50">Comment</dt><dd>{detail.comment || '—'}</dd>
        </dl>
        <pre
          class="bg-base-200 rounded-lg p-4 text-xs font-mono whitespace-pre-wrap break-all"
          aria-label="Authorized entry line"
        >{detail.line}</pre>
      {/if}

      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={closeDetail} aria-label="Close authorized entry dialog">
          Close
        </button>
        {#if detail !== null}
          <button
            class="btn btn-primary btn-sm"
            onclick={() => void copyText(detail?.line ?? '')}
            aria-label="Copy authorized entry line"
          >
            Copy
          </button>
        {/if}
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={closeDetail} onkeydown={closeDetail}></div>
  </div>
{/if}

<!-- Remove confirmation dialog -->
{#if entryToRemove !== null}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Remove authorized entry confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Remove Authorized Entry</h3>
      <p class="text-sm text-base-content/70">
        Are you sure you want to remove
        <span class="font-mono font-semibold">{entryToRemove.name}</span>?
        The key will no longer be accepted for this scope and owner.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { entryToRemove = null; }}>
          Cancel
        </button>
        <button
          class="btn btn-error btn-sm"
          disabled={removing}
          onclick={confirmRemove}
          aria-label="Confirm remove authorized entry"
        >
          {removing ? 'Removing…' : 'Remove'}
        </button>
      </div>
    </div>
    <div
      class="modal-backdrop"
      role="none"
      onclick={() => { entryToRemove = null; }}
      onkeydown={() => { entryToRemove = null; }}
    ></div>
  </div>
{/if}
