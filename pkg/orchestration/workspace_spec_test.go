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
	"testing"
)

// workspaceProject is a project with a repository and two context
// repositories beside it.
func workspaceProject() *Project {
	return &Project{
		ID: "p1", TenantID: "t1", Name: "go-truststrap",
		Git: ProjectGit{RepoURL: "https://forge.invalid/o/go-truststrap", DefaultBranch: "main"},
		ContextRepos: []ContextRepo{
			{Name: "handbook", RepoURL: "https://forge.invalid/o/handbook", Ref: "main"},
			{Name: "specs", RepoURL: "https://forge.invalid/o/specs", Ref: "v2"},
		},
	}
}

// =============================================================================
// ContextRepo validation
// =============================================================================

func TestContextRepo_Validate(t *testing.T) {
	t.Parallel()
	valid := ContextRepo{Name: "handbook", RepoURL: "https://forge.invalid/o/handbook", Ref: "main"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("a complete context repository was refused: %v", err)
	}
	for name, repo := range map[string]ContextRepo{
		"empty name":     {RepoURL: "u", Ref: "main"},
		"dot":            {Name: ".", RepoURL: "u", Ref: "main"},
		"dot dot":        {Name: "..", RepoURL: "u", Ref: "main"},
		"slash":          {Name: "a/b", RepoURL: "u", Ref: "main"},
		"backslash":      {Name: "a\\b", RepoURL: "u", Ref: "main"},
		"nul":            {Name: "a\x00b", RepoURL: "u", Ref: "main"},
		"empty repo url": {Name: "handbook", Ref: "main"},
		"empty ref":      {Name: "handbook", RepoURL: "u"},
	} {
		err := repo.Validate()
		var typed *ContextRepoInvalidError
		if !errors.As(err, &typed) || !errors.Is(err, ErrContextRepoInvalid) {
			t.Errorf("%s: got %v, want ContextRepoInvalidError", name, err)
		}
	}
}

func TestValidateContextRepos_RefusesDuplicateNames(t *testing.T) {
	t.Parallel()
	if err := ValidateContextRepos(workspaceProject().ContextRepos); err != nil {
		t.Fatalf("distinct names refused: %v", err)
	}
	if err := ValidateContextRepos(nil); err != nil {
		t.Fatalf("no context repositories is not an error: %v", err)
	}
	err := ValidateContextRepos([]ContextRepo{
		{Name: "handbook", RepoURL: "u", Ref: "main"},
		{Name: "handbook", RepoURL: "v", Ref: "main"},
	})
	var typed *ContextRepoInvalidError
	if !errors.As(err, &typed) || typed.Name != "handbook" {
		t.Fatalf("duplicate names: got %v, want ContextRepoInvalidError", err)
	}
}

// =============================================================================
// The spec
// =============================================================================

func TestNewWorkspaceSpec_CarriesTheProjectAndItsContextRepos(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{
		Name: "issue/7-thing", Base: "main", Create: true,
	})
	if err != nil {
		t.Fatalf("NewWorkspaceSpec: %v", err)
	}
	if spec.Project.Name != "go-truststrap" || spec.Project.RepoURL != "https://forge.invalid/o/go-truststrap" ||
		spec.Project.Ref != "main" {
		t.Fatalf("project = %+v", spec.Project)
	}
	if spec.CheckoutRoot != WorkspaceCheckoutRoot {
		t.Fatalf("checkout root = %q", spec.CheckoutRoot)
	}
	if len(spec.ContextRepos) != 2 || spec.ContextRepos[0].Name != "handbook" || spec.ContextRepos[1].Ref != "v2" {
		t.Fatalf("context repos = %+v", spec.ContextRepos)
	}
	if spec.Branch.Name != "issue/7-thing" || spec.Branch.Base != "main" || !spec.Branch.Create {
		t.Fatalf("branch = %+v", spec.Branch)
	}
}

func TestNewWorkspaceSpec_Refusals(t *testing.T) {
	t.Parallel()
	plan := WorkspaceBranchPlan{Name: "issue/7-thing", Base: "main", Create: true}

	noRepo := workspaceProject()
	noRepo.Git.RepoURL = ""
	collides := workspaceProject()
	collides.ContextRepos = append(collides.ContextRepos, ContextRepo{
		Name: collides.Name, RepoURL: "u", Ref: "main",
	})
	badContext := workspaceProject()
	badContext.ContextRepos[0].Ref = ""
	badName := workspaceProject()
	badName.Name = "a/b"

	for name, tc := range map[string]struct {
		project *Project
		plan    WorkspaceBranchPlan
	}{
		"nil project":         {nil, plan},
		"no repo url":         {noRepo, plan},
		"no branch":           {workspaceProject(), WorkspaceBranchPlan{}},
		"new branch no base":  {workspaceProject(), WorkspaceBranchPlan{Name: "b", Create: true}},
		"context collides":    {collides, plan},
		"project name a path": {badName, plan},
	} {
		if _, err := NewWorkspaceSpec(tc.project, tc.plan); err == nil {
			t.Errorf("%s: must be refused", name)
		} else {
			var typed *WorkspaceSpecInvalidError
			if !errors.As(err, &typed) || !errors.Is(err, ErrWorkspaceSpecInvalid) {
				t.Errorf("%s: got %v, want WorkspaceSpecInvalidError", name, err)
			}
		}
	}

	_, err := NewWorkspaceSpec(badContext, plan)
	var repoErr *ContextRepoInvalidError
	if !errors.As(err, &repoErr) {
		t.Fatalf("an invalid context repository must surface typed: %v", err)
	}
}

// =============================================================================
// Deciding and writing the spec directory
// =============================================================================

// writtenSpec decides spec's directory and writes it under a fresh temporary
// parent, the way the host that executes a session does.
func writtenSpec(t *testing.T, spec WorkspaceSpec) (*WorkspaceSpecFiles, string) {
	t.Helper()
	files, err := NewWorkspaceSpecFiles(spec, false)
	if err != nil {
		t.Fatalf("NewWorkspaceSpecFiles: %v", err)
	}
	dir, cleanup, err := writeWorkspaceSpecDir(t.TempDir(), files.Files)
	if err != nil {
		t.Fatalf("writeWorkspaceSpecDir: %v", err)
	}
	t.Cleanup(cleanup)
	return files, dir
}

// TestWorkspaceSpecFiles_WritesTheSpecReadOnly proves the spec lands in a
// private directory as a read-only file, and that it names the token file on
// the credential tier while no file the host writes carries a token
// (go-quicbaas#899).
func TestWorkspaceSpecFiles_WritesTheSpecReadOnly(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	files, err := NewWorkspaceSpecFiles(spec, true)
	if err != nil {
		t.Fatalf("NewWorkspaceSpecFiles: %v", err)
	}
	dir, cleanup, err := writeWorkspaceSpecDir(t.TempDir(), files.Files)
	if err != nil {
		t.Fatalf("writeWorkspaceSpecDir: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != workspaceSpecDirMode {
		t.Fatalf("spec directory mode = %v, want %v", perm, workspaceSpecDirMode)
	}
	fi, err := os.Stat(filepath.Join(dir, WorkspaceSpecFile))
	if err != nil {
		t.Fatalf("stat %s: %v", WorkspaceSpecFile, err)
	}
	if perm := fi.Mode().Perm(); perm != workspaceSpecFileMode {
		t.Fatalf("%s mode = %v, want %v", WorkspaceSpecFile, perm, workspaceSpecFileMode)
	}
	if len(files.Files) != 1 {
		t.Fatalf("files = %v, want the spec alone: the token is not the host's to write", files.Files)
	}

	raw, err := os.ReadFile(filepath.Join(dir, WorkspaceSpecFile))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var decoded WorkspaceSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	if decoded.ForgeTokenFile != WorkspaceForgeCredentialFile || files.Spec.ForgeTokenFile != WorkspaceForgeCredentialFile {
		t.Fatalf("the spec must name the token file on the credential tier: %q", decoded.ForgeTokenFile)
	}
	if !strings.HasPrefix(decoded.ForgeTokenFile, WorkspaceCredentialDir+"/") {
		t.Fatalf("token file %q is not under the credential tier %s", decoded.ForgeTokenFile, WorkspaceCredentialDir)
	}
	if decoded.Project.RepoURL != spec.Project.RepoURL || len(decoded.ContextRepos) != 2 ||
		decoded.Branch.Name != "issue/7" || decoded.CheckoutRoot != WorkspaceCheckoutRoot {
		t.Fatalf("round trip = %+v", decoded)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cleanup must remove the directory: %v", err)
	}
}

// TestApplyWorkspaceSpec_MountsTheCredentialTierAsTmpfs: a spec that names a
// forge token gets the credential tier as a sized tmpfs with no host source,
// and one that names none gets no tier at all.
func TestApplyWorkspaceSpec_MountsTheCredentialTierAsTmpfs(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, forge := range []bool{true, false} {
		files, err := NewWorkspaceSpecFiles(spec, forge)
		if err != nil {
			t.Fatal(err)
		}
		req := namedWorkspaceRequest()
		ApplyWorkspaceSpec(req, files, "")
		var tiers []ContainerMount
		for _, m := range req.ExtraMounts {
			if m.Target == WorkspaceCredentialDir {
				tiers = append(tiers, m)
			}
		}
		if !forge {
			if len(tiers) != 0 {
				t.Fatalf("a spec with no token got a credential tier: %+v", tiers)
			}
			continue
		}
		if len(tiers) != 1 {
			t.Fatalf("credential tiers = %+v, want exactly one", tiers)
		}
		tier := tiers[0]
		if tier.Type != MountTypeTmpfs || tier.Source != "" || tier.TmpfsSizeBytes != WorkspaceCredentialTmpfsBytes {
			t.Fatalf("credential tier = %+v, want a %d-byte tmpfs with no host source", tier, WorkspaceCredentialTmpfsBytes)
		}
		if err := tier.Validate(); err != nil {
			t.Fatalf("the credential tier does not validate: %v", err)
		}
	}
}

// TestWorkspaceSpecFiles_NoTokenWritesNoTokenFile: a spawn with no forge
// token has no token file and says so in the spec.
func TestWorkspaceSpecFiles_NoTokenWritesNoTokenFile(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	spec.ForgeTokenFile = "stale"
	files, dir := writtenSpec(t, spec)
	if len(files.Files) != 1 {
		t.Fatalf("files = %v, want the spec alone", files.Files)
	}
	raw, err := os.ReadFile(filepath.Join(dir, WorkspaceSpecFile))
	if err != nil {
		t.Fatal(err)
	}
	var decoded WorkspaceSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ForgeTokenFile != "" {
		t.Fatalf("the spec must not name a token file that is not there: %q", decoded.ForgeTokenFile)
	}
}

// sshWorkspaceProject is workspaceProject with an ssh remote: the shape the
// Software Factory actually runs, where the clone is made by an ssh the
// container has to be told about a host key for.
func sshWorkspaceProject() *Project {
	p := workspaceProject()
	p.Git.RepoURL = "ssh://git@forge.invalid:2022/o/go-truststrap.git"
	return p
}

// TestWorkspaceSpecFiles_SSHWritesTheKnownHosts proves the ssh half of the
// checkout: the pinned key becomes a read-only known_hosts beside the spec,
// the spec names it, and the file pins the port the remote names.
func TestWorkspaceSpecFiles_SSHWritesTheKnownHosts(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(sshWorkspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	hostKey := testHostKeyLine(t)
	spec.SSHHostKey = hostKey

	files, dir := writtenSpec(t, spec)
	if files.Spec.KnownHostsFile != WorkspaceKnownHostsFile {
		t.Fatalf("the decided spec must name the known_hosts it holds: %q", files.Spec.KnownHostsFile)
	}
	info, err := os.Stat(filepath.Join(dir, WorkspaceKnownHostsFile))
	if err != nil {
		t.Fatalf("stat known_hosts: %v", err)
	}
	if perm := info.Mode().Perm(); perm != workspaceSpecFileMode {
		t.Fatalf("known_hosts mode = %v, want %v", perm, workspaceSpecFileMode)
	}
	content, err := os.ReadFile(filepath.Join(dir, WorkspaceKnownHostsFile))
	if err != nil {
		t.Fatal(err)
	}
	if want := "[forge.invalid]:2022 " + hostKey + "\n"; string(content) != want {
		t.Fatalf("known_hosts = %q, want %q", content, want)
	}

	raw, err := os.ReadFile(filepath.Join(dir, WorkspaceSpecFile))
	if err != nil {
		t.Fatal(err)
	}
	var decoded WorkspaceSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.KnownHostsFile != WorkspaceKnownHostsFile || decoded.SSHHostKey != hostKey {
		t.Fatalf("the container must read the same pinning the daemon decided: %+v", decoded)
	}
}

// TestWorkspaceSpecFiles_SSHWithoutAPinnedKeyIsRefused is the burn-in
// blocker's second half: an ssh remote whose binding pinned nothing must be
// refused HERE, before any container starts, with an error naming the field
// the operator has to fill in — never softened into accept-new.
func TestWorkspaceSpecFiles_SSHWithoutAPinnedKeyIsRefused(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(sshWorkspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, hostKey := range map[string]string{
		"unpinned":    "",
		"unparseable": "ssh-ed25519 not-base64",
	} {
		t.Run(name, func(t *testing.T) {
			spec.SSHHostKey = hostKey
			files, decideErr := NewWorkspaceSpecFiles(spec, false)
			if decideErr == nil || files != nil {
				t.Fatalf("an unpinned ssh checkout was decided: %+v", files)
			}
			var typed *SSHHostKeyError
			if !errors.As(decideErr, &typed) || !errors.Is(decideErr, ErrSSHHostKeyInvalid) {
				t.Fatalf("got %T %v, want *SSHHostKeyError", decideErr, decideErr)
			}
			if typed.Field() != "forge.ssh_host_key" {
				t.Fatalf("the refusal must name the missing field: %q", typed.Field())
			}
		})
	}
}

// TestWorkspaceSpecFiles_HTTPSWritesNoKnownHosts: an https checkout needs no
// host key, so no file is decided and no stale name survives on the spec the
// container reads.
func TestWorkspaceSpecFiles_HTTPSWritesNoKnownHosts(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	spec.KnownHostsFile = "stale"
	files, dir := writtenSpec(t, spec)
	if _, err := os.Stat(filepath.Join(dir, WorkspaceKnownHostsFile)); !os.IsNotExist(err) {
		t.Fatalf("an https checkout writes no known_hosts: %v", err)
	}
	if files.Spec.KnownHostsFile != "" {
		t.Fatalf("the spec must not name a known_hosts that is not there: %q", files.Spec.KnownHostsFile)
	}
}

// TestWriteWorkspaceSpecDir_UnwritableParent reports a spec directory the
// host could not create, typed and with the cause, leaving no cleanup.
func TestWriteWorkspaceSpecDir_UnwritableParent(t *testing.T) {
	t.Parallel()
	dir, cleanup, err := writeWorkspaceSpecDir(filepath.Join(t.TempDir(), "does-not-exist"),
		map[string][]byte{WorkspaceSpecFile: []byte("{}")})
	if dir != "" || cleanup != nil {
		t.Fatal("a failed write has nothing to clean up")
	}
	var typed *WorkspaceSpecMaterializeError
	if !errors.As(err, &typed) || !errors.Is(err, ErrWorkspaceSpecMaterialize) {
		t.Fatalf("got %v, want WorkspaceSpecMaterializeError", err)
	}
}

// TestWorkspaceSpecFiles_ASigningKeyWritesTheAllowedSigners: the file
// admits exactly the attempt's key for git signatures, read-only, and the
// spec names it.
func TestWorkspaceSpecFiles_ASigningKeyWritesTheAllowedSigners(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	spec.SigningKey = "ssh-ed25519 AAAAkey agent"
	files, dir := writtenSpec(t, spec)
	file := filepath.Join(dir, WorkspaceAllowedSignersFile)
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("allowed_signers: %v", err)
	}
	if string(content) != `* namespaces="git" ssh-ed25519 AAAAkey agent`+"\n" {
		t.Fatalf("allowed_signers = %q", content)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != workspaceSpecFileMode {
		t.Fatalf("mode = %v, want %v", info.Mode().Perm(), workspaceSpecFileMode)
	}
	if files.Spec.AllowedSignersFile != WorkspaceAllowedSignersFile {
		t.Fatalf("spec names %q", files.Spec.AllowedSignersFile)
	}
	// And the file is one a host provisioning the workspace will write: the
	// closed set of spec file names admits it.
	ws := namedWorkspace()
	ws.Spec = files
	if err := ws.Validate(); err != nil {
		t.Fatalf("a spec carrying allowed_signers is refused: %v", err)
	}
}

// TestWorkspaceSpecFiles_NoSigningKeyWritesNoAllowedSigners: an attempt
// that signs nothing gets no file, and a stale name in the spec is cleared.
func TestWorkspaceSpecFiles_NoSigningKeyWritesNoAllowedSigners(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	spec.AllowedSignersFile = "stale"
	files, dir := writtenSpec(t, spec)
	if _, err := os.Stat(filepath.Join(dir, WorkspaceAllowedSignersFile)); !os.IsNotExist(err) {
		t.Fatalf("an attempt that signs nothing writes no allowed_signers: %v", err)
	}
	if files.Spec.AllowedSignersFile != "" {
		t.Fatalf("the spec names %q", files.Spec.AllowedSignersFile)
	}
}

// =============================================================================
// The spawn request
// =============================================================================

// namedWorkspaceRequest is a spawn request that names its workspace, the only
// shape ApplyWorkspaceSpec acts on.
func namedWorkspaceRequest() *SpawnSessionRequest {
	return &SpawnSessionRequest{
		Backend:   "claude",
		ExtraArgs: []string{"--kept"},
		Workspace: &SessionWorkspace{TenantID: "t1", ProjectID: "p1", Key: "task-a1"},
	}
}

// TestApplyWorkspaceSpec_CarriesTheFilesAndNamesPathsOnly proves the spawn
// request carries the spec files for the executing host, the signing socket's
// mount and the two path variables — and no secret in the environment. The
// spec directory is NOT mounted here: it does not exist until the executing
// host writes it.
func TestApplyWorkspaceSpec_CarriesTheFilesAndNamesPathsOnly(t *testing.T) {
	t.Parallel()
	req := namedWorkspaceRequest()
	files := &WorkspaceSpecFiles{Files: map[string][]byte{WorkspaceSpecFile: []byte(`{}`)}}
	ApplyWorkspaceSpec(req, files, "/tmp/agent.sock")

	if req.Workspace.Spec != files {
		t.Fatal("the spec files must travel on the named workspace")
	}
	if len(req.ExtraMounts) != 1 {
		t.Fatalf("only the signing socket is mounted from this host: %+v", req.ExtraMounts)
	}
	sock := req.ExtraMounts[0]
	if sock.Source != "/tmp/agent.sock" || sock.Target != WorkspaceSigningAgentSocket || !sock.ReadOnly {
		t.Fatalf("signing agent mount = %+v", sock)
	}
	if req.Env[WorkspaceSpecEnv] != filepath.Join(WorkspaceSpecDir, WorkspaceSpecFile) {
		t.Fatalf("%s = %q", WorkspaceSpecEnv, req.Env[WorkspaceSpecEnv])
	}
	if req.Env[WorkspaceSigningAgentEnv] != WorkspaceSigningAgentSocket {
		t.Fatalf("%s = %q", WorkspaceSigningAgentEnv, req.Env[WorkspaceSigningAgentEnv])
	}
	if len(req.ExtraArgs) != 1 || req.ExtraArgs[0] != "--kept" {
		t.Fatalf("the caller's argv must survive: %v", req.ExtraArgs)
	}
	if _, named := req.Env[WorkspaceSSHCommandEnv]; named {
		t.Fatalf("an https checkout writes no known_hosts and must name no ssh command: %q",
			req.Env[WorkspaceSSHCommandEnv])
	}
}

// TestApplyWorkspaceSpec_SSHNamesTheKnownHostsStrictly: when the spec holds a
// known_hosts, the spawn carries the GIT_SSH_COMMAND that makes the worker's
// git verify the forge against it — the PATH inside the container.
func TestApplyWorkspaceSpec_SSHNamesTheKnownHostsStrictly(t *testing.T) {
	t.Parallel()
	req := namedWorkspaceRequest()
	ApplyWorkspaceSpec(req, &WorkspaceSpecFiles{Spec: WorkspaceSpec{KnownHostsFile: WorkspaceKnownHostsFile}}, "")

	got := req.Env[WorkspaceSSHCommandEnv]
	want := WorkspaceSSHCommand(filepath.Join(WorkspaceSpecDir, WorkspaceKnownHostsFile))
	if got != want {
		t.Fatalf("%s = %q, want %q", WorkspaceSSHCommandEnv, got, want)
	}
	if len(req.ExtraMounts) != 0 {
		t.Fatalf("no signing socket means nothing mounted from this host: %+v", req.ExtraMounts)
	}
}

// TestApplyWorkspaceSpec_NothingToApply: a nil request, a request that names
// no workspace, and a nil spec are all no-ops.
func TestApplyWorkspaceSpec_NothingToApply(t *testing.T) {
	t.Parallel()
	files := &WorkspaceSpecFiles{}
	ApplyWorkspaceSpec(nil, files, "/tmp/agent.sock")

	unnamed := &SpawnSessionRequest{Backend: "claude"}
	ApplyWorkspaceSpec(unnamed, files, "/tmp/agent.sock")
	if len(unnamed.ExtraMounts) != 0 || len(unnamed.Env) != 0 {
		t.Fatalf("a request naming no workspace is left alone: %+v", unnamed)
	}

	named := namedWorkspaceRequest()
	ApplyWorkspaceSpec(named, nil, "/tmp/agent.sock")
	if named.Workspace.Spec != nil || len(named.ExtraMounts) != 0 || len(named.Env) != 0 {
		t.Fatalf("a nil spec applies nothing: %+v", named)
	}
	if mounts := WorkspaceSpecMounts("", ""); len(mounts) != 0 {
		t.Fatalf("mounts = %+v", mounts)
	}
}

// TestWorkspaceSpecErrors_MessageAndSentinel: every workspace-spec error
// names what went wrong and unwraps to its sentinel.
func TestWorkspaceSpecErrors_MessageAndSentinel(t *testing.T) {
	t.Parallel()
	invalid := &WorkspaceSpecInvalidError{Field: "branch.name", Message: "is required"}
	if !errors.Is(invalid, ErrWorkspaceSpecInvalid) || !strings.Contains(invalid.Error(), "branch.name is required") {
		t.Fatalf("invalid error = %q", invalid.Error())
	}
	cause := errors.New("read-only file system")
	mat := &WorkspaceSpecMaterializeError{Message: "writing workspace.json failed", Cause: cause}
	if !errors.Is(mat, ErrWorkspaceSpecMaterialize) || !errors.Is(mat, cause) ||
		!strings.Contains(mat.Error(), "read-only file system") {
		t.Fatalf("materialize error = %q", mat.Error())
	}
}

// TestContextRepoInvalidError_NamesTheRepository proves the message carries
// the offending repository when there is one.
func TestContextRepoInvalidError_NamesTheRepository(t *testing.T) {
	t.Parallel()
	named := &ContextRepoInvalidError{Name: "handbook", Field: "ref", Message: "is required"}
	if !strings.Contains(named.Error(), "handbook: ref is required") {
		t.Fatalf("named error = %q", named.Error())
	}
	anonymous := &ContextRepoInvalidError{Field: "name", Message: "is required"}
	if strings.Contains(anonymous.Error(), "::") || !strings.Contains(anonymous.Error(), "name is required") {
		t.Fatalf("anonymous error = %q", anonymous.Error())
	}
}
