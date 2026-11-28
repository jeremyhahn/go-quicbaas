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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// turnAdmissionKeyspace holds one record per session with a turn between
// its spend check and the moment its consumption is visible to every node.
const turnAdmissionKeyspace = "orch/turnadmit/"

const (
	turnAdmissionTTL          = 2 * time.Minute // renewed every TTL/3 while the turn runs
	turnAdmissionAbandonAfter = time.Hour       // an expired record older than this is reclaimed
)

// turnAdmissionAcquireAttempts bounds the create/read race of one acquire:
// a record that vanishes between a lost create and its read is retried.
const turnAdmissionAcquireAttempts = 3

var (
	errTurnAdmissionBusy       = errors.New("turn admission held by a live turn")
	errTurnAdmissionUnrecorded = errors.New("turn admission holds consumption not yet recorded")
	errTurnAdmissionLost       = errors.New("turn admission record no longer held by this turn")
)

// turnAdmission is the record a turn holds in the ledger's store from before
// its spend check until its consumption is in the ledger or on the shared
// pending list. Every control-plane node over the store sees it.
type turnAdmission struct {
	Holder     string    `json:"holder"` // node + "/" + turnID
	SessionID  string    `json:"session_id"`
	ExpiresAt  time.Time `json:"expires_at"`
	Unrecorded bool      `json:"unrecorded,omitempty"`
}

// turnAdmissionStore serializes a session's turns across control-plane
// nodes and keeps a turn's unrecorded consumption visible to all of them.
type turnAdmissionStore interface {
	AcquireTurnAdmission(ctx context.Context, tenantID, accountID, sessionID, holder string, now time.Time) ([]byte, error)
	RenewTurnAdmission(ctx context.Context, tenantID, accountID, sessionID string, held []byte, now time.Time) ([]byte, error)
	MarkTurnAdmissionUnrecorded(ctx context.Context, tenantID, accountID, sessionID string, held []byte) ([]byte, error)
	ReleaseTurnAdmission(ctx context.Context, tenantID, accountID, sessionID string, held []byte) error
	StaleTurnAdmissions(ctx context.Context, tenantID, accountID string, now time.Time) (int, error)
}

var _ turnAdmissionStore = (*kvConsumptionLedger)(nil)

func (l *kvConsumptionLedger) turnAdmissionPrefix(tenantID, accountID string) (string, error) {
	if err := requireSegment("tenant_id", tenantID); err != nil {
		return "", err
	}
	if err := requireSegment("account_id", accountID); err != nil {
		return "", err
	}
	return turnAdmissionKeyspace + tenantID + keySeparator + accountID + keySeparator, nil
}

func (l *kvConsumptionLedger) turnAdmissionKey(tenantID, accountID, sessionID string) (string, error) {
	prefix, err := l.turnAdmissionPrefix(tenantID, accountID)
	if err != nil {
		return "", err
	}
	if err := requireSegment("session_id", sessionID); err != nil {
		return "", err
	}
	return prefix + sessionID, nil
}

// turnAdmissionGuard refuses a store that cannot decide a contested write,
// as Record does: an admission that is not exclusive serializes nothing.
func (l *kvConsumptionLedger) turnAdmissionGuard(needSwap, needDelete bool) error {
	if l.kv == nil {
		return &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	if l.creator == nil || (needSwap && l.swapper == nil) || (needDelete && l.deleter == nil) {
		return &ConsumptionUnguardedError{
			Message: "exactly-once accounting needs a store that can decide a create; this one cannot",
		}
	}
	return nil
}

func decodeTurnAdmission(key string, raw []byte) (turnAdmission, error) {
	var held turnAdmission
	if err := json.Unmarshal(raw, &held); err != nil {
		return turnAdmission{}, fmt.Errorf("decode turn admission %s: %w", key, err)
	}
	return held, nil
}

// AcquireTurnAdmission creates the session's admission record for holder and
// returns its stored bytes. A live record of another turn is
// errTurnAdmissionBusy; an unrecorded or expired one is
// errTurnAdmissionUnrecorded; one expired for longer than
// turnAdmissionAbandonAfter is reclaimed.
func (l *kvConsumptionLedger) AcquireTurnAdmission(ctx context.Context, tenantID, accountID, sessionID, holder string, now time.Time) ([]byte, error) {
	// Renewal and the unrecorded mark swap the record: a store without
	// compare-and-swap could admit a turn it can never keep.
	if err := l.turnAdmissionGuard(true, true); err != nil {
		return nil, err
	}
	key, err := l.turnAdmissionKey(tenantID, accountID, sessionID)
	if err != nil {
		return nil, err
	}
	mine, err := json.Marshal(turnAdmission{Holder: holder, SessionID: sessionID, ExpiresAt: now.Add(turnAdmissionTTL).UTC()})
	if err != nil {
		return nil, fmt.Errorf("encode turn admission %s: %w", key, err)
	}
	for attempt := 0; attempt < turnAdmissionAcquireAttempts; attempt++ {
		won, err := l.creator.PutIfAbsent(ctx, key, mine)
		if err != nil {
			return nil, fmt.Errorf("create turn admission %s: %w", key, err)
		}
		if won {
			return mine, nil
		}
		raw, err := l.kv.Get(ctx, key)
		if err != nil {
			if qerrors.IsNotFound(err) {
				continue
			}
			return nil, fmt.Errorf("read turn admission %s: %w", key, err)
		}
		held, err := decodeTurnAdmission(key, raw)
		if err != nil {
			return nil, err
		}
		switch {
		case now.After(held.ExpiresAt.Add(turnAdmissionAbandonAfter)):
			removed, err := l.deleter.CompareAndDelete(ctx, key, raw)
			if err != nil {
				return nil, fmt.Errorf("reclaim turn admission %s: %w", key, err)
			}
			if !removed {
				return nil, errTurnAdmissionBusy
			}
			won, err := l.creator.PutIfAbsent(ctx, key, mine)
			if err != nil {
				return nil, fmt.Errorf("create turn admission %s: %w", key, err)
			}
			if !won {
				return nil, errTurnAdmissionBusy
			}
			return mine, nil
		case held.Unrecorded || now.After(held.ExpiresAt):
			return nil, errTurnAdmissionUnrecorded
		default:
			return nil, errTurnAdmissionBusy
		}
	}
	return nil, errTurnAdmissionBusy
}

// RenewTurnAdmission moves the held record's expiry to now+TTL.
func (l *kvConsumptionLedger) RenewTurnAdmission(ctx context.Context, tenantID, accountID, sessionID string, held []byte, now time.Time) ([]byte, error) {
	return l.swapTurnAdmission(ctx, tenantID, accountID, sessionID, held, func(rec *turnAdmission) {
		rec.ExpiresAt = now.Add(turnAdmissionTTL).UTC()
	})
}

// MarkTurnAdmissionUnrecorded marks the held record as holding consumption
// that is in neither the ledger nor the shared pending list.
func (l *kvConsumptionLedger) MarkTurnAdmissionUnrecorded(ctx context.Context, tenantID, accountID, sessionID string, held []byte) ([]byte, error) {
	return l.swapTurnAdmission(ctx, tenantID, accountID, sessionID, held, func(rec *turnAdmission) {
		rec.Unrecorded = true
	})
}

func (l *kvConsumptionLedger) swapTurnAdmission(ctx context.Context, tenantID, accountID, sessionID string, held []byte, edit func(*turnAdmission)) ([]byte, error) {
	if err := l.turnAdmissionGuard(true, false); err != nil {
		return nil, err
	}
	key, err := l.turnAdmissionKey(tenantID, accountID, sessionID)
	if err != nil {
		return nil, err
	}
	rec, err := decodeTurnAdmission(key, held)
	if err != nil {
		return nil, err
	}
	edit(&rec)
	next, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("encode turn admission %s: %w", key, err)
	}
	swapped, err := l.swapper.CompareAndSwap(ctx, key, held, next)
	if err != nil {
		return nil, fmt.Errorf("update turn admission %s: %w", key, err)
	}
	if !swapped {
		return nil, fmt.Errorf("update turn admission %s: %w", key, errTurnAdmissionLost)
	}
	return next, nil
}

// ReleaseTurnAdmission deletes the held record. A record that is no longer
// the one held is not an error: a reclaimer owns the key now.
func (l *kvConsumptionLedger) ReleaseTurnAdmission(ctx context.Context, tenantID, accountID, sessionID string, held []byte) error {
	if err := l.turnAdmissionGuard(false, true); err != nil {
		return err
	}
	key, err := l.turnAdmissionKey(tenantID, accountID, sessionID)
	if err != nil {
		return err
	}
	if _, err := l.deleter.CompareAndDelete(ctx, key, held); err != nil {
		return fmt.Errorf("release turn admission %s: %w", key, err)
	}
	return nil
}

// StaleTurnAdmissions counts the account's records that are unrecorded or
// expired. A record expired for longer than turnAdmissionAbandonAfter is not
// counted: the next acquire of its session reclaims it.
func (l *kvConsumptionLedger) StaleTurnAdmissions(ctx context.Context, tenantID, accountID string, now time.Time) (int, error) {
	if l.kv == nil {
		return 0, &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	prefix, err := l.turnAdmissionPrefix(tenantID, accountID)
	if err != nil {
		return 0, err
	}
	stale := 0
	err = l.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		held, err := decodeTurnAdmission(key, value)
		if err != nil {
			return err
		}
		if now.After(held.ExpiresAt.Add(turnAdmissionAbandonAfter)) {
			return nil
		}
		if held.Unrecorded || now.After(held.ExpiresAt) {
			stale++
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("scan turn admissions %s: %w", prefix, err)
	}
	return stale, nil
}
