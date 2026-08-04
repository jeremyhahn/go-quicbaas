/**
 * session_lifecycle.spec.ts
 *
 * Full session lifecycle E2E tests.  These require an ACP backend (claude,
 * codex, or opencode) installed on PATH because they actually spawn a session
 * and interact with it.
 *
 * Project guard: this file is ONLY included in the "chromium-acp" Playwright
 * project (see playwright.config.ts).  Running `make test-e2e` (the default
 * no-backend project) never touches this file.
 *
 * To run:
 *   cd frontend && QUICBAAS_E2E_BACKEND=acp \
 *     QUICBAAS_UI_URL=https://localhost:18080/ui/ \
 *     NODE_EXTRA_CA_CERTS=/tmp/quicbaas-chrometest-data/certs/ca-chain.crt \
 *     npx playwright test --project=chromium-acp
 *
 * Or via Makefile:
 *   make test-e2e-acp
 *
 * The daemon must be started with an ACP backend registered:
 *   ./bin/quicbaasd start --http-port 18080 --grpc-port 0 \
 *     --data-dir /tmp/quicbaas-chrometest-data --log-level info \
 *     --embedded-agent --embedded-agent-backend claude
 */

import { test, expect } from '@playwright/test';
import { spawnTestSession, closeTestSession } from './fixtures/api.js';

// Serial mode so each test builds on the previous session state.
test.describe.configure({ mode: 'serial' });

const BASE = process.env['QUICBAAS_UI_URL'] ?? 'https://localhost:18080/ui/';

// Verify QUICBAAS_E2E_BACKEND=acp is set.  If not, fail with a clear message
// rather than silently passing hollow tests.
if (!process.env['QUICBAAS_E2E_BACKEND']) {
  throw new Error(
    'session_lifecycle.spec.ts requires QUICBAAS_E2E_BACKEND=acp.\n' +
      'Run: make test-e2e-acp  (not make test-e2e).\n' +
      'This file must only be executed under the chromium-acp Playwright project.',
  );
}

test.use({ ignoreHTTPSErrors: true });

test.describe('Session lifecycle — requires ACP backend', () => {
  let sessionID = '';

  test.beforeAll(async () => {
    const spawned = await spawnTestSession();
    sessionID = spawned.session_id;
  });

  test.afterAll(async () => {
    if (sessionID) {
      await closeTestSession(sessionID);
    }
  });

  test('1. Session row appears in table after spawn', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });

    // Wait until the spinner disappears.
    await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });

    // The seeded session must appear as a clickable row.
    const rowSelector = '[role="button"][aria-label^="Open session"]';
    await expect(page.locator(rowSelector).first()).toBeVisible({ timeout: 10_000 });

    // Sessions counter must show at least one session.
    await expect(page.locator('text=/[1-9]\\d* of [1-9]\\d* sessions/').first()).toBeVisible();
  });

  test('2. Clicking a session row opens detail view', async ({ page }) => {
    await page.goto(`${BASE}#/sessions`, { waitUntil: 'networkidle' });

    const rowSelector = '[role="button"][aria-label^="Open session"]';
    await expect(page.locator(rowSelector).first()).toBeVisible({ timeout: 10_000 });

    await page.locator(rowSelector).first().click();
    // URL should change to #/sessions/:id.
    await expect(page).toHaveURL(/#\/sessions\/.+/, { timeout: 5_000 });
  });

  test('3. Session detail shows event stream area', async ({ page }) => {
    await page.goto(`${BASE}#/sessions/${sessionID}`, { waitUntil: 'networkidle' });
    await expect(page.locator('[role="log"]')).toBeVisible({ timeout: 10_000 });
  });

  test('4. Scroll to top loads history sentinel', async ({ page }) => {
    await page.goto(`${BASE}#/sessions/${sessionID}`, { waitUntil: 'networkidle' });

    const log = page.locator('[role="log"]');
    await expect(log).toBeVisible({ timeout: 10_000 });

    // Scroll to top inside the log area.
    await log.evaluate((el) => {
      el.scrollTop = 0;
    });

    // Either "Beginning of session" or "Scroll up to load history" must appear.
    // The EventStream renders one of these sentinels at the top of the log
    // (EventStream.svelte): "Beginning of session" when history is exhausted, or
    // "Scroll up to load history" when more pages remain.
    await expect(
      page.getByText(/Beginning of session|Scroll up to load history/).first(),
    ).toBeVisible({ timeout: 5_000 });
  });

  test('5. Submit a prompt and see it appear in the event stream', async ({ page }) => {
    await page.goto(`${BASE}#/sessions/${sessionID}`, { waitUntil: 'networkidle' });

    const composer = page.locator('[aria-label="Prompt input"]');
    await expect(composer).toBeVisible({ timeout: 10_000 });

    await composer.fill('ping');
    await page.locator('[aria-label="Send prompt"]').click();

    // The prompt event must appear in the event stream.
    await expect(page.locator('text=ping').first()).toBeVisible({ timeout: 15_000 });
  });

  test('6. Session detail shows Prompt input and Send button', async ({ page }) => {
    await page.goto(`${BASE}#/sessions/${sessionID}`, { waitUntil: 'networkidle' });

    await expect(page.locator('[aria-label="Prompt input"]')).toBeVisible({ timeout: 10_000 });
    await expect(page.locator('[aria-label="Send prompt"]')).toBeVisible();
  });

  test('7. Close session reflects in UI', async ({ page }) => {
    await page.goto(`${BASE}#/sessions/${sessionID}`, { waitUntil: 'networkidle' });

    const closeBtn = page.locator('[aria-label="Close session"]');
    await expect(closeBtn).toBeVisible({ timeout: 10_000 });

    // Closing is a confirm flow: the header button opens a confirm dialog; the
    // confirm button actually closes the session.
    await closeBtn.click();
    await page.locator('[aria-label="Confirm close session"]').click();

    // After closing, the SessionClosedEvent banner or "session is closed" must appear.
    await expect(
      page.getByText(/Session ended|session is closed/).first(),
    ).toBeVisible({ timeout: 15_000 });

    // The session was closed by the UI — suppress afterAll cleanup error.
    sessionID = '';
  });
});
