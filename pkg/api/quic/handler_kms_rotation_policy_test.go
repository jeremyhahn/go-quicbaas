//go:build quic

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// KMS Rotation Policy Handler Tests
//
// Covers handleKMSSetRotationPolicy / handleKMSGetRotationPolicy /
// handleKMSDeleteRotationPolicy which had 0% coverage. Each handler has four
// branches: nil service, decode error, service error, and success.
// =============================================================================

func TestHandleKMSSetRotationPolicy_Success(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	payload, err := EncodePayload(&service.SetKeyRotationPolicyRequest{KeyID: "test/key"})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSSetRotationPolicy, Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSSetRotationPolicy_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetKeyRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSSetRotationPolicy, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSSetRotationPolicy_DecodeError(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSSetRotationPolicy, Payload: []byte("invalid")})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSSetRotationPolicy_ServiceError(t *testing.T) {
	handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetKeyRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSSetRotationPolicy, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSGetRotationPolicy_Success(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	payload, err := EncodePayload(&service.GetKeyRotationPolicyRequest{KeyID: "test/key"})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetRotationPolicy, Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSGetRotationPolicy_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetKeyRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetRotationPolicy, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSGetRotationPolicy_DecodeError(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetRotationPolicy, Payload: []byte("invalid")})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSGetRotationPolicy_ServiceError(t *testing.T) {
	handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetKeyRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSGetRotationPolicy, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSDeleteRotationPolicy_Success(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	payload, err := EncodePayload(&service.DeleteKeyRotationPolicyRequest{KeyID: "test/key"})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSDeleteRotationPolicy, Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSDeleteRotationPolicy_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.DeleteKeyRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSDeleteRotationPolicy, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSDeleteRotationPolicy_DecodeError(t *testing.T) {
	handler := NewHandler(&mockService{}, &securityTestKMSService{}, nil, nil, createKMSTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSDeleteRotationPolicy, Payload: []byte("invalid")})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSDeleteRotationPolicy_ServiceError(t *testing.T) {
	handler := NewHandler(&mockService{}, &errorKMSService{}, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.DeleteKeyRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpKMSDeleteRotationPolicy, Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}
