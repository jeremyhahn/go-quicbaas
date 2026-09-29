// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Regression tests for the SDK fleet admin path routing.
//
// The fleet registry is served at /api/v1/fleet, outside the orchestrator
// prefix. Neither retired path -- /api/v1/agents/:id/... nor
// /api/v1/orchestrator/agents/:id/... -- is registered by the server, so
// these tests pin the URL the SDK sends to the canonical
// /api/v1/fleet/:nodeID/disable|enable path so any drift is caught.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

// ---------------------------------------------------------------------------
// Global fetch mock lifecycle
// ---------------------------------------------------------------------------

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeOK(payload: unknown = {}): Response {
  return new Response(JSON.stringify(payload), {
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

// ---------------------------------------------------------------------------
// disableFleetNode — canonical URL path
// ---------------------------------------------------------------------------

describe('RestOrchestratorClient.disableFleetNode', () => {
  it('POSTs to /api/v1/fleet/:id/disable', async () => {
    fetchMock.mockResolvedValue(
      makeOK({ node_id: 'agent-42', disabled: true }),
    );

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.disableFleetNode({ node_id: 'agent-42', tenant_id: 'system' });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];

    // Must be under /api/v1/fleet — NOT a retired /agents path.
    expect(url).toBe(
      'http://localhost:8080/api/v1/fleet/agent-42/disable?tenant_id=system',
    );
    expect(init.method).toBe('POST');
  });

  it('URL-encodes the node ID', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.disableFleetNode({ node_id: 'agent/with/slash', tenant_id: 'system' });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('agent%2Fwith%2Fslash');
    // Confirm it is NOT at a retired /agents path.
    expect(url).not.toMatch(/\/agents\/[^/]+\/disable/);
  });

  it('URL-encodes the tenant_id query parameter', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.disableFleetNode({ node_id: 'a1', tenant_id: 'my tenant' });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    // URLSearchParams encodes spaces as '+' (application/x-www-form-urlencoded).
    // Both '+' and '%20' represent a space; assert the tenant ID is present.
    const parsedUrl = new URL(url);
    expect(parsedUrl.searchParams.get('tenant_id')).toBe('my tenant');
  });

  it('throws APIError on 404 (agent not found)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(404, 'agent not found'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.disableFleetNode({ node_id: 'missing', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 409 (already disabled)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(409, 'agent already disabled'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.disableFleetNode({ node_id: 'a1', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });
});

// ---------------------------------------------------------------------------
// enableFleetNode — canonical URL path
// ---------------------------------------------------------------------------

describe('RestOrchestratorClient.enableFleetNode', () => {
  it('POSTs to /api/v1/fleet/:id/enable', async () => {
    fetchMock.mockResolvedValue(
      makeOK({ node_id: 'agent-42', enabled: true }),
    );

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.enableFleetNode({ node_id: 'agent-42', tenant_id: 'system' });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];

    expect(url).toBe(
      'http://localhost:8080/api/v1/fleet/agent-42/enable?tenant_id=system',
    );
    expect(init.method).toBe('POST');
  });

  it('URL-encodes the node ID', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.enableFleetNode({ node_id: 'agent/with/slash', tenant_id: 'system' });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('agent%2Fwith%2Fslash');
    // Confirm it is NOT at a retired /agents path.
    expect(url).not.toMatch(/\/agents\/[^/]+\/enable/);
  });

  it('URL-encodes the tenant_id query parameter', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.enableFleetNode({ node_id: 'a1', tenant_id: 'my tenant' });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    // URLSearchParams encodes spaces as '+' (application/x-www-form-urlencoded).
    // Both '+' and '%20' represent a space; assert the tenant ID is present.
    const parsedUrl = new URL(url);
    expect(parsedUrl.searchParams.get('tenant_id')).toBe('my tenant');
  });

  it('throws APIError on 404 (agent not found)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(404, 'agent not found'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.enableFleetNode({ node_id: 'missing', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 409 (agent not disabled)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(409, 'agent not disabled'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.enableFleetNode({ node_id: 'a1', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });
});
