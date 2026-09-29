// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for OrchestratorClient.deployFleetNode across every transport.
// The Wails transport must successfully forward to the Go binding; every
// other transport must throw DeployNotSupportedError so callers know to
// fall back to the CLI or desktop GUI.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import type { DeployFleetNodeSpec } from '../src/types.js';
import { DeployNotSupportedError } from '../src/errors.js';

function baseSpec(): DeployFleetNodeSpec {
  return {
    host: 'node01',
    ssh_user: 'deploy',
    ssh_password: 'pw',
    insecure_host_key: true,
    server: 'https://c:8443',
    tenant: 'acme',
    node_bind: '0.0.0.0:9091',
    backends: ['acp'],
    binary: '/tmp/bin/quicbaas',
  };
}

// ---------------------------------------------------------------------------
// Stub transports
// ---------------------------------------------------------------------------

describe('REST transport deployFleetNode', () => {
  it('throws DeployNotSupportedError with the rest transport label', async () => {
    const { RestClient } = await import('../src/rest.js');
    const client = new RestClient('http://localhost', {});
    await expect(client.orchestrator.deployFleetNode(baseSpec())).rejects.toBeInstanceOf(DeployNotSupportedError);
  });
});

describe('grpc-web transport deployFleetNode', () => {
  it('throws DeployNotSupportedError', async () => {
    const { GrpcWebClient } = await import('../src/grpc_web.js');
    const client = new GrpcWebClient('http://localhost', {});
    await expect(client.orchestrator.deployFleetNode(baseSpec())).rejects.toBeInstanceOf(DeployNotSupportedError);
  });
});

describe('mcp transport deployFleetNode', () => {
  it('throws DeployNotSupportedError', async () => {
    const { McpClient } = await import('../src/mcp.js');
    const client = new McpClient('http://localhost/mcp', {});
    await expect(client.orchestrator.deployFleetNode(baseSpec())).rejects.toBeInstanceOf(DeployNotSupportedError);
  });
});

describe('unix transport deployFleetNode', () => {
  it('throws DeployNotSupportedError', async () => {
    const { UnixClient } = await import('../src/unix.js');
    const client = new UnixClient('/tmp/q.sock', {});
    await expect(client.orchestrator.deployFleetNode(baseSpec())).rejects.toBeInstanceOf(DeployNotSupportedError);
  });
});

describe('quic transport deployFleetNode', () => {
  it('throws DeployNotSupportedError', async () => {
    const { QuicClient } = await import('../src/quic.js');
    const client = new QuicClient('https://q/quic', {});
    await expect(client.orchestrator.deployFleetNode(baseSpec())).rejects.toBeInstanceOf(DeployNotSupportedError);
  });
});

// ---------------------------------------------------------------------------
// Wails transport forwards to the FleetDeployBinding proxy.
// ---------------------------------------------------------------------------

describe('wails transport deployFleetNode', () => {
  const originalWindow = globalThis.window;

  afterEach(() => {
    if (originalWindow === undefined) {
      // Keep node environment clean.
      (globalThis as { window?: unknown }).window = undefined;
    }
    vi.resetModules();
  });

  it('forwards the spec to FleetDeployBinding.Deploy and resolves the result', async () => {
    const deployMock = vi.fn().mockResolvedValue({
      host: 'node01',
      installed_path: '/home/deploy/.quicbaas/bin/quicbaas',
      service_status: 'systemd',
      node_id: 'agent-xyz',
      online_at: '2099-01-01T00:00:00Z',
    });
    (globalThis as { window?: unknown }).window = {
      go: {
        main: {
          FleetDeployBinding: { Deploy: deployMock },
          ConnectionBinding: {},
          OrchestratorBinding: {},
          SessionBinding: {},
        },
      },
      runtime: { EventsOn: vi.fn().mockReturnValue(vi.fn()), EventsOff: vi.fn() },
    };

    const { WailsClient } = await import('../src/wails.js');
    const client = new WailsClient();
    const result = await client.orchestrator.deployFleetNode(baseSpec());
    expect(deployMock).toHaveBeenCalledOnce();
    expect(deployMock.mock.calls[0][0]).toMatchObject({ host: 'node01' });
    expect(result.node_id).toBe('agent-xyz');
    expect(result.service_status).toBe('systemd');
  });

  it('propagates errors from the Go binding verbatim', async () => {
    const boom = new Error('ssh dial failed');
    const deployMock = vi.fn().mockRejectedValue(boom);
    (globalThis as { window?: unknown }).window = {
      go: {
        main: {
          FleetDeployBinding: { Deploy: deployMock },
          ConnectionBinding: {},
          OrchestratorBinding: {},
          SessionBinding: {},
        },
      },
      runtime: { EventsOn: vi.fn().mockReturnValue(vi.fn()), EventsOff: vi.fn() },
    };

    const { WailsClient } = await import('../src/wails.js');
    const client = new WailsClient();
    await expect(client.orchestrator.deployFleetNode(baseSpec())).rejects.toBe(boom);
  });
});

// ---------------------------------------------------------------------------
// DeployNotSupportedError carries the transport label.
// ---------------------------------------------------------------------------

describe('DeployNotSupportedError', () => {
  it('includes the transport in its message', () => {
    const err = new DeployNotSupportedError('rest');
    expect(err.message).toContain('rest');
    expect(err.name).toBe('DeployNotSupportedError');
    expect(err).toBeInstanceOf(Error);
  });
});
