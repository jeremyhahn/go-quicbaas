/**
 * global-setup.ts
 *
 * Runs once before the live-daemon Playwright projects and leaves the whole run
 * signed in as one principal. There is no static bootstrap token and no dev
 * bypass, so the session is always earned by a real passkey ceremony; the two
 * supported ways of getting one differ only in how the passkey comes to exist.
 *
 *   FIRST-BOOT ADMIN (default, `make test-e2e`) — a freshly built daemon on a
 *   scratch data dir prints a one-time invite URL. Walk it: activate a passkey
 *   for `admin` through the invite UI, then sign in.
 *
 *   LIVE TENANT USER (`make test-e2e-live`) — a long-lived node, an account that
 *   already exists, and no invite left to walk. Enrol a passkey for that user
 *   with an admin client certificate (fixtures/live_tenant.ts), then sign in as
 *   them. Selected by QUICBAAS_E2E_TENANT + QUICBAAS_E2E_USER_EMAIL.
 *
 * Both paths attach the same CDP virtual authenticator, finish through the same
 * unified sign-in ceremony (POST /api/v1/auth/login/{begin,finish}, type=fido2),
 * and persist the same two artifacts:
 *   • the browser storageState, so every spec starts signed in;
 *   • the raw tokens, so the REST seeding helpers in fixtures/api.ts can
 *     authenticate their out-of-band calls.
 *
 * Failing here is fatal by design: an unauthenticated suite would 401 on every
 * page and produce meaningless red, so we want a loud, early failure instead.
 */

import { chromium, type BrowserContext, type CDPSession, type FullConfig, type Page } from '@playwright/test';
import { mkdirSync, writeFileSync } from 'node:fs';
import {
  AUTH_DIR,
  ADMIN_STORAGE_STATE,
  ADMIN_TOKEN_FILE,
  ADMIN_EMAIL,
  TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  readAdminInviteToken,
  readLiveTenantConfig,
  type AdminTokens,
  type LiveTenantConfig,
} from './fixtures/auth.ts';
import { chromiumLaunchOptions, trustCA } from './fixtures/browser.ts';
import { serverBase } from './fixtures/_http.ts';
import { ensurePasskey } from './fixtures/live_tenant.ts';

/** The shape context.storageState() returns — Playwright does not export it. */
type StorageState = Awaited<ReturnType<BrowserContext['storageState']>>;

/** The account the run signs in as, and the tenant it belongs to. */
interface Principal {
  email: string;
  /** Typed into the login Organization field; the SPA defaults it to `system`. */
  tenant?: string;
}

function uiBaseURL(): string {
  return process.env['QUICBAAS_UI_URL'] ?? 'https://localhost:18080/ui/';
}

/**
 * Attach a CTAP2 internal virtual authenticator with user verification always
 * satisfied, so navigator.credentials.create()/get() resolve without human
 * interaction. Returns its id, which the live path needs to seed and read back
 * the credential it holds.
 *
 * Playwright's own CDP session is used deliberately: a second client attached to
 * the same target disturbs the first one's WebAuthn environment.
 */
async function attachVirtualAuthenticator(cdp: CDPSession): Promise<string> {
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
  return authenticatorId;
}

/**
 * Walk the first-boot invite: activate a passkey for the admin account through
 * the InviteActivateView UI (POST .../activate/fido2/begin+finish behind it).
 */
async function activateFirstBootAdmin(page: Page, baseURL: string): Promise<Principal> {
  const inviteToken = readAdminInviteToken();

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

  return { email: ADMIN_EMAIL };
}

/**
 * Bind a passkey to the configured tenant user, unless the authenticator already
 * holds one the node still accepts.
 *
 * The page is reloaded afterwards whether or not a ceremony ran. An enrolment
 * leaves the credentials API settled, and the sign-in that follows opens a new
 * ceremony on it: reloading is the documented way to guarantee the second one
 * does not meet "A request is already pending" left by the first.
 */
async function enrolLiveTenantUser(
  page: Page,
  cdp: CDPSession,
  authenticatorId: string,
  cfg: LiveTenantConfig,
  baseURL: string,
): Promise<Principal> {
  await page.goto(baseURL, { waitUntil: 'networkidle' });
  const enrolled = await ensurePasskey(page, cdp, authenticatorId, cfg, serverBase());
  // eslint-disable-next-line no-console
  console.log(
    `[global-setup] ${cfg.email} @ ${cfg.tenant}: ` +
      (enrolled ? 'enrolled a new passkey' : 'reused the passkey from a previous run'),
  );
  await page.reload({ waitUntil: 'networkidle' });

  return { email: cfg.email, tenant: cfg.tenant };
}

/**
 * Sign in through the unified passkey ceremony and leave the SPA inside the app
 * shell with its tokens persisted to localStorage.
 */
async function signInWithPasskey(page: Page, principal: Principal): Promise<void> {
  const loginFinish = page.waitForResponse(
    (r) => r.url().includes('/auth/login/finish') && r.request().method() === 'POST',
  );
  await page.evaluate(() => {
    window.location.hash = '#/login';
  });
  await page.getByRole('button', { name: /sign in with passkey/i }).click();
  if (principal.tenant !== undefined) {
    await page.getByLabel(/organization/i).fill(principal.tenant);
  }
  await page.getByLabel(/email address/i).fill(principal.email);
  await page.getByRole('button', { name: /authenticate with passkey/i }).click();
  const loginResp = await loginFinish;
  if (loginResp.status() < 200 || loginResp.status() >= 300) {
    throw new Error(`passkey login as ${principal.email} failed: HTTP ${loginResp.status()}`);
  }

  // The route guard must keep us inside the app shell (Sign out present).
  await page.getByRole('button', { name: /sign out/i }).waitFor({ timeout: 15_000 });
  // Let the shell's opening burst of API calls finish before anything reads the
  // stored credentials: a renewal racing the read would persist a rotated
  // refresh credential the reader had already missed.
  await page.waitForLoadState('networkidle');
}

/**
 * Strip the refresh credential from a storageState before it is persisted.
 *
 * The server issues a SINGLE-USE, rotating refresh token and revokes it the
 * moment it is redeemed. A storageState file, though, is replayed into a fresh
 * browser context for every test in the run, so all of them would hold the same
 * copy: the first context to renew spends it, and every context after that
 * presents a revoked credential, is refused, and is signed out mid-test. One
 * spec would poison all the others, for no reason connected to what it tested.
 *
 * Without the credential nothing renews and nothing can be poisoned: the SPA
 * declines to retry a 401 when it holds no refresh token (stores/authFetch.ts
 * isRenewable) and leaves the session alone, so every context runs on the access
 * token minted here for as long as it is valid. The raw pair still goes to
 * ADMIN_TOKEN_FILE, where a single worker process owns it and its rotations are
 * coherent (fixtures/_http.ts).
 */
function withoutRefreshCredential(state: StorageState): StorageState {
  return {
    ...state,
    origins: state.origins.map((origin) => ({
      ...origin,
      localStorage: origin.localStorage.filter((entry) => entry.name !== REFRESH_TOKEN_KEY),
    })),
  };
}

export default async function globalSetup(_config: FullConfig): Promise<void> {
  const baseURL = uiBaseURL();
  const live = readLiveTenantConfig();

  // A live node serves a certificate chaining to a private CA. Teach the browser
  // that one root — the suite does not answer a real chain by ignoring it. The
  // scratch daemon of the first-boot path has no CA to hand out, so that path
  // keeps its long-standing ignoreHTTPSErrors context.
  if (live !== null) trustCA(live.ca);

  const browser = await chromium.launch(chromiumLaunchOptions());
  const context = await browser.newContext(live === null ? { ignoreHTTPSErrors: true } : {});
  const page = await context.newPage();

  // Surface page errors during setup — a silent failure here is very costly.
  page.on('console', (msg) => {
    if (msg.type() === 'error') console.log('[global-setup page error]', msg.text());
  });

  try {
    const cdp = await context.newCDPSession(page);
    const authenticatorId = await attachVirtualAuthenticator(cdp);

    // --- Step 1: make sure the principal has a passkey this browser can use. ---
    const principal =
      live === null
        ? await activateFirstBootAdmin(page, baseURL)
        : await enrolLiveTenantUser(page, cdp, authenticatorId, live, baseURL);

    // --- Step 2: sign in with it. ---
    await signInWithPasskey(page, principal);

    // --- Step 3: harvest the tokens the SPA persisted to localStorage. ---
    const tokens = await page.evaluate(
      ([tk, rk]) => ({
        access_token: window.localStorage.getItem(tk) ?? '',
        refresh_token: window.localStorage.getItem(rk) ?? '',
      }),
      [TOKEN_KEY, REFRESH_TOKEN_KEY],
    );
    if (!tokens.access_token) {
      throw new Error(
        `login as ${principal.email} succeeded but no access token was persisted to localStorage`,
      );
    }

    // --- Step 4: persist storageState + raw tokens for the rest of the run. ---
    mkdirSync(AUTH_DIR, { recursive: true });
    writeFileSync(
      ADMIN_STORAGE_STATE,
      JSON.stringify(withoutRefreshCredential(await context.storageState()), null, 2),
    );
    const out: AdminTokens = {
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token,
      email: principal.email,
    };
    writeFileSync(ADMIN_TOKEN_FILE, JSON.stringify(out, null, 2));
    // eslint-disable-next-line no-console
    console.log(
      `[global-setup] session ready for ${principal.email} (token ${tokens.access_token.slice(0, 12)}…)`,
    );
  } finally {
    await context.close();
    await browser.close();
  }
}
