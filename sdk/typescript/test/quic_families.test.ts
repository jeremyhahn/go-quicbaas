// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The server's QUIC listener speaks qrdb's frame protocol, not REST over
// HTTP/3, so the QUIC transport refuses projects, sdlc and accounts. A refusal
// must never reach the network: a REST request aimed at the QUIC endpoint would
// fail against the real listener or silently ride TCP HTTP while claiming QUIC.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { QuicClient } from '../src/quic';
import { createClient } from '../src/factory';
import { TransportNotSupportedError } from '../src/errors';
import type { Client } from '../src/interfaces';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function familyCalls(c: Client): Array<[string, string, Promise<unknown>]> {
  return [
    ['projects', 'createProject', c.projects.createProject({ tenant_id: 'tenant-a', name: 'api' })],
    ['projects', 'updateProject', c.projects.updateProject({ tenant_id: 'tenant-a', project_id: 'proj-1', metadata: {} })],
    ['sdlc', 'binding', c.sdlc.binding('tenant-a', 'proj-1')],
    ['accounts', 'listAccountsPage', c.accounts.listAccountsPage({ tenant_id: 'tenant-a' })],
    ['accounts', 'deleteAccount', c.accounts.deleteAccount('tenant-a', 'acct-1')],
  ];
}

async function expectRefusals(c: Client): Promise<void> {
  for (const [family, method, call] of familyCalls(c)) {
    const err = await call.catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TransportNotSupportedError);
    const refusal = err as TransportNotSupportedError;
    expect(refusal.transport).toBe('quic');
    expect(refusal.family).toBe(family);
    expect(refusal.method).toBe(method);
    expect(refusal.reason).toContain('frame protocol');
  }
  expect(fetchMock).not.toHaveBeenCalled();
}

describe('QuicClient — projects, sdlc and accounts', () => {
  it('refuses every verb with the quic transport named and sends nothing', async () => {
    await expectRefusals(new QuicClient('https://node.example:8443', { token: 'live-token' }));
  });

  it('is not mistaken for a promise when awaited', async () => {
    const c = new QuicClient('https://node.example:8443');
    expect(await Promise.resolve(c.accounts)).toBe(c.accounts);
  });
});

describe('createClient — quic', () => {
  it('returns a client whose families refuse rather than fall back to HTTP', async () => {
    const client = await createClient({ transport: 'quic', quic: { endpoint: 'https://node.example:8443' } });
    await expectRefusals(client);
  });
});
