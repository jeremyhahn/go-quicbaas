// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package sdlc

import (
	"path"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The pre-flight — prove the workspace before a token is spent on it
// =============================================================================
//
// A WORKSPACE FAULT MUST COST ZERO AGENT TOKENS, and the only way to guarantee
// that is to prove the workspace fit BEFORE the first prompt. Measured on the
// burn-in node, 2026-09-11 (go-quicbaas#908): a worker was asked to adopt a
// file from a repository nobody had declared, spent a whole session reading the
// issue, the repository, its Makefile and the one context repository that WAS
// there, ran five filesystem probes to establish that the file was absent, and
// then escalated. Its behaviour was exactly right. The cost was not: every one
// of those probes is a git command with a deterministic answer, and the
// owner's standing ruling is that deterministic work belongs in Go.
//
// WHAT IT PROVES, PER REPOSITORY, is the smallest set of facts that together
// mean "the worker can do the work here":
//
//  1. the directory is a git repository — the empty-checkout case, which this
//     same workspace produced on the attempt before (an empty worktree, a
//     worker that answered "OK", an attempt charged);
//  2. its `origin` is the remote the spec named — an attempt directory a dead
//     predecessor left holding a DIFFERENT repository is adopted by the
//     cloner's idempotence (isRepository answers yes and the clone is
//     skipped), and nothing else would ever notice;
//  3. HEAD resolves to a commit — a clone killed part-way leaves a repository
//     with no commit in it, which reads as present to every check but the
//     one that asks for an object.
//
// And for the project checkout, additionally, that HEAD is ON THE BRANCH THE
// PLAN NAMED. The pinned-commit half of that is the cloner's
// (verifyPinnedHead) and is not repeated here.
//
// IT IS NOT A SECOND CHECKOUT AND MUST NOT BECOME ONE. It repairs nothing,
// clones nothing and writes nothing: every command is a probe whose exit
// status is the answer, so a pre-flight that itself failed could never be
// mistaken for a workspace that passed. What it produces is either silence or
// one typed error NAMING THE REPOSITORY an operator has to fix.
//
// WHY IT IS NOT REDUNDANT WITH THE CLONE. The clone is idempotent by design —
// it has to be, because a resumed attempt is bind-mounted through the directory
// its predecessor cloned into — and every skip it takes is a fact it did not
// establish. The pre-flight asks for the facts themselves rather than for the
// absence of a failure, which is the difference between "nothing went wrong
// this time" and "the workspace is fit".

// gitOriginProbe asks git for the URL of a repository's origin remote.
var gitOriginProbe = []string{"remote", "get-url", gitRemote}

// gitHeadProbe asks git for the commit HEAD resolves to. It fails, by exit
// status, in a repository that holds no commit.
var gitHeadProbe = []string{"rev-parse", "HEAD"}

// gitBranchProbe asks git which branch HEAD is on, or `HEAD` for a detached
// one.
var gitBranchProbe = []string{"rev-parse", "--abbrev-ref", "HEAD"}

// auditPreflight is the audit event the pre-flight's probes are recorded
// under. Separate from auditCheckout so a reader can tell the commands that
// MADE the checkout from the ones that judged it.
const auditPreflight = "sdlc:workspace_preflight"

// preflight proves, inside the container and before anything is prompted, that
// the checkout the spec described is the checkout that is there.
//
// EVERY COMMAND GOES THROUGH gitSteps.probe, never through run: the probe's
// EXIT STATUS is the answer, so an absent directory or a repository with no
// commit is a "no" the pre-flight reports as a named fault, and only the exec
// seam itself failing — no session, no container — is an error out of here.
type preflight struct {
	gitSteps

	// spec is the workspace spec the daemon materialized for this spawn: the
	// same bytes the container reads at $QUICBAAS_WORKSPACE_SPEC, and
	// therefore the description every fact below is judged against.
	spec orchestration.WorkspaceSpec
}

// Verify proves the project checkout and every declared context repository is
// fit, and answers the FIRST fault it finds, naming the repository.
//
// The first rather than all of them, deliberately: an operator repairs a
// workspace one cause at a time, and a checkout root that is entirely empty
// would otherwise report one indistinguishable fault per declared repository.
// The project comes first because a missing project checkout makes every other
// answer uninteresting.
func (p *preflight) Verify() error {
	if err := p.validate(); err != nil {
		return err
	}
	root := p.spec.CheckoutRoot
	if err := p.verifyRepo(root, p.spec.Project); err != nil {
		return err
	}
	if err := p.verifyBranch(root); err != nil {
		return err
	}
	for _, repo := range p.spec.ContextRepos {
		if err := p.verifyRepo(root, repo); err != nil {
			return err
		}
	}
	return nil
}

// validate refuses a spec that describes no checkout to judge. It is the same
// refusal the cloner makes, asked again here because the pre-flight is also
// reached on a path where nothing cloned — a spec assembled by hand in a test,
// or a future caller that inherits a mount rather than making one — and a
// pre-flight that PASSED an empty spec would be the worst possible answer: a
// guarantee that verified nothing.
func (p *preflight) validate() error {
	switch {
	case strings.TrimSpace(p.spec.CheckoutRoot) == "":
		return &InvalidInputError{Field: "workspace.checkout_root", Message: "the pre-flight judges a named directory"}
	case strings.TrimSpace(p.spec.Project.Name) == "":
		return &InvalidInputError{Field: "workspace.project.name", Message: "names the directory the project was cloned into"}
	case strings.TrimSpace(p.spec.Project.RepoURL) == "":
		return &InvalidInputError{Field: "workspace.project.repo_url", Message: "names the remote the checkout must have come from"}
	case strings.TrimSpace(p.spec.Branch.Name) == "":
		return &InvalidInputError{Field: "workspace.branch.name", Message: "names the branch the checkout must be on"}
	}
	return nil
}

// verifyRepo proves one repository is present, is the one the spec named, and
// holds a commit.
func (p *preflight) verifyRepo(root string, repo orchestration.WorkspaceRepoSpec) error {
	dir := path.Join(root, repo.Name)
	present, err := p.probe(root, append([]string{"-C", repo.Name}, gitDirProbe...)...)
	if err != nil {
		return err
	}
	if !present.Succeeded() {
		return &WorkspaceUnfitError{
			TaskID: p.taskID, Repo: repo.Name, Dir: dir,
			Reason: "is not a git repository — nothing was cloned there, or what was cloned is gone",
		}
	}

	origin, err := p.probe(root, append([]string{"-C", repo.Name}, gitOriginProbe...)...)
	if err != nil {
		return err
	}
	// A repository with no origin is as unusable as one with the wrong
	// origin: nothing in it can be fetched or pushed, and the worker's own
	// git would fail on its first remote operation.
	got := strings.TrimSpace(origin.Stdout)
	if !origin.Succeeded() || got != repo.RepoURL {
		return &WorkspaceUnfitError{
			TaskID: p.taskID, Repo: repo.Name, Dir: dir,
			Reason: "holds " + displayOrigin(got) + ", not " + repo.RepoURL,
		}
	}

	head, err := p.probe(root, append([]string{"-C", repo.Name}, gitHeadProbe...)...)
	if err != nil {
		return err
	}
	if !head.Succeeded() || strings.TrimSpace(head.Stdout) == "" {
		return &WorkspaceUnfitError{
			TaskID: p.taskID, Repo: repo.Name, Dir: dir,
			Reason: "holds no commit — the clone did not finish",
		}
	}
	return nil
}

// verifyBranch proves the project checkout is on the branch the plan named.
//
// THE BRANCH IS THE WORKER'S CONTRACT. The charter tells the worker its branch
// is already checked out and that it commits to that branch; a checkout left
// on the base, or detached, is one where every commit the worker makes lands
// somewhere the publisher will not find it — and the worker has no way to
// know, because it was told otherwise.
func (p *preflight) verifyBranch(root string) error {
	dir := path.Join(root, p.spec.Project.Name)
	branch, err := p.probe(root, append([]string{"-C", p.spec.Project.Name}, gitBranchProbe...)...)
	if err != nil {
		return err
	}
	got := strings.TrimSpace(branch.Stdout)
	if !branch.Succeeded() || got != p.spec.Branch.Name {
		return &WorkspaceUnfitError{
			TaskID: p.taskID, Repo: p.spec.Project.Name, Dir: dir,
			Reason: "is on " + displayBranch(got) + ", not on the planned branch " + p.spec.Branch.Name,
		}
	}
	return nil
}

// WorkspaceUnfitParkReason is the reason an operator reads on the issue when
// the pre-flight refused a workspace: what was unfit, that nothing was prompted,
// and what to do about it — carrying the operator_repair unblock condition so
// the sweeper knows who clears it.
//
// IT NAMES THE REPOSITORY AND SAYS NOTHING WAS SPENT. Both halves are what the
// burn-in was missing: an operator who reads "the workspace is unfit" looks at
// the project checkout, and an owner who reads an escalation assumes an agent
// burned a session reaching it.
func WorkspaceUnfitParkReason(set prompts.Renderer, fault error) (string, error) {
	detail := "the workspace was refused before anything was prompted"
	if fault != nil {
		detail = fault.Error()
	}
	return ComposeBlockedReason(UnblockCondition{Kind: UnblockOperatorRepair},
		set.MustRender("workspace_unfit", prompts.Vars{"fault": detail}))
}

// displayOrigin renders a repository's origin for a person, including the case
// where it has none.
func displayOrigin(origin string) string {
	if origin == "" {
		return "no origin remote at all"
	}
	return origin
}

// displayBranch renders which branch a checkout is on, including git's own
// answer for a detached HEAD.
func displayBranch(branch string) string {
	switch branch {
	case "":
		return "no branch git could name"
	case "HEAD":
		return "a detached HEAD"
	default:
		return "branch " + branch
	}
}
