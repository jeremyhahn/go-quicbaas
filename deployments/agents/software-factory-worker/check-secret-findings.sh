#!/usr/bin/env bash
# check-secret-findings.sh <trivy-json> <scan-root>
#
# Gates the worker-image layer scan (.github/workflows/worker-image.yml). The
# step extracts every layer tarball to <scan-root>/<layer-dir>/ and runs
# `trivy fs --scanners secret --format json` over <scan-root>; this reads that
# report (.Results[].Target, .Results[].Secrets[]) and allows a finding only
# when its image path (the target with <scan-root>/<layer-dir>/ stripped) is
# listed in secret-allowlist.txt beside this script AND the file's sha256
# under <scan-root> is the pinned one. Anything else fails, closed: an
# unlisted finding, a listed path with other content, a target outside the
# scan root, and a missing, empty or non-JSON report.
#
# Needs bash, jq and sha256sum only.
set -euo pipefail

fail() {
	echo "::error::$*"
	exit 1
}

[ "$#" -eq 2 ] || fail "usage: $0 <trivy-json> <scan-root>"
report="$1"
root="${2%/}"
allowlist="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/secret-allowlist.txt"

[ -f "$report" ] && [ -r "$report" ] || fail "secret findings report ${report} is missing or unreadable"
[ -d "$root" ] || fail "scan root ${root} is not a directory"
[ -f "$allowlist" ] && [ -r "$allowlist" ] || fail "secret allowlist ${allowlist} is missing or unreadable"

# image path -> pinned sha256
declare -A pinned=()
while IFS= read -r line || [ -n "$line" ]; do
	case "$line" in '' | '#'*) continue ;; esac
	read -r go_version path hash extra <<<"$line"
	if [ -n "${extra:-}" ] || [[ ! "$go_version" =~ ^go[0-9] ]] || [ -z "${path:-}" ] || [[ ! "${hash:-}" =~ ^[0-9a-f]{64}$ ]]; then
		fail "secret allowlist entry is not \`go<version> <path> <sha256>\`: ${line}"
	fi
	pinned["$path"]="$hash"
done <"$allowlist"

if ! jq -e 'type == "object" and (.SchemaVersion | type) == "number" and ((.Results // []) | type) == "array"' "$report" >/dev/null 2>&1; then
	fail "secret findings report ${report} is not a trivy JSON report"
fi
# One line per finding: target, rule. @tsv escapes tabs and newlines, so an
# odd path stays one line and simply matches no allowlist entry.
if ! findings="$(jq -r '(.Results // [])[] | .Target as $t | (.Secrets // [])[] | [($t | tostring), (.RuleID // "unknown" | tostring)] | @tsv' "$report")"; then
	fail "cannot read the findings in ${report}"
fi

failed=0
allowed=0
while IFS=$'\t' read -r target rule; do
	[ -n "$target$rule" ] || continue
	# Targets are relative to the scan root; accept the absolute form too.
	case "$target" in
	"$root"/*) rel="${target#"$root"/}" ;;
	/*)
		echo "::error::secret finding (${rule}) outside the scan root: ${target}"
		failed=1
		continue
		;;
	*) rel="$target" ;;
	esac
	if [[ "$rel" != */* ]] || [[ "/$rel/" == */../* ]] || [[ "/$rel/" == */./* ]] || [[ "/$rel/" == *//* ]]; then
		echo "::error::secret finding (${rule}) at an unexpected path: ${target}"
		failed=1
		continue
	fi
	image_path="${rel#*/}"
	want="${pinned[$image_path]:-}"
	if [ -z "$want" ]; then
		echo "::error::secret finding (${rule}) in the image at /${image_path} (${rel}) is not allowlisted"
		failed=1
		continue
	fi
	file="$root/$rel"
	if [ -L "$file" ] || [ ! -f "$file" ]; then
		echo "::error::allowlisted secret finding /${image_path} is not a regular file under the scan root (${rel})"
		failed=1
		continue
	fi
	got=""
	read -r got _ < <(sha256sum -- "$file") || true
	if [ "$got" != "$want" ]; then
		echo "::error::secret finding (${rule}) at /${image_path} (${rel}) has sha256 ${got:-unreadable}, the allowlist pins ${want}"
		failed=1
		continue
	fi
	echo "allowed pinned secret finding (${rule}) at /${image_path} (${rel}), sha256 ${got}"
	allowed=$((allowed + 1))
done <<<"$findings"

if [ "$failed" -ne 0 ]; then
	echo "::error::the layer secret scan has findings outside ${allowlist##*/}"
	exit 1
fi
echo "secret scan: no findings outside the allowlist (${allowed} allowed by pinned hash)"
