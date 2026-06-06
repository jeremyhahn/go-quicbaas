/**
 * bootstrap_activate.spec.ts
 *
 * Playwright tests for the invite-activation flow that renders
 * InviteActivateView.svelte at the hash route #/invite/:token.
 *
 * Background: when the daemon boots fresh (no admin assigned) it prints a
 * one-time activation URL containing a hex invite token. That bootstrap
 * URL uses the path #/activate?token=<hex>, but the SPA router maps the
 * invite-activation view at #/invite/:token (path parameter, not query
 * string). This test suite uses the standard IAM user-creation API
 * (POST /api/v1/tenants/default/users) to seed a deterministic invite
 * token in the daemon's in-memory invite store, then exercises the UI via
 * the #/invite/:token route.
 *
 * Tests:
 *   1. Navigate to #/invite/<known-good-token>; activation view renders.
 *   2. Activation view shows the invited user's name/email from invite metadata.
 *   3. WebAuthn registration button is visible (registration not completed —
 *      that requires hardware).
 *   4. Invalid token (random 64-char hex string) shows an error state.
 *
 * Placement: chromium-no-backend project (no ACP backend needed).
 */

import { test, expect, type Page } from '@playwright/test';
import { createTestInvite, deleteTestUser, getInviteMetadata } from './fixtures/api.ts';
import type { CreateUserResponse } from './fixtures/api.ts';

test.use({ ignoreHTTPSErrors: true });

// ---------------------------------------------------------------------------
// Shared state — populated in beforeAll.
// ---------------------------------------------------------------------------

let invite: CreateUserResponse | null = null;

// ---------------------------------------------------------------------------
// Navigation helper
// ---------------------------------------------------------------------------

async function gotoInvite(page: Page, token: string): Promise<void> {
  // Navigate to the app root first, then switch to the invite hash so that
  // the SPA is fully mounted before the route change fires.
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate((tok: string) => {
    window.location.hash = `#/invite/${tok}`;
  }, token);
  await page.waitForLoadState('networkidle');
}

// ---------------------------------------------------------------------------
// beforeAll: create a fresh invite in the daemon's in-memory store.
// ---------------------------------------------------------------------------

test.beforeAll(async () => {
  // Create a pending user; this seeds an invite token in the in-memory store.
  invite = await createTestInvite('default', 'e2e-activate@quicbaas.test', 'E2E Activate Test');

  // Hard-assert the token is accessible via the invite metadata endpoint.
  const meta = await getInviteMetadata(invite.invite_token);
  expect(meta).not.toBeNull();
  expect(meta?.email).toContain('e2e-activate');
});

// ---------------------------------------------------------------------------
// Teardown: remove the invite user created in beforeAll so it does not
// appear in the IAM Users table across test runs.
// ---------------------------------------------------------------------------

test.afterAll(async () => {
  if (invite?.id) {
    await deleteTestUser('default', invite.id);
  }
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Security > Invite Activation — bootstrap activate flow', () => {

  test('1. #/invite/<token> renders the activation view', async ({ page }) => {
    const token = invite?.invite_token ?? '';
    expect(token).not.toBe('');

    await gotoInvite(page, token);

    // The InviteActivateView renders an "Activate Your Account" heading.
    await expect(
      page.getByRole('heading', { name: /Activate Your Account/i }),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('2. Activation view shows invited user email/name from invite metadata', async ({ page }) => {
    const token = invite?.invite_token ?? '';
    await gotoInvite(page, token);

    // Wait for the loading spinner to clear (invite metadata fetch completes).
    await expect(page.locator('.loading')).toHaveCount(0, { timeout: 10_000 });

    // Email is shown in the invite-info block.
    await expect(page.getByText(/e2e-activate/i)).toBeVisible({ timeout: 5_000 });
    // Name is also shown.
    await expect(page.getByText(/E2E Activate Test/i)).toBeVisible({ timeout: 5_000 });
  });

  test('3. WebAuthn registration button is visible', async ({ page }) => {
    const token = invite?.invite_token ?? '';
    await gotoInvite(page, token);

    await expect(page.locator('.loading')).toHaveCount(0, { timeout: 10_000 });

    // The Register Security Key / Passkey button must be present.
    await expect(
      page.getByRole('button', { name: /Register Security Key/i }),
    ).toBeVisible({ timeout: 5_000 });
  });

  test('4. Invalid token shows an error state', async ({ page }) => {
    // Use a syntactically valid 64-hex-char token that does not exist.
    const badToken = 'deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef';
    await gotoInvite(page, badToken);

    await expect(page.locator('.loading')).toHaveCount(0, { timeout: 10_000 });

    // The error alert must be visible. The server returns 404 → the SDK/view
    // sets phase='error' and shows the loadError message.
    await expect(page.getByRole('alert')).toBeVisible({ timeout: 5_000 });
    // The error message must be non-empty.
    const alertText = await page.getByRole('alert').textContent();
    expect((alertText ?? '').length).toBeGreaterThan(0);
  });

});
