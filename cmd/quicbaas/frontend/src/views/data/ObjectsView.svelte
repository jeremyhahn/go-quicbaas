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
  ObjectsView provides an object-storage browser: list objects by prefix,
  upload a file (PUT), download by key, and delete objects.
  Mirrors the `quicbaas objects put/get/list/delete` CLI surface.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { ObjectEntry } from '@quicbaas/sdk';
  import { APIError } from '@quicbaas/sdk';
  import { Upload, Trash2, Download, Copy, Search, RefreshCw, FolderOpen, Package } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { formatDateTime } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Object list state
  // ---------------------------------------------------------------------------

  let prefix = $state('');
  let objects = $state<ObjectEntry[]>([]);
  let loading = $state(false);
  let fetchError: string | null = $state(null);
  let subsystemDisabled = $state(false);

  function isDisabledError(err: unknown): boolean {
    if (err instanceof APIError && (err.status === 408 || err.status === 503 || err.status === 500 || err.status === 501)) {
      const msg = err.message.toLowerCase();
      return msg.includes('not available') || msg.includes('not configured') || msg.includes('not compiled');
    }
    return false;
  }

  async function fetchObjects(): Promise<void> {
    loading = true;
    fetchError = null;
    subsystemDisabled = false;
    try {
      const client = await getSDKClient();
      const resp = await client.objects.listObjects(prefix || undefined);
      const seen = new Set<string>();
      objects = resp.objects.filter((o) => {
        if (seen.has(o.key)) return false;
        seen.add(o.key);
        return true;
      });
    } catch (err: unknown) {
      if (isDisabledError(err)) {
        subsystemDisabled = true;
        objects = [];
      } else {
        const msg = err instanceof Error ? err.message : String(err);
        fetchError = `Failed to load objects: ${msg}`;
        objects = [];
      }
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchObjects(); });

  // ---------------------------------------------------------------------------
  // Upload
  // ---------------------------------------------------------------------------

  let fileInput: HTMLInputElement | null = $state(null);
  let uploading = $state(false);
  let uploadKey = $state('');
  let showUploadDialog = $state(false);
  let selectedFile = $state<File | null>(null);
  let uploadError = $state('');

  function openUploadDialog(): void {
    uploadKey = '';
    selectedFile = null;
    uploadError = '';
    showUploadDialog = true;
  }

  function handleFileSelect(e: Event): void {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0] ?? null;
    selectedFile = file;
    if (file && !uploadKey.trim()) {
      uploadKey = file.name;
    }
  }

  async function handleUpload(): Promise<void> {
    if (!selectedFile) {
      uploadError = 'Select a file first.';
      return;
    }
    if (!uploadKey.trim()) {
      uploadError = 'Object key is required.';
      return;
    }
    uploading = true;
    uploadError = '';
    try {
      const client = await getSDKClient();
      const buf = await selectedFile.arrayBuffer();
      await client.objects.putObject(
        uploadKey.trim(),
        buf,
        selectedFile.type || 'application/octet-stream',
      );
      showToast('success', `Object "${uploadKey}" uploaded.`);
      showUploadDialog = false;
      await fetchObjects();
    } catch (err: unknown) {
      uploadError = err instanceof Error ? err.message : String(err);
    } finally {
      uploading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Download
  // ---------------------------------------------------------------------------

  async function handleDownload(key: string): Promise<void> {
    try {
      const client = await getSDKClient();
      const blob = await client.objects.getObject(key);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = key.split('/').pop() ?? key;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    }
  }

  // ---------------------------------------------------------------------------
  // Delete (confirm modal)
  // ---------------------------------------------------------------------------

  let objectToDelete = $state<ObjectEntry | null>(null);
  let deletingObject = $state(false);

  async function confirmDeleteObject(): Promise<void> {
    if (!objectToDelete) return;
    deletingObject = true;
    try {
      const client = await getSDKClient();
      await client.objects.deleteObject(objectToDelete.key);
      showToast('success', `Object "${objectToDelete.key}" deleted.`);
      objectToDelete = null;
      await fetchObjects();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      deletingObject = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Copy URL
  // ---------------------------------------------------------------------------

  async function copyObjectURL(key: string): Promise<void> {
    const client = await getSDKClient();
    const address = client.connection.getAddress();
    const url = `${address}/api/v1/objects/${encodeURIComponent(key)}`;
    try {
      await navigator.clipboard.writeText(url);
      showToast('success', 'URL copied to clipboard.');
    } catch {
      showToast('error', 'Copy failed — check clipboard permissions.');
    }
  }

  // ---------------------------------------------------------------------------
  // Size formatting
  // ---------------------------------------------------------------------------

  function formatBytes(bytes?: number): string {
    if (bytes === undefined || bytes === null) return '—';
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  }
</script>

<div class="p-6">
  <PageHeader title="Objects" subtitle="Browse and manage object storage" icon={Package}>
    {#snippet children()}
      <button class="btn btn-ghost btn-sm" onclick={() => { void fetchObjects(); }}>
        <RefreshCw class="h-4 w-4" />
        Refresh
      </button>
      <button
        class="btn btn-primary btn-sm"
        onclick={openUploadDialog}
        disabled={subsystemDisabled}
        title={subsystemDisabled ? 'Object storage subsystem is not enabled on this node' : undefined}
      >
        <Upload class="h-4 w-4" />
        Upload
      </button>
    {/snippet}
  </PageHeader>

  {#if subsystemDisabled}
    <div class="alert alert-info text-sm mb-4" role="alert">
      <span>
        Object storage is not enabled on this node. Start the daemon with
        <code class="font-mono">QUICBAAS_OBJECTSTORAGE_ENABLED=true</code>
        or set <code class="font-mono">object_storage.enabled: true</code>
        in the config file.
      </span>
    </div>
  {/if}

  <!-- Prefix filter -->
  <label class="flex items-center gap-2 mb-4">
    <Search class="h-4 w-4 text-base-content/40 shrink-0" />
    <input
      type="text"
      class="input input-bordered input-sm flex-1"
      bind:value={prefix}
      placeholder="Filter by key prefix (e.g. uploads/)…"
      onchange={() => { void fetchObjects(); }}
    />
  </label>

  {#if loading}
    <div class="flex justify-center py-12">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else if subsystemDisabled}
    <div class="text-center text-base-content/30 py-8 text-sm">Subsystem disabled.</div>
  {:else if fetchError}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{fetchError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void fetchObjects(); }}>Retry</button>
    </div>
  {:else if objects.length === 0}
    {#if prefix}
      <EmptyState
        icon={FolderOpen}
        title={`No objects with prefix "${prefix}"`}
        description="Try a different prefix or clear the filter to see all objects."
      />
    {:else}
      <EmptyState
        icon={FolderOpen}
        title="No objects yet — upload is how you start."
        description="Store any file: images, documents, binaries. QuicBaaS handles the rest."
        actionLabel="Upload Object"
        onaction={openUploadDialog}
      />
    {/if}
  {:else}
    <div class="overflow-x-auto rounded-box border border-base-300/50">
      <table class="table table-sm table-zebra w-full">
        <thead>
          <tr>
            <th>Key</th>
            <th>Size</th>
            <th>Content-Type</th>
            <th>Created</th>
            <th class="text-right">Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each objects as obj (obj.key)}
            <tr class="hover:bg-base-200/50">
              <td class="font-mono text-xs max-w-xs truncate">{obj.key}</td>
              <td class="text-xs">{formatBytes(obj.size)}</td>
              <td class="text-xs text-base-content/50">{obj.content_type ?? '—'}</td>
              <td class="text-xs text-base-content/50">
                {formatDateTime(obj.created_at)}
              </td>
              <td class="text-right">
                <div class="flex justify-end gap-1">
                  <button
                    class="btn btn-ghost btn-xs"
                    onclick={() => { void handleDownload(obj.key); }}
                    aria-label={`Download ${obj.key}`}
                    title={`Download ${obj.key}`}
                  >
                    <Download class="h-3 w-3" />
                  </button>
                  <button
                    class="btn btn-ghost btn-xs"
                    onclick={() => { void copyObjectURL(obj.key); }}
                    aria-label={`Copy URL for ${obj.key}`}
                    title={`Copy URL for ${obj.key}`}
                  >
                    <Copy class="h-3 w-3" />
                  </button>
                  <button
                    class="btn btn-ghost btn-xs text-error"
                    onclick={() => { objectToDelete = obj; }}
                    aria-label={`Delete ${obj.key}`}
                    title={`Delete ${obj.key}`}
                  >
                    <Trash2 class="h-3 w-3" />
                  </button>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<!-- Delete confirm modal -->
{#if objectToDelete}
  <dialog class="modal modal-open" aria-label="Delete object confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Object?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{objectToDelete.key}</span>? This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { objectToDelete = null; }}>Cancel</button>
        <button class="btn btn-error" onclick={() => { void confirmDeleteObject(); }} disabled={deletingObject}>
          {#if deletingObject}<span class="loading loading-spinner loading-xs"></span>{/if}
          Delete
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { objectToDelete = null; }} aria-label="Close delete dialog"></button>
  </dialog>
{/if}

<!-- Upload dialog -->
{#if showUploadDialog}
  <dialog class="modal modal-open" aria-label="Upload object dialog">
    <div class="modal-box">
      <h3 class="text-base font-semibold mb-4">Upload Object</h3>

      {#if uploadError}
        <div class="alert alert-error text-sm mb-3">{uploadError}</div>
      {/if}

      <label class="form-control mb-3">
        <div class="label"><span class="label-text">Object Key</span></div>
        <input
          type="text"
          class="input input-bordered font-mono"
          bind:value={uploadKey}
          placeholder="path/to/file.txt"
        />
      </label>

      <label class="form-control mb-4">
        <div class="label"><span class="label-text">File</span></div>
        <input
          bind:this={fileInput}
          type="file"
          class="file-input file-input-bordered w-full"
          onchange={handleFileSelect}
        />
        {#if selectedFile}
          <div class="label">
            <span class="label-text-alt text-base-content/40">
              {selectedFile.name} ({formatBytes(selectedFile.size)})
            </span>
          </div>
        {/if}
      </label>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { showUploadDialog = false; }}>Cancel</button>
        <button
          class="btn btn-primary"
          onclick={() => { void handleUpload(); }}
          disabled={uploading || !selectedFile}
        >
          {#if uploading}<span class="loading loading-spinner loading-xs"></span>{/if}
          Upload
        </button>
      </div>
    </div>
    <button class="modal-backdrop" onclick={() => { showUploadDialog = false; }} aria-label="Close dialog"></button>
  </dialog>
{/if}
