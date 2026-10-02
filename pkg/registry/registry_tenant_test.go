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

package registry

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestRegistry_OversizeTenantRejected proves a tenant ID longer than the
// platform bound is rejected before any backend call, across the three
// tenant-scoped verbs (Register/Resolve/Watch/Deregister) that resolve the tenant
// from context. This guards the maxTenantLen length check in
// tenantFromContext.
func TestRegistry_OversizeTenantRejected(t *testing.T) {
	r := New(newFakeBackend())
	oversize := strings.Repeat("a", maxTenantLen+1)
	ctx := tenantctx.WithTenant(context.Background(), oversize)

	asInvalid := func(t *testing.T, err error) {
		t.Helper()
		var ie *InvalidInstanceError
		if !errors.As(err, &ie) {
			t.Fatalf("error = %T (%v), want *InvalidInstanceError for oversize tenant", err, err)
		}
	}

	_, err := r.Register(ctx, sampleInstance(), time.Second)
	asInvalid(t, err)
	_, _, err = r.Resolve(ctx, "payments")
	asInvalid(t, err)
	_, err = r.Watch(ctx, "payments", 0)
	asInvalid(t, err)
	err = r.Deregister(ctx, 1)
	asInvalid(t, err)
}

// TestRegistry_WatchPropagatesUpstreamClose proves the forwarding
// goroutine drains the upstream watch channel and closes the downstream
// Event channel when the backend stream ends (the `for resp := range src`
// loop terminating). This is the normal teardown path for Watch.
func TestRegistry_WatchPropagatesUpstreamClose(t *testing.T) {
	b := newFakeBackend()
	r := New(b)
	ch, err := r.Watch(sysCtx(), "payments", 0)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	// Closing the upstream source ends the range loop; the downstream
	// channel must then close so consumers observe EOF rather than hang.
	close(b.watchCh)
	select {
	case _, open := <-ch:
		if open {
			t.Fatal("expected downstream Event channel to close, got an event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("downstream channel was not closed after upstream close")
	}
}

// sysCtx is the context of an in-process caller acting for the platform: the
// system tenant bound explicitly, which the registry files under its
// unprefixed namespace.
func sysCtx() context.Context {
	return tenantctx.WithSystem(context.Background())
}

// TestRegistry_UnboundTenantIsRefused pins go-quicbaas#1150: a context with no
// tenant bound is refused on every verb rather than read as the system
// namespace.
func TestRegistry_UnboundTenantIsRefused(t *testing.T) {
	r := New(newFakeBackend())
	ctx := context.Background()
	asMissing := func(t *testing.T, err error) {
		t.Helper()
		var missing *tenantctx.MissingTenantError
		if !errors.As(err, &missing) {
			t.Fatalf("error = %T (%v), want *tenantctx.MissingTenantError", err, err)
		}
	}
	_, err := r.Register(ctx, sampleInstance(), time.Second)
	asMissing(t, err)
	_, _, err = r.Resolve(ctx, "payments")
	asMissing(t, err)
	_, err = r.Watch(ctx, "payments", 0)
	asMissing(t, err)
	_, err = r.Renew(ctx, 1)
	asMissing(t, err)
	asMissing(t, r.Deregister(ctx, 1))
}
