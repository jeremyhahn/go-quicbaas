#!/bin/sh
# touched-packages.sh names the per-package unit targets (`make test-<stem>`)
# a change must run: the packages it changed and their direct importers.
#
# Usage: scripts/touched-packages.sh BASE TIP
#
# TIP must be the checked-out commit: importers are read from the working tree
# with `go list`. A changed file belongs to the nearest directory at or above
# it, within its top-level directory, that holds a .go file (so testdata and
# embedded files count for their package); a file outside every package names
# none. A directory that held a .go file at BASE counts as a package too, so
# deleting a package still selects its importers. Stems follow the Makefile's targets: pkg/api/grpc -> api-grpc (test-%),
# sdk/go/x/y -> sdk-go-x-y (test-sdk-go-%), scripts/... and test/buildgates/...
# -> tooling (test-tooling). A package with no unit target is reported on
# stderr only. go list runs with TOUCHED_TAGS when set, else the Makefile's
# TEST_TAGS.
#
# It prints the stems, one per line, sorted. Exit 0 on success, 2 on a bad
# argument, a TIP that is not checked out, a go list failure or a package that
# fails to load (a .Error, or a .DepsErrors entry that names no package the
# change deleted); nothing is printed on stdout then.
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
if [ "$(git rev-parse "$tip^{commit}")" != "$(git rev-parse HEAD)" ]; then
	echo "$0: check out $tip first" >&2
	exit 2
fi

# modpath FILE prints the module path a go.mod declares.
modpath() {
	while read -r word path _; do
		if [ "$word" = module ]; then
			echo "$path"
			return 0
		fi
	done <"$1"
	return 1
}
mod=$(modpath go.mod) || {
	echo "$0: no module line in go.mod" >&2
	exit 2
}
if [ -f sdk/go/go.mod ] && [ "$(modpath sdk/go/go.mod)" != "$mod/sdk/go" ]; then
	echo "$0: sdk/go/go.mod does not declare $mod/sdk/go" >&2
	exit 2
fi

# hasgo DIR exits 0 when DIR holds a .go file.
hasgo() {
	set +f
	for g in "$1"/*.go; do
		set -f
		[ -e "$g" ] && return 0
		return 1
	done
}

# basego DIR exits 0 when DIR held a .go file at BASE: a package the change
# deleted still names its surviving importers.
basego() {
	while IFS= read -r n; do
		case $n in
		*.go) return 0 ;;
		esac
	done <<EOF
$(git -c core.quotePath=false ls-tree --name-only "$base" -- "$1/")
EOF
	return 1
}

# pkgdir FILE prints the package directory FILE belongs to, if any.
pkgdir() {
	case $1 in
	*/*) ;;
	*) return 0 ;;
	esac
	top=${1%%/*}
	d=${1%/*}
	while :; do
		if { [ -d "$d" ] && hasgo "$d"; } || basego "$d"; then
			echo "$d"
			return 0
		fi
		[ "$d" != "$top" ] || return 0
		d=${d%/*}
	done
}

changed=" "
deleted=" "
while IFS= read -r file; do
	[ -n "$file" ] || continue
	d=$(pkgdir "$file")
	[ -n "$d" ] || continue
	case $changed in
	*" $mod/$d "*) ;;
	*)
		changed="$changed$mod/$d "
		{ [ -d "$d" ] && hasgo "$d"; } || deleted="$deleted$mod/$d "
		;;
	esac
done <<EOF
$(git -c core.quotePath=false diff --name-only --no-renames "$base" "$tip")
EOF
[ "$changed" != " " ] || exit 0

if [ -n "${TOUCHED_TAGS+x}" ]; then
	tags=$TOUCHED_TAGS
else
	unset MAKEFLAGS MFLAGS MAKELEVEL
	tags=$(${MAKE:-make} -s --no-print-directory print-TEST_TAGS) || {
		echo "$0: cannot read TEST_TAGS" >&2
		exit 2
	}
fi
# A package's own load error, then each error of its dependencies, comes
# first on a line starting with !, so a package that failed to load is never
# read as a complete import list.
format='{{if .Error}}!error {{.ImportPath}}: {{printf "%q" .Error.Err}}{{"\n"}}{{end}}{{range .DepsErrors}}!deps {{$.ImportPath}}: {{printf "%q" .Err}}{{"\n"}}{{end}}{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}'
listing=$(go list -e -tags "$tags" -f "$format" ./...) || {
	echo "$0: go list failed" >&2
	exit 2
}
if [ -f sdk/go/go.mod ]; then
	more=$(cd sdk/go && go list -e -f "$format" ./...) || {
		echo "$0: go list failed in sdk/go" >&2
		exit 2
	}
	listing="$listing
$more"
fi

# Refuse to select from a graph with a load error. The one error accepted is a
# dependency error naming a package this change deleted: its importers are
# what the selection is for.
bad=0
while IFS= read -r line; do
	case $line in
	'!error '*)
		echo "$0: ${line#!error }" >&2
		bad=1
		;;
	'!deps '*)
		err=${line#*: }
		ok=0
		# shellcheck disable=SC2086 # word-split the package list (set -f is on)
		for p in $deleted; do
			case $err in
			*" $p:"* | *" $p;"* | *" $p\""*)
				ok=1
				break
				;;
			esac
		done
		if [ "$ok" -eq 0 ]; then
			echo "$0: ${line#!deps }" >&2
			bad=1
		fi
		;;
	esac
done <<EOF
$listing
EOF
if [ "$bad" -ne 0 ]; then
	echo "$0: a package failed to load; no selection" >&2
	exit 2
fi

dirs=
while read -r pkg imports; do
	[ -n "$pkg" ] || continue
	case $pkg in
	'!'*) continue ;;
	esac
	# shellcheck disable=SC2086 # word-split the import list (set -f is on)
	for p in "$pkg" $imports; do
		case $changed in
		*" $p "*)
			if [ "$pkg" = "$mod" ]; then
				dirs="$dirs.
"
			else
				dirs="$dirs${pkg#"$mod"/}
"
			fi
			break
			;;
		esac
	done
done <<EOF
$listing
EOF

printf '%s' "$dirs" | LC_ALL=C sort -u | while IFS= read -r d; do
	case $d in
	pkg/?* | cmd/?*) d=${d#*/} && echo "$d" | tr / - ;;
	sdk/go) echo sdk-go ;;
	sdk/go/?*) d=${d#sdk/go/} && echo "sdk-go-$d" | tr / - ;;
	scripts/* | test/buildgates | test/buildgates/*) echo tooling ;;
	*) echo "touched-packages: no unit target for $d" >&2 ;;
	esac
done | LC_ALL=C sort -u
