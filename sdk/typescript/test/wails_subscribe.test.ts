// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for WailsOrchestratorClient: session subscribe, spawn, history.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { SessionEventEnvelope, SubscribeSessionEventsRequest } from '../src/types.js';
import { WailsClient } from '../src/wails.js';

// ---------------------------------------------------------------------------
// Window / Wails stub setup
// ---------------------------------------------------------------------------

type EventCallback = (...args: unknown[]) => void;

interface WailsWindowStub {
  go: {
    main: {
      ConnectionBinding: {
        Connect: ReturnType<typeof vi.fn>;
        Disconnect: ReturnType<typeof vi.fn>;
      };
      OrchestratorBinding: Record<string, ReturnType<typeof vi.fn>>;
      SessionBinding: {
        Subscribe: ReturnType<typeof vi.fn>;
        GetHistory: ReturnType<typeof vi.fn>;
        Spawn: ReturnType<typeof vi.fn>;
        Prompt: ReturnType<typeof vi.fn>;
        Close: ReturnType<typeof vi.fn>;
      };
    };
  };
  runtime: {
    EventsOn: ReturnType<typeof vi.fn>;
    EventsOff: ReturnType<typeof vi.fn>;
  };
}

function makeWailsStub(): WailsWindowStub {
  const eventListeners: Map<string, EventCallback> = new Map();

  return {
    go: {
      main: {
        ConnectionBinding: {
          Connect: vi.fn().mockResolvedValue(undefined),
          Disconnect: vi.fn().mockResolvedValue(undefined),
        },
        OrchestratorBinding: {},
        SessionBinding: {
          Subscribe: vi.fn().mockResolvedValue([]),
          GetHistory: vi.fn().mockResolvedValue({ events: [], next_seq: 0 }),
          Spawn: vi.fn().mockResolvedValue({ session_id: 'spawned', backend: 'claude' }),
          Prompt: vi.fn().mockResolvedValue({ session_id: 's', response: 'ok', done: true }),
          Close: vi.fn().mockResolvedValue({ session_id: 's', closed: true }),
        },
      },
    },
    runtime: {
      EventsOn: vi.fn((topic: string, cb: EventCallback) => {
        eventListeners.set(topic, cb);
        // Return an unsubscribe function.
        return () => eventListeners.delete(topic);
      }),
      EventsOff: vi.fn((topic: string) => eventListeners.delete(topic)),
    },
  };
}

// Consumes up to `count` items from an AsyncIterable, then stops.
async function takeN<T>(iter: AsyncIterable<T>, n: number): Promise<T[]> {
  const results: T[] = [];
  for await (const item of iter) {
    results.push(item);
    if (results.length >= n) break;
  }
  return results;
}

async function collectAll<T>(iter: AsyncIterable<T>): Promise<T[]> {
  const results: T[] = [];
  for await (const item of iter) {
    results.push(item);
  }
  return results;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('WailsOrchestratorClient.subscribeSession', () => {
  let stub: WailsWindowStub;
  let client: WailsClient;

  beforeEach(() => {
    stub = makeWailsStub();
    Object.defineProperty(globalThis, 'window', {
      value: stub,
      writable: true,
      configurable: true,
    });
    client = new WailsClient();
  });

  afterEach(() => {
    Object.defineProperty(globalThis, 'window', {
      value: undefined,
      writable: true,
      configurable: true,
    });
    vi.clearAllMocks();
  });

  it('yields historical events returned by SessionBinding.Subscribe', async () => {
    const history: SessionEventEnvelope[] = [
      { session_id: 'sess', sequence: 1, payload_json: { kind: 'output' }, timestamp: new Date().toISOString(), from_replay: true },
      { session_id: 'sess', sequence: 2, payload_json: { kind: 'output' }, timestamp: new Date().toISOString(), from_replay: true },
    ];
    stub.go.main.SessionBinding.Subscribe.mockResolvedValue(history);

    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess',
      follow: false,
    };
    const events = await collectAll(client.orchestrator.subscribeSession(req));

    expect(events).toHaveLength(2);
    expect(events[0].sequence).toBe(1);
    expect(events[1].sequence).toBe(2);
    expect(stub.go.main.SessionBinding.Subscribe).toHaveBeenCalledWith(req);
  });

  it('returns after replay when follow=false, no runtime listener registered', async () => {
    stub.go.main.SessionBinding.Subscribe.mockResolvedValue([]);

    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-no-follow',
      follow: false,
    };
    const events = await collectAll(client.orchestrator.subscribeSession(req));

    expect(events).toHaveLength(0);
    // EventsOn must not be called when follow=false.
    expect(stub.runtime.EventsOn).not.toHaveBeenCalled();
  });

  it('registers EventsOn listener for the correct topic when follow=true', async () => {
    stub.go.main.SessionBinding.Subscribe.mockResolvedValue([]);

    const controller = new AbortController();
    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-live',
      follow: true,
      signal: controller.signal,
    };

    // Start the subscription but do not await — we abort immediately.
    const iterPromise = collectAll(client.orchestrator.subscribeSession(req));
    // Give the generator a tick to register the event listener.
    await new Promise((r) => setTimeout(r, 0));

    expect(stub.runtime.EventsOn).toHaveBeenCalledWith(
      'session-event:sess-live',
      expect.any(Function),
    );

    controller.abort();
    // The generator must exit cleanly on abort.
    const events = await iterPromise;
    expect(Array.isArray(events)).toBe(true);
  });

  it('yields live events emitted via the event bus after replay', async () => {
    stub.go.main.SessionBinding.Subscribe.mockResolvedValue([]);

    const controller = new AbortController();
    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-push',
      follow: true,
      signal: controller.signal,
    };

    // Capture the registered EventsOn callback so we can drive it manually.
    // Use an object wrapper to avoid TypeScript narrowing busCallback to null
    // across the async boundary.
    const captured: { cb: EventCallback | null } = { cb: null };
    stub.runtime.EventsOn.mockImplementation((_topic: string, cb: EventCallback) => {
      captured.cb = cb;
      return () => { captured.cb = null; };
    });

    const liveEvent: SessionEventEnvelope = {
      session_id: 'sess-push',
      sequence: 10,
      payload_json: { kind: 'live' },
      timestamp: new Date().toISOString(),
    };

    // Collect one live event then abort.
    const collectPromise = takeN(client.orchestrator.subscribeSession(req), 1);
    // Wait for listener registration.
    await new Promise((r) => setTimeout(r, 0));

    // Emit a live event via the bus.
    captured.cb?.(liveEvent);

    const events = await collectPromise;
    controller.abort();

    expect(events).toHaveLength(1);
    expect(events[0].sequence).toBe(10);
  });

  it('calls the unsub function on generator return', async () => {
    stub.go.main.SessionBinding.Subscribe.mockResolvedValue([]);

    let unsubCalled = false;
    stub.runtime.EventsOn.mockReturnValue(() => { unsubCalled = true; });

    const controller = new AbortController();
    const req: SubscribeSessionEventsRequest = {
      session_id: 'sess-unsub',
      follow: true,
      signal: controller.signal,
    };

    const iter = client.orchestrator.subscribeSession(req);
    await new Promise((r) => setTimeout(r, 0));
    controller.abort();
    await iter[Symbol.asyncIterator]().next();

    // The unsub function must eventually be called when the generator exits.
    await new Promise((r) => setTimeout(r, 0));
    expect(unsubCalled).toBe(true);
  });
});

describe('WailsOrchestratorClient.spawnSession', () => {
  let stub: WailsWindowStub;
  let client: WailsClient;

  beforeEach(() => {
    stub = makeWailsStub();
    Object.defineProperty(globalThis, 'window', {
      value: stub,
      writable: true,
      configurable: true,
    });
    client = new WailsClient();
  });

  afterEach(() => {
    Object.defineProperty(globalThis, 'window', {
      value: undefined,
      writable: true,
      configurable: true,
    });
    vi.clearAllMocks();
  });

  it('calls SessionBinding.Spawn with the full request including SandboxMode and HostAffinity', async () => {
    stub.go.main.SessionBinding.Spawn.mockResolvedValue({
      session_id: 'spawned-1',
      backend: 'claude',
      container_id: 'ctr-abc',
    });

    const resp = await client.orchestrator.spawnSession({
      backend: 'claude',
      model: 'claude-opus-4-5',
      sandbox_mode: 1, // SandboxMode.Required
      host_affinity: 'node-42',
    });

    expect(resp.session_id).toBe('spawned-1');
    expect(stub.go.main.SessionBinding.Spawn).toHaveBeenCalledWith({
      backend: 'claude',
      model: 'claude-opus-4-5',
      sandbox_mode: 1,
      host_affinity: 'node-42',
    });
  });

  it('propagates errors from the Go binding', async () => {
    stub.go.main.SessionBinding.Spawn.mockRejectedValue(new Error('no capacity'));

    await expect(
      client.orchestrator.spawnSession({ backend: 'claude' }),
    ).rejects.toThrow('no capacity');
  });
});

describe('WailsOrchestratorClient.closeSession', () => {
  let stub: WailsWindowStub;
  let client: WailsClient;

  beforeEach(() => {
    stub = makeWailsStub();
    Object.defineProperty(globalThis, 'window', { value: stub, writable: true, configurable: true });
    client = new WailsClient();
  });

  afterEach(() => {
    Object.defineProperty(globalThis, 'window', { value: undefined, writable: true, configurable: true });
    vi.clearAllMocks();
  });

  it('calls SessionBinding.Close and returns the response', async () => {
    stub.go.main.SessionBinding.Close.mockResolvedValue({ session_id: 'close-me', closed: true });

    const resp = await client.orchestrator.closeSession({ session_id: 'close-me' });

    expect(resp.closed).toBe(true);
    expect(stub.go.main.SessionBinding.Close).toHaveBeenCalledWith({ session_id: 'close-me' });
  });
});
