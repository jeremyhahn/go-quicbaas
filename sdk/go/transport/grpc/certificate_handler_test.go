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

package grpc

import (
	"testing"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

func TestIsCertificateService(t *testing.T) {
	tests := []struct {
		service  string
		expected bool
	}{
		{"quicbaas.Certificate", true},
		{"quicbaas.Certificate.Admin", true},
		{"quicbaas.Crypto", false},
		{"quicbaas.KMS", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := isCertificateService(tt.service); got != tt.expected {
			t.Errorf("isCertificateService(%q) = %v, want %v", tt.service, got, tt.expected)
		}
	}
}

func TestCertificateHandlerHandleRequest_UnsupportedMethod(t *testing.T) {
	handler := newCertificateHandler(nil)

	// CA-administration verbs have no proto analogue and must return a
	// clear unimplemented error rather than panicking on a nil conn.
	_, err := handler.handleRequest(nil, "quicbaas.Certificate", "CAInfo", nil)
	if err == nil {
		t.Fatal("expected error for method not exposed by CertificateService proto")
	}
}

func TestCertificateHandlerHandleIssueCertificate_InvalidRequestType(t *testing.T) {
	handler := newCertificateHandler(nil)

	_, err := handler.handleIssueCertificate(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestCertificateHandlerHandleSignCSR_InvalidRequestType(t *testing.T) {
	handler := newCertificateHandler(nil)

	_, err := handler.handleSignCSR(nil, nil, 42)
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestCertificateHandlerHandleRevokeCertificate_InvalidRequestType(t *testing.T) {
	handler := newCertificateHandler(nil)

	_, err := handler.handleRevokeCertificate(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestCertificateHandlerHandleVerifyCertificate_InvalidRequestType(t *testing.T) {
	handler := newCertificateHandler(nil)

	_, err := handler.handleVerifyCertificate(nil, nil, struct{}{})
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestCertificateHandlerHandleGetCACertificate_InvalidRequestType(t *testing.T) {
	handler := newCertificateHandler(nil)

	// A non-nil request of the wrong type must be rejected before any
	// network call.
	_, err := handler.handleGetCACertificate(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestCertificateHandlerHandleGetTLSConfig_InvalidRequestType(t *testing.T) {
	handler := newCertificateHandler(nil)

	_, err := handler.handleGetTLSConfig(nil, nil, "invalid")
	if err == nil {
		t.Error("expected error for invalid request type")
	}
}

func TestCertUsageToProto(t *testing.T) {
	cases := map[string]pb.CertificateUsage{
		"client":  pb.CertificateUsage_CERTIFICATE_USAGE_CLIENT,
		"server":  pb.CertificateUsage_CERTIFICATE_USAGE_SERVER,
		"both":    pb.CertificateUsage_CERTIFICATE_USAGE_BOTH,
		"":        pb.CertificateUsage_CERTIFICATE_USAGE_UNSPECIFIED,
		"unknown": pb.CertificateUsage_CERTIFICATE_USAGE_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := certUsageToProto(in); got != want {
			t.Errorf("certUsageToProto(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestCertKeyAlgToProto(t *testing.T) {
	cases := map[string]pb.CertKeyAlgorithm{
		"ecdsa":   pb.CertKeyAlgorithm_CERT_KEY_ALG_ECDSA,
		"rsa":     pb.CertKeyAlgorithm_CERT_KEY_ALG_RSA,
		"":        pb.CertKeyAlgorithm_CERT_KEY_ALG_UNSPECIFIED,
		"unknown": pb.CertKeyAlgorithm_CERT_KEY_ALG_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := certKeyAlgToProto(in); got != want {
			t.Errorf("certKeyAlgToProto(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTLSVersionToProto(t *testing.T) {
	cases := map[string]pb.TLSVersion{
		"1.2":     pb.TLSVersion_TLS_VERSION_1_2,
		"1.3":     pb.TLSVersion_TLS_VERSION_1_3,
		"":        pb.TLSVersion_TLS_VERSION_UNSPECIFIED,
		"unknown": pb.TLSVersion_TLS_VERSION_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := tlsVersionToProto(in); got != want {
			t.Errorf("tlsVersionToProto(%q) = %v, want %v", in, got, want)
		}
	}
}

// Ensure the certificate handler map covers exactly the proto-exposed
// methods and nothing more.
func TestCertificateHandlerMap_Coverage(t *testing.T) {
	want := []string{
		"IssueCertificate",
		"SignCSR",
		"RevokeCertificate",
		"VerifyCertificate",
		"GetCACertificate",
		"GetTLSConfig",
	}
	if len(certificateHandlerMap) != len(want) {
		t.Fatalf("certificateHandlerMap has %d entries, want %d", len(certificateHandlerMap), len(want))
	}
	for _, m := range want {
		if _, ok := certificateHandlerMap[m]; !ok {
			t.Errorf("certificateHandlerMap missing method %q", m)
		}
	}
}

// Compile-time guard: the SDK certificate response types referenced by the
// handler exist with the fields the handler writes. This keeps the test
// package honest if the SDK request/response shapes change.
var (
	_ = certificate.IssueCertificateResponse{}
	_ = certificate.GetCACertificateResponse{}
)
