<!--
  PromptComposer is the bottom input bar for the session detail view.
  Ctrl+Enter submits. Disables while a response is in-flight.
  Shows a subtle typing indicator when the assistant is streaming.
-->
<script lang="ts">
  import {
    sessionStore,
    appendLocalPrompt,
    removeLocalPrompt,
    clearLocalTranscript,
    requestCloseSession,
  } from '../../stores/sessionStore.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import { dispatchSlashCommand, isSlashCommand } from '../../lib/slashCommands.ts';

  // ---------------------------------------------------------------------------
  // Props
  // ---------------------------------------------------------------------------

  let { sessionId }: { sessionId: string } = $props();

  // ---------------------------------------------------------------------------
  // State
  // ---------------------------------------------------------------------------

  let promptText = $state('');
  let inFlight = $state(false);
  let errorMessage = $state('');
  // Local-only feedback for a dispatched slash command (/help, /status, …).
  // Never appended to the transcript: it exists only on this client.
  let commandOutput = $state('');
  let commandIsError = $state(false);

  // Activity state is derived centrally in sessionStore (see TurnStatus doc
  // comment there) from real transcript signals — the prompt-in-flight and
  // assistant-streaming states are not tracked separately here anymore, so
  // this composer and the header's SessionActivityIndicator never disagree.
  const turnStatus = $derived(sessionStore.turnStatus);
  const disabled = $derived(inFlight || turnStatus === 'working' || turnStatus === 'pending');

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  async function submit(): Promise<void> {
    const text = promptText.trim();
    if (text === '' || disabled) return;

    // Slash commands never reach the agent: they are handled entirely on the
    // client (or via a targeted SDK call like closeSession) and their output
    // is rendered as a transient local message, not a transcript event.
    if (isSlashCommand(text)) {
      promptText = '';
      commandOutput = '';
      commandIsError = false;
      const active = sessionStore.activeSession;
      const result = await dispatchSlashCommand(text, {
        sessionId,
        session: {
          status: active?.status ?? '',
          backend: active?.backend ?? '',
          model: active?.model,
          tenantId: active?.tenant_id,
        },
        eventCount: sessionStore.events.length,
        closeSession: () => requestCloseSession(sessionId),
        clearTranscript: clearLocalTranscript,
      });
      commandOutput = result.output ?? '';
      commandIsError = result.isError === true;
      return;
    }

    inFlight = true;
    errorMessage = '';
    promptText = '';

    // Render the prompt immediately as a chat bubble instead of waiting for the
    // server's prompt_submitted event to round-trip back over the SSE tail. The
    // live tail reconciles this placeholder with the server echo (dedup by text)
    // so the bubble never double-renders.
    appendLocalPrompt(text);

    try {
      const client = await getSDKClient();
      await client.orchestrator.promptSession({
        session_id: sessionId,
        prompt: text,
      });
    } catch (err: unknown) {
      errorMessage = err instanceof Error ? err.message : String(err);
      // Restore the text so the user doesn't lose it, and roll back the
      // optimistic bubble unless the SSE echo has already confirmed it.
      promptText = text;
      removeLocalPrompt(text);
    } finally {
      inFlight = false;
    }
  }

  function handleKeydown(e: KeyboardEvent): void {
    if (e.key === 'Enter' && e.ctrlKey) {
      e.preventDefault();
      void submit();
    }
  }
</script>

<div class="border-t border-base-300/50 p-3 flex flex-col gap-2">
  {#if errorMessage}
    <div class="text-xs text-error" role="alert">{errorMessage}</div>
  {/if}

  {#if commandOutput}
    <div
      class="text-xs whitespace-pre-line rounded-md px-2 py-1.5 {commandIsError ? 'text-error bg-error/10' : 'text-base-content/60 bg-base-200/60'}"
      role={commandIsError ? 'alert' : 'status'}
      aria-live="polite"
    >{commandOutput}</div>
  {/if}

  {#if turnStatus === 'working'}
    <div class="flex items-center gap-1.5 text-xs text-base-content/40" aria-live="polite">
      <span class="loading loading-dots loading-xs"></span>
      <span>Assistant is typing…</span>
    </div>
  {:else if turnStatus === 'pending'}
    <div class="flex items-center gap-1.5 text-xs text-base-content/40" aria-live="polite">
      <span class="loading loading-spinner loading-xs"></span>
      <span>Waiting for the agent to respond…</span>
    </div>
  {:else if turnStatus === 'disconnected'}
    <div class="flex items-center gap-1.5 text-xs text-warning" aria-live="polite">
      <span>Live updates unavailable — the transcript may be out of date.</span>
    </div>
  {/if}

  <div class="flex gap-2 items-end">
    <textarea
      class="textarea textarea-bordered flex-1 resize-none text-sm min-h-[60px] max-h-36"
      placeholder="Send a prompt or /command… (Ctrl+Enter to submit, /help for commands)"
      bind:value={promptText}
      disabled={disabled}
      onkeydown={handleKeydown}
      aria-label="Prompt input"
      aria-disabled={disabled}
      rows="2"
    ></textarea>

    <button
      class="btn btn-primary btn-sm h-auto py-2"
      onclick={() => { void submit(); }}
      disabled={disabled || promptText.trim() === ''}
      aria-label="Send prompt"
    >
      {#if inFlight}
        <span class="loading loading-spinner loading-xs"></span>
      {:else}
        Send
      {/if}
    </button>
  </div>

  <div class="text-xs text-base-content/25 text-right">Ctrl+Enter to send</div>
</div>
