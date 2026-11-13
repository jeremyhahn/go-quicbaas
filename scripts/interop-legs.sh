#!/usr/bin/env bash
# interop-legs.sh - the external-implementation interop legs (go-quicbaas#203).
#
# Each leg drives our server with a client implementation that is not ours.
# A leg runs its baseline test alone first, so a broken baseline is reported
# as such and not as a vacuous pass, and then every test of the leg. A
# failure is a finding, not a flake. The claims each leg enforces are listed
# in docs/multiprotocol-api/engineering.md, "Interop claims audit".
#
#   interop-legs.sh list         one line per leg: leg pkg baseline tests
#   interop-legs.sh run <leg>    baseline, then the leg's tests
#
# INTEROP_TAGS is the build tag list (`make interop-test` passes the unit
# suite's TEST_TAGS); GO is the go binary (default: go).
# test/buildgates/interop_legs_test.go checks that every leg and pattern
# selects real tests.
set -euo pipefail

# leg|package|baseline -run pattern|leg -run pattern
LEGS=(
	# golang.org/x/oauth2, lestrrat-go/jwx and go-jose against the
	# OIDC/OAuth2 endpoints of the REST router.
	'oidc|./pkg/api/rest|^TestOAuth2Interop_Discovery$|^(TestOIDCInterop|TestOAuth2Interop)_'
	# quic-go's http3.Transport against the REST router over HTTP/3.
	'http3|./pkg/api/rest|^TestHTTP3Interop_DiscoveryIsServedOverHTTP3$|^TestHTTP3Interop_'
	# connectrpc.com/connect in gRPC mode (no grpc-go code) against the gRPC
	# facade and its interceptor chain.
	'grpc|./pkg/api/grpc|^TestGRPCInterop_BaselineHealth$|^TestGRPCInterop_'
	# The official MCP Go SDK against the module tool surface over
	# streamable HTTP.
	'mcp|./pkg/api/mcp|^TestOfficialClient_Initialize$|^TestOfficialClient_'
)

GO=${GO:-go}
INTEROP_TAGS=${INTEROP_TAGS:-}

usage() {
	echo "usage: $0 list | run <leg>" >&2
	exit 2
}

# run_step <leg> <step> <pkg> <pattern>: one go test pass; fails when a test
# fails or when the pattern ran no passing test.
run_step() {
	local leg=$1 step=$2 pkg=$3 pattern=$4 log rc=0
	log=$(mktemp)
	"$GO" test -count=1 -timeout=20m -tags="$INTEROP_TAGS" -run "$pattern" -v "$pkg" >"$log" 2>&1 || rc=$?
	while IFS= read -r line; do
		case $line in
		'--- FAIL'* | FAIL*) echo "INTEROP-FINDING $leg $step $line" ;;
		esac
	done <"$log"
	if [ "$rc" -eq 0 ]; then
		local passed=0
		while IFS= read -r line; do
			case $line in '--- PASS'*) passed=1 ;; esac
		done <"$log"
		if [ "$passed" -eq 0 ]; then
			echo "INTEROP-FINDING $leg $step: $pattern ran no passing test in $pkg"
			rc=1
		fi
	fi
	if [ "$rc" -ne 0 ]; then
		echo "--- go test output ($leg $step) ---"
		tail -n 60 "$log"
	fi
	rm -f "$log"
	return "$rc"
}

case ${1:-} in
list)
	[ $# -eq 1 ] || usage
	for entry in "${LEGS[@]}"; do
		IFS='|' read -r leg pkg baseline tests <<<"$entry"
		printf '%s %s %s %s\n' "$leg" "$pkg" "$baseline" "$tests"
	done
	;;
run)
	[ $# -eq 2 ] || usage
	for entry in "${LEGS[@]}"; do
		IFS='|' read -r leg pkg baseline tests <<<"$entry"
		[ "$leg" = "$2" ] || continue
		echo "interop $leg: baseline $baseline"
		run_step "$leg" baseline "$pkg" "$baseline"
		echo "interop $leg: tests $tests"
		run_step "$leg" tests "$pkg" "$tests"
		echo "interop $leg: ok"
		exit 0
	done
	echo "interop: unknown leg $2" >&2
	exit 2
	;;
*) usage ;;
esac
