/**
 * bootstrap_activate.spec.ts
 *
 * Playwright tests for the invite-activation flow that renders
 * InviteActivateView.svelte at the hash route #/invite/:token.
 *
 * Background: when the daemon boots fresh (no admin assigned) it prints a
 * one-time activation URL containing a hex invite token. The banner and the
 * SPA router now agree on the single canonical route #/invite/:token (path
 * parameter). A prior bug printed the dead query-string form
 * #/activate?token=<hex>, which the router never matched, silently bouncing
 * the operator to #/login; that is fixed (pkg/server/iam_init.go) and guarded
 * by a Go unit test (TestFirstBootBootstrap_ActivationURLUsesCanonicalInviteRoute).
 * This suite uses the standard IAM user-creation API
 * (POST /api/v1/tenants/system/users) to seed a deterministic invite token in
 * the daemon's in-memory invite store, then exercises the UI via the
 * #/invite/:token route.
 *
 * Tests:
 *   1. Navigate to #/invite/<known-good-token>; activation view renders.
 *   2. Activation view shows the invited user's name/email from invite metadata.
 *   3. WebAuthn registration button is visible (registration not completed —
 *      that requires hardware).
 *   4. Invalid token (random 64-char hex string) shows an error state.
 *   5. The dead #/activate?token= route is NOT mapped (bounces to login).
 *
 * Placement: chromium-no-backend project (no ACP backend needed).
 */

import { test, expect, type Page } from '@playwright/test';
import { createTestInvite, deleteTestUser, getInviteMetadata, SYSTEM_TENANT_ID } from './fixtures/api.ts';
import type { CreateUserResponse } from './fixtures/api.ts';

// This suite exercises the LOGGED-OUT invite-activation and passkey-login flow,
// so it must NOT inherit the admin storageState the live project applies by
// default — start every page from a clean, unauthenticated browser.
test.use({ ignoreHTTPSErrors: true, storageState: { cookies: [], origins: [] } });

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
  invite = await createTestInvite(SYSTEM_TENANT_ID, 'e2e-activate@quicbaas.test', 'E2E Activate Test');

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
    await deleteTestUser(SYSTEM_TENANT_ID, invite.id);
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

    // Security regression guard: the invite-activation page is public and
    // unauthenticated — it must render WITHOUT the app shell's sidebar/nav.
    // Disclosing the feature sidebar to an anonymous visitor on this page
    // (and firing the authenticated calls a click would trigger) is the bug
    // this suite pins.
    await expect(page.locator('aside')).toHaveCount(0);
    await expect(page.getByRole('navigation')).toHaveCount(0);
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

  test('6. completes FIDO2 registration with a virtual authenticator', async ({ page }) => {
    // End-to-end guard for the WebAuthn serialisation bug: the view must unwrap
    // the server's { publicKey: { challenge, ... } } envelope, base64url-decode
    // the challenge / user.id for navigator.credentials.create(), and base64url-
    // re-encode the attestation (rawId / attestationObject / clientDataJSON) so
    // the go-webauthn finish parser accepts it. A regression throws the
    // "challenge ... Required member is undefined" DOMException before any
    // network call, so reaching the success state proves the fix end-to-end.
    //
    // A fresh invite is used because activation consumes the token (one-time).
    const fresh = await createTestInvite(
      SYSTEM_TENANT_ID,
      'e2e-activate-vauth@quicbaas.test',
      'E2E VAuth Register',
    );

    try {
      // Attach a CDP virtual authenticator: CTAP2 / internal transport with user
      // verification always satisfied and presence auto-simulated so create()
      // resolves without human interaction.
      const client = await page.context().newCDPSession(page);
      await client.send('WebAuthn.enable');
      const { authenticatorId } = await client.send('WebAuthn.addVirtualAuthenticator', {
        options: {
          protocol: 'ctap2',
          transport: 'internal',
          hasResidentKey: true,
          hasUserVerification: true,
          isUserVerified: true,
          automaticPresenceSimulation: true,
        },
      });

      // Capture the finish-call HTTP status to assert a 2xx server acceptance.
      const finishStatus = page.waitForResponse((resp) =>
        resp.url().includes(`/activate/fido2/finish`) && resp.request().method() === 'POST',
      );

      await gotoInvite(page, fresh.invite_token);
      await expect(page.locator('.loading')).toHaveCount(0, { timeout: 10_000 });

      await page.getByRole('button', { name: /Register Security Key/i }).click();

      // The success alert renders the activation message from the server.
      await expect(page.getByRole('alert')).toBeVisible({ timeout: 15_000 });
      await expect(page.getByText(/sign in to your account/i)).toBeVisible({ timeout: 5_000 });

      const resp = await finishStatus;
      expect(resp.status()).toBeGreaterThanOrEqual(200);
      expect(resp.status()).toBeLessThan(300);

      await client.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId });
    } finally {
      if (fresh.id) {
        await deleteTestUser(SYSTEM_TENANT_ID, fresh.id);
      }
    }
  });

  test('7. passkey LOGIN after registration lands in the app (no silent reset)', async ({ page }) => {
    // End-to-end guard for the silent-login-reset bug: a fully successful
    // passkey login (begin 200 → credentials.get → finish 200 with tokens)
    // must establish the in-tab session and leave the user inside the app
    // shell. The defect persisted the token but never flipped MainApp's in-tab
    // auth status, so the route guard bounced the user straight back to the
    // login chooser despite a 200 finish. Reaching an authenticated app view
    // (Sign out control present, login chooser gone) proves the fix.
    //
    // A fresh invite is registered with a virtual authenticator so the same
    // authenticator can satisfy the subsequent login assertion.
    const fresh = await createTestInvite(
      SYSTEM_TENANT_ID,
      'e2e-login-vauth@quicbaas.test',
      'E2E VAuth Login',
    );

    try {
      const client = await page.context().newCDPSession(page);
      await client.send('WebAuthn.enable');
      const { authenticatorId } = await client.send('WebAuthn.addVirtualAuthenticator', {
        options: {
          protocol: 'ctap2',
          transport: 'internal',
          hasResidentKey: true,
          hasUserVerification: true,
          isUserVerified: true,
          automaticPresenceSimulation: true,
        },
      });

      // --- Phase 1: register the credential via the activation flow. ---
      await gotoInvite(page, fresh.invite_token);
      await expect(page.locator('.loading')).toHaveCount(0, { timeout: 10_000 });
      await page.getByRole('button', { name: /Register Security Key/i }).click();
      await expect(page.getByText(/sign in to your account/i)).toBeVisible({ timeout: 15_000 });

      // --- Phase 2: log in with a passkey using the same authenticator. ---
      const finishStatus = page.waitForResponse((resp) =>
        resp.url().includes('/auth/login/finish') && resp.request().method() === 'POST',
      );

      await page.evaluate(() => {
        window.location.hash = '#/login';
      });
      await page.getByRole('button', { name: /sign in with passkey/i }).click();

      // Security regression guard: the login chooser is public/unauthenticated
      // — the app shell's sidebar/nav must be absent while it's on screen.
      await expect(page.locator('aside')).toHaveCount(0);
      await expect(page.getByRole('navigation')).toHaveCount(0);

      // createTestInvite uniquifies the email (e2e-login-vauth+<ts>@…), so the
      // passkey is registered against fresh.email — log in with that exact value,
      // not the base address, or login/begin can't find the user.
      await page.getByLabel(/email address/i).fill(fresh.email);
      await page.getByRole('button', { name: /authenticate with passkey/i }).click();

      // The finish call must succeed (2xx) — this is the same 200 the bug had.
      const resp = await finishStatus;
      expect(resp.status()).toBeGreaterThanOrEqual(200);
      expect(resp.status()).toBeLessThan(300);

      // The fix: the app must NOT bounce back to the login chooser. The login
      // chooser's passkey button must disappear and an authenticated control
      // (Sign out) must be present.
      await expect(
        page.getByRole('button', { name: /sign in with passkey/i }),
      ).toHaveCount(0, { timeout: 10_000 });
      await expect(
        page.getByRole('button', { name: /sign out/i }),
      ).toBeVisible({ timeout: 10_000 });

      // Now that the session is authenticated, the full app shell — sidebar
      // and nav landmark — must be present.
      await expect(page.locator('aside')).toHaveCount(1);
      await expect(page.getByRole('navigation')).toBeVisible();

      await client.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId });
    } finally {
      if (fresh.id) {
        await deleteTestUser(SYSTEM_TENANT_ID, fresh.id);
      }
    }
  });

  test('5. dead #/activate?token= route does NOT render the activation view', async ({ page }) => {
    // Regression guard for the dead-activation-URL bug: the daemon banner used
    // to print #/activate?token=<hex>, which the SPA router never maps. Navigate
    // there with a real token and assert the activation view does NOT render
    // (the router resolves it to 'not-found', not 'invite-activate').
    const token = invite?.invite_token ?? '';
    expect(token).not.toBe('');

    await page.goto('./', { waitUntil: 'networkidle' });
    await page.evaluate((tok: string) => {
      window.location.hash = `#/activate?token=${tok}`;
    }, token);
    await page.waitForLoadState('networkidle');

    await expect(
      page.getByRole('heading', { name: /Activate Your Account/i }),
    ).toHaveCount(0, { timeout: 5_000 });
  });

});
