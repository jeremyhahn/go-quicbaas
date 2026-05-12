/**
 * ai_async_surfaces.spec.ts
 *
 * Smoke-level Playwright tests for the four AI/ML and ASYNC sidebar surfaces:
 *   AI/ML:  RAG (#/rag), Embeddings (#/embeddings)
 *   ASYNC:  Messaging (#/messaging), Scheduler (#/scheduler)
 *
 * Each test navigates to the route via the sidebar, asserts the page heading
 * is visible, and checks that at least one primary action button is present.
 *
 * These tests require a running quicbaasd at QUICBAAS_UI_URL.
 * They do NOT perform real API calls — they only assert the UI renders.
 *
 * Run:
 *   cd frontend && npx playwright test test/e2e/ai_async_surfaces.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Surface definitions
// ---------------------------------------------------------------------------

const aiAsyncSurfaces = [
  {
    section:     'AI/ML',
    label:       'RAG',
    hash:        '#/rag',
    heading:     'RAG',
    crudButton:  'Ingest Document',
  },
  {
    section:     'AI/ML',
    label:       'Embeddings',
    hash:        '#/embeddings',
    heading:     'Embeddings',
    crudButton:  'Generate Embedding',
  },
  {
    section:     'ASYNC',
    label:       'Messaging',
    hash:        '#/messaging',
    heading:     'Messaging',
    crudButton:  'Create Channel',
  },
  {
    section:     'ASYNC',
    label:       'Scheduler',
    hash:        '#/scheduler',
    heading:     'Scheduler',
    crudButton:  'Create Job',
  },
] as const;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type Page = Parameters<Parameters<typeof test>[1]>[0];

async function navigateSurface(
  page: Page,
  label: string,
  hash: string,
  heading: string,
  crudButton: string,
): Promise<void> {
  const btn = page.locator('aside nav button', { hasText: label }).first();
  await expect(btn).toBeVisible();
  await btn.click();

  await expect(page).toHaveURL(new RegExp(hash.replace('/', '\\/').replace('#', '#?')));
  await expect(page.getByRole('heading', { name: heading, exact: true }).first()).toBeVisible();
  await expect(
    page.getByRole('button', { name: new RegExp(crudButton, 'i') }).first(),
  ).toBeVisible();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('AI/ML and ASYNC sidebar surfaces — smoke', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/ui/', { waitUntil: 'networkidle' });
    await expect(page.locator('body')).not.toBeEmpty();
  });

  for (const surf of aiAsyncSurfaces) {
    test(`${surf.section} > ${surf.label} — heading and primary button visible`, async ({ page }) => {
      await navigateSurface(page, surf.label, surf.hash, surf.heading, surf.crudButton);
    });
  }

  test('AI/ML and ASYNC sidebar items have no "soon" badge', async ({ page }) => {
    for (const surf of aiAsyncSurfaces) {
      const btn = page.locator('aside nav button', { hasText: surf.label }).first();
      const soonBadge = btn.locator('.badge', { hasText: 'soon' });
      await expect(soonBadge).toHaveCount(0);
    }
  });

  test('RAG breadcrumb shows AI/ML > RAG', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'RAG' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('AI/ML');
    await expect(breadcrumb).toContainText('RAG');
  });

  test('Embeddings breadcrumb shows AI/ML > Embeddings', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Embeddings' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('AI/ML');
    await expect(breadcrumb).toContainText('Embeddings');
  });

  test('Messaging breadcrumb shows Async > Messaging', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Messaging' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Async');
    await expect(breadcrumb).toContainText('Messaging');
  });

  test('Scheduler breadcrumb shows Async > Scheduler', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Scheduler' }).first().click();
    const breadcrumb = page.locator('.breadcrumbs');
    await expect(breadcrumb).toContainText('Async');
    await expect(breadcrumb).toContainText('Scheduler');
  });

  test('RAG — "Run Query" button is visible on load', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'RAG' }).first().click();
    await expect(page.getByRole('button', { name: /Run Query/i }).first()).toBeVisible();
  });

  test('RAG — clicking "Ingest Document" opens a dialog', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'RAG' }).first().click();
    await page.getByRole('button', { name: /Ingest Document/i }).first().click();
    await expect(page.getByRole('dialog', { name: /Ingest Document/i })).toBeVisible();
  });

  test('Scheduler — clicking "Create Job" opens a dialog', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Scheduler' }).first().click();
    await page.getByRole('button', { name: /Create Job/i }).first().click();
    await expect(page.getByRole('dialog', { name: /Create Job/i })).toBeVisible();
  });

  test('Messaging — clicking "Create Channel" opens a dialog', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Messaging' }).first().click();
    await page.getByRole('button', { name: /Create Channel/i }).first().click();
    await expect(page.getByRole('dialog', { name: /Create Channel/i })).toBeVisible();
  });

  test('Embeddings — "Generate" button is visible on load', async ({ page }) => {
    await page.locator('aside nav button', { hasText: 'Embeddings' }).first().click();
    await expect(page.getByRole('button', { name: /Generate Embedding/i }).first()).toBeVisible();
  });
});
