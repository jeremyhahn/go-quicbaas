/**
 * tenant_selector_layering.spec.ts
 *
 * Bug #60 regression: the top-right tenant chooser must layer ABOVE all
 * page CTAs (e.g. Register node on Fleet, Put Key on KV Store).
 *
 * Verifies:
 *   1. The header carries an effective z-index >= 1000 so the tenant
 *      control's stacking context dominates page content.
 *   2. The tenant select is anchored top-right of the viewport.
 *   3. The tenant select does not visually clip with any visible page CTA
 *      (button) in the page body — i.e. either it sits in the header
 *      band without overlap, or its z-index/stacking is higher.
 */

import { test, expect, type Page, type Locator } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

async function navigateTo(page: Page, hash: string): Promise<void> {
  await page.goto('/ui/', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  await page.waitForLoadState('networkidle');
}

interface BBox { x: number; y: number; width: number; height: number; }

function intersects(a: BBox | null, b: BBox | null): boolean {
  if (!a || !b) return false;
  if (a.width === 0 || a.height === 0 || b.width === 0 || b.height === 0) return false;
  return !(a.x + a.width <= b.x ||
           b.x + b.width <= a.x ||
           a.y + a.height <= b.y ||
           b.y + b.height <= a.y);
}

async function effectiveZIndex(locator: Locator): Promise<number> {
  // Walk up the DOM and return the highest non-auto z-index in the
  // ancestor chain — that is the effective stacking context root for
  // the element.
  return await locator.evaluate((el: Element): number => {
    let cur: Element | null = el;
    let max = 0;
    while (cur && cur !== document.body) {
      const z = window.getComputedStyle(cur as HTMLElement).zIndex;
      const n = z === 'auto' ? NaN : Number(z);
      if (!Number.isNaN(n) && n > max) max = n;
      cur = cur.parentElement;
    }
    return max;
  });
}

test.describe('Bug #60 — tenant selector z-index and viewport anchoring', () => {
  test('tenant selector header has an effective z-index >= 1000', async ({ page }) => {
    await navigateTo(page, '#/fleet');
    const tenantSelector = page.getByTestId('tenant-selector');
    await expect(tenantSelector).toBeAttached();

    const z = await effectiveZIndex(tenantSelector);
    expect(z).toBeGreaterThanOrEqual(1000);
  });

  test('tenant selector is anchored to the top-right of the viewport', async ({ page }) => {
    await navigateTo(page, '#/fleet');
    const tenantSelector = page.getByTestId('tenant-selector');
    await expect(tenantSelector).toBeVisible();

    const box = await tenantSelector.boundingBox();
    expect(box).not.toBeNull();
    if (!box) return;

    const viewport = page.viewportSize();
    expect(viewport).not.toBeNull();
    if (!viewport) return;

    // Anchored top: top edge within the first 60px (header band).
    expect(box.y).toBeLessThan(60);

    // Anchored right: right edge within the rightmost 25% of the viewport.
    const rightEdge = box.x + box.width;
    expect(rightEdge).toBeGreaterThan(viewport.width * 0.75);
  });

  test('tenant selector does not visually clip any visible page CTA on Fleet', async ({ page }) => {
    await navigateTo(page, '#/fleet');

    const tenantSelector = page.getByTestId('tenant-selector');
    await expect(tenantSelector).toBeVisible();

    const tenantBox = await tenantSelector.boundingBox();
    expect(tenantBox).not.toBeNull();
    if (!tenantBox) return;

    // Find the Register node CTA — the one this bug specifically called out.
    const registerCTA = page.getByRole('button', { name: /Register a fleet node/i }).first();
    if ((await registerCTA.count()) > 0 && await registerCTA.isVisible()) {
      const ctaBox = await registerCTA.boundingBox();
      // Either bounding boxes don't intersect (clean separation) OR
      // the tenant selector layers above (z-index >= 1000).
      const z = await effectiveZIndex(tenantSelector);
      const clipFree = !intersects(tenantBox, ctaBox);
      const layeredAbove = z >= 1000;
      expect(clipFree || layeredAbove).toBe(true);
    }
  });

  test('tenant selector does not visually clip any visible page CTA on KV Store', async ({ page }) => {
    await navigateTo(page, '#/kv');

    const tenantSelector = page.getByTestId('tenant-selector');
    await expect(tenantSelector).toBeVisible();

    const tenantBox = await tenantSelector.boundingBox();
    expect(tenantBox).not.toBeNull();
    if (!tenantBox) return;

    const z = await effectiveZIndex(tenantSelector);

    // Walk every visible button on the page; if any intersects the
    // tenant selector's bounding box, the selector MUST layer above.
    const buttons = page.locator('button:visible');
    const count = await buttons.count();
    for (let i = 0; i < count; i++) {
      const btn = buttons.nth(i);
      const bbox = await btn.boundingBox().catch(() => null);
      if (!bbox) continue;
      if (intersects(tenantBox, bbox)) {
        // Overlap is fine if the selector stacks above.
        expect(z).toBeGreaterThanOrEqual(1000);
      }
    }
  });
});
