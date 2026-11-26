/**
 * global-setup.ts
 *
 * Runs once before the live-daemon Playwright projects and leaves the whole run
 * signed in as one principal. There is no static bootstrap token and no dev
 * bypass, so the session is always earned by a real login ceremony:
 *
 *   FIRST-BOOT ADMIN (default, `make test-e2e`) — a freshly built daemon on a
 *   scratch data dir issues `admin` a client certificate and nothing else.
 *   Present it over mTLS to POST /api/v1/iam/login/piv, as the integration
 *   suite does, and hand the returned tokens to the SPA's localStorage.
 *
 *   LIVE TENANT USER (`make test-e2e-live`) — a long-lived node and an account
 *   that already exists. Enrol a passkey for that user with an admin client
 *   certificate (fixtures/live_tenant.ts) on a CDP virtual authenticator, then
 *   sign in as them through the unified ceremony (POST
 *   /api/v1/auth/login/{begin,finish}, type=fido2). Selected by
 *   QUICBAAS_E2E_TENANT + QUICBAAS_E2E_USER_EMAIL.
 *
 * Both paths persist the same two artifacts:
 *   • the browser storageState, so every spec starts signed in;
 *   • the raw tokens, so the REST seeding helpers in fixtures/api.ts can
 *     authenticate their out-of-band calls.
 *
 * Failing here is fatal by design: an unauthenticated suite would 401 on every
 * page and produce meaningless red, so we want a loud, early failure instead.
 */

import {
  chromium,
  type Browser,
  type BrowserContext,
  type CDPSession,
  type FullConfig,
  type Page,
} from '@playwright/test';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  AUTH_DIR,
  ADMIN_STORAGE_STATE,
  ADMIN_TOKEN_FILE,
  ADMIN_EMAIL,
  FACTORY_STORAGE_STATE,
  FACTORY_TOKEN_FILE,
  type FactoryTokens,
  TOKEN_KEY,
  REFRESH_TOKEN_KEY,
  readFirstBootAdminConfig,
  readLiveTenantConfig,
  type AdminTokens,
  type LiveTenantConfig,
} from './fixtures/auth.ts';
import { chromiumLaunchOptions, trustCA } from './fixtures/browser.ts';
import { serverBase } from './fixtures/_http.ts';
import { ensurePasskey, mtlsRequest } from './fixtures/live_tenant.ts';

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
 * Log the first-boot admin in with its client certificate (POST
 * /api/v1/iam/login/piv over mTLS) and leave the SPA holding the tokens.
 */
async function loginFirstBootAdmin(page: Page, baseURL: string): Promise<Principal> {
  const cfg = readFirstBootAdminConfig();

  // Present the first-boot admin certificate to the PIV login route, exactly as
  // the integration suite signs the system admin in: no body, the client
  // certificate is the credential.
  const url = `${serverBase()}/api/v1/iam/login/piv`;
  const resp = await mtlsRequest(url, 'POST', cfg, { Accept: 'application/json' });
  if (resp.status < 200 || resp.status >= 300) {
    throw new Error(`admin mTLS login at ${url} failed: HTTP ${resp.status}: ${resp.body}`);
  }
  const body = JSON.parse(resp.body) as { access_token?: string; refresh_token?: string };
  if (!body.access_token) {
    throw new Error(`admin mTLS login at ${url} returned no access_token`);
  }

  // Hand the tokens to the SPA the way its own login does: localStorage.
  await page.goto(baseURL, { waitUntil: 'networkidle' });
  await page.evaluate(
    ([tk, rk, access, refresh]) => {
      window.localStorage.setItem(tk, access);
      if (refresh !== '') window.localStorage.setItem(rk, refresh);
    },
    [TOKEN_KEY, REFRESH_TOKEN_KEY, body.access_token, body.refresh_token ?? ''],
  );
  await page.reload({ waitUntil: 'networkidle' });

  return { email: ADMIN_EMAIL };
}


/** Tenant ids tried for the factory principal, in order. */
const FACTORY_TENANT_CANDIDATES = ['e2e-factory', 'e2efactory'];

interface FactoryPrincipal {
  tenant: string;
  email: string;
  access_token: string;
  refresh_token: string;
}

/**
 * Provision a non-system tenant with a certificate-enrolled admin, then log
 * that admin in over mTLS. Orchestrator, fleet and scheduler permissions are
 * tenant-only, so the software-factory specs run as this principal.
 *
 * The key and CSR are made here with openssl; the server signs the CSR and
 * returns the admin certificate, written beside the key under AUTH_DIR.
 */
async function provisionFactoryPrincipal(systemAccessToken: string): Promise<FactoryPrincipal> {
  const admin = readFirstBootAdminConfig();
  mkdirSync(AUTH_DIR, { recursive: true });

  let lastError = '';
  for (const tenant of FACTORY_TENANT_CANDIDATES) {
    const keyPath = resolve(AUTH_DIR, `${tenant}-admin.key`);
    const csrPath = resolve(AUTH_DIR, `${tenant}-admin.csr`);
    const certPath = resolve(AUTH_DIR, `${tenant}-admin.crt`);

    // Make the new key beside the saved pair, so a re-run whose tenant already
    // exists still has the key that matches the saved certificate.
    const newKeyPath = `${keyPath}.new`;
    execFileSync('openssl', [
      'ecparam', '-name', 'prime256v1', '-genkey', '-noout', '-out', newKeyPath,
    ]);
    execFileSync('openssl', [
      'req', '-new', '-key', newKeyPath, '-subj', '/CN=admin', '-out', csrPath,
    ]);
    const csrPEM = readFileSync(csrPath, 'utf8');

    const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(tenant)}/provision`;
    const resp = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        Authorization: `Bearer ${systemAccessToken}`,
      },
      body: JSON.stringify({
        isolation_type: 'prefix',
        admin_enrollment: 'cert',
        admin_csr_pem: csrPEM,
      }),
    });
    const text = await resp.text().catch(() => '');

    if (resp.ok) {
      const body = JSON.parse(text) as { admin_cert_pem?: string };
      if (!body.admin_cert_pem) {
        throw new Error(`provision ${tenant}: response carries no admin_cert_pem`);
      }
      renameSync(newKeyPath, keyPath);
      writeFileSync(certPath, body.admin_cert_pem);
    } else if (resp.status === 409 && existsSync(certPath) && existsSync(keyPath)) {
      // A re-run against the same daemon: the tenant exists and an earlier run
      // saved its admin certificate and key. Log in with that pair.
      rmSync(newKeyPath, { force: true });
    } else if (resp.status === 400 && /tenant[ _]?id|invalid/i.test(text)) {
      // The id format was refused; try the next candidate.
      lastError = `provision ${tenant}: HTTP 400: ${text}`;
      continue;
    } else {
      throw new Error(`provision ${tenant} at ${url} failed: HTTP ${resp.status}: ${text}`);
    }

    const cfg: LiveTenantConfig = {
      tenant,
      email: 'admin',
      adminCert: certPath,
      adminKey: keyPath,
      ca: admin.ca,
    };
    const loginURL = `${serverBase()}/api/v1/iam/login/piv`;
    const login = await mtlsRequest(loginURL, 'POST', cfg, { Accept: 'application/json' });
    if (login.status < 200 || login.status >= 300) {
      throw new Error(
        `tenant ${tenant} admin mTLS login at ${loginURL} failed: HTTP ${login.status}: ${login.body}`,
      );
    }
    const tokens = JSON.parse(login.body) as {
      access_token?: string;
      refresh_token?: string;
      email?: string;
      username?: string;
    };
    if (!tokens.access_token) {
      throw new Error(`tenant ${tenant} admin mTLS login returned no access_token`);
    }
    return {
      tenant,
      email: tokens.email ?? tokens.username ?? `admin@${tenant}`,
      access_token: tokens.access_token,
      refresh_token: tokens.refresh_token ?? '',
    };
  }
  throw new Error(`could not provision a factory tenant: ${lastError}`);
}

/**
 * Save the factory principal's browser session: a fresh context whose
 * localStorage holds its tokens, stripped of the refresh token like the admin
 * state, plus the raw tokens for REST seeding.
 */
async function saveFactoryPrincipal(
  browser: Browser,
  contextOptions: Parameters<Browser['newContext']>[0],
  baseURL: string,
  principal: FactoryPrincipal,
): Promise<void> {
  const context = await browser.newContext(contextOptions);
  try {
    const page = await context.newPage();
    await page.goto(baseURL, { waitUntil: 'networkidle' });
    await page.evaluate(
      ([tk, rk, access, refresh]) => {
        window.localStorage.setItem(tk, access);
        if (refresh !== '') window.localStorage.setItem(rk, refresh);
      },
      [TOKEN_KEY, REFRESH_TOKEN_KEY, principal.access_token, principal.refresh_token],
    );
    await page.reload({ waitUntil: 'networkidle' });
    writeFileSync(
      FACTORY_STORAGE_STATE,
      JSON.stringify(withoutRefreshCredential(await context.storageState()), null, 2),
    );
  } finally {
    await context.close();
  }
  const out: FactoryTokens = {
    access_token: principal.access_token,
    refresh_token: principal.refresh_token,
    email: principal.email,
    tenant: principal.tenant,
  };
  writeFileSync(FACTORY_TOKEN_FILE, JSON.stringify(out, null, 2));
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
    // --- Steps 1-2: sign the principal in. ---
    // The first-boot admin holds only a client certificate, so it logs in over
    // mTLS. A live tenant user gets a passkey this browser can use, then signs
    // in with it.
    let principal: Principal;
    if (live === null) {
      principal = await loginFirstBootAdmin(page, baseURL);
    } else {
      const cdp = await context.newCDPSession(page);
      const authenticatorId = await attachVirtualAuthenticator(cdp);
      principal = await enrolLiveTenantUser(page, cdp, authenticatorId, live, baseURL);
      await signInWithPasskey(page, principal);
    }

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

    // --- Step 5: the software-factory principal. ---
    // The first-boot admin belongs to the system tenant, which is refused the
    // tenant-only orchestrator, fleet and scheduler permissions; provision a
    // tenant and sign its admin in. A live tenant user already is a tenant
    // principal, so the factory session is a copy of the one just saved.
    if (live === null) {
      const factory = await provisionFactoryPrincipal(tokens.access_token);
      await saveFactoryPrincipal(browser, { ignoreHTTPSErrors: true }, baseURL, factory);
      // eslint-disable-next-line no-console
      console.log(`[global-setup] factory session ready for tenant ${factory.tenant}`);
    } else {
      writeFileSync(
        FACTORY_STORAGE_STATE,
        JSON.stringify(withoutRefreshCredential(await context.storageState()), null, 2),
      );
      const factoryOut: FactoryTokens = { ...out, tenant: live.tenant };
      writeFileSync(FACTORY_TOKEN_FILE, JSON.stringify(factoryOut, null, 2));
    }
    // eslint-disable-next-line no-console
    console.log(
      `[global-setup] session ready for ${principal.email} (token ${tokens.access_token.slice(0, 12)}…)`,
    );
  } finally {
    await context.close();
    await browser.close();
  }
}
