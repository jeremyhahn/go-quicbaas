// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// A scripted MCP Streamable HTTP server for the McpClient suites: it answers
// fetch the way the mcp-go server does, so a test states only what each
// tools/call returns.

import { vi } from 'vitest';

type FetchMock = ReturnType<typeof vi.fn>;

export interface RpcMessage {
  jsonrpc: string;
  id?: number;
  method: string;
  params?: { name?: string; arguments?: Record<string, unknown> };
}

export function jsonReply(body: unknown, headers: Record<string, string> = {}): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json', ...headers },
  });
}

// toolText is a successful tools/call result: the service response as JSON text.
export function toolText(id: number, value: unknown): unknown {
  return { jsonrpc: '2.0', id, result: { content: [{ type: 'text', text: JSON.stringify(value) }] } };
}

// scriptServer answers like the mcp-go server: initialize issues a session,
// the initialized notification is accepted with 202, and each tools/call is
// answered by `onTool`.
export function scriptServer(
  fetchMock: FetchMock,
  onTool: (msg: RpcMessage, session: string | null) => Response,
  sessionId = 'sess-1',
): void {
  fetchMock.mockImplementation(async (_url: string, init: RequestInit) => {
    const msg = JSON.parse(init.body as string) as RpcMessage;
    const session = (init.headers as Record<string, string>)['Mcp-Session-Id'] ?? null;
    if (msg.method === 'initialize') {
      return jsonReply({ jsonrpc: '2.0', id: msg.id, result: { protocolVersion: '2025-03-26' } }, { 'Mcp-Session-Id': sessionId });
    }
    if (msg.method === 'notifications/initialized') {
      return new Response(null, { status: 202 });
    }
    return onTool(msg, session);
  });
}

// toolCalls is every tools/call the client sent, in order.
export function toolCalls(fetchMock: FetchMock): RpcMessage[] {
  return fetchMock.mock.calls
    .map((call) => JSON.parse((call[1] as RequestInit).body as string) as RpcMessage)
    .filter((msg) => msg.method === 'tools/call');
}
