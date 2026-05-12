<!--
  StatusBadge renders a daisyUI badge with an optional pulse indicator dot.

  Variants map to daisyUI semantic colors so badges inherit theme tokens and
  stay legible across both light and dark themes. The previous `badge-soft`
  modifier produced near-invisible tinted text on the light theme; the
  canonical filled variants ship with proper foreground contrast.
-->
<script lang="ts">
  /**
   * StatusVariant is the canonical mapping from a high-level intent to a
   * daisyUI semantic color. The mapping is consumed by view-specific helpers
   * that translate raw status strings (e.g. "active", "failed", "queued")
   * into a variant — see `statusToVariant()` below for the canonical mapping.
   */
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
    success: 'bg-success-content',
    error: 'bg-error-content',
    warning: 'bg-warning-content',
    info: 'bg-info-content',
    primary: 'bg-primary-content',
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
