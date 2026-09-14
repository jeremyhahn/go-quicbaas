/**
 * badges.test.ts
 *
 * Pins the canonical status → variant mapping consumed by every view that
 * renders a status badge. The mapping lives in
 * `src/components/core/statusVariant.ts` and is the single source of truth.
 *
 * Test cases:
 *   - common state strings map to the expected daisyUI semantic class
 *   - StatusBadge renders the correct CSS class for each variant
 *   - The faded `badge-soft` modifier is no longer applied (regression for
 *     #32: nearly-invisible tinted badges in the light theme).
 */

import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/svelte';
import StatusBadge from '../src/components/core/StatusBadge.svelte';
import { statusToVariant, statusBadgeClass } from '../src/components/core/statusVariant.ts';

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

describe('StatusBadge — renders canonical daisyUI variant classes', () => {
  it('applies badge-success class for the success variant', () => {
    const { container } = render(StatusBadge, { props: { text: 'active', variant: 'success' } });
    const span = container.querySelector('span.badge')!;
    expect(span.className).toContain('badge-success');
  });

  it('applies badge-warning class for the warning variant', () => {
    const { container } = render(StatusBadge, { props: { text: 'pending', variant: 'warning' } });
    const span = container.querySelector('span.badge')!;
    expect(span.className).toContain('badge-warning');
  });

  it('applies badge-error class for the error variant', () => {
    const { container } = render(StatusBadge, { props: { text: 'failed', variant: 'error' } });
    const span = container.querySelector('span.badge')!;
    expect(span.className).toContain('badge-error');
  });

  it('applies badge-info class for the info variant', () => {
    const { container } = render(StatusBadge, { props: { text: 'observer', variant: 'info' } });
    const span = container.querySelector('span.badge')!;
    expect(span.className).toContain('badge-info');
  });

  it('applies badge-neutral class for the neutral variant', () => {
    const { container } = render(StatusBadge, { props: { text: 'disabled', variant: 'neutral' } });
    const span = container.querySelector('span.badge')!;
    expect(span.className).toContain('badge-neutral');
  });

  // -------------------------------------------------------------------------
  // Regression: the `badge-soft` modifier produced near-invisible tinted
  // badges on the light theme. The fix removes badge-soft so the canonical
  // filled variants take over.
  // -------------------------------------------------------------------------
  it('does not apply the faded badge-soft modifier (regression: bug #32)', () => {
    for (const variant of ['success', 'warning', 'error', 'info', 'primary', 'neutral'] as const) {
      const { container } = render(StatusBadge, { props: { text: 't', variant } });
      const span = container.querySelector('span.badge')!;
      expect(span.className).not.toContain('badge-soft');
    }
  });

  it('renders the supplied text content', () => {
    const { container } = render(StatusBadge, { props: { text: 'hello', variant: 'success' } });
    const span = container.querySelector('span.badge')!;
    expect(span.textContent?.trim()).toBe('hello');
  });
});
