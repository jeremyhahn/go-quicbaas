#!/bin/sh
# run_packages.sh runs integration test packages one at a time, each under its
# own time limit, so one hung package costs at most 11 minutes instead of the
# whole suite (#1591).
#
# Usage: run_packages.sh LOG TAGS FLAGS PKG...
#        run_packages.sh --filter LOG TAGS FILTER
#
#   LOG    file every package's output is appended to (and echoed)
#   TAGS   build tags, passed to go test -tags
#   FLAGS  extra go test flags, split on spaces (may be empty), e.g. "-run X$"
#   PKG    packages or package patterns, each run by its own go test
#   FILTER a QUICBAAS_TEST_FILTER value, go test flags and packages mixed, e.g.
#          "-run TestX ./test/integration/...". It is split on spaces: a token
#          starting with - is a flag, and a flag that takes a value (-run,
#          -skip, -count, -timeout, -tags, ...) takes the next token with it
#          unless written -flag=value; every other token is a package. A
#          filter without packages runs on FILTER_DEFAULT_ROOT.
#
# After a failed package the cluster probe (health_check.sh on PATH) runs,
# bounded by HEALTH_PROBE_TIMEOUT: if the cluster is down, or the probe does
# not answer in time, the remaining packages are not run. The script exits 0
# only if every package passed, and ends with a table of package durations.
#
# Tested by run_packages_test.sh (`make check-workspace-script`).
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
	# shellcheck disable=SC2086 # FLAGS is split into separate go test flags.
	{
		timeout --kill-after=30s "$PACKAGE_KILL_TIMEOUT" \
			go test -v -p 1 -failfast -tags "$tags" -timeout "$PACKAGE_TIMEOUT" $flags "$pkg"
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
