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
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The pre-flight
// =============================================================================
//
// EVERY TEST HERE IS ABOUT WHAT THE WORKSPACE IS, not about what the clone
// did. The container is put into a state — a repository missing, an origin
// wrong, a repository with no commit, a checkout on the wrong branch — and the
// pre-flight has to name it. That is the whole difference between this and
// checkout_test.go: the clone is asked whether it failed, the pre-flight is
// asked whether the workspace is fit.

// materializedCheckout is a container that already holds the checkout the spec
// describes: every repository present, cloned from its own remote, holding a
// commit, and the project on the planned branch. Each test below breaks exactly
// one of those facts.
func materializedCheckout(spec orchestration.WorkspaceSpec) *fakeContainer {
	container := &fakeContainer{}
	container.mu.Lock()
	defer container.mu.Unlock()
	repos := append([]orchestration.WorkspaceRepoSpec{spec.Project}, spec.ContextRepos...)
	for _, repo := range repos {
		dir := path.Join(spec.CheckoutRoot, repo.Name)
		container.markRepo(dir, repo.RepoURL)
		container.setBranch(dir, []string{repo.Ref})
	}
	container.setBranch(path.Join(spec.CheckoutRoot, spec.Project.Name),
		[]string{"-B", spec.Branch.Name, "origin/" + spec.Branch.Base})
	return container
}

// testPreflight builds the pre-flight over a fake container.
func testPreflight(container *fakeContainer, spec orchestration.WorkspaceSpec) *preflight {
	return &preflight{
		gitSteps: gitSteps{
			exec: func(s *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
				return container.exec("container-1", s)
			},
			taskID: testTask,
		},
		spec: spec,
	}
}

// unfit asserts the pre-flight refused, and answers the refusal so the caller
// can say what it must name.
func unfit(t *testing.T, p *preflight) *WorkspaceUnfitError {
	t.Helper()
	err := p.Verify()
	var fault *WorkspaceUnfitError
	if !errors.As(err, &fault) {
		t.Fatalf("err = %v, want a WorkspaceUnfitError", err)
	}
	if !errors.Is(err, ErrWorkspaceUnfit) || !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want both sentinels", err)
	}
	return fault
}

// TestPreflight_AMaterializedCheckoutPasses is the baseline every other test
// here breaks one fact of: a workspace the cloner made is fit, and the
// pre-flight says nothing about it.
func TestPreflight_AMaterializedCheckoutPasses(t *testing.T) {
	spec := testWorkspaceSpec(t, true,
		orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"})
	p := testPreflight(materializedCheckout(spec), spec)
	if err := p.Verify(); err != nil {
		t.Fatalf("a materialized checkout was refused: %v", err)
	}
	// AND IT WROTE NOTHING. Every command is a probe; a pre-flight that
	// cloned, fetched or checked out would be a second checkout.
	for _, step := range p.steps {
		for _, forbidden := range []string{"clone", "fetch", "checkout ", "commit", "push"} {
			if strings.Contains(step.Command, forbidden) {
				t.Fatalf("the pre-flight ran %q, which changes the workspace", step.Command)
			}
		}
	}
}

// TestPreflight_ADeclaredContextRepositoryThatIsNotThereIsRefusedAndNamed is
// the burn-in fault, go-quicbaas#908: the project checkout is perfect and the
// repository the work actually needs is absent. A refusal that did not NAME it
// would send its reader at the project checkout, which is fine.
func TestPreflight_ADeclaredContextRepositoryThatIsNotThereIsRefusedAndNamed(t *testing.T) {
	spec := testWorkspaceSpec(t, true,
		orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"},
		orchestration.ContextRepo{Name: "automatethethings", RepoURL: "https://forge.test/acme/att.git", Ref: "main"})
	container := materializedCheckout(spec)
	container.mu.Lock()
	delete(container.repos, path.Join(spec.CheckoutRoot, "automatethethings"))
	container.mu.Unlock()

	fault := unfit(t, testPreflight(container, spec))
	if fault.Repo != "automatethethings" {
		t.Fatalf("the refusal names %q, not the repository that is missing", fault.Repo)
	}
	if !strings.Contains(fault.Error(), "automatethethings") ||
		!strings.Contains(fault.Error(), "not a git repository") {
		t.Fatalf("the refusal does not say what is wrong: %v", fault)
	}
	if fault.TaskID != testTask {
		t.Fatalf("task = %q, want the attempt's", fault.TaskID)
	}
}

// TestPreflight_ADeclaringProjectAndANonDeclaringOneDifferOnlyByTheDeclaration:
// a project that declares no context repository is judged exactly as it was,
// and one that declares two has both proved.
func TestPreflight_ADeclaringProjectAndANonDeclaringOneDifferOnlyByTheDeclaration(t *testing.T) {
	bare := testWorkspaceSpec(t, true)
	bareFlight := testPreflight(materializedCheckout(bare), bare)
	if err := bareFlight.Verify(); err != nil {
		t.Fatalf("a project declaring nothing was refused: %v", err)
	}

	declared := testWorkspaceSpec(t, true,
		orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"},
		orchestration.ContextRepo{Name: "automatethethings", RepoURL: "https://forge.test/acme/att.git", Ref: "main"})
	declaredFlight := testPreflight(materializedCheckout(declared), declared)
	if err := declaredFlight.Verify(); err != nil {
		t.Fatalf("two declared context repositories were refused: %v", err)
	}

	// The declaring project is judged on MORE, and on nothing else: the extra
	// probes all name the declared directories.
	for _, dir := range []string{"handbook", "automatethethings"} {
		if !strings.Contains(strings.Join(commandsOf(declaredFlight), "\n"), "-C "+dir) {
			t.Fatalf("%s was never probed", dir)
		}
		if strings.Contains(strings.Join(commandsOf(bareFlight), "\n"), "-C "+dir) {
			t.Fatalf("a project declaring nothing probed %s", dir)
		}
	}
}

// commandsOf is every command one pre-flight ran.
func commandsOf(p *preflight) []string {
	out := make([]string, 0, len(p.steps))
	for _, step := range p.steps {
		out = append(out, step.Command)
	}
	return out
}

// TestPreflight_AnEmptyCheckoutIsRefused is the other fault this workspace
// produced: an attempt whose /workspace/<project> held nothing. A worker
// prompted on it read an empty directory, answered "OK", and was charged.
func TestPreflight_AnEmptyCheckoutIsRefused(t *testing.T) {
	spec := testWorkspaceSpec(t, true)
	container := materializedCheckout(spec)
	container.mu.Lock()
	container.repos = map[string]bool{}
	container.mu.Unlock()

	fault := unfit(t, testPreflight(container, spec))
	if fault.Repo != spec.Project.Name {
		t.Fatalf("the refusal names %q, not the project", fault.Repo)
	}
	if fault.Dir != path.Join(spec.CheckoutRoot, spec.Project.Name) {
		t.Fatalf("dir = %q, want the container's own path", fault.Dir)
	}
}

// TestPreflight_ARepositoryWithNoCommitIsRefused: a clone killed part-way
// leaves a directory that is a git repository and holds no object, which reads
// as present to everything but the check that asks for one.
func TestPreflight_ARepositoryWithNoCommitIsRefused(t *testing.T) {
	spec := testWorkspaceSpec(t, true)
	container := materializedCheckout(spec)
	container.mu.Lock()
	container.heads = map[string]string{}
	container.mu.Unlock()

	fault := unfit(t, testPreflight(container, spec))
	if !strings.Contains(fault.Reason, "holds no commit") {
		t.Fatalf("reason = %q, want the unfinished clone", fault.Reason)
	}
}

// TestPreflight_ADirectoryHoldingAnotherRepositoryIsRefused: the fault the
// clone's own idempotence cannot see. A resumed attempt is bind-mounted through
// the directory its predecessor used; if that directory holds a DIFFERENT
// repository the clone is skipped as "already present" and nothing else would
// ever notice.
func TestPreflight_ADirectoryHoldingAnotherRepositoryIsRefused(t *testing.T) {
	spec := testWorkspaceSpec(t, true)
	dir := path.Join(spec.CheckoutRoot, spec.Project.Name)
	container := materializedCheckout(spec)
	container.mu.Lock()
	container.origins[dir] = "https://forge.test/acme/some-other-repository.git"
	container.mu.Unlock()

	fault := unfit(t, testPreflight(container, spec))
	if !strings.Contains(fault.Reason, "some-other-repository") ||
		!strings.Contains(fault.Reason, spec.Project.RepoURL) {
		t.Fatalf("reason = %q, want both the found and the wanted remote", fault.Reason)
	}
}

// TestPreflight_ARepositoryWithNoOriginIsRefused: a repository nothing can be
// fetched from or pushed to is as unusable as one cloned from the wrong place,
// and the worker's own git would only find out on its first remote operation.
func TestPreflight_ARepositoryWithNoOriginIsRefused(t *testing.T) {
	spec := testWorkspaceSpec(t, true)
	container := materializedCheckout(spec)
	container.mu.Lock()
	container.origins = map[string]string{}
	container.mu.Unlock()

	fault := unfit(t, testPreflight(container, spec))
	if !strings.Contains(fault.Reason, "no origin remote at all") {
		t.Fatalf("reason = %q, want the missing remote", fault.Reason)
	}
}

// TestPreflight_ACheckoutOnTheWrongBranchIsRefused: the charter tells the
// worker its branch is already checked out, so a checkout left on the base or
// detached is one where every commit it makes lands where the publisher will
// not look — and the worker has no way to know, having been told otherwise.
func TestPreflight_ACheckoutOnTheWrongBranchIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		branch string
		want   string
	}{
		{name: "left on the base", branch: "main", want: "branch main"},
		{name: "detached", branch: "HEAD", want: "a detached HEAD"},
		{name: "unknown to git", branch: "", want: "a detached HEAD"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := testWorkspaceSpec(t, true)
			dir := path.Join(spec.CheckoutRoot, spec.Project.Name)
			container := materializedCheckout(spec)
			container.mu.Lock()
			if tc.branch == "" {
				delete(container.branches, dir)
			} else {
				container.branches[dir] = tc.branch
			}
			container.mu.Unlock()

			fault := unfit(t, testPreflight(container, spec))
			if !strings.Contains(fault.Reason, tc.want) ||
				!strings.Contains(fault.Reason, spec.Branch.Name) {
				t.Fatalf("reason = %q, want %q and the planned branch", fault.Reason, tc.want)
			}
		})
	}
}

// TestPreflight_TheProjectIsJudgedBeforeItsContextRepositories: a checkout root
// that is entirely empty must report the project, once, rather than one
// indistinguishable fault per declared repository.
func TestPreflight_TheProjectIsJudgedBeforeItsContextRepositories(t *testing.T) {
	spec := testWorkspaceSpec(t, true,
		orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"})
	container := &fakeContainer{}

	fault := unfit(t, testPreflight(container, spec))
	if fault.Repo != spec.Project.Name {
		t.Fatalf("the refusal names %q, want the project first", fault.Repo)
	}
}

// TestPreflight_RefusesASpecThatDescribesNoCheckout: a pre-flight handed a spec
// with a field missing must refuse rather than pass, because a pre-flight that
// verified nothing is a guarantee that verified nothing.
func TestPreflight_RefusesASpecThatDescribesNoCheckout(t *testing.T) {
	base := testWorkspaceSpec(t, true)
	for _, tc := range []struct {
		name    string
		breakIt func(*orchestration.WorkspaceSpec)
		field   string
	}{
		{name: "no checkout root", breakIt: func(s *orchestration.WorkspaceSpec) { s.CheckoutRoot = " " }, field: "workspace.checkout_root"},
		{name: "no project name", breakIt: func(s *orchestration.WorkspaceSpec) { s.Project.Name = "" }, field: "workspace.project.name"},
		{name: "no project remote", breakIt: func(s *orchestration.WorkspaceSpec) { s.Project.RepoURL = "" }, field: "workspace.project.repo_url"},
		{name: "no branch", breakIt: func(s *orchestration.WorkspaceSpec) { s.Branch.Name = "" }, field: "workspace.branch.name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := base
			tc.breakIt(&spec)
			p := testPreflight(materializedCheckout(base), spec)
			err := p.Verify()
			var invalid *InvalidInputError
			if !errors.As(err, &invalid) || invalid.Field != tc.field {
				t.Fatalf("err = %v, want %s refused", err, tc.field)
			}
			if len(p.steps) != 0 {
				t.Fatalf("a spec that describes no checkout still ran %d commands", len(p.steps))
			}
		})
	}
}

// TestPreflight_AnExecThatCouldNotRunIsReturnedAsItIs: a container that is gone
// is not an unfit workspace, and dressing it as one would send an operator to
// repair a checkout when what broke was the runtime.
func TestPreflight_AnExecThatCouldNotRunIsReturnedAsItIs(t *testing.T) {
	gone := errors.New("no such container")
	spec := testWorkspaceSpec(t, true)
	p := &preflight{
		gitSteps: gitSteps{
			exec:   func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) { return nil, gone },
			taskID: testTask,
		},
		spec: spec,
	}
	err := p.Verify()
	if !errors.Is(err, gone) {
		t.Fatalf("err = %v, want the exec's own error", err)
	}
	var fault *WorkspaceUnfitError
	if errors.As(err, &fault) {
		t.Fatal("a dead container was reported as an unfit workspace")
	}
}

// TestPreflight_EveryProbeIsRecordedAsEvidence: the point of running the probes
// in Go is that nobody has to run them by hand afterwards, which only holds if
// what they said is kept.
func TestPreflight_EveryProbeIsRecordedAsEvidence(t *testing.T) {
	spec := testWorkspaceSpec(t, true,
		orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"})
	p := testPreflight(materializedCheckout(spec), spec)
	if err := p.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	evidence := p.Evidence()
	for _, want := range []string{"rev-parse --git-dir", "remote get-url origin", "rev-parse HEAD", "--abbrev-ref"} {
		if !strings.Contains(evidence, want) {
			t.Fatalf("the evidence does not carry %q:\n%s", want, evidence)
		}
	}
	// A REFUSAL KEEPS ITS EVIDENCE TOO — that is the case a person reads.
	container := materializedCheckout(spec)
	container.mu.Lock()
	container.repos = map[string]bool{}
	container.mu.Unlock()
	refused := testPreflight(container, spec)
	if err := refused.Verify(); err == nil {
		t.Fatal("an empty checkout passed")
	}
	if refused.Evidence() == "" {
		t.Fatal("a refusal recorded no evidence")
	}
}

// TestWorkspaceUnfitParkReason_NamesTheFaultAndCarriesTheOperatorCondition: the
// reason an operator reads has to say which repository, that nothing was spent,
// and who clears it.
func TestWorkspaceUnfitParkReason_NamesTheFaultAndCarriesTheOperatorCondition(t *testing.T) {
	fault := &WorkspaceUnfitError{
		TaskID: testTask, Repo: "automatethethings",
		Dir: "/workspace/automatethethings", Reason: "is not a git repository",
	}
	reason, err := WorkspaceUnfitParkReason(fault)
	if err != nil {
		t.Fatalf("WorkspaceUnfitParkReason: %v", err)
	}
	for _, want := range []string{"automatethethings", "no tokens were spent", "context repository"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("reason = %q, want it to carry %q", reason, want)
		}
	}
	condition, err := ParseUnblockCondition(reason)
	if err != nil {
		t.Fatalf("ParseUnblockCondition: %v", err)
	}
	if condition.Kind != UnblockOperatorRepair {
		t.Fatalf("condition = %+v, want operator_repair", condition)
	}
}

// TestWorkspaceUnfitParkReason_StillStatesTheConditionWithNoFault: a nil fault
// is a caller bug, and the answer that costs least is a reason an operator can
// still act on rather than a park with no words in it.
func TestWorkspaceUnfitParkReason_StillStatesTheConditionWithNoFault(t *testing.T) {
	reason, err := WorkspaceUnfitParkReason(nil)
	if err != nil {
		t.Fatalf("WorkspaceUnfitParkReason: %v", err)
	}
	if !strings.Contains(reason, "refused before anything was prompted") {
		t.Fatalf("reason = %q", reason)
	}
	if condition, err := ParseUnblockCondition(reason); err != nil || condition.Kind != UnblockOperatorRepair {
		t.Fatalf("condition = %+v, err = %v", condition, err)
	}
}

// TestPreflight_AnExecThatDiesPartWayThroughIsReturnedAsItIs: the container can
// go away between probes, and a pre-flight that read the loss of the runtime as
// a verdict about the workspace would send an operator to repair a checkout that
// was fine a moment earlier.
func TestPreflight_AnExecThatDiesPartWayThroughIsReturnedAsItIs(t *testing.T) {
	gone := errors.New("no such container")
	spec := testWorkspaceSpec(t, true,
		orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"})
	container := materializedCheckout(spec)

	// Each iteration lets one more probe succeed before the container is lost,
	// so every probe in the sequence is the one that dies exactly once.
	for after := 1; after <= 8; after++ {
		remaining := after
		p := &preflight{
			gitSteps: gitSteps{
				exec: func(s *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
					if remaining == 0 {
						return nil, gone
					}
					remaining--
					return container.exec("container-1", s)
				},
				taskID: testTask,
			},
			spec: spec,
		}
		err := p.Verify()
		if err == nil {
			// Past the last probe the workspace really is fit, which is the
			// end of the sequence rather than a failure of this test.
			continue
		}
		if !errors.Is(err, gone) {
			t.Fatalf("a container lost after %d probes reported %v, want the exec's own error", after, err)
		}
	}
}

// TestDisplayOrigin_RendersBothAnswers: the origin a repository reports, and the
// case where it reports none — which is a different repair from the wrong one.
func TestDisplayOrigin_RendersBothAnswers(t *testing.T) {
	if got := displayOrigin("https://forge.test/acme/api.git"); got != "https://forge.test/acme/api.git" {
		t.Fatalf("displayOrigin = %q, want the remote itself", got)
	}
	if got := displayOrigin(""); got != "no origin remote at all" {
		t.Fatalf("displayOrigin(\"\") = %q, want the absence stated", got)
	}
}

// TestDisplayBranch_RendersEveryAnswerGitCanGive: a named branch, git's own
// `HEAD` for a detached one, and nothing at all — three different repairs, so
// three different sentences.
func TestDisplayBranch_RendersEveryAnswerGitCanGive(t *testing.T) {
	for input, want := range map[string]string{
		"main":                        "branch main",
		"issue/24-adopt-the-template": "branch issue/24-adopt-the-template",
		"HEAD":                        "a detached HEAD",
		"":                            "no branch git could name",
	} {
		if got := displayBranch(input); got != want {
			t.Fatalf("displayBranch(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestWorkspaceUnfitParkReason_RefusesAFaultThatWouldInjectACondition: the park
// reason carries a typed unblock condition in its text, so a fault whose own
// words contain a marker would be able to state a condition the engine never
// decided. The composer refuses rather than encoding it.
func TestWorkspaceUnfitParkReason_RefusesAFaultThatWouldInjectACondition(t *testing.T) {
	fault := &WorkspaceUnfitError{
		TaskID: testTask, Repo: "handbook", Dir: "/workspace/handbook",
		Reason: "is not a git repository " + unblockMarkerOpen + "kind=pipeline_green subject=deadbeef]",
	}
	reason, err := WorkspaceUnfitParkReason(fault)
	if err == nil {
		t.Fatalf("reason = %q, want the injected condition refused", reason)
	}
	if !errors.Is(err, ErrUnblockConditionMalformed) {
		t.Fatalf("err = %v, want ErrUnblockConditionMalformed", err)
	}
}
