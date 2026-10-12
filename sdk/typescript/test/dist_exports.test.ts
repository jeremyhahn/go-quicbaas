// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// package.json points main and exports at the tracked dist/, so a consumer
// runs the built files, not src/. A dist/ that was not rebuilt after a src/
// change ships the old transports: these tests import the built package and
// fail when it lacks the MCP family clients or the unsupported refusals.

import { describe, it, expect } from 'vitest';
import * as pkg from '../dist/index.js';
import { QuicClient } from '../dist/quic.js';
import { unsupportedClient } from '../dist/unsupported.js';

describe('dist/index.js', () => {
  it('exports the MCP family clients and their session', () => {
    expect(typeof pkg.McpAccountsClient).toBe('function');
    expect(typeof pkg.McpProjectsClient).toBe('function');
    expect(typeof pkg.McpSdlcClient).toBe('function');
    expect(typeof pkg.McpSession).toBe('function');
  });

  it('exports the unsupported-transport error', () => {
    expect(typeof pkg.TransportNotSupportedError).toBe('function');
  });
});

describe('dist unsupported refusals', () => {
  it('builds unsupportedClient, which rejects with TransportNotSupportedError', async () => {
    const err = await unsupportedClient<{ ping(): Promise<void> }>('test', 'family', 'because')
      .ping()
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(pkg.TransportNotSupportedError);
  });

  it('makes the built QuicClient refuse accounts instead of sending REST', async () => {
    const err = await new QuicClient('https://node.example:8443').accounts
      .getAccount('tenant-a', 'acct-1')
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(pkg.TransportNotSupportedError);
    expect((err as InstanceType<typeof pkg.TransportNotSupportedError>).transport).toBe('quic');
  });
});
