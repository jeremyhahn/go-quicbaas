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

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// consumptionPendingKeyspace holds pooled consumption entries that are not
// yet recorded, laid out as orch/costpending/{tenant}/{account}/{entryID}. A
// pooled entry is held there from before its ledger write until the ledger
// holds it, so every node over the ledger's store sees a consumption another
// node has not yet recorded.
const consumptionPendingKeyspace = "orch/costpending/"

// pendingConsumptionStore is a ledger's shared list of pooled entries whose
// ledger write has not yet landed. kvConsumptionLedger implements it. With a
// ledger that does not, pending entries stay visible only to the node that
// wrote them.
type pendingConsumptionStore interface {
	MarkPending(ctx context.Context, entry ConsumptionEntry) error
	ClearPending(ctx context.Context, entry ConsumptionEntry) error
	PendingFor(ctx context.Context, tenantID, accountID string) ([]ConsumptionEntry, error)
}

var _ pendingConsumptionStore = (*kvConsumptionLedger)(nil)

// pendingKey is the shared pending marker key of one entry.
func (l *kvConsumptionLedger) pendingKey(tenantID, accountID, entryID string) string {
	return consumptionPendingKeyspace + tenantID + keySeparator + accountID + keySeparator + entryID
}

// MarkPending stores a pooled entry on the shared pending list before its
// ledger write, so a turn on any node over this store counts it.
func (l *kvConsumptionLedger) MarkPending(ctx context.Context, entry ConsumptionEntry) error {
	if l.kv == nil {
		return &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	if err := validateConsumptionEntry(entry); err != nil {
		return err
	}
	if entry.AccountID == "" {
		return &ConsumptionInvalidError{Field: "account_id", Message: "a pending marker needs a pooled account"}
	}
	key := l.pendingKey(entry.TenantID, entry.AccountID, entry.EntryID)
	data, err := json.Marshal(entry)
	if err != nil {
		return &ConsumptionWriteError{EntryID: entry.EntryID, Plane: consumptionPlanePending, Key: key, Cause: err}
	}
	if err := l.kv.Put(ctx, key, data); err != nil {
		return &ConsumptionWriteError{EntryID: entry.EntryID, Plane: consumptionPlanePending, Key: key, Cause: err}
	}
	return nil
}

// ClearPending removes an entry's shared pending marker once the ledger holds
// it. A marker that is already gone is success.
func (l *kvConsumptionLedger) ClearPending(ctx context.Context, entry ConsumptionEntry) error {
	if l.kv == nil {
		return &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	key := l.pendingKey(entry.TenantID, entry.AccountID, entry.EntryID)
	if err := l.kv.Delete(ctx, key); err != nil && !qerrors.IsNotFound(err) {
		return &ConsumptionWriteError{EntryID: entry.EntryID, Plane: consumptionPlanePending, Key: key, Cause: err}
	}
	return nil
}

// PendingFor lists the account's shared pending entries, written by any
// node. A decode or scan error is returned as is: the caller fails closed.
func (l *kvConsumptionLedger) PendingFor(ctx context.Context, tenantID, accountID string) ([]ConsumptionEntry, error) {
	if l.kv == nil {
		return nil, &ConsumptionInvalidError{Field: "store", Message: "kv store must not be nil"}
	}
	if err := requireSegment("tenant_id", tenantID); err != nil {
		return nil, err
	}
	if err := requireSegment("account_id", accountID); err != nil {
		return nil, err
	}
	var entries []ConsumptionEntry
	prefix := consumptionPendingKeyspace + tenantID + keySeparator + accountID + keySeparator
	err := l.scanEntries(ctx, prefix, func(_ string, entry ConsumptionEntry) error {
		entries = append(entries, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}
