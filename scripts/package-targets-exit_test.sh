#!/bin/sh
# package-targets-exit_test.sh proves the per-package make targets report a
# failing `go test` as a failure. coverage-% used to run `go test ...;` and then
# write its report, so a red package exited 0 and the target went green.
#
# It runs each target for real with a stand-in `go` first on PATH: `go test`
# writes an empty coverage profile and exits FAKE_GO_TEST_EXIT, `go tool cover`
# writes or prints a trivial report, and every other command runs the real go.
# Nothing compiles and nothing outside a temporary directory is written.
#
# Usage: scripts/package-targets-exit_test.sh
# (make check-package-targets runs it).
set -eu

MAKE=${MAKE:-make}
# The child make must not inherit the parent's jobserver or flags.
unset MAKEFLAGS MFLAGS MAKELEVEL

real_go=$(command -v go)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/bin" "$tmp/coverage"

cat >"$tmp/bin/go" <<EOF
#!/bin/sh
case "\$1 \${2:-}" in
'test '*)
	for arg; do
		case \$arg in
		-coverprofile=*) echo 'mode: set' >"\${arg#-coverprofile=}" ;;
		esac
	done
	exit "\$FAKE_GO_TEST_EXIT"
	;;
'tool cover')
	out=
	for arg; do
		if [ "\$out" = next ]; then
			out=\$arg
		elif [ "\$arg" = -o ]; then
			out=next
		fi
	done
	if [ -n "\$out" ]; then
		: >"\$out"
	else
		printf 'total:\t(statements)\t0.0%%\n'
	fi
	exit 0
	;;
esac
exec "$real_go" "\$@"
EOF
chmod +x "$tmp/bin/go"

failures=0

fail() {
	echo "FAIL: $*"
	failures=$((failures + 1))
}

# run CODE TARGET runs TARGET with `go test` exiting CODE and prints make's
# exit status.
run() {
	code=0
	PATH="$tmp/bin:$PATH" FAKE_GO_TEST_EXIT=$1 \
		$MAKE --no-print-directory "$2" GOCMD=go COVERAGE_DIR="$tmp/coverage" \
		>"$tmp/out" 2>&1 || code=$?
	echo "$code"
}

# fails TARGET checks TARGET exits non-zero when `go test` fails.
fails() {
	if [ "$(run 1 "$1")" = 0 ]; then
		fail "make $1 exited 0 after go test failed: $(tail -n 5 "$tmp/out")"
	fi
}

# reports TARGET checks TARGET exits 0 and writes its HTML report when
# `go test` passes.
reports() {
	rm -f "$tmp/coverage/$1.html"
	code=$(run 0 "$1")
	if [ "$code" != 0 ]; then
		fail "make $1 exited $code after go test passed: $(tail -n 5 "$tmp/out")"
	elif [ ! -f "$tmp/coverage/$1.html" ]; then
		fail "make $1 wrote no $tmp/coverage/$1.html"
	fi
}

for target in test-api-grpc bench-api-grpc coverage-api-grpc \
	test-sdk-go-transport-grpc bench-sdk-go-transport-grpc \
	coverage-sdk-go-transport-grpc; do
	fails "$target"
done
reports coverage-api-grpc
reports coverage-sdk-go-transport-grpc

if [ "$failures" -ne 0 ]; then
	echo "package-targets-exit_test.sh: $failures failure(s)"
	exit 1
fi
echo "package-targets-exit_test.sh: ok"
