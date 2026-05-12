/**
 * renderer_freeze_reproducer.spec.ts
 *
 * Reproduces the CDP screenshot-timeout freeze that real Chromium users hit on:
 *   - /ui/#/kms
 *   - /ui/#/rag
 *   - /ui/#/embeddings
 *
 * Why existing tests miss it:
 *   Standard `goto(url, { waitUntil: 'load' })` + element assertions complete
 *   before the page enters the freeze state. The Chrome MCP DevTools-Protocol
 *   extension calls `Page.captureScreenshot` AFTER navigation returns, which
 *   waits for the renderer to finish painting — exactly where the freeze occurs.
 *
 * Reproduction method:
 *   1. Navigate with `waitUntil: 'networkidle'` (matches CDP's deeper wait).
 *   2. Call `page.screenshot({ fullPage: true })` with a 5 s timeout.
 *   3. If the renderer is frozen the screenshot times out and the test fails.
 *
 * All three URLs must produce a screenshot within 5 s after networkidle.
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Helper: navigate and take a screenshot, failing fast on timeout.
// ---------------------------------------------------------------------------

async function screenshotWithin5s(page: Page, hash: string): Promise<Buffer> {
  await page.goto('/ui/', { waitUntil: 'networkidle' });
  await page.evaluate((h: string) => { window.location.hash = h; }, hash);
  // networkidle after hash navigation ensures all in-flight XHRs have settled.
  await page.waitForLoadState('networkidle');

  // This is the exact operation that the Chrome MCP extension performs.
  // A frozen renderer will cause this to time out.
  const buf = await page.screenshot({ fullPage: true, timeout: 5_000 });
  return buf;
}

// ---------------------------------------------------------------------------
// Freeze-reproducer tests (must all complete within 5 s)
// ---------------------------------------------------------------------------

test.describe('CDP screenshot freeze — KMS / RAG / Embeddings', () => {
  test('KMS view — screenshot completes within 5 s after networkidle', async ({ page }) => {
    const buf = await screenshotWithin5s(page, '#/kms');
    expect(buf.length).toBeGreaterThan(0);
    // The KMS heading must be present in the rendered page.
    await expect(page.getByRole('heading', { name: 'KMS' })).toBeVisible({ timeout: 2_000 });
  });

  test('RAG view — screenshot completes within 5 s after networkidle', async ({ page }) => {
    const buf = await screenshotWithin5s(page, '#/rag');
    expect(buf.length).toBeGreaterThan(0);
    // Use exact: true to avoid matching nested headings like "Query RAG".
    await expect(page.getByRole('heading', { name: 'RAG', exact: true }).first()).toBeVisible({ timeout: 2_000 });
  });

  test('Embeddings view — screenshot completes within 5 s after networkidle', async ({ page }) => {
    const buf = await screenshotWithin5s(page, '#/embeddings');
    expect(buf.length).toBeGreaterThan(0);
    await expect(page.getByRole('heading', { name: 'Embeddings' })).toBeVisible({ timeout: 2_000 });
  });

  // ---------------------------------------------------------------------------
  // Direct-navigate variant: goto the hash URL directly (matches the MCP flow
  // which opens a new tab to the full URL including the hash fragment).
  // ---------------------------------------------------------------------------

  test('KMS view — direct URL navigate screenshot within 5 s', async ({ page }) => {
    await page.goto('/ui/#/kms', { waitUntil: 'networkidle' });
    const buf = await page.screenshot({ fullPage: true, timeout: 5_000 });
    expect(buf.length).toBeGreaterThan(0);
  });

  test('RAG view — direct URL navigate screenshot within 5 s', async ({ page }) => {
    await page.goto('/ui/#/rag', { waitUntil: 'networkidle' });
    const buf = await page.screenshot({ fullPage: true, timeout: 5_000 });
    expect(buf.length).toBeGreaterThan(0);
  });

  test('Embeddings view — direct URL navigate screenshot within 5 s', async ({ page }) => {
    await page.goto('/ui/#/embeddings', { waitUntil: 'networkidle' });
    const buf = await page.screenshot({ fullPage: true, timeout: 5_000 });
    expect(buf.length).toBeGreaterThan(0);
  });
});
