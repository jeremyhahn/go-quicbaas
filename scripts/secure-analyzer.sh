#!/bin/sh
set -eu

label=${1:?usage: secure-analyzer.sh <label> <tags> <vettool>}
tags=${2-}
vettool=${3:?usage: secure-analyzer.sh <label> <tags> <vettool>}

GOCMD=${GOCMD:-go}
root=$(git rev-parse --show-toplevel)

case "$vettool" in
	/*) ;;
	*) vettool=$root/$vettool ;;
esac

run_module() {
	module_dir=$1
	packages=${SECURE_ANALYZER_PACKAGES:-./...}

	printf 'secure-analyzer[%s]: module=%s tags=%s\n' "$label" "$module_dir" "$tags"
	(
		cd "$root/$module_dir"
		pkg_list=$($GOCMD list -tags "$tags" $packages)
		if [ -z "$pkg_list" ]; then
			printf 'secure-analyzer[%s]: module=%s matched no packages\n' "$label" "$module_dir" >&2
			exit 1
		fi
		pkg_count=$(printf '%s\n' "$pkg_list" | wc -l | tr -d ' ')
		printf 'secure-analyzer[%s]: module=%s packages=%s\n' "$label" "$module_dir" "$pkg_count"
		$GOCMD vet -vettool="$vettool" -tags "$tags" $packages
	)
}

if [ -n "${SECURE_ANALYZER_MODULES:-}" ]; then
	modules=$SECURE_ANALYZER_MODULES
else
	modules=$(git ls-files 'go.mod' '*/go.mod')
fi

for gomod in $modules; do
	case "$gomod" in
		go.mod) run_module "." ;;
		*/go.mod) run_module "${gomod%/go.mod}" ;;
		*) run_module "$gomod" ;;
	esac
done
