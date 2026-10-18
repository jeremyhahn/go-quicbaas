// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The server's QUIC listener speaks qrdb's frame protocol, not REST over
// HTTP/3, so the QUIC transport refuses every family it exposes. A refusal
// must never reach the network: a REST request aimed at the QUIC endpoint would
// fail against the real listener or silently ride TCP HTTP while claiming QUIC.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { QuicClient, QuicNotImplementedError } from '../src/quic';
import { createClient } from '../src/factory';
import { DeployNotSupportedError, TransportNotSupportedError } from '../src/errors';
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

// The remaining families. Orchestration, cost and worker image have no QUIC
// route on the server at all (go-quicbaas#819); auth, barrier, ceremony,
// bootstrap and the connection itself sit behind qrdb's frame protocol, which
// this SDK does not speak. Every method is enumerated from the prototype so a
// verb added later cannot slip in as a bare stub.

// refusalOf runs one verb to completion, draining it when it is a stream, and
// returns what it threw.
async function refusalOf(call: () => unknown): Promise<unknown> {
  try {
    const result = call();
    if (result !== null && typeof result === 'object' && Symbol.asyncIterator in result) {
      for await (const _event of result as AsyncIterable<unknown>) {
        return new Error('stream yielded an event');
      }
    } else {
      await result;
    }
  } catch (err) {
    return err;
  }
  return new Error('verb resolved');
}

function verbsOf(target: object): string[] {
  return Object.getOwnPropertyNames(Object.getPrototypeOf(target)).filter((name) => name !== 'constructor');
}

const familyCases: Array<{
  family: string;
  pick: (c: QuicClient) => object;
  reason: string;
  skip?: string[];
}> = [
  { family: 'orchestrator', pick: (c) => c.orchestrator, reason: "serves the orchestrator's unary methods", skip: ['deployFleetNode'] },
  { family: 'auth', pick: (c) => c.auth, reason: 'frame protocol' },
  { family: 'barrier', pick: (c) => c.barrier, reason: 'frame protocol' },
  { family: 'ceremony', pick: (c) => c.ceremony, reason: 'frame protocol' },
  {
    family: 'connection',
    pick: (c) => c.connection,
    reason: 'frame protocol',
    skip: ['isConnected', 'getAddress', 'getTransport'],
  },
];

describe('QuicClient — every other family refuses with a typed reason', () => {
  for (const tc of familyCases) {
    it(`refuses every ${tc.family} verb and sends nothing`, async () => {
      const target = tc.pick(new QuicClient('https://node.example:8443', { token: 'live-token' })) as Record<
        string,
        (...args: unknown[]) => unknown
      >;
      const verbs = verbsOf(target).filter((name) => !tc.skip?.includes(name));
      expect(verbs.length).toBeGreaterThan(0);

      for (const verb of verbs) {
        const err = await refusalOf(() => target[verb]('tenant-a', 'id-1'));
        expect(err, verb).toBeInstanceOf(QuicNotImplementedError);
        expect(err, verb).toBeInstanceOf(TransportNotSupportedError);
        const refusal = err as QuicNotImplementedError;
        expect(refusal.transport).toBe('quic');
        expect(refusal.family).toBe(tc.family);
        // A paged listing answers through its unpaged sibling.
        expect(refusal.method).toBe(verb.replace(/Page$/, ''));
        expect(refusal.reason).toContain(tc.reason);
        expect(refusal.message).toContain(`quic transport does not carry ${tc.family}.`);
      }
      expect(fetchMock).not.toHaveBeenCalled();
    });
  }

  it('opens no job-event subscription: subscribeJobEvents throws before returning a close handle', () => {
    const onEvent = vi.fn();
    const c = new QuicClient('https://node.example:8443');
    expect(() => c.orchestrator.subscribeJobEvents('tenant-a', 'job-1', onEvent)).toThrow(QuicNotImplementedError);
    expect(onEvent).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('ends the session stream with the refusal instead of yielding', async () => {
    const c = new QuicClient('https://node.example:8443');
    const stream = c.orchestrator.subscribeSession({ tenant_id: 'tenant-a', session_id: 's-1' })[Symbol.asyncIterator]();
    await expect(stream.next()).rejects.toBeInstanceOf(QuicNotImplementedError);
    await expect(stream.next()).resolves.toEqual({ done: true, value: undefined });
  });

  it('keeps deployFleetNode on its own typed refusal', async () => {
    const c = new QuicClient('https://node.example:8443');
    const err = await refusalOf(() => c.orchestrator.deployFleetNode({} as never));
    expect(err).toBeInstanceOf(DeployNotSupportedError);
  });
});
