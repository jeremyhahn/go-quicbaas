#!/usr/bin/env bash
# ci-merge-base.sh -- validate the MERGE RESULT, not the pull request head.
#
# WHY THIS SCRIPT EXISTS, and why the obvious `actions/checkout` option does
# not do the job on this server (#436):
#
# On github.com a `pull_request` run already builds the merge result. GitHub
# pre-computes a merge commit, publishes it as `refs/pull/<n>/merge`, and
# points `github.ref` at it, so checkout lands on merge(base, head) without
# anyone asking. Anyone who has configured this on github.com will reach for a
# `ref:` on the checkout step and conclude the problem is already solved.
#
# GITEA DOES NOT PUBLISH THAT REF. Measured on this server 2026-09-03:
#
#   git ls-remote origin 'refs/pull/*'   -> 118 refs, every one .../head,
#                                           not a single .../merge
#   git fetch origin refs/pull/437/merge -> fatal: couldn't find remote ref
#
# and the run metadata agrees: `"path": "ci.yml@refs/pull/438/head"`. There is
# no merge commit to check out, so no checkout option can reach one -- the
# workflow has to construct the merge itself. That is all this script does.
#
# THE DEFECT IT CLOSES: two pull requests, each green against its own base, can
# be individually correct and jointly broken. #374 made `GetUserByEmail` take a
# tenant argument; #385 added a caller using the old two-argument form. No
# textual conflict, so git merged both without complaint and reported nothing
# wrong -- ABSENCE OF CONFLICTING HUNKS IS NOT A COMPILING TREE. `feature/software-factory`
# did not build for about an hour (fixed in 0b05ac46). Nothing in the pipeline
# had ever compiled the two together, because the pipeline only ever compiles
# one head at a time.
#
# Usage:  ci-merge-base.sh <event-name> <base-ref> [pinned-base-sha]
#
#   event-name       github.event_name. Anything other than `pull_request` is a
#                    no-op: a push to main has no base to merge into.
#   base-ref         github.base_ref, the branch the PR targets.
#   pinned-base-sha  optional. The base commit resolved ONCE by the `merge` job
#                    and handed to every other job, so all of them validate the
#                    same tree even when the base branch moves mid-run -- runs
#                    here last up to two hours and pull requests do merge inside
#                    that window. Omitted (or empty), the script resolves the
#                    base tip itself and says so, which is correct but no longer
#                    guarantees that two jobs saw the same base.
#
# The merge is LOCAL to the runner workspace. Nothing here pushes anything.
#
# FAILURE CLASSES ARE KEPT APART ON PURPOSE. Twice on this fleet a real finding
# was read as infrastructure noise and waved through. So a merge that fails
# because the branch genuinely conflicts prints a banner that names the
# conflicting paths and says what to do about it, and exits 2; everything else
# -- an unreachable remote, a missing base ref, a base sha that no longer
# resolves -- says CI INFRASTRUCTURE in its first line, states that it is not a
# finding about the branch, and exits 3. Neither is allowed to pass.
set -uo pipefail

EVENT="${1:-}"
BASE_REF="${2:-}"
PINNED_SHA="${3:-}"

EXIT_CONFLICT=2
EXIT_INFRA=3

banner() {
	echo "============================================================"
	printf '%s\n' "$@"
	echo "============================================================"
}

infra_fail() {
	banner "CI INFRASTRUCTURE: $1" \
		"" \
		"This is NOT a finding about this branch. The merge-result check" \
		"could not be performed at all, so nothing about the code has been" \
		"proven either way. Re-run; if it persists it is a runner or Gitea" \
		"problem, not a reason to change the branch."
	exit $EXIT_INFRA
}

if [ "$EVENT" != "pull_request" ]; then
	echo "event is '${EVENT}', not a pull request -- there is no base branch to merge."
	echo "Building the checked-out tree as-is."
	exit 0
fi

if [ -z "$BASE_REF" ]; then
	infra_fail "the pull_request event carried no base ref (github.base_ref was empty)."
fi

HEAD_SHA="$(git rev-parse HEAD 2>/dev/null)" ||
	infra_fail "the workspace is not a git repository (git rev-parse HEAD failed)."

# Bring the base branch into the workspace. `actions/checkout` fetches only the
# ref it checked out, so the base is not present until we ask for it, and a
# depth-1 clone has no merge base to three-way merge against -- every job that
# runs this script therefore sets fetch-depth: 0.
if ! git fetch --no-tags origin "+refs/heads/${BASE_REF}:refs/remotes/origin/${BASE_REF}" 2>&1; then
	infra_fail "could not fetch base branch '${BASE_REF}' from origin."
fi

if [ -n "$PINNED_SHA" ]; then
	BASE_SHA="$PINNED_SHA"
	BASE_SOURCE="pinned by the merge job"
	# A pinned base is an ancestor of the branch tip under normal operation --
	# the base only moves forward, by merges. It fails to resolve only if the
	# base was force-pushed or rewritten under the run, which is not a finding
	# about this branch either.
	git cat-file -e "${BASE_SHA}^{commit}" 2>/dev/null ||
		infra_fail "the base commit ${BASE_SHA} pinned by the merge job is not reachable on '${BASE_REF}' (force-push or rewritten history under this run?)."
else
	BASE_SHA="$(git rev-parse "refs/remotes/origin/${BASE_REF}" 2>/dev/null)" ||
		infra_fail "could not resolve the tip of base branch '${BASE_REF}'."
	BASE_SOURCE="resolved by this job (NOT pinned -- another job may see a different base)"
fi

echo "base branch : ${BASE_REF}"
echo "base commit : ${BASE_SHA} (${BASE_SOURCE})"
echo "head commit : ${HEAD_SHA}"

# Publish the base commit so the `merge` job can hand the SAME one to every
# other job. Outside Actions GITHUB_OUTPUT is unset and this is a no-op, which
# is what makes the script runnable (and testable) from a plain shell.
if [ -n "${GITHUB_OUTPUT:-}" ]; then
	echo "base_sha=${BASE_SHA}" >> "$GITHUB_OUTPUT"
fi

if git merge-base --is-ancestor "$BASE_SHA" HEAD 2>/dev/null; then
	echo "the base commit is already contained in this branch -- the tree checked out IS the merge result."
	exit 0
fi

# --no-ff so the merge is always a real three-way merge and the log states what
# was combined. The identity is a throwaway: this commit exists for the length
# of one job and is never pushed.
#
# commit.gpgsign is forced OFF. This commit is an artefact of the check, not
# history, and there is no reason for it to need a signing key -- but every
# agent-facing git path on this fleet sets commit.gpgsign=true, and an
# environment that inherits it turns a perfectly clean merge into
# "Couldn't sign message (signer): agent refused operation" and then
# "fatal: failed to write commit object". Measured in the local harness for
# this change, where exactly that happened: the clean-merge case failed, and
# fell into the INFRASTRUCTURE class (correctly -- no conflicting paths), which
# is a fail-closed gate for a reason having nothing to do with the branch.
if git -c user.name='CI merge-result check' \
       -c user.email='ci@localhost' \
       -c commit.gpgsign=false \
       merge --no-edit --no-ff "$BASE_SHA"; then
	echo
	echo "merged tree : $(git rev-parse HEAD^{tree})"
	echo "This job is validating merge(${BASE_REF}@${BASE_SHA:0:8}, head@${HEAD_SHA:0:8}), not the head alone."
	exit 0
fi

CONFLICTS="$(git diff --name-only --diff-filter=U 2>/dev/null)"

if [ -z "$CONFLICTS" ]; then
	git merge --abort 2>/dev/null
	infra_fail "git merge failed but reported no conflicting paths, so the failure is not a conflict in this branch."
fi

banner "MERGE CONFLICT -- this pull request conflicts with '${BASE_REF}'." \
	"" \
	"This is a finding about THIS BRANCH, not an infrastructure failure." \
	"" \
	"Merging ${BASE_REF} (${BASE_SHA}) into this branch (${HEAD_SHA})" \
	"leaves conflicts in:" \
	"" \
	"$(printf '%s\n' "$CONFLICTS" | sed 's/^/  /')" \
	"" \
	"CI cannot validate a tree that does not exist, so nothing downstream" \
	"has run. Rebase this branch onto ${BASE_REF}, resolve the conflicts," \
	"and push; the gate will then judge the tree that would actually land."
git merge --abort 2>/dev/null
exit $EXIT_CONFLICT
