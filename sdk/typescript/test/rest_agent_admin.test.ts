// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Regression tests for SDK agent admin path routing (BUG 1).
//
// Bug: disableAgent and enableAgent had a comment saying they mapped to
// /api/v1/agents/:id/... (without the orchestrator prefix). The SDK's
// RestOrchestratorClient.fetchJSON prepends API_PREFIX = '/api/v1/orchestrator',
// so the ACTUAL URL sent was always /api/v1/orchestrator/agents/:id/disable|enable.
// The server, however, registered those handlers at /api/v1/agents/:id/... —
// outside the orchestrator subrouter — causing 404s.
//
// Fix: the server now registers disable/enable inside the /orchestrator
// subrouter at /api/v1/orchestrator/agents/{agentID}/disable|enable,
// matching what the SDK already sends.
//
// These tests verify that the SDK sends requests to the correct
// /api/v1/orchestrator/agents/... path so any future drift is caught.

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
// disableAgent — canonical URL path
// ---------------------------------------------------------------------------

describe('RestOrchestratorClient.disableAgent', () => {
  it('POSTs to /api/v1/orchestrator/agents/:id/disable', async () => {
    fetchMock.mockResolvedValue(
      makeOK({ agent_id: 'agent-42', disabled: true }),
    );

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.disableAgent({ agent_id: 'agent-42', tenant_id: 'system' });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];

    // Must be under the orchestrator prefix — NOT the old /api/v1/agents/... path.
    expect(url).toBe(
      'http://localhost:8080/api/v1/orchestrator/agents/agent-42/disable?tenant_id=system',
    );
    expect(init.method).toBe('POST');
  });

  it('URL-encodes the agent ID', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.disableAgent({ agent_id: 'agent/with/slash', tenant_id: 'system' });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('agent%2Fwith%2Fslash');
    // Confirm it is NOT at the old non-orchestrator path.
    expect(url).not.toMatch(/\/api\/v1\/agents\/[^/]+\/disable/);
  });

  it('URL-encodes the tenant_id query parameter', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.disableAgent({ agent_id: 'a1', tenant_id: 'my tenant' });

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
      client.orchestrator.disableAgent({ agent_id: 'missing', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 409 (already disabled)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(409, 'agent already disabled'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.disableAgent({ agent_id: 'a1', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });
});

// ---------------------------------------------------------------------------
// enableAgent — canonical URL path
// ---------------------------------------------------------------------------

describe('RestOrchestratorClient.enableAgent', () => {
  it('POSTs to /api/v1/orchestrator/agents/:id/enable', async () => {
    fetchMock.mockResolvedValue(
      makeOK({ agent_id: 'agent-42', enabled: true }),
    );

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.enableAgent({ agent_id: 'agent-42', tenant_id: 'system' });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];

    expect(url).toBe(
      'http://localhost:8080/api/v1/orchestrator/agents/agent-42/enable?tenant_id=system',
    );
    expect(init.method).toBe('POST');
  });

  it('URL-encodes the agent ID', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.enableAgent({ agent_id: 'agent/with/slash', tenant_id: 'system' });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('agent%2Fwith%2Fslash');
    // Confirm it is NOT at the old non-orchestrator path.
    expect(url).not.toMatch(/\/api\/v1\/agents\/[^/]+\/enable/);
  });

  it('URL-encodes the tenant_id query parameter', async () => {
    fetchMock.mockResolvedValue(makeOK());

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.enableAgent({ agent_id: 'a1', tenant_id: 'my tenant' });

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
      client.orchestrator.enableAgent({ agent_id: 'missing', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 409 (agent not disabled)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(409, 'agent not disabled'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.enableAgent({ agent_id: 'a1', tenant_id: 'system' }),
    ).rejects.toBeInstanceOf(APIError);
  });
});
