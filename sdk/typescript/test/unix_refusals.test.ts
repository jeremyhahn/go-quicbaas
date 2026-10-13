// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The server's Unix listener speaks native gRPC only, and this SDK carries no
// gRPC client. Every Unix verb must therefore reject with
// TransportNotSupportedError naming the family, the verb and the reason, so a
// caller can tell "this transport cannot ask" from a server-side failure, and
// no verb ever resolves with an answer it never fetched.

import { describe, it, expect } from 'vitest';
import { UnixClient } from '../src/unix';
import { DeployNotSupportedError, TransportNotSupportedError } from '../src/errors';

// outcome runs one verb and returns what it rejected or threw with: a verb may
// throw synchronously (subscribeJobEvents), return a promise, or return an
// async iterable that fails on its first pull (subscribeSession).
async function outcome(call: () => unknown): Promise<unknown> {
  try {
    const result = call();
    if (result !== null && typeof result === 'object' && Symbol.asyncIterator in result) {
      await (result as AsyncIterable<unknown>)[Symbol.asyncIterator]().next();
      return undefined;
    }
    await result;
    return undefined;
  } catch (e: unknown) {
    return e;
  }
}

// verbs lists every method a client object declares, constructor excluded.
function verbs(target: object): string[] {
  return Object.getOwnPropertyNames(Object.getPrototypeOf(target)).filter(
    (name) => name !== 'constructor',
  );
}

function expectRefusal(err: unknown, family: string, method: string): void {
  expect(err).toBeInstanceOf(TransportNotSupportedError);
  const refusal = err as TransportNotSupportedError;
  expect(refusal.transport).toBe('unix');
  expect(refusal.family).toBe(family);
  expect(refusal.method).toBe(method);
  expect(refusal.reason).toContain('no Node gRPC client');
  expect(refusal.message).toBe(`unix transport does not carry ${family}.${method}: ${refusal.reason}`);
}

describe('unix transport refusals', () => {
  const client = new UnixClient('/run/quicbaas.sock');
  const orchestrator = client.orchestrator as unknown as Record<string, (...args: unknown[]) => unknown>;
  const auth = client.auth as unknown as Record<string, (...args: unknown[]) => unknown>;

  const orchestratorVerbs = verbs(client.orchestrator).filter((v) => v !== 'deployFleetNode');

  it.each(orchestratorVerbs)('orchestrator.%s rejects with TransportNotSupportedError', async (verb) => {
    const err = await outcome(() => orchestrator[verb]('tenant-a', {}));
    // A *Page verb refuses through its unpaged sibling.
    expectRefusal(err, 'orchestrator', verb.replace(/Page$/, ''));
  });

  it.each(verbs(client.auth))('auth.%s rejects with TransportNotSupportedError', async (verb) => {
    const err = await outcome(() => auth[verb]({}));
    expectRefusal(err, 'auth', verb);
  });

  it('connect and disconnect reject with TransportNotSupportedError', async () => {
    expectRefusal(await outcome(() => client.connection.connect('/run/quicbaas.sock')), 'connection', 'connect');
    expectRefusal(await outcome(() => client.connection.disconnect()), 'connection', 'disconnect');
    expect(client.connection.isConnected()).toBe(false);
    expect(client.connection.getAddress()).toBe('/run/quicbaas.sock');
    expect(client.connection.getTransport()).toBe('unix');
  });

  it('subscribeJobEvents refuses before it hands back an unsubscribe', () => {
    const events: unknown[] = [];
    expect(() => client.orchestrator.subscribeJobEvents('tenant-a', 'job-1', (e) => events.push(e)))
      .toThrow(TransportNotSupportedError);
    expect(events).toEqual([]);
  });

  it('deployFleetNode keeps its own DeployNotSupportedError', async () => {
    const err = await outcome(() => client.orchestrator.deployFleetNode({} as never));
    expect(err).toBeInstanceOf(DeployNotSupportedError);
  });
});
