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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Publishing — the container commits, signs and pushes
// =============================================================================
//
// THE DAEMON RUNS NO GIT. Every command below is an argument vector executed
// inside the worker's container, in the checkout the initializing step made
// there (checkout.go), against a signing agent the container reaches through
// a socket and a forge token it reaches through a read-only file. There is no
// second path: a daemon that also had one would be a daemon holding a
// tenant's identity on the host it happens to run on.
//
// WHAT IS AND IS NOT IN THE ARGUMENT VECTOR. Configuration is: the author's
// name and address, the signing key's PUBLIC half, the credential helper's
// SHAPE. Credentials are not: the helper reads the token out of the mounted
// file when git asks it to, so the value never reaches argv, the environment,
// or a log line.

// gitCommand is the executable every publish step runs.
const gitCommand = "git"

// gitRemote is the remote the checkout step cloned from.
const gitRemote = "origin"

// publishPlan is one publication of the worker's work.
type publishPlan struct {
	// WorkDir is the checkout INSIDE the container.
	WorkDir string

	// Branch is the local branch the work is on.
	Branch string

	// RemoteBranch is the ref it is published as. Empty publishes under the
	// local name, which is the reviewed path; the direct-push carve-out
	// names the target so an exempt change lands without a pull request.
	RemoteBranch string

	// Base is the ref the branch was cut from, used to ask whether there is
	// anything to publish at all.
	Base string

	// Message is the commit message, when there is something to commit.
	Message string

	// Force publishes with --force-with-lease, which is what a MID-ATTEMPT
	// publication needs: a worker that amended or rebased its own commits
	// between turns leaves a branch the remote cannot fast-forward to, and a
	// plain push would fail the job over work that is perfectly fine.
	//
	// IT IS --force-with-lease AND NOT --force. The lease refuses the push
	// when the remote moved since this checkout last saw it, so the one thing
	// a force push must never do — discard a commit this container never
	// observed — is refused by git rather than trusted to the caller. A branch
	// with no remote-tracking ref, a fast-forward, and a push after an amend
	// all succeed under it; only a genuinely divergent remote does not.
	Force bool

	// Lease is the commit the remote branch must still hold for a Force push
	// to land: the head the factory last published. Empty leaves git to lease
	// against the checkout's remote-tracking ref, which is what a mid-attempt
	// publication with no recorded head has.
	//
	// A REWRITE NAMES IT EXPLICITLY. A worker rebasing onto its base runs
	// `git fetch`, and a fetch refreshes the very tracking ref an implicit
	// lease compares against. So an implicit lease would accept a remote that
	// moved while the worker was working. Pinned to the published head, the
	// push lands only on the branch the factory itself left there.
	Lease string
}

// gitConfigArgs are the "-c key=value" pairs every git invocation carries.
//
// SIGNING IS ALWAYS STATED, never left to the image. A worker image whose
// global git config sets commit.gpgsign — which a developer-derived image
// commonly does — would otherwise decide for the project, and a commit
// carrying a signature the project did not choose is a provenance claim that
// is wrong and permanent.
func gitConfigArgs(project *orchestration.Project, signing *SigningSession) []string {
	args := []string{
		"-c", "user.name=" + project.Git.Author.Name,
		"-c", "user.email=" + project.Git.Author.Email,
	}
	if signing == nil {
		return append(args, "-c", "commit.gpgsign=false")
	}
	return append(args,
		"-c", "gpg.format=ssh",
		"-c", "user.signingkey=key::"+signing.PublicKey,
		"-c", "commit.gpgsign=true",
	)
}

// credentialHelperArgs teach git to read the forge token out of the file the
// spawn mounted, for the one repository URL scheme that needs it.
//
// The helper is a SHELL SNIPPET NAMING A PATH. It is the shape the repository
// already uses for a container credential (account_materialize.go): the value
// is read at the moment git asks for it, by a process inside the container,
// and never appears in an argument vector any other process on the host can
// read.
//
// An empty first helper clears whatever the image configured, so the token is
// the only credential git can offer.
func credentialHelperArgs(repoURL string) []string {
	if !strings.HasPrefix(repoURL, "https://") {
		return nil
	}
	tokenFile := path.Join(orchestration.WorkspaceSpecDir, orchestration.WorkspaceForgeTokenFile)
	helper := "!f(){ echo username=" + forgeTokenUsername + "; echo \"password=$(cat " + tokenFile + ")\"; }; f"
	return []string{"-c", "credential.helper=", "-c", "credential.helper=" + helper}
}

// forgeTokenUsername is the username a forge token is presented under. Gitea,
// GitHub and GitLab all authenticate on the token and ignore the name; a
// fixed, obviously-not-a-person value keeps the audit trail honest.
const forgeTokenUsername = "x-access-token"

// publisher runs the publish steps in the worker's container.
type publisher struct {
	// exec runs one command in the container, returning what it did.
	exec func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error)

	// project supplies the git identity and the repository URL.
	project *orchestration.Project

	// signing is the attempt's signing agent, nil when the project signs
	// nothing.
	signing *SigningSession

	// taskID names the attempt in every error.
	taskID string
}

// git runs one git command in the checkout and refuses on a non-zero exit.
func (p *publisher) git(plan publishPlan, args ...string) (*orchestration.ExecResult, error) {
	result, err := p.exec(&orchestration.ExecSpec{
		Cmd:     append([]string{gitCommand}, args...),
		WorkDir: plan.WorkDir,
	})
	if err != nil {
		return nil, err
	}
	if !result.Succeeded() {
		return nil, &PublishCommandError{
			TaskID:   p.taskID,
			Command:  strings.Join(args, " "),
			ExitCode: result.ExitCode,
			Output:   strings.TrimSpace(result.Stderr + "\n" + result.Stdout),
		}
	}
	return result, nil
}

// probe runs one git command whose EXIT STATUS is the answer, so a non-zero
// exit is a "no" rather than a failure.
func (p *publisher) probe(plan publishPlan, args ...string) (*orchestration.ExecResult, error) {
	return p.exec(&orchestration.ExecSpec{
		Cmd:     append([]string{gitCommand}, args...),
		WorkDir: plan.WorkDir,
	})
}

// HasChanges reports whether the worker left anything uncommitted.
func (p *publisher) HasChanges(plan publishPlan) (bool, error) {
	result, err := p.git(plan, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return result.Output() != "", nil
}

// AheadOfBase reports whether the branch carries commits its base does not.
func (p *publisher) AheadOfBase(plan publishPlan) (bool, error) {
	result, err := p.probe(plan, "rev-list", "--count", gitRemote+"/"+plan.Base+"..HEAD")
	if err != nil {
		return false, err
	}
	if !result.Succeeded() {
		// A base the container cannot resolve is not "not ahead": answering
		// false would let an attempt with real commits be discarded as empty.
		return false, &PublishCommandError{
			TaskID:   p.taskID,
			Command:  "rev-list --count " + gitRemote + "/" + plan.Base + "..HEAD",
			ExitCode: result.ExitCode,
			Output:   strings.TrimSpace(result.Stderr),
		}
	}
	count, convErr := strconv.Atoi(result.Output())
	if convErr != nil {
		return false, &PublishCommandError{
			TaskID:  p.taskID,
			Command: "rev-list --count",
			Output:  "git answered " + strconv.Quote(result.Output()) + " for a count",
		}
	}
	return count > 0, nil
}

// Commit records everything the worker changed as one signed commit.
func (p *publisher) Commit(plan publishPlan) error {
	if _, err := p.git(plan, "add", "--all"); err != nil {
		return err
	}
	args := gitConfigArgs(p.project, p.signing)
	args = append(args, "commit", "-m", plan.Message)
	_, err := p.git(plan, args...)
	return err
}

// Push publishes the branch and answers the head that landed.
func (p *publisher) Push(plan publishPlan) (string, error) {
	refspec := plan.Branch
	if plan.RemoteBranch != "" {
		refspec = plan.Branch + ":" + plan.RemoteBranch
	}
	args := credentialHelperArgs(p.project.Git.RepoURL)
	args = append(args, "push")
	if plan.Force {
		args = append(args, forceWithLease(plan))
	}
	args = append(args, gitRemote, refspec)
	if _, err := p.git(plan, args...); err != nil {
		return "", err
	}
	return p.Head(plan)
}

// forceWithLease is the lease flag for a Force publication: pinned to the
// plan's Lease on the ref being published when it names one, and git's own
// remote-tracking lease when it does not. Never an unleased force.
func forceWithLease(plan publishPlan) string {
	if plan.Lease == "" {
		return "--force-with-lease"
	}
	ref := plan.Branch
	if plan.RemoteBranch != "" {
		ref = plan.RemoteBranch
	}
	return "--force-with-lease=" + ref + ":" + plan.Lease
}

// Head is the commit the branch now points at.
func (p *publisher) Head(plan publishPlan) (string, error) {
	result, err := p.git(plan, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return result.Output(), nil
}

// Publish is the whole publication: commit what is uncommitted, refuse an
// attempt that produced nothing, push, and answer the head.
func (p *publisher) Publish(plan publishPlan) (string, error) {
	changed, err := p.HasChanges(plan)
	if err != nil {
		return "", err
	}
	if changed {
		if err := p.Commit(plan); err != nil {
			return "", err
		}
	}
	ahead, err := p.AheadOfBase(plan)
	if err != nil {
		return "", err
	}
	if !changed && !ahead {
		return "", &NoChangesError{TaskID: p.taskID, WorkDir: plan.WorkDir}
	}
	return p.Push(plan)
}
