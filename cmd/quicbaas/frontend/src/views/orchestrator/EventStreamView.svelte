<!--
  EventStreamView renders the full live event stream with auto-scroll toggle,
  type filters, and clear functionality.
-->
<script lang="ts">
  import { orchestratorStore, clearEvents } from '../../stores/orchestrator.svelte.ts';
  import PageHeader from '../../components/core/PageHeader.svelte';
  import EventLog from '../../components/orchestrator/EventLog.svelte';
  import { Trash2, Activity } from 'lucide-svelte';
  import EmptyState from '../../components/core/EmptyState.svelte';

  let autoScroll = $state(true);
  let typeFilter = $state('all');

  const safeEvents = $derived(Array.isArray(orchestratorStore.events) ? orchestratorStore.events : []);

  const eventTypes = $derived(() => {
    const types = new Set<string>();
    for (const e of safeEvents) {
      types.add(e.type);
    }
    return ['all', ...Array.from(types).sort()];
  });

  const filteredEvents = $derived(
    typeFilter === 'all'
      ? safeEvents
      : safeEvents.filter((e) => e.type === typeFilter)
  );
</script>

<div class="page h-full">
  <PageHeader title="Live Events" subtitle="Real-time job event stream" icon={Activity}>
    {#snippet children()}
      <select class="select select-sm w-auto min-w-40 bg-base-100" aria-label="Event type" bind:value={typeFilter}>
        {#each eventTypes() as type}
          <option value={type}>{type}</option>
        {/each}
      </select>
      <label class="label cursor-pointer gap-2 px-1">
        <span class="text-sm text-base-content">Auto-scroll</span>
        <input type="checkbox" class="toggle toggle-sm toggle-primary" bind:checked={autoScroll} />
      </label>
      <button class="btn btn-ghost btn-sm" onclick={clearEvents} title="Clear events">
        <Trash2 class="h-4 w-4" />
      </button>
    {/snippet}
  </PageHeader>

  <div class="card bg-base-200 rounded-box flex-1">
    <div class="card-body p-5">
      {#if filteredEvents.length === 0}
        <EmptyState
          icon={Activity}
          title="No events captured — events stream live as jobs run."
          description="Start a job to see real-time execution events appear here."
        />
      {:else}
        <div class="text-xs text-base-content/40 mb-2">
          {filteredEvents.length} events
          {#if typeFilter !== 'all'}(filtered from {safeEvents.length}){/if}
        </div>
        <EventLog events={filteredEvents} maxHeight="calc(100vh - 280px)" autoScroll={autoScroll} />
      {/if}
    </div>
  </div>
</div>
