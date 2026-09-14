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
	"sort"
	"sync"
	"time"
)

// DefaultShutdownDrainTimeout bounds the drain when the caller sets no
// deadline of its own.
//
// IT IS A BOUND, NOT A PROMISE TO FINISH. An agent turn runs for minutes; a
// daemon that waited for one would look hung to whatever is stopping it. The
// window is long enough for a loop that is already concluding to write its
// terminal status, and short enough that a stop is still a stop.
const DefaultShutdownDrainTimeout = 10 * time.Second

// Drain phases, as they appear in a DrainTimeoutError.
const (
	// DrainPhaseJobs is the wait for launched job goroutines.
	DrainPhaseJobs = "jobs"

	// DrainPhaseTurns is the wait for asynchronous agent turns.
	DrainPhaseTurns = "turns"
)

// Shutdown drains the service's running control loops and returns when they
// are done or when the bound expires, whichever comes first.
//
// WHY IT EXISTS. The daemon used to close its stores under running work: the
// job goroutines launchJob starts were tracked by nothing, so shutdown never
// waited for them, and a loop still writing met a closed store — which the
// engine reports by panicking (see StoreClosedError). The drain is what puts
// the wait back: new work is refused, work in flight is given a bounded
// window to conclude, and only then does the daemon go on to close storage.
//
// WHAT IT DOES NOT DO — and this is a deliberate decision, not an omission.
// It does NOT cancel the loops it is waiting for. Cancelling a loop that is
// mid-turn would make it return context.Canceled, which the envelope records
// as a FAILED job: work that was merely interrupted would be charged as work
// that failed, and its task's attempt budget spent on a restart. Instead a
// loop the window could not accommodate is left exactly as it stands, with a
// non-terminal record; RecoverJobs resumes it from that record on the next
// start. That is the same disposition the loop has today when the daemon is
// killed, and the one the SDLC runner's per-state resume expects.
//
// It is safe to call more than once; only the first call drains.
func (s *agentServiceImpl) Shutdown(ctx context.Context) error {
	if !s.draining.CompareAndSwap(false, true) {
		return nil
	}
	if _, bounded := ctx.Deadline(); !bounded {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultShutdownDrainTimeout)
		defer cancel()
	}

	// Jobs first, turns second: a running job is what starts a turn, so
	// waiting for the loops first gives the turns they own a chance to have
	// finished by the time the turn phase looks.
	if err := s.drainPhase(ctx, DrainPhaseJobs, &s.jobsInFlight, s.runningJobIDs); err != nil {
		return err
	}
	return s.drainPhase(ctx, DrainPhaseTurns, &s.turnsInFlight, nil)
}

// drainPhase waits for one group of goroutines to finish, and reports what it
// gave up on when the window closes first. running names what is still in
// flight for the log line, and may be nil when the phase cannot say.
func (s *agentServiceImpl) drainPhase(ctx context.Context, phase string, wg *sync.WaitGroup, running func() []string) error {
	started := time.Now()
	select {
	case <-waitGroupDone(wg):
		return nil
	case <-ctx.Done():
		timedOut := &DrainTimeoutError{
			Phase:  phase,
			Waited: time.Since(started).Round(time.Millisecond),
		}
		if running != nil {
			timedOut.Running = running()
		}
		s.logger.Warnf("orchestration: %s", timedOut.Error())
		return timedOut
	}
}

// runningJobIDs lists the jobs the service still has a goroutine for, keyed
// as tenant/job, in a stable order.
func (s *agentServiceImpl) runningJobIDs() []string {
	var ids []string
	s.jobs.Range(func(key, _ any) bool {
		if id, ok := key.(string); ok {
			ids = append(ids, id)
		}
		return true
	})
	sort.Strings(ids)
	return ids
}

// waitGroupDone returns a channel closed when wg reaches zero.
//
// The goroutine it starts outlives a drain that gave up, deliberately: it
// holds nothing but the wait, it ends when the work it is waiting for does,
// and the process it belongs to is already on its way out.
func waitGroupDone(wg *sync.WaitGroup) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}
