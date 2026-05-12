<!--
  EventLog renders a scrollable log of JobEvents with timestamp, type badge,
  state badge, and message. Supports auto-scroll to the latest entry.
-->
<script lang="ts">
  import type { JobEvent } from '../../stores/orchestrator.svelte.ts';
  import StatusBadge from '../core/StatusBadge.svelte';
  import { formatTimeOnly } from '../../lib/time.ts';

  let {
    events,
    maxHeight = '400px',
    autoScroll = true,
  }: {
    events: JobEvent[];
    maxHeight?: string;
    autoScroll?: boolean;
  } = $props();

  let scrollEl: HTMLDivElement | undefined = $state();

  const formatTime = (ts: number): string => formatTimeOnly(ts);

  function eventTypeVariant(type: string): 'info' | 'success' | 'warning' | 'error' | 'primary' {
    if (type === 'error') return 'error';
    if (type === 'warning') return 'warning';
    if (type === 'state_change') return 'primary';
    if (type === 'completed') return 'success';
    return 'info';
  }

  $effect(() => {
    if (autoScroll && scrollEl && events.length > 0) {
      scrollEl.scrollTop = scrollEl.scrollHeight;
    }
  });
</script>

<div
  bind:this={scrollEl}
  class="overflow-y-auto font-mono text-xs space-y-1 pr-1"
  style:max-height={maxHeight}
>
  {#each events as event (`${event.timestamp}:${event.job_id}:${event.type}:${event.iteration}`)}
    <div class="flex items-start gap-2 py-1 border-b border-base-300/30">
      <span class="text-base-content/40 shrink-0 w-20">{formatTime(event.timestamp)}</span>
      <StatusBadge text={event.type} variant={eventTypeVariant(event.type)} />
      {#if event.state}
        <StatusBadge text={event.state} variant="ghost" />
      {/if}
      <span class="text-base-content/80 break-all">{event.message}</span>
    </div>
  {:else}
    <div class="text-base-content/30 text-center py-8">No events yet</div>
  {/each}
</div>
