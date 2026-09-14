// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Tests for the spend-report verb across the TypeScript transports.
// go-quicbaas#794.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { QuicClient } from '../src/quic';
import { UnixClient } from '../src/unix';
import { McpClient } from '../src/mcp';
import { GrpcWebClient } from '../src/grpc_web';
import type { GetSpendReportResponse } from '../src/types';

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
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

// A tenant-level report with one paying account that carries no cap — the
// shape the burn-in deployment actually produces today.
const serverBody: GetSpendReportResponse = {
  level: 'tenant',
  tenant_id: 'acme',
  entries: 3,
  input_tokens: 120,
  output_tokens: 80,
  total_tokens: 200,
  cost_micros: 0,
  cost_measured: false,
  successes: 2,
  failures: 1,
  child_level: 'project',
  children: [
    { id: 'proj-1', level: 'project', total_tokens: 150 },
    { level: 'project', unattributed: true, total_tokens: 50 },
  ],
  accounts: [
    { account_id: 'acct-1', total_tokens: 200, policy_status: 'none', display_name: 'alice-work' },
  ],
  models: [{ engine: 'acp', model: 'claude-opus-5', total_tokens: 200 }],
};

// =============================================================================
// GET /api/v1/orchestrator/cost
// =============================================================================

describe('RestOrchestratorClient.getSpendReport', () => {
  it('requests the cost endpoint with the tenant scope', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    await new RestClient('http://localhost').orchestrator.getSpendReport({ tenant_id: 'acme' });
    expect(fetchMock.mock.calls[0][0]).toBe(
      'http://localhost/api/v1/orchestrator/cost?tenant_id=acme',
    );
  });

  it('sends every chain level the caller supplied, in the ledger order', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    await new RestClient('http://localhost').orchestrator.getSpendReport({
      tenant_id: 'acme',
      project_id: 'proj-1',
      task_id: 'task-1',
      job_id: 'job-1',
      session_id: 'sess-1',
    });
    const url = new URL(fetchMock.mock.calls[0][0] as string);
    expect(url.pathname).toBe('/api/v1/orchestrator/cost');
    expect(url.searchParams.get('project_id')).toBe('proj-1');
    expect(url.searchParams.get('task_id')).toBe('task-1');
    expect(url.searchParams.get('job_id')).toBe('job-1');
    expect(url.searchParams.get('session_id')).toBe('sess-1');
  });

  it('sends round 0 when round_set is true, and omits round otherwise', async () => {
    // A fresh Response per call: a Response body may only be read once.
    fetchMock.mockImplementation(() => Promise.resolve(makeResponse(serverBody)));
    const client = new RestClient('http://localhost').orchestrator;

    // Round 0 is a REAL level. A client that dropped it because the number is
    // falsy would silently widen the scope to every round.
    await client.getSpendReport({ tenant_id: 'acme', round: 0, round_set: true });
    expect(new URL(fetchMock.mock.calls[0][0] as string).searchParams.get('round')).toBe('0');

    await client.getSpendReport({ tenant_id: 'acme', round: 3 });
    expect(new URL(fetchMock.mock.calls[1][0] as string).searchParams.get('round')).toBeNull();
  });

  it('carries cost_measured so a renderer can tell unmeasured from free', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    const report = await new RestClient('http://localhost').orchestrator.getSpendReport({
      tenant_id: 'acme',
    });
    expect(report.cost_measured).toBe(false);
    expect(report.cost_micros).toBe(0);
    expect(report.total_tokens).toBe(200);
  });

  it('carries the drill-down, including the unattributed row', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    const report = await new RestClient('http://localhost').orchestrator.getSpendReport({
      tenant_id: 'acme',
    });
    expect(report.child_level).toBe('project');
    expect(report.children).toHaveLength(2);
    expect(report.children![1]!.unattributed).toBe(true);
    expect(report.children![1]!.id).toBeUndefined();
  });

  it('carries the account that paid and its policy status', async () => {
    fetchMock.mockResolvedValue(makeResponse(serverBody));
    const report = await new RestClient('http://localhost').orchestrator.getSpendReport({
      tenant_id: 'acme',
    });
    expect(report.accounts).toHaveLength(1);
    expect(report.accounts![0]!.account_id).toBe('acct-1');
    expect(report.accounts![0]!.policy_status).toBe('none');
  });

  it('throws an APIError carrying the status when the endpoint refuses', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'not recording spend' }, 503));
    await expect(
      new RestClient('http://localhost').orchestrator.getSpendReport({ tenant_id: 'acme' }),
    ).rejects.toMatchObject({ status: 503 });
  });
});

// =============================================================================
// Transport parity
// =============================================================================

describe('getSpendReport transport parity', () => {
  // The browser-side transports carry no orchestrator surface of their own;
  // they must REFUSE rather than resolve, so a caller never mistakes an
  // unimplemented transport for a tenant that has spent nothing.
  const unsupported: [string, () => Promise<unknown>][] = [
    ['quic', () => new QuicClient('https://localhost').orchestrator.getSpendReport({ tenant_id: 'a' })],
    ['unix', () => new UnixClient('/tmp/x.sock').orchestrator.getSpendReport({ tenant_id: 'a' })],
    ['mcp', () => new McpClient('http://localhost').orchestrator.getSpendReport({ tenant_id: 'a' })],
    [
      'grpc-web',
      () => new GrpcWebClient('http://localhost').orchestrator.getSpendReport({ tenant_id: 'a' }),
    ],
  ];

  it.each(unsupported)('%s refuses rather than reporting zero spend', async (_name, call) => {
    await expect(call()).rejects.toThrow();
  });
});
