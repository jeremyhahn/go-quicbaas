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
	"time"
)

// CreateConsent validation rejects each missing required field.
func TestBackendConsentStorage_CreateValidation(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendConsentStorage(kv, nil)
	ctx := context.Background()

	base := func() CreateConsentOptions {
		return CreateConsentOptions{
			UserID: 30001, TenantID: "acme", Purpose: ConsentPurposeMarketing, Version: "v1",
		}
	}
	cases := map[string]func(o *CreateConsentOptions){
		"no-user":    func(o *CreateConsentOptions) { o.UserID = 0 },
		"no-tenant":  func(o *CreateConsentOptions) { o.TenantID = "" },
		"no-purpose": func(o *CreateConsentOptions) { o.Purpose = "" },
		"no-version": func(o *CreateConsentOptions) { o.Version = "" },
	}
	for name, mut := range cases {
		opts := base()
		mut(&opts)
		if _, err := store.CreateConsent(ctx, opts); err == nil {
			t.Fatalf("CreateConsent(%s) want error", name)
		}
	}

	// Duplicate purpose for the same user errors.
	if _, err := store.CreateConsent(ctx, base()); err != nil {
		t.Fatalf("CreateConsent: %v", err)
	}
	if _, err := store.CreateConsent(ctx, base()); err == nil {
		t.Fatal("CreateConsent(duplicate purpose) want error")
	}
}

// CreateConsent and UpdateConsent populate optional fields and reconcile the
// granted/withdrawn timestamps.
func TestBackendConsentStorage_OptionalFields(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendConsentStorage(kv, nil)
	ctx := context.Background()

	expiry := time.Now().Add(48 * time.Hour)
	consent, err := store.CreateConsent(ctx, CreateConsentOptions{
		UserID: 30001, TenantID: "acme", Purpose: ConsentPurposeMarketing,
		Status: ConsentStatusWithdrawn, Version: "v1",
		IPAddress: "1.2.3.4", UserAgent: "agent", ExpiresAt: &expiry,
		Metadata: map[string]string{"src": "test"},
	})
	if err != nil {
		t.Fatalf("CreateConsent: %v", err)
	}
	if consent.WithdrawnAt == nil {
		t.Fatal("WithdrawnAt should be set for a withdrawn consent")
	}
	if consent.Metadata["src"] != "test" {
		t.Fatalf("metadata not copied: %v", consent.Metadata)
	}

	// Update every optional field and flip to granted.
	newExpiry := time.Now().Add(72 * time.Hour)
	updated, err := store.UpdateConsent(ctx, consent.ID, UpdateConsentOptions{
		Status:    ConsentStatusGranted,
		Version:   "v2",
		ExpiresAt: &newExpiry,
		IPAddress: "5.6.7.8",
		UserAgent: "agent2",
		Metadata:  map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("UpdateConsent: %v", err)
	}
	if updated.Status != ConsentStatusGranted || updated.GrantedAt == nil || updated.WithdrawnAt != nil {
		t.Fatalf("granted reconciliation wrong: %+v", updated)
	}
	if updated.Version != "v2" || updated.IPAddress != "5.6.7.8" || updated.UserAgent != "agent2" {
		t.Fatalf("optional fields not applied: %+v", updated)
	}
	if updated.Metadata["k"] != "v" || updated.Metadata["src"] != "test" {
		t.Fatalf("metadata merge wrong: %v", updated.Metadata)
	}

	// Update a missing consent errors.
	if _, err := store.UpdateConsent(ctx, encodeTenantID("acme", "missing"), UpdateConsentOptions{}); err == nil {
		t.Fatal("UpdateConsent(missing) want error")
	}
}

// ListConsents without an explicit TenantID sweeps every tenant in the lister
// and applies user/purpose/status filters.
func TestBackendConsentStorage_ListAcrossTenants(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendConsentStorage(kv, staticTenantLister{ids: []string{"acme", "globex"}})
	ctx := context.Background()

	mk := func(tenant string, user uint64, purpose ConsentPurpose, status ConsentStatus) {
		if _, err := store.CreateConsent(ctx, CreateConsentOptions{
			UserID: user, TenantID: tenant, Purpose: purpose, Status: status, Version: "v1",
		}); err != nil {
			t.Fatalf("CreateConsent(%s/%d): %v", tenant, user, err)
		}
	}
	mk("acme", 30001, ConsentPurposeMarketing, ConsentStatusGranted)
	mk("acme", 30002, ConsentPurposeAnalytics, ConsentStatusWithdrawn)
	mk("globex", 30003, ConsentPurposeMarketing, ConsentStatusGranted)

	// No filter: every tenant's records.
	all, err := store.ListConsents(ctx, ListConsentsOptions{})
	if err != nil || len(all) != 3 {
		t.Fatalf("ListConsents(all) = %d, %v; want 3, nil", len(all), err)
	}

	// Purpose + status filter.
	granted, err := store.ListConsents(ctx, ListConsentsOptions{
		Purpose: ConsentPurposeMarketing, Status: ConsentStatusGranted,
	})
	if err != nil || len(granted) != 2 {
		t.Fatalf("ListConsents(marketing/granted) = %d, %v; want 2, nil", len(granted), err)
	}

	// User filter narrows to one.
	byUser, err := store.ListConsents(ctx, ListConsentsOptions{UserID: 30002})
	if err != nil || len(byUser) != 1 {
		t.Fatalf("ListConsents(u2) = %d, %v; want 1, nil", len(byUser), err)
	}

	// GetUserConsents with a missing index entry returns empty without error.
	empty, err := store.GetUserConsents(ctx, "acme", 99998)
	if err != nil || len(empty) != 0 {
		t.Fatalf("GetUserConsents(missing) = %d, %v; want 0, nil", len(empty), err)
	}
}

// CreateDeletionRequest validation and optional fields (grace period, metadata,
// categories) plus cross-tenant ListDeletionRequests.
func TestBackendDeletionStorage_OptionalAndValidation(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendDeletionStorage(kv, staticTenantLister{ids: []string{"acme", "globex"}})
	ctx := context.Background()

	// Validation.
	for name, opts := range map[string]CreateDeletionRequestOptions{
		"no-user":   {TenantID: "acme", RequestedBy: 30040},
		"no-tenant": {UserID: 30001, RequestedBy: 30040},
		"no-by":     {UserID: 30001, TenantID: "acme"},
	} {
		if _, err := store.CreateDeletionRequest(ctx, opts); err == nil {
			t.Fatalf("CreateDeletionRequest(%s) want error", name)
		}
	}

	req, err := store.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID: 30001, TenantID: "acme", RequestedBy: 30040,
		Reason:             "gdpr",
		GracePeriodDays:    7,
		DeleteCategories:   []string{"profile"},
		PreserveCategories: []string{"audit"},
		Metadata:           map[string]string{"ticket": "123"},
	})
	if err != nil {
		t.Fatalf("CreateDeletionRequest: %v", err)
	}
	if req.GracePeriodEndsAt == nil {
		t.Fatal("GracePeriodEndsAt should be set when GracePeriodDays > 0")
	}
	if req.Metadata["ticket"] != "123" || len(req.DeleteCategories) != 1 {
		t.Fatalf("optional fields not applied: %+v", req)
	}

	if _, err := store.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID: 30002, TenantID: "globex", RequestedBy: 30040,
	}); err != nil {
		t.Fatalf("CreateDeletionRequest(globex): %v", err)
	}

	// Cross-tenant list without an explicit TenantID.
	all, err := store.ListDeletionRequests(ctx, ListDeletionRequestsOptions{})
	if err != nil || len(all) != 2 {
		t.Fatalf("ListDeletionRequests(all) = %d, %v; want 2, nil", len(all), err)
	}
	byUser, err := store.ListDeletionRequests(ctx, ListDeletionRequestsOptions{UserID: 30001})
	if err != nil || len(byUser) != 1 {
		t.Fatalf("ListDeletionRequests(u1) = %d, %v; want 1, nil", len(byUser), err)
	}
}

// Cross-tenant ListExportRequests without an explicit TenantID.
func TestBackendDataExportStorage_ListAcrossTenants(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendDataExportStorage(kv, staticTenantLister{ids: []string{"acme", "globex"}})
	ctx := context.Background()

	if _, err := store.CreateExportRequest(ctx, CreateDataExportOptions{
		UserID: 30001, TenantID: "acme", RequestedBy: 30040,
		Metadata: map[string]string{"k": "v"}, IncludeCategories: []string{"profile"},
	}); err != nil {
		t.Fatalf("CreateExportRequest(acme): %v", err)
	}
	if _, err := store.CreateExportRequest(ctx, CreateDataExportOptions{
		UserID: 30002, TenantID: "globex", RequestedBy: 30040,
	}); err != nil {
		t.Fatalf("CreateExportRequest(globex): %v", err)
	}

	all, err := store.ListExportRequests(ctx, ListExportRequestsOptions{})
	if err != nil || len(all) != 2 {
		t.Fatalf("ListExportRequests(all) = %d, %v; want 2, nil", len(all), err)
	}

	// Validation errors.
	for name, opts := range map[string]CreateDataExportOptions{
		"no-user":   {TenantID: "acme", RequestedBy: 30040},
		"no-tenant": {UserID: 30001, RequestedBy: 30040},
		"no-by":     {UserID: 30001, TenantID: "acme"},
	} {
		if _, err := store.CreateExportRequest(ctx, opts); err == nil {
			t.Fatalf("CreateExportRequest(%s) want error", name)
		}
	}
}
