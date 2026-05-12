/**
 * fixtures/api.ts
 *
 * Thin REST helper for Playwright beforeAll/afterAll fixtures.
 * Calls the QuicBaaS REST API directly (not through the browser) so that
 * tests can seed and tear down server-side state deterministically.
 *
 * All calls use Node's native fetch with certificate verification disabled
 * via the NODE_TLS_REJECT_UNAUTHORIZED env override, which is acceptable
 * in a test harness against a locally-run development daemon.
 */

// Derive the server base URL from the same env var that playwright.config.ts
// uses so the two are always in sync.
function serverBase(): string {
  const uiUrl = process.env['QUICBAAS_UI_URL'] ?? 'http://localhost:18080/ui/';
  // Strip the /ui/ path suffix — the REST API lives at the root.
  return uiUrl.replace(/\/ui\/?$/, '');
}

// apiURL constructs a full REST path.
function apiURL(path: string): string {
  return `${serverBase()}/api/v1/orchestrator${path}`;
}

// apiFetch is a thin wrapper around Node fetch that always sends/receives JSON.
// TLS errors are suppressed via NODE_TLS_REJECT_UNAUTHORIZED=0 which the
// Makefile / CI wrapper sets before invoking Playwright.
async function apiFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json',
      ...(init?.headers as Record<string, string> | undefined),
    },
  });
  if (!resp.ok) {
    const body = await resp.text().catch(() => '');
    throw new Error(`API ${init?.method ?? 'GET'} ${url} failed ${resp.status}: ${body}`);
  }
  // Some endpoints return 204 No Content.
  const ct = resp.headers.get('content-type') ?? '';
  if (resp.status === 204 || !ct.includes('application/json')) {
    return undefined as T;
  }
  return resp.json() as Promise<T>;
}

// SpawnedSession holds the session_id created by spawnTestSession.
export interface SpawnedSession {
  session_id: string;
  backend: string;
}

// spawnTestSession creates a new session against the embedded ollama backend
// and returns the session_id.  Throws (and therefore fails the test) if the
// daemon is unreachable or the backend cannot start a session.
export async function spawnTestSession(tenantID = 'default'): Promise<SpawnedSession> {
  return apiFetch<SpawnedSession>(
    apiURL(`/sessions?tenant_id=${encodeURIComponent(tenantID)}`),
    {
      method: 'POST',
      body: JSON.stringify({ backend: 'ollama', tenant_id: tenantID }),
    },
  );
}

// closeTestSession terminates a session created by spawnTestSession.
// Errors are swallowed so afterAll cleanup never masks a real test failure.
export async function closeTestSession(sessionID: string, tenantID = 'default'): Promise<void> {
  try {
    await apiFetch<unknown>(
      apiURL(`/sessions/${encodeURIComponent(sessionID)}?tenant_id=${encodeURIComponent(tenantID)}`),
      { method: 'DELETE' },
    );
  } catch {
    // Cleanup failure is non-fatal.
  }
}

// AgentList mirrors the server's ListAgentsResponse.
interface AgentList {
  agents?: Array<{ agent_id: string; name: string; host: string; backend: string }>;
  count?: number;
}

// listTestAgents returns all agents registered for the given tenant.
// Throws if the daemon is unreachable, so beforeAll will fail fast.
export async function listTestAgents(tenantID = 'default'): Promise<AgentList> {
  return apiFetch<AgentList>(
    apiURL(`/agents?tenant_id=${encodeURIComponent(tenantID)}`),
  );
}

// =============================================================================
// Seal REST helpers
// =============================================================================

// sealApiURL constructs a full URL for seal endpoints.
function sealApiURL(path: string): string {
  return `${serverBase()}/api/v1/seal${path}`;
}

// SealStatus mirrors the daemon's GET /api/v1/seal/status response.
export interface SealStatus {
  sealed: boolean;
  initialized: boolean;
  mode: string;
  threshold: number;
  total_shares: number;
  shares_provided: number;
  version: number;
}

// SealInitResponse mirrors the daemon's POST /api/v1/seal/init response.
// shares is a [][]byte on the server, encoded as base64 strings in JSON.
export interface SealInitResponse {
  shares: string[];
  root_token: string;
  version: number;
}

// UnsealResponse mirrors the daemon's POST /api/v1/seal/unseal response.
export interface UnsealResponse {
  sealed: boolean;
  shares_provided: number;
  threshold: number;
}

// getSealStatus returns the current seal status.
// Throws if the daemon is unreachable or the seal subsystem is not enabled.
export async function getSealStatus(): Promise<SealStatus> {
  return apiFetch<SealStatus>(sealApiURL('/status'));
}

// initSeal initialises the seal with Shamir secret sharing.
// Returns the generated key shares as base64-encoded strings.
// This endpoint uses `threshold` and `total_shares` (not `secret_threshold`).
export async function initSeal(threshold: number, totalShares: number): Promise<SealInitResponse> {
  return apiFetch<SealInitResponse>(sealApiURL('/init'), {
    method: 'POST',
    body: JSON.stringify({ threshold, total_shares: totalShares }),
  });
}

// submitUnsealShare submits one Shamir key share toward unsealing.
// The share must be a base64-encoded string (as returned by initSeal).
export async function submitUnsealShare(share: string): Promise<UnsealResponse> {
  return apiFetch<UnsealResponse>(sealApiURL('/unseal'), {
    method: 'POST',
    body: JSON.stringify({ share }),
  });
}

// sealDatabase re-seals the database immediately.
export async function sealDatabase(): Promise<void> {
  await apiFetch<unknown>(sealApiURL('/seal'), { method: 'POST' });
}

// RekeyResponse mirrors the daemon's POST /api/v1/seal/rekey response.
export interface RekeyResponse {
  shares: string[];
  version: number;
}

// rekeyDatabase performs a Shamir rekey and returns the new shares.
// The database must be unsealed before calling this.
export async function rekeyDatabase(threshold: number, totalShares: number): Promise<RekeyResponse> {
  return apiFetch<RekeyResponse>(sealApiURL('/rekey'), {
    method: 'POST',
    body: JSON.stringify({ new_threshold: threshold, new_total_shares: totalShares }),
  });
}

// ensureSealInitialized ensures the seal is initialized, returning a set of
// usable key shares. If the seal is not initialized it calls initSeal; if it
// is already initialized and sealed it uses rekey after unsealing with the
// provided currentShares. When currentShares is not provided and the seal is
// already initialized, this function throws — the caller must supply shares.
//
// For test suites that always start from a known state, use initSeal directly.
export async function ensureSealShares(
  threshold: number,
  totalShares: number,
): Promise<{ shares: string[]; fresh: boolean }> {
  const status = await getSealStatus();
  if (!status.initialized) {
    const resp = await initSeal(threshold, totalShares);
    return { shares: resp.shares, fresh: true };
  }
  // Already initialized — return a sentinel so callers know to use rekey after
  // unsealing with externally-known shares.
  return { shares: [], fresh: false };
}

// =============================================================================
// IAM invite helpers
// =============================================================================

// iamApiURL constructs a full URL for IAM endpoints.
function iamApiURL(path: string): string {
  return `${serverBase()}/api/v1/iam${path}`;
}

// InviteMetadata mirrors GET /api/v1/iam/invites/{token}.
export interface InviteMetadata {
  user_id: string;
  email?: string;
  name?: string;
  auth_methods: string[];
  expires_at: string;
}

// getInviteMetadata fetches the invite metadata for the given token.
// Returns null when the token is not found (404) rather than throwing,
// so callers can gate tests on a valid token being present.
export async function getInviteMetadata(token: string): Promise<InviteMetadata | null> {
  const url = iamApiURL(`/invites/${encodeURIComponent(token)}`);
  const resp = await fetch(url, {
    headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
  });
  if (resp.status === 404) return null;
  if (!resp.ok) {
    const body = await resp.text().catch(() => '');
    throw new Error(`GET ${url} failed ${resp.status}: ${body}`);
  }
  return resp.json() as Promise<InviteMetadata>;
}

// CreateUserResponse mirrors the daemon's POST /api/v1/tenants/{id}/users response.
export interface CreateUserResponse {
  user_id: string;
  email: string;
  invite_token: string;
  invite_url: string;
  expires_at: string;
}

// deleteTestUser removes a user from the given tenant by user ID.
// Errors are swallowed so afterAll cleanup never masks a real test failure.
export async function deleteTestUser(tenantID = 'default', userID: string): Promise<void> {
  try {
    const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`;
    const resp = await fetch(url, { method: 'DELETE' });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteTestUser: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    // Cleanup failure is non-fatal.
    console.warn('deleteTestUser failed:', e);
  }
}

// createTestInvite creates a pending user in the given tenant and returns the
// invite token. Uses POST /api/v1/tenants/{tenant_id}/users which issues a
// one-time invite token stored in the daemon's in-memory invite store.
export async function createTestInvite(
  tenantID = 'default',
  email = 'e2e-test@quicbaas.test',
  name = 'E2E Test User',
): Promise<CreateUserResponse> {
  const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(tenantID)}/users`;
  // Include a unique suffix to avoid duplicate-email conflicts across runs.
  const uniqueEmail = email.replace('@', `+${Date.now()}@`);
  const username = `e2e-${Date.now()}`;
  return apiFetch<CreateUserResponse>(url, {
    method: 'POST',
    body: JSON.stringify({
      email: uniqueEmail,
      username,
      name,
      auth_methods: ['webauthn'],
      invite_expiry_hours: 1,
    }),
  });
}
