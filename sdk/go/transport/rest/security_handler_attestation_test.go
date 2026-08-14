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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// attestationRecorder captures the path and decoded body of the request the
// transport issued, and replies with the supplied JSON body.
type attestationRecorder struct {
	path   string
	method string
	body   map[string]interface{}
}

func newAttestationServer(t *testing.T, rec *attestationRecorder, status int, reply interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.path = r.URL.Path
		rec.method = r.Method
		_ = json.NewDecoder(r.Body).Decode(&rec.body)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if reply != nil {
			_ = json.NewEncoder(w).Encode(reply)
		}
	}))
}

func TestIsSecurityService_IncludesAttestation(t *testing.T) {
	if !isSecurityService("quicbaas.Attestation") {
		t.Fatal("quicbaas.Attestation must route through the security REST handler")
	}
	if isSecurityService("quicbaas.KV") {
		t.Fatal("unrelated services must not route through the security handler")
	}
}

func TestAttestationRESTHandler_Verify(t *testing.T) {
	rec := &attestationRecorder{}
	server := newAttestationServer(t, rec, http.StatusOK, map[string]interface{}{
		"valid":           true,
		"format":          "tpm2",
		"chain_valid":     true,
		"signature_valid": true,
	})
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify",
		&attestationclient.VerifyRequest{
			Format:          "tpm2",
			ChainPEM:        [][]byte{[]byte("cert")},
			AttestedKeyDER:  []byte("key"),
			AttestationData: []byte("data"),
			Signature:       []byte("sig"),
		})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	typed, ok := resp.(*attestationclient.VerifyResponse)
	if !ok {
		t.Fatalf("expected *attestation.VerifyResponse, got %T", resp)
	}
	if !typed.Valid || !typed.ChainValid || !typed.SignatureValid {
		t.Fatalf("unexpected verdict: %+v", typed)
	}
	if rec.path != "/api/v1/attestation/verify" {
		t.Fatalf("path = %q", rec.path)
	}
	if rec.method != http.MethodPost {
		t.Fatalf("method = %q", rec.method)
	}
	if rec.body["format"] != "tpm2" {
		t.Fatalf("format not sent: %+v", rec.body)
	}
}

func TestAttestationRESTHandler_Verify_Errors(t *testing.T) {
	rec := &attestationRecorder{}
	server := newAttestationServer(t, rec, http.StatusOK, map[string]interface{}{"valid": true})
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "NilRequest", req: nil},
		{name: "WrongType", req: &attestationclient.VerifyChainRequest{}},
		{
			name: "MissingFormat",
			req:  &attestationclient.VerifyRequest{ChainPEM: [][]byte{[]byte("c")}},
		},
		{
			name: "MissingChain",
			req:  &attestationclient.VerifyRequest{Format: "tpm2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tr.Request(context.Background(), "quicbaas.Attestation", "Verify", tt.req); err == nil {
				t.Fatal("expected a client-side validation error")
			}
		})
	}
}

func TestAttestationRESTHandler_VerifyChain(t *testing.T) {
	rec := &attestationRecorder{}
	server := newAttestationServer(t, rec, http.StatusOK, map[string]interface{}{"valid": true})
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Attestation", "VerifyChain",
		&attestationclient.VerifyChainRequest{
			Format:   "tpm2",
			ChainPEM: [][]byte{[]byte("leaf")},
		})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	typed, ok := resp.(*attestationclient.VerifyChainResponse)
	if !ok {
		t.Fatalf("expected *attestation.VerifyChainResponse, got %T", resp)
	}
	if !typed.Valid {
		t.Fatal("chain should validate")
	}
	if rec.path != "/api/v1/attestation/verify-chain" {
		t.Fatalf("path = %q", rec.path)
	}
}

func TestAttestationRESTHandler_VerifyChain_Errors(t *testing.T) {
	rec := &attestationRecorder{}
	server := newAttestationServer(t, rec, http.StatusOK, map[string]interface{}{"valid": true})
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.Attestation", "VerifyChain", nil); err == nil {
		t.Fatal("expected an error for a nil request")
	}
	if _, err := tr.Request(context.Background(), "quicbaas.Attestation", "VerifyChain",
		&attestationclient.VerifyChainRequest{Format: "tpm2"}); err == nil {
		t.Fatal("expected an error for an empty chain")
	}
}

func TestAttestationRESTHandler_AttestContainer(t *testing.T) {
	rec := &attestationRecorder{}
	server := newAttestationServer(t, rec, http.StatusOK, map[string]interface{}{
		"attested":      true,
		"container_id":  "container-1",
		"config_digest": "sha256:cfg",
		"measured":      true,
		"attested_at":   "2026-08-09T12:00:00Z",
	})
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer",
		&attestationclient.AttestContainerRequest{
			Image:       "alpine@sha256:abc",
			ContainerID: "container-1",
			TrustLevel:  "measured",
		})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}

	typed, ok := resp.(*attestationclient.AttestContainerResponse)
	if !ok {
		t.Fatalf("expected *attestation.AttestContainerResponse, got %T", resp)
	}
	if !typed.Attested || !typed.Measured {
		t.Fatalf("unexpected evidence: %+v", typed)
	}
	if typed.AttestedAt != "2026-08-09T12:00:00Z" {
		t.Fatalf("attested_at = %q", typed.AttestedAt)
	}
	if rec.path != "/api/v1/attestation/attest-container" {
		t.Fatalf("path = %q", rec.path)
	}
}

func TestAttestationRESTHandler_AttestContainer_Errors(t *testing.T) {
	t.Run("ClientSideValidation", func(t *testing.T) {
		rec := &attestationRecorder{}
		server := newAttestationServer(t, rec, http.StatusOK, map[string]interface{}{"attested": true})
		defer server.Close()

		tr := newTestTransport(t, server)
		defer tr.Close()

		if _, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer", nil); err == nil {
			t.Fatal("expected an error for a nil request")
		}
		if _, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer",
			&attestationclient.AttestContainerRequest{}); err == nil {
			t.Fatal("expected an error for a missing image")
		}
	})

	// A server that has no attestor registered answers 503; the transport
	// must surface that rather than a decode failure.
	t.Run("NotConfiguredPropagates", func(t *testing.T) {
		rec := &attestationRecorder{}
		server := newAttestationServer(t, rec, http.StatusServiceUnavailable,
			map[string]interface{}{"error": "no container attestor is registered on this server"})
		defer server.Close()

		tr := newTestTransport(t, server)
		defer tr.Close()

		_, err := tr.Request(context.Background(), "quicbaas.Attestation", "AttestContainer",
			&attestationclient.AttestContainerRequest{Image: "alpine@sha256:abc"})
		if err == nil {
			t.Fatal("expected the server error to propagate")
		}
	})
}

// TestAttestationRESTHandler_UnknownMethod pins the closed surface: a verb the
// server does not expose returns a typed Unimplemented naming the verb rather
// than a 404 from a nonexistent endpoint.
func TestAttestationRESTHandler_UnknownMethod(t *testing.T) {
	rec := &attestationRecorder{}
	server := newAttestationServer(t, rec, http.StatusOK, nil)
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Attestation", "Quarantine", nil)
	if err == nil {
		t.Fatal("expected an unimplemented error")
	}
	if code := sdkerrors.Code(err); code != sdkerrors.CodeUnimplemented {
		t.Fatalf("error code = %v, want CodeUnimplemented (err: %v)", code, err)
	}
	if rec.path != "" {
		t.Fatalf("no request should have been issued, but the server saw %q", rec.path)
	}
}
