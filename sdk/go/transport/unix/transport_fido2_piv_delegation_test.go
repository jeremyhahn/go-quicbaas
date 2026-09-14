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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestTransport_FIDO2_Delegates proves the Unix transport routes the FIDO2
// ceremony surface through the gRPC FIDO2 handler.
//
// Unix is gRPC on a domain socket, so the server registers ONE FIDO2Service
// for both. With no server listening and a short deadline every call fails
// on connection — but crucially NOT with "not supported" or "method not
// exposed", which would mean the dispatch table never saw the request. The
// gRPC handler tests assert the full proto round-trip; this pins the
// delegation contract.
func TestTransport_FIDO2_Delegates(t *testing.T) {
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"begin-registration", "BeginRegistration", &xkmstransport.BeginRegistrationRequest{Email: "alice@example.com"}},
		{"finish-registration", "FinishRegistration", &xkmstransport.FinishRegistrationRequest{SessionID: "s", AttestationJSON: "{}"}},
		{"begin-authentication", "BeginAuthentication", &xkmstransport.BeginAuthenticationRequest{Email: "alice@example.com"}},
		{"finish-authentication", "FinishAuthentication", &xkmstransport.FinishAuthenticationRequest{SessionID: "s", Email: "alice@example.com", AssertionJSON: "{}"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, "quicbaas.FIDO2", tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("fido2 request was rejected as unsupported, dispatch regression: %v", err)
			}
			if strings.Contains(msg, "not exposed") {
				t.Fatalf("fido2 method %q is not in the gRPC dispatch table: %v", tc.method, err)
			}
			if strings.Contains(msg, "invalid request type") {
				t.Fatalf("fido2 method %q rejected its own wire request type: %v", tc.method, err)
			}
		})
	}
}

// TestTransport_PIV_Delegates proves the Unix transport routes the PIV
// surface through the gRPC PIV handler with the xkms wire request types.
func TestTransport_PIV_Delegates(t *testing.T) {
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"list-slots", "ListPIVSlots", &xkmstransport.ListPIVSlotsRequest{Backend: "file"}},
		{"get-cert", "GetPIVCertificate", &xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"}},
		{"export-cert", "ExportPIVCertificate", &xkmstransport.GetPIVCertificateRequest{Backend: "file", Slot: "9a", Format: "pem"}},
		{"store-cert", "StorePIVCertificate", &xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9c", Certificate: []byte("cert"), Format: "pem"}},
		{"import-cert", "ImportPIVCertificate", &xkmstransport.StorePIVCertificateRequest{Backend: "file", Slot: "9c", Certificate: []byte("cert"), Format: "pem"}},
		{"delete-cert", "DeletePIVCertificate", &xkmstransport.DeletePIVCertificateRequest{Backend: "file", Slot: "9a"}},
		{"generate-key", "GeneratePIVKey", &xkmstransport.GeneratePIVKeyRequest{Backend: "file", Slot: "9a", Algorithm: "ecdsap256", Subject: "CN=alice"}},
		{"generate-csr", "GeneratePIVCSR", &xkmstransport.GeneratePIVCSRRequest{Backend: "file", Slot: "9a", Subject: "CN=alice"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, "quicbaas.PIV", tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("piv request was rejected as unsupported, dispatch regression: %v", err)
			}
			if strings.Contains(msg, "not exposed") {
				t.Fatalf("piv method %q is not in the gRPC dispatch table: %v", tc.method, err)
			}
			if strings.Contains(msg, "invalid request type") {
				t.Fatalf("piv method %q rejected its own wire request type: %v", tc.method, err)
			}
		})
	}
}

// TestTransport_FIDO2PIV_ClosedTransport proves a closed Unix transport
// rejects the new verbs instead of dialing a dead socket.
func TestTransport_FIDO2PIV_ClosedTransport(t *testing.T) {
	tr := newWiredTransport(t)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := tr.Request(context.Background(), "quicbaas.FIDO2", "BeginAuthentication",
		&xkmstransport.BeginAuthenticationRequest{Email: "u"}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a closed-transport error, got: %v", err)
	}
	if _, err := tr.Request(context.Background(), "quicbaas.PIV", "ListPIVSlots",
		&xkmstransport.ListPIVSlotsRequest{Backend: "file"}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a closed-transport error, got: %v", err)
	}
}
