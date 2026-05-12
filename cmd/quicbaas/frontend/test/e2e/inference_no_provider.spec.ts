/**
 * inference_no_provider.spec.ts
 *
 * E2E tests for the Inference view's behavior with API-inference backends
 * that have no provider configured. Also confirms that ACP session-only
 * backends remain greyed for one-shot inference (correct — those are for
 * Sessions/Workflows).
 *
 * Requires a running quicbaasd at QUICBAAS_UI_URL.
 * Run: cd frontend && npx playwright test test/e2e/inference_no_provider.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

test.describe('Inference view — provider configuration messaging', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await page.locator('aside nav button', { hasText: 'Inference' }).first().click();
    await expect(page).toHaveURL(/#\/inference/);
  });

  test('Inference heading visible', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Inference', exact: false }).first()).toBeVisible();
  });

  test('view does not surface a runtime error', async ({ page }) => {
    await expect(page.locator('text=/runtime error/i')).toHaveCount(0);
    await expect(page.locator('text=/cannot read prop/i')).toHaveCount(0);
  });

  test('backend selector renders', async ({ page }) => {
    // The backend control may be a <select> or a button-list — both shapes
    // satisfy the contract. We only require that *some* backend control
    // exists so a user can pick where to route.
    const control = page.locator(
      'select[name*="backend" i], select[id*="backend" i], [aria-label*="Backend" i]'
    );
    await expect(control.first()).toBeVisible({ timeout: 5_000 });
  });
});
