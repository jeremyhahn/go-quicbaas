# quicbaas server

Server management commands for QuicBaaS.

## Commands

### start

Start the QuicBaaS server with HTTP, gRPC, and Raft interfaces.

```bash
quicbaas start
```

#### Flags
- `--http-port int`: HTTP server port (default: 8080)
- `--grpc-port int`: gRPC server port (default: 9090)
- `--raft-port int`: Raft communication port (default: 63000)
- `--raft-address string`: Raft node address (default: <hostname>:<raft-port>)
- `--node-id uint64`: Raft node ID (default: 1)
- `--config string`: Path to configuration file
- `--fips`: Enable FIPS mode
- `--log-level string`: Log level (debug, info, warn, error) (default: "info")

#### Examples

```bash
# Start server with defaults
quicbaas start

# Start with custom ports
quicbaas start --http-port 8080 --grpc-port 9090 --raft-port 63000

# Start in FIPS mode
quicbaas start --fips --log-level debug

# Start with custom configuration file
quicbaas start --config /etc/quicbaas/config.yaml

# Start as a cluster node
quicbaas start --node-id 2 --raft-address 192.168.1.2:63000
```

## Server Features

### HTTP REST API
- RESTful API for data operations
- OpenAPI/Swagger documentation
- JSON request/response format
- CORS support for browser clients

### gRPC API
- High-performance RPC interface
- Protocol Buffers serialization
- Streaming support
- Native language bindings

### Raft Consensus
- Multi-Raft for horizontal scalability
- Strong consistency guarantees
- Automatic leader election
- Log replication and snapshotting

### Storage Engine
- PebbleDB (RocksDB fork) backend
- LSM-tree architecture
- Automatic compaction
- Write-ahead logging (WAL)

### Security
- TLS/mTLS support
- RBAC authorization
- Encryption at rest
- Audit logging

## Configuration File

The configuration file supports YAML format with sections for:
- Server settings (ports, timeouts, limits)
- Raft cluster configuration
- Storage settings (data directory, cache size)
- Security settings (TLS, encryption, FIPS)
- Logging configuration

Example configuration:
```yaml
server:
  port: 8080
  grpc_port: 9090
  shutdown_timeout: 30s

raft:
  node_id: 1
  raft_address: "localhost:63000"
  data_dir: "/var/lib/quicbaas/raft"

storage:
  data_dir: "/var/lib/quicbaas/data"
  cache_size: 512MB

security:
  tls_enabled: true
  tls_cert: "/etc/quicbaas/server.crt"
  tls_key: "/etc/quicbaas/server.key"
  tls_ca: "/etc/quicbaas/ca.crt"
```

## Startup Process

1. **Configuration**: Load config from file and CLI flags
2. **Data Directory**: Create/verify data directory
3. **Server Instance**: Create server with config
4. **Raft Initialization**: Initialize Raft node
5. **Storage**: Initialize storage engine
6. **API Servers**: Start HTTP and gRPC servers
7. **Ready**: Server ready to accept requests

## Graceful Shutdown

The server handles shutdown signals (SIGTERM, SIGINT) gracefully:
1. Stop accepting new requests
2. Complete in-flight requests
3. Flush pending writes
4. Close Raft connections
5. Shutdown storage engine

## Environment Variables

- `QUICBAAS_CONFIG`: Path to configuration file
- `QUICBAAS_DATA_DIR`: Data directory
- `QUICBAAS_LOG_LEVEL`: Log level
- `QUICBAAS_FIPS`: Enable FIPS mode (true/false)

## Notes

- Server requires write access to data directory
- Default data directory: `/var/lib/quicbaas` or `./data`
- HTTP and gRPC servers run concurrently
- Raft address must be reachable by other cluster nodes
- Node ID must be unique within the cluster
- FIPS mode enforces cryptographic standards
