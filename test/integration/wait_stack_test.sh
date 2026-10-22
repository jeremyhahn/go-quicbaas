#!/bin/sh
# wait_stack_test.sh runs wait_stack.sh against stub compose, docker, curl and
# sleep binaries placed first on PATH and a stub clock: each gate passes when
# every probe answers, and when one never does it exits 1 once its timeout of
# clock time passes, probing time included, naming the gate and what was not
# ready; every probe call is bounded by the time left. It also proves every
# Makefile call of wait_stack.sh stops the target on that failure, so no test
# runs against a stack that never became ready.
#
# Run by `make check-workspace-script`.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
script=$here/wait_stack.sh
makefile=$here/../../Makefile
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "wait_stack_test: $*" >&2
	exit 1
}

mkdir "$tmp/bin"

# STUB_DOWN names what never becomes ready: NODE:PORT for a port inside a
# node, a service name for a storage backend or ollama, a URL prefix for an
# HTTP probe. Every other probe succeeds.
#
# Time is a stub clock: the file $STUB_CLOCK holds the seconds since the gate
# started, NOW_CMD prints it, sleep moves it on, and each curl call or
# `compose exec` moves it on by STUB_PROBE_SECS (default 0), or by its limit
# (curl --max-time, the timeout stub's first argument) when that is smaller,
# and then fails as a timed-out call does; STUB_OVERRUN replaces curl's limit,
# for a call that runs past it. Every limit a probe call receives is
# appended to $STUB_LIMITS.

# The stub clock prints the stub time.
cat >"$tmp/bin/clock" <<'EOF'
#!/bin/sh
cat "$STUB_CLOCK"
EOF

# advance SECS LIMIT moves the stub clock on by SECS, or by LIMIT when that is
# smaller, and then exits 1, as a call its limit cut short does.
cat >"$tmp/bin/advance" <<'EOF'
#!/bin/sh
secs=$1
cut=0
if [ "$2" -lt "$secs" ]; then
	secs=$2
	cut=1
fi
echo $(($(cat "$STUB_CLOCK") + secs)) >"$STUB_CLOCK"
exit $cut
EOF

# The stub timeout records its limit and runs the command with STUB_LIMIT set.
cat >"$tmp/bin/timeout" <<'EOF'
#!/bin/sh
echo "$1" >>"$STUB_LIMITS"
STUB_LIMIT=$1
export STUB_LIMIT
shift
exec "$@"
EOF

# The stub compose answers `exec -T NODE sh -c "nc -z localhost PORT"`,
# `exec -T ollama ollama list` and `ps -q SERVICE`.
cat >"$tmp/bin/compose" <<'EOF'
#!/bin/sh
while [ $# -gt 0 ] && [ "$1" != exec ] && [ "$1" != ps ]; do
	shift
done
case $1 in
exec)
	"$(dirname "$0")/advance" "${STUB_PROBE_SECS:-0}" "${STUB_LIMIT:?compose exec ran without timeout}" || exit 124
	if [ "$3" = ollama ]; then
		target=ollama
	else
		target="$3:${6##* }"
	fi
	[ "$target" != "${STUB_DOWN:-}" ] || exit 1
	;;
ps) echo "id-$3" ;;
esac
exit 0
EOF

# The stub docker answers `inspect --format ... ID` with the health status.
cat >"$tmp/bin/docker" <<'EOF'
#!/bin/sh
for id; do :; done
if [ "$id" = "id-${STUB_DOWN:-}" ]; then
	echo starting
else
	echo healthy
fi
EOF

# The stub curl prints the -w status for its last argument, the URL: 503 for a
# URL under STUB_DOWN or while the stub clock is before STUB_UP_AT, else 200.
cat >"$tmp/bin/curl" <<'EOF'
#!/bin/sh
limit=
prev=
for url; do
	[ "$prev" != --max-time ] || limit=$url
	prev=$url
done
echo "${limit:?curl ran without --max-time}" >>"$STUB_LIMITS"
"$(dirname "$0")/advance" "${STUB_PROBE_SECS:-0}" "${STUB_OVERRUN:-$limit}" || {
	printf 000
	exit 28
}
case $url in
"${STUB_DOWN:-none}"*) printf 503 ;;
*) if [ "$(cat "$STUB_CLOCK")" -lt "${STUB_UP_AT:-0}" ]; then printf 503; else printf 200; fi ;;
esac
EOF

# The stub sleep returns at once, moves the stub clock on and counts the probe
# rounds that waited.
cat >"$tmp/bin/sleep" <<'EOF'
#!/bin/sh
echo "$1" >>"$STUB_SLEEPS"
echo $(($(cat "$STUB_CLOCK") + $1)) >"$STUB_CLOCK"
EOF
chmod +x "$tmp/bin/clock" "$tmp/bin/advance" "$tmp/bin/timeout" \
	"$tmp/bin/compose" "$tmp/bin/docker" "$tmp/bin/curl" "$tmp/bin/sleep"

# run_gate GATE [ARGS...] runs wait_stack.sh with the stubs and the stub clock
# at 0, storing its output in $tmp/out, its sleeps in $tmp/sleeps and its probe
# limits in $tmp/limits, and returns the script's status.
run_gate() {
	: >"$tmp/sleeps"
	: >"$tmp/limits"
	echo 0 >"$tmp/clock"
	PATH="$tmp/bin:$PATH" \
		COMPOSE="$tmp/bin/compose -p stub -f stub.yml" \
		DOCKER="$tmp/bin/docker" \
		NOW_CMD="$tmp/bin/clock" \
		STUB_CLOCK="$tmp/clock" \
		STUB_LIMITS="$tmp/limits" \
		STUB_SLEEPS="$tmp/sleeps" \
		bash "$script" "$@" >"$tmp/out" 2>&1
}

# expect_ready LABEL GATE [ARGS...]: every probe answers, so the gate exits 0
# without waiting.
expect_ready() {
	label=$1
	shift
	run_gate "$@" || fail "$label ready: exit $?, want 0; output: $(tail -n 3 "$tmp/out")"
	grep -q '✓ .* ready after 0s' "$tmp/out" || fail "$label ready: no ready line"
	[ ! -s "$tmp/sleeps" ] || fail "$label ready: waited before passing"
}

# expect_timeout LABEL ROUNDS MESSAGE GATE [ARGS...]: STUB_DOWN never becomes
# ready, so the gate exits 1 after ROUNDS waits with MESSAGE.
expect_timeout() {
	label=$1
	rounds=$2
	message=$3
	shift 3
	status=0
	run_gate "$@" || status=$?
	[ "$status" = 1 ] || fail "$label timeout: exit $status, want 1"
	grep -qF "$message" "$tmp/out" ||
		fail "$label timeout: want '$message'; output: $(tail -n 2 "$tmp/out")"
	got=$(wc -l <"$tmp/sleeps" | tr -d ' ')
	[ "$got" = "$rounds" ] || fail "$label timeout: waited $got rounds, want $rounds"
	if grep -q 'ready after' "$tmp/out"; then
		fail "$label timeout: a ready line was printed"
	fi
}

urls="http://test-node1:8080 http://test-node2:8080 http://test-node3:8080"

# (i) gRPC: test-node2's port 50051 never listens.
expect_ready gRPC grpc
STUB_DOWN=test-node2:50051 WAIT_TIMEOUT=4 expect_timeout gRPC 2 \
	"✗ Timeout after 4s waiting for the gRPC ports (50051) on test-node1..3: 2/3 ready, not ready: test-node2" grpc

# (ii) MCP: test-node3's port 8888 never listens.
expect_ready MCP mcp
STUB_DOWN=test-node3:8888 WAIT_TIMEOUT=4 expect_timeout MCP 2 \
	"✗ Timeout after 4s waiting for the MCP ports (8888) on test-node1..3: 2/3 ready, not ready: test-node3" mcp

# (iii) Storage: MinIO's healthcheck never reports healthy.
expect_ready storage storage
STUB_DOWN=minio WAIT_TIMEOUT=6 expect_timeout storage 3 \
	"✗ Timeout after 6s waiting for the storage backends (Floci, MinIO, Azurite): 2/3 ready, not ready: minio" storage

# (iv) Ollama: the local container never answers.
expect_ready Ollama ollama
STUB_DOWN=ollama WAIT_TIMEOUT=10 expect_timeout Ollama 2 \
	"✗ Timeout after 10s waiting for the local Ollama container" ollama

# (v) Cluster readiness: node 2 answers 503.
# shellcheck disable=SC2086 # urls is a list of URLs.
expect_ready "cluster nodes" ready "cluster nodes 1-3" 6 $urls
# shellcheck disable=SC2086
STUB_DOWN=http://test-node2 expect_timeout "cluster nodes" 2 \
	"✗ Timeout after 6s waiting for cluster nodes 1-3 to report /api/v1/health/ready: HTTP status http://test-node1:8080=200 http://test-node2:8080=503 http://test-node3:8080=200" \
	ready "cluster nodes 1-3" 6 $urls

# (vi) AddNode: no node accepts it.
# shellcheck disable=SC2086
expect_ready AddNode add-node 6 $urls
grep -q 'AddNode accepted by http://test-node1:8080' "$tmp/out" || fail "AddNode ready: the accepting node is not named"
# shellcheck disable=SC2086
STUB_DOWN=http://test-node expect_timeout AddNode 2 \
	"✗ Timeout after 6s waiting for a node to accept AddNode for node4: HTTP status http://test-node1:8080=503" \
	add-node 6 $urls

# (vii) node4 readiness.
STUB_DOWN=http://test-node4 expect_timeout node4 3 \
	"✗ Timeout after 9s waiting for node4 to report /api/v1/health/ready: HTTP status http://test-node4:8080=503" \
	ready node4 9 http://test-node4:8080

# expect_clock LABEL CLOCK LIMITS: the gate just run ended at stub time CLOCK,
# and its probe calls received LIMITS, in order, each no more than the time
# left before the deadline.
expect_clock() {
	got=$(cat "$tmp/clock")
	[ "$got" = "$2" ] || fail "$1: ended at ${got}s, want ${2}s"
	got=$(tr '\n' ' ' <"$tmp/limits")
	[ "$got" = "$3 " ] || fail "$1: probe limits '$got', want '$3 '"
}

# (viii) Probes slower than STEP still time out at TIMEOUT, not TIMEOUT times the
# probes: node 1 answers after 5s, node 2's call gets the 1s left and is cut,
# and node 3 is not called once no time is left.
# shellcheck disable=SC2086
STUB_DOWN=http://test-node2 STUB_PROBE_SECS=5 expect_timeout "slow nodes" 0 \
	"✗ Timeout after 6s waiting for cluster nodes 1-3 to report /api/v1/health/ready: HTTP status http://test-node1:8080=200 http://test-node2:8080=000 http://test-node3:8080=000" \
	ready "cluster nodes 1-3" 6 $urls
expect_clock "slow nodes" 6 "5 1"
STUB_PROBE_SECS=3 WAIT_TIMEOUT=4 expect_timeout "slow gRPC" 0 \
	"✗ Timeout after 4s waiting for the gRPC ports (50051) on test-node1..3: 1/3 ready, not ready: test-node2 test-node3" grpc
expect_clock "slow gRPC" 4 "4 1"

# (ix) A probe that would succeed only after the deadline is a timeout: node4
# answers 200 from 7s, the second probe gets the 1s left and is cut at 6s.
STUB_UP_AT=7 STUB_PROBE_SECS=2 expect_timeout "late node4" 1 \
	"✗ Timeout after 6s waiting for node4 to report /api/v1/health/ready: HTTP status http://test-node4:8080=000" \
	ready node4 6 http://test-node4:8080
grep -qF '[2s] waiting for node4' "$tmp/out" || fail "late node4: the wait line does not print the elapsed 2s"
expect_clock "late node4" 6 "5 1"
# A call that overruns its limit and answers 200 at 8s is still a timeout.
STUB_OVERRUN=99 STUB_PROBE_SECS=8 expect_timeout "overrun node4" 0 \
	"✗ Timeout after 6s waiting for node4 to report /api/v1/health/ready: HTTP status http://test-node4:8080=200" \
	ready node4 6 http://test-node4:8080

# (x) Probes that succeed in time print ready with the real elapsed time.
status=0
# shellcheck disable=SC2086
STUB_PROBE_SECS=1 run_gate ready "cluster nodes 1-3" 6 $urls || status=$?
[ "$status" = 0 ] || fail "slow ready: exit $status, want 0; output: $(tail -n 2 "$tmp/out")"
grep -qF '✓ cluster nodes 1-3 to report /api/v1/health/ready ready after 3s' "$tmp/out" ||
	fail "slow ready: no 'ready after 3s' line; output: $(tail -n 2 "$tmp/out")"
expect_clock "slow ready" 3 "5 5 4"

# (xi) An unknown gate or a missing argument is a usage error.
status=0
run_gate bogus || status=$?
[ "$status" = 2 ] || fail "unknown gate: exit $status, want 2"
status=0
run_gate ready node4 || status=$?
[ "$status" = 2 ] || fail "ready without URLs: exit $status, want 2"

# (xii) Every Makefile call stops its target when the gate times out: each is
# followed by `|| exit 1` or is the condition of an `if !` that exits 1.
calls=$(grep -c 'wait_stack\.sh [a-z]' "$makefile")
[ "$calls" -ge 7 ] || fail "Makefile: $calls wait_stack.sh calls, want at least 7"
unguarded=$(grep 'wait_stack\.sh [a-z]' "$makefile" |
	grep -v -e '|| exit 1; \\$' -e 'if ! bash test/integration/wait_stack\.sh .*; then \\$' || true)
[ -z "$unguarded" ] || fail "Makefile: a wait_stack.sh call does not stop the target: $unguarded"
grep -A 2 'if ! bash test/integration/wait_stack\.sh' "$makefile" | grep -q 'exit 1' ||
	fail "Makefile: the node4 wait does not exit 1 on timeout"

echo "wait_stack_test: ok"
