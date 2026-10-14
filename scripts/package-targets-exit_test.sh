#!/bin/sh
# package-targets-exit_test.sh proves the per-package make targets report a
# failing `go test` as a failure. coverage-% used to run `go test ...;` and then
# write its report, so a red package exited 0 and the target went green. It
# also proves test-sdk-go, test-sdk-go-coverage and coverage refuse to run
# `go test` when their `go list` fails or leaves no package.
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
'list -m') ;;
'list '*)
	case \${FAKE_GO_LIST:-} in
	'') ;;
	fail)
		echo 'go: errors parsing go.mod (stand-in)' >&2
		exit 1
		;;
	*)
		for pkg in \${FAKE_GO_LIST#pkgs:}; do
			echo "\$pkg"
		done
		exit 0
		;;
	esac
	;;
'test '*)
	printf '%s\n' "\$@" >"$tmp/go-test-args"
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
# exit status. When go_list is set, `go list` answers from it instead of
# running: "fail" exits 1 as a broken go.mod does, "pkgs:A B" prints A and B.
go_list=
run() {
	code=0
	rm -f "$tmp/go-test-args"
	PATH="$tmp/bin:$PATH" FAKE_GO_TEST_EXIT=$1 FAKE_GO_LIST=$go_list \
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

# refuses TARGET LIST checks TARGET exits non-zero, names itself and never
# runs `go test` when `go list` answers LIST. `go test` with no package
# arguments tests the current directory's package and can pass, so a failed
# or empty package list must stop the target, not shrink its run.
refuses() {
	go_list=$2
	code=$(run 0 "$1")
	go_list=
	if [ "$code" = 0 ]; then
		fail "make $1 exited 0 with go list '$2': $(tail -n 5 "$tmp/out")"
	elif [ -f "$tmp/go-test-args" ]; then
		fail "make $1 ran go test with go list '$2'"
	elif ! grep -q "$1:" "$tmp/out"; then
		fail "make $1 did not name itself when refusing go list '$2': $(tail -n 5 "$tmp/out")"
	fi
}

# tests TARGET DROPPED checks TARGET exits 0 and hands `go test` the listed
# package but not DROPPED, the package its filter excludes.
tests() {
	go_list="pkgs:example.com/m/pkg/a $2"
	code=$(run 0 "$1")
	go_list=
	if [ "$code" != 0 ]; then
		fail "make $1 exited $code with a good package list: $(tail -n 5 "$tmp/out")"
	elif ! grep -qx example.com/m/pkg/a "$tmp/go-test-args"; then
		fail "make $1 did not pass the listed package to go test"
	elif grep -qx "$2" "$tmp/go-test-args"; then
		fail "make $1 passed the excluded package $2 to go test"
	fi
}

for target in test-sdk-go test-sdk-go-coverage coverage; do
	refuses "$target" fail
	refuses "$target" pkgs:
done
refuses test-sdk-go pkgs:example.com/m/test/integration
refuses test-sdk-go-coverage pkgs:example.com/m/test/integration
refuses coverage pkgs:example.com/m/pkg/x/proto
# test-sdk-go-coverage is not run on a good list: its profile path is fixed
# inside sdk/go, outside the temporary directory.
tests test-sdk-go example.com/m/test/integration
tests coverage example.com/m/pkg/x/proto

if [ "$failures" -ne 0 ]; then
	echo "package-targets-exit_test.sh: $failures failure(s)"
	exit 1
fi
echo "package-targets-exit_test.sh: ok"
