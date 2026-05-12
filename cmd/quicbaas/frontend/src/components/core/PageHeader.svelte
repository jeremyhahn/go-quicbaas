<!--
  PageHeader renders a page title, optional subtitle, optional icon, and an actions slot.
-->
<script lang="ts">
  import type { Component } from 'svelte';

  let {
    title,
    subtitle = '',
    icon: Icon,
    children,
  }: {
    title: string;
    subtitle?: string;
    icon?: Component<{ class?: string }>;
    children?: import('svelte').Snippet;
  } = $props();
</script>

<div class="flex items-start justify-between mb-6">
  <div class="flex items-center gap-3">
    {#if Icon}
      <div class="flex items-center justify-center w-9 h-9 rounded-lg bg-primary/10 text-primary shrink-0">
        <Icon class="h-5 w-5" />
      </div>
    {/if}
    <div>
      <h1 class="text-2xl font-bold text-base-content">{title}</h1>
      {#if subtitle}
        <p class="text-sm text-base-content/60 mt-0.5">{subtitle}</p>
      {/if}
    </div>
  </div>
  {#if children}
    <div class="flex items-center gap-2">
      {@render children()}
    </div>
  {/if}
</div>
