// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The MCP transport carries the barrier and ceremony families over the
// server's qrdb_barrier_* and qrdb_ceremony_* tools (pkg/api/mcp), with the
// tool names and argument keys of the Go SDK's MCP transport
// (sdk/go/transport/mcp). These tests drive McpClient against a scripted
// Streamable HTTP server and pin, for each verb, the tool it calls, the
// arguments it sends and how it maps the reply.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { McpClient } from '../src/mcp';
import { McpError, SOPINRequiresTLSError } from '../src/errors';
import { jsonReply, scriptServer, toolCalls, toolText } from './mcp_server';

const endpoint = 'https://node.example:8081/mcp';
const plainEndpoint = 'http://node.example:8081/mcp';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

const utf8 = (s: string): Uint8Array => new TextEncoder().encode(s);

interface ToolCase {
  name: string;
  call: (client: McpClient) => Promise<unknown>;
  tool: string;
  args: Record<string, unknown>;
  reply: unknown;
  want: unknown;
}

// expectToolCase runs one verb against a server that answers with c.reply and
// checks the single tool call it made and the value it returned.
async function expectToolCase(c: ToolCase): Promise<void> {
  scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, c.reply)));

  const got = await c.call(new McpClient(endpoint));

  const calls = toolCalls(fetchMock);
  expect(calls).toHaveLength(1);
  expect(calls[0].params?.name).toBe(c.tool);
  expect(calls[0].params?.arguments).toEqual(c.args);
  expect(got).toEqual(c.want);
}

// toolError answers every tool call with an MCP tool error result.
function toolError(): void {
  scriptServer(fetchMock, (msg) =>
    jsonReply({ jsonrpc: '2.0', id: msg.id, result: { isError: true, content: [{ type: 'text', text: 'barrier is sealed' }] } }),
  );
}

describe('McpBarrierClient tools', () => {
  const cases: ToolCase[] = [
    {
      name: 'status',
      call: (c) => c.barrier.status(),
      tool: 'qrdb_barrier_status',
      args: {},
      reply: { sealed: true, strategy: 'shamir', hardware_backed: false, initialized_at: '2026-10-17T00:00:00Z' },
      want: { sealed: true, strategy: 'shamir', hardware_backed: false, initialized_at: '2026-10-17T00:00:00Z' },
    },
    {
      name: 'initialize',
      call: (c) => c.barrier.initialize({ secret: utf8('pass phrase') }),
      tool: 'qrdb_barrier_initialize',
      args: { secret: 'pass phrase' },
      reply: { status: 'initialized' },
      want: undefined,
    },
    {
      name: 'unseal',
      call: (c) => c.barrier.unseal({ secret: utf8('pass phrase') }),
      tool: 'qrdb_barrier_unseal',
      args: { secret: 'pass phrase' },
      reply: { status: 'unsealed' },
      want: undefined,
    },
    {
      name: 'initializeShamir',
      call: (c) => c.barrier.initializeShamir({ secret: utf8('pw'), threshold: 2, total_shares: 3 }),
      tool: 'qrdb_barrier_shamir_initialize',
      args: { secret: 'pw', threshold: 2, total_shares: 3 },
      reply: { threshold: 2, total_shares: 3, share_count: 3 },
      want: { shares: [], threshold: 2, total_shares: 3 },
    },
    {
      name: 'generateRecoveryKeys',
      call: (c) => c.barrier.generateRecoveryKeys({ threshold: 2, total: 4 }),
      tool: 'qrdb_barrier_recovery_generate',
      args: { threshold: 2, total: 4 },
      reply: { threshold: 2, total: 4, share_count: 4 },
      want: { keys: [], threshold: 2, total: 4 },
    },
    {
      name: 'deleteRecoveryKeys',
      call: (c) => c.barrier.deleteRecoveryKeys(),
      tool: 'qrdb_barrier_recovery_delete',
      args: {},
      reply: { status: 'deleted' },
      want: undefined,
    },
    {
      name: 'recoveryStatus',
      call: (c) => c.barrier.recoveryStatus(),
      tool: 'qrdb_barrier_recovery_status',
      args: {},
      reply: { has_recovery_keys: true },
      want: { has_keys: true },
    },
    {
      name: 'enableEncryption on the system barrier',
      call: (c) => c.barrier.enableEncryption({ strategy: 'passphrase', passphrase: utf8('pw') }),
      tool: 'qrdb_barrier_enable_encryption',
      args: {
        target_strategy: 'passphrase',
        mechanism: '',
        availability: '',
        passphrase: 'pw',
        shamir_threshold: 0,
        shamir_total: 0,
      },
      reply: { status: 'encryption enabled' },
      want: { status: 'encryption enabled', scope: 'system' },
    },
    {
      name: 'enableEncryption on a tenant barrier',
      call: (c) =>
        c.barrier.enableEncryption({
          tenant_id: 'tenant-a',
          strategy: 'shamir',
          mechanism: 'swap',
          availability: 'online',
          shamir_threshold: 2,
          shamir_total: 3,
        }),
      tool: 'qrdb_tenant_barrier_enable_encryption',
      args: {
        tenant_id: 'tenant-a',
        target_strategy: 'shamir',
        mechanism: 'swap',
        availability: 'online',
        passphrase: '',
        shamir_threshold: 2,
        shamir_total: 3,
      },
      reply: { status: 'encryption enabled', tenant_id: 'tenant-a' },
      want: { status: 'encryption enabled', scope: 'tenant' },
    },
  ];

  it.each(cases)('$name calls $tool', expectToolCase);

  it('surfaces a tool error reply as McpError', async () => {
    toolError();

    await expect(new McpClient(endpoint).barrier.status()).rejects.toBeInstanceOf(McpError);
  });

});

describe('McpCeremonyClient tools', () => {
  const cases: ToolCase[] = [
    {
      name: 'initialize',
      call: (c) =>
        c.ceremony.initialize({
          so_pin: 'so-pin-1234',
          user_pin: 'user-pin-1234',
          threshold: 2,
          credential_seal_strategy: 'passphrase',
          officers: [{ username: 'officer-1', csr_pem: 'CSR-1' }],
          hostname: 'node.example',
          sans: ['node.example', '10.0.0.1'],
        }),
      tool: 'qrdb_ceremony_initialize',
      args: {
        so_pin: 'so-pin-1234',
        user_pin: 'user-pin-1234',
        threshold: 2,
        credential_seal_strategy: 'passphrase',
        officers: [{ username: 'officer-1', csr_pem: 'CSR-1' }],
        hostname: 'node.example',
        sans: ['node.example', '10.0.0.1'],
      },
      reply: { spki_pin: 'pin', ca_cert_pem: 'CA', tls_cert_pem: 'TLS', state: 'initialized', share_count: 2 },
      want: { spki_pin: 'pin', ca_cert_pem: 'CA', tls_cert_pem: 'TLS', state: 'initialized', share_count: 2 },
    },
    {
      name: 'initialize without the optional fields',
      call: (c) => c.ceremony.initialize({ so_pin: 'so-pin-1234', user_pin: 'user-pin-1234', threshold: 0 }),
      tool: 'qrdb_ceremony_initialize',
      args: { so_pin: 'so-pin-1234', user_pin: 'user-pin-1234', threshold: 0 },
      reply: { spki_pin: 'pin', ca_cert_pem: 'CA', tls_cert_pem: 'TLS', state: 'initialized', share_count: 0 },
      want: { spki_pin: 'pin', ca_cert_pem: 'CA', tls_cert_pem: 'TLS', state: 'initialized', share_count: 0 },
    },
    {
      name: 'status',
      call: (c) => c.ceremony.status(),
      tool: 'qrdb_ceremony_status',
      args: {},
      reply: { state: 'initialized', spki_pin: 'pin' },
      want: { state: 'initialized', spki_pin: 'pin' },
    },
    {
      name: 'signCSR',
      call: (c) => c.ceremony.signCSR({ username: 'svc-1', so_pin: 'so-pin-1234', csr_pem: 'CSR', role: 'admin' }),
      tool: 'qrdb_ceremony_sign_csr',
      args: { username: 'svc-1', so_pin: 'so-pin-1234', csr_pem: 'CSR', role: 'admin' },
      reply: { cert_pem: 'CERT' },
      want: { cert_pem: 'CERT' },
    },
  ];

  it.each(cases)('$name calls $tool', expectToolCase);

  it('surfaces a tool error reply as McpError', async () => {
    toolError();

    await expect(new McpClient(endpoint).ceremony.status()).rejects.toBeInstanceOf(McpError);
  });

  it.each([
    ['initialize', (c: McpClient) => c.ceremony.initialize({ so_pin: 'so-pin-1234', user_pin: 'u', threshold: 0 })],
    ['signCSR', (c: McpClient) => c.ceremony.signCSR({ username: 'u', so_pin: 'so-pin-1234', csr_pem: 'CSR', role: 'r' })],
  ] as const)('%s refuses to send an SO PIN over plaintext http', async (_name, call) => {
    const err = await call(new McpClient(plainEndpoint)).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(SOPINRequiresTLSError);
    expect(fetchMock).not.toHaveBeenCalled();
  });

});

describe('McpClient bootstrap', () => {
  // Trust bootstrap is served over REST only, so the MCP client has no
  // bootstrap member and the compiler refuses the call.
  it('has no bootstrap member', () => {
    const client = new McpClient(endpoint);
    // @ts-expect-error bootstrap is a RestClient-only member
    expect(client.bootstrap).toBeUndefined();
    expect('bootstrap' in client).toBe(false);
  });
});
