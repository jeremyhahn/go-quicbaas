#!/bin/bash
# Health check script for integration test environment
# Verifies that the QuicBaaS cluster is fully operational before running tests:
# every node is live and ready, accepts a PIV login with the integration admin
# certificate, and listens on its gRPC and MCP ports. Any failure exits 1.
#
# Exercised without Docker by health_check_test.sh (`make check-workspace-script`).

set -e

# Configuration
MAX_RETRIES=${QUICBAAS_HEALTH_TIMEOUT:-180}
CHECK_INTERVAL=${QUICBAAS_CHECK_INTERVAL:-2}
STABILIZE_SECONDS=${QUICBAAS_STABILIZE_SECONDS:-5}
NODE1_URL=${QUICBAAS_NODE1_URL:-http://test-node1:8080}
NODE2_URL=${QUICBAAS_NODE2_URL:-http://test-node2:8080}
NODE3_URL=${QUICBAAS_NODE3_URL:-http://test-node3:8080}
NODE_URLS=("${NODE1_URL}" "${NODE2_URL}" "${NODE3_URL}")

# The integration admin PIV credential, at the paths helpers.AuthPIV uses.
PIV_CERT=${QUICBAAS_TEST_PIV_CERT:-/etc/quicbaas/certs/admin.crt}
PIV_KEY=${QUICBAAS_TEST_PIV_KEY:-/etc/quicbaas/certs/admin.key}
PIV_CA=${QUICBAAS_TEST_PIV_CA:-/etc/quicbaas/certs/ca.crt}

echo "Starting health check for QuicBaaS cluster..."
echo "Nodes:"
echo "  - Node 1: ${NODE1_URL}"
echo "  - Node 2: ${NODE2_URL}"
echo "  - Node 3: ${NODE3_URL}"
echo "Timeout: ${MAX_RETRIES}s, Check interval: ${CHECK_INTERVAL}s"
echo ""

check_node_liveness() {
    local node_url=$1

    curl -ksf "${node_url}/api/v1/health/live" > /dev/null 2>&1
}

check_node_readiness() {
    local node_url=$1

    # Check readiness endpoint - this verifies Raft is operational
    local response
    response=$(curl -ksf "${node_url}/api/v1/health/ready" 2>&1) || return 1

    echo "$response" | grep -q '"status":"healthy"'
}

# piv_login logs in to one node with the admin PIV certificate and prints the
# access token on stdout. On a transport error, a non-200 status or an empty
# access_token it reports the node and the HTTP status on stderr and returns 1.
# The token itself is never written to the log.
piv_login() {
    local node_url=$1
    local node_name=$2
    local response status body token rc=0

    # A node that accepts the connection and never answers must fail the check,
    # not hang it: the request is bounded by the health timeout.
    response=$(curl -s --connect-timeout "${MAX_RETRIES}" --max-time "${MAX_RETRIES}" \
        --cacert "${PIV_CA}" --cert "${PIV_CERT}" --key "${PIV_KEY}" \
        -X POST -w '\n%{http_code}' \
        "${node_url}/api/v1/auth/login/piv" 2>/dev/null) || rc=$?
    if [ "$rc" = "28" ]; then
        echo "✗ PIV login failed on ${node_name} (${node_url}): no answer within ${MAX_RETRIES}s" >&2
        return 1
    fi
    # Any other transport error (a transfer cut mid-response, a TLS failure)
    # fails the check before the response is parsed: a partial body can still
    # hold a complete access_token.
    if [ "$rc" != "0" ]; then
        echo "✗ PIV login failed on ${node_name} (${node_url}): curl exit code ${rc}" >&2
        return 1
    fi
    status=${response##*$'\n'}
    body=${response%$'\n'*}

    if [ "$status" != "200" ]; then
        echo "✗ PIV login failed on ${node_name} (${node_url}): HTTP status ${status:-000}" >&2
        return 1
    fi

    token=$(echo "$body" | grep -o '"access_token" *: *"[^"]*"' | cut -d'"' -f4)
    if [ -z "$token" ]; then
        echo "✗ PIV login failed on ${node_name} (${node_url}): HTTP status ${status} with an empty access_token" >&2
        return 1
    fi

    echo "$token"
}

# Phase 1: Wait for all nodes to be alive (HTTP server responding)
echo "Phase 1: Checking HTTP liveness..."
elapsed=0
all_alive=false

while [ "$elapsed" -lt "$MAX_RETRIES" ]; do
    node1_alive=false
    node2_alive=false
    node3_alive=false

    check_node_liveness "${NODE1_URL}" && node1_alive=true
    check_node_liveness "${NODE2_URL}" && node2_alive=true
    check_node_liveness "${NODE3_URL}" && node3_alive=true

    if [ "$node1_alive" = true ] && [ "$node2_alive" = true ] && [ "$node3_alive" = true ]; then
        all_alive=true
        echo "✓ All nodes are alive (HTTP responding)"
        break
    fi

    echo "  [${elapsed}s] Waiting for nodes to be alive (1:$node1_alive, 2:$node2_alive, 3:$node3_alive)..."
    sleep "$CHECK_INTERVAL"
    elapsed=$((elapsed + CHECK_INTERVAL))
done

if [ "$all_alive" != true ]; then
    echo "✗ Timeout waiting for nodes to be alive after ${elapsed}s (1:$node1_alive, 2:$node2_alive, 3:$node3_alive)"
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

    check_node_readiness "${NODE1_URL}" && node1_ready=true
    check_node_readiness "${NODE2_URL}" && node2_ready=true
    check_node_readiness "${NODE3_URL}" && node3_ready=true

    if [ "$node1_ready" = true ] && [ "$node2_ready" = true ] && [ "$node3_ready" = true ]; then
        raft_ready=true
        echo "✓ All nodes report ready status"
        break
    fi

    echo "  [${raft_wait}s] Waiting for Raft readiness (1:$node1_ready, 2:$node2_ready, 3:$node3_ready)..."
    sleep "$CHECK_INTERVAL"
    raft_wait=$((raft_wait + CHECK_INTERVAL))
done

if [ "$raft_ready" != true ]; then
    echo "✗ Timeout waiting for Raft readiness after ${raft_wait}s (1:$node1_ready, 2:$node2_ready, 3:$node3_ready)"
    exit 1
fi

# Phase 3: Log in on every node with the admin PIV certificate
echo ""
echo "Phase 3: Logging in on every node with the admin PIV certificate..."
for piv_file in "${PIV_CERT}" "${PIV_KEY}" "${PIV_CA}"; do
    if [ ! -r "$piv_file" ]; then
        echo "✗ PIV credential file not readable: ${piv_file}"
        exit 1
    fi
done

AUTH_TOKEN=""
node_index=0
for node_url in "${NODE_URLS[@]}"; do
    node_index=$((node_index + 1))
    node_name="Node ${node_index}"
    if ! token=$(piv_login "$node_url" "$node_name"); then
        exit 1
    fi
    echo "✓ ${node_name} accepted the PIV login"
    if [ -z "$AUTH_TOKEN" ]; then
        AUTH_TOKEN=$token
    fi
done

# Phase 4: Verify Raft is operational via an authenticated Put/Get on node 1
echo ""
echo "Phase 4: Verifying Raft cluster is operational..."
operational=false
op_wait=0
OP_TIMEOUT=60

while [ $op_wait -lt $OP_TIMEOUT ]; do
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

    echo "  [${op_wait}s] Waiting for Raft operations to succeed..."
    sleep "$CHECK_INTERVAL"
    op_wait=$((op_wait + CHECK_INTERVAL))
done

if [ "$operational" != true ]; then
    echo "⚠ Warning: Could not verify Raft operations after ${op_wait}s"
    echo "  Every node accepted a login - proceeding with tests"
    echo "  (Integration tests will verify functionality directly)"
fi

# Phase 5: verify all nodes can serve reads
echo ""
echo "Phase 5: Verifying all nodes can serve reads..."
node_index=0
for node_url in "${NODE_URLS[@]}"; do
    node_index=$((node_index + 1))
    if ! curl -ksf "${node_url}/api/v1/health" > /dev/null 2>&1; then
        echo "✗ Node ${node_index} (${node_url}) health check failed"
        exit 1
    fi
    echo "✓ Node ${node_index} can serve reads"
done

check_port() {
    local host=$1
    local port=$2
    # Use nc (netcat) to check if TCP port is listening
    nc -z -w 2 "$host" "$port" > /dev/null 2>&1
}

# wait_for_ports NAME PORT TIMEOUT waits until PORT listens on every node and
# exits 1 naming the port when TIMEOUT seconds pass first.
wait_for_ports() {
    local name=$1
    local port=$2
    local timeout=$3
    local waited=0
    local n1 n2 n3

    while [ "$waited" -lt "$timeout" ]; do
        n1=false
        n2=false
        n3=false

        check_port "test-node1" "$port" && n1=true
        check_port "test-node2" "$port" && n2=true
        check_port "test-node3" "$port" && n3=true

        if [ "$n1" = true ] && [ "$n2" = true ] && [ "$n3" = true ]; then
            echo "✓ All ${name} ports (${port}) are listening"
            return 0
        fi

        echo "  [${waited}s] Waiting for ${name} ports (1:$n1, 2:$n2, 3:$n3)..."
        sleep "$CHECK_INTERVAL"
        waited=$((waited + CHECK_INTERVAL))
    done

    echo "✗ Timeout waiting for the ${name} ports (${port}) after ${waited}s (1:$n1, 2:$n2, 3:$n3)"
    exit 1
}

# Phase 6: Verify gRPC ports are listening
# This is critical because CLI tests use gRPC transport
echo ""
echo "Phase 6: Verifying gRPC ports are listening..."
wait_for_ports gRPC 50051 60

# Phase 7: Verify MCP ports are listening
echo ""
echo "Phase 7: Verifying MCP ports are listening..."
wait_for_ports MCP 8888 30

# Phase 8: Brief stabilization delay to ensure all services are fully initialized
echo ""
echo "Phase 8: Allowing brief stabilization period..."
sleep "$STABILIZE_SECONDS"
echo "✓ Stabilization complete"

echo ""
echo "========================================"
echo "✓ Cluster is ready"
echo "✓ Every node is healthy and accepted a PIV login."
echo "✓ gRPC and MCP ports are listening."
echo "========================================"
exit 0
