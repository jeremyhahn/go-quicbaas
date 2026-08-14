<!--
  TurnLifecycleEvent renders turn_started / turn_completed / turn_failed —
  the async-turn markers the orchestrator appends around every dispatched
  turn (see pkg/orchestration/session_events.go). turn_started and
  turn_completed are intentionally unobtrusive: a small centered badge, the
  same visual weight as the operator-presence and session-resumed markers,
  so the transcript stays readable during a long-running agent turn.
  turn_failed is the exception — it carries the agent's own failure reason
  and must be visible, so it renders as a full-width error alert like
  SystemMessageEvent.
-->
<script lang="ts">
  let {
    kind,
    backend,
    model,
    stopReason,
    durationMs,
    error,
  }: {
    kind: 'turn_started' | 'turn_completed' | 'turn_failed';
    backend?: string;
    model?: string;
    stopReason?: string;
    durationMs?: number;
    error?: string;
  } = $props();

  /** Formats a millisecond duration as a short human-readable badge, e.g. "420ms" or "4.2s". */
  function formatDuration(ms: number | undefined): string {
    if (ms === undefined || ms <= 0) return '';
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  }

  const duration = $derived(formatDuration(durationMs));
</script>

{#if kind === 'turn_failed'}
  <div
    class="alert alert-error py-1.5 px-3 text-xs flex items-start gap-2 my-0.5"
    role="alert"
    aria-label="Turn failed"
  >
    <span class="badge badge-xs badge-outline">turn failed</span>
    <span class="whitespace-pre-wrap break-words flex-1">{error || 'unknown error'}</span>
    {#if duration}
      <span class="text-base-content/50 shrink-0">{duration}</span>
    {/if}
  </div>
{:else if kind === 'turn_started'}
  <div class="flex justify-center my-0.5" aria-hidden="true" data-testid="turn-started-marker">
    <span class="badge badge-xs badge-ghost gap-1">
      <span aria-hidden="true">&#x25B8;</span>
      {model || backend || 'turn started'}
    </span>
  </div>
{:else}
  <div class="flex justify-center my-0.5" aria-hidden="true" data-testid="turn-completed-marker">
    <span class="badge badge-xs badge-ghost gap-1">
      <span aria-hidden="true">&#x2713;</span>
      {duration || stopReason || 'done'}
    </span>
  </div>
{/if}
