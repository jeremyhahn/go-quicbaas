/**
 * rapid_nav_no_duplicate_keys.spec.ts
 *
 * Regression test for https://svelte.dev/e/each_key_duplicate.
 *
 * Symptom: navigating rapidly through multiple views fires a Svelte 5
 * each_key_duplicate console exception — a keyed {#each} block briefly
 * receives two items with the same key.
 *
 * This test FAILS before the fix and PASSES after.
 *
 * Run:
 *   cd frontend && npx playwright test test/e2e/rapid_nav_no_duplicate_keys.spec.ts
 */

import { test, expect } from '@playwright/test';

test.use({ ignoreHTTPSErrors: true });

/**
 * All hash routes that the sidebar can navigate to, in the order used to
 * reproduce the bug (matches the sequence in the issue report).
 */
const routes = [
  '#/iam',
  '#/secrets',
  '#/barrier',
  '#/ceremony',
  '#/bootstrap',
  '#/scheduler',
  '#/messaging',
  '#/embeddings',
  '#/rag',
  '#/vectors',
  '#/timeseries',
  '#/objects',
  '#/sessions',
  '#/jobs',
  '#/tasks',
  '#/workflows',
  '#/cost',
  '#/events',
  '#/kv',
  '#/kms',
  '#/ssh-keys',
  '#/inference',
  '#/fleet',
] as const;

test.describe('Rapid navigation — no each_key_duplicate', () => {
  test('navigating through all routes rapidly emits no each_key_duplicate error', async ({
    page,
  }) => {
    const duplicateKeyErrors: string[] = [];

    // Capture every console error that mentions each_key_duplicate.
    page.on('console', (msg) => {
      if (msg.type() === 'error' || msg.type() === 'warning') {
        const text = msg.text();
        if (text.includes('each_key_duplicate') || text.includes('duplicate key')) {
          duplicateKeyErrors.push(text);
        }
      }
    });

    // Also capture uncaught page errors thrown as exceptions.
    page.on('pageerror', (err) => {
      if (
        err.message.includes('each_key_duplicate') ||
        err.message.includes('duplicate key')
      ) {
        duplicateKeyErrors.push(err.message);
      }
    });

    await page.goto('/ui/', { waitUntil: 'domcontentloaded' });

    // Navigate through every route without pausing — this replicates the
    // rapid back-to-back navigation that surfaces stale store state.
    for (const route of routes) {
      await page.evaluate((hash: string) => {
        window.location.hash = hash;
      }, route);
      // 50 ms is enough for Svelte to process the route change and reconcile
      // the keyed {#each} blocks; it is far too short for API responses to
      // arrive, which is intentional — we want the store state mid-flight.
      await page.waitForTimeout(50);
    }

    // A final round-trip at 250 ms intervals to also catch errors that only
    // fire when enough time has passed for in-flight API responses to land.
    for (const route of routes) {
      await page.evaluate((hash: string) => {
        window.location.hash = hash;
      }, route);
      await page.waitForTimeout(250);
    }

    // Wait for all pending microtasks / effects to flush.
    await page.waitForTimeout(500);

    expect(
      duplicateKeyErrors,
      `each_key_duplicate fired during rapid navigation:\n${duplicateKeyErrors.join('\n')}`,
    ).toHaveLength(0);
  });

  test('repeated single-view reload emits no each_key_duplicate error', async ({
    page,
  }) => {
    const duplicateKeyErrors: string[] = [];

    page.on('console', (msg) => {
      if (msg.type() === 'error' || msg.type() === 'warning') {
        const text = msg.text();
        if (text.includes('each_key_duplicate') || text.includes('duplicate key')) {
          duplicateKeyErrors.push(text);
        }
      }
    });

    page.on('pageerror', (err) => {
      if (
        err.message.includes('each_key_duplicate') ||
        err.message.includes('duplicate key')
      ) {
        duplicateKeyErrors.push(err.message);
      }
    });

    await page.goto('/ui/', { waitUntil: 'domcontentloaded' });

    // Navigate to each "data-heavy" view twice in rapid succession to exercise
    // the mount → unmount → remount cycle.
    const dataHeavyRoutes = [
      '#/sessions',
      '#/fleet',
      '#/jobs',
      '#/tasks',
      '#/iam',
    ];

    for (const route of dataHeavyRoutes) {
      await page.evaluate((hash: string) => { window.location.hash = hash; }, route);
      await page.waitForTimeout(100);
      await page.evaluate(() => { window.location.hash = '#/dashboard'; });
      await page.waitForTimeout(100);
      await page.evaluate((hash: string) => { window.location.hash = hash; }, route);
      await page.waitForTimeout(300);
    }

    await page.waitForTimeout(500);

    expect(
      duplicateKeyErrors,
      `each_key_duplicate fired during repeated single-view navigation:\n${duplicateKeyErrors.join('\n')}`,
    ).toHaveLength(0);
  });
});
