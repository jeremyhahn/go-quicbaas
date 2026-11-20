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
  MessagingView lists notification channels, supports creating new channels,
  publishing messages per channel, and shows recently published messages
  via per-session in-memory log (no SSE wiring needed for the smoke target).
-->
<script lang="ts">
  import PageHeader from '../../components/core/PageHeader.svelte';
  import DataTable from '../../components/core/DataTable.svelte';
  import type { Column } from '../../lib/dataTable.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { showToast } from '../../stores/toast.svelte.ts';
  import type { MessagingChannel } from '@quicbaas/sdk';
  import { Megaphone, Plus, Send, RefreshCw, Trash2, X } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';
  import { formatDateTime, formatTimeOnly } from '../../lib/time.ts';

  // ---------------------------------------------------------------------------
  // Channel list state
  // ---------------------------------------------------------------------------

  let channels = $state<MessagingChannel[]>([]);

  const CHANNEL_COLUMNS: Column<MessagingChannel>[] = [
    { key: 'name', label: 'Name', sortable: true, searchable: true },
    { key: 'subscribers', label: 'Subscribers', sortable: true },
    { key: 'buffer_size', label: 'Buffer', sortable: true },
    { key: 'persistent', label: 'Persistent', sortable: true },
    { key: 'created_at', label: 'Created', sortable: true },
    { key: 'actions', label: 'Actions', headerClass: 'text-right', srLabel: true },
  ];
  let loading = $state(false);
  let loadError = $state('');

  async function fetchChannels(): Promise<void> {
    loading = true;
    loadError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.messaging.listChannels({ limit: 100 });
      const raw = resp.channels ?? [];
      const seen = new Set<string>();
      channels = raw.filter((ch) => {
        if (seen.has(ch.name)) return false;
        seen.add(ch.name);
        return true;
      });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      loadError = `Failed to load channels: ${msg}`;
    } finally {
      loading = false;
    }
  }

  $effect(() => { void fetchChannels(); });

  // ---------------------------------------------------------------------------
  // Create channel dialog
  // ---------------------------------------------------------------------------

  let showCreateDialog = $state(false);
  let createName = $state('');
  let createBufferSize = $state(256);
  let createPersistent = $state(false);
  let createError = $state('');
  let creating = $state(false);

  function openCreateDialog(): void {
    showCreateDialog = true;
    createName = '';
    createBufferSize = 256;
    createPersistent = false;
    createError = '';
  }

  function closeCreateDialog(): void {
    showCreateDialog = false;
  }

  async function handleCreate(): Promise<void> {
    if (!createName.trim()) {
      createError = 'Channel name is required.';
      return;
    }
    creating = true;
    createError = '';
    try {
      const client = await getSDKClient();
      await client.messaging.createChannel({
        name: createName.trim(),
        buffer_size: createBufferSize,
        persistent: createPersistent,
      });
      showToast({ type: 'success', message: `Channel "${createName}" created.` });
      closeCreateDialog();
      await fetchChannels();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      createError = `Failed to create channel: ${msg}`;
    } finally {
      creating = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Delete channel (confirm modal)
  // ---------------------------------------------------------------------------

  let channelToDelete = $state<MessagingChannel | null>(null);
  let deletingChannel = $state(false);

  async function confirmDeleteChannel(): Promise<void> {
    if (!channelToDelete) return;
    deletingChannel = true;
    try {
      const client = await getSDKClient();
      await client.messaging.deleteChannel(channelToDelete.name);
      showToast({ type: 'success', message: `Channel "${channelToDelete.name}" deleted.` });
      channelToDelete = null;
      await fetchChannels();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      showToast({ type: 'error', message: `Delete failed: ${msg}` });
    } finally {
      deletingChannel = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Publish message dialog
  // ---------------------------------------------------------------------------

  interface SentMessage {
    channel: string;
    id: string;
    timestamp: string;
    payload: string;
  }

  let showPublishDialog = $state(false);
  let publishChannel = $state('');
  let publishPayload = $state('');
  let publishError = $state('');
  let publishing = $state(false);
  let sentMessages = $state<SentMessage[]>([]);

  function openPublishDialog(channelName: string): void {
    publishChannel = channelName;
    publishPayload = '';
    publishError = '';
    showPublishDialog = true;
  }

  function closePublishDialog(): void {
    showPublishDialog = false;
  }

  async function handlePublish(): Promise<void> {
    if (!publishPayload.trim()) {
      publishError = 'Payload is required.';
      return;
    }
    publishing = true;
    publishError = '';
    try {
      const client = await getSDKClient();
      const resp = await client.messaging.publish({
        channel: publishChannel,
        payload: publishPayload.trim(),
      });
      sentMessages = [
        {
          channel: publishChannel,
          id: resp.message_id,
          timestamp: resp.timestamp,
          payload: publishPayload.trim(),
        },
        ...sentMessages,
      ].slice(0, 50);
      showToast({ type: 'success', message: `Published to "${publishChannel}".` });
      closePublishDialog();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      publishError = `Publish failed: ${msg}`;
    } finally {
      publishing = false;
    }
  }
</script>

<div class="flex h-full flex-col">
  <div class="p-6 pb-0">
    <PageHeader title="Messaging" subtitle="Notification channels and message publishing" icon={Megaphone}>
      {#snippet children()}
        <button
          class="btn btn-ghost btn-sm gap-2"
          onclick={fetchChannels}
          disabled={loading}
          aria-label="Refresh channels"
        >
          <RefreshCw class={`h-4 w-4${loading ? ' animate-spin' : ''}`} />
          Refresh
        </button>
        <button
          class="btn btn-primary btn-sm gap-2"
          onclick={openCreateDialog}
          aria-label="Create Channel"
        >
          <Plus class="h-4 w-4" />
          Create Channel
        </button>
      {/snippet}
    </PageHeader>
  </div>

  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-6">

    <!-- Channel Table -->
    <section class="card bg-base-200 shadow-sm">
      <div class="card-body">
        <h2 class="card-title text-base">Channels</h2>

        {#if loadError}
          <div class="alert alert-error text-sm flex items-center justify-between">
            <span>{loadError}</span>
            <button class="btn btn-xs btn-ghost" onclick={() => { void fetchChannels(); }}>Retry</button>
          </div>
        {:else if loading}
          <div class="flex justify-center py-8">
            <span class="loading loading-spinner loading-md"></span>
          </div>
        {:else}
          <DataTable
            columns={CHANNEL_COLUMNS}
            rows={channels}
            rowKey={(ch: MessagingChannel) => ch.name}
            urlKey="channels"
            ariaLabel="Channels"
            searchPlaceholder="Search channels..."
            searchLabel="Search channels"
            noMatchText="No channels match the search."
          >
            {#snippet empty()}
              <EmptyState
                icon={Megaphone}
                title="No channels yet — channels broadcast to subscribed daemons."
                description="Create a channel to start publishing messages to connected agents and services."
                actionLabel="Create Channel"
                onaction={openCreateDialog}
              />
            {/snippet}

            {#snippet row(ch: MessagingChannel)}
              <tr>
                <td class="font-mono text-xs font-semibold">{ch.name}</td>
                <td>{ch.subscribers}</td>
                <td>{ch.buffer_size}</td>
                <td>
                  {#if ch.persistent}
                    <span class="badge badge-success badge-xs">yes</span>
                  {:else}
                    <span class="badge badge-ghost badge-xs">no</span>
                  {/if}
                </td>
                <td class="text-xs text-base-content/50">
                  {formatDateTime(ch.created_at)}
                </td>
                <td class="text-right">
                  <button
                    class="btn btn-xs btn-ghost gap-1"
                    onclick={() => openPublishDialog(ch.name)}
                    aria-label={`Publish to ${ch.name}`}
                  >
                    <Send class="h-3 w-3" />
                    Publish
                  </button>
                  <button
                    class="btn btn-xs btn-ghost text-error ml-1"
                    onclick={() => { channelToDelete = ch; }}
                    aria-label={`Delete channel ${ch.name}`}
                    title={`Delete channel ${ch.name}`}
                  >
                    <Trash2 class="h-3 w-3" />
                  </button>
                </td>
              </tr>
            {/snippet}
          </DataTable>
        {/if}
      </div>
    </section>

    <!-- Recent Messages Log -->
    {#if sentMessages.length > 0}
      <section class="card bg-base-200 shadow-sm">
        <div class="card-body">
          <h2 class="card-title text-base">Recently Published</h2>
          <div class="space-y-2 max-h-64 overflow-y-auto">
            {#each sentMessages as msg (msg.id)}
              <div class="rounded bg-base-300 px-3 py-2 text-xs font-mono">
                <span class="text-base-content/40">{formatTimeOnly(msg.timestamp)}</span>
                <span class="mx-2 text-primary">{msg.channel}</span>
                <span class="text-base-content/80">{msg.payload.slice(0, 120)}</span>
              </div>
            {/each}
          </div>
        </div>
      </section>
    {/if}

  </div>
</div>

<!-- Delete Channel Confirm Dialog -->
{#if channelToDelete}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Delete channel confirmation">
    <div class="modal-box max-w-sm">
      <h3 class="text-base font-semibold mb-2">Delete Channel?</h3>
      <p class="text-sm text-base-content/70">
        Delete <span class="font-mono font-semibold">{channelToDelete.name}</span>? All subscribers will be disconnected. This cannot be undone.
      </p>
      <div class="modal-action">
        <button class="btn btn-ghost" onclick={() => { channelToDelete = null; }}>Cancel</button>
        <button class="btn btn-error gap-2" onclick={confirmDeleteChannel} disabled={deletingChannel}>
          {#if deletingChannel}<span class="loading loading-spinner loading-xs"></span>{/if}
          Delete
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="presentation" onclick={() => { channelToDelete = null; }}></div>
  </div>
{/if}

<!-- Create Channel Dialog -->
{#if showCreateDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Create Channel">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">Create Channel</h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={closeCreateDialog} aria-label="Close">
          <X class="h-4 w-4" />
        </button>
      </div>

      <div class="flex flex-col gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Channel Name</span></div>
          <input
            class="input input-bordered w-full"
            type="text"
            placeholder="e.g. orders"
            bind:value={createName}
          />
        </label>

        <label class="form-control w-full">
          <div class="label"><span class="label-text">Buffer Size</span></div>
          <input
            class="input input-bordered w-full"
            type="number"
            min="1"
            max="65536"
            bind:value={createBufferSize}
          />
        </label>

        <label class="flex cursor-pointer items-center gap-3">
          <input type="checkbox" class="checkbox checkbox-sm" bind:checked={createPersistent} />
          <span class="label-text">Persistent (retain messages)</span>
        </label>

        {#if createError}
          <div class="alert alert-error text-sm">{createError}</div>
        {/if}
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={closeCreateDialog}>Cancel</button>
        <button
          class="btn btn-primary gap-2"
          onclick={handleCreate}
          disabled={creating}
          aria-label="Create"
        >
          {#if creating}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Create
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="presentation" onclick={closeCreateDialog}></div>
  </div>
{/if}

<!-- Publish Message Dialog -->
{#if showPublishDialog}
  <div class="modal modal-open" role="dialog" aria-modal="true" aria-label="Publish Message">
    <div class="modal-box">
      <div class="mb-4 flex items-center justify-between">
        <h3 class="text-base font-semibold">
          Publish to <span class="font-mono text-primary">{publishChannel}</span>
        </h3>
        <button class="btn btn-ghost btn-square btn-sm" onclick={closePublishDialog} aria-label="Close">
          <X class="h-4 w-4" />
        </button>
      </div>

      <div class="flex flex-col gap-4">
        <label class="form-control w-full">
          <div class="label"><span class="label-text">Payload (JSON or plain text)</span></div>
          <textarea
            class="textarea textarea-bordered w-full font-mono text-xs"
            rows="6"
            placeholder='&#123;"event": "order.created", "id": "ord_001"&#125;'
            bind:value={publishPayload}
          ></textarea>
        </label>

        {#if publishError}
          <div class="alert alert-error text-sm">{publishError}</div>
        {/if}
      </div>

      <div class="modal-action">
        <button class="btn btn-ghost" onclick={closePublishDialog}>Cancel</button>
        <button
          class="btn btn-primary gap-2"
          onclick={handlePublish}
          disabled={publishing}
          aria-label="Publish"
        >
          {#if publishing}
            <span class="loading loading-spinner loading-xs"></span>
          {:else}
            <Send class="h-4 w-4" />
          {/if}
          Publish
        </button>
      </div>
    </div>
    <div class="modal-backdrop" role="presentation" onclick={closePublishDialog}></div>
  </div>
{/if}
