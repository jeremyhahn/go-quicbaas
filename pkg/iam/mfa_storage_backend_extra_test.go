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

// DeleteSecret removes the per-user secret in the owning tenant.
func TestBackendMFAStorage_DeleteSecret(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	if err := store.SaveSecret(ctx, validMFASecret("acme", 30001)); err != nil {
		t.Fatalf("SaveSecret: %v", err)
	}
	if err := store.DeleteSecret(ctx, 30001, "acme"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if _, err := store.GetSecret(ctx, 30001, "acme"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("GetSecret after delete err = %v, want ErrMFANotEnrolled", err)
	}
}

// DeleteEnrollment and DeleteEnrollmentsByUser clear records plus their indexes.
func TestBackendMFAStorage_DeleteEnrollment(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	enr := validEnrollment("acme", 30001)
	if err := store.SaveEnrollment(ctx, enr); err != nil {
		t.Fatalf("SaveEnrollment: %v", err)
	}
	if err := store.DeleteEnrollment(ctx, enr.ID); err != nil {
		t.Fatalf("DeleteEnrollment: %v", err)
	}
	if _, err := store.GetEnrollment(ctx, enr.ID); !errors.Is(err, ErrMFAEnrollmentNotFound) {
		t.Fatalf("GetEnrollment after delete err = %v, want ErrMFAEnrollmentNotFound", err)
	}
	active, err := store.HasActiveEnrollment(ctx, 30001, "acme")
	if err != nil || active {
		t.Fatalf("HasActiveEnrollment after delete = %v, %v; want false, nil", active, err)
	}

	// Bulk delete via the per-user index.
	for _, uid := range []string{"u2", "u2-b"} {
		e := validEnrollment("acme", 30002)
		e.ID = "enr-" + uid
		if err := store.SaveEnrollment(ctx, e); err != nil {
			t.Fatalf("SaveEnrollment(%s): %v", uid, err)
		}
	}
	if err := store.DeleteEnrollmentsByUser(ctx, 30002, "acme"); err != nil {
		t.Fatalf("DeleteEnrollmentsByUser: %v", err)
	}
	active, err = store.HasActiveEnrollment(ctx, 30002, "acme")
	if err != nil || active {
		t.Fatalf("HasActiveEnrollment after bulk delete = %v, %v; want false, nil", active, err)
	}

	// DeleteEnrollment is tolerant of a missing record.
	if err := store.DeleteEnrollment(ctx, encodeTenantID("acme", "gone")); err != nil {
		t.Fatalf("DeleteEnrollment(missing) = %v, want nil", err)
	}
}

// DeleteChallenge removes a single challenge plus its index.
func TestBackendMFAStorage_DeleteChallenge(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	chl := validChallenge("acme", 30001)
	if err := store.SaveChallenge(ctx, chl); err != nil {
		t.Fatalf("SaveChallenge: %v", err)
	}
	if err := store.DeleteChallenge(ctx, chl.ID); err != nil {
		t.Fatalf("DeleteChallenge: %v", err)
	}
	if _, err := store.GetChallenge(ctx, chl.ID); !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Fatalf("GetChallenge after delete err = %v, want ErrMFAChallengeNotFound", err)
	}
	// Idempotent on missing.
	if err := store.DeleteChallenge(ctx, encodeTenantID("acme", "gone")); err != nil {
		t.Fatalf("DeleteChallenge(missing) = %v, want nil", err)
	}
}

// Backup codes round-trip: save, read, delete, plus validation errors.
func TestBackendMFAStorage_BackupCodes(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendMFAStorage(kv, nil)
	ctx := context.Background()

	codes := &BackupCodeSet{
		ID:       "bc-1",
		UserID:   30001,
		TenantID: "acme",
		Codes:    []BackupCode{{Code: "hash-a"}, {Code: "hash-b"}},
		Created:  time.Now(),
	}
	if err := store.SaveBackupCodes(ctx, codes); err != nil {
		t.Fatalf("SaveBackupCodes: %v", err)
	}
	got, err := store.GetBackupCodes(ctx, 30001, "acme")
	if err != nil {
		t.Fatalf("GetBackupCodes: %v", err)
	}
	if len(got.Codes) != 2 {
		t.Fatalf("GetBackupCodes len = %d, want 2", len(got.Codes))
	}

	if err := store.DeleteBackupCodes(ctx, 30001, "acme"); err != nil {
		t.Fatalf("DeleteBackupCodes: %v", err)
	}
	if _, err := store.GetBackupCodes(ctx, 30001, "acme"); !errors.Is(err, ErrMFANotEnrolled) {
		t.Fatalf("GetBackupCodes after delete err = %v, want ErrMFANotEnrolled", err)
	}

	// Validation: missing ID / user / tenant each error.
	for name, bad := range map[string]*BackupCodeSet{
		"no-id":     {UserID: 30001, TenantID: "acme"},
		"no-user":   {ID: "bc", TenantID: "acme"},
		"no-tenant": {ID: "bc", UserID: 30001},
	} {
		if err := store.SaveBackupCodes(ctx, bad); err == nil {
			t.Fatalf("SaveBackupCodes(%s) want error", name)
		}
	}
}
