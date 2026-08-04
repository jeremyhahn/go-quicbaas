//go:build quic

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

package quic

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// KMS Key Administration Handler Tests
//
// Covers handleKMSRevokeKey / handleKMSGetPublicKey /
// handleKMSGetSupportedAlgorithms wired on opcodes 0xC2-0xC4.
// =============================================================================

func TestHandleKMSRevokeKey_Success(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	payload, err := EncodePayload(&service.RevokeKeyRequest{KeyID: "test/key", Reason: "compromised"})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSRevokeKey, Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	var out service.RevokeKeyResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestHandleKMSRevokeKey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RevokeKeyRequest{KeyID: "test/key"})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSRevokeKey, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSRevokeKey_DecodeError(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSRevokeKey, Payload: []byte("invalid")})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSRevokeKey_ServiceError(t *testing.T) {
	handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RevokeKeyRequest{KeyID: "test/key"})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSRevokeKey, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSGetPublicKey_Success(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	payload, err := EncodePayload(&service.GetPublicKeyRequest{KeyID: "test/key"})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetPublicKey, Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	var out service.GetPublicKeyResponse
	if err := DecodePayload(resp.Payload, &out); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
}

func TestHandleKMSGetPublicKey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetPublicKeyRequest{KeyID: "test/key"})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetPublicKey, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSGetPublicKey_DecodeError(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetPublicKey, Payload: []byte("invalid")})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSGetPublicKey_ServiceError(t *testing.T) {
	handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetPublicKeyRequest{KeyID: "test/key"})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetPublicKey, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSGetSupportedAlgorithms_Success(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetSupportedAlgorithms})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	var algorithms []string
	if err := DecodePayload(resp.Payload, &algorithms); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(algorithms) == 0 {
		t.Error("expected at least one supported algorithm")
	}
}

func TestHandleKMSGetSupportedAlgorithms_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetSupportedAlgorithms})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSGetSupportedAlgorithms_ServiceError(t *testing.T) {
	handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetSupportedAlgorithms})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// TestKMSAdminOpcodeAuthzMapping asserts the new opcodes resolve to the
// expected coarse resource/action pairs used by the wire authorizer.
func TestKMSAdminOpcodeAuthzMapping(t *testing.T) {
	tests := []struct {
		name         string
		opcode       byte
		wantResource string
		wantAction   string
	}{
		{"revoke key is a delete", OpKMSRevokeKey, "kms", "delete"},
		{"get public key is a read", OpKMSGetPublicKey, "kms", "read"},
		{"supported algorithms is a read", OpKMSGetSupportedAlgorithms, "kms", "read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action := getResourceAndAction(tt.opcode)
			if resource != tt.wantResource || action != tt.wantAction {
				t.Errorf("got (%s, %s), want (%s, %s)", resource, action, tt.wantResource, tt.wantAction)
			}
		})
	}
}
