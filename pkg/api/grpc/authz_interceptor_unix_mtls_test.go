package grpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// certAuthorizerFake stands in for the node's authorizer: it refuses every
// bearer (the callers below carry none) and, on the certificate seam, admits
// only the administrator's certificate for the permission the method maps to.
type certAuthorizerFake struct {
	adminCN   string
	gotPerms  []string
	gotLeaves []*x509.Certificate
}

func (f *certAuthorizerFake) Authorize(context.Context, string, string, string, string, string) (*platformtoken.AuthorizedContext, error) {
	return nil, &platformtoken.TokenInactiveError{}
}

func (f *certAuthorizerFake) AuthorizeVerifiedCertificate(_ context.Context, leaf *x509.Certificate, requiredPerm string) (*platformtoken.AuthorizedContext, error) {
	f.gotPerms = append(f.gotPerms, requiredPerm)
	f.gotLeaves = append(f.gotLeaves, leaf)
	if leaf.Subject.CommonName != f.adminCN {
		return nil, &platformtoken.ScopeDeniedError{}
	}
	return &platformtoken.AuthorizedContext{}, nil
}

func newUnixMTLSTestCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return cert
}

func unixPeerContext(state tls.ConnectionState) context.Context {
	return peer.NewContext(context.Background(), &peer.Peer{
		AuthInfo: credentials.TLSInfo{State: state},
	})
}

// TestAuthorizeGRPCRequest_UnixVerifiedAdminCertificate drives the Unix
// listener's authorization with an mTLS-only caller, as the node's TCP
// listener sees it: a CA-verified administrator certificate and no bearer.
func TestAuthorizeGRPCRequest_UnixVerifiedAdminCertificate(t *testing.T) {
	const method = "/qrdb.v1.QRDBService/GetUserByUsername"
	resource, action, ok := grpcResourceAndAction(method)
	if !ok {
		t.Fatalf("%s has no permission mapping", method)
	}
	wantPerm := resource + ":" + action

	admin := newUnixMTLSTestCert(t, "admin")
	other := newUnixMTLSTestCert(t, "someone-else")

	t.Run("verified administrator certificate is admitted", func(t *testing.T) {
		authz := &certAuthorizerFake{adminCN: "admin"}
		ctx := unixPeerContext(tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{admin},
			VerifiedChains:   [][]*x509.Certificate{{admin}},
		})
		if _, err := authorizeGRPCRequest(ctx, method, authz, nil); err != nil {
			t.Fatalf("verified administrator certificate refused: %v", err)
		}
		if len(authz.gotPerms) != 1 || authz.gotPerms[0] != wantPerm {
			t.Fatalf("certificate checked against %v, want [%s]", authz.gotPerms, wantPerm)
		}
		if authz.gotLeaves[0] != admin {
			t.Fatalf("certificate seam got a different leaf")
		}
	})

	t.Run("verified certificate still meets the permission check", func(t *testing.T) {
		authz := &certAuthorizerFake{adminCN: "admin"}
		ctx := unixPeerContext(tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{other},
			VerifiedChains:   [][]*x509.Certificate{{other}},
		})
		_, err := authorizeGRPCRequest(ctx, method, authz, nil)
		if status.Code(err) != codes.PermissionDenied {
			t.Fatalf("non-administrator certificate: got %v, want PermissionDenied", err)
		}
	})

	refused := []struct {
		name string
		ctx  context.Context
	}{
		{"no certificate and no bearer", context.Background()},
		{"peer certificate without a verified chain", unixPeerContext(tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{admin},
		})},
	}
	for _, tc := range refused {
		t.Run(tc.name+" is refused", func(t *testing.T) {
			authz := &certAuthorizerFake{adminCN: "admin"}
			_, err := authorizeGRPCRequest(tc.ctx, method, authz, nil)
			if status.Code(err) != codes.Unauthenticated {
				t.Fatalf("got %v, want Unauthenticated", err)
			}
			if len(authz.gotPerms) != 0 {
				t.Fatalf("certificate seam consulted without a verified chain")
			}
		})
	}

	t.Run("authorizer without the certificate seam refuses", func(t *testing.T) {
		ctx := unixPeerContext(tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{admin},
			VerifiedChains:   [][]*x509.Certificate{{admin}},
		})
		_, err := authorizeGRPCRequest(ctx, method, bearerOnlyAuthorizer{}, nil)
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("got %v, want Unauthenticated", err)
		}
	})
}

type bearerOnlyAuthorizer struct{}

func (bearerOnlyAuthorizer) Authorize(context.Context, string, string, string, string, string) (*platformtoken.AuthorizedContext, error) {
	return nil, &platformtoken.TokenInactiveError{}
}
