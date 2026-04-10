<!--
  PageHeader renders a page title, optional subtitle, and an actions slot.
-->
<script lang="ts">
  let {
    title,
    subtitle = '',
    children,
  }: {
    title: string;
    subtitle?: string;
    children?: import('svelte').Snippet;
  } = $props();
</script>

<div class="flex items-start justify-between mb-6">
  <div>
    <h1 class="text-xl font-bold text-base-content">{title}</h1>
    {#if subtitle}
      <p class="text-sm text-base-content/50 mt-0.5">{subtitle}</p>
    {/if}
  </div>
  {#if children}
    <div class="flex items-center gap-2">
      {@render children()}
    </div>
  {/if}
</div>
