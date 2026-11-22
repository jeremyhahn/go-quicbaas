#!/bin/sh
# touched-features.sh names the features whose integration target
# (`make integration-test-<feature>`) a change must run, read from the feature
# mapping table in the Makefile (INTEGRATION_FEATURES, FEATURE_PATHS_<feature>,
# INTEGRATION_SHARED_PATHS, INTEGRATION_DOC_PATHS).
#
# Usage: scripts/touched-features.sh BASE TIP
#
# It prints the touched features, one per line, sorted. A changed file under a
# documentation path is skipped; a changed file under a shared path, or owned
# by no feature, means every feature: the output is then the single line ALL.
# A change confined to documentation, or no change, prints nothing.
#
# The table is read from the working tree's Makefile. Exit 0 on success, 2 on
# a bad argument or an unreadable table (nothing is printed on stdout then).
set -euf

usage() {
	echo "usage: $0 BASE TIP" >&2
	exit 2
}

[ $# -eq 2 ] || usage
cd "$(git rev-parse --show-toplevel)"
for rev in "$1" "$2"; do
	case $rev in
	'' | -*) usage ;;
	esac
	git rev-parse --verify --quiet "$rev^{commit}" >/dev/null || usage
done
base=$1
tip=$2

unset MAKEFLAGS MFLAGS MAKELEVEL
mk() {
	${MAKE:-make} -s --no-print-directory "$@"
}

map=$(mk print-feature-map 2>/dev/null) || map=
if [ -z "$map" ]; then
	echo "$0: cannot read the feature mapping table (make print-feature-map)" >&2
	exit 2
fi
shared=$(mk print-INTEGRATION_SHARED_PATHS 2>/dev/null) || {
	echo "$0: cannot read INTEGRATION_SHARED_PATHS" >&2
	exit 2
}
docs=$(mk print-INTEGRATION_DOC_PATHS 2>/dev/null) || {
	echo "$0: cannot read INTEGRATION_DOC_PATHS" >&2
	exit 2
}

# owns FILE PATH... exits 0 when one PATH owns FILE.
owns() {
	file=$1
	shift
	for p in "$@"; do
		case $p in
		*/)
			case $file in
			"$p"*) return 0 ;;
			esac
			;;
		*)
			# shellcheck disable=SC2254 # PATH is a shell pattern
			case $file in
			$p) return 0 ;;
			esac
			;;
		esac
	done
	return 1
}

changed=$(git -c core.quotePath=false diff --name-only --no-renames "$base" "$tip")
touched=
all=0
nl='
'
while IFS= read -r file; do
	[ -n "$file" ] || continue
	# shellcheck disable=SC2086 # word-split the path lists (set -f is on)
	if owns "$file" $docs; then
		continue
	fi
	# shellcheck disable=SC2086
	if owns "$file" $shared; then
		all=1
		break
	fi
	hit=0
	while read -r feature paths; do
		# shellcheck disable=SC2086
		if [ -n "$paths" ] && owns "$file" $paths; then
			touched="$touched$feature$nl"
			hit=1
		fi
	done <<EOF
$map
EOF
	if [ "$hit" -eq 0 ]; then
		all=1
		break
	fi
done <<EOF
$changed
EOF

if [ "$all" -eq 1 ]; then
	echo ALL
elif [ -n "$touched" ]; then
	printf '%s' "$touched" | LC_ALL=C sort -u
fi
