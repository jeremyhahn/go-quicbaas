/**
 * fixtures/iam.ts
 *
 * REST helpers for IAM (roles / users) seeding and tear-down used by the
 * iam_rbac.spec.ts Playwright suite. These call the QuicBaaS REST API
 * directly (not through the browser) so tests can seed deterministic state.
 *
 * Uses the shared auth + fetch helpers from fixtures/_http.ts so the
 * admin-bearer token refresh logic is not duplicated across fixture files.
 */

import { serverBase, authHeader, apiFetch, SYSTEM_TENANT_ID } from './_http.ts';

export { SYSTEM_TENANT_ID };

// ---------------------------------------------------------------------------
// Role fixtures
// ---------------------------------------------------------------------------

export interface IAMRole {
  id: string;
  name: string;
  description?: string;
  tenant_id: string;
}

// createTestRole creates a role in the given tenant and returns the server
// response. Uses a unique suffix so parallel test runs don't collide.
export async function createTestRole(
  name: string,
  tenantID = SYSTEM_TENANT_ID,
): Promise<IAMRole> {
  const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(tenantID)}/roles`;
  return apiFetch<IAMRole>(url, {
    method: 'POST',
    body: JSON.stringify({ name, description: `E2E test role ${name}` }),
  });
}

// deleteTestRole removes a role by ID. Errors are swallowed so afterAll
// cleanup never masks a real test failure.
export async function deleteTestRole(
  roleID: string,
  tenantID = SYSTEM_TENANT_ID,
): Promise<void> {
  try {
    const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/${encodeURIComponent(roleID)}`;
    const resp = await fetch(url, { method: 'DELETE', headers: authHeader() });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteTestRole: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteTestRole failed:', e);
  }
}

// ---------------------------------------------------------------------------
// User fixtures
// ---------------------------------------------------------------------------

export interface IAMUser {
  id: string;
  email: string;
  username?: string;
  tenant_id: string;
  invite_token?: string;
  invite_url?: string;
}

// listTestUsers returns all users in the given tenant. Throws if the daemon
// is unreachable.
export async function listTestUsers(
  tenantID = SYSTEM_TENANT_ID,
): Promise<IAMUser[]> {
  const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(tenantID)}/users`;
  const resp = await apiFetch<{ users?: IAMUser[] }>(url);
  return resp.users ?? [];
}

// deleteTestUser removes a user by ID. Errors are swallowed.
export async function deleteTestUser(
  userID: string,
  tenantID = SYSTEM_TENANT_ID,
): Promise<void> {
  try {
    const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`;
    const resp = await fetch(url, { method: 'DELETE', headers: authHeader() });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteTestUser: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteTestUser failed:', e);
  }
}
