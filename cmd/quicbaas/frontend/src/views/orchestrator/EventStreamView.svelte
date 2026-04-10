<!--
  EventStreamView renders the full live event stream with auto-scroll toggle,
  type filters, and clear functionality.
-->
<script lang="ts">
  import { orchestratorStore, clearEvents } from '../../stores/orchestrator.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EventLog from '../../components/orchestrator/EventLog.svelte';
  import { Trash2 } from 'lucide-svelte';

  let autoScroll = $state(true);
  let typeFilter = $state('all');

  const eventTypes = $derived(() => {
    const types = new Set<string>();
    for (const e of orchestratorStore.events) {
      types.add(e.type);
    }
    return ['all', ...Array.from(types).sort()];
  });

  const filteredEvents = $derived(
    typeFilter === 'all'
      ? orchestratorStore.events
      : orchestratorStore.events.filter((e) => e.type === typeFilter)
  );
</script>

<div class="p-6 space-y-4 flex flex-col h-full">
  <PageHeader title="Live Events" subtitle="Real-time job event stream">
    {#snippet children()}
      <select class="select select-bordered select-sm" bind:value={typeFilter}>
        {#each eventTypes() as type}
          <option value={type}>{type}</option>
        {/each}
      </select>
      <label class="label cursor-pointer gap-2">
        <span class="label-text text-sm">Auto-scroll</span>
        <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={autoScroll} />
      </label>
      <button class="btn btn-ghost btn-sm" onclick={clearEvents} title="Clear events">
        <Trash2 class="h-4 w-4" />
      </button>
    {/snippet}
  </PageHeader>

  <div class="card bg-base-200 rounded-box flex-1">
    <div class="card-body p-4">
      <div class="text-xs text-base-content/40 mb-2">
        {filteredEvents.length} events
        {#if typeFilter !== 'all'}(filtered from {orchestratorStore.events.length}){/if}
      </div>
      <EventLog events={filteredEvents} maxHeight="calc(100vh - 280px)" autoScroll={autoScroll} />
    </div>
  </div>
</div>
