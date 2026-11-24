#!/bin/sh
# touched-packages_test.sh proves scripts/touched-packages.sh names the changed
# packages and their direct importers as make test-<stem> stems. It runs the
# script on a temporary two-module repository, then checks on this repository
# that the stem of every package under pkg/ and cmd/ resolves back to that
# package through scripts/resolve-package.sh (what test-% runs).
#
# Usage: scripts/touched-packages_test.sh (make check-feature-map runs it).
set -eu

MAKE=${MAKE:-make}
export MAKE
# The child make must not inherit the parent's jobserver or flags.
unset MAKEFLAGS MFLAGS MAKELEVEL

root=$(git rev-parse --show-toplevel)
failures=0

fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}

tree=$(mktemp -d)
trap 'rm -rf "$tree"' EXIT

# tgit runs git in the temporary repository, never with the caller's config.
tgit() {
	GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 git -C "$tree" "$@"
}

# commit records every change in the temporary repository and prints its id.
commit() {
	tgit add -A
	tgit -c user.name=test -c user.email=test@example.invalid \
		-c commit.gpgsign=false commit -q -m "$1"
	tgit rev-parse HEAD
}

# gofile PATH PACKAGE [IMPORT...] writes a Go file importing each IMPORT.
gofile() {
	f=$tree/$1
	mkdir -p "${f%/*}"
	{
		echo "package $2"
		shift 2
		for i in "$@"; do
			echo "import _ \"$i\""
		done
	} >"$f"
}

# packages BASE TIP WANT checks touched-packages.sh prints exactly WANT.
packages() {
	got=$(cd "$tree" && GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 \
		GOFLAGS=-mod=mod GOWORK=off GOTOOLCHAIN=local GOPROXY=off TOUCHED_TAGS= \
		sh scripts/touched-packages.sh "$1" "$2" 2>/dev/null) || got="exit $?"
	if [ "$got" != "$3" ]; then
		fail "touched-packages $1 $2: got '$got', want '$3'"
	fi
}

tgit init -q
mkdir -p "$tree/scripts" "$tree/docs" "$tree/pkg/a/testdata" "$tree/sdk/go"
cp "$root/scripts/touched-packages.sh" "$tree/scripts/touched-packages.sh"
printf '%s\n' 'module example.com/m' '' 'go 1.21' '' \
	'require example.com/m/sdk/go v0.0.0' '' \
	'replace example.com/m/sdk/go => ./sdk/go' >"$tree/go.mod"
printf '%s\n' 'module example.com/m/sdk/go' '' 'go 1.21' >"$tree/sdk/go/go.mod"
printf 'all:\n' >"$tree/Makefile"
echo '{}' >"$tree/pkg/a/testdata/f.json"
echo x >"$tree/docs/x.md"
gofile pkg/a/a.go a
gofile pkg/a/inner/i.go inner
gofile pkg/b/b.go b example.com/m/pkg/a
gofile pkg/c/c.go c example.com/m/pkg/b
gofile pkg/d/d_test.go d_test example.com/m/pkg/a
gofile cmd/tool/main.go main example.com/m/pkg/c
gofile scripts/x/x.go x example.com/m/pkg/a
gofile test/integration/foo/foo.go foo example.com/m/pkg/a
gofile api/v1/v.go v1
gofile pkg/e/e.go e example.com/m/api/v1
gofile pkg/f/f.go f example.com/m/sdk/go/client/k
gofile sdk/go/client/k/k.go k
gofile pkg/g/g.go g
gofile pkg/h/h.go h example.com/m/pkg/g
prev=$(commit base)

# step FILE WANT changes FILE, commits and checks the output is WANT.
step() {
	echo "// $prev" >>"$tree/$1"
	next=$(commit "$1")
	packages "$prev" "$next" "$2"
	prev=$next
}

step pkg/a/a.go "a
b
d
tooling"
step pkg/a/testdata/f.json "a
b
d
tooling"
step pkg/c/c.go "c
tool"
step api/v1/v.go e
step sdk/go/client/k/k.go "f
sdk-go-client-k"
step docs/x.md ''
step Makefile ''

# Deleting a package's last Go file still selects its surviving importers.
rm "$tree/pkg/g/g.go"
next=$(commit "delete pkg/g")
packages "$prev" "$next" h
prev=$next

# The importers come from the working tree, so TIP must be checked out.
packages "$prev~2" "$prev~1" "exit 2"

# A package that fails to load (a malformed import) has an incomplete import
# list: the selector refuses rather than leave its tests out.
mkdir -p "$tree/pkg/m"
cp "$root/scripts/testdata/touched-packages/malformed.go.txt" "$tree/pkg/m/m.go"
prev=$(commit "malformed pkg/m")
step pkg/a/a.go "exit 2"

# Every package under pkg/ and cmd/ of this repository: its stem must lead
# make test-<stem> (scripts/resolve-package.sh) back to the same directory.
dirs=$(cd "$root" && git ls-files -- 'pkg/*.go' 'cmd/*.go' | while IFS= read -r f; do
	echo "${f%/*}"
done | LC_ALL=C sort -u)
while IFS= read -r d; do
	[ -n "$d" ] || continue
	stem=$(echo "${d#*/}" | tr / -)
	got=$(cd "$root" && sh scripts/resolve-package.sh "$stem" pkg cmd 2>&1) || got="$got (exit $?)"
	if [ "$got" != "$d" ]; then
		fail "stem $stem of $d: resolve-package.sh prints '$got'"
	fi
done <<EOF
$dirs
EOF

if [ "$failures" -ne 0 ]; then
	echo "touched-packages_test.sh: $failures failure(s)"
	exit 1
fi
echo "touched-packages_test.sh: ok"
