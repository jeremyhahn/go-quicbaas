#!/usr/bin/env bash
# ci-image-cache_test.sh (#2503): drives scripts/ci-image-cache.sh against stub
# docker and make binaries on PATH, so no daemon or registry is needed.
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
script="$here/ci-image-cache.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

bin="$work/bin"
mkdir -p "$bin"
cat > "$bin/make" <<'EOF'
#!/usr/bin/env bash
[ "$*" = "-s toolchain-image" ] && { echo "go-quicbaas-toolchain:abc123"; exit 0; }
exit 2
EOF
cat > "$bin/docker" <<'EOF'
#!/usr/bin/env bash
echo "$*" >> "$STUB_LOG"
case "$1 ${2:-}" in
  "image inspect")
    case "$*" in
      *--format*) echo "127.0.0.1:1/go-quicbaas-toolchain@sha256:feed"; exit 0 ;;
    esac
    [ "${STUB_HAS_LOCAL:-}" = 1 ] && exit 0
    exit 1 ;;
  "pull "*) [ "${STUB_PULL:-fail}" = ok ] && exit 0; exit 1 ;;
  "push "*) echo "push-config ${DOCKER_CONFIG:-}" >> "$STUB_LOG"
    [ "${STUB_PUSH:-fail}" = ok ] && exit 0; exit 1 ;;
  "tag "*) exit 0 ;;
esac
exit 0
EOF
chmod +x "$bin/make" "$bin/docker"

fails=0
fail() { echo "FAIL: $*" >&2; fails=$((fails + 1)); }

# run <args...>: runs the script with the stubs first on PATH; sets out and rc.
run() {
  out=$(cd "$work" && PATH="$bin:$PATH" "$script" "$@" 2>&1) && rc=0 || rc=$?
}

newcase() {
  export RUNNER_TEMP="$work/tmp-$1" STUB_LOG="$work/log-$1"
  mkdir -p "$RUNNER_TEMP"
  : > "$STUB_LOG"
  unset STUB_PULL STUB_PUSH STUB_HAS_LOCAL CI_IMAGE_REGISTRY GITHUB_SERVER_URL CI_IMAGE_NAMESPACE \
    CI_IMAGE_DOCKER_CONFIG
  # push runs on a push to main only; the refusal cases override these.
  export GITHUB_EVENT_NAME=push GITHUB_REF=refs/heads/main
}

ref=127.0.0.1:1/go-quicbaas-toolchain:abc123

# (a) no CI_IMAGE_REGISTRY on github.com: today's behaviour.
newcase a
export GITHUB_SERVER_URL=https://github.com
run pull
[ "$rc" = 0 ] || fail "(a) pull exited $rc"
[ "$out" = "ci-image-cache: no forge registry on this host; nothing to do" ] || fail "(a) pull printed '$out'"
run push
[ "$rc" = 0 ] || fail "(a) push exited $rc"
[ -z "$out" ] || fail "(a) push printed '$out'"
[ ! -s "$STUB_LOG" ] || fail "(a) docker was called: $(tr '\n' ';' < "$STUB_LOG")"

# (b) unreachable registry: pull and push fail, the job stays green.
newcase b
export CI_IMAGE_REGISTRY=127.0.0.1:1
run pull
[ "$rc" = 0 ] || fail "(b) pull exited $rc"
[ -z "$out" ] || fail "(b) pull printed '$out'"
run push
[ "$rc" = 0 ] || fail "(b) push exited $rc"
[ "$out" = "miss, built, push failed" ] || fail "(b) push printed '$out'"

# (b2) a miss whose push succeeds.
newcase b2
export CI_IMAGE_REGISTRY=127.0.0.1:1 STUB_PUSH=ok CI_IMAGE_DOCKER_CONFIG="$work/login-cfg"
run pull
run push
[ "$rc" = 0 ] || fail "(b2) push exited $rc"
[ "$out" = "miss, built and pushed $ref" ] || fail "(b2) push printed '$out'"
pushcfg=$(while read -r a b; do [ "$a" != push-config ] || echo "$b"; done < "$STUB_LOG")
[ "$pushcfg" = "$work/login-cfg" ] || fail "(b2) push used DOCKER_CONFIG '$pushcfg', not the logged-in one"

# (f) push refuses outside a push to main: a pull request, and another ref.
for ev in "pull_request refs/pull/1/merge" "push refs/heads/feature"; do
  newcase f
  export CI_IMAGE_REGISTRY=127.0.0.1:1 STUB_PUSH=ok
  export GITHUB_EVENT_NAME=${ev% *} GITHUB_REF=${ev#* }
  run push
  [ "$rc" = 0 ] || fail "(f $ev) push exited $rc"
  [ "$out" = "ci-image-cache: push runs on a push to main only; nothing pushed" ] || fail "(f $ev) push printed '$out'"
  [ ! -s "$STUB_LOG" ] || fail "(f $ev) push called docker: $(tr '\n' ';' < "$STUB_LOG")"
done

# (c) a hit: pull prints ref and digest, push afterwards pushes nothing.
newcase c
export CI_IMAGE_REGISTRY=127.0.0.1:1 STUB_PULL=ok STUB_PUSH=ok
run pull
[ "$rc" = 0 ] || fail "(c) pull exited $rc"
[ "$out" = "pulled $ref 127.0.0.1:1/go-quicbaas-toolchain@sha256:feed" ] || fail "(c) pull printed '$out'"
: > "$STUB_LOG"
run push
[ "$rc" = 0 ] || fail "(c) push exited $rc"
[ -z "$out" ] || fail "(c) push printed '$out'"
[ ! -s "$STUB_LOG" ] || fail "(c) push called docker: $(tr '\n' ';' < "$STUB_LOG")"

# (d) CI_IMAGE_REGISTRY is read before GITHUB_SERVER_URL.
newcase d
export CI_IMAGE_REGISTRY=127.0.0.1:1 GITHUB_SERVER_URL=https://forge.example:3000 CI_IMAGE_NAMESPACE=ns STUB_PULL=ok
run pull
[ "$rc" = 0 ] || fail "(d) pull exited $rc"
[ "$out" = "pulled $ref 127.0.0.1:1/go-quicbaas-toolchain@sha256:feed" ] || fail "(d) pull printed '$out'"
pulls=$(while read -r a _ c; do [ "$a" != pull ] || echo "$c"; done < "$STUB_LOG")
[ "$pulls" = "$ref" ] || fail "(d) pulled '$pulls', not $ref"

# (e) no credential on the registry path: docker login is never called.
for f in "$work"/log-*; do
  while read -r a _; do
    [ "$a" != login ] || fail "docker login called ($f)"
  done < "$f"
done

if [ "$fails" -ne 0 ]; then
  echo "ci-image-cache_test: $fails failure(s)" >&2
  exit 1
fi
echo "ci-image-cache_test: OK"
