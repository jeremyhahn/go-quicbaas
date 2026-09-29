<!--
  StatCard renders a daisyUI stat component with label, value, description,
  and an optional icon.
-->
<script lang="ts">
  let {
    label,
    value,
    description = '',
    icon,
    variant = 'default',
  }: {
    label: string;
    value: string | number;
    description?: string;
    // Lucide icons are SvelteComponentTyped classes; use a permissive type.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    icon?: new (...args: any[]) => any;
    variant?: 'default' | 'primary' | 'success' | 'warning' | 'error';
  } = $props();

  const valueClasses: Readonly<Record<string, string>> = {
    default: 'stat-value display text-[2rem]',
    primary: 'stat-value display text-[2rem] text-primary',
    success: 'stat-value display text-[2rem] text-success',
    warning: 'stat-value display text-[2rem] text-warning',
    error: 'stat-value display text-[2rem] text-error',
  };
</script>

<div class="stat rounded-box border border-base-300/80 bg-base-200 px-5 py-4">
  {#if icon}
    {@const Icon = icon}
    <div class="stat-figure self-start text-base-content/40">
      <Icon class="h-5 w-5" strokeWidth={1.75} />
    </div>
  {/if}
  <div class="stat-title text-xs font-medium text-base-content/60">{label}</div>
  <div class={valueClasses[variant] ?? 'stat-value display text-[2rem]'}>{value}</div>
  {#if description}
    <div class="stat-desc mt-1 text-xs text-base-content/60">{description}</div>
  {/if}
</div>
