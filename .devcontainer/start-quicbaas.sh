#!/bin/bash
# QuicBaaS Devcontainer Startup Script
# This script starts the QuicBaaS server with all services enabled

set -e

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}QuicBaaS Development Container Startup${NC}"
echo -e "${BLUE}========================================${NC}"

# Change to workspace directory
cd /workspace

# Ensure binary is built with IAM support
echo -e "${YELLOW}Building QuicBaaS with IAM support...${NC}"
if go build -tags 'iam' -o /tmp/quicbaasd ./cmd/quicbaasd; then
    echo -e "${GREEN}Build successful${NC}"
else
    echo -e "${RED}Build failed, skipping server startup${NC}"
    exit 0
fi

# Create necessary directories
echo -e "${YELLOW}Creating runtime directories...${NC}"
mkdir -p /data
mkdir -p /var/log/quicbaas
mkdir -p /var/run/quicbaas
mkdir -p /tmp

# Ensure Unix socket directory exists and remove stale socket
if [ -S "/tmp/quicbaas.sock" ]; then
    echo -e "${YELLOW}Removing stale Unix socket...${NC}"
    rm -f /tmp/quicbaas.sock
fi

# Create development configuration
echo -e "${YELLOW}Creating development configuration...${NC}"
cat > /tmp/quicbaas-dev.yaml <<EOF
# QuicBaaS Development Configuration
# Auto-generated for devcontainer

server:
  host: "0.0.0.0"
  port: 8080
  grpc_port: 9090
  quic_port: 4433
  unix_socket: "/tmp/quicbaas.sock"
  enable_tcp: true
  enable_rest: true
  enable_quic: true
  read_timeout: 30s
  write_timeout: 30s
  idle_timeout: 60s
  shutdown_timeout: 30s
  max_connections: 1000

raft:
  node_id: 1
  election_rtt: 5
  heartbeat_rtt: 1
  snapshot_entries: 1000
  compaction_overhead: 500
  request_timeout: 3s
  rtt_millisecond: 100
  raft_address: "127.0.0.1:63000"
  listen_address: "0.0.0.0:63000"
  mutual_tls: false
  enable_metrics: true

storage:
  data_dir: "/data"
  wal_dir: "/data/wal"
  cache_size: 67108864  # 64MB
  max_open_files: 500
  max_concurrent_reads: 50
  max_concurrent_writes: 50
  disable_wal: false
  sync_writes: false  # Faster for development
  bytes_per_sync: 1048576
  mem_table_size: 33554432  # 32MB
  mem_table_stop_writes: 2
  l0_compaction_threshold: 4
  l0_stop_writes_threshold: 8
  block_size: 4096
  target_file_size: 33554432  # 32MB
  compression_type: "snappy"

security:
  tls_enabled: false
  tls_cert_file: ""
  tls_key_file: ""
  tls_ca_file: ""
  tls_client_auth: false
  api_key_enabled: false
  api_keys: []
  encryption_enabled: false
  encryption_key_file: ""
  fips_mode: false
  audit_log_enabled: false
  audit_log_file: "/var/log/quicbaas/audit.log"

logging:
  level: "debug"
  format: "console"
  output: "stdout"
  file: "/var/log/quicbaas/quicbaas.log"
  max_size: 100
  max_backups: 3
  max_age: 28
  compress: true

cache:
  enabled: true
  default_ttl: 5m
  max_size: 10000
  cleanup_interval: 1m
  write_through: true
  l2_enabled: false

kms:
  enabled: true
  backend: software
  auto_generate_key: true

secrets:
  enabled: true
  encryption:
    default_key_id: "quicbaas-secrets-key"

iam:
  enabled: true
  mode: primary
  tenants:
    default: "dev-tenant"
    isolation: strict
    auto_create: true
  auth:
    local:
      enabled: true
EOF

# Start QuicBaaS server in background
echo -e "${YELLOW}Starting QuicBaaS server...${NC}"
nohup /tmp/quicbaasd cluster --config /tmp/quicbaas-dev.yaml \
    --log-level debug \
    --data-dir /data \
    > /var/log/quicbaas/server.log 2>&1 &

QUICBAAS_PID=$!
echo $QUICBAAS_PID > /tmp/quicbaas.pid

# Wait for server to be ready
echo -e "${YELLOW}Waiting for server to be ready...${NC}"
MAX_RETRIES=30
RETRY_COUNT=0
READY=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    # Check if process is still running
    if ! kill -0 $QUICBAAS_PID 2>/dev/null; then
        echo -e "${RED}Server process died unexpectedly${NC}"
        echo -e "${RED}Last 20 lines of log:${NC}"
        tail -n 20 /var/log/quicbaas/server.log
        exit 1
    fi

    # Try to connect to REST API
    if curl -s http://localhost:8080/health >/dev/null 2>&1; then
        READY=true
        break
    fi

    # Try to connect to Unix socket
    if [ -S "/tmp/quicbaas.sock" ]; then
        READY=true
        break
    fi

    sleep 1
    RETRY_COUNT=$((RETRY_COUNT + 1))
done

if [ "$READY" = true ]; then
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}QuicBaaS Server Started Successfully!${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "${BLUE}Available Services:${NC}"
    echo -e "  ${GREEN}REST API:${NC}      http://localhost:8080"
    echo -e "  ${GREEN}gRPC TCP:${NC}      localhost:9090"
    echo -e "  ${GREEN}QUIC:${NC}          localhost:4433"
    echo -e "  ${GREEN}Unix Socket:${NC}   /tmp/quicbaas.sock"
    echo -e "  ${GREEN}Raft:${NC}          localhost:63000"
    echo ""
    echo -e "${BLUE}Quick Tests:${NC}"
    echo -e "  ${YELLOW}curl http://localhost:8080/health${NC}"
    echo -e "  ${YELLOW}curl http://localhost:8080/metrics${NC}"
    echo ""
    echo -e "${BLUE}Logs:${NC}"
    echo -e "  ${YELLOW}tail -f /var/log/quicbaas/server.log${NC}"
    echo ""
    echo -e "${BLUE}Process:${NC}"
    echo -e "  ${YELLOW}PID: $QUICBAAS_PID${NC}"
    echo -e "  ${YELLOW}Stop: kill \$(cat /tmp/quicbaas.pid)${NC}"
    echo ""

    # Show server info
    if curl -s http://localhost:8080/health >/dev/null 2>&1; then
        echo -e "${GREEN}Server health check: OK${NC}"
    fi
else
    echo -e "${RED}========================================${NC}"
    echo -e "${RED}Server failed to start within ${MAX_RETRIES} seconds${NC}"
    echo -e "${RED}========================================${NC}"
    echo -e "${RED}Last 50 lines of log:${NC}"
    tail -n 50 /var/log/quicbaas/server.log

    # Kill the process if it's still running
    if kill -0 $QUICBAAS_PID 2>/dev/null; then
        kill $QUICBAAS_PID
    fi
    exit 1
fi

# Create a helper script for stopping the server
cat > /tmp/stop-quicbaas.sh <<'STOP_SCRIPT'
#!/bin/bash
if [ -f /tmp/quicbaas.pid ]; then
    PID=$(cat /tmp/quicbaas.pid)
    if kill -0 $PID 2>/dev/null; then
        echo "Stopping QuicBaaS (PID: $PID)..."
        kill $PID
        sleep 2
        if kill -0 $PID 2>/dev/null; then
            echo "Force killing QuicBaaS..."
            kill -9 $PID
        fi
        rm -f /tmp/quicbaas.pid
        rm -f /tmp/quicbaas.sock
        echo "QuicBaaS stopped"
    else
        echo "QuicBaaS is not running"
        rm -f /tmp/quicbaas.pid
    fi
else
    echo "No PID file found"
fi
STOP_SCRIPT

chmod +x /tmp/stop-quicbaas.sh

echo -e "${BLUE}========================================${NC}"
echo -e "${GREEN}Setup complete! Happy coding!${NC}"
echo -e "${BLUE}========================================${NC}"
