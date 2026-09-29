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
 * The design system's colour tokens, as plain hex — the one place every
 * foreground/background pairing this app renders is declared, and the one
 * place `contrast.test.ts` reads to prove every pairing clears WCAG AA.
 *
 * TWO FILES, ONE PALETTE. `src/app.css` carries the SAME values for the
 * tokens daisyUI/Tailwind consume at render time (`--color-primary`, and
 * friends), spelled as `oklch()` because `test/appcss.test.ts` pins that
 * format and inspects the hue channel directly — a Tailwind v4 theme token
 * cannot be a CSS variable reference to a value computed here without
 * losing daisyUI's ability to derive contrast-safe accents from it. Keeping
 * a hex mirror here, rather than trying to parse or replicate oklch() math
 * in both directions, is what makes this contrast test possible without a
 * browser: hex is exact and the WCAG formula is defined in sRGB. The two
 * are kept in step by hand; a mismatch is a defect in app.css, not here.
 *
 * The STATUS TONES (the soft pill StatusBadge renders) exist ONLY here —
 * they are not daisyUI tokens, never round-trip through app.css, and are
 * applied via inline style from `statusToVariant`'s theme-aware lookup.
 */

export type ThemeName = 'light' | 'dark';

/** Palette is the base surface and semantic scale for one theme. */
export interface Palette {
  base100: string;
  base200: string;
  base300: string;
  baseContent: string;
  /** contentSecondary is body-weight secondary text — descriptions, timestamps. */
  contentSecondary: string;
  /** contentTertiary is the faintest text still required to read at AA — labels, placeholders. */
  contentTertiary: string;
  primary: string;
  primaryContent: string;
  secondary: string;
  secondaryContent: string;
  accent: string;
  accentContent: string;
  success: string;
  successContent: string;
  warning: string;
  warningContent: string;
  error: string;
  errorContent: string;
  info: string;
  infoContent: string;
  neutral: string;
  neutralContent: string;
}

/**
 * PALETTES mirrors the `oklch()` values in `src/app.css`'s two theme blocks.
 * Every base/content pair below was chosen by sweeping OKLCH lightness
 * against the paired surface until the WCAG ratio cleared 4.5:1 — see
 * `contrast.test.ts`, which asserts it stayed that way.
 */
export const PALETTES: Readonly<Record<ThemeName, Palette>> = {
  light: {
    base100: '#f5f6f8',
    base200: '#eceef1',
    base300: '#dadee3',
    baseContent: '#15181c',
    contentSecondary: '#474d56',
    contentTertiary: '#565c66',
    primary: '#06489c',
    primaryContent: '#ffffff',
    secondary: '#c0401f',
    secondaryContent: '#ffffff',
    accent: '#bb061e',
    accentContent: '#ffffff',
    success: '#007136',
    successContent: '#ffffff',
    warning: '#cd8800',
    warningContent: '#1b150b',
    error: '#ac0010',
    errorContent: '#ffffff',
    info: '#005ba0',
    infoContent: '#ffffff',
    neutral: '#2b3036',
    neutralContent: '#f5f6f8',
  },
  dark: {
    base100: '#0e1013',
    base200: '#15181c',
    base300: '#20242a',
    baseContent: '#e4e7eb',
    contentSecondary: '#a6acb4',
    contentTertiary: '#8e949d',
    primary: '#79afff',
    primaryContent: '#061223',
    secondary: '#f28058',
    secondaryContent: '#230802',
    accent: '#fd736d',
    accentContent: '#1e0403',
    success: '#58c97d',
    successContent: '#001102',
    warning: '#e9ab2b',
    warningContent: '#1d0e00',
    error: '#f66c6d',
    errorContent: '#1c0203',
    info: '#69b5f5',
    infoContent: '#000e1d',
    neutral: '#2a2f36',
    neutralContent: '#e4e7eb',
  },
};

/** Tone is the palette family a status pill draws from. */
export type Tone = 'success' | 'warning' | 'error' | 'info' | 'primary' | 'neutral';

/** StatusTone is one soft pill's background tint and foreground text. */
export interface StatusTone {
  bg: string;
  fg: string;
}

/**
 * STATUS_TONES is what StatusBadge renders: a pale (light theme) or deep
 * (dark theme) tint of the semantic hue behind the text, and the same
 * hue's deep/vivid tone AS the text — never a solid saturated fill with
 * white text stamped over it. Every pair here clears 4.5:1 (contrast.test.ts).
 */
export const STATUS_TONES: Readonly<Record<ThemeName, Record<Tone, StatusTone>>> = {
  light: {
    success: { bg: '#e0f5e5', fg: '#005a20' },
    warning: { bg: '#fdecd5', fg: '#7a3a00' },
    error: { bg: '#ffe7e4', fg: '#9b0007' },
    info: { bg: '#e1f1ff', fg: '#004a88' },
    primary: { bg: '#e4efff', fg: '#013e8b' },
    neutral: { bg: '#e2e5e9', fg: '#3a4048' },
  },
  dark: {
    success: { bg: '#022710', fg: '#58c97d' },
    warning: { bg: '#2e1b00', fg: '#e9ab2b' },
    error: { bg: '#371112', fg: '#f66c6d' },
    info: { bg: '#01213a', fg: '#69b5f5' },
    primary: { bg: '#0b1f3b', fg: '#79afff' },
    neutral: { bg: '#272c32', fg: '#b5bbc3' },
  },
};

/** ContrastPair is one foreground/background pairing this app renders, graded against a WCAG floor. */
export interface ContrastPair {
  name: string;
  fg: string;
  bg: string;
  minimum: number;
}

const AA_BODY = 4.5;

/**
 * contrastPairs enumerates every fg/bg pairing the design system commits
 * to — the base surfaces, the semantic solid fills (buttons, alerts), and
 * every status pill — for both themes. `contrast.test.ts` is a thin loop
 * over this list; add a token here and it is graded for free.
 */
export function contrastPairs(): ContrastPair[] {
  const pairs: ContrastPair[] = [];
  for (const theme of Object.keys(PALETTES) as ThemeName[]) {
    const p = PALETTES[theme];
    const surfaces: Array<[string, string]> = [
      ['base-100', p.base100],
      ['base-200', p.base200],
      ['base-300', p.base300],
    ];
    for (const [surfaceName, surfaceHex] of surfaces) {
      pairs.push({ name: `${theme}/base-content on ${surfaceName}`, fg: p.baseContent, bg: surfaceHex, minimum: AA_BODY });
      pairs.push({ name: `${theme}/content-secondary on ${surfaceName}`, fg: p.contentSecondary, bg: surfaceHex, minimum: AA_BODY });
      pairs.push({ name: `${theme}/content-tertiary on ${surfaceName}`, fg: p.contentTertiary, bg: surfaceHex, minimum: AA_BODY });
    }

    const solids: Array<[string, string, string]> = [
      ['primary', p.primary, p.primaryContent],
      ['secondary', p.secondary, p.secondaryContent],
      ['accent', p.accent, p.accentContent],
      ['success', p.success, p.successContent],
      ['warning', p.warning, p.warningContent],
      ['error', p.error, p.errorContent],
      ['info', p.info, p.infoContent],
      ['neutral', p.neutral, p.neutralContent],
    ];
    for (const [tokenName, bg, fg] of solids) {
      pairs.push({ name: `${theme}/${tokenName}-content on ${tokenName}`, fg, bg, minimum: AA_BODY });
    }

    const tones = STATUS_TONES[theme];
    for (const tone of Object.keys(tones) as Tone[]) {
      const t = tones[tone];
      pairs.push({ name: `${theme}/status-${tone} pill`, fg: t.fg, bg: t.bg, minimum: AA_BODY });
    }
  }
  return pairs;
}
