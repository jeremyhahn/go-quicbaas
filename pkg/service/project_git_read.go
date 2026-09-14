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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// =============================================================================
// The read side, and the one remote read
// =============================================================================
//
// Clone, branch, commit and push were the whole surface; the SDLC
// worker's git path needs four more facts about a workspace — does it carry
// changes, what is its head, is its head signed, is its base clone current —
// and all four are git invocations this type already knows how to make.
// Asking them here rather than in the adapter keeps every git process the
// module starts behind one binary selection and one error shape.

// GitFetchRequest brings a clone's remote-tracking refs up to date.
type GitFetchRequest struct {
	// Dir is the workspace holding the clone. Required, absolute.
	Dir string

	// RepoURL is the remote. Required — it selects the credential path and is
	// what the SSH host-key pin is keyed on.
	RepoURL string

	GitCredentials
}

// Fetch updates the clone's remote-tracking refs, authenticating exactly as a
// clone does, and prunes refs the remote no longer has.
//
// A base clone a worker cuts branches from goes stale the moment anything
// merges, and a branch cut from a stale base carries a merge conflict into
// its pull request. Fetching before every cut is what keeps the base current.
func (p *GitProvisioner) Fetch(ctx context.Context, req *GitFetchRequest) error {
	if req == nil {
		return &GitCloneInvalidError{Field: "request", Reason: "must not be nil"}
	}
	if req.Dir == "" || !filepath.IsAbs(req.Dir) {
		return &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	if req.RepoURL == "" {
		return &GitCloneInvalidError{Field: "repo_url", Reason: "repository URL is required"}
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
	args = append(args, "-C", req.Dir, "fetch", "--prune", "--", req.RepoURL,
		"+refs/heads/*:refs/remotes/origin/*")
	return p.runGit(ctx, req.RepoURL, "fetch", env, args)
}

// HasChanges reports whether the workspace carries uncommitted work —
// modified, deleted or untracked files.
func (p *GitProvisioner) HasChanges(ctx context.Context, dir string) (bool, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return false, &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	cmd := exec.CommandContext(ctx, p.gitBinary(), "-C", dir, "status", "--porcelain", "--untracked-files=all")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, &GitCloneError{Stage: "status", Output: string(out), Err: err}
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// HeadSHA returns the workspace's HEAD commit.
func (p *GitProvisioner) HeadSHA(ctx context.Context, dir string) (string, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return "", &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	return p.headSHA(ctx, "", dir)
}

// UnsignedCommitError reports a commit that carries no SSH signature where
// the policy required one.
type UnsignedCommitError struct {
	Dir string
	SHA string
}

// Error implements the error interface.
func (e *UnsignedCommitError) Error() string {
	return "git: commit " + e.SHA + " in " + e.Dir + " carries no SSH signature; an unsigned commit must not reach the branch"
}

// RequireSignedCommit refuses a commit that carries no SSH signature.
//
// The check reads the commit OBJECT, not git's verification verdict: the
// module's allowed-signers policy is the forge's to apply when it marks the
// commit verified, and what this guards is narrower — that signing was
// performed at all. A commit.gpgsign the host silently overrode, a signer
// that returned without signing, a provisioner change that dropped the flag:
// each produces a commit that looks ordinary and would push. This is the
// abort condition the burn-in names, enforced before the push instead of
// discovered on the forge.
func (p *GitProvisioner) RequireSignedCommit(ctx context.Context, dir, sha string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return &GitCloneInvalidError{Field: "dir", Reason: "an absolute workspace directory is required"}
	}
	if strings.TrimSpace(sha) == "" {
		return &GitCloneInvalidError{Field: "sha", Reason: "a commit to check is required"}
	}
	cmd := exec.CommandContext(ctx, p.gitBinary(), "-C", dir, "cat-file", "commit", sha)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &GitCloneError{Stage: "cat-file", Output: string(out), Err: err}
	}
	if !commitHeadersCarrySSHSignature(out) {
		return &UnsignedCommitError{Dir: dir, SHA: sha}
	}
	return nil
}

// commitHeadersCarrySSHSignature reports whether the commit object's HEADER
// section carries a gpgsig field with an SSH signature.
//
// STRUCTURAL, NOT A SUBSTRING SWEEP (review 998): a commit object is headers,
// a blank line, then the free-text message — and a MESSAGE that merely
// contains "BEGIN SSH SIGNATURE" must not pass an unsigned commit. Only the
// headers can carry a signature: the gpgsig field's first line, and its
// continuation lines (which git prefixes with a single space).
func commitHeadersCarrySSHSignature(raw []byte) bool {
	headers, _, _ := strings.Cut(string(raw), "\n\n")
	inSig := false
	for _, line := range strings.Split(headers, "\n") {
		switch {
		case strings.HasPrefix(line, "gpgsig "):
			inSig = true
			if strings.Contains(line, "BEGIN SSH SIGNATURE") {
				return true
			}
		case inSig && strings.HasPrefix(line, " "):
			if strings.Contains(line, "BEGIN SSH SIGNATURE") {
				return true
			}
		default:
			inSig = false
		}
	}
	return false
}
