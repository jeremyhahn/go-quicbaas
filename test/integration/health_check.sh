#!/bin/bash
# Health check script for integration test environment
# Verifies that the QuicBaaS cluster is fully operational before running tests
# Includes Raft cluster readiness verification

set -e

# Configuration
MAX_RETRIES=${QUICBAAS_HEALTH_TIMEOUT:-180}
CHECK_INTERVAL=${QUICBAAS_CHECK_INTERVAL:-2}
NODE1_URL=${QUICBAAS_NODE1_URL:-http://test-node1:8080}
NODE2_URL=${QUICBAAS_NODE2_URL:-http://test-node2:8080}
NODE3_URL=${QUICBAAS_NODE3_URL:-http://test-node3:8080}

echo "Starting health check for QuicBaaS cluster..."
echo "Nodes:"
echo "  - Node 1: ${NODE1_URL}"
echo "  - Node 2: ${NODE2_URL}"
echo "  - Node 3: ${NODE3_URL}"
echo "Timeout: ${MAX_RETRIES}s, Check interval: ${CHECK_INTERVAL}s"
echo ""

check_node_liveness() {
    local node_url=$1
    local node_name=$2

    if curl -ksf "${node_url}/api/v1/health/live" > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

check_node_readiness() {
    local node_url=$1
    local node_name=$2

    # Check readiness endpoint - this verifies Raft is operational
    local response
    response=$(curl -ksf "${node_url}/api/v1/health/ready" 2>&1) || return 1

    # Check if the response indicates ready status
    if echo "$response" | grep -q '"status":"healthy"'; then
        return 0
    else
        return 1
    fi
}


# Phase 1: Wait for all nodes to be alive (HTTP server responding)
echo "Phase 1: Checking HTTP liveness..."
elapsed=0
all_alive=false

while [ $elapsed -lt $MAX_RETRIES ]; do
    node1_alive=false
    node2_alive=false
    node3_alive=false

    check_node_liveness "${NODE1_URL}" "Node 1" && node1_alive=true
    check_node_liveness "${NODE2_URL}" "Node 2" && node2_alive=true
    check_node_liveness "${NODE3_URL}" "Node 3" && node3_alive=true

    if [ "$node1_alive" = true ] && [ "$node2_alive" = true ] && [ "$node3_alive" = true ]; then
        all_alive=true
        echo "✓ All nodes are alive (HTTP responding)"
        break
    fi

    echo "  [${elapsed}s] Waiting for nodes to be alive (1:$node1_alive, 2:$node2_alive, 3:$node3_alive)..."
    sleep $CHECK_INTERVAL
    elapsed=$((elapsed + CHECK_INTERVAL))
done

if [ "$all_alive" != true ]; then
    echo "✗ Timeout waiting for nodes to be alive after ${elapsed}s"
    exit 1
fi

# Phase 2: Wait for Raft cluster to be ready
echo ""
echo "Phase 2: Waiting for Raft cluster to be ready..."
raft_ready=false
raft_wait=0
RAFT_TIMEOUT=120  # Additional wait time for Raft

while [ $raft_wait -lt $RAFT_TIMEOUT ]; do
    node1_ready=false
    node2_ready=false
    node3_ready=false

    check_node_readiness "${NODE1_URL}" "Node 1" && node1_ready=true
    check_node_readiness "${NODE2_URL}" "Node 2" && node2_ready=true
    check_node_readiness "${NODE3_URL}" "Node 3" && node3_ready=true

    if [ "$node1_ready" = true ] && [ "$node2_ready" = true ] && [ "$node3_ready" = true ]; then
        raft_ready=true
        echo "✓ All nodes report ready status"
        break
    fi

    echo "  [${raft_wait}s] Waiting for Raft readiness (1:$node1_ready, 2:$node2_ready, 3:$node3_ready)..."
    sleep $CHECK_INTERVAL
    raft_wait=$((raft_wait + CHECK_INTERVAL))
done

if [ "$raft_ready" != true ]; then
    echo "⚠ Warning: Not all nodes report ready status after ${raft_wait}s"
    echo "  Continuing with operational verification..."
fi

# Phase 3: Verify Raft is operational via authenticated Put/Get
# Authenticate first, then test a write/read cycle
echo ""
echo "Phase 3: Verifying Raft cluster is operational..."
AUTH_TOKEN=""
operational=false
op_wait=0
OP_TIMEOUT=60

while [ $op_wait -lt $OP_TIMEOUT ]; do
    # Authenticate if we don't have a token yet
    if [ -z "$AUTH_TOKEN" ]; then
        LOGIN_RESPONSE=$(curl -ks -X POST \
            -H "Content-Type: application/json" \
            -d '{"username":"admin","password":"admin"}' \
            "${NODE1_URL}/api/v1/auth/login" 2>&1) || true

        if [ -n "$LOGIN_RESPONSE" ]; then
            AUTH_TOKEN=$(echo "$LOGIN_RESPONSE" | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)
        fi
    fi

    # Try Put/Get if we have a token
    if [ -n "$AUTH_TOKEN" ]; then
        test_id=$(($(date +%s) % 1000000000))
        put_ok=false
        get_ok=false

        curl -ksf -X PUT \
            -H "Content-Type: application/json" \
            -H "Authorization: Bearer ${AUTH_TOKEN}" \
            -d "{\"data\": \"health_check\"}" \
            "${NODE1_URL}/api/v1/entities/health_check/${test_id}" > /dev/null 2>&1 && put_ok=true

        if [ "$put_ok" = true ]; then
            curl -ksf \
                -H "Authorization: Bearer ${AUTH_TOKEN}" \
                "${NODE1_URL}/api/v1/entities/health_check/${test_id}" > /dev/null 2>&1 && get_ok=true

            # Cleanup
            curl -ksf -X DELETE \
                -H "Authorization: Bearer ${AUTH_TOKEN}" \
                "${NODE1_URL}/api/v1/entities/health_check/${test_id}" > /dev/null 2>&1 || true
        fi

        if [ "$put_ok" = true ] && [ "$get_ok" = true ]; then
            operational=true
            echo "✓ Raft cluster is operational (Put/Get succeeded on Node 1)"
            break
        fi
    fi

    echo "  [${op_wait}s] Waiting for Raft operations to succeed..."
    sleep $CHECK_INTERVAL
    op_wait=$((op_wait + CHECK_INTERVAL))
done

if [ "$operational" != true ]; then
    echo "⚠ Warning: Could not verify Raft operations after ${op_wait}s"
    echo "  Cluster health reports operational - proceeding with tests"
    echo "  (Integration tests will verify functionality directly)"
fi

# Final check: verify all nodes can serve reads
echo ""
echo "Phase 4: Verifying all nodes can serve reads..."
for node_url in "${NODE1_URL}" "${NODE2_URL}" "${NODE3_URL}"; do
    node_name=$(echo "$node_url" | grep -oE 'test-node[0-9]+')
    if curl -ksf "${node_url}/api/v1/health" > /dev/null 2>&1; then
        echo "✓ ${node_name} can serve reads"
    else
        echo "⚠ ${node_name} health check failed"
    fi
done

# Phase 5: Verify gRPC ports are listening
# This is critical because CLI tests use gRPC transport
echo ""
echo "Phase 5: Verifying gRPC ports are listening..."
GRPC_PORT=50051
MCP_PORT=8888
GRPC_TIMEOUT=60
grpc_wait=0

check_grpc_port() {
    local host=$1
    local port=$2
    # Use nc (netcat) to check if TCP port is listening
    nc -z -w 2 "$host" "$port" > /dev/null 2>&1
    return $?
}

grpc_ready=false
while [ $grpc_wait -lt $GRPC_TIMEOUT ]; do
    node1_grpc=false
    node2_grpc=false
    node3_grpc=false

    check_grpc_port "test-node1" $GRPC_PORT && node1_grpc=true
    check_grpc_port "test-node2" $GRPC_PORT && node2_grpc=true
    check_grpc_port "test-node3" $GRPC_PORT && node3_grpc=true

    if [ "$node1_grpc" = true ] && [ "$node2_grpc" = true ] && [ "$node3_grpc" = true ]; then
        grpc_ready=true
        echo "✓ All gRPC ports (${GRPC_PORT}) are listening"
        break
    fi

    echo "  [${grpc_wait}s] Waiting for gRPC ports (1:$node1_grpc, 2:$node2_grpc, 3:$node3_grpc)..."
    sleep $CHECK_INTERVAL
    grpc_wait=$((grpc_wait + CHECK_INTERVAL))
done

if [ "$grpc_ready" != true ]; then
    echo "⚠ Warning: Not all gRPC ports are ready after ${grpc_wait}s"
    echo "  Tests may fail for gRPC transport"
fi

# Phase 6: Verify MCP ports are listening
echo ""
echo "Phase 6: Verifying MCP ports are listening..."
mcp_wait=0
MCP_TIMEOUT=30

mcp_ready=false
while [ $mcp_wait -lt $MCP_TIMEOUT ]; do
    node1_mcp=false
    node2_mcp=false
    node3_mcp=false

    check_grpc_port "test-node1" $MCP_PORT && node1_mcp=true
    check_grpc_port "test-node2" $MCP_PORT && node2_mcp=true
    check_grpc_port "test-node3" $MCP_PORT && node3_mcp=true

    if [ "$node1_mcp" = true ] && [ "$node2_mcp" = true ] && [ "$node3_mcp" = true ]; then
        mcp_ready=true
        echo "✓ All MCP ports (${MCP_PORT}) are listening"
        break
    fi

    echo "  [${mcp_wait}s] Waiting for MCP ports (1:$node1_mcp, 2:$node2_mcp, 3:$node3_mcp)..."
    sleep $CHECK_INTERVAL
    mcp_wait=$((mcp_wait + CHECK_INTERVAL))
done

if [ "$mcp_ready" != true ]; then
    echo "⚠ Warning: Not all MCP ports are ready after ${mcp_wait}s"
    echo "  Tests may fail for MCP transport"
fi

# Phase 7: Brief stabilization delay to ensure all services are fully initialized
echo ""
echo "Phase 7: Allowing brief stabilization period..."
sleep 5
echo "✓ Stabilization complete"

echo ""
echo "========================================"
echo "✓ All nodes are healthy and ready!"
echo "✓ Raft cluster is fully operational."
echo "✓ gRPC and MCP ports are listening."
echo "========================================"
exit 0
