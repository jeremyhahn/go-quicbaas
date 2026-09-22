<!--
  SessionDetail shows session metadata, the live event stream, and the
  prompt composer for a single session.

  Layout:
  - Header: session ID + ROLE badge (worker / reviewer / triage, from the
    runner of the session's job) + backend chip and model + status badge +
    Rotate-account + Close-confirm + Prompt-modal
  - Right-side metadata panel: tenant, model, container_id, job, created_at,
    last_active
    (collapsible on narrow viewports via a toggle button)
  - Body: Conversation | Event Log tabs
      Conversation — SessionConversation, the transcript folded into turns,
      and what an operator opened the page to read. Selected by default.
      Event Log    — EventStream (virtualized, scroll-managed), the raw log
      every fragment of that transcript is made of.
  - "Send Prompt" inline composer pinned to bottom (disabled when closed)
-->
<script lang="ts">
  import { untrack } from 'svelte';
  import type { SessionStatusResponse, RotateSessionAccountResponse } from '@quicbaas/sdk';
  import { sessionStore, openSession, closeActiveSession, requestCloseSession } from '../../stores/sessionStore.svelte.ts';
  import { routeJobDetail } from '../../stores/router.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import EventStream from './EventStream.svelte';
  import SessionConversation from './SessionConversation.svelte';
  import PromptComposer from './PromptComposer.svelte';
  import PresenceIndicator from './PresenceIndicator.svelte';
  import SessionActivityIndicator from './SessionActivityIndicator.svelte';
  import StatusBadge from '../core/StatusBadge.svelte';
  import { formatDateTime } from '../../lib/time.ts';
  import { sessionModel, sessionRole } from '../../lib/sessions.ts';
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

  // Which view of the transcript the body shows. The conversation leads:
  // the raw event log was the page's only content for long enough that a
  // session with four turns in it read as empty (go-quicbaas#834).
  type BodyTab = 'conversation' | 'events';
  let bodyTab: BodyTab = $state('conversation');
  const BODY_TABS: { id: BodyTab; label: string }[] = [
    { id: 'conversation', label: 'Conversation' },
    { id: 'events', label: 'Event Log' },
  ];

  // Rotate-account dialog state. The dialog IS the confirmation gate — opening
  // it only reveals the form; the API call fires only when the operator
  // clicks the dialog's own confirm button.
  let rotateDialogOpen = $state(false);
  let rotating = $state(false);
  let rotateReason = $state('');
  let rotateHoldOutgoing = $state(false);
  // datetime-local input value (local time, no timezone) for available_at.
  let rotateAvailableAt = $state('');
  let rotateError = $state('');
  let rotateResult: RotateSessionAccountResponse | null = $state(null);

  // Optimistic close: a successful Close should reflect in the UI immediately
  // rather than waiting for the server's session_closed event to echo back over
  // the live tail (same fragility the prompt echo had). displayStatus overlays
  // the local close on the server-provided status; it resets per session id.
  let closedLocally = $state(false);
  const displayStatus = $derived(closedLocally ? 'closed' : session.status);

  // ---------------------------------------------------------------------------
  // The role this session played
  // ---------------------------------------------------------------------------
  //
  // A WORKER SESSION READS LIKE A REVIEWER'S. Its transcript discusses "the
  // review's findings" because the worker was handed them, and with no role on
  // the page the reader concludes the model in front of them performed the
  // review -- which is how the owner came to believe the wrong model was
  // reviewing code (go-quicbaas#969).
  //
  // THE RECORD SAYS WHICH ROLE IT WAS. The listing resolves the runner of the
  // session's job server-side, per page, so this reads one field instead of
  // searching a job list that is now itself a page -- which would have left
  // the badge blank for every session whose job fell off it.

  const role = $derived(sessionRole(session));
  const model = $derived(sessionModel(session));

  // ---------------------------------------------------------------------------
  // Lifecycle
  // ---------------------------------------------------------------------------

  // Switching sessions returns to the conversation. The tab is a reading
  // preference for the session in front of you, not a sticky mode that
  // follows you into the next one.
  $effect(() => {
    void session.session_id;
    bodyTab = 'conversation';
  });

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
      await requestCloseSession(session.session_id);
    } catch (err: unknown) {
      closedLocally = false;
      closeError = err instanceof Error ? err.message : String(err);
    } finally {
      isClosing = false;
    }
  }

  // openRotateDialog resets the form to a blank slate every time it opens so
  // a previous rotation's reason/result never bleeds into the next attempt.
  function openRotateDialog(): void {
    rotateReason = '';
    rotateHoldOutgoing = false;
    rotateAvailableAt = '';
    rotateError = '';
    rotateResult = null;
    rotateDialogOpen = true;
  }

  async function confirmRotateAccount(): Promise<void> {
    if (rotating) return;
    rotating = true;
    rotateError = '';
    try {
      const client = await getSDKClient();
      const availableAtISO =
        rotateHoldOutgoing && rotateAvailableAt ? new Date(rotateAvailableAt).toISOString() : undefined;
      rotateResult = await client.orchestrator.rotateSessionAccount({
        session_id: session.session_id,
        reason: rotateReason.trim() || undefined,
        hold_outgoing: rotateHoldOutgoing || undefined,
        available_at: availableAtISO,
      });
    } catch (err: unknown) {
      rotateError = parseRotateError(err);
    } finally {
      rotating = false;
    }
  }

  // parseRotateError mirrors SpawnDialog's error parsing: a structured
  // envelope ({error, hint, request_id}), a JSON-string error body (e.g. the
  // 409 SessionNotRotatable response), or a plain Error.
  function parseRotateError(err: unknown): string {
    if (err && typeof err === 'object' && 'envelope' in err) {
      const env = (err as { envelope?: { error?: string } }).envelope;
      if (env?.error) return env.error;
    }
    const raw = err instanceof Error ? err.message : String(err);
    try {
      const parsed = JSON.parse(raw) as Record<string, unknown>;
      if (typeof parsed.error === 'string') return parsed.error;
      if (typeof parsed.message === 'string') return parsed.message;
    } catch {
      // not JSON — fall through
    }
    return raw;
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

<!-- Rotate account dialog -->
{#if rotateDialogOpen}
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <dialog
    class="modal modal-open"
    aria-modal="true"
    aria-label="Rotate session account"
    onkeydown={(e) => { if (e.key === 'Escape' && !rotating) { rotateDialogOpen = false; } }}
  >
    <div class="modal-box max-w-lg">
      <h3 class="font-bold text-base mb-1">Rotate Account</h3>
      <p class="text-xs text-base-content/40 font-mono mb-3">{session.session_id}</p>

      {#if rotateResult}
        <div class="space-y-2 text-sm" data-testid="rotate-result">
          <p class="text-base-content/70">Rotation succeeded.</p>
          <div class="flex items-center gap-2 font-mono text-xs">
            <span class="badge badge-ghost">{rotateResult.previous_account_id || '(none)'}</span>
            <span>&rarr;</span>
            <span class="badge badge-primary">{rotateResult.account_id}</span>
          </div>
          <p>
            Mode: <span class="font-mono">{rotateResult.mode}</span>
            {#if rotateResult.mode === 'resume'}
              <span class="text-base-content/50"> — the agent process was restarted; the transcript was replayed.</span>
            {:else}
              <span class="text-base-content/50"> — the running process kept its environment swapped in place.</span>
            {/if}
          </p>
        </div>
        <div class="modal-action">
          <button class="btn btn-sm btn-primary" onclick={() => { rotateDialogOpen = false; }}>
            Done
          </button>
        </div>
      {:else}
        <p class="text-xs text-base-content/60 mb-3">
          Moves this session onto a different pooled provider account. If the
          agent runs as a persistent or sandboxed process, rotation
          <strong>restarts</strong> it (<span class="font-mono">mode: resume</span>) —
          the transcript is preserved via replay, but the process itself does
          not survive in place. Only sessions spawned with automatic account
          selection or an allow-list can rotate.
        </p>

        <label class="form-control">
          <div class="label"><span class="label-text">Reason (optional)</span></div>
          <input
            type="text"
            class="input input-bordered input-sm"
            bind:value={rotateReason}
            placeholder="e.g. rebalancing load"
            disabled={rotating}
            aria-label="Rotation reason"
          />
        </label>

        <label class="form-control mt-2">
          <div class="flex items-center gap-2 py-1">
            <input
              type="checkbox"
              class="checkbox checkbox-sm"
              bind:checked={rotateHoldOutgoing}
              disabled={rotating}
              aria-label="Hold outgoing account"
            />
            <span class="label-text">Hold the outgoing account (mark it limited)</span>
          </div>
        </label>

        {#if rotateHoldOutgoing}
          <label class="form-control">
            <div class="label"><span class="label-text-alt">Available again at (optional)</span></div>
            <input
              type="datetime-local"
              class="input input-bordered input-sm"
              bind:value={rotateAvailableAt}
              disabled={rotating}
              aria-label="Outgoing account available at"
            />
          </label>
        {/if}

        {#if rotateError}
          <div class="alert alert-error py-2 px-3 text-sm mt-2" role="alert">{rotateError}</div>
        {/if}

        <div class="modal-action">
          <button
            class="btn btn-sm btn-ghost"
            onclick={() => { rotateDialogOpen = false; }}
            disabled={rotating}
          >
            Cancel
          </button>
          <button
            class="btn btn-sm btn-warning"
            onclick={() => { void confirmRotateAccount(); }}
            disabled={rotating}
            aria-label="Confirm rotate account"
          >
            {#if rotating}
              <span class="loading loading-spinner loading-xs"></span>
            {:else}
              Rotate Account
            {/if}
          </button>
        </div>
      {/if}
    </div>
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-backdrop" onclick={() => { if (!rotating) rotateDialogOpen = false; }} onkeydown={() => {}}></div>
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
        {#if role !== ''}
          <span
            class="badge badge-sm badge-outline"
            data-testid="session-role"
            title="The runner of this session's job decides its role"
          >{role}</span>
        {/if}
        <span class="badge badge-sm badge-primary">{session.backend}</span>
        {#if model !== ''}
          <span class="font-mono text-xs text-base-content/50" data-testid="session-header-model">{model}</span>
        {/if}
      </div>
    </div>

    <div class="flex items-center gap-2 flex-wrap">
      {#if displayStatus !== 'closed'}
        <SessionActivityIndicator />
      {/if}
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
          class="btn btn-xs btn-warning"
          onclick={() => { openRotateDialog(); }}
          aria-label="Rotate session account"
        >
          Rotate Account
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

  <!-- Body: transcript (conversation | event log) + metadata panel -->
  <div class="flex flex-1 overflow-hidden">

    <!-- Transcript (flex-1) -->
    <div class="flex min-w-0 flex-1 flex-col overflow-hidden">
      <div role="tablist" class="tabs tabs-bordered shrink-0 px-2">
        {#each BODY_TABS as tab (tab.id)}
          <button
            role="tab"
            class="tab tab-sm"
            class:tab-active={bodyTab === tab.id}
            aria-selected={bodyTab === tab.id}
            onclick={() => { bodyTab = tab.id; }}
          >
            {tab.label}
            {#if tab.id === 'events' && sessionStore.events.length > 0}
              <span class="badge badge-xs badge-ghost ml-1.5">{sessionStore.events.length}</span>
            {/if}
          </button>
        {/each}
      </div>

      <div class="relative flex-1 overflow-hidden">
        {#if bodyTab === 'conversation'}
          <SessionConversation sessionClosed={displayStatus === 'closed'} />
        {:else}
          <EventStream />
        {/if}
      </div>
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
          {#if model !== ''}
            <div>
              <div class="text-base-content/40">Model</div>
              <div class="font-mono">{model}</div>
            </div>
          {/if}
          {#if session.container_id}
            <div>
              <div class="text-base-content/40">Container</div>
              <div class="font-mono break-all">{session.container_id.slice(0, 12)}</div>
            </div>
          {/if}
          {#if session.job_id}
            <div>
              <div class="text-base-content/40">Job</div>
              <a
                class="link link-primary font-mono break-all"
                href="#/jobs/{session.job_id}"
                onclick={(e) => { e.preventDefault(); routeJobDetail(session.job_id ?? ''); }}
                title="Open job {session.job_id}"
              >{session.job_id}</a>
            </div>
          {/if}
          <div>
            <div class="text-base-content/40">Created</div>
            <div data-testid="session-created">{formatTime(session.created_at)}</div>
          </div>
          <div>
            <div class="text-base-content/40">Last Active</div>
            <div data-testid="session-last-active">{formatTime(session.last_active)}</div>
          </div>
          <div>
            <div class="text-base-content/40">Turns</div>
            <div>{sessionStore.conversation.length} loaded</div>
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
          {#if model !== ''}
            <span>Model: <span class="font-mono">{model}</span></span>
          {/if}
          {#if session.job_id}
            <span>Job: <a class="link link-primary font-mono" href="#/jobs/{session.job_id}">{session.job_id}</a></span>
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
