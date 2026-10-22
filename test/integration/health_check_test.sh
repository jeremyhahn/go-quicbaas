#!/bin/sh
# health_check_test.sh runs health_check.sh against stub curl, nc and sleep
# binaries placed first on PATH: the script reports the cluster ready only when
# every node accepts the PIV login, and a 401, an empty access_token, a login
# that never answers or a login transfer cut mid-response on any node exits 1
# naming that node, without printing a token. Every other fatal gate (liveness,
# Raft readiness, the PIV credential files, read health, the gRPC and MCP
# ports) exits 1 naming the gate and the node, before the next phase starts.
#
# Run by `make check-workspace-script`.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
script=$here/health_check.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "health_check_test: $*" >&2
	exit 1
}

mkdir "$tmp/bin"

# The stub curl answers by the URL, its last argument. A login on the host in
# STUB_LOGIN_401_HOST returns 401; STUB_EMPTY_TOKEN=1 makes every login return
# an empty access_token. -w appends the status line, -f fails a non-200.
cat >"$tmp/bin/curl" <<'EOF'
#!/bin/sh
url=
write=
failflag=
maxtime=
prev=
for arg in "$@"; do
	[ "$prev" != --max-time ] || maxtime=$arg
	prev=$arg
	case $arg in
	-w) write=1 ;;
	--*) ;;
	-*f*) failflag=1 ;;
	esac
	url=$arg
done
code=200
body='{}'
host=${url#*://}
host=${host%%:*}
case $url in
*/api/v1/health/live)
	[ "$host" != "${STUB_LIVE_DOWN_HOST:-}" ] || code=503
	;;
*/api/v1/health/ready)
	body='{"status":"healthy"}'
	[ "$host" != "${STUB_NOT_READY_HOST:-}" ] || body='{"status":"starting"}'
	;;
*/api/v1/health)
	[ "$host" != "${STUB_READ_DOWN_HOST:-}" ] || code=503
	;;
*/api/v1/auth/login/piv)
	if [ "$host" = "${STUB_LOGIN_STALL_HOST:-}" ]; then
		# A stalled node: a bounded request ends as curl's timeout (28);
		# an unbounded one would hang, so it fails at once instead.
		[ "$maxtime" = "${QUICBAAS_HEALTH_TIMEOUT:-}" ] || exit 99
		exit 28
	elif [ "$host" = "${STUB_LOGIN_PARTIAL_HOST:-}" ]; then
		# A transfer cut mid-response: status 200 and a complete
		# access_token are printed, then curl exits 18.
		printf '{"access_token":"stub-token-%s","token_type":"Bearer"}' "$host"
		[ -z "$write" ] || printf '\n200'
		exit 18
	elif [ "$host" = "${STUB_LOGIN_401_HOST:-}" ]; then
		code=401
		body='{"error":"unauthorized"}'
	elif [ "${STUB_EMPTY_TOKEN:-}" = 1 ]; then
		body='{"access_token":"","token_type":"Bearer"}'
	else
		body="{\"access_token\":\"stub-token-$host\",\"token_type\":\"Bearer\"}"
	fi
	;;
esac
printf '%s' "$body"
[ -z "$write" ] || printf '\n%s' "$code"
[ -z "$failflag" ] || [ "$code" = 200 ] || exit 22
exit 0
EOF

# The stub nc (`nc -z -w 2 HOST PORT`) fails for the HOST:PORT in STUB_NC_DOWN.
cat >"$tmp/bin/nc" <<'EOF'
#!/bin/sh
[ "$4:$5" != "${STUB_NC_DOWN:-}" ]
EOF

# The stub sleep returns at once, so a gate that never passes reaches its
# timeout in counted probe rounds instead of wall-clock seconds.
printf '#!/bin/sh\nexit 0\n' >"$tmp/bin/sleep"
chmod +x "$tmp/bin/curl" "$tmp/bin/nc" "$tmp/bin/sleep"

# The PIV credential files must exist; the stub curl never reads them.
for f in admin.crt admin.key ca.crt; do
	: >"$tmp/$f"
done

# run_check runs health_check.sh with the stubs and stores its output in
# $tmp/out, returning the script's exit status.
run_check() {
	PATH="$tmp/bin:$PATH" \
		QUICBAAS_NODE1_URL=https://test-node1:8080 \
		QUICBAAS_NODE2_URL=https://test-node2:8080 \
		QUICBAAS_NODE3_URL=https://test-node3:8080 \
		QUICBAAS_TEST_PIV_CERT="$tmp/admin.crt" \
		QUICBAAS_TEST_PIV_KEY="${STUB_PIV_KEY:-$tmp/admin.key}" \
		QUICBAAS_TEST_PIV_CA="$tmp/ca.crt" \
		QUICBAAS_HEALTH_TIMEOUT=2 \
		QUICBAAS_CHECK_INTERVAL=1 \
		QUICBAAS_STABILIZE_SECONDS=0 \
		bash "$script" >"$tmp/out" 2>&1
}

no_token_printed() {
	if grep -q 'stub-token' "$tmp/out"; then
		fail "$1: the output contains an access token"
	fi
}

# (i) Every node is healthy and returns a token: the cluster is ready.
run_check || fail "all nodes healthy: exit $?, want 0; output: $(tail -n 5 "$tmp/out")"
grep -q 'Cluster is ready' "$tmp/out" || fail "all nodes healthy: 'Cluster is ready' not printed"
no_token_printed "all nodes healthy"

# (ii) One node's login returns 401: exit 1, naming that node and the status.
status=0
STUB_LOGIN_401_HOST=test-node2 run_check || status=$?
[ "$status" = 1 ] || fail "node 2 login 401: exit $status, want 1"
grep -q 'Node 2 (https://test-node2:8080): HTTP status 401' "$tmp/out" ||
	fail "node 2 login 401: the output does not name node 2 and status 401"
if grep -q 'Cluster is ready' "$tmp/out"; then
	fail "node 2 login 401: 'Cluster is ready' printed"
fi
no_token_printed "node 2 login 401"

# (iii) Health is OK but the login returns an empty access_token: exit 1.
status=0
STUB_EMPTY_TOKEN=1 run_check || status=$?
[ "$status" = 1 ] || fail "empty access_token: exit $status, want 1"
grep -q 'empty access_token' "$tmp/out" || fail "empty access_token: the failure is not reported"
if grep -q 'Cluster is ready' "$tmp/out"; then
	fail "empty access_token: 'Cluster is ready' printed"
fi

# (iv) One node accepts the login and never answers: the request is bounded by
# the health timeout, and the check exits 1 naming that node.
status=0
STUB_LOGIN_STALL_HOST=test-node3 run_check || status=$?
[ "$status" = 1 ] || fail "node 3 login stalls: exit $status, want 1"
grep -q 'Node 3 (https://test-node3:8080): no answer within 2s' "$tmp/out" ||
	fail "node 3 login stalls: the output does not name node 3 and the timeout"
if grep -q 'Cluster is ready' "$tmp/out"; then
	fail "node 3 login stalls: 'Cluster is ready' printed"
fi

# (v) One node's login transfer is cut mid-response (curl exit 18) after a 200
# status and a complete access_token: exit 1, naming that node and the exit code.
status=0
STUB_LOGIN_PARTIAL_HOST=test-node1 run_check || status=$?
[ "$status" = 1 ] || fail "node 1 login cut mid-response: exit $status, want 1"
grep -q 'Node 1 (https://test-node1:8080): curl exit code 18' "$tmp/out" ||
	fail "node 1 login cut mid-response: the output does not name node 1 and curl exit code 18"
if grep -q 'Cluster is ready' "$tmp/out"; then
	fail "node 1 login cut mid-response: 'Cluster is ready' printed"
fi
no_token_printed "node 1 login cut mid-response"

# expect_gate_fails LABEL MESSAGE LATER runs the check, which must exit 1
# printing MESSAGE, which names the gate and the node, without reaching LATER,
# the first line of the next phase, or the ready message.
expect_gate_fails() {
	status=0
	run_check || status=$?
	[ "$status" = 1 ] || fail "$1: exit $status, want 1"
	grep -qF "$2" "$tmp/out" || fail "$1: want '$2'; output: $(tail -n 2 "$tmp/out")"
	if grep -qF "$3" "$tmp/out"; then
		fail "$1: '$3' was reached"
	fi
	if grep -q 'Cluster is ready' "$tmp/out"; then
		fail "$1: 'Cluster is ready' printed"
	fi
}

# (vi) Node 1 never answers its liveness probe.
STUB_LIVE_DOWN_HOST=test-node1 expect_gate_fails "node 1 not live" \
	"✗ Timeout waiting for nodes to be alive after 2s (1:false, 2:true, 3:true)" "Phase 2:"

# (vii) Node 2 never reports Raft readiness.
STUB_NOT_READY_HOST=test-node2 expect_gate_fails "node 2 Raft not ready" \
	"✗ Timeout waiting for Raft readiness after 120s (1:true, 2:false, 3:true)" "Phase 3:"

# (viii) The PIV key is missing: no login is attempted.
STUB_PIV_KEY="$tmp/missing.key" expect_gate_fails "PIV key missing" \
	"✗ PIV credential file not readable: $tmp/missing.key" "accepted the PIV login"

# (ix) Node 3 cannot serve reads.
STUB_READ_DOWN_HOST=test-node3 expect_gate_fails "node 3 read health" \
	"✗ Node 3 (https://test-node3:8080) health check failed" "Phase 6:"

# (x) Node 2's gRPC port never listens.
STUB_NC_DOWN=test-node2:50051 expect_gate_fails "node 2 gRPC port" \
	"✗ Timeout waiting for the gRPC ports (50051) after 60s (1:true, 2:false, 3:true)" "Phase 7:"

# (xi) Node 1's MCP port never listens.
STUB_NC_DOWN=test-node1:8888 expect_gate_fails "node 1 MCP port" \
	"✗ Timeout waiting for the MCP ports (8888) after 30s (1:false, 2:true, 3:true)" "Phase 8:"

echo "health_check_test: ok"
