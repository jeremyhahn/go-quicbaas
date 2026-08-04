/**
 * register.spec.ts
 *
 * Playwright tests for the self-service tenant registration view
 * (RegisterView.svelte at hash route #/register).
 *
 * This is a PUBLIC route — it renders without authentication. Verified in:
 *   src/views/MainApp.svelte:
 *     const PUBLIC_ROUTES: ReadonlySet<string> = new Set(['login', 'register', 'invite-activate']);
 *   src/stores/router.svelte.ts:
 *     if (path === '/register') { return { name: 'register', params: {}, hash }; }
 *
 * Selectors verified against src/views/orchestrator/RegisterView.svelte:
 *   - "QuicBaaS" brand text        (div.gradient-text "QuicBaaS")
 *   - "Create your organization"   (div.text-sm.text-base-content/50 subtitle)
 *   - aria-label "Organization name" (input bound to tenantName)
 *   - aria-label "Display name"      (input bound to displayName)
 *   - aria-label "Email address"     (input bound to email, type="email")
 *   - name="isolation" radios        (keyspace / barrier)
 *   - aria-label "Create account with passkey" (submit button)
 *   - [role="alert"]                 (error banner)
 *   - .loading.loading-spinner       (ceremony stage spinner)
 *
 * REST paths confirmed against pkg/api/rest/iam.go lines 103-105:
 *   router.Route("/api/v1/auth", func(r chi.Router) {
 *     r.Post("/register/begin",  h.RegisterSelfBegin)
 *     r.Post("/register/finish", h.RegisterSelfFinish)
 *   })
 *
 * Terminal state logic (RegisterView.svelte handleSubmit):
 *   - registerBegin 403 → stage='error', errorMessage = "Self-service registration
 *     is not enabled on this server. Ask your admin for an invite."
 *   - registerBegin 200 → credentials.create() invoked (virtual authenticator
 *     responds) → registerFinish → stage='done'
 *       · finish.status 'active'                → "Account created!" + redirect
 *       · finish.status 'pending'               → "Awaiting approval" message
 *       · finish.status 'verification_required' → "Check your email" message
 *
 * This spec uses a clean unauthenticated storageState because it tests the
 * logged-out self-registration flow. The CDP virtual authenticator (pattern
 * from bootstrap_activate.spec.ts) is used so credentials.create() resolves
 * without requiring a physical security key.
 *
 * No ACP backend required.
 */

import { test, expect } from '@playwright/test';

// Public flow — start each test unauthenticated.
test.use({ ignoreHTTPSErrors: true, storageState: { cookies: [], origins: [] } });

// ---------------------------------------------------------------------------
// Navigation helper
// ---------------------------------------------------------------------------

type TestPage = Parameters<Parameters<typeof test>[1]>[0];

async function gotoRegister(page: TestPage): Promise<void> {
  // Navigate to app root first so the SPA is fully mounted before the hash
  // change fires. This matches the pattern used in bootstrap_activate.spec.ts.
  await page.goto('./', { waitUntil: 'networkidle' });
  await page.evaluate(() => { window.location.hash = '#/register'; });
  await page.waitForLoadState('networkidle');
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Orchestrator > Register — self-service tenant registration', () => {

  test('1. #/register renders brand heading and "Create your organization" subtitle', async ({ page }) => {
    await gotoRegister(page);

    // The card header renders two elements:
    //   <div class="text-2xl font-bold gradient-text">QuicBaaS</div>
    //   <div class="text-sm text-base-content/50 mt-1">Create your organization</div>
    // Scoping to div.gradient-text avoids the strict-mode violation caused by
    // "QuicBaaS" appearing in 4+ places (sidebar span, card div, etc.).
    await expect(page.locator('div.gradient-text', { hasText: 'QuicBaaS' })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText('Create your organization')).toBeVisible();
  });

  test('2. form fields render: Organization name, Display name, Email address', async ({ page }) => {
    await gotoRegister(page);

    // Each field uses an explicit aria-label matching its visual label text.
    await expect(page.getByRole('textbox', { name: /organization name/i })).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('textbox', { name: /display name/i })).toBeVisible();
    await expect(page.getByRole('textbox', { name: /email address/i })).toBeVisible();
  });

  test('3. CTA is disabled when fields are empty, enabled when all required fields are filled', async ({ page }) => {
    await gotoRegister(page);

    const cta = page.getByRole('button', { name: /create account with passkey/i });
    await expect(cta).toBeVisible({ timeout: 10_000 });

    // All fields are empty on mount → button must be disabled.
    // Condition: disabled={busy || !tenantName.trim() || !displayName.trim() || !email.trim()}
    await expect(cta).toBeDisabled();

    // Fill only one field — still disabled (two others empty).
    await page.getByRole('textbox', { name: /organization name/i }).fill('Acme Corp');
    await expect(cta).toBeDisabled();

    // Fill the second field — still disabled (email empty).
    await page.getByRole('textbox', { name: /display name/i }).fill('Jane Smith');
    await expect(cta).toBeDisabled();

    // Fill the last required field — now enabled.
    await page.getByRole('textbox', { name: /email address/i }).fill('jane@example.com');
    await expect(cta).toBeEnabled();
  });

  test('4. isolation mode radio buttons render with "keyspace" selected by default', async ({ page }) => {
    await gotoRegister(page);

    // Two radio buttons with name="isolation" for keyspace/barrier modes.
    const keyspaceRadio = page.locator('input[type="radio"][name="isolation"][value="keyspace"]');
    const barrierRadio  = page.locator('input[type="radio"][name="isolation"][value="barrier"]');

    await expect(keyspaceRadio).toBeVisible({ timeout: 10_000 });
    await expect(barrierRadio).toBeVisible();

    // Default is "keyspace" (let isolationMode = $state('keyspace')).
    await expect(keyspaceRadio).toBeChecked();
    await expect(barrierRadio).not.toBeChecked();
  });

  test('5. selecting barrier isolation mode updates the description hint', async ({ page }) => {
    await gotoRegister(page);

    // Keyspace hint is visible by default.
    await expect(page.getByText(/fastest option/i)).toBeVisible({ timeout: 10_000 });

    // Switch to barrier mode.
    await page.locator('input[type="radio"][name="isolation"][value="barrier"]').click();

    // Description swaps to barrier copy.
    await expect(page.getByText(/dedicated cryptographic barrier/i)).toBeVisible();
    await expect(page.getByText(/fastest option/i)).toHaveCount(0);
  });

  test('6. "Sign in" link navigates to #/login', async ({ page }) => {
    await gotoRegister(page);

    const signInLink = page.getByRole('button', { name: /go to sign-in/i });
    await expect(signInLink).toBeVisible({ timeout: 10_000 });
    await signInLink.click();

    // Router should resolve to the login view.
    await expect(page).toHaveURL(/#\/login/, { timeout: 5_000 });
  });

  test('7. submitting with a CDP virtual authenticator fires POST /api/v1/auth/register/begin and reaches a terminal state', async ({ page }) => {
    // Install a CTAP2 virtual authenticator so navigator.credentials.create()
    // resolves without human interaction if self-registration is enabled on
    // this daemon. Pattern copied from bootstrap_activate.spec.ts test 6.
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

    try {
      await gotoRegister(page);

      // Capture the begin call BEFORE clicking to avoid timing races.
      // The begin endpoint is POST /api/v1/auth/register/begin (confirmed:
      // pkg/api/rest/iam.go router.Route("/api/v1/auth") → r.Post("/register/begin")).
      const beginResponse = page.waitForResponse(
        (resp) =>
          resp.url().includes('/auth/register/begin') &&
          resp.request().method() === 'POST',
        { timeout: 30_000 },
      );

      // Fill all required fields to enable the submit button.
      const ts = Date.now();
      await page.getByRole('textbox', { name: /organization name/i }).fill(`E2E Org ${ts}`);
      await page.getByRole('textbox', { name: /display name/i }).fill('E2E Registrant');
      await page.getByRole('textbox', { name: /email address/i }).fill(`e2e-register+${ts}@quicbaas.test`);

      const cta = page.getByRole('button', { name: /create account with passkey/i });
      await expect(cta).toBeEnabled({ timeout: 5_000 });

      // Click fires: stage='ceremony' → registerBegin → (credentials.create if 2xx).
      await cta.click();

      // The begin request MUST fire — this is the primary contract under test.
      const resp = await beginResponse;

      // ---------------------------------------------------------------------------
      // Terminal state: two branches depending on whether self-registration is
      // enabled on this server.
      //
      // Branch A — 403 (most likely in CI/test environment where self-service
      //   registration is disabled by default for security):
      //     stage → 'error'
      //     errorMessage → "Self-service registration is not enabled on this server."
      //     [role="alert"] visible + error text present
      //
      // Branch B — 200 (self-registration enabled):
      //     Virtual authenticator handles credentials.create()
      //     registerFinish fires; stage → 'done'
      //     One of three done-state messages renders:
      //       'active'                → "Account created!"
      //       'pending'              → "Awaiting approval"
      //       'verification_required'→ "Check your email"
      // ---------------------------------------------------------------------------

      if (resp.status() === 403) {
        // Branch A: self-service registration is disabled (expected in most envs).
        // The spinner clears (stage back to 'form'/'error') and the error alert appears.
        await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 10_000 });
        await expect(page.getByRole('alert')).toBeVisible({ timeout: 5_000 });
        await expect(
          page.getByText(/self-service registration is not enabled/i),
        ).toBeVisible();

        // The "Try again" button brings the user back to the form.
        const tryAgainBtn = page.getByRole('button', { name: /try again/i });
        await expect(tryAgainBtn).toBeVisible();
      } else {
        // Branch B: self-registration enabled. Virtual authenticator handles
        // credentials.create(); registerFinish fires. The spinner clears after
        // the ceremony stage and one of three done-state messages renders.
        await expect(page.locator('.loading.loading-spinner')).toHaveCount(0, { timeout: 20_000 });

        // Assert that ONE of the three done-state messages is visible.
        const doneMsg = page.locator(
          'text=Account created!, text=Awaiting approval, text=Check your email',
        );
        await expect(doneMsg.first()).toBeVisible({ timeout: 10_000 });
      }
    } finally {
      await client.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId });
    }
  });

  test('8. "Try again" button from error state re-shows the form', async ({ page }) => {
    // Trigger an error state by navigating with a known-bad email to an
    // organization that already exists, or by submitting once to get the 403
    // (self-reg disabled). Rather than relying on the server state, we use the
    // same approach as the form validation — fill an empty field after the
    // initial submission so we can re-test the form state. But a simpler
    // approach: just assert that clicking "Try again" from any error state
    // re-renders the form. We trigger the error by clicking CTA without filling
    // all fields — but the button is disabled until fields are filled, so we
    // can't do that. Instead we directly manipulate the stage through a submit
    // attempt via the full flow (same as test 7 but focused on the retry path).
    //
    // If self-registration is disabled (the 403 path), we get the error state
    // deterministically. If it's enabled, Branch B (done screen) has no
    // "Try again" button so we'd skip.

    const cdp = await page.context().newCDPSession(page);
    await cdp.send('WebAuthn.enable');
    const { authenticatorId } = await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2',
        transport: 'internal',
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    });

    try {
      await gotoRegister(page);

      const beginResponse = page.waitForResponse(
        (resp) =>
          resp.url().includes('/auth/register/begin') && resp.request().method() === 'POST',
        { timeout: 30_000 },
      );

      const ts = Date.now();
      await page.getByRole('textbox', { name: /organization name/i }).fill(`E2E Retry Org ${ts}`);
      await page.getByRole('textbox', { name: /display name/i }).fill('Retry User');
      await page.getByRole('textbox', { name: /email address/i }).fill(`e2e-retry+${ts}@quicbaas.test`);
      await page.getByRole('button', { name: /create account with passkey/i }).click();

      const resp = await beginResponse;

      if (resp.status() !== 403) {
        // Branch B (self-reg enabled) — this test is only meaningful when the
        // server returns a 403 (error state). Skip rather than fail.
        test.skip(true, 'server returned 2xx (self-reg enabled) — retry test only applies to error state');
        return;
      }

      // 403 path: error state is shown.
      await expect(page.getByRole('alert')).toBeVisible({ timeout: 10_000 });

      // Clicking "Try again" resets stage to 'form' and clears errorMessage.
      const tryAgainBtn = page.getByRole('button', { name: /try again/i });
      await expect(tryAgainBtn).toBeVisible();
      await tryAgainBtn.click();

      // The form fields must be visible again and the error alert gone.
      await expect(page.getByRole('textbox', { name: /organization name/i })).toBeVisible({ timeout: 5_000 });
      await expect(page.getByRole('alert')).toHaveCount(0);
    } finally {
      await cdp.send('WebAuthn.removeVirtualAuthenticator', { authenticatorId });
    }
  });

});
