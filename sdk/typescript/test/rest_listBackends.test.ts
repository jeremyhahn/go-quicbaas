// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Regression tests for RestOrchestratorClient.listBackends: ensures the
// envelope shape returned by /api/v1/orchestrator/backends ({backends: [...]})
// is correctly unwrapped to a bare AgentBackendInfo[] matching the Go SDK
// contract.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';
import type { AgentBackendInfo, JobRunnerInfo } from '../src/types.js';

const sample: AgentBackendInfo[] = [
  { name: 'ollama', type: 'ollama' } as AgentBackendInfo,
  { name: 'claude', type: 'acp' } as AgentBackendInfo,
];

const runnerSample: JobRunnerInfo[] = [
  { name: 'sdlc', states: [] } as JobRunnerInfo,
  { name: 'review', states: [] } as JobRunnerInfo,
];

function makeFetch(body: unknown): typeof globalThis.fetch {
  return vi.fn(async () =>
    new Response(JSON.stringify(body), {
      status: 200,
      headers: { 'content-type': 'application/json' },
    }),
  ) as unknown as typeof globalThis.fetch;
}

describe('RestOrchestratorClient.listBackends', () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it('unwraps {"backends": [...]} envelope', async () => {
    globalThis.fetch = makeFetch({ backends: sample });
    const client = new RestClient('https://example');
    const list = await client.orchestrator.listBackends();
    expect(Array.isArray(list)).toBe(true);
    expect(list).toEqual(sample);
    expect(list.find((b) => b.name === 'ollama')).toBeDefined();
  });

  it('passes through bare-array responses unchanged', async () => {
    globalThis.fetch = makeFetch(sample);
    const client = new RestClient('https://example');
    const list = await client.orchestrator.listBackends();
    expect(list).toEqual(sample);
  });

  it('returns empty array when envelope omits backends key', async () => {
    globalThis.fetch = makeFetch({});
    const client = new RestClient('https://example');
    const list = await client.orchestrator.listBackends();
    expect(list).toEqual([]);
  });

  it('follows next_offset until every backend is returned', async () => {
    const fetchMock = vi.fn(async (url: RequestInfo | URL) => {
      const requestURL = String(url);
      const body =
        requestURL.includes('offset=1')
          ? { backends: [sample[1]], total: 2, limit: 1, offset: 1 }
          : { backends: [sample[0]], total: 2, limit: 1, offset: 0, next_offset: 1 };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    const client = new RestClient('https://example');

    const list = await client.orchestrator.listBackends();

    expect(list).toEqual(sample);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });

  it('follows next_offset until every runner is returned', async () => {
    const fetchMock = vi.fn(async (url: RequestInfo | URL) => {
      const requestURL = String(url);
      const body =
        requestURL.includes('offset=1')
          ? { runners: [runnerSample[1]], total: 2, limit: 1, offset: 1 }
          : { runners: [runnerSample[0]], total: 2, limit: 1, offset: 0, next_offset: 1 };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    const client = new RestClient('https://example');

    const list = await client.orchestrator.listRunners();

    expect(list).toEqual(runnerSample);
    expect(fetchMock).toHaveBeenCalledTimes(2);
  });
});
