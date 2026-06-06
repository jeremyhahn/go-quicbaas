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

// certificate_extra_test.go covers the Certificate MCP handler transforms the
// end-to-end client tests miss: SignCSR and Revoke request/response, the
// unsupported-method path, invalid request types, and timestamp parsing.

package mcp

import (
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func newCertHandler(t *testing.T) *certificateHandler {
	t.Helper()
	tr, err := New(&transport.Config{Address: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return newCertificateHandler(tr)
}

func TestCertificateSignCSR(t *testing.T) {
	h := newCertHandler(t)
	args, err := h.transformRequest("SignCSR", &certificate.SignCSRRequest{
		CSRPEM: "CSR", Usage: "server", ValidityDays: 90})
	if err != nil {
		t.Fatalf("transformRequest SignCSR: %v", err)
	}
	if args["csr_pem"] != "CSR" || args["usage"] != "server" || args["validity_days"] != 90 {
		t.Errorf("args = %v", args)
	}

	out, err := h.transformResponse("SignCSR", nil, map[string]interface{}{
		"certificate_pem": "CERT", "ca_certificate_pem": "CA",
		"chain_pem": "CHAIN", "serial_number": "0A"})
	if err != nil {
		t.Fatalf("transformResponse SignCSR: %v", err)
	}
	resp := out.(*certificate.SignCSRResponse)
	if resp.CertificatePEM != "CERT" || resp.SerialNumber != "0A" {
		t.Errorf("response mismatch: %+v", resp)
	}
}

func TestCertificateRevoke(t *testing.T) {
	h := newCertHandler(t)
	args, err := h.transformRequest("RevokeCertificate", &certificate.RevokeCertificateRequest{
		SerialNumber: "0A1B"})
	if err != nil {
		t.Fatalf("transformRequest Revoke: %v", err)
	}
	if args["serial_number"] != "0A1B" {
		t.Errorf("serial_number = %v", args["serial_number"])
	}

	out, err := h.transformResponse("RevokeCertificate", nil, map[string]interface{}{"success": true})
	if err != nil {
		t.Fatalf("transformResponse Revoke: %v", err)
	}
	if !out.(*certificate.RevokeCertificateResponse).Success {
		t.Error("expected revoke success")
	}
}

func TestCertificateUnsupportedMethod(t *testing.T) {
	h := newCertHandler(t)
	if _, err := h.transformRequest("GenerateCRL", nil); err == nil {
		t.Error("unsupported request method must error")
	}
	if _, err := h.transformResponse("GenerateCRL", nil, nil); err == nil {
		t.Error("unsupported response method must error")
	}
}

func TestCertificateInvalidRequestTypes(t *testing.T) {
	h := newCertHandler(t)
	for _, m := range []string{"IssueCertificate", "SignCSR", "RevokeCertificate",
		"VerifyCertificate", "GetTLSConfig"} {
		if _, err := h.transformRequest(m, "wrong"); err == nil {
			t.Errorf("%s with wrong type must error", m)
		}
	}
}

func TestCertificateGetTLSConfigNotConfigured(t *testing.T) {
	h := newCertHandler(t)
	_, err := h.transformResponse("GetTLSConfig",
		&certificate.GetTLSConfigRequest{ServerName: "svc"},
		map[string]interface{}{"configured": false})
	if err == nil {
		t.Fatal("expected error when server reports not configured")
	}
}

func TestCertificateGetTLSConfigDefaultsAndClientAuth(t *testing.T) {
	h := newCertHandler(t)
	// is_server + require_client_cert and an explicit 1.2 min version.
	out, err := h.transformResponse("GetTLSConfig",
		&certificate.GetTLSConfigRequest{
			ServerName: "svc", MinVersion: "1.2", IsServer: true, RequireClientCert: true},
		map[string]interface{}{"configured": true})
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	cfg := out.(*certificate.GetTLSConfigResponse).Config
	if cfg.ServerName != "svc" {
		t.Errorf("server name = %q", cfg.ServerName)
	}
}

func TestCertificateGetTLSConfigNilRequestFallback(t *testing.T) {
	h := newCertHandler(t)
	// A nil/typed-wrong req falls back to an empty GetTLSConfigRequest.
	out, err := h.transformResponse("GetTLSConfig", "not-a-request",
		map[string]interface{}{"configured": true})
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	if out.(*certificate.GetTLSConfigResponse).Config == nil {
		t.Error("expected non-nil config from fallback")
	}
}

func TestTransformTimeFromString(t *testing.T) {
	if transformTimeFromString("2026-05-29T00:00:00Z").IsZero() {
		t.Error("RFC3339 parse failed")
	}
	if transformTimeFromString("2026-05-29T00:00:00.5Z").IsZero() {
		t.Error("RFC3339Nano parse failed")
	}
	if !transformTimeFromString("garbage").IsZero() {
		t.Error("garbage should yield zero time")
	}
	if !transformTimeFromString("").Equal(time.Time{}) {
		t.Error("empty string should yield zero time")
	}
}
