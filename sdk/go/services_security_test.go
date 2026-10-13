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

	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"
	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
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
	wire := &acmeclient.IssueResponse{
		CertificatePEM: []byte("cert"),
		ChainPEM:       []byte("chain"),
		SerialNumber:   "S/N",
	}
	mt := &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		if service != acmeServiceName || method != "Issue" {
			t.Errorf("unexpected dispatch: %s/%s", service, method)
		}
		return wire, nil
	}}
	srv := newACMEServer(mt)
	got, err := srv.Issue(context.Background(), []byte("csr"), "http-01", "value")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got.CertificatePEM) != "cert" || string(got.ChainPEM) != "chain" || got.SerialNumber != "S/N" {
		t.Errorf("unexpected issue response: %+v", got)
	}
}

// =============================================================================
// acmeClientImpl tests
// =============================================================================
//
// QuicBaaS is the broker-as-CA, so the protocol-client verbs are in-process
// service calls routed over the quicbaas transport (REST + embedded), NOT
// JWS-signed RFC 8555 exchanges. NewOrder returns a pending order descriptor,
// FinalizeOrder signs the supplied CSR, and Renew resolves a cert by serial.

func TestACMEClient_NewOrder_HappyPath(t *testing.T) {
	wire := &acmeclient.NewOrderResponse{
		OrderID:     42,
		Status:      "pending",
		Identifiers: []acmeclient.OrderIdentifier{{Type: "dns", Value: "example.com"}},
		Finalize:    "https://baas/acme/order/42/finalize",
		URL:         "https://baas/acme/order/42",
	}
	mt := &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		if service != acmeServiceName || method != "NewOrder" {
			t.Errorf("unexpected dispatch: %s/%s", service, method)
		}
		r, ok := req.(*acmeclient.NewOrderRequest)
		if !ok || len(r.Identifiers) != 1 || r.Identifiers[0].Value != "example.com" || r.ChallengeType != "http-01" {
			t.Errorf("unexpected request: %+v", req)
		}
		return wire, nil
	}}
	c := &acmeClientImpl{t: mt}
	got, err := c.NewOrder(context.Background(),
		[]ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}}, "http-01")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != 42 || string(got.Status) != "pending" || got.Finalize == "" || len(got.Identifiers) != 1 {
		t.Errorf("unexpected order: %+v", got)
	}
}

func TestACMEClient_NewOrder_NoIdentifiers(t *testing.T) {
	touched := false
	c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		touched = true
		return nil, nil
	}}}
	_, err := c.NewOrder(context.Background(), nil, "http-01")
	if sdkerrors.Code(err) != sdkerrors.CodeInvalidInput {
		t.Errorf("expected invalid input, got %v", err)
	}
	if touched {
		t.Error("must not touch the transport on validation failure")
	}
}

func TestACMEClient_FinalizeOrder_HappyPath(t *testing.T) {
	wire := &acmeclient.FinalizeOrderResponse{
		CertificatePEM: []byte("cert"),
		ChainPEM:       []byte("chain"),
		SerialNumber:   "ff",
	}
	mt := &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		if service != acmeServiceName || method != "FinalizeOrder" {
			t.Errorf("unexpected dispatch: %s/%s", service, method)
		}
		r, ok := req.(*acmeclient.FinalizeOrderRequest)
		if !ok || string(r.CSRPEM) != "csr" || r.OrderURL != "url" {
			t.Errorf("unexpected request: %+v", req)
		}
		return wire, nil
	}}
	c := &acmeClientImpl{t: mt}
	got, err := c.FinalizeOrder(context.Background(), "url", []byte("csr"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != "cert" {
		t.Errorf("unexpected cert: %q", got)
	}
}

func TestACMEClient_FinalizeOrder_EmptyCSR(t *testing.T) {
	touched := false
	c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		touched = true
		return nil, nil
	}}}
	_, err := c.FinalizeOrder(context.Background(), "url", nil)
	if sdkerrors.Code(err) != sdkerrors.CodeInvalidInput {
		t.Errorf("expected invalid input, got %v", err)
	}
	if touched {
		t.Error("must not touch the transport on validation failure")
	}
}

func TestACMEClient_Renew_PropagatesTransportError(t *testing.T) {
	wantErr := sdkerrors.NewInvalidInput("acme.Renew",
		"renewal requires a fresh CSR via NewOrder/FinalizeOrder")
	mt := &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		if service != acmeServiceName || method != "Renew" {
			t.Errorf("unexpected dispatch: %s/%s", service, method)
		}
		r, ok := req.(*acmeclient.RenewRequest)
		if !ok || r.CertURL != "ff" {
			t.Errorf("unexpected request: %+v", req)
		}
		return nil, wantErr
	}}
	c := &acmeClientImpl{t: mt}
	_, err := c.Renew(context.Background(), "ff")
	if sdkerrors.Code(err) != sdkerrors.CodeInvalidInput {
		t.Errorf("expected propagated invalid-input error, got %v", err)
	}
}

func TestACMEClient_Renew_EmptyURL(t *testing.T) {
	touched := false
	c := &acmeClientImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		touched = true
		return nil, nil
	}}}
	_, err := c.Renew(context.Background(), "")
	if sdkerrors.Code(err) != sdkerrors.CodeInvalidInput {
		t.Errorf("expected invalid input, got %v", err)
	}
	if touched {
		t.Error("must not touch the transport on validation failure")
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
	var capturedReq *trustclient.PublishDANERequest
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
		capturedReq = req.(*trustclient.PublishDANERequest)
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

func TestTrust_CrossSignedRoots_ReturnsEachRootInOrder(t *testing.T) {
	first := makeSelfSignedCert(t)
	second := makeSelfSignedCert(t)
	s := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &TrustCrossSignedRootPoolResponse{
			RootPEMs: [][]byte{pemEncode(first.Raw), second.Raw},
		}, nil
	}}}
	roots, err := s.CrossSignedRoots(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roots) != 2 || !roots[0].Equal(first) || !roots[1].Equal(second) {
		t.Fatalf("roots = %v, want [first second]", roots)
	}
}

func TestTrust_CrossSignedRoots_Errors(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{}}
	if _, err := s.CrossSignedRoots(nil); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("nil context: expected InvalidInput, got %v", err)
	}

	bad := &trustServiceImpl{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return &TrustCrossSignedRootPoolResponse{RootPEMs: [][]byte{[]byte("totally invalid")}}, nil
	}}}
	if _, err := bad.CrossSignedRoots(context.Background()); err == nil {
		t.Error("expected error parsing invalid DER cert")
	}
}

func TestTrust_CrossSignedRoots_NilResponse_ReturnsNoRoots(t *testing.T) {
	s := &trustServiceImpl{t: &mockTransport{}}
	roots, err := s.CrossSignedRoots(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(roots) != 0 {
		t.Fatalf("roots = %v, want none", roots)
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
// fido2TransportClient tests
// =============================================================================

func TestFIDO2_BeginRegistration_Validation(t *testing.T) {
	c := &fido2TransportClient{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *FIDO2BeginRegistrationRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"empty username", &FIDO2BeginRegistrationRequest{}, context.Background()},
		{"nil ctx", &FIDO2BeginRegistrationRequest{Email: "u"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.BeginRegistration(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestFIDO2_BeginRegistration_TransportAndType(t *testing.T) {
	req := &FIDO2BeginRegistrationRequest{Email: "u"}
	t.Run("transport error", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.BeginRegistration(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{}}
		if _, err := c.BeginRegistration(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.BeginRegistration(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_BeginRegistration_HappyPath(t *testing.T) {
	want := &FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: "{}"}
	var gotService, gotMethod string
	c := &fido2TransportClient{t: &mockTransport{requestFn: func(_ context.Context, service, method string, _ interface{}) (interface{}, error) {
		gotService, gotMethod = service, method
		return want, nil
	}}}
	got, err := c.BeginRegistration(context.Background(), &FIDO2BeginRegistrationRequest{Email: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if gotService != fido2ServiceName || gotMethod != "BeginRegistration" {
		t.Errorf("routed to %s.%s, want %s.BeginRegistration", gotService, gotMethod, fido2ServiceName)
	}
}

func TestFIDO2_BeginAddCredential_HappyPath(t *testing.T) {
	want := &FIDO2BeginRegistrationResponse{SessionID: "s", OptionsJSON: "{}"}
	var gotMethod string
	var gotReq interface{} = "unset"
	c := &fido2TransportClient{t: &mockTransport{requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
		gotMethod, gotReq = method, req
		return want, nil
	}}}
	got, err := c.BeginAddCredential(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if gotMethod != "BeginAddCredential" || gotReq != nil {
		t.Errorf("routed %s with %v, want BeginAddCredential with no payload", gotMethod, gotReq)
	}
}

func TestFIDO2_BeginAddCredential_Errors(t *testing.T) {
	t.Run("nil context", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{}}
		if _, err := c.BeginAddCredential(nil); err == nil || !sdkerrors.IsInvalidInput(err) {
			t.Fatal("expected error")
		}
	})
	t.Run("transport error", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.BeginAddCredential(context.Background()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{}}
		if _, err := c.BeginAddCredential(context.Background()); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.BeginAddCredential(context.Background()); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_FinishRegistration_Validation(t *testing.T) {
	c := &fido2TransportClient{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *FIDO2FinishRegistrationRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"missing session", &FIDO2FinishRegistrationRequest{AttestationJSON: "{}"}, context.Background()},
		{"missing attestation", &FIDO2FinishRegistrationRequest{SessionID: "s"}, context.Background()},
		{"nil ctx", &FIDO2FinishRegistrationRequest{SessionID: "s", AttestationJSON: "{}"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.FinishRegistration(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestFIDO2_FinishRegistration_TransportAndType(t *testing.T) {
	req := &FIDO2FinishRegistrationRequest{SessionID: "s", AttestationJSON: "{}"}
	t.Run("transport error", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.FinishRegistration(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{}}
		if _, err := c.FinishRegistration(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.FinishRegistration(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_FinishRegistration_HappyPath(t *testing.T) {
	want := &FIDO2FinishRegistrationResponse{Success: true, CredentialID: "cred-1", Token: "tok"}
	c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.FinishRegistration(context.Background(), &FIDO2FinishRegistrationRequest{SessionID: "s", AttestationJSON: "{}"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFIDO2_BeginAuthentication_Validation(t *testing.T) {
	c := &fido2TransportClient{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *FIDO2BeginAuthenticationRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"empty username", &FIDO2BeginAuthenticationRequest{}, context.Background()},
		{"nil ctx", &FIDO2BeginAuthenticationRequest{Email: "u"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.BeginAuthentication(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestFIDO2_BeginAuthentication_TransportAndType(t *testing.T) {
	req := &FIDO2BeginAuthenticationRequest{Email: "u"}
	t.Run("transport error", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.BeginAuthentication(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{}}
		if _, err := c.BeginAuthentication(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.BeginAuthentication(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_BeginAuthentication_HappyPath(t *testing.T) {
	want := &FIDO2BeginAuthenticationResponse{SessionID: "s", OptionsJSON: "{}"}
	c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.BeginAuthentication(context.Background(), &FIDO2BeginAuthenticationRequest{Email: "u"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFIDO2_FinishAuthentication_Validation(t *testing.T) {
	c := &fido2TransportClient{t: &mockTransport{}}
	tests := []struct {
		name string
		req  *FIDO2FinishAuthenticationRequest
		ctx  context.Context
	}{
		{"nil req", nil, context.Background()},
		{"missing session", &FIDO2FinishAuthenticationRequest{AssertionJSON: "{}"}, context.Background()},
		{"missing assertion", &FIDO2FinishAuthenticationRequest{SessionID: "s"}, context.Background()},
		{"nil ctx", &FIDO2FinishAuthenticationRequest{SessionID: "s", AssertionJSON: "{}"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.FinishAuthentication(tc.ctx, tc.req); err == nil || !sdkerrors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestFIDO2_FinishAuthentication_TransportAndType(t *testing.T) {
	req := &FIDO2FinishAuthenticationRequest{SessionID: "s", AssertionJSON: "{}"}
	t.Run("transport error", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.FinishAuthentication(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{}}
		if _, err := c.FinishAuthentication(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.FinishAuthentication(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestFIDO2_FinishAuthentication_HappyPath(t *testing.T) {
	want := &FIDO2FinishAuthenticationResponse{Success: true, Token: "tok-1", Username: "u"}
	c := &fido2TransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.FinishAuthentication(context.Background(), &FIDO2FinishAuthenticationRequest{SessionID: "s", AssertionJSON: "{}"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// =============================================================================
// pivTransportClient tests
// =============================================================================

func TestPIV_ListPIVSlots_Validation(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if _, err := c.ListPIVSlots(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if _, err := c.ListPIVSlots(nil, &PIVListSlotsRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil ctx, got %v", err)
	}
}

func TestPIV_ListPIVSlots_TransportAndType(t *testing.T) {
	req := &PIVListSlotsRequest{Backend: "file"}
	t.Run("transport error", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.ListPIVSlots(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response yields empty", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{}}
		got, err := c.ListPIVSlots(context.Background(), req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == nil || len(got.Slots) != 0 {
			t.Errorf("expected empty response, got %+v", got)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.ListPIVSlots(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPIV_ListPIVSlots_HappyPath(t *testing.T) {
	want := &PIVListSlotsResponse{Slots: []PIVSlotStatus{{Slot: "9a"}}}
	var gotService, gotMethod string
	c := &pivTransportClient{t: &mockTransport{requestFn: func(_ context.Context, service, method string, _ interface{}) (interface{}, error) {
		gotService, gotMethod = service, method
		return want, nil
	}}}
	got, err := c.ListPIVSlots(context.Background(), &PIVListSlotsRequest{Backend: "file"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if gotService != pivServiceName || gotMethod != "ListPIVSlots" {
		t.Errorf("routed to %s.%s, want %s.ListPIVSlots", gotService, gotMethod, pivServiceName)
	}
}

func TestPIV_GetPIVCertificate_Validation(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if _, err := c.GetPIVCertificate(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if _, err := c.GetPIVCertificate(context.Background(), &PIVGetCertificateRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for missing slot, got %v", err)
	}
	if _, err := c.GetPIVCertificate(nil, &PIVGetCertificateRequest{Slot: "9a"}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil ctx, got %v", err)
	}
}

func TestPIV_GetPIVCertificate_TransportAndType(t *testing.T) {
	req := &PIVGetCertificateRequest{Slot: "9a"}
	t.Run("transport error", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.GetPIVCertificate(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response is not found", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{}}
		if _, err := c.GetPIVCertificate(context.Background(), req); err == nil || !sdkerrors.IsNotFound(err) {
			t.Errorf("expected NotFound, got %v", err)
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.GetPIVCertificate(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPIV_GetPIVCertificate_HappyPath(t *testing.T) {
	want := &PIVGetCertificateResponse{Slot: "9a", Certificate: []byte("PEM"), Format: "pem"}
	c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.GetPIVCertificate(context.Background(), &PIVGetCertificateRequest{Slot: "9a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPIV_StorePIVCertificate_Validation(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if err := c.StorePIVCertificate(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if err := c.StorePIVCertificate(context.Background(), &PIVStoreCertificateRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for missing slot, got %v", err)
	}
	if err := c.StorePIVCertificate(nil, &PIVStoreCertificateRequest{Slot: "9a"}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil ctx, got %v", err)
	}
}

func TestPIV_StorePIVCertificate_TransportPaths(t *testing.T) {
	req := &PIVStoreCertificateRequest{Slot: "9a", Certificate: []byte("PEM")}
	t.Run("transport error", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if err := c.StorePIVCertificate(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("happy path", func(t *testing.T) {
		var gotMethod string
		c := &pivTransportClient{t: &mockTransport{requestFn: func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
			gotMethod = method
			return nil, nil
		}}}
		if err := c.StorePIVCertificate(context.Background(), req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotMethod != "StorePIVCertificate" {
			t.Errorf("method = %q", gotMethod)
		}
	})
}

func TestPIV_DeletePIVCertificate_Validation(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if err := c.DeletePIVCertificate(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if err := c.DeletePIVCertificate(context.Background(), &PIVDeleteCertificateRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for missing slot, got %v", err)
	}
	if err := c.DeletePIVCertificate(nil, &PIVDeleteCertificateRequest{Slot: "9a"}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil ctx, got %v", err)
	}
}

func TestPIV_DeletePIVCertificate_TransportPaths(t *testing.T) {
	req := &PIVDeleteCertificateRequest{Slot: "9a"}
	t.Run("transport error", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if err := c.DeletePIVCertificate(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("happy path", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{}}
		if err := c.DeletePIVCertificate(context.Background(), req); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestPIV_GeneratePIVKey_Validation(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if _, err := c.GeneratePIVKey(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if _, err := c.GeneratePIVKey(context.Background(), &PIVGenerateKeyRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for missing slot, got %v", err)
	}
	if _, err := c.GeneratePIVKey(nil, &PIVGenerateKeyRequest{Slot: "9a"}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil ctx, got %v", err)
	}
}

func TestPIV_GeneratePIVKey_TransportAndType(t *testing.T) {
	req := &PIVGenerateKeyRequest{Slot: "9a", Algorithm: "ecdsap256"}
	t.Run("transport error", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return nil, errBoom
		}}}
		if _, err := c.GeneratePIVKey(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("nil response", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{}}
		if _, err := c.GeneratePIVKey(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := c.GeneratePIVKey(context.Background(), req); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestPIV_GeneratePIVKey_HappyPath(t *testing.T) {
	want := &PIVGenerateKeyResponse{Slot: "9a", Certificate: []byte("CERT")}
	c := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := c.GeneratePIVKey(context.Background(), &PIVGenerateKeyRequest{Slot: "9a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPIV_ImportPIVCertificate_Paths(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if err := c.ImportPIVCertificate(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if err := c.ImportPIVCertificate(context.Background(), &PIVStoreCertificateRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for missing slot, got %v", err)
	}
	var gotMethod string
	ok := &pivTransportClient{t: &mockTransport{requestFn: func(_ context.Context, _, method string, _ interface{}) (interface{}, error) {
		gotMethod = method
		return nil, nil
	}}}
	if err := ok.ImportPIVCertificate(context.Background(), &PIVStoreCertificateRequest{Slot: "9a"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "ImportPIVCertificate" {
		t.Errorf("method = %q", gotMethod)
	}
}

func TestPIV_ExportPIVCertificate_Paths(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if _, err := c.ExportPIVCertificate(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if _, err := c.ExportPIVCertificate(context.Background(), &PIVGetCertificateRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for missing slot, got %v", err)
	}
	t.Run("nil response is not found", func(t *testing.T) {
		if _, err := c.ExportPIVCertificate(context.Background(), &PIVGetCertificateRequest{Slot: "9a"}); err == nil || !sdkerrors.IsNotFound(err) {
			t.Errorf("expected NotFound, got %v", err)
		}
	})
	want := &PIVGetCertificateResponse{Slot: "9a", Certificate: []byte("PEM")}
	ok := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := ok.ExportPIVCertificate(context.Background(), &PIVGetCertificateRequest{Slot: "9a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPIV_GeneratePIVCSR_Paths(t *testing.T) {
	c := &pivTransportClient{t: &mockTransport{}}
	if _, err := c.GeneratePIVCSR(context.Background(), nil); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil req, got %v", err)
	}
	if _, err := c.GeneratePIVCSR(context.Background(), &PIVGenerateCSRRequest{}); err == nil || !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for missing slot, got %v", err)
	}
	t.Run("nil response", func(t *testing.T) {
		if _, err := c.GeneratePIVCSR(context.Background(), &PIVGenerateCSRRequest{Slot: "9a"}); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("invalid type", func(t *testing.T) {
		bad := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
			return "wrong", nil
		}}}
		if _, err := bad.GeneratePIVCSR(context.Background(), &PIVGenerateCSRRequest{Slot: "9a"}); err == nil {
			t.Fatal("expected error")
		}
	})
	want := &PIVGenerateCSRResponse{Slot: "9a", CSR: []byte("CSR")}
	ok := &pivTransportClient{t: &mockTransport{requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
		return want, nil
	}}}
	got, err := ok.GeneratePIVCSR(context.Background(), &PIVGenerateCSRRequest{Slot: "9a"})
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
	if svc.PIV() == nil {
		t.Error("PIV() should return non-nil")
	}
	if svc.Devices() == nil {
		t.Error("Devices() should return non-nil")
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
