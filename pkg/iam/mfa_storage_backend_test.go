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
	"fmt"
	"testing"
	"time"
)

func validMFASecret(tenantID string, userID uint64) *MFASecret {
	return &MFASecret{
		ID:       fmt.Sprintf("sec-%d", userID),
		UserID:   userID,
		TenantID: tenantID,
		Secret:   testTOTPSecret,
		Digits:   6,
		Period:   30,
	}
}

func validEnrollment(tenantID string, userID uint64) *MFAEnrollment {
	return &MFAEnrollment{
		ID:        fmt.Sprintf("enr-%d", userID),
		UserID:    userID,
		TenantID:  tenantID,
		Method:    MFAMethodTOTP,
		Secret:    testTOTPSecret,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
}

func validChallenge(tenantID string, userID uint64) *MFAChallenge {
	return &MFAChallenge{
		ID:           fmt.Sprintf("chl-%d", userID),
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

	if err := store.SaveSecret(ctx, validMFASecret("acme", 30001)); err != nil {
		t.Fatalf("SaveSecret: %v", err)
	}
	if tenantStore.keyCount() == 0 {
		t.Fatal("secret did not land in the barrier tenant's store")
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("MFA secret leaked into the shared store")
	}

	got, err := store.GetSecret(ctx, 30001, "acme")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got.Secret != testTOTPSecret {
		t.Fatalf("secret = %q, want %q", got.Secret, testTOTPSecret)
	}
}

// (b) Enrollment by-ID lookup parses the tenant from the encoded id and routes
// to the owning tenant (round-trip).
func TestBackendMFAStorage_EnrollmentByIDRoutes(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	enr := validEnrollment("acme", 30001)
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
	if got.UserID != 30001 {
		t.Fatalf("enrollment userID = %d, want 30001", got.UserID)
	}

	active, err := store.HasActiveEnrollment(ctx, 30001, "acme")
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

	chl := validChallenge("acme", 30001)
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

	if err := store.DeleteChallengesByUser(ctx, 30001, "acme"); err != nil {
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

	if err := store.SaveSecret(ctx, validMFASecret("acme", 30001)); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("SaveSecret(sealed) err = %v, want sealed", err)
	}
	if _, err := store.GetSecret(ctx, 30001, "acme"); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("GetSecret(sealed) err = %v, want sealed", err)
	}
	if err := store.SaveEnrollment(ctx, validEnrollment("acme", 30001)); !errors.Is(err, errTenantSealedTest) {
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

	if err := store.SaveEnrollment(ctx, validEnrollment("acme", 40060)); err != nil {
		t.Fatalf("SaveEnrollment(acme): %v", err)
	}
	// globex must report no active enrollment for the same user id.
	active, err := store.HasActiveEnrollment(ctx, 40060, "globex")
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

	expired := validEnrollment("acme", 30001)
	expired.ExpiresAt = time.Now().Add(-time.Hour)
	if err := store.SaveEnrollment(ctx, expired); err != nil {
		t.Fatalf("SaveEnrollment(expired): %v", err)
	}
	fresh := validEnrollment("globex", 30002)
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
	if active, _ := store.HasActiveEnrollment(ctx, 30002, "globex"); !active {
		t.Fatal("CleanupExpired removed a non-expired enrollment")
	}
}
