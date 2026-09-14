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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// =============================================================================
// The SDLC backstops
// =============================================================================
//
// BOTH HALVES OF THE FACTORY ARE WOKEN BY THE FORGE, AND NEITHER MAY DEPEND ON
// IT. A verified delivery triages an issue or reviews a pull request within a
// second; THE BACKSTOPS ARE FOR WHEN THAT DID NOT HAPPEN — a hook nobody
// provisioned, a forge that was down, a daemon that was restarting through the
// delivery. Without them an issue filed during any of those is untriaged for
// ever and a change that went green is unreviewed for ever, and both are
// exactly the things nobody is watching.
//
// THE TWO RUN ON ONE GOROUTINE AND TWO TICKERS, because they are the same kind
// of work at different prices. A triage pass starts a JOB per bound project; a
// review pass makes forge reads and starts work only for a pull request that is
// genuinely unjudged. Sharing a cadence would either make triage as frequent as
// the cheap pass or the review floor as slow as the expensive one, so each
// keeps its own interval — and a third goroutine buys nothing, since neither
// pass blocks on anything but the forge.

// sdlcBackstops is the running loop and the means to stop it.
type sdlcBackstops struct {
	stop context.CancelFunc
	done chan struct{}
}

// sdlcBackstopSweeper is one backstop pass as the daemon drives it: visit
// every bound project of every tenant, report how many it acted on, and hand
// back what it could not do.
//
// An interface so the loop's cadence, its boot pass and its shutdown can be
// proven without a forge or an engine. sdlc.TriageScheduler and
// sdlc.ReviewSweeper both satisfy it.
type sdlcBackstopSweeper interface {
	Sweep(ctx context.Context, tenants orchestration.TenantLister) (int, error)
	SweepFailures() []error
}

// sdlcBackstop is one backstop as the loop holds it: what to call it in the
// log, what its count means, how often it runs, and what runs it.
type sdlcBackstop struct {
	what     string
	counted  string
	interval time.Duration
	sweeper  sdlcBackstopSweeper
}

// startSDLCBackstops runs both backstops until shutdown.
//
// A NODE THAT CANNOT ENUMERATE TENANTS GETS NEITHER, and says so. The webhook
// path still works — it carries its own tenant in the route index — so the
// module degrades to delivery-driven rather than stopping, and the log says
// which of the two a reader is looking at.
func (s *Server) startSDLCBackstops(triage, review sdlcBackstopSweeper) {
	if triage == nil && review == nil {
		return
	}
	tenants, ok := s.sdlcBackstopTenants()
	if !ok {
		return
	}
	s.runSDLCBackstops(
		sdlcBackstop{
			what: "triage backstop", counted: "nudged",
			interval: s.config.SDLC.TriageSweepInterval, sweeper: triage,
		},
		sdlcBackstop{
			what: "review floor", counted: "started a review for",
			interval: s.config.SDLC.ReviewSweepInterval, sweeper: review,
		},
		tenants,
	)
}

// sdlcBackstopTenants is the tenant source the backstops walk.
func (s *Server) sdlcBackstopTenants() (orchestration.TenantLister, bool) {
	if s.iamComponents == nil || s.iamComponents.GetTenantManager() == nil {
		s.logger.Info("sdlc: backstops not started: no tenant manager to enumerate tenants " +
			"(webhook-driven triage and review are unaffected)")
		return nil, false
	}
	return &iamTenantLister{tenants: s.iamComponents.GetTenantManager()}, true
}

// runSDLCBackstops starts the loop: one pass each at boot, then on each
// backstop's own tick.
//
// THE BOOT PASS IS THE POINT. The deliveries this node missed are exactly the
// ones that arrived while it was down, so waiting a whole interval before the
// first pass would leave them unacted-on for precisely the window the
// backstops exist to close.
//
// A BACKSTOP WITH NO SWEEPER IS LEFT OUT RATHER THAN FAKED: its ticker channel
// stays nil, which a select never chooses, so a node that mounts one half of
// the module runs one backstop and does not pretend to run the other.
func (s *Server) runSDLCBackstops(triage, review sdlcBackstop, tenants orchestration.TenantLister) {
	triage.interval = backstopInterval(triage.interval, sdlc.DefaultTriageSweepInterval)
	review.interval = backstopInterval(review.interval, sdlc.DefaultReviewSweepInterval)

	ctx, stop := context.WithCancel(context.Background())
	backstops := &sdlcBackstops{stop: stop, done: make(chan struct{})}
	s.sdlcBackstops = backstops
	go func() {
		defer close(backstops.done)
		triageTicks, stopTriage := backstopTicker(triage)
		defer stopTriage()
		reviewTicks, stopReview := backstopTicker(review)
		defer stopReview()

		s.sweepSDLCBackstop(ctx, triage, tenants)
		s.sweepSDLCBackstop(ctx, review, tenants)
		for {
			select {
			case <-ctx.Done():
				return
			case <-triageTicks:
				s.sweepSDLCBackstop(ctx, triage, tenants)
			case <-reviewTicks:
				s.sweepSDLCBackstop(ctx, review, tenants)
			}
		}
	}()
	s.logger.Infof("sdlc: backstops started (triage=%s review=%s)", triage.interval, review.interval)
}

// backstopInterval takes the module's default for an interval an operator
// left unset or set to nonsense, so a misconfiguration is neither a hot loop
// nor a dead one.
func backstopInterval(configured, fallback time.Duration) time.Duration {
	if configured <= 0 {
		return fallback
	}
	return configured
}

// backstopTicker is the tick channel for one backstop, and a nil channel for a
// backstop with no sweeper.
func backstopTicker(backstop sdlcBackstop) (<-chan time.Time, func()) {
	if backstop.sweeper == nil {
		return nil, func() {}
	}
	ticker := time.NewTicker(backstop.interval)
	return ticker.C, ticker.Stop
}

// sweepSDLCBackstop runs one pass and records what it could not do.
func (s *Server) sweepSDLCBackstop(ctx context.Context, backstop sdlcBackstop, tenants orchestration.TenantLister) {
	if backstop.sweeper == nil {
		return
	}
	acted, err := backstop.sweeper.Sweep(ctx, tenants)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		s.logger.WithError(err).Warnf("sdlc: %s pass did not complete", backstop.what)
		return
	}
	// The per-project failures are drained whether or not the pass as a
	// whole succeeded: a pass that visited nine projects and failed on the
	// tenth must say so, and a failure left in the slice would be reported
	// again on every later pass.
	for _, failure := range backstop.sweeper.SweepFailures() {
		s.logger.WithError(failure).Infof("sdlc: %s skipped a project", backstop.what)
	}
	if acted > 0 {
		s.logger.Debugf("sdlc: %s %s %d bound project(s)", backstop.what, backstop.counted, acted)
	}
}

// stopSDLCBackstops ends the loop and waits for it.
func (s *Server) stopSDLCBackstops() {
	if s.sdlcBackstops == nil {
		return
	}
	s.sdlcBackstops.stop()
	<-s.sdlcBackstops.done
	s.sdlcBackstops = nil
}
