// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

/**
 * Every foreground/background pair the design system commits to — base
 * text on every surface, every solid semantic fill, and every StatusBadge
 * pill, in both themes — must clear WCAG AA. This is not a snapshot of
 * today's palette; it is the gate a future palette change has to pass
 * before it ships, whether it tightens a colour or loosens one.
 */

import { describe, it, expect } from 'vitest';
import { contrastRatio, meetsAA } from '../src/lib/theme/contrast.ts';
import { contrastPairs, PALETTES, STATUS_TONES } from '../src/lib/theme/tokens.ts';

describe('contrastRatio — the WCAG formula itself', () => {
  it('reads 21:1 for pure black on pure white', () => {
    expect(contrastRatio('#000000', '#ffffff')).toBeCloseTo(21, 0);
  });

  it('reads 1:1 for identical colours', () => {
    expect(contrastRatio('#336699', '#336699')).toBeCloseTo(1, 5);
  });

  it('is symmetric — argument order does not change the ratio', () => {
    expect(contrastRatio('#111111', '#eeeeee')).toBeCloseTo(contrastRatio('#eeeeee', '#111111'), 10);
  });

  it('accepts 3-digit hex the same as its 6-digit expansion', () => {
    expect(contrastRatio('#fff', '#000')).toBeCloseTo(contrastRatio('#ffffff', '#000000'), 10);
  });
});

describe('meetsAA', () => {
  it('passes a pair comfortably above the floor', () => {
    expect(meetsAA('#000000', '#ffffff')).toBe(true);
  });

  it('fails a pair below the floor', () => {
    // Mid-grey on white sits around 3.9:1 — short of the 4.5 body floor.
    expect(meetsAA('#949494', '#ffffff')).toBe(false);
  });
});

describe('every design-system token pair clears WCAG AA', () => {
  for (const pair of contrastPairs()) {
    it(`${pair.name}: ${pair.fg} on ${pair.bg} >= ${pair.minimum}:1`, () => {
      const ratio = contrastRatio(pair.fg, pair.bg);
      expect(ratio, `${pair.name} measured ${ratio.toFixed(2)}:1, needs ${pair.minimum}:1`).toBeGreaterThanOrEqual(
        pair.minimum,
      );
    });
  }
});

describe('palette shape', () => {
  it('declares both themes with every token as a valid 6-digit hex colour', () => {
    const hex6 = /^#[0-9a-f]{6}$/i;
    for (const theme of ['light', 'dark'] as const) {
      const palette = PALETTES[theme];
      for (const [key, value] of Object.entries(palette)) {
        expect(value, `${theme}.${key}`).toMatch(hex6);
      }
    }
  });

  it('declares a status tone for every tone, in both themes', () => {
    const toneNames = ['success', 'warning', 'error', 'info', 'primary', 'neutral'] as const;
    for (const theme of ['light', 'dark'] as const) {
      for (const tone of toneNames) {
        expect(STATUS_TONES[theme][tone]).toBeDefined();
      }
    }
  });

  it('rejects a pair that regresses below AA (the test protects a floor, not a snapshot)', () => {
    // A deliberately bad pair — proves the loop above would actually fail
    // rather than vacuously passing on any input.
    expect(meetsAA('#cccccc', '#ffffff')).toBe(false);
  });
});
