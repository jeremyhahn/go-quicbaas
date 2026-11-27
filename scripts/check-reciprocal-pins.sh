#!/usr/bin/env bash
#
# Vendored copy: fail when a cross-module pin is not reciprocal.
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
