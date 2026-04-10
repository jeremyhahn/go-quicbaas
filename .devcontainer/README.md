# QuicBaaS Development Container

This devcontainer provides a complete development environment for QuicBaaS with all services pre-configured and running.

## Features

- Go 1.25 development environment
- All Go development tools (golangci-lint, delve, gopls, etc.)
- Docker-in-Docker support for integration tests
- QuicBaaS server auto-starts with all services enabled
- Port forwarding for all services

## Automatically Started Services

When the devcontainer starts, the QuicBaaS server is automatically launched with ALL services enabled:

| Service | Port/Path | Description |
|---------|-----------|-------------|
| REST API | http://localhost:8080 | HTTP REST API |
| gRPC TCP | localhost:9090 | gRPC over TCP |
| QUIC | localhost:4433 | QUIC protocol |
| Unix Socket | /tmp/quicbaas.sock | Unix domain socket |
| Raft | localhost:63000 | Raft consensus |

## Quick Start

1. Open this repository in VS Code
2. Click "Reopen in Container" when prompted
3. Wait for the container to build and start
4. The QuicBaaS server starts automatically!

## Testing the Server

```bash
# Health check
curl http://localhost:8080/health

# Metrics
curl http://localhost:8080/metrics

# View server logs
tail -f /var/log/quicbaas/server.log

# Check server status
cat /tmp/quicbaas.pid
```

## Server Management

### Check Server Status
```bash
# Check if running
if [ -f /tmp/quicbaas.pid ]; then
    PID=$(cat /tmp/quicbaas.pid)
    if kill -0 $PID 2>/dev/null; then
        echo "QuicBaaS is running (PID: $PID)"
    else
        echo "QuicBaaS is not running"
    fi
fi
```

### Stop Server
```bash
/tmp/stop-quicbaas.sh
```

### Restart Server
```bash
# Stop the server
/tmp/stop-quicbaas.sh

# Start it again
/workspace/.devcontainer/start-quicbaas.sh
```

### View Logs
```bash
# Real-time logs
tail -f /var/log/quicbaas/server.log

# Last 100 lines
tail -n 100 /var/log/quicbaas/server.log

# Search logs
grep -i error /var/log/quicbaas/server.log
```

## Development Workflow

### Building
```bash
# Build with IAM support
make build

# Build specific components
go build -tags 'iam' ./cmd/quicbaasd
go build -tags 'iam' ./cmd/quicbaas
```

### Testing
```bash
# Run unit tests
make test

# Run integration tests
make integration-test

# Run specific package tests
go test -v -tags 'iam' ./pkg/api/rest/...

# Run benchmarks
make bench
```

### Code Quality
```bash
# Format code
make fmt

# Run linters
make lint

# Run security scan
make security-scan
```

## Environment Variables

The devcontainer sets the following environment variables:

```bash
QUICBAAS_LOG_LEVEL=debug
QUICBAAS_LOG_FORMAT=console
QUICBAAS_DATA_DIR=/data
QUICBAAS_SERVER_ENABLE_REST=true
QUICBAAS_SERVER_ENABLE_TCP=true
QUICBAAS_SERVER_ENABLE_QUIC=true
QUICBAAS_SERVER_PORT=8080
QUICBAAS_SERVER_GRPC_PORT=9090
QUICBAAS_SERVER_QUIC_PORT=4433
QUICBAAS_SERVER_UNIX_SOCKET=/tmp/quicbaas.sock
```

## Data Persistence

Data is persisted in Docker volumes:

- `quicbaas-data`: Server data directory
- `quicbaas-gomod-cache`: Go module cache
- `quicbaas-gobuild-cache`: Go build cache

To reset data:
```bash
docker volume rm quicbaas-data
```

## Configuration

The server uses a development configuration file generated at `/tmp/quicbaas-dev.yaml`.

Key features enabled:
- All protocols (REST, gRPC, QUIC, Unix socket)
- KMS with software backend
- Secrets management
- IAM with local authentication
- Debug logging
- Fast development settings (reduced sync, smaller buffers)

## Troubleshooting

### Server won't start

Check the logs:
```bash
tail -n 100 /var/log/quicbaas/server.log
```

### Port already in use

Stop any conflicting processes:
```bash
sudo lsof -i :8080
sudo lsof -i :9090
sudo lsof -i :4433
```

### Container rebuild

If you encounter issues, rebuild the container:
```bash
# From VS Code Command Palette
Dev Containers: Rebuild Container

# Or from command line
docker compose -f .devcontainer/docker-compose.yml build --no-cache
```

### Reset everything

```bash
# Stop container
docker compose -f .devcontainer/docker-compose.yml down

# Remove volumes
docker volume rm quicbaas-data quicbaas-gomod-cache quicbaas-gobuild-cache

# Rebuild
docker compose -f .devcontainer/docker-compose.yml up --build
```

## VS Code Integration

### Recommended Extensions

All required extensions are automatically installed:
- Go (official)
- Docker
- GitLens
- YAML
- Protocol Buffers
- Error Lens

### Debugging

Launch configurations are available for:
- Running the server with debugger attached
- Debugging tests
- Remote debugging

Press F5 to start debugging.

## Performance

The devcontainer is configured with:
- 8GB memory minimum
- 4 CPU cores minimum
- Docker-in-Docker for isolation
- Volume caching for fast builds

## Security

Development mode has security features disabled for convenience:
- No TLS
- No encryption at rest
- No FIPS mode
- Local authentication only

For production configuration, see `/home/jhahn/sources/go-quicbaas/.quicbaas.example.yaml`.

## Additional Resources

- [QuicBaaS Documentation](../docs/)
- [Cache Documentation](../docs/cache/)
- [Security Documentation](../docs/security/)
- [KMS Documentation](../docs/kms/)
- [IAM Documentation](../docs/iam/)
