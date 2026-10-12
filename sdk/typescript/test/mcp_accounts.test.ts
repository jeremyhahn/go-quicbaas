// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// The MCP transport carries the accounts family over the server's
// quicbaas_agent_account_* tools (pkg/api/mcp/account_tools.go). These tests
// drive McpClient against a scripted Streamable HTTP server: the initialize
// handshake, the session header, tools/call, SSE replies, tool errors and a
// lost session.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { McpClient } from '../src/mcp';
import { APIError, McpError, TransportNotSupportedError } from '../src/errors';
import { jsonReply, scriptServer, toolText, type RpcMessage } from './mcp_server';

const endpoint = 'http://node.example:8081/mcp';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function sentMessages(): RpcMessage[] {
  return fetchMock.mock.calls.map((call) => JSON.parse((call[1] as RequestInit).body as string) as RpcMessage);
}

const wireAccount = { id: 'acct-1', tenant_id: 'tenant-a', provider: 'claude', status: 'available' };

describe('McpClient.accounts — the tools/call wire', () => {
  it('handshakes once, carries the session and maps each verb to its tool', async () => {
    const seen: Array<{ name?: string; session: string | null }> = [];
    scriptServer(fetchMock, (msg, session) => {
      seen.push({ name: msg.params?.name, session });
      return jsonReply(toolText(msg.id as number, { account: wireAccount }));
    });
    const client = new McpClient(endpoint);

    const got = await client.accounts.getAccount('tenant-a', 'acct-1');
    await client.accounts.revokeAccount({ tenant_id: 'tenant-a', account_id: 'acct-1', reason: 'leaked' });

    expect(got.account.id).toBe('acct-1');
    const methods = sentMessages().map((m) => m.method);
    expect(methods).toEqual(['initialize', 'notifications/initialized', 'tools/call', 'tools/call']);
    expect(seen).toEqual([
      { name: 'quicbaas_agent_account_get', session: 'sess-1' },
      { name: 'quicbaas_agent_account_revoke', session: 'sess-1' },
    ]);
    expect(sentMessages()[2]?.params?.arguments).toEqual({ tenant_id: 'tenant-a', account_id: 'acct-1' });
    expect(sentMessages()[3]?.params?.arguments).toEqual({ tenant_id: 'tenant-a', account_id: 'acct-1', reason: 'leaked' });
  });

  it('passes the create request through flat, as the tool reads it', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { account: wireAccount })));
    const client = new McpClient(endpoint);

    await client.accounts.createAccount({
      tenant_id: 'tenant-a',
      provider: 'claude',
      display_name: 'pool 1',
      refresh_policy: 'static_token',
      credential: { access_token: 'tok' },
    });

    const args = sentMessages()[2]?.params?.arguments;
    expect(args?.['tenant_id']).toBe('tenant-a');
    expect(args?.['display_name']).toBe('pool 1');
    expect(args?.['credential']).toEqual({ access_token: 'tok' });
  });

  // The create and rotate tools read credential.expires_at, so the expiry must
  // reach them inside the credential envelope rather than being dropped.
  it('carries credential.expires_at on create and rotation', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { account: wireAccount, account_id: 'acct-1' })));
    const client = new McpClient(endpoint);
    const credential = { access_token: 'tok', expires_at: '2026-11-01T12:30:00Z' };

    await client.accounts.createAccount({ tenant_id: 'tenant-a', provider: 'claude', refresh_policy: 'static_token', credential });
    await client.accounts.rotateAccountCredential({ tenant_id: 'tenant-a', account_id: 'acct-1', credential });

    const [create, rotate] = sentMessages().filter((m) => m.method === 'tools/call');
    expect(create?.params?.name).toBe('quicbaas_agent_account_create');
    expect(create?.params?.arguments?.['credential']).toEqual(credential);
    expect(rotate?.params?.name).toBe('quicbaas_agent_account_rotate');
    expect(rotate?.params?.arguments?.['credential']).toEqual(credential);
  });

  it('sends the bearer token on every request', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, { account_id: 'acct-1' })));
    const client = new McpClient(endpoint, { token: async () => 'live-token' });

    await client.accounts.setDefaultAccount('tenant-a', 'acct-1');

    for (const call of fetchMock.mock.calls) {
      expect(((call[1] as RequestInit).headers as Record<string, string>)['Authorization']).toBe('Bearer live-token');
    }
  });

  it('reads a reply streamed as text/event-stream', async () => {
    scriptServer(fetchMock, (msg) => new Response(
      `event: message\ndata: ${JSON.stringify(toolText(msg.id as number, { account_id: 'acct-1', deleted: true }))}\n\n`,
      { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
    ));
    const client = new McpClient(endpoint);

    const resp = await client.accounts.deleteAccount('tenant-a', 'acct-1');
    expect(resp).toEqual({ account_id: 'acct-1', deleted: true });
  });

  it('pages accounts through the list tool and adopts the envelope', async () => {
    scriptServer(fetchMock, (msg) => jsonReply(toolText(msg.id as number, {
      accounts: [wireAccount],
      total: 4,
      limit: 1,
      offset: 2,
      next_offset: 3,
      timestamp: '2026-10-12T00:00:00Z',
    })));
    const client = new McpClient(endpoint);

    const page = await client.accounts.listAccountsPage({ tenant_id: 'tenant-a', provider: 'claude', limit: 1, offset: 2 });

    expect(sentMessages()[2]?.params?.arguments).toEqual({ tenant_id: 'tenant-a', provider: 'claude', limit: 1, offset: 2 });
    expect(page.items).toEqual([wireAccount]);
    expect(page.total).toBe(4);
    expect(page.next_offset).toBe(3);
  });
});

describe('McpClient.accounts — failures', () => {
  it('turns a tool error result into McpError with the server message', async () => {
    scriptServer(fetchMock, (msg) => jsonReply({
      jsonrpc: '2.0',
      id: msg.id,
      result: { content: [{ type: 'text', text: 'account not found' }], isError: true },
    }));
    const client = new McpClient(endpoint);

    const err = await client.accounts.getAccount('tenant-a', 'missing').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(McpError);
    expect((err as McpError).tool).toBe('quicbaas_agent_account_get');
    expect((err as McpError).message).toContain('account not found');
  });

  it('turns a JSON-RPC error into McpError carrying the code', async () => {
    scriptServer(fetchMock, (msg) => jsonReply({ jsonrpc: '2.0', id: msg.id, error: { code: -32602, message: 'tool not registered' } }));
    const client = new McpClient(endpoint);

    const err = await client.accounts.getAccountUsage('tenant-a', 'acct-1').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(McpError);
    expect((err as McpError).code).toBe(-32602);
  });

  it('re-initializes once when the server has forgotten the session', async () => {
    let sessions = 0;
    let calls = 0;
    fetchMock.mockImplementation(async (_url: string, init: RequestInit) => {
      const msg = JSON.parse(init.body as string) as RpcMessage;
      if (msg.method === 'initialize') {
        sessions++;
        return jsonReply({ jsonrpc: '2.0', id: msg.id, result: {} }, { 'Mcp-Session-Id': `sess-${sessions}` });
      }
      if (msg.method === 'notifications/initialized') return new Response(null, { status: 202 });
      calls++;
      if (calls === 1) return new Response('session not found', { status: 404 });
      return jsonReply(toolText(msg.id as number, { account: wireAccount }));
    });
    const client = new McpClient(endpoint);

    const resp = await client.accounts.getAccount('tenant-a', 'acct-1');
    expect(resp.account.id).toBe('acct-1');
    expect(sessions).toBe(2);
    const last = fetchMock.mock.calls.at(-1)?.[1] as RequestInit;
    expect((last.headers as Record<string, string>)['Mcp-Session-Id']).toBe('sess-2');
  });

  it('does not retry a refused handshake as a lost session', async () => {
    fetchMock.mockResolvedValue(new Response('no mcp here', { status: 404 }));
    const client = new McpClient(endpoint);

    const err = await client.accounts.getAccount('tenant-a', 'acct-1').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(APIError);
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('refuses a status filter the list tool cannot apply, without calling it', async () => {
    const client = new McpClient(endpoint);

    const err = await client.accounts
      .listAccountsPage({ tenant_id: 'tenant-a', status: 'available' })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(TransportNotSupportedError);
    expect((err as TransportNotSupportedError).method).toBe('listAccountsPage');
    expect(fetchMock).not.toHaveBeenCalled();
  });
});
