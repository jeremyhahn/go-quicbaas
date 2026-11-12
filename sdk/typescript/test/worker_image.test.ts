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

// Tests for the worker image refresh verbs across the TypeScript transports.
// go-quicbaas#1021.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import { WailsClient } from '../src/wails';
import { QuicClient } from '../src/quic';
import { UnixClient } from '../src/unix';
import { McpClient } from '../src/mcp';
import { GrpcWebClient } from '../src/grpc_web';
import type { WorkerImageStatus } from '../src/types';

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

// The shape the server encodes: workerimage.Status.
const status: WorkerImageStatus = {
  serving: true,
  running: false,
  queued: false,
  last: {
    trigger: 'manual',
    outcome: 'promoted',
    started_at: '2026-09-22T10:00:00Z',
    finished_at: '2026-09-22T10:20:00Z',
    latest: { claude_code: '2.1.280', codex: '0.156.0' },
    bound: [
      {
        image: 'quicbaas-software-factory-worker:0.10.0-alpha',
        agents: { claude_code: '2.1.278', codex: '0.150.0' },
        uses: 2,
        stale: true,
      },
    ],
    reference: 'reg:3000/o/w:0.11.0-alpha-cc2.1.280-cx0.156.0@sha256:abc',
    probes: [{ name: 'toolchain, signing and both agents', passed: true }],
  },
};

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe('RestOrchestratorClient worker image verbs', () => {
  it('reads the status from the SDLC route, outside the orchestrator prefix', async () => {
    fetchMock.mockResolvedValue(makeResponse(status));
    const got = await new RestClient('http://localhost').orchestrator.getWorkerImageStatus();
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/sdlc/worker-image');
    expect(got.last?.bound?.[0]?.agents.claude_code).toBe('2.1.278');
    expect(got.last?.latest.claude_code).toBe('2.1.280');
  });

  it('requests a pass with a POST and accepts the 202', async () => {
    fetchMock.mockResolvedValue(makeResponse({ ...status, queued: true }, 202));
    const got = await new RestClient('http://localhost').orchestrator.refreshWorkerImage();
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe('http://localhost/api/v1/sdlc/worker-image/refresh');
    expect(init.method).toBe('POST');
    expect(got.queued).toBe(true);
  });

  it('surfaces a busy loop as an APIError with the status', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'worker image: a refresh is already in progress' }, 409));
    await expect(new RestClient('http://localhost').orchestrator.refreshWorkerImage()).rejects.toMatchObject({
      status: 409,
    });
  });
});

describe('WailsOrchestratorClient worker image verbs', () => {
  const getStatus = vi.fn();
  const refresh = vi.fn();

  beforeEach(() => {
    Object.defineProperty(globalThis, 'window', {
      value: {
        go: { main: { OrchestratorBinding: { GetWorkerImageStatus: getStatus, RefreshWorkerImage: refresh } } },
        runtime: { EventsOn: vi.fn(), EventsOff: vi.fn() },
      },
      writable: true,
      configurable: true,
    });
  });

  afterEach(() => {
    Object.defineProperty(globalThis, 'window', { value: undefined, writable: true, configurable: true });
    vi.clearAllMocks();
  });

  it('delegates both verbs to the Go binding', async () => {
    getStatus.mockResolvedValue(status);
    refresh.mockResolvedValue({ ...status, queued: true });
    const client = new WailsClient();
    expect((await client.orchestrator.getWorkerImageStatus()).last?.outcome).toBe('promoted');
    expect((await client.orchestrator.refreshWorkerImage()).queued).toBe(true);
    expect(getStatus).toHaveBeenCalledTimes(1);
    expect(refresh).toHaveBeenCalledTimes(1);
  });
});

describe('worker image transport parity', () => {
  const unsupported: [string, () => Promise<unknown>][] = [
    ['quic', () => new QuicClient('https://localhost').orchestrator.refreshWorkerImage()],
    ['unix', () => new UnixClient('/tmp/x.sock').orchestrator.getWorkerImageStatus()],
    ['mcp', () => new McpClient('http://localhost').orchestrator.refreshWorkerImage()],
    ['grpc-web', () => new GrpcWebClient('http://localhost').orchestrator.getWorkerImageStatus()],
  ];

  it.each(unsupported)('%s refuses rather than reporting an idle loop', async (_name, call) => {
    await expect(call()).rejects.toThrow();
  });
});
