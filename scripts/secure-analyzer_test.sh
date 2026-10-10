#!/bin/sh
set -eu

GOCMD=${GOCMD:-go}
MAKE=${MAKE:-make}
root=$(git rev-parse --show-toplevel)
selftest_dir="$root/.secure-analyzer-selftest"
hardware_selftest_file="$root/cmd/quicbaas/piv_local_pkcs11.go"
hardware_selftest_backup="$selftest_dir/piv_local_pkcs11.go.orig"

cleanup() {
	if [ -f "$hardware_selftest_backup" ]; then
		cp "$hardware_selftest_backup" "$hardware_selftest_file"
	fi
	rm -rf "$selftest_dir"
}
trap cleanup EXIT INT TERM

cleanup
mkdir -p "$selftest_dir"
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
	printf 'func borrowBuffer() error {\n'
	printf '\tbuf, err := secure.NewBufferFromBytes([]byte("probe"))\n'
	printf '\tif err != nil {\n'
	printf '\t\treturn err\n'
	printf '\t}\n'
	printf '\tdefer buf.Destroy()\n'
	printf '\t_ = buf.Bytes()\n'
	printf '\treturn nil\n'
	printf '}\n'
	printf '\n'
	printf 'func copyOut() error {\n'
	printf '\tbuf, err := secure.NewBufferFromBytes([]byte("probe"))\n'
	printf '\tif err != nil {\n'
	printf '\t\treturn err\n'
	printf '\t}\n'
	printf '\tdefer buf.Destroy()\n'
	printf '\tout := make([]byte, len(buf.Bytes()))\n'
	printf '\tcopy(out, buf.Bytes())\n'
	printf '\t_ = out\n'
	printf '\treturn nil\n'
	printf '}\n'
} > "$selftest_dir/selftest.go"

set +e
output=$(SECURE_ANALYZER_MODULES=go.mod \
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
assert_diagnostic buffer-bytes 'secure.Buffer.Bytes holds no borrow'
assert_diagnostic secure-copy 'copies protected bytes into ordinary memory'

cleanup

mkdir -p "$selftest_dir"
cp "$hardware_selftest_file" "$hardware_selftest_backup"
hardware_probe="$selftest_dir/piv_local_pkcs11.go"
while IFS= read -r line; do
	printf '%s\n' "$line"
	if [ "$line" = "import (" ]; then
		printf '\t_ "github.com/awnumar/memguard"\n'
	fi
done < "$hardware_selftest_file" > "$hardware_probe"
cp "$hardware_probe" "$hardware_selftest_file"

set +e
hardware_output=$(GOCMD="$GOCMD" "$MAKE" -C "$root" secure-analyzer 2>&1)
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
cleanup

SECURE_ANALYZER_MODULES=go.mod \
SECURE_ANALYZER_PACKAGES=./... \
GOCMD="$GOCMD" \
	"$root/scripts/secure-analyzer.sh" self-test-clean "${SECURE_ANALYZER_SELFTEST_TAGS:-}" "${1:?usage: secure-analyzer_test.sh <vettool>}" >/dev/null

printf 'secure-analyzer self-test: planted violations failed with rc=%s and clean run passed\n' "$rc"
