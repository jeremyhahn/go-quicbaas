<!--
  EventCard dispatches a ParsedSessionEvent to the appropriate sub-component.
  Consecutive assistant chunks with the same source are coalesced before being
  passed here by the parent (EventStream).
-->
<script lang="ts">
  import type { ParsedSessionEvent } from '../../stores/sessionStore.svelte.ts';
  import PromptEvent from './events/PromptEvent.svelte';
  import AssistantEvent from './events/AssistantEvent.svelte';
  import ToolInvocationEvent from './events/ToolInvocationEvent.svelte';
  import ToolResultEvent from './events/ToolResultEvent.svelte';
  import FileOpEvent from './events/FileOpEvent.svelte';
  import SystemMessageEvent from './events/SystemMessageEvent.svelte';
  import OperatorPresenceEvent from './events/OperatorPresenceEvent.svelte';
  import SessionClosedEvent from './events/SessionClosedEvent.svelte';

  let {
    event,
    isStreaming = false,
  }: {
    event: ParsedSessionEvent;
    isStreaming?: boolean;
  } = $props();
</script>

<div data-seq={event.sequence} data-kind={event.kind}>
  {#if event.kind === 'prompt'}
    <PromptEvent prompt={event.prompt ?? ''} />

  {:else if event.kind === 'assistant_chunk'}
    <AssistantEvent
      text={event.assistantChunk ?? ''}
      source={event.source ?? ''}
      {isStreaming}
    />

  {:else if event.kind === 'tool_invocation'}
    <ToolInvocationEvent
      toolName={event.toolName ?? ''}
      toolArgs={event.toolArgs ?? {}}
      invocationId={event.invocationId ?? ''}
    />

  {:else if event.kind === 'tool_result'}
    <ToolResultEvent
      result={event.toolResult ?? ''}
      invocationId={event.toolResultInvocationId ?? ''}
    />

  {:else if event.kind === 'file_op'}
    <FileOpEvent
      op={event.fileOp ?? ''}
      path={event.filePath ?? ''}
      diff={event.fileDiff ?? ''}
    />

  {:else if event.kind === 'system'}
    <SystemMessageEvent
      message={event.systemMessage ?? ''}
      level={event.systemLevel ?? 'info'}
    />

  {:else if event.kind === 'operator_joined' || event.kind === 'operator_left'}
    <OperatorPresenceEvent
      kind={event.kind}
      operatorId={event.operatorId ?? ''}
    />

  {:else if event.kind === 'closed'}
    <SessionClosedEvent reason={event.closeReason ?? ''} />

  {:else if event.kind === 'resumed'}
    <div class="flex justify-center my-1">
      <span class="badge badge-xs badge-info gap-1">
        <span aria-hidden="true">&#x21BA;</span>
        session resumed
      </span>
    </div>

  {:else}
    <!-- Unknown event kind: render raw JSON for debugging. -->
    <details class="my-0.5">
      <summary class="text-xs text-base-content/30 cursor-pointer list-none">
        Unknown event: {event.kind}
      </summary>
      <pre class="text-xs font-mono bg-base-300 rounded p-2 mt-1 overflow-x-auto">{JSON.stringify(event.raw, null, 2)}</pre>
    </details>
  {/if}
</div>
