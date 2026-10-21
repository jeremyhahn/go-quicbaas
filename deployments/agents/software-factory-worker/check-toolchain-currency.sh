#!/usr/bin/env bash
# Copyright (c) 2026 Jeremy Hahn
# Copyright (c) 2026 Automate The Things, LLC
#
# This file is part of go-quicbaas.
#
# go-quicbaas is dual-licensed:
#
# 1. GNU Affero General Public License v3.0 (AGPL-3.0)
#    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
#
# 2. Commercial License
#    Contact licensing@automatethethings.com for commercial licensing options.
#
# check-toolchain-currency.sh TOOLCHAIN_MK
#
# Fails when any worker-image pin is behind its upstream's latest release.
# Checked pins: every *_VERSION in TOOLCHAIN_MK (pkg/version/toolchain.mk), each
# module@version in its GO_TOOL_DEPENDENCY_FLOORS, and the CLAUDE_CODE_VERSION
# and CODEX_VERSION the Makefile passes in the environment. Each pin is
# compared with the latest release its upstream publishes, and a pin with no
# entry in SOURCES fails too, so a new pin cannot slip past the check.
#
# SERENA_DEPENDENCY_FLOORS is not checked. Its entries are lower bounds
# (urllib3>=2.8.0) that uv already resolves to the newest release, so one can
# never be behind.
#
# NPM_BUNDLED_DEPENDENCY_FLOORS is checked the other way round. Each entry is
# a stopgap for a package npm bundles, so it is OBSOLETE -- and fails the
# check -- once the latest npm release already bundles that version or newer,
# or no longer bundles the package: drop it then, as the build would refuse it
# on the next npm upgrade.
#
# Run it through `make check-worker-toolchain-currency`; nightly CI runs the
# same target. It needs curl, grep, sed, tar and sort -V. The GitHub lookups read
# where releases/latest redirects rather than calling the API, so they need no
# token and meet no API rate limit.

set -euo pipefail

if [ "$#" -ne 1 ] || [ ! -r "$1" ]; then
	echo "usage: $0 TOOLCHAIN_MK" >&2
	exit 2
fi

# Where each pin's latest release is read from, as KIND:ARGUMENT. latest_KIND
# below reads one kind.
declare -A SOURCES=(
	[GO_VERSION]="go:"
	[GOLANGCI_VERSION]="goproxy:github.com/golangci/golangci-lint/v2"
	[GOSEC_VERSION]="goproxy:github.com/securego/gosec/v2"
	[GOVULNCHECK_VERSION]="goproxy:golang.org/x/vuln"
	[GOPLS_VERSION]="goproxy:golang.org/x/tools/gopls"
	[ACTIONLINT_VERSION]="goproxy:github.com/rhysd/actionlint"
	[PROTOC_GEN_GO_VERSION]="goproxy:google.golang.org/protobuf"
	[PROTOC_GEN_GO_GRPC_VERSION]="goproxy:google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	[SERENA_VERSION]="github:oraios/serena"
	[SHELLCHECK_VERSION]="github:koalaman/shellcheck"
	[OPENCODE_VERSION]="github:anomalyco/opencode"
	[PROTOC_VERSION]="github:protocolbuffers/protobuf"
	[UV_VERSION]="github:astral-sh/uv"
	[CLANGD_VERSION]="github:clangd/clangd"
	[RUST_ANALYZER_VERSION]="github:rust-lang/rust-analyzer"
	[MARKSMAN_VERSION]="github:artempyanykh/marksman"
	[KOTLIN_LSP_VERSION]="github:Kotlin/kotlin-lsp"
	[NODE_VERSION]="node:"
	[NPM_VERSION]="npm:npm"
	[PYRIGHT_VERSION]="npm:pyright"
	[TYPESCRIPT_LANGUAGE_SERVER_VERSION]="npm:typescript-language-server"
	[BASH_LANGUAGE_SERVER_VERSION]="npm:bash-language-server"
	[YAML_LANGUAGE_SERVER_VERSION]="npm:yaml-language-server"
	[CLAUDE_CODE_VERSION]="npm:@anthropic-ai/claude-code"
	[CODEX_VERSION]="npm:@openai/codex"
	# TypeScript 7 ships no tsserver.js for typescript-language-server to drive,
	# so this pin tracks the newest 6.x (toolchain.mk).
	[TYPESCRIPT_VERSION]="npm_major:typescript@6"
	[RUBY_LSP_VERSION]="gem:ruby-lsp"
)

fetch() {
	curl -fsSL --retry 3 --max-time 60 "$@"
}

# first_match PATTERN: the first match of the extended regex on stdin.
first_match() {
	grep -oE "$1" | sed -n '1p'
}

# json_string KEY: the first "KEY":"value" string value on stdin.
json_string() {
	first_match "\"$1\" *: *\"[^\"]*\"" | sed -E 's/.*: *"([^"]*)"/\1/'
}

latest_go() {
	fetch 'https://go.dev/dl/?mode=json' | json_string version
}

latest_goproxy() {
	fetch "https://proxy.golang.org/$1/@latest" | json_string Version
}

# The newest non-prerelease release, from where releases/latest redirects.
latest_github() {
	local url
	url="$(curl -fsSI --retry 3 --max-time 60 -o /dev/null -w '%{redirect_url}' "https://github.com/$1/releases/latest")"
	case "$url" in
		*/releases/tag/*) printf '%s\n' "${url##*/releases/tag/}" ;;
		*) return 1 ;;
	esac
}

latest_node() {
	fetch 'https://nodejs.org/dist/index.json' | json_string version
}

# From the dist-tags document, which holds nothing but tags: the /latest
# manifest nests other "version" keys before its own (npm's has a 5.1.1).
latest_npm() {
	fetch "https://registry.npmjs.org/-/package/$1/dist-tags" | json_string latest
}

# The newest release on one major line: argument PACKAGE@MAJOR.
latest_npm_major() {
	local package="${1%@*}" major="${1##*@}"
	fetch -H 'Accept: application/vnd.npm.install-v1+json' "https://registry.npmjs.org/$package" |
		grep -oE "\"${major}\\.[0-9]+\\.[0-9]+\":" | tr -d '":' | sort -V | sed -n '$p'
}

latest_gem() {
	fetch "https://rubygems.org/api/v1/versions/$1/latest.json" | json_string version
}

# normalize VERSION: comparable across sources that do and do not prefix one.
normalize() {
	# A monorepo tag names its project first ("kotlin-lsp/v263.7084.0").
	local version="${1##*/}"
	version="${version#go}"
	printf '%s\n' "${version#v}"
}

behind=0
checked=0

# check NAME PIN SOURCE: reports the pin and counts it behind when upstream's
# latest release sorts after it, or when upstream cannot be read.
check() {
	local name="$1" pin="$2" kind="${3%%:*}" argument="${3#*:}" latest
	checked=$((checked + 1))
	if ! latest="$("latest_$kind" "$argument")" || [ -z "$latest" ]; then
		echo "UNREADABLE $name: no latest release from ${kind} ${argument}"
		behind=$((behind + 1))
		return
	fi
	local have want
	have="$(normalize "$pin")"
	want="$(normalize "$latest")"
	if [ "$have" != "$want" ] && [ "$(printf '%s\n%s\n' "$have" "$want" | sort -V | sed -n '$p')" = "$want" ]; then
		echo "BEHIND     $name: pinned $pin, latest $latest"
		behind=$((behind + 1))
		return
	fi
	echo "current    $name $pin"
}

# check_npm_floor NAME@VERSION NPM_TARBALL NPM_VERSION: counts the floor
# obsolete when the npm release in NPM_TARBALL bundles NAME at VERSION or
# newer, or does not bundle it at all.
check_npm_floor() {
	local name="${1%@*}" floor="${1##*@}" manifest bundled
	checked=$((checked + 1))
	if ! manifest="$(tar -xzOf "$2" "package/node_modules/$name/package.json" 2>/dev/null)"; then
		echo "OBSOLETE   NPM_BUNDLED_DEPENDENCY_FLOORS $name: npm $3 no longer bundles it"
		behind=$((behind + 1))
		return
	fi
	if ! bundled="$(printf '%s\n' "$manifest" | json_string version)" || [ -z "$bundled" ]; then
		echo "UNREADABLE NPM_BUNDLED_DEPENDENCY_FLOORS $name: no version in npm $3's copy"
		behind=$((behind + 1))
		return
	fi
	if [ "$(printf '%s\n%s\n' "$floor" "$bundled" | sort -V | sed -n '$p')" = "$bundled" ]; then
		echo "OBSOLETE   NPM_BUNDLED_DEPENDENCY_FLOORS $name: floor $floor, npm $3 bundles $bundled"
		behind=$((behind + 1))
		return
	fi
	echo "needed     NPM_BUNDLED_DEPENDENCY_FLOORS $name $floor (npm $3 bundles $bundled)"
}

declare -A pins=()
while IFS= read -r line; do
	case "$line" in
		[A-Z]*' := '*) pins["${line%% := *}"]="${line#* := }" ;;
	esac
done <"$1"
pins[CLAUDE_CODE_VERSION]="${CLAUDE_CODE_VERSION:?the Makefile passes CLAUDE_CODE_VERSION}"
pins[CODEX_VERSION]="${CODEX_VERSION:?the Makefile passes CODEX_VERSION}"

for name in $(printf '%s\n' "${!pins[@]}" | sort); do
	case "$name" in
		*_VERSION) ;;
		*) continue ;;
	esac
	if [ -z "${SOURCES[$name]:-}" ]; then
		echo "UNCHECKED  $name: no upstream source in $0"
		behind=$((behind + 1))
		continue
	fi
	check "$name" "${pins[$name]}" "${SOURCES[$name]}"
done

for floor in ${pins[GO_TOOL_DEPENDENCY_FLOORS]:-}; do
	check "GO_TOOL_DEPENDENCY_FLOORS ${floor%@*}" "${floor#*@}" "goproxy:${floor%@*}"
done

if [ -n "${pins[NPM_BUNDLED_DEPENDENCY_FLOORS]:-}" ]; then
	npm_tarball="$(mktemp)"
	trap 'rm -f "$npm_tarball"' EXIT
	if npm_latest="$(latest_npm npm)" && [ -n "$npm_latest" ] &&
		fetch -o "$npm_tarball" "https://registry.npmjs.org/npm/-/npm-${npm_latest}.tgz"; then
		for floor in ${pins[NPM_BUNDLED_DEPENDENCY_FLOORS]}; do
			check_npm_floor "${floor%%=*}" "$npm_tarball" "$npm_latest"
		done
	else
		echo "UNREADABLE NPM_BUNDLED_DEPENDENCY_FLOORS: no tarball for the latest npm"
		checked=$((checked + 1))
		behind=$((behind + 1))
	fi
fi

if [ "$behind" -ne 0 ]; then
	echo "$behind of $checked worker toolchain pins are behind, obsolete or unreadable."
	exit 1
fi
echo "All $checked worker toolchain pins are current."
