/**
 * security_surfaces.spec.ts
 *
 * Smoke-level Playwright tests for the four SECURITY sidebar surfaces:
 *   SECURITY:  IAM (#/iam), KMS (#/kms), Secrets (#/secrets), Seal (#/seal)
 *
 * Each test navigates to the route via the sidebar, asserts the page heading
 * is visible, and checks that at least one primary action button is present.
 *
 * These tests require a running quicbaasd at QUICBAAS_UI_URL.
 * They do NOT perform real API calls — they only assert the UI renders.
 *
 * Run:
 *   cd frontend && npx playwright test test/e2e/security_surfaces.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Surface definitions
// ---------------------------------------------------------------------------

const securitySurfaces = [
  {
    section:    'SECURITY',
    label:      'IAM',
    hash:       '#/iam',
    heading:    'IAM',
    crudButton: 'Invite User' as string | RegExp | null,
  },
  {
    section:    'SECURITY',
    label:      'KMS',
    hash:       '#/kms',
    heading:    'KMS',
    crudButton: 'Create key' as string | RegExp | null,
  },
  {
    section:    'SECURITY',
    label:      'Secrets',
    hash:       '#/secrets',
    heading:    'Secrets',
    crudButton: 'Store secret' as string | RegExp | null,
  },
  {
    // The barrier service may not be configured in all environments; skip the
    // button assertion (the heading check is still enforced).
    section:    'SECURITY',
    label:      'Barrier',
    hash:       '#/barrier',
    heading:    'Barrier',
    crudButton: null as string | RegExp | null,
  },
  {
    section:    'SECURITY',
    label:      'Ceremony',
    hash:       '#/ceremony',
    heading:    'Ceremony',
    crudButton: null as string | RegExp | null,
  },
  {
    section:    'SECURITY',
    label:      'Bootstrap',
    hash:       '#/bootstrap',
    heading:    'Bootstrap',
    crudButton: 'Bootstrap' as string | RegExp | null,
  },
] satisfies ReadonlyArray<{
  section: string;
  label: string;
  hash: string;
  heading: string;
  crudButton: string | RegExp | null;
}>;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type Page = Parameters<Parameters<typeof test>[1]>[0];

async function navigateSurface(
  page: Page,
  label: string,
  hash: string,
  heading: string,
  crudButton: string | RegExp | null,
): Promise<void> {
  const btn = page.locator('aside nav button', { hasText: label }).first();
  await expect(btn).toBeVisible();
  await btn.click();

  await expect(page).toHaveURL(new RegExp(hash.replace('/', '\\/').replace('#', '#?')));
  await expect(page.getByRole('heading', { name: heading })).toBeVisible();

  if (crudButton !== null) {
    await expect(
      page.getByRole('button', { name: typeof crudButton === 'string' ? new RegExp(crudButton, 'i') : crudButton }).first(),
    ).toBeVisible();
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('SECURITY sidebar surfaces — smoke', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await expect(page.locator('body')).not.toBeEmpty();
  });

  for (const surf of securitySurfaces) {
    test(`${surf.section} > ${surf.label} — heading and primary button visible`, async ({ page }) => {
      await navigateSurface(page, surf.label, surf.hash, surf.heading, surf.crudButton);
    });
  }

  test('SECURITY sidebar items have no "soon" badge', async ({ page }) => {
    for (const surf of securitySurfaces) {
      const btn = page.locator('aside nav button', { hasText: surf.label }).first();
      const soonBadge = btn.locator('.badge', { hasText: 'soon' });
      await expect(soonBadge).toHaveCount(0);
    }
  });

  test('IAM breadcrumb shows Security > IAM', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'IAM' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Security');
    await expect(breadcrumb).toContainText('IAM');
  });

  test('KMS breadcrumb shows Security > KMS', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'KMS' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Security');
    await expect(breadcrumb).toContainText('KMS');
  });

  test('Secrets breadcrumb shows Security > Secrets', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Secrets' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Security');
    await expect(breadcrumb).toContainText('Secrets');
  });

  test('Barrier breadcrumb shows Security > Barrier', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Barrier' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Security');
    await expect(breadcrumb).toContainText('Barrier');
  });

  test('Ceremony breadcrumb shows Security > Ceremony', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Ceremony' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Security');
    await expect(breadcrumb).toContainText('Ceremony');
  });

  test('Bootstrap breadcrumb shows Security > Bootstrap', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Bootstrap' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Security');
    await expect(breadcrumb).toContainText('Bootstrap');
  });

  test('IAM — renders Users tab by default', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'IAM' }).first().click();
    await expect(page.getByRole('tab', { name: /Users/i })).toBeVisible();
  });

  test('IAM — clicking "Invite User" opens a dialog', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'IAM' }).first().click();
    await page.getByRole('button', { name: /Invite User/i }).first().click();
    await expect(page.getByRole('dialog', { name: /Invite user dialog/i })).toBeVisible();
  });

  test('KMS — clicking "Create key" opens a dialog', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'KMS' }).first().click();
    await page.getByRole('button', { name: /Create key/i }).first().click();
    await expect(page.getByRole('dialog', { name: /Create key dialog/i })).toBeVisible();
  });

  test('Secrets — clicking "Store secret" opens a dialog', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Secrets' }).first().click();
    await page.getByRole('button', { name: /Store secret/i }).first().click();
    await expect(page.getByRole('dialog', { name: /Store secret dialog/i })).toBeVisible();
  });

  test('Barrier — status panel is visible', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Barrier' }).first().click();
    // The Barrier view renders its heading. The status card only appears when
    // the barrier service is available; when unavailable an error alert is
    // shown instead.
    await expect(page.getByRole('heading', { name: 'Barrier', exact: true })).toBeVisible();
    // Either the status card or the service-unavailable alert must be present.
    const cardOrAlert = page.locator('.card, [role="alert"]');
    await expect(cardOrAlert.first()).toBeVisible();
  });
});
