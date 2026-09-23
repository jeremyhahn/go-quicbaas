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

	"golang.org/x/crypto/ssh"

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
	for _, field := range signingConfig(signing.PublicKey) {
		args = append(args, "-c", field.Key+"="+field.Value)
	}
	return args
}

// gitConfigField is one git configuration key and its value.
type gitConfigField struct{ Key, Value string }

// signingConfig is the git configuration that signs commits with publicKey's
// private half, which the signing agent holds. The publisher passes it on its
// own invocations and the checkout writes it into the repository, so a commit
// the WORKER makes is signed by the same key as one the publisher makes.
func signingConfig(publicKey string) []gitConfigField {
	return []gitConfigField{
		{"gpg.format", "ssh"},
		{"user.signingkey", "key::" + publicKey},
		{"commit.gpgsign", "true"},
		{"gpg.ssh.allowedSignersFile", allowedSignersPath},
	}
}

// allowedSignersPath is where the container reads the allowed_signers file
// the daemon wrote beside the spec.
var allowedSignersPath = path.Join(orchestration.WorkspaceSpecDir, orchestration.WorkspaceAllowedSignersFile)

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
//
// It answers whether a commit was made. Staging can find nothing even though
// status reported a change — a file rewritten to its committed content between
// the two — and "git commit" then exits 1 on "nothing to commit". That is not a
// failure: it is an attempt whose change is already in the branch, and
// refusing it discarded finished work the worker had committed itself
// (go-quicbaas#1048). The index is asked, so no empty commit is attempted.
func (p *publisher) Commit(plan publishPlan) (bool, error) {
	if _, err := p.git(plan, "add", "--all"); err != nil {
		return false, err
	}
	staged, err := p.HasStaged(plan)
	if err != nil || !staged {
		return false, err
	}
	args := gitConfigArgs(p.project, p.signing)
	args = append(args, "commit", "-m", plan.Message)
	if _, err := p.git(plan, args...); err != nil {
		return false, err
	}
	return true, nil
}

// HasStaged reports whether the index holds anything to commit.
// "git diff --cached --quiet" exits 1 when it does and 0 when it does not; any
// other exit is git failing, which is refused rather than read as either.
func (p *publisher) HasStaged(plan publishPlan) (bool, error) {
	result, err := p.probe(plan, "diff", "--cached", "--quiet")
	if err != nil {
		return false, err
	}
	switch result.ExitCode {
	case 0:
		return false, nil
	case 1:
		return true, nil
	}
	return false, &PublishCommandError{
		TaskID:   p.taskID,
		Command:  "diff --cached --quiet",
		ExitCode: result.ExitCode,
		Output:   strings.TrimSpace(result.Stderr),
	}
}

// VerifySigned refuses the publication when any commit the branch carries
// beyond its base is not signed by the attempt's key.
//
// A commit passes only when git reports a good signature ("G") AND the
// signing key's fingerprint is the SHA-256 fingerprint of the attempt's SSH
// public key. The status alone is not enough: git picks the verification
// backend from the signature's own format, so a trusted OpenPGP signature
// reports "G" without the SSH allowed_signers file ever being consulted. A
// project that signs nothing has nothing to verify.
func (p *publisher) VerifySigned(plan publishPlan) error {
	if p.signing == nil {
		return nil
	}
	want, err := sshKeyFingerprint(p.signing.PublicKey)
	if err != nil {
		return &SigningSessionError{Scope: SigningScopeDelivery, Message: "its public key does not parse: " + err.Error()}
	}
	// THE RANGE ENDS AT THE REF BEING PUSHED, not at HEAD. A worker can
	// leave an unsigned commit on the delivery branch and check out a signed
	// ancestor: HEAD would verify while the push publishes the branch.
	rangeSpec := gitRemote + "/" + plan.Base + ".." + pushSource(plan)
	result, err := p.git(plan,
		"-c", "gpg.ssh.allowedSignersFile="+allowedSignersPath,
		"log", "--format=%H %G? %GF", rangeSpec)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(result.Output(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		commit, status, signer := fields[0], "", ""
		if len(fields) > 1 {
			status = fields[1]
		}
		if len(fields) > 2 {
			signer = fields[2]
		}
		if status != "G" || signer != want {
			return &UnsignedCommitError{TaskID: p.taskID, Commit: commit, Status: status, Signer: signer}
		}
	}
	return nil
}

// sshKeyFingerprint is the SHA-256 fingerprint ("SHA256:<base64>") of an
// OpenSSH authorized_keys line, which is how git reports an SSH signer (%GF).
func sshKeyFingerprint(authorizedKey string) (string, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(authorizedKey))
	if err != nil {
		return "", err
	}
	return ssh.FingerprintSHA256(key), nil
}

// Push publishes the branch and answers the head that landed.
//
// EVERY PUSH IS A PLAIN, FAST-FORWARD PUSH. The factory never rewrites a
// pushed branch: no force of any kind, leased or not (owner ruling
// 2026-09-04, handbook version-control §5b). A branch that conflicts with its
// base is brought up to date by merging the base into it, which the pushed
// head is an ancestor of, so the push still fast-forwards. A remote that
// refuses the push as non-fast-forward is a real error and surfaces as a
// PublishCommandError: the local branch no longer descends from what was
// published, and replacing the remote would discard a commit.
//
// EVERY PUSH IS VERIFIED HERE, not by its callers: the end-of-turn publish,
// the teardown publish of a failed attempt and anything added later share
// this one path, so no route to the forge can carry a commit the attempt's
// key did not sign.
func (p *publisher) Push(plan publishPlan) (string, error) {
	if err := p.VerifySigned(plan); err != nil {
		return "", err
	}
	refspec := plan.Branch
	if plan.RemoteBranch != "" {
		refspec = plan.Branch + ":" + plan.RemoteBranch
	}
	args := credentialHelperArgs(p.project.Git.RepoURL)
	args = append(args, "push", gitRemote, refspec)
	if _, err := p.git(plan, args...); err != nil {
		return "", err
	}
	return p.Head(plan)
}

// pushSource is the local ref a publication pushes, spelled in full so that
// nothing (a tag, a detached HEAD, a remote-tracking ref) can stand in for it.
func pushSource(plan publishPlan) string {
	return "refs/heads/" + plan.Branch
}

// Head is the commit the branch now points at: the branch's own ref, which is
// what Push publishes, not whatever the worker left checked out.
func (p *publisher) Head(plan publishPlan) (string, error) {
	result, err := p.git(plan, "rev-parse", pushSource(plan))
	if err != nil {
		return "", err
	}
	return result.Output(), nil
}

// Publish is the whole publication: commit what is uncommitted, refuse an
// attempt that produced nothing or carries a commit the attempt's key did not
// sign, push, and answer the head.
func (p *publisher) Publish(plan publishPlan) (string, error) {
	changed, err := p.HasChanges(plan)
	if err != nil {
		return "", err
	}
	committed := false
	if changed {
		if committed, err = p.Commit(plan); err != nil {
			return "", err
		}
	}
	ahead, err := p.AheadOfBase(plan)
	if err != nil {
		return "", err
	}
	if !committed && !ahead {
		return "", &NoChangesError{TaskID: p.taskID, WorkDir: plan.WorkDir}
	}
	return p.Push(plan)
}
