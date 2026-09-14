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
// Materialization
// =============================================================================

// TestMaterializeWorkspaceSpec_WritesTheSpecReadOnly proves the spec and the
// forge token land in a private directory as read-only files, and that the
// spec names the token file rather than carrying the token.
func TestMaterializeWorkspaceSpec_WritesTheSpecReadOnly(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := MaterializeWorkspaceSpec(spec, []byte("forge-secret"))
	if err != nil {
		t.Fatalf("MaterializeWorkspaceSpec: %v", err)
	}
	dir, cleanup := ws.Dir, ws.Cleanup
	t.Cleanup(cleanup)

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != workspaceSpecDirMode {
		t.Fatalf("spec directory mode = %v, want %v", perm, workspaceSpecDirMode)
	}

	for _, name := range []string{WorkspaceSpecFile, WorkspaceForgeTokenFile} {
		fi, statErr := os.Stat(filepath.Join(dir, name))
		if statErr != nil {
			t.Fatalf("stat %s: %v", name, statErr)
		}
		if perm := fi.Mode().Perm(); perm != workspaceSpecFileMode {
			t.Fatalf("%s mode = %v, want %v", name, perm, workspaceSpecFileMode)
		}
	}

	raw, err := os.ReadFile(filepath.Join(dir, WorkspaceSpecFile))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var decoded WorkspaceSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
	if decoded.ForgeTokenFile != WorkspaceForgeTokenFile {
		t.Fatalf("the spec must name the token file: %q", decoded.ForgeTokenFile)
	}
	if decoded.Project.RepoURL != spec.Project.RepoURL || len(decoded.ContextRepos) != 2 ||
		decoded.Branch.Name != "issue/7" || decoded.CheckoutRoot != WorkspaceCheckoutRoot {
		t.Fatalf("round trip = %+v", decoded)
	}
	if strings.Contains(string(raw), "forge-secret") {
		t.Fatal("the spec must not carry the token value")
	}

	token, err := os.ReadFile(filepath.Join(dir, WorkspaceForgeTokenFile))
	if err != nil || string(token) != "forge-secret" {
		t.Fatalf("token file = %q, %v", token, err)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cleanup must remove the directory: %v", err)
	}
}

// TestMaterializeWorkspaceSpec_NoTokenWritesNoTokenFile: a spawn with no
// forge token has no token file and says so in the spec.
func TestMaterializeWorkspaceSpec_NoTokenWritesNoTokenFile(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	spec.ForgeTokenFile = "stale"
	ws, err := MaterializeWorkspaceSpec(spec, nil)
	if err != nil {
		t.Fatalf("MaterializeWorkspaceSpec: %v", err)
	}
	dir := ws.Dir
	t.Cleanup(ws.Cleanup)
	if _, err := os.Stat(filepath.Join(dir, WorkspaceForgeTokenFile)); !os.IsNotExist(err) {
		t.Fatalf("no token means no token file: %v", err)
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
	p.Git.RepoURL = "ssh://git@forge.invalid:2222/o/go-truststrap.git"
	return p
}

// TestMaterializeWorkspaceSpec_SSHWritesTheKnownHosts proves the ssh half of
// the checkout: the pinned key becomes a read-only known_hosts beside the
// spec, the spec names it, and the file pins the port the remote names.
func TestMaterializeWorkspaceSpec_SSHWritesTheKnownHosts(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(sshWorkspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	hostKey := testHostKeyLine(t)
	spec.SSHHostKey = hostKey

	ws, err := MaterializeWorkspaceSpec(spec, nil)
	if err != nil {
		t.Fatalf("MaterializeWorkspaceSpec: %v", err)
	}
	t.Cleanup(ws.Cleanup)

	if ws.Spec.KnownHostsFile != WorkspaceKnownHostsFile {
		t.Fatalf("the returned spec must name the known_hosts it wrote: %q", ws.Spec.KnownHostsFile)
	}
	info, err := os.Stat(filepath.Join(ws.Dir, WorkspaceKnownHostsFile))
	if err != nil {
		t.Fatalf("stat known_hosts: %v", err)
	}
	if perm := info.Mode().Perm(); perm != workspaceSpecFileMode {
		t.Fatalf("known_hosts mode = %v, want %v", perm, workspaceSpecFileMode)
	}
	content, err := os.ReadFile(filepath.Join(ws.Dir, WorkspaceKnownHostsFile))
	if err != nil {
		t.Fatal(err)
	}
	if want := "[forge.invalid]:2222 " + hostKey + "\n"; string(content) != want {
		t.Fatalf("known_hosts = %q, want %q", content, want)
	}

	raw, err := os.ReadFile(filepath.Join(ws.Dir, WorkspaceSpecFile))
	if err != nil {
		t.Fatal(err)
	}
	var decoded WorkspaceSpec
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.KnownHostsFile != WorkspaceKnownHostsFile || decoded.SSHHostKey != hostKey {
		t.Fatalf("the container must read the same pinning the daemon wrote: %+v", decoded)
	}
}

// TestMaterializeWorkspaceSpec_SSHWithoutAPinnedKeyIsRefused is the burn-in
// blocker's second half: an ssh remote whose binding pinned nothing must be
// refused HERE, before any container starts, with an error naming the field
// the operator has to fill in — never softened into accept-new.
func TestMaterializeWorkspaceSpec_SSHWithoutAPinnedKeyIsRefused(t *testing.T) {
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
			ws, matErr := MaterializeWorkspaceSpecIn(t.TempDir(), spec, nil)
			if matErr == nil {
				t.Fatal("an unpinned ssh checkout was materialized")
			}
			if ws.Dir != "" || ws.Cleanup != nil {
				t.Fatalf("a refused materialization must leave nothing behind: %+v", ws.Dir)
			}
			var typed *SSHHostKeyError
			if !errors.As(matErr, &typed) || !errors.Is(matErr, ErrSSHHostKeyInvalid) {
				t.Fatalf("got %T %v, want *SSHHostKeyError", matErr, matErr)
			}
			if typed.Field() != "forge.ssh_host_key" {
				t.Fatalf("the refusal must name the missing field: %q", typed.Field())
			}
		})
	}
}

// TestMaterializeWorkspaceSpec_HTTPSWritesNoKnownHosts: an https checkout
// needs no host key, so no file is written and no stale name survives on the
// spec the container reads.
func TestMaterializeWorkspaceSpec_HTTPSWritesNoKnownHosts(t *testing.T) {
	t.Parallel()
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	spec.KnownHostsFile = "stale"
	ws, err := MaterializeWorkspaceSpec(spec, nil)
	if err != nil {
		t.Fatalf("MaterializeWorkspaceSpec: %v", err)
	}
	t.Cleanup(ws.Cleanup)
	if _, err := os.Stat(filepath.Join(ws.Dir, WorkspaceKnownHostsFile)); !os.IsNotExist(err) {
		t.Fatalf("an https checkout writes no known_hosts: %v", err)
	}
	if ws.Spec.KnownHostsFile != "" {
		t.Fatalf("the spec must not name a known_hosts that is not there: %q", ws.Spec.KnownHostsFile)
	}
}

// =============================================================================
// Mounts and the spawn request
// =============================================================================

// TestApplyWorkspaceSpec_MountsReadOnlyAndNamesPathsOnly proves the spawn
// request carries the mounts and the two path variables, and no secret.
func TestApplyWorkspaceSpec_MountsReadOnlyAndNamesPathsOnly(t *testing.T) {
	t.Parallel()
	req := &SpawnSessionRequest{Backend: "claude", ExtraArgs: []string{"--kept"}}
	ApplyWorkspaceSpec(req, MaterializedWorkspace{Dir: "/tmp/spec-dir"}, "/tmp/agent.sock")

	if len(req.ExtraMounts) != 2 {
		t.Fatalf("mounts = %+v", req.ExtraMounts)
	}
	spec := req.ExtraMounts[0]
	if spec.Source != "/tmp/spec-dir" || spec.Target != WorkspaceSpecDir || !spec.ReadOnly {
		t.Fatalf("the spec mount must be read-only at its fixed target: %+v", spec)
	}
	sock := req.ExtraMounts[1]
	if sock.Source != "/tmp/agent.sock" || sock.Target != WorkspaceSigningAgentSocket {
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

// TestApplyWorkspaceSpec_SSHNamesTheKnownHostsStrictly: when the spec wrote a
// known_hosts, the spawn carries the GIT_SSH_COMMAND that makes the worker's
// git verify the forge against it — the PATH inside the container, never the
// daemon's directory.
func TestApplyWorkspaceSpec_SSHNamesTheKnownHostsStrictly(t *testing.T) {
	t.Parallel()
	req := &SpawnSessionRequest{Backend: "claude"}
	ws := MaterializedWorkspace{
		Dir:  "/srv/projects/t1/p1/quicbaas-workspace-abc",
		Spec: WorkspaceSpec{KnownHostsFile: WorkspaceKnownHostsFile},
	}
	ApplyWorkspaceSpec(req, ws, "")

	got := req.Env[WorkspaceSSHCommandEnv]
	want := WorkspaceSSHCommand(filepath.Join(WorkspaceSpecDir, WorkspaceKnownHostsFile))
	if got != want {
		t.Fatalf("%s = %q, want %q", WorkspaceSSHCommandEnv, got, want)
	}
	if strings.Contains(got, ws.Dir) {
		t.Fatalf("the daemon's own path must not reach the container: %q", got)
	}
}

// TestApplyWorkspaceSpec_NothingToApply: a nil request and an empty spec
// directory are both no-ops.
func TestApplyWorkspaceSpec_NothingToApply(t *testing.T) {
	t.Parallel()
	ApplyWorkspaceSpec(nil, MaterializedWorkspace{Dir: "/tmp/spec-dir"}, "")

	req := &SpawnSessionRequest{Backend: "claude"}
	ApplyWorkspaceSpec(req, MaterializedWorkspace{}, "")
	if len(req.ExtraMounts) != 0 {
		t.Fatalf("nothing to mount: %+v", req.ExtraMounts)
	}
	if len(req.Env) != 0 {
		t.Fatalf("nothing to name: %+v", req.Env)
	}
	if mounts := WorkspaceSpecMounts("", ""); len(mounts) != 0 {
		t.Fatalf("mounts = %+v", mounts)
	}
}

// TestMaterializeWorkspaceSpec_UnwritableTempDir reports a spec directory the
// daemon could not create, typed and with the cause.
func TestMaterializeWorkspaceSpec_UnwritableTempDir(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))
	spec, err := NewWorkspaceSpec(workspaceProject(), WorkspaceBranchPlan{Name: "issue/7", Base: "main", Create: true})
	if err != nil {
		t.Fatal(err)
	}
	ws, err := MaterializeWorkspaceSpec(spec, nil)
	if ws.Cleanup != nil {
		t.Fatal("a failed materialization has nothing to clean up")
	}
	var typed *WorkspaceSpecMaterializeError
	if !errors.As(err, &typed) || !errors.Is(err, ErrWorkspaceSpecMaterialize) {
		t.Fatalf("got %v, want WorkspaceSpecMaterializeError", err)
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
