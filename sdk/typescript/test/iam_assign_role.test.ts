// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Regression tests for the assignRoleToUser / revokeRoleFromUser wire format.
//
// Bug: assignRoleToUser was POSTing { "role": <name> } but the Go handler
// decodes { "role_id": <uuid> }.  The fix makes the SDK send { "role_id" }.
// These tests verify the corrected wire contract and guard against regression.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { APIError } from '../src/errors';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeOK(): Response {
  // fetchJSON always calls resp.json() regardless of status, so even for
  // operations that return 204 No Content in production we must supply a
  // parseable body in the mock (the test is about the request wire format,
  // not response handling).
  return new Response('{}', {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeError(status: number, message = 'error'): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// =============================================================================
// assignRoleToUser — wire field regression
// =============================================================================

describe('RestIAMClient.assignRoleToUser', () => {
  it('POSTs { role_id } to the correct URL', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost');
    await client.iam.assignRoleToUser('system', 'user-123', 'role-uuid-456');

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/tenants/system/users/user-123/roles');
    expect(init.method).toBe('POST');

    // The wire field MUST be "role_id" — the Go handler decodes this field and
    // passes it to IdentityStore.AssignRole which keys on the role UUID.
    const body = JSON.parse(init.body as string) as Record<string, unknown>;
    expect(body).toHaveProperty('role_id', 'role-uuid-456');

    // The old broken field name must NOT be present.
    expect(body).not.toHaveProperty('role');
  });

  it('URL-encodes tenant ID and user ID', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost');
    await client.iam.assignRoleToUser('my tenant', 'user/1', 'editor');

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('my%20tenant');
    expect(url).toContain('user%2F1');
  });

  it('throws APIError on 400 (handler rejects empty role_id)', async () => {
    fetchMock.mockResolvedValue(makeError(400, 'role_id is required'));

    const client = new RestClient('http://localhost');
    await expect(
      client.iam.assignRoleToUser('system', 'u1', ''),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 404 (role or user not found)', async () => {
    fetchMock.mockResolvedValue(makeError(404, 'role not found'));

    const client = new RestClient('http://localhost');
    await expect(
      client.iam.assignRoleToUser('system', 'u1', 'nonexistent-role'),
    ).rejects.toBeInstanceOf(APIError);
  });
});

// =============================================================================
// revokeRoleFromUser — passes role as URL path param (no body mismatch)
// =============================================================================

describe('RestIAMClient.revokeRoleFromUser', () => {
  it('sends DELETE to the correct URL with role ID in the path', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost');
    await client.iam.revokeRoleFromUser('system', 'user-123', 'role-uuid-456');

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/tenants/system/users/user-123/roles/role-uuid-456');
    expect(init.method).toBe('DELETE');
  });

  it('URL-encodes the role ID in the path', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost');
    await client.iam.revokeRoleFromUser('system', 'u1', 'role with spaces');

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('role%20with%20spaces');
  });

  it('throws APIError on 404', async () => {
    fetchMock.mockResolvedValue(makeError(404, 'role not assigned'));

    const client = new RestClient('http://localhost');
    await expect(
      client.iam.revokeRoleFromUser('system', 'u1', 'no-such-role'),
    ).rejects.toBeInstanceOf(APIError);
  });
});
