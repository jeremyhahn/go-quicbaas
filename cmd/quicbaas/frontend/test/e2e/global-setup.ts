/**
 * global-setup.ts
 *
 * Runs once before the live-daemon Playwright project. Obtains a real admin
 * bearer token from a fresh hardened daemon by walking the production-correct
 * first-boot path — there is no static bootstrap token or dev bypass:
 *
 *   1. Read the one-time admin invite token (printed to the daemon log / env).
 *   2. Activate a passkey for the admin user via a CDP virtual authenticator
 *      (POST .../activate/fido2/begin+finish behind the InviteActivateView UI).
 *   3. Sign in with that passkey (POST /api/v1/auth/login/{begin,finish} (unified ceremony, type=fido2)) so the
 *      SPA persists access + refresh tokens to localStorage.
 *   4. Save the browser storageState (so every spec starts signed in) and the
 *      raw tokens (so REST seeding helpers in fixtures/api.ts can authenticate).
 *
 * Failing here is fatal by design: an unauthenticated suite would 401 on every
 * page and produce meaningless red, so we want a loud, early failure instead.
 */

import { chromium, type FullConfig } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import {
  AUTH_DIR,
  ADMIN_STORAGE_STATE,
  ADMIN_TOKEN_FILE,
  ADMIN_EMAIL,
  TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  readAdminInviteToken,
  type AdminTokens,
} from './fixtures/auth.ts';

function uiBaseURL(): string {
  return process.env['QUICBAAS_UI_URL'] ?? 'https://localhost:18080/ui/';
}

export default async function globalSetup(_config: FullConfig): Promise<void> {
  const inviteToken = readAdminInviteToken();
  const baseURL = uiBaseURL();

  // In a container the browser runs as root, where Chromium's setuid sandbox is
  // unavailable; QUICBAAS_E2E_NO_SANDBOX=1 (set by the dockerized runner) drops
  // it. On the host the sandbox stays on.
  const launchArgs = process.env['QUICBAAS_E2E_NO_SANDBOX'] === '1'
    ? { args: ['--no-sandbox', '--disable-setuid-sandbox'] }
    : {};
  const browser = await chromium.launch(launchArgs);
  const context = await browser.newContext({ ignoreHTTPSErrors: true });
  const page = await context.newPage();

  // Surface page errors during setup — a silent failure here is very costly.
  page.on('console', (msg) => {
    if (msg.type() === 'error') console.log('[global-setup page error]', msg.text());
  });

  try {
    // Attach a CTAP2 internal virtual authenticator with UV always satisfied so
    // navigator.credentials.create()/get() resolve without human interaction.
    const cdp = await context.newCDPSession(page);
    await cdp.send('WebAuthn.enable');
    await cdp.send('WebAuthn.addVirtualAuthenticator', {
      options: {
        protocol: 'ctap2',
        transport: 'internal',
        hasResidentKey: true,
        hasUserVerification: true,
        isUserVerified: true,
        automaticPresenceSimulation: true,
      },
    });

    // --- Step 1: activate the admin passkey via the invite UI. ---
    await page.goto(baseURL, { waitUntil: 'networkidle' });
    await page.evaluate((tok: string) => {
      window.location.hash = `#/invite/${tok}`;
    }, inviteToken);
    await page.waitForLoadState('networkidle');

    const activateFinish = page.waitForResponse(
      (r) => r.url().includes('/activate/fido2/finish') && r.request().method() === 'POST',
    );
    await page.getByRole('button', { name: /Register Security Key/i }).click();
    const actResp = await activateFinish;
    if (actResp.status() < 200 || actResp.status() >= 300) {
      throw new Error(`admin passkey activation failed: HTTP ${actResp.status()}`);
    }
    await page.getByText(/sign in to your account/i).waitFor({ timeout: 15_000 });

    // --- Step 2: sign in with the freshly-registered passkey. ---
    const loginFinish = page.waitForResponse(
      (r) => r.url().includes('/auth/login/finish') && r.request().method() === 'POST',
    );
    await page.evaluate(() => {
      window.location.hash = '#/login';
    });
    await page.getByRole('button', { name: /sign in with passkey/i }).click();
    await page.getByLabel(/email address/i).fill(ADMIN_EMAIL);
    await page.getByRole('button', { name: /authenticate with passkey/i }).click();
    const loginResp = await loginFinish;
    if (loginResp.status() < 200 || loginResp.status() >= 300) {
      throw new Error(`admin passkey login failed: HTTP ${loginResp.status()}`);
    }

    // The route guard must keep us inside the app shell (Sign out present).
    await page.getByRole('button', { name: /sign out/i }).waitFor({ timeout: 15_000 });

    // --- Step 3: harvest the tokens the SPA persisted to localStorage. ---
    const tokens = await page.evaluate(
      ([tk, rk]) => ({
        access_token: window.localStorage.getItem(tk) ?? '',
        refresh_token: window.localStorage.getItem(rk) ?? '',
      }),
      [TOKEN_KEY, REFRESH_TOKEN_KEY],
    );
    if (!tokens.access_token) {
      throw new Error('admin login succeeded but no access token was persisted to localStorage');
    }

    // --- Step 4: persist storageState + raw tokens for the rest of the run. ---
    mkdirSync(AUTH_DIR, { recursive: true });
    await context.storageState({ path: ADMIN_STORAGE_STATE });
    const out: AdminTokens = {
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
      email: ADMIN_EMAIL,
    };
    writeFileSync(ADMIN_TOKEN_FILE, JSON.stringify(out, null, 2));
    // eslint-disable-next-line no-console
    console.log(`[global-setup] admin session ready (token ${tokens.access_token.slice(0, 12)}…)`);
  } finally {
    await context.close();
    await browser.close();
  }
}
