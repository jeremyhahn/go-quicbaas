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
	"testing"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestIsAttestationService(t *testing.T) {
	if !isAttestationService("quicbaas.Attestation") {
		t.Fatal("quicbaas.Attestation must branch into the typed attestation handler")
	}
	for _, svc := range []string{"quicbaas.Trust", "quicbaas.Certificate", "quicbaas.KV", ""} {
		if isAttestationService(svc) {
			t.Errorf("%q must not branch into the attestation handler", svc)
		}
	}
}

// TestAttestationHandler_UnknownMethod pins the closed surface. The handler
// resolves the method before touching the connection, so a nil conn is fine
// here and the test needs no server.
func TestAttestationHandler_UnknownMethod(t *testing.T) {
	h := newAttestationHandler(nil)

	_, err := h.handleRequest(context.Background(), "quicbaas.Attestation", "Quarantine", nil)
	if err == nil {
		t.Fatal("expected an unimplemented error")
	}
	if code := sdkerrors.Code(err); code != sdkerrors.CodeUnimplemented {
		t.Fatalf("error code = %v, want CodeUnimplemented (err: %v)", code, err)
	}
}

// TestAttestationHandler_RequestValidation proves every verb rejects a
// malformed request before it opens a connection — with a nil conn any call
// that reached the wire would panic, so reaching a clean typed error is
// itself the assertion.
func TestAttestationHandler_RequestValidation(t *testing.T) {
	h := newAttestationHandler(nil)

	tests := []struct {
		name   string
		method string
		req    interface{}
	}{
		{name: "VerifyWrongType", method: "Verify", req: &attestationclient.VerifyChainRequest{}},
		{name: "VerifyNil", method: "Verify", req: (*attestationclient.VerifyRequest)(nil)},
		{
			name:   "VerifyMissingFormat",
			method: "Verify",
			req:    &attestationclient.VerifyRequest{ChainPEM: [][]byte{{1}}},
		},
		{
			name:   "VerifyMissingChain",
			method: "Verify",
			req:    &attestationclient.VerifyRequest{Format: "tpm2"},
		},
		{name: "VerifyChainWrongType", method: "VerifyChain", req: &attestationclient.VerifyRequest{}},
		{
			name:   "VerifyChainEmpty",
			method: "VerifyChain",
			req:    &attestationclient.VerifyChainRequest{Format: "tpm2"},
		},
		{name: "AttestContainerWrongType", method: "AttestContainer", req: &attestationclient.VerifyRequest{}},
		{
			name:   "AttestContainerMissingImage",
			method: "AttestContainer",
			req:    &attestationclient.AttestContainerRequest{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), "quicbaas.Attestation", tt.method, tt.req)
			if err == nil {
				t.Fatal("expected a client-side validation error")
			}
			if code := sdkerrors.Code(err); code != sdkerrors.CodeInvalidInput {
				t.Fatalf("error code = %v, want CodeInvalidInput (err: %v)", code, err)
			}
		})
	}
}

func TestAttestationOptionsToProto(t *testing.T) {
	t.Run("NilStaysNil", func(t *testing.T) {
		if got := attestationOptionsToProto(nil); got != nil {
			t.Fatalf("nil options must stay nil so the server applies its secure defaults, got %+v", got)
		}
	})

	t.Run("FieldsPassThrough", func(t *testing.T) {
		got := attestationOptionsToProto(&attestationclient.VerifyOptions{
			TrustedRootsPEM:        [][]byte{[]byte("root")},
			CheckFreshness:         true,
			ExpectedNonce:          []byte("nonce"),
			FreshnessWindowSeconds: 42,
			VerifyPCRs:             true,
			ExpectedPCRs:           map[uint32][]byte{7: {0x01}},
			AllowSelfSigned:        true,
		})
		if got == nil {
			t.Fatal("expected options")
		}
		if len(got.TrustedRootsPem) != 1 || !got.CheckFreshness || got.FreshnessWindowSeconds != 42 {
			t.Fatalf("fields did not pass through: %+v", got)
		}
		if !got.VerifyPcrs || len(got.ExpectedPcrs) != 1 || !got.AllowSelfSigned {
			t.Fatalf("fields did not pass through: %+v", got)
		}
	})
}

// TestAttestationServicePrefixMatchesProto pins the SDK routing identifier
// against the generated descriptor's own name so the two cannot drift into a
// state where the SDK routes to a service the server never registered.
func TestAttestationServicePrefixMatchesProto(t *testing.T) {
	if attestationServicePrefix != attestationclient.ServiceName {
		t.Fatalf("prefix %q does not match the leaf service name %q",
			attestationServicePrefix, attestationclient.ServiceName)
	}
	const wantProtoService = "quicbaas.attestation.v1.AttestationService"
	if pb.AttestationService_ServiceDesc.ServiceName != wantProtoService {
		t.Fatalf("proto service name = %q, want %q",
			pb.AttestationService_ServiceDesc.ServiceName, wantProtoService)
	}
	if len(pb.AttestationService_ServiceDesc.Methods) != 3 {
		t.Fatalf("AttestationService exposes %d RPCs, want 3",
			len(pb.AttestationService_ServiceDesc.Methods))
	}
}
