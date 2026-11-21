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
	"time"

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
	if signing.Helper != nil {
		args = append(args, "-c", "gpg.ssh.program="+signing.Helper.Program)
	}
	return args
}

// signingEnv is the environment a signing git command needs: the helper's,
// when the session signs through it, and none when it signs through the
// socket, whose path the container already carries.
func signingEnv(signing *SigningSession) map[string]string {
	if signing == nil || signing.Helper == nil {
		return nil
	}
	return signing.Helper.Env
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
// runner wrote on the container's credential tier (a tmpfs), for the one
// repository URL scheme that needs it.
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
	helper := "!f(){ echo username=" + forgeTokenUsername + "; echo \"password=$(cat " +
		orchestration.WorkspaceForgeCredentialFile + ")\"; }; f"
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

	// cleanup runs one command whose job is to UNDO something this publisher
	// already did, on a context that does NOT end when the attempt's does.
	//
	// A CANCELLED ATTEMPT IS EXACTLY WHEN CLEANUP MATTERS. `exec` carries the
	// stage's context, so a removal issued through it after a timeout or a
	// shutdown is refused before it ever reaches the container — and what is
	// then left behind is a git worktree inside a checkout that OUTLIVES the
	// job on a persistent workspace tier, which the next attempt on that node
	// finds in its way. The bound on the detached context belongs to whoever
	// supplies this seam; nil falls back to exec, which is the honest answer
	// for a caller that has no second seam rather than a silent one.
	cleanup func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error)

	// project supplies the git identity and the repository URL.
	project *orchestration.Project

	// signing is the attempt's signing agent, nil when the project signs
	// nothing.
	signing *SigningSession

	// taskID names the attempt in every error.
	taskID string

	// admit re-reads the task and refuses when the attempt no longer holds
	// it. Push asks it before every push, so a block that lands while the
	// work is being committed still stops the publication. Required: a
	// publisher with no way to ask pushes nothing. See attempt_admission.go.
	admit func() error
}

// git runs one git command in the checkout and refuses on a non-zero exit.
func (p *publisher) git(plan publishPlan, args ...string) (*orchestration.ExecResult, error) {
	return p.gitEnv(plan, nil, args...)
}

// gitEnv is git with an environment added to the one the container already
// has, for the single caller that has to name GIT_INDEX_FILE.
//
// THE WORKER'S INDEX IS THE WORKER'S. Staging the working tree to answer
// "what would the gate judge" must not disturb what the worker has staged for
// its own commit, and the only way git stages anywhere else is to be told so
// in the environment. See SnapshotWorkTree in merge_result.go.
func (p *publisher) gitEnv(plan publishPlan, env map[string]string, args ...string) (*orchestration.ExecResult, error) {
	result, err := p.exec(&orchestration.ExecSpec{
		Cmd:     append([]string{gitCommand}, args...),
		Env:     env,
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
	if _, err := p.gitEnv(plan, signingEnv(p.signing), args...); err != nil {
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
// beyond its base was not signed by a key the project trusts for it.
//
// A commit passes when git reports a good signature ("G") AND the signing
// key's fingerprint is one the project trusts:
//
//   - the attempt's own key, for any commit; or
//   - a key in the project's trusted signing history (CommitSigning.Retired),
//     for a commit the project's own identity committed BEFORE that key was
//     retired. A rotation must not orphan the pull requests the old key
//     signed, and the factory never rewrites a pushed branch to re-sign them.
//
// The status alone is not enough: git picks the verification backend from
// the signature's own format, so a trusted OpenPGP signature reports "G"
// without the SSH allowed_signers file ever being consulted. The retirement
// time is enforced twice — by the allowed_signers file's valid-before, which
// git checks against the commit's time, and here — so neither a git that
// ignores the option nor a file written without it can widen the window. A
// commit someone else committed is held to the attempt's key alone, exactly
// as before the history existed. A project that signs nothing has nothing to
// verify.
func (p *publisher) VerifySigned(plan publishPlan) error {
	// THE RANGE ENDS AT THE REF BEING PUSHED, not at HEAD. A worker can
	// leave an unsigned commit on the delivery branch and check out a signed
	// ancestor: HEAD would verify while the push publishes the branch.
	return p.verifyRange(plan.WorkDir, gitRemote+"/"+plan.Base+".."+pushSource(plan))
}

// verifySignedFormat is the git log format verifyRange reads: the commit, the
// signature status, the committer time and address, and the signer's
// fingerprint last because it is the one field that may be empty.
const verifySignedFormat = "--format=%H%x1f%G?%x1f%ct%x1f%ce%x1f%GF"

// verifyRange checks every commit in rangeSpec, in the checkout at workDir,
// against the project's trusted signing keys. See VerifySigned.
func (p *publisher) verifyRange(workDir, rangeSpec string) error {
	if p.signing == nil {
		return nil
	}
	trust, err := p.trustedSigners()
	if err != nil {
		return err
	}
	result, err := p.git(publishPlan{WorkDir: workDir},
		"-c", "gpg.ssh.allowedSignersFile="+allowedSignersPath,
		"log", verifySignedFormat, rangeSpec)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(result.Output(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		commit := parseSignedCommit(line)
		if !trust.admits(commit) {
			return &UnsignedCommitError{TaskID: p.taskID, Commit: commit.SHA, Status: commit.Status, Signer: commit.Signer}
		}
	}
	return nil
}

// signedCommit is one line of verifySignedFormat.
type signedCommit struct {
	SHA, Status, Committer, Signer string

	// Time is the committer time; zero when git gave none that parses.
	Time time.Time
}

// parseSignedCommit reads one line of verifySignedFormat. A missing or
// unreadable field is left empty, which no trust rule admits.
func parseSignedCommit(line string) signedCommit {
	fields := strings.Split(strings.TrimSpace(line), "\x1f")
	field := func(i int) string {
		if i < len(fields) {
			return strings.TrimSpace(fields[i])
		}
		return ""
	}
	commit := signedCommit{SHA: field(0), Status: field(1), Committer: field(3), Signer: field(4)}
	if seconds, err := strconv.ParseInt(field(2), 10, 64); err == nil {
		commit.Time = time.Unix(seconds, 0).UTC()
	}
	return commit
}

// trustedSigners is what one verification trusts: the attempt's key, and the
// project's retired keys by fingerprint with their retirement times.
type trustedSigners struct {
	current  string
	identity string
	retired  map[string]time.Time
}

// trustedSigners builds the trust for this publisher's project and session.
func (p *publisher) trustedSigners() (trustedSigners, error) {
	current, err := sshKeyFingerprint(p.signing.PublicKey)
	if err != nil {
		return trustedSigners{}, &SigningSessionError{Scope: SigningScopeDelivery, Message: "its public key does not parse: " + err.Error()}
	}
	trust := trustedSigners{current: current, retired: make(map[string]time.Time)}
	if p.project == nil {
		return trust, nil
	}
	trust.identity = strings.TrimSpace(p.project.Git.Author.Email)
	for _, old := range p.project.Git.Signing.Retired {
		// An entry that does not validate is not trusted: the history
		// admits less than it names, never more.
		if old.Validate() != nil {
			continue
		}
		fingerprint, err := old.Fingerprint()
		if err != nil {
			continue
		}
		trust.retired[fingerprint] = old.RetiredAt
	}
	return trust, nil
}

// admits reports whether commit's signature is one the project trusts.
func (t trustedSigners) admits(commit signedCommit) bool {
	if commit.Status != "G" || commit.Signer == "" {
		return false
	}
	if commit.Signer == t.current {
		return true
	}
	retiredAt, ok := t.retired[commit.Signer]
	if !ok || t.identity == "" || commit.Time.IsZero() {
		return false
	}
	return strings.EqualFold(commit.Committer, t.identity) && commit.Time.Before(retiredAt)
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
//
// THE TASK IS RE-READ LAST, immediately before the push (admit). The
// verification runs a command in the container, and a block that lands on
// another node while it runs must still stop the push. A read made before the
// verification would miss that block.
func (p *publisher) Push(plan publishPlan) (string, error) {
	if p.admit == nil {
		return "", &InvalidInputError{Field: "admit", Message: "a publisher that cannot re-read its task must not push"}
	}
	if err := p.VerifySigned(plan); err != nil {
		return "", err
	}
	if err := p.admit(); err != nil {
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
