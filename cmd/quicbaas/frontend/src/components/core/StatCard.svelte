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
    default: 'stat-value',
    primary: 'stat-value text-primary',
    success: 'stat-value text-success',
    warning: 'stat-value text-warning',
    error: 'stat-value text-error',
  };
</script>

<div class="stat bg-base-200 rounded-box p-5 border border-base-300 shadow-sm">
  {#if icon}
    {@const Icon = icon}
    <div class="stat-figure text-base-content/25">
      <Icon class="h-6 w-6" strokeWidth={1.5} />
    </div>
  {/if}
  <div class="stat-title text-xs uppercase tracking-wider text-base-content/50">{label}</div>
  <div class={valueClasses[variant] ?? 'stat-value text-2xl'}>{value}</div>
  {#if description}
    <div class="stat-desc text-base-content/40 mt-1">{description}</div>
  {/if}
</div>
