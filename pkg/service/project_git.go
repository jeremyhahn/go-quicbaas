// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package service

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// gitTokenEnvVar carries the resolved HTTPS token into the git child process.
// The token travels ONLY in the child environment: the credential helper text
// on the command line references the variable by name, so the secret never
// appears in process arguments and is never written to disk.
const gitTokenEnvVar = "QUICBAAS_GIT_TOKEN" //nolint:gosec // G101: an environment variable name, not a credential

// gitAgentShutdownBound is how long a teardown waits for the ssh agent's
// connection handlers to finish before reporting what it had to abandon.
//
// The bound is the caller's to choose, and choosing it is not optional: a
// handler wedged inside an uninterruptible Signer.Sign — a TPM awaiting touch,
// a stalled KMS — cannot be woken by closing its connection, so an unbounded
// wait hands control of the module's shutdown to that device. The socket is
// already closed and every client disconnected before the wait begins, so the
// bound trades no authorization: it only decides how long we watch work that
// has nowhere left to deliver a signature.
const gitAgentShutdownBound = 10 * time.Second

// gitUsernameEnvVar carries the username paired with the HTTPS token.
const gitUsernameEnvVar = "QUICBAAS_GIT_USERNAME"

// defaultGitTokenUsername is the username sent alongside an HTTPS token when
// the caller supplies none. Token-authenticated forges (GitHub, GitLab,
// Gitea) accept an arbitrary non-empty username for PAT auth.
const defaultGitTokenUsername = "x-access-token"

// gitTokenCredentialHelper is the inline git credential helper that answers
// credential fills from the child environment. It contains no secret itself.
const gitTokenCredentialHelper = `!f() { echo "username=${` + gitUsernameEnvVar + `}"; echo "password=${` + gitTokenEnvVar + `}"; }; f`

// =============================================================================
// Typed errors
// =============================================================================

// GitRepoURLError reports a repository URL the provisioner cannot clone.
type GitRepoURLError struct {
	// URL is the rejected repository URL.
	URL string

	// Reason states why the URL was rejected.
	Reason string
}

// Error implements the error interface.
func (e *GitRepoURLError) Error() string {
	return "git: invalid repository url " + strconv.Quote(e.URL) + ": " + e.Reason
}

// GitCredentialError reports a failure resolving the credential referenced by
// a clone request. The credential material itself never appears in the error.
type GitCredentialError struct {
	// Ref identifies the credential reference (secret ID or SSH key name).
	Ref string

	// Reason states why resolution failed.
	Reason string

	// Err is the underlying resolution error, when one exists.
	Err error
}

// Error implements the error interface.
func (e *GitCredentialError) Error() string {
	msg := "git: credential " + strconv.Quote(e.Ref) + ": " + e.Reason
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the underlying error to errors.Is / errors.As.
func (e *GitCredentialError) Unwrap() error { return e.Err }

// GitCloneError reports a failed git operation. Output is the combined
// stdout/stderr of the git child process, which never contains credential
// material (tokens travel in the environment, SSH keys never leave xkms).
type GitCloneError struct {
	// RepoURL is the repository the operation targeted.
	RepoURL string

	// Stage names the failed step ("clone" or "rev-parse").
	Stage string

	// Output is the git child process output.
	Output string

	// Err is the underlying process error.
	Err error
}

// Error implements the error interface.
//
// The URL is REDACTED of userinfo before it is rendered. A clone URL may carry
// "https://user:token@host", and this error is logged and persisted onto the
// project record — so an unredacted render writes a live credential into two
// places that outlive the failure.
func (e *GitCloneError) Error() string {
	msg := "git: " + e.Stage + " " + strconv.Quote(redactURLCredentials(e.RepoURL)) + " failed"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if out := strings.TrimSpace(e.Output); out != "" {
		msg += ": " + redactURLCredentials(out)
	}
	return msg
}

// Unwrap exposes the underlying process error to errors.Is / errors.As.
func (e *GitCloneError) Unwrap() error { return e.Err }

// GitCloneInvalidError reports a clone request that fails validation before
// any process is started.
type GitCloneInvalidError struct {
	// Field names the offending request field.
	Field string

	// Reason states why the field is invalid.
	Reason string
}

// Error implements the error interface.
func (e *GitCloneInvalidError) Error() string {
	return "git: invalid clone request: " + e.Field + ": " + e.Reason
}

// =============================================================================
// GitCloneRequest
// =============================================================================

// GitCloneRequest describes one clone into a managed workspace. Credential
// fields are references into the quicbaas secrets store / xkms SSH key store;
// the material they name is resolved at clone time and never persisted.
type GitCloneRequest struct {
	// RepoURL is the repository to clone. Required.
	RepoURL string

	// Branch is the branch to clone. Empty clones the remote default branch.
	Branch string

	// Dir is the absolute workspace directory to clone into. Required.
	Dir string

	// GitCredentials carries the authentication half. Embedded, so every
	// existing field access (req.TokenSecretRef, req.SSHKey, req.TenantID)
	// keeps working unchanged.
	GitCredentials
}

// GitCredentials is the authentication half of any git operation against a
// remote.
//
// Separated from GitCloneRequest so push, fetch and every later operation
// compose the SAME credential path a clone does rather than growing a second
// one. There is exactly one place that turns these fields into a child
// environment, and it is authenticate below — a second spelling would drift,
// and the thing it would drift about is where a token is allowed to appear.
type GitCredentials struct {
	// TenantID scopes credential resolution and SSH host-key pins.
	TenantID string

	// TokenSecretRef names a secret (SecretID) in the quicbaas secrets store
	// holding an HTTPS access token. Requires an https:// RepoURL. Mutually
	// exclusive with SSHKey.
	TokenSecretRef string

	// TokenUsername is the username paired with the HTTPS token. Empty
	// selects defaultGitTokenUsername.
	TokenUsername string

	// SSHKey references an xkms-held SSH key used for git-over-SSH auth.
	// Selected when SSHKey.Name is non-empty. Mutually exclusive with
	// TokenSecretRef.
	SSHKey SSHKeyRef
}

// Validate reports credential combinations that cannot be honoured.
func (c GitCredentials) Validate() error {
	if c.TokenSecretRef != "" && c.SSHKey.Name != "" {
		return &GitCloneInvalidError{
			Field:  "credentials",
			Reason: "token secret ref and ssh key ref are mutually exclusive",
		}
	}
	return nil
}

// =============================================================================
// GitProvisioner
// =============================================================================

// GitProvisioner clones project repositories into managed workspaces using
// the system git client. Authentication composes the platform's own stores:
// HTTPS tokens resolve through the quicbaas SecretsService and reach git via
// the child environment only; SSH keys stay inside xkms — the xkms SSH SDK's
// StartConnect serves the key over an ephemeral in-process SSH agent that the
// system ssh client (driven by git via GIT_SSH_COMMAND) authenticates with,
// so no private key ever touches disk.
type GitProvisioner struct {
	// Secrets resolves HTTPS token references. Optional; a clone carrying a
	// TokenSecretRef fails with GitCredentialError when nil.
	Secrets SecretsService

	// SSHSigner resolves xkms SSH key references. Optional; a clone carrying
	// an SSHKey fails with GitCredentialError when nil.
	SSHSigner SSHSignerResolver

	// HostKeys is the xkms host-key pin store SSH clones verify against.
	// First-contact host keys are pinned automatically (accept-new) and
	// verified on every later clone. Required for SSH clones.
	HostKeys xkmsssh.HostKeyStore

	// GitBinary overrides the git executable. Empty selects "git" from PATH.
	GitBinary string

	// Logger receives non-fatal diagnostics (e.g. a host-key pin sync that
	// failed after a successful clone). Optional.
	Logger interfaces.Logger
}

// Clone clones the requested repository into req.Dir and returns the
// resulting HEAD commit SHA. The target directory must already exist (the
// workspace manager provisions it); git itself requires it to be empty.
func (p *GitProvisioner) Clone(ctx context.Context, req *GitCloneRequest) (string, error) {
	if req == nil {
		return "", &GitCloneInvalidError{Field: "request", Reason: "must not be nil"}
	}
	if req.RepoURL == "" {
		return "", &GitCloneInvalidError{Field: "repo_url", Reason: "repository URL is required"}
	}
	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return "", &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	if err := req.Validate(); err != nil {
		return "", err
	}

	env, authArgs, stop, err := p.authenticate(ctx, req.RepoURL, req.GitCredentials)
	if err != nil {
		return "", err
	}
	defer stop()

	// A fresh slice rather than appending onto authArgs: appending to a slice
	// the credential path owns can write into its backing array, and the thing
	// that array holds is credential-helper configuration.
	args := make([]string, 0, len(authArgs)+8)
	args = append(args, authArgs...)
	args = append(args, gitCloneArgs(req.RepoURL, req.Branch, req.Dir)...)
	if err := p.runGit(ctx, req.RepoURL, "clone", env, args); err != nil {
		return "", err
	}
	return p.headSHA(ctx, req.RepoURL, req.Dir)
}

// authenticate composes the child environment and git arguments for one
// operation against repoURL.
//
// THE ONE PLACE credentials become a child process's environment. Clone, push
// and everything after them go through here, so the rules that matter — a
// token appears only in the environment, an SSH private key never leaves
// xkms, host keys are pinned per tenant — hold for every operation rather than
// for whichever ones remembered.
//
// The returned stop function is always non-nil and must always be called; it
// syncs first-contact host-key pins and tears the ephemeral agent down.
func (p *GitProvisioner) authenticate(ctx context.Context, repoURL string, creds GitCredentials) (env, args []string, stop func(), err error) {
	env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	stop = func() {}

	switch {
	case creds.TokenSecretRef != "":
		tokenEnv, tokenArgs, tokenErr := p.tokenAuth(ctx, repoURL, creds)
		if tokenErr != nil {
			return nil, nil, stop, tokenErr
		}
		env = append(env, tokenEnv...)
		args = tokenArgs

	case creds.SSHKey.Name != "":
		sshEnv, sshStop, sshErr := p.sshAuth(ctx, repoURL, creds)
		if sshErr != nil {
			return nil, nil, stop, sshErr
		}
		env = append(env, sshEnv...)
		stop = sshStop
	}
	return env, args, stop, nil
}

// tokenAuth resolves the HTTPS token reference and returns the child
// environment entries plus the git config arguments installing the inline
// credential helper. The token appears only in the environment.
func (p *GitProvisioner) tokenAuth(ctx context.Context, repoURL string, creds GitCredentials) ([]string, []string, error) {
	if !strings.HasPrefix(repoURL, "https://") {
		return nil, nil, &GitRepoURLError{
			URL:    repoURL,
			Reason: "token authentication requires an https:// repository URL",
		}
	}
	if p.Secrets == nil {
		return nil, nil, &GitCredentialError{
			Ref:    creds.TokenSecretRef,
			Reason: "secrets service is not configured",
		}
	}
	resp, err := p.Secrets.GetSecret(ctx, &GetSecretRequest{SecretID: creds.TokenSecretRef})
	if err != nil {
		return nil, nil, &GitCredentialError{
			Ref:    creds.TokenSecretRef,
			Reason: "failed to resolve token secret",
			Err:    err,
		}
	}
	token := strings.TrimSpace(string(resp.Data))
	if token == "" {
		return nil, nil, &GitCredentialError{
			Ref:    creds.TokenSecretRef,
			Reason: "token secret is empty",
		}
	}
	username := creds.TokenUsername
	if username == "" {
		username = defaultGitTokenUsername
	}
	env := []string{
		gitTokenEnvVar + "=" + token,
		gitUsernameEnvVar + "=" + username,
	}
	args := []string{"-c", "credential.helper=" + gitTokenCredentialHelper}
	return env, args, nil
}

// sshAuth resolves the xkms SSH key, starts the ephemeral agent + seeded
// known_hosts via the xkms SSH SDK, and returns the child environment
// (SSH_AUTH_SOCK + GIT_SSH_COMMAND) plus a stop function that syncs
// first-contact host-key pins back into the store and tears the agent down.
func (p *GitProvisioner) sshAuth(ctx context.Context, repoURL string, creds GitCredentials) ([]string, func(), error) {
	endpoint, err := parseGitSSHEndpoint(repoURL)
	if err != nil {
		return nil, nil, err
	}
	if p.SSHSigner == nil {
		return nil, nil, &GitCredentialError{
			Ref:    creds.SSHKey.Name,
			Reason: "ssh signer resolver is not configured",
		}
	}
	if p.HostKeys == nil {
		return nil, nil, &GitCredentialError{
			Ref:    creds.SSHKey.Name,
			Reason: "ssh host-key store is not configured",
		}
	}
	// One effective (name, scope, owner) triple selects both the signing key
	// and the host-key pin namespace: scope defaults to "tenant" and owner to
	// the requesting tenant, so one tenant's keys and pins can never be
	// resolved on behalf of another by omission.
	keyRef := creds.SSHKey
	if keyRef.Scope == "" {
		keyRef.Scope = "tenant"
	}
	if keyRef.Owner == "" {
		keyRef.Owner = creds.TenantID
	}
	signer, err := p.SSHSigner.Signer(ctx, keyRef)
	if err != nil {
		return nil, nil, &GitCredentialError{
			Ref:    creds.SSHKey.Name,
			Reason: "failed to resolve ssh key",
			Err:    err,
		}
	}

	session, err := xkmsssh.StartConnect(ctx, xkmsssh.ConnectOptions{
		Host:                  endpoint.host,
		Port:                  endpoint.port,
		User:                  endpoint.user,
		Signer:                signer,
		HostKeys:              p.HostKeys,
		Scope:                 keyRef.Scope,
		Owner:                 keyRef.Owner,
		StrictHostKeyChecking: "accept-new",
	})
	if err != nil {
		return nil, nil, &GitCredentialError{
			Ref:    creds.SSHKey.Name,
			Reason: "failed to start ssh agent session",
			Err:    err,
		}
	}

	env := append(append([]string(nil), session.Env...),
		"GIT_SSH_COMMAND="+buildGitSSHCommand(session.KnownHostsFile()))
	stop := func() {
		// Pin any first-contact host key the clone accepted, then stop the
		// agent and remove its temp artifacts. Sync failures only lose the
		// pin (the next clone re-pins); they cannot fail a finished clone.
		shutdownCtx, cancel := p.shutdownContext(ctx)
		defer cancel()
		_, report, syncErr := session.SyncAndStop(shutdownCtx)
		if syncErr != nil && p.Logger != nil {
			p.Logger.Warnf("git ssh host-key pin sync failed: %v", syncErr)
		}
		p.reportAgentShutdown("git ssh session", report)
	}
	return env, stop, nil
}

// redactURLCredentials removes userinfo from any URL in text.
//
// Applied to git's combined output as well as the URL itself, because git
// echoes the remote back in its own messages — redacting only the field we
// control would leave the credential in the half we do not.
func redactURLCredentials(text string) string {
	return urlUserinfoPattern.ReplaceAllString(text, "$1//[redacted]@")
}

// urlUserinfoPattern matches the userinfo component of a URL.
var urlUserinfoPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*:)//[^/@\s]*@`)

// runGit executes one git command with the prepared environment.
func (p *GitProvisioner) runGit(ctx context.Context, repoURL, stage string, env, args []string) error {
	cmd := exec.CommandContext(ctx, p.gitBinary(), args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &GitCloneError{RepoURL: repoURL, Stage: stage, Output: string(out), Err: err}
	}
	return nil
}

// headSHA returns the workspace's HEAD commit after a successful clone.
func (p *GitProvisioner) headSHA(ctx context.Context, repoURL, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, p.gitBinary(), "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", &GitCloneError{RepoURL: repoURL, Stage: "rev-parse", Output: string(out), Err: err}
	}
	return strings.TrimSpace(string(out)), nil
}

// gitBinary returns the configured git executable, defaulting to "git".
func (p *GitProvisioner) gitBinary() string {
	if p.GitBinary != "" {
		return p.GitBinary
	}
	return "git"
}

// gitCloneArgs builds the clone argument list. The URL is placed after "--"
// so a hostile URL can never be parsed as an option.
func gitCloneArgs(repoURL, branch, dir string) []string {
	args := []string{"clone"}
	if branch != "" {
		args = append(args, "--branch", branch, "--single-branch")
	}
	return append(args, "--", repoURL, dir)
}

// buildGitSSHCommand renders the GIT_SSH_COMMAND value that points git's ssh
// invocations at the ephemeral agent's seeded known_hosts file. BatchMode
// forbids every interactive prompt so a background clone can never hang, and
// IdentitiesOnly pins authentication to the agent-served xkms key.
func buildGitSSHCommand(knownHostsFile string) string {
	return "ssh -o BatchMode=yes -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new" +
		" -o UserKnownHostsFile=" + shellSingleQuote(knownHostsFile)
}

// shellSingleQuote quotes s for POSIX shell word splitting (GIT_SSH_COMMAND
// is parsed with shell rules).
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// gitSSHEndpoint is a parsed SSH remote.
type gitSSHEndpoint struct {
	user string
	host string
	port int
}

// parseGitSSHEndpoint extracts user/host/port from an SSH repository URL.
// Both the ssh:// scheme and the scp-like "user@host:path" form are
// supported, matching what git itself accepts for SSH remotes.
func parseGitSSHEndpoint(repoURL string) (*gitSSHEndpoint, error) {
	if strings.HasPrefix(repoURL, "ssh://") {
		parsed, err := url.Parse(repoURL)
		if err != nil {
			return nil, &GitRepoURLError{URL: repoURL, Reason: "not a parseable ssh:// URL"}
		}
		if parsed.Hostname() == "" {
			return nil, &GitRepoURLError{URL: repoURL, Reason: "ssh:// URL has no host"}
		}
		endpoint := &gitSSHEndpoint{user: parsed.User.Username(), host: parsed.Hostname()}
		if portStr := parsed.Port(); portStr != "" {
			port, portErr := strconv.Atoi(portStr)
			if portErr != nil {
				return nil, &GitRepoURLError{URL: repoURL, Reason: "ssh:// URL has an invalid port"}
			}
			endpoint.port = port
		}
		return endpoint, nil
	}

	// scp-like syntax: [user@]host:path — the colon must come before any
	// slash, otherwise the string is a local path, not a remote.
	if strings.Contains(repoURL, "://") {
		return nil, &GitRepoURLError{URL: repoURL, Reason: "ssh key authentication requires an ssh remote"}
	}
	colon := strings.Index(repoURL, ":")
	slash := strings.Index(repoURL, "/")
	if colon <= 0 || (slash >= 0 && slash < colon) {
		return nil, &GitRepoURLError{URL: repoURL, Reason: "ssh key authentication requires an ssh remote"}
	}
	endpoint := &gitSSHEndpoint{}
	hostPart := repoURL[:colon]
	if at := strings.LastIndex(hostPart, "@"); at >= 0 {
		endpoint.user = hostPart[:at]
		hostPart = hostPart[at+1:]
	}
	if hostPart == "" {
		return nil, &GitRepoURLError{URL: repoURL, Reason: "ssh remote has no host"}
	}
	endpoint.host = hostPart
	return endpoint, nil
}

// =============================================================================
// The write path
// =============================================================================
//
// Clone was the whole git surface until now: a worker could read a repository
// and nothing more. These are the operations that let it produce work — cut a
// branch, record a commit, publish it — and they authenticate through the SAME
// composition a clone does (see authenticate), because a second credential
// path is a second place for a token to leak.

// requireAuthor reports an identity a commit cannot be recorded under. A
// project may carry no author at all (its workspace never commits); a commit
// may not.
func requireAuthor(author orchestration.GitAuthor) error {
	if strings.TrimSpace(author.Name) == "" {
		return &GitCloneInvalidError{Field: "author.name", Reason: "a commit must name its author"}
	}
	if strings.TrimSpace(author.Email) == "" {
		return &GitCloneInvalidError{Field: "author.email", Reason: "a commit must carry an author email"}
	}
	return nil
}

// sshKeyRefFrom converts the project-level KMS key reference into the SSH
// key store's resolver shape. Same triple, two vocabularies: the project
// record is a domain type and must not import the service layer.
func sshKeyRefFrom(ref orchestration.KMSKeyRef) SSHKeyRef {
	return SSHKeyRef{Name: ref.Name, Scope: ref.Scope, Owner: ref.Owner}
}

// GitBranchRequest cuts a branch in an existing workspace.
type GitBranchRequest struct {
	// Dir is the workspace holding the clone. Required, absolute.
	Dir string

	// Name is the branch to create and check out. Required.
	Name string

	// BaseRef is what to branch from. Empty branches from current HEAD.
	BaseRef string
}

// GitCommitRequest records a commit in an existing workspace.
type GitCommitRequest struct {
	// Dir is the workspace holding the clone. Required, absolute.
	Dir string

	// Message is the commit message. Required.
	Message string

	// Author is recorded as both author and committer. Required.
	Author orchestration.GitAuthor

	// Paths limits what is staged. EMPTY STAGES NOTHING rather than
	// everything: `git add -A` in a workspace a worker has been running tools
	// in sweeps up build output, caches and editor droppings, and a commit
	// that carries them is discovered at review time. A caller that means
	// everything says so with AllChanges.
	Paths []string

	// AllChanges stages every modification, including deletions. Explicit
	// because of what Paths says.
	AllChanges bool

	// AllowEmpty permits a commit with no changes. Off by default: an empty
	// commit usually means the tool that was supposed to produce work did
	// nothing, and recording it hides that.
	AllowEmpty bool

	// Signing decides whether and how this commit is signed. The zero value
	// signs nothing, EXPLICITLY — see orchestration.CommitSigning.
	//
	// It rides the request rather than the provisioner because the key is a
	// property of the project's workspace, not of the machinery. CommitForProject
	// fills it, with Author, from the project record.
	Signing orchestration.CommitSigning
}

// GitPushRequest publishes a branch to its remote.
type GitPushRequest struct {
	// Dir is the workspace holding the clone. Required, absolute.
	Dir string

	// RepoURL is the remote. Required — it selects the credential path and is
	// what the SSH host-key pin is keyed on.
	RepoURL string

	// Branch is the local branch to publish. Required.
	Branch string

	// RemoteBranch is the ref to publish Branch as. Empty publishes under
	// the same name — the ordinary case. Naming a different ref is how a
	// change exempted from review lands on the trunk directly: the work is
	// still committed and signed on its own branch, and only the refspec
	// differs.
	RemoteBranch string

	// SetUpstream records the remote branch as the local branch's upstream.
	SetUpstream bool

	// Force is deliberately absent. A force push destroys history the module
	// did not write, and no module operation needs one; a branch that cannot
	// fast-forward is a conflict to surface, not to overwrite.

	GitCredentials
}

// Branch creates a branch in an existing workspace and checks it out.
//
// Local only: nothing is published until Push, so a failed worker leaves no
// trace on the forge.
func (p *GitProvisioner) Branch(ctx context.Context, req *GitBranchRequest) error {
	if req == nil {
		return &GitCloneInvalidError{Field: "request", Reason: "must not be nil"}
	}
	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	if strings.TrimSpace(req.Name) == "" {
		return &GitCloneInvalidError{Field: "name", Reason: "a branch must be named"}
	}

	args := []string{"-C", req.Dir, "checkout", "-b", req.Name}
	if req.BaseRef != "" {
		args = append(args, req.BaseRef)
	}
	return p.runGit(ctx, "", "branch", append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), args)
}

// Commit records a commit in an existing workspace.
//
// The identity is passed as git configuration for THIS invocation rather than
// written into the workspace's config, so concurrent workers in separate
// checkouts cannot read each other's author identity.
func (p *GitProvisioner) Commit(ctx context.Context, req *GitCommitRequest) (string, error) {
	if req == nil {
		return "", &GitCloneInvalidError{Field: "request", Reason: "must not be nil"}
	}
	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return "", &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	if strings.TrimSpace(req.Message) == "" {
		return "", &GitCloneInvalidError{Field: "message", Reason: "a commit must carry a message"}
	}
	if err := requireAuthor(req.Author); err != nil {
		return "", err
	}
	if err := req.Signing.Validate(); err != nil {
		return "", err
	}
	if !req.AllChanges && len(req.Paths) == 0 {
		return "", &GitCloneInvalidError{
			Field:  "paths",
			Reason: "name the paths to stage, or set AllChanges; staging nothing by default keeps build output out of history",
		}
	}

	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	stage := []string{"-C", req.Dir, "add", "--"}
	if req.AllChanges {
		stage = []string{"-C", req.Dir, "add", "--all", "--"}
	} else {
		stage = append(stage, req.Paths...)
	}
	if err := p.runGit(ctx, "", "add", env, stage); err != nil {
		return "", err
	}

	// Signing is decided HERE, never inherited. A host whose global git config
	// sets commit.gpgsign — which is the normal state of a developer machine,
	// and is true on this project's own workstation — would otherwise have the
	// module sign an agent's commit with whatever key that config names. The
	// benign outcome is the failure this was found by ("agent refused
	// operation"); the bad one is a commit by an autonomous worker carrying a
	// human's signature.
	//
	// The value comes from the request's CommitSigning. Either way it is this
	// code's decision and not the host's.
	signingArgs, stopAgent, err := p.prepareSigning(ctx, &env, req.Signing)
	if err != nil {
		return "", err
	}
	defer stopAgent()

	args := []string{
		"-C", req.Dir,
		"-c", "user.name=" + req.Author.Name,
		"-c", "user.email=" + req.Author.Email,
	}
	args = append(args, signingArgs...)
	args = append(args, "commit", "-m", req.Message)
	if req.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if err := p.runGit(ctx, "", "commit", env, args); err != nil {
		return "", err
	}
	return p.headSHA(ctx, "", req.Dir)
}

// prepareSigning turns a CommitSigning policy into git arguments, starting an
// agent session when the policy calls for one.
//
// SIGNING IS ALWAYS STATED, never left to the host. Off means an explicit
// commit.gpgsign=false, because a developer machine's global config commonly
// sets it true — and a module commit carrying a human's signature is a
// provenance claim that is wrong and permanent.
//
// The private half never appears here. The resolver hands back an ssh.Signer
// whose key lives in the KMS, the agent serves signature REQUESTS over a
// socket, and git is told to use the public half by value — so nothing is
// written to disk for git to read, and there is no key file to clean up.
//
// The returned stop function is always non-nil and always safe to call.
func (p *GitProvisioner) prepareSigning(ctx context.Context, env *[]string, policy orchestration.CommitSigning) ([]string, func(), error) {
	noop := func() {}

	if !policy.Enabled {
		return []string{"-c", "commit.gpgsign=false"}, noop, nil
	}

	if p.SSHSigner == nil {
		return nil, noop, &GitCredentialError{
			Ref:    policy.Key.Name,
			Reason: "signing is enabled but no signer resolver is configured; an enabled policy must never degrade to unsigned",
		}
	}

	signer, err := p.SSHSigner.Signer(ctx, sshKeyRefFrom(policy.Key))
	if err != nil {
		return nil, noop, err
	}
	if signer == nil {
		return nil, noop, &GitCredentialError{
			Ref:    policy.Key.Name,
			Reason: "the signer resolver returned no signer for the policy key",
		}
	}

	// THE KEY MUST BE THE ALGORITHM THE POLICY NAMES. A policy selecting
	// ECDSA-P256 for a FIPS deployment, served by an ed25519 key, would sign
	// every commit with an algorithm that deployment forbids — and the
	// signatures would verify, so nothing downstream would notice.
	if err := requireSigningAlgorithm(signer, policy); err != nil {
		return nil, noop, err
	}

	session, err := xkmsssh.StartAgent(ctx, xkmsssh.AgentOptions{Signers: []ssh.Signer{signer}})
	if err != nil {
		return nil, noop, err
	}

	// Env carries SSH_AUTH_SOCK for the child git process only. The ambient
	// environment is untouched, so a concurrent worker cannot reach this agent.
	*env = append(*env, session.Env...)

	// "key::" hands git the public key BY VALUE. The alternative is a path to
	// a file we would have to write and delete, which is a key artefact on
	// disk for exactly as long as the commit takes.
	publicKey := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))

	stop := func() {
		shutdownCtx, cancel := p.shutdownContext(ctx)
		defer cancel()
		p.reportAgentShutdown("commit signing agent", session.Stop(shutdownCtx))
	}

	return []string{
		"-c", "gpg.format=ssh",
		"-c", "user.signingkey=key::" + publicKey,
		"-c", "commit.gpgsign=true",
	}, stop, nil
}

// shutdownContext derives the teardown budget for an ssh agent session.
//
// It deliberately does NOT inherit the operation's cancellation. By the time a
// stop function runs, the git command it served has finished and the caller's
// context is routinely already cancelled — inheriting that would collapse the
// handler join to zero, abandon handlers that needed a millisecond, and start
// the host-key import on an already-expired deadline, silently dropping the
// first-contact keys ssh had just accepted. Values are preserved so tracing and
// request scope survive into the teardown.
func (p *GitProvisioner) shutdownContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), gitAgentShutdownBound)
}

// reportAgentShutdown surfaces an unclean agent shutdown.
//
// A ShutdownReport that is not Clean means either a handler never joined or a
// signature was still executing when the bound elapsed. Neither can fail the
// git operation — it has already completed — but neither may be swallowed: a
// bounded stop that reported nothing would be the same surprise as no bound at
// all, and a recurring unclean count is how a wedged signer announces itself.
func (p *GitProvisioner) reportAgentShutdown(where string, report xkmsssh.AgentShutdownReport) {
	if report.Clean() || p.Logger == nil {
		return
	}
	p.Logger.Warnf(
		"%s: ssh agent shutdown incomplete: %d signature(s) still executing, handlers joined=%t",
		where, report.Abandoned, report.Joined,
	)
}

// sshAlgorithmsBySigningAlgorithm maps a policy algorithm to the SSH public-key
// type a key of that algorithm presents.
var sshAlgorithmsBySigningAlgorithm = map[string]string{
	orchestration.SigningAlgorithmEd25519:   ssh.KeyAlgoED25519,
	orchestration.SigningAlgorithmECDSAP256: ssh.KeyAlgoECDSA256,
}

// requireSigningAlgorithm refuses a key that is not the algorithm the policy
// selected.
func requireSigningAlgorithm(signer ssh.Signer, policy orchestration.CommitSigning) error {
	want, known := sshAlgorithmsBySigningAlgorithm[policy.SigningAlgorithm()]
	if !known {
		return &GitCloneInvalidError{
			Field:  "signing.algorithm",
			Reason: "unsupported signing algorithm " + policy.SigningAlgorithm(),
		}
	}
	if got := signer.PublicKey().Type(); got != want {
		return &GitCloneInvalidError{
			Field: "signing.key",
			Reason: "policy selects " + policy.SigningAlgorithm() + " but the key is " + got +
				"; signing with an algorithm the policy did not choose still verifies, so nothing downstream would catch it",
		}
	}
	return nil
}

// Push publishes a branch to its remote, authenticating exactly as a clone
// does.
func (p *GitProvisioner) Push(ctx context.Context, req *GitPushRequest) error {
	if req == nil {
		return &GitCloneInvalidError{Field: "request", Reason: "must not be nil"}
	}
	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	if req.RepoURL == "" {
		return &GitCloneInvalidError{Field: "repo_url", Reason: "repository URL is required"}
	}
	if strings.TrimSpace(req.Branch) == "" {
		return &GitCloneInvalidError{Field: "branch", Reason: "a push must name the branch to publish"}
	}
	if err := req.Validate(); err != nil {
		return err
	}

	env, authArgs, stop, err := p.authenticate(ctx, req.RepoURL, req.GitCredentials)
	if err != nil {
		return err
	}
	defer stop()

	args := make([]string, 0, len(authArgs)+6)
	args = append(args, authArgs...)
	args = append(args, "-C", req.Dir, "push")
	if req.SetUpstream {
		args = append(args, "--set-upstream")
	}
	// The URL goes after "--" so a hostile URL can never be parsed as an
	// option — the same guard gitCloneArgs documents. Clone had it and push
	// did not: the hazard was known in this file and missed one function away.
	refspec := req.Branch
	if remote := strings.TrimSpace(req.RemoteBranch); remote != "" {
		refspec = req.Branch + ":" + remote
	}
	args = append(args, "--", req.RepoURL, refspec)
	return p.runGit(ctx, req.RepoURL, "push", env, args)
}

// =============================================================================
// Project-driven commits
// =============================================================================

// CommitForProject records a commit in the project's workspace under the
// identity and signing policy the PROJECT carries. The request's Author and
// Signing are overwritten from project.Git — they are properties of the
// workspace, and a caller that could substitute its own would be the
// provenance hole this method closes.
func (p *GitProvisioner) CommitForProject(ctx context.Context, project *orchestration.Project, req *GitCommitRequest) (string, error) {
	if project == nil {
		return "", &GitCloneInvalidError{Field: "project", Reason: "a project-driven commit needs its project"}
	}
	if req == nil {
		return "", &GitCloneInvalidError{Field: "request", Reason: "must not be nil"}
	}
	req.Author = project.Git.Author
	req.Signing = project.Git.Signing
	return p.Commit(ctx, req)
}
