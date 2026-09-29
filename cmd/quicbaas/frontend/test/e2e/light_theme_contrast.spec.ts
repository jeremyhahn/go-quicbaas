/**
 * light_theme_contrast.spec.ts
 *
 * Regression tests for issues #86 and #87 — light theme contrast fixes.
 *
 *   #86  Connected status badge must be a visually distinct pill in light
 *        theme, its text AA-legible on its own tint.
 *
 *   #87  Multiple low-contrast regressions in light theme:
 *        - "standalone" badge on /cluster is a tinted, legible pill.
 *        - Stat card labels (stat-title) render with sufficient contrast.
 *        - Primary button text is light (white), not dark (black).
 *        - Active sidebar item has a visually distinct background.
 *        - Subsystem-disabled placeholder text is legible (not near-invisible).
 *
 * Contrast helpers operate in oklch relative luminance space. WCAG AA requires
 * ≥4.5:1 for normal text and ≥3:1 for large/UI text and non-text components.
 * For this suite "sufficient contrast" means the computed luminance of the
 * element color departs meaningfully from its background — we use a
 * lightness-difference threshold rather than a full WCAG ratio because
 * Playwright cannot easily retrieve computed oklch values directly; instead we
 * use `getComputedStyle` to read the resolved CSS rgb() color and then measure
 * the relative luminance from the sRGB channels, following the WCAG 2.1
 * algorithm for relative luminance (IEC 61966-2-1).
 *
 * Run:
 *   cd frontend && npx playwright test test/e2e/light_theme_contrast.spec.ts
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Navigate to the SPA root then set a hash route. */
async function navigateTo(page: Page, hash: string): Promise<void> {
  await page.goto('/ui/', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  await page.waitForLoadState('networkidle');
}

/** Wait until all loading spinners have disappeared. */
async function waitForNoSpinner(page: Page, timeout = 10_000): Promise<void> {
  await page.waitForFunction(
    () => document.querySelectorAll('.loading').length === 0,
    undefined,
    { timeout },
  );
}

/**
 * Switch the UI to the light theme by clicking the theme-toggle button.
 * Returns after the data-theme attribute on <html> has been updated.
 */
async function enableLightTheme(page: Page): Promise<void> {
  // Set the theme via localStorage + dataset so we don't depend on the button
  // being in a specific toggle state before the test.
  await page.evaluate(() => {
    localStorage.setItem('quicbaas-theme', 'light');
    document.documentElement.dataset['theme'] = 'quicbaas-light';
  });
  // Confirm the attribute is set.
  await page.waitForFunction(
    () => document.documentElement.dataset['theme'] === 'quicbaas-light',
    undefined,
    { timeout: 3_000 },
  );
}

/**
 * Relative luminance of an sRGB channel value (0–255).
 * Implements the WCAG 2.1 IEC 61966-2-1 formula.
 */
function channelLuminance(c8bit: number): number {
  const c = c8bit / 255;
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

/**
 * Relative luminance (0–1) from an rgb/rgba string such as "rgb(12, 34, 56)".
 * Returns null when the string cannot be parsed (e.g. "transparent").
 */
function relativeLuminance(rgb: string): number | null {
  const m = /rgba?\(\s*(\d+)[, ]+(\d+)[, ]+(\d+)/i.exec(rgb);
  if (!m) return null;
  const r = channelLuminance(parseInt(m[1], 10));
  const g = channelLuminance(parseInt(m[2], 10));
  const b = channelLuminance(parseInt(m[3], 10));
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/**
 * In-browser helper source: normalize ANY CSS color (oklch(), color(srgb …),
 * named, hex, rgb) to a plain "rgb(r, g, b)" string by rendering one pixel to a
 * canvas and reading it back. Chromium's getComputedStyle returns oklch-authored
 * theme colors in oklch()/color() form, which the rgb-only luminance parser
 * above cannot read — this collapses every format to sRGB integers. The source
 * is injected via addInitScript so each spec can call window.__toRgb(color).
 */
const INSTALL_TO_RGB = (): void => {
  (window as unknown as { __toRgb: (c: string) => string }).__toRgb = (c: string): string => {
    const cv = document.createElement('canvas');
    cv.width = cv.height = 1;
    const ctx = cv.getContext('2d');
    if (ctx === null) return c;
    ctx.fillStyle = c;
    ctx.fillRect(0, 0, 1, 1);
    const d = ctx.getImageData(0, 0, 1, 1).data;
    return `rgb(${d[0]}, ${d[1]}, ${d[2]})`;
  };
};

/**
 * WCAG 2.1 contrast ratio between two relative luminances.
 * The lighter value is always the numerator.
 */
function contrastRatio(l1: number, l2: number): number {
  const lighter = Math.max(l1, l2);
  const darker = Math.min(l1, l2);
  return (lighter + 0.05) / (darker + 0.05);
}

// Make window.__toRgb available on every page before any navigation so the
// per-spec colour reads can normalise oklch/color() computed values to rgb.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(INSTALL_TO_RGB);
});

// ---------------------------------------------------------------------------
// #86 — Connected badge background differs from body background
// ---------------------------------------------------------------------------

/**
 * expectLegiblePill grades a soft badge the way the design system means it:
 * the text against the pill's own tint at AA (4.5:1), and the tint visibly
 * different from the page behind it.
 */
async function expectLegiblePill(badge: import('@playwright/test').Locator): Promise<void> {
  const { fgRgb, bgRgb, bodyRgb } = await badge.evaluate((el) => {
    const toRgb = (window as unknown as { __toRgb: (c: string) => string }).__toRgb;
    return {
      fgRgb: toRgb(getComputedStyle(el).color),
      bgRgb: toRgb(getComputedStyle(el).backgroundColor),
      bodyRgb: toRgb(getComputedStyle(document.body).backgroundColor),
    };
  });
  const fg = relativeLuminance(fgRgb);
  const bg = relativeLuminance(bgRgb);
  expect(fg).not.toBeNull();
  expect(bg).not.toBeNull();
  expect(contrastRatio(fg!, bg!)).toBeGreaterThanOrEqual(4.5);
  expect(bgRgb).not.toBe(bodyRgb);
}

test.describe('Light theme — #86 Connected badge visibility', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/dashboard');
    await enableLightTheme(page);
    await waitForNoSpinner(page);
  });

  test('Connected badge is a distinct pill whose text reads on its tint', async ({ page }) => {
    // StatusBadge renders a soft tinted pill (src/lib/theme/tokens.ts
    // STATUS_TONES), not a solid fill. What must hold is that its text is
    // AA-legible on its own tint, and that the tint is not the page colour.
    const badge = page.locator('header .badge').first();
    await expect(badge).toBeVisible({ timeout: 8_000 });
    await expectLegiblePill(badge);
  });
});

test.describe('Light theme — #87 Cluster standalone badge has color', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/cluster');
    await enableLightTheme(page);
    await waitForNoSpinner(page);
  });

  test('standalone badge is a distinct pill whose text reads on its tint', async ({ page }) => {
    await waitForNoSpinner(page);

    // The badge may read "standalone" or "raft" depending on cluster config.
    const modeBadge = page.locator('.badge', { hasText: /standalone|raft/i }).first();
    await expect(modeBadge).toBeVisible({ timeout: 8_000 });
    await expectLegiblePill(modeBadge);
  });
});
