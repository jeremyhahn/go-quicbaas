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
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// What wakes triage
// =============================================================================
//
// A verified webhook delivery nudges this scheduler, which starts ONE triage
// job for the project. A periodic pass over the bound projects is the
// backstop for a delivery that never arrived — a hook that was not
// provisioned, a forge that was down, a daemon that was restarting.
//
// THE SCHEDULER DOES NOT ENFORCE THE PER-PROJECT BOUND; THE LEASE DOES. Two
// nudges arriving together, or a nudge landing on a second node, both reach
// the engine and both try to take the same lease subject — and exactly one
// wins (see TriageLeaseSubject). A scheduler-side "is one running" flag would
// be a second, weaker answer that disagrees with the first across a restart
// or a node boundary, so there is not one. What the scheduler does with the
// refusal is recognise it and stay quiet: a project already being triaged is
// the system working, not a failure to report.

// TriageJobStarter starts jobs on the engine; orchestration.AgentService
// satisfies it.
type TriageJobStarter interface {
	StartJob(ctx context.Context, req *orchestration.StartJobRequest) (*orchestration.StartJobResponse, error)
}

// triageWakeEvents are the forge deliveries that can change a verdict, and
// therefore wake triage. Map-based so an event answers in constant time and
// the set is data a test can walk.
//
// The three that matter are the issue's own lifecycle. A CLOSED issue is
// absent deliberately: triage has nothing to conclude about work nobody is
// waiting on, and waking for it would spend a pass per close. Pull-request,
// review and check deliveries belong to the delivery half of the loop.
var triageWakeEvents = map[ForgeEventKind]bool{
	EventIssueOpened:  true,
	EventIssueEdited:  true,
	EventIssueLabeled: true,
}

// TriageWakesOn reports whether a forge event should wake triage for its
// project.
func TriageWakesOn(kind ForgeEventKind) bool { return triageWakeEvents[kind] }

// TriageSchedulerConfig wires the scheduler.
type TriageSchedulerConfig struct {
	// Bindings decides whether a project has a forge to triage at all.
	Bindings BindingReader

	// Jobs is the engine the triage job is started on.
	Jobs TriageJobStarter
}

// TriageScheduler turns a nudge into a triage job.
type TriageScheduler struct {
	bindings BindingReader
	jobs     TriageJobStarter

	// failures are the per-tenant and per-project failures a sweep deferred,
	// drained by SweepFailures. Only the sweep goroutine touches them, and
	// there is exactly one.
	failures []error
}

// TriageScheduler is what a verified webhook delivery nudges.
var _ ProjectNudger = (*TriageScheduler)(nil)

// NewTriageScheduler builds the scheduler; every collaborator is required.
func NewTriageScheduler(cfg TriageSchedulerConfig) (*TriageScheduler, error) {
	switch {
	case cfg.Bindings == nil:
		return nil, &InvalidInputError{Field: "bindings", Message: "only a bound project has a forge to triage"}
	case cfg.Jobs == nil:
		return nil, &InvalidInputError{Field: "jobs", Message: "a nudge that starts no job changes nothing"}
	}
	return &TriageScheduler{bindings: cfg.Bindings, jobs: cfg.Jobs}, nil
}

// NudgeProject implements ProjectNudger: a verified delivery asks for the
// project to be triaged now.
func (s *TriageScheduler) NudgeProject(ctx context.Context, tenantID, projectID string) error {
	return s.TriageProject(ctx, tenantID, projectID)
}

// TriageProject starts one triage job for a bound project.
//
// An unbound project is a QUIET no-op. Most projects on a node have no forge
// binding, and starting a job for each of them that could only fail on
// resolve would fill the job record with failures that say nothing.
func (s *TriageScheduler) TriageProject(ctx context.Context, tenantID, projectID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return &InvalidInputError{Field: "tenant_id", Message: "triage is scoped to a tenant"}
	}
	if strings.TrimSpace(projectID) == "" {
		return &InvalidInputError{Field: "project_id", Message: "triage is per project"}
	}
	binding, err := s.bindings.GetBinding(ctx, tenantID, projectID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if binding == nil {
		return nil
	}
	// THE JOB SAYS WHAT IT WILL RUN ON. Without these the job record carries
	// no backend and no model at all, so an operator watching the queue sees a
	// row that cannot tell them whether it is about to spend a local GPU turn
	// or a frontier-model one — which is the difference between a triage sweep
	// that costs nothing and one that costs real money.
	agent := binding.Triage
	if strings.TrimSpace(agent.Backend) == "" {
		agent = binding.Worker
	}
	_, err = s.jobs.StartJob(ctx, &orchestration.StartJobRequest{
		TenantID:    tenantID,
		ProjectID:   projectID,
		Runner:      TriageRunnerName,
		Description: "Triage the open issues of " + binding.Repo().String(),
		LeadBackend: strings.TrimSpace(agent.Backend),
		LeadModel:   strings.TrimSpace(agent.Model),
	})
	if err == nil {
		return nil
	}
	if triageAlreadyRunning(err) {
		// The project is already being triaged. That is the per-project
		// bound doing its job, not a failure: the run in flight will see
		// whatever this delivery was about, because it lists the issues
		// when it starts.
		return nil
	}
	return backendError("StartJob", err)
}

// triageAlreadyRunning reports whether a refusal means another triage job
// already holds this project's lease.
func triageAlreadyRunning(err error) bool {
	return errors.Is(err, orchestration.ErrJobLeaseTaken) ||
		errors.Is(err, orchestration.ErrJobSuperseded)
}

// =============================================================================
// The periodic backstop
// =============================================================================
//
// A webhook that never arrived — a hook that was not provisioned, a forge
// that was down, a daemon that was restarting — leaves an issue untriaged
// for ever, and the issue is the one thing nobody is watching. The sweep is
// what bounds that: every bound project, every pass.
//
// It is CHEAP BY CONSTRUCTION rather than by rate limiting. A triage job for
// a project with nothing to judge lists its issues, finds no candidate, and
// finishes without spawning a session (see triageRun.initialize) — so a
// sweep over a quiet fleet costs one forge listing per project.

// DefaultTriageSweepInterval is how often the backstop pass runs.
const DefaultTriageSweepInterval = 15 * time.Minute

// Sweep nudges every bound project of every tenant, and reports how many it
// nudged.
//
// THE ONE FAILURE THAT STOPS IT is not being able to enumerate the tenants:
// the sweep then does not know what it was meant to visit, and reporting a
// clean pass would be a lie. Everything narrower — a tenant whose bindings
// cannot be listed, a project whose job the engine refuses — costs that one
// tenant or project a pass and nothing else, because the sweep is the only
// path the others have.
func (s *TriageScheduler) Sweep(ctx context.Context, tenants orchestration.TenantLister) (int, error) {
	if tenants == nil {
		return 0, &InvalidInputError{Field: "tenants", Message: "a sweep that enumerates no tenant visits nothing while claiming a clean pass"}
	}
	tenantIDs, err := tenants.ListTenantIDs(ctx)
	if err != nil {
		return 0, backendError("ListTenantIDs", err)
	}
	nudged := 0
	for _, tenantID := range tenantIDs {
		if err := ctx.Err(); err != nil {
			return nudged, err
		}
		// THE TENANT TRAVELS ON THE CONTEXT, NOT ONLY IN THE ARGUMENT. The
		// backstop runs on a background context of its own, and the engine
		// guards StartJob on the tenant bound to the CONTEXT — so a sweep
		// passing only the argument is refused `tenant ID required` for every
		// bound project of every tenant on every pass, and the backstop triages
		// nothing while reporting itself started. Measured on the burn-in node:
		// four consecutive passes, one project, four refusals. The task-source
		// poller stamps it for the same reason (task_source.go).
		tenantCtx := orchestration.ContextWithTenant(ctx, tenantID)
		bindings, err := s.bindings.ListBindings(tenantCtx, tenantID)
		if err != nil {
			s.failures = append(s.failures, err)
			continue
		}
		for _, binding := range bindings {
			if binding == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return nudged, err
			}
			if err := s.TriageProject(tenantCtx, tenantID, binding.ProjectID); err != nil {
				s.failures = append(s.failures, err)
				continue
			}
			nudged++
		}
	}
	return nudged, nil
}

// SweepFailures returns and clears the per-tenant and per-project failures
// the last sweeps deferred, so the caller can log them where it logs
// everything else rather than the sweep writing to a logger of its own.
func (s *TriageScheduler) SweepFailures() []error {
	failures := s.failures
	s.failures = nil
	return failures
}
