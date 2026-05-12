<!--
  ToolInvocationEvent renders a collapsible tool call summary.
  Summary line: tool-name("arg1, arg2"); click to expand full args JSON.
-->
<script lang="ts">
  let {
    toolName,
    toolArgs = {},
    invocationId = '',
  }: {
    toolName: string;
    toolArgs?: Record<string, unknown>;
    invocationId?: string;
  } = $props();

  // Build a compact one-line preview of the args.
  const argPreview = $derived((): string => {
    const values = Object.values(toolArgs);
    if (values.length === 0) return '';
    const preview = values
      .slice(0, 2)
      .map((v) => JSON.stringify(v))
      .join(', ');
    return values.length > 2 ? `${preview}, ...` : preview;
  });

  const argsJSON = $derived(JSON.stringify(toolArgs, null, 2));
</script>

<div class="my-1" data-invocation-id={invocationId}>
  <details class="group">
    <summary
      class="flex items-center gap-1.5 cursor-pointer list-none text-sm text-base-content/70 hover:text-base-content transition-colors py-0.5"
      aria-label="Toggle tool invocation details"
    >
      <span class="text-accent font-mono text-xs" aria-hidden="true">&#x1F527;</span>
      <span class="font-mono font-medium text-accent">{toolName}</span>
      <span class="text-base-content/40 font-mono text-xs truncate max-w-xs">
        ({argPreview()})
      </span>
      <span
        class="ml-auto text-base-content/30 group-open:rotate-90 transition-transform text-xs"
        aria-hidden="true"
      >&#9654;</span>
    </summary>
    <pre
      class="mt-1 ml-4 p-2 text-xs font-mono bg-base-300 rounded overflow-x-auto max-h-48 text-base-content/80"
      role="region"
      aria-label="Tool arguments JSON"
    >{argsJSON}</pre>
  </details>
</div>
