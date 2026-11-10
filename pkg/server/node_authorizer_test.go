//go:build !integration

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

package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// countingAuthorizer stands in for the node's authorizer: it counts its calls
// and returns err.
type countingAuthorizer struct {
	calls atomic.Int64
	err   error
}

func (c *countingAuthorizer) Authorize(context.Context, *qrdbsdk.Identity, string, string) error {
	c.calls.Add(1)
	return c.err
}

// assertRefused checks that authz refuses the request with the hold's error
// and that the inner authorizer is never called.
func assertRefused(t *testing.T, authz qrdbsdk.Authorizer, rec *countingAuthorizer, ctx context.Context, identity *qrdbsdk.Identity, resource, action string) {
	t.Helper()
	before := rec.calls.Load()
	if err := authz.Authorize(ctx, identity, resource, action); !errors.Is(err, qrdbsdk.ErrTenantStoreSealed) {
		t.Fatalf("Authorize(%s, %s) on a held tenant = %v, want ErrTenantStoreSealed", resource, action, err)
	}
	if rec.calls.Load() != before {
		t.Fatalf("Authorize(%s, %s) on a held tenant reached the inner authorizer", resource, action)
	}
}

// assertReached checks that authz hands the request to the inner authorizer.
func assertReached(t *testing.T, authz qrdbsdk.Authorizer, rec *countingAuthorizer, ctx context.Context, identity *qrdbsdk.Identity, resource, action string) {
	t.Helper()
	before := rec.calls.Load()
	if err := authz.Authorize(ctx, identity, resource, action); err != nil {
		t.Fatalf("Authorize(%s, %s) = %v, want the inner authorizer's nil", resource, action, err)
	}
	if rec.calls.Load() != before+1 {
		t.Fatalf("Authorize(%s, %s) did not reach the inner authorizer", resource, action)
	}
}

// A tenant whose unseal reload failed is held, and every native request that
// targets it is refused at the node authorizer, whichever service it names;
// the barrier verbs that clear the hold and other tenants still pass.
func TestAdmittingAuthorizer_FailedReloadRefusesTheTenant(t *testing.T) {
	s := &Server{}
	gateErr := errors.New("policy reload failed")
	var gateFails atomic.Bool
	gateFails.Store(true)
	s.tenantUnsealHooks.RegisterGate(service.TenantUnsealGateFunc(func(context.Context, string) error {
		if gateFails.Load() {
			return gateErr
		}
		return nil
	}))
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	rec := &countingAuthorizer{}
	authz := s.wrapNodeAuthorizer(rec)
	if got := s.capturedNodeAuthorizer(); got != qrdbsdk.Authorizer(rec) {
		t.Fatalf("the capture must record the node's authorizer unwrapped, got %T", got)
	}
	if s.wrapNodeAuthorizer(nil) != nil {
		t.Fatal("a nil node authorizer must stay nil")
	}

	if err := barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")); !errors.Is(err, gateErr) {
		t.Fatalf("the gate's failure must fail the unseal, got %v", err)
	}

	acme := qrdbsdk.ContextWithTenant(context.Background(), "acme")
	acmeAdmin := &qrdbsdk.Identity{Subject: "admin", TenantID: "acme"}
	for _, ra := range [][2]string{{"kv", "read"}, {"user", "create"}, {"role", "update"}} {
		assertRefused(t, authz, rec, acme, acmeAdmin, ra[0], ra[1])
	}
	systemAdmin := &qrdbsdk.Identity{Subject: "root", TenantID: qrdbsdk.SystemTenantID}
	assertRefused(t, authz, rec, acme, systemAdmin, "user", "create")
	assertRefused(t, authz, rec, context.Background(), acmeAdmin, "kv", "read")

	assertReached(t, authz, rec, qrdbsdk.ContextWithTenant(context.Background(), "beta"),
		&qrdbsdk.Identity{Subject: "admin", TenantID: "beta"}, "kv", "read")
	assertReached(t, authz, rec, acme, acmeAdmin, "tenant_barrier", "unseal")
	assertReached(t, authz, rec, acme, acmeAdmin, "tenant_barrier", "seal")

	gateFails.Store(false)
	if err := barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatalf("an unseal whose gates pass must open the tenant, got %v", err)
	}
	assertReached(t, authz, rec, acme, acmeAdmin, "kv", "read")
	assertReached(t, authz, rec, acme, acmeAdmin, "user", "create")
}

// While a tenant's unseal reload is still running the tenant is held: native
// requests to it are refused, and only the gate's own requests reach the
// node's authorizer until the reload passes.
func TestAdmittingAuthorizer_ReloadInProgressRefusesTheTenant(t *testing.T) {
	s := &Server{}
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	s.tenantUnsealHooks.RegisterGate(service.TenantUnsealGateFunc(func(ctx context.Context, _ string) error {
		started <- ctx
		<-release
		return nil
	}))
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	rec := &countingAuthorizer{}
	authz := s.wrapNodeAuthorizer(rec)

	done := make(chan error, 1)
	go func() {
		done <- barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key"))
	}()
	gateCtx := <-started

	acme := qrdbsdk.ContextWithTenant(context.Background(), "acme")
	acmeAdmin := &qrdbsdk.Identity{Subject: "admin", TenantID: "acme"}
	assertRefused(t, authz, rec, acme, acmeAdmin, "kv", "read")
	assertReached(t, authz, rec, qrdbsdk.ContextWithTenant(gateCtx, "acme"), acmeAdmin, "kv", "read")

	close(release)
	if err := <-done; err != nil {
		t.Fatalf("an unseal whose gates pass must open the tenant, got %v", err)
	}
	assertReached(t, authz, rec, acme, acmeAdmin, "kv", "read")
}

// With no hold the inner authorizer's denial is returned unchanged.
func TestAdmittingAuthorizer_InnerDenialPassesThrough(t *testing.T) {
	s := &Server{}
	denied := errors.New("permission denied")
	rec := &countingAuthorizer{err: denied}
	authz := s.wrapNodeAuthorizer(rec)

	err := authz.Authorize(qrdbsdk.ContextWithTenant(context.Background(), "acme"),
		&qrdbsdk.Identity{Subject: "admin", TenantID: "acme"}, "kv", "write")
	if err != denied {
		t.Fatalf("Authorize = %v, want the inner authorizer's denial unchanged", err)
	}
	if n := rec.calls.Load(); n != 1 {
		t.Fatalf("the inner authorizer was called %d times, want 1", n)
	}
}
