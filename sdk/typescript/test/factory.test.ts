// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for factory.ts: transport detection and client construction.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// ---------------------------------------------------------------------------
// Helpers to manipulate the global window object per test.
// ---------------------------------------------------------------------------

function setWindow(value: typeof globalThis.window | undefined): void {
  if (value === undefined) {
    // Simulate a non-browser (Node.js) environment.
    Object.defineProperty(globalThis, 'window', {
      value: undefined,
      writable: true,
      configurable: true,
    });
  } else {
    Object.defineProperty(globalThis, 'window', {
      value,
      writable: true,
      configurable: true,
    });
  }
}

// Minimal Wails window stub.
function makeWailsWindow(): typeof globalThis.window {
  return {
    go: {
      main: {
        ConnectionBinding: {
          Connect: vi.fn().mockResolvedValue(undefined),
          Disconnect: vi.fn().mockResolvedValue(undefined),
        },
        OrchestratorBinding: {},
        SessionBinding: {},
      },
    },
    runtime: {
      EventsOn: vi.fn().mockReturnValue(vi.fn()),
      EventsOff: vi.fn(),
    },
  } as unknown as typeof globalThis.window;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('isWailsRuntime', () => {
  afterEach(() => {
    setWindow(undefined);
    vi.resetModules();
  });

  it('returns false when window is undefined', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { isWailsRuntime } = await import('../src/factory.js');
    expect(isWailsRuntime()).toBe(false);
  });

  it('returns false when window.go is absent', async () => {
    setWindow({} as typeof globalThis.window);
    vi.resetModules();
    const { isWailsRuntime } = await import('../src/factory.js');
    expect(isWailsRuntime()).toBe(false);
  });

  it('returns true when window.go is populated (Wails runtime)', async () => {
    setWindow(makeWailsWindow());
    vi.resetModules();
    const { isWailsRuntime } = await import('../src/factory.js');
    expect(isWailsRuntime()).toBe(true);
  });
});

describe('isDesktop', () => {
  afterEach(() => {
    setWindow(undefined);
    vi.resetModules();
  });

  it('is an alias for isWailsRuntime', async () => {
    setWindow(makeWailsWindow());
    vi.resetModules();
    const { isDesktop, isWailsRuntime } = await import('../src/factory.js');
    expect(isDesktop()).toBe(isWailsRuntime());
  });
});

describe('createClient transport selection', () => {
  afterEach(() => {
    setWindow(undefined);
    vi.resetModules();
  });

  it('selects rest transport when no window and no explicit transport', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({ baseURL: 'http://localhost:8080' });
    expect(client.connection.getTransport()).toBe('rest');
  });

  it('selects rest transport when window is present without go (browser mode)', async () => {
    setWindow({} as typeof globalThis.window);
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({ baseURL: 'http://localhost:8080' });
    expect(client.connection.getTransport()).toBe('rest');
  });

  it('selects wails transport when window.go is populated', async () => {
    setWindow(makeWailsWindow());
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient();
    expect(client.connection.getTransport()).toBe('wails');
  });

  it('respects explicit transport=rest override even in Wails env', async () => {
    setWindow(makeWailsWindow());
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({ transport: 'rest', baseURL: 'http://localhost' });
    expect(client.connection.getTransport()).toBe('rest');
  });

  it('respects explicit transport=grpc-web and returns stub client', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({
      transport: 'grpc-web',
      baseURL: 'http://localhost:9090',
    });
    expect(client.connection.getTransport()).toBe('grpc-web');
  });

  it('respects explicit transport=unix and returns stub client', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({
      transport: 'unix',
      unix: { socketPath: '/tmp/quicbaas.sock' },
    });
    expect(client.connection.getTransport()).toBe('unix');
    expect(client.connection.getAddress()).toBe('/tmp/quicbaas.sock');
  });

  it('respects explicit transport=mcp and returns stub client', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({
      transport: 'mcp',
      mcp: { endpoint: 'http://localhost:8081/mcp' },
    });
    expect(client.connection.getTransport()).toBe('mcp');
    expect(client.connection.getAddress()).toBe('http://localhost:8081/mcp');
  });

  it('respects explicit transport=quic and returns stub client', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({
      transport: 'quic',
      quic: { endpoint: 'quic://localhost:4433' },
    });
    expect(client.connection.getTransport()).toBe('quic');
    expect(client.connection.getAddress()).toBe('quic://localhost:4433');
  });

  it('getClient (legacy alias) delegates to createClient', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { getClient } = await import('../src/factory.js');
    const client = await getClient({ baseURL: 'http://localhost:8080' });
    expect(client.connection.getTransport()).toBe('rest');
  });

  it('baseUrl (legacy field) is accepted alongside baseURL', async () => {
    setWindow(undefined);
    vi.resetModules();
    const { createClient } = await import('../src/factory.js');
    // Should not throw; legacy field is accepted.
    const client = await createClient({ baseUrl: 'http://legacy:8080' });
    expect(client.connection.getTransport()).toBe('rest');
  });
});

describe('stub transport errors', () => {
  beforeEach(() => {
    setWindow(undefined);
    vi.resetModules();
  });

  it('grpc-web methods throw GrpcWebNotImplementedError', async () => {
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({ transport: 'grpc-web', baseURL: '' });
    await expect(client.orchestrator.healthCheck()).rejects.toThrow('grpc-web transport');
  });

  it('unix methods throw TransportNotSupportedError', async () => {
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({
      transport: 'unix',
      unix: { socketPath: '/tmp/test.sock' },
    });
    await expect(client.orchestrator.healthCheck()).rejects.toThrow('unix transport');
  });

  it('mcp methods throw McpNotImplementedError', async () => {
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({ transport: 'mcp', mcp: { endpoint: '' } });
    await expect(client.barrier.shamirVerify()).rejects.toThrow('mcp transport');
  });

  it('quic methods throw QuicNotImplementedError', async () => {
    const { createClient } = await import('../src/factory.js');
    const client = await createClient({ transport: 'quic', quic: { endpoint: '' } });
    await expect(client.orchestrator.healthCheck()).rejects.toThrow('quic transport');
  });
});

// ---------------------------------------------------------------------------
// Convenience sub-client factories — createBarrierClient / createCeremonyClient
// / createBootstrapClient. Each delegates to createClient and returns the
// matching sub-client. The REST path returns a working implementation; the
// stub transports return clients whose methods throw the corresponding
// transport-not-implemented error.
// ---------------------------------------------------------------------------

describe('createBarrierClient', () => {
  beforeEach(() => {
    setWindow(undefined);
    vi.resetModules();
  });

  it('returns a working REST barrier client for transport=rest', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({ sealed: false, strategy: 'shamir', hardware_backed: false }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    const original = globalThis.fetch;
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    try {
      const { createBarrierClient } = await import('../src/factory.js');
      const barrier = await createBarrierClient('rest', { baseURL: 'http://localhost' });
      const status = await barrier.status();
      expect(status.sealed).toBe(false);
      expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/barrier/status');
    } finally {
      globalThis.fetch = original;
    }
  });

  it('returns a stub barrier client for transport=grpc-web', async () => {
    const { createBarrierClient } = await import('../src/factory.js');
    const barrier = await createBarrierClient('grpc-web', { baseURL: 'http://localhost' });
    await expect(barrier.status()).rejects.toThrow('grpc-web transport');
  });

  it('returns a stub barrier client for transport=quic', async () => {
    const { createBarrierClient } = await import('../src/factory.js');
    const barrier = await createBarrierClient('quic', { quic: { endpoint: '' } });
    await expect(barrier.status()).rejects.toThrow('quic transport');
  });

  it('returns the MCP barrier client for transport=mcp', async () => {
    const { createBarrierClient } = await import('../src/factory.js');
    const barrier = await createBarrierClient('mcp', { mcp: { endpoint: '' } });
    await expect(barrier.shamirListShares()).rejects.toThrow('mcp transport');
  });

  it('defaults to the auto-detected transport when none is supplied', async () => {
    const { createBarrierClient } = await import('../src/factory.js');
    // No window -> defaults to REST. The REST sub-client is constructed
    // without making any network call so this assertion does not need fetch.
    const barrier = await createBarrierClient(undefined, { baseURL: 'http://localhost' });
    expect(barrier).toBeDefined();
    expect(typeof barrier.status).toBe('function');
  });
});

describe('createCeremonyClient', () => {
  beforeEach(() => {
    setWindow(undefined);
    vi.resetModules();
  });

  it('returns a working REST ceremony client for transport=rest', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ state: 'complete' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const original = globalThis.fetch;
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    try {
      const { createCeremonyClient } = await import('../src/factory.js');
      const ceremony = await createCeremonyClient('rest', { baseURL: 'http://localhost' });
      const result = await ceremony.status();
      expect(result.state).toBe('complete');
      expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/init/status');
    } finally {
      globalThis.fetch = original;
    }
  });

  it('returns a stub ceremony client for transport=grpc-web', async () => {
    const { createCeremonyClient } = await import('../src/factory.js');
    const ceremony = await createCeremonyClient('grpc-web', { baseURL: '' });
    await expect(ceremony.status()).rejects.toThrow('grpc-web transport');
  });

  it('returns a stub ceremony client for transport=quic', async () => {
    const { createCeremonyClient } = await import('../src/factory.js');
    const ceremony = await createCeremonyClient('quic', { quic: { endpoint: '' } });
    await expect(ceremony.status()).rejects.toThrow('quic transport');
  });

  it('returns the MCP ceremony client for transport=mcp', async () => {
    const { createCeremonyClient } = await import('../src/factory.js');
    const ceremony = await createCeremonyClient('mcp', { mcp: { endpoint: '' } });
    await expect(ceremony.claimShare({ username: 'officer-1' })).rejects.toThrow('mcp transport');
  });
});

describe('createBootstrapClient', () => {
  beforeEach(() => {
    setWindow(undefined);
    vi.resetModules();
  });

  it('returns a working REST bootstrap client for transport=rest', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          method: 'auto',
          bundle_pem: 'PEM',
          content_type: 'application/pem-certificate-chain',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    );
    const original = globalThis.fetch;
    globalThis.fetch = fetchMock as unknown as typeof fetch;
    try {
      const { createBootstrapClient } = await import('../src/factory.js');
      const bootstrap = await createBootstrapClient('rest', { baseURL: 'http://localhost' });
      const result = await bootstrap.auto();
      expect(result.method).toBe('auto');
      expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/bootstrap/auto');
    } finally {
      globalThis.fetch = original;
    }
  });

  it.each(['grpc-web', 'quic', 'mcp'] as const)(
    'returns a stub bootstrap client whose methods throw for transport=%s',
    async (transport) => {
      const { createBootstrapClient } = await import('../src/factory.js');
      const bootstrap = await createBootstrapClient(transport, {
        baseURL: '',
        quic: { endpoint: '' },
        mcp: { endpoint: '' },
      });
      // Each strategy must surface the transport-specific error.
      await expect(bootstrap.auto()).rejects.toThrow(`${transport} transport`);
      await expect(bootstrap.dane()).rejects.toThrow(`${transport} transport`);
      await expect(bootstrap.noise()).rejects.toThrow(`${transport} transport`);
      await expect(bootstrap.spki()).rejects.toThrow(`${transport} transport`);
    },
  );
});
