<!--
  ThoughtEvent renders a coalesced agent_thought fragment — the backend's
  streamed reasoning text, distinct from its final assistant_chunk answer.
  Collapsed by default behind a muted, italic "Thinking…" summary row
  (mirroring Claude Code's thinking blocks); expand to read the full
  reasoning text. Multiple consecutive thought fragments with the same
  source are merged into one rendering block by the parent (EventStream),
  the same way assistant_chunk is; this component receives the
  already-merged text.
-->
<script lang="ts">
  import { Brain } from 'lucide-svelte';

  let {
    text,
    source = '',
  }: {
    text: string;
    source?: string;
  } = $props();
</script>

<div class="my-1">
  <details class="group">
    <summary
      class="flex items-center gap-1.5 cursor-pointer list-none text-xs italic text-base-content/50 hover:text-base-content/70 transition-colors py-0.5"
      aria-label="Toggle agent thinking"
    >
      <Brain class="h-3 w-3 shrink-0" aria-hidden="true" />
      <span>Thinking{source ? ` (${source})` : ''}&hellip;</span>
      <span
        class="ml-auto text-base-content/30 group-open:rotate-90 transition-transform text-xs"
        aria-hidden="true"
      >&#9654;</span>
    </summary>
    <p
      class="mt-1 ml-4 pl-2 border-l-2 border-base-300/60 text-xs italic text-base-content/50 whitespace-pre-wrap break-words"
      role="region"
      aria-label="Agent thought"
    >{text}</p>
  </details>
</div>
