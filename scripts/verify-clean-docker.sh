#!/usr/bin/env bash
# verify-clean-docker.sh — pin the three halves of clean-docker's contract: it
# covers every image this repository creates under a name it writes literally,
# it removes those and nothing else, and it never reports success after a
# removal failed.
#
# This target used to be `docker system prune -af --volumes`, which on the
# shared CI runners deleted every unused image and named volume on the host and
# repeatedly destroyed another job's image (#3). The scope checks are what keep
# it from drifting back. The coverage check requires DOCKER_LOCAL_IMAGES to
# equal the inventory scripts/docker-image-inventory.sh derives from the build
# commands themselves — in both directions, because a tag the list holds but no
# build produces is another repository's, and reaching it is the failure #3 is
# about. The failure checks cover the shell trap that a `for` loop reports only
# its last command's status, so an early `docker rmi` failure followed by an
# absent tag would otherwise exit 0.
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

# Compares what the repository at $1 creates against what its cleanup removes,
# writing three files under $2:
#
#   created — the inventory, derived from the build commands rather than from
#             DOCKER_LOCAL_IMAGES, which is what lets this see a drift in it
#   missing — created but never cleaned: images left behind
#   extra   — cleaned but never created: tags outside this repository's own
#
# Returns non-zero if the inventory could not be derived at all. That is a
# failure in its own right: an inventory that quietly dropped a producer would
# read here as "nothing missing".
compare_coverage() {
    local root=$1 out=$2
    mkdir -p "$out"
    if ! ROOT="$root" bash "$ROOT/scripts/docker-image-inventory.sh" > "$out/created" 2> "$out/err"; then
        return 1
    fi
    sort -u -o "$out/created" "$out/created"
    make --no-print-directory -C "$root" print-DOCKER_LOCAL_IMAGES |
        tr ' ' '\n' | sed '/^$/d' | sort -u > "$out/cleaned"
    comm -23 "$out/created" "$out/cleaned" > "$out/missing"
    comm -13 "$out/created" "$out/cleaned" > "$out/extra"
}

# Writes a self-contained repository under $1 whose DOCKER_LOCAL_IMAGES is $2
# and whose sole build produces $3, plus a compose file that builds
# fixture-compose:latest and pulls redis:7-alpine. The build is spread over
# continuation lines because a line-at-a-time scan would miss it.
write_fixture() {
    local dir=$1 cleaned=$2 built=$3
    mkdir -p "$dir"
    {
        echo "DOCKER_IMAGE := fixture"
        echo "DOCKER_TAG := 9.9.9"
        echo "DOCKER_REGISTRY := registry.invalid"
        echo "DOCKER_LOCAL_IMAGES := $cleaned"
        echo
        echo "print-%:"
        printf '\t@echo "$($*)"\n'
        echo
        echo ".PHONY: fixture-build"
        echo "fixture-build:"
        printf '\tdocker build \\\n'
        printf '\t\t-f Dockerfile.fixture \\\n'
        printf '\t\t-t %s \\\n' "$built"
        printf '\t\t.\n'
    } > "$dir/Makefile"
    cat > "$dir/docker-compose.yml" <<'FIXTURE'
services:
  built:
    image: fixture-compose:latest
    build:
      context: .
  pulled:
    image: redis:7-alpine
FIXTURE
}

# The behaviour cases take their tags from the derived inventory rather than
# from DOCKER_LOCAL_IMAGES, so a tag added to that list without a producer
# cannot vouch for itself.
echo "case: cleanup covers exactly the images this repository creates"
if ! compare_coverage "$ROOT" "$WORK/real"; then
    fail "the image inventory could not be derived: $(tr '\n' ' ' < "$WORK/real/err")"
fi
if [ ! -s "$WORK/real/created" ]; then
    fail "derived no image identities at all — the inventory has stopped matching the build commands"
fi
if [ -s "$WORK/real/missing" ]; then
    fail "this repository creates images clean-docker leaves behind: $(tr '\n' ' ' < "$WORK/real/missing")"
    fail "add them to DOCKER_LOCAL_IMAGES in the Makefile"
fi
if [ -s "$WORK/real/extra" ]; then
    fail "clean-docker removes tags this repository does not create: $(tr '\n' ' ' < "$WORK/real/extra")"
    fail "cleanup may only reach this repository's own images — see #3"
fi

read -r -a our_tags <<< "$(tr '\n' ' ' < "$WORK/real/created")"
if [ "${#our_tags[@]}" -lt 2 ]; then
    echo "verify-clean-docker: expected the inventory to derive at least two images" >&2
    exit 1
fi
present_tag="${our_tags[0]}"
absent_tag="${our_tags[1]}"

# A build spread over continuation lines and a Compose service's explicit
# `image:` are both easy for a line-at-a-time scan to miss, and missing either
# would let the coverage check pass while cleanup leaves images behind. Neither
# is listed in this fixture, so both must come back as gaps.
echo "case: a multiline build and a compose image: tag are both seen as gaps"
write_fixture "$WORK/gaps" "" "fixture-multiline:latest"
if ! compare_coverage "$WORK/gaps" "$WORK/gaps-out"; then
    fail "the fixture inventory could not be derived: $(tr '\n' ' ' < "$WORK/gaps-out/err")"
fi
if ! grep -qx "fixture-multiline:latest" "$WORK/gaps-out/missing"; then
    fail "a 'docker build -t' spread over continuation lines was not derived"
fi
if ! grep -qx "fixture-compose:latest" "$WORK/gaps-out/missing"; then
    fail "the explicit image: of a building compose service was not derived"
fi
if grep -qx "redis:7-alpine" "$WORK/gaps-out/missing"; then
    fail "claimed an image the repository only pulls"
fi

# The other direction. A tag nothing here builds is another repository's, and
# cleanup reaching it is the whole failure #3 is about — so listing one has to
# fail verification, not be blessed by it.
echo "case: a cleanup tag this repository does not create is rejected"
write_fixture "$WORK/foreign" \
    "fixture-multiline:latest fixture-compose:latest someone-elses/image:latest" \
    "fixture-multiline:latest"
if ! compare_coverage "$WORK/foreign" "$WORK/foreign-out"; then
    fail "the fixture inventory could not be derived: $(tr '\n' ' ' < "$WORK/foreign-out/err")"
fi
if ! grep -qx "someone-elses/image:latest" "$WORK/foreign-out/extra"; then
    fail "a cleanup tag no build produces was not reported as out of scope"
fi
if [ -s "$WORK/foreign-out/missing" ]; then
    fail "reported a gap in a fixture that lists everything it builds: $(tr '\n' ' ' < "$WORK/foreign-out/missing")"
fi

# A producer named by a variable resolved at run time cannot be cleaned up by
# name, so it must break the inventory rather than drop out of it — otherwise
# an image that escapes cleanup reads here as "nothing missing".
echo "case: a build tag that cannot be resolved to a literal fails the inventory"
write_fixture "$WORK/unresolved" "" '"$$RUNTIME_TAG"'
if compare_coverage "$WORK/unresolved" "$WORK/unresolved-out"; then
    fail "an unresolvable build tag was silently dropped from the inventory"
fi

echo "case: only this project's tags, and only the ones the daemon reports"
printf '%s\n' "$present_tag" "someone-elses/image:latest" > "$WORK/images.out"
echo 0 > "$WORK/images.rc"
: > "$WORK/rmi.fail"
run_clean_docker
if [ "$RC" -ne 0 ]; then
    fail "a clean run exited $RC"
fi
if ! called "rmi $present_tag"; then
    fail "the project tag the daemon reported was not removed"
fi
if called "rmi $absent_tag"; then
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
# These tags are fixed, so a concurrent checkout on a shared runner can be using
# one. Forcing would untag it out from under that run.
if grep -qE "^rmi .*(-f|--force)" "$WORK/calls.log"; then
    fail "forced removal of a shared fixed tag"
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
echo "verify-clean-docker: clean-docker covers this project's images, only those, and fails closed"
