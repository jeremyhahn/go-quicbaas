<!--
  EmptyState renders a centered placeholder with icon, title, description,
  and an optional action button.
-->
<script lang="ts">
  let {
    icon,
    title,
    description = '',
    actionLabel = '',
    onaction,
  }: {
    // Lucide icons are SvelteComponentTyped classes; use a permissive type.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: new (...args: any[]) => any;
    title: string;
    description?: string;
    actionLabel?: string;
    onaction?: () => void;
  } = $props();
</script>

<div class="flex flex-col items-center justify-center gap-4 p-8 text-center">
  {#if icon}
    {@const Icon = icon}
    <Icon class="h-16 w-16 text-base-content/20" strokeWidth={1} />
  {/if}
  <div>
    <h3 class="text-lg font-semibold text-base-content/70">{title}</h3>
    {#if description}
      <p class="mt-1 text-sm text-base-content/40">{description}</p>
    {/if}
  </div>
  {#if actionLabel && onaction}
    <button class="btn btn-primary btn-sm" onclick={onaction}>{actionLabel}</button>
  {/if}
</div>
