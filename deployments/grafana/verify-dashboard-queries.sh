#!/bin/bash
#
# Verify Grafana Dashboard Queries
#
# This script tests that the Prometheus queries in the Grafana dashboard
# would return data when the server is running under load.
#
# Usage:
#   ./verify-dashboard-queries.sh [prometheus-url]
#
# Default prometheus-url: http://localhost:9090

set -e

PROMETHEUS_URL="${1:-http://localhost:9090}"
QUERIES_FILE="$(mktemp)"
FAILED=0
PASSED=0

echo "Testing Grafana dashboard queries against Prometheus at: $PROMETHEUS_URL"
echo ""

# Function to test a Prometheus query
test_query() {
    local description="$1"
    local query="$2"

    echo -n "Testing: $description ... "

    # URL encode the query
    encoded_query=$(echo -n "$query" | jq -sRr @uri)

    # Query Prometheus
    response=$(curl -s "${PROMETHEUS_URL}/api/v1/query?query=${encoded_query}")

    # Check if query succeeded
    status=$(echo "$response" | jq -r '.status')

    if [ "$status" != "success" ]; then
        echo "FAILED (query error)"
        echo "  Query: $query"
        echo "  Response: $response"
        FAILED=$((FAILED + 1))
        return 1
    fi

    # Check if data was returned
    result_type=$(echo "$response" | jq -r '.data.resultType')
    result_count=$(echo "$response" | jq -r '.data.result | length')

    if [ "$result_count" -eq 0 ]; then
        echo "WARNING (no data)"
        echo "  Query: $query"
        echo "  This is expected if no load test is running"
        PASSED=$((PASSED + 1))
        return 0
    fi

    echo "PASSED ($result_count series)"
    PASSED=$((PASSED + 1))
    return 0
}

# Test throughput queries
echo "=== Throughput Queries ==="
test_query "Write Ops/Sec" \
    'sum(rate(quicbaas_http_requests_total{method="PUT",path=~"/v1/entities/kv/.*",status=~"2.."}[1m]))'

test_query "Read Ops/Sec" \
    'sum(rate(quicbaas_http_requests_total{method="GET",path=~"/v1/entities/kv/.*",status=~"2.."}[1m]))'

test_query "Total Ops/Sec" \
    'sum(rate(quicbaas_http_requests_total{path=~"/v1/entities/kv/.*",path!~"/v1/health.*|/v1/metrics.*",status=~"2.."}[1m]))'

echo ""

# Test latency queries
echo "=== Latency Percentile Queries ==="
test_query "Read p50" \
    'histogram_quantile(0.50, sum(rate(quicbaas_http_request_duration_seconds_bucket{method="GET",path=~"/v1/entities/kv/.*"}[5m])) by (le))'

test_query "Read p95" \
    'histogram_quantile(0.95, sum(rate(quicbaas_http_request_duration_seconds_bucket{method="GET",path=~"/v1/entities/kv/.*"}[5m])) by (le))'

test_query "Read p99" \
    'histogram_quantile(0.99, sum(rate(quicbaas_http_request_duration_seconds_bucket{method="GET",path=~"/v1/entities/kv/.*"}[5m])) by (le))'

test_query "Write p50" \
    'histogram_quantile(0.50, sum(rate(quicbaas_http_request_duration_seconds_bucket{method="PUT",path=~"/v1/entities/kv/.*"}[5m])) by (le))'

test_query "Write p95" \
    'histogram_quantile(0.95, sum(rate(quicbaas_http_request_duration_seconds_bucket{method="PUT",path=~"/v1/entities/kv/.*"}[5m])) by (le))'

test_query "Write p99" \
    'histogram_quantile(0.99, sum(rate(quicbaas_http_request_duration_seconds_bucket{method="PUT",path=~"/v1/entities/kv/.*"}[5m])) by (le))'

echo ""

# Test storage metrics (should always have data)
echo "=== Storage Metrics ==="
test_query "Cache Hit Rate" \
    'quicbaas_storage_cache_hit_rate_percent'

test_query "Bloom Filter Effectiveness" \
    '100 * rate(quicbaas_storage_bloom_filter_hits_total[5m]) / (rate(quicbaas_storage_bloom_filter_hits_total[5m]) + rate(quicbaas_storage_bloom_filter_misses_total[5m]))'

test_query "L0 Files" \
    'quicbaas_storage_l0_files'

test_query "Disk Usage" \
    'quicbaas_storage_disk_usage_bytes'

echo ""

# Test pool metrics (should always have data if pool is configured)
echo "=== Connection Pool Metrics ==="
test_query "Pool Size" \
    'quicbaas_pool_size'

test_query "Active Connections" \
    'quicbaas_pool_active_connections'

test_query "Idle Connections" \
    'quicbaas_pool_idle_connections'

test_query "Connection Reuse Rate" \
    '100 * rate(quicbaas_pool_connection_reuse_total[5m]) / rate(quicbaas_pool_total_requests_total[5m])'

echo ""
echo "=== Summary ==="
echo "Passed: $PASSED"
echo "Failed: $FAILED"

if [ $FAILED -gt 0 ]; then
    echo ""
    echo "Some queries failed. Please check the dashboard configuration."
    exit 1
fi

echo ""
echo "All queries are valid!"
echo ""
echo "Note: Queries showing 'WARNING (no data)' are expected when:"
echo "  - No load test is currently running"
echo "  - The server was just started (metrics need time to accumulate)"
echo ""
echo "To generate test data, run: ./scripts/load-test.sh"

exit 0
