#!/bin/sh
# resolve-package.sh: map a per-package make target's stem to a package directory (`make test-api-grpc` -> pkg/api/grpc).
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
