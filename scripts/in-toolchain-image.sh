#!/usr/bin/env bash
# Run a command in the toolchain image against a COPY of the checkout.
#
#   usage: scripts/in-toolchain-image.sh <image> <command...>
#   e.g.   scripts/in-toolchain-image.sh go-enclave-toolchain:abc make build
#
# in-toolchain-template: v5 (ci-template v1.34.1; v4 was v1.29.4). Ships beside ci.yml; copy it to
# scripts/ with the workflow. From go-quicraft's scripts/in-toolchain-image.sh
# (a141c3c), with the container prefix and uid taken from the environment.
#
# How CI runs a stage in the devcontainer image (handbook ci-standard.md §2). A
# CI runner reaches its Docker daemon through a socket, so a bind mount would
# resolve on the daemon's host, where the checkout does not exist. This
# creates the container, streams the checkout into it (`.git` included: the
# releasability tests read it) owned by the image's non-root user, then starts
# it and returns the command's exit status. It works the same against a local
# daemon; on a workstation `make docker-run CMD=...` (bind mount) is the
# shorter path.
#
# The container keeps the integration service's posture where a copy-in allows
# it: every capability dropped, no-new-privileges, the same ulimits and pid
# bound, a non-root user, no network. The root filesystem is writable only
# because `docker cp` cannot write into a read-only one.
#
# IN_IMAGE_NETWORK=bridge gives the container a network, for the stages that
# must reach one: `make deps-check` asks the module proxy for the newest
# releases, and `make security` fetches the vulnerability databases. Only then
# are GOPROXY reopened, a writable module cache mounted (below; /tmp/gomodcache
# with CI_CACHE=off), and the forge credentials passed through, by NAME from
# this environment (the four variables dependency-resolution.md §6 has the
# runner inject), so no token ever appears on a command line.
#
# v2 (ci-template v1.22, Company Agent m616 item 2): the caches live in named
# volumes on the daemon's host (ci-standard.md §6), never in the container's
# writable layer or its 3 GB /tmp: GOCACHE at /cache/gobuild and
# GOLANGCI_LINT_CACHE at /cache/golangci for every stage, and, for a bridge
# stage, GOMODCACHE at /cache/gomod (a no-network stage keeps the image's own
# read-only module cache, so it still proves the image works offline). The
# volumes are <CI_STACK_PREFIX>cache-<class>-{gobuild,golangci,gomod}, one set
# per repo, trust class and daemon host, kept across jobs. CI_CACHE_CLASS
# (default pr) is `main` only for a push to main, so a pull request never
# writes a cache that main's build reads. A new volume's root is given to
# IN_IMAGE_UID by a helper container holding CHOWN and nothing else.
# scripts/trim-ci-caches.sh bounds their size. CI_CACHE=off mounts none (v1).
#
# v3 (ci-template v1.22.2, Company Agent m641 item 2 and m646 item 3): the
# stage runs in this sibling container, so the sibling holds the job slot's
# CPU bound. The job container's cgroup cpu.max gives it (rounded up; nproc
# when the job has no quota; IN_IMAGE_CPUS overrides), and the sibling gets
# --cpus N with GOMAXPROCS=N and GOFLAGS=-p=N. golangci-lint's default
# concurrency follows the container CPU quota, so lint takes N with no flag.
# A 4-CPU slot on the Dells gives 4; never a hardcoded number.
#
# v4 (ci-template v1.29.4, Company Agent ruling 261(c)):
# - The sibling holds the job slot's memory bound the same way. go-quicbaas
#   keeps its own form of it (IN_IMAGE_MEMORY below: the tightest memory.max
#   from the caller's cgroup up to the root, 8g when unbounded, `none` for no
#   cap), with --memory-swap set to the same size, so the stage gets no swap.
# - Go's build and link scratch is on disk, not the 3 GB /tmp tmpfs (go-xkms
#   #1733, 6d40166): `make coverage` links cgo test binaries in $WORK and filled
#   /tmp (ENOSPC "final link failed") in 8 go-xkms Devcontainer jobs on
#   2026-10-26. An anonymous volume on the image's /var/tmp (IN_IMAGE_SCRATCH
#   overrides the path) is TMPDIR and GOTMPDIR; it is on the daemon's data disk,
#   and cleanup's `docker rm -v` removes it. A scratch directory the image
#   lacks, or the run's user cannot write, fails the stage by name.
# - A bridge stage's GOPROXY puts DEVCACHE_GOPROXY (Athens, devops.md §2a)
#   first when the runner sets it: <DEVCACHE_GOPROXY>|https://proxy.golang.org,
#   so a miss or an unreachable cache falls through to the public proxy.
#   GOPRIVATE travels with it, so a private path never reaches either proxy.
#   Unset, the chain is https://proxy.golang.org. There is no `direct` entry
#   (v5, dependency-resolution.md, "No direct fetch, anywhere"); a private
#   path is fetched by GOPRIVATE/GONOPROXY, never through GOPROXY.
#
# The container is named <CI_STACK_PREFIX>tc-<pid>-<random>, under the prefix
# scripts/reap-stale-ci-stacks.sh reaps (default <checkout dir>-ci-, which in CI
# is <repo>-ci-), and is removed on every exit. IN_IMAGE_UID is the non-root
# uid the command runs as (default 10001; devcontainer.md §5: any non-root uid).
#
# IN_IMAGE_MEMORY caps the container's memory and swap (bytes, or a size with m
# or g, e.g. 16g; default the tightest memory.max from the caller's cgroup up to
# the cgroup root, or 8g when unbounded; none sets no cap, for GitHub-hosted
# runners, where the job owns the VM).
# IN_IMAGE_CGROUP_ROOT and IN_IMAGE_PROC_CGROUP name the cgroup v2 mount and the /proc/self/cgroup file the default reads (tests only).
# CI_CGROUP_PARENT, when set, is the container's --cgroup-parent.
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
