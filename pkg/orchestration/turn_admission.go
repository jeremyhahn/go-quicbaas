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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	turnAdmissionFirstWait = 50 * time.Millisecond
	turnAdmissionMaxWait   = time.Second
)

// turnAdmissionHold is a turn's claim on its session's admission record
// (account_ledger_admission.go). It renews the record while the turn runs,
// and is released once the turn's consumption is visible to every node, or
// kept, marked unrecorded, while it is not.
type turnAdmissionHold struct {
	store     turnAdmissionStore
	logger    interfaces.Logger
	tenantID  string
	accountID string
	sessionID string

	// base carries the admitting context's values, never its cancellation:
	// the record outlives the caller's request.
	base context.Context

	mu sync.Mutex
	// raw is the record as stored; nil once released.
	raw  []byte
	kept bool

	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

func newTurnAdmissionHold(ctx context.Context, store turnAdmissionStore, logger interfaces.Logger, tenantID, accountID, sessionID string, raw []byte) *turnAdmissionHold {
	hold := &turnAdmissionHold{
		store:     store,
		logger:    logger,
		tenantID:  tenantID,
		accountID: accountID,
		sessionID: sessionID,
		base:      context.WithoutCancel(ctx),
		raw:       raw,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go hold.renew()
	return hold
}

// renew moves the record's expiry forward every TTL/3 until stopped. A
// failed renewal is logged and retried on the next tick.
func (h *turnAdmissionHold) renew() {
	defer close(h.done)
	ticker := time.NewTicker(turnAdmissionTTL / 3)
	defer ticker.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-ticker.C:
		}
		h.mu.Lock()
		if h.raw != nil {
			rctx, cancel := recordingContext(h.base)
			next, err := h.store.RenewTurnAdmission(rctx, h.tenantID, h.accountID, h.sessionID, h.raw, time.Now().UTC())
			cancel()
			if err != nil {
				h.warnf("orchestration: turn admission not renewed; it expires unless a later renewal lands: session=%s account=%s err=%s",
					h.sessionID, h.accountID, err.Error())
			} else {
				h.raw = next
			}
		}
		h.mu.Unlock()
	}
}

func (h *turnAdmissionHold) warnf(format string, args ...interface{}) {
	if h.logger != nil {
		h.logger.Warnf(format, args...)
	}
}

// stopRenewal ends the renewal goroutine and waits for it. Idempotent.
func (h *turnAdmissionHold) stopRenewal() {
	h.stopOnce.Do(func() { close(h.stop) })
	<-h.done
}

// release deletes the admission record. A nil hold, or one already
// released, is a no-op.
func (h *turnAdmissionHold) release(ctx context.Context) {
	if h == nil {
		return
	}
	h.stopRenewal()
	h.mu.Lock()
	raw := h.raw
	h.raw = nil
	h.mu.Unlock()
	if raw == nil {
		return
	}
	rctx, cancel := recordingContext(ctx)
	defer cancel()
	if err := h.store.ReleaseTurnAdmission(rctx, h.tenantID, h.accountID, h.sessionID, raw); err != nil {
		h.warnf("orchestration: turn admission not released; the session waits until it expires: session=%s account=%s err=%s",
			h.sessionID, h.accountID, err.Error())
	}
}

// keep stops renewing the record and marks it unrecorded: the turn's
// consumption is in neither the ledger nor the shared pending list, so every
// node refuses the account's turns until it is. If the mark fails the record
// still expires after the TTL, and an expired record refuses as well.
func (h *turnAdmissionHold) keep(ctx context.Context) {
	if h == nil {
		return
	}
	h.stopRenewal()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.kept = true
	if h.raw == nil {
		return
	}
	rctx, cancel := recordingContext(ctx)
	defer cancel()
	next, err := h.store.MarkTurnAdmissionUnrecorded(rctx, h.tenantID, h.accountID, h.sessionID, h.raw)
	if err != nil {
		h.warnf("orchestration: turn admission not marked unrecorded; other nodes refuse the account once it expires: session=%s account=%s err=%s",
			h.sessionID, h.accountID, err.Error())
		return
	}
	h.raw = next
}

// releaseUnlessKept releases the record unless keep claimed it for an
// unrecorded consumption.
func (h *turnAdmissionHold) releaseUnlessKept(ctx context.Context) {
	if h == nil {
		return
	}
	h.mu.Lock()
	kept := h.kept
	h.mu.Unlock()
	if kept {
		return
	}
	h.release(ctx)
}

// admitTurn claims the session's admission record in the ledger's shared
// store, so a turn on any control-plane node over that store waits for this
// one to be recorded. waited reports that another turn held it first: its
// consumption may have landed since the caller's last pending check.
//
// A ledger that keeps no admission records, or a session with no pooled
// account, admits without a hold. An unrecorded or expired record refuses
// the turn as pending consumption; a store fault refuses it as an
// unavailable spend check. Neither refusal is run or charged.
func (s *agentServiceImpl) admitTurn(ctx context.Context, sessionID, turnID string) (*turnAdmissionHold, bool, error) {
	store, ok := s.ledger.(turnAdmissionStore)
	if !ok || sessionID == "" {
		return nil, false, nil
	}
	tenantID, accountID, err := s.turnSessionAccount(ctx, sessionID)
	if err != nil {
		return nil, false, spendCheckFault(ctx, sessionID, "", err)
	}
	if accountID == "" {
		return nil, false, nil
	}
	holder := s.node + "/" + turnID
	wait := turnAdmissionFirstWait
	waited := false
	for {
		raw, err := store.AcquireTurnAdmission(ctx, tenantID, accountID, sessionID, holder, time.Now().UTC())
		switch {
		case err == nil:
			return newTurnAdmissionHold(ctx, store, s.logger, tenantID, accountID, sessionID, raw), waited, nil
		case errors.Is(err, errTurnAdmissionBusy):
			waited = true
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, waited, ctx.Err()
			case <-timer.C:
			}
			wait *= 2
			if wait > turnAdmissionMaxWait {
				wait = turnAdmissionMaxWait
			}
		case errors.Is(err, errTurnAdmissionUnrecorded):
			return nil, waited, &ConsumptionPendingError{SessionID: sessionID, AccountID: accountID, Pending: 1, Err: err}
		default:
			return nil, waited, spendCheckFault(ctx, sessionID, accountID, err)
		}
	}
}
