#!/bin/sh
# resolve-package_test.sh proves the per-package make targets reach nested
# packages. It checks scripts/resolve-package.sh against a temporary tree,
# then asks `make -n` what test-%, bench-%, coverage-% and their Go SDK forms
# (test-sdk-go-%, bench-sdk-go-%, coverage-sdk-go-%) would run for real
# packages, and runs an unknown name to see it fail.
#
# Usage: TEST_TAGS=... IAM_TEST_TAGS=... scripts/resolve-package_test.sh
# (make check-package-targets passes both).
set -eu

: "${TEST_TAGS:?TEST_TAGS must be set}"
: "${IAM_TEST_TAGS:?IAM_TEST_TAGS must be set}"
MAKE=${MAKE:-make}
# The child make must not inherit the parent's jobserver or flags.
unset MAKEFLAGS MFLAGS MAKELEVEL

resolver=$(pwd)/scripts/resolve-package.sh
failures=0

fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}

# resolves NAME WANT ROOT... checks the resolver prints WANT for NAME.
resolves() {
	name=$1
	want=$2
	shift 2
	got=$("$resolver" "$name" "$@" 2>/dev/null) || got="exit $?"
	if [ "$got" != "$want" ]; then
		fail "resolve $name in $*: got '$got', want '$want'"
	fi
}

# refuses NAME CODE ROOT... checks the resolver exits CODE for NAME.
refuses() {
	name=$1
	want=$2
	shift 2
	code=0
	"$resolver" "$name" "$@" >/dev/null 2>&1 || code=$?
	if [ "$code" != "$want" ]; then
		fail "resolve $name in $*: exit $code, want $want"
	fi
}

tree=$(mktemp -d)
trap 'rm -rf "$tree"' EXIT
mkdir -p "$tree/pkg/api/grpc" "$tree/pkg/server/tpm2-keys" "$tree/pkg/a-b/c" \
	"$tree/pkg/a/b-c" "$tree/pkg/x-y" "$tree/pkg/x/y" "$tree/cmd/git-sign" \
	"$tree/cmd/api/grpc"
(
	cd "$tree"
	resolves api-grpc pkg/api/grpc pkg cmd
	resolves server-tpm2-keys pkg/server/tpm2-keys pkg cmd
	resolves server/tpm2-keys pkg/server/tpm2-keys pkg cmd
	resolves git-sign cmd/git-sign pkg cmd
	resolves x-y pkg/x-y pkg cmd
	resolves api-grpc cmd/api/grpc cmd pkg
	refuses a-b-c 2 pkg cmd
	refuses nope 1 pkg cmd
	refuses api--grpc 1 pkg cmd
	refuses -api 1 pkg cmd
	refuses ../pkg 1 pkg cmd
	exit "$failures"
) || failures=$?

# runs TARGET TEXT... checks every TEXT appears in what `make -n TARGET` runs.
runs() {
	target=$1
	shift
	out=$($MAKE --no-print-directory -n "$target" 2>&1) || {
		fail "make -n $target exited non-zero: $out"
		return 0
	}
	for text in "$@"; do
		case $out in
		*"$text"*) ;;
		*) fail "make -n $target: want $text" ;;
		esac
	done
}

# expect TARGET DIR [TAGS] checks a pattern target resolves DIR, with TAGS.
expect() {
	if [ $# -gt 2 ]; then
		runs "$1" "PKG_DIR='$2'" "TAGS='$3'"
	else
		runs "$1" "PKG_DIR='$2'"
	fi
}

# sdk_expect TARGET DIR TEXT... checks a Go SDK pattern target resolves DIR and
# runs go inside the sdk/go module on that package, with every TEXT.
sdk_expect() {
	target=$1
	dir=$2
	shift 2
	# shellcheck disable=SC2016 # the text make -n prints, unexpanded
	runs "$target" "PKG_DIR='$dir'" 'SDK_PKG_PATH="./${PKG_DIR#sdk/go/}/..."' \
		'cd sdk/go && go test' "$@"
}

# unknown TARGET checks a target naming no package fails before running go.
unknown() {
	out=$($MAKE --no-print-directory "$1" 2>&1) && {
		fail "make $1 succeeded"
		return 0
	}
	case $out in
	*"not found in"*) ;;
	*) fail "make $1: want a 'not found' error, got: $out" ;;
	esac
}

expect test-api-grpc pkg/api/grpc "$TEST_TAGS"
expect test-server-tpm2-keys pkg/server/tpm2-keys "$TEST_TAGS"
expect test-quicbaas-git-sign cmd/quicbaas-git-sign "$TEST_TAGS"
# test-iam is an explicit target (a longer race timeout); it must keep the IAM tags.
runs test-iam "-tags '$IAM_TEST_TAGS'" ./pkg/iam/...
expect bench-iam-dpop pkg/iam/dpop "$IAM_TEST_TAGS"
expect coverage-iam-dpop pkg/iam/dpop "$IAM_TEST_TAGS"
expect test-iam-dpop pkg/iam/dpop "$IAM_TEST_TAGS"
expect bench-api-grpc pkg/api/grpc "$TEST_TAGS"
expect coverage-api-grpc pkg/api/grpc "$TEST_TAGS"
sdk_expect test-sdk-go-transport-grpc sdk/go/transport/grpc "go test -v -race -count=1"
sdk_expect bench-sdk-go-transport-grpc sdk/go/transport/grpc "go test -bench=. -benchmem"
# shellcheck disable=SC2016 # the text make -n prints, unexpanded
sdk_expect coverage-sdk-go-transport-grpc sdk/go/transport/grpc \
	"/coverage/coverage-sdk-go-transport-grpc'" '-coverprofile="${PROFILE}.out"'
unknown test-no-such-package
unknown bench-no-such-package
unknown coverage-no-such-package
unknown test-sdk-go-no-such-package
unknown bench-sdk-go-no-such-package
unknown coverage-sdk-go-no-such-package

if [ "$failures" -ne 0 ]; then
	echo "resolve-package_test.sh: $failures failure(s)"
	exit 1
fi
echo "resolve-package_test.sh: ok"
