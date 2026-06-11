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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// MFA Save* reject invalid records; Get* report not-found.
func TestBackendMFAStorage_ValidationAndNotFound(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	if err := store.SaveSecret(ctx, &MFASecret{}); err == nil {
		t.Fatal("SaveSecret(invalid) want error")
	}
	if err := store.SaveEnrollment(ctx, &MFAEnrollment{}); err == nil {
		t.Fatal("SaveEnrollment(invalid) want error")
	}
	if err := store.SaveChallenge(ctx, &MFAChallenge{}); err == nil {
		t.Fatal("SaveChallenge(invalid) want error")
	}

	if _, err := store.GetSecret(ctx, "nobody", "acme"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("GetSecret(missing) err = %v, want ErrMFANotEnrolled", err)
	}
	if _, err := store.GetEnrollment(ctx, encodeTenantID("acme", "nope")); !errors.Is(err, ErrMFAEnrollmentNotFound) {
		t.Fatalf("GetEnrollment(missing) err = %v, want ErrMFAEnrollmentNotFound", err)
	}
	if _, err := store.GetChallenge(ctx, encodeTenantID("acme", "nope")); !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Fatalf("GetChallenge(missing) err = %v, want ErrMFAChallengeNotFound", err)
	}
	if _, err := store.GetBackupCodes(ctx, "nobody", "acme"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("GetBackupCodes(missing) err = %v, want ErrMFANotEnrolled", err)
	}
}

// CleanupExpired sweeps expired challenges (not just enrollments) per tenant.
func TestBackendMFAStorage_CleanupExpiredChallenges(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, staticTenantLister{ids: []string{"acme"}})
	ctx := context.Background()

	chal := validChallenge("acme", "u1")
	chal.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.SaveChallenge(ctx, chal); err != nil {
		t.Fatalf("SaveChallenge: %v", err)
	}
	enr := validEnrollment("acme", "u1")
	enr.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.SaveEnrollment(ctx, enr); err != nil {
		t.Fatalf("SaveEnrollment: %v", err)
	}

	n, err := store.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if n != 2 {
		t.Fatalf("CleanupExpired removed %d, want 2 (1 enrollment + 1 challenge)", n)
	}
	if _, err := store.GetChallenge(ctx, chal.ID); !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Fatalf("expired challenge survived cleanup: %v", err)
	}
}

// Session Create rejects invalid input and duplicates; Get reports not-found.
func TestBackendSessionStorage_ValidationAndNotFound(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendSessionStorage(kv, nil)
	ctx := context.Background()

	if err := store.CreateSession(ctx, nil); err == nil {
		t.Fatal("CreateSession(nil) want error")
	}
	if err := store.CreateSession(ctx, &Session{TenantID: "acme"}); err == nil {
		t.Fatal("CreateSession(no id) want error")
	}

	sess := basicSession("acme", "u1", "s1")
	if err := store.CreateSession(ctx, sess); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if err := store.CreateSession(ctx, sess); err == nil {
		t.Fatal("CreateSession(duplicate) want error")
	}

	if _, err := store.GetSession(ctx, encodeTenantID("acme", "nope")); err == nil {
		t.Fatal("GetSession(missing) want error")
	}
}

// SessionExt Create validates and rejects duplicates; Get reports not-found.
func TestBackendSessionExtStorage_ValidationAndNotFound(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendSessionExtStorage(kv, nil)
	ctx := ContextWithTenant(context.Background(), "acme")

	if err := store.Create(ctx, &SessionExt{}); err == nil {
		t.Fatal("Create(invalid) want error")
	}

	sess := extSession("acme", "u1", "s1", "th")
	if err := store.Create(ctx, sess); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Create(ctx, sess); err == nil {
		t.Fatal("Create(duplicate) want error")
	}

	if _, err := store.Get(ctx, ""); err == nil {
		t.Fatal("Get(empty) want error")
	}
	if _, err := store.Get(ctx, encodeTenantID("acme", "nope")); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("Get(missing) err = %v, want ErrSessionNotFound", err)
	}
	if _, err := store.GetByToken(ctx, ""); err == nil {
		t.Fatal("GetByToken(empty) want error")
	}
	if _, err := store.GetByToken(ctx, "no-such-token"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("GetByToken(missing) err = %v, want ErrSessionNotFound", err)
	}
}

// Provider CreateProvider validates input and rejects duplicate name/id.
func TestBackendProviderStorage_CreateValidationAndDuplicate(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendProviderStorage(kv)
	ctx := context.Background()

	if err := store.CreateProvider(ctx, &ExternalProvider{}); err == nil {
		t.Fatal("CreateProvider(invalid) want error")
	}

	if err := store.CreateProvider(ctx, testProvider("acme", "p1", "Okta")); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}
	// Duplicate id.
	if err := store.CreateProvider(ctx, testProvider("acme", "p1", "Other")); err == nil {
		t.Fatal("CreateProvider(duplicate id) want error")
	}
	// Duplicate name.
	if err := store.CreateProvider(ctx, testProvider("acme", "p2", "Okta")); err == nil {
		t.Fatal("CreateProvider(duplicate name) want error")
	}

	// Missing provider get reports not-found.
	if _, err := store.GetProvider(ctx, "acme", "missing"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("GetProvider(missing) err = %v, want ErrProviderNotFound", err)
	}
	if _, err := store.GetProviderByName(ctx, "acme", "missing"); !errors.Is(err, ErrProviderNotFound) {
		t.Fatalf("GetProviderByName(missing) err = %v, want ErrProviderNotFound", err)
	}
}

// Federation link Create validates and rejects duplicate external id.
func TestBackendFederationStorage_CreateValidationAndDuplicate(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendFederationStorage(kv)
	ctx := ContextWithTenant(context.Background(), "acme")

	if err := store.CreateLink(ctx, &FederatedIdentity{}); err == nil {
		t.Fatal("CreateLink(invalid) want error")
	}

	link := testLink("acme", "u1", "l1", "p1", "ext-1")
	if err := store.CreateLink(ctx, link); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
	// Duplicate external id under a fresh link id.
	dup := testLink("acme", "u2", "l2", "p1", "ext-1")
	if err := store.CreateLink(ctx, dup); err == nil {
		t.Fatal("CreateLink(duplicate external) want error")
	}

	// Missing link get reports not-found.
	if _, err := store.GetLink(ctx, encodeTenantID("acme", "nope")); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("GetLink(missing) err = %v, want ErrLinkNotFound", err)
	}
	if _, err := store.GetLinkByExternal(ctx, "p1", "no-ext"); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("GetLinkByExternal(missing) err = %v, want ErrLinkNotFound", err)
	}
}

// Consent DeleteConsent on a missing record surfaces not-found (QBError).
func TestBackendConsentStorage_DeleteMissingIsNotFound(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendConsentStorage(kv, nil)
	ctx := context.Background()

	if err := store.DeleteConsent(ctx, encodeTenantID("acme", "ghost")); !dberrors.IsNotFound(err) {
		t.Fatalf("DeleteConsent(missing) err = %v, want NotFound", err)
	}
}
