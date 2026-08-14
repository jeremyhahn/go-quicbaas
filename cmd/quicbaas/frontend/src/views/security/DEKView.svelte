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
  DEKView renders an operator console for envelope-encryption Data
  Encryption Keys (DEKs). Equivalent to the SDK's dek client verb surface:
  create, get, list, rotate, delete, status, set-default, safety
  get/set/clear, start-reencryption, get-reencryption-progress.

  Actions:
    - DEK list with path-prefix filter, status/default badges, usage stats
    - Create DEK dialog
    - View Details (fresh single-record fetch via getDEK)
    - Rotate (new version)
    - Set Default
    - AEAD Safety limits (set / clear)
    - Delete (force / migrate-to confirmation)
    - Re-encryption dialog: start (explicit confirm, since it is a
      long-running, destructive-adjacent operation) + on-demand progress
      refresh by operation id
-->
<script lang="ts">
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { formatDateTime } from '../../lib/time.ts';
  import type {
    DEKMetadata,
    DEKAlgorithm,
    CreateDEKRequest,
    ReencryptionProgress,
    GetReencryptionProgressResponse,
    StartReencryptionResponse,
  } from '@quicbaas/sdk';
  import {
    KeySquare,
    Plus,
    RefreshCw,
    Trash2,
    Star,
    ShieldAlert,
    Repeat,
    PlayCircle,
    Eye,
  } from 'lucide-svelte';

  const ALGORITHMS: DEKAlgorithm[] = [
    'AES-128-GCM',
    'AES-192-GCM',
    'AES-256-GCM',
    'CHACHA20-POLY1305',
    'XCHACHA20-POLY1305',
  ];

  // ---------------------------------------------------------------------------
  // List
  // ---------------------------------------------------------------------------

  let deks = $state<DEKMetadata[]>([]);
  let totalCount = $state(0);
  let listLoading = $state(false);
  let listError = $state('');
  let filterPrefix = $state('');
  let filterRecursive = $state(true);

  async function loadDEKs(): Promise<void> {
    listLoading = true;
    listError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.dek.listDEKs({
        prefix: filterPrefix.trim() || undefined,
        recursive: filterRecursive,
        limit: 200,
      });
      deks = resp.deks;
      totalCount = resp.total_count;
    } catch (err: unknown) {
      listError = err instanceof Error ? err.message : String(err);
    } finally {
      listLoading = false;
    }
  }

  $effect(() => { void loadDEKs(); });

  // ---------------------------------------------------------------------------
  // Create dialog
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let creating = $state(false);
  let createError = $state('');
  let createPath = $state('');
  let createAlgorithm = $state<DEKAlgorithm>('AES-256-GCM');
  let createDescription = $state('');
  let createMaxOperations = $state(0);
  let createMaxBytes = $state(0);
  let createWarningPercent = $state(0);

  function openCreateDialog(): void {
    showCreateDialog = true;
    createError = '';
    createPath = '';
    createAlgorithm = 'AES-256-GCM';
    createDescription = '';
    createMaxOperations = 0;
    createMaxBytes = 0;
    createWarningPercent = 0;
  }

  async function submitCreate(): Promise<void> {
    if (!createPath.trim()) {
      createError = 'Path is required.';
      return;
    }
    creating = true;
    createError = '';
    try {
      const client = await getSDKClient();
      const req: CreateDEKRequest = {
        path: createPath.trim(),
        algorithm: createAlgorithm,
        description: createDescription.trim() || undefined,
        max_operations: createMaxOperations > 0 ? createMaxOperations : undefined,
        max_bytes: createMaxBytes > 0 ? createMaxBytes : undefined,
        warning_percent: createWarningPercent > 0 ? createWarningPercent : undefined,
      };
      await client.dek.createDEK(req);
      showCreateDialog = false;
      showToast('success', `DEK created at "${createPath.trim()}"`);
      await loadDEKs();
    } catch (err: unknown) {
      createError = err instanceof Error ? err.message : String(err);
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // View details (fresh fetch)
  // ---------------------------------------------------------------------------

  let detailTarget = $state<DEKMetadata | null>(null);
  let detailProgress = $state<ReencryptionProgress | null>(null);
  let detailLoading = $state(false);
  let detailError = $state('');

  // openDetail fetches the full DEK status — metadata plus any in-flight
  // re-encryption progress — rather than reusing the (possibly stale)
  // list-cached row.
  async function openDetail(path: string): Promise<void> {
    detailTarget = null;
    detailProgress = null;
    detailError = '';
    detailLoading = true;
    try {
      const client = await getSDKClient();
      const resp = await client.dek.getDEKStatus(path);
      detailTarget = resp.dek;
      detailProgress = resp.reencryption_progress ?? null;
    } catch (err: unknown) {
      detailError = err instanceof Error ? err.message : String(err);
    } finally {
      detailLoading = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Rotate
  // ---------------------------------------------------------------------------

  let rotateTarget = $state<DEKMetadata | null>(null);
  let rotateBackground = $state(false);
  let rotateRetainOld = $state(true);
  let rotating = $state(false);

  function openRotateDialog(dek: DEKMetadata): void {
    rotateTarget = dek;
    rotateBackground = false;
    rotateRetainOld = true;
  }

  async function confirmRotate(): Promise<void> {
    if (!rotateTarget) return;
    rotating = true;
    try {
      const client = await getSDKClient();
      const resp = await client.dek.rotateDEK(rotateTarget.path, {
        background: rotateBackground,
        retain_old_version: rotateRetainOld,
      });
      showToast('success', `Rotated "${resp.path}" to version ${resp.new_version}`);
      rotateTarget = null;
      await loadDEKs();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      rotating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Set default
  // ---------------------------------------------------------------------------

  let settingDefaultPath = $state('');

  async function setDefault(dek: DEKMetadata): Promise<void> {
    settingDefaultPath = dek.path;
    try {
      const client = await getSDKClient();
      await client.dek.setDefaultDEK(dek.path);
      showToast('success', `"${dek.path}" is now the default DEK`);
      await loadDEKs();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      settingDefaultPath = '';
    }
  }

  // ---------------------------------------------------------------------------
  // Safety limits (set / clear)
  // ---------------------------------------------------------------------------

  let safetyTarget = $state<DEKMetadata | null>(null);
  let safetyMaxOperations = $state(0);
  let safetyMaxBytes = $state(0);
  let safetyWarningPercent = $state(80);
  let savingSafety = $state(false);
  let clearingSafety = $state(false);
  let safetyError = $state('');

  function openSafetyDialog(dek: DEKMetadata): void {
    safetyTarget = dek;
    safetyError = '';
    safetyMaxOperations = dek.max_usage_count ?? 0;
    safetyMaxBytes = dek.max_bytes_used ?? 0;
    safetyWarningPercent = dek.warning_percent ?? 80;
  }

  async function submitSafety(): Promise<void> {
    if (!safetyTarget) return;
    savingSafety = true;
    safetyError = '';
    try {
      const client = await getSDKClient();
      await client.dek.setDEKSafety(safetyTarget.path, {
        max_operations: safetyMaxOperations > 0 ? safetyMaxOperations : undefined,
        max_bytes: safetyMaxBytes > 0 ? safetyMaxBytes : undefined,
        warning_percent: safetyWarningPercent > 0 ? safetyWarningPercent : undefined,
      });
      showToast('success', `Safety limits updated for "${safetyTarget.path}"`);
      safetyTarget = null;
      await loadDEKs();
    } catch (err: unknown) {
      safetyError = err instanceof Error ? err.message : String(err);
    } finally {
      savingSafety = false;
    }
  }

  async function clearSafety(): Promise<void> {
    if (!safetyTarget) return;
    clearingSafety = true;
    safetyError = '';
    try {
      const client = await getSDKClient();
      await client.dek.clearDEKSafety(safetyTarget.path);
      showToast('success', `Safety limits cleared for "${safetyTarget.path}"`);
      safetyTarget = null;
      await loadDEKs();
    } catch (err: unknown) {
      safetyError = err instanceof Error ? err.message : String(err);
    } finally {
      clearingSafety = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete
  // ---------------------------------------------------------------------------

  let deleteTarget = $state<DEKMetadata | null>(null);
  let deleteForce = $state(false);
  let deleteMigrateTo = $state('');
  let deleting = $state(false);

  function openDeleteDialog(dek: DEKMetadata): void {
    deleteTarget = dek;
    deleteForce = false;
    deleteMigrateTo = '';
  }

  async function confirmDelete(): Promise<void> {
    if (!deleteTarget) return;
    deleting = true;
    try {
      const client = await getSDKClient();
      await client.dek.deleteDEK(deleteTarget.path, {
        force: deleteForce,
        migrate_to: deleteMigrateTo.trim() || undefined,
      });
      showToast('success', `"${deleteTarget.path}" deleted`);
      deleteTarget = null;
      await loadDEKs();
    } catch (err: unknown) {
      showToast('error', err instanceof Error ? err.message : String(err));
    } finally {
      deleting = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Re-encryption: start (explicit confirm) + on-demand progress check
  // ---------------------------------------------------------------------------

  let reencryptTarget = $state<DEKMetadata | null>(null);
  let reencryptFromVersion = $state(0);
  let reencryptToVersion = $state(0);
  let reencryptBatchSize = $state(0);
  let reencryptForce = $state(false);
  let reencryptConfirmed = $state(false);
  let starting = $state(false);
  let startError = $state('');
  let startResult = $state<StartReencryptionResponse | null>(null);
  let progressResult = $state<GetReencryptionProgressResponse | null>(null);
  let progressError = $state('');
  let checkingProgress = $state(false);

  // openReencryptDialog re-fetches the DEK by path before prefilling
  // from/to version — the list-cached row can be stale, and starting a
  // long-running re-encryption against the wrong version range is exactly
  // the kind of mistake this dialog exists to prevent.
  async function openReencryptDialog(dek: DEKMetadata): Promise<void> {
    reencryptTarget = dek;
    reencryptBatchSize = 0;
    reencryptForce = false;
    reencryptConfirmed = false;
    startError = '';
    startResult = null;
    progressResult = null;
    progressError = '';
    reencryptFromVersion = dek.version;
    reencryptToVersion = dek.version + 1;
    try {
      const client = await getSDKClient();
      const resp = await client.dek.getDEK(dek.path);
      reencryptFromVersion = resp.dek.version;
      reencryptToVersion = resp.dek.version + 1;
    } catch {
      // Fall back silently to the list-cached version — the dialog still
      // lets the operator adjust the fields manually before starting.
    }
  }

  async function confirmStartReencryption(): Promise<void> {
    if (!reencryptTarget || !reencryptConfirmed) return;
    starting = true;
    startError = '';
    try {
      const client = await getSDKClient();
      startResult = await client.dek.startReencryption(reencryptTarget.path, {
        from_version: reencryptFromVersion > 0 ? reencryptFromVersion : undefined,
        to_version: reencryptToVersion > 0 ? reencryptToVersion : undefined,
        batch_size: reencryptBatchSize > 0 ? reencryptBatchSize : undefined,
        force: reencryptForce,
      });
      showToast('success', `Re-encryption started for "${reencryptTarget.path}"`);
    } catch (err: unknown) {
      startError = err instanceof Error ? err.message : String(err);
    } finally {
      starting = false;
    }
  }

  async function checkProgress(): Promise<void> {
    if (!reencryptTarget) return;
    checkingProgress = true;
    progressError = '';
    try {
      const client = await getSDKClient();
      progressResult = await client.dek.getReencryptionProgress(
        reencryptTarget.path,
        startResult?.operation_id,
      );
    } catch (err: unknown) {
      progressError = err instanceof Error ? err.message : String(err);
    } finally {
      checkingProgress = false;
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Data Encryption Keys (DEKs)" subtitle="Envelope-encryption keys — create, rotate, and re-encrypt" icon={KeySquare}>
    {#snippet children()}
      <button class="btn btn-ghost btn-sm gap-1" onclick={() => void loadDEKs()} disabled={listLoading} aria-label="Refresh">
        <RefreshCw class="h-3.5 w-3.5 {listLoading ? 'animate-spin' : ''}" />
        Refresh
      </button>
      <button class="btn btn-primary btn-sm gap-1" onclick={openCreateDialog} aria-label="Create DEK">
        <Plus class="h-3.5 w-3.5" />
        Create DEK
      </button>
    {/snippet}
  </PageHeader>

  <!-- Filters -->
  <div class="flex flex-wrap items-end gap-3">
    <div class="form-control">
      <label class="label label-text text-xs" for="dek-filter-prefix">Path Prefix</label>
      <input
        id="dek-filter-prefix"
        class="input input-bordered input-sm font-mono"
        type="text"
        placeholder="tenant/app"
        bind:value={filterPrefix}
        onkeydown={(e) => { if (e.key === 'Enter') void loadDEKs(); }}
      />
    </div>
    <label class="flex items-center gap-1 text-xs cursor-pointer pb-2">
      <input type="checkbox" class="checkbox checkbox-xs" bind:checked={filterRecursive} />
      Recursive
    </label>
    <button class="btn btn-sm btn-outline" onclick={() => void loadDEKs()}>Apply Filters</button>
  </div>

  {#if listError}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{listError}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => void loadDEKs()}>Retry</button>
    </div>
  {/if}

  {#if listLoading && deks.length === 0}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else if deks.length === 0}
    <EmptyState
      icon={KeySquare}
      title="No DEKs found"
      description="Create a Data Encryption Key or adjust the filters above to see existing ones."
      actionLabel="Create DEK"
      onaction={openCreateDialog}
    />
  {:else}
    <div class="text-xs text-base-content/40">{totalCount} total</div>
    <div class="overflow-x-auto rounded-box border border-base-300/50">
      <table class="table table-sm table-zebra">
        <thead>
          <tr class="bg-base-200/80 text-xs uppercase tracking-wider text-base-content/50">
            <th>Path</th>
            <th>Algorithm</th>
            <th>Version</th>
            <th>Status</th>
            <th>Usage</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {#each deks as dek (dek.path)}
            <tr class="hover">
              <td class="font-mono text-xs truncate max-w-[14rem]" title={dek.path}>
                {dek.path}
                {#if dek.is_default}<span class="badge badge-xs badge-primary ml-1">default</span>{/if}
              </td>
              <td class="font-mono text-xs">{dek.algorithm}</td>
              <td class="text-xs">{dek.version}</td>
              <td><span class="badge badge-xs {dek.status === 'active' ? 'badge-success' : 'badge-ghost'}">{dek.status}</span></td>
              <td class="text-xs text-base-content/50">{dek.usage_count} ops / {dek.bytes_used}B</td>
              <td>
                <div class="flex gap-1">
                  <div class="tooltip tooltip-left" data-tip="View Details">
                    <button class="btn btn-ghost btn-xs" onclick={() => void openDetail(dek.path)} aria-label={`View details for ${dek.path}`}>
                      <Eye class="h-3 w-3" />
                    </button>
                  </div>
                  <div class="tooltip tooltip-left" data-tip="Rotate">
                    <button class="btn btn-ghost btn-xs" onclick={() => openRotateDialog(dek)} aria-label={`Rotate ${dek.path}`}>
                      <Repeat class="h-3 w-3" />
                    </button>
                  </div>
                  {#if !dek.is_default}
                    <div class="tooltip tooltip-left" data-tip="Set Default">
                      <button
                        class="btn btn-ghost btn-xs"
                        onclick={() => void setDefault(dek)}
                        disabled={settingDefaultPath === dek.path}
                        aria-label={`Set ${dek.path} as default`}
                      >
                        <Star class="h-3 w-3" />
                      </button>
                    </div>
                  {/if}
                  <div class="tooltip tooltip-left" data-tip="Safety Limits">
                    <button class="btn btn-ghost btn-xs" onclick={() => openSafetyDialog(dek)} aria-label={`Safety limits for ${dek.path}`}>
                      <ShieldAlert class="h-3 w-3" />
                    </button>
                  </div>
                  <div class="tooltip tooltip-left" data-tip="Re-encrypt">
                    <button class="btn btn-ghost btn-xs" onclick={() => void openReencryptDialog(dek)} aria-label={`Re-encrypt ${dek.path}`}>
                      <PlayCircle class="h-3 w-3" />
                    </button>
                  </div>
                  <div class="tooltip tooltip-left" data-tip="Delete">
                    <button class="btn btn-ghost btn-xs text-error" onclick={() => openDeleteDialog(dek)} aria-label={`Delete ${dek.path}`}>
                      <Trash2 class="h-3 w-3" />
                    </button>
                  </div>
                </div>
              </td>
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
  {/if}
</div>

<!-- =========================================================================
     CREATE DEK DIALOG
     ========================================================================= -->
{#if showCreateDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create DEK dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">Create DEK</h3>
      {#if createError}
        <div class="alert alert-error text-sm mb-3" role="alert">{createError}</div>
      {/if}
      <div class="space-y-3">
        <div class="form-control">
          <label class="label label-text" for="dek-create-path">Path <span class="text-error">*</span></label>
          <input id="dek-create-path" class="input input-bordered input-sm font-mono" type="text" placeholder="tenant/app/key" bind:value={createPath} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="dek-create-algo">Algorithm</label>
          <select id="dek-create-algo" class="select select-bordered select-sm" bind:value={createAlgorithm}>
            {#each ALGORITHMS as algo (algo)}
              <option value={algo}>{algo}</option>
            {/each}
          </select>
        </div>
        <div class="form-control">
          <label class="label label-text" for="dek-create-desc">Description</label>
          <input id="dek-create-desc" class="input input-bordered input-sm" type="text" bind:value={createDescription} />
        </div>
        <div class="grid grid-cols-3 gap-3">
          <div class="form-control">
            <label class="label label-text" for="dek-create-max-ops">Max Operations (0 = default)</label>
            <input id="dek-create-max-ops" class="input input-bordered input-sm" type="number" min="0" bind:value={createMaxOperations} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="dek-create-max-bytes">Max Bytes (0 = default)</label>
            <input id="dek-create-max-bytes" class="input input-bordered input-sm" type="number" min="0" bind:value={createMaxBytes} />
          </div>
          <div class="form-control">
            <label class="label label-text" for="dek-create-warn">Warning % (0 = default)</label>
            <input id="dek-create-warn" class="input input-bordered input-sm" type="number" min="0" max="100" bind:value={createWarningPercent} />
          </div>
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { showCreateDialog = false; }} disabled={creating}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={creating} onclick={() => void submitCreate()}>
          {creating ? 'Creating…' : 'Create'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { showCreateDialog = false; }} onkeydown={() => { showCreateDialog = false; }}></div>
  </div>
{/if}

<!-- =========================================================================
     VIEW DETAILS
     ========================================================================= -->
{#if detailLoading || detailTarget || detailError}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="DEK details">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-4">DEK Details</h3>
      {#if detailError}
        <div class="alert alert-error text-sm" role="alert">{detailError}</div>
      {:else if detailLoading}
        <div class="skeleton h-24 w-full rounded-box"></div>
      {:else if detailTarget}
        <div class="grid grid-cols-2 gap-3 text-xs">
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Path</div><div class="font-mono">{detailTarget.path}</div></div>
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Algorithm</div><div class="font-mono">{detailTarget.algorithm}</div></div>
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Version</div><div>{detailTarget.version}</div></div>
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Status</div><div>{detailTarget.status}</div></div>
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Created</div><div>{formatDateTime(detailTarget.created_at)}</div></div>
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Rotated</div><div>{formatDateTime(detailTarget.rotated_at)}</div></div>
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Usage Count</div><div>{detailTarget.usage_count}</div></div>
          <div><div class="text-base-content/40 uppercase tracking-wide mb-0.5">Bytes Used</div><div>{detailTarget.bytes_used}</div></div>
        </div>
        {#if detailProgress}
          <div class="mt-4 pt-3 border-t border-base-300/50">
            <div class="text-xs font-semibold text-base-content/50 uppercase tracking-wide mb-2">Re-encryption Status</div>
            <progress class="progress progress-primary w-full" value={detailProgress.percent_complete} max="100"></progress>
            <div class="text-xs text-base-content/60 mt-1">
              {detailProgress.processed_keys} / {detailProgress.total_keys} processed
              ({detailProgress.percent_complete.toFixed(1)}%), {detailProgress.failed_keys} failed — {detailProgress.status}
            </div>
          </div>
        {/if}
      {/if}
      <div class="modal-action">
        <button class="btn btn-primary btn-sm" onclick={() => { detailTarget = null; detailError = ''; }}>Close</button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { detailTarget = null; detailError = ''; }} onkeydown={() => { detailTarget = null; detailError = ''; }}></div>
  </div>
{/if}

<!-- =========================================================================
     ROTATE DIALOG
     ========================================================================= -->
{#if rotateTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Rotate DEK dialog">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Rotate DEK?</h3>
      <p class="text-sm text-base-content/70 mb-3">
        Create a new version of <span class="font-mono font-semibold">{rotateTarget.path}</span>.
      </p>
      <label class="flex items-center gap-2 cursor-pointer text-sm mb-2">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={rotateBackground} />
        Run in background
      </label>
      <label class="flex items-center gap-2 cursor-pointer text-sm">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={rotateRetainOld} />
        Retain old version
      </label>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { rotateTarget = null; }} disabled={rotating}>Cancel</button>
        <button class="btn btn-primary btn-sm" disabled={rotating} onclick={() => void confirmRotate()}>
          {rotating ? 'Rotating…' : 'Rotate'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { rotateTarget = null; }} onkeydown={() => { rotateTarget = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     SAFETY LIMITS DIALOG
     ========================================================================= -->
{#if safetyTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="DEK safety limits dialog">
    <div class="modal-box max-w-md">
      <h3 class="text-base font-semibold mb-4">Safety Limits — <span class="font-mono">{safetyTarget.path}</span></h3>
      {#if safetyError}
        <div class="alert alert-error text-sm mb-3" role="alert">{safetyError}</div>
      {/if}
      <div class="grid grid-cols-3 gap-3">
        <div class="form-control">
          <label class="label label-text" for="dek-safety-max-ops">Max Operations</label>
          <input id="dek-safety-max-ops" class="input input-bordered input-sm" type="number" min="0" bind:value={safetyMaxOperations} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="dek-safety-max-bytes">Max Bytes</label>
          <input id="dek-safety-max-bytes" class="input input-bordered input-sm" type="number" min="0" bind:value={safetyMaxBytes} />
        </div>
        <div class="form-control">
          <label class="label label-text" for="dek-safety-warn">Warning %</label>
          <input id="dek-safety-warn" class="input input-bordered input-sm" type="number" min="0" max="100" bind:value={safetyWarningPercent} />
        </div>
      </div>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { safetyTarget = null; }} disabled={savingSafety || clearingSafety}>Close</button>
        <button class="btn btn-outline btn-error btn-sm" disabled={savingSafety || clearingSafety} onclick={() => void clearSafety()}>
          {clearingSafety ? 'Clearing…' : 'Clear'}
        </button>
        <button class="btn btn-primary btn-sm" disabled={savingSafety || clearingSafety} onclick={() => void submitSafety()}>
          {savingSafety ? 'Saving…' : 'Save'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { safetyTarget = null; }} onkeydown={() => { safetyTarget = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     DELETE CONFIRM DIALOG
     ========================================================================= -->
{#if deleteTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete DEK confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete DEK?</h3>
      <p class="text-sm text-base-content/70 mb-3">
        Delete <span class="font-mono font-semibold">{deleteTarget.path}</span>? This cannot be undone.
      </p>
      <div class="form-control mb-2">
        <label class="label label-text" for="dek-delete-migrate">Migrate To (optional path)</label>
        <input id="dek-delete-migrate" class="input input-bordered input-sm font-mono" type="text" bind:value={deleteMigrateTo} />
      </div>
      <label class="flex items-center gap-2 cursor-pointer text-sm">
        <input type="checkbox" class="checkbox checkbox-sm" bind:checked={deleteForce} />
        Force delete
      </label>
      <div class="modal-action">
        <button class="btn btn-ghost btn-sm" onclick={() => { deleteTarget = null; }} disabled={deleting}>Cancel</button>
        <button class="btn btn-error btn-sm" disabled={deleting} onclick={() => void confirmDelete()}>
          {deleting ? 'Deleting…' : 'Delete'}
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { deleteTarget = null; }} onkeydown={() => { deleteTarget = null; }}></div>
  </div>
{/if}

<!-- =========================================================================
     RE-ENCRYPTION DIALOG
     ========================================================================= -->
{#if reencryptTarget}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Re-encryption dialog">
    <div class="modal-box max-w-lg">
      <h3 class="text-base font-semibold mb-1">Re-encrypt — <span class="font-mono">{reencryptTarget.path}</span></h3>
      <p class="text-xs text-warning mb-4">
        This starts a long-running background operation that re-encrypts all data protected by this DEK onto a
        newer version. It cannot be cancelled once started.
      </p>

      {#if startError}
        <div class="alert alert-error text-sm mb-3" role="alert">{startError}</div>
      {/if}

      {#if !startResult}
        <div class="space-y-3">
          <div class="grid grid-cols-2 gap-3">
            <div class="form-control">
              <label class="label label-text" for="reencrypt-from">From Version</label>
              <input id="reencrypt-from" class="input input-bordered input-sm" type="number" min="0" bind:value={reencryptFromVersion} />
            </div>
            <div class="form-control">
              <label class="label label-text" for="reencrypt-to">To Version</label>
              <input id="reencrypt-to" class="input input-bordered input-sm" type="number" min="0" bind:value={reencryptToVersion} />
            </div>
          </div>
          <div class="form-control">
            <label class="label label-text" for="reencrypt-batch">Batch Size (0 = default)</label>
            <input id="reencrypt-batch" class="input input-bordered input-sm" type="number" min="0" bind:value={reencryptBatchSize} />
          </div>
          <label class="flex items-center gap-2 cursor-pointer text-sm">
            <input type="checkbox" class="checkbox checkbox-sm" bind:checked={reencryptForce} />
            Force (even if one is already in progress)
          </label>
          <label class="flex items-center gap-2 cursor-pointer text-sm font-semibold text-warning">
            <input type="checkbox" class="checkbox checkbox-sm checkbox-warning" bind:checked={reencryptConfirmed} />
            I understand this cannot be undone and will re-encrypt all affected data.
          </label>
        </div>
        <div class="modal-action">
          <button class="btn btn-ghost btn-sm" onclick={() => { reencryptTarget = null; }} disabled={starting}>Cancel</button>
          <button class="btn btn-warning btn-sm" disabled={starting || !reencryptConfirmed} onclick={() => void confirmStartReencryption()}>
            {starting ? 'Starting…' : 'Start Re-encryption'}
          </button>
        </div>
      {:else}
        <div class="alert alert-success text-sm mb-3" role="alert">
          Started (operation <span class="font-mono">{startResult.operation_id}</span>, ~{startResult.estimated_items} items).
        </div>

        {#if progressError}
          <div class="alert alert-error text-sm mb-3" role="alert">{progressError}</div>
        {/if}

        {#if progressResult?.progress}
          <div class="space-y-2">
            <progress class="progress progress-primary w-full" value={progressResult.progress.percent_complete} max="100"></progress>
            <div class="text-xs text-base-content/60">
              {progressResult.progress.processed_keys} / {progressResult.progress.total_keys} processed
              ({progressResult.progress.percent_complete.toFixed(1)}%), {progressResult.progress.failed_keys} failed
              — status: {progressResult.status}
            </div>
            {#if progressResult.error}
              <div class="alert alert-error text-xs" role="alert">{progressResult.error}</div>
            {/if}
          </div>
        {/if}

        <div class="modal-action">
          <button class="btn btn-ghost btn-sm" onclick={() => { reencryptTarget = null; }}>Close</button>
          <button class="btn btn-outline btn-sm" disabled={checkingProgress} onclick={() => void checkProgress()}>
            {checkingProgress ? 'Checking…' : 'Check Progress'}
          </button>
        </div>
      {/if}
    </div>
    <div class="modal-backdrop" role="none" onclick={() => { reencryptTarget = null; }} onkeydown={() => { reencryptTarget = null; }}></div>
  </div>
{/if}
