#!/bin/sh
# run_packages_test.sh runs run_packages.sh against stub go, timeout and
# health_check.sh binaries placed first on PATH: every package gets a summary
# line, a failed package with a healthy cluster lets the rest run, a failed
# package with a dead cluster stops the suite, exit 124 reads as timeout,
# --filter splits go test flags from packages, and a probe that never answers
# is cut off at HEALTH_PROBE_TIMEOUT.
#
# Run by `make check-workspace-script`.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
script=$here/run_packages.sh
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() {
	echo "run_packages_test: $*" >&2
	exit 1
}

mkdir "$tmp/bin"

# The stub timeout drops its own options and duration and runs the command.
cat >"$tmp/bin/timeout" <<'EOF'
#!/bin/sh
while [ $# -gt 0 ]; do
	case $1 in
	--*) shift ;;
	*) shift; break ;;
	esac
done
exec "$@"
EOF

# The stub go records the package it ran (its last argument) and its whole
# command line, and exits with the code STUB_GO_EXIT_<name> names for it, 0 by
# default.
cat >"$tmp/bin/go" <<'EOF'
#!/bin/sh
for arg in "$@"; do
	pkg=$arg
done
echo "$pkg" >>"$STUB_RAN"
echo "$*" >>"$STUB_RAN.args"
echo "ok $pkg"
name=${pkg##*/}
case $name in
*[!A-Za-z0-9_]*) code=0 ;;
*) eval "code=\${STUB_GO_EXIT_$name:-0}" ;;
esac
exit "$code"
EOF

# The stub probe passes unless STUB_PROBE_FAIL=1.
printf '#!/bin/sh\n[ "${STUB_PROBE_FAIL:-}" != 1 ]\n' >"$tmp/bin/health_check.sh"
chmod +x "$tmp/bin/timeout" "$tmp/bin/go" "$tmp/bin/health_check.sh"

# run_suite runs run_packages.sh over packages a b c d, storing its output in
# $tmp/out and the packages go ran in $tmp/ran; it returns the exit status.
run_suite() {
	rm -f "$tmp/ran" "$tmp/log"
	: >"$tmp/ran"
	PATH="$tmp/bin:$PATH" STUB_RAN="$tmp/ran" \
		sh "$script" "$tmp/log" "integration" "-run TestX$" \
		example/a example/b example/c example/d >"$tmp/out" 2>&1
}

ran() {
	tr '\n' ' ' <"$tmp/ran"
}

# (i) All packages pass: exit 0 and one summary line each.
run_suite || fail "all pass: exit $?, want 0"
for p in a b c d; do
	grep -q "^package example/$p: [0-9]*s pass$" "$tmp/out" || fail "all pass: no summary line for example/$p"
done
grep -q '^Package durations:$' "$tmp/out" || fail "all pass: no duration table"
grep -q '^package example/a' "$tmp/log" || fail "all pass: the summary is not in the log"

# (ii) b fails, the probe passes: c and d still run, exit 1.
status=0
STUB_GO_EXIT_b=1 run_suite || status=$?
[ "$status" = 1 ] || fail "b fails, cluster up: exit $status, want 1"
[ "$(ran)" = "example/a example/b example/c example/d " ] || fail "b fails, cluster up: ran '$(ran)'"
grep -q '^package example/b: [0-9]*s fail$' "$tmp/out" || fail "b fails, cluster up: b not reported as fail"

# (iii) b fails and the probe fails: c and d do not run, exit 1.
status=0
STUB_GO_EXIT_b=1 STUB_PROBE_FAIL=1 run_suite || status=$?
[ "$status" = 1 ] || fail "b fails, cluster down: exit $status, want 1"
[ "$(ran)" = "example/a example/b " ] || fail "b fails, cluster down: ran '$(ran)'"
grep -q '^infrastructure failure after example/b; 2 packages not run$' "$tmp/out" ||
	fail "b fails, cluster down: the stop line does not name the count"

# (iv) Exit 124 from timeout is reported as timeout.
status=0
STUB_GO_EXIT_c=124 run_suite || status=$?
[ "$status" = 1 ] || fail "c times out: exit $status, want 1"
grep -q '^package example/c: [0-9]*s timeout$' "$tmp/out" || fail "c times out: c not reported as timeout"

# run_filter runs run_packages.sh --filter with filter $1, storing the go
# command lines in $tmp/ran.args; it returns the exit status.
run_filter() {
	rm -f "$tmp/ran" "$tmp/ran.args" "$tmp/log"
	: >"$tmp/ran"
	: >"$tmp/ran.args"
	PATH="$tmp/bin:$PATH" STUB_RAN="$tmp/ran" \
		sh "$script" --filter "$tmp/log" "integration" "$1" >"$tmp/out" 2>&1
}

calls() {
	wc -l <"$tmp/ran.args" | tr -d ' '
}

# (v) A value flag takes the next token: one go test, -run TestX on ./a/....
run_filter "-run TestX ./a/..." || fail "filter -run TestX ./a/...: exit $?, want 0"
[ "$(calls)" = 1 ] || fail "filter -run TestX ./a/...: $(calls) go test calls, want 1"
grep -q -- ' -run TestX \./a/\.\.\.$' "$tmp/ran.args" ||
	fail "filter -run TestX ./a/...: ran '$(cat "$tmp/ran.args")'"

# (vi) -flag=value is one token; each package's go test carries the flag.
run_filter "-run=TestX ./a/... ./b/..." || fail "filter -run=TestX two packages: exit $?, want 0"
[ "$(calls)" = 2 ] || fail "filter -run=TestX two packages: $(calls) go test calls, want 2"
grep -q -- ' -run=TestX \./a/\.\.\.$' "$tmp/ran.args" || fail "filter -run=TestX: ./a/... ran without the flag"
grep -q -- ' -run=TestX \./b/\.\.\.$' "$tmp/ran.args" || fail "filter -run=TestX: ./b/... ran without the flag"

# (vii) A packages-only filter runs each package with no extra flags.
run_filter "./test/integration/iam/..." || fail "filter packages only: exit $?, want 0"
[ "$(ran)" = "./test/integration/iam/... " ] || fail "filter packages only: ran '$(ran)'"
if grep -q -- '-run' "$tmp/ran.args"; then
	fail "filter packages only: a -run flag was passed"
fi

# (viii) A flags-only filter runs on the default root.
run_filter "-run TestX -count 1 -v" || fail "filter flags only: exit $?, want 0"
grep -q -- ' -run TestX -count 1 -v \./test/integration/\.\.\.$' "$tmp/ran.args" ||
	fail "filter flags only: ran '$(cat "$tmp/ran.args")'"

# (ix) A probe that never answers is cut off at HEALTH_PROBE_TIMEOUT and read
# as an infrastructure failure. The probe blocks opening a fifo nobody writes;
# the real timeout runs here, so only go and the probe are stubbed.
mkdir "$tmp/probe-bin"
cp "$tmp/bin/go" "$tmp/probe-bin/go"
mkfifo "$tmp/fifo"
printf '#!/bin/sh\nread -r line <"%s"\n' "$tmp/fifo" >"$tmp/probe-bin/health_check.sh"
chmod +x "$tmp/probe-bin/health_check.sh"
: >"$tmp/ran"
status=0
PATH="$tmp/probe-bin:$PATH" STUB_RAN="$tmp/ran" STUB_GO_EXIT_a=1 HEALTH_PROBE_TIMEOUT=1s \
	sh "$script" "$tmp/log" "integration" "" example/a example/b >"$tmp/out" 2>&1 || status=$?
[ "$status" = 1 ] || fail "probe hangs: exit $status, want 1"
[ "$(ran)" = "example/a " ] || fail "probe hangs: ran '$(ran)'"
grep -q '^infrastructure failure after example/a; 1 packages not run$' "$tmp/out" ||
	fail "probe hangs: no infrastructure failure line"

# Too few arguments are refused, in both modes.
if sh "$script" --filter "$tmp/log" integration 2>/dev/null; then
	fail "a --filter call without a filter was accepted"
fi
if sh "$script" "$tmp/log" integration "" 2>/dev/null; then
	fail "a call without packages was accepted"
fi

echo "run_packages_test: ok"
