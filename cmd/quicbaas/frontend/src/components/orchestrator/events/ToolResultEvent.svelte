<!--
  ToolResultEvent renders the result of a tool call as a reply-indented card
  under its matching invocation. Long content is truncated at 512 chars with
  an expand toggle (no extra fetch — data is already in memory).
-->
<script lang="ts">
  const TRUNCATE_AT = 512;

  let {
    result,
    invocationId = '',
  }: {
    result: string;
    invocationId?: string;
  } = $props();

  let expanded = $state(false);

  const needsTruncation = $derived(result.length > TRUNCATE_AT);
  const displayText = $derived(
    needsTruncation && !expanded ? result.slice(0, TRUNCATE_AT) : result,
  );
</script>

<div class="ml-4 my-0.5 border-l-2 border-base-300/60 pl-3" data-result-for={invocationId}>
  <div
    class="bg-base-200/50 rounded p-2 text-xs font-mono text-base-content/70 whitespace-pre-wrap break-words max-h-40 overflow-y-auto"
    role="region"
    aria-label="Tool result"
  >
    {displayText}
    {#if needsTruncation && !expanded}
      <span class="text-base-content/30">…</span>
    {/if}
  </div>
  {#if needsTruncation}
    <button
      class="text-xs text-primary/60 hover:text-primary mt-0.5 transition-colors"
      onclick={() => { expanded = !expanded; }}
      aria-expanded={expanded}
    >
      {expanded ? 'Show less' : `Show ${result.length - TRUNCATE_AT} more chars`}
    </button>
  {/if}
</div>
