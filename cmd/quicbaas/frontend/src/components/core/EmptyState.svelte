<!--
  EmptyState renders a centered placeholder with icon, title, description,
  primary action button, and an optional secondary "Learn more" link.
-->
<script lang="ts">
  let {
    icon,
    title,
    description = '',
    actionLabel = '',
    onaction,
    learnMoreHref = '',
    learnMoreLabel = 'Learn more',
  }: {
    // Lucide icons are SvelteComponentTyped classes; use a permissive type.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: new (...args: any[]) => any;
    title: string;
    description?: string;
    actionLabel?: string;
    onaction?: () => void;
    learnMoreHref?: string;
    learnMoreLabel?: string;
  } = $props();
</script>

<div class="flex flex-col items-center justify-center gap-4 py-16 px-8 text-center">
  {#if icon}
    {@const Icon = icon}
    <Icon class="h-10 w-10 text-base-content/30 mb-3" strokeWidth={1} />
  {/if}
  <div class="space-y-1">
    <p class="text-sm font-semibold text-base-content">{title}</p>
    {#if description}
      <p class="text-sm text-base-content/60 max-w-md text-center">{description}</p>
    {/if}
  </div>
  <div class="flex items-center gap-3 flex-wrap justify-center">
    {#if actionLabel && onaction}
      <button class="btn btn-primary btn-sm" onclick={onaction}>{actionLabel}</button>
    {/if}
    {#if learnMoreHref}
      <a
        href={learnMoreHref}
        class="link link-primary text-sm"
        target="_blank"
        rel="noopener noreferrer"
      >{learnMoreLabel}</a>
    {/if}
  </div>
</div>
