<!--
  AssistantEvent renders a coalesced assistant response as a left-aligned
  chat bubble. Multiple consecutive chunks with the same source are merged
  into one rendering block by the parent (EventStream); this component
  receives the already-merged text.
-->
<script lang="ts">
  let {
    text,
    source = '',
    isStreaming = false,
  }: {
    text: string;
    source?: string;
    isStreaming?: boolean;
  } = $props();
</script>

<div class="chat chat-start">
  {#if source}
    <div class="chat-header text-xs text-base-content/40 mb-0.5">{source}</div>
  {/if}
  <div class="chat-bubble bg-base-300 text-base-content text-sm whitespace-pre-wrap break-words max-w-[75%]">
    {text}
    {#if isStreaming}
      <span class="inline-block w-2 h-4 bg-base-content/50 animate-pulse ml-0.5 align-middle"></span>
    {/if}
  </div>
</div>
