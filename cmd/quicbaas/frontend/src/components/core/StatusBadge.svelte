<!--
  StatusBadge renders a daisyUI badge with an optional pulse indicator dot.
-->
<script lang="ts">
  export type StatusVariant = 'success' | 'error' | 'warning' | 'info' | 'primary' | 'ghost' | 'neutral';

  let {
    text,
    variant = 'info',
    pulse = false,
  }: {
    text: string;
    variant?: StatusVariant;
    pulse?: boolean;
  } = $props();

  const variantClasses: Readonly<Record<StatusVariant, string>> = {
    success: 'badge-success',
    error: 'badge-error',
    warning: 'badge-warning',
    info: 'badge-info',
    primary: 'badge-primary',
    ghost: 'badge-ghost',
    neutral: 'badge-neutral',
  } as const;

  const dotColors: Readonly<Record<StatusVariant, string>> = {
    success: 'bg-success',
    error: 'bg-error',
    warning: 'bg-warning',
    info: 'bg-info',
    primary: 'bg-primary',
    ghost: 'bg-base-content/40',
    neutral: 'bg-neutral-content',
  } as const;
</script>

<span class="badge badge-sm gap-1.5 {variantClasses[variant]}">
  {#if pulse}
    <span
      class="inline-block h-1.5 w-1.5 rounded-full {dotColors[variant]} animate-pulse"
    ></span>
  {/if}
  {text}
</span>
