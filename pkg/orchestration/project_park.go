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
	"time"
)

// =============================================================================
// A parked project drains
// =============================================================================
//
// PARKING CLOSES THE INTAKE AND STOPS NOTHING. The owner's ruling
// (2026-09-25): parking "should not cancel or stop anything already in flight
// or cause data loss. it should simply stop accepting new work and allow
// everything in flight to complete." So a parked project refuses the work it
// has not started and finishes the work it has, through every step that work
// has left: the worker turn, the pull request, the review, the merge train.
//
// STARTED IS "DISPATCHED AT LEAST ONCE". A task is started once the engine
// has claimed it for a job (ClaimTaskSpawn charges Task.Attempts), and it
// stays started after that attempt ends: a task requeued by an environment
// fault, or readmitted from a park, is part of the work in flight and is
// re-dispatched while the project is parked. A task whose spawn claim was
// released because its job never launched is refunded to zero attempts and
// is not started. See Task.Started.
//
// A JOB BOUND TO NO TASK is new work unless its runner says it finishes work
// already started (StartedWorkRunner): a review of an open pull request, a
// merge train landing approved ones. Triage and a hand-started workflow job
// are intake, and are refused.
//
// ONE GATE. admitIntake is the only place the rule is applied to a dispatch;
// the sweeps that enumerate projects read Project.Parked only to avoid asking
// the gate a question whose answer is already known, never to decide
// differently.
//
// NOTHING IS WRITTEN ON PARK and nothing is deleted. A refused task stays
// pending exactly as it was, and Resume reopens the intake: the next sweep
// picks it up. Parked and Autonomous are independent switches.

// ErrProjectParked indicates work was refused because its project is parked.
var ErrProjectParked = errors.New("orchestration: project is parked")

// ProjectParkedError reports new work a parked project refused.
type ProjectParkedError struct {
	// TenantID owns the project.
	TenantID string

	// ProjectID identifies the parked project.
	ProjectID string

	// Work names what was refused: a task, a runner, an adoption.
	Work string

	// ParkedAt is when the project was parked.
	ParkedAt time.Time

	// ParkedBy names who parked it, when the request that did carried an
	// identity.
	ParkedBy string
}

// Error implements the error interface.
func (e *ProjectParkedError) Error() string {
	msg := ErrProjectParked.Error() + ": project " + e.ProjectID
	if e.Work != "" {
		msg += " refused " + e.Work
	}
	if !e.ParkedAt.IsZero() {
		msg += " (parked " + e.ParkedAt.UTC().Format(time.RFC3339)
		if e.ParkedBy != "" {
			msg += " by " + e.ParkedBy
		}
		msg += ")"
	}
	return msg + "; resume the project to admit new work"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProjectParkedError) Unwrap() error { return ErrProjectParked }

// Park closes the project's intake, recording who parked it and when. Parking
// a project that is already parked keeps the original record, so the answer
// to "since when" does not move every time somebody repeats the request.
func (p *Project) Park(by string, at time.Time) {
	if p == nil || p.Parked {
		return
	}
	p.Parked = true
	p.ParkedAt = at.UTC()
	p.ParkedBy = by
}

// Resume reopens the project's intake. It does not touch Autonomous.
func (p *Project) Resume() {
	if p == nil {
		return
	}
	p.Parked = false
	p.ParkedAt = time.Time{}
	p.ParkedBy = ""
}

// RefuseIntake returns the refusal a parked project gives for work, or nil
// when the project is open. work names what was refused.
func (p *Project) RefuseIntake(work string) error {
	if p == nil || !p.Parked {
		return nil
	}
	return &ProjectParkedError{
		TenantID:  p.TenantID,
		ProjectID: p.ID,
		Work:      work,
		ParkedAt:  p.ParkedAt,
		ParkedBy:  p.ParkedBy,
	}
}

// AdmitsTask reports whether the project may dispatch task now as far as
// parking is concerned: an open project dispatches anything, a parked one
// only a task already started.
func (p *Project) AdmitsTask(task *Task) bool {
	return p == nil || !p.Parked || task.Started()
}

// Started reports whether the engine has dispatched the task at least once.
// A started task is work in flight, which a parked project still finishes.
func (t *Task) Started() bool {
	return t != nil && t.Attempts > 0
}

// StartedWorkRunner is the optional half of a runner whose jobs, bound to no
// task, finish work that has already started — a review of an open pull
// request, a merge train landing approved ones — rather than taking in new
// work. A parked project still admits them. A runner that does not implement
// it is intake, and a parked project refuses its unbound jobs.
type StartedWorkRunner interface {
	// FinishesStartedWork reports whether the runner's unbound jobs serve
	// work already in flight.
	FinishesStartedWork() bool
}

// runnerFinishesStartedWork reports whether a runner's unbound jobs serve
// work already in flight.
func runnerFinishesStartedWork(runner JobRunner) bool {
	finisher, ok := runner.(StartedWorkRunner)
	return ok && finisher != nil && finisher.FinishesStartedWork()
}

// admitIntake is THE park gate for a job that is about to be started: nil
// when the project is open or the job finishes work already in flight, a
// *ProjectParkedError otherwise.
//
// task is the task the job is an attempt at, read BEFORE the spawn claim
// charges it an attempt; nil for a job bound to no task, which is then judged
// by its runner.
func admitIntake(project *Project, runner JobRunner, task *Task) error {
	if project == nil || !project.Parked {
		return nil
	}
	if task != nil {
		if project.AdmitsTask(task) {
			return nil
		}
		return project.RefuseIntake("task " + task.ID + ", which has not started")
	}
	if runner != nil && runnerFinishesStartedWork(runner) {
		return nil
	}
	name := JobRunnerWorkflow
	if runner != nil {
		name = runner.Name()
	}
	return project.RefuseIntake("a new " + name + " job")
}

// =============================================================================
// The gate on the agent service
// =============================================================================

// intakeProject reads the project a dispatch names, for the park gate. A job
// naming no project, a daemon with no project store, and a project the store
// holds no record for are all answered nil: there is no park to honour, and
// the paths that care about a missing project refuse it themselves. Any other
// read failure is returned, because a gate that cannot read the switch it
// enforces must not guess that the switch is off.
func (s *agentServiceImpl) intakeProject(ctx context.Context, tenantID, projectID string) (*Project, error) {
	if projectID == "" || s.projectStore == nil {
		return nil, nil
	}
	project, err := s.projectStore.Get(ctx, tenantID, projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, err
	}
	return project, nil
}

// admitTaskDispatch applies the park gate to a dispatch of task, before the
// spawn claim charges it an attempt.
func (s *agentServiceImpl) admitTaskDispatch(ctx context.Context, task *Task) error {
	if task == nil {
		return nil
	}
	project, err := s.intakeProject(ctx, task.TenantID, task.ProjectID)
	if err != nil {
		return err
	}
	if err := admitIntake(project, nil, task); err != nil {
		s.logger.Infof("orchestration: %s", err.Error())
		return err
	}
	return nil
}

// admitUnboundJob applies the park gate to a request for a job bound to no
// task. A request the start path will refuse anyway — nil, no tenant, an
// unknown runner — is left for it to refuse with its own error.
func (s *agentServiceImpl) admitUnboundJob(ctx context.Context, req *StartJobRequest) error {
	if req == nil || req.ProjectID == "" {
		return nil
	}
	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = TenantFromContext(ctx)
	}
	if tenantID == "" {
		return nil
	}
	runner, ok := LookupJobRunner(ResolveJobRunnerName(req.Runner))
	if !ok {
		return nil
	}
	project, err := s.intakeProject(ctx, tenantID, req.ProjectID)
	if err != nil {
		return err
	}
	if err := admitIntake(project, runner, nil); err != nil {
		s.logger.Infof("orchestration: %s", err.Error())
		return err
	}
	return nil
}
