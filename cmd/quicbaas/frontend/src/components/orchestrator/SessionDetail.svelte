<!--
  SessionDetail shows session metadata, the live event stream, and the
  prompt composer for a single session.

  Layout:
  - Header: session ID + backend chip + status badge + Close-confirm + Prompt-modal
  - Right-side metadata panel: tenant, model, container_id, created_at, last_active
    (collapsible on narrow viewports via a toggle button)
  - Event timeline: EventStream (virtualized, scroll-managed)
  - "Send Prompt" inline composer pinned to bottom (disabled when closed)
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import type { SessionStatusResponse } from '@quicbaas/sdk';
  import { sessionStore, openSession, closeActiveSession } from '../../stores/sessionStore.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import EventStream from './EventStream.svelte';
  import PromptComposer from './PromptComposer.svelte';
  import PresenceIndicator from './PresenceIndicator.svelte';
  import StatusBadge from '../core/StatusBadge.svelte';
  import { formatDateTime } from '../../lib/time.ts';
  import { X, ChevronRight, ChevronDown } from 'lucide-svelte';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let { session }: { session: SessionStatusResponse } = $props();

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let isClosing = $state(false);
  let closeError = $state('');
  let closeConfirmOpen = $state(false);
  let promptModalOpen = $state(false);
  let promptText = $state('');
  let prompting = $state(false);
  let metadataOpen = $state(true); // collapsible on narrow viewports

  // Optimistic close: a successful Close should reflect in the UI immediately
  // rather than waiting for the server's session_closed event to echo back over
  // the live tail (same fragility the prompt echo had). displayStatus overlays
  // the local close on the server-provided status; it resets per session id.
  let closedLocally = $state(false);
  const displayStatus = $derived(closedLocally ? 'closed' : session.status);

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  // Run-once-per-session-id effect. The naive shape `$effect(() => openSession(session))`
  // re-fires whenever any reactive read inside `openSession` changes — including
  // sessionStore.events, which openSession itself mutates — producing an infinite
  // re-subscribe loop that blows the Svelte effect-update-depth limit and tears
  // down the global reactivity graph (manifests as sticky session-detail view).
  // We track only `session.session_id` and call openSession through `untrack`
  // so its body's reads don't register as dependencies.
  $effect(() => {
    const id = session.session_id; // sole tracked dependency
    closedLocally = false; // reset optimistic close when switching sessions
    untrack(() => { void openSession(session); });
    void id;
    return () => {
      untrack(() => { closeActiveSession(); });
    };
  });

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  async function closeSession(): Promise<void> {
    if (isClosing) return;
    isClosing = true;
    closeError = '';
    closeConfirmOpen = false;
    // Reflect the close optimistically — the backend teardown (agent subprocess
    // shutdown + ACP close handshake) can take seconds, but the UI should show
    // the closed state the instant the operator confirms. Roll back if the
    // request fails so a transient error doesn't leave a live session looking
    // closed.
    closedLocally = true;
    try {
      const client = await getSDKClient();
      await client.orchestrator.closeSession({ session_id: session.session_id });
    } catch (err: unknown) {
      closedLocally = false;
      closeError = err instanceof Error ? err.message : String(err);
    } finally {
      isClosing = false;
    }
  }

  async function sendPrompt(): Promise<void> {
    if (!promptText.trim() || prompting) return;
    prompting = true;
    try {
      const client = await getSDKClient();
      await client.orchestrator.promptSession({
        session_id: session.session_id,
        prompt: promptText.trim(),
      });
      promptText = '';
      promptModalOpen = false;
    } catch {
      // Silently ignore — PromptComposer handles its own error display.
    } finally {
      prompting = false;
    }
  }

  const formatTime = (ts: number): string => formatDateTime(ts);

  function statusVariant(s: string): 'success' | 'error' | 'warning' | 'info' | 'ghost' {
    if (s === 'active' || s === 'running') return 'success';
    if (s === 'error') return 'error';
    if (s === 'closed') return 'ghost';
    return 'info';
  }
</script>

<!-- Close confirm dialog -->
{#if closeConfirmOpen}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Close session confirmation"
    onkeydown={(e) => { if (e.key === 'Escape') { closeConfirmOpen = false; } }}
  >
    <div class="modal-box max-w-sm">
      <h3 class="font-bold text-base">Close session?</h3>
      <p class="text-sm text-base-content/60 mt-2">
        Session <span class="font-mono">{session.session_id.slice(0, 16)}…</span> will be terminated.
        This cannot be undone.
      </p>
      <div class="modal-action">
        <button
          class="btn btn-sm btn-ghost"
          onclick={() => { closeConfirmOpen = false; }}
          disabled={isClosing}
        >
          Keep Open
        </button>
        <button
          class="btn btn-sm btn-error"
          onclick={() => { void closeSession(); }}
          disabled={isClosing}
          aria-label="Confirm close session"
        >
          {#if isClosing}
            <span class="loading loading-spinner loading-xs"></span>
          {:else}
            Close Session
          {/if}
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { closeConfirmOpen = false; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<!-- Prompt modal -->
{#if promptModalOpen}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Send prompt to session"
    onkeydown={(e) => { if (e.key === 'Escape') { promptModalOpen = false; } }}
  >
    <div class="modal-box max-w-lg">
      <h3 class="font-bold text-base mb-1">Send Prompt</h3>
      <p class="text-xs text-base-content/40 font-mono mb-3">{session.session_id}</p>
      <textarea
        class="textarea textarea-bordered w-full h-32 text-sm"
        placeholder="Enter your prompt..."
        bind:value={promptText}
        disabled={prompting}
        onkeydown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { void sendPrompt(); } }}
      ></textarea>
      <div class="modal-action">
        <button class="btn btn-sm btn-ghost" onclick={() => { promptModalOpen = false; }} disabled={prompting}>
          Cancel
        </button>
        <button
          class="btn btn-sm btn-primary"
          onclick={() => { void sendPrompt(); }}
          disabled={prompting || !promptText.trim()}
        >
          {#if prompting}
            <span class="loading loading-spinner loading-xs"></span>
          {:else}
            Send
          {/if}
        </button>
      </div>
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { promptModalOpen = false; }} onkeydown={() => {}}></div>
  </dialog>
{/if}

<div class="flex flex-col h-full overflow-hidden">

  <!-- Session header -->
  <div class="flex items-start justify-between px-4 py-3 border-b border-base-300/50 bg-base-200/50 gap-3 flex-wrap shrink-0">
    <div class="flex flex-col gap-1">
      <div class="flex items-center gap-2 flex-wrap">
        <span class="font-mono text-sm font-semibold text-base-content">
          {session.session_id.slice(0, 16)}…
        </span>
        <StatusBadge
          text={displayStatus}
          variant={statusVariant(displayStatus)}
          pulse={displayStatus === 'active'}
        />
        <span class="badge badge-sm badge-primary">{session.backend}</span>
      </div>
    </div>

    <div class="flex items-center gap-2 flex-wrap">
      <PresenceIndicator />

      <!-- Metadata panel toggle (mobile) -->
      <button
        class="btn btn-xs btn-ghost sm:hidden"
        onclick={() => { metadataOpen = !metadataOpen; }}
        aria-label="Toggle metadata panel"
        aria-expanded={metadataOpen}
      >
        {#if metadataOpen}
          <ChevronDown class="h-3 w-3" />
        {:else}
          <ChevronRight class="h-3 w-3" />
        {/if}
        Info
      </button>

      {#if displayStatus !== 'closed'}
        <button
          class="btn btn-xs btn-primary"
          onclick={() => { promptModalOpen = true; }}
          aria-label="Send prompt to session"
        >
          Prompt
        </button>
        <button
          class="btn btn-xs btn-error gap-1"
          onclick={() => { closeConfirmOpen = true; }}
          disabled={isClosing}
          aria-label="Close session"
        >
          {#if isClosing}
            <span class="loading loading-spinner loading-xs"></span>
          {:else}
            <X class="h-3 w-3" />
          {/if}
          Close
        </button>
      {/if}
    </div>
  </div>

  <!-- Error banner -->
  {#if closeError}
    <div class="alert alert-error py-1.5 px-4 text-xs rounded-none" role="alert">
      {closeError}
    </div>
  {/if}

  <!-- Body: event stream + metadata panel -->
  <div class="flex flex-1 overflow-hidden">

    <!-- Event stream (flex-1) -->
    <div class="relative flex-1 overflow-hidden">
      <EventStream />
    </div>

    <!-- Right-side metadata panel (collapsible on narrow) -->
    {#if metadataOpen}
      <aside class="w-56 shrink-0 border-l border-base-300/50 bg-base-200/30 p-3 overflow-y-auto hidden sm:block">
        <div class="text-[10px] font-semibold uppercase tracking-widest text-base-content/30 mb-3">
          Session Info
        </div>
        <div class="space-y-3 text-xs">
          {#if session.tenant_id}
            <div>
              <div class="text-base-content/40">Tenant</div>
              <div class="font-mono break-all">{session.tenant_id}</div>
            </div>
          {/if}
          {#if session.model}
            <div>
              <div class="text-base-content/40">Model</div>
              <div class="font-mono">{session.model}</div>
            </div>
          {/if}
          {#if session.container_id}
            <div>
              <div class="text-base-content/40">Container</div>
              <div class="font-mono break-all">{session.container_id.slice(0, 12)}</div>
            </div>
          {/if}
          <div>
            <div class="text-base-content/40">Created</div>
            <div>{formatTime(session.created_at)}</div>
          </div>
          <div>
            <div class="text-base-content/40">Last Active</div>
            <div>{formatTime(session.last_active)}</div>
          </div>
          <div>
            <div class="text-base-content/40">Events</div>
            <div>{sessionStore.events.length} loaded</div>
          </div>
        </div>
      </aside>
    {/if}

    <!-- Mobile metadata panel (shown below header when toggled) -->
    {#if metadataOpen}
      <div class="sm:hidden w-full shrink-0 border-t border-base-300/50 bg-base-200/30 px-4 py-2">
        <div class="flex flex-wrap gap-4 text-xs">
          {#if session.tenant_id}
            <span>Tenant: <span class="font-mono">{session.tenant_id}</span></span>
          {/if}
          {#if session.model}
            <span>Model: <span class="font-mono">{session.model}</span></span>
          {/if}
          <span>Created: {formatTime(session.created_at)}</span>
          <span>Last active: {formatTime(session.last_active)}</span>
        </div>
      </div>
    {/if}

  </div>

  <!-- Prompt composer (pinned to bottom) -->
  {#if displayStatus !== 'closed'}
    <PromptComposer sessionId={session.session_id} />
  {:else}
    <div class="p-3 text-center text-sm text-base-content/30 border-t border-base-300/50">
      This session is closed. Spawn a new session to continue.
    </div>
  {/if}

</div>
