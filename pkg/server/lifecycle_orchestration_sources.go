// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package server

import (
	"context"
	"errors"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// jobLifecyclePolicy builds the operator's lifecycle policy from daemon
// configuration: the command allowlist (orchestration.verification_commands)
// and the per-moment bounds (orchestration.stage_timeouts).
func (s *Server) jobLifecyclePolicy() (orchestration.JobLifecyclePolicy, error) {
	cfg := s.config.Orchestration
	guard, err := s.executionGuard()
	if err != nil {
		return orchestration.JobLifecyclePolicy{}, err
	}
	return orchestration.JobLifecyclePolicy{
		Guard:          guard,
		AgentConfigDir: cfg.AgentConfigDir,
		StageTimeouts: orchestration.StageTimeouts{
			Prepare:    cfg.StageTimeouts.Prepare,
			Reviewable: cfg.StageTimeouts.Reviewable,
			Complete:   cfg.StageTimeouts.Complete,
			Discard:    cfg.StageTimeouts.Cleanup,
			Blocked:    cfg.StageTimeouts.Blocked,
		}.WithDefaults(),
	}, nil
}

// executionGuard builds the operator's command allowlist
// (orchestration.verification_commands): the one guard the job policy
// enforces at validation and the SDLC module checks bindings against.
func (s *Server) executionGuard() (orchestration.ExecutionGuard, error) {
	return orchestration.NewExecutionGuard(s.config.Orchestration.VerificationCommands)
}

// renderWorkerSettings writes the host worker's shell allowlist
// (orchestration.agent_shell_allow plus every verification command) into the
// engine's agent configuration directory, so a fresh data dir does not
// silently leave host workers with edits and no shell. The file is generated:
// one that says anything else is overwritten, and the overwrite is logged so
// an operator who hand-edited it learns where the setting now lives. Unset
// agent_config_dir writes nothing.
func (s *Server) renderWorkerSettings() error {
	cfg := s.config.Orchestration
	if cfg.AgentConfigDir == "" {
		return nil
	}
	replaced, err := orchestration.WriteWorkerSettings(
		cfg.AgentConfigDir, cfg.AgentShellAllow, cfg.VerificationCommands)
	if err != nil {
		return err
	}
	if replaced {
		s.logger.Warnf("orchestration: %s/%s differed from the configuration and was regenerated; "+
			"set worker shell commands in orchestration.agent_shell_allow, not in the file",
			cfg.AgentConfigDir, orchestration.WorkerSettingsFile)
	}
	return nil
}

// orchestrationSweepCollaborators are the stores a sweep over projects needs:
// the tenants to enumerate, the projects to visit and the leases that say how
// many jobs each project is already running. A boot missing any of them runs
// neither the poller nor the dispatcher, and says which one was missing.
func (s *Server) orchestrationSweepCollaborators(what string) (tenants orchestration.TenantLister, projects orchestration.ProjectLister, ok bool) {
	if s.iamComponents == nil || s.iamComponents.GetTenantManager() == nil {
		s.logger.Infof("%s not started: no tenant manager to enumerate tenants", what)
		return nil, nil, false
	}
	if s.quicBaaSService != nil {
		if psvc, storeOK := s.quicBaaSService.Project().(interface {
			Store() orchestration.ProjectStore
		}); storeOK {
			projects = psvc.Store()
		}
	}
	if projects == nil {
		s.logger.Infof("%s not started: no project store to sweep", what)
		return nil, nil, false
	}
	if s.orchTaskStore == nil {
		s.logger.Infof("%s not started: no task store to read the queue from", what)
		return nil, nil, false
	}
	if s.orchJobLeaseStore == nil || s.orchLeaseTTL <= 0 {
		s.logger.Infof("%s not started: no job lease store to count running jobs with", what)
		return nil, nil, false
	}
	return &iamTenantLister{tenants: s.iamComponents.GetTenantManager()}, projects, true
}

// orchestrationAdmission builds the admission controller both sweeps consult.
//
// ONE CONTROLLER PER SWEEP RATHER THAN ONE SHARED, and the difference is only
// which clock and which stores each holds — both are the same handles. They are
// separate because a node may run one sweep and not the other, and a controller
// half-owned by whichever started first would be a lifetime nobody owns.
//
// A CONTROLLER THAT CANNOT BE BUILT IS REPORTED AND NOT SUBSTITUTED. The sweep
// still starts, bounded by per-project quotas exactly as it was before this
// existed, because the alternative — refusing to sweep at all — stops a
// deployment that was working from working. What it must never do is start
// while claiming a fleet bound it is not enforcing, so the log line says which
// axes are live.
func (s *Server) orchestrationAdmission(what string, tenants orchestration.TenantLister, projects orchestration.ProjectLister) *orchestration.AdmissionController {
	capacity := s.orchFleetCapacity
	if capacity == nil {
		capacity = s.orchestrationFleetCapacity()
	}
	admission, err := orchestration.NewAdmissionController(orchestration.AdmissionControllerConfig{
		Tenants:  tenants,
		Projects: projects,
		Queue:    s.orchTaskStore,
		Leases:   s.orchJobLeaseStore,
		LeaseTTL: s.orchLeaseTTL,
		Capacity: capacity,
		Headroom: s.orchPoolHeadroom,
		Spend:    s.orchScopeSpend,
	})
	if err != nil {
		s.logger.Warnf("%s: admission controller not built, falling back to per-project quotas only: %v", what, err)
		return nil
	}
	// ONE BOUNDED READ, PURELY SO THE LOG LINE NAMES A NUMBER. An operator
	// starting the daemon should be able to read which axes are live without
	// waiting for the first sweep -- but a store that will not answer must not
	// hold up start-up for it, so the read is bounded and its failure is a
	// warning rather than a fault. The sweeps read the capacity again on every
	// tick regardless.
	readCtx, cancel := context.WithTimeout(context.Background(), orchestrationCapacityReadTimeout)
	defer cancel()
	reading, readErr := admission.FleetCapacity(readCtx, time.Now().UTC())
	if readErr != nil {
		s.logger.Warnf("%s: admission wired, but the fleet capacity could not be read at start-up: %v", what, readErr)
	} else {
		s.logger.Infof("%s: admission bounded by project quota, fleet capacity (%s), provider usage headroom (%v) and project/tenant spend limits (%v)",
			what, reading.Describe(), s.orchPoolHeadroom != nil, s.orchScopeSpend != nil)
	}
	return admission
}

// orchestrationCapacityReadTimeout bounds the one start-up read of the fleet's
// capacity. Short on purpose: nothing depends on the answer except a log line.
const orchestrationCapacityReadTimeout = 5 * time.Second

// orchestrationFleetCapacity builds the reader that answers how many worker
// slots the deployment has.
//
// TWO SOURCES, COMBINED BY orchestration.NewFleetCapacity AND NOWHERE ELSE: the
// fleet's own daemons, which report their slot counts on every heartbeat, and
// the operator's orchestration.fleet.max_concurrent_jobs ceiling. Either may be
// absent; when both answer, the tighter one binds.
//
// THE MEASUREMENT IS LEFT OUT RATHER THAN FAKED when there is no tenant manager
// or no agent registry to read. An unmeasured fleet is not a fleet with no
// slots — see the rule stated in fleet_capacity.go — so what remains is the
// operator's number, or no fleet axis at all.
func (s *Server) orchestrationFleetCapacity() orchestration.FleetCapacityReader {
	declared := s.config.Orchestration.Fleet.MaxConcurrentJobs

	var measured orchestration.FleetCapacityReader
	if s.fleetRegistry != nil && s.iamComponents != nil && s.iamComponents.GetTenantManager() != nil {
		reader, err := orchestration.NewRegistryFleetCapacity(
			&iamTenantLister{tenants: s.iamComponents.GetTenantManager()},
			s.fleetRegistry,
			s.config.Orchestration.HeartbeatTimeout)
		if err != nil {
			s.logger.Warnf("Fleet capacity: the daemons' own reports are unavailable, leaving only the operator's declaration: %v", err)
		} else {
			measured = reader
		}
	}

	capacity, err := orchestration.NewFleetCapacity(declared, measured)
	if err != nil {
		// A negative configured ceiling, which the config validator already
		// refuses. Reported and left out rather than substituted: a fleet axis
		// nobody can read is the pre-existing behaviour, and a number invented
		// here would be a bound the operator never set.
		s.logger.Warnf("Fleet capacity: not enforced, the configured ceiling is unusable: %v", err)
		undeclared, _ := orchestration.NewDeclaredFleetCapacity(0)
		return undeclared
	}
	return capacity
}

// startTaskSourcePoller starts the sweep over every registered task source.
//
// The poller reads projects from the SAME store the project service writes
// and creates tasks through the agent service, so an adopted task is exactly
// the task an operator would have created by hand. It sweeps only projects
// the operator turned autonomous, and adopts only as much as each project's
// remaining concurrency allows.
func (s *Server) startTaskSourcePoller(tasks orchestration.TaskCreator) {
	tenants, projects, ok := s.orchestrationSweepCollaborators("Task-source poller")
	if !ok {
		return
	}

	poller, err := orchestration.NewTaskSourcePoller(orchestration.TaskSourcePollerConfig{
		Sources:   orchestration.DefaultTaskSourceRegistry(),
		Tenants:   tenants,
		Projects:  projects,
		Tasks:     tasks,
		Queue:     s.orchTaskStore,
		Leases:    s.orchJobLeaseStore,
		LeaseTTL:  s.orchLeaseTTL,
		Admission: s.orchestrationAdmission("Task-source poller", tenants, projects),
		Logger:    &logger.LoggerAdapter{Logger: s.logger},
	})
	if err != nil {
		s.logger.Warnf("Task-source poller not started: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.taskSourcePoller = poller
	s.taskSourcePollerStop = cancel
	s.taskSourcePollerDone = done
	interval := s.config.Orchestration.EffectiveSweepInterval()
	go func() {
		defer close(done)
		if runErr := poller.Run(ctx, interval); runErr != nil && !errors.Is(runErr, context.Canceled) {
			s.logger.Warnf("Task-source poller stopped: %v", runErr)
		}
	}()
	s.logger.Infof("Task-source poller started (sweep=%s)", interval)
}

// stopTaskSourcePoller ends the poller and waits for it to return.
func (s *Server) stopTaskSourcePoller() {
	if s.taskSourcePollerStop == nil {
		return
	}
	s.taskSourcePollerStop()
	<-s.taskSourcePollerDone
	s.taskSourcePoller = nil
	s.taskSourcePollerStop = nil
	s.taskSourcePollerDone = nil
}

// startTaskDispatcher starts the sweep that gives every pending task a second
// chance to be dispatched.
//
// IT RUNS BESIDE THE POLLER, at the same cadence and on the same
// collaborators, because they are two halves of one answer: the poller decides
// what becomes work and the dispatcher decides what runs. A node with one and
// not the other either adopts work nothing starts, or starts work nothing
// adopts.
func (s *Server) startTaskDispatcher(spawner orchestration.TaskSpawner) {
	tenants, projects, ok := s.orchestrationSweepCollaborators("Task dispatcher")
	if !ok {
		return
	}

	dispatcher, err := orchestration.NewTaskDispatcher(orchestration.TaskDispatcherConfig{
		Tenants:   tenants,
		Projects:  projects,
		Tasks:     s.orchTaskStore,
		Leases:    s.orchJobLeaseStore,
		LeaseTTL:  s.orchLeaseTTL,
		Spawner:   spawner,
		Admission: s.orchestrationAdmission("Task dispatcher", tenants, projects),
		Logger:    &logger.LoggerAdapter{Logger: s.logger},
	})
	if err != nil {
		s.logger.Warnf("Task dispatcher not started: %v", err)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.taskDispatcher = dispatcher
	s.taskDispatcherStop = cancel
	s.taskDispatcherDone = done
	interval := s.config.Orchestration.EffectiveSweepInterval()
	gate := s.dispatchGate
	go func() {
		defer close(done)
		if !awaitDispatchGate(ctx, gate, s.logger) {
			return
		}
		if runErr := dispatcher.Run(ctx, interval); runErr != nil && !errors.Is(runErr, context.Canceled) {
			s.logger.Warnf("Task dispatcher stopped: %v", runErr)
		}
	}()
	s.logger.Infof("Task dispatcher started (sweep=%s)", interval)
}

// awaitDispatchGate holds the dispatcher's first sweep until every resumed
// job has settled its lease. It reports false when ctx ended first, and the
// dispatcher never sweeps. A nil gate is open.
func awaitDispatchGate(ctx context.Context, gate <-chan struct{}, log *logger.Logger) bool {
	if gate == nil {
		return true
	}
	select {
	case <-gate:
		return true
	default:
	}
	log.Info("Task dispatcher waiting: resumed jobs have not all taken their leases yet")
	select {
	case <-gate:
		log.Info("Task dispatcher released: every resumed job holds its lease")
		return true
	case <-ctx.Done():
		return false
	}
}

// stopTaskDispatcher ends the dispatcher and waits for it to return.
func (s *Server) stopTaskDispatcher() {
	if s.taskDispatcherStop == nil {
		return
	}
	s.taskDispatcherStop()
	<-s.taskDispatcherDone
	s.taskDispatcher = nil
	s.taskDispatcherStop = nil
	s.taskDispatcherDone = nil
}

// NudgeProject sweeps one project now: its sources first, then its queue. It
// is the module's push path into the engine; without a poller (no tenants to
// sweep, orchestration disabled) the nudge is refused typed rather than
// dropped.
//
// THE ORDER IS THE POINT. A verified webhook delivery says work appeared;
// polling first and dispatching second turns that one call into a running job,
// where dispatching first would start whatever was already queued and leave
// the new issue for the next tick.
//
// A parked project answers *orchestration.ProjectParkedError once its queue
// has been swept: its intake is closed, its work in flight is not.
func (s *Server) NudgeProject(ctx context.Context, tenantID, projectID string) error {
	poller := s.taskSourcePoller
	if poller == nil {
		return &orchestration.TaskSourcePollerConfigError{Field: "poller", Message: "is not running on this node"}
	}
	// A PARKED PROJECT'S QUEUE IS STILL SWEPT. The poller refuses to adopt
	// new work for it, but the tasks it has already started keep draining
	// (orchestration/project_park.go), so the refusal is reported only after
	// the dispatcher has had its turn.
	adoptErr := poller.SweepProject(ctx, tenantID, projectID)
	if adoptErr != nil && !errors.Is(adoptErr, orchestration.ErrProjectParked) {
		return adoptErr
	}
	if dispatcher := s.taskDispatcher; dispatcher != nil {
		if err := dispatcher.SweepProject(ctx, tenantID, projectID); err != nil {
			return err
		}
	}
	return adoptErr
}

// iamTenantLister enumerates tenants through the IAM tenant manager, and
// answers whether one exists (orchestration.TenantResolver).
type iamTenantLister struct {
	tenants iam.TenantManager
}

// ListTenantIDs implements orchestration.TenantLister.
func (l *iamTenantLister) ListTenantIDs(ctx context.Context) ([]string, error) {
	tenants, err := l.tenants.ListTenants(ctx, iam.ListOptions{})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(tenants))
	for _, t := range tenants {
		if t == nil || t.ID == "" {
			continue
		}
		ids = append(ids, t.ID)
	}
	return ids, nil
}

// TenantExists implements orchestration.TenantResolver. The system tenant
// always exists: it is qrdb's reserved scope, not an IAM record. An ID IAM
// would refuse as malformed names no tenant; any other failure is the tenant
// directory failing to answer, and is returned as such.
//
// The lookup is bound to the system tenant. Tenant records are control-plane
// records, written by the system admin into the system scope, while the
// caller's context is usually bound to the very tenant being checked (a fleet
// registration carries the registering tenant). Read in that tenant's own
// scope, the record is never found (#1605). The IAM tenant key is the one the
// routing store consults first (tenantFromAnyContext), so it also overrides an
// orchestration tenant on the context.
func (l *iamTenantLister) TenantExists(ctx context.Context, tenantID string) (bool, error) {
	if qrdbsdk.IsSystemTenant(tenantID) {
		return true, nil
	}
	_, err := l.tenants.GetTenant(tenantctx.WithSystem(ctx), tenantID)
	switch {
	case err == nil:
		return true, nil
	case quicbaaserrors.IsNotFound(err), quicbaaserrors.IsInvalidInput(err):
		return false, nil
	default:
		return false, err
	}
}
