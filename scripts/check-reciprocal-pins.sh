#!/usr/bin/env bash
#
# VENDORED COPY. The canonical script is
# software/scripts/check-reciprocal-pins.sh in the automatethethings repo;
# this is a verbatim copy, kept here because Gitea runners check out only
# this repository and cannot reach the company repo at CI time.
#
# Keep the body byte-identical to the canonical one. If the canonical script
# changes, re-copy rather than edit — a locally "improved" copy is how an
# enforcement mechanism starts disagreeing with the rule it enforces.
#
# Wired into `make check-reciprocal-pins`, which the CI lint job runs. It was
# unwired here until 2026-08-20, which is why go-qrdb v0.7/v0.8 skew between
# the root and sdk/go modules survived from 32389ca until a review round
# found it: the mechanism existed and nothing ran it.
#
# THE ONE SHELL SCRIPT LEFT UNDER scripts/, AND DELIBERATELY SO. go-quicbaas#942
# converted the other seven gates to Go on the directive that a Go product's
# gates be Go. This one was not converted, because converting it HERE would fork
# the canonical, and the value of this gate is that every repository in the
# portfolio enforces the identical rule: a rewrite differing even slightly in
# tag-namespace detection or ignore-file glob semantics would make this repo's
# answer differ from go-xkms's, silently -- which is what the paragraph above
# warns about. The canonical has to move first. That is filed as handbook#51,
# and closing it closes the last item of go-quicbaas#942.
# check-reciprocal-pins.sh — fail when a repo's own modules pin each other at a
# version other than the one in VERSION.
#
#   ./check-reciprocal-pins.sh [repo-root]        # defaults to $PWD
#
# WHY THIS EXISTS
# A multi-module repo publishes its modules in lockstep, and each one requires
# the others. In-repo builds never notice when those requires go stale, because
# a `replace => ../..` or `=> ./sdk/go` satisfies them locally. The mismatch is
# invisible until an OUTSIDE consumer resolves the tag — at which point it gets
# the new root code paired with an old sibling module.
#
# Measured instances (2026-08-19), all found the hard way:
#   - go-xkms  v0.7.4-alpha handed consumers go-xkms v0.7.1-alpha, three
#     releases behind what they asked for.
#   - go-quicbaas PR #41 had it in BOTH directions at once: root required
#     sdk/go v0.2.1-alpha behind `replace => ./sdk/go`, and sdk/go required
#     root v0.2.1-alpha behind `replace => ../..`. The agent fixed one and
#     walked past the other; Codex caught the mirror.
#   - go-qrdb fixed its pair in 5113dc5 after shipping a bad tag.
#
# THE TRAP THAT CATCHES PEOPLE FIXING IT: the correct value is the version
# ABOUT TO BE PUBLISHED, not the last one released. Pinning the previous
# release still hands consumers a stale sibling. This script compares against
# VERSION precisely because VERSION is the version being prepared.
#
# Pinning a not-yet-published version breaks nothing locally: each module's
# replace directive satisfies its own side without ever consulting the proxy.
#
# THE VERSION BUMP AND THE PINS MUST MOVE IN THE SAME COMMIT.
# This script reads VERSION as the statement of what the tree will become, so
# splitting them leaves every commit in between failing a check that is
# correct to fail. The natural instinct is to treat the version bump as a
# release-time step taken after the dependency work — it cannot be. Found the
# hard way on go-quicbaas, whose plan put the pins in a PR and the VERSION
# bump in a later release commit; the guard failed the intermediate state on
# its first run.
#
# CHECK 2 — SIBLING AGREEMENT ON SHARED FIRST-PARTY PINS.
# The reciprocal check above only sees a module pinning its OWN repo. It
# cannot see two sibling modules disagreeing about a THIRD first-party
# dependency, which is the same defect wearing different clothes: go-quicbaas
# sdk/go sat on go-qrdb v0.5.8 and go-quicraft v0.4.2 while its root was
# several releases ahead. Each module publishes its own go.mod, so a consumer
# importing only sdk/go gets sdk/go's pins — the disagreement is real even
# though MVS papers over it for anyone importing both.
#
# EXIT: 0 all consistent · 1 mismatch found · 2 usage/environment error
set -uo pipefail

ROOT="${1:-$PWD}"
cd "$ROOT" 2>/dev/null || { echo "check-reciprocal-pins: no such directory: $ROOT" >&2; exit 2; }

[ -f VERSION ] || { echo "check-reciprocal-pins: no VERSION file in $ROOT" >&2; exit 2; }
WANT="$(tr -d '[:space:]' < VERSION)"
[ -n "$WANT" ] || { echo "check-reciprocal-pins: VERSION is empty" >&2; exit 2; }

# Modules that deliberately do NOT ship with the release and may lag. One
# path prefix per line, '#' comments allowed. Keep this list SHORT and
# justified — every entry is a module nobody is checking.
IGNORE_FILE=".reciprocal-pins-ignore"
should_ignore() {
  [ -f "$IGNORE_FILE" ] || return 1
  local dir="$1" pat
  while read -r pat; do
    case "$pat" in ''|'#'*) continue ;; esac
    case "$dir" in $pat*) return 0 ;; esac
  done < "$IGNORE_FILE"
  return 1
}

# ONLY ENFORCE ON MODULES THAT ARE ACTUALLY PUBLISHED.
# A module with no tag namespace has no resolvable version, so no consumer can
# ever be handed a stale sibling through it — the defect this script exists to
# catch is impossible there by construction. Enforcing anyway is not merely
# noisy, it is DANGEROUS: go-qrdb's sdk/go/gorm/conformance deliberately points
# both first-party requires at local paths so the GORM suite runs against the
# WORKING TREE, and "fixing" those pins to the release version would silently
# defeat what the module is for. A permanently-red required gate reliably
# produces exactly that fix.
#
# NOTE the tempting wrong criterion: "skip modules whose first-party requires
# are all satisfied by local replace directives." That would skip the ROOT
# module of go-quicbaas and go-xkms, both of which replace their own siblings
# (`=> ./sdk/go`, `=> ./xkey`) — a false negative on precisely the defect
# Codex caught in go-quicbaas PR #41. The tag namespace is the honest test:
# published or not.
#
# Consequence to accept: a module being published for the FIRST time in this
# release has no prior tags and will be skipped. Add it to the ignore file's
# inverse — or simply re-run after its first tag exists.
has_tag_namespace() {
  local dir="$1" prefix
  case "$dir" in .|'') prefix="" ;; *) prefix="${dir}/" ;; esac
  [ -n "$(git tag -l "${prefix}v*" 2>/dev/null | head -1)" ]
}

ROOT_MOD="$(awk '/^module /{print $2; exit}' go.mod 2>/dev/null)"
[ -n "$ROOT_MOD" ] || { echo "check-reciprocal-pins: no module line in ./go.mod" >&2; exit 2; }
# github.com/jeremyhahn/go-xkms  (strip any /vN or subpath)
BASE="$(printf '%s' "$ROOT_MOD" | grep -oE '^[^/]+/[^/]+/[^/]+')"

echo "check-reciprocal-pins: $BASE must pin its own modules at $WANT"

bad=0
checked=0
while IFS= read -r modfile; do
  dir="$(dirname "$modfile")"; dir="${dir#./}"
  if should_ignore "$dir"; then
    echo "  skip     $modfile (ignored)"
    continue
  fi
  if ! has_tag_namespace "$dir"; then
    echo "  skip     $modfile (never published — no tag namespace, so no consumer can resolve it)"
    continue
  fi
  # requires naming this same repo, ignoring the module line itself
  while IFS= read -r line; do
    path="$(printf '%s' "$line" | awk '{print $1}')"
    ver="$(printf '%s' "$line"  | awk '{print $2}')"
    checked=$((checked + 1))
    if [ "$ver" = "v$WANT" ]; then
      echo "  ok       $modfile -> $path $ver"
    else
      echo "  MISMATCH $modfile -> $path $ver (expected v$WANT)"
      bad=$((bad + 1))
    fi
  done < <(grep -vE '^\s*module ' "$modfile" \
           | grep -oE "$BASE(/[A-Za-z0-9._/-]+)? v[0-9][A-Za-z0-9.+-]*" \
           | sort -u)
done < <(find . -name go.mod -not -path '*/vendor/*' -not -path '*/node_modules/*' | sort)

echo
# ---- check 2: do sibling modules agree on shared FIRST-PARTY dependencies? ----
# Same defect class, different axis. Only compares deps OUTSIDE this repo.
echo "check-reciprocal-pins: sibling agreement on shared first-party deps"
VENDOR="$(printf '%s' "$BASE" | cut -d/ -f1-2)"   # github.com/jeremyhahn
pairs=""
while IFS= read -r modfile; do
  dir="$(dirname "$modfile")"; dir="${dir#./}"
  should_ignore "$dir" && continue
  has_tag_namespace "$dir" || continue
  while IFS= read -r line; do
    dep="$(printf '%s' "$line" | awk '{print $1}')"
    ver="$(printf '%s' "$line" | awk '{print $2}')"
    case "$dep" in "$BASE"|"$BASE"/*) continue ;; esac   # own repo: check 1's job
    pairs="$pairs$dep $ver $modfile
"
  done < <(grep -vE '^\s*module ' "$modfile" \
           | grep -oE "$VENDOR/[A-Za-z0-9._-]+(/[A-Za-z0-9._/-]+)? v[0-9][A-Za-z0-9.+-]*" \
           | sort -u)
done < <(find . -name go.mod -not -path '*/vendor/*' -not -path '*/node_modules/*' | sort)

disagree=0
for dep in $(printf '%s' "$pairs" | awk 'NF{print $1}' | sort -u); do
  vers="$(printf '%s' "$pairs" | awk -v d="$dep" '$1==d{print $2}' | sort -u)"
  n="$(printf '%s' "$vers" | grep -c . || true)"
  if [ "${n:-0}" -gt 1 ]; then
    echo "  DISAGREE $dep"
    printf '%s' "$pairs" | awk -v d="$dep" '$1==d{printf "             %s  in %s\n", $2, $3}' | sort -u
    disagree=$((disagree + 1))
  fi
done
[ "$disagree" -eq 0 ] && echo "  ok       all shared first-party deps agree across modules"

echo
if [ "$bad" -gt 0 ] || [ "$disagree" -gt 0 ]; then
  [ "$bad" -gt 0 ] && {
    echo "FAIL: $bad of $checked reciprocal require(s) do not name v$WANT."
    echo "Fix them to v$WANT — the version being published, NOT the last released one."
    echo "Move VERSION and the pins in the SAME commit; splitting them fails this correctly."
  }
  [ "$disagree" -gt 0 ] && {
    echo "FAIL: $disagree shared first-party dep(s) pinned differently across modules."
    echo "Each module publishes its own go.mod, so a consumer importing one of them"
    echo "gets that module's pins. MVS hides this from anyone importing both."
  }
  echo "A local build cannot detect either; only an outside consumer of the tag can."
  exit 1
fi
echo "OK: $checked reciprocal require(s) name v$WANT; shared first-party deps agree."
