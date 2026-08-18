#!/usr/bin/env bash
# verify-integration-teardown.sh — prove that `make integration-test` tears its
# Compose stack down even when it leaves early.
#
# The Integration workflow used to run `sudo docker system prune -af --volumes`
# on the shared runner, which reclaimed this job's leftovers by deleting every
# unused image and named volume on the host — including other repositories'
# (#3). That prune is gone, so nothing behind this job collects what it leaves:
# the stack has to take itself down on every exit path, not only on the one
# where the tests actually ran. The recipe arms an EXIT trap before it creates
# anything, and this pins that: a build that fails still ends with a
# `compose down --volumes --remove-orphans`.
#
# Docker and nvidia-smi are faked through PATH, so this decides in a second or
# two without a daemon, a GPU or a single container.

set -euo pipefail

ROOT=${ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
cd "$ROOT"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

export FAKE_DOCKER_DIR="$WORK"

# Logs every invocation and fails whatever `fail.match` names, so a case can
# choose which step of the recipe breaks. `compose version` must succeed or the
# recipe stops at its preflight check.
cat > "$WORK/bin/docker" <<'FAKE'
#!/usr/bin/env bash
echo "$*" >> "$FAKE_DOCKER_DIR/calls.log"
if [ -s "$FAKE_DOCKER_DIR/fail.match" ] && grep -qF "$(cat "$FAKE_DOCKER_DIR/fail.match")" <<< "$*"; then
    echo "fake docker: refusing '$*'" >&2
    exit 1
fi
exit 0
FAKE
chmod +x "$WORK/bin/docker"

# Present but non-functional, which is what the recipe's own GPU probe expects
# of a host without one. Without this the outcome would depend on whether the
# runner happens to have a GPU.
cat > "$WORK/bin/nvidia-smi" <<'FAKE'
#!/usr/bin/env bash
exit 1
FAKE
chmod +x "$WORK/bin/nvidia-smi"

# The recipe backs go.mod up to fixed paths under /tmp. Leave whatever was
# there as it was.
restore=()
for f in /tmp/quicbaas-gomod-backup /tmp/quicbaas-sdk-gomod-backup; do
    [ -e "$f" ] || restore+=("$f")
done
cleanup_tmp() {
    local f
    for f in "${restore[@]+"${restore[@]}"}"; do
        rm -f "$f"
    done
}
trap 'cleanup_tmp; rm -rf "$WORK"' EXIT

failures=0

fail() {
    echo "  ✗ $1" >&2
    failures=$((failures + 1))
}

# The line number of the last call matching $1, or empty when there is none.
last_call() {
    grep -n -- "$1" "$WORK/calls.log" | tail -1 | cut -d: -f1
}

echo "case: a failed image build still brings the stack down"
: > "$WORK/calls.log"
echo "build --no-cache" > "$WORK/fail.match"
set +e
PATH="$WORK/bin:$PATH" make --no-print-directory integration-test \
    GOMODAUTH_ENV="$WORK/gomodauth.env" > "$WORK/out.log" 2>&1
rc=$?
set -e

if [ "$rc" -eq 0 ]; then
    fail "reported success after the image build failed"
fi

build_line=$(last_call "build --no-cache")
if [ -z "$build_line" ]; then
    fail "the recipe never reached the image build — this check is no longer exercising it"
    echo "--- recipe output ---" >&2
    cat "$WORK/out.log" >&2
else
    teardown_line=$(awk -v start="$build_line" \
        'NR > start && /down --volumes --remove-orphans/ { print NR; exit }' "$WORK/calls.log")
    if [ -z "$teardown_line" ]; then
        fail "the stack was left up after the build failed — teardown must not depend on reaching the end of the recipe (#3)"
        echo "--- docker calls ---" >&2
        cat "$WORK/calls.log" >&2
    fi
fi

if grep -q "system prune" "$WORK/calls.log"; then
    fail "pruned the host — see #3"
fi

if [ "$failures" -ne 0 ]; then
    echo "verify-integration-teardown: $failures check(s) failed" >&2
    exit 1
fi
echo "verify-integration-teardown: the integration stack is torn down on the early-exit path too"
