#!/usr/bin/env bash
# ci-merge-base.sh: validate the merge result, not the pull request head.
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
# depth-1 clone has no merge base to three-way merge against. The ci-template's
# checkout step is depth 1 and is not ours to change, so a shallow workspace is
# deepened to full history here first.
if [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" = "true" ] &&
	! git fetch --no-tags --unshallow origin 2>&1; then
	infra_fail "could not fetch the full history of this branch from origin."
fi
if ! git fetch --no-tags origin "+refs/heads/${BASE_REF}:refs/remotes/origin/${BASE_REF}" 2>&1; then
	infra_fail "could not fetch base branch '${BASE_REF}' from origin."
fi

if [ -n "$PINNED_SHA" ]; then
	BASE_SHA="$PINNED_SHA"
	BASE_SOURCE="pinned for the run"
	# A pinned base is an ancestor of the branch tip under normal operation --
	# the base only moves forward, by merges. It fails to resolve only if the
	# base was force-pushed or rewritten under the run, which is not a finding
	# about this branch either.
	git cat-file -e "${BASE_SHA}^{commit}" 2>/dev/null ||
		infra_fail "the base commit ${BASE_SHA} pinned for the run is not reachable on '${BASE_REF}' (force-push or rewritten history under this run?)."
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
