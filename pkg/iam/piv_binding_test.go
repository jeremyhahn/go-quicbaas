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
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// TestUser_PIVBindingHelpers exercises the PIV cert-binding helpers on the user
// model: setting, listing, single-value access, and the non-expired predicate.
func TestUser_PIVBindingHelpers(t *testing.T) {
	u := &User{ID: 1, TenantID: "t1"}

	// Empty user: every accessor reports no binding.
	if got := u.PIVThumbprints(); len(got) != 0 {
		t.Fatalf("empty user: expected no thumbprints, got %v", got)
	}
	if u.PIVThumbprint() != "" {
		t.Fatalf("empty user: expected blank single thumbprint, got %q", u.PIVThumbprint())
	}
	if u.HasPIVThumbprint("anything") {
		t.Fatal("empty user must not match any thumbprint")
	}
	if u.HasPIVThumbprint("") {
		t.Fatal("a blank thumbprint must never match")
	}

	// Set one binding.
	u.SetPIVThumbprint("thumb-1")
	if got := u.PIVThumbprints(); len(got) != 1 || got[0] != "thumb-1" {
		t.Fatalf("expected [thumb-1], got %v", got)
	}
	if u.PIVThumbprint() != "thumb-1" {
		t.Fatalf("expected single thumbprint thumb-1, got %q", u.PIVThumbprint())
	}
	if !u.HasPIVThumbprint("thumb-1") {
		t.Fatal("expected thumb-1 to match")
	}
	if len(u.CertBindings) != 1 || u.CertBindings[0].Kind != qrdbsdk.CertBindingPIVAttested {
		t.Fatalf("binding must be kind PIVAttested, got %+v", u.CertBindings)
	}

	// SetPIVThumbprint replaces (not appends) the PIV binding.
	u.SetPIVThumbprint("thumb-2")
	if got := u.PIVThumbprints(); len(got) != 1 || got[0] != "thumb-2" {
		t.Fatalf("SetPIVThumbprint must replace; got %v", got)
	}
	if u.HasPIVThumbprint("thumb-1") {
		t.Fatal("old thumbprint must be gone after replace")
	}

	// Clearing removes the binding entirely.
	u.SetPIVThumbprint("")
	if len(u.CertBindings) != 0 {
		t.Fatalf("clearing must leave no bindings, got %+v", u.CertBindings)
	}
}

// TestUser_SetPIVThumbprint_PreservesNonPIVBindings verifies SetPIVThumbprint
// only touches PIV-attested bindings and leaves other binding kinds intact.
func TestUser_SetPIVThumbprint_PreservesNonPIVBindings(t *testing.T) {
	u := &User{ID: 1, TenantID: "t1"}
	u.CertBindings = []qrdbsdk.CertBinding{
		{Thumbprint: "spki-1", Kind: qrdbsdk.CertBindingSPKIThumbprint},
		{Thumbprint: "old-piv", Kind: qrdbsdk.CertBindingPIVAttested},
	}

	u.SetPIVThumbprint("new-piv")

	var sawSPKI, sawPIV bool
	for _, b := range u.CertBindings {
		switch b.Kind {
		case qrdbsdk.CertBindingSPKIThumbprint:
			sawSPKI = b.Thumbprint == "spki-1"
		case qrdbsdk.CertBindingPIVAttested:
			sawPIV = b.Thumbprint == "new-piv"
		}
	}
	if !sawSPKI {
		t.Fatal("SetPIVThumbprint must preserve the non-PIV SPKI binding")
	}
	if !sawPIV {
		t.Fatal("SetPIVThumbprint must install the new PIV binding")
	}
	if u.HasPIVThumbprint("old-piv") {
		t.Fatal("old PIV binding must be replaced")
	}
}

// TestUser_HasPIVThumbprint_ExpiredRejected ensures a PIV binding past its
// NotAfter is never resolved — a decommissioned credential must fail closed.
func TestUser_HasPIVThumbprint_ExpiredRejected(t *testing.T) {
	now := time.Now().UTC()
	u := &User{ID: 1, TenantID: "t1"}
	u.CertBindings = []qrdbsdk.CertBinding{
		{Thumbprint: "expired", Kind: qrdbsdk.CertBindingPIVAttested, NotAfter: now.Add(-time.Hour)},
		{Thumbprint: "valid", Kind: qrdbsdk.CertBindingPIVAttested, NotAfter: now.Add(time.Hour)},
	}

	if u.HasPIVThumbprint("expired") {
		t.Fatal("an expired PIV binding must be rejected (fail closed)")
	}
	if !u.HasPIVThumbprint("valid") {
		t.Fatal("a non-expired PIV binding must resolve")
	}
}

// TestMemoryStorage_PIV_MultiBindingLookup verifies a user holding more than one
// PIV binding is resolvable through any of its thumbprints, and that removing
// one binding leaves the other resolvable.
func TestMemoryStorage_PIV_MultiBindingLookup(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	user := &User{ID: 1, TenantID: "t1", Email: "multi@test.com"}
	user.CertBindings = []qrdbsdk.CertBinding{
		{Thumbprint: "card-a", Kind: qrdbsdk.CertBindingPIVAttested},
		{Thumbprint: "card-b", Kind: qrdbsdk.CertBindingPIVAttested},
	}
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	for _, tp := range []string{"card-a", "card-b"} {
		got, err := storage.GetUserByPIVThumbprint(ctx, tp)
		if err != nil {
			t.Fatalf("lookup %q: %v", tp, err)
		}
		if got.ID != 1 {
			t.Fatalf("lookup %q: expected 1, got %d", tp, got.ID)
		}
	}

	// Drop one binding; the other must still resolve, the dropped one must not.
	user.CertBindings = []qrdbsdk.CertBinding{
		{Thumbprint: "card-b", Kind: qrdbsdk.CertBindingPIVAttested},
	}
	if err := storage.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser drop: %v", err)
	}
	if _, err := storage.GetUserByPIVThumbprint(ctx, "card-a"); err == nil {
		t.Fatal("dropped binding card-a must no longer resolve")
	}
	if _, err := storage.GetUserByPIVThumbprint(ctx, "card-b"); err != nil {
		t.Fatalf("retained binding card-b must still resolve: %v", err)
	}
}

// TestMemoryStorage_PIV_UnknownThumbprintFailsClosed verifies an unknown
// thumbprint never resolves a user.
func TestMemoryStorage_PIV_UnknownThumbprintFailsClosed(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryStorage()
	if err := storage.CreateTenant(ctx, &Tenant{ID: "t1", Name: "Test"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user := &User{ID: 1, TenantID: "t1", Email: "a@test.com"}
	user.SetPIVThumbprint("known")
	if err := storage.CreateUser(ctx, user); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := storage.GetUserByPIVThumbprint(ctx, "unknown"); err == nil {
		t.Fatal("unknown thumbprint must not resolve a user")
	}
}

// The persistent-backend PIV multi-binding test was removed in phase C1-F:
// BackendStorage no longer persists users or the PIV lookup index (qrdb owns
// the user record). The MemoryStorage variant above still covers multi-binding
// lookup semantics for the UserStorage seam, and the qrdbadapter test suite
// covers the qrdb-backed index.
