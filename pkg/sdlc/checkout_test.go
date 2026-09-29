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
	"context"
	"errors"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The checkout the container makes
// =============================================================================
//
// Every assertion here is about WHERE the git ran and WHAT it carried. The
// commands go through the fake container, which is the only path an exec has,
// so a checkout that found some other way to clone — a daemon-side git, an
// image entrypoint the spawn never runs — fails these tests rather than
// passing them quietly.

// testCloner builds a cloner over the fake container, with the project's own
// repository and the branch plan a strategy would have produced.
func testCloner(container *fakeContainer, spec orchestration.WorkspaceSpec) *cloner {
	return &cloner{
		gitSteps: gitSteps{
			exec: func(s *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
				return container.exec("container-1", s)
			},
			taskID: testTask,
		},
		project: testOrchProject(),
		spec:    spec,
	}
}

// testWorkspaceSpec is the spec the runner materializes for one attempt.
func testWorkspaceSpec(t *testing.T, create bool, contextRepos ...orchestration.ContextRepo) orchestration.WorkspaceSpec {
	t.Helper()
	project := testOrchProject()
	project.ContextRepos = contextRepos
	plan := orchestration.WorkspaceBranchPlan{Name: "issue/25-fix-the-thing", Base: "main", Create: create}
	spec, err := orchestration.NewWorkspaceSpec(project, plan)
	if err != nil {
		t.Fatalf("NewWorkspaceSpec: %v", err)
	}
	spec.CheckoutRoot = orchestration.WorkspaceCheckoutRoot
	return spec
}

// TestCloner_ClonesTheProjectIntoTheCheckoutRootInsideTheContainer: the whole
// point. The project lands at <checkout_root>/<project name>, the clone runs
// in the checkout ROOT — the container's own bind mount — and the branch is
// cut from its base.
func TestCloner_ClonesTheProjectIntoTheCheckoutRootInsideTheContainer(t *testing.T) {
	container := &fakeContainer{}
	c := testCloner(container, testWorkspaceSpec(t, true))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	clones := container.clonesMade()
	if len(clones) != 1 {
		t.Fatalf("clones = %+v, want exactly the project", clones)
	}
	wantDir := path.Join(orchestration.WorkspaceCheckoutRoot, "api")
	if clones[0].Dir != wantDir {
		t.Errorf("cloned into %q, want %q", clones[0].Dir, wantDir)
	}
	if clones[0].URL != "https://forge.test/acme/api.git" {
		t.Errorf("cloned %q, want the project's repository", clones[0].URL)
	}
	for _, dir := range container.dirsUsed() {
		if dir != orchestration.WorkspaceCheckoutRoot && dir != wantDir {
			t.Errorf("a command ran in %q, which is neither the checkout root nor the project checkout", dir)
		}
	}
	checkouts := container.checkoutsRun()
	if len(checkouts) != 1 {
		t.Fatalf("checkouts = %+v, want one", checkouts)
	}
	if got := strings.Join(checkouts[0].Args, " "); got != "-B issue/25-fix-the-thing origin/main" {
		t.Errorf("checkout args = %q, want the branch cut from its base", got)
	}
}

// TestCloner_ContinuesTheBranchAPriorAttemptPushed: a branch that is not being
// created is FETCHED and continued, which is what keeps one pull request and
// its reviews across a resumed attempt.
func TestCloner_ContinuesTheBranchAPriorAttemptPushed(t *testing.T) {
	container := &fakeContainer{}
	c := testCloner(container, testWorkspaceSpec(t, false))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	fetches := container.fetchesRun()
	if len(fetches) != 1 {
		t.Fatalf("fetches = %+v, want one", fetches)
	}
	if got := strings.Join(fetches[0].Refs, " "); got != "main issue/25-fix-the-thing" {
		t.Errorf("fetched %q, want the base and the branch the prior attempt pushed", got)
	}
	checkouts := container.checkoutsRun()
	if len(checkouts) != 1 || checkouts[0].Args[len(checkouts[0].Args)-1] != "origin/issue/25-fix-the-thing" {
		t.Fatalf("checkouts = %+v, want the branch continued from its remote", checkouts)
	}
}

// TestCloner_SkipsTheCloneWhenTheCheckoutIsAlreadyThere: a resumed attempt is
// bind-mounted through the directory its predecessor's container cloned into.
// Cloning again would fail on a non-empty directory, so the step probes first
// and only refreshes the branch.
func TestCloner_SkipsTheCloneWhenTheCheckoutIsAlreadyThere(t *testing.T) {
	container := &fakeContainer{repos: map[string]bool{path.Join(orchestration.WorkspaceCheckoutRoot, "api"): true}}
	c := testCloner(container, testWorkspaceSpec(t, false))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if clones := container.clonesMade(); len(clones) != 0 {
		t.Fatalf("cloned over an existing checkout: %+v", clones)
	}
	if fetches := container.fetchesRun(); len(fetches) != 1 {
		t.Fatalf("fetches = %+v, want the branch refreshed on the checkout that was already there", fetches)
	}
}

// TestCloner_ClonesEveryContextRepositoryBesideTheProject: the handbook sits
// at /workspace/handbook while the project sits at /workspace/api — beside it,
// never inside it, so nothing the worker reads can be committed by accident.
func TestCloner_ClonesEveryContextRepositoryBesideTheProject(t *testing.T) {
	container := &fakeContainer{}
	spec := testWorkspaceSpec(t, true,
		orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"},
		orchestration.ContextRepo{Name: "specs", RepoURL: "https://forge.test/acme/specs.git", Ref: "v1.2.0"},
	)
	c := testCloner(container, spec)
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	clones := container.clonesMade()
	if len(clones) != 3 {
		t.Fatalf("clones = %+v, want the project and both context repositories", clones)
	}
	for i, want := range []string{"api", "handbook", "specs"} {
		if got := clones[i].Dir; got != path.Join(orchestration.WorkspaceCheckoutRoot, want) {
			t.Errorf("clone %d landed in %q, want it beside the project as %q", i, got, want)
		}
	}
	// Each context repository is put on the ref the project pinned it at.
	checkouts := container.checkoutsRun()
	if len(checkouts) != 3 {
		t.Fatalf("checkouts = %+v, want the branch plus one per context repository", checkouts)
	}
	if got := strings.Join(checkouts[1].Args, " "); got != "main" {
		t.Errorf("handbook checked out %q, want its pinned ref", got)
	}
	if got := strings.Join(checkouts[2].Args, " "); got != "v1.2.0" {
		t.Errorf("specs checked out %q, want its pinned tag", got)
	}
}

// TestCloner_TheCloneAuthenticatesThroughTheMountedTokenAndCarriesNoSecret:
// the credential helper names the file the spawn mounted read-only. What
// reaches argv is the helper's SHAPE; the value is read inside the container
// at the moment git asks for it.
func TestCloner_TheCloneAuthenticatesThroughTheMountedTokenAndCarriesNoSecret(t *testing.T) {
	container := &fakeContainer{}
	c := testCloner(container, testWorkspaceSpec(t, true))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	tokenFile := path.Join(orchestration.WorkspaceSpecDir, orchestration.WorkspaceForgeTokenFile)
	var authenticated int
	for _, cmd := range container.commandsRun() {
		if !strings.Contains(cmd, "credential.helper=") {
			continue
		}
		authenticated++
		if !strings.Contains(cmd, tokenFile) {
			t.Errorf("the helper does not name the mounted token file: %q", cmd)
		}
	}
	if authenticated == 0 {
		t.Fatal("no command offered the forge token; a private repository could not be cloned")
	}
	// The two commands that talk to the remote are the ones that carry it.
	if len(container.clonesMade()[0].Config) == 0 {
		t.Error("the clone was run without a credential helper")
	}
	if len(container.fetchesRun()[0].Config) == 0 {
		t.Error("the fetch was run without a credential helper")
	}
}

// TestCloner_WritesTheProjectCommitIdentityIntoTheCheckout: a git the WORKER
// runs commits under the project's identity, not the image's.
func TestCloner_WritesTheProjectCommitIdentityIntoTheCheckout(t *testing.T) {
	container := &fakeContainer{}
	c := testCloner(container, testWorkspaceSpec(t, true))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	configs := container.configsWritten()
	if len(configs) != 2 {
		t.Fatalf("configs = %+v, want the author's name and address", configs)
	}
	if configs[0].Key != "user.name" || configs[0].Value != "agent-api" {
		t.Errorf("config[0] = %+v, want the project's author name", configs[0])
	}
	if configs[1].Key != "user.email" || configs[1].Value != "agent-api@example.test" {
		t.Errorf("config[1] = %+v, want the project's author address", configs[1])
	}
	if configs[0].Dir != path.Join(orchestration.WorkspaceCheckoutRoot, "api") {
		t.Errorf("the identity was written in %q, not the project checkout", configs[0].Dir)
	}
}

// TestCloner_ASigningAttemptMakesTheWorkersCommitsSigned is go-quicbaas#1048's
// review finding at its root: a worker that commits on its own must sign with
// the attempt's key, so the checkout writes the signing configuration beside
// the identity.
func TestCloner_ASigningAttemptMakesTheWorkersCommitsSigned(t *testing.T) {
	spec := testWorkspaceSpec(t, true)
	spec.SigningKey = "ssh-ed25519 AAAAkey"
	container := &fakeContainer{}
	c := testCloner(container, spec)
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	written := map[string]string{}
	for _, cfg := range container.configsWritten() {
		written[cfg.Key] = cfg.Value
	}
	want := map[string]string{
		"gpg.format":                 "ssh",
		"user.signingkey":            "key::ssh-ed25519 AAAAkey",
		"commit.gpgsign":             "true",
		"gpg.ssh.allowedSignersFile": path.Join(orchestration.WorkspaceSpecDir, orchestration.WorkspaceAllowedSignersFile),
	}
	for key, value := range want {
		if written[key] != value {
			t.Errorf("%s = %q, want %q", key, written[key], value)
		}
	}
}

// TestCloner_AnAttemptThatSignsNothingWritesNoSigningConfig: a reviewer or
// triage checkout has no key, and its git is not told to sign.
func TestCloner_AnAttemptThatSignsNothingWritesNoSigningConfig(t *testing.T) {
	container := &fakeContainer{}
	c := testCloner(container, testWorkspaceSpec(t, true))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	for _, cfg := range container.configsWritten() {
		if strings.HasPrefix(cfg.Key, "gpg.") || cfg.Key == "commit.gpgsign" || cfg.Key == "user.signingkey" {
			t.Fatalf("wrote %s for an attempt that signs nothing", cfg.Key)
		}
	}
}

// TestCloner_ARefusedCloneIsATypedErrorThatScrubsItsOutput: a clone git
// refused ends the attempt with CheckoutCommandError, whose STRING says what
// git said — scrubbed, because an error reaches logs and, through the module's
// surfacing path, a comment on a public issue, and bounded, because that
// string is read by a person.
//
// It used to say only how many bytes it was not showing, and go-quicbaas#751
// is what that cost: `exited 128 (102 bytes of output withheld)` withheld
// `sh: ssh: not found`, and reading it took exec'ing into a leaked container.
func TestCloner_ARefusedCloneIsATypedErrorThatScrubsItsOutput(t *testing.T) {
	container := &fakeContainer{cloneErr: errors.New("remote: authentication failed for ghp_0123456789abcdefghij\nfatal: could not read from remote repository")}
	c := testCloner(container, testWorkspaceSpec(t, true))
	err := c.Checkout()
	var refused *CheckoutCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want *CheckoutCommandError", err)
	}
	if !errors.Is(err, ErrRunner) {
		t.Error("the refusal does not unwrap to the runner sentinel")
	}
	if !strings.Contains(refused.Output, "ghp_0123456789abcdefghij") {
		t.Errorf("the output was not kept verbatim as evidence: %q", refused.Output)
	}
	if strings.Contains(err.Error(), "ghp_0123456789abcdefghij") {
		t.Errorf("a credential git echoed reached the error string: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "fatal: could not read from remote repository") {
		t.Errorf("the diagnosis is still withheld: %q", err.Error())
	}
	if refused.TaskID != testTask {
		t.Errorf("TaskID = %q, want the attempt's task", refused.TaskID)
	}
}

// TestCloner_ARefusedCheckoutStopsBeforeTheContextRepositories: a branch that
// cannot be put on the project is the attempt's failure, not something to
// carry on past.
func TestCloner_ARefusedCheckoutStopsBeforeTheContextRepositories(t *testing.T) {
	container := &fakeContainer{checkoutErr: errors.New("pathspec did not match")}
	spec := testWorkspaceSpec(t, true, orchestration.ContextRepo{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"})
	c := testCloner(container, spec)
	if err := c.Checkout(); !errors.Is(err, ErrRunner) {
		t.Fatalf("err = %v, want the runner sentinel", err)
	}
	if clones := container.clonesMade(); len(clones) != 1 {
		t.Fatalf("clones = %+v, want the checkout to have stopped at the project", clones)
	}
}

// TestCloner_AnExecThatCouldNotRunIsReturnedAsItIs: a container that is gone
// is not a git failure, and dressing it as one would hide what happened.
func TestCloner_AnExecThatCouldNotRunIsReturnedAsItIs(t *testing.T) {
	gone := errors.New("no such container")
	c := &cloner{
		gitSteps: gitSteps{
			exec:   func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) { return nil, gone },
			taskID: testTask,
		},
		project: testOrchProject(),
		spec:    testWorkspaceSpec(t, true),
	}
	if err := c.Checkout(); !errors.Is(err, gone) {
		t.Fatalf("err = %v, want the exec's own error", err)
	}
}

// TestCloner_RefusesASpecThatNamesNoCheckout: every field the git commands
// interpolate is checked before one is built, so a hand-made spec becomes a
// refusal rather than a git command with an empty operand.
func TestCloner_RefusesASpecThatNamesNoCheckout(t *testing.T) {
	base := testWorkspaceSpec(t, true)
	cases := map[string]func(s *orchestration.WorkspaceSpec){
		"no checkout root": func(s *orchestration.WorkspaceSpec) { s.CheckoutRoot = "" },
		"no project name":  func(s *orchestration.WorkspaceSpec) { s.Project.Name = "" },
		"no repository":    func(s *orchestration.WorkspaceSpec) { s.Project.RepoURL = "" },
		"no branch":        func(s *orchestration.WorkspaceSpec) { s.Branch.Name = "" },
		"no base to cut from": func(s *orchestration.WorkspaceSpec) {
			s.Branch.Create = true
			s.Branch.Base = ""
		},
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			container := &fakeContainer{}
			spec := base
			break_(&spec)
			c := testCloner(container, spec)
			if err := c.Checkout(); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("err = %v, want ErrInvalidInput", err)
			}
			if commands := container.commandsRun(); len(commands) != 0 {
				t.Fatalf("a refused spec still ran %v", commands)
			}
		})
	}
}

// TestCloner_RefusesWithoutAProject: the commit identity written into the
// checkout comes off the project record, and there is nothing to guess from.
func TestCloner_RefusesWithoutAProject(t *testing.T) {
	container := &fakeContainer{}
	c := testCloner(container, testWorkspaceSpec(t, true))
	c.project = nil
	if err := c.Checkout(); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}

// TestCloner_EvidenceRecordsEveryCommandIncludingTheOneThatFailed: the
// evidence is what a person reads when an attempt dies in its checkout, so it
// must survive the failure that produced it.
func TestCloner_EvidenceRecordsEveryCommandIncludingTheOneThatFailed(t *testing.T) {
	container := &fakeContainer{cloneErr: errors.New("remote: repository not found")}
	c := testCloner(container, testWorkspaceSpec(t, true))
	if err := c.Checkout(); err == nil {
		t.Fatal("Checkout: want a refusal")
	}
	evidence := c.Evidence()
	if !strings.Contains(evidence, "git -C api rev-parse --git-dir") {
		t.Errorf("the probe is missing from the evidence:\n%s", evidence)
	}
	if !strings.Contains(evidence, "clone") || !strings.Contains(evidence, "repository not found") {
		t.Errorf("the failed clone and what it said are missing from the evidence:\n%s", evidence)
	}
}

// TestCloner_EvidenceIsCappedPerCommand: a clone that prints a great deal is
// recorded, not transcribed — the job's audit trail is bounded.
func TestCloner_EvidenceIsCappedPerCommand(t *testing.T) {
	c := &cloner{
		gitSteps: gitSteps{
			exec: func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) {
				return &orchestration.ExecResult{Stdout: strings.Repeat("x", maxCheckoutStepOutput*2)}, nil
			},
			taskID: testTask,
		},
		project: testOrchProject(),
		spec:    testWorkspaceSpec(t, true),
	}
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	if !strings.Contains(c.Evidence(), "truncated") {
		t.Error("a command with a great deal of output was not truncated")
	}
	for _, step := range c.steps {
		if strings.Count(step.Output, "x") > maxCheckoutStepOutput {
			t.Errorf("%q kept %d bytes of output, past the cap", step.Command, strings.Count(step.Output, "x"))
		}
	}
}

// =============================================================================
// Through the runner
// =============================================================================

// TestRunner_TheInitializingStepMakesTheCheckoutInsideTheContainer is
// go-quicbaas#627: across a whole delivery the project and its context
// repositories are cloned by commands the RUNNER execs into the worker's
// container, and the daemon's own filesystem is never named.
func TestRunner_TheInitializingStepMakesTheCheckoutInsideTheContainer(t *testing.T) {
	rig := newRunnerRig(t)
	rig.contextRepos = []orchestration.ContextRepo{
		{Name: "handbook", RepoURL: "https://forge.test/acme/handbook.git", Ref: "main"},
	}
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}

	clones := rig.container.clonesMade()
	if len(clones) != 2 {
		t.Fatalf("clones = %+v, want the project and the handbook", clones)
	}
	if clones[0].Dir != path.Join(orchestration.WorkspaceCheckoutRoot, "api") ||
		clones[1].Dir != path.Join(orchestration.WorkspaceCheckoutRoot, "handbook") {
		t.Fatalf("the checkout is not laid out side by side: %+v", clones)
	}
	// NOTHING RAN ON THE DAEMON. Every command named a container path, and
	// the project's workspace root — the host side of the bind mount — is
	// named by none of them.
	for _, e := range rig.container.execs {
		if !strings.HasPrefix(e.WorkDir, orchestration.WorkspaceCheckoutRoot) {
			t.Fatalf("a command ran in %q, which is outside the container's checkout root", e.WorkDir)
		}
		if strings.Contains(strings.Join(e.Cmd, " "), rig.root) {
			t.Fatalf("a command named the daemon's host path: %v", e.Cmd)
		}
	}
}

// TestRunner_TheCheckoutEvidenceIsRecordedOnTheJob: what the clone did is on
// the job, because a job that died in its checkout is otherwise a job that
// died for no stated reason.
func TestRunner_TheCheckoutEvidenceIsRecordedOnTheJob(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	if err := rig.runner.Run(context.Background(), rig.job(t)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	recorded := rig.auditDetails(auditCheckout)
	if len(recorded) != 1 {
		t.Fatalf("audit events for the checkout = %d, want one", len(recorded))
	}
	evidence := recorded[0]
	if !strings.Contains(evidence, "clone") || !strings.Contains(evidence, "https://forge.test/acme/api.git") {
		t.Errorf("the evidence does not say what was cloned:\n%s", evidence)
	}
}

// TestRunner_ACheckoutThatFailsEndsTheAttemptWithItsEvidence: the worker is
// never prompted against a checkout that was never made, and the reason is on
// the job.
//
// AND IT IS THE OPERATOR'S FAULT, NOT THE WORK'S. A clone that could not reach
// the forge — or of a context repository the project declared and the forge
// does not serve — happens before anything is prompted, so it is parked with
// the workspace fault class rather than charged against the task
// (go-quicbaas#908).
func TestRunner_ACheckoutThatFailsEndsTheAttemptWithItsEvidence(t *testing.T) {
	rig := newRunnerRig(t)
	rig.container.cloneErr = errors.New("remote: repository not found")

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)
	// The run ends as every park does — an escalation the engine reads the
	// ATTRIBUTION off, rather than off the error text.
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("err = %v, want the run to end parked", err)
	}
	parked := job.Parked()
	if parked == nil || parked.FaultClass != orchestration.FaultWorkspaceUnfit {
		t.Fatalf("parked = %+v, want the workspace fault class", parked)
	}
	if !strings.Contains(parked.Message, "repository not found") {
		t.Fatalf("the park lost what git said: %q", parked.Message)
	}
	if len(rig.agents.promptTexts()) != 0 {
		t.Error("the worker was prompted against a checkout that was never made")
	}
	var sawEvidence bool
	for _, details := range rig.auditDetails(auditCheckout) {
		if strings.Contains(details, "repository not found") {
			sawEvidence = true
		}
	}
	if !sawEvidence {
		t.Error("the failed checkout left no evidence on the job")
	}
}

// TestRunner_TheJobsWorkDirIsTheProjectCheckout: the mount holds the project
// AND every context repository beside it, so a job that recorded the mount
// root would point the engine's command validator — and every person reading
// the job — one directory above the repository the attempt is about.
func TestRunner_TheJobsWorkDirIsTheProjectCheckout(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")

	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := filepath.Join(rig.root, testTenant, testProject, "workspace", testTask+"-a1", "api")
	if got := job.WorkDir(); got != want {
		t.Errorf("job work dir = %q, want the project checkout %q", got, want)
	}
}

// =============================================================================
// A pinned checkout — the tree that was read is the tree that was named
// =============================================================================
//
// A caller pins because it intends to state afterwards which tree it acted
// on. These are the three things that promise is made of: the reset names
// the commit and not the branch, HEAD is read back rather than assumed, and a
// commit the remote no longer has stops the attempt instead of substituting
// whatever replaced it.

// pinnedSpec is the spec of a checkout pinned to one commit on an existing
// branch, which is the shape the review runner builds.
func pinnedSpec(t *testing.T, commit string) orchestration.WorkspaceSpec {
	t.Helper()
	plan := orchestration.WorkspaceBranchPlan{Name: "issue/25-fix-the-thing", Base: "main", Commit: commit}
	spec, err := orchestration.NewWorkspaceSpec(testOrchProject(), plan)
	if err != nil {
		t.Fatalf("NewWorkspaceSpec: %v", err)
	}
	spec.CheckoutRoot = orchestration.WorkspaceCheckoutRoot
	return spec
}

func TestCloner_APinnedPlanResetsOntoTheCommitAndNotTheMovedBranch(t *testing.T) {
	const captured, pushedSince = "cafebabe11", "deadbeef22"

	// The author pushed between the caller reading the head and the
	// container cloning: origin/<branch> is now a commit the caller never
	// saw, and the captured one is still reachable behind it.
	container := &fakeContainer{
		remoteBranches: map[string]string{"issue/25-fix-the-thing": pushedSince},
		knownCommits:   map[string]bool{captured: true, pushedSince: true},
	}
	c := testCloner(container, pinnedSpec(t, captured))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	// THE OBSERVABLE PROPERTY: the checkout is on the commit that was
	// pinned, not on wherever the branch has since moved to.
	if head := container.currentHead(); head != captured {
		t.Fatalf("HEAD = %q, want the pinned %q; the checkout followed the moving branch", head, captured)
	}

	// And it got there by naming the commit. A reset onto origin/<branch>
	// that happened to land right today lands wrong tomorrow.
	checkouts := container.checkoutsRun()
	if len(checkouts) != 1 {
		t.Fatalf("checkouts = %+v, want exactly one", checkouts)
	}
	if got := checkouts[0].Args[len(checkouts[0].Args)-1]; got != captured {
		t.Fatalf("reset onto %q, want the pinned commit %q", got, captured)
	}
	// The branch is still FETCHED — that is how the object arrives.
	fetches := container.fetchesRun()
	if len(fetches) != 1 || !containsString(fetches[0].Refs, "issue/25-fix-the-thing") {
		t.Fatalf("fetches = %+v, want the head branch fetched", fetches)
	}
}

func TestCloner_APinnedPlanRefusesACommitTheRemoteNoLongerHas(t *testing.T) {
	const captured, forcePushed = "cafebabe11", "deadbeef22"

	// A FORCE-PUSH: the branch moved and the captured commit went with it.
	container := &fakeContainer{
		remoteBranches: map[string]string{"issue/25-fix-the-thing": forcePushed},
		knownCommits:   map[string]bool{forcePushed: true},
	}
	c := testCloner(container, pinnedSpec(t, captured))
	err := c.Checkout()
	if err == nil {
		t.Fatal("a checkout pinned to a commit the remote dropped succeeded; it reviewed some other tree")
	}
	var refused *CheckoutCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %T %v, want the refused git command", err, err)
	}
	if head := container.currentHead(); head == forcePushed {
		t.Fatalf("HEAD landed on %q, the commit that replaced the pinned one", head)
	}
}

func TestCloner_APinnedPlanRefusesAHeadThatIsNotTheCommit(t *testing.T) {
	const captured = "cafebabe11"

	// The reset REPORTED success and HEAD is somewhere else — a ref that
	// resolved to something unexpected, a hook, the wrong repository. The
	// pin is only worth anything if the fact is read back.
	container := &fakeContainer{head: "9999999999", knownCommits: map[string]bool{}}
	container.knownCommits[captured] = true
	// resolveStart answers the commit, but the container's HEAD does not
	// follow it: the plain "checkout" form is what this models.
	c := testCloner(container, pinnedSpec(t, captured))
	c.exec = func(spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
		if len(spec.Cmd) > 1 && spec.Cmd[1] == "checkout" {
			return &orchestration.ExecResult{}, nil
		}
		return container.exec("container-1", spec)
	}

	err := c.Checkout()
	var mismatch *CheckoutHeadMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %T %v, want a head mismatch", err, err)
	}
	if mismatch.Want != captured || mismatch.Got != "9999999999" {
		t.Fatalf("mismatch = %+v, want %s over %s", mismatch, captured, "9999999999")
	}
	if !errors.Is(err, ErrRunner) {
		t.Fatal("a head mismatch does not unwrap to the runner sentinel")
	}
}

func TestCloner_APinnedPlanRefusesAHeadItCannotRead(t *testing.T) {
	const captured = "cafebabe11"

	// A checkout whose HEAD cannot be read is not a checkout that is on the
	// pinned commit. Treating an unreadable answer as a pass would defeat
	// the verification on exactly the repository that is in trouble.
	container := &fakeContainer{headErr: errors.New("fatal: not a git repository")}
	c := testCloner(container, pinnedSpec(t, captured))

	err := c.Checkout()
	var refused *CheckoutCommandError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %T %v, want the refused rev-parse", err, err)
	}
	if !strings.Contains(refused.Command, "rev-parse HEAD") {
		t.Fatalf("the refusal names %q, want the head read", refused.Command)
	}
}

func TestCloner_AnUnpinnedPlanVerifiesNoHeadAtAll(t *testing.T) {
	// A worker continuing its own branch was promised no particular commit,
	// so reading HEAD back would be asserting something nobody stated — and
	// on a fresh branch there is nothing to read.
	container := &fakeContainer{}
	c := testCloner(container, testWorkspaceSpec(t, false))
	if err := c.Checkout(); err != nil {
		t.Fatalf("Checkout: %v", err)
	}
	for _, cmd := range container.commandsRun() {
		if strings.Contains(cmd, "rev-parse HEAD") {
			t.Fatalf("an unpinned checkout read HEAD back: %q", cmd)
		}
	}
}

func TestNewWorkspaceSpec_ABranchThatDoesNotExistYetCannotBePinned(t *testing.T) {
	plan := orchestration.WorkspaceBranchPlan{Name: "issue/25-fix", Base: "main", Create: true, Commit: "cafebabe11"}
	_, err := orchestration.NewWorkspaceSpec(testOrchProject(), plan)
	var invalid *orchestration.WorkspaceSpecInvalidError
	if !errors.As(err, &invalid) || invalid.Field != "branch.commit" {
		t.Fatalf("err = %T %v, want branch.commit refused", err, err)
	}
}

// containsString reports whether list holds want.
func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// =============================================================================
// A session whose container is gone (go-quicbaas#932)
// =============================================================================

// goneSteps builds the shared git seam over an exec that the runtime refuses
// with err, for the session and container a gone-container error must name.
func goneSteps(err error) gitSteps {
	return gitSteps{
		exec:      func(*orchestration.ExecSpec) (*orchestration.ExecResult, error) { return nil, err },
		taskID:    testTask,
		sessionID: "session-7",
	}
}

// assertContainerGone asserts err is the typed gone-container error naming the
// session and the container and carrying the runtime's own message — and is
// NOT a git command that ran and failed.
func assertContainerGone(t *testing.T, err error, wantContainer, wantMessage string) {
	t.Helper()
	var gone *SessionContainerGoneError
	if !errors.As(err, &gone) {
		t.Fatalf("err = %T (%v), want *SessionContainerGoneError", err, err)
	}
	var command *CheckoutCommandError
	if errors.As(err, &command) {
		t.Fatalf("a container that ran nothing was reported as a git command: %v", err)
	}
	if gone.SessionID != "session-7" || gone.ContainerID != wantContainer || gone.TaskID != testTask {
		t.Errorf("gone = %+v, want session-7, container %s, task %s", gone, wantContainer, testTask)
	}
	for _, want := range []string{"session-7", wantContainer, wantMessage} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Error() = %q, want it to name %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "git clone") || strings.Contains(err.Error(), "exited") {
		t.Errorf("Error() = %q names a git command that never ran", err.Error())
	}
	if !errors.Is(err, ErrSessionContainerGone) || !errors.Is(err, ErrWorkspaceUnfit) {
		t.Error("the error does not unwrap to the gone-container and workspace sentinels")
	}
}

// TestGitSteps_AProbeOfAStoppedContainerIsTheContainerGone: the probe's
// non-zero exit is an answer, but a runtime that refused to start git is no
// answer at all. It used to read as "not a repository" and send the checkout
// on to a clone into the same dead container.
func TestGitSteps_AProbeOfAStoppedContainerIsTheContainerGone(t *testing.T) {
	message := "Error response from daemon: container 41b0eedc is not running"
	steps := goneSteps(&orchestration.ContainerNotRunningError{ContainerID: "41b0eedc", Message: message})

	result, err := steps.probe(orchestration.WorkspaceCheckoutRoot, "-C", "api", "rev-parse", "--git-dir")
	if result != nil {
		t.Errorf("result = %+v, want none for a command that never ran", result)
	}
	assertContainerGone(t, err, "41b0eedc", message)
	if !errors.Is(err, orchestration.ErrContainerNotRunning) {
		t.Error("the runtime's own error is not kept as the cause")
	}
}

// TestGitSteps_ARunAgainstAMissingContainerIsTheContainerGone: the run's
// refusal of a non-zero exit must not fire either, and a container the
// runtime cannot find at all is the same condition as one that stopped.
func TestGitSteps_ARunAgainstAMissingContainerIsTheContainerGone(t *testing.T) {
	steps := goneSteps(&orchestration.ContainerNotFoundError{ContainerID: "quicbaas-acp-56e2"})

	_, err := steps.run(orchestration.WorkspaceCheckoutRoot, "clone", "--origin", "origin", "ssh://forge/acme/api.git", "api")
	assertContainerGone(t, err, "quicbaas-acp-56e2", orchestration.ErrContainerNotFound.Error())
	if !errors.Is(err, orchestration.ErrContainerNotFound) {
		t.Error("the runtime's own error is not kept as the cause")
	}
}

// TestCloner_EvidenceSaysTheContainerWasGoneNotAGitCommand: the job's evidence
// is what a person reads, and a git command line printed for a command that
// never ran is the misreading go-quicbaas#932 is about.
func TestCloner_EvidenceSaysTheContainerWasGoneNotAGitCommand(t *testing.T) {
	container := &fakeContainer{stopped: true}
	c := testCloner(container, testWorkspaceSpec(t, true))
	c.sessionID = "session-7"

	assertContainerGone(t, c.Checkout(), "container-1", "is not running")
	evidence := c.Evidence()
	if strings.Contains(evidence, "$ git") {
		t.Errorf("the evidence prints a git command that never ran:\n%s", evidence)
	}
	for _, want := range []string{"session-7", "container-1", "is gone", "is not running"} {
		if !strings.Contains(evidence, want) {
			t.Errorf("the evidence does not say %q:\n%s", want, evidence)
		}
	}
	if clones := container.clonesMade(); len(clones) != 0 {
		t.Errorf("the checkout went on to clone into a dead container: %+v", clones)
	}
}

// TestPreflight_AStoppedContainerIsTheContainerGone: the pre-flight's probes go
// through the same seam, so a container that died between the clone and the
// proof is reported the same way.
func TestPreflight_AStoppedContainerIsTheContainerGone(t *testing.T) {
	spec := testWorkspaceSpec(t, true)
	container := materializedCheckout(spec)
	container.stopped = true
	p := testPreflight(container, spec)
	p.sessionID = "session-7"

	assertContainerGone(t, p.Verify(), "container-1", "is not running")
	if strings.Contains(p.Evidence(), "$ git") {
		t.Errorf("the evidence prints a git command that never ran:\n%s", p.Evidence())
	}
}

// TestRunner_ADeadContainerIsNamedOnTheJobNotAFailedClone: through the whole
// runner, the job's evidence line and its park say the container was gone.
// The park itself is unchanged — a workspace fault, no attempt charged —
// because nothing was prompted and no retry in that session can succeed.
func TestRunner_ADeadContainerIsNamedOnTheJobNotAFailedClone(t *testing.T) {
	rig := newRunnerRig(t)
	rig.container.stopped = true

	job := rig.job(t)
	err := rig.runner.Run(context.Background(), job)
	var escalated *EscalatedError
	if !errors.As(err, &escalated) {
		t.Fatalf("err = %v, want the run to end parked", err)
	}
	parked := job.Parked()
	if parked == nil || parked.FaultClass != orchestration.FaultWorkspaceUnfit {
		t.Fatalf("parked = %+v, want the workspace fault class", parked)
	}
	if !strings.Contains(parked.Message, "is gone") || strings.Contains(parked.Message, "git clone") {
		t.Errorf("the park does not name the dead container: %q", parked.Message)
	}
	if len(rig.agents.promptTexts()) != 0 {
		t.Error("the worker was prompted against a container that is gone")
	}
	details := rig.auditDetails(auditCheckout)
	if len(details) == 0 {
		t.Fatal("the dead container left no evidence on the job")
	}
	for _, d := range details {
		if strings.Contains(d, "$ git") {
			t.Errorf("the evidence prints a git command that never ran:\n%s", d)
		}
		if !strings.Contains(d, "is gone") {
			t.Errorf("the evidence does not say the container was gone:\n%s", d)
		}
	}
}
