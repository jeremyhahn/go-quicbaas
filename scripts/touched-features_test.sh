#!/bin/sh
# touched-features_test.sh proves scripts/touched-features.sh names the
# features a change touched, and scripts/check-feature-map.sh finds an orphan
# integration test, a feature with no target and a stale path. Both run
# against a temporary git repository with a small feature mapping table; then
# touched-features.sh runs once on this repository.
#
# Usage: scripts/touched-features_test.sh (make check-feature-map runs it).
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
		-c commit.gpgsign=false commit -q --allow-empty-message -m "$1"
	tgit rev-parse HEAD
}

tab=$(printf '\t')
# makefile [LINE...] writes the fixture Makefile, then each extra LINE.
makefile() {
	{
		printf '%s\n' \
			'INTEGRATION_FEATURES := alpha beta' \
			'FEATURE_PATHS_alpha := test/integration/alpha/ pkg/alpha/' \
			'FEATURE_PATHS_beta := test/integration/cli/beta_test.go' \
			'INTEGRATION_SHARED_PATHS := test/integration/helpers/' \
			'INTEGRATION_DOC_PATHS := docs/ *.md' \
			'print-feature-map:' \
			"$tab"'@$(foreach f,$(INTEGRATION_FEATURES),echo '"'"'$(f) $(strip $(FEATURE_PATHS_$(f)))'"'"';)' \
			'print-%:' \
			"$tab"'@echo "$($*)"' \
			'integration-test-beta:' \
			"$tab"'@echo beta' \
			'integration-test-%:' \
			"$tab"'@echo $*'
		for line in "$@"; do
			printf '%s\n' "$line"
		done
	} >"$tree/Makefile"
}

# run SCRIPT ARG... runs a script under test in the temporary repository.
run() {
	script=$1
	shift
	(cd "$tree" && GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 sh "scripts/$script" "$@")
}

# features BASE TIP WANT checks touched-features.sh prints exactly WANT.
features() {
	got=$(run touched-features.sh "$1" "$2" 2>/dev/null) || got="exit $?"
	if [ "$got" != "$3" ]; then
		fail "touched-features $1 $2: got '$got', want '$3'"
	fi
}

# refused ARG... checks touched-features.sh exits non-zero with no output.
refused() {
	code=0
	got=$(run touched-features.sh "$@" 2>/dev/null) || code=$?
	if [ "$code" -eq 0 ] || [ -n "$got" ]; then
		fail "touched-features $*: exit $code, output '$got', want a refusal"
	fi
}

# check WANT TEXT checks check-feature-map.sh exits WANT and prints TEXT.
check() {
	code=0
	out=$(run check-feature-map.sh 2>&1) || code=$?
	if [ "$code" != "$1" ]; then
		fail "check-feature-map: exit $code, want $1: $out"
	fi
	case $out in
	*"$2"*) ;;
	*) fail "check-feature-map: want '$2' in: $out" ;;
	esac
}

tgit init -q
mkdir -p "$tree/scripts" "$tree/docs" "$tree/pkg/alpha" "$tree/pkg/shared" \
	"$tree/pkg/alpha/prompts" "$tree/pkg/shared/prompts" \
	"$tree/test/integration/alpha" "$tree/test/integration/cli" \
	"$tree/test/integration/helpers" "$tree/.github/workflows"
for s in touched-features.sh touched-packages.sh check-feature-map.sh; do
	cp "$root/scripts/$s" "$tree/scripts/$s"
done
for f in docs/a.md README.md pkg/alpha/a.go pkg/shared/s.go \
	test/integration/alpha/a_test.go test/integration/cli/beta_test.go \
	test/integration/helpers/h.go .github/workflows/ci.yml \
	pkg/alpha/prompts/p.md pkg/shared/prompts/p.md; do
	echo 0 >"$tree/$f"
done
makefile
prev=$(commit base)

# step FILE... changes each FILE, commits, and leaves the commit in next.
step() {
	for f in "$@"; do
		echo "# $prev" >>"$tree/$f"
	done
	next=$(commit "$*")
}

step docs/a.md README.md
features "$prev" "$next" ''
features "$next" "$next" ''
prev=$next

for f in Makefile .github/workflows/ci.yml pkg/shared/s.go test/integration/helpers/h.go; do
	step "$f"
	features "$prev" "$next" ALL
	prev=$next
done

step pkg/alpha/a.go
features "$prev" "$next" alpha
prev=$next

# An embedded prompt is not documentation: it belongs to its package's feature.
step pkg/alpha/prompts/p.md
features "$prev" "$next" alpha
prev=$next

# An embedded prompt no row owns runs every feature.
step pkg/shared/prompts/p.md
features "$prev" "$next" ALL
prev=$next

step pkg/alpha/a.go test/integration/cli/beta_test.go docs/a.md
features "$prev" "$next" "alpha
beta"
prev=$next

refused "$prev" HEAD:Makefile
refused nosuchrevision "$prev"
refused -p "$prev"
refused "$prev"

cp "$tree/Makefile" "$tree/Makefile.keep"
printf 'all:\n' >"$tree/Makefile"
refused "$prev~1" "$prev"
mv "$tree/Makefile.keep" "$tree/Makefile"

check 0 'ok'
mkdir -p "$tree/test/integration/gamma"
echo 0 >"$tree/test/integration/gamma/g_test.go"
tgit add test/integration/gamma/g_test.go
check 1 'test/integration/gamma/g_test.go: in no feature'
tgit rm -q --cached test/integration/gamma/g_test.go
rm -r "$tree/test/integration/gamma"
cp "$tree/Makefile" "$tree/Makefile.keep"
makefile 'INTEGRATION_FEATURES += delta' 'FEATURE_PATHS_delta := pkg/alpha/'
check 1 'delta: no integration-test-delta rule'
makefile 'FEATURE_PATHS_alpha += pkg/nope/'
check 1 'alpha: path pkg/nope/ matches no tracked file'
mv "$tree/Makefile.keep" "$tree/Makefile"
check 0 'ok'

mkdir -p "$tree/pkg/other"
tgit mv pkg/alpha/a.go pkg/other/a.go
next=$(commit move)
features "$prev" "$next" ALL

got=$(cd "$root" && sh scripts/touched-features.sh HEAD HEAD) || got="exit $?"
if [ -n "$got" ]; then
	fail "touched-features HEAD HEAD on this repository: got '$got', want nothing"
fi

if [ "$failures" -ne 0 ]; then
	echo "touched-features_test.sh: $failures failure(s)"
	exit 1
fi
echo "touched-features_test.sh: ok"
