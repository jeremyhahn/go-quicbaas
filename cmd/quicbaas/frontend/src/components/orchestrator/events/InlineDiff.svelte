<!--
  InlineDiff renders a minimal unified diff. Lines starting with '+' are
  shown in green, '-' in red, '@@' in muted blue, everything else neutral.
  No monaco, no shiki — pure CSS rendering of the diff text.
-->
<script lang="ts">
  let { diff }: { diff: string } = $props();

  interface DiffLine {
    kind: 'add' | 'remove' | 'hunk' | 'context';
    text: string;
  }

  const lines = $derived((): DiffLine[] => {
    return diff.split('\n').map((line): DiffLine => {
      if (line.startsWith('+')) return { kind: 'add', text: line };
      if (line.startsWith('-')) return { kind: 'remove', text: line };
      if (line.startsWith('@@')) return { kind: 'hunk', text: line };
      return { kind: 'context', text: line };
    });
  });

  const lineClass: Record<DiffLine['kind'], string> = {
    add: 'bg-success/10 text-success',
    remove: 'bg-error/10 text-error',
    hunk: 'text-info/70 italic',
    context: 'text-base-content/60',
  };
</script>

<div
  class="font-mono text-xs leading-5 overflow-x-auto bg-base-300/50 rounded p-2 max-h-56"
  role="region"
  aria-label="File diff"
>
  {#each lines() as line, i (i)}
    <div class="whitespace-pre {lineClass[line.kind]}">{line.text}</div>
  {/each}
</div>
