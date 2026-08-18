#!/usr/bin/env bash
# verify-clean-docker.sh — pin the three halves of clean-docker's contract: it
# covers every image this repository creates, it removes those and nothing else,
# and it never reports success after a removal failed.
#
# This target used to be `docker system prune -af --volumes`, which on the
# shared CI runners deleted every unused image and named volume on the host and
# repeatedly destroyed another job's image (#3). The scope checks are what keep
# it from drifting back. The coverage check is what keeps DOCKER_LOCAL_IMAGES
# from silently falling behind the build targets: it re-derives the inventory
# from the `docker build -t` / `docker tag` / `docker rmi` commands themselves,
# so a new image this repo starts producing fails the check until cleanup knows
# about it. The failure checks cover the shell trap that a `for` loop reports
# only its last command's status, so an early `docker rmi` failure followed by
# an absent tag would otherwise exit 0.
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

# Every image identity this repository creates, read back out of the commands
# that create it: each `-t` of a `docker build` / `buildx build`, each `docker
# tag` destination, and each literal argument of a `docker rmi`. Deriving it
# here rather than from DOCKER_LOCAL_IMAGES is the whole point — an omission
# from that list is exactly what this check has to be able to see.
#
# Make variables are expanded from make itself; a shell variable is resolved
# when the same file assigns it a literal. Anything left unresolved is a hard
# error, not a silent gap.
derive_created_images() {
    local file
    for file in Makefile scripts/*.sh; do
        [ "$file" = "scripts/verify-clean-docker.sh" ] && continue
        [ -f "$file" ] || continue
        awk \
            -v docker_image="$(make --no-print-directory print-DOCKER_IMAGE)" \
            -v docker_tag="$(make --no-print-directory print-DOCKER_TAG)" \
            -v docker_registry="$(make --no-print-directory print-DOCKER_REGISTRY)" \
            '
            function resolve(tok,   name) {
                gsub(/^["'"'"']|["'"'"']$/, "", tok)
                gsub(/\$\(DOCKER_IMAGE\)/, docker_image, tok)
                gsub(/\$\(DOCKER_TAG\)/, docker_tag, tok)
                gsub(/\$\(DOCKER_REGISTRY\)/, docker_registry, tok)
                if (tok ~ /^\$\{?[A-Za-z_][A-Za-z0-9_]*\}?$/) {
                    name = tok
                    gsub(/[$\{\}]/, "", name)
                    if (name in literal) tok = literal[name]
                }
                return tok
            }
            # A shell assignment of a bare literal, so `-t "$AGENT_IMAGE"` resolves.
            /^[A-Za-z_][A-Za-z0-9_]*=["'"'"']?[A-Za-z0-9._\/:-]+["'"'"']?$/ {
                eq = index($0, "=")
                value = substr($0, eq + 1)
                gsub(/^["'"'"']|["'"'"']$/, "", value)
                literal[substr($0, 1, eq - 1)] = value
            }
            # Comments describe these commands without running them.
            /^[ \t]*@?#/ { next }
            {
                n = split($0, tok, /[ \t]+/)
                if ($0 ~ /(docker|\$\(DOCKER\))[ \t]+(buildx[ \t]+)?build[ \t]/) {
                    for (i = 1; i < n; i++) if (tok[i] == "-t") print resolve(tok[i + 1])
                }
                if ($0 ~ /(docker|\$\(DOCKER\))[ \t]+tag[ \t]/) {
                    for (i = 1; i + 2 <= n; i++) if (tok[i] == "tag") { print resolve(tok[i + 2]); break }
                }
                if ($0 ~ /(docker|\$\(DOCKER\))[ \t]+rmi[ \t]/) {
                    seen = 0
                    for (i = 1; i <= n; i++) {
                        if (tok[i] == "rmi") { seen = 1; continue }
                        if (!seen || tok[i] ~ /^-/) continue
                        # `$$foo` is a runtime shell variable, never an identity.
                        if (tok[i] ~ /\$\$/) continue
                        if (tok[i] !~ /^["'"'"']?[A-Za-z0-9$._\/:{}-]+["'"'"']?$/) break
                        print resolve(tok[i])
                    }
                }
            }
            ' "$file"
    done | awk '{ if ($0 !~ /:/) $0 = $0 ":latest"; print }' | sort -u
}

# The tag list comes from the Makefile so the behaviour cases cannot drift from it.
read -r -a our_tags <<< "$(make --no-print-directory print-DOCKER_LOCAL_IMAGES)"
if [ "${#our_tags[@]}" -lt 2 ]; then
    echo "verify-clean-docker: expected DOCKER_LOCAL_IMAGES to list this project's tags" >&2
    exit 1
fi
present_tag="${our_tags[0]}"
absent_tag="${our_tags[1]}"

echo "case: cleanup covers every image this repository creates"
printf '%s\n' "${our_tags[@]}" | sort -u > "$WORK/cleaned.txt"
derive_created_images > "$WORK/created.txt"
if [ ! -s "$WORK/created.txt" ]; then
    fail "derived no image identities at all — the deriver has stopped matching the build commands"
fi
if grep -q '\$' "$WORK/created.txt"; then
    fail "could not resolve an image identity to a literal: $(grep '\$' "$WORK/created.txt" | tr '\n' ' ')"
fi
uncleaned=$(comm -23 "$WORK/created.txt" "$WORK/cleaned.txt")
if [ -n "$uncleaned" ]; then
    fail "this repository creates images that clean-docker leaves behind: $(echo "$uncleaned" | tr '\n' ' ')"
    fail "add them to DOCKER_LOCAL_IMAGES in the Makefile"
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
