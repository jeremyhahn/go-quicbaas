#!/bin/bash
# wait_stack.sh GATE [ARGS...] waits until one readiness gate of an integration
# stack passes, probing every STEP seconds, and exits 1 naming the gate when its
# timeout passes first. The Makefile's integration-test and
# integration-test-cluster targets call it after `compose up` and stop on a
# nonzero exit, so no test runs against a stack that never became ready.
#
#   grpc                 the gRPC port (50051) listens in test-node1..3
#   mcp                  the MCP port (8888) listens in test-node1..3
#   storage              the Floci, MinIO and Azurite healthchecks report healthy
#   ollama               the local Ollama container answers `ollama list`
#   ready NAME TIMEOUT URL...
#                        every URL's /api/v1/health/ready answers 200
#   add-node TIMEOUT URL...
#                        one URL accepts AddNode for node4
#
# COMPOSE is the compose command with its project and file flags, e.g.
# "docker compose -p quicbaas-integration -f docker-compose.integration.yml".
# DOCKER is the docker binary. WAIT_TIMEOUT overrides the timeout, in seconds,
# of the grpc, mcp, storage and ollama gates.
#
# Exercised without Docker by wait_stack_test.sh (`make check-workspace-script`).

set -u

COMPOSE=${COMPOSE:-}
DOCKER=${DOCKER:-docker}
NODES="test-node1 test-node2 test-node3"

# NOW_CMD prints the current time in whole seconds; wait_stack_test.sh replaces
# it with a stub clock.
NOW_CMD=${NOW_CMD:-date +%s}

# PROBE_CEILING is the most seconds one probe call may take.
PROBE_CEILING=5

# PROGRESS is set by each probe to what it saw, for the wait and timeout lines.
PROGRESS=

# DEADLINE is the NOW_CMD reading at which the running wait_for gives up; every
# probe call is bounded by the time left before it.
DEADLINE=

now() {
    # shellcheck disable=SC2086 # NOW_CMD is a command with its arguments.
    $NOW_CMD
}

# probe_limit prints the seconds the next probe call may take: PROBE_CEILING,
# or the time left before DEADLINE when that is less. It fails when no time is
# left, so the caller skips the call: curl and timeout read a limit of 0 as no
# limit at all.
probe_limit() {
    local left=$((DEADLINE - $(now)))

    [ "$left" -gt 0 ] || return 1
    [ "$left" -lt "$PROBE_CEILING" ] || left=$PROBE_CEILING
    echo "$left"
}

# wait_for NAME TIMEOUT STEP PROBE [ARGS...] runs PROBE every STEP seconds until
# it succeeds, and exits 1 naming NAME once TIMEOUT seconds of wall-clock time
# pass first. The time spent probing counts against TIMEOUT, a probe that
# succeeds only after the deadline is a timeout, and no sleep runs past it.
wait_for() {
    local name=$1
    local timeout=$2
    local step=$3
    local start elapsed nap
    shift 3

    start=$(now)
    DEADLINE=$((start + timeout))
    elapsed=0
    while [ "$elapsed" -lt "$timeout" ]; do
        if "$@"; then
            elapsed=$(($(now) - start))
            if [ "$elapsed" -lt "$timeout" ]; then
                echo "  ✓ ${name} ready after ${elapsed}s"
                return 0
            fi
            break
        fi
        elapsed=$(($(now) - start))
        [ "$elapsed" -lt "$timeout" ] || break
        echo "  [${elapsed}s] waiting for ${name}: ${PROGRESS}"
        nap=$((timeout - elapsed))
        [ "$nap" -lt "$step" ] || nap=$step
        sleep "$nap"
        elapsed=$(($(now) - start))
    done

    echo "  ✗ Timeout after ${timeout}s waiting for ${name}: ${PROGRESS}"
    exit 1
}

# ports_ready PORT succeeds when PORT listens inside every integration node.
ports_ready() {
    local port=$1
    local ready=0
    local down=
    local node limit

    for node in $NODES; do
        # shellcheck disable=SC2086 # COMPOSE is a command with its flags.
        if limit=$(probe_limit) && timeout "$limit" $COMPOSE exec -T "$node" sh -c "nc -z localhost ${port}" >/dev/null 2>&1; then
            ready=$((ready + 1))
        else
            down="${down} ${node}"
        fi
    done
    PROGRESS="${ready}/3 ready, not ready:${down}"
    [ "$ready" -eq 3 ]
}

# storage_ready succeeds when every object-storage backend's Compose
# healthcheck reports healthy. The Floci and MinIO images have no curl, so the
# health is read from the engine, not probed inside the container.
storage_ready() {
    local ready=0
    local down=
    local svc id limit

    for svc in floci minio azurite; do
        id=
        if limit=$(probe_limit); then
            # shellcheck disable=SC2086 # COMPOSE is a command with its flags.
            id=$(timeout "$limit" $COMPOSE ps -q "$svc" 2>/dev/null) || id=
        fi
        if [ -n "$id" ] && limit=$(probe_limit) &&
            [ "$(timeout "$limit" "$DOCKER" inspect --format '{{.State.Health.Status}}' "$id" 2>/dev/null)" = healthy ]; then
            ready=$((ready + 1))
        else
            down="${down} ${svc}"
        fi
    done
    PROGRESS="${ready}/3 ready, not ready:${down}"
    [ "$ready" -eq 3 ]
}

# ollama_ready succeeds when the local Ollama container answers.
ollama_ready() {
    local limit

    PROGRESS="not answering"
    limit=$(probe_limit) || return 1
    # shellcheck disable=SC2086 # COMPOSE is a command with its flags.
    timeout "$limit" $COMPOSE exec -T ollama ollama list >/dev/null 2>&1
}

# http_status METHOD URL [CURL ARGS...] prints URL's HTTP status, 000 when no
# answer arrives in the time probe_limit allows.
http_status() {
    local method=$1
    local url=$2
    local code limit
    shift 2

    if ! limit=$(probe_limit); then
        echo 000
        return 0
    fi
    code=$(curl -s -o /dev/null -w '%{http_code}' --max-time "$limit" -X "$method" "$@" "$url" 2>/dev/null) || code=000
    echo "${code:-000}"
}

# nodes_ready URL... succeeds when every URL's readiness endpoint answers 200.
nodes_ready() {
    local url code
    local ready=0

    PROGRESS="HTTP status"
    for url in "$@"; do
        code=$(http_status GET "${url}/api/v1/health/ready")
        PROGRESS="${PROGRESS} ${url}=${code}"
        if [ "$code" = 200 ]; then
            ready=$((ready + 1))
        fi
    done
    [ "$ready" -eq $# ]
}

# node4_added URL... succeeds when one URL accepts AddNode for node4; only the
# Raft leader does, so every node is tried.
node4_added() {
    local url code

    PROGRESS="HTTP status"
    for url in "$@"; do
        code=$(http_status POST "${url}/api/v1/cluster/nodes" \
            -H 'Content-Type: application/json' \
            ${WAIT_STACK_BEARER:+-H "Authorization: Bearer ${WAIT_STACK_BEARER}"} \
            -d '{"shard_id":1,"node_id":4,"address":"quicbaas-node4:63004"}')
        PROGRESS="${PROGRESS} ${url}=${code}"
        if [ "$code" = 200 ]; then
            echo "  AddNode accepted by ${url}"
            return 0
        fi
    done
    return 1
}

usage() {
    echo "usage: wait_stack.sh grpc|mcp|storage|ollama|ready NAME TIMEOUT URL...|add-node TIMEOUT URL..." >&2
    exit 2
}

gate=${1:-}
[ $# -gt 0 ] && shift

case $gate in
grpc) wait_for "the gRPC ports (50051) on test-node1..3" "${WAIT_TIMEOUT:-30}" 2 ports_ready 50051 ;;
mcp) wait_for "the MCP ports (8888) on test-node1..3" "${WAIT_TIMEOUT:-15}" 2 ports_ready 8888 ;;
storage) wait_for "the storage backends (Floci, MinIO, Azurite)" "${WAIT_TIMEOUT:-30}" 2 storage_ready ;;
ollama) wait_for "the local Ollama container" "${WAIT_TIMEOUT:-120}" 5 ollama_ready ;;
ready)
    [ $# -ge 3 ] || usage
    name=$1
    timeout=$2
    shift 2
    wait_for "${name} to report /api/v1/health/ready" "$timeout" 3 nodes_ready "$@"
    ;;
add-node)
    [ $# -ge 2 ] || usage
    timeout=$1
    shift
    wait_for "a node to accept AddNode for node4" "$timeout" 3 node4_added "$@"
    ;;
*) usage ;;
esac
