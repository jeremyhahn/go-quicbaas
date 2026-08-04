/**
 * fixtures/security_seed.ts
 *
 * REST helpers for seeding and tearing down KMS keys, secrets, SSH keys, and
 * KV entries during Playwright e2e tests. Used exclusively in afterAll hooks
 * to clean up server-side state created by the operational round-trip suites.
 *
 * Uses the shared auth + fetch helpers from fixtures/_http.ts.
 *
 * Verified REST paths against pkg/api/rest/server.go:
 *   DELETE /api/v1/kms/keys/{name}                 → HandleDeleteKey    (line 459)
 *   DELETE /api/v1/secrets/{path}?destroy=true      → HandleDeleteSecret (line 496)
 *   DELETE /api/v1/ssh/keys/{scope}/{owner}/{name}  → HandleDeleteSSHKey (line 533)
 *   DELETE /api/v1/kv/{type}/{key}                 → HandleKVDelete     (line 437)
 */

import { serverBase, authFetch } from './_http.ts';

// ---------------------------------------------------------------------------
// Public cleanup helpers — errors swallowed so afterAll never masks failures
// ---------------------------------------------------------------------------

/**
 * deleteKMSKey deletes a KMS key by name or ID.
 * Maps to DELETE /api/v1/kms/keys/{name}  (pkg/api/rest/server.go line 459).
 */
export async function deleteKMSKey(keyNameOrID: string): Promise<void> {
  if (!keyNameOrID) return;
  try {
    const url = `${serverBase()}/api/v1/kms/keys/${encodeURIComponent(keyNameOrID)}`;
    const resp = await authFetch(url, { method: 'DELETE' });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteKMSKey: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteKMSKey failed (non-fatal):', e);
  }
}

/**
 * deleteSecret permanently destroys all versions of a secret at the given path.
 * Maps to DELETE /api/v1/secrets/{path}?destroy=true  (pkg/api/rest/server.go line 496).
 *
 * The path argument must NOT be URL-encoded (e.g. "e2e/roundtrip-123"); this
 * function encodes each segment individually so multi-segment paths survive.
 */
export async function deleteSecret(secretPath: string): Promise<void> {
  if (!secretPath) return;
  try {
    // Preserve path separators; encode each segment separately.
    const encodedPath = secretPath.split('/').map(encodeURIComponent).join('/');
    const url = `${serverBase()}/api/v1/secrets/${encodedPath}?destroy=true`;
    const resp = await authFetch(url, { method: 'DELETE' });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteSecret: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteSecret failed (non-fatal):', e);
  }
}

/**
 * deleteSSHKey deletes a scoped SSH key by scope, owner, and name.
 * Maps to DELETE /api/v1/ssh/keys/{scope}/{owner}/{name}  (server.go line 533).
 */
export async function deleteSSHKey(scope: string, owner: string, name: string): Promise<void> {
  if (!name) return;
  try {
    const url = [
      serverBase(),
      '/api/v1/ssh/keys/',
      encodeURIComponent(scope),
      '/',
      encodeURIComponent(owner),
      '/',
      encodeURIComponent(name),
    ].join('');
    const resp = await authFetch(url, { method: 'DELETE' });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteSSHKey: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteSSHKey failed (non-fatal):', e);
  }
}

/**
 * deleteKVEntry deletes a KV entry by entity type and string key.
 * Maps to DELETE /api/v1/kv/{type}/{key}  (pkg/api/rest/server.go line 437).
 */
export async function deleteKVEntry(entityType: string, key: string): Promise<void> {
  if (!key) return;
  try {
    const url = `${serverBase()}/api/v1/kv/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`;
    const resp = await authFetch(url, { method: 'DELETE' });
    if (!resp.ok && resp.status !== 404) {
      console.warn(`deleteKVEntry: DELETE ${url} returned ${resp.status}`);
    }
  } catch (e) {
    console.warn('deleteKVEntry failed (non-fatal):', e);
  }
}
