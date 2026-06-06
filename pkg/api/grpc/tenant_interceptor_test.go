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

package grpc

import (
	"context"
	"crypto/x509"
	"testing"

	"google.golang.org/grpc"
)

type tenantTestKey struct{}

// tenantStamp is a TenantContextFunc that records the cert it saw and stamps a
// sentinel tenant onto the context, mirroring what the iam-backed resolver does
// without pulling the iam build tag into this transport package's tests.
func tenantStamp(tenant string, sawCert *bool) TenantContextFunc {
	return func(ctx context.Context, cert *x509.Certificate) context.Context {
		if sawCert != nil {
			*sawCert = cert != nil
		}
		return context.WithValue(ctx, tenantTestKey{}, tenant)
	}
}

// fakeServerStream is a minimal grpc.ServerStream exposing a fixed context.
type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

// The unary interceptor stamps the tenant when a verified peer cert is present.
func TestUnaryTenantInterceptor_StampsTenant(t *testing.T) {
	var sawCert bool
	interceptor := UnaryTenantInterceptor(tenantStamp("tenant-acme", &sawCert))

	ctx := mtlsPeerContext(grpcTestCert(t))
	var observed string
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		if v, ok := ctx.Value(tenantTestKey{}).(string); ok {
			observed = v
		}
		return "ok", nil
	}

	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/svc/Method"}, handler)
	if err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("resp = %v, want ok", resp)
	}
	if !sawCert {
		t.Fatal("resolver should have received the verified peer cert")
	}
	if observed != "tenant-acme" {
		t.Fatalf("handler observed tenant %q, want tenant-acme", observed)
	}
}

// The unary interceptor is a no-op pass-through when there is no peer cert: it
// proceeds, returns no error, and never invokes the resolver.
func TestUnaryTenantInterceptor_NoCertProceeds(t *testing.T) {
	var sawCert bool
	resolverInvoked := false
	resolve := func(ctx context.Context, cert *x509.Certificate) context.Context {
		resolverInvoked = true
		sawCert = cert != nil
		return ctx
	}
	interceptor := UnaryTenantInterceptor(resolve)

	called := false
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		called = true
		if _, ok := ctx.Value(tenantTestKey{}).(string); ok {
			t.Fatal("no tenant should be stamped without a peer cert")
		}
		return "ok", nil
	}

	// Plain context: no peer info at all.
	if _, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/M"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler must still be called when no cert is present")
	}
	if resolverInvoked {
		t.Fatal("resolver must not be invoked without a verified peer cert")
	}
	_ = sawCert
}

// A nil resolve func makes the unary interceptor a transparent pass-through
// even when a peer cert is present.
func TestUnaryTenantInterceptor_NilResolver(t *testing.T) {
	interceptor := UnaryTenantInterceptor(nil)
	ctx := mtlsPeerContext(grpcTestCert(t))
	called := false
	handler := func(_ context.Context, _ interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	}
	if _, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/svc/M"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler must be called with a nil resolver")
	}
}

// The stream interceptor stamps the tenant onto the wrapped stream's context.
func TestStreamTenantInterceptor_StampsTenant(t *testing.T) {
	interceptor := StreamTenantInterceptor(tenantStamp("tenant-acme", nil))

	ss := &fakeServerStream{ctx: mtlsPeerContext(grpcTestCert(t))}
	var observed string
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		if v, ok := stream.Context().Value(tenantTestKey{}).(string); ok {
			observed = v
		}
		return nil
	}

	if err := interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/svc/Stream"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if observed != "tenant-acme" {
		t.Fatalf("stream handler observed tenant %q, want tenant-acme", observed)
	}
}

// The stream interceptor proceeds unchanged when no peer cert is present.
func TestStreamTenantInterceptor_NoCertProceeds(t *testing.T) {
	interceptor := StreamTenantInterceptor(tenantStamp("tenant-acme", nil))

	ss := &fakeServerStream{ctx: context.Background()}
	called := false
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		called = true
		if _, ok := stream.Context().Value(tenantTestKey{}).(string); ok {
			t.Fatal("no tenant should be stamped without a peer cert")
		}
		return nil
	}
	if err := interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/svc/S"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler must still be called when no cert is present")
	}
}
