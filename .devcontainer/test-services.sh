#!/bin/bash
# Test script to verify all QuicBaaS services are accessible

set -e

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "Testing QuicBaaS Services..."
echo "=============================="

# Test 1: Check if server process is running
echo -n "1. Server process: "
if [ -f /tmp/quicbaas.pid ]; then
    PID=$(cat /tmp/quicbaas.pid)
    if kill -0 $PID 2>/dev/null; then
        echo -e "${GREEN}RUNNING${NC} (PID: $PID)"
    else
        echo -e "${RED}NOT RUNNING${NC}"
        exit 1
    fi
else
    echo -e "${RED}NO PID FILE${NC}"
    exit 1
fi

# Test 2: Check Unix socket
echo -n "2. Unix socket: "
if [ -S /tmp/quicbaas.sock ]; then
    echo -e "${GREEN}EXISTS${NC}"
else
    echo -e "${YELLOW}NOT FOUND${NC} (may not be created yet)"
fi

# Test 3: Check REST API
echo -n "3. REST API (8080): "
if curl -s -f http://localhost:8080/health >/dev/null 2>&1; then
    echo -e "${GREEN}ACCESSIBLE${NC}"
else
    echo -e "${RED}NOT ACCESSIBLE${NC}"
fi

# Test 4: Check gRPC port
echo -n "4. gRPC port (9090): "
if nc -z localhost 9090 2>/dev/null; then
    echo -e "${GREEN}LISTENING${NC}"
else
    echo -e "${RED}NOT LISTENING${NC}"
fi

# Test 5: Check QUIC port
echo -n "5. QUIC port (4433): "
if nc -z localhost 4433 2>/dev/null; then
    echo -e "${GREEN}LISTENING${NC}"
else
    echo -e "${YELLOW}NOT LISTENING${NC} (may not be enabled)"
fi

# Test 6: Check Raft port
echo -n "6. Raft port (63000): "
if nc -z localhost 63000 2>/dev/null; then
    echo -e "${GREEN}LISTENING${NC}"
else
    echo -e "${RED}NOT LISTENING${NC}"
fi

echo ""
echo "=============================="
echo -e "${GREEN}Service check complete!${NC}"
echo ""
echo "Try these commands:"
echo "  curl http://localhost:8080/health"
echo "  curl http://localhost:8080/metrics"
echo "  tail -f /var/log/quicbaas/server.log"
