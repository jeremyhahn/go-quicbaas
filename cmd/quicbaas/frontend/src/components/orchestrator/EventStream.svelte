<!--
  EventStream is the virtualized event log for a session.

  Technique: windowed DOM rendering with IntersectionObserver sentinels.
  - We keep a window of ~50 events visible in the DOM at any time.
  - A top sentinel element is observed: when it enters the viewport the
    window shifts up (older events) and triggers loadMoreHistory().
  - A bottom sentinel is observed: when visible it means the user is at
    the bottom, so auto-scroll is active. Scrolling away deactivates it
    and reveals a "jump to latest (N new)" sticky button.
  - On new events, if auto-scroll is active, we call scrollIntoView on the
    bottom sentinel. Otherwise we increment the pending-new counter.

  Coalescing: consecutive assistant_chunk events with the same source are
  merged into one AssistantEvent block before rendering. The last chunk
  in an in-flight stream has no stop_reason; once stop_reason appears the
  block is finalised.
-->
<script lang="ts">
  import { sessionStore, loadMoreHistory } from '../../stores/sessionStore.svelte.ts';
  import type { ParsedSessionEvent } from '../../stores/sessionStore.svelte.ts';
  import EventCard from './EventCard.svelte';

  // ---------------------------------------------------------------------------
  // Constants
  // ---------------------------------------------------------------------------

  const WINDOW_SIZE = 50;
  const SCROLL_DEBOUNCE_MS = 80;

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  // No props needed; driven entirely by sessionStore.

  // ---------------------------------------------------------------------------
  // Window state
  // ---------------------------------------------------------------------------

  /**
   * windowStart is the index into sessionStore.events[] at which the current
   * DOM window begins. We render events[windowStart .. windowStart+WINDOW_SIZE].
   */
  let windowStart = $state(0);
  let atBottom = $state(true);
  let pendingNew = $state(0);
  let isLoadingHistory = $state(false);

  // Sentinel refs.
  let topSentinelEl: HTMLDivElement | undefined = $state();
  let bottomSentinelEl: HTMLDivElement | undefined = $state();
  let scrollContainerEl: HTMLDivElement | undefined = $state();

  // ---------------------------------------------------------------------------
  // Coalesced render list
  // ---------------------------------------------------------------------------

  /**
   * A CoalescedItem is either a single event or a merged assistant block.
   */
  interface CoalescedItem {
    key: number; // sequence of the first event in the merge group
    event: ParsedSessionEvent;
    isStreaming: boolean;
  }

  /**
   * Build the coalesced render list from the current window slice.
   * Consecutive assistant_chunk events from the same source are merged.
   * The merged event carries the concatenated text; isStreaming is true
   * while no stop_reason has been seen yet.
   */
  function coalesce(events: ParsedSessionEvent[]): CoalescedItem[] {
    const items: CoalescedItem[] = [];

    for (const ev of events) {
      if (ev.kind !== 'assistant_chunk') {
        items.push({ key: ev.sequence, event: ev, isStreaming: false });
        continue;
      }

      const last = items[items.length - 1];
      if (
        last !== undefined &&
        last.event.kind === 'assistant_chunk' &&
        last.event.source === ev.source
      ) {
        // Merge into previous block.
        const merged: ParsedSessionEvent = {
          ...last.event,
          assistantChunk: (last.event.assistantChunk ?? '') + (ev.assistantChunk ?? ''),
          stopReason: ev.stopReason,
        };
        items[items.length - 1] = {
          key: last.key,
          event: merged,
          isStreaming: ev.stopReason === undefined || ev.stopReason === '',
        };
      } else {
        items.push({
          key: ev.sequence,
          event: ev,
          isStreaming: ev.stopReason === undefined || ev.stopReason === '',
        });
      }
    }

    return items;
  }

  const allEvents = $derived(sessionStore.events);

  const windowSlice = $derived(
    allEvents.slice(windowStart, windowStart + WINDOW_SIZE),
  );

  const coalescedItems = $derived(coalesce(windowSlice));

  // ---------------------------------------------------------------------------
  // Scroll management
  // ---------------------------------------------------------------------------

  // When new events arrive and we're at the bottom, scroll to bottom.
  let _prevLength = 0;
  $effect(() => {
    const len = allEvents.length;
    if (len === _prevLength) return;
    const added = len - _prevLength;
    _prevLength = len;

    if (added <= 0) return;

    // Advance window to keep the latest events visible.
    const newStart = Math.max(0, len - WINDOW_SIZE);
    if (newStart > windowStart) {
      windowStart = newStart;
    }

    if (atBottom) {
      pendingNew = 0;
      scrollToBottom();
    } else {
      pendingNew += added;
    }
  });

  function scrollToBottom(): void {
    if (bottomSentinelEl !== undefined) {
      bottomSentinelEl.scrollIntoView({ block: 'end', behavior: 'smooth' });
    }
  }

  function jumpToLatest(): void {
    const len = allEvents.length;
    windowStart = Math.max(0, len - WINDOW_SIZE);
    pendingNew = 0;
    atBottom = true;
    // Let the DOM settle, then scroll.
    requestAnimationFrame(scrollToBottom);
  }

  // ---------------------------------------------------------------------------
  // IntersectionObserver setup
  // ---------------------------------------------------------------------------

  $effect(() => {
    if (topSentinelEl === undefined || bottomSentinelEl === undefined) return;

    let scrollDebounce: ReturnType<typeof setTimeout> | null = null;

    const topObserver = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (entry === undefined || !entry.isIntersecting) return;
        if (isLoadingHistory) return;
        if (sessionStore.historyExhausted) return;

        // Shift window up by half a window worth of events.
        const shift = Math.floor(WINDOW_SIZE / 2);
        if (windowStart === 0) return;
        windowStart = Math.max(0, windowStart - shift);

        isLoadingHistory = true;
        void loadMoreHistory().then(() => { isLoadingHistory = false; });
      },
      { threshold: 0.1 },
    );

    const bottomObserver = new IntersectionObserver(
      (entries) => {
        const entry = entries[0];
        if (entry === undefined) return;
        if (entry.isIntersecting) {
          // User scrolled back to the bottom.
          if (scrollDebounce !== null) clearTimeout(scrollDebounce);
          scrollDebounce = setTimeout(() => {
            atBottom = true;
            pendingNew = 0;
          }, SCROLL_DEBOUNCE_MS);
        } else {
          if (scrollDebounce !== null) clearTimeout(scrollDebounce);
          scrollDebounce = setTimeout(() => {
            atBottom = false;
          }, SCROLL_DEBOUNCE_MS);
        }
      },
      { threshold: 0.1 },
    );

    topObserver.observe(topSentinelEl);
    bottomObserver.observe(bottomSentinelEl);

    return () => {
      topObserver.disconnect();
      bottomObserver.disconnect();
      if (scrollDebounce !== null) clearTimeout(scrollDebounce);
    };
  });
</script>

<!-- Scroll container -->
<div
  bind:this={scrollContainerEl}
  class="relative flex flex-col h-full overflow-y-auto overflow-x-hidden px-4 py-2"
  role="log"
  aria-live="polite"
  aria-label="Session event stream"
>
  <!-- Top sentinel: triggers history load when scrolled into view. -->
  <div bind:this={topSentinelEl} class="h-1 w-full shrink-0" aria-hidden="true">
    {#if isLoadingHistory}
      <div class="flex justify-center py-2">
        <span class="loading loading-dots loading-xs text-base-content/30"></span>
      </div>
    {:else if !sessionStore.historyExhausted && allEvents.length > 0}
      <div class="flex justify-center py-1">
        <span class="text-xs text-base-content/20">Scroll up to load history</span>
      </div>
    {:else if sessionStore.historyExhausted}
      <div class="flex justify-center py-1">
        <span class="text-xs text-base-content/20">Beginning of session</span>
      </div>
    {/if}
  </div>

  <!-- Windowed event list -->
  {#each coalescedItems as item (item.key)}
    <EventCard event={item.event} isStreaming={item.isStreaming} />
  {/each}

  <!-- Empty state -->
  {#if allEvents.length === 0}
    <div class="flex flex-1 items-center justify-center text-base-content/30 text-sm">
      No events yet. Send a prompt to start the session.
    </div>
  {/if}

  <!-- Bottom sentinel: auto-scroll target. -->
  <div bind:this={bottomSentinelEl} class="h-1 w-full shrink-0" aria-hidden="true"></div>
</div>

<!-- Jump-to-latest sticky button (shown when user has scrolled up). -->
{#if !atBottom && pendingNew > 0}
  <div class="absolute bottom-16 left-0 right-0 flex justify-center pointer-events-none z-10">
    <button
      class="btn btn-sm btn-primary shadow-lg pointer-events-auto"
      onclick={jumpToLatest}
      aria-label="Jump to latest events"
    >
      {pendingNew} new &darr;
    </button>
  </div>
{/if}
