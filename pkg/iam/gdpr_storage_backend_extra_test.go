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
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// GetConsent resolves the per-purpose index; DeleteConsent clears all indexes.
func TestBackendConsentStorage_GetAndDelete(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendConsentStorage(kv, nil)
	ctx := context.Background()

	consent, err := store.CreateConsent(ctx, CreateConsentOptions{
		UserID: 30001, TenantID: "acme", Purpose: ConsentPurposeMarketing,
		Status: ConsentStatusGranted, Version: "v1",
	})
	if err != nil {
		t.Fatalf("CreateConsent: %v", err)
	}

	got, err := store.GetConsent(ctx, "acme", 30001, ConsentPurposeMarketing)
	if err != nil {
		t.Fatalf("GetConsent: %v", err)
	}
	if got.ID != consent.ID {
		t.Fatalf("GetConsent id = %q, want %q", got.ID, consent.ID)
	}

	// Missing purpose returns not-found.
	if _, err := store.GetConsent(ctx, "acme", 30001, ConsentPurposeAnalytics); !dberrors.IsNotFound(err) {
		t.Fatalf("GetConsent(missing) err = %v, want NotFound", err)
	}

	// Delete clears the record and indexes.
	if err := store.DeleteConsent(ctx, consent.ID); err != nil {
		t.Fatalf("DeleteConsent: %v", err)
	}
	if _, err := store.GetConsent(ctx, "acme", 30001, ConsentPurposeMarketing); !dberrors.IsNotFound(err) {
		t.Fatalf("GetConsent after delete err = %v, want NotFound", err)
	}

	// Delete of a missing consent surfaces not-found.
	if err := store.DeleteConsent(ctx, consent.ID); !dberrors.IsNotFound(err) {
		t.Fatalf("DeleteConsent(missing) err = %v, want NotFound", err)
	}
}

// ListExportRequests filters by user/status and paginates.
func TestBackendDataExportStorage_ListFilterPaginate(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendDataExportStorage(kv, nil)
	ctx := context.Background()

	for _, uid := range []uint64{30001, 30002, 30003} {
		if _, err := store.CreateExportRequest(ctx, CreateDataExportOptions{
			UserID: uid, TenantID: "acme", RequestedBy: 30040,
		}); err != nil {
			t.Fatalf("CreateExportRequest(%d): %v", uid, err)
		}
	}

	// Filter to a single user.
	byUser, err := store.ListExportRequests(ctx, ListExportRequestsOptions{TenantID: "acme", UserID: 30002})
	if err != nil || len(byUser) != 1 {
		t.Fatalf("ListExportRequests(u2) = %d, %v; want 1, nil", len(byUser), err)
	}

	// Paginate: limit 2 of the tenant's three records.
	page, err := store.ListExportRequests(ctx, ListExportRequestsOptions{TenantID: "acme", Limit: 2})
	if err != nil || len(page) != 2 {
		t.Fatalf("ListExportRequests(limit 2) = %d, %v; want 2, nil", len(page), err)
	}

	// Offset beyond the set yields empty.
	none, err := store.ListExportRequests(ctx, ListExportRequestsOptions{TenantID: "acme", Offset: 99})
	if err != nil || len(none) != 0 {
		t.Fatalf("ListExportRequests(offset 99) = %d, %v; want 0, nil", len(none), err)
	}

	// Status filter that matches nothing.
	noneStatus, err := store.ListExportRequests(ctx, ListExportRequestsOptions{TenantID: "acme", Status: DataExportStatusCompleted})
	if err != nil || len(noneStatus) != 0 {
		t.Fatalf("ListExportRequests(status) = %d, %v; want 0, nil", len(noneStatus), err)
	}
}

// UpdateDeletionRequest overwrites; ListDeletionRequests filters and paginates.
func TestBackendDeletionStorage_UpdateAndList(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendDeletionStorage(kv, nil)
	ctx := context.Background()

	req, err := store.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID: 30001, TenantID: "acme", RequestedBy: 30040,
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest: %v", err)
	}

	req.Status = DeletionStatusCompleted
	if err := store.UpdateDeletionRequest(ctx, req); err != nil {
		t.Fatalf("UpdateDeletionRequest: %v", err)
	}
	got, err := store.GetDeletionRequest(ctx, req.ID)
	if err != nil || got.Status != DeletionStatusCompleted {
		t.Fatalf("reread status = %v, %v", got, err)
	}

	// Update a missing record returns not-found.
	missing := *req
	missing.ID = encodeTenantID("acme", "does-not-exist")
	if err := store.UpdateDeletionRequest(ctx, &missing); !dberrors.IsNotFound(err) {
		t.Fatalf("UpdateDeletionRequest(missing) err = %v, want NotFound", err)
	}

	// Add a second user's request to exercise list filtering.
	if _, err := store.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID: 30002, TenantID: "acme", RequestedBy: 30040,
	}); err != nil {
		t.Fatalf("CreateDeletionRequest(u2): %v", err)
	}

	all, err := store.ListDeletionRequests(ctx, ListDeletionRequestsOptions{TenantID: "acme"})
	if err != nil || len(all) != 2 {
		t.Fatalf("ListDeletionRequests = %d, %v; want 2, nil", len(all), err)
	}

	completed, err := store.ListDeletionRequests(ctx, ListDeletionRequestsOptions{
		TenantID: "acme", Status: DeletionStatusCompleted,
	})
	if err != nil || len(completed) != 1 {
		t.Fatalf("ListDeletionRequests(completed) = %d, %v; want 1, nil", len(completed), err)
	}

	page, err := store.ListDeletionRequests(ctx, ListDeletionRequestsOptions{TenantID: "acme", Limit: 1})
	if err != nil || len(page) != 1 {
		t.Fatalf("ListDeletionRequests(limit 1) = %d, %v; want 1, nil", len(page), err)
	}
	none, err := store.ListDeletionRequests(ctx, ListDeletionRequestsOptions{TenantID: "acme", Offset: 50})
	if err != nil || len(none) != 0 {
		t.Fatalf("ListDeletionRequests(offset 50) = %d, %v; want 0, nil", len(none), err)
	}
}
