<!--
  SessionClosedEvent renders a prominent "session ended" banner.
-->
<script lang="ts">
  let { reason = '' }: { reason?: string } = $props();
</script>

<div
  class="alert alert-error my-2 flex items-center gap-3"
  role="status"
  aria-label="Session closed"
>
  <span class="text-lg" aria-hidden="true">&#x1F534;</span>
  <div>
    <div class="font-semibold text-sm">Session ended</div>
    {#if reason}
      <div class="text-xs opacity-75">{reason}</div>
    {/if}
  </div>
</div>
