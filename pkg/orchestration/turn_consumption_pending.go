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
	"strconv"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// consumptionRecordTimeout bounds one ledger write of a turn's consumption.
// The write runs on a context detached from the turn's cancellation: a turn
// whose caller went away still spent its tokens, and the spend cap reads
// them from the ledger.
const consumptionRecordTimeout = 10 * time.Second

// pendingRetryFirst and pendingRetryMax bound the background drainer's
// backoff: it first retries pending consumption after pendingRetryFirst and
// doubles the wait each round up to pendingRetryMax.
const (
	pendingRetryFirst = 5 * time.Second
	pendingRetryMax   = 2 * time.Minute
)

// ErrConsumptionPending indicates a turn refused because an earlier turn's
// consumption is not yet in the ledger, so the spend cap cannot be read.
var ErrConsumptionPending = errors.New("account: an earlier turn's consumption is not recorded")

// ConsumptionPendingError reports a turn refused because the ledger write of
// an earlier turn on the same session or account failed and its retry failed
// again. The spend cap reads the ledger; admitting the turn would decide on
// a sum that leaves out tokens already spent.
type ConsumptionPendingError struct {
	SessionID string
	AccountID string

	// Pending is how many entries still wait for the ledger.
	Pending int

	// Err is the last ledger error.
	Err error
}

// Error implements the error interface.
func (e *ConsumptionPendingError) Error() string {
	msg := ErrConsumptionPending.Error() + ": session " + e.SessionID
	if e.AccountID != "" {
		msg += " account " + e.AccountID
	}
	msg += ": " + strconv.Itoa(e.Pending) + " entries pending"
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ConsumptionPendingError) Unwrap() error { return ErrConsumptionPending }

// pendingConsumption holds the consumption entries whose ledger write
// failed, until a retry stores them. Record is idempotent on the entry id,
// so a retry after an ambiguous failure converges instead of double-charging.
type pendingConsumption struct {
	mu      sync.Mutex
	entries []ConsumptionEntry

	// draining is true while a drainer goroutine is live
	// (drainPendingConsumption); at most one runs at a time.
	draining bool
	// firstRetry is the drainer's first wait. Zero means pendingRetryFirst;
	// tests set it lower.
	firstRetry time.Duration
	// stop, closed once by stopDrainer at shutdown, ends the drainer.
	stop     chan struct{}
	stopOnce sync.Once
	// stopped is set by stopDrainer; no drainer is claimed after it.
	stopped bool
	// drainers counts the live drainer goroutines, so shutdown can wait
	// for them. It is added to under mu, and never once stopped is set.
	drainers sync.WaitGroup
	// drainCtx is the drainer's retry context; stopDrainer cancels it.
	drainCtx    context.Context
	drainCancel context.CancelFunc
}

// initLocked creates stop and the drainer's retry context if they are not
// yet set. p.mu must be held.
func (p *pendingConsumption) initLocked() {
	if p.stop == nil {
		p.stop = make(chan struct{})
	}
	if p.drainCtx == nil {
		p.drainCtx, p.drainCancel = context.WithCancel(context.Background())
	}
}

// add keeps an entry whose ledger write failed.
func (p *pendingConsumption) add(entry ConsumptionEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = append(p.entries, entry)
}

// matching returns a copy of tenantID's entries of sessionID, and of
// accountID when it is set: a pooled account's other sessions spend the same
// cap. Account IDs are scoped to a tenant, so the tenant is part of the
// match: an entry of another tenant never matches. The entries stay pending
// until the ledger holds them, so a session that comes while another one
// retries an entry still sees it.
func (p *pendingConsumption) matching(tenantID, sessionID, accountID string) []ConsumptionEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	var matched []ConsumptionEntry
	for _, entry := range p.entries {
		if entry.TenantID != tenantID {
			continue
		}
		if entry.SessionID == sessionID || (accountID != "" && entry.AccountID == accountID) {
			matched = append(matched, entry)
		}
	}
	return matched
}

// remove deletes the entry with entryID once the ledger holds it, and
// reports whether it was still pending.
func (p *pendingConsumption) remove(entryID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, entry := range p.entries {
		if entry.EntryID == entryID {
			p.entries = append(p.entries[:i:i], p.entries[i+1:]...)
			return true
		}
	}
	return false
}

// size reports how many entries wait for the ledger.
func (p *pendingConsumption) size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.entries)
}

// all returns a copy of every pending entry.
func (p *pendingConsumption) all() []ConsumptionEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ConsumptionEntry, len(p.entries))
	copy(out, p.entries)
	return out
}

// claimDrainer marks a drainer live. It reports true only to the caller that
// turned draining on, so at most one drainer runs, and false once
// stopDrainer has run. On true it has added the drainer to p.drainers: the
// caller must start a goroutine that calls p.drainers.Done when it ends.
func (p *pendingConsumption) claimDrainer() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initLocked()
	if p.stopped || p.draining {
		return false
	}
	p.draining = true
	p.drainers.Add(1)
	return true
}

// reclaimDrainer is claimDrainer for the live drainer, which keeps going
// after releaseDrainerIfEmpty returned false: it does not add to p.drainers.
func (p *pendingConsumption) reclaimDrainer() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.draining {
		return false
	}
	p.draining = true
	return true
}

// releaseDrainerIfEmpty clears draining and reports whether no entry is
// pending. A drainer that gets false re-claims, so an entry added between its
// retry and this call is not left without one.
func (p *pendingConsumption) releaseDrainerIfEmpty() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.draining = false
	return len(p.entries) == 0
}

// stopChan returns the channel stopDrainer closes.
func (p *pendingConsumption) stopChan() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initLocked()
	return p.stop
}

// drainContext returns the drainer's retry context, which stopDrainer
// cancels.
func (p *pendingConsumption) drainContext() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.initLocked()
	return p.drainCtx
}

// stopDrainer ends the drainer, if one runs: it cancels a retry in progress
// and keeps any drainer from starting or waiting again.
func (p *pendingConsumption) stopDrainer() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopped = true
	p.initLocked()
	p.stopOnce.Do(func() { close(p.stop) })
	p.drainCancel()
}

// waitDrainer waits for every drainer goroutine to end. Call it only after
// stopDrainer, so that none can start meanwhile.
func (p *pendingConsumption) waitDrainer() {
	p.drainers.Wait()
}

// recordingContext returns a context for writing a turn's consumption: it
// keeps ctx's values (the tenant, the attribution) but not its cancellation,
// and it is bounded by consumptionRecordTimeout.
func recordingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), consumptionRecordTimeout)
}

// recordLedgerEntry writes one consumption entry, and keeps it pending when
// the write fails: the session's next turn retries it before the spend
// check, and a background drainer (drainPendingConsumption) retries it with
// backoff, so an entry whose session has ended still reaches the ledger. It
// reports whether the ledger holds the entry.
func (s *agentServiceImpl) recordLedgerEntry(ctx context.Context, entry ConsumptionEntry) bool {
	rctx, cancel := recordingContext(ctx)
	defer cancel()
	if err := s.ledger.Record(rctx, entry); err != nil {
		s.pendingLedger.add(entry)
		s.startPendingDrain()
		s.logger.Warnf(
			"orchestration: failed to record consumption; spend reports and caps are under-counting until it is retried, and the session's next turn waits for it: session=%s account=%s entry=%s err=%s",
			entry.SessionID, entry.AccountID, entry.EntryID, err.Error())
		return false
	}
	return true
}

// flushPendingConsumption retries every pending entry of sessionID and of
// its pooled account. It returns a *ConsumptionPendingError while any of
// them is still not in the ledger: the turn is refused rather than admitted
// against a spend sum that leaves those tokens out. An entry stays pending
// while it is retried, so a session that finds an entry another session is
// retrying retries it too (Record is idempotent on the entry id), and
// neither is admitted before the ledger holds it. The background drainer
// retries the same entries on its own schedule; whichever write lands first
// removes the entry. Only the session's own tenant's entries are retried, and
// a caller of another tenant is refused as *SessionNotFoundError before any
// retry. The retry runs on the caller's ctx and holds the session's turn lock,
// so it ends with the caller: a cancelled or expired ctx stops it, leaves the
// entries it did not store pending for the drainer, and is returned as
// ctx.Err().
func (s *agentServiceImpl) flushPendingConsumption(ctx context.Context, sessionID string) error {
	if s.ledger == nil || sessionID == "" {
		return nil
	}
	tenantID := TenantFromContext(ctx)
	accountID := ""
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		if caller := tenantID; caller != "" && caller != mem.TenantID {
			return &SessionNotFoundError{SessionID: sessionID}
		}
		accountID = mem.AccountID()
		tenantID = mem.TenantID
	}
	pending := s.pendingLedger.matching(tenantID, sessionID, accountID)
	if len(pending) == 0 {
		return nil
	}
	failed, lastErr := s.retryPendingEntries(ctx, pending)
	if failed == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		// The caller went away or ran out of time: the turn ends with its
		// own cancellation, not a pending-consumption fault. The entries
		// not stored stay pending for the drainer.
		return err
	}
	return &ConsumptionPendingError{SessionID: sessionID, AccountID: accountID, Pending: failed, Err: lastErr}
}

// retryPendingEntries writes each pending entry once more, each under its own
// tenant and bounded by consumptionRecordTimeout within ctx's deadline. Once
// ctx is done, the remaining entries are counted as failed and are not tried.
// A caller that must not inherit ctx's cancellation passes a detached ctx.
// An entry the ledger now holds is removed and counted; one that conflicts
// with the stored entry is removed and logged; any other failure leaves it
// pending. It returns how many are still pending and the last error.
func (s *agentServiceImpl) retryPendingEntries(ctx context.Context, entries []ConsumptionEntry) (failed int, lastErr error) {
	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			failed += len(entries) - i
			lastErr = err
			break
		}
		ectx, cancel := context.WithTimeout(
			ContextWithTenant(tenantctx.WithTenant(ctx, entry.TenantID), entry.TenantID),
			consumptionRecordTimeout)
		err := s.ledger.Record(ectx, entry)
		cancel()
		var conflict *ConsumptionConflictError
		switch {
		case err == nil:
			// Counted once the ledger holds it, as recordConsumption does,
			// and only by the retry that removes it: two sessions retrying
			// one entry count it once.
			if s.pendingLedger.remove(entry.EntryID) {
				countFactoryTurn(metrics.GetMetrics(), entry, s.prices.Lists(entry.TenantID, entry.Engine, entry.Model))
			}
		case errors.As(err, &conflict):
			// The id is already stored with a different record: the
			// ledger holds a consumption for it, and retrying cannot
			// change that. Logged, not kept.
			s.pendingLedger.remove(entry.EntryID)
			s.logger.Warnf(
				"orchestration: pending consumption conflicts with the stored entry; dropped: session=%s entry=%s err=%s",
				entry.SessionID, entry.EntryID, err.Error())
		default:
			// Still pending: it never left the list.
			lastErr = err
			failed++
		}
	}
	return failed, lastErr
}

// startPendingDrain starts the background drainer unless one is live, the
// service is draining, or there is no ledger.
func (s *agentServiceImpl) startPendingDrain() {
	if s.ledger != nil && !s.draining.Load() && s.pendingLedger.claimDrainer() {
		go s.drainPendingConsumption()
	}
}

// drainPendingConsumption retries every pending entry, independently of any
// turn, until none is left or the service shuts down. It waits firstRetry
// (pendingRetryFirst when unset) before the first round and doubles the wait
// each round up to pendingRetryMax. An entry whose session has closed, or
// whose account takes no more turns, is retried here and nowhere else.
func (s *agentServiceImpl) drainPendingConsumption() {
	defer s.pendingLedger.drainers.Done()
	wait := s.pendingLedger.firstRetry
	if wait <= 0 {
		wait = pendingRetryFirst
	}
	stop := s.pendingLedger.stopChan()
	rctx := s.pendingLedger.drainContext()
	for {
		select {
		case <-time.After(wait):
		case <-stop:
			s.pendingLedger.releaseDrainerIfEmpty()
			return
		}
		failed, lastErr := s.retryPendingEntries(rctx, s.pendingLedger.all())
		if rctx.Err() != nil {
			// Shutdown stopped the drainer mid-retry.
			s.pendingLedger.releaseDrainerIfEmpty()
			return
		}
		if s.pendingLedger.releaseDrainerIfEmpty() {
			return
		}
		if !s.pendingLedger.reclaimDrainer() {
			return
		}
		if failed > 0 && lastErr != nil {
			s.logger.Warnf(
				"orchestration: pending consumption still not recorded; spend reports and caps are under-counting until it is: pending=%d err=%s",
				failed, lastErr.Error())
		}
		wait *= 2
		if wait > pendingRetryMax {
			wait = pendingRetryMax
		}
		if s.draining.Load() {
			s.pendingLedger.releaseDrainerIfEmpty()
			return
		}
	}
}
