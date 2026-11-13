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

// The test helpers here are shared by files that build without the
// codec_json tag and files gated on it, so they live in an untagged file.

import (
	"context"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// fakeJobStarter records the jobs a caller asked the engine to start. It is
// the package's one job-starting double — the review dispatcher and the
// triage scheduler both drive orchestration.JobStarter, so a second copy
// would only be a second thing to keep in step.
type fakeJobStarter struct {
	mu      sync.Mutex
	started []*orchestration.StartJobRequest
	err     error
	noID    bool
	seq     int

	// ctxTenants is the tenant bound to the CONTEXT of each call, which the
	// engine guards StartJob on and which a request field cannot stand in for.
	// Recorded because a double that discarded the context is what let
	// go-quicbaas#918's sibling defect — a triage backstop refused `tenant ID
	// required` on every pass — pass a green suite.
	ctxTenants []string
}

func (f *fakeJobStarter) StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctxTenants = append(f.ctxTenants, orchestration.TenantFromContext(ctx))
	if f.err != nil {
		return nil, f.err
	}
	f.seq++
	f.started = append(f.started, req)
	if f.noID {
		return &orchestration.StartJobResponse{}, nil
	}
	return &orchestration.StartJobResponse{JobID: "job-" + itoa(f.seq)}, nil
}

func (f *fakeJobStarter) requests() []*orchestration.StartJobRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*orchestration.StartJobRequest(nil), f.started...)
}

// contextTenants returns the tenant each call carried on its context.
func (f *fakeJobStarter) contextTenants() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ctxTenants...)
}

// deliveredTo is the binding a delivery is dispatched against. Only the
// project id and the discovery mode are read on that path, and the zero mode
// is DiscoveryBoth — the delivery path every binding had before the floor
// existed.
func deliveredTo(mode DiscoveryMode) *ProjectBinding {
	return &ProjectBinding{ProjectID: testProject, Discovery: mode}
}

// sshAccessProject is a project on an SSH remote, which is the shape the
// burn-in node runs and the shape triage could not clone.
func sshAccessProject() *orchestration.Project {
	project := testOrchProject()
	project.Git.RepoURL = "ssh://git@forge.test:2022/acme/api.git"
	return project
}

// accessRig is a forgeAccess with fakes behind it.
type accessRig struct {
	access  forgeAccess
	signing *fakeSigningAgent
	tokens  *fakeTokens
	forge   ForgeConfig
}

// testRepoGuard is the operator allowlist every test project's forge is on.
func testRepoGuard(t *testing.T) RepoHostGuard {
	t.Helper()
	guard, err := NewRepoHostGuard([]string{"forge.test"}, false)
	if err != nil {
		t.Fatalf("NewRepoHostGuard: %v", err)
	}
	return guard
}

// testWorkspaceKey is the workspace key the rig's plans name.
const testWorkspaceKey = "attempt-1"

// open runs the seam for one plan, defaulting the project, forge and key.
func (r *accessRig) open(t *testing.T, project *orchestration.Project, scope SigningScope) (*orchestration.SpawnSessionRequest, *containerWorkspace, error) {
	t.Helper()
	req := &orchestration.SpawnSessionRequest{Backend: "claude", ProjectID: project.ID}
	workspace, err := r.access.Open(context.Background(), req, workspacePlan{
		TenantID: project.TenantID,
		Project:  project,
		Forge:    r.forge,
		Branch:   orchestration.WorkspaceBranchPlan{Name: "main"},
		Key:      testWorkspaceKey,
		Scope:    scope,
	})
	if workspace != nil {
		t.Cleanup(func() { workspace.Release(context.Background()) })
	}
	return req, workspace, err
}

// newAccessRig builds the seam over fakes.
func newAccessRig(t *testing.T) *accessRig {
	t.Helper()
	signing := &fakeSigningAgent{}
	tokens := &fakeTokens{token: []byte("t0ken")}
	access, err := newForgeAccess(tokens, signing, testRepoGuard(t), orchestration.WorkspaceCheckoutRoot, testSocketsRoot(t))
	if err != nil {
		t.Fatalf("newForgeAccess: %v", err)
	}
	forge := testBinding().Forge
	forge.SSHHostKey = testSSHHostKey
	return &accessRig{access: access, signing: signing, tokens: tokens, forge: forge}
}

const (
	qwenProfile = "qwen"
	qwenLogin   = "qwen-reviewer"
)

// qwenAlternate is the advisory alternate every profile test binds: the
// owner's reference profile, on the local model.
func qwenAlternate() ReviewerConfig {
	return ReviewerConfig{
		Name: qwenProfile, Agent: testQwenAgent,
		Login: qwenLogin, TokenSecretRef: "sdlc/reviewer/api/qwen-token",
	}
}

// profiledBinding is testBinding with a codex default and the qwen alternate.
func profiledBinding() *ProjectBinding {
	binding := testBinding()
	binding.Reviewer = ReviewerConfig{Agent: testReviewerAgent, Login: fakeReviewerLogin, TokenSecretRef: "reviewer-tok"}
	binding.Reviewers = []ReviewerConfig{qwenAlternate()}
	return binding
}

func boolPtr(b bool) *bool { return &b }

// qwenJob is a review job for pull request 41 run as the qwen profile.
func (r *reviewRunnerRig) qwenJob() *orchestration.JobRun {
	job := r.job(41)
	job.Config.Params = ReviewJobSubject{PullRequest: 41, Profile: qwenProfile}.Params()
	return job
}

// tenantEditedResolver is a resolver over a store in which the test tenant
// has edited the sdlc prompt name to open with marker.
func tenantEditedResolver(t *testing.T, name, marker string) *prompts.Resolver {
	t.Helper()
	store, err := prompts.NewStore(newMemoryResolver())
	if err != nil {
		t.Fatal(err)
	}
	spec, known := SDLCPrompts().Spec().Prompt(name)
	if !known {
		t.Fatalf("%s is not in the sdlc set", name)
	}
	ref := prompts.Ref{TenantID: testTenant, Set: SetSDLC, Name: spec.Name}
	if _, err := store.Save(context.Background(), ref, prompts.Change{
		Stamp:   prompts.Stamp{Author: "alice"},
		Content: marker + spec.Default,
	}); err != nil {
		t.Fatal(err)
	}
	return prompts.NewResolver(store)
}
