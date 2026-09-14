/**
 * fixtures/browser.ts
 *
 * Chromium launch options shared by playwright.config.ts and global-setup.ts,
 * plus the CA trust anchor the live-node path installs for the browser.
 *
 * Two concerns live here because both are decided before a browser exists and
 * both must be identical in the setup browser and in the per-spec browser:
 *
 *   • The sandbox drop. In a container Chromium runs as root without its setuid
 *     sandbox, so QUICBAAS_E2E_NO_SANDBOX=1 (set by the dockerized runner) drops
 *     it. On the host the sandbox stays enabled. The config and global-setup
 *     used to spell this out separately; they now share one definition.
 *
 *   • The trust anchor. The live-node path (see live_tenant.ts) talks to a
 *     daemon whose serving certificate chains to a private CA, and this suite
 *     does not get to answer that by turning verification off. Chromium on Linux
 *     reads locally-added roots from the NSS user database at $HOME/.pki/nssdb,
 *     so trustCA() builds a database holding exactly that CA and
 *     chromiumLaunchOptions() points the browser's HOME at it. The host's own
 *     ~/.pki is never touched, and the browser still verifies the chain — it is
 *     told about one more root, not told to stop checking.
 */

import { execFileSync } from 'node:child_process';
import { existsSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';

import { AUTH_DIR } from './auth.ts';

/**
 * The HOME handed to Chromium. It exists only to carry the NSS database below,
 * so it lives beside the other run artifacts under test/e2e/.auth/.
 */
export const CHROMIUM_HOME = resolve(AUTH_DIR, 'chromium-home');

/** Chromium's locally-added-roots database, relative to its HOME. */
const NSSDB_DIR = resolve(CHROMIUM_HOME, '.pki', 'nssdb');

/** Nickname the CA is stored under. Re-importing under it replaces in place. */
const CA_NICKNAME = 'quicbaas-e2e-ca';

/** Launch overrides applied identically to the setup and per-spec browsers. */
export interface ChromiumLaunchOptions {
  args?: string[];
  env?: Record<string, string | undefined>;
}

/**
 * Install a CA certificate as a trusted TLS root for the suite's Chromium.
 *
 * Creates the NSS database on first use and imports the PEM at caPath with the
 * "C,," trust flag (trusted to issue TLS server certificates). Throws when
 * certutil is unavailable or refuses the certificate — a browser that silently
 * failed to trust the CA would fail later as an unauthenticated suite, which is
 * exactly the diagnosis this harness exists to avoid.
 */
export function trustCA(caPath: string): void {
  if (!existsSync(caPath)) {
    throw new Error(`trustCA: CA certificate not found at ${caPath}`);
  }
  mkdirSync(NSSDB_DIR, { recursive: true });

  try {
    if (!existsSync(resolve(NSSDB_DIR, 'cert9.db'))) {
      execFileSync('certutil', ['-d', `sql:${NSSDB_DIR}`, '-N', '--empty-password'], {
        stdio: 'pipe',
      });
    }
    execFileSync(
      'certutil',
      ['-d', `sql:${NSSDB_DIR}`, '-A', '-n', CA_NICKNAME, '-t', 'C,,', '-i', caPath],
      { stdio: 'pipe' },
    );
  } catch (e: unknown) {
    const detail = e instanceof Error ? e.message : String(e);
    throw new Error(
      `trustCA: could not add ${caPath} to the browser trust store at ${NSSDB_DIR}: ${detail}. ` +
        `certutil (libnss3-tools) must be installed — the suite does not disable TLS verification instead.`,
    );
  }
}

/**
 * Build the Chromium launch overrides for this run: the sandbox drop when the
 * runner asked for it, and the trust-store HOME when a CA was configured.
 *
 * playwright.config.ts calls this while loading the config, before global-setup
 * has built the database. That is safe: only the path is baked in here, and
 * global-setup runs trustCA() before any worker launches a browser.
 */
export function chromiumLaunchOptions(): ChromiumLaunchOptions {
  const options: ChromiumLaunchOptions = {};
  if (process.env['QUICBAAS_E2E_NO_SANDBOX'] === '1') {
    options.args = ['--no-sandbox', '--disable-setuid-sandbox'];
  }
  if (process.env['QUICBAAS_E2E_CA']) {
    options.env = { ...process.env, HOME: CHROMIUM_HOME };
  }
  return options;
}
