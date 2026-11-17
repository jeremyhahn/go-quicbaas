#!/usr/bin/env bash
# devcontainer-build_test.sh (#2503): runs the real `make` DEVCONTAINER_BUILD
# path (make docker-build-toolchain-image, make docker-build) with a stub
# docker first on PATH, so the compose and plain docker build paths are proven
# without a daemon.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

bin="$work/bin"
mkdir -p "$bin"
cat > "$bin/docker" <<'EOF'
#!/usr/bin/env bash
echo "$*" >> "$STUB_LOG"
case $1 in
  compose)
    if [ "${2:-}" = version ]; then [ "${STUB_COMPOSE:-}" = 1 ]; exit; fi
    [ "${STUB_COMPOSE:-}" = 1 ] && exit 0
    echo "unknown shorthand flag: 'f' in -f" >&2
    exit 125 ;;
  image)
    case "$*" in *:toolchain-*) [ "${STUB_HAS_TAG:-}" = 1 ] && exit 0 ;; esac
    exit 1 ;;
  build) [ "${STUB_BUILD_FAIL:-}" = 1 ] && exit 1; exit 0 ;;
esac
exit 0
EOF
chmod +x "$bin/docker"
: > "$work/gomodauth.env"

fails=0
fail() { echo "FAIL: $*" >&2; fails=$((fails + 1)); }

# build <target>: runs make with the stub first on PATH; sets rc.
build() {
  PATH="$bin:$PATH" make -C "$root" --no-print-directory "$1" \
    MODULE_ENV_FILE="$work/gomodauth.env" > "$work/out" 2>&1 && rc=0 || rc=$?
}

newcase() {
  export STUB_LOG="$work/log-$1"
  : > "$STUB_LOG"
  unset STUB_COMPOSE STUB_HAS_TAG STUB_BUILD_FAIL
}

# lines <glob>: prints the stub log lines that match the glob.
lines() {
  local l
  while IFS= read -r l; do
    # shellcheck disable=SC2254
    case $l in $1) echo "$l" ;; esac
  done < "$STUB_LOG"
}

# (1) compose available: the compose build runs, no plain docker build.
newcase compose
export STUB_COMPOSE=1
build docker-build-toolchain-image
[ "$rc" = 0 ] || fail "(1) make exited $rc: $(tail -n 3 "$work/out")"
[ -n "$(lines 'compose -f build/docker/devcontainer/compose.yaml build toolchain')" ] \
  || fail "(1) no compose build: $(tr '\n' ';' < "$STUB_LOG")"
[ -z "$(lines 'build *')" ] || fail "(1) plain docker build ran"

# (2) compose missing: plain docker build with the Dockerfile, build args,
# secret, target and toolchain tag.
newcase plain
build docker-build-toolchain-image
[ "$rc" = 0 ] || fail "(2) make exited $rc: $(tail -n 3 "$work/out")"
b=$(lines 'build *')
for want in '-f build/docker/devcontainer/Dockerfile' '--build-arg GO_VERSION=' \
  '--build-arg GO_IMAGE_DIGEST=' '--build-arg GO_BUILD_PROXY=' \
  "--secret id=gomodauth,src=$work/gomodauth.env" '--target toolchain' \
  '-t quicbaas-devcontainer:toolchain-'; do
  case $b in *"$want"*) ;; *) fail "(2) docker build lacks '$want': '$b'" ;; esac
done
[ -z "$(lines 'compose * build*')" ] || fail "(2) compose build ran"

# (2b) compose missing, both images: the integration build gets its target
# and BUILD_TAGS.
newcase both
build docker-build
[ "$rc" = 0 ] || fail "(2b) make exited $rc: $(tail -n 3 "$work/out")"
[ -n "$(lines 'build *--target integration --build-arg BUILD_TAGS=*-t quicbaas-devcontainer:integration-*')" ] \
  || fail "(2b) no integration build: $(tr '\n' ';' < "$STUB_LOG")"

# (3) compose missing, the daemon holds the toolchain tag: no build.
newcase skip
export STUB_HAS_TAG=1
build docker-build-toolchain-image
[ "$rc" = 0 ] || fail "(3) make exited $rc: $(tail -n 3 "$work/out")"
[ -z "$(lines 'build *')" ] || fail "(3) docker build ran: $(lines 'build *')"

# (4) compose missing, the build fails: make fails.
newcase failing
export STUB_BUILD_FAIL=1
build docker-build-toolchain-image
[ "$rc" != 0 ] || fail "(4) make exited 0 on a failed docker build"

if [ "$fails" -ne 0 ]; then
  echo "devcontainer-build_test: $fails failure(s)" >&2
  exit 1
fi
echo "devcontainer-build_test: OK"
