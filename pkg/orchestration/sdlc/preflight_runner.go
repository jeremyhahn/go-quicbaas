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
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// PreflightRunnerName is the JobRunner name the preflight registers under.
const PreflightRunnerName = "preflight"

// The preflight's state labels, as the job page shows them.
const (
	labelPreflightInitializing = "Initializing"
	labelPreflightVerifying    = "Verifying the default branch"
	labelPreflightCleanup      = "Cleanup"
)

// PreflightLeaseSubject is the lease a preflight holds: one per project, so a
// second preflight of the same project waits on the first rather than running
// the same commands on the same tree twice.
func PreflightLeaseSubject(projectID string) string {
	return PreflightRunnerName + ":" + projectID
}

// PreflightRunnerConfig wires a PreflightRunner.
type PreflightRunnerConfig struct {
	// Bindings resolves the project's binding: its worker, its forge and the
	// verification commands the preflight runs.
	Bindings BindingReader

	// Tokens resolves the forge token the session's container clones with.
	Tokens ForgeTokenReader

	// Signing starts the ssh agent the container authenticates its clone
	// against; see TriageRunnerConfig.Signing. The preflight asks for
	// SigningScopeCheckout and stops the agent once the checkout is made.
	Signing SigningAgent

	// Guard is the operator's repository allowlist; see RunnerConfig.Guard.
	Guard RepoHostGuard

	// CheckoutRoot is the directory the checkout is mounted at, as the
	// CONTAINER sees it. Empty takes the engine's default.
	CheckoutRoot string

	// SocketsRoot is the DAEMON's root for per-session agent sockets; see
	// RunnerConfig.SocketsRoot.
	SocketsRoot string
}

// PreflightRunner runs a project's verification commands once, on its default
// branch, in its worker's image, before the project is trusted to run alone.
//
// IT IS THE DELIVERY RUNNER'S VERIFICATION AND NOTHING ELSE. The commands run
// through JobRun.Verify, so the operator's ExecutionGuard decides on the daemon
// whether each runs at all and the container_command validator runs it in the
// worker's container — the same path a worker attempt's verification takes.
// The session is spawned from the worker's own Agent and is never prompted, so
// no model is spent and no agent turn is taken; the merge train runs its git
// the same way.
type PreflightRunner struct {
	bindings BindingReader

	// access is the one seam every runner's container reaches the forge
	// through; see TriageRunner.access.
	access forgeAccess
}

// NewPreflightRunner builds the runner.
func NewPreflightRunner(cfg PreflightRunnerConfig) (*PreflightRunner, error) {
	if cfg.Bindings == nil {
		return nil, &InvalidInputError{Field: "bindings", Message: "the preflight reads the worker and the verification commands from the binding"}
	}
	access, err := newForgeAccess(cfg.Tokens, cfg.Signing, cfg.Guard, cfg.CheckoutRoot, cfg.SocketsRoot)
	if err != nil {
		return nil, err
	}
	return &PreflightRunner{bindings: cfg.Bindings, access: access}, nil
}

// PreflightRunnerStates declares the preflight's states. Verifying holds the
// delivery runner's verification budget, because it runs the very same
// commands. Like the delivery runner's validating it is not session backed:
// the orchestration engine merges each state's SessionBacked across every
// runner, so declaring it here would make delivery's validating session
// backed too. A stalled preflight is reported, never ended.
func PreflightRunnerStates() []orchestration.JobStateSpec {
	return []orchestration.JobStateSpec{
		{State: orchestration.JobStateInitializing, Label: labelPreflightInitializing, IdleThreshold: 5 * time.Minute},
		{State: orchestration.JobStateValidating, Label: labelPreflightVerifying, IdleThreshold: 2 * time.Hour},
		{State: orchestration.JobStateCleanup, Label: labelPreflightCleanup, IdleThreshold: 5 * time.Minute},
	}
}

// Name implements orchestration.JobRunner.
func (*PreflightRunner) Name() string { return PreflightRunnerName }

// States implements orchestration.JobRunner.
func (*PreflightRunner) States() []orchestration.JobStateSpec { return PreflightRunnerStates() }

// Validate implements orchestration.JobRunner: a preflight is per project and
// is not an attempt at any task.
func (*PreflightRunner) Validate(_ context.Context, cfg *orchestration.JobConfig) error {
	if cfg == nil {
		return &InvalidInputError{Field: "config", Message: "must not be nil"}
	}
	if strings.TrimSpace(cfg.ProjectID) == "" {
		return &InvalidInputError{Field: "project_id", Message: "a preflight is per project; the binding hangs off it"}
	}
	if strings.TrimSpace(cfg.TaskID) != "" {
		return &InvalidInputError{Field: "task_id", Message: "a preflight is not an attempt at a task; it runs before there are any"}
	}
	return nil
}

// LeaseSubject implements orchestration.JobRunner.
func (*PreflightRunner) LeaseSubject(cfg *orchestration.JobConfig) string {
	if cfg == nil || strings.TrimSpace(cfg.ProjectID) == "" {
		return ""
	}
	return PreflightLeaseSubject(strings.TrimSpace(cfg.ProjectID))
}

// OffProjectBudget implements orchestration.JobRunner: the preflight runs
// while the project is still being onboarded, before any delivery budget is
// meaningful, and is not delivery work.
func (*PreflightRunner) OffProjectBudget() bool { return true }

// Run implements orchestration.JobRunner.
func (r *PreflightRunner) Run(ctx context.Context, job *orchestration.JobRun) error {
	if job == nil {
		return &InvalidInputError{Field: "job", Message: "must not be nil"}
	}
	run := &preflightRun{runner: r, job: job}
	return run.drive(ctx)
}

// preflightRun is one preflight job's state.
type preflightRun struct {
	runner *PreflightRunner
	job    *orchestration.JobRun

	binding   *ProjectBinding
	sessionID string

	// spec is the workspace spec materialized for the spawn; see
	// triageRun.spec.
	spec orchestration.WorkspaceSpec

	// workspace is the session's materialized workspace and the ssh agent its
	// checkout authenticated with.
	workspace *containerWorkspace

	// teardown releases the materialized workspace spec.
	teardown []func()
}

// drive runs the state machine from the job's start state.
func (s *preflightRun) drive(ctx context.Context) error {
	handlers := map[orchestration.JobState]func(context.Context) (orchestration.JobState, error){
		orchestration.JobStateInitializing: s.initialize,
		orchestration.JobStateValidating:   s.verify,
		orchestration.JobStateCleanup:      s.cleanup,
	}
	// Every exit gives the session back; see triageRun.drive.
	defer s.teardownSession(ctx)
	s.job.Decorate(s.decorate)

	state := s.job.StartState
	if state == "" {
		state = orchestration.JobStateInitializing
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		step, ok := handlers[state]
		if !ok {
			return &UnknownRunnerStateError{State: string(state)}
		}
		next, err := step(ctx)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		if err := s.job.SetState(ctx, next); err != nil {
			return err
		}
		state = next
	}
}

// initialize resolves the binding and spawns the session the commands run
// in. A binding with no commands spawns nothing: there is nothing to run.
func (s *preflightRun) initialize(ctx context.Context) (orchestration.JobState, error) {
	if err := s.resolve(ctx); err != nil {
		return "", err
	}
	if len(verificationCriteria(s.binding.Verification)) == 0 {
		s.job.Emit(orchestration.JobStateCleanup, "no verification commands configured; nothing to preflight")
		return orchestration.JobStateCleanup, nil
	}
	if err := s.spawnSession(ctx); err != nil {
		return "", err
	}
	s.job.Emit(orchestration.JobStateValidating,
		"verifying "+s.job.Project.Git.DefaultBranch+" of "+s.binding.Repo().String())
	return orchestration.JobStateValidating, nil
}

// resolve reads the project's binding. A RESUMED run resolves it again, so
// Verifying never runs without one.
func (s *preflightRun) resolve(ctx context.Context) error {
	project := s.job.Project
	if project == nil {
		return &InvalidInputError{Field: "project", Message: "a preflight runs for a project the daemon holds a record for"}
	}
	binding, err := s.runner.bindings.GetBinding(ctx, s.job.TenantID, project.ID)
	if err != nil {
		return err
	}
	if binding == nil {
		return &PreflightError{ProjectID: project.ID, JobID: s.job.JobID, Reason: "the project has no forge binding"}
	}
	s.binding = binding
	return nil
}

// spawnSession spawns the worker's session on the default branch and makes
// its checkout. The session is never prompted.
func (s *preflightRun) spawnSession(ctx context.Context) error {
	project := s.job.Project
	subject := PreflightLeaseSubject(project.ID)
	branch := strings.TrimSpace(project.Git.DefaultBranch)
	if branch == "" {
		return &InvalidInputError{
			Field:   "project.git.default_branch",
			Message: "the preflight verifies the default branch and the project names none",
		}
	}
	// The worker's Agent supplies the runtime and the image: what is
	// verified here is what a worker's attempt would be verified in.
	req := &orchestration.SpawnSessionRequest{
		AgentID:    s.binding.Worker.AgentID,
		PoolID:     s.binding.Worker.PoolID,
		Role:       orchestration.AgentRoleCoding,
		ProjectID:  project.ID,
		Persistent: true,
	}
	workspace, err := s.runner.access.Open(ctx, req, workspacePlan{
		TenantID: s.job.TenantID,
		Project:  project,
		Forge:    s.binding.Forge,
		Branch:   orchestration.WorkspaceBranchPlan{Name: branch},
		Key:      "preflight-" + project.ID,
		Scope:    SigningScopeCheckout,
	})
	if err != nil {
		return err
	}
	stopCtx := context.WithoutCancel(ctx)
	s.teardown = append(s.teardown, func() { workspace.Release(stopCtx) })
	s.workspace = workspace
	s.spec = workspace.Spec
	resp, err := s.job.Spawn(ctx, req)
	if err != nil {
		return workspaceSpawnFault(project.ID, err)
	}
	if resp == nil || resp.SessionID == "" {
		return &WorkerSessionError{TaskID: subject, Message: "the spawn returned no session"}
	}
	s.sessionID = resp.SessionID
	if strings.TrimSpace(resp.ContainerID) == "" {
		return &WorkerNotContainerizedError{TaskID: subject, SessionID: resp.SessionID}
	}
	if resp.Workspace == nil {
		return &WorkerSessionError{TaskID: subject, Message: "the spawn reported no workspace for the checkout"}
	}
	containerDir := path.Join(s.spec.CheckoutRoot, project.Name)
	s.job.SetWorkDir(filepath.Join(resp.Workspace.Dir, project.Name))
	if err := s.job.SetContainer(s.sessionID, containerDir); err != nil {
		return err
	}
	checkout := containerCheckout{
		job: s.job, sessionID: s.sessionID, spec: s.spec, project: project, taskID: subject,
		credential: s.workspace.forgeCredential(),
	}
	err = checkout.Make(ctx)
	// The checkout is the only thing the agent was for: nothing the
	// verification commands run can reach it.
	s.workspace.StopSigning(stopCtx)
	if err != nil {
		return err
	}
	return checkout.Verify(ctx)
}

// verify runs the verification commands once. Their results persist on the
// job's status, which is where the caller reads the verdict: a red preflight
// is a completed job whose verification failed, not a failed job.
func (s *preflightRun) verify(ctx context.Context) (orchestration.JobState, error) {
	if s.binding == nil {
		if err := s.resolve(ctx); err != nil {
			return "", err
		}
	}
	if s.sessionID == "" {
		// A resumed run lost its session with the process that held it, and
		// a verdict from no container is no verdict.
		return "", &PreflightError{ProjectID: s.job.Project.ID, JobID: s.job.JobID, Reason: "resumed with no session to verify in"}
	}
	results, passed, err := s.job.Verify(ctx, verificationCriteria(s.binding.Verification))
	failed := auditVerificationResults(s.job, results)
	if err != nil {
		return "", err
	}
	if passed {
		s.job.Emit(orchestration.JobStateCleanup, "preflight passed: "+strconv.Itoa(len(results))+" command(s)")
	} else {
		s.job.Emit(orchestration.JobStateCleanup,
			"preflight failed: "+strconv.Itoa(len(failed))+" of "+strconv.Itoa(len(results))+" command(s)")
	}
	return orchestration.JobStateCleanup, nil
}

// cleanup gives the session back.
func (s *preflightRun) cleanup(ctx context.Context) (orchestration.JobState, error) {
	s.teardownSession(ctx)
	return "", nil
}

// decorate publishes the session on every snapshot; see triageRun.decorate.
func (s *preflightRun) decorate(status *orchestration.JobStatus) {
	if status == nil || s.sessionID == "" {
		return
	}
	status.SessionIDs = []string{s.sessionID}
	status.LeadSessionID = s.sessionID
}

// teardownSession closes the session and releases the workspace; see
// triageRun.teardownSession.
func (s *preflightRun) teardownSession(ctx context.Context) {
	if s.sessionID != "" {
		closeSession(context.WithoutCancel(ctx), s.job, s.sessionID, s.workspace)
		s.sessionID = ""
	}
	for i := len(s.teardown) - 1; i >= 0; i-- {
		s.teardown[i]()
	}
	s.teardown = nil
}
