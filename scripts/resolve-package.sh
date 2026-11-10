#!/bin/sh
# resolve-package.sh maps the stem of a per-package make target to a package
# directory, so `make test-api-grpc`, `make bench-api-grpc` and
# `make coverage-api-grpc` all reach pkg/api/grpc.
#
# Usage: scripts/resolve-package.sh NAME ROOT...
#
# GNU make strips the directory part of a target that contains a slash before
# it matches a slash-free pattern such as test-%, so `make test-api/grpc` never
# reaches the rule: nested packages are named with dashes instead. A dash can
# also be part of a real directory name (cmd/quicbaas-git-sign), so the search
# is:
#
#   1. NAME as given, under each ROOT in order;
#   2. NAME with every dash either kept or read as a slash, under each ROOT in
#      order.
#
# It prints ROOT/DIR for the first match and exits 0. It exits 1 when nothing
# matches, and 2 when one ROOT holds more than one step-2 match (pkg/a-b/c and
# pkg/a/b-c, say), listing them on stderr rather than guessing.
set -eu

if [ $# -lt 2 ]; then
	echo "usage: $0 NAME ROOT..." >&2
	exit 2
fi
name=$1
shift

case $name in
'' | -* | *- | *--* | /* | */ | *..*) exit 1 ;;
esac

# walk DIR REST prints every directory under DIR that REST names, where each
# dash in REST is either part of a directory name or a path separator.
walk() (
	dir=$1
	rest=$2
	head=
	while :; do
		seg=${rest%%-*}
		head=${head:+$head-}$seg
		if [ "$seg" = "$rest" ]; then
			if [ -d "$dir/$head" ]; then
				echo "$dir/$head"
			fi
			return 0
		fi
		rest=${rest#*-}
		if [ -d "$dir/$head" ]; then
			walk "$dir/$head" "$rest"
		fi
	done
)

for root in "$@"; do
	if [ -d "$root/$name" ]; then
		echo "$root/$name"
		exit 0
	fi
done

for root in "$@"; do
	found=$(walk "$root" "$name")
	if [ -z "$found" ]; then
		continue
	fi
	if [ "$(echo "$found" | wc -l)" -gt 1 ]; then
		echo "$0: '$name' names more than one package under $root/:" >&2
		echo "$found" | while read -r dir; do echo "  $dir" >&2; done
		exit 2
	fi
	echo "$found"
	exit 0
done
exit 1
