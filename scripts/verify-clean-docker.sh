#!/usr/bin/env bash
# verify-clean-docker.sh — pin the two halves of clean-docker's contract: it
# removes this project's image tags and nothing else, and it never reports
# success after a removal failed.
#
# This target used to be `docker system prune -af --volumes`, which on the
# shared CI runners deleted every unused image and named volume on the host and
# repeatedly destroyed another job's image (#3). The scope checks are what keep
# it from drifting back. The failure checks cover the shell trap that a `for`
# loop reports only its last command's status, so an early `docker rmi` failure
# followed by an absent tag would otherwise exit 0.
#
# Docker is faked through PATH; no daemon, no images, no host state.

set -euo pipefail

ROOT=${ROOT:-$(cd "$(dirname "$0")/.." && pwd)}
cd "$ROOT"

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/bin"

export FAKE_DOCKER_DIR="$WORK"

# The fake reads its behaviour from files each case writes:
#   images.out — what `docker images` prints
#   images.rc  — the status it exits with
#   rmi.fail   — tags whose removal fails
# and appends every invocation to calls.log.
cat > "$WORK/bin/docker" <<'FAKE'
#!/usr/bin/env bash
echo "$*" >> "$FAKE_DOCKER_DIR/calls.log"
case "${1:-}" in
    images)
        cat "$FAKE_DOCKER_DIR/images.out" 2>/dev/null || true
        exit "$(cat "$FAKE_DOCKER_DIR/images.rc" 2>/dev/null || echo 0)"
        ;;
    rmi)
        for arg in "$@"; do
            if grep -qxF "$arg" "$FAKE_DOCKER_DIR/rmi.fail" 2>/dev/null; then
                echo "Error response from daemon: conflict: unable to remove $arg" >&2
                exit 1
            fi
        done
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
FAKE
chmod +x "$WORK/bin/docker"

failures=0

fail() {
    echo "  ✗ $1" >&2
    failures=$((failures + 1))
}

# Runs clean-docker against the fake docker; its status lands in RC.
run_clean_docker() {
    : > "$WORK/calls.log"
    set +e
    PATH="$WORK/bin:$PATH" make --no-print-directory clean-docker > "$WORK/out.log" 2>&1
    RC=$?
    set -e
}

called() {
    grep -qx "$1" "$WORK/calls.log"
}

# The tag list comes from the Makefile so the cases cannot drift from it.
read -r -a our_tags <<< "$(make --no-print-directory print-DOCKER_LOCAL_IMAGES)"
if [ "${#our_tags[@]}" -lt 2 ]; then
    echo "verify-clean-docker: expected DOCKER_LOCAL_IMAGES to list this project's tags" >&2
    exit 1
fi
present_tag="${our_tags[0]}"
absent_tag="${our_tags[1]}"

echo "case: only this project's tags, and only the ones the daemon reports"
printf '%s\n' "$present_tag" "someone-elses/image:latest" > "$WORK/images.out"
echo 0 > "$WORK/images.rc"
: > "$WORK/rmi.fail"
run_clean_docker
if [ "$RC" -ne 0 ]; then
    fail "a clean run exited $RC"
fi
if ! called "rmi -f $present_tag"; then
    fail "the project tag the daemon reported was not removed"
fi
if called "rmi -f $absent_tag"; then
    fail "removed a tag the daemon never reported as present"
fi
if grep -q "someone-elses/image" "$WORK/calls.log"; then
    fail "touched an image that is not this project's"
fi
if grep -q "system prune" "$WORK/calls.log"; then
    fail "pruned the host — see #3"
fi
if grep -qE "compose .*down|volume rm|network rm" "$WORK/calls.log"; then
    fail "tore down resources beyond this project's image tags"
fi

echo "case: an early removal failure survives a later absent tag"
printf '%s\n' "$present_tag" > "$WORK/images.out"
echo 0 > "$WORK/images.rc"
printf '%s\n' "$present_tag" > "$WORK/rmi.fail"
run_clean_docker
if [ "$RC" -eq 0 ]; then
    fail "reported success after 'docker rmi' failed"
fi

echo "case: an unreachable daemon is not read as 'no such image'"
: > "$WORK/images.out"
echo 1 > "$WORK/images.rc"
: > "$WORK/rmi.fail"
run_clean_docker
if [ "$RC" -eq 0 ]; then
    fail "reported success after 'docker images' failed"
fi
if grep -q "^rmi" "$WORK/calls.log"; then
    fail "removed images without knowing which exist"
fi

if [ "$failures" -ne 0 ]; then
    echo "verify-clean-docker: $failures check(s) failed" >&2
    exit 1
fi
echo "verify-clean-docker: clean-docker is scoped to this project and fails closed"
