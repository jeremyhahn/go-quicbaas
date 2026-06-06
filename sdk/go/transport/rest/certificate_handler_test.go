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

package rest

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newTestTransport stands up a Transport pointed at the supplied
// httptest server. Returned transports must be closed by the caller.
func newTestTransport(t *testing.T, server *httptest.Server) *Transport {
	t.Helper()
	tr, err := New(&transport.Config{
		Address:    server.URL,
		MaxRetries: 0, // Disable retries to keep error-path tests fast and deterministic.
	})
	if err != nil {
		t.Fatalf("failed to create transport: %v", err)
	}
	return tr
}

// =============================================================================
// IssueCertificate
// =============================================================================

func TestCertificateRESTHandler_IssueCertificate_HappyPath(t *testing.T) {
	const (
		certPEM   = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"
		keyPEM    = "-----BEGIN EC PRIVATE KEY-----\nMHc\n-----END EC PRIVATE KEY-----\n"
		caPEM     = "-----BEGIN CERTIFICATE-----\nMIIA\n-----END CERTIFICATE-----\n"
		chainPEM  = certPEM + caPEM
		serial    = "abc123"
		notBefore = int64(1700000000)
		notAfter  = int64(1700864000)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/certificate/issue" {
			t.Errorf("expected /api/v1/certificate/issue, got %s", r.URL.Path)
		}

		var req issueCertificateRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.CommonName != "leaf.example.com" {
			t.Errorf("expected CommonName 'leaf.example.com', got %q", req.CommonName)
		}
		if req.Usage != "server" {
			t.Errorf("expected Usage 'server', got %q", req.Usage)
		}
		if req.KeyAlgorithm != "ecdsa" {
			t.Errorf("expected KeyAlgorithm 'ecdsa', got %q", req.KeyAlgorithm)
		}

		resp := issueCertificateRESTResponse{
			CertificatePEM:   certPEM,
			PrivateKeyPEM:    keyPEM,
			CACertificatePEM: caPEM,
			ChainPEM:         chainPEM,
			SerialNumber:     serial,
			NotBefore:        notBefore,
			NotAfter:         notAfter,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	ctx := context.Background()
	result, err := tr.Request(ctx, "quicbaas.Certificate", "IssueCertificate", &certificate.IssueCertificateRequest{
		CommonName:   "leaf.example.com",
		Usage:        "server",
		KeyAlgorithm: "ecdsa",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("IssueCertificate failed: %v", err)
	}

	resp, ok := result.(*certificate.IssueCertificateResponse)
	if !ok {
		t.Fatalf("expected *certificate.IssueCertificateResponse, got %T", result)
	}
	if resp.SerialNumber != serial {
		t.Errorf("expected serial %q, got %q", serial, resp.SerialNumber)
	}
	if resp.CertificatePEM != certPEM {
		t.Errorf("CertificatePEM mismatch")
	}
	if resp.PrivateKeyPEM != keyPEM {
		t.Errorf("PrivateKeyPEM mismatch")
	}
	if !resp.NotBefore.Equal(time.Unix(notBefore, 0).UTC()) {
		t.Errorf("expected NotBefore %v, got %v", time.Unix(notBefore, 0).UTC(), resp.NotBefore)
	}
	if !resp.NotAfter.Equal(time.Unix(notAfter, 0).UTC()) {
		t.Errorf("expected NotAfter %v, got %v", time.Unix(notAfter, 0).UTC(), resp.NotAfter)
	}
}

func TestCertificateRESTHandler_IssueCertificate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"common_name is required","code":"invalid_input"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "IssueCertificate",
		&certificate.IssueCertificateRequest{CommonName: "x.example.com"})
	if err == nil {
		t.Fatal("expected error from server, got nil")
	}
}

func TestCertificateRESTHandler_IssueCertificate_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "IssueCertificate",
		&certificate.IssueCertificateRequest{CommonName: "x.example.com"})
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

// =============================================================================
// SignCSR
// =============================================================================

func TestCertificateRESTHandler_SignCSR_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/certificate/sign-csr" {
			t.Errorf("expected /api/v1/certificate/sign-csr, got %s", r.URL.Path)
		}

		var req signCSRRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.CSRPEM != "csr-body" {
			t.Errorf("expected CSRPEM 'csr-body', got %q", req.CSRPEM)
		}

		resp := signCSRRESTResponse{
			CertificatePEM: "cert-pem",
			SerialNumber:   "deadbeef",
			NotBefore:      1700000000,
			NotAfter:       1700864000,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Certificate", "SignCSR",
		&certificate.SignCSRRequest{CSRPEM: "csr-body", Usage: "client"})
	if err != nil {
		t.Fatalf("SignCSR failed: %v", err)
	}
	resp, ok := result.(*certificate.SignCSRResponse)
	if !ok {
		t.Fatalf("expected *certificate.SignCSRResponse, got %T", result)
	}
	if resp.SerialNumber != "deadbeef" {
		t.Errorf("expected serial 'deadbeef', got %q", resp.SerialNumber)
	}
	if resp.CertificatePEM != "cert-pem" {
		t.Errorf("CertificatePEM mismatch: %q", resp.CertificatePEM)
	}
}

func TestCertificateRESTHandler_SignCSR_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"signing failed","code":"internal"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "SignCSR",
		&certificate.SignCSRRequest{CSRPEM: "csr-body"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestCertificateRESTHandler_SignCSR_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{garbage`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "SignCSR",
		&certificate.SignCSRRequest{CSRPEM: "csr-body"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// =============================================================================
// RevokeCertificate
// =============================================================================

func TestCertificateRESTHandler_RevokeCertificate_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/certificate/revoke" {
			t.Errorf("expected /api/v1/certificate/revoke, got %s", r.URL.Path)
		}
		var req revokeCertificateRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.SerialNumber != "abc123" {
			t.Errorf("expected serial 'abc123', got %q", req.SerialNumber)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(revokeCertificateRESTResponse{Success: true})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Certificate", "RevokeCertificate",
		&certificate.RevokeCertificateRequest{SerialNumber: "abc123"})
	if err != nil {
		t.Fatalf("RevokeCertificate failed: %v", err)
	}
	resp, ok := result.(*certificate.RevokeCertificateResponse)
	if !ok {
		t.Fatalf("expected *certificate.RevokeCertificateResponse, got %T", result)
	}
	if !resp.Success {
		t.Error("expected Success=true")
	}
}

func TestCertificateRESTHandler_RevokeCertificate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"certificate not found","code":"not_found"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "RevokeCertificate",
		&certificate.RevokeCertificateRequest{SerialNumber: "missing"})
	if err == nil {
		t.Fatal("expected error for missing certificate")
	}
}

func TestCertificateRESTHandler_RevokeCertificate_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not even close to JSON`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "RevokeCertificate",
		&certificate.RevokeCertificateRequest{SerialNumber: "abc"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// =============================================================================
// VerifyCertificate
// =============================================================================

func TestCertificateRESTHandler_VerifyCertificate_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/certificate/verify" {
			t.Errorf("expected /api/v1/certificate/verify, got %s", r.URL.Path)
		}
		var req verifyCertificateRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.CertificatePEM != "cert-body" {
			t.Errorf("expected CertificatePEM 'cert-body', got %q", req.CertificatePEM)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verifyCertificateRESTResponse{Valid: true})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Certificate", "VerifyCertificate",
		&certificate.VerifyCertificateRequest{CertificatePEM: "cert-body"})
	if err != nil {
		t.Fatalf("VerifyCertificate failed: %v", err)
	}
	resp, ok := result.(*certificate.VerifyCertificateResponse)
	if !ok {
		t.Fatalf("expected *certificate.VerifyCertificateResponse, got %T", result)
	}
	if !resp.Valid {
		t.Error("expected Valid=true")
	}
}

func TestCertificateRESTHandler_VerifyCertificate_InvalidResult(t *testing.T) {
	// Server reports an invalid certificate via the response body (not via HTTP 4xx);
	// that path returns a properly typed response so the SDK caller can inspect Valid/ErrorMessage.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verifyCertificateRESTResponse{
			Valid:        false,
			ErrorMessage: "certificate has expired",
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Certificate", "VerifyCertificate",
		&certificate.VerifyCertificateRequest{CertificatePEM: "expired-cert"})
	if err != nil {
		t.Fatalf("VerifyCertificate returned unexpected error: %v", err)
	}
	resp, ok := result.(*certificate.VerifyCertificateResponse)
	if !ok {
		t.Fatalf("expected *certificate.VerifyCertificateResponse, got %T", result)
	}
	if resp.Valid {
		t.Error("expected Valid=false for expired cert")
	}
	if resp.ErrorMessage != "certificate has expired" {
		t.Errorf("expected error message, got %q", resp.ErrorMessage)
	}
}

func TestCertificateRESTHandler_VerifyCertificate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid pem","code":"invalid_input"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "VerifyCertificate",
		&certificate.VerifyCertificateRequest{CertificatePEM: "bad"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestCertificateRESTHandler_VerifyCertificate_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`bogus`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "VerifyCertificate",
		&certificate.VerifyCertificateRequest{CertificatePEM: "cert"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// =============================================================================
// GetTLSConfig
// =============================================================================

func TestCertificateRESTHandler_GetTLSConfig_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/certificate/tls-config" {
			t.Errorf("expected /api/v1/certificate/tls-config, got %s", r.URL.Path)
		}
		var req getTLSConfigRESTRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.ServerName != "node1.example.com" {
			t.Errorf("expected ServerName 'node1.example.com', got %q", req.ServerName)
		}
		if req.MinVersion != "1.3" {
			t.Errorf("expected MinVersion '1.3', got %q", req.MinVersion)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(getTLSConfigRESTResponse{Configured: true})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Certificate", "GetTLSConfig",
		&certificate.GetTLSConfigRequest{
			ServerName: "node1.example.com",
			MinVersion: "1.3",
		})
	if err != nil {
		t.Fatalf("GetTLSConfig failed: %v", err)
	}
	resp, ok := result.(*certificate.GetTLSConfigResponse)
	if !ok {
		t.Fatalf("expected *certificate.GetTLSConfigResponse, got %T", result)
	}
	if resp.Config == nil {
		t.Fatal("expected non-nil tls.Config")
	}
	if resp.Config.ServerName != "node1.example.com" {
		t.Errorf("expected ServerName 'node1.example.com', got %q", resp.Config.ServerName)
	}
	if resp.Config.MinVersion != tls.VersionTLS13 {
		t.Errorf("expected MinVersion TLS 1.3, got %d", resp.Config.MinVersion)
	}
}

func TestCertificateRESTHandler_GetTLSConfig_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"certificate service unavailable","code":"unavailable"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "GetTLSConfig",
		&certificate.GetTLSConfigRequest{})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestCertificateRESTHandler_GetTLSConfig_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{nope`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "GetTLSConfig",
		&certificate.GetTLSConfigRequest{})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestCertificateRESTHandler_GetTLSConfig_NotConfigured(t *testing.T) {
	// Server explicitly reports Configured=false; handler must error out
	// because the SDK contract requires a usable tls.Config on success.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(getTLSConfigRESTResponse{Configured: false})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "GetTLSConfig",
		&certificate.GetTLSConfigRequest{})
	if err == nil {
		t.Fatal("expected error when server reports Configured=false")
	}
}

// =============================================================================
// GenerateCRL
// =============================================================================

func TestCertificateRESTHandler_GenerateCRL_HappyPath_PEM(t *testing.T) {
	const crlPEM = "-----BEGIN X509 CRL-----\nMIIB\n-----END X509 CRL-----\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/xkms/ca/crl" {
			t.Errorf("expected /api/v1/xkms/ca/crl, got %s", r.URL.Path)
		}
		// crl_pem is []byte on the wire (base64 in JSON).
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(generateCRLRESTResponse{CRLPEM: []byte(crlPEM)})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Certificate", "GenerateCRL",
		&certificate.GenerateCRLRequest{})
	if err != nil {
		t.Fatalf("GenerateCRL failed: %v", err)
	}
	resp, ok := result.(*certificate.GenerateCRLResponse)
	if !ok {
		t.Fatalf("expected *certificate.GenerateCRLResponse, got %T", result)
	}
	if resp.CRLPEM != crlPEM {
		t.Errorf("expected CRLPEM %q, got %q", crlPEM, resp.CRLPEM)
	}
}

func TestCertificateRESTHandler_GenerateCRL_HappyPath_DERFallback(t *testing.T) {
	// Server returns raw DER (no PEM headers); handler must PEM-encode it.
	derBytes := []byte{0x30, 0x82, 0x01, 0x00}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(generateCRLRESTResponse{CRLPEM: derBytes})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Certificate", "GenerateCRL",
		&certificate.GenerateCRLRequest{})
	if err != nil {
		t.Fatalf("GenerateCRL failed: %v", err)
	}
	resp, ok := result.(*certificate.GenerateCRLResponse)
	if !ok {
		t.Fatalf("expected *certificate.GenerateCRLResponse, got %T", result)
	}
	if resp.CRLPEM == "" {
		t.Fatal("expected non-empty CRLPEM after DER->PEM encoding")
	}
	// PEM-encoded form must contain the standard header.
	if want := "-----BEGIN X509 CRL-----"; !contains(resp.CRLPEM, want) {
		t.Errorf("expected CRLPEM to contain %q, got %q", want, resp.CRLPEM)
	}
}

func TestCertificateRESTHandler_GenerateCRL_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"CA not configured","code":"unavailable"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "GenerateCRL",
		&certificate.GenerateCRLRequest{})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestCertificateRESTHandler_GenerateCRL_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{garbled`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Certificate", "GenerateCRL",
		&certificate.GenerateCRLRequest{})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// =============================================================================
// helpers
// =============================================================================

// contains is a small substring check helper; avoids pulling strings
// into the test imports for a single use site.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
