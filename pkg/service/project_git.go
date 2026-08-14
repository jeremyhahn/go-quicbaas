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
	"strconv"
	"strings"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// gitTokenEnvVar carries the resolved HTTPS token into the git child process.
// The token travels ONLY in the child environment: the credential helper text
// on the command line references the variable by name, so the secret never
// appears in process arguments and is never written to disk.
const gitTokenEnvVar = "QUICBAAS_GIT_TOKEN" //nolint:gosec // G101: an environment variable name, not a credential

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
func (e *GitCloneError) Error() string {
	msg := "git: " + e.Stage + " " + strconv.Quote(e.RepoURL) + " failed"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	if out := strings.TrimSpace(e.Output); out != "" {
		msg += ": " + out
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
	if req.TokenSecretRef != "" && req.SSHKey.Name != "" {
		return "", &GitCloneInvalidError{
			Field:  "credentials",
			Reason: "token secret ref and ssh key ref are mutually exclusive",
		}
	}

	args := gitCloneArgs(req.RepoURL, req.Branch, req.Dir)
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	switch {
	case req.TokenSecretRef != "":
		tokenEnv, tokenArgs, err := p.tokenAuth(ctx, req)
		if err != nil {
			return "", err
		}
		env = append(env, tokenEnv...)
		args = append(tokenArgs, args...)

	case req.SSHKey.Name != "":
		sshEnv, stop, err := p.sshAuth(ctx, req)
		if err != nil {
			return "", err
		}
		defer stop()
		env = append(env, sshEnv...)
	}

	if err := p.runGit(ctx, req.RepoURL, "clone", env, args); err != nil {
		return "", err
	}
	return p.headSHA(ctx, req.RepoURL, req.Dir)
}

// tokenAuth resolves the HTTPS token reference and returns the child
// environment entries plus the git config arguments installing the inline
// credential helper. The token appears only in the environment.
func (p *GitProvisioner) tokenAuth(ctx context.Context, req *GitCloneRequest) ([]string, []string, error) {
	if !strings.HasPrefix(req.RepoURL, "https://") {
		return nil, nil, &GitRepoURLError{
			URL:    req.RepoURL,
			Reason: "token authentication requires an https:// repository URL",
		}
	}
	if p.Secrets == nil {
		return nil, nil, &GitCredentialError{
			Ref:    req.TokenSecretRef,
			Reason: "secrets service is not configured",
		}
	}
	resp, err := p.Secrets.GetSecret(ctx, &GetSecretRequest{SecretID: req.TokenSecretRef})
	if err != nil {
		return nil, nil, &GitCredentialError{
			Ref:    req.TokenSecretRef,
			Reason: "failed to resolve token secret",
			Err:    err,
		}
	}
	token := strings.TrimSpace(string(resp.Data))
	if token == "" {
		return nil, nil, &GitCredentialError{
			Ref:    req.TokenSecretRef,
			Reason: "token secret is empty",
		}
	}
	username := req.TokenUsername
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
func (p *GitProvisioner) sshAuth(ctx context.Context, req *GitCloneRequest) ([]string, func(), error) {
	endpoint, err := parseGitSSHEndpoint(req.RepoURL)
	if err != nil {
		return nil, nil, err
	}
	if p.SSHSigner == nil {
		return nil, nil, &GitCredentialError{
			Ref:    req.SSHKey.Name,
			Reason: "ssh signer resolver is not configured",
		}
	}
	if p.HostKeys == nil {
		return nil, nil, &GitCredentialError{
			Ref:    req.SSHKey.Name,
			Reason: "ssh host-key store is not configured",
		}
	}
	// One effective (name, scope, owner) triple selects both the signing key
	// and the host-key pin namespace: scope defaults to "tenant" and owner to
	// the requesting tenant, so one tenant's keys and pins can never be
	// resolved on behalf of another by omission.
	keyRef := req.SSHKey
	if keyRef.Scope == "" {
		keyRef.Scope = "tenant"
	}
	if keyRef.Owner == "" {
		keyRef.Owner = req.TenantID
	}
	signer, err := p.SSHSigner.Signer(ctx, keyRef)
	if err != nil {
		return nil, nil, &GitCredentialError{
			Ref:    req.SSHKey.Name,
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
			Ref:    req.SSHKey.Name,
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
		if _, syncErr := session.SyncAndStop(); syncErr != nil && p.Logger != nil {
			p.Logger.Warnf("git ssh host-key pin sync failed: %v", syncErr)
		}
	}
	return env, stop, nil
}

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
