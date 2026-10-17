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
	"context"
	"errors"
	"fmt"
	"runtime/debug"
)

// =============================================================================
// The job envelope
// =============================================================================
//
// launchJob IS THE ONE START PATH. A job started by a caller and a job
// resumed after a restart differ in exactly two values — the job ID, and the
// state it enters — so they are the same call with those two as arguments
// rather than two code paths that drift.
//
// The envelope owns everything that is true of every job whatever its shape:
// the identity, the records the run reads, the lease, the heartbeat, the
// terminal status, the discard, and the accounting that follows. The runner
// owns only the states in between.

// ErrJobPanic indicates a runner panicked; the job fails with it.
var ErrJobPanic = errors.New("orchestration: job runner panicked")

// JobPanicError reports a runner that panicked. The job is failed with
// FailureClassPanic: no runner reports that class, because a runner that
// panicked reported nothing.
type JobPanicError struct {
	JobID  string
	Runner string
	Value  string
	Stack  string
}

// Error implements the error interface.
func (e *JobPanicError) Error() string {
	return ErrJobPanic.Error() + ": job=" + e.JobID + " runner=" + e.Runner + ": " + e.Value
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *JobPanicError) Unwrap() error {
	return ErrJobPanic
}

// launchJob starts a job, fresh or resumed, and returns its ID. jobID is
// empty for a fresh job and the persisted one for a resumed job; resumed is
// the persisted status a resumed job continues and nil for a fresh one.
//
// What can be refused without touching a store is refused here, before the
// goroutine: an unregistered runner, a workflow nothing registered, a
// workflow naming a hook set nothing registered. The task is read later, by
// the run itself (acquireJobLease -> loadTask), because it is the lease
// subject: a task that cannot be read fails the run rather than the launch.
func (s *agentServiceImpl) launchJob(ctx context.Context, config *JobConfig, jobID string, resumed *JobStatus) (string, error) {
	if config == nil {
		return "", &AgentServiceError{
			Sentinel: ErrAgentServiceUnavailable,
			Message:  "job config must not be nil",
		}
	}

	// A job started after the drain began is work the daemon has no time
	// left to do, and the transports are still accepting requests while it
	// runs; see agentServiceImpl.Shutdown.
	if s.draining.Load() {
		return "", &ServiceShuttingDownError{Operation: "launch job"}
	}

	runnerName := ResolveJobRunnerName(config.Runner)
	runner, ok := LookupJobRunner(runnerName)
	if !ok {
		return "", &UnknownJobRunnerError{Name: runnerName}
	}
	config.Runner = runnerName

	if jobID == "" {
		minted, err := generateJobID()
		if err != nil {
			return "", &ControlLoopError{
				Sentinel: ErrControlLoopFailed,
				Message:  "failed to generate job ID: " + err.Error(),
			}
		}
		jobID = minted
	}

	if config.PollInterval == 0 {
		config.PollInterval = defaultPollInterval
	}

	tenantID := config.TenantID
	runCfg := JobRunConfig{
		JobID:       jobID,
		Runner:      runnerName,
		Config:      config,
		Resumed:     resumed,
		Agents:      s,
		Logger:      s.logger,
		CostTracker: s.costTracker,
		Leases:      s.jobLeases,
		Node:        s.node,
		LeaseTTL:    s.leaseTTL,
	}

	runCfg.Project = s.loadJobProject(ctx, config)

	// A workflow job's definition and its hook set are the envelope's to
	// resolve: a workflow naming a hook set nothing registered is refused
	// here, before any goroutine, with the typed error registration would
	// have given.
	if runnerName == JobRunnerWorkflow {
		workflow, err := s.resolveJobWorkflow(config, resumed != nil)
		if err != nil {
			return "", err
		}
		runCfg.Workflow = workflow
		if workflow != nil && workflow.Settings.Hooks != "" {
			hooks, found := LookupJobHooks(workflow.Settings.Hooks)
			if !found {
				return "", &UnknownJobHooksError{Name: workflow.Settings.Hooks}
			}
			runCfg.Hooks = hooks
			runCfg.HooksName = workflow.Settings.Hooks
		}
	}

	// The prompt set is resolved here, once, beside the runner, project and
	// workflow: the job renders from it for its whole life, and a resumed
	// job resolves afresh. A set that cannot be resolved refuses the launch.
	if kind := runnerPromptSet(runner); kind != "" {
		set, err := s.resolveJobPrompts(ctx, kind, config)
		if err != nil {
			return "", err
		}
		runCfg.Prompts = set
	}

	if s.kvStore != nil {
		// The status already carries the tenant: the run is built with it
		// and every snapshot sets it before publishing. The hook only
		// writes, so that the persisted record and the published one are
		// the same record.
		runCfg.PersistFn = func(status *JobStatus) {
			s.persistJobStatus(tenantID, status)
		}
		runCfg.StoreFn = func(status *JobStatus) error {
			return s.storeJobStatus(tenantID, status)
		}
	}

	// The job outlives the call that launched it: the caller's context
	// carries the request or recovery deadline and is cancelled when it
	// returns, and a job bound to it would fail its first lifecycle hook
	// with "context canceled". Its values — the tenant above all — are
	// kept; only the cancellation is dropped.
	jobCtx := context.WithoutCancel(ctx)
	if tenantID != "" && TenantFromContext(jobCtx) == "" {
		jobCtx = ContextWithTenant(jobCtx, tenantID)
	}
	jobCtx, cancel := context.WithCancel(jobCtx)

	runCfg.AuditFn = func(eventType, details string) {
		s.logger.Infof("orchestration: [%s] %s", eventType, details)
		s.publishEvent(jobCtx, tenantID, eventType, map[string]string{
			"details": details,
			"job_id":  jobID,
		})
	}

	run := newJobRun(runCfg)
	if run.Prompts != nil {
		run.Audit(AuditPromptsResolved, promptsResolvedDetail(run.Prompts))
	}
	run.StartState = entryStateFor(runner)
	if resumed != nil {
		// newJobRun already restored the recoverable state from cfg.Resumed.
		run.StartState = resumeStateFor(runner, resumed.State)
	}

	// The record exists before the goroutine does, so a status poll or a
	// cancellation that arrives immediately finds the job rather than a gap.
	// The run rewrites this snapshot as it enters the state for real.
	run.snapshot(run.StartState)
	s.jobs.Store(tenantID+"/"+jobID, &runningJob{run: run, cancel: cancel})

	// The goroutine is counted BEFORE it is started, so a drain that begins
	// between these two statements still waits for it.
	s.jobsInFlight.Add(1)
	go func() {
		defer s.jobsInFlight.Done()
		runErr := run.execute(jobCtx, runner)
		if runErr != nil {
			s.logger.Warnf("orchestration: job %s completed with error: %s", jobID, runErr.Error())
		}
		s.finishJob(tenantID, run, cancel, runErr)
	}()

	return jobID, nil
}

// loadTask reads the task the job is an attempt at, once, before the lease
// is taken. A job whose task cannot be read fails: the task is the job's
// lease subject and the record its outcome is written to, so running without
// it would produce work nothing accounts for.
func (r *JobRun) loadTask(ctx context.Context) error {
	if r.Task != nil || r.Config.TaskID == "" {
		return nil
	}
	task, err := r.Agents.GetTask(ctx, r.Config.TenantID, r.Config.TaskID)
	if err != nil {
		return err
	}
	if task == nil {
		return &TaskNotFoundError{TaskID: r.Config.TaskID}
	}
	r.Task = task
	return nil
}

// loadJobProject reads the project record a job runs for. A project the
// daemon holds no record for is not an error — a job may name a project the
// operator never registered — so the run simply carries none.
//
// A NOT-FOUND IS EXPECTED; ANY OTHER FAILURE IS NOT. Both used to be logged at
// Debug and both returned nil, so a store that could not answer was
// indistinguishable from a project that was never registered — and the SDLC
// runner's refusal ("the job carries no project record") arrived with nothing
// upstream to explain it. That cost two resumed jobs on the burn-in node and
// gave no clue why: the read was addressed with no tenant, which the store now
// prevents, but the diagnosis had to come from reading the resume path rather
// than from the log, which is the part worth fixing here.
func (s *agentServiceImpl) loadJobProject(ctx context.Context, config *JobConfig) *Project {
	if config.ProjectID == "" || s.projectStore == nil {
		return nil
	}
	project, err := s.projectStore.Get(ctx, config.TenantID, config.ProjectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			s.logger.Debugf("orchestration: job names project %s, which the daemon holds no record for", config.ProjectID)
			return nil
		}
		s.logger.Warnf("orchestration: project %s could not be read for job in tenant %s; the run carries no project and a bound task will refuse: %s",
			config.ProjectID, config.TenantID, err.Error())
		return nil
	}
	return project
}

// resolveJobWorkflow finds the definition a workflow job runs and derives the
// lead's backend and model from it when the caller named neither. A job that
// names no workflow runs without a definition.
//
// A workflow a fresh job names must exist: a missing one is a
// ReferenceNotFoundError and a registry that cannot answer (none attached) is
// a ReferenceUnavailableError. A resumed job whose definition has since gone
// runs without one, because refusing it would strand work already in flight.
func (s *agentServiceImpl) resolveJobWorkflow(config *JobConfig, resumed bool) (*WorkflowDefinition, error) {
	if config.WorkflowName != "" && !resumed {
		if err := RequireWorkflow(s.workflowGetter(), "workflow_name", config.TenantID, config.WorkflowName); err != nil {
			return nil, err
		}
	}
	if s.workflowLoader == nil {
		if config.WorkflowName != "" {
			s.logger.Warnf("orchestration: workflow %q requested but no workflow loader available", config.WorkflowName)
		}
		return nil, nil
	}

	if config.WorkflowName != "" {
		wf, err := s.workflowLoader.Get(config.TenantID, config.WorkflowName)
		if err != nil {
			s.logger.Warnf("orchestration: could not reload workflow %q for resumed job: %s",
				config.WorkflowName, err.Error())
			return nil, nil
		}
		s.applyWorkflowLead(config, wf)
		s.logger.Infof("orchestration: loaded workflow %q (mode=%s, agents=%d)", wf.Name, wf.Mode, len(wf.Agents))
		return wf, nil
	}

	if config.LeadBackend == "" {
		return nil, nil
	}

	// No workflow was named: find the one whose lead role matches the
	// backend and model the caller asked for, preferring the definition
	// with the most roles as the more specific match.
	var best *WorkflowDefinition
	var bestScore int
	for _, wf := range s.workflowLoader.ListDefinitions(config.TenantID) {
		for i := range wf.Agents {
			if wf.Agents[i].Role != "lead" || wf.Agents[i].Backend != config.LeadBackend {
				continue
			}
			if config.LeadModel != "" && wf.Agents[i].Model != config.LeadModel {
				continue
			}
			if score := len(wf.Agents); score > bestScore {
				bestScore, best = score, wf
			}
			break
		}
	}
	if best == nil {
		return nil, nil
	}
	config.WorkflowName = best.Name
	s.logger.Infof("orchestration: auto-matched workflow %q from lead backend=%s model=%s (score=%d)",
		best.Name, config.LeadBackend, config.LeadModel, bestScore)
	return best, nil
}

// workflowGetter returns the loader as a WorkflowGetter, or a nil interface
// when none is attached, so RequireWorkflow sees "no store" rather than a
// typed nil it would call through.
func (s *agentServiceImpl) workflowGetter() WorkflowGetter {
	if s.workflowLoader == nil {
		return nil
	}
	return s.workflowLoader
}

// applyWorkflowLead fills the lead's backend and model from the workflow's
// lead role when the caller set neither.
func (s *agentServiceImpl) applyWorkflowLead(config *JobConfig, wf *WorkflowDefinition) {
	for i := range wf.Agents {
		if wf.Agents[i].Role != "lead" {
			continue
		}
		if config.LeadBackend == "" {
			config.LeadBackend = wf.Agents[i].Backend
		}
		if config.LeadModel == "" {
			config.LeadModel = wf.Agents[i].Model
		}
		s.logger.Infof("orchestration: lead agent backend=%s model=%s", config.LeadBackend, config.LeadModel)
		return
	}
}

// restore reinstates the envelope's recoverable state from the status a
// resumed job continues. The runner restores its own from JobRun.Resumed.
func (r *JobRun) restore(status *JobStatus) {
	if status == nil {
		return
	}
	if status.Memory != nil {
		// The dedup sets are unexported map fields and do not survive
		// serialization; they are rebuilt from the restored entries.
		status.Memory.RebuildDedupSets()
		r.memory = status.Memory
	}
	r.iteration = status.Iteration
	// THE PARK COMES BACK, and it has to come back HERE — before anything
	// the resumed run does writes a snapshot.
	//
	// snapshot() rebuilds JobStatus.Park from r.park every time, so a
	// resumed run that did not restore it publishes a status with no park
	// and OVERWRITES the only durable record of the judgement. A second
	// crash then hands the lease reaper a record it reads as an undiagnosed
	// death, and the task is requeued — the same budget burn the durable
	// park exists to prevent, arriving by way of the restart that was
	// supposed to recover it.
	//
	// A CLONE, for the reason every other park copy is one: the status
	// belongs to the persisted record and the run must not write through
	// it.
	r.fanoutMu.Lock()
	r.park = status.Park.clone()
	r.fanoutMu.Unlock()
	// AND THE WORKSPACE PIN, for the park's own reason: snapshot() rebuilds
	// it from the run, so a resumed run that did not restore it would erase
	// the only record of where its checkout is — and its respawn would then
	// be free to clone afresh on whatever host the dispatcher picked.
	if status.WorkspacePin != nil {
		pin := *status.WorkspacePin
		r.workspacePin.Store(&pin)
	}
}

// execute runs the job to a terminal state. It owns the whole envelope: the
// lease, the heartbeat, the terminal status, the discard and the panic
// boundary. The runner sees none of it.
func (r *JobRun) execute(ctx context.Context, runner JobRunner) (runErr error) {
	if r.TenantID != "" && TenantFromContext(ctx) == "" {
		ctx = ContextWithTenant(ctx, r.TenantID)
	}

	// The run context is the job's own: a heartbeat that finds the job
	// superseded, or a detector that finds it stalled, cancels it from
	// outside the runner. The cancel is published before the lease is taken
	// so an abort can never miss it. On exit the heartbeat is stopped
	// first, then an unbound job's lease is released, then the run reports
	// itself finished.
	runCtx, cancelRun := context.WithCancel(ctx)
	r.cancelRun.Store(&cancelRun)
	defer r.finished.Store(true)
	defer r.settleLease()
	defer r.releaseOwnLease()
	defer r.stopHeartbeat()

	// The record exists from the moment the run starts, so a status poll
	// that arrives before the first state finds the job rather than a gap.
	r.snapshot(r.StartState)
	r.Emit(r.StartState, "job started: "+r.Config.Description)

	// Discard is the one moment that runs however the job ended —
	// completed, failed, cancelled or panicking — so it is the run's exit
	// path, on a context the job's cancellation cannot reach. A discard
	// failure on a job that had completed is a failure of that job; on a
	// job already failed it is audited beside the original cause rather
	// than replacing it.
	completed := false
	defer func() {
		recovered := recover()

		// THE WHOLE EXIT PATH IS GUARDED, not just the runner. Reporting a
		// runner that panicked, the terminal status and the discard moment
		// all write to the store, and the store can be closed under them:
		// a node shutting down does exactly that to a job that is still
		// running. The engine beneath go-qrdb reports an already-closed
		// store by panicking, so an unguarded exit path takes the daemon
		// down with it — the job's own conclusion becomes the crash.
		if closed := guardStoreClosed("conclude", r.JobID, func() {
			if recovered != nil {
				runErr = r.reportRunnerPanic(recovered)
			}
			completed = r.writeTerminalStatus(runErr, completed)

			discardErr := r.discard(ctx, completed)
			if discardErr == nil {
				return
			}
			if runErr == nil {
				r.Fail(FailureClassHook, discardErr)
				runErr = discardErr
				return
			}
			r.Logger.Warnf("orchestration: discard failed after job %s failed: %s", r.JobID, discardErr.Error())
		}); closed != nil {
			r.Logger.Warnf("orchestration: job %s could not record its conclusion: %s", r.JobID, closed.Error())
			if runErr == nil {
				runErr = closed
			}
		}
	}()

	// THE LEASE COMES BEFORE THE FIRST LIFECYCLE MOMENT. A job two nodes
	// run does two jobs' worth of side effects, and Prepare is the first
	// one that has any — it plans a branch, it labels an issue. Taking the
	// lease first means the capacity gate is enforced before anything
	// outside the daemon has changed.
	leaseErr := r.acquireJobLease(runCtx, runner)
	r.settleLease()
	if leaseErr != nil {
		return leaseErr
	}

	if err := r.SetState(runCtx, r.StartState); err != nil {
		return err
	}

	if err := runner.Run(runCtx, r); err != nil {
		return r.terminalError(err)
	}

	completed = true
	return nil
}

// reportRunnerPanic turns a value recovered from the runner into the job's
// terminal error and records it.
//
// A closed store is not the runner's failure: the node is going down under
// the job, and the record cannot be written at all, so it is reported as
// what it is and the job is left where it stood for RecoverJobs to resume.
func (r *JobRun) reportRunnerPanic(recovered any) error {
	if IsStoreClosed(recovered) {
		closed := &StoreClosedError{JobID: r.JobID, Op: "run", Value: fmt.Sprint(recovered)}
		r.Logger.Warnf("orchestration: job %s stopped: %s", r.JobID, closed.Error())
		return closed
	}
	panicked := &JobPanicError{
		JobID:  r.JobID,
		Runner: r.Runner,
		Value:  fmt.Sprint(recovered),
		Stack:  string(debug.Stack()),
	}
	r.Logger.Errorf("orchestration: job %s panicked: %s\n%s",
		r.JobID, panicked.Error(), panicked.Stack)
	r.Fail(FailureClassPanic, panicked)
	return panicked
}

// writeTerminalStatus records the job's ending when the runner did not.
// A runner that drove itself to completed or failed has already written the
// status it wants; one that returned early has not.
func (r *JobRun) writeTerminalStatus(runErr error, completed bool) bool {
	status := r.Status()
	terminal := status != nil && IsTerminalJobState(status.State)

	if runErr != nil {
		if !terminal || status.State != JobStateFailed {
			r.Fail("", runErr)
		}
		return false
	}
	if !terminal {
		r.snapshot(JobStateCompleted)
		r.Emit(JobStateCompleted, "job completed")
		return true
	}
	return completed || status.State == JobStateCompleted
}
