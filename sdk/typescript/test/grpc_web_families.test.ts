// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// go-qrdb serves no gRPC-Web endpoint yet and this SDK has no
// @connectrpc/connect-web client, so the gRPC-Web transport refuses every verb
// of every family it exposes with GrpcWebNotImplementedError, a
// TransportNotSupportedError naming `family.verb`. A refusal must never reach
// the network. Every method is enumerated from the prototype so a verb added
// later cannot slip in as a bare stub.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { GrpcWebClient, GrpcWebNotImplementedError } from '../src/grpc_web';
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

function expectRefusal(err: unknown, family: string, method: string): void {
  const label = `${family}.${method}`;
  expect(err, label).toBeInstanceOf(GrpcWebNotImplementedError);
  expect(err, label).toBeInstanceOf(TransportNotSupportedError);
  const refusal = err as GrpcWebNotImplementedError;
  expect(refusal.name, label).toBe('GrpcWebNotImplementedError');
  expect(refusal.transport, label).toBe('grpc-web');
  expect(refusal.family, label).toBe(family);
  expect(refusal.method, label).toBe(method);
  expect(refusal.reason, label).toContain('@connectrpc/connect-web');
  expect(refusal.message, label).toBe(`grpc-web transport does not carry ${family}.${method}: ${refusal.reason}`);
}

function verbsOf(target: object): string[] {
  return Object.getOwnPropertyNames(Object.getPrototypeOf(target)).filter((name) => name !== 'constructor');
}

const familyCases: Array<{
  family: string;
  pick: (c: Client) => object;
  skip?: string[];
}> = [
  {
    family: 'connection',
    pick: (c) => c.connection,
    skip: ['isConnected', 'getAddress', 'getTransport'],
  },
  { family: 'orchestrator', pick: (c) => c.orchestrator, skip: ['deployFleetNode'] },
  { family: 'auth', pick: (c) => c.auth },
  { family: 'barrier', pick: (c) => c.barrier },
  { family: 'ceremony', pick: (c) => c.ceremony },
];

async function expectEveryVerbRefuses(c: Client): Promise<void> {
  for (const tc of familyCases) {
    const target = tc.pick(c) as Record<string, (...args: unknown[]) => unknown>;
    const verbs = verbsOf(target).filter((name) => !tc.skip?.includes(name));
    expect(verbs.length, tc.family).toBeGreaterThan(0);
    for (const verb of verbs) {
      const err = await refusalOf(() => target[verb]('tenant-a', 'id-1'));
      // A paged listing answers through its unpaged sibling.
      expectRefusal(err, tc.family, verb.replace(/Page$/, ''));
    }
  }
  expect(fetchMock).not.toHaveBeenCalled();
}

// familyCalls names verbs of the families GrpcWebClient builds as proxies,
// which have no prototype to walk. Each call is deferred until it is checked.
function familyCalls(c: Client): Array<[string, string, () => Promise<unknown>]> {
  return [
    ['projects', 'createProject', () => c.projects.createProject({ tenant_id: 'tenant-a', name: 'api' })],
    ['projects', 'updateProject', () => c.projects.updateProject({ tenant_id: 'tenant-a', project_id: 'proj-1', metadata: {} })],
    ['sdlc', 'binding', () => c.sdlc.binding('tenant-a', 'proj-1')],
    ['accounts', 'listAccountsPage', () => c.accounts.listAccountsPage({ tenant_id: 'tenant-a' })],
    ['accounts', 'deleteAccount', () => c.accounts.deleteAccount('tenant-a', 'acct-1')],
    ['prompts', 'listPrompts', () => c.prompts.listPrompts({ set: 'sdlc' })],
  ];
}

async function expectNamedRefusals(c: Client): Promise<void> {
  for (const [family, method, call] of familyCalls(c)) {
    expectRefusal(await refusalOf(call), family, method);
  }
  expect(fetchMock).not.toHaveBeenCalled();
}

describe('GrpcWebClient — every verb refuses with GrpcWebNotImplementedError', () => {
  for (const tc of familyCases) {
    it(`refuses every ${tc.family} verb and sends nothing`, async () => {
      const c = new GrpcWebClient('http://node.example', { token: 'live-token' });
      const target = tc.pick(c) as Record<string, (...args: unknown[]) => unknown>;
      const verbs = verbsOf(target).filter((name) => !tc.skip?.includes(name));
      expect(verbs.length).toBeGreaterThan(0);
      for (const verb of verbs) {
        const err = await refusalOf(() => target[verb]('tenant-a', 'id-1'));
        expectRefusal(err, tc.family, verb.replace(/Page$/, ''));
      }
      expect(fetchMock).not.toHaveBeenCalled();
    });
  }

  it('refuses projects, sdlc, accounts and prompts verbs and sends nothing', async () => {
    await expectNamedRefusals(new GrpcWebClient('http://node.example', { token: 'live-token' }));
  });

  it('opens no job-event subscription: subscribeJobEvents throws before returning a close handle', () => {
    const onEvent = vi.fn();
    const c = new GrpcWebClient('http://node.example');
    expect(() => c.orchestrator.subscribeJobEvents('tenant-a', 'job-1', onEvent)).toThrow(GrpcWebNotImplementedError);
    expect(onEvent).not.toHaveBeenCalled();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('ends the session stream with the refusal instead of yielding', async () => {
    const c = new GrpcWebClient('http://node.example');
    const stream = c.orchestrator.subscribeSession({ tenant_id: 'tenant-a', session_id: 's-1' })[Symbol.asyncIterator]();
    await expect(stream.next()).rejects.toBeInstanceOf(GrpcWebNotImplementedError);
    await expect(stream.next()).resolves.toEqual({ done: true, value: undefined });
  });

  it('keeps deployFleetNode on its own typed refusal', async () => {
    const c = new GrpcWebClient('http://node.example');
    const err = await refusalOf(() => c.orchestrator.deployFleetNode({} as never));
    expect(err).toBeInstanceOf(DeployNotSupportedError);
  });
});

describe('createClient — grpc-web', () => {
  it('returns a client whose every verb gives the same refusals', async () => {
    const client = await createClient({ transport: 'grpc-web', baseURL: 'http://node.example' });
    await expectEveryVerbRefuses(client);
    await expectNamedRefusals(client);
  });
});
