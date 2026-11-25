#!/usr/bin/env bash
# reap-stale-ci-stacks_test.sh: drives scripts/reap-stale-ci-stacks.sh against
# a stub docker on PATH, so no daemon is needed, and checks that the CI
# Integration job tears its stack down through the integration compose file.
#
# The leak it covers: a cancelled CI run's Integration teardown named the
# devcontainer compose file, so `down --volumes` removed the project's
# containers but none of the integration file's named volumes. The reaper
# found projects only through their containers, so those volumes stayed on
# the runner host for good (61 on one host, from 21 cancelled runs).
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/.." && pwd)
script="$here/reap-stale-ci-stacks.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

bin="$work/bin"
mkdir -p "$bin"
# The stub reads its world from files in $STUB_DIR:
#   containers: "<id> <project> <status> <finished-at>" per line ("-" = no project)
#   volumes:    "<name> <project> <created-at>" per line ("-" = anonymous)
cat > "$bin/docker" <<'EOF'
#!/usr/bin/env bash
echo "$*" >> "$STUB_DIR/log"
C="$STUB_DIR/containers"; V="$STUB_DIR/volumes"
touch "$C" "$V"
label_value() { case "$1" in label=com.docker.compose.project=*) echo "${1#label=com.docker.compose.project=}" ;; esac; }
case "$1" in
  ps)
    shift; quiet=0; want=; fmt=
    while [ $# -gt 0 ]; do
      case "$1" in
        -a) ;; -q) quiet=1 ;; -aq) quiet=1 ;;
        --filter) shift; v=$(label_value "$1"); [ -n "$v" ] && want=$v
          [ "$1" = label=com.docker.compose.project ] && want='*' ;;
        --format) shift; fmt=$1 ;;
      esac
      shift
    done
    while read -r id project status finished; do
      [ -n "$id" ] || continue
      case $want in
        '') ;;
        '*') [ "$project" != - ] || continue ;;
        *) [ "$project" = "$want" ] || continue ;;
      esac
      if [ "$quiet" = 1 ]; then echo "$id"
      elif [ -n "$fmt" ] && [ "$want" = '*' ]; then echo "$project"
      fi
    done < "$C"
    ;;
  inspect)
    id=${!#}
    while read -r cid project status finished; do
      [ "$cid" = "$id" ] && { echo "$status $finished $finished"; exit 0; }
    done < "$C"
    exit 1 ;;
  volume)
    case "$2" in
      ls)
        shift 2; want=; fmt=
        while [ $# -gt 0 ]; do
          case "$1" in
            --filter) shift; v=$(label_value "$1"); [ -n "$v" ] && want=$v
              [ "$1" = label=com.docker.compose.project ] && want='*' ;;
            --format) shift; fmt=$1 ;;
          esac
          shift
        done
        while read -r name project created; do
          [ -n "$name" ] || continue
          case $want in
            '') ;;
            '*') [ "$project" != - ] || continue ;;
            *) [ "$project" = "$want" ] || continue ;;
          esac
          if [ -n "$fmt" ]; then echo "$name $project"; else echo "$name"; fi
        done < "$V"
        ;;
      inspect)
        name=${!#}
        while read -r vname project created; do
          [ "$vname" = "$name" ] && { echo "$created"; exit 0; }
        done < "$V"
        exit 1 ;;
      rm) exit 0 ;;
    esac
    ;;
  network) [ "$2" = ls ] && exit 0; exit 0 ;;
  images) exit 0 ;;
  rm) exit 0 ;;
esac
exit 0
EOF
chmod +x "$bin/docker"

fails=0
fail() { echo "FAIL: $*" >&2; fails=$((fails + 1)); }

old=$(date -u -d '-2 days' +%Y-%m-%dT%H:%M:%SZ)
new=$(date -u -d '-5 minutes' +%Y-%m-%dT%H:%M:%SZ)

# reap: runs the reaper in dry-run mode against the stub world in $STUB_DIR.
reap() {
  out=$(PATH="$bin:$PATH" STUB_DIR="$STUB_DIR" REAP_DRY_RUN=1 bash "$script" go-quicbaas-ci- 2>&1) || fail "reaper exited non-zero: $out"
}

# 1. A project whose containers are gone: its old volumes are reaped.
STUB_DIR="$work/w1"; mkdir -p "$STUB_DIR"
printf '%s\n' \
  "go-quicbaas-ci-100-1_quicbaas-certs go-quicbaas-ci-100-1 $old" \
  "go-quicbaas-ci-100-1_integration-test-results go-quicbaas-ci-100-1 $old" \
  > "$STUB_DIR/volumes"
reap
for v in go-quicbaas-ci-100-1_quicbaas-certs go-quicbaas-ci-100-1_integration-test-results; do
  case $out in *"dry   docker volume rm $v"*) ;; *) fail "case 1: orphaned volume $v was not reaped: $out" ;; esac
done

# 2. A project volume created within min-age is kept: its run may be starting.
STUB_DIR="$work/w2"; mkdir -p "$STUB_DIR"
echo "go-quicbaas-ci-200-1_quicbaas-certs go-quicbaas-ci-200-1 $new" > "$STUB_DIR/volumes"
reap
case $out in *"volume rm go-quicbaas-ci-200-1_quicbaas-certs"*) fail "case 2: a fresh volume was reaped: $out" ;; esac

# 3. Another repository's volumes and anonymous volumes are never touched.
STUB_DIR="$work/w3"; mkdir -p "$STUB_DIR"
printf '%s\n' \
  "qrdb-ci-300-1_data qrdb-ci-300-1 $old" \
  "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef - $old" \
  > "$STUB_DIR/volumes"
reap
case $out in *"volume rm"*) fail "case 3: a volume outside the prefix was reaped: $out" ;; esac

# 4. A project with a running container keeps its volumes, however old.
STUB_DIR="$work/w4"; mkdir -p "$STUB_DIR"
echo "c4 go-quicbaas-ci-400-1 running $old" > "$STUB_DIR/containers"
echo "go-quicbaas-ci-400-1_quicbaas-certs go-quicbaas-ci-400-1 $old" > "$STUB_DIR/volumes"
reap
case $out in *"volume rm go-quicbaas-ci-400-1_quicbaas-certs"*) fail "case 4: a live project's volume was reaped: $out" ;; esac

# 5. The CI Integration job tears down through the integration compose file.
ci="$root/.github/workflows/ci.yml"
td=$(sed -n '/^  integration:/,/^  [a-z][a-z-]*:$/p' "$ci" | sed -n '/- name: Tear down/,/^      - /p')
case $td in
  *'make cluster-down-integration IT_PROJECT="$IT_PROJECT"'*) ;;
  *) fail "case 5: the Integration job's Tear down does not run make cluster-down-integration with its IT_PROJECT" ;;
esac
case $td in
  *devcontainer/compose.yaml*) fail "case 5: the Integration job's Tear down still names the devcontainer compose file" ;;
esac
dn=$(cd "$root" && make -s -n cluster-down-integration IT_PROJECT=go-quicbaas-ci-500-1 2>/dev/null) || fail "case 5: make -n cluster-down-integration failed"
case $dn in
  *"-p go-quicbaas-ci-500-1 -f test/integration/docker-compose.integration.yml down --volumes"*) ;;
  *) fail "case 5: cluster-down-integration does not take the job's project down with its volumes: $dn" ;;
esac

if [ "$fails" -ne 0 ]; then
  echo "reap-stale-ci-stacks_test: $fails failure(s)" >&2
  exit 1
fi
echo "reap-stale-ci-stacks_test: ok"
