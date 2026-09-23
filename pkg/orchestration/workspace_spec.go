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

package orchestration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// =============================================================================
// The workspace spec — what the daemon hands a worker container
// =============================================================================
//
// THE DAEMON DOES NO CLONE, COPY OR WORKTREE. It hands the container a
// description of the checkout it should make and the credentials to make it
// with, and the clone happens INSIDE the container: the project and every
// context repository fresh, side by side, on the branch the strategy planned.
// A daemon that cloned would own a filesystem that has to be reaped, kept in
// step with the record, and reasoned about after a crash; a container that
// clones owns nothing that outlives it.
//
// HOW THE CREDENTIALS TRAVEL is the same way a pooled provider account's
// credential already travels (account_materialize.go): written into a FRESH
// per-spawn directory the daemon owns, bind-mounted read-only into the
// container. Never a value in argv, where every process on the host can read
// it, and never a value in the environment, where every child of the agent
// inherits it. The signing key never travels at all — what the container gets
// is a socket to an agent that holds it, so a compromised worker can ask for
// signatures while its container lives and hold nothing afterwards.

// WorkspaceSpecDir is where the spec directory is mounted in the container.
const WorkspaceSpecDir = "/run/quicbaas/workspace"

// WorkspaceSpecFile is the spec file's name inside WorkspaceSpecDir.
const WorkspaceSpecFile = "workspace.json"

// WorkspaceForgeTokenFile is the forge token's file name inside
// WorkspaceSpecDir. Present only when the caller supplied a token.
const WorkspaceForgeTokenFile = "forge-token"

// WorkspaceAllowedSignersFile is the allowed_signers file's name inside
// WorkspaceSpecDir. Present only when the attempt signs its commits, and
// holding exactly one entry: the public half of the key the signing agent
// serves. git reads it (gpg.ssh.allowedSignersFile) to decide whether a commit
// was signed by that key, which is how a commit the WORKER made is checked
// before anything publishes it.
const WorkspaceAllowedSignersFile = "allowed_signers"

// WorkspaceSpecEnv names the environment variable carrying the spec file's
// PATH — the path, never the contents.
const WorkspaceSpecEnv = "QUICBAAS_WORKSPACE_SPEC"

// WorkspaceKnownHostsFile is the known_hosts file's name inside
// WorkspaceSpecDir. Present only when the checkout is made over ssh, and
// holding exactly one entry: the forge host the project is cloned from,
// pinned to the public key the binding carries.
const WorkspaceKnownHostsFile = "known_hosts"

// WorkspaceSSHCommandEnv names the environment variable git reads the ssh
// invocation from. The daemon sets it so the worker's git verifies the forge
// against the mounted known_hosts and nothing else.
const WorkspaceSSHCommandEnv = "GIT_SSH_COMMAND"

// WorkspaceSigningAgentSocket is where the ephemeral, KMS-backed SSH signing
// agent's socket is mounted in the container. The worker's git reaches it
// through SSH_AUTH_SOCK; the key itself never enters the container.
const WorkspaceSigningAgentSocket = "/run/quicbaas/ssh-agent.sock"

// WorkspaceSigningAgentSocketFile is the socket's file name inside the
// per-session directory the daemon binds it in. It is fixed by the go-xkms
// SDK, which is why the mount policy can recognise a signing socket by its
// shape without being told about any one session.
const WorkspaceSigningAgentSocketFile = "agent.sock"

// WorkspaceSigningAgentEnv names the environment variable git and ssh read
// the signing agent's socket path from.
const WorkspaceSigningAgentEnv = "SSH_AUTH_SOCK"

// WorkspaceCheckoutRoot is the directory the container clones into: the
// project at <root>/<project name>, each context repository at <root>/<name>.
const WorkspaceCheckoutRoot = "/workspace"

// workspaceSpecDirPrefix names the temporary directory created to hold one
// spawn's workspace spec and forge token.
const workspaceSpecDirPrefix = "quicbaas-workspace-*"

// workspaceSpecDirMode is the mode of the directory the daemon writes; the
// container sees it read-only through the mount.
const workspaceSpecDirMode os.FileMode = 0o700

// workspaceSpecFileMode is the mode of every file in it. Read-only to its
// owner: nothing on the daemon side ever rewrites one of these files, and the
// container gets its copy through a read-only mount.
const workspaceSpecFileMode os.FileMode = 0o400

// WorkspaceRepoSpec names one repository the container clones.
type WorkspaceRepoSpec struct {
	// Name is the directory under WorkspaceCheckoutRoot the repository is
	// cloned into.
	Name string `json:"name"`

	// RepoURL is the remote.
	RepoURL string `json:"repo_url"`

	// Ref is the branch, tag or commit to check out.
	Ref string `json:"ref"`
}

// WorkspaceBranchPlan is the branch the container works on.
type WorkspaceBranchPlan struct {
	// Name is the branch the container checks out and the worker commits to.
	Name string `json:"name"`

	// Base is the ref the branch is created from when Create is set.
	Base string `json:"base,omitempty"`

	// Create says the branch does not exist on the remote yet. When it is
	// false the container fetches the branch a prior attempt pushed and
	// continues it.
	Create bool `json:"create,omitempty"`

	// Commit PINS the checkout to one commit: the container puts Name on
	// exactly this object rather than on wherever the remote branch happens
	// to point when it fetches.
	//
	// IT EXISTS BECAUSE A BRANCH MOVES AND A REVIEW MUST NOT. A caller that
	// read a head from the forge and then described the checkout by branch
	// name alone gets whatever the author pushed in between, and everything
	// it goes on to say — a verdict, a commit status — is then about a tree
	// nothing read. Naming the commit makes the two the same object or makes
	// the checkout fail, and there is no third outcome.
	//
	// Empty leaves the checkout on the remote branch, which is what a worker
	// continuing its own branch wants. It is mutually exclusive with Create:
	// a branch that does not exist yet has no commit to be pinned to.
	Commit string `json:"commit,omitempty"`
}

// WorkspaceSpec is the whole of what a worker container is told about the
// checkout it must make. It carries no credential values — the forge token
// is a sibling file and the signing key is behind a socket.
type WorkspaceSpec struct {
	// Project is the repository the work is about.
	Project WorkspaceRepoSpec `json:"project"`

	// ContextRepos are cloned beside the project, in this order.
	ContextRepos []WorkspaceRepoSpec `json:"context_repos,omitempty"`

	// Branch is the branch plan the strategy produced.
	Branch WorkspaceBranchPlan `json:"branch"`

	// CheckoutRoot is where the clones go; WorkspaceCheckoutRoot unless the
	// caller says otherwise.
	CheckoutRoot string `json:"checkout_root"`

	// ForgeTokenFile names the file holding the forge token, relative to the
	// spec file's own directory. Empty when no token was supplied.
	ForgeTokenFile string `json:"forge_token_file,omitempty"`

	// SSHHostKey is the forge's pinned host public key, one OpenSSH
	// public-key line. It is PUBLIC data — it is in the spec so the
	// container can see which key its clone was made to trust — and it is
	// required when Project.RepoURL is an ssh URL.
	SSHHostKey string `json:"ssh_host_key,omitempty"`

	// KnownHostsFile names the file holding the known_hosts written from
	// SSHHostKey, relative to the spec file's own directory. Empty when the
	// checkout needs no ssh.
	KnownHostsFile string `json:"known_hosts_file,omitempty"`

	// SigningKey is the public half of the key the attempt's signing agent
	// serves, one OpenSSH public-key line. It is PUBLIC data. The checkout
	// configures the worker's git to sign with it, and it is the one key the
	// allowed_signers file admits. Empty when the attempt signs nothing.
	SigningKey string `json:"signing_key,omitempty"`

	// AllowedSignersFile names the file holding the allowed_signers written
	// from SigningKey, relative to the spec file's own directory. Empty when
	// the attempt signs nothing.
	AllowedSignersFile string `json:"allowed_signers_file,omitempty"`
}

// MaterializedWorkspace is one spawn's written workspace spec: the directory
// the caller mounts read-only into the container, the spec exactly as the
// container will read it, and the cleanup that removes both.
//
// THE SPEC COMES BACK because materializing DECIDES things — which sibling
// files were written, and therefore which paths the spawn must name in the
// container's environment. A caller that kept its own copy would be applying
// a spec no container ever sees.
type MaterializedWorkspace struct {
	// Dir is the directory holding the spec and its sibling files.
	Dir string

	// Spec is what was written, with its file-naming fields filled in.
	Spec WorkspaceSpec

	// Cleanup removes Dir. It is nil when materializing failed.
	Cleanup func()
}

// NewWorkspaceSpec builds the spec for one attempt at a project, taking the
// project's repository and context repositories and the planned branch.
func NewWorkspaceSpec(project *Project, branch WorkspaceBranchPlan) (WorkspaceSpec, error) {
	if project == nil {
		return WorkspaceSpec{}, &WorkspaceSpecInvalidError{Field: "project", Message: "a workspace belongs to a project"}
	}
	if project.Git.RepoURL == "" {
		return WorkspaceSpec{}, &WorkspaceSpecInvalidError{Field: "project.git.repo_url", Message: "the project names no repository"}
	}
	if branch.Name == "" {
		return WorkspaceSpec{}, &WorkspaceSpecInvalidError{Field: "branch.name", Message: "the container checks out a named branch"}
	}
	if branch.Create && branch.Base == "" {
		return WorkspaceSpec{}, &WorkspaceSpecInvalidError{Field: "branch.base", Message: "a new branch needs the ref it starts from"}
	}
	if branch.Create && branch.Commit != "" {
		return WorkspaceSpec{}, &WorkspaceSpecInvalidError{
			Field:   "branch.commit",
			Message: "a branch the remote does not have yet cannot be pinned to a commit on it",
		}
	}
	if err := ValidateContextRepos(project.ContextRepos); err != nil {
		return WorkspaceSpec{}, err
	}
	name := project.Name
	if err := (ContextRepo{Name: name, RepoURL: project.Git.RepoURL, Ref: branch.Name}).Validate(); err != nil {
		return WorkspaceSpec{}, &WorkspaceSpecInvalidError{
			Field:   "project.name",
			Message: "is the directory the project is cloned into and must be one path segment: " + name,
		}
	}
	spec := WorkspaceSpec{
		Project: WorkspaceRepoSpec{
			Name:    name,
			RepoURL: project.Git.RepoURL,
			Ref:     project.Git.DefaultBranch,
		},
		Branch:       branch,
		CheckoutRoot: WorkspaceCheckoutRoot,
	}
	for _, repo := range project.ContextRepos {
		if repo.Name == name {
			return WorkspaceSpec{}, &WorkspaceSpecInvalidError{
				Field:   "context_repos",
				Message: "context repository " + repo.Name + " would occupy the project's own directory",
			}
		}
		spec.ContextRepos = append(spec.ContextRepos, WorkspaceRepoSpec(repo))
	}
	return spec, nil
}

// MaterializeWorkspaceSpec writes spec into a fresh directory under the OS
// temporary directory; see MaterializeWorkspaceSpecIn.
func MaterializeWorkspaceSpec(spec WorkspaceSpec, forgeToken []byte) (MaterializedWorkspace, error) {
	return MaterializeWorkspaceSpecIn("", spec, forgeToken)
}

// MaterializeWorkspaceSpecIn writes spec, and its sibling credential and
// known_hosts files, into a fresh directory under parent. An empty parent
// takes the OS temporary directory. The caller mounts the returned directory
// read-only into the container with WorkspaceSpecMounts; nothing here is
// written where anything but this spawn can read it.
//
// PARENT EXISTS BECAUSE THE MOUNT POLICY IS DENY-BY-DEFAULT. Every bind-mount
// source is authorized against the operator's projects root and allowed
// roots, and the OS temporary directory is neither. A caller that already
// owns an admitted directory — the project's own workspace, which is where
// the attempt's checkout is mounted from — names it here so the spec travels
// through the same door as everything else the spawn mounts.
//
// AN SSH CHECKOUT IS REFUSED WITHOUT A PINNED HOST KEY, here, before any
// container is started. There is no accept-new and no learned key: a spec
// whose project is cloned over ssh and whose binding pinned nothing names a
// forge nothing can vouch for, and the error says which binding field the
// operator has to fill in.
func MaterializeWorkspaceSpecIn(parent string, spec WorkspaceSpec, forgeToken []byte) (MaterializedWorkspace, error) {
	if len(forgeToken) > 0 {
		spec.ForgeTokenFile = WorkspaceForgeTokenFile
	} else {
		spec.ForgeTokenFile = ""
	}
	knownHosts, hostKeyErr := KnownHostsFor(spec.Project.RepoURL, spec.SSHHostKey)
	if hostKeyErr != nil {
		return MaterializedWorkspace{}, hostKeyErr
	}
	if knownHosts != "" {
		spec.KnownHostsFile = WorkspaceKnownHostsFile
	} else {
		spec.KnownHostsFile = ""
	}
	allowedSigners := AllowedSignersFor(spec.SigningKey)
	if allowedSigners != "" {
		spec.AllowedSignersFile = WorkspaceAllowedSignersFile
	} else {
		spec.AllowedSignersFile = ""
	}
	encoded, marshalErr := json.MarshalIndent(spec, "", "  ")
	if marshalErr != nil {
		return MaterializedWorkspace{}, &WorkspaceSpecInvalidError{Field: "spec", Message: "does not encode: " + marshalErr.Error()}
	}

	root, mkErr := os.MkdirTemp(parent, workspaceSpecDirPrefix)
	if mkErr != nil {
		return MaterializedWorkspace{}, &WorkspaceSpecMaterializeError{Message: "creating the spec directory failed", Cause: mkErr}
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	if chmodErr := os.Chmod(root, workspaceSpecDirMode); chmodErr != nil {
		cleanup()
		return MaterializedWorkspace{}, &WorkspaceSpecMaterializeError{Message: "sealing the spec directory failed", Cause: chmodErr}
	}

	files := map[string][]byte{WorkspaceSpecFile: encoded}
	if len(forgeToken) > 0 {
		files[WorkspaceForgeTokenFile] = forgeToken
	}
	if knownHosts != "" {
		files[WorkspaceKnownHostsFile] = []byte(knownHosts)
	}
	if allowedSigners != "" {
		files[WorkspaceAllowedSignersFile] = []byte(allowedSigners)
	}
	for name, content := range files {
		if writeErr := os.WriteFile(filepath.Join(root, name), content, workspaceSpecFileMode); writeErr != nil {
			cleanup()
			return MaterializedWorkspace{}, &WorkspaceSpecMaterializeError{Message: "writing " + name + " failed", Cause: writeErr}
		}
	}
	return MaterializedWorkspace{Dir: root, Spec: spec, Cleanup: cleanup}, nil
}

// AllowedSignersFor is the allowed_signers content admitting exactly
// signingKey for git commit signatures, or empty when there is no key.
//
// THE PRINCIPAL IS "*" because the question the file answers is "was this
// signed by THE key", not "by whom": the attempt has one key, and the
// author's address is already stated by the commit itself.
func AllowedSignersFor(signingKey string) string {
	key := strings.TrimSpace(signingKey)
	if key == "" {
		return ""
	}
	return `* namespaces="git" ` + key + "\n"
}

// WorkspaceSpecMounts are the read-only mounts a container needs to act on a
// materialized spec: the spec directory, and the signing agent's socket when
// one was started for this spawn.
func WorkspaceSpecMounts(specDir, signingAgentSocket string) []ContainerMount {
	mounts := make([]ContainerMount, 0, 2)
	if specDir != "" {
		mounts = append(mounts, ContainerMount{Source: specDir, Target: WorkspaceSpecDir, ReadOnly: true})
	}
	if signingAgentSocket != "" {
		// READ-ONLY: the container asks the agent for signatures, and never
		// needs to write, replace or unlink the socket's pathname. A unix
		// socket is exempt from a read-only superblock's write check
		// (sb_permission covers regular files, directories and symlinks only),
		// so connect() still works — and it is the condition on which the
		// mount policy admits the daemon's sockets root at all.
		mounts = append(mounts, ContainerMount{Source: signingAgentSocket, Target: WorkspaceSigningAgentSocket, ReadOnly: true})
	}
	return mounts
}

// WorkspaceSSHCommand is the ssh invocation git makes for a checkout verified
// against the mounted known_hosts.
//
// StrictHostKeyChecking=yes, ALWAYS. It is not a tunable and there is no
// caller that may soften it: the whole point of pinning the key on the
// binding is that ssh refuses a host whose key is not the pinned one, and
// "accept-new" would silently restore trust-on-first-use for a worker that
// holds a signing agent's socket.
func WorkspaceSSHCommand(knownHostsPath string) string {
	return "ssh -o UserKnownHostsFile=" + knownHostsPath + " -o StrictHostKeyChecking=yes"
}

// ApplyWorkspaceSpec puts a materialized workspace onto a spawn request: the
// read-only mounts, and the environment variables that name their PATHS.
// No credential value is ever put on the request.
func ApplyWorkspaceSpec(req *SpawnSessionRequest, ws MaterializedWorkspace, signingAgentSocket string) {
	if req == nil {
		return
	}
	req.ExtraMounts = append(req.ExtraMounts, WorkspaceSpecMounts(ws.Dir, signingAgentSocket)...)
	if req.Env == nil {
		req.Env = make(map[string]string, 3)
	}
	if ws.Dir != "" {
		req.Env[WorkspaceSpecEnv] = filepath.Join(WorkspaceSpecDir, WorkspaceSpecFile)
	}
	if signingAgentSocket != "" {
		req.Env[WorkspaceSigningAgentEnv] = WorkspaceSigningAgentSocket
	}
	if ws.Dir != "" && ws.Spec.KnownHostsFile != "" {
		req.Env[WorkspaceSSHCommandEnv] = WorkspaceSSHCommand(
			filepath.Join(WorkspaceSpecDir, ws.Spec.KnownHostsFile))
	}
}

// =============================================================================
// Errors
// =============================================================================

// ErrWorkspaceSpecInvalid indicates a workspace spec the container could not
// act on.
var ErrWorkspaceSpecInvalid = errors.New("workspace spec: invalid")

// ErrWorkspaceSpecMaterialize indicates a spec that could not be written.
var ErrWorkspaceSpecMaterialize = errors.New("workspace spec: materialization failed")

// WorkspaceSpecInvalidError reports a spec that names no checkout the
// container could make.
type WorkspaceSpecInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *WorkspaceSpecInvalidError) Error() string {
	return ErrWorkspaceSpecInvalid.Error() + ": " + e.Field + " " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *WorkspaceSpecInvalidError) Unwrap() error { return ErrWorkspaceSpecInvalid }

// WorkspaceSpecMaterializeError reports a spec directory the daemon could not
// write.
type WorkspaceSpecMaterializeError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *WorkspaceSpecMaterializeError) Error() string {
	return ErrWorkspaceSpecMaterialize.Error() + ": " + e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *WorkspaceSpecMaterializeError) Unwrap() []error {
	return []error{ErrWorkspaceSpecMaterialize, e.Cause}
}
