/**
 * light_theme_contrast.spec.ts
 *
 * Regression tests for issues #86 and #87 — light theme contrast fixes.
 *
 *   #86  Connected status badge must have a visually distinct background
 *        in light theme (badge-success green, not the body cream color).
 *
 *   #87  Multiple low-contrast regressions in light theme:
 *        - "standalone" badge on /cluster has a colored background.
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
 * WCAG 2.1 contrast ratio between two relative luminances.
 * The lighter value is always the numerator.
 */
function contrastRatio(l1: number, l2: number): number {
  const lighter = Math.max(l1, l2);
  const darker = Math.min(l1, l2);
  return (lighter + 0.05) / (darker + 0.05);
}

// ---------------------------------------------------------------------------
// #86 — Connected badge background differs from body background
// ---------------------------------------------------------------------------

test.describe('Light theme — #86 Connected badge visibility', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/dashboard');
    await enableLightTheme(page);
    await waitForNoSpinner(page);
  });

  test('Connected badge computed bg-color differs from body bg-color', async ({ page }) => {
    // The badge rendered by StatusBadge with variant="success" must not share
    // its background with the body (--color-base-100).
    const bodyBg: string = await page.evaluate((): string => {
      return getComputedStyle(document.documentElement).getPropertyValue('--color-base-100').trim();
    });

    // Locate the Connected / Disconnected badge in the header.
    const badge = page.locator('header .badge').first();
    await expect(badge).toBeVisible({ timeout: 8_000 });

    const badgeBgRgb: string = await badge.evaluate((el): string => {
      return getComputedStyle(el).backgroundColor;
    });

    const bodyBgRgb: string = await page.evaluate((): string => {
      // Resolve --color-base-100 as a CSS variable on an element.
      const tmp = document.createElement('div');
      tmp.style.backgroundColor = 'var(--color-base-100)';
      document.body.appendChild(tmp);
      const resolved = getComputedStyle(tmp).backgroundColor;
      tmp.remove();
      return resolved;
    });

    // Both must parse to valid rgb() strings.
    const badgeLum = relativeLuminance(badgeBgRgb);
    const bodyLum = relativeLuminance(bodyBgRgb);

    expect(badgeLum).not.toBeNull();
    expect(bodyLum).not.toBeNull();

    // The badge background must have meaningfully lower luminance than the
    // near-white body (success green vs cream) — at minimum 3:1 UI contrast.
    const ratio = contrastRatio(badgeLum!, bodyLum!);
    expect(ratio).toBeGreaterThanOrEqual(3);
  });
});

// ---------------------------------------------------------------------------
// #87 — Stat card label contrast
// ---------------------------------------------------------------------------

test.describe('Light theme — #87 Stat card label contrast', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/dashboard');
    await enableLightTheme(page);
    await waitForNoSpinner(page);
  });

  test('stat-title computed color has ≥4.5:1 contrast against stat card bg', async ({ page }) => {
    // Stat cards render with bg-base-200. The .stat-title must be legible against it.
    const statTitle = page.locator('.stat-title').first();
    await expect(statTitle).toBeVisible({ timeout: 8_000 });

    const { titleRgb, bgRgb } = await statTitle.evaluate((el): { titleRgb: string; bgRgb: string } => {
      const card = el.closest('.stat') ?? el.parentElement!;
      return {
        titleRgb: getComputedStyle(el).color,
        bgRgb: getComputedStyle(card).backgroundColor,
      };
    });

    const titleLum = relativeLuminance(titleRgb);
    const bgLum = relativeLuminance(bgRgb);

    expect(titleLum).not.toBeNull();
    expect(bgLum).not.toBeNull();

    const ratio = contrastRatio(titleLum!, bgLum!);
    // WCAG AA normal text: ≥4.5:1. Labels are small text so full AA applies.
    expect(ratio).toBeGreaterThanOrEqual(4.5);
  });
});

// ---------------------------------------------------------------------------
// #87 — Primary button text is light (white), not dark (black)
// ---------------------------------------------------------------------------

test.describe('Light theme — #87 Primary button text is light', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/timeseries');
    await enableLightTheme(page);
    await waitForNoSpinner(page);
  });

  test('primary button text computed color has high lightness (white, not black)', async ({ page }) => {
    // "Write Point" is a btn-primary. Its text must be white (light) not black.
    const primaryBtn = page.locator('button.btn-primary').first();
    await expect(primaryBtn).toBeVisible({ timeout: 8_000 });

    const { textRgb, bgRgb } = await primaryBtn.evaluate((el): { textRgb: string; bgRgb: string } => {
      const s = getComputedStyle(el);
      return { textRgb: s.color, bgRgb: s.backgroundColor };
    });

    const textLum = relativeLuminance(textRgb);
    const bgLum = relativeLuminance(bgRgb);

    expect(textLum).not.toBeNull();
    expect(bgLum).not.toBeNull();

    // The primary button bg is a saturated blue (low luminance). Its text
    // must be lighter than the bg — i.e., white-on-blue, not black-on-blue.
    expect(textLum!).toBeGreaterThan(bgLum!);

    // And the contrast ratio must meet WCAG AA for normal text.
    const ratio = contrastRatio(textLum!, bgLum!);
    expect(ratio).toBeGreaterThanOrEqual(4.5);
  });
});

// ---------------------------------------------------------------------------
// #87 — Cluster standalone badge has a colored background
// ---------------------------------------------------------------------------

test.describe('Light theme — #87 Cluster standalone badge has color', () => {
  test.beforeEach(async ({ page }) => {
    await navigateTo(page, '#/cluster');
    await enableLightTheme(page);
    await waitForNoSpinner(page);
  });

  test('standalone badge background differs from body bg (≥3:1 contrast)', async ({ page }) => {
    await waitForNoSpinner(page);

    // The badge may read "standalone" or "raft" depending on cluster config.
    const modeBadge = page.locator('.badge', { hasText: /standalone|raft/i }).first();
    await expect(modeBadge).toBeVisible({ timeout: 8_000 });

    const { badgeBgRgb, bodyBgRgb } = await modeBadge.evaluate((el): { badgeBgRgb: string; bodyBgRgb: string } => {
      return {
        badgeBgRgb: getComputedStyle(el).backgroundColor,
        bodyBgRgb: getComputedStyle(document.body).backgroundColor,
      };
    });

    const badgeLum = relativeLuminance(badgeBgRgb);
    const bodyLum = relativeLuminance(bodyBgRgb);

    expect(badgeLum).not.toBeNull();
    expect(bodyLum).not.toBeNull();

    // A non-transparent badge on a cream body must contrast at ≥3:1.
    const ratio = contrastRatio(badgeLum!, bodyLum!);
    expect(ratio).toBeGreaterThanOrEqual(3);
  });
});
