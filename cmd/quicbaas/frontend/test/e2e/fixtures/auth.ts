/**
 * fixtures/auth.ts
 *
 * Admin authentication harness for the live-daemon Playwright suite.
 *
 * A freshly-booted hardened daemon enforces default-deny authorization on every
 * /api/v1/orchestrator/* and /api/v1/tenants/* route, and first-boot creates the
 * `admin` user with NO credential bound — it prints a one-time WebAuthn invite
 * URL to its log instead. There is no static bootstrap token and no dev bypass,
 * so the ONLY production-correct way for the harness to obtain an admin bearer
 * token is to walk that invite: activate a passkey via a CDP virtual
 * authenticator, then log in.
 *
 * global-setup.ts performs that walk once and persists the result here:
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
 * Read the admin invite token. Prefers the QUICBAAS_ADMIN_INVITE_TOKEN env var
 * (the Makefile greps it from the daemon log and exports it); otherwise parses
 * the daemon log file at QUICBAAS_DAEMON_LOG. Throws when neither yields a token
 * so global-setup fails loudly rather than producing an unauthenticated suite.
 */
export function readAdminInviteToken(): string {
  const fromEnv = process.env['QUICBAAS_ADMIN_INVITE_TOKEN'];
  if (fromEnv && /^[0-9a-f]{64}$/.test(fromEnv)) return fromEnv;

  const logPath = process.env['QUICBAAS_DAEMON_LOG'] ?? '/tmp/quicbaasd-e2e.log';
  if (!existsSync(logPath)) {
    throw new Error(
      `readAdminInviteToken: no QUICBAAS_ADMIN_INVITE_TOKEN and daemon log not found at ${logPath}. ` +
        `Set QUICBAAS_ADMIN_INVITE_TOKEN or QUICBAAS_DAEMON_LOG.`,
    );
  }
  const log = readFileSync(logPath, 'utf8');
  // The first-boot banner prints exactly one line: …/ui/#/invite/<64-hex>.
  const m = log.match(/#\/invite\/([0-9a-f]{64})/);
  if (!m) {
    throw new Error(
      `readAdminInviteToken: no '#/invite/<token>' line found in ${logPath}. ` +
        `The daemon may not be a fresh first-boot instance.`,
    );
  }
  return m[1];
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
