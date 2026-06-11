// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package iam

import (
	"context"
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// (a)/(b) A consent for a barrier tenant lands in its store; by-id update routes
// via the encoded id.
func TestBackendConsentStorage_BarrierAndByID(t *testing.T) {
	kv := newRoutingTestKV()
	tenantStore := kv.barrierTenant("acme")
	store := NewBackendConsentStorage(kv, nil)
	ctx := context.Background()

	consent, err := store.CreateConsent(ctx, CreateConsentOptions{
		UserID:   "u1",
		TenantID: "acme",
		Purpose:  ConsentPurposeMarketing,
		Status:   ConsentStatusGranted,
		Version:  "v1",
	})
	if err != nil {
		t.Fatalf("CreateConsent: %v", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("consent leaked into shared store")
	}
	if tenantStore.keyCount() == 0 {
		t.Fatal("consent not in barrier store")
	}
	if tenant, _ := decodeTenantID(consent.ID); tenant != "acme" {
		t.Fatalf("consent id not tenant-encoded: %q", consent.ID)
	}

	// UpdateConsent with only the id must route via the encoded id.
	updated, err := store.UpdateConsent(context.Background(), consent.ID, UpdateConsentOptions{
		Status: ConsentStatusWithdrawn,
	})
	if err != nil {
		t.Fatalf("UpdateConsent: %v", err)
	}
	if updated.Status != ConsentStatusWithdrawn {
		t.Fatalf("status = %q, want withdrawn", updated.Status)
	}

	byUser, err := store.GetUserConsents(ctx, "acme", "u1")
	if err != nil || len(byUser) != 1 {
		t.Fatalf("GetUserConsents = %d, %v; want 1, nil", len(byUser), err)
	}
}

// (c) Consent sealed fail-closed.
func TestBackendConsentStorage_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendConsentStorage(kv, nil)
	ctx := context.Background()

	_, err := store.CreateConsent(ctx, CreateConsentOptions{
		UserID: "u1", TenantID: "acme", Purpose: ConsentPurposeMarketing,
		Status: ConsentStatusGranted, Version: "v1",
	})
	if !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("CreateConsent(sealed) err = %v, want sealed", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed op wrote to shared store: %d", kv.shared.keyCount())
	}
}

// (b) Export request by-id get routes via the encoded id.
func TestBackendDataExportStorage_ByID(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendDataExportStorage(kv, nil)
	ctx := context.Background()

	req, err := store.CreateExportRequest(ctx, CreateDataExportOptions{
		UserID:      "u1",
		TenantID:    "acme",
		RequestedBy: "admin",
	})
	if err != nil {
		t.Fatalf("CreateExportRequest: %v", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("export leaked into shared store")
	}

	got, err := store.GetExportRequest(context.Background(), req.ID)
	if err != nil || got.UserID != "u1" {
		t.Fatalf("GetExportRequest = %v, %v", got, err)
	}

	got.Status = DataExportStatusCompleted
	if err := store.UpdateExportRequest(context.Background(), got); err != nil {
		t.Fatalf("UpdateExportRequest: %v", err)
	}
	reread, err := store.GetExportRequest(ctx, req.ID)
	if err != nil || reread.Status != DataExportStatusCompleted {
		t.Fatalf("reread status = %v, %v", reread, err)
	}
}

// (b) Deletion request by-id get routes via the encoded id; cross-tenant get
// for the wrong tenant returns not-found (isolation).
func TestBackendDeletionStorage_ByIDAndIsolation(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendDeletionStorage(kv, nil)
	ctx := context.Background()

	req, err := store.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID:      "u1",
		TenantID:    "acme",
		RequestedBy: "admin",
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest: %v", err)
	}

	got, err := store.GetDeletionRequest(context.Background(), req.ID)
	if err != nil || got.UserID != "u1" {
		t.Fatalf("GetDeletionRequest = %v, %v", got, err)
	}

	// A request id forged with a different tenant prefix must not resolve to
	// acme's record — it routes to globex's (empty) store.
	_, fullRaw := decodeTenantID(req.ID)
	forged := encodeTenantID("globex", fullRaw[len("acme."):])
	if _, err := store.GetDeletionRequest(context.Background(), forged); !dberrors.IsNotFound(err) {
		t.Fatalf("forged-tenant lookup err = %v, want NotFound", err)
	}
}

// (c) Deletion sealed fail-closed.
func TestBackendDeletionStorage_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendDeletionStorage(kv, nil)
	ctx := context.Background()

	_, err := store.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID: "u1", TenantID: "acme", RequestedBy: "admin",
	})
	if !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("CreateDeletionRequest(sealed) err = %v, want sealed", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed op wrote to shared store: %d", kv.shared.keyCount())
	}
}

// ListConsents with a tenant filter routes directly to that tenant.
func TestBackendConsentStorage_ListByTenantFilter(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendConsentStorage(kv, nil)
	ctx := context.Background()

	if _, err := store.CreateConsent(ctx, CreateConsentOptions{
		UserID: "u1", TenantID: "acme", Purpose: ConsentPurposeMarketing,
		Status: ConsentStatusGranted, Version: "v1",
	}); err != nil {
		t.Fatalf("CreateConsent(acme): %v", err)
	}
	if _, err := store.CreateConsent(ctx, CreateConsentOptions{
		UserID: "u2", TenantID: "globex", Purpose: ConsentPurposeAnalytics,
		Status: ConsentStatusGranted, Version: "v1",
	}); err != nil {
		t.Fatalf("CreateConsent(globex): %v", err)
	}

	acme, err := store.ListConsents(ctx, ListConsentsOptions{TenantID: "acme"})
	if err != nil || len(acme) != 1 {
		t.Fatalf("ListConsents(acme) = %d, %v; want 1, nil", len(acme), err)
	}
	if acme[0].TenantID != "acme" {
		t.Fatalf("ListConsents returned cross-tenant record: %q", acme[0].TenantID)
	}
}
