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
// isWailsRuntime returns true when running inside a Wails desktop application.
// Wails injects window.go before any JS executes, so this check is synchronous.
export function isWailsRuntime() {
    return (typeof window !== 'undefined' &&
        typeof window.go !== 'undefined' &&
        window.go !== null);
}
// isDesktop is a semantic alias for isWailsRuntime.
export function isDesktop() {
    return isWailsRuntime();
}
// detectTransport returns the best transport for the current runtime environment.
// Callers may override the detection by passing config.transport explicitly.
function detectTransport() {
    if (isWailsRuntime())
        return 'wails';
    // When running in a browser context default to REST over fetch().
    if (typeof window !== 'undefined')
        return 'rest';
    // Node.js / Deno / server context — REST is the safe default.
    return 'rest';
}
// resolveBaseURL returns the effective base URL from the config.
// The canonical field is baseURL; baseUrl is supported for backwards compat.
function resolveBaseURL(config) {
    return config.baseURL ?? config.baseUrl ?? '';
}
// createClient constructs a Client for the selected (or auto-detected) transport.
// Dynamic imports ensure that code for unused transports is never bundled into
// the environment that does not need it.
//
// Transport support matrix:
//   wails    — fully implemented; delegates to Go bindings over Wails IPC.
//   rest     — fully implemented; uses fetch() for HTTP/SSE.
//   grpc-web — stub; requires @improbable-eng/grpc-web or grpc-web npm package
//              (not installed — see TRANSPORT_NOTES.md for operator instructions).
//   unix     — stub; requires a Node.js net.Socket adapter; not available in browsers.
//   mcp      — stub; pending MCP protocol client implementation.
//   quic     — stub; pending QUIC client library integration.
export async function createClient(config = {}) {
    const transport = config.transport ?? detectTransport();
    switch (transport) {
        case 'wails': {
            const { WailsClient } = await import('./wails.js');
            return new WailsClient();
        }
        case 'rest': {
            const { RestClient } = await import('./rest.js');
            return new RestClient(resolveBaseURL(config), config);
        }
        case 'grpc-web': {
            const { GrpcWebClient } = await import('./grpc_web.js');
            return new GrpcWebClient(resolveBaseURL(config), config);
        }
        case 'unix': {
            const { UnixClient } = await import('./unix.js');
            return new UnixClient(config.unix?.socketPath ?? '', config);
        }
        case 'mcp': {
            const { McpClient } = await import('./mcp.js');
            return new McpClient(config.mcp?.endpoint ?? '', config);
        }
        case 'quic': {
            const { QuicClient } = await import('./quic.js');
            return new QuicClient(config.quic?.endpoint ?? '', config);
        }
        default: {
            // TypeScript exhaustiveness guard — transport is typed so this path
            // can only be hit if the runtime receives an unrecognised string.
            const _exhaustive = transport;
            throw new Error(`Unknown transport: ${String(_exhaustive)}`);
        }
    }
}
// getClient is the legacy two-transport factory kept for backwards compatibility.
// New code should call createClient with an explicit ClientConfig.
export async function getClient(config) {
    return createClient(config ?? {});
}
// resolveSubClientConfig merges the transport into a ClientConfig and returns
// the effective configuration used by the convenience factories below.
function resolveSubClientConfig(transport, config) {
    const merged = { ...(config ?? {}) };
    if (transport !== undefined) {
        merged.transport = transport;
    }
    return merged;
}
// createBarrierClient returns the BarrierClient sub-surface for the requested
// transport. REST is fully implemented; the gRPC-Web, QUIC, and MCP transports
// return stub implementations whose methods throw their respective
// transport-not-implemented errors. The Wails desktop transport currently
// proxies barrier/ceremony/bootstrap operations through REST; if a Wails
// runtime is detected the factory transparently constructs a REST client.
export async function createBarrierClient(transport, config) {
    const client = await createClient(resolveSubClientConfig(transport, config));
    return client.barrier;
}
// createCeremonyClient returns the CeremonyClient sub-surface for the requested
// transport. See createBarrierClient for the support matrix.
export async function createCeremonyClient(transport, config) {
    const client = await createClient(resolveSubClientConfig(transport, config));
    return client.ceremony;
}
// createBootstrapClient returns the BootstrapClient sub-surface for the
// requested transport. See createBarrierClient for the support matrix.
export async function createBootstrapClient(transport, config) {
    const client = await createClient(resolveSubClientConfig(transport, config));
    return client.bootstrap;
}
//# sourceMappingURL=factory.js.map