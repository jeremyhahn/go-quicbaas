/**
 * appcss.test.ts
 *
 * Regression: primary CTA buttons across the app (`+ Spawn`, `+ Start Job`,
 * `Run Query`, `Send`, `Bootstrap`, `Search`, ...) previously rendered with
 * a coral/red fill because `--color-primary` was set to the brand orange/red.
 * That coloring made every non-destructive action look like a danger button.
 *
 * The fix:
 *   * `--color-primary`  → confident accent (blue) — used by btn-primary.
 *   * `--color-secondary`→ brand orange — kept for chrome and gradient text.
 *   * `--color-error`    → reserved for destructive / error semantics.
 *
 * This test pins the contract by inspecting the OKLCH hue channel on each
 * theme: the primary token must NOT live in the brand red/orange band
 * (hue ≈ 25–30). The error token, by contrast, must remain in that band.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);

function readAppCss(): string {
  const path = resolve(__dirname, '../src/app.css');
  return readFileSync(path, 'utf8');
}

interface OkLCH {
  l: number;
  c: number;
  h: number;
}

/**
 * extractToken finds the first `--color-<name>: oklch(L C H)` declaration
 * inside the given theme block and returns the parsed L/C/H triple. Tokens
 * declared at the @theme top level are intentionally ignored — daisyUI
 * resolves theme-scoped tokens against [data-theme="..."], not the
 * top-level @theme block. Returns null when the token is not declared
 * in the block (the theme inherits the daisyUI default).
 */
function extractToken(css: string, themeSelector: string, name: string): OkLCH | null {
  const blockStart = css.indexOf(themeSelector);
  if (blockStart < 0) throw new Error(`theme block ${themeSelector} not found`);
  const braceOpen = css.indexOf('{', blockStart);
  const braceClose = css.indexOf('}', braceOpen);
  const block = css.slice(braceOpen, braceClose);

  const re = new RegExp(`--color-${name}\\s*:\\s*oklch\\(\\s*([\\d.]+)\\s+([\\d.]+)\\s+([\\d.]+)\\s*\\)`);
  const m = re.exec(block);
  if (m === null) {
    return null;
  }
  return {
    l: parseFloat(m[1] ?? '0'),
    c: parseFloat(m[2] ?? '0'),
    h: parseFloat(m[3] ?? '0'),
  };
}

describe('app.css design tokens', () => {
  const css = readAppCss();
  const themes = [
    '[data-theme="quicbaas-dark"]',
    '[data-theme="quicbaas-light"]',
  ];

  for (const theme of themes) {
    describe(theme, () => {
      it('--color-primary lives outside the brand red/orange hue band', () => {
        const primary = extractToken(css, theme, 'primary');
        expect(primary, `--color-primary must be defined in ${theme}`).not.toBeNull();
        // Brand red/orange sits in the 15-50 hue band. Primary must
        // sit clearly outside it (e.g. blue ≈ 240-260, teal ≈ 160-180).
        const inRedOrangeBand = primary!.h >= 15 && primary!.h <= 50;
        expect(inRedOrangeBand).toBe(false);
        // Sanity: hue must be a real number.
        expect(Number.isFinite(primary!.h)).toBe(true);
      });

      it('--color-error, when overridden, remains in the brand red band (destructive intent)', () => {
        const error = extractToken(css, theme, 'error');
        // Themes that don't override --color-error inherit daisyUI's
        // default red — that's also a valid destructive token. Only
        // assert when the token is overridden in this theme.
        if (error === null) return;
        expect(error.h).toBeGreaterThanOrEqual(15);
        expect(error.h).toBeLessThanOrEqual(50);
      });

      it('--color-secondary stays in the brand orange/red band (gradient text)', () => {
        const secondary = extractToken(css, theme, 'secondary');
        expect(secondary, `--color-secondary must be defined in ${theme}`).not.toBeNull();
        // The gradient-text mixes secondary + accent. Secondary keeps
        // the brand identity by sitting in the same hue band as error.
        expect(secondary!.h).toBeGreaterThanOrEqual(15);
        expect(secondary!.h).toBeLessThanOrEqual(50);
      });
    });
  }

  // ---------------------------------------------------------------------------
  // Bug #39: inline `<code>` chips inside alert banners must adopt a
  // translucent-light background so they don't clash with the alert's
  // colored fill. The rule is theme-token driven (color-mix on
  // --color-base-100), so we just assert the rule is present.
  // ---------------------------------------------------------------------------

  describe('alert > code rule (#39)', () => {
    it('declares an `.alert code` rule with a translucent base-100 background', () => {
      // The rule must select both `.alert code` and `.alert .code`.
      expect(css).toMatch(/\.alert code,\s*\.alert \.code\s*{/);
    });

    it('uses color-mix on --color-base-100 so the chip blends with the active theme', () => {
      const ruleStart = css.indexOf('.alert code');
      expect(ruleStart).toBeGreaterThan(-1);
      const ruleEnd = css.indexOf('}', ruleStart);
      const rule = css.slice(ruleStart, ruleEnd);
      expect(rule).toMatch(/color-mix\([^)]*--color-base-100/);
    });

    it('inherits the alert text color so contrast stays balanced', () => {
      const ruleStart = css.indexOf('.alert code');
      const ruleEnd = css.indexOf('}', ruleStart);
      const rule = css.slice(ruleStart, ruleEnd);
      expect(rule).toMatch(/color:\s*inherit/);
    });
  });

  // ---------------------------------------------------------------------------
  // Bug #38: light theme polish — table zebra striping and sidebar hover
  // must be explicitly addressed for the cream palette.
  // ---------------------------------------------------------------------------

  describe('light theme polish (#38)', () => {
    it('declares a sidebar hover background for the light theme', () => {
      expect(css).toMatch(/\[data-theme="quicbaas-light"\]\s+aside button:hover\s*{/);
    });

    it('declares a darker zebra striping for the light theme', () => {
      expect(css).toMatch(/\[data-theme="quicbaas-light"\]\s+\.table-zebra tbody tr:nth-child\(even\)\s*{/);
    });
  });
});
