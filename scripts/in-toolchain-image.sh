#!/usr/bin/env bash
# Run a command in the toolchain image against a copy of the checkout.
set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: $0 <image> <command...>" >&2
  exit 2
fi
image=$1
shift
network=${IN_IMAGE_NETWORK:-none}
case $network in
  none | bridge) ;;
  *) echo "IN_IMAGE_NETWORK must be none or bridge: $network" >&2; exit 2 ;;
esac

uid=${IN_IMAGE_UID:-10001}
case $uid in
  "" | 0 | *[!0-9]*) echo "IN_IMAGE_UID must be a non-root numeric uid: $uid" >&2; exit 2 ;;
esac
prefix=${CI_STACK_PREFIX:-$(basename "$PWD")-ci-}
name="${prefix}tc-$$-${RANDOM}"
cache=${CI_CACHE:-on}
case $cache in
  on | off) ;;
  *) echo "CI_CACHE must be on or off: $cache" >&2; exit 2 ;;
esac
class=${CI_CACHE_CLASS:-pr}
case $class in
  pr | main) ;;
  *) echo "CI_CACHE_CLASS must be pr or main: $class" >&2; exit 2 ;;
esac
cpus=${IN_IMAGE_CPUS:-}
if [ -z "$cpus" ]; then
  quota=max period=100000
  if [ -r /sys/fs/cgroup/cpu.max ]; then read -r quota period < /sys/fs/cgroup/cpu.max; fi
  case $quota in
    max | "" | *[!0-9]*) cpus=$(nproc) ;;
    *) cpus=$(((quota + period - 1) / period)) ;;
  esac
fi
case $cpus in
  "" | 0 | *[!0-9]*) echo "IN_IMAGE_CPUS must be a positive integer: $cpus" >&2; exit 2 ;;
esac
# cgroup_memory_limit prints the tightest numeric memory.max from the
# caller's cgroup v2 path (/proc/self/cgroup's 0:: line) up to the cgroup
# root, or nothing when every level is unbounded or unreadable. A path that
# leaves the cgroup namespace (/..) reads the root only.
cgroup_memory_limit() {
  local root=${IN_IMAGE_CGROUP_ROOT:-/sys/fs/cgroup} self=${IN_IMAGE_PROC_CGROUP:-/proc/self/cgroup}
  local path="" line dir value best=""
  if [ -r "$self" ]; then
    while IFS= read -r line || [ -n "$line" ]; do
      case $line in 0::*) path=${line#0::} ;; esac
    done < "$self"
  fi
  case $path in /*) ;; *) path=/ ;; esac
  case $path in */.. | */../*) path=/ ;; esac
  dir=${path%/}
  while :; do
    value=""
    if [ -r "$root$dir/memory.max" ]; then read -r value < "$root$dir/memory.max" || true; fi
    case $value in
      "" | max | *[!0-9]*) ;;
      *) if [ -z "$best" ] || [ "$value" -lt "$best" ]; then best=$value; fi ;;
    esac
    [ -n "$dir" ] || break
    dir=${dir%/*}
  done
  printf '%s' "$best"
}
memory=${IN_IMAGE_MEMORY:-}
if [ -z "$memory" ]; then
  memory=$(cgroup_memory_limit)
  [ -n "$memory" ] || memory=8g
fi
if [ "$memory" != none ] && ! [[ $memory =~ ^[1-9][0-9]*[mg]?$ ]]; then
  echo "IN_IMAGE_MEMORY must be a size in bytes, m or g, e.g. 16g: $memory" >&2
  exit 2
fi
scratch=${IN_IMAGE_SCRATCH:-/var/tmp}
case $scratch in
  /*) ;;
  *) echo "IN_IMAGE_SCRATCH must be an absolute path: $scratch" >&2; exit 2 ;;
esac
# The daemon refuses --cpus above its own CPU count, and the job container can
# see more CPUs than the daemon that runs the stage (a pooled runner host), so
# the bound is capped at the daemon's NCPU when the daemon reports one. It
# runs after every argument check, so a bad value fails before Docker is asked.
if ! daemon_cpus=$(docker info --format '{{.NCPU}}' 2>/dev/null); then daemon_cpus=; fi
case $daemon_cpus in
  "" | 0 | *[!0-9]*) ;;
  *) if [ "$cpus" -gt "$daemon_cpus" ]; then cpus=$daemon_cpus; fi ;;
esac
args=(
  --name "$name"
  --cpus "$cpus"
  --env GOMAXPROCS="$cpus"
  --env GOFLAGS="-p=$cpus"
  --init
  --cap-drop ALL
  --security-opt no-new-privileges:true
  --pids-limit 4096
  --ulimit memlock=67108864:67108864
  --ulimit core=1048576:1048576
  --ulimit nofile=65536:65536
  --tmpfs "/tmp:rw,exec,nosuid,nodev,size=3g,mode=1777"
  # Go's build and link scratch on disk, not the tmpfs (v4, above).
  -v "$scratch"
  --env TMPDIR="$scratch"
  --env GOTMPDIR="$scratch"
  --network "$network"
  --user "$uid:$uid"
  --workdir /src
  --env HOME=/tmp
  --env CI="${CI:-}"
)
if [ "$memory" != none ]; then args+=(--memory "$memory" --memory-swap "$memory"); fi
# The sidecar is then charged to the runner's slice, not the daemon root.
if [ -n "${CI_CGROUP_PARENT:-}" ]; then args+=(--cgroup-parent "$CI_CGROUP_PARENT"); fi
modcache=/tmp/gomodcache
if [ "$cache" = on ]; then
  vols="${prefix}cache-${class}"
  docker run --rm --network none --cap-drop ALL --cap-add CHOWN \
    --security-opt no-new-privileges:true --user 0:0 \
    -v "$vols-gobuild:/c/gobuild" -v "$vols-golangci:/c/golangci" -v "$vols-gomod:/c/gomod" \
    "$image" sh -c "for d in /c/*; do [ \"\$(stat -c %u:%g \"\$d\")\" = $uid:$uid ] || chown $uid:$uid \"\$d\"; done" >/dev/null
  args+=(
    -v "$vols-gobuild:/cache/gobuild" --env GOCACHE=/cache/gobuild
    -v "$vols-golangci:/cache/golangci" --env GOLANGCI_LINT_CACHE=/cache/golangci
  )
  if [ "$network" = bridge ]; then
    args+=(-v "$vols-gomod:/cache/gomod")
    modcache=/cache/gomod
  fi
fi
if [ "$network" = bridge ]; then
  # The baked module cache is read-only to the run's user and holds only the
  # root module's build list; the nested modules deps-check walks fetch more.
  # Athens first when the runner sets DEVCACHE_GOPROXY (v4, above), and no
  # `direct` entry (v5). go-quicbaas: no forge credential reaches a job (the
  # jobs refuse injected GIT_CONFIG_*), so none is passed to the stage.
  args+=(--env "GOPROXY=${DEVCACHE_GOPROXY:+$DEVCACHE_GOPROXY|}https://proxy.golang.org" --env "GOMODCACHE=$modcache")
  # ci-template v1.36: the Trivy database repositories the runner sets (the
  # dev-cache ghcr mirror on the LAN first, the public ones after) travel
  # with the stage, so `make security` and `trivy image` fetch the vulnerability
  # and Java databases from the LAN instead of the internet.
  for v in TRIVY_DB_REPOSITORY TRIVY_JAVA_DB_REPOSITORY TRIVY_INSECURE; do
    if [ -n "${!v:-}" ]; then args+=(--env "$v"); fi
  done
fi

cleanup() { docker rm -f -v "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT

# A scratch directory the image lacks, or the run's user cannot write, fails
# the stage here by name, not later as a tool's ENOSPC or EACCES.
# shellcheck disable=SC2016 # expands in the container, not here
guard='[ -d "$TMPDIR" ] && [ -w "$TMPDIR" ] || { echo "in-toolchain-image: scratch directory $TMPDIR is missing or not writable" >&2; exit 2; }'
docker create "${args[@]}" "$image" sh -c "$guard; $*" >/dev/null
tar -C "$PWD" --owner="$uid" --group="$uid" --numeric-owner -cf - . |
  docker cp -a - "$name:/src"
docker start -a "$name"
