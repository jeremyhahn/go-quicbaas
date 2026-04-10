# QuicBaaS Project Overview

## Purpose
QuicBaaS is a high-performance, multi-faceted, distributed key-value database built on multi-Raft consensus (via go-quicraft) and PebbleDB storage. Designed for speed, strong consistency, horizontal scalability, security, and operational simplicity.

## Version
0.1.0-alpha

## Tech Stack
- **Language**: Go 1.26
- **Consensus**: Multi-Raft via go-quicraft (QuicRaft)
- **Storage**: CockroachDB PebbleDB (LSM-tree)
- **Protocols**: gRPC, REST (chi router), QUIC, MCP (Model Context Protocol), WebSocket (MQTT, AMQP), Unix socket
- **Auth**: JWT, WebAuthn, OIDC, RBAC/ABAC (IAM)
- **Crypto**: AES-256-GCM barrier with epoch key rotation, Shamir secret sharing, FIPS 140-3
- **Serialization**: Protobuf, JSON, YAML, TOML, MsgPack, BSON, CBOR, Avro
- **SDKs**: Go, JavaScript
- **Build**: Makefile-driven, Docker, devcontainer

## Architecture Layers
1. **Transport** (`pkg/api/`): gRPC, REST, QUIC, MCP, WebSocket, Unix socket
2. **Service** (`pkg/service/`): QuicBaaS facade (business logic)
3. **Raft** (`pkg/raft/`): Multi-Raft consensus (node, statemachine, batch, writemode, bootstrap, coordinator)
4. **Storage** (`pkg/storage/`, `pkg/kvstore/`): PebbleDB-backed KV store
5. **Security** (`pkg/seal/`, `pkg/kms/`, `pkg/secrets/`, `pkg/auth/`, `pkg/iam/`): Barrier, KMS, secrets, auth
6. **Features** (`pkg/vector/`, `pkg/rag/`, `pkg/scheduler/`, `pkg/backup/`, etc.)

## Binaries
- `cmd/quicbaasd/` — Server daemon
- `cmd/quicbaas/` — CLI client

## Key Dependencies
- `github.com/jeremyhahn/go-quicraft` — QuicRaft consensus library
- `github.com/cockroachdb/pebble` — LSM storage engine
- `github.com/go-chi/chi/v5` — REST router
- `google.golang.org/grpc` — gRPC framework
- `github.com/jeremyhahn/go-codec` — Multi-format codec
- `github.com/jeremyhahn/go-objstore` — Object storage abstraction
