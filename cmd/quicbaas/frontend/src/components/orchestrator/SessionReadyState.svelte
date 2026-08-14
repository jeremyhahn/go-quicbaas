<!--
  SessionReadyState is shown in place of a blank scrollback when a session
  has no transcript events yet — i.e. a brand-new chat. It must never be
  shown while history is still being fetched (EventStream gates on
  sessionStore.historyLoading before rendering this so "we haven't looked
  yet" is never mistaken for "we looked, and it's genuinely empty").
-->
<script lang="ts">
  import { MessageSquare } from 'lucide-svelte';

  let { backend, model }: { backend: string; model?: string } = $props();
</script>

<div
  class="flex flex-1 flex-col items-center justify-center gap-2 text-center px-6 py-8"
  data-testid="session-ready-state"
>
  <MessageSquare class="h-8 w-8 text-base-content/20" aria-hidden="true" />

  <div class="flex items-center gap-1.5 text-xs">
    {#if backend}
      <span class="badge badge-sm badge-primary">{backend}</span>
    {/if}
    {#if model}
      <span class="font-mono text-base-content/40">{model}</span>
    {/if}
  </div>

  <div class="text-sm font-medium text-base-content/70">New chat — nothing here yet</div>

  <p class="text-xs text-base-content/40 max-w-sm">
    The agent is ready and waiting for your first prompt.
  </p>

  <p class="text-xs text-base-content/25">
    Type <span class="font-mono">/help</span> to see available commands.
  </p>
</div>
