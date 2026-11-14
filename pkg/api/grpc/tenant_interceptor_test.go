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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"google.golang.org/grpc"
)

const tenantInterceptorTrustDomain = "example.com"

type fakeServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (f *fakeServerStream) Context() context.Context { return f.ctx }

func tenantClientCert(t *testing.T, trustDomain, tenantID string) *x509.Certificate {
	t.Helper()
	spiffeURI, err := url.Parse("spiffe://" + trustDomain + "/tenant/" + tenantID + "/user/alice")
	if err != nil {
		t.Fatalf("parse spiffe uri: %v", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "alice"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		URIs:         []*url.URL{spiffeURI},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert
}

func TestUnaryTenantInterceptor_StampsQRDBTenant(t *testing.T) {
	interceptor := UnaryTenantInterceptor(tenantInterceptorTrustDomain)
	ctx := mtlsPeerContext(tenantClientCert(t, tenantInterceptorTrustDomain, "tenant-a"))
	var observed string
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		if tenantID, ok := qrdbsdk.TenantFromContext(ctx); ok {
			observed = tenantID
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
	if observed != "tenant-a" {
		t.Fatalf("handler observed tenant %q, want tenant-a", observed)
	}
}

func TestTenantContextFromVerifiedPeer_BindsCertTenant(t *testing.T) {
	ctx := mtlsPeerContext(tenantClientCert(t, tenantInterceptorTrustDomain, "tenant-a"))

	scoped, err := TenantContextFromVerifiedPeer(ctx, tenantInterceptorTrustDomain)
	if err != nil {
		t.Fatalf("TenantContextFromVerifiedPeer: %v", err)
	}
	if tenantID, ok := qrdbsdk.TenantFromContext(scoped); !ok || tenantID != "tenant-a" {
		t.Fatalf("bound tenant = %q (%v), want tenant-a", tenantID, ok)
	}
}

func TestTenantContextFromVerifiedPeer_NoCertLeavesContextUnbound(t *testing.T) {
	ctx := context.Background()

	scoped, err := TenantContextFromVerifiedPeer(ctx, tenantInterceptorTrustDomain)
	if err != nil {
		t.Fatalf("TenantContextFromVerifiedPeer: %v", err)
	}
	if _, ok := qrdbsdk.TenantFromContext(scoped); ok {
		t.Fatal("a certless call bound a tenant")
	}
}

func TestTenantContextFromVerifiedPeer_ForeignTrustDomainFails(t *testing.T) {
	ctx := mtlsPeerContext(tenantClientCert(t, "other.example", "tenant-a"))

	_, err := TenantContextFromVerifiedPeer(ctx, tenantInterceptorTrustDomain)
	if !errors.Is(err, qrdbsdk.ErrInvalidClientCertificate) {
		t.Fatalf("err = %v, want ErrInvalidClientCertificate", err)
	}
}

// The subject OU carries the tenant (#1388): an OU naming the SAN's tenant
// binds it, and an OU naming another tenant is refused.
func TestTenantContextFromVerifiedPeer_OUMustAgreeWithSAN(t *testing.T) {
	agree := tenantClientCert(t, tenantInterceptorTrustDomain, "tenant-a")
	agree.Subject.OrganizationalUnit = []string{"tenant-a"}
	got, err := TenantContextFromVerifiedPeer(mtlsPeerContext(agree), tenantInterceptorTrustDomain)
	if err != nil {
		t.Fatalf("agreeing OU: %v", err)
	}
	if tenant, _ := qrdbsdk.TenantFromContext(got); tenant != "tenant-a" {
		t.Fatalf("tenant = %q, want tenant-a", tenant)
	}

	disagree := tenantClientCert(t, tenantInterceptorTrustDomain, "tenant-a")
	disagree.Subject.OrganizationalUnit = []string{"tenant-b"}
	_, err = TenantContextFromVerifiedPeer(mtlsPeerContext(disagree), tenantInterceptorTrustDomain)
	var mismatch *iam.CertOUTenantMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("disagreeing OU: err = %v, want *iam.CertOUTenantMismatchError", err)
	}
}

func TestUnaryTenantInterceptor_NoCertProceeds(t *testing.T) {
	interceptor := UnaryTenantInterceptor(tenantInterceptorTrustDomain)
	called := false
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		called = true
		if _, ok := qrdbsdk.TenantFromContext(ctx); ok {
			t.Fatal("no tenant should be stamped without a peer cert")
		}
		return "ok", nil
	}

	if _, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/svc/M"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("handler must still be called when no cert is present")
	}
}

func TestUnaryTenantInterceptor_NoTenantCertFails(t *testing.T) {
	interceptor := UnaryTenantInterceptor(tenantInterceptorTrustDomain)
	ctx := mtlsPeerContext(grpcTestCert(t))
	handler := func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("handler must not be called when qrdb cannot derive a tenant")
		return nil, nil
	}

	if _, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/svc/M"}, handler); !errors.Is(err, qrdbsdk.ErrInvalidClientCertificate) {
		t.Fatalf("err = %v, want ErrInvalidClientCertificate", err)
	}
}

func TestUnaryTenantInterceptor_TrustDomainMismatchFails(t *testing.T) {
	interceptor := UnaryTenantInterceptor(tenantInterceptorTrustDomain)
	ctx := mtlsPeerContext(tenantClientCert(t, "other.example", "tenant-a"))
	handler := func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("handler must not be called when qrdb rejects the trust domain")
		return nil, nil
	}

	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/svc/M"}, handler)
	if !errors.Is(err, qrdbsdk.ErrInvalidClientCertificate) {
		t.Fatalf("err = %v, want ErrInvalidClientCertificate", err)
	}
	if !errors.Is(err, qrdbsdk.ErrSpiffeTrustDomainMismatch) {
		t.Fatalf("err = %v, want ErrSpiffeTrustDomainMismatch", err)
	}
}

func TestStreamTenantInterceptor_StampsQRDBTenant(t *testing.T) {
	interceptor := StreamTenantInterceptor(tenantInterceptorTrustDomain)
	ss := &fakeServerStream{ctx: mtlsPeerContext(tenantClientCert(t, tenantInterceptorTrustDomain, "tenant-a"))}
	var observed string
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		if tenantID, ok := qrdbsdk.TenantFromContext(stream.Context()); ok {
			observed = tenantID
		}
		return nil
	}

	if err := interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/svc/Stream"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if observed != "tenant-a" {
		t.Fatalf("stream handler observed tenant %q, want tenant-a", observed)
	}
}

func TestStreamTenantInterceptor_NoCertProceeds(t *testing.T) {
	interceptor := StreamTenantInterceptor(tenantInterceptorTrustDomain)
	ss := &fakeServerStream{ctx: context.Background()}
	called := false
	handler := func(_ interface{}, stream grpc.ServerStream) error {
		called = true
		if _, ok := qrdbsdk.TenantFromContext(stream.Context()); ok {
			t.Fatal("no tenant should be stamped without a peer cert")
		}
		return nil
	}

	if err := interceptor(nil, ss, &grpc.StreamServerInfo{FullMethod: "/svc/Stream"}, handler); err != nil {
		t.Fatalf("interceptor returned error: %v", err)
	}
	if !called {
		t.Fatal("stream handler must still be called when no cert is present")
	}
}
