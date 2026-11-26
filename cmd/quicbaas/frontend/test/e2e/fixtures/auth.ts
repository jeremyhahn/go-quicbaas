/**
 * fixtures/auth.ts
 *
 * Admin authentication harness for the live-daemon Playwright suite.
 *
 * A freshly-booted hardened daemon enforces default-deny authorization on every
 * /api/v1/orchestrator/* and /api/v1/tenants/* route, and first-boot creates the
 * `admin` user with only a client certificate, written under the daemon's data
 * dir. There is no static bootstrap token and no dev bypass, so the harness
 * earns an admin bearer token the production way: present that certificate to
 * enrol a passkey via a CDP virtual authenticator, then log in with it.
 *
 * global-setup.ts performs that once and persists the result here:
 *   • ADMIN_STORAGE_STATE — Playwright storageState (localStorage tokens) so
 *     every spec starts already signed in as admin in the browser.
 *   • ADMIN_TOKEN_FILE — the raw admin access/refresh tokens so the REST seeding
 *     helpers in fixtures/api.ts can authenticate their out-of-band calls.
 *
 * Both files live under test/e2e/.auth/ (git-ignored).
 */

import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';
import { readFileSync, existsSync } from 'node:fs';

const here = dirname(fileURLToPath(import.meta.url));

/** Directory holding the persisted admin session artifacts. */
export const AUTH_DIR = resolve(here, '..', '.auth');

/** Playwright storageState file — localStorage tokens for the admin session. */
export const ADMIN_STORAGE_STATE = resolve(AUTH_DIR, 'admin.json');

/** Raw admin tokens (access + refresh + email) for REST seeding helpers. */
export const ADMIN_TOKEN_FILE = resolve(AUTH_DIR, 'admin-token.json');

/** The first-boot admin user's canonical email/username. */
export const ADMIN_EMAIL = 'admin@system.qrdb.local';

/** localStorage keys the SPA reads its tokens from (see auth.svelte.ts). */
export const TOKEN_KEY = 'quicbaas_token';
export const REFRESH_TOKEN_KEY = 'quicbaas_refresh_token';

/** Persisted admin tokens shape. */
export interface AdminTokens {
  access_token: string;
  refresh_token: string;
  email: string;
}

/**
 * Locate the scratch daemon's data dir. Prefers QUICBAAS_E2E_DATA_DIR (set by
 * test/e2e/run-e2e.sh); otherwise derives it from NODE_EXTRA_CA_CERTS, which
 * every first-boot runner points at `<data dir>/certs/ca-chain.crt`.
 */
function firstBootDataDir(): string {
  const fromEnv = process.env['QUICBAAS_E2E_DATA_DIR'] ?? '';
  if (fromEnv !== '') return fromEnv;
  const caChain = process.env['NODE_EXTRA_CA_CERTS'] ?? '';
  if (caChain !== '') return dirname(dirname(caChain));
  throw new Error(
    'readFirstBootAdminConfig: neither QUICBAAS_E2E_DATA_DIR nor NODE_EXTRA_CA_CERTS is set, ' +
      "so the daemon's data dir (and the first-boot admin certificate in it) cannot be found.",
  );
}

/**
 * Build the mTLS configuration for the first-boot system administrator.
 *
 * First boot issues the `admin` user a client certificate, binds its SPKI
 * thumbprint as an mtls login factor, and writes the bundle to
 * `<data dir>/admin/admin.{crt,key}` (pkg/server writeAdminBundle). Global setup
 * presents that certificate to enrol a passkey for `admin` — the same
 * administrator-only operation the live path performs — then signs in with it.
 * Throws when a file is missing so global-setup fails loudly rather than
 * producing an unauthenticated suite.
 */
export function readFirstBootAdminConfig(): LiveTenantConfig {
  const dataDir = firstBootDataDir();
  const cfg: LiveTenantConfig = {
    tenant: 'system',
    email: ADMIN_EMAIL,
    adminCert: resolve(dataDir, 'admin', 'admin.crt'),
    adminKey: resolve(dataDir, 'admin', 'admin.key'),
    ca: resolve(dataDir, 'certs', 'ca-chain.crt'),
  };
  for (const path of [cfg.adminCert, cfg.adminKey, cfg.ca]) {
    if (!existsSync(path)) {
      throw new Error(
        `readFirstBootAdminConfig: ${path} not found. ` +
          `The daemon may not be a fresh first-boot instance on data dir ${dataDir}.`,
      );
    }
  }
  return cfg;
}

/** Read the persisted admin tokens written by global-setup. */
export function readAdminTokens(): AdminTokens {
  if (!existsSync(ADMIN_TOKEN_FILE)) {
    throw new Error(
      `readAdminTokens: ${ADMIN_TOKEN_FILE} missing — global-setup did not run or failed.`,
    );
  }
  return JSON.parse(readFileSync(ADMIN_TOKEN_FILE, 'utf8')) as AdminTokens;
}

// ---------------------------------------------------------------------------
// Live-node tenant sign-in
// ---------------------------------------------------------------------------

/**
 * Configuration for signing the suite in as an EXISTING TENANT USER on an
 * ALREADY-RUNNING node, instead of walking a fresh daemon's first boot.
 *
 * The first-boot path above can only ever produce the system admin, and only
 * on a scratch data dir whose admin bundle the runner can read. A burn-in node, though, is exactly the thing worth driving a browser
 * against, and the interesting principal on it is a tenant user. This config
 * names that user and the admin client certificate used to enrol a passkey for
 * them (see live_tenant.ts) — an operation only an administrator may perform.
 */
export interface LiveTenantConfig {
  /** Tenant id the user belongs to, typed into the login Organization field. */
  tenant: string;
  /** The existing user's email — their username in the login ceremony. */
  email: string;
  /** PEM client certificate authorized for user_authfactors:add. */
  adminCert: string;
  /** PEM private key for adminCert. */
  adminKey: string;
  /** PEM CA bundle the node's serving certificate chains to. */
  ca: string;
}

/** Env var carrying each LiveTenantConfig field. */
const LIVE_TENANT_ENV: Readonly<Record<keyof LiveTenantConfig, string>> = {
  tenant: 'QUICBAAS_E2E_TENANT',
  email: 'QUICBAAS_E2E_USER_EMAIL',
  adminCert: 'QUICBAAS_E2E_ADMIN_CERT',
  adminKey: 'QUICBAAS_E2E_ADMIN_KEY',
  ca: 'QUICBAAS_E2E_CA',
};

/**
 * Read the live-node tenant configuration from the environment.
 *
 * Returns null when the run selected no live target at all, which is the signal
 * to walk the first-boot admin path instead. A PARTIAL configuration throws:
 * silently falling back would run the suite against the wrong node as the wrong
 * principal, and a 401 on every page is a far worse diagnosis than a missing
 * variable named here.
 */
export function readLiveTenantConfig(): LiveTenantConfig | null {
  const names = Object.keys(LIVE_TENANT_ENV) as (keyof LiveTenantConfig)[];
  const present = names.filter((n) => (process.env[LIVE_TENANT_ENV[n]] ?? '') !== '');
  if (present.length === 0) return null;

  const missing = names.filter((n) => !present.includes(n)).map((n) => LIVE_TENANT_ENV[n]);
  if (missing.length > 0) {
    throw new Error(
      `readLiveTenantConfig: live-node sign-in is partially configured — ` +
        `set ${missing.join(', ')} as well, or none of ${names.map((n) => LIVE_TENANT_ENV[n]).join(', ')}.`,
    );
  }

  const value = (n: keyof LiveTenantConfig): string => process.env[LIVE_TENANT_ENV[n]] as string;
  return {
    tenant: value('tenant'),
    email: value('email'),
    adminCert: value('adminCert'),
    adminKey: value('adminKey'),
    ca: value('ca'),
  };
}
