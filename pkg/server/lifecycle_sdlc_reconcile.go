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

package server

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// =============================================================================
// The label reconciliation sweep — mounted on the node, not on an operator
// =============================================================================
//
// IT RUNS WITHOUT ANYBODY ASKING. The state it repairs is by definition state
// nobody noticed: seven go-truststrap issues carried `factory:in-progress` for
// tasks that had been terminal for DAYS, and they were cleared by hand. A
// repair that needs an operator to invoke it is a repair that does not happen,
// because the operator is exactly the person the stale label is lying to.
//
// So it runs at boot and on a ticker, beside the module's other work, and adds
// no operator verb: the SDLC module exposes none for its other sweeps either
// (the unblock sweep has no CLI, no REST route and no transport verb), and
// inventing one here would be the only surface of its kind in the module.
//
// THE BOOT PASS IS THE POINT OF THE BOOT PASS. A node that died between
// concluding a task and posting its outcome comes back with exactly the
// inconsistency this sweep fixes, and the moment it comes back is the moment to
// fix it — not five minutes later.

// sdlcLabelSweeps is the running reconciliation goroutine and the means to
// stop it.
type sdlcLabelSweeps struct {
	stop context.CancelFunc
	done chan struct{}
}

// labelReconciler is the sweep as the daemon drives it. An interface so the
// loop's cadence, its boot pass and its shutdown can be proven without a forge.
type labelReconciler interface {
	Reconcile(ctx context.Context) (sdlc.LabelReconcileReport, error)
}

// startSDLCLabelReconcile builds the sweep and runs it until shutdown.
//
// A MISSING COLLABORATOR IS AN INFO, NOT A REFUSED BOOT, and that is the
// difference between this and the rest of startSDLC. Every other component
// there is load-bearing — a daemon serving a webhook route with no ingestor
// behind it looks like a working module with a quiet forge. This one only
// repairs display state: a node without a task store to compare against still
// delivers every issue it adopts, and refusing its boot over a sweep would
// trade a stale label for an outage.
func (s *Server) startSDLCLabelReconcile(hooks *sdlc.Hooks, bindings sdlc.BindingReader) {
	tenants, ok := s.sdlcReconcileTenants()
	if !ok {
		return
	}
	if s.orchTaskStore == nil {
		s.logger.Info("sdlc: label reconciliation not started: no task store to compare the forge against")
		return
	}

	cfg := s.config.SDLC.LabelReconcile
	reconciler, err := sdlc.NewLabelReconciler(sdlc.LabelReconcilerConfig{
		Tenants: tenants, Bindings: bindings, Tasks: s.orchTaskStore, Hooks: hooks,
		Pacer:        sdlc.NewForgePacer(cfg.ForgeInterval),
		Reservations: s.sdlcOutcomeReserver(s.durableKVStore()),
	})
	if err != nil {
		s.logger.WithError(err).Warn("sdlc: label reconciliation not started")
		return
	}
	s.runSDLCLabelReconcile(reconciler, cfg.Interval)
}

// sdlcOutcomeReserver returns the cross-node reservation over the durable shared
// store, or nil when there is none.
//
// NIL IS THE DOCUMENTED SINGLE-NODE BEHAVIOUR, NOT A FALLBACK TO A LOCAL STORE.
// An embedded or in-memory node has no durable store another node could also
// see, so a reservation kept in process memory would arbitrate nothing; the
// sweep then relies on the comment-marker check alone, which is idempotent
// within the one node that exists. A durable store that cannot decide a guarded
// write is refused by the constructor and handled the same way, with a warning.
func (s *Server) sdlcOutcomeReserver(durable interfaces.KVStore) sdlc.OutcomeReserver {
	if durable == nil {
		s.logger.Info("sdlc: no durable shared store; outcome comments are idempotent within this node only")
		return nil
	}
	reserver, err := sdlc.NewOutcomeReserver(durable, s.nodeName(), nil)
	if err != nil {
		s.logger.WithError(err).Warn("sdlc: outcome reservation unavailable; outcome comments are idempotent within this node only")
		return nil
	}
	return reserver
}

// sdlcReconcileTenants returns the tenant lister the sweep enumerates with.
//
// It is the SAME lister the task-source poller uses, because the sweep must
// reach exactly the projects the poller adopts work from: a reconciliation with
// narrower reach than the thing it reconciles leaves a class of issues that no
// pass ever visits, which is the defect wearing a fix's clothes.
func (s *Server) sdlcReconcileTenants() (orchestration.TenantLister, bool) {
	if s.iamComponents == nil || s.iamComponents.GetTenantManager() == nil {
		s.logger.Info("sdlc: label reconciliation not started: no tenant manager to enumerate tenants")
		return nil, false
	}
	return &iamTenantLister{tenants: s.iamComponents.GetTenantManager()}, true
}

// runSDLCLabelReconcile sweeps once at boot and then on a ticker until the
// node stops.
func (s *Server) runSDLCLabelReconcile(reconciler labelReconciler, interval time.Duration) {
	if interval <= 0 {
		interval = sdlc.DefaultLabelReconcileInterval
	}
	ctx, stop := context.WithCancel(context.Background())
	sweeps := &sdlcLabelSweeps{stop: stop, done: make(chan struct{})}
	s.sdlcLabelSweeps = sweeps
	go func() {
		defer close(sweeps.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		s.sweepSDLCLabels(ctx, reconciler)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.sweepSDLCLabels(ctx, reconciler)
			}
		}
	}()
	s.logger.Infof("sdlc: label reconciliation started (sweep=%s)", interval)
}

// sweepSDLCLabels runs one pass and logs what it found.
//
// A FAILED PASS IS A WARNING AND NOTHING MORE. The sweep repairs state that is
// already wrong; a pass that could not run leaves it wrong for another
// interval, which is what the previous behaviour did for ever.
func (s *Server) sweepSDLCLabels(ctx context.Context, reconciler labelReconciler) {
	report, err := reconciler.Reconcile(ctx)
	if err != nil && ctx.Err() == nil {
		s.logger.WithError(err).Warn("sdlc: label reconciliation pass failed; it is retried on the next sweep")
	}
	s.logSDLCLabelReconcile(report)
}

// stopSDLCLabelReconcile ends the sweep and waits for it.
func (s *Server) stopSDLCLabelReconcile() {
	if s.sdlcLabelSweeps == nil {
		return
	}
	s.sdlcLabelSweeps.stop()
	<-s.sdlcLabelSweeps.done
	s.sdlcLabelSweeps = nil
}

// logSDLCLabelReconcile records what a pass concluded.
//
// A CONSISTENT ISSUE IS SILENT and everything else is not. The verdicts that
// WROTE something rise to WARN because each is an ending that never reached the
// forge — the thing an operator wants to know happened, and wants to stop
// happening. The rest are INFO: they name work the sweep could not settle, and
// an operator reading them is reading a to-do list rather than an incident.
func (s *Server) logSDLCLabelReconcile(report sdlc.LabelReconcileReport) {
	for _, f := range report.Drifted() {
		line := "sdlc: label reconciliation " + string(f.Verdict) + " " + f.Subject()
		if f.TaskID != "" {
			line += " task=" + f.TaskID
		}
		// A restoration announces nothing, so it carries no outcome; printing
		// an empty one would read as an outcome named "".
		if f.Outcome != "" {
			line += " outcome=" + string(f.Outcome)
		}
		line += ": " + f.Evidence
		if f.Verdict.Wrote() {
			s.logger.Warn(line)
			continue
		}
		s.logger.Info(line)
	}
}
