//go:build iam

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
)

func validMFASecret(tenantID, userID string) *MFASecret {
	return &MFASecret{
		ID:       "sec-" + userID,
		UserID:   userID,
		TenantID: tenantID,
		Secret:   "JBSWY3DPEHPK3PXP",
		Digits:   6,
		Period:   30,
	}
}

func validEnrollment(tenantID, userID string) *MFAEnrollment {
	return &MFAEnrollment{
		ID:        "enr-" + userID,
		UserID:    userID,
		TenantID:  tenantID,
		Method:    MFAMethodTOTP,
		Secret:    "JBSWY3DPEHPK3PXP",
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
}

func validChallenge(tenantID, userID string) *MFAChallenge {
	return &MFAChallenge{
		ID:           "chl-" + userID,
		UserID:       userID,
		TenantID:     tenantID,
		SessionToken: "partial-token",
		Method:       MFAMethodTOTP,
		ExpiresAt:    time.Now().Add(5 * time.Minute),
	}
}

// (a) An MFA secret for a barrier tenant lands in that tenant's own store and is
// absent from the shared store.
func TestBackendMFAStorage_SecretIsolatedToBarrier(t *testing.T) {
	kv := newRoutingTestKV()
	tenantStore := kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	if err := store.SaveSecret(ctx, validMFASecret("acme", "u1")); err != nil {
		t.Fatalf("SaveSecret: %v", err)
	}
	if tenantStore.keyCount() == 0 {
		t.Fatal("secret did not land in the barrier tenant's store")
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("MFA secret leaked into the shared store")
	}

	got, err := store.GetSecret(ctx, "u1", "acme")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got.Secret != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("secret = %q, want JBSWY3DPEHPK3PXP", got.Secret)
	}
}

// (b) Enrollment by-ID lookup parses the tenant from the encoded id and routes
// to the owning tenant (round-trip).
func TestBackendMFAStorage_EnrollmentByIDRoutes(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	enr := validEnrollment("acme", "u1")
	if err := store.SaveEnrollment(ctx, enr); err != nil {
		t.Fatalf("SaveEnrollment: %v", err)
	}
	// The save must have tenant-encoded the id.
	tenant, _ := decodeTenantID(enr.ID)
	if tenant != "acme" {
		t.Fatalf("enrollment id not tenant-encoded: %q", enr.ID)
	}

	// Get with NO tenant in context must still resolve via the encoded id.
	got, err := store.GetEnrollment(context.Background(), enr.ID)
	if err != nil {
		t.Fatalf("GetEnrollment: %v", err)
	}
	if got.UserID != "u1" {
		t.Fatalf("enrollment userID = %q, want u1", got.UserID)
	}

	active, err := store.HasActiveEnrollment(ctx, "u1", "acme")
	if err != nil || !active {
		t.Fatalf("HasActiveEnrollment = %v, %v; want true, nil", active, err)
	}
}

// Challenge by-ID lookup round-trips through the encoded id.
func TestBackendMFAStorage_ChallengeByIDRoutes(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	chl := validChallenge("acme", "u1")
	if err := store.SaveChallenge(ctx, chl); err != nil {
		t.Fatalf("SaveChallenge: %v", err)
	}
	got, err := store.GetChallenge(context.Background(), chl.ID)
	if err != nil {
		t.Fatalf("GetChallenge: %v", err)
	}
	if got.SessionToken != "partial-token" {
		t.Fatalf("challenge token = %q", got.SessionToken)
	}

	if err := store.DeleteChallengesByUser(ctx, "u1", "acme"); err != nil {
		t.Fatalf("DeleteChallengesByUser: %v", err)
	}
	if _, err := store.GetChallenge(context.Background(), chl.ID); !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Fatalf("after delete GetChallenge err = %v, want ErrMFAChallengeNotFound", err)
	}
}

// (c) A sealed barrier tenant fails closed on every MFA op and never writes to
// the shared store.
func TestBackendMFAStorage_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	if err := store.SaveSecret(ctx, validMFASecret("acme", "u1")); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("SaveSecret(sealed) err = %v, want sealed", err)
	}
	if _, err := store.GetSecret(ctx, "u1", "acme"); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("GetSecret(sealed) err = %v, want sealed", err)
	}
	if err := store.SaveEnrollment(ctx, validEnrollment("acme", "u1")); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("SaveEnrollment(sealed) err = %v, want sealed", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatalf("sealed tenant op wrote %d keys to shared store; must be 0", kv.shared.keyCount())
	}
}

// Two barrier tenants must not see each other's enrollments via the by-user
// index — isolation across tenants.
func TestBackendMFAStorage_CrossTenantIsolation(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	if err := store.SaveEnrollment(ctx, validEnrollment("acme", "shared-user")); err != nil {
		t.Fatalf("SaveEnrollment(acme): %v", err)
	}
	// globex must report no active enrollment for the same user id.
	active, err := store.HasActiveEnrollment(ctx, "shared-user", "globex")
	if err != nil {
		t.Fatalf("HasActiveEnrollment(globex): %v", err)
	}
	if active {
		t.Fatal("globex saw acme's enrollment; tenant isolation broken")
	}
}

// CleanupExpired sweeps each tenant via the tenant lister (no global scan).
func TestBackendMFAStorage_CleanupExpiredPerTenant(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.barrierTenant("globex")
	lister := staticTenantLister{ids: []string{"acme", "globex"}}
	store := NewBackendMFAStorage(kv, lister)
	ctx := context.Background()

	expired := validEnrollment("acme", "u1")
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.SaveEnrollment(ctx, expired); err != nil {
		t.Fatalf("SaveEnrollment(expired): %v", err)
	}
	fresh := validEnrollment("globex", "u2")
	if err := store.SaveEnrollment(ctx, fresh); err != nil {
		t.Fatalf("SaveEnrollment(fresh): %v", err)
	}

	n, err := store.CleanupExpired(ctx)
	if err != nil {
		t.Fatalf("CleanupExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("CleanupExpired removed %d, want 1", n)
	}
	// Fresh enrollment in globex survives.
	if active, _ := store.HasActiveEnrollment(ctx, "u2", "globex"); !active {
		t.Fatal("CleanupExpired removed a non-expired enrollment")
	}
}
