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
  BackupView lists the cluster's backups from /api/v1/backups and lets an
  operator create, verify and delete them. Restore is in the SDK client but
  has no button here: it overwrites shards and needs its own confirmation.
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import { HardDrive } from 'lucide-svelte';
  import { formatDateTime } from '../../lib/time.ts';
  import type { BackupInfo, VerifyBackupResponse } from '@quicbaas/sdk';
  import type { Component } from 'svelte';

  /** The lucide-svelte icon typed as the component PageHeader takes. */
  const ICON = HardDrive as unknown as Component<{ class?: string }>;

  /** Shown when the connection's SDK client has no backups client. */
  const UNAVAILABLE = 'Backups are not available over this connection';

  const COLUMNS: Column<BackupInfo>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true, value: (b) => b.name || b.backup_id },
    { key: 'timestamp', label: 'Created', sortable: true },
    { key: 'size', label: 'Size', sortable: true },
    { key: 'status', label: 'Status', sortable: true, searchable: true },
    { key: 'shards', label: 'Shards', value: (b) => b.shards?.length ?? 0 },
    { key: 'actions', label: 'Actions', srLabel: true },
  ];

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let backups: BackupInfo[] = $state([]);
  let loading = $state(false);
  let error: string | null = $state(null);

  let name = $state('');
  let description = $state('');
  let creating = $state(false);

  /** verifyResults holds the last verify outcome per backup id. */
  let verifyResults: Record<string, VerifyBackupResponse> = $state({});
  /** busyId is the backup a verify or delete is running on. */
  let busyId: string | null = $state(null);

  // ---------------------------------------------------------------------------
  // Data loading and actions
  // ---------------------------------------------------------------------------

  async function loadBackups(): Promise<void> {
    loading = true;
    error = null;
    try {
      const client = await getSDKClient();
      if (!client.backups) { backups = []; error = UNAVAILABLE; return; }
      const resp = await client.backups.list();
      backups = resp.backups ?? [];
    } catch (e) {
      error = e instanceof Error ? e.message : 'Failed to load backups';
    } finally {
      loading = false;
    }
  }

  $effect(() => { void loadBackups(); });

  async function createBackup(): Promise<void> {
    const trimmed = name.trim();
    if (!trimmed || creating) return;
    creating = true;
    try {
      const client = await getSDKClient();
      if (!client.backups) { showToast('error', UNAVAILABLE); return; }
      await client.backups.create({ name: trimmed, description: description.trim() });
      showToast('success', `Backup "${trimmed}" created`);
      name = '';
      description = '';
      await loadBackups();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to create backup');
    } finally {
      creating = false;
    }
  }

  async function verifyBackup(b: BackupInfo): Promise<void> {
    busyId = b.backup_id;
    try {
      const client = await getSDKClient();
      if (!client.backups) { showToast('error', UNAVAILABLE); return; }
      const resp = await client.backups.verify(b.backup_id);
      verifyResults = { ...verifyResults, [b.backup_id]: resp };
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to verify backup');
    } finally {
      busyId = null;
    }
  }

  async function deleteBackup(b: BackupInfo): Promise<void> {
    const label = b.name || b.backup_id;
    if (!window.confirm(`Delete backup "${label}"? This cannot be undone.`)) return;
    busyId = b.backup_id;
    try {
      const client = await getSDKClient();
      if (!client.backups) { showToast('error', UNAVAILABLE); return; }
      await client.backups.delete(b.backup_id);
      showToast('success', `Backup "${label}" deleted`);
      await loadBackups();
    } catch (e) {
      showToast('error', e instanceof Error ? e.message : 'Failed to delete backup');
    } finally {
      busyId = null;
    }
  }

  // ---------------------------------------------------------------------------
  // Display helpers
  // ---------------------------------------------------------------------------

  function formatSize(bytes: number): string {
    const units = ['B', 'KiB', 'MiB', 'GiB'];
    let v = bytes;
    let i = 0;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    return i === 0 ? `${v} ${units[i]}` : `${v.toFixed(1)} ${units[i]}`;
  }

  function statusClass(status: string): string {
    switch (status) {
      case 'completed':
      case 'complete':
      case 'success':
        return 'badge-success';
      case 'failed':
      case 'error':
        return 'badge-error';
      default:
        return 'badge-warning';
    }
  }
</script>

<div class="p-6 space-y-6">
  <PageHeader title="Backup" subtitle="Snapshots of the cluster's shards" icon={ICON} />

  <form
    class="card bg-base-200 shadow-sm"
    aria-label="Create backup"
    onsubmit={(e) => { e.preventDefault(); void createBackup(); }}
  >
    <div class="card-body py-4 gap-3">
      <h2 class="font-medium">Create backup</h2>
      <div class="flex flex-wrap gap-3 items-end">
        <label class="form-control">
          <span class="label-text text-sm">Name</span>
          <input class="input input-bordered input-sm" type="text" name="name" bind:value={name} required />
        </label>
        <label class="form-control flex-1 min-w-48">
          <span class="label-text text-sm">Description</span>
          <input class="input input-bordered input-sm" type="text" name="description" bind:value={description} />
        </label>
        <button class="btn btn-primary btn-sm" type="submit" disabled={!name.trim() || creating}>
          {#if creating}<span class="loading loading-spinner loading-xs"></span>{/if}
          Create
        </button>
      </div>
    </div>
  </form>

  {#if error}
    <div class="alert alert-error text-sm flex items-center justify-between" role="alert">
      <span>{error}</span>
      <button class="btn btn-xs btn-ghost" onclick={() => { void loadBackups(); }}>Retry</button>
    </div>
  {/if}

  {#if loading}
    <div class="flex justify-center py-8">
      <span class="loading loading-spinner loading-md"></span>
    </div>
  {:else if !error}
    <DataTable
      columns={COLUMNS}
      rows={backups}
      rowKey={(b: BackupInfo) => b.backup_id}
      ariaLabel="Backups"
      emptyTitle="No backups yet"
    >
      {#snippet row(b: BackupInfo)}
        <tr class="hover">
          <td>
            <div class="font-medium">{b.name || b.backup_id}</div>
            {#if b.description}
              <div class="text-xs text-base-content/60">{b.description}</div>
            {/if}
          </td>
          <td class="text-sm">{formatDateTime(b.timestamp)}</td>
          <td class="font-mono text-xs">{formatSize(b.size)}</td>
          <td><span class="badge badge-sm {statusClass(b.status)}">{b.status}</span></td>
          <td class="font-mono text-xs">{b.shards?.length ?? 0}</td>
          <td>
            <div class="flex items-center gap-2">
              <button
                class="btn btn-xs btn-ghost"
                disabled={busyId === b.backup_id}
                onclick={() => { void verifyBackup(b); }}
              >Verify</button>
              <button
                class="btn btn-xs btn-ghost text-error"
                disabled={busyId === b.backup_id}
                onclick={() => { void deleteBackup(b); }}
              >Delete</button>
              {#if verifyResults[b.backup_id]}
                {@const r = verifyResults[b.backup_id]}
                {#if r?.valid}
                  <span class="badge badge-success badge-sm">Valid</span>
                {:else}
                  <span class="text-xs text-error">
                    {r?.issues && r.issues.length > 0 ? r.issues.join('; ') : 'Invalid'}
                  </span>
                {/if}
              {/if}
            </div>
          </td>
        </tr>
      {/snippet}
    </DataTable>
  {/if}
</div>
