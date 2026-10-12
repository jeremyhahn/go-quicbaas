import type { ClientConfig } from './types.js';
export declare class McpSession {
    private readonly endpoint;
    private readonly config;
    private sessionId;
    private ready;
    private nextId;
    constructor(endpoint: string, config?: ClientConfig);
    callTool<T>(tool: string, args: object): Promise<T>;
    private callToolOnce;
    private initialize;
    private handshake;
    private request;
    private post;
}
//# sourceMappingURL=mcp_rpc.d.ts.map