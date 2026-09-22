<!--
  StatusBadge renders one status as a soft, legibly-padded pill: a pale
  (light theme) or deep (dark theme) tint of the status's tone behind the
  text, a small dot for a glanceable colour cue, and the tone's own deep or
  vivid colour AS the text — never white stamped over a saturated fill.

  Every tone/theme pairing this renders is enumerated in
  `src/lib/theme/tokens.ts` and graded against WCAG AA by
  `test/themeContrast.test.ts`; this component only ever looks up a pair
  from that table, it does not invent colour.
-->
<script lang="ts">
  import { themeStore } from '../../stores/theme.svelte.ts';
  import { STATUS_TONES, type Tone } from '../../lib/theme/tokens.ts';

  /**
   * StatusVariant is the canonical mapping from a high-level intent to a
   * tone. The mapping is consumed by view-specific helpers that translate
   * raw status strings (e.g. "active", "failed", "queued") into a variant —
   * see `statusToVariant()` in statusVariant.ts.
   *
   * 'ghost' is kept as an alias of the neutral tone: call sites that used to
   * ask for the daisyUI ghost badge (an outline with no fill) get the same
   * calm, low-emphasis treatment as an explicit 'neutral'.
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

  const TONE_FOR_VARIANT: Readonly<Record<StatusVariant, Tone>> = {
    success: 'success',
    error: 'error',
    warning: 'warning',
    info: 'info',
    primary: 'primary',
    ghost: 'neutral',
    neutral: 'neutral',
  };

  const tone = $derived(STATUS_TONES[themeStore.theme === 'light' ? 'light' : 'dark'][TONE_FOR_VARIANT[variant]]);
</script>

<!--
  `badge` is kept as a plain marker class so existing selectors (e2e specs,
  `header .badge`) keep finding this element — it contributes none of
  daisyUI's `.badge` visual defaults here, because every visual property
  (colour, padding, radius, gap, size) is set explicitly below and Tailwind's
  utility layer already wins the cascade against daisyUI's component layer.
-->
<span
  class="badge status-badge inline-flex items-center gap-1.5 rounded-full border-0 px-2.5 py-1 text-xs font-medium leading-none whitespace-nowrap h-auto"
  style:background-color={tone.bg}
  style:color={tone.fg}
  data-variant={variant}
>
  <span
    class="h-1.5 w-1.5 shrink-0 rounded-full"
    class:animate-pulse={pulse}
    style:background-color={tone.fg}
    aria-hidden="true"
  ></span>
  {text}
</span>
