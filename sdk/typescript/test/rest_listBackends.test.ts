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
import type { AgentBackendInfo } from '../src/types.js';

const sample: AgentBackendInfo[] = [
  { name: 'ollama', type: 'ollama' } as AgentBackendInfo,
  { name: 'claude', type: 'acp' } as AgentBackendInfo,
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
});
