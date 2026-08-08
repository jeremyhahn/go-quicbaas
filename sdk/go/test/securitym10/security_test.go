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

// Package securitym10 contains the M10 SDK conformance tests for the
// quicbaas Security service extensions (ACME, Trust, AuthService,
// CertificateExtensions). The tests use only exported APIs from the
// quicbaas SDK so they survive in a separate test package and never
// depend on broken mocks elsewhere in the parent test suite.
//
// Tests use the embedded transport via mock.Transport to dispatch
// canned responses without requiring a real server.
package securitym10

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

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Test Helpers
// =============================================================================

// makeSelfSignedCert builds a small self-signed cert for testing.
func makeSelfSignedCert(t *testing.T, cn string) (*x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	return cert, der
}

// =============================================================================
// SDK Surface — single-dependency invariant
// =============================================================================

// TestSDK_M10_Surface_Reachable asserts that every M10 sub-interface is
// reachable through the quicbaas SDK *exclusively*. This file imports
// only `go-quicbaas/sdk/go` and `go-quicbaas/sdk/go/errors`; if any M10
// type were missing from the SDK surface, this file would fail to
// compile.
func TestSDK_M10_Surface_Reachable(t *testing.T) {
	// Compile-time references to every M10 surface type. We assign each
	// to a typed nil so the compiler verifies the types exist and are
	// usable.
	var (
		_ quicbaas.ACMEService           = nil
		_ quicbaas.ACMEServerClient      = nil
		_ quicbaas.ACMEClientAPI         = nil
		_ quicbaas.TrustService          = nil
		_ quicbaas.CertificateExtensions = nil
		_ quicbaas.LocalDevices          = nil
		_ quicbaas.WebAuthnClient        = nil
		_ quicbaas.PIVClient             = nil
		_ quicbaas.AuthFactorsClient     = nil
		_ quicbaas.AuthService           = nil

		_ quicbaas.ACMEDirectory              = quicbaas.ACMEDirectory{}
		_ quicbaas.ACMECertificateAuthority   = nil
		_ quicbaas.ACMEEnroll01TCGCSRVerifier = nil
		_ quicbaas.ACMEChallengeVerifierFunc  = nil
		_ quicbaas.ACMEClientConfig           = quicbaas.ACMEClientConfig{}
		_ quicbaas.ACMECertificateRequest     = quicbaas.ACMECertificateRequest{}
		_ quicbaas.ACMERenewalConfig          = quicbaas.ACMERenewalConfig{}
		_ quicbaas.ACMECrossSignConfig        = quicbaas.ACMECrossSignConfig{}
		_ quicbaas.ACMEEnrollmentConfig       = quicbaas.ACMEEnrollmentConfig{}

		_ quicbaas.FIDO2BeginRegistrationRequest     = quicbaas.FIDO2BeginRegistrationRequest{}
		_ quicbaas.FIDO2BeginRegistrationResponse    = quicbaas.FIDO2BeginRegistrationResponse{}
		_ quicbaas.FIDO2FinishRegistrationRequest    = quicbaas.FIDO2FinishRegistrationRequest{}
		_ quicbaas.FIDO2FinishRegistrationResponse   = quicbaas.FIDO2FinishRegistrationResponse{}
		_ quicbaas.FIDO2BeginAuthenticationRequest   = quicbaas.FIDO2BeginAuthenticationRequest{}
		_ quicbaas.FIDO2BeginAuthenticationResponse  = quicbaas.FIDO2BeginAuthenticationResponse{}
		_ quicbaas.FIDO2FinishAuthenticationRequest  = quicbaas.FIDO2FinishAuthenticationRequest{}
		_ quicbaas.FIDO2FinishAuthenticationResponse = quicbaas.FIDO2FinishAuthenticationResponse{}

		_ quicbaas.PIVListSlotsRequest       = quicbaas.PIVListSlotsRequest{}
		_ quicbaas.PIVListSlotsResponse      = quicbaas.PIVListSlotsResponse{}
		_ quicbaas.PIVSlotStatus             = quicbaas.PIVSlotStatus{}
		_ quicbaas.PIVGetCertificateRequest  = quicbaas.PIVGetCertificateRequest{}
		_ quicbaas.PIVGetCertificateResponse = quicbaas.PIVGetCertificateResponse{}
		_ quicbaas.PIVGenerateKeyRequest     = quicbaas.PIVGenerateKeyRequest{}
		_ quicbaas.PIVGenerateKeyResponse    = quicbaas.PIVGenerateKeyResponse{}
		_ quicbaas.PIVGenerateCSRRequest     = quicbaas.PIVGenerateCSRRequest{}
		_ quicbaas.PIVGenerateCSRResponse    = quicbaas.PIVGenerateCSRResponse{}

		_ quicbaas.AuthFactorEnrollRequest     = quicbaas.AuthFactorEnrollRequest{}
		_ quicbaas.AuthFactorEnrollResponse    = quicbaas.AuthFactorEnrollResponse{}
		_ quicbaas.AuthFactorChallengeRequest  = quicbaas.AuthFactorChallengeRequest{}
		_ quicbaas.AuthFactorChallengeResponse = quicbaas.AuthFactorChallengeResponse{}
		_ quicbaas.AuthFactorVerifyRequest     = quicbaas.AuthFactorVerifyRequest{}
		_ quicbaas.AuthFactorListRequest       = quicbaas.AuthFactorListRequest{}
		_ quicbaas.AuthFactorListResponse      = quicbaas.AuthFactorListResponse{}
		_ quicbaas.AuthFactorEntry             = quicbaas.AuthFactorEntry{}
		_ quicbaas.AuthFactorRevokeRequest     = quicbaas.AuthFactorRevokeRequest{}

		// Constants exposed by the SDK.
		_ = quicbaas.ACMEChallengeTypeEnroll01
		_ = quicbaas.ACMEChallengeTypeEndorse01
		_ = quicbaas.ACMEChallengeTypeHTTP01
		_ = quicbaas.ACMEChallengeTypeDNS01
		_ = quicbaas.ACMEStatusValid

		// Verifier function vars.
		_ = quicbaas.ACMEVerifyHTTP01
		_ = quicbaas.ACMEVerifyDNS01
		_ = quicbaas.ACMEVerifyEnroll01
		_ = quicbaas.ACMEVerifyEndorse01
	)
}

// =============================================================================
// SecurityService — Sub-Interface Accessors
// =============================================================================

func newClientWithMockTransport(t *testing.T) (quicbaas.Client, *recordingTransport) {
	t.Helper()
	rt := &recordingTransport{
		responses: map[string]map[string]any{},
	}
	c := quicbaas.NewClientWithTransport(rt)
	return c, rt
}

// TestSDK_M10_SecurityAccessors exercises the sub-interface accessors
// reachable from a real Client constructed from the quicbaas SDK. The
// accessors must be non-nil and stable across repeated calls.
func TestSDK_M10_SecurityAccessors(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()

	sec := c.Security()
	if sec.ACME() == nil {
		t.Error("Security().ACME() returned nil")
	}
	if sec.Trust() == nil {
		t.Error("Security().Trust() returned nil")
	}
	if sec.CertificateExt() == nil {
		t.Error("Security().CertificateExt() returned nil")
	}

	auth := sec.Auth()
	if auth == nil {
		t.Fatal("Security().Auth() returned nil")
	}
	if auth.Devices() == nil {
		t.Error("Auth().Devices() returned nil")
	}
	if auth.WebAuthn() == nil {
		t.Error("Auth().WebAuthn() returned nil")
	}
	if auth.PIV() == nil {
		t.Error("Auth().PIV() returned nil")
	}
	if auth.Factors() == nil {
		t.Error("Auth().Factors() returned nil")
	}

	// Stability: multiple Auth() calls must return the same instance so
	// caches in the underlying sub-services are consistent.
	if sec.Auth() != auth {
		t.Error("Auth() must return the same instance across calls")
	}
}

// =============================================================================
// ACME — Server (registration + Issue)
// =============================================================================

func TestSDK_ACMEServer_RegisterChallenge_Idempotent(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.ACME", "RegisterChallenge", nil, nil)

	verifier := func(*net.Resolver, quicbaas.ACMECertificateAuthority, string, string, string, string) error {
		return nil
	}

	srv := c.Security().ACME().Server()
	if err := srv.RegisterChallenge(context.Background(), "enroll-01", verifier); err != nil {
		t.Fatalf("RegisterChallenge: %v", err)
	}

	wireCalls := rt.callsFor("quicbaas.ACME", "RegisterChallenge")
	if wireCalls != 1 {
		t.Errorf("expected 1 wire call, got %d", wireCalls)
	}

	// Idempotent: re-registering must not produce another wire call.
	if err := srv.RegisterChallenge(context.Background(), "enroll-01", verifier); err != nil {
		t.Fatalf("second RegisterChallenge: %v", err)
	}
	if wire := rt.callsFor("quicbaas.ACME", "RegisterChallenge"); wire != 1 {
		t.Errorf("RegisterChallenge must be idempotent on the wire, got %d calls", wire)
	}
}

func TestSDK_ACMEServer_RegisterChallenge_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	srv := c.Security().ACME().Server()

	verifier := func(*net.Resolver, quicbaas.ACMECertificateAuthority, string, string, string, string) error {
		return nil
	}

	if err := srv.RegisterChallenge(context.Background(), "", verifier); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty name, got %v", err)
	}
	if err := srv.RegisterChallenge(context.Background(), "x", nil); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil verifier, got %v", err)
	}
	if err := srv.RegisterChallenge(nil, "x", verifier); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil ctx, got %v", err)
	}
}

func TestSDK_ACMEServer_Issue_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	// The transport exchanges the shared client/acme wire type; the SDK
	// converts it to the public ACMEIssueResponse.
	rt.setResponse("quicbaas.ACME", "Issue", &acmeclient.IssueResponse{
		CertificatePEM: []byte("CERT"),
		ChainPEM:       []byte("CHAIN"),
		SerialNumber:   "01",
	}, nil)

	got, err := c.Security().ACME().Server().Issue(context.Background(), []byte("CSR"), "enroll-01", "device-1")
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if string(got.CertificatePEM) != "CERT" || string(got.ChainPEM) != "CHAIN" || got.SerialNumber != "01" {
		t.Errorf("unexpected issue response: %+v", got)
	}
}

func TestSDK_ACMEServer_Issue_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	srv := c.Security().ACME().Server()

	if _, err := srv.Issue(context.Background(), nil, "http-01", ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty CSR, got %v", err)
	}
	if _, err := srv.Issue(context.Background(), []byte("X"), "", ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty challenge, got %v", err)
	}
	if _, err := srv.Issue(nil, []byte("X"), "http-01", ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil ctx, got %v", err)
	}
}

// =============================================================================
// ACME — Client (NewOrder, FinalizeOrder, Renew)
// =============================================================================
//
// QuicBaaS is the broker-as-CA: it IS the certificate authority. The ACME
// protocol-client verbs are therefore in-process service calls routed over the
// quicbaas transport (REST + embedded), NOT JWS-signed RFC 8555 exchanges
// against an external directory. NewOrder returns a pending order descriptor,
// FinalizeOrder signs the supplied CSR, and Renew resolves a cert by serial.

func TestSDK_ACMEClient_NewOrder_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.ACME", "NewOrder", &acmeclient.NewOrderResponse{
		OrderID:     9,
		Status:      "pending",
		Identifiers: []acmeclient.OrderIdentifier{{Type: "dns", Value: "example.com"}},
		Finalize:    "https://acme/order/9/finalize",
		URL:         "https://acme/order/9",
	}, nil)

	order, err := c.Security().ACME().Client().NewOrder(
		context.Background(),
		[]quicbaas.ACMEOrderIdentifier{{Type: "dns", Value: "example.com"}},
		"dns-01",
	)
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	if order.ID != 9 || order.Finalize == "" || len(order.Identifiers) != 1 {
		t.Errorf("unexpected order: %+v", order)
	}
	if wire := rt.callsFor("quicbaas.ACME", "NewOrder"); wire != 1 {
		t.Errorf("NewOrder must route over the transport once, got %d calls", wire)
	}
}

func TestSDK_ACMEClient_FinalizeOrder_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.ACME", "FinalizeOrder", &acmeclient.FinalizeOrderResponse{
		CertificatePEM: []byte("CERT"),
		ChainPEM:       []byte("CHAIN"),
		SerialNumber:   "01",
	}, nil)

	certPEM, err := c.Security().ACME().Client().FinalizeOrder(
		context.Background(), "https://acme/order/1", []byte("CSR"))
	if err != nil {
		t.Fatalf("FinalizeOrder: %v", err)
	}
	if string(certPEM) != "CERT" {
		t.Errorf("unexpected cert: %q", certPEM)
	}
	if wire := rt.callsFor("quicbaas.ACME", "FinalizeOrder"); wire != 1 {
		t.Errorf("FinalizeOrder must route over the transport once, got %d calls", wire)
	}
}

func TestSDK_ACMEClient_Renew_PropagatesNeedsCSR(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	// Broker-as-CA renewal needs a fresh CSR: the server returns a typed
	// invalid-input error which the SDK propagates.
	rt.setResponse("quicbaas.ACME", "Renew", nil,
		sdkerrors.NewInvalidInput("acme.Renew", "renewal requires a fresh CSR"))

	_, err := c.Security().ACME().Client().Renew(context.Background(), "https://acme/cert/1")
	if sdkerrors.Code(err) != sdkerrors.CodeInvalidInput {
		t.Errorf("expected invalid-input, got %v", err)
	}
	if wire := rt.callsFor("quicbaas.ACME", "Renew"); wire != 1 {
		t.Errorf("Renew must route over the transport once, got %d calls", wire)
	}
}

// =============================================================================
// Trust Service
// =============================================================================

func TestSDK_Trust_PublishDANE_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	trust := c.Security().Trust()
	cert, _ := makeSelfSignedCert(t, "validation")

	if err := trust.PublishDANE(context.Background(), "", 443, cert); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty name, got %v", err)
	}
	if err := trust.PublishDANE(context.Background(), "x", 0, cert); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for non-positive port, got %v", err)
	}
	if err := trust.PublishDANE(context.Background(), "x", 443, nil); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil cert, got %v", err)
	}
}

func TestSDK_Trust_CrossSignedRootPool_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	_, der := makeSelfSignedCert(t, "cross-root")
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	rt.setResponse("quicbaas.Trust", "CrossSignedRootPool",
		&quicbaas.TrustCrossSignedRootPoolResponse{RootPEMs: [][]byte{pemBlock, der}}, nil)

	pool, err := c.Security().Trust().CrossSignedRootPool(context.Background())
	if err != nil {
		t.Fatalf("CrossSignedRootPool: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

func TestSDK_Trust_AutoImportAIA_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	trust := c.Security().Trust()

	if err := trust.AutoImportAIA(context.Background(), nil); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty chain, got %v", err)
	}
	if err := trust.AutoImportAIA(context.Background(), []*x509.Certificate{nil}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil chain entry, got %v", err)
	}
}

// =============================================================================
// Certificate Extensions
// =============================================================================

func TestSDK_CertExt_IssueDeviceIdentity_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	ext := c.Security().CertificateExt()

	if _, err := ext.IssueDeviceIdentity(context.Background(), nil); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil request, got %v", err)
	}
	if _, err := ext.IssueDeviceIdentity(context.Background(), &quicbaas.DeviceIdentityRequest{}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty type, got %v", err)
	}
	if _, err := ext.IssueDeviceIdentity(context.Background(), &quicbaas.DeviceIdentityRequest{Type: quicbaas.DeviceIdentityEK}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty device id, got %v", err)
	}
}

func TestSDK_CertExt_IssueDeviceIdentity_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	expected := &quicbaas.DeviceIdentityResponse{
		CertificatePEM: []byte("DEVCERT"),
		SerialNumber:   "0xDEAD",
		NotBefore:      time.Now(),
		NotAfter:       time.Now().Add(time.Hour),
	}
	rt.setResponse("quicbaas.Certificate", "IssueDeviceIdentity", expected, nil)

	out, err := c.Security().CertificateExt().IssueDeviceIdentity(context.Background(), &quicbaas.DeviceIdentityRequest{
		Type:     quicbaas.DeviceIdentityIDevID,
		DeviceID: "dev-1",
		CSRPEM:   []byte("CSR"),
	})
	if err != nil {
		t.Fatalf("IssueDeviceIdentity: %v", err)
	}
	if out != expected {
		t.Error("expected canned response")
	}
}

func TestSDK_CertExt_CrossSign_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.Certificate", "CrossSign",
		&quicbaas.CertCrossSignResponse{CrossSignedPEM: []byte("XPEM")}, nil)

	out, err := c.Security().CertificateExt().CrossSign(context.Background(), []byte("ROOTPEM"))
	if err != nil {
		t.Fatalf("CrossSign: %v", err)
	}
	if string(out) != "XPEM" {
		t.Errorf("unexpected output %q", out)
	}
}

func TestSDK_CertExt_Chain_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.Certificate", "Chain",
		&quicbaas.CertChainResponse{ChainPEM: [][]byte{[]byte("A"), []byte("B")}}, nil)

	chain, err := c.Security().CertificateExt().Chain(context.Background(), []byte("CERTPEM"))
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 2 {
		t.Errorf("unexpected chain length %d", len(chain))
	}
}

// =============================================================================
// CTAP2 local-device helper
//
// The former FIDO2 transport surface (ListDevices/DeviceInfo/Register/Assert)
// was collapsed: Register/Assert are redundant with the WebAuthn ceremony
// (see below) and ListDevices/DeviceInfo are client-LOCAL CTAP2 operations
// exposed via Security().Auth().Devices() (covered by devices_local_test.go).
// This smoke test asserts the local accessor is reachable and performs no
// transport request.
// =============================================================================

func TestSDK_Devices_LocalAccessorReachable(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()

	dev := c.Security().Auth().Devices()
	if dev == nil {
		t.Fatal("Devices() should return the local CTAP2 helper")
	}
	// List is a local USB-HID probe; in a headless CI host it returns an
	// empty slice (not an error) and never touches the transport.
	got, err := dev.List()
	if err != nil {
		t.Fatalf("Devices().List: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil device slice")
	}
}

// =============================================================================
// WebAuthn
// =============================================================================

func TestSDK_WebAuthn_BeginRegistration_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.WebAuthn", "BeginRegistration",
		&quicbaas.FIDO2BeginRegistrationResponse{SessionID: "go"}, nil)

	out, err := c.Security().Auth().WebAuthn().BeginRegistration(context.Background(), &quicbaas.WebAuthnUser{Username: "alice"})
	if err != nil {
		t.Fatalf("BeginRegistration: %v", err)
	}
	if out.SessionID != "go" {
		t.Errorf("unexpected session id %q", out.SessionID)
	}
}

func TestSDK_WebAuthn_FinishLogin_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.WebAuthn", "FinishLogin",
		&quicbaas.WebAuthnSession{UserID: "u-1", Username: "alice", Token: "JWT"}, nil)

	sess, err := c.Security().Auth().WebAuthn().FinishLogin(
		context.Background(),
		&quicbaas.WebAuthnUser{Username: "alice"},
		&quicbaas.FIDO2FinishAuthenticationRequest{Username: "alice"},
	)
	if err != nil {
		t.Fatalf("FinishLogin: %v", err)
	}
	if sess.Token != "JWT" {
		t.Errorf("unexpected token %q", sess.Token)
	}
}

func TestSDK_WebAuthn_BeginLogin_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	cli := c.Security().Auth().WebAuthn()

	if _, err := cli.BeginLogin(context.Background(), nil); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil user, got %v", err)
	}
	if _, err := cli.BeginLogin(context.Background(), &quicbaas.WebAuthnUser{}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty username, got %v", err)
	}
}

// =============================================================================
// PIV — vendor-agnostic
// =============================================================================

func TestSDK_PIV_ListReaders_VendorAgnostic(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.PIV", "ListReaders",
		&quicbaas.PIVListReadersResponse{
			Readers: []quicbaas.PIVReader{
				{Name: "Yubico YubiKey OTP+CCID", Vendor: "Yubico", CardPresent: true},
				{Name: "Nitrokey Pro 2", Vendor: "Nitrokey", CardPresent: true},
			},
		}, nil)

	out, err := c.Security().Auth().PIV().ListReaders(context.Background())
	if err != nil {
		t.Fatalf("ListReaders: %v", err)
	}
	if len(out.Readers) != 2 {
		t.Fatalf("expected 2 readers, got %d", len(out.Readers))
	}
	if out.Readers[0].Vendor == out.Readers[1].Vendor {
		t.Error("expected distinct vendors (vendor-agnostic API)")
	}
}

func TestSDK_PIV_GetCertificate_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	_, der := makeSelfSignedCert(t, "piv-cert")
	rt.setResponse("quicbaas.PIV", "GetCertificate",
		&quicbaas.PIVGetCertificateResponsePayload{CertDER: der}, nil)

	cert, err := c.Security().Auth().PIV().GetCertificate(context.Background(), "reader1", "9a")
	if err != nil {
		t.Fatalf("GetCertificate: %v", err)
	}
	if cert.Subject.CommonName != "piv-cert" {
		t.Errorf("unexpected CN %q", cert.Subject.CommonName)
	}
}

func TestSDK_PIV_Sign_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	cli := c.Security().Auth().PIV()

	if _, err := cli.Sign(context.Background(), nil); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for nil request, got %v", err)
	}
	if _, err := cli.Sign(context.Background(), &quicbaas.PIVSignRequest{Slot: "9a"}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty reader, got %v", err)
	}
}

func TestSDK_PIV_Attest_NilResponse_Unsupported(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	rt.setResponse("quicbaas.PIV", "Attest", nil, nil)

	_, err := c.Security().Auth().PIV().Attest(context.Background(), "r1", "9a")
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if !errors.Is(err, quicbaas.ErrPIVAttestationUnsupported) {
		t.Errorf("expected ErrPIVAttestationUnsupported, got %v", err)
	}
}

// =============================================================================
// Auth Factors — multi-type N-AuthFactor model
// =============================================================================

// TestSDK_AuthFactors_Add_RequiresCredentialID asserts the current SDK
// enrollment contract: a fido2 factor cannot be enrolled without the
// WebAuthn credentialID in MethodParams["credential_id"], because login
// resolves the factor by that key. The check runs client-side before any
// transport dispatch, so it holds regardless of how AuthFactors routes.
func TestSDK_AuthFactors_Add_RequiresCredentialID(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()

	_, err := c.Security().Auth().Factors().Add(context.Background(), &quicbaas.AuthFactorEnrollRequest{
		FactorName: "fido2",
		Subject:    "alice",
	})
	if !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("expected InvalidInput for fido2 enrollment without credential_id, got %v", err)
	}
}

// TestSDK_AuthFactors_Add_RoutesToQRDBUserSurface asserts that, once
// client-side validation passes, enrollment is delegated to the qrdb
// user-side auth-factor surface rather than the quicbaas transport. A
// client built over a bare quicbaas transport (no embedded qrdb, no
// network address) therefore surfaces the qrdb routing error. The dead
// rt.setResponse("quicbaas.AuthFactors", "Add", ...) mock from the old
// transport-dispatch contract no longer applies.
func TestSDK_AuthFactors_Add_RoutesToQRDBUserSurface(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()

	_, err := c.Security().Auth().Factors().Add(context.Background(), &quicbaas.AuthFactorEnrollRequest{
		FactorName: "fido2",
		Subject:    "alice",
		MethodParams: map[string]any{
			"credential_id": "cred-abc",
		},
	})
	if err == nil {
		t.Fatal("expected error: AuthFactors.Add must route to the qrdb user surface, which is unavailable on a bare quicbaas transport")
	}
}

// TestSDK_AuthFactors_List_RoutesToQRDBUserSurface asserts that listing a
// subject's factors is delegated to the qrdb user-side auth-factor surface
// rather than the quicbaas transport. A client over a bare quicbaas
// transport surfaces the qrdb routing error, proving the delegation. The
// old rt.setResponse("quicbaas.AuthFactors", "List", ...) mock is dead.
func TestSDK_AuthFactors_List_RoutesToQRDBUserSurface(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()

	if _, err := c.Security().Auth().Factors().List(context.Background(), "alice"); err == nil {
		t.Fatal("expected error: AuthFactors.List must route to the qrdb user surface, which is unavailable on a bare quicbaas transport")
	}

	// Input validation still runs client-side, before any routing.
	if _, err := c.Security().Auth().Factors().List(context.Background(), ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty subject, got %v", err)
	}
}

func TestSDK_AuthFactors_Login_HappyPath(t *testing.T) {
	c, rt := newClientWithMockTransport(t)
	defer c.Close()

	expected := &quicbaas.AuthFactorLoginResponse{
		UserID:    "u-1",
		Username:  "alice",
		Token:     "JWT",
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	rt.setResponse("quicbaas.AuthFactors", "Login", expected, nil)

	out, err := c.Security().Auth().Factors().Login(context.Background(), &quicbaas.AuthFactorLoginRequest{
		Username:   "alice",
		FactorName: "fido2",
		Proof:      []byte("assertion"),
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if out.Token != "JWT" {
		t.Errorf("unexpected token %q", out.Token)
	}
}

func TestSDK_AuthFactors_Validation(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()
	cli := c.Security().Auth().Factors()

	if _, err := cli.Add(context.Background(), &quicbaas.AuthFactorEnrollRequest{}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty factor name, got %v", err)
	}
	if err := cli.Remove(context.Background(), "", true); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty factor id, got %v", err)
	}
	if err := cli.Relabel(context.Background(), "id", ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty label, got %v", err)
	}
	if _, err := cli.Rotate(context.Background(), "", &quicbaas.AuthFactorEnrollRequest{FactorName: "f", Subject: "s"}); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput for empty old id, got %v", err)
	}
}

// =============================================================================
// AuthService — Embedded AuthClient surface preservation
// =============================================================================

// TestSDK_AuthService_EmbedsAuthClient validates that the AuthService
// returned by Security().Auth() still satisfies the AuthClient surface,
// so existing callers of Security().Auth().Login(...) continue to work
// after the M10 extension.
func TestSDK_AuthService_EmbedsAuthClient(t *testing.T) {
	c, _ := newClientWithMockTransport(t)
	defer c.Close()

	auth := c.Security().Auth()
	var _ quicbaas.AuthClient = auth // compile-time check

	// Runtime check: invoking RefreshToken with empty args must return
	// InvalidInput, proving the embedded AuthClient is wired through.
	if _, err := auth.RefreshToken(context.Background(), ""); !sdkerrors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput from embedded RefreshToken, got %v", err)
	}
}
