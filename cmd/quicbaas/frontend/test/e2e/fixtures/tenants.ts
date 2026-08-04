/**
 * fixtures/tenants.ts
 *
 * REST helpers for seeding and tearing down test tenants in beforeAll/afterAll
 * hooks. Used by tenant_barrier.spec.ts and any future spec that needs a live
 * tenant provisioned out-of-band.
 *
 * Create:  POST /api/v1/tenants    body: { id, name }
 * Delete:  DELETE /api/v1/tenants/{id}
 *
 * Uses the shared auth + fetch helpers from fixtures/_http.ts.
 *
 * Verified REST paths against pkg/api/rest/iam.go and iam_tenants.go:
 *   POST   /api/v1/tenants          → IAMHandler.CreateTenant (body: { id, name })
 *   DELETE /api/v1/tenants/{id}     → IAMHandler.DeleteTenant
 */

import { serverBase, authHeader, apiFetch } from './_http.ts';
export { SYSTEM_TENANT_ID } from './_http.ts';

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

/**
 * CreatedTenant holds the id and name returned by POST /api/v1/tenants.
 * The daemon echoes back at minimum { id, name } in the IAMTenant shape.
 */
export interface CreatedTenant {
  id: string;
  name: string;
}

// ---------------------------------------------------------------------------
// Public helpers
// ---------------------------------------------------------------------------

/**
 * createTestTenant provisions a fresh tenant via the admin REST API.
 *
 * The tenant ID is generated from a timestamp suffix to avoid duplicate-id
 * conflicts across parallel runs. Both `id` and `name` are required by the
 * CreateTenant handler (pkg/api/rest/iam_tenants.go lines 120–129).
 *
 * @param baseName  Short slug used as a prefix for both id and name.
 * @param _isolationMode  Reserved parameter — not yet wired into the admin
 *   create-tenant endpoint (isolation_mode is only on the self-register path).
 *   Passed here so callers can document intent without silent API differences.
 */
export async function createTestTenant(
  baseName = 'e2e-test-tenant',
  _isolationMode?: string,
): Promise<CreatedTenant> {
  const suffix = Date.now();
  const slug = baseName.replace(/[^a-z0-9]/gi, '-').toLowerCase();
  const id = `${slug}-${suffix}`;
  const url = `${serverBase()}/api/v1/tenants`;
  return apiFetch<CreatedTenant>(url, {
    method: 'POST',
    body: JSON.stringify({ id, name: `${slug}-${suffix}` }),
  });
}

/**
 * deleteTestTenant removes a tenant by ID.
 *
 * Errors are swallowed (non-fatal) so afterAll cleanup never masks a real test
 * failure. 404 responses are silently ignored (tenant was never created or was
 * already cleaned up by a previous run).
 */
export async function deleteTestTenant(id: string): Promise<void> {
  try {
    const url = `${serverBase()}/api/v1/tenants/${encodeURIComponent(id)}`;
    const resp = await fetch(url, {
      method: 'DELETE',
      headers: { ...authHeader(), Accept: 'application/json' },
    });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteTestTenant: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteTestTenant failed (non-fatal):', e);
  }
}
