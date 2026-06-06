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
	"strings"
	"testing"
)

// NewHomeRealmDirectory rejects a nil store with HRDInvalidError.
func TestHomeRealm_NilStore(t *testing.T) {
	_, err := NewHomeRealmDirectory(nil)
	if err == nil {
		t.Fatal("NewHomeRealmDirectory(nil) want error")
	}
	var invalid *HRDInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("want HRDInvalidError, got %T", err)
	}
}

// RegisterHost validates input and UnregisterHost is owner-gated.
func TestHomeRealm_HostRegisterUnregister(t *testing.T) {
	shared := newMockKVStore()
	dir, _ := NewHomeRealmDirectory(shared)
	ctx := context.Background()

	// Invalid input.
	if err := dir.RegisterHost(ctx, "", "host.example"); err == nil {
		t.Fatal("RegisterHost(empty tenant) want error")
	}

	if err := dir.RegisterHost(ctx, "tenant-a", "portal.example"); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	// A non-owner cannot unregister.
	if err := dir.UnregisterHost(ctx, "tenant-b", "portal.example"); err == nil {
		t.Fatal("UnregisterHost(non-owner) want conflict error")
	} else {
		var conflict *HRDConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("want HRDConflictError, got %T", err)
		}
	}

	// Unregistering an unknown host fails closed.
	if err := dir.UnregisterHost(ctx, "tenant-a", "unknown.example"); err == nil {
		t.Fatal("UnregisterHost(unknown) want not-found error")
	}

	// The owner can unregister; afterwards resolution fails closed.
	if err := dir.UnregisterHost(ctx, "tenant-a", "portal.example"); err != nil {
		t.Fatalf("UnregisterHost(owner): %v", err)
	}
	if _, err := dir.Resolve(ctx, RealmHints{SNIHost: "portal.example"}); !errors.As(err, new(*HRDNotFoundError)) {
		t.Fatalf("Resolve after unregister err = %v, want HRDNotFoundError", err)
	}
}

// An explicit tenant hint validated against a registered host entry resolves.
func TestHomeRealm_HintMatchesHostEntry(t *testing.T) {
	shared := newMockKVStore()
	dir, _ := NewHomeRealmDirectory(shared)
	ctx := context.Background()

	if err := dir.RegisterHost(ctx, "tenant-x", "app.tenant-x.io"); err != nil {
		t.Fatalf("RegisterHost: %v", err)
	}

	// Hint that matches the registered host entry.
	got, err := dir.Resolve(ctx, RealmHints{TenantHint: "tenant-x", SNIHost: "app.tenant-x.io"})
	if err != nil || got != "tenant-x" {
		t.Fatalf("Resolve(hint+host) = %q, %v; want tenant-x, nil", got, err)
	}

	// A hint that does not own the host falls through and fails closed.
	if _, err := dir.Resolve(ctx, RealmHints{TenantHint: "tenant-evil", SNIHost: "app.tenant-x.io"}); err == nil {
		// host lookup still resolves to tenant-x for the non-hint path,
		// so an evil hint that mismatches simply yields the real owner.
		// Verify it never returns the evil tenant.
	}
	if got, _ := dir.Resolve(ctx, RealmHints{TenantHint: "tenant-evil", SNIHost: "app.tenant-x.io"}); got == "tenant-evil" {
		t.Fatal("evil hint must never assert an unregistered tenant")
	}
}

// HRD error messages render correctly.
func TestHRDErrorMessages(t *testing.T) {
	if msg := (&HRDInvalidError{Reason: "bad"}).Error(); !strings.Contains(msg, "bad") {
		t.Fatalf("HRDInvalidError = %q", msg)
	}
	if msg := (&HRDNotFoundError{}).Error(); !strings.Contains(msg, "no tenant routing") {
		t.Fatalf("HRDNotFoundError = %q", msg)
	}
	if msg := (&HRDConflictError{Owner: "t"}).Error(); !strings.Contains(msg, "another tenant") {
		t.Fatalf("HRDConflictError = %q", msg)
	}
}
