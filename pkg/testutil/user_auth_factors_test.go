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

package testutil

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

func newTestFactor(credential string) *qrdbsdk.AuthFactor {
	return &qrdbsdk.AuthFactor{Type: qrdbsdk.AuthFactorFIDO2, Credential: []byte(credential)}
}

func TestMemoryUserAuthFactors_AddConditional_NoActiveOfTypeOneWinner(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryUserAuthFactors()
	cond := qrdbsdk.FactorAddCondition{NoActiveOfType: true}

	version, err := m.AddConditional(ctx, 7, newTestFactor("first"), cond)
	if err != nil {
		t.Fatalf("first conditional add failed: %v", err)
	}
	if version != 1 {
		t.Fatalf("expected factor-set version 1 after the first add, got %d", version)
	}

	_, err = m.AddConditional(ctx, 7, newTestFactor("second"), cond)
	var failed *qrdbsdk.FactorConditionFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("expected FactorConditionFailedError, got %v", err)
	}
	if failed.Condition != qrdbsdk.FactorConditionNoActiveOfType || failed.ExistingFactorID == "" {
		t.Fatalf("expected no-active-of-type failure naming the existing factor, got %+v", failed)
	}
}

func TestMemoryUserAuthFactors_AddConditional_ExpectedVersion(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryUserAuthFactors()

	zero := uint64(0)
	if _, err := m.AddConditional(ctx, 7, newTestFactor("first"), qrdbsdk.FactorAddCondition{ExpectedVersion: &zero}); err != nil {
		t.Fatalf("add at the current version failed: %v", err)
	}

	_, err := m.AddConditional(ctx, 7, newTestFactor("second"), qrdbsdk.FactorAddCondition{ExpectedVersion: &zero})
	var failed *qrdbsdk.FactorConditionFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("expected FactorConditionFailedError on a stale version, got %v", err)
	}
	if failed.CurrentVersion != 1 || failed.ExpectedVersion != 0 {
		t.Fatalf("expected current 1 / expected 0, got %+v", failed)
	}
}

func TestMemoryUserAuthFactors_AddConditional_RefusesEmptyCondition(t *testing.T) {
	m := NewMemoryUserAuthFactors()

	if _, err := m.AddConditional(context.Background(), 7, newTestFactor("x"), qrdbsdk.FactorAddCondition{}); err == nil {
		t.Fatal("an empty condition must be refused, not treated as an unconditional add")
	}
	version, err := m.FactorSetVersion(context.Background(), 7)
	if err != nil || version != 0 {
		t.Fatalf("a refused add must not move the version, got (%d, %v)", version, err)
	}
}

func TestMemoryUserAuthFactors_FactorSetVersion_MovesOnEveryChange(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryUserAuthFactors()

	factor := newTestFactor("x")
	if err := m.Add(ctx, 7, factor); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := m.Remove(ctx, factor.ID, false); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	version, err := m.FactorSetVersion(ctx, 7)
	if err != nil {
		t.Fatalf("FactorSetVersion failed: %v", err)
	}
	if version != 2 {
		t.Fatalf("expected version 2 after an add and a revoke, got %d", version)
	}
}

func TestMemoryUserAuthFactors_FactorSetVersion_UnknownUserIsZero(t *testing.T) {
	version, err := NewMemoryUserAuthFactors().FactorSetVersion(context.Background(), 99)
	if err != nil {
		t.Fatalf("FactorSetVersion failed: %v", err)
	}
	if version != 0 {
		t.Fatalf("a user whose set never changed is at version 0, got %d", version)
	}
}

func TestMemoryUserAuthFactors_Delete_RemovesRecordAndIndex(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryUserAuthFactors()

	factor := newTestFactor("x")
	if err := m.Add(ctx, 7, factor); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := m.Delete(ctx, factor.ID, false); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	var notFound *FactorNotFoundError
	if _, err := m.Get(ctx, factor.ID); !errors.As(err, &notFound) {
		t.Fatalf("expected FactorNotFoundError after Delete, got %v", err)
	}
	// The credential is free again: the index entry went with the record.
	if err := m.Add(ctx, 7, newTestFactor("x")); err != nil {
		t.Fatalf("re-adding a deleted credential failed: %v", err)
	}
}

func TestMemoryUserAuthFactors_Delete_Errors(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryUserAuthFactors()

	var notFound *FactorNotFoundError
	if err := m.Delete(ctx, "missing", false); !errors.As(err, &notFound) {
		t.Fatalf("expected FactorNotFoundError, got %v", err)
	}

	factor := newTestFactor("x")
	if err := m.Add(ctx, 7, factor); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := m.Remove(ctx, factor.ID, false); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	var revoked *qrdbsdk.FactorRevokedError
	if err := m.Delete(ctx, factor.ID, false); !errors.As(err, &revoked) {
		t.Fatalf("expected FactorRevokedError for a revoked factor, got %v", err)
	}
}
