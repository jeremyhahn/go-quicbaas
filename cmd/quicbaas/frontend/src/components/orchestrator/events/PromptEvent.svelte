<!--
  PromptEvent renders the user's prompt as a right-aligned chat bubble.
-->
<script lang="ts">
  let { prompt }: { prompt: string } = $props();
</script>

<div class="chat chat-end">
  <div class="chat-bubble chat-bubble-primary text-sm whitespace-pre-wrap break-words max-w-[75%]">
    {prompt}
  </div>
</div>
