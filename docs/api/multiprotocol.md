# Multi-Protocol Parity

QuicBaaS exposes the same operation surface over five transports.
Every CLI command, every SDK call, every GUI action goes through the
SDK's transport abstraction — if a feature works on one transport it
works on all of them. The integration tests under
`test/integration/cli/` enforce this with the `RunForAllTransports`
table-driven harness.

## Transports

| Transport | Default port | Wire | Auth | Streaming |
|-----------|--------------|------|------|-----------|
| REST      | `:18080` (TLS) | HTTP/1.1 + JSON | OAuth2 / mTLS | NDJSON over chunked |
| gRPC      | `:19090` (TLS, `h2` ALPN) | Protocol Buffers | OAuth2 / mTLS | bidi streams |
| QUIC      | `:10443` (TLS 1.3) | QUIC streams | OAuth2 / mTLS | native streams |
| MCP       | `:11443` (TLS) | JSON-RPC over WSS | OAuth2 / mTLS | server push |
| Unix      | `/tmp/quicbaas.sock` | gRPC over UDS | filesystem perms (no TLS) | bidi streams |

The `embedded` transport is also available — it's used by the Wails
desktop binary, which links the SDK and the daemon together so calls
are direct Go function calls (no IPC, no serialisation).

## Selecting a transport

Auto-detection from `--address`:

| Address | Transport |
|---------|-----------|
| `https://host:port` | REST |
| `host:port` (no scheme) | gRPC over TLS |
| `quic://host:port` | QUIC |
| `mcp://host:port` | MCP |
| `/abs/path` (starts with `/`) | Unix socket |

Override with `--transport <name>`. The CLI always uses the SDK
abstraction internally — there is no transport-specific CLI code.

## Coverage matrix

The SDK exposes the same `Client` interface for every transport
(`sdk/typescript/src/interfaces.ts` for the TS SDK, `sdk/go` for Go).
Per-method coverage is enforced by the table-driven integration tests:

```go
func TestOrchestratorInference(t *testing.T) {
    RunForAllTransports(t, "Inference", func(t *testing.T, runner *TestRunner) {
        // ... same test body executes over REST, gRPC, QUIC, MCP, Unix.
    })
}
```

Tests live in `test/integration/cli/*_test.go`. CI fails if any
transport returns `notImpl` or a `method-not-supported` error for a
method that any other transport implements.

## Adding a new method

When you add a method to the orchestration / data / IAM / KMS / etc.
service:

1. Add it to the domain interface in the relevant `pkg/<service>` package.
2. Add the proto RPC if it's gRPC-bound.
3. Implement the server side once per transport: REST handler,
   gRPC handler, QUIC handler (usually shares with gRPC), MCP tool,
   Unix-socket gRPC (shares with gRPC).
4. Implement the SDK client side once per transport. The TypeScript
   SDK's per-transport stubs (`sdk/typescript/src/{rest,grpc_web,quic,mcp,unix}.ts`)
   must all have a real implementation — `notImpl` calls fail the
   integration tests.
5. Add a `RunForAllTransports` test that exercises the method.

DRY rule: the orchestration service's domain method is the canonical
implementation. Every transport handler delegates to it — no business
logic in transport handlers.

## TLS / mTLS

REST, gRPC, QUIC, and MCP all run over TLS by default. Server certs
are issued by the daemon's embedded CA at startup; clients can
auto-discover the CA chain at `--data-dir/certs/ca-chain.crt`. mTLS
is engaged whenever the client presents a cert (`--tls-cert` /
`--tls-key`).

The Unix transport runs without TLS — the wire is a local socket and
filesystem permissions guard access.

## See also

- [REST API](rest.md)
- [gRPC API](grpc.md)
- [QUIC](quic.md)
- [MCP](../agents/mcp-tools.md)
- [Fleet quick start](../agents/fleet-quickstart.md) — same `inference` command on every transport.
