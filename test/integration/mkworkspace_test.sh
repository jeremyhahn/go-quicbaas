#!/bin/sh
# mkworkspace_test.sh exercises mkworkspace.sh in a temporary directory: the go
# line follows the toolchain (real and stubbed), the result parses as a go.work,
# and an unusable GOVERSION is refused rather than written.
#
# Run by `make check-workspace-script`.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
script=$here/mkworkspace.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "mkworkspace_test: $*" >&2
	exit 1
}

# stub_go NAME GOVERSION writes a fake go binary whose `env GOVERSION` prints
# GOVERSION, and prints its path.
stub_go() {
	stub=$tmp/$1
	printf '#!/bin/sh\necho "%s"\n' "$2" >"$stub"
	chmod +x "$stub"
	echo "$stub"
}

# The real toolchain: the go line is its version and `go work edit` parses it.
real=$(go env GOVERSION)
real=${real%% *}
(cd "$tmp" && sh "$script")
first=$(head -n 1 "$tmp/go.work")
[ "$first" = "go ${real#go}" ] || fail "real toolchain: go line '$first', want 'go ${real#go}'"
go work edit -json "$tmp/go.work" >/dev/null || fail "real toolchain: go.work does not parse"

# A stubbed toolchain, an explicit output path and a GOVERSION suffix.
GO=$(stub_go go-suffix "go1.99.3 X:nodwarf5") sh "$script" "$tmp/stub.work"
first=$(head -n 1 "$tmp/stub.work")
[ "$first" = "go 1.99.3" ] || fail "stubbed toolchain: go line '$first', want 'go 1.99.3'"

# Release candidates keep their suffix.
GO=$(stub_go go-rc "go1.28rc1") sh "$script" "$tmp/rc.work"
first=$(head -n 1 "$tmp/rc.work")
[ "$first" = "go 1.28rc1" ] || fail "rc toolchain: go line '$first', want 'go 1.28rc1'"

# Versions that cannot become a go line are refused and nothing is written.
for bad in "devel +abc123" "1.27.2" "go" ""; do
	if GO=$(stub_go go-bad "$bad") sh "$script" "$tmp/bad.work" 2>/dev/null; then
		fail "GOVERSION '$bad' was accepted"
	fi
	[ ! -e "$tmp/bad.work" ] || fail "GOVERSION '$bad' left a go.work behind"
done

# A go binary that fails is refused.
if GO=$tmp/no-such-go sh "$script" "$tmp/bad.work" 2>/dev/null; then
	fail "a missing go binary was accepted"
fi

echo "mkworkspace_test: ok"
