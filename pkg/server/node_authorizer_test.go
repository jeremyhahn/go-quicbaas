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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	authmw "github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

// assertRefused checks that authz refuses the request with the hold's error,
// that the refusal is in the unavailable class (503, UNAVAILABLE) and that
// the inner authorizer is never called.
func assertRefused(t *testing.T, authz qrdbsdk.Authorizer, rec *countingAuthorizer, ctx context.Context, identity *qrdbsdk.Identity, resource, action string) {
	t.Helper()
	before := rec.calls.Load()
	err := authz.Authorize(ctx, identity, resource, action)
	if !errors.Is(err, qrdbsdk.ErrTenantStoreSealed) {
		t.Fatalf("Authorize(%s, %s) on a held tenant = %v, want ErrTenantStoreSealed", resource, action, err)
	}
	var unavailable *authmw.StoreUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Authorize(%s, %s) on a held tenant = %T, want *middleware.StoreUnavailableError", resource, action, err)
	}
	if got := authmw.HTTPStatus(err, http.StatusUnauthorized); got != http.StatusServiceUnavailable {
		t.Fatalf("Authorize(%s, %s) on a held tenant maps to HTTP %d, want 503", resource, action, got)
	}
	if got := status.Code(authmw.GRPCError(err, codes.Unauthenticated)); got != codes.Unavailable {
		t.Fatalf("Authorize(%s, %s) on a held tenant maps to gRPC %v, want Unavailable", resource, action, got)
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

// fixedAuthenticator authenticates every request as a copy of identity.
type fixedAuthenticator struct {
	identity qrdbsdk.Identity
}

func (f fixedAuthenticator) Authenticate(context.Context) (*qrdbsdk.Identity, error) {
	identity := f.identity
	return &identity, nil
}

func (fixedAuthenticator) Name() string { return "test" }

// A held tenant's request is answered unavailable by the node's own REST
// middleware and gRPC interceptor, not as a failed credential, and never
// reaches the handler or the inner authorizer.
func TestAdmittingAuthorizer_HeldTenantIsUnavailableOnNativeTransports(t *testing.T) {
	s := &Server{}
	gateErr := errors.New("policy reload failed")
	s.tenantUnsealHooks.RegisterGate(service.TenantUnsealGateFunc(func(context.Context, string) error {
		return gateErr
	}))
	barrier := service.NewTenantBarrierService(unsealingTenantClient{}, service.WithTenantUnsealHooks(&s.tenantUnsealHooks))
	rec := &countingAuthorizer{}
	authz := s.wrapNodeAuthorizer(rec)
	if err := barrier.TenantBarrierUnseal(tenantAdminCtx("acme"), "acme", []byte("key")); !errors.Is(err, gateErr) {
		t.Fatalf("the gate's failure must fail the unseal, got %v", err)
	}

	mw := authmw.New(authz, authmw.WithAuthenticator(fixedAuthenticator{
		identity: qrdbsdk.Identity{Subject: "admin", TenantID: "acme"},
	}))
	var handled atomic.Int64

	routes := authmw.NewRESTRouteRegistry()
	routes.Register(http.MethodGet, "/v1/kv/held", permission.KVGet)
	rest := mw.RESTMiddleware(routes, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		handled.Add(1)
	}))
	resp := httptest.NewRecorder()
	rest.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, "/v1/kv/held", nil))
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("REST request to a held tenant = %d (%s), want 503", resp.Code, resp.Body.String())
	}

	_, err := mw.UnaryInterceptor()(context.Background(), nil,
		&ggrpc.UnaryServerInfo{FullMethod: "/qrdb.v1.QRDBService/Get"},
		func(context.Context, any) (any, error) {
			handled.Add(1)
			return nil, nil
		})
	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("gRPC request to a held tenant = %v (%v), want Unavailable", got, err)
	}

	if handled.Load() != 0 {
		t.Fatalf("a held tenant's request reached the handler %d time(s)", handled.Load())
	}
	if rec.calls.Load() != 0 {
		t.Fatalf("a held tenant's request reached the inner authorizer %d time(s)", rec.calls.Load())
	}
}
