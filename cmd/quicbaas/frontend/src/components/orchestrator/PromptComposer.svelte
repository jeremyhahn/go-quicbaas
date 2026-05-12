<!--
  PromptComposer is the bottom input bar for the session detail view.
  Ctrl+Enter submits. Disables while a response is in-flight.
  Shows a subtle typing indicator when the assistant is streaming.
-->
<script lang="ts">
  import { sessionStore } from '../../stores/sessionStore.svelte.ts';
  import { getSDKClient } from '../../stores/sdkClient.ts';
  import type { ParsedSessionEvent } from '../../stores/sessionStore.svelte.ts';

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

  // Detect streaming: any recent assistant_chunk without a stop_reason.
  const isStreaming = $derived((): boolean => {
    const evs = sessionStore.events;
    if (evs.length === 0) return false;
    for (let i = evs.length - 1; i >= Math.max(0, evs.length - 10); i--) {
      const ev: ParsedSessionEvent | undefined = evs[i];
      if (ev === undefined) continue;
      if (ev.kind === 'assistant_chunk') {
        return ev.stopReason === undefined || ev.stopReason === '';
      }
      if (ev.kind === 'prompt') break; // Stop looking at the previous turn.
    }
    return false;
  });

  const disabled = $derived(inFlight || isStreaming());

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  async function submit(): Promise<void> {
    const text = promptText.trim();
    if (text === '' || disabled) return;

    inFlight = true;
    errorMessage = '';
    promptText = '';

    try {
      const client = await getSDKClient();
      await client.orchestrator.promptSession({
        session_id: sessionId,
        prompt: text,
      });
    } catch (err: unknown) {
      errorMessage = err instanceof Error ? err.message : String(err);
      // Restore the text so the user doesn't lose it.
      promptText = text;
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

  {#if isStreaming()}
    <div class="flex items-center gap-1.5 text-xs text-base-content/40" aria-live="polite">
      <span class="loading loading-dots loading-xs"></span>
      <span>Assistant is typing…</span>
    </div>
  {/if}

  <div class="flex gap-2 items-end">
    <textarea
      class="textarea textarea-bordered flex-1 resize-none text-sm min-h-[60px] max-h-36"
      placeholder="Send a prompt… (Ctrl+Enter to submit)"
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
