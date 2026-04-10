# QuicBaaS API Documentation

QuicBaaS provides multiple API protocols for diverse client requirements. All APIs share a common service facade, ensuring consistent behavior across protocols.

## Available APIs

| Protocol | Use Case | Transport | Performance | Documentation |
|----------|----------|-----------|-------------|---------------|
| gRPC | Service-to-service communication | Unix Socket/TCP, Binary | 150K+ ops/sec, <5ms P99 | [grpc.md](grpc.md) |
| QUIC | Mobile clients, high-latency networks | UDP/TLS 1.3, Binary | 100K+ ops/sec, <3ms P99 | [quic.md](quic.md) |
| REST | Web applications, HTTP clients | HTTP/1.1-2, JSON | 25K+ ops/sec, <15ms P99 | [rest.md](rest.md) |
| MCP | AI agents, LLM integration | Stdio/HTTP, JSON-RPC | Context-aware operations | [mcp.md](mcp.md) |

## Default Transport: Unix Socket

QuicBaaS uses Unix domain sockets as the default transport for gRPC communication:

| Environment | Socket Path |
|-------------|-------------|
| Development | `/tmp/quicbaas.sock` |
| Production | `/var/run/quicbaas/quicbaas.sock` |

Unix sockets are enabled by default. TCP and REST are optional transports that must be explicitly enabled.

## When to Use Each API

### gRPC (Unix Socket or TCP)

Choose gRPC when you need:
- Maximum throughput and minimal latency
- Strong typing with Protocol Buffers
- Bidirectional streaming
- Multi-language client support
- Service mesh integration

**Unix Socket (Default):** Lowest latency for local connections, no port management, enhanced security through filesystem permissions.

**TCP:** Required for remote connections, service mesh integration, or when clients don't support Unix sockets.

Best for microservices, internal APIs, and high-performance systems.

### QUIC

Choose QUIC when you need:
- Mobile client connectivity with connection migration
- Low latency on high-latency networks (0-RTT resumption)
- Concurrent operations without head-of-line blocking
- Built-in TLS 1.3 encryption
- Binary protocol efficiency

Best for mobile applications, edge deployments, and unreliable network conditions.

### REST

Choose REST when you need:
- Universal HTTP client compatibility
- Browser-based applications
- Simple debugging with curl
- OpenAPI/Swagger documentation
- Standard HTTP tooling

Best for public APIs, web applications, and third-party integrations.

### MCP

Choose MCP when you need:
- AI agent integration (Claude, ChatGPT)
- Tool-based LLM interactions
- Context-aware database operations
- RAG workflows with vector search
- Semantic operations

Best for AI-powered applications, chatbots, and intelligent systems.

## Common Features

### Authentication

All APIs support the same authentication methods:

**API Keys:**
```bash
# REST
curl -H "X-API-Key: sk_live_abc123" http://localhost:8080/api/v1/health

# gRPC (via metadata)
md := metadata.New(map[string]string{"x-api-key": "sk_live_abc123"})
```

**Mutual TLS:**
```bash
curl --cert client.crt --key client.key --cacert ca.crt https://localhost:8443/api/v1/health
```

### Consistency Levels

All write operations support tunable consistency:
- `local` - Write to local node only (low latency)
- `quorum` - Write to majority of replicas (strong consistency)

### Error Codes

Consistent error codes across all protocols:

| Code | HTTP | gRPC | Meaning |
|------|------|------|---------|
| NOT_FOUND | 404 | NOT_FOUND | Entity does not exist |
| INVALID_ARGUMENT | 400 | INVALID_ARGUMENT | Invalid request parameters |
| PERMISSION_DENIED | 403 | PERMISSION_DENIED | Authorization failed |
| UNAVAILABLE | 503 | UNAVAILABLE | Service temporarily unavailable |
| INTERNAL | 500 | INTERNAL | Internal server error |

## Architecture

All API protocols share a common service facade (`service.QuicBaaS` interface):

```
┌──────────────────────────────────────────┐
│          API Protocol Layer              │
│  ┌──────┐ ┌──────┐ ┌──────┐ ┌──────┐    │
│  │ gRPC │ │ QUIC │ │ REST │ │ MCP  │    │
│  └───┬──┘ └───┬──┘ └───┬──┘ └───┬──┘    │
└──────┼────────┼────────┼────────┼────────┘
       └────────┴────────┴────────┘
                │
      ┌─────────────────────┐
      │  service.QuicBaaS   │
      │  • KV Operations    │
      │  • Transactions     │
      │  • Vector Search    │
      │  • Cluster Mgmt     │
      │  • Time-Series      │
      │  • Auth/RBAC        │
      └─────────────────────┘
```

This architecture provides:
- Protocol independence - add new APIs without changing business logic
- Code reuse - shared implementations across all protocols
- Consistent behavior - same operations produce identical results
- Easy testing - test once, deploy everywhere

## See Also

- [gRPC API](grpc.md) - High-performance binary protocol
- [QUIC API](quic.md) - Mobile and high-latency network protocol
- [REST API](rest.md) - HTTP/JSON interface
- [MCP API](mcp.md) - AI agent integration
- [CLI Guide](../cli/README.md) - Command-line interface
- [Architecture](../architecture/README.md) - System design
