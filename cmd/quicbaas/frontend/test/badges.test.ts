/**
 * badges.test.ts
 *
 * Pins the canonical status → variant mapping consumed by every view that
 * renders a status badge. The mapping lives in
 * `src/components/core/statusVariant.ts` and is the single source of truth.
 *
 * Test cases:
 *   - common state strings map to the expected canonical variant
 *   - `statusBadgeClass` still answers the raw daisyUI class, for the few
 *     call sites that render a plain `<span class="badge {cls}">` rather
 *     than the StatusBadge component (e.g. an outline/plain badge elsewhere)
 *   - StatusBadge itself renders the tinted-pill tone for each variant — a
 *     pale/deep background from the SAME tone family as its text, never a
 *     solid saturated fill with white stamped over it (regression for #32:
 *     nearly-invisible tinted badges in the light theme, and its converse,
 *     a badge so saturated its text washed out).
 */

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import StatusBadge from '../src/components/core/StatusBadge.svelte';
import { statusToVariant, statusBadgeClass } from '../src/components/core/statusVariant.ts';
import { STATUS_TONES } from '../src/lib/theme/tokens.ts';
import { meetsAA } from '../src/lib/theme/contrast.ts';

describe('statusToVariant — canonical status → variant mapping', () => {
  const cases: Array<[string, string]> = [
    // success
    ['active', 'success'],
    ['enabled', 'success'],
    ['available', 'success'],
    ['ok', 'success'],
    ['healthy', 'success'],
    ['running', 'success'],
    ['succeeded', 'success'],
    ['completed', 'success'],

    // warning
    ['pending', 'warning'],
    ['queued', 'warning'],
    ['paused', 'warning'],
    ['degraded', 'warning'],
    // task lifecycle: held but not worked, or parked on a person
    ['claimed', 'warning'],
    ['blocked', 'warning'],

    // primary — work actively happening
    ['in_progress', 'primary'],

    // error
    ['error', 'error'],
    ['failed', 'error'],
    ['revoked', 'error'],
    ['destroyed', 'error'],
    ['cancelled', 'error'],

    // neutral / disabled
    ['disabled', 'neutral'],
    ['closed', 'neutral'],
    ['terminated', 'neutral'],
    ['offline', 'neutral'],

    // info / role labels
    ['idle', 'info'],
    ['observer', 'info'],
    ['worker', 'info'],
    ['lead', 'info'],
  ];

  for (const [status, expected] of cases) {
    it(`maps "${status}" → ${expected}`, () => {
      expect(statusToVariant(status)).toBe(expected);
    });
  }

  it('matches case-insensitively', () => {
    expect(statusToVariant('ACTIVE')).toBe('success');
    expect(statusToVariant(' Failed ')).toBe('error');
  });

  it('unknown tokens degrade to info so user-defined values stay visible', () => {
    expect(statusToVariant('foobar')).toBe('info');
    expect(statusToVariant('')).toBe('info');
  });
});

describe('statusBadgeClass — raw daisyUI badge class for a status', () => {
  it('returns badge-success for active/healthy states', () => {
    expect(statusBadgeClass('active')).toBe('badge-success');
    expect(statusBadgeClass('healthy')).toBe('badge-success');
  });

  it('returns badge-warning for transitional states', () => {
    expect(statusBadgeClass('pending')).toBe('badge-warning');
    expect(statusBadgeClass('paused')).toBe('badge-warning');
  });

  it('returns badge-error for failure states', () => {
    expect(statusBadgeClass('failed')).toBe('badge-error');
    expect(statusBadgeClass('revoked')).toBe('badge-error');
    expect(statusBadgeClass('destroyed')).toBe('badge-error');
  });

  it('returns badge-neutral for disabled states', () => {
    expect(statusBadgeClass('disabled')).toBe('badge-neutral');
    expect(statusBadgeClass('closed')).toBe('badge-neutral');
  });

  it('returns badge-info for unknown tokens (legible fallback)', () => {
    expect(statusBadgeClass('xyzzy')).toBe('badge-info');
  });
});

describe('StatusBadge — renders a tinted pill from the tone table', () => {
  // themeStore defaults to 'dark' (see src/stores/theme.svelte.ts), so an
  // unconfigured render reads the dark palette.
  const DARK = STATUS_TONES.dark;

  it('applies the dark success tone background and matching text colour', () => {
    const { container } = render(StatusBadge, { props: { text: 'active', variant: 'success' } });
    const span = container.querySelector('span.badge')! as HTMLElement;
    expect(span.style.backgroundColor).toBe(hexToRgbString(DARK.success.bg));
    expect(span.style.color).toBe(hexToRgbString(DARK.success.fg));
  });

  it('applies the dark warning tone', () => {
    const { container } = render(StatusBadge, { props: { text: 'pending', variant: 'warning' } });
    const span = container.querySelector('span.badge')! as HTMLElement;
    expect(span.style.backgroundColor).toBe(hexToRgbString(DARK.warning.bg));
    expect(span.style.color).toBe(hexToRgbString(DARK.warning.fg));
  });

  it('applies the dark error tone', () => {
    const { container } = render(StatusBadge, { props: { text: 'failed', variant: 'error' } });
    const span = container.querySelector('span.badge')! as HTMLElement;
    expect(span.style.backgroundColor).toBe(hexToRgbString(DARK.error.bg));
    expect(span.style.color).toBe(hexToRgbString(DARK.error.fg));
  });

  it('applies the dark info tone', () => {
    const { container } = render(StatusBadge, { props: { text: 'observer', variant: 'info' } });
    const span = container.querySelector('span.badge')! as HTMLElement;
    expect(span.style.backgroundColor).toBe(hexToRgbString(DARK.info.bg));
    expect(span.style.color).toBe(hexToRgbString(DARK.info.fg));
  });

  it('applies the dark neutral tone for both "neutral" and "ghost"', () => {
    for (const variant of ['neutral', 'ghost'] as const) {
      const { container } = render(StatusBadge, { props: { text: 'disabled', variant } });
      const span = container.querySelector('span.badge')! as HTMLElement;
      expect(span.style.backgroundColor).toBe(hexToRgbString(DARK.neutral.bg));
      expect(span.style.color).toBe(hexToRgbString(DARK.neutral.fg));
    }
  });

  // -------------------------------------------------------------------------
  // Regression #32 / redesign 2026-09-22: neither a near-invisible tinted
  // badge NOR a solid saturated fill with washed-out text — every tone this
  // component can render clears WCAG AA on its own pill background.
  // -------------------------------------------------------------------------
  it('every tone this component renders clears WCAG AA text contrast (regression: bug #32)', () => {
    for (const theme of ['light', 'dark'] as const) {
      for (const tone of Object.values(STATUS_TONES[theme])) {
        expect(meetsAA(tone.fg, tone.bg)).toBe(true);
      }
    }
  });

  it('renders a dot indicator whose colour matches the text, not the fill', () => {
    const { container } = render(StatusBadge, { props: { text: 'active', variant: 'success' } });
    const dot = container.querySelector('span.badge > span')! as HTMLElement;
    expect(dot.style.backgroundColor).toBe(hexToRgbString(DARK.success.fg));
  });

  it('only animates the dot when pulse is set', () => {
    const { container: still } = render(StatusBadge, { props: { text: 'active', variant: 'success' } });
    expect(still.querySelector('span.badge > span')!.className).not.toContain('animate-pulse');

    const { container: pulsing } = render(StatusBadge, {
      props: { text: 'active', variant: 'success', pulse: true },
    });
    expect(pulsing.querySelector('span.badge > span')!.className).toContain('animate-pulse');
  });

  it('renders the supplied text content', () => {
    const { container } = render(StatusBadge, { props: { text: 'hello', variant: 'success' } });
    const span = container.querySelector('span.badge')!;
    expect(span.textContent?.trim()).toBe('hello');
  });
});

/** hexToRgbString mirrors how jsdom normalises an inline `style:` hex colour. */
function hexToRgbString(hex: string): string {
  const n = parseInt(hex.slice(1), 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  return `rgb(${r}, ${g}, ${b})`;
}
