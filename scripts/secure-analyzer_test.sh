#!/bin/sh
set -eu

GOCMD=${GOCMD:-go}
MAKE=${MAKE:-make}
root=$(git rev-parse --show-toplevel)
tmp_dir=$(mktemp -d)

# Every planted source lives in tmp_dir, outside the clone, and reaches the
# analyzer through go's -overlay, so a run killed at any point leaves no
# tracked file changed. selftest_dir is created empty because the vettool
# runs inside the package directory; git never shows an empty directory.
selftest_dir="$root/.secure-analyzer-selftest"
selftest_src="$tmp_dir/selftest.go"
selftest_overlay="$tmp_dir/selftest-overlay.json"
hardware_selftest_file="$root/cmd/quicbaas/piv_local_pkcs11.go"
hardware_probe="$tmp_dir/piv_local_pkcs11.go"
hardware_overlay="$tmp_dir/hardware-overlay.json"

cleanup() {
	rm -rf "$tmp_dir" "$selftest_dir"
}
trap cleanup EXIT
trap 'exit 1' INT TERM

# write_overlay <overlay-json> <path-go-sees> <planted-copy>
write_overlay() {
	printf '{"Replace":{"%s":"%s"}}\n' "$2" "$3" > "$1"
}

# overlay_goflags <overlay-json> prints GOFLAGS with -overlay appended.
overlay_goflags() {
	printf '%s' "${GOFLAGS:+$GOFLAGS }-overlay=$1"
}

rm -rf "$selftest_dir"
mkdir "$selftest_dir"
write_overlay "$selftest_overlay" "$selftest_dir/selftest.go" "$selftest_src"
{
	printf 'package secureanalyzerselftest\n\n'
	printf 'import (\n'
	printf '\t_ "github.com/awnumar/memguard"\n'
	printf '\t"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"\n'
	printf '\t"golang.org/x/crypto/ssh"\n'
	printf ')\n\n'
	printf 'type rawHolder struct {\n'
	printf '\tSecret []byte\n'
	printf '}\n'
	printf '\n'
	printf 'func parsePrivateKey(raw []byte) {\n'
	printf '\t_, _ = ssh.ParsePrivateKey(raw)\n'
	printf '}\n'
	printf '\n'
	printf '\n'
	printf 'func copyOut() error {\n'
	printf '\tbuf, err := secure.NewBufferFromBytes([]byte("probe"))\n'
	printf '\tif err != nil {\n'
	printf '\t\treturn err\n'
	printf '\t}\n'
	printf '\tdefer buf.Destroy()\n'
	printf '\tvar out []byte\n'
	printf '\terr = buf.WithBytes(func(b []byte) error {\n'
	printf '\t\tout = make([]byte, len(b))\n'
	printf '\t\tcopy(out, b)\n'
	printf '\t\treturn nil\n'
	printf '\t})\n'
	printf '\t_ = out\n'
	printf '\treturn err\n'
	printf '}\n'
} > "$selftest_src"

set +e
output=$(GOFLAGS=$(overlay_goflags "$selftest_overlay") \
	SECURE_ANALYZER_MODULES=go.mod \
	SECURE_ANALYZER_PACKAGES=./.secure-analyzer-selftest \
	GOCMD="$GOCMD" \
	"$root/scripts/secure-analyzer.sh" self-test "${SECURE_ANALYZER_SELFTEST_TAGS:-}" "${1:?usage: secure-analyzer_test.sh <vettool>}" 2>&1)
rc=$?
set -e
printf '%s\n' "$output"

if [ "$rc" -eq 0 ]; then
	printf 'secure-analyzer self-test: expected planted violations to fail, got rc=0\n' >&2
	exit 1
fi

assert_diagnostic() {
	name=$1
	needle=$2
	case "$output" in
		*"$needle"*) ;;
		*)
			printf 'secure-analyzer self-test: missing %s diagnostic containing: %s\n' "$name" "$needle" >&2
			exit 1
			;;
	esac
}

assert_diagnostic memguard 'import of "github.com/awnumar/memguard" is forbidden'
assert_diagnostic parse-private-key 'ssh.ParsePrivateKey'
assert_diagnostic sensitive-field 'field "Secret" holds sensitive material'
assert_diagnostic secure-copy 'copies protected bytes into ordinary memory'

rm -rf "$selftest_dir"

while IFS= read -r line; do
	printf '%s\n' "$line"
	if [ "$line" = "import (" ]; then
		printf '\t_ "github.com/awnumar/memguard"\n'
	fi
done < "$hardware_selftest_file" > "$hardware_probe"
write_overlay "$hardware_overlay" "$hardware_selftest_file" "$hardware_probe"

set +e
hardware_output=$(GOFLAGS=$(overlay_goflags "$hardware_overlay") GOCMD="$GOCMD" "$MAKE" -C "$root" secure-analyzer 2>&1)
hardware_rc=$?
set -e

if [ "$hardware_rc" -eq 0 ]; then
	printf 'secure-analyzer self-test: expected hardware-unit planted violation to fail make secure-analyzer, got rc=0\n' >&2
	printf '%s\n' "$hardware_output" >&2
	exit 1
fi

case "$hardware_output" in
	*"secure-analyzer[hardware-unit]"*) ;;
	*)
		printf 'secure-analyzer self-test: hardware-unit proof did not exercise the hardware-unit pass\n' >&2
		printf '%s\n' "$hardware_output" >&2
		exit 1
		;;
esac

case "$hardware_output" in
	*"import of \"github.com/awnumar/memguard\" is forbidden"*) ;;
	*)
		printf 'secure-analyzer self-test: hardware-unit proof did not report the planted memguard diagnostic\n' >&2
		printf '%s\n' "$hardware_output" >&2
		exit 1
		;;
esac

printf 'secure-analyzer self-test: hardware-unit planted violation failed make secure-analyzer with rc=%s\n' "$hardware_rc"

SECURE_ANALYZER_MODULES=go.mod \
SECURE_ANALYZER_PACKAGES=./... \
GOCMD="$GOCMD" \
	"$root/scripts/secure-analyzer.sh" self-test-clean "${SECURE_ANALYZER_SELFTEST_TAGS:-}" "${1:?usage: secure-analyzer_test.sh <vettool>}" >/dev/null

printf 'secure-analyzer self-test: planted violations failed with rc=%s and clean run passed\n' "$rc"
