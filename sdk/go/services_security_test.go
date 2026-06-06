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

package quicbaas

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"testing"
	"time"

	trustedca "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	trustbootstrap "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/bootstrap"
)

// =============================================================================
// Test fixtures and helpers
// =============================================================================

// errBoom is a sentinel error for asserting the transport error path.
var errBoom = errors.New("boom: transport failure")

// makeSelfSignedCert returns a deterministic, in-memory X.509 certificate
// suitable for tests that need a real *x509.Certificate. Keys are ephemeral
// per-call.
func makeSelfSignedCert(t *testing.T) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate test key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{Organization: []string{"quicbaas-sdk-test"}},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageCertSign,
		IsCA:         true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("failed to create test cert: %v", err)
	}
	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		t.Fatalf("failed to parse test cert: %v", err)
	}
	return cert
}

// pemEncode emits a CERTIFICATE PEM block for the supplied DER bytes.
func pemEncode(derBytes []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
}

// =============================================================================
// acmeServerImpl tests
// =============================================================================

func TestACMEServer_RegisterChallenge_InvalidInput(t *testing.T) {
	srv := newACMEServer(&mockTransport{})

	noopVerifier := func(resolver *net.Resolver, ca trustedca.ACMECertificateAuthority, authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
		return nil
	}

	tests := []struct {
		name     string
		ctx      context.Context
		nameArg  string
		verifier trustedca.ACMEChallengeVerifierFunc
	}{
		{"empty name", context.Background(), "", noopVerifier},
		{"nil verifier", context.Background(), "http-01", nil},
		{"nil context", nil, "http-01", noopVerifier},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := srv.RegisterChallenge(tc.ctx, tc.nameArg, tc.verifier)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestACMEServer_RegisterChallenge_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errBoom
		},
	}
	srv := newACMEServer(mt)
	v := func(resolver *net.Resolver, ca trustedca.ACMECertificateAuthority, authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
		return nil
	}
	if err := srv.RegisterChallenge(context.Background(), "http-01", v); err == nil {
		t.Fatal("expected transport error to propagate")
	}
	// Local verifier must still be cached for re-use, but registered must be false.
	if got := srv.Verifier("http-01"); got == nil {
		t.Error("verifier should be cached locally even after transport error")
	}
	if srv.registered["http-01"] {
		t.Error("registered map should not mark success after transport error")
	}
}

func TestACMEServer_RegisterChallenge_Idempotent(t *testing.T) {
	calls := 0
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			calls++
			return nil, nil
		},
	}
	srv := newACMEServer(mt)
	v := func(resolver *net.Resolver, ca trustedca.ACMECertificateAuthority, authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
		return nil
	}

	if err := srv.RegisterChallenge(context.Background(), "http-01", v); err != nil {
		t.Fatalf("first RegisterChallenge: %v", err)
	}
	if err := srv.RegisterChallenge(context.Background(), "http-01", v); err != nil {
		t.Fatalf("second RegisterChallenge: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected idempotent transport call (1), got %d", calls)
	}
	if got := srv.Verifier("http-01"); got == nil {
		t.Error("expected non-nil verifier")
	}
	if got := srv.Verifier("nonexistent"); got != nil {
		t.Error("expected nil verifier for unknown name")
	}
}

func TestACMEServer_Issue_InvalidInput(t *testing.T) {
	srv := newACMEServer(&mockTransport{})
	csr := []byte("csr-bytes")

	tests := []struct {
		name string
		csr  []byte
		typ  string
		ctx  context.Context
	}{
		{"empty csr", nil, "http-01", context.Background()},
		{"empty challenge type", csr, "", context.Background()},
		{"nil context", csr, "http-01", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := srv.Issue(tc.ctx, tc.csr, tc.typ, "challenge-value")
			if err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestACMEServer_Issue_TransportError(t *testing.T) {
	mt := &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}
	srv := newACMEServer(mt)
	_, err := srv.Issue(context.Background(), []byte("csr"), "http-01", "value")
	if err == nil {
		t.Fatal("expected transport error to propagate")
	}
}

func TestACMEServer_Issue_NilResponse(t *testing.T) {
	mt := &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, nil
	}}
	srv := newACMEServer(mt)
	_, err := srv.Issue(context.Background(), []byte("csr"), "http-01", "value")
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

func TestACMEServer_Issue_InvalidResponseType(t *testing.T) {
	mt := &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return "not the right type", nil
	}}
	srv := newACMEServer(mt)
	_, err := srv.Issue(context.Background(), []byte("csr"), "http-01", "value")
	if err == nil {
		t.Fatal("expected invalid response type error")
	}
}

func TestACMEServer_Issue_HappyPath(t *testing.T) {
	want := &ACMEIssueResponse{
		CertificatePEM: []byte("cert"),
		ChainPEM:       []byte("chain"),
		SerialNumber:   "S/N",
		NotBefore:      time.Now().UTC().Truncate(time.Second),
		NotAfter:       time.Now().UTC().Add(time.Hour).Truncate(time.Second),
	}
	mt := &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		if service != acmeServiceName || method != "Issue" {
			t.Errorf("unexpected dispatch: %s/%s", service, method)
		}
		return want, nil
	}}
	srv := newACMEServer(mt)
	got, err := srv.Issue(context.Background(), []byte("csr"), "http-01", "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// =============================================================================
// acmeClientImpl tests
// =============================================================================

func TestACMEClient_NewOrder_Validation(t *testing.T) {
	c := &acmeClientImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		ids  []ACMEOrderIdentifier
		ctx  context.Context
	}{
		{"empty identifiers", nil, context.Background()},
		{"nil context", []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.NewOrder(tc.ctx, tc.ids, "http-01")
			if err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestACMEClient_NewOrder_TransportError(t *testing.T) {
	c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}}
	_, err := c.NewOrder(context.Background(), []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}}, "http-01")
	if err == nil {
		t.Fatal("expected transport error to propagate")
	}
}

func TestACMEClient_NewOrder_NilAndInvalidResponse(t *testing.T) {
	t.Run("nil response", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, nil
		}}}
		_, err := c.NewOrder(context.Background(), []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}}, "http-01")
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return 42, nil
		}}}
		_, err := c.NewOrder(context.Background(), []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}}, "http-01")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestACMEClient_NewOrder_HappyPath(t *testing.T) {
	want := &ACMEOrder{Status: ACMEStatusValid, URL: "https://acme.example/order/1"}
	c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.NewOrder(context.Background(), []ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}}, "http-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestACMEClient_FinalizeOrder_Validation(t *testing.T) {
	c := &acmeClientImpl{t: &mockTransport{}}
	tests := []struct {
		name     string
		orderURL string
		csr      []byte
		ctx      context.Context
	}{
		{"empty url", "", []byte("csr"), context.Background()},
		{"empty csr", "url", nil, context.Background()},
		{"nil ctx", "url", []byte("csr"), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.FinalizeOrder(tc.ctx, tc.orderURL, tc.csr)
			if err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestACMEClient_FinalizeOrder_TransportAndType(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		_, err := c.FinalizeOrder(context.Background(), "url", []byte("csr"))
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, nil
		}}}
		_, err := c.FinalizeOrder(context.Background(), "url", []byte("csr"))
		if err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		_, err := c.FinalizeOrder(context.Background(), "url", []byte("csr"))
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestACMEClient_FinalizeOrder_HappyPath(t *testing.T) {
	want := []byte("CERT-PEM")
	c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &acmeFinalizeOrderResponse{CertificatePEM: want}, nil
	}}}
	got, err := c.FinalizeOrder(context.Background(), "url", []byte("csr"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestACMEClient_Renew_Validation(t *testing.T) {
	c := &acmeClientImpl{t: &mockTransport{}}
	t.Run("empty url", func(t *testing.T) {
		if _, err := c.Renew(context.Background(), ""); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := c.Renew(nil, "url"); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestACMEClient_Renew_TransportAndTypeErrors(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.Renew(context.Background(), "url"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{}}
		if _, err := c.Renew(context.Background(), "url"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return 1.0, nil
		}}}
		if _, err := c.Renew(context.Background(), "url"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestACMEClient_Renew_HappyPath(t *testing.T) {
	want := []byte("RENEWED-CERT")
	c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &acmeRenewResponse{CertificatePEM: want}, nil
	}}}
	got, err := c.Renew(context.Background(), "url")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// =============================================================================
// acmeServiceImpl composite tests
// =============================================================================

func TestNewACMEService_ReturnsBothClients(t *testing.T) {
	svc := newACMEService(&mockTransport{})
	if svc == nil {
		t.Fatal("expected non-nil ACMEService")
	}
	if svc.Server() == nil {
		t.Error("expected non-nil Server()")
	}
	if svc.Client() == nil {
		t.Error("expected non-nil Client()")
	}
}

// =============================================================================
// trustServiceImpl tests
// =============================================================================

func TestTrust_PublishDANE_Validation(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{}}
	cert := makeSelfSignedCert(t)
	tests := []struct {
		name string
		ctx  context.Context
		host string
		port int
		c    *x509.Certificate
	}{
		{"empty name", context.Background(), "", 443, cert},
		{"non-positive port", context.Background(), "example.com", 0, cert},
		{"nil cert", context.Background(), "example.com", 443, nil},
		{"nil ctx", nil, "example.com", 443, cert},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := s.PublishDANE(tc.ctx, tc.host, tc.port, tc.c)
			if err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestTrust_PublishDANE_TransportError(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}}
	cert := makeSelfSignedCert(t)
	if err := s.PublishDANE(context.Background(), "example.com", 443, cert); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestTrust_PublishDANE_HappyPath(t *testing.T) {
	cert := makeSelfSignedCert(t)
	var capturedReq *trustPublishDANERequest
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		capturedReq = req.(*trustPublishDANERequest)
		return nil, nil
	}}}
	if err := s.PublishDANE(context.Background(), "example.com", 443, cert); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedReq == nil || capturedReq.Name != "example.com" || capturedReq.Port != 443 {
		t.Errorf("transport got wrong payload: %+v", capturedReq)
	}
	if len(capturedReq.CertDER) == 0 {
		t.Error("expected non-empty DER payload")
	}
}

func TestTrust_CrossSignedRootPool_Validation(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{}}
	if _, err := s.CrossSignedRootPool(nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput, got %v", err)
	}
}

func TestTrust_CrossSignedRootPool_TransportError(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}}
	if _, err := s.CrossSignedRootPool(context.Background()); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestTrust_CrossSignedRootPool_NilResponse_ReturnsEmptyPool(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{}}
	pool, err := s.CrossSignedRootPool(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

func TestTrust_CrossSignedRootPool_InvalidResponseType(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return "wrong", nil
	}}}
	if _, err := s.CrossSignedRootPool(context.Background()); err == nil {
		t.Fatal("expected error for invalid response type")
	}
}

func TestTrust_CrossSignedRootPool_DecodesPEM(t *testing.T) {
	cert := makeSelfSignedCert(t)
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &TrustCrossSignedRootPoolResponse{
			RootPEMs: [][]byte{pemEncode(cert.Raw)},
		}, nil
	}}}
	pool, err := s.CrossSignedRootPool(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

func TestTrust_CrossSignedRootPool_DecodesDER(t *testing.T) {
	cert := makeSelfSignedCert(t)
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &TrustCrossSignedRootPoolResponse{
			RootPEMs: [][]byte{cert.Raw}, // DER fallback
		}, nil
	}}}
	pool, err := s.CrossSignedRootPool(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

func TestTrust_CrossSignedRootPool_BadPEM(t *testing.T) {
	bogusPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a cert")})
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &TrustCrossSignedRootPoolResponse{RootPEMs: [][]byte{bogusPEM}}, nil
	}}}
	if _, err := s.CrossSignedRootPool(context.Background()); err == nil {
		t.Fatal("expected error parsing invalid PEM cert")
	}
}

func TestTrust_CrossSignedRootPool_BadDER(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &TrustCrossSignedRootPoolResponse{RootPEMs: [][]byte{[]byte("totally invalid")}}, nil
	}}}
	if _, err := s.CrossSignedRootPool(context.Background()); err == nil {
		t.Fatal("expected error parsing invalid DER cert")
	}
}

func TestTrust_AutoImportAIA_Validation(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{}}
	cert := makeSelfSignedCert(t)
	tests := []struct {
		name  string
		ctx   context.Context
		chain []*x509.Certificate
	}{
		{"empty chain", context.Background(), nil},
		{"nil ctx", nil, []*x509.Certificate{cert}},
		{"nil chain entry", context.Background(), []*x509.Certificate{cert, nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := s.AutoImportAIA(tc.ctx, tc.chain)
			if err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestTrust_AutoImportAIA_TransportError(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}}
	cert := makeSelfSignedCert(t)
	if err := s.AutoImportAIA(context.Background(), []*x509.Certificate{cert}); err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestTrust_AutoImportAIA_HappyPath(t *testing.T) {
	cert := makeSelfSignedCert(t)
	var captured *trustAutoImportAIARequest
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		captured = req.(*trustAutoImportAIARequest)
		return nil, nil
	}}}
	if err := s.AutoImportAIA(context.Background(), []*x509.Certificate{cert, cert}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if captured == nil || len(captured.ChainDER) != 2 {
		t.Errorf("expected 2 chain entries, got %+v", captured)
	}
}

func TestTrust_Bootstrap_ReturnsClient(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{}}
	if s.Bootstrap() == nil {
		t.Fatal("expected non-nil bootstrap client")
	}
}

func TestNewTrustService_NonNil(t *testing.T) {
	svc := newTrustService(&mockTransport{})
	if svc == nil {
		t.Fatal("expected non-nil TrustService")
	}
}

// =============================================================================
// trustBootstrapImpl tests
// =============================================================================

func TestTrustBootstrap_DeriveNoiseAddr(t *testing.T) {
	b := trustBootstrapImpl{}
	got := b.DeriveNoiseAddr("https://example.com:443")
	if got == "" {
		t.Error("expected non-empty noise address")
	}
}

func TestTrustBootstrap_Fetch_ErrorPath(t *testing.T) {
	// Fetch validates mode/url before doing any I/O. An unconfigured
	// Config returns an error immediately (e.g., ErrServerURLRequired or
	// ErrInvalidMode), proving the delegation works without touching the
	// network.
	b := trustBootstrapImpl{}
	_, err := b.Fetch(context.Background(), trustbootstrap.Config{})
	if err == nil {
		t.Fatal("expected error from Fetch with empty config")
	}
}

// =============================================================================
// certExtImpl tests
// =============================================================================

func TestCertExt_CrossSign_Validation(t *testing.T) {
	c := &certExtImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		root []byte
		ctx  context.Context
	}{
		{"empty root", nil, context.Background()},
		{"nil ctx", []byte("pem"), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.CrossSign(tc.ctx, tc.root); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestCertExt_CrossSign_TransportAndType(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.CrossSign(context.Background(), []byte("p")); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{}}
		if _, err := c.CrossSign(context.Background(), []byte("p")); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.CrossSign(context.Background(), []byte("p")); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestCertExt_CrossSign_HappyPath(t *testing.T) {
	want := []byte("CROSS-SIGNED")
	c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &CertCrossSignResponse{CrossSignedPEM: want}, nil
	}}}
	got, err := c.CrossSign(context.Background(), []byte("p"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCertExt_Chain_Validation(t *testing.T) {
	c := &certExtImpl{t: &mockTransport{}}
	t.Run("empty pem", func(t *testing.T) {
		if _, err := c.Chain(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := c.Chain(nil, []byte("p")); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestCertExt_Chain_TransportAndType(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.Chain(context.Background(), []byte("p")); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{}}
		if _, err := c.Chain(context.Background(), []byte("p")); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return 12, nil
		}}}
		if _, err := c.Chain(context.Background(), []byte("p")); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestCertExt_Chain_HappyPath(t *testing.T) {
	want := [][]byte{[]byte("LEAF"), []byte("ROOT")}
	c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &CertChainResponse{ChainPEM: want}, nil
	}}}
	got, err := c.Chain(context.Background(), []byte("p"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
}

func TestCertExt_IssueDeviceIdentity_Validation(t *testing.T) {
	c := &certExtImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *DeviceIdentityRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"empty type", &DeviceIdentityRequest{DeviceID: "d"}, context.Background()},
		{"empty deviceid", &DeviceIdentityRequest{Type: DeviceIdentityEK}, context.Background()},
		{"nil ctx", &DeviceIdentityRequest{Type: DeviceIdentityEK, DeviceID: "d"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.IssueDeviceIdentity(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestCertExt_IssueDeviceIdentity_TransportAndType(t *testing.T) {
	req := &DeviceIdentityRequest{Type: DeviceIdentityIDevID, DeviceID: "d-1"}
	t.Run("transport error", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.IssueDeviceIdentity(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{}}
		if _, err := c.IssueDeviceIdentity(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.IssueDeviceIdentity(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestCertExt_IssueDeviceIdentity_HappyPath(t *testing.T) {
	want := &DeviceIdentityResponse{SerialNumber: "S/N", CertificatePEM: []byte("pem")}
	c := &certExtImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.IssueDeviceIdentity(context.Background(), &DeviceIdentityRequest{Type: DeviceIdentityAK, DeviceID: "d"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNewCertificateExtensions_NonNil(t *testing.T) {
	if newCertificateExtensions(&mockTransport{}) == nil {
		t.Fatal("expected non-nil CertificateExtensions")
	}
}

// =============================================================================
// fido2Impl tests
// =============================================================================

func TestFIDO2_ListDevices_Validation(t *testing.T) {
	c := &fido2Impl{t: &mockTransport{}}
	if _, err := c.ListDevices(nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput, got %v", err)
	}
}

func TestFIDO2_ListDevices_TransportError(t *testing.T) {
	c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}}
	if _, err := c.ListDevices(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestFIDO2_ListDevices_NilResponseReturnsEmpty(t *testing.T) {
	c := &fido2Impl{t: &mockTransport{}}
	got, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestFIDO2_ListDevices_InvalidType(t *testing.T) {
	c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return "wrong", nil
	}}}
	if _, err := c.ListDevices(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestFIDO2_ListDevices_HappyPath(t *testing.T) {
	want := &FIDO2ListDevicesResponse{Devices: []FIDO2Device{{DeviceID: "d1"}}}
	c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFIDO2_DeviceInfo_Validation(t *testing.T) {
	c := &fido2Impl{t: &mockTransport{}}
	t.Run("empty id", func(t *testing.T) {
		if _, err := c.DeviceInfo(context.Background(), ""); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := c.DeviceInfo(nil, "d"); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestFIDO2_DeviceInfo_TransportAndType(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.DeviceInfo(context.Background(), "d"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{}}
		if _, err := c.DeviceInfo(context.Background(), "d"); err == nil || !sdkerrors.IsNotFound(err) {
			t.Errorf("expected NotFound, got %v", err)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.DeviceInfo(context.Background(), "d"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_DeviceInfo_HappyPath(t *testing.T) {
	want := &FIDO2DeviceInfo{DeviceID: "d1", AAGUID: "aaguid"}
	c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.DeviceInfo(context.Background(), "d1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFIDO2_Register_Validation(t *testing.T) {
	c := &fido2Impl{t: &mockTransport{}}
	t.Run("nil opts", func(t *testing.T) {
		if _, err := c.Register(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := c.Register(nil, &FIDO2BeginRegistrationRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestFIDO2_Register_TransportAndType(t *testing.T) {
	req := &FIDO2BeginRegistrationRequest{}
	t.Run("transport error", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.Register(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{}}
		if _, err := c.Register(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.Register(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_Register_HappyPath(t *testing.T) {
	want := &FIDO2BeginRegistrationResponse{}
	c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.Register(context.Background(), &FIDO2BeginRegistrationRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFIDO2_Assert_Validation(t *testing.T) {
	c := &fido2Impl{t: &mockTransport{}}
	t.Run("nil opts", func(t *testing.T) {
		if _, err := c.Assert(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := c.Assert(nil, &FIDO2BeginAuthenticationRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestFIDO2_Assert_TransportAndType(t *testing.T) {
	req := &FIDO2BeginAuthenticationRequest{}
	t.Run("transport error", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.Assert(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{}}
		if _, err := c.Assert(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return 7, nil
		}}}
		if _, err := c.Assert(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_Assert_HappyPath(t *testing.T) {
	want := &FIDO2BeginAuthenticationResponse{}
	c := &fido2Impl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.Assert(context.Background(), &FIDO2BeginAuthenticationRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// =============================================================================
// webauthnImpl tests
// =============================================================================

func TestWebAuthn_BeginRegistration_Validation(t *testing.T) {
	c := &webauthnImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		user *WebAuthnUser
		ctx  context.Context
	}{
		{"nil user", nil, context.Background()},
		{"empty username", &WebAuthnUser{}, context.Background()},
		{"nil ctx", &WebAuthnUser{Username: "u"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.BeginRegistration(tc.ctx, tc.user); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestWebAuthn_BeginRegistration_TransportAndType(t *testing.T) {
	user := &WebAuthnUser{Username: "u"}
	t.Run("transport error", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.BeginRegistration(context.Background(), user); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{}}
		if _, err := c.BeginRegistration(context.Background(), user); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return 7, nil
		}}}
		if _, err := c.BeginRegistration(context.Background(), user); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestWebAuthn_BeginRegistration_HappyPath(t *testing.T) {
	want := &FIDO2BeginRegistrationResponse{}
	c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.BeginRegistration(context.Background(), &WebAuthnUser{Username: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWebAuthn_FinishRegistration_Validation(t *testing.T) {
	c := &webauthnImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		user *WebAuthnUser
		resp *FIDO2FinishRegistrationRequest
		ctx  context.Context
	}{
		{"nil user", nil, &FIDO2FinishRegistrationRequest{}, context.Background()},
		{"nil response", &WebAuthnUser{Username: "u"}, nil, context.Background()},
		{"nil ctx", &WebAuthnUser{Username: "u"}, &FIDO2FinishRegistrationRequest{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.FinishRegistration(tc.ctx, tc.user, tc.resp); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestWebAuthn_FinishRegistration_TransportAndType(t *testing.T) {
	user := &WebAuthnUser{Username: "u"}
	resp := &FIDO2FinishRegistrationRequest{}
	t.Run("transport error", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.FinishRegistration(context.Background(), user, resp); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{}}
		if _, err := c.FinishRegistration(context.Background(), user, resp); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.FinishRegistration(context.Background(), user, resp); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestWebAuthn_FinishRegistration_HappyPath(t *testing.T) {
	want := &WebAuthnCredential{CredentialID: "cred-1"}
	c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.FinishRegistration(context.Background(), &WebAuthnUser{Username: "u"}, &FIDO2FinishRegistrationRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWebAuthn_BeginLogin_Validation(t *testing.T) {
	c := &webauthnImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		user *WebAuthnUser
		ctx  context.Context
	}{
		{"nil user", nil, context.Background()},
		{"empty username", &WebAuthnUser{}, context.Background()},
		{"nil ctx", &WebAuthnUser{Username: "u"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.BeginLogin(tc.ctx, tc.user); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestWebAuthn_BeginLogin_TransportAndType(t *testing.T) {
	user := &WebAuthnUser{Username: "u"}
	t.Run("transport error", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.BeginLogin(context.Background(), user); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{}}
		if _, err := c.BeginLogin(context.Background(), user); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return 4, nil
		}}}
		if _, err := c.BeginLogin(context.Background(), user); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestWebAuthn_BeginLogin_HappyPath(t *testing.T) {
	want := &FIDO2BeginAuthenticationResponse{}
	c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.BeginLogin(context.Background(), &WebAuthnUser{Username: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestWebAuthn_FinishLogin_Validation(t *testing.T) {
	c := &webauthnImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		user *WebAuthnUser
		resp *FIDO2FinishAuthenticationRequest
		ctx  context.Context
	}{
		{"nil user", nil, &FIDO2FinishAuthenticationRequest{}, context.Background()},
		{"nil response", &WebAuthnUser{Username: "u"}, nil, context.Background()},
		{"nil ctx", &WebAuthnUser{Username: "u"}, &FIDO2FinishAuthenticationRequest{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.FinishLogin(tc.ctx, tc.user, tc.resp); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestWebAuthn_FinishLogin_TransportAndType(t *testing.T) {
	user := &WebAuthnUser{Username: "u"}
	resp := &FIDO2FinishAuthenticationRequest{}
	t.Run("transport error", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.FinishLogin(context.Background(), user, resp); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{}}
		if _, err := c.FinishLogin(context.Background(), user, resp); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.FinishLogin(context.Background(), user, resp); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestWebAuthn_FinishLogin_HappyPath(t *testing.T) {
	want := &WebAuthnSession{Token: "tok-1"}
	c := &webauthnImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.FinishLogin(context.Background(), &WebAuthnUser{Username: "u"}, &FIDO2FinishAuthenticationRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// =============================================================================
// pivImpl tests
// =============================================================================

func TestPIV_ListReaders_Validation(t *testing.T) {
	c := &pivImpl{t: &mockTransport{}}
	if _, err := c.ListReaders(nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput, got %v", err)
	}
}

func TestPIV_ListReaders_TransportAndType(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.ListReaders(context.Background()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response returns empty", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{}}
		got, err := c.ListReaders(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil PIVListReadersResponse")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.ListReaders(context.Background()); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPIV_ListReaders_HappyPath(t *testing.T) {
	want := &PIVListReadersResponse{Readers: []PIVReader{{Name: "yubikey"}}}
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.ListReaders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPIV_ListSlots_Validation(t *testing.T) {
	c := &pivImpl{t: &mockTransport{}}
	t.Run("empty reader", func(t *testing.T) {
		if _, err := c.ListSlots(context.Background(), ""); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil ctx", func(t *testing.T) {
		if _, err := c.ListSlots(nil, "r"); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestPIV_ListSlots_TransportAndType(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.ListSlots(context.Background(), "r"); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response returns empty", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{}}
		got, err := c.ListSlots(context.Background(), "r")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil {
			t.Fatal("expected non-nil PIVListSlotsResponse")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.ListSlots(context.Background(), "r"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPIV_ListSlots_HappyPath(t *testing.T) {
	want := &PIVListSlotsResponse{}
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.ListSlots(context.Background(), "r")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPIV_GetCertificate_Validation(t *testing.T) {
	c := &pivImpl{t: &mockTransport{}}
	tests := []struct {
		name, reader, slot string
		ctx                context.Context
	}{
		{"empty reader", "", "9a", context.Background()},
		{"empty slot", "r", "", context.Background()},
		{"nil ctx", "r", "9a", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.GetCertificate(tc.ctx, tc.reader, tc.slot); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestPIV_GetCertificate_TransportError(t *testing.T) {
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}}
	if _, err := c.GetCertificate(context.Background(), "r", "9a"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPIV_GetCertificate_NilResponse(t *testing.T) {
	c := &pivImpl{t: &mockTransport{}}
	if _, err := c.GetCertificate(context.Background(), "r", "9a"); err == nil || !sdkerrors.IsNotFound(err) {
		t.Errorf("expected NotFound, got %v", err)
	}
}

func TestPIV_GetCertificate_InvalidResponseType(t *testing.T) {
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return "wrong", nil
	}}}
	if _, err := c.GetCertificate(context.Background(), "r", "9a"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPIV_GetCertificate_BadDER(t *testing.T) {
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &PIVGetCertificateResponsePayload{CertDER: []byte("not a cert")}, nil
	}}}
	if _, err := c.GetCertificate(context.Background(), "r", "9a"); err == nil {
		t.Fatal("expected DER parse error")
	}
}

func TestPIV_GetCertificate_HappyPath(t *testing.T) {
	cert := makeSelfSignedCert(t)
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &PIVGetCertificateResponsePayload{CertDER: cert.Raw}, nil
	}}}
	got, err := c.GetCertificate(context.Background(), "r", "9a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil cert")
	}
}

func TestPIV_Sign_Validation(t *testing.T) {
	c := &pivImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *PIVSignRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"empty reader", &PIVSignRequest{Slot: "9a", Digest: []byte{1}}, context.Background()},
		{"empty slot", &PIVSignRequest{Reader: "r", Digest: []byte{1}}, context.Background()},
		{"empty digest", &PIVSignRequest{Reader: "r", Slot: "9a"}, context.Background()},
		{"nil ctx", &PIVSignRequest{Reader: "r", Slot: "9a", Digest: []byte{1}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Sign(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestPIV_Sign_TransportAndType(t *testing.T) {
	req := &PIVSignRequest{Reader: "r", Slot: "9a", Digest: []byte{1}}
	t.Run("transport error", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.Sign(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{}}
		if _, err := c.Sign(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.Sign(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPIV_Sign_HappyPath(t *testing.T) {
	want := &PIVSignResponse{Signature: []byte{0xDE, 0xAD}, Algo: "ecdsa"}
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.Sign(context.Background(), &PIVSignRequest{Reader: "r", Slot: "9a", Digest: []byte{1}, Algo: "ecdsa"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPIV_Attest_Validation(t *testing.T) {
	c := &pivImpl{t: &mockTransport{}}
	tests := []struct {
		name, reader, slot string
		ctx                context.Context
	}{
		{"empty reader", "", "9a", context.Background()},
		{"empty slot", "r", "", context.Background()},
		{"nil ctx", "r", "9a", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Attest(tc.ctx, tc.reader, tc.slot); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestPIV_Attest_TransportError(t *testing.T) {
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return nil, errBoom
	}}}
	if _, err := c.Attest(context.Background(), "r", "9a"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPIV_Attest_NilResponse_ReturnsUnsupported(t *testing.T) {
	c := &pivImpl{t: &mockTransport{}}
	_, err := c.Attest(context.Background(), "r", "9a")
	if err != ErrPIVAttestationUnsupported {
		t.Errorf("expected ErrPIVAttestationUnsupported, got %v", err)
	}
}

func TestPIV_Attest_InvalidType(t *testing.T) {
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return "wrong", nil
	}}}
	if _, err := c.Attest(context.Background(), "r", "9a"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPIV_Attest_HappyPath(t *testing.T) {
	want := &PIVAttestResponse{AttestationCertPEM: []byte("pem")}
	c := &pivImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.Attest(context.Background(), "r", "9a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// =============================================================================
// authFactorsImpl tests
// =============================================================================

func TestAuthFactors_Add_Validation(t *testing.T) {
	c := &authFactorsImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *AuthFactorEnrollRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"missing factor", &AuthFactorEnrollRequest{Subject: "s"}, context.Background()},
		{"missing subject", &AuthFactorEnrollRequest{FactorName: "fido2"}, context.Background()},
		{"nil ctx", &AuthFactorEnrollRequest{FactorName: "fido2", Subject: "s"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Add(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

// newAuthFactorsImplNoStore builds an authFactorsImpl whose qrdb sub-client
// cannot be constructed (TransportEmbedded without WithEmbeddedQRDB), so the
// user-login factor routing surfaces a typed error rather than silently
// reaching a store. This locks in the fail-closed contract: enrollment never
// proceeds without the qrdb UserAuthFactor store login reads.
func newAuthFactorsImplNoStore() *authFactorsImpl {
	opts := defaultOptions()
	opts.transportType = TransportEmbedded
	c := &client{opts: opts, address: "embedded://local"}
	return &authFactorsImpl{c: c, t: &mockTransport{}}
}

func TestAuthFactors_Add_FailsClosedWithoutStore(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	// mtls supplies a derivable credential, so the request passes credential
	// derivation and reaches the (unavailable) store, which must error.
	req := &AuthFactorEnrollRequest{
		FactorName:   "mtls",
		Subject:      "s",
		MethodParams: map[string]any{"credential_sha256": "00ff"},
	}
	if _, err := c.Add(context.Background(), req); err == nil {
		t.Fatal("expected error when the qrdb UserAuthFactor store is unavailable")
	}
}

func TestAuthFactors_Add_RejectsUnknownType(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	if _, err := c.Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName: "totp", Subject: "s",
	}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("expected InvalidInput for unsupported factor type, got %v", err)
	}
}

func TestAuthFactors_Add_FIDO2RequiresCredentialID(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	// fido2 without credential_id must fail closed: storing a fido2 factor
	// without the credentialID lookup key would make it unresolvable at login.
	if _, err := c.Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName: "fido2", Subject: "s",
	}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("expected InvalidInput when fido2 credential_id is absent, got %v", err)
	}
}

func TestAuthFactors_Add_MTLSRequiresCredentialSHA256(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	if _, err := c.Add(context.Background(), &AuthFactorEnrollRequest{
		FactorName: "mtls", Subject: "s",
	}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("expected InvalidInput when mtls credential_sha256 is absent, got %v", err)
	}
}

func TestAuthFactors_List_Validation(t *testing.T) {
	c := &authFactorsImpl{t: &mockTransport{}}
	if _, err := c.List(nil, ""); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput, got %v", err)
	}
}

func TestAuthFactors_List_RequiresSubject(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	if _, err := c.List(context.Background(), ""); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("expected InvalidInput when subject is empty, got %v", err)
	}
}

func TestAuthFactors_List_FailsClosedWithoutStore(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	if _, err := c.List(context.Background(), "user-1"); err == nil {
		t.Fatal("expected error when the qrdb UserAuthFactor store is unavailable")
	}
}

func TestAuthFactors_Remove_Validation(t *testing.T) {
	c := &authFactorsImpl{t: &mockTransport{}}
	t.Run("empty id", func(t *testing.T) {
		if err := c.Remove(context.Background(), "", false); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil ctx", func(t *testing.T) {
		if err := c.Remove(nil, "id", false); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestAuthFactors_Remove_FailsClosedWithoutStore(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	if err := c.Remove(context.Background(), "id", true); err == nil {
		t.Fatal("expected error when the qrdb UserAuthFactor store is unavailable")
	}
}

func TestAuthFactors_Rotate_Validation(t *testing.T) {
	c := &authFactorsImpl{t: &mockTransport{}}
	tests := []struct {
		name  string
		oldID string
		nf    *AuthFactorEnrollRequest
		ctx   context.Context
	}{
		{"empty old id", "", &AuthFactorEnrollRequest{FactorName: "fido2"}, context.Background()},
		{"nil new factor", "old", nil, context.Background()},
		{"missing new factor name", "old", &AuthFactorEnrollRequest{}, context.Background()},
		{"nil ctx", "old", &AuthFactorEnrollRequest{FactorName: "fido2"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Rotate(tc.ctx, tc.oldID, tc.nf); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestAuthFactors_Rotate_RequiresNewSubject(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	// A new factor without a subject cannot resolve a uint64 user id.
	if _, err := c.Rotate(context.Background(), "old", &AuthFactorEnrollRequest{
		FactorName: "fido2",
	}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("expected InvalidInput when new factor subject is empty, got %v", err)
	}
}

func TestAuthFactors_Rotate_FailsClosedWithoutStore(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	nf := &AuthFactorEnrollRequest{
		FactorName:   "mtls",
		Subject:      "s",
		MethodParams: map[string]any{"credential_sha256": "00ff"},
	}
	if _, err := c.Rotate(context.Background(), "old", nf); err == nil {
		t.Fatal("expected error when the qrdb UserAuthFactor store is unavailable")
	}
}

func TestAuthFactors_Relabel_Validation(t *testing.T) {
	c := &authFactorsImpl{t: &mockTransport{}}
	tests := []struct {
		name, id, label string
		ctx             context.Context
	}{
		{"empty id", "", "label", context.Background()},
		{"empty label", "id", "", context.Background()},
		{"nil ctx", "id", "label", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := c.Relabel(tc.ctx, tc.id, tc.label); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestAuthFactors_Relabel_FailsClosedWithoutStore(t *testing.T) {
	c := newAuthFactorsImplNoStore()
	if err := c.Relabel(context.Background(), "id", "label"); err == nil {
		t.Fatal("expected error when the qrdb UserAuthFactor store is unavailable")
	}
}

func TestAuthFactors_Login_Validation(t *testing.T) {
	c := &authFactorsImpl{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *AuthFactorLoginRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"empty username", &AuthFactorLoginRequest{}, context.Background()},
		{"nil ctx", &AuthFactorLoginRequest{Username: "u"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Login(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestAuthFactors_Login_TransportAndType(t *testing.T) {
	req := &AuthFactorLoginRequest{Username: "u"}
	t.Run("transport error", func(t *testing.T) {
		c := &authFactorsImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.Login(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &authFactorsImpl{t: &mockTransport{}}
		if _, err := c.Login(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &authFactorsImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.Login(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAuthFactors_Login_HappyPath(t *testing.T) {
	want := &AuthFactorLoginResponse{UserID: "u-1", Token: "tok"}
	c := &authFactorsImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.Login(context.Background(), &AuthFactorLoginRequest{Username: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// =============================================================================
// authServiceImpl tests
// =============================================================================

func TestAuthService_Accessors(t *testing.T) {
	mt := &mockTransport{}
	c := &client{transport: mt, opts: defaultOptions()}
	svc := newAuthService(c)

	if svc.FIDO2() == nil {
		t.Error("FIDO2() should return non-nil")
	}
	if svc.WebAuthn() == nil {
		t.Error("WebAuthn() should return non-nil")
	}
	if svc.PIV() == nil {
		t.Error("PIV() should return non-nil")
	}
	if svc.Factors() == nil {
		t.Error("Factors() should return non-nil")
	}
}

// =============================================================================
// extendedSecurityService wiring tests (sync.Once paths in services.go)
// =============================================================================

func TestSecurity_ACME_Idempotent(t *testing.T) {
	mt := &mockTransport{}
	c := &client{transport: mt, opts: defaultOptions()}
	sec := c.Security().(*securityService)
	first := sec.ACME()
	second := sec.ACME()
	if first == nil || second == nil {
		t.Fatal("ACME() must not return nil")
	}
	if first != second {
		t.Error("ACME() must return the same instance across calls (sync.Once)")
	}
}

func TestSecurity_Trust_Idempotent(t *testing.T) {
	mt := &mockTransport{}
	c := &client{transport: mt, opts: defaultOptions()}
	sec := c.Security().(*securityService)
	first := sec.Trust()
	second := sec.Trust()
	if first == nil || second == nil {
		t.Fatal("Trust() must not return nil")
	}
	if first != second {
		t.Error("Trust() must return the same instance across calls (sync.Once)")
	}
}

func TestSecurity_CertificateExt_Idempotent(t *testing.T) {
	mt := &mockTransport{}
	c := &client{transport: mt, opts: defaultOptions()}
	sec := c.Security().(*securityService)
	first := sec.CertificateExt()
	second := sec.CertificateExt()
	if first == nil || second == nil {
		t.Fatal("CertificateExt() must not return nil")
	}
	if first != second {
		t.Error("CertificateExt() must return the same instance across calls (sync.Once)")
	}
}

func TestSecurity_Auth_Idempotent(t *testing.T) {
	mt := &mockTransport{}
	c := &client{transport: mt, opts: defaultOptions()}
	sec := c.Security().(*securityService)
	first := sec.Auth()
	second := sec.Auth()
	if first == nil || second == nil {
		t.Fatal("Auth() must not return nil")
	}
	if first != second {
		t.Error("Auth() must return the same instance across calls (sync.Once)")
	}
}

// =============================================================================
// NewClientWithTransport tests
// =============================================================================

func TestNewClientWithTransport_NilReturnsNil(t *testing.T) {
	if c := NewClientWithTransport(nil); c != nil {
		t.Errorf("expected nil client when transport is nil, got %v", c)
	}
}

func TestNewClientWithTransport_NonNil(t *testing.T) {
	c := NewClientWithTransport(&mockTransport{})
	if c == nil {
		t.Fatal("expected non-nil client")
	}
	if err := c.Close(); err != nil {
		t.Errorf("Close should succeed: %v", err)
	}
}
