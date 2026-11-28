<!--
  PageHeader renders a page title, optional subtitle, optional icon, and an actions slot.
-->
<script lang="ts">
  import type { Component } from 'svelte';
  import type { Icon as LucideIcon } from 'lucide-svelte';

  let {
    title,
    subtitle = '',
    icon: Icon,
    children,
  }: {
    title: string;
    subtitle?: string;
    icon?: Component<{ class?: string }> | typeof LucideIcon;
    children?: import('svelte').Snippet;
  } = $props();
</script>

<!--
  The title is set in the condensed display cut (`.display`, app.css) — the
  one place the app raises its voice. The icon sits beside it as a quiet
  glyph in the accent colour rather than inside a tinted tile, and the
  actions wrap under the title on a narrow screen instead of squeezing it.
-->
<header class="page-header mb-6 flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
  <div class="flex min-w-0 items-start gap-3">
    {#if Icon}
      <Icon class="mt-1.5 h-5 w-5 shrink-0 text-primary" />
    {/if}
    <div class="min-w-0">
      <h1 class="display text-[1.75rem] text-base-content">{title}</h1>
      {#if subtitle}
        <p class="mt-1 text-sm text-base-content/60">{subtitle}</p>
      {/if}
    </div>
  </div>
  {#if children}
    <div class="flex flex-wrap items-center gap-2">
      {@render children()}
    </div>
  {/if}
</header>
