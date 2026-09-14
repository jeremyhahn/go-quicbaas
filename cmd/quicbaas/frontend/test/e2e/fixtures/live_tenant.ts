/**
 * fixtures/live_tenant.ts
 *
 * Passkey enrolment for an EXISTING tenant user on an ALREADY-RUNNING node.
 *
 * The first-boot path in global-setup.ts gets its credential for free: the
 * invite UI enrols one as part of activating the account. A user who already
 * exists has no invite left to walk, and the browser cannot enrol for them
 * either — binding a new authenticator to somebody's account is
 * `user_authfactors:add`, which no unauthenticated page holds. So the enrolment
 * half of the ceremony is driven from node with an ADMIN CLIENT CERTIFICATE
 * against the tenant-scoped WebAuthn surface, and only the authenticator half
 * happens in the page:
 *
 *   1. POST /api/v1/iam/{tenant}/webauthn/registration/begin  (mTLS)
 *        → creation options, plus the ceremony id in X-Session-Id
 *   2. navigator.credentials.create() in the page, answered by the CDP virtual
 *      authenticator global-setup attached
 *   3. POST /api/v1/iam/{tenant}/webauthn/registration/finish (mTLS, same
 *      X-Session-Id) with the attestation
 *
 * Enrolment is conditional, not unconditional. A virtual authenticator is empty
 * every time Chromium starts, so the credential it holds is persisted (CDP hands
 * back the private key) and restored on the next run; the run only enrols when
 * the server no longer offers that credential — which is what a node whose
 * credential store is in memory does after a restart. Without that, every run
 * would bind one more authenticator to the account forever.
 *
 * Every request here verifies the node's certificate against the configured CA.
 * Nothing in this module turns verification off.
 */

import { readFileSync, existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { request as httpsRequest } from 'node:https';
import { resolve } from 'node:path';
import type { CDPSession, Page } from '@playwright/test';

import { AUTH_DIR, type LiveTenantConfig } from './auth.ts';

/**
 * The virtual authenticator's credential, kept between runs so a re-run against
 * a node that still knows it does not enrol a second one. Git-ignored, same as
 * the rest of test/e2e/.auth/.
 */
const LIVE_CREDENTIAL_FILE = resolve(AUTH_DIR, 'live-credential.json');

/** A credential as CDP models it — every binary member standard-base64. */
interface VirtualCredential {
  credentialId: string;
  isResidentCredential: boolean;
  rpId: string;
  privateKey: string;
  signCount: number;
  userHandle?: string;
}

/** Minimal view of an mTLS response: everything this module reads. */
interface MTLSResponse {
  status: number;
  sessionID: string;
  body: string;
}

// ---------------------------------------------------------------------------
// base64url ↔ base64
//
// The WebAuthn wire is base64url; CDP's binary members are standard base64.
// Credential ids cross that line whenever a persisted credential is compared
// against what the server offers, so the two spellings are reconciled here.
// ---------------------------------------------------------------------------

function base64ToBase64URL(s: string): string {
  return s.replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

// ---------------------------------------------------------------------------
// mTLS transport
// ---------------------------------------------------------------------------

/**
 * Send one request to the node, presenting the admin client certificate and
 * verifying the server against the configured CA.
 *
 * Rejects on transport failure — including a certificate that does not verify,
 * which is a real finding here and not something to route around.
 */
function mtlsRequest(
  url: string,
  method: string,
  cfg: LiveTenantConfig,
  headers: Record<string, string>,
  body?: string,
): Promise<MTLSResponse> {
  return new Promise<MTLSResponse>((fulfil, reject) => {
    const req = httpsRequest(
      url,
      {
        method,
        ca: readFileSync(cfg.ca),
        cert: readFileSync(cfg.adminCert),
        key: readFileSync(cfg.adminKey),
        headers,
      },
      (res: NodeJS.ReadableStream & { statusCode?: number; headers: Record<string, unknown> }) => {
        const chunks: string[] = [];
        res.setEncoding('utf8');
        res.on('data', (c: string) => chunks.push(c));
        res.on('end', () =>
          fulfil({
            status: res.statusCode ?? 0,
            sessionID: String(res.headers['x-session-id'] ?? ''),
            body: chunks.join(''),
          }),
        );
      },
    );
    req.on('error', reject);
    if (body !== undefined) req.write(body);
    req.end();
  });
}

/** POST a JSON body over mTLS and fail loudly on any non-2xx answer. */
async function postJSON(
  url: string,
  cfg: LiveTenantConfig,
  headers: Record<string, string>,
  payload: string,
): Promise<MTLSResponse> {
  const resp = await mtlsRequest(url, 'POST', cfg, { 'Content-Type': 'application/json', ...headers }, payload);
  if (resp.status < 200 || resp.status >= 300) {
    throw new Error(`POST ${url} failed: HTTP ${resp.status}: ${resp.body.trim()}`);
  }
  return resp;
}

// ---------------------------------------------------------------------------
// What the server will accept
// ---------------------------------------------------------------------------

/**
 * Ask the unified login ceremony which credentials the user can assert with.
 *
 * This is the same public endpoint the SPA calls, so the answer is exactly the
 * allowCredentials list the browser would be handed — the only honest test of
 * whether a restored credential is still usable. A user the server cannot
 * resolve answers 401 by design (it refuses to confirm which tenants and users
 * exist), which is reported here as an empty list: unresolvable and
 * credential-less both mean "enrol".
 */
export async function assertableCredentialIDs(
  cfg: LiveTenantConfig,
  serverBase: string,
): Promise<string[]> {
  const resp = await mtlsRequest(
    `${serverBase}/api/v1/auth/login/begin`,
    'POST',
    cfg,
    { 'Content-Type': 'application/json' },
    JSON.stringify({ type: 'fido2', hint: cfg.email, tenant_id: cfg.tenant }),
  );
  if (resp.status === 401 || resp.status === 404) return [];
  if (resp.status < 200 || resp.status >= 300) {
    throw new Error(`login/begin probe failed: HTTP ${resp.status}: ${resp.body.trim()}`);
  }
  const begin = JSON.parse(resp.body) as { challenge_metadata?: Record<string, string> };
  const optionsJSON = begin.challenge_metadata?.['options'];
  if (!optionsJSON) return [];
  const options = JSON.parse(optionsJSON) as {
    publicKey?: { allowCredentials?: { id: string }[] };
  };
  return (options.publicKey?.allowCredentials ?? []).map((c) => c.id);
}

// ---------------------------------------------------------------------------
// Virtual-authenticator credential persistence
// ---------------------------------------------------------------------------

/**
 * Load the credential saved by a previous run into the virtual authenticator.
 * Returns its base64url id, or null when there is nothing saved.
 */
export async function restoreCredential(
  cdp: CDPSession,
  authenticatorId: string,
): Promise<string | null> {
  if (!existsSync(LIVE_CREDENTIAL_FILE)) return null;
  const credential = JSON.parse(readFileSync(LIVE_CREDENTIAL_FILE, 'utf8')) as VirtualCredential;
  await cdp.send('WebAuthn.addCredential', { authenticatorId, credential });
  return base64ToBase64URL(credential.credentialId);
}

/** Save the authenticator's sole credential so the next run can restore it. */
async function saveCredential(cdp: CDPSession, authenticatorId: string): Promise<void> {
  const { credentials } = (await cdp.send('WebAuthn.getCredentials', { authenticatorId })) as {
    credentials: VirtualCredential[];
  };
  const credential = credentials[credentials.length - 1];
  if (credential === undefined) {
    throw new Error('saveCredential: the virtual authenticator holds no credential after enrolment');
  }
  mkdirSync(AUTH_DIR, { recursive: true });
  writeFileSync(LIVE_CREDENTIAL_FILE, JSON.stringify(credential, null, 2));
}

// ---------------------------------------------------------------------------
// Enrolment
// ---------------------------------------------------------------------------

/** The subset of the creation options this module reshapes for the browser. */
interface CreationOptionsJSON {
  publicKey: {
    challenge: string;
    user: { id: string; name: string; displayName: string };
    excludeCredentials?: { id: string; type: string; transports?: string[] }[];
  } & Record<string, unknown>;
}

/**
 * Run navigator.credentials.create() in the page against the attached virtual
 * authenticator and return the attestation as the base64url JSON go-webauthn
 * parses.
 *
 * The options arrive as JSON with base64url binary members and have to reach
 * the browser as ArrayBuffers, so they are decoded inside the page: passing
 * ArrayBuffers through page.evaluate would serialize them away.
 */
async function createCredentialInPage(page: Page, optionsJSON: string): Promise<string> {
  return page.evaluate(async (raw: string): Promise<string> => {
    const decode = (s: string): Uint8Array => {
      const b64 = s.replace(/-/g, '+').replace(/_/g, '/');
      const padded = b64.padEnd(b64.length + ((4 - (b64.length % 4)) % 4), '=');
      const binary = atob(padded);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
      return bytes;
    };
    const encode = (b: ArrayBuffer): string => {
      let binary = '';
      for (const byte of new Uint8Array(b)) binary += String.fromCharCode(byte);
      return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    };

    const options = (JSON.parse(raw) as CreationOptionsJSON).publicKey;
    const publicKey = {
      ...options,
      challenge: decode(options.challenge),
      user: { ...options.user, id: decode(options.user.id) },
      excludeCredentials: (options.excludeCredentials ?? []).map((c) => ({
        ...c,
        id: decode(c.id),
      })),
    } as unknown as PublicKeyCredentialCreationOptions;

    const credential = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null;
    if (credential === null) {
      throw new Error('navigator.credentials.create() returned no credential');
    }
    const response = credential.response as AuthenticatorAttestationResponse;
    return JSON.stringify({
      id: credential.id,
      rawId: encode(credential.rawId),
      type: credential.type,
      clientExtensionResults: credential.getClientExtensionResults(),
      response: {
        clientDataJSON: encode(response.clientDataJSON),
        attestationObject: encode(response.attestationObject),
      },
    });
  }, optionsJSON);
}

/**
 * Enrol a passkey for the configured tenant user, unless the authenticator
 * already holds one the server will accept.
 *
 * Returns true when a new credential was bound. The page must already be on the
 * node's origin — the relying-party id is checked against it — and must have no
 * ceremony in flight: a second overlapping call to the credentials API fails
 * with "A request is already pending", which is recovered from by reloading the
 * page, never by retrying into it.
 */
export async function ensurePasskey(
  page: Page,
  cdp: CDPSession,
  authenticatorId: string,
  cfg: LiveTenantConfig,
  serverBase: string,
): Promise<boolean> {
  const restored = await restoreCredential(cdp, authenticatorId);
  if (restored !== null && (await assertableCredentialIDs(cfg, serverBase)).includes(restored)) {
    return false;
  }

  const tenantBase = `${serverBase}/api/v1/iam/${encodeURIComponent(cfg.tenant)}/webauthn/registration`;
  const begin = await postJSON(
    `${tenantBase}/begin`,
    cfg,
    {},
    JSON.stringify({ email: cfg.email, display_name: cfg.email }),
  );
  if (!begin.sessionID) {
    throw new Error('registration/begin returned no X-Session-Id header');
  }

  const attestation = await createCredentialInPage(page, begin.body);
  await postJSON(`${tenantBase}/finish`, cfg, { 'X-Session-Id': begin.sessionID }, attestation);

  await saveCredential(cdp, authenticatorId);
  return true;
}
