#!/bin/sh
# check-feature-map.sh checks the feature mapping table in the Makefile
# (testing-guidelines section 4): every integration test belongs to a feature,
# every feature has an integration target, and every path of the table names
# a tracked file. A table path with no / names a file at the repository root
# only (so *.md is the root's markdown, not an embedded prompt under pkg/).
#
# Usage: scripts/check-feature-map.sh
#
# It fails when:
#   - a feature of INTEGRATION_FEATURES has no FEATURE_PATHS_<feature> row;
#   - a feature has neither an explicit integration-test-<feature> rule nor a
#     directory test/integration/<feature>/ (the integration-test-% rule);
#   - a path of a row, of INTEGRATION_SHARED_PATHS, INTEGRATION_DOC_PATHS or
#     INTEGRATION_UNTARGETED_PATHS matches no file of `git ls-files`;
#   - a tracked *_test.go or *.test.js under any test/integration/ directory
#     (test/integration/, sdk/go/test/integration/, ...), or a tracked
#     *_test.go anywhere whose //go:build line requires the integration tag
#     (pkg/sdlc/forge_gitea_live_test.go, ...), matches no feature row, no
#     shared path and no INTEGRATION_UNTARGETED_PATHS path.
#
# Exit 0 when the table is clean, 1 listing every problem, 2 when the table or
# make's rule database cannot be read.
set -euf

cd "$(git rev-parse --show-toplevel)"
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
untargeted=$(mk print-INTEGRATION_UNTARGETED_PATHS 2>/dev/null) || {
	echo "$0: cannot read INTEGRATION_UNTARGETED_PATHS" >&2
	exit 2
}

# The explicit integration-test-<name> rules in make's database. .DEFAULT has
# no recipe, so -q runs nothing; the exit code is not meaningful.
rules=" "
skip=0
db=$(${MAKE:-make} -npq __FEATURE_MAP_DB__=1 .DEFAULT 2>/dev/null) || :
while IFS= read -r line; do
	case $line in
	'# Not a target:'*)
		skip=1
		continue
		;;
	integration-test-*:*)
		name=${line%%:*}
		name=${name#integration-test-}
		case $name in
		*%* | *' '* | '') ;;
		*) [ "$skip" -eq 1 ] || rules="$rules$name " ;;
		esac
		;;
	esac
	skip=0
done <<EOF
$db
EOF
if [ "$rules" = " " ]; then
	echo "$0: make's rule database shows no integration-test- rule" >&2
	exit 2
fi

problems=0
problem() {
	echo "$*"
	problems=$((problems + 1))
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
		*/*)
			# shellcheck disable=SC2254 # PATH is a shell pattern
			case $file in
			$p) return 0 ;;
			esac
			;;
		*)
			# A path with no / names a repository-root file only.
			# shellcheck disable=SC2254 # PATH is a shell pattern
			case $file in
			*/*) ;;
			$p) return 0 ;;
			esac
			;;
		esac
	done
	return 1
}

# tracked PATH exits 0 when PATH names at least one tracked file.
tracked() {
	[ -n "$(git ls-files -- "$1")" ]
}

owned=
while read -r feature paths; do
	if [ -z "$paths" ]; then
		problem "$feature: no FEATURE_PATHS_$feature row in the mapping table"
	fi
	case $rules in
	*" $feature "*) ;;
	*)
		[ -d "test/integration/$feature" ] ||
			problem "$feature: no integration-test-$feature rule and no test/integration/$feature/"
		;;
	esac
	# shellcheck disable=SC2086 # word-split the path list (set -f is on)
	for p in $paths; do
		tracked "$p" || problem "$feature: path $p matches no tracked file"
	done
	owned="$owned $paths"
done <<EOF
$map
EOF
# shellcheck disable=SC2086
for p in $shared; do
	tracked "$p" || problem "INTEGRATION_SHARED_PATHS: path $p matches no tracked file"
done
# shellcheck disable=SC2086
for p in $docs; do
	tracked "$p" || problem "INTEGRATION_DOC_PATHS: path $p matches no tracked file"
done
# shellcheck disable=SC2086
for p in $untargeted; do
	tracked "$p" || problem "INTEGRATION_UNTARGETED_PATHS: path $p matches no tracked file"
done

# The tracked *_test.go files whose //go:build line requires the integration
# tag (a term integration not negated by !), wherever they live.
tagged=$(git -c core.quotePath=false grep -l -E '^//go:build.*[ (&|]integration([ )]|$)' -- '*_test.go') || {
	[ $? -eq 1 ] || {
		echo "$0: cannot search the build constraints (git grep)" >&2
		exit 2
	}
}

while IFS= read -r file; do
	[ -n "$file" ] || continue
	# shellcheck disable=SC2086
	owns "$file" $owned $shared $untargeted ||
		problem "$file: in no feature of the mapping table (Makefile INTEGRATION_FEATURES)"
done <<EOF
$({
	git -c core.quotePath=false ls-files ':(glob)**/test/integration/**/*_test.go' ':(glob)**/test/integration/**/*.test.js'
	printf '%s\n' "$tagged"
} | sort -u)
EOF

if [ "$problems" -ne 0 ]; then
	echo "check-feature-map.sh: $problems problem(s)"
	exit 1
fi
echo "check-feature-map.sh: ok"
