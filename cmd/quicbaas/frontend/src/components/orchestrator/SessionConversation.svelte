<!--
  SessionConversation renders a session's transcript as the turns it is made
  of: who spoke, when, and what they said.

  It is the session page's primary content and the counterpart to
  EventStream. EventStream shows the raw log — every fragment, thought,
  presence change and file operation, in the order the server recorded them.
  This shows the same transcript folded: a streamed answer is one block here
  and forty events there.

  Oldest first, newest last, scrolled to the end on arrival — the reading
  order of a conversation, and the one the composer below the page continues.
-->
<script lang="ts">
  import type { SessionTurn } from '@quicbaas/sdk';
  import { sessionStore, loadMoreConversation } from '../../stores/sessionStore.svelte.ts';
  import { formatDateTime } from '../../lib/time.ts';
  import { MessageSquare } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  // sessionClosed changes only what the empty state says. A closed session
  // with no turns said nothing and never will; an open one has not spoken
  // yet. Rendering the same words for both tells an operator a session is
  // waiting on them when it is finished.
  let { sessionClosed = false }: { sessionClosed?: boolean } = $props();

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let loadingMore = $state(false);
  let scrollEl: HTMLDivElement | undefined = $state();

  const turns = $derived(sessionStore.conversation);

  // Retention floor. The server reports the lowest sequence it still holds;
  // above 1 means earlier turns were dropped, so the top of the list is the
  // beginning of what remains rather than the beginning of the session.
  const truncatedByRetention = $derived(sessionStore.conversationOldestSequence > 1);

  // ---------------------------------------------------------------------------
  // Scroll management
  // ---------------------------------------------------------------------------

  /**
   * Keep the newest turn in view as turns arrive, unless the operator has
   * scrolled up to read something — in which case moving the viewport under
   * them would be the bug, not the feature.
   */
  $effect(() => {
    const count = turns.length;
    const el = scrollEl;
    if (el === undefined || count === 0) return;
    const distanceFromBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    if (distanceFromBottom < 120) {
      el.scrollTop = el.scrollHeight;
    }
  });

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  async function onLoadMore(): Promise<void> {
    if (loadingMore) return;
    loadingMore = true;
    try {
      await loadMoreConversation();
    } finally {
      loadingMore = false;
    }
  }

  // ---------------------------------------------------------------------------
  // Presentation
  // ---------------------------------------------------------------------------

  function roleLabel(turn: SessionTurn): string {
    if (turn.role === 'user') return 'You';
    if (turn.role === 'assistant') return turn.model || 'Assistant';
    return 'System';
  }

  const ROLE_BADGE: Record<SessionTurn['role'], string> = {
    user: 'badge-primary',
    assistant: 'badge-secondary',
    system: 'badge-ghost',
  };

  const ROLE_ACCENT: Record<SessionTurn['role'], string> = {
    user: 'border-primary/40',
    assistant: 'border-secondary/40',
    system: 'border-base-300',
  };

  /**
   * The message a failed read shows. The transport's own text goes
   * underneath as detail; this line says what it means for the operator.
   */
  const errorHeadline = $derived.by(() => {
    switch (sessionStore.conversationErrorKind) {
      case 'forbidden':
        return "You are not permitted to read this session's conversation.";
      case 'unavailable':
        return 'The conversation is not available on this node right now.';
      default:
        return "This session's conversation could not be loaded.";
    }
  });
</script>

<div
  class="flex h-full flex-col overflow-y-auto px-4 py-3"
  bind:this={scrollEl}
  data-testid="session-conversation"
>
  <!-- Loading: the first page is still in flight. Gated ahead of the empty
       state so a session that has spoken never flashes "nothing here yet". -->
  {#if sessionStore.conversationLoading && turns.length === 0}
    <div
      class="flex flex-1 items-center justify-center gap-2 text-sm text-base-content/30"
      data-testid="conversation-loading"
    >
      <span class="loading loading-spinner loading-sm"></span>
      Loading conversation…
    </div>

  <!-- Unreadable: gated ahead of the empty state, because a conversation
       that could not be fetched and one that does not exist used to render
       identically (go-quicbaas#793, #834). -->
  {:else if sessionStore.conversationError && turns.length === 0}
    <div
      class="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center text-sm text-error"
      data-testid="conversation-error"
      role="alert"
    >
      <span>{errorHeadline}</span>
      <span class="max-w-md break-words text-xs text-base-content/50">
        {sessionStore.conversationError}
      </span>
    </div>

  <!-- Empty: we looked, and the session genuinely has no turns. -->
  {:else if turns.length === 0}
    <div
      class="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center"
      data-testid="conversation-empty"
    >
      <MessageSquare class="h-8 w-8 text-base-content/20" aria-hidden="true" />
      {#if sessionClosed}
        <div class="text-sm font-medium text-base-content/70">
          This session closed without a conversation
        </div>
        <p class="max-w-sm text-xs text-base-content/40">
          Nothing was said before it ended. The event log may still show why.
        </p>
      {:else}
        <div class="text-sm font-medium text-base-content/70">No turns yet</div>
        <p class="max-w-sm text-xs text-base-content/40">
          Send a prompt and the conversation will appear here.
        </p>
      {/if}
    </div>

  {:else}
    <!-- A read failed while turns were already on screen: say so without
         taking the conversation away. -->
    {#if sessionStore.conversationError}
      <div class="alert alert-warning mb-2 px-3 py-1.5 text-xs" role="alert">
        {errorHeadline}
        <span class="text-base-content/60">{sessionStore.conversationError}</span>
      </div>
    {/if}

    {#if truncatedByRetention}
      <div class="mb-2 text-center text-xs text-base-content/25">
        Earlier turns are no longer retained — this is the beginning of what remains.
      </div>
    {:else}
      <div class="mb-2 text-center text-xs text-base-content/20">Beginning of conversation</div>
    {/if}

    <div class="flex flex-col gap-3">
      {#each turns as turn (turn.sequence)}
        <article
          class="rounded-box border-l-2 bg-base-200/40 px-3 py-2 {ROLE_ACCENT[turn.role]}"
          data-testid="conversation-turn"
          data-role={turn.role}
          data-sequence={turn.sequence}
        >
          <header class="mb-1 flex flex-wrap items-center gap-2">
            <span class="badge badge-xs {ROLE_BADGE[turn.role]}">{turn.role}</span>
            <span class="text-xs font-medium text-base-content/70">{roleLabel(turn)}</span>
            <time class="text-xs text-base-content/35" datetime={turn.timestamp}>
              {formatDateTime(turn.timestamp)}
            </time>
            {#if turn.stop_reason}
              <span class="badge badge-xs badge-outline">{turn.stop_reason}</span>
            {/if}
          </header>

          <pre class="whitespace-pre-wrap break-words font-mono text-xs leading-relaxed text-base-content">{turn.text}</pre>

          {#if turn.truncated_bytes}
            <div class="mt-1 text-xs text-warning/80">
              {turn.truncated_bytes} bytes were trimmed by the server's storage cap.
            </div>
          {/if}

          {#if turn.tools && turn.tools.length > 0}
            <footer class="mt-1.5 flex flex-wrap items-center gap-1">
              <span class="text-xs text-base-content/30">tools:</span>
              {#each turn.tools as tool, i (tool.invocation_id ?? `${turn.sequence}-${i}`)}
                <span class="badge badge-xs {tool.is_error ? 'badge-error' : 'badge-ghost'}">
                  {tool.name}
                </span>
              {/each}
            </footer>
          {/if}
        </article>
      {/each}
    </div>

    {#if sessionStore.conversationHasMore}
      <div class="flex justify-center py-3">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { void onLoadMore(); }}
          disabled={loadingMore}
          data-testid="conversation-load-more"
        >
          {#if loadingMore}
            <span class="loading loading-spinner loading-xs"></span>
          {/if}
          Load more turns
        </button>
      </div>
    {/if}
  {/if}
</div>
