#!/bin/sh
# run_packages.sh runs integration packages one at a time, each under its own time limit.
set -u
set -f

usage() {
	echo "usage: run_packages.sh LOG TAGS FLAGS PKG..." >&2
	echo "       run_packages.sh --filter LOG TAGS FILTER" >&2
	exit 2
}

# FILTER_DEFAULT_ROOT is the package pattern a filter without packages runs.
FILTER_DEFAULT_ROOT=${FILTER_DEFAULT_ROOT:-./test/integration/...}

# flag_takes_value reports whether go test flag $1, given without =value,
# takes the next token as its value.
flag_takes_value() {
	name=${1#-}
	name=${name#-}
	case $name in
	run | skip | count | timeout | tags | bench | benchtime | cpu | parallel | p | \
		shuffle | list | fuzz | fuzztime | fuzzminimizetime | vet | exec | o | \
		outputdir | coverprofile | covermode | coverpkg | cpuprofile | memprofile | \
		memprofilerate | blockprofile | blockprofilerate | mutexprofile | \
		mutexprofilefraction | trace | mod | modfile | ldflags | gcflags | asmflags | \
		toolexec | overlay | pkgdir | C)
		return 0
		;;
	esac
	return 1
}

if [ "${1:-}" = --filter ]; then
	[ $# -eq 4 ] || usage
	log=$2
	tags=$3
	flags=
	pkgs=
	value_next=0
	# shellcheck disable=SC2086 # FILTER is split into tokens on spaces.
	for token in $4; do
		if [ $value_next = 1 ]; then
			flags="$flags $token"
			value_next=0
			continue
		fi
		case $token in
		-*=*) flags="$flags $token" ;;
		-*)
			flags="$flags $token"
			if flag_takes_value "$token"; then
				value_next=1
			fi
			;;
		*) pkgs="$pkgs $token" ;;
		esac
	done
	# shellcheck disable=SC2086 # each package is its own argument.
	set -- ${pkgs:-$FILTER_DEFAULT_ROOT}
else
	[ $# -ge 4 ] || usage
	log=$1
	tags=$2
	flags=$3
	shift 3
fi

PACKAGE_TIMEOUT=${PACKAGE_TIMEOUT:-10m}
PACKAGE_KILL_TIMEOUT=${PACKAGE_KILL_TIMEOUT:-11m}
# IAM_PACKAGE_TIMEOUT is the iam package's own limit; packages without their own limit
# below keep PACKAGE_TIMEOUT. The iam package spends its time creating and deleting
# tenants: run 45348624843 measured about 19.5 s per tenant lifecycle (create
# plus delete) and timed the package out at 10m. After the shared-tenant
# fixture (test/integration/iam/shared_tenant_test.go) about 95 lifecycles
# remain (87 fresh tenants plus 8 shared ones), so
#   ceil(1.5 x ((95 x 19.5 s) + 60 s)) = ceil(1.5 x 1912.5 s) = 2869 s (~48m),
# which is capped at 30m. The kill limit stays one minute above it.
IAM_PACKAGE_TIMEOUT=${IAM_PACKAGE_TIMEOUT:-30m}
IAM_PACKAGE_KILL_TIMEOUT=${IAM_PACKAGE_KILL_TIMEOUT:-31m}
# CLI_PACKAGE_TIMEOUT is the cli package's own limit. Its length is legitimate,
# not a hang: run 45364801188 ran 64 of its 615 top-level tests in 523 s
# (TestAgentAccount* 296 s, the barrier per-test daemons 171 s) before the 10m
# limit. The 551 tests still to run all use the shared docker cluster (only
# barrier_test.go spawns per-test daemons), at about 2 s each:
#   523 s + 15 s (TestBarrierAuthPosture_DefaultDeny) + 551 x 2 s = 1640 s,
#   ceil(1.5 x 1640 s) = 2460 s (41m). A loaded runner used all of a 30m cap
#   (TestAgentAccount* 87-164 s each), so the package keeps the measured 41m.
# The kill limit stays one minute above it.
CLI_PACKAGE_TIMEOUT=${CLI_PACKAGE_TIMEOUT:-41m}
CLI_PACKAGE_KILL_TIMEOUT=${CLI_PACKAGE_KILL_TIMEOUT:-42m}
# CLUSTER_PACKAGE_TIMEOUT is the cluster package's own limit. Its length is
# legitimate, not a hang: on main e2a3ed26 the 34 top-level tests before
# TestDataPersistence passed in 588 s (TestClusterConsistency 202 s,
# TestClusterManagement_Rebalancing 155-182 s, TestClusterShardOperations
# 82-102 s) before the 10m limit. The unrun leader-election, multiprotocol
# and four rebalancing tests add about 930 s:
#   ceil(1.5 x (590 s + 930 s)) = 2280 s (38m), capped at 30m like iam.
# The kill limit stays one minute above it.
CLUSTER_PACKAGE_TIMEOUT=${CLUSTER_PACKAGE_TIMEOUT:-30m}
CLUSTER_PACKAGE_KILL_TIMEOUT=${CLUSTER_PACKAGE_KILL_TIMEOUT:-31m}
# HEALTH_PROBE_TIMEOUT bounds the cluster probe after a failed package: a node
# that accepts connections but never answers would otherwise hang the runner.
HEALTH_PROBE_TIMEOUT=${HEALTH_PROBE_TIMEOUT:-120s}

summary=$(mktemp)
status_file=$(mktemp)
trap 'rm -f "$summary" "$status_file"' EXIT

overall=0
remaining=$#
for pkg in "$@"; do
	remaining=$((remaining - 1))
	start=$(date +%s)
	pkg_timeout=$PACKAGE_TIMEOUT
	pkg_kill_timeout=$PACKAGE_KILL_TIMEOUT
	case "$pkg" in
	*/integration/iam | */integration/iam/ | */integration/iam/...)
		pkg_timeout=$IAM_PACKAGE_TIMEOUT
		pkg_kill_timeout=$IAM_PACKAGE_KILL_TIMEOUT
		;;
	*/integration/cli | */integration/cli/ | */integration/cli/...)
		pkg_timeout=$CLI_PACKAGE_TIMEOUT
		pkg_kill_timeout=$CLI_PACKAGE_KILL_TIMEOUT
		;;
	*/integration/cluster | */integration/cluster/ | */integration/cluster/...)
		pkg_timeout=$CLUSTER_PACKAGE_TIMEOUT
		pkg_kill_timeout=$CLUSTER_PACKAGE_KILL_TIMEOUT
		;;
	esac
	# shellcheck disable=SC2086 # FLAGS is split into separate go test flags.
	{
		timeout --kill-after=30s "$pkg_kill_timeout" \
			go test -v -p 1 -failfast -tags "$tags" -timeout "$pkg_timeout" $flags "$pkg"
		echo $? >"$status_file"
	} 2>&1 | tee -a "$log"
	read -r rc <"$status_file"
	seconds=$(($(date +%s) - start))

	case $rc in
	0) result=pass ;;
	124 | 137) result=timeout ;;
	*) result=fail ;;
	esac
	echo "package $pkg: ${seconds}s $result" | tee -a "$log"
	printf '%-70s %6s %s\n' "$pkg" "$seconds" "$result" >>"$summary"

	if [ "$result" != pass ]; then
		overall=1
		if [ $remaining -gt 0 ] && ! QUICBAAS_STABILIZE_SECONDS=0 \
			timeout --kill-after=10s "$HEALTH_PROBE_TIMEOUT" health_check.sh >>"$log" 2>&1; then
			echo "infrastructure failure after $pkg; $remaining packages not run" | tee -a "$log"
			break
		fi
	fi
done

{
	echo ""
	echo "Package durations:"
	printf '%-70s %6s %s\n' "package" "seconds" "result"
	cat "$summary"
} | tee -a "$log"

exit $overall
