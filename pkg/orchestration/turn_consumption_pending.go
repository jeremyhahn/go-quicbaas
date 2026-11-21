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
)

// consumptionRecordTimeout bounds one ledger write of a turn's consumption.
// The write runs on a context detached from the turn's cancellation: a turn
// whose caller went away still spent its tokens, and the spend cap reads
// them from the ledger.
const consumptionRecordTimeout = 10 * time.Second

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
}

// add keeps an entry whose ledger write failed.
func (p *pendingConsumption) add(entry ConsumptionEntry) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries = append(p.entries, entry)
}

// matching returns a copy of the entries of sessionID, and of accountID
// when it is set: a pooled account's other sessions spend the same cap. The
// entries stay pending until the ledger holds them, so a session that comes
// while another one retries an entry still sees it.
func (p *pendingConsumption) matching(sessionID, accountID string) []ConsumptionEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	var matched []ConsumptionEntry
	for _, entry := range p.entries {
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

// recordingContext returns a context for writing a turn's consumption: it
// keeps ctx's values (the tenant, the attribution) but not its cancellation,
// and it is bounded by consumptionRecordTimeout.
func recordingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), consumptionRecordTimeout)
}

// recordLedgerEntry writes one consumption entry, and keeps it pending when
// the write fails so the session's next turn retries it before the spend
// check. It reports whether the ledger holds the entry.
func (s *agentServiceImpl) recordLedgerEntry(ctx context.Context, entry ConsumptionEntry) bool {
	rctx, cancel := recordingContext(ctx)
	defer cancel()
	if err := s.ledger.Record(rctx, entry); err != nil {
		s.pendingLedger.add(entry)
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
// neither is admitted before the ledger holds it.
func (s *agentServiceImpl) flushPendingConsumption(ctx context.Context, sessionID string) error {
	if s.ledger == nil || sessionID == "" {
		return nil
	}
	accountID := ""
	if mem, ok := s.liveSessionRecord(sessionID); ok {
		accountID = mem.AccountID()
	}
	pending := s.pendingLedger.matching(sessionID, accountID)
	if len(pending) == 0 {
		return nil
	}
	rctx, cancel := recordingContext(ctx)
	defer cancel()
	var lastErr error
	failed := 0
	for _, entry := range pending {
		err := s.ledger.Record(rctx, entry)
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
	if failed == 0 {
		return nil
	}
	return &ConsumptionPendingError{SessionID: sessionID, AccountID: accountID, Pending: failed, Err: lastErr}
}
