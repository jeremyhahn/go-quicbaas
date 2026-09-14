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

package iamtest

import (
	"context"
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestTenantRoutingKV_IsolatesBarrierTenants pins the property the double
// exists for: a barrier tenant's keys are invisible to every other context, and
// only that context can read them back.
func TestTenantRoutingKV_IsolatesBarrierTenants(t *testing.T) {
	kv := NewTenantRoutingKV()
	kv.BarrierTenant("burnin")

	burnin := iam.ContextWithTenant(context.Background(), "burnin")
	if err := kv.Put(burnin, "k", []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := kv.Get(burnin, "k")
	if err != nil {
		t.Fatalf("Get under the writing tenant: %v", err)
	}
	if string(got) != "v" {
		t.Fatalf("Get = %q, want v", got)
	}
	if kv.SharedKeyCount() != 0 {
		t.Fatalf("a barrier tenant's write landed in the shared store (%d keys)", kv.SharedKeyCount())
	}

	for _, name := range []string{"", "acme"} {
		ctx := context.Background()
		if name != "" {
			ctx = iam.ContextWithTenant(ctx, name)
		}
		if _, err := kv.Get(ctx, "k"); err == nil {
			t.Fatalf("tenant %q read the burn-in tenant's key", name)
		}
		exists, err := kv.Exists(ctx, "k")
		if err != nil {
			t.Fatalf("Exists under tenant %q: %v", name, err)
		}
		if exists {
			t.Fatalf("tenant %q sees the burn-in tenant's key", name)
		}
	}
}

// TestTenantRoutingKV_SealedTenantFailsClosed is the error path: an
// unresolvable store must refuse every verb rather than fall back to the
// shared namespace, which is the fail-closed contract production relies on.
func TestTenantRoutingKV_SealedTenantFailsClosed(t *testing.T) {
	kv := NewTenantRoutingKV()
	kv.BarrierTenant("burnin")
	kv.SealTenant("burnin")
	ctx := iam.ContextWithTenant(context.Background(), "burnin")

	sealed := &TenantSealedError{}
	assertSealed := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s against a sealed tenant returned nil", name)
		}
		if !errors.As(err, &sealed) {
			t.Fatalf("%s returned %v, want the sealed-tenant refusal", name, err)
		}
	}

	assertSealed("Put", kv.Put(ctx, "k", []byte("v")))
	_, err := kv.Get(ctx, "k")
	assertSealed("Get", err)
	assertSealed("Delete", kv.Delete(ctx, "k"))
	assertSealed("Scan", kv.Scan(ctx, "", func(string, []byte) error { return nil }))
	_, err = kv.List(ctx, "")
	assertSealed("List", err)
	_, err = kv.Exists(ctx, "k")
	assertSealed("Exists", err)

	if kv.SharedKeyCount() != 0 {
		t.Fatalf("a sealed tenant's operations reached the shared store (%d keys)", kv.SharedKeyCount())
	}
}

// TestTenantRoutingKV_ScanAndListSeeOnlyTheContextTenant covers the prefix
// verbs, which a store resolver bug would otherwise leak across.
func TestTenantRoutingKV_ScanAndListSeeOnlyTheContextTenant(t *testing.T) {
	kv := NewTenantRoutingKV()
	kv.BarrierTenant("burnin")

	burnin := iam.ContextWithTenant(context.Background(), "burnin")
	shared := context.Background()
	if err := kv.Put(burnin, "p:1", []byte("a")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := kv.Put(shared, "p:2", []byte("b")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	keys, err := kv.List(burnin, "p:")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 || keys[0] != "p:1" {
		t.Fatalf("List = %v, want [p:1]", keys)
	}

	var scanned []string
	if err := kv.Scan(shared, "p:", func(key string, _ []byte) error {
		scanned = append(scanned, key)
		return nil
	}); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(scanned) != 1 || scanned[0] != "p:2" {
		t.Fatalf("Scan = %v, want [p:2]", scanned)
	}
}

// TestTenantRoutingKV_MissingKeyIsNotFound pins the error the Backend* stores
// branch on: a missing key must be the quicbaas not-found, not a nil value.
func TestTenantRoutingKV_MissingKeyIsNotFound(t *testing.T) {
	kv := NewTenantRoutingKV()
	_, err := kv.Get(context.Background(), "absent")
	if err == nil {
		t.Fatal("Get of an absent key returned nil error")
	}
	if !dberrors.IsNotFound(err) {
		t.Fatalf("Get of an absent key returned %v, want not-found", err)
	}
}
