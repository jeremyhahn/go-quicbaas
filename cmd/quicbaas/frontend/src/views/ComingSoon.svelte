<!--
  ComingSoon renders a placeholder for features not yet implemented.
-->
<script lang="ts">
  import { Construction } from 'lucide-svelte';

  let { title = 'Coming Soon' }: { title?: string } = $props();
</script>

<div class="flex items-center justify-center h-full min-h-64 p-8">
  <div class="flex flex-col items-center gap-4 text-center">
    <Construction class="h-16 w-16 text-base-content/15" strokeWidth={1} />
    <div>
      <h3 class="text-lg font-semibold text-base-content/50">{title}</h3>
      <p class="text-sm text-base-content/30 mt-1">This feature is under development</p>
    </div>
  </div>
</div>
