#!/bin/sh
# docs-only.sh: whether a pull request changes documentation alone, so ci.yml can skip the build and test stages.
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
