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

package iam

import (
	"context"
	"errors"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

func sampleProfile(tenantID string, qrdbID uint64) *BrokerProfile {
	enrolled := time.Now().Add(-24 * time.Hour).UTC()
	lastAuth := time.Now().Add(-1 * time.Hour).UTC()
	expires := time.Now().Add(48 * time.Hour).UTC()
	return &BrokerProfile{
		QRDBUserID:    qrdbID,
		TenantID:      tenantID,
		Email:         "alice@example.com",
		Name:          "Alice Example",
		Groups:        []string{"g-eng", "g-admins"},
		AuthMethods:   []string{"webauthn", "piv"},
		State:         UserStateActive,
		Suspended:     false,
		Locked:        false,
		MFAEnabled:    true,
		MFAMethod:     MFAMethodWebAuthn,
		MFAEnrolledAt: &enrolled,
		MFAVerified:   true,
		MFATimestamp:  &lastAuth,
		LastAuth:      &lastAuth,
		CertBindings: []qrdbsdk.CertBinding{
			{Thumbprint: "ab12cd34", Kind: qrdbsdk.CertBindingPIVAttested},
		},
		InviteToken:     "tok-123",
		InviteExpiresAt: &expires,
	}
}

// SaveProfile then GetProfile round-trips every broker-only field for a
// barrier-isolated tenant, and the record lands in that tenant's store only.
func TestBackendBrokerProfile_RoundTrip(t *testing.T) {
	kv := newRoutingTestKV()
	tenantStore := kv.barrierTenant("acme")
	store := NewBackendBrokerProfileStore(kv)
	ctx := context.Background()

	want := sampleProfile("acme", 42)
	if err := store.SaveProfile(ctx, want); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	if tenantStore.keyCount() == 0 {
		t.Fatal("profile did not land in the barrier tenant's store")
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("broker profile leaked into the shared store")
	}

	got, err := store.GetProfile(ctx, "acme", 42)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	assertProfileEqual(t, want, got)
}

func assertProfileEqual(t *testing.T, want, got *BrokerProfile) {
	t.Helper()
	if got.QRDBUserID != want.QRDBUserID || got.TenantID != want.TenantID {
		t.Fatalf("key mismatch: got (%d,%q) want (%d,%q)", got.QRDBUserID, got.TenantID, want.QRDBUserID, want.TenantID)
	}
	if got.Email != want.Email || got.Name != want.Name {
		t.Fatalf("display mismatch: got (%q,%q) want (%q,%q)", got.Email, got.Name, want.Email, want.Name)
	}
	if len(got.Groups) != len(want.Groups) || len(got.AuthMethods) != len(want.AuthMethods) {
		t.Fatalf("slice len mismatch: groups %v authmethods %v", got.Groups, got.AuthMethods)
	}
	if got.State != want.State || got.Suspended != want.Suspended || got.Locked != want.Locked {
		t.Fatalf("lifecycle mismatch: %+v", got)
	}
	if got.MFAEnabled != want.MFAEnabled || got.MFAMethod != want.MFAMethod || got.MFAVerified != want.MFAVerified {
		t.Fatalf("mfa mismatch: %+v", got)
	}
	if !sameCertBindings(got.CertBindings, want.CertBindings) || got.InviteToken != want.InviteToken {
		t.Fatalf("piv/invite mismatch: %+v", got)
	}
	if got.MFAEnrolledAt == nil || !got.MFAEnrolledAt.Equal(*want.MFAEnrolledAt) {
		t.Fatalf("MFAEnrolledAt mismatch: %v want %v", got.MFAEnrolledAt, want.MFAEnrolledAt)
	}
	if got.LastAuth == nil || !got.LastAuth.Equal(*want.LastAuth) {
		t.Fatalf("LastAuth mismatch: %v want %v", got.LastAuth, want.LastAuth)
	}
	if got.InviteExpiresAt == nil || !got.InviteExpiresAt.Equal(*want.InviteExpiresAt) {
		t.Fatalf("InviteExpiresAt mismatch: %v want %v", got.InviteExpiresAt, want.InviteExpiresAt)
	}
}

// sameCertBindings reports whether two cert-binding slices are equal by
// (thumbprint, kind, notAfter). It compares the wall clock with Equal so a
// JSON round-trip's location change does not register as a difference.
func sameCertBindings(a, b []qrdbsdk.CertBinding) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Thumbprint != b[i].Thumbprint || a[i].Kind != b[i].Kind || !a[i].NotAfter.Equal(b[i].NotAfter) {
			return false
		}
	}
	return true
}

// SaveProfile stamps Created once and advances Updated on re-save.
func TestBackendBrokerProfile_Timestamps(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendBrokerProfileStore(kv)
	ctx := context.Background()

	p := sampleProfile("acme", 7)
	if err := store.SaveProfile(ctx, p); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	first, _ := store.GetProfile(ctx, "acme", 7)
	if first.Created.IsZero() || first.Updated.IsZero() {
		t.Fatal("timestamps not stamped on first save")
	}

	p.Email = "alice2@example.com"
	if err := store.SaveProfile(ctx, p); err != nil {
		t.Fatalf("re-SaveProfile: %v", err)
	}
	second, _ := store.GetProfile(ctx, "acme", 7)
	if !second.Created.Equal(first.Created) {
		t.Fatalf("Created changed on re-save: %v -> %v", first.Created, second.Created)
	}
	if second.Updated.Before(first.Updated) {
		t.Fatalf("Updated went backwards: %v -> %v", first.Updated, second.Updated)
	}
	if second.Email != "alice2@example.com" {
		t.Fatalf("update not persisted: %q", second.Email)
	}
}

// DeleteProfile removes the record; a subsequent Get reports not-found.
func TestBackendBrokerProfile_Delete(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendBrokerProfileStore(kv)
	ctx := context.Background()

	if err := store.SaveProfile(ctx, sampleProfile("acme", 9)); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	if err := store.DeleteProfile(ctx, "acme", 9); err != nil {
		t.Fatalf("DeleteProfile: %v", err)
	}
	if _, err := store.GetProfile(ctx, "acme", 9); !errors.Is(err, ErrBrokerProfileNotFound) {
		t.Fatalf("GetProfile after delete = %v, want ErrBrokerProfileNotFound", err)
	}
	// Deleting a missing profile is a no-op.
	if err := store.DeleteProfile(ctx, "acme", 9); err != nil {
		t.Fatalf("DeleteProfile (missing) = %v, want nil", err)
	}
}

// GetProfile for a never-saved key reports not-found.
func TestBackendBrokerProfile_GetMissing(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	store := NewBackendBrokerProfileStore(kv)

	if _, err := store.GetProfile(context.Background(), "acme", 404); !errors.Is(err, ErrBrokerProfileNotFound) {
		t.Fatalf("GetProfile(missing) = %v, want ErrBrokerProfileNotFound", err)
	}
}

// Invalid inputs are rejected with ErrBrokerProfileInvalid on every verb.
func TestBackendBrokerProfile_InvalidInput(t *testing.T) {
	store := NewBackendBrokerProfileStore(newRoutingTestKV())
	ctx := context.Background()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"nil profile", func() error { return store.SaveProfile(ctx, nil) }},
		{"zero id save", func() error { return store.SaveProfile(ctx, &BrokerProfile{TenantID: "acme"}) }},
		{"empty tenant save", func() error { return store.SaveProfile(ctx, &BrokerProfile{QRDBUserID: 1}) }},
		{"empty tenant get", func() error { _, e := store.GetProfile(ctx, "", 1); return e }},
		{"zero id get", func() error { _, e := store.GetProfile(ctx, "acme", 0); return e }},
		{"empty tenant delete", func() error { return store.DeleteProfile(ctx, "", 1) }},
		{"zero id delete", func() error { return store.DeleteProfile(ctx, "acme", 0) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.fn(); !errors.Is(err, ErrBrokerProfileInvalid) {
				t.Fatalf("%s = %v, want ErrBrokerProfileInvalid", tc.name, err)
			}
		})
	}
}

// A sealed barrier tenant fails closed: Save/Get propagate the sealed error and
// never fall back to the shared store.
func TestBackendBrokerProfile_SealedFailsClosed(t *testing.T) {
	kv := newRoutingTestKV()
	kv.barrierTenant("acme")
	kv.sealTenant("acme")
	store := NewBackendBrokerProfileStore(kv)
	ctx := context.Background()

	if err := store.SaveProfile(ctx, sampleProfile("acme", 1)); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("SaveProfile on sealed tenant = %v, want sealed error", err)
	}
	if _, err := store.GetProfile(ctx, "acme", 1); !errors.Is(err, errTenantSealedTest) {
		t.Fatalf("GetProfile on sealed tenant = %v, want sealed error", err)
	}
	if kv.shared.keyCount() != 0 {
		t.Fatal("sealed tenant fell back to the shared store")
	}
}

// A corrupted on-disk record surfaces as an internal error from GetProfile,
// not a successful decode or a not-found.
func TestBackendBrokerProfile_CorruptRecord(t *testing.T) {
	kv := newRoutingTestKV()
	st := kv.barrierTenant("acme")
	store := NewBackendBrokerProfileStore(kv)

	st.data[brokerProfileKey("acme", 5)] = []byte("{not-valid-json")
	_, err := store.GetProfile(context.Background(), "acme", 5)
	if err == nil {
		t.Fatal("GetProfile on a corrupt record should error")
	}
	if errors.Is(err, ErrBrokerProfileNotFound) {
		t.Fatalf("corrupt record reported as not-found: %v", err)
	}
}
