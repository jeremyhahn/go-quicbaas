#!/bin/sh
# docs-only.sh decides whether a pull request changes documentation alone, so
# ci.yml can skip the build, test, security and integration stages for it
# (DS-3161).
#
# Usage: EVENT=<event name> BASE_SHA=<base commit> scripts/docs-only.sh
#
# It prints exactly one line on stdout, docs_only=true or docs_only=false, in
# the form $GITHUB_OUTPUT takes. The changed files and a "docs only:" line go
# to stderr.
#
# Only a pull_request event can be docs-only; every other event prints false.
# Documentation is docs/, changelog.d/, a top-level *.md or any README.md. The
# diff runs with --no-renames, so a code file moved into docs/ lists its
# deletion and is not documentation. An empty change set is not docs-only.
#
# It exits 1, printing nothing on stdout, when a pull_request event has no
# BASE_SHA or the diff against it fails: an unknown change set must never
# skip the stages.
set -eu

docs_only=false
if [ "${EVENT:-}" = pull_request ]; then
	if [ -z "${BASE_SHA:-}" ]; then
		echo "::error::BASE_SHA is not set for a pull_request event" >&2
		exit 1
	fi
	if ! files=$(git diff --no-renames --name-only "$BASE_SHA"...HEAD); then
		echo "::error::cannot list the files this pull request changes" >&2
		exit 1
	fi
	code=$(printf '%s\n' "$files" | grep -vE '^(docs/|changelog\.d/)|^[^/]+\.md$|(^|/)README\.md$' | grep -c . || true)
	if [ -n "$files" ] && [ "$code" -eq 0 ]; then docs_only=true; fi
	printf 'changed files:\n%s\n' "$files" >&2
fi
echo "docs only: $docs_only" >&2
echo "docs_only=$docs_only"
