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
	stderrors "errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// admissionCreateOnlyKV exposes a KV's plain verbs and its guarded create, but no
// compare-and-swap or compare-and-delete.
type admissionCreateOnlyKV struct {
	interfaces.KVStore
	creator guardedCreator
}

func (k admissionCreateOnlyKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return k.creator.PutIfAbsent(ctx, key, value)
}

func decodeAdmissionForTest(t *testing.T, raw []byte) turnAdmission {
	t.Helper()
	var rec turnAdmission
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("decode admission: %v", err)
	}
	return rec
}

func TestTurnAdmission_LiveStaleAbandoned(t *testing.T) {
	ctx := context.Background()
	store, ok := NewConsumptionLedger(NewMemoryTaskKV()).(turnAdmissionStore)
	if !ok {
		t.Fatal("the KV ledger keeps no turn admissions")
	}
	const tenant, account, session = "tenant-a", "acct-1", "sess-1"
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	stale := func(at time.Time) int {
		t.Helper()
		n, err := store.StaleTurnAdmissions(ctx, tenant, account, at)
		if err != nil {
			t.Fatalf("StaleTurnAdmissions: %v", err)
		}
		return n
	}

	// Live: one holder at a time.
	held, err := store.AcquireTurnAdmission(ctx, tenant, account, session, "h1", now)
	if err != nil {
		t.Fatalf("h1 acquire: %v", err)
	}
	if _, err := store.AcquireTurnAdmission(ctx, tenant, account, session, "h2", now); !stderrors.Is(err, errTurnAdmissionBusy) {
		t.Fatalf("h2 acquire on a live record = %v, want busy", err)
	}
	if n := stale(now); n != 0 {
		t.Fatalf("stale with a live record = %d, want 0", n)
	}
	renewed, err := store.RenewTurnAdmission(ctx, tenant, account, session, held, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if got, want := decodeAdmissionForTest(t, renewed).ExpiresAt, now.Add(time.Minute+turnAdmissionTTL); !got.Equal(want) {
		t.Fatalf("renewed expiry = %v, want %v", got, want)
	}
	if _, err := store.RenewTurnAdmission(ctx, tenant, account, session, held, now); err == nil {
		t.Fatal("renew with a superseded record succeeded")
	}
	if err := store.ReleaseTurnAdmission(ctx, tenant, account, session, renewed); err != nil {
		t.Fatalf("release: %v", err)
	}
	held, err = store.AcquireTurnAdmission(ctx, tenant, account, session, "h2", now)
	if err != nil {
		t.Fatalf("h2 acquire after release: %v", err)
	}

	// Expired: refused as unrecorded and counted, until it is abandoned.
	expired := now.Add(turnAdmissionTTL + time.Second)
	if _, err := store.AcquireTurnAdmission(ctx, tenant, account, session, "h3", expired); !stderrors.Is(err, errTurnAdmissionUnrecorded) {
		t.Fatalf("acquire on an expired record = %v, want unrecorded", err)
	}
	if n := stale(expired); n != 1 {
		t.Fatalf("stale with an expired record = %d, want 1", n)
	}
	abandoned := now.Add(turnAdmissionTTL + turnAdmissionAbandonAfter + time.Second)
	if n := stale(abandoned); n != 0 {
		t.Fatalf("stale with an abandoned record = %d, want 0", n)
	}
	reclaimed, err := store.AcquireTurnAdmission(ctx, tenant, account, session, "h3", abandoned)
	if err != nil {
		t.Fatalf("h3 reclaim of an abandoned record: %v", err)
	}
	if got := decodeAdmissionForTest(t, reclaimed).Holder; got != "h3" {
		t.Fatalf("reclaimed holder = %q, want h3", got)
	}
	// h2's release of the record it lost is not an error and frees nothing.
	if err := store.ReleaseTurnAdmission(ctx, tenant, account, session, held); err != nil {
		t.Fatalf("release of a reclaimed record: %v", err)
	}
	if _, err := store.AcquireTurnAdmission(ctx, tenant, account, session, "h4", abandoned); !stderrors.Is(err, errTurnAdmissionBusy) {
		t.Fatalf("acquire after a lost release = %v, want busy", err)
	}

	// Unrecorded: a live record marked unrecorded refuses and is counted.
	marked, err := store.MarkTurnAdmissionUnrecorded(ctx, tenant, account, session, reclaimed)
	if err != nil {
		t.Fatalf("mark unrecorded: %v", err)
	}
	if !decodeAdmissionForTest(t, marked).Unrecorded {
		t.Fatal("marked record is not unrecorded")
	}
	if _, err := store.AcquireTurnAdmission(ctx, tenant, account, session, "h4", abandoned); !stderrors.Is(err, errTurnAdmissionUnrecorded) {
		t.Fatalf("acquire on an unrecorded record = %v, want unrecorded", err)
	}
	if n := stale(abandoned); n != 1 {
		t.Fatalf("stale with an unrecorded record = %d, want 1", n)
	}
	if err := store.ReleaseTurnAdmission(ctx, tenant, account, session, marked); err != nil {
		t.Fatalf("release of the unrecorded record: %v", err)
	}
	if n := stale(abandoned); n != 0 {
		t.Fatalf("stale after release = %d, want 0", n)
	}

	// A store without compare-and-swap cannot keep an admission.
	kv := NewMemoryTaskKV()
	creator, ok := kv.(guardedCreator)
	if !ok {
		t.Fatalf("memory KV %T has no guarded create", kv)
	}
	unguarded, ok := NewConsumptionLedger(admissionCreateOnlyKV{KVStore: kv, creator: creator}).(turnAdmissionStore)
	if !ok {
		t.Fatal("the KV ledger keeps no turn admissions")
	}
	var refused *ConsumptionUnguardedError
	if _, err := unguarded.AcquireTurnAdmission(ctx, tenant, account, session, "h1", now); !stderrors.As(err, &refused) {
		t.Fatalf("acquire over a store without compare-and-swap = %v, want *ConsumptionUnguardedError", err)
	}
}
