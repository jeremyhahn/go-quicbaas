//go:build rest

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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
)

// These are the unit-tier tests of the adapter: its delegation and its
// failure handling, decided in memory against an authorizer double. What the
// node's real composed authorizer decides for each caller and scope — role
// grants, the tenant binding, the system-tenant rule — is pinned end to end
// against a running daemon in
// test/integration/tenantbarrier/enable_encryption_wire_test.go.

// Before the qrdb node is up, no decision can be made: the authorizer must
// say so with the typed error the REST handler maps to 503, never admit.
func TestRESTBarrierScopeAuthorizer_NoNodeFailsClosed(t *testing.T) {
	authz := &restBarrierScopeAuthorizer{server: &Server{}}

	err := authz.Authorize(context.Background(),
		&qrdbsdk.Identity{Subject: "root", TenantID: qrdbsdk.SystemTenantID, Roles: []string{"admin"}},
		"seal", "enable_encryption")

	var unavailable *restapi.BarrierScopeAuthorizerUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Authorize without a node = %v, want *BarrierScopeAuthorizerUnavailableError", err)
	}
}

// recordingAuthorizer is a qrdbsdk.Authorizer double that records the call it
// received and answers with a fixed error.
type recordingAuthorizer struct {
	calls    int
	ctx      context.Context
	identity *qrdbsdk.Identity
	resource string
	action   string
	err      error
}

func (r *recordingAuthorizer) Authorize(ctx context.Context, identity *qrdbsdk.Identity, resource, action string) error {
	r.calls++
	r.ctx = ctx
	r.identity = identity
	r.resource = resource
	r.action = action
	return r.err
}

// serverWithNodeAuthorizer returns a Server whose captured node authorizer is
// authz, exactly as nodeAuthorizerCapture records it at node construction.
func serverWithNodeAuthorizer(authz qrdbsdk.Authorizer) *Server {
	s := &Server{}
	s.nodeAuthorizer.Store(&authz)
	return s
}

// The adapter hands the node's authorizer the caller's context, identity,
// resource and action verbatim: the identity pointer itself, so an app
// token's scp-derived ScopePermissions reach the decision, and the context
// carrying the tenant the REST handler bound.
func TestRESTBarrierScopeAuthorizer_DelegatesVerbatim(t *testing.T) {
	inner := &recordingAuthorizer{}
	authz := &restBarrierScopeAuthorizer{server: serverWithNodeAuthorizer(inner)}

	ctx := qrdbsdk.ContextWithTenant(context.Background(), "acme")
	identity := &qrdbsdk.Identity{Subject: "app", TenantID: "acme",
		ScopePermissions: []string{"tenant_barrier:enable_encryption"}}

	if err := authz.Authorize(ctx, identity, "tenant_barrier", "enable_encryption"); err != nil {
		t.Fatalf("Authorize = %v, want the node authorizer's nil", err)
	}
	if inner.calls != 1 {
		t.Fatalf("node authorizer called %d times, want 1", inner.calls)
	}
	if inner.identity != identity {
		t.Fatalf("node authorizer received identity %+v, want the caller's own %+v", inner.identity, identity)
	}
	if inner.resource != "tenant_barrier" || inner.action != "enable_encryption" {
		t.Fatalf("node authorizer received %s:%s, want tenant_barrier:enable_encryption", inner.resource, inner.action)
	}
	if got, ok := qrdbsdk.TenantFromContext(inner.ctx); !ok || got != "acme" {
		t.Fatalf("node authorizer received tenant %q (bound=%v), want the bound acme", got, ok)
	}
}

// A node denial is returned unchanged — the REST handler maps it to 403 — and
// is never reported as the authorizer being unavailable (503).
func TestRESTBarrierScopeAuthorizer_DenialPassesThrough(t *testing.T) {
	denial := errors.New("permission denied: seal:enable_encryption")
	inner := &recordingAuthorizer{err: denial}
	authz := &restBarrierScopeAuthorizer{server: serverWithNodeAuthorizer(inner)}

	err := authz.Authorize(context.Background(),
		&qrdbsdk.Identity{Subject: "nobody", TenantID: "acme"}, "seal", "enable_encryption")
	if !errors.Is(err, denial) {
		t.Fatalf("Authorize = %v, want the node authorizer's denial", err)
	}
	var unavailable *restapi.BarrierScopeAuthorizerUnavailableError
	if errors.As(err, &unavailable) {
		t.Fatalf("a node denial was reported as the authorizer unavailable: %v", err)
	}
}

// The node is resolved per call, not at construction: an adapter built before
// the node (as lifecycle builds it) fails closed until the capture records the
// node's authorizer, and delegates from then on.
func TestRESTBarrierScopeAuthorizer_ResolvesNodePerCall(t *testing.T) {
	s := &Server{}
	authz := &restBarrierScopeAuthorizer{server: s}
	identity := &qrdbsdk.Identity{Subject: "root", TenantID: qrdbsdk.SystemTenantID, Roles: []string{"admin"}}

	var unavailable *restapi.BarrierScopeAuthorizerUnavailableError
	if err := authz.Authorize(context.Background(), identity, "seal", "enable_encryption"); !errors.As(err, &unavailable) {
		t.Fatalf("Authorize before the node = %v, want *BarrierScopeAuthorizerUnavailableError", err)
	}

	inner := &recordingAuthorizer{}
	var node qrdbsdk.Authorizer = inner
	s.nodeAuthorizer.Store(&node)

	if err := authz.Authorize(context.Background(), identity, "seal", "enable_encryption"); err != nil {
		t.Fatalf("Authorize after the node = %v, want the node authorizer's nil", err)
	}
	if inner.calls != 1 {
		t.Fatalf("node authorizer called %d times, want 1", inner.calls)
	}
}
