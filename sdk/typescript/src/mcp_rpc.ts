// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// McpSession speaks the Model Context Protocol's Streamable HTTP transport to
// the server's MCP endpoint (go-qrdb's mcp-go server, which carries the
// quicbaas_* tool registry): JSON-RPC 2.0 over POST, an `initialize`
// handshake that issues an Mcp-Session-Id, and `tools/call` for every verb.
// It is the TypeScript counterpart of sdk/go/transport/mcp.

import { APIError, McpError } from './errors.js';
import { parseJSONPreservingLargeIntegers } from './json.js';
import { resolveToken } from './rest.js';
import type { ClientConfig } from './types.js';

// mcpProtocolVersion is the MCP revision that introduced Streamable HTTP.
const mcpProtocolVersion = '2025-03-26';

const sessionHeader = 'Mcp-Session-Id';

interface JsonRpcResponse {
  id?: number | string | null;
  result?: unknown;
  error?: { code: number; message: string };
}

interface ToolCallResult {
  content?: Array<{ type: string; text?: string }>;
  isError?: boolean;
}

export class McpSession {
  /** endpoint is the MCP URL; callers check its scheme before sending secrets. */
  readonly endpoint: string;
  private readonly config: ClientConfig;
  private sessionId = '';
  private ready: Promise<void> | null = null;
  private nextId = 1;

  constructor(endpoint: string, config: ClientConfig = {}) {
    this.endpoint = endpoint;
    this.config = config;
  }

  // callTool invokes one tool and returns its result decoded from the JSON
  // text the server answers with. A tool that reports an error is an McpError
  // carrying the server's message.
  //
  // The server answers a request whose session it no longer knows with 404;
  // the remedy the protocol prescribes is a fresh handshake, so callTool
  // re-initializes once and retries before giving up.
  async callTool<T>(tool: string, args: object): Promise<T> {
    try {
      return await this.callToolOnce<T>(tool, args);
    } catch (err) {
      if (!(err instanceof APIError) || err.status !== 404 || this.sessionId === '') throw err;
      this.sessionId = '';
      this.ready = null;
      return this.callToolOnce<T>(tool, args);
    }
  }

  private async callToolOnce<T>(tool: string, args: object): Promise<T> {
    await this.initialize();
    const result = (await this.request('tools/call', { name: tool, arguments: args }, tool)) as ToolCallResult;
    const text = (result?.content ?? [])
      .filter((item) => item.type === 'text')
      .map((item) => item.text ?? '')
      .join('');
    if (result?.isError) throw new McpError(tool, text || 'tool reported an error');
    // The tool's text is the same JSON the REST route returns, so it is parsed
    // the same way: a 64-bit identity past 2^53 arrives as its exact decimal
    // string (see json.ts).
    return (text ? parseJSONPreservingLargeIntegers<T>(text) : (undefined as T));
  }

  // initialize performs the handshake once per session; concurrent callers
  // share it, and a failed handshake is forgotten so the next call retries.
  private initialize(): Promise<void> {
    if (!this.ready) {
      this.ready = this.handshake().catch((err: unknown) => {
        this.ready = null;
        throw err;
      });
    }
    return this.ready;
  }

  private async handshake(): Promise<void> {
    await this.request('initialize', {
      protocolVersion: mcpProtocolVersion,
      capabilities: {},
      clientInfo: { name: '@quicbaas/sdk', version: '0.1.0' },
    }, 'initialize');
    await this.post({ jsonrpc: '2.0', method: 'notifications/initialized' });
  }

  // request sends one JSON-RPC request and returns its result member.
  private async request(method: string, params: object, label: string): Promise<unknown> {
    const id = this.nextId++;
    const resp = await this.post({ jsonrpc: '2.0', id, method, params });
    const message = await readReply(resp, id);
    if (!message) throw new McpError(label, `no JSON-RPC reply to request ${id}`);
    if (message.error) throw new McpError(label, message.error.message, message.error.code);
    return message.result;
  }

  private async post(message: object): Promise<Response> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
    };
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.sessionId) headers[sessionHeader] = this.sessionId;
    const resp = await fetch(this.endpoint, { method: 'POST', headers, body: JSON.stringify(message) });
    if (!resp.ok) {
      const text = await resp.text().catch(() => resp.statusText);
      throw new APIError(resp.status, text);
    }
    const issued = resp.headers.get(sessionHeader);
    if (issued) this.sessionId = issued;
    return resp;
  }
}

// readReply finds the JSON-RPC response with the given id. The server answers
// either with one JSON body or, when it streams, with a text/event-stream
// whose `data:` events are JSON-RPC messages.
async function readReply(resp: Response, id: number): Promise<JsonRpcResponse | undefined> {
  const text = await resp.text();
  if (!text) return undefined;
  const contentType = resp.headers.get('Content-Type') ?? '';
  if (!contentType.includes('text/event-stream')) {
    return JSON.parse(text) as JsonRpcResponse;
  }
  for (const event of text.split(/\r?\n\r?\n/)) {
    const data = event
      .split(/\r?\n/)
      .filter((line) => line.startsWith('data:'))
      .map((line) => line.slice('data:'.length).trimStart())
      .join('\n');
    if (!data) continue;
    const message = JSON.parse(data) as JsonRpcResponse;
    if (message.id === id) return message;
  }
  return undefined;
}
