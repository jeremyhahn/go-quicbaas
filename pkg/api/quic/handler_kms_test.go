//go:build quic

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

package quic

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// KMS DEK Handler Tests
// These tests cover the handleKMSDEK* functions that have 0% coverage
// =============================================================================

// Test handleKMSDEKCreate - success path
func TestHandleKMSDEKCreate_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, err := EncodePayload(&service.CreateDEKRequest{
		Path:      "test/dek",
		Algorithm: service.KeyAlgAES256GCM,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpKMSDEKCreate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKCreate - nil service error
func TestHandleKMSDEKCreate_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.CreateDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKCreate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKCreate - decode error
func TestHandleKMSDEKCreate_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKCreate,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKCreate - service error
func TestHandleKMSDEKCreate_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.CreateDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKCreate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKGet - success path
func TestHandleKMSDEKGet_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetDEKRequest{
		Path: "test/dek",
	})

	req := &Request{
		Opcode:  OpKMSDEKGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKGet - nil service error
func TestHandleKMSDEKGet_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKGet - decode error
func TestHandleKMSDEKGet_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKGet,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKGet - service error
func TestHandleKMSDEKGet_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKList - success path
func TestHandleKMSDEKList_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ListDEKsRequest{})

	req := &Request{
		Opcode:  OpKMSDEKList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKList - nil service error
func TestHandleKMSDEKList_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ListDEKsRequest{})
	req := &Request{
		Opcode:  OpKMSDEKList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKList - decode error
func TestHandleKMSDEKList_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKList,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKList - service error
func TestHandleKMSDEKList_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ListDEKsRequest{})
	req := &Request{
		Opcode:  OpKMSDEKList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKRotate - success path
func TestHandleKMSDEKRotate_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RotateDEKRequest{
		Path: "test/dek",
	})

	req := &Request{
		Opcode:  OpKMSDEKRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKRotate - nil service error
func TestHandleKMSDEKRotate_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RotateDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKRotate - decode error
func TestHandleKMSDEKRotate_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKRotate,
		Payload: []byte("not valid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKRotate - service error
func TestHandleKMSDEKRotate_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RotateDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKDelete - success path
func TestHandleKMSDEKDelete_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.DeleteDEKRequest{
		Path: "test/dek",
	})

	req := &Request{
		Opcode:  OpKMSDEKDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKDelete - nil service error
func TestHandleKMSDEKDelete_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.DeleteDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKDelete - decode error
func TestHandleKMSDEKDelete_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKDelete,
		Payload: []byte("invalid json payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKDelete - service error
func TestHandleKMSDEKDelete_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.DeleteDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKStatus - success path
func TestHandleKMSDEKStatus_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetDEKStatusRequest{
		Path: "test/dek",
	})

	req := &Request{
		Opcode:  OpKMSDEKStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKStatus - nil service error
func TestHandleKMSDEKStatus_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetDEKStatusRequest{})
	req := &Request{
		Opcode:  OpKMSDEKStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKStatus - decode error
func TestHandleKMSDEKStatus_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKStatus,
		Payload: []byte("bad payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKStatus - service error
func TestHandleKMSDEKStatus_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.GetDEKStatusRequest{})
	req := &Request{
		Opcode:  OpKMSDEKStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKSetDefault - success path
func TestHandleKMSDEKSetDefault_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetDefaultDEKRequest{
		Path: "test/dek",
	})

	req := &Request{
		Opcode:  OpKMSDEKSetDefault,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKSetDefault - nil service error
func TestHandleKMSDEKSetDefault_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetDefaultDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKSetDefault,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKSetDefault - decode error
func TestHandleKMSDEKSetDefault_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKSetDefault,
		Payload: []byte("not json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKSetDefault - service error
func TestHandleKMSDEKSetDefault_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetDefaultDEKRequest{})
	req := &Request{
		Opcode:  OpKMSDEKSetDefault,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKSetSafety - success path
func TestHandleKMSDEKSetSafety_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetDEKSafetyRequest{
		Path:          "test/dek",
		MaxOperations: 1000,
	})

	req := &Request{
		Opcode:  OpKMSDEKSetSafety,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKSetSafety - nil service error
func TestHandleKMSDEKSetSafety_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetDEKSafetyRequest{})
	req := &Request{
		Opcode:  OpKMSDEKSetSafety,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKSetSafety - decode error
func TestHandleKMSDEKSetSafety_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKSetSafety,
		Payload: []byte("invalid payload data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKSetSafety - service error
func TestHandleKMSDEKSetSafety_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.SetDEKSafetyRequest{})
	req := &Request{
		Opcode:  OpKMSDEKSetSafety,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSDEKClearSafety - success path
func TestHandleKMSDEKClearSafety_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ClearDEKSafetyRequest{
		Path: "test/dek",
	})

	req := &Request{
		Opcode:  OpKMSDEKClearSafety,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSDEKClearSafety - nil service error
func TestHandleKMSDEKClearSafety_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ClearDEKSafetyRequest{})
	req := &Request{
		Opcode:  OpKMSDEKClearSafety,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSDEKClearSafety - decode error
func TestHandleKMSDEKClearSafety_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSDEKClearSafety,
		Payload: []byte("garbage data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSDEKClearSafety - service error
func TestHandleKMSDEKClearSafety_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ClearDEKSafetyRequest{})
	req := &Request{
		Opcode:  OpKMSDEKClearSafety,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// KMS Backend Handler Tests
// These tests cover the handleKMSBackend* functions that have 0% coverage
// =============================================================================

// Test handleKMSBackendStatus - success path
func TestHandleKMSBackendStatus_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSBackendStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSBackendStatus - nil service error
func TestHandleKMSBackendStatus_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSBackendStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSBackendStatus - service error
func TestHandleKMSBackendStatus_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSBackendStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSBackendTest - success path
func TestHandleKMSBackendTest_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSBackendTest,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSBackendTest - nil service error
func TestHandleKMSBackendTest_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSBackendTest,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSBackendTest - service error
func TestHandleKMSBackendTest_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSBackendTest,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// KMS Key Version and Status Handler Tests
// These tests cover handleKMSKeyVersions and handleKMSKeyStatus
// =============================================================================

// Test handleKMSKeyVersions - success path
func TestHandleKMSKeyVersions_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ListKeyVersionsRequest{
		KeyID: "test-key-id",
	})

	req := &Request{
		Opcode:  OpKMSKeyVersions,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSKeyVersions - nil service error
func TestHandleKMSKeyVersions_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ListKeyVersionsRequest{})
	req := &Request{
		Opcode:  OpKMSKeyVersions,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSKeyVersions - decode error
func TestHandleKMSKeyVersions_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSKeyVersions,
		Payload: []byte("not valid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSKeyVersions - service error
func TestHandleKMSKeyVersions_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.ListKeyVersionsRequest{})
	req := &Request{
		Opcode:  OpKMSKeyVersions,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// Test handleKMSKeyStatus - success path
func TestHandleKMSKeyStatus_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.UpdateKeyStatusRequest{
		KeyID:  "test-key-id",
		Status: "active",
	})

	req := &Request{
		Opcode:  OpKMSKeyStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleKMSKeyStatus - nil service error
func TestHandleKMSKeyStatus_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.UpdateKeyStatusRequest{})
	req := &Request{
		Opcode:  OpKMSKeyStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

// Test handleKMSKeyStatus - decode error
func TestHandleKMSKeyStatus_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpKMSKeyStatus,
		Payload: []byte("bad json data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleKMSKeyStatus - service error
func TestHandleKMSKeyStatus_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.UpdateKeyStatusRequest{})
	req := &Request{
		Opcode:  OpKMSKeyStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// SecretsRotate Handler Tests
// This test covers handleSecretsRotate which has 0% coverage
// =============================================================================

// Test handleSecretsRotate - success path
func TestHandleSecretsRotate_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RotateSecretRequest{
		SecretID: "secret/test",
	})

	req := &Request{
		Opcode:  OpSecretsRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// Test handleSecretsRotate - nil service error
func TestHandleSecretsRotate_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RotateSecretRequest{})
	req := &Request{
		Opcode:  OpSecretsRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil Secrets service")
	}
}

// Test handleSecretsRotate - decode error
func TestHandleSecretsRotate_DecodeError(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createKMSTestLogger())

	req := &Request{
		Opcode:  OpSecretsRotate,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

// Test handleSecretsRotate - service error
func TestHandleSecretsRotate_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsRotateService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.RotateSecretRequest{})
	req := &Request{
		Opcode:  OpSecretsRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Helpers
// =============================================================================

// errorSecretsRotateService is a dedicated mock that returns an error on RotateSecret
type errorSecretsRotateService struct {
	securityTestSecretsService
}

func (s *errorSecretsRotateService) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return nil, errors.New("rotate secret error")
}

func createKMSTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-kms-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

// =============================================================================
// KMS Encrypt/Decrypt Handler Tests
// Tests for handleKMSEncrypt and handleKMSDecrypt nil crypto service and
// service error paths.
// =============================================================================

// TestHandleKMSEncrypt_NilCryptoService verifies that handleKMSEncrypt returns
// an error when the crypto service is nil.
func TestHandleKMSEncrypt_NilCryptoService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.EncryptRequest{
		KeyID:     "key1",
		Plaintext: []byte("data"),
	})
	req := &Request{Opcode: OpKMSEncrypt, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil crypto service")
	}
}

// TestHandleKMSEncrypt_ServiceError verifies that handleKMSEncrypt returns
// an error when the crypto service returns an error.
func TestHandleKMSEncrypt_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createKMSTestLogger())

	payload, _ := EncodePayload(&service.EncryptRequest{
		KeyID:     "key1",
		Plaintext: []byte("data"),
	})
	req := &Request{Opcode: OpKMSEncrypt, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from crypto service")
	}
}

// TestHandleKMSDecrypt_NilCryptoService verifies that handleKMSDecrypt returns
// an error when the crypto service is nil.
func TestHandleKMSDecrypt_NilCryptoService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createKMSTestLogger())

	payload, _ := EncodePayload(&service.DecryptRequest{
		KeyID:      "key1",
		Ciphertext: []byte("data"),
	})
	req := &Request{Opcode: OpKMSDecrypt, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil crypto service")
	}
}

// TestHandleKMSDecrypt_ServiceError verifies that handleKMSDecrypt returns
// an error when the crypto service returns an error.
func TestHandleKMSDecrypt_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createKMSTestLogger())

	payload, _ := EncodePayload(&service.DecryptRequest{
		KeyID:      "key1",
		Ciphertext: []byte("data"),
	})
	req := &Request{Opcode: OpKMSDecrypt, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from crypto service")
	}
}

// --- Content moved from security_handlers_test.go ---

// =============================================================================
// Security Handler Coverage Tests
// These tests target low-coverage handler functions (KMS, Seal, Secrets, Crypto)
// All tests ensure both success and error paths are covered.
// =============================================================================

// Test all security handlers with nil service (error path)
func TestSecurityHandlers_NilServiceErrors(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	tests := []struct {
		name   string
		opcode byte
	}{
		// KMS Operations - already covered by services_coverage_test.go
		// Secrets Operations
		{"SecretsStore_NilService", OpSecretsStore},
		{"SecretsGet_NilService", OpSecretsGet},
		{"SecretsList_NilService", OpSecretsList},
		{"SecretsDelete_NilService", OpSecretsDelete},
		{"SecretsListVersions_NilService", OpSecretsListVersions},
		{"SecretsRollback_NilService", OpSecretsRollback},
		// Crypto Operations
		{"CryptoEncrypt_NilService", OpCryptoEncrypt},
		{"CryptoDecrypt_NilService", OpCryptoDecrypt},
		{"CryptoRewrap_NilService", OpCryptoRewrap},
		{"CryptoSign_NilService", OpCryptoSign},
		{"CryptoVerify_NilService", OpCryptoVerify},
		{"CryptoHash_NilService", OpCryptoHash},
		{"CryptoRandom_NilService", OpCryptoRandom},
		{"CryptoDataKey_NilService", OpCryptoDataKey},
		// Barrier / Ceremony dispatchers: nil service returns ServiceUnavailable.
		// Sub-op byte 0x01 (Status / Initialize) is supplied so dispatch
		// reaches the per-handler nil-service check instead of being rejected
		// as a missing sub-opcode by the dispatcher.
		{"BarrierStatus_NilService", OpBarrier},
		{"CeremonyStatus_NilService", OpCeremony},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := EncodePayload(map[string]interface{}{"test": "data"})
			// Barrier and Ceremony are dispatcher opcodes; the first byte of
			// the payload is the sub-op. Prepend a known sub-op so the
			// request reaches the per-handler nil-service check.
			switch tt.opcode {
			case OpBarrier:
				payload = append([]byte{SubOpBarrierStatus}, payload...)
			case OpCeremony:
				payload = append([]byte{SubOpCeremonyStatus}, payload...)
			}
			req := &Request{Opcode: tt.opcode, Payload: payload}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected error status when service is nil, got StatusOK")
			}
		})
	}
}

// Test all security handlers with decode errors
func TestSecurityHandlers_DecodeErrors(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	tests := []struct {
		name   string
		opcode byte
	}{
		// All opcodes that decode payload
		{"SecretsStore_DecodeError", OpSecretsStore},
		{"SecretsGet_DecodeError", OpSecretsGet},
		{"SecretsList_DecodeError", OpSecretsList},
		{"SecretsDelete_DecodeError", OpSecretsDelete},
		{"SecretsListVersions_DecodeError", OpSecretsListVersions},
		{"SecretsRollback_DecodeError", OpSecretsRollback},
		{"CryptoEncrypt_DecodeError", OpCryptoEncrypt},
		{"CryptoDecrypt_DecodeError", OpCryptoDecrypt},
		{"CryptoRewrap_DecodeError", OpCryptoRewrap},
		{"CryptoSign_DecodeError", OpCryptoSign},
		{"CryptoVerify_DecodeError", OpCryptoVerify},
		{"CryptoHash_DecodeError", OpCryptoHash},
		{"CryptoRandom_DecodeError", OpCryptoRandom},
		{"CryptoDataKey_DecodeError", OpCryptoDataKey},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Invalid payload that will fail decoding
			req := &Request{Opcode: tt.opcode, Payload: []byte("invalid json")}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected error status for invalid payload, got StatusOK")
			}
		})
	}
}

// =============================================================================
// Security Handlers Success Path Tests
// These test the success paths through the handlers with working mocks
// =============================================================================

// Mock service implementations for security services
type securityTestKMSService struct{}

func (s *securityTestKMSService) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	return &service.GenerateKeyResponse{}, nil
}
func (s *securityTestKMSService) ImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	return &service.ImportKeyResponse{}, nil
}
func (s *securityTestKMSService) GetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	return &service.GetKeyResponse{}, nil
}
func (s *securityTestKMSService) ListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	return &service.ListKeysResponse{}, nil
}
func (s *securityTestKMSService) RotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	return &service.RotateKeyResponse{}, nil
}
func (s *securityTestKMSService) RevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	return &service.RevokeKeyResponse{}, nil
}
func (s *securityTestKMSService) DeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	return &service.DeleteKeyResponse{}, nil
}
func (s *securityTestKMSService) GetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	return &service.GetKeyMetadataResponse{}, nil
}
func (s *securityTestKMSService) UpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	return &service.UpdateKeyMetadataResponse{}, nil
}
func (s *securityTestKMSService) SetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	return &service.SetKeyPolicyResponse{}, nil
}
func (s *securityTestKMSService) GetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	return &service.GetKeyPolicyResponse{}, nil
}
func (s *securityTestKMSService) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return &service.CreateDEKResponse{}, nil
}
func (s *securityTestKMSService) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return &service.GetDEKResponse{}, nil
}
func (s *securityTestKMSService) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return &service.ListDEKsResponse{}, nil
}
func (s *securityTestKMSService) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return &service.RotateDEKResponse{}, nil
}
func (s *securityTestKMSService) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return &service.DeleteDEKResponse{}, nil
}
func (s *securityTestKMSService) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return &service.GetDEKStatusResponse{}, nil
}
func (s *securityTestKMSService) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return &service.SetDefaultDEKResponse{}, nil
}
func (s *securityTestKMSService) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return &service.SetDEKSafetyResponse{}, nil
}
func (s *securityTestKMSService) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return &service.ClearDEKSafetyResponse{}, nil
}
func (s *securityTestKMSService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return &service.StartReencryptionResponse{}, nil
}
func (s *securityTestKMSService) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return &service.GetReencryptionProgressResponse{}, nil
}
func (s *securityTestKMSService) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{"AES-256-GCM", "AES-128-GCM"}, nil
}
func (s *securityTestKMSService) ListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	return &service.ListKeyVersionsResponse{}, nil
}
func (s *securityTestKMSService) UpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	return &service.UpdateKeyStatusResponse{}, nil
}
func (s *securityTestKMSService) BackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	return &service.BackendStatusResponse{}, nil
}
func (s *securityTestKMSService) BackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	return &service.BackendTestResponse{}, nil
}
func (s *securityTestKMSService) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	return &service.ExportKeyResponse{}, nil
}
func (s *securityTestKMSService) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	return &service.UnwrapKeyResponse{}, nil
}
func (s *securityTestKMSService) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	return &service.SetKeySafetyResponse{}, nil
}
func (s *securityTestKMSService) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	return &service.ClearKeySafetyResponse{}, nil
}
func (s *securityTestKMSService) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	return &service.SafetyCheckResponse{}, nil
}
func (s *securityTestKMSService) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	return &service.SafetyReportResponse{}, nil
}
func (s *securityTestKMSService) GetPublicKey(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	return &service.GetPublicKeyResponse{}, nil
}
func (s *securityTestKMSService) SetDefaultKey(ctx context.Context, req *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error) {
	return &service.SetDefaultKeyResponse{}, nil
}
func (s *securityTestKMSService) DeleteKeyPolicy(ctx context.Context, req *service.DeleteKeyPolicyRequest) (*service.DeleteKeyPolicyResponse, error) {
	return &service.DeleteKeyPolicyResponse{}, nil
}
func (s *securityTestKMSService) SetKeyRotationPolicy(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	return &service.SetKeyRotationPolicyResponse{}, nil
}
func (s *securityTestKMSService) GetKeyRotationPolicy(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	return &service.GetKeyRotationPolicyResponse{}, nil
}
func (s *securityTestKMSService) DeleteKeyRotationPolicy(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	return &service.DeleteKeyRotationPolicyResponse{}, nil
}

// errorKMSService is a mock KMS service that returns errors for all operations.
// Used to test error handling paths in handler functions.
type errorKMSService struct{}

var errKMSServiceError = errors.New("kms service error")

func (s *errorKMSService) GenerateKey(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) ImportKey(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetKey(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) ListKeys(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) RotateKey(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) RevokeKey(ctx context.Context, req *service.RevokeKeyRequest) (*service.RevokeKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) DeleteKey(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetKeyMetadata(ctx context.Context, req *service.GetKeyMetadataRequest) (*service.GetKeyMetadataResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) UpdateKeyMetadata(ctx context.Context, req *service.UpdateKeyMetadataRequest) (*service.UpdateKeyMetadataResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SetKeyPolicy(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetKeyPolicy(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetReencryptionProgress(ctx context.Context, req *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) ListKeyVersions(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) UpdateKeyStatus(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) BackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) BackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetPublicKey(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SetDefaultKey(ctx context.Context, req *service.SetDefaultKeyRequest) (*service.SetDefaultKeyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) DeleteKeyPolicy(ctx context.Context, req *service.DeleteKeyPolicyRequest) (*service.DeleteKeyPolicyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) SetKeyRotationPolicy(ctx context.Context, req *service.SetKeyRotationPolicyRequest) (*service.SetKeyRotationPolicyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) GetKeyRotationPolicy(ctx context.Context, req *service.GetKeyRotationPolicyRequest) (*service.GetKeyRotationPolicyResponse, error) {
	return nil, errKMSServiceError
}
func (s *errorKMSService) DeleteKeyRotationPolicy(ctx context.Context, req *service.DeleteKeyRotationPolicyRequest) (*service.DeleteKeyRotationPolicyResponse, error) {
	return nil, errKMSServiceError
}

type securityTestSecretsService struct{}

func (s *securityTestSecretsService) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return &service.StoreSecretResponse{}, nil
}
func (s *securityTestSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return &service.GetSecretResponse{}, nil
}
func (s *securityTestSecretsService) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return &service.ListSecretsResponse{}, nil
}
func (s *securityTestSecretsService) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return &service.DeleteSecretResponse{}, nil
}
func (s *securityTestSecretsService) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	return &service.GetSecretVersionResponse{}, nil
}
func (s *securityTestSecretsService) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return &service.ListSecretVersionsResponse{}, nil
}
func (s *securityTestSecretsService) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return &service.RollbackSecretResponse{}, nil
}
func (s *securityTestSecretsService) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return &service.RotateSecretResponse{}, nil
}
func (s *securityTestSecretsService) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	return &service.SetRotationPolicyResponse{}, nil
}
func (s *securityTestSecretsService) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	return &service.GetRotationPolicyResponse{}, nil
}
func (s *securityTestSecretsService) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return &service.UpdateSecretStatusResponse{}, nil
}
func (s *securityTestSecretsService) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	return &service.GetSecretsByPrefixResponse{}, nil
}
func (s *securityTestSecretsService) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	return nil, nil
}

type securityTestCryptoService struct{}

func (s *securityTestCryptoService) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	return &service.EncryptResponse{}, nil
}
func (s *securityTestCryptoService) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	return &service.DecryptResponse{}, nil
}
func (s *securityTestCryptoService) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	return &service.RewrapResponse{}, nil
}
func (s *securityTestCryptoService) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	return &service.EncryptBatchResponse{}, nil
}
func (s *securityTestCryptoService) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	return &service.DecryptBatchResponse{}, nil
}
func (s *securityTestCryptoService) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	return &service.GenerateDataKeyResponse{}, nil
}
func (s *securityTestCryptoService) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	return &service.GenerateWrappedDataKeyResponse{}, nil
}
func (s *securityTestCryptoService) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	return &service.SignResponse{}, nil
}
func (s *securityTestCryptoService) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	return &service.VerifyResponse{}, nil
}
func (s *securityTestCryptoService) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	return &service.HMACResponse{}, nil
}
func (s *securityTestCryptoService) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	return &service.VerifyHMACResponse{}, nil
}
func (s *securityTestCryptoService) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	return &service.GenerateRandomResponse{}, nil
}
func (s *securityTestCryptoService) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	return &service.HashResponse{}, nil
}

// Test KMS handlers with success paths
func TestSecurityHandlers_KMS_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, cryptoSvc, createTestLogger())

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"GenerateKey", OpKMSGenerateKey, &service.GenerateKeyRequest{}},
		{"ImportKey", OpKMSImportKey, &service.ImportKeyRequest{}},
		{"GetKey", OpKMSGetKey, &service.GetKeyRequest{}},
		{"ListKeys", OpKMSListKeys, &service.ListKeysRequest{}},
		{"RotateKey", OpKMSRotateKey, &service.RotateKeyRequest{}},
		{"DeleteKey", OpKMSDeleteKey, &service.DeleteKeyRequest{}},
		{"Encrypt", OpKMSEncrypt, &service.EncryptRequest{}},
		{"Decrypt", OpKMSDecrypt, &service.DecryptRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_Success", func(t *testing.T) {
			payload, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("failed to encode: %v", err)
			}

			req := &Request{Opcode: tt.opcode, Payload: payload}
			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK, got %d", resp.Status)
			}
		})
	}
}

// Test Secrets handlers with success paths
func TestSecurityHandlers_Secrets_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"Store", OpSecretsStore, &service.StoreSecretRequest{}},
		{"Get", OpSecretsGet, &service.GetSecretRequest{}},
		{"List", OpSecretsList, &service.ListSecretsRequest{}},
		{"Delete", OpSecretsDelete, &service.DeleteSecretRequest{}},
		{"ListVersions", OpSecretsListVersions, &service.ListSecretVersionsRequest{}},
		{"Rollback", OpSecretsRollback, &service.RollbackSecretRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_Success", func(t *testing.T) {
			payload, _ := EncodePayload(tt.payload)
			req := &Request{Opcode: tt.opcode, Payload: payload}
			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK, got %d", resp.Status)
			}
		})
	}
}

// Test Crypto handlers with success paths
func TestSecurityHandlers_Crypto_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"Encrypt", OpCryptoEncrypt, &service.EncryptRequest{}},
		{"Decrypt", OpCryptoDecrypt, &service.DecryptRequest{}},
		{"Rewrap", OpCryptoRewrap, &service.RewrapRequest{}},
		{"Sign", OpCryptoSign, &service.SignRequest{}},
		{"Verify", OpCryptoVerify, &service.VerifyRequest{}},
		{"Hash", OpCryptoHash, &service.HashRequest{}},
		{"Random", OpCryptoRandom, &service.GenerateRandomRequest{}},
		{"DataKey", OpCryptoDataKey, &service.GenerateDataKeyRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_Success", func(t *testing.T) {
			payload, _ := EncodePayload(tt.payload)
			req := &Request{Opcode: tt.opcode, Payload: payload}
			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK, got %d", resp.Status)
			}
		})
	}
}

// --- Content moved from handler_coverage_test.go ---

// =============================================================================
// Tests for Handler Operations with 0% Coverage
// =============================================================================

// TestHandler_handleList tests the handleList handler
func TestHandler_handleList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			listFn: func(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
				stream <- &service.ListResponse{
					Entities: []*service.Entity{
						{ID: 1, Data: []byte("test-data")},
					},
					TotalCount: 1,
				}
				close(stream)
				return nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.ListRequest{
			EntityType: "user",
			PageSize:   10,
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpList, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.ListRequest{EntityType: "user"})
		req := &Request{Opcode: OpList, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpList, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			listFn: func(ctx context.Context, req *service.ListRequest, stream chan<- *service.ListResponse) error {
				close(stream)
				return errors.New("list failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.ListRequest{EntityType: "user"})
		req := &Request{Opcode: OpList, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleBatchPut tests the handleBatchPut handler
func TestHandler_handleBatchPut(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			batchPutFn: func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
				return &service.BatchPutResponse{
					Responses: make([]*service.PutResponse, len(req.Requests)),
				}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.BatchPutRequest{
			Requests: []*service.PutRequest{{EntityType: "user", ID: 1, Data: []byte("data")}},
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpBatchPut, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.BatchPutRequest{})
		req := &Request{Opcode: OpBatchPut, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpBatchPut, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			batchPutFn: func(ctx context.Context, req *service.BatchPutRequest) (*service.BatchPutResponse, error) {
				return nil, errors.New("batch put failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.BatchPutRequest{})
		req := &Request{Opcode: OpBatchPut, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleBatchGet tests the handleBatchGet handler
func TestHandler_handleBatchGet(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			batchGetFn: func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
				return &service.BatchGetResponse{
					Responses: []*service.GetResponse{{ID: 1, Data: []byte("data"), Found: true}},
				}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.BatchGetRequest{
			Requests: []*service.GetRequest{{EntityType: "user", ID: 1}},
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpBatchGet, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.BatchGetRequest{})
		req := &Request{Opcode: OpBatchGet, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpBatchGet, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			batchGetFn: func(ctx context.Context, req *service.BatchGetRequest) (*service.BatchGetResponse, error) {
				return nil, errors.New("batch get failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.BatchGetRequest{})
		req := &Request{Opcode: OpBatchGet, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleBatchDelete tests the handleBatchDelete handler
func TestHandler_handleBatchDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			batchDeleteFn: func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
				return &service.BatchDeleteResponse{
					Responses: make([]*service.DeleteResponse, len(req.Requests)),
				}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.BatchDeleteRequest{
			Requests: []*service.DeleteRequest{{EntityType: "user", ID: 1}},
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpBatchDelete, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.BatchDeleteRequest{})
		req := &Request{Opcode: OpBatchDelete, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpBatchDelete, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			batchDeleteFn: func(ctx context.Context, req *service.BatchDeleteRequest) (*service.BatchDeleteResponse, error) {
				return nil, errors.New("batch delete failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.BatchDeleteRequest{})
		req := &Request{Opcode: OpBatchDelete, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleTransaction tests the handleTransaction handler
func TestHandler_handleTransaction(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			transactionFn: func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
				return &service.TransactionResponse{Succeeded: true}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.TransactionRequest{
			Success: []*service.TransactionOp{
				{OpType: service.TransactionOpTypePut, EntityType: "user", ID: 1, Data: []byte("data")},
			},
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpTransaction, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.TransactionRequest{})
		req := &Request{Opcode: OpTransaction, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpTransaction, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			transactionFn: func(ctx context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
				return nil, errors.New("transaction failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.TransactionRequest{})
		req := &Request{Opcode: OpTransaction, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleQuery tests the handleQuery handler
func TestHandler_handleQuery(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			queryFn: func(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
				stream <- &service.QueryResponse{
					Entities: []*service.Entity{{ID: 1, Data: []byte("data")}},
				}
				close(stream)
				return nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.QueryRequest{
			EntityType: "user",
			IndexName:  "name_idx",
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpQuery, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.QueryRequest{})
		req := &Request{Opcode: OpQuery, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpQuery, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			queryFn: func(ctx context.Context, req *service.QueryRequest, stream chan<- *service.QueryResponse) error {
				close(stream)
				return errors.New("query failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.QueryRequest{})
		req := &Request{Opcode: OpQuery, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleScanIndex tests the handleScanIndex handler
func TestHandler_handleScanIndex(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			scanIndexFn: func(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
				stream <- &service.ScanResponse{
					Entities: []*service.Entity{{ID: 1, Data: []byte("data")}},
				}
				close(stream)
				return nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.ScanRequest{
			EntityType: "user",
			IndexName:  "name_idx",
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpScanIndex, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.ScanRequest{})
		req := &Request{Opcode: OpScanIndex, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpScanIndex, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			scanIndexFn: func(ctx context.Context, req *service.ScanRequest, stream chan<- *service.ScanResponse) error {
				close(stream)
				return errors.New("scan failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.ScanRequest{})
		req := &Request{Opcode: OpScanIndex, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleClusterStatus tests the handleClusterStatus handler
func TestHandler_handleClusterStatus(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
				return &service.ClusterStatusResponse{
					ClusterID: "test-cluster",
					Nodes:     []*service.NodeInfo{{NodeID: 1, Address: "localhost:9000"}},
				}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.ClusterStatusRequest{})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpClusterStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.ClusterStatusRequest{})
		req := &Request{Opcode: OpClusterStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpClusterStatus, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
				return nil, errors.New("cluster status failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.ClusterStatusRequest{})
		req := &Request{Opcode: OpClusterStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleGetLeader tests the handleGetLeader handler
func TestHandler_handleGetLeader(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			getLeaderFn: func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
				return &service.GetLeaderResponse{
					LeaderID: 1,
					Address:  "localhost:9000",
				}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.GetLeaderRequest{ShardID: 1})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpGetLeader, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.GetLeaderRequest{})
		req := &Request{Opcode: OpGetLeader, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpGetLeader, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			getLeaderFn: func(ctx context.Context, req *service.GetLeaderRequest) (*service.GetLeaderResponse, error) {
				return nil, errors.New("get leader failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.GetLeaderRequest{})
		req := &Request{Opcode: OpGetLeader, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleAddNode tests the handleAddNode handler
func TestHandler_handleAddNode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			addNodeFn: func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
				return &service.AddNodeResponse{Success: true}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.AddNodeRequest{
			NodeID:  2,
			Address: "localhost:9001",
			ShardID: 1,
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpAddNode, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.AddNodeRequest{})
		req := &Request{Opcode: OpAddNode, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpAddNode, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			addNodeFn: func(ctx context.Context, req *service.AddNodeRequest) (*service.AddNodeResponse, error) {
				return nil, errors.New("add node failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.AddNodeRequest{})
		req := &Request{Opcode: OpAddNode, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleRemoveNode tests the handleRemoveNode handler
func TestHandler_handleRemoveNode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			removeNodeFn: func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
				return &service.RemoveNodeResponse{Success: true}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.RemoveNodeRequest{NodeID: 2})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpRemoveNode, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.RemoveNodeRequest{})
		req := &Request{Opcode: OpRemoveNode, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpRemoveNode, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			removeNodeFn: func(ctx context.Context, req *service.RemoveNodeRequest) (*service.RemoveNodeResponse, error) {
				return nil, errors.New("remove node failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.RemoveNodeRequest{})
		req := &Request{Opcode: OpRemoveNode, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// TestHandler_handleCreateShard tests the handleCreateShard handler
func TestHandler_handleCreateShard(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		svc := &mockService{
			createShardFn: func(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
				return &service.CreateShardResponse{ShardID: 2, Status: "created"}, nil
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())

		payload, err := EncodePayload(&service.CreateShardRequest{
			ShardID: 2,
		})
		if err != nil {
			t.Fatalf("Failed to encode payload: %v", err)
		}

		req := &Request{Opcode: OpCreateShard, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status != StatusOK {
			t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
		}
	})

	t.Run("nil_service", func(t *testing.T) {
		handler := NewHandler(nil, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.CreateShardRequest{})
		req := &Request{Opcode: OpCreateShard, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for nil service")
		}
	})

	t.Run("invalid_payload", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
		req := &Request{Opcode: OpCreateShard, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for invalid payload")
		}
	})

	t.Run("service_error", func(t *testing.T) {
		svc := &mockService{
			createShardFn: func(ctx context.Context, req *service.CreateShardRequest) (*service.CreateShardResponse, error) {
				return nil, errors.New("create shard failed")
			},
		}
		handler := NewHandler(svc, nil, nil, nil, createTestLogger())
		payload, _ := EncodePayload(&service.CreateShardRequest{})
		req := &Request{Opcode: OpCreateShard, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)
		if resp.Status == StatusOK {
			t.Error("Expected error status for service error")
		}
	})
}

// --- Content moved from handler_coverage_91_test.go ---

// =============================================================================
// HandleRequest Dispatch - Uncovered Opcode Paths
// These tests exercise HandleRequest switch-case branches that are
// exercised directly in other tests but not through the HandleRequest
// dispatch path, leaving the dispatch lines uncovered.
// =============================================================================

// TestHandleRequest_DefaultUnknownOpcode verifies the default case in
// HandleRequest for opcodes not covered by any switch case.
// NOTE: 0xFF is handled by the IAM catch-all case (0xFD-0xFF), so
// we use 0xFC which is truly unhandled.
func TestHandleRequest_DefaultUnknownOpcode(t *testing.T) {
	handler := createTestHandler()
	req := &Request{Opcode: 0xFC, Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("Expected error status for truly unknown opcode 0xFC")
	}
}

// TestHandleRequest_OpOSAdminDispatch exercises the OpOSAdmin dispatch
// path in HandleRequest. The handler has no objectStorageAdminService,
// so all sub-operations will return service unavailable errors.
func TestHandleRequest_OpOSAdminDispatch(t *testing.T) {
	handler := createTestHandler()
	// OpOSAdmin requires a sub-opcode in payload[0]
	req := &Request{Opcode: OpOSAdmin, Payload: []byte{0x01, '{', '}'}}
	resp := handler.HandleRequest(context.Background(), req)
	// Should fail because no OS admin service is configured
	if resp.Status == StatusOK {
		t.Error("Expected error when OS admin service is not available")
	}
}

// TestHandleRequest_OpFetchObjectDispatch exercises the OpFetchObject
// dispatch path in HandleRequest.
func TestHandleRequest_OpFetchObjectDispatch(t *testing.T) {
	handler := createTestHandler()
	// No object transfer registry configured
	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "ref-1",
	})
	req := &Request{Opcode: OpFetchObject, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	// Should fail because no object transfer registry is configured
	if resp.Status == StatusOK {
		t.Error("Expected error when object transfer registry is nil")
	}
}

// TestHandleRequest_OpStreamFetchObjectsDispatch exercises the
// OpStreamFetchObjects dispatch path in HandleRequest.
func TestHandleRequest_OpStreamFetchObjectsDispatch(t *testing.T) {
	handler := createTestHandler()
	payload, _ := EncodeStreamFetchObjectsRequest(&StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "ref-1"}},
	})
	req := &Request{Opcode: OpStreamFetchObjects, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	// Should fail because no object transfer registry is configured
	if resp.Status == StatusOK {
		t.Error("Expected error when object transfer registry is nil")
	}
}

// TestHandleRequest_OpFetchObject_Success exercises the full dispatch
// path for a successful object fetch through HandleRequest.
func TestHandleRequest_OpFetchObject_Success(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return []byte("data"), nil
		},
	}
	registry.RegisterStore(1, store)

	handler := createTestHandler()
	handler.objectTransferRegistry = registry

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "test-ref",
	})
	req := &Request{Opcode: OpFetchObject, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("Expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// TestHandleRequest_OpStreamFetchObjects_Success exercises the full
// dispatch path for a successful stream fetch through HandleRequest.
func TestHandleRequest_OpStreamFetchObjects_Success(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return []byte("data-" + objectRef), nil
		},
	}
	registry.RegisterStore(1, store)

	handler := createTestHandler()
	handler.objectTransferRegistry = registry

	payload, _ := EncodeStreamFetchObjectsRequest(&StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []ObjectFetchItem{
			{ObjectRef: "ref-1"},
			{ObjectRef: "ref-2"},
		},
	})
	req := &Request{Opcode: OpStreamFetchObjects, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("Expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// handleFetchObject - Additional Branch Coverage
// =============================================================================

// TestHandleFetchObject_ChecksumMatch_ViaDispatch tests the checksum
// verification success path through HandleRequest dispatch.
func TestHandleFetchObject_ChecksumMatch_ViaDispatch(t *testing.T) {
	data := []byte("verified-data")
	hash := sha256.Sum256(data)

	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return data, nil
		},
	}
	registry.RegisterStore(1, store)

	handler := createTestHandler()
	handler.objectTransferRegistry = registry

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "checksum-ref",
		Checksum:  hash[:],
		RequestID: "req-123",
	})
	req := &Request{Opcode: OpFetchObject, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("Expected StatusOK, got %d", resp.Status)
	}
}

// =============================================================================
// handleVectorExtendedDispatch - Uncovered Sub-Opcode Paths
// =============================================================================

// TestHandleVectorExtendedDispatch_UnknownSubOpcode tests the unknown
// sub-opcode error path in the switch default case.
func TestHandleVectorExtendedDispatch_UnknownSubOpcode(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleVectorExtendedDispatch(context.Background(), &Request{
		Opcode:  OpScheduler,
		Payload: []byte{0xEE}, // Unknown sub-opcode
	})
	if resp.Status == StatusOK {
		t.Error("Expected error for unknown sub-opcode")
	}
}

// =============================================================================
// Barrier Dispatcher - Success Paths Via HandleRequest
// =============================================================================

// successBarrierService returns minimal success responses for every
// BarrierServicer method. Used to exercise the HandleRequest dispatch path
// for barrier sub-operations.
type successBarrierService struct{}

func (s *successBarrierService) BarrierStatus(ctx context.Context) (*service.BarrierStatusInfo, error) {
	return &service.BarrierStatusInfo{Sealed: false, Strategy: "test"}, nil
}
func (s *successBarrierService) BarrierInitialize(ctx context.Context, secret string) error {
	return nil
}
func (s *successBarrierService) BarrierUnseal(ctx context.Context, secret string) error { return nil }
func (s *successBarrierService) BarrierSeal(ctx context.Context) error                  { return nil }
func (s *successBarrierService) BarrierInitializeShamir(ctx context.Context, secret string, threshold, total int) (*service.ShamirInitInfo, error) {
	return &service.ShamirInitInfo{Shares: []string{"a"}, Threshold: threshold, TotalShares: total}, nil
}
func (s *successBarrierService) BarrierUnsealWithShare(ctx context.Context, share string) (*service.UnsealShareProgress, error) {
	return &service.UnsealShareProgress{}, nil
}
func (s *successBarrierService) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	return nil
}
func (s *successBarrierService) BarrierRekey(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
	return &service.ShamirInitInfo{Shares: []string{"a"}, Threshold: threshold, TotalShares: total}, nil
}
func (s *successBarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
	return &service.RecoveryKeysInfo{Keys: []string{"k"}, Threshold: threshold, Total: total}, nil
}
func (s *successBarrierService) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	return nil
}
func (s *successBarrierService) BarrierDeleteRecoveryKeys(ctx context.Context) error { return nil }
func (s *successBarrierService) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	return false, nil
}
func (s *successBarrierService) BarrierGenerateRootToken(ctx context.Context, shares []string) (*service.RootTokenInfo, error) {
	return &service.RootTokenInfo{Token: "tok"}, nil
}

// TestHandleBarrierStatus_Success_ViaDispatch verifies the barrier status
// success path through HandleRequest with a wired BarrierServicer.
func TestHandleBarrierStatus_Success_ViaDispatch(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithBarrierService(&successBarrierService{}))
	req := &Request{Opcode: OpBarrier, Payload: []byte{SubOpBarrierStatus}}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("Expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// Cluster Management - Additional Coverage
// =============================================================================

// TestHandleListNodes_NilService verifies the nil service error path.
func TestHandleListNodes_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())
	payload, _ := EncodePayload(&service.ClusterStatusRequest{})
	resp := handler.handleListNodes(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("Expected error for nil service")
	}
}

// TestHandleListNodes_DecodeError verifies the decode error path.
func TestHandleListNodes_DecodeError(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleListNodes(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("Expected error for invalid payload")
	}
}

// TestHandleRebalanceStatus_NoOperationID verifies the empty operation
// ID path returns default not_running status.
func TestHandleRebalanceStatus_NoOperationID(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: []byte("{}")})
	if resp.Status != StatusOK {
		t.Errorf("Expected StatusOK, got %d", resp.Status)
	}
}

// TestHandleCancelMigration_DecodeError verifies decode error handling.
func TestHandleCancelMigration_DecodeError(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleCancelMigration(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("Expected error for invalid payload")
	}
}

// TestHandleRebalancePlan_DecodeError verifies decode error handling.
func TestHandleRebalancePlan_DecodeError(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleRebalancePlan(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("Expected error for invalid payload")
	}
}

// TestHandleNodeReplaceRequest_DecodeError verifies decode error path.
func TestHandleNodeReplaceRequest_DecodeError(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleNodeReplaceRequest(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("Expected error for invalid payload")
	}
}

// =============================================================================
// DecodeFetchObjectRequest / DecodeStreamFetchObjectsRequest - Edge Cases
// =============================================================================

// TestDecodeFetchObjectRequest_MinimalValid verifies a minimal valid
// request with zero-length optional fields decodes correctly.
func TestDecodeFetchObjectRequest_MinimalValid(t *testing.T) {
	buf := make([]byte, 14)
	binary.BigEndian.PutUint64(buf[0:], 42) // ShardID
	binary.BigEndian.PutUint16(buf[8:], 0)  // refLen = 0
	binary.BigEndian.PutUint16(buf[10:], 0) // checksumLen = 0
	binary.BigEndian.PutUint16(buf[12:], 0) // reqIDLen = 0

	req, err := DecodeFetchObjectRequest(buf)
	if err != nil {
		t.Fatalf("expected success for minimal valid request, got: %v", err)
	}
	if req.ShardID != 42 {
		t.Errorf("expected ShardID=42, got %d", req.ShardID)
	}
	if req.ObjectRef != "" {
		t.Errorf("expected empty ObjectRef, got %q", req.ObjectRef)
	}
}

// TestDecodeStreamFetchObjectsRequest_ZeroItems verifies decoding
// with zero items.
func TestDecodeStreamFetchObjectsRequest_ZeroItems(t *testing.T) {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[0:], 5) // ShardID = 5
	binary.BigEndian.PutUint32(buf[8:], 0) // itemCount = 0

	req, err := DecodeStreamFetchObjectsRequest(buf)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if req.ShardID != 5 {
		t.Errorf("expected ShardID=5, got %d", req.ShardID)
	}
	if len(req.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(req.Items))
	}
}

// =============================================================================
// Messaging - Additional Coverage
// =============================================================================

// TestMsgDedupStats_DecodeError_ViaDispatch tests dedup stats decode
// error through the dispatch path.
func TestMsgDedupStats_DecodeError_ViaDispatch(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()

	fullPayload := append([]byte{SubOpMsgDedupStats}, []byte("{bad")...)
	resp := h.handleMsgDispatch(context.Background(), &Request{
		Opcode:  OpMsg,
		Payload: fullPayload,
	})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload through dispatch")
	}
}

// TestMsgCreateChannel_ServiceError tests the error path when
// CreateChannel fails.
func TestMsgCreateChannel_ServiceError(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()

	// Creating a channel with empty name should fail
	payload, _ := EncodePayload(&MsgCreateChannelRequest{Name: ""})
	_, err := h.handleMsgCreateChannel(context.Background(), payload)
	if err == nil {
		t.Fatal("Expected error for empty channel name")
	}
}

// TestMsgListChannels_ViaDispatch_Success tests the ListChannels
// success path through the dispatcher.
func TestMsgListChannels_ViaDispatch_Success(t *testing.T) {
	h, cleanup := createHandlerWithStartedMsgService(t)
	defer cleanup()

	statsPayload := []byte("{}")
	fullPayload := append([]byte{SubOpMsgListChannels}, statsPayload...)
	resp := h.handleMsgDispatch(context.Background(), &Request{
		Opcode:  OpMsg,
		Payload: fullPayload,
	})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// =============================================================================
// Server - NewServer Edge Cases
// =============================================================================

// TestNewServer_NilTLS verifies that NewServer returns an error
// when TLS config is nil.
func TestNewServer_NilTLS(t *testing.T) {
	_, err := NewServer(&mockService{}, &Config{}, nil)
	if err == nil {
		t.Fatal("Expected error for nil TLS config")
	}
}

// TestNewServer_NilLogger verifies that NewServer creates a default
// logger when nil is passed.
func TestNewServer_NilLogger(t *testing.T) {
	tlsCfg := generateTestTLS(t)
	s, err := NewServer(&mockService{}, &Config{TLSConfig: tlsCfg}, nil)
	if err != nil {
		t.Fatalf("Expected success with nil logger, got: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non-nil server")
	}
}

// TestNewServer_WithOptions verifies functional options are applied.
func TestNewServer_WithOptions(t *testing.T) {
	tlsCfg := generateTestTLS(t)
	s, err := NewServer(&mockService{}, &Config{TLSConfig: tlsCfg}, createTestLogger(),
		WithAuthorizationService(nil),
		WithAuthManager(nil),
	)
	if err != nil {
		t.Fatalf("Expected success, got: %v", err)
	}
	if s == nil {
		t.Fatal("Expected non-nil server")
	}
}

// TestServerErrorTypes_Messages verifies the error type string methods.
func TestServerErrorTypes_Messages(t *testing.T) {
	t.Run("AlreadyRunning", func(t *testing.T) {
		err := &ServerAlreadyRunningError{}
		if err.Error() != "server is already running" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})

	t.Run("NotRunning", func(t *testing.T) {
		err := &ServerNotRunningError{}
		if err.Error() != "server is not running" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
	})
}

// TestServer_StopNotRunning verifies that stopping a non-running
// server returns an error.
func TestServer_StopNotRunning(t *testing.T) {
	tlsCfg := generateTestTLS(t)
	s, err := NewServer(&mockService{}, &Config{TLSConfig: tlsCfg}, createTestLogger())
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	err = s.Stop()
	if err == nil {
		t.Fatal("Expected error when stopping non-running server")
	}
}

// TestServer_GetMetrics verifies metrics are returned correctly.
func TestServer_GetMetrics(t *testing.T) {
	tlsCfg := generateTestTLS(t)
	s, err := NewServer(&mockService{}, &Config{TLSConfig: tlsCfg}, createTestLogger())
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	metrics := s.GetMetrics()
	if metrics == nil {
		t.Fatal("Expected non-nil metrics")
	}
	if metrics["running"] != false {
		t.Error("Expected running=false before start")
	}
}

// TestServer_IsRunning verifies IsRunning returns correct state.
func TestServer_IsRunning(t *testing.T) {
	tlsCfg := generateTestTLS(t)
	s, err := NewServer(&mockService{}, &Config{TLSConfig: tlsCfg}, createTestLogger())
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}
	if s.IsRunning() {
		t.Error("Expected IsRunning=false before start")
	}
}

// =============================================================================
// Scheduler Service - getSchedulerService Coverage
// =============================================================================

// TestGetSchedulerService_ReturnsNilScheduler verifies that
// getSchedulerService returns nil when the underlying service
// returns a nil scheduler.
func TestGetSchedulerService_ReturnsNilScheduler(t *testing.T) {
	// mockService implements schedulerProviderInterface (returns nil)
	handler := createTestHandler()
	svc := handler.getSchedulerService()
	if svc != nil {
		t.Error("Expected nil scheduler service from mockService")
	}
}

// --- Content moved from coverage_90_test.go ---

// =============================================================================
// Server Functional Options Tests - Target 0% coverage functions
// =============================================================================

func TestWithAuthorizationService_Coverage(t *testing.T) {
	t.Parallel()
	server := &Server{}

	// Just test that WithAuthorizationService returns a valid option
	opt := WithAuthorizationService(nil)
	opt(server)

	if server.authzService != nil {
		t.Error("WithAuthorizationService set value for nil input")
	}
}

func TestWithAuthManager_Coverage(t *testing.T) {
	t.Parallel()
	server := &Server{}

	// Just test that WithAuthManager returns a valid option
	opt := WithAuthManager(nil)
	opt(server)

	if server.authManager != nil {
		t.Error("WithAuthManager set value for nil input")
	}
}

// =============================================================================
// Barrier Handler Tests - Service Error Paths
// =============================================================================

type coverageErrorBarrierService struct{}

var errCoverageBarrierError = errors.New("barrier service error")

func (s *coverageErrorBarrierService) BarrierStatus(ctx context.Context) (*service.BarrierStatusInfo, error) {
	return nil, errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierInitialize(ctx context.Context, secret string) error {
	return errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierUnseal(ctx context.Context, secret string) error {
	return errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierSeal(ctx context.Context) error {
	return errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierInitializeShamir(ctx context.Context, secret string, threshold, total int) (*service.ShamirInitInfo, error) {
	return nil, errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierUnsealWithShare(ctx context.Context, share string) (*service.UnsealShareProgress, error) {
	return nil, errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierUnsealWithShares(ctx context.Context, shares []string) error {
	return errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierRekey(ctx context.Context, threshold, total int) (*service.ShamirInitInfo, error) {
	return nil, errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierGenerateRecoveryKeys(ctx context.Context, threshold, total int) (*service.RecoveryKeysInfo, error) {
	return nil, errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierRecoverWithKeys(ctx context.Context, keys []string) error {
	return errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	return errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierHasRecoveryKeys(ctx context.Context) (bool, error) {
	return false, errCoverageBarrierError
}
func (s *coverageErrorBarrierService) BarrierGenerateRootToken(ctx context.Context, shares []string) (*service.RootTokenInfo, error) {
	return nil, errCoverageBarrierError
}

func TestHandleBarrierStatus_ServiceError_Coverage(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithBarrierService(&coverageErrorBarrierService{}))
	req := &Request{Opcode: OpBarrier, Payload: []byte{SubOpBarrierStatus}}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from barrier service")
	}
}

func TestHandleBarrierInitialize_ServiceError_Coverage(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithBarrierService(&coverageErrorBarrierService{}))
	body, _ := EncodePayload(&barrierInitializeWireRequest{Secret: "pw"})
	req := &Request{Opcode: OpBarrier, Payload: append([]byte{SubOpBarrierInitialize}, body...)}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from barrier service")
	}
}

func TestHandleBarrierUnseal_ServiceError_Coverage(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithBarrierService(&coverageErrorBarrierService{}))
	body, _ := EncodePayload(&barrierUnsealWireRequest{Secret: "pw"})
	req := &Request{Opcode: OpBarrier, Payload: append([]byte{SubOpBarrierUnseal}, body...)}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from barrier service")
	}
}

func TestHandleBarrierSeal_ServiceError_Coverage(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithBarrierService(&coverageErrorBarrierService{}))
	req := &Request{Opcode: OpBarrier, Payload: []byte{SubOpBarrierSeal}}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from barrier service")
	}
}

func TestHandleBarrierRekey_ServiceError_Coverage(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(),
		WithBarrierService(&coverageErrorBarrierService{}))
	body, _ := EncodePayload(&barrierRekeyWireRequest{Threshold: 1, Total: 1})
	req := &Request{Opcode: OpBarrier, Payload: append([]byte{SubOpBarrierRekey}, body...)}
	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from barrier service")
	}
}

// =============================================================================
// KMS Backend Tests - Target 50% coverage functions
// =============================================================================

func TestHandleKMSBackendStatus_Success_Coverage(t *testing.T) {
	t.Parallel()
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{Opcode: OpKMSBackendStatus, Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSBackendStatus_NilService_Coverage(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	req := &Request{Opcode: OpKMSBackendStatus, Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSBackendStatus_ServiceError_Coverage(t *testing.T) {
	t.Parallel()
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{Opcode: OpKMSBackendStatus, Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSBackendTest_Success_Coverage(t *testing.T) {
	t.Parallel()
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{Opcode: OpKMSBackendTest, Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSBackendTest_NilService_Coverage(t *testing.T) {
	t.Parallel()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	req := &Request{Opcode: OpKMSBackendTest, Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSBackendTest_ServiceError_Coverage(t *testing.T) {
	t.Parallel()
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{Opcode: OpKMSBackendTest, Payload: nil}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Additional KMS Handler Tests
// =============================================================================

func TestHandleKMSEncrypt_Success_Coverage(t *testing.T) {
	t.Parallel()
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.EncryptRequest{
		KeyID:     "test-key",
		Plaintext: []byte("test data"),
	})
	req := &Request{Opcode: OpKMSEncrypt, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSDecrypt_Success_Coverage(t *testing.T) {
	t.Parallel()
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.DecryptRequest{
		KeyID:      "test-key",
		Ciphertext: []byte("encrypted data"),
	})
	req := &Request{Opcode: OpKMSDecrypt, Payload: payload}
	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSListKeyVersions_AllPaths(t *testing.T) {
	t.Parallel()

	// Success path
	t.Run("Success", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.ListKeyVersionsRequest{KeyID: "test-key"})
		req := &Request{Opcode: OpKMSKeyVersions, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	// Nil service path
	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.ListKeyVersionsRequest{})
		req := &Request{Opcode: OpKMSKeyVersions, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil KMS service")
		}
	})

	// Decode error path
	t.Run("DecodeError", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		req := &Request{Opcode: OpKMSKeyVersions, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	// Service error path
	t.Run("ServiceError", func(t *testing.T) {
		kmsSvc := &errorKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.ListKeyVersionsRequest{})
		req := &Request{Opcode: OpKMSKeyVersions, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleKMSUpdateKeyStatus_AllPaths(t *testing.T) {
	t.Parallel()

	// Success path
	t.Run("Success", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UpdateKeyStatusRequest{KeyID: "test-key", Status: "active"})
		req := &Request{Opcode: OpKMSKeyStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	// Nil service path
	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UpdateKeyStatusRequest{})
		req := &Request{Opcode: OpKMSKeyStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil KMS service")
		}
	})

	// Decode error path
	t.Run("DecodeError", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		req := &Request{Opcode: OpKMSKeyStatus, Payload: []byte("bad")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	// Service error path
	t.Run("ServiceError", func(t *testing.T) {
		kmsSvc := &errorKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UpdateKeyStatusRequest{})
		req := &Request{Opcode: OpKMSKeyStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleCryptoDataKey_AllPaths(t *testing.T) {
	t.Parallel()

	// Success path
	t.Run("Success", func(t *testing.T) {
		cryptoSvc := &securityTestCryptoService{}
		handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

		payload, _ := EncodePayload(&service.GenerateDataKeyRequest{KeyID: "master-key"})
		req := &Request{Opcode: OpCryptoDataKey, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	// Nil service path
	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.GenerateDataKeyRequest{})
		req := &Request{Opcode: OpCryptoDataKey, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil crypto service")
		}
	})

	// Decode error path
	t.Run("DecodeError", func(t *testing.T) {
		cryptoSvc := &securityTestCryptoService{}
		handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

		req := &Request{Opcode: OpCryptoDataKey, Payload: []byte("invalid json")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	// Service error path
	t.Run("ServiceError", func(t *testing.T) {
		cryptoSvc := &errorCryptoService{}
		handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

		payload, _ := EncodePayload(&service.GenerateDataKeyRequest{})
		req := &Request{Opcode: OpCryptoDataKey, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

// =============================================================================
// Secrets Handler Additional Tests
// =============================================================================

func TestHandleSecretsRotate_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		secretsSvc := &securityTestSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.RotateSecretRequest{SecretID: "secret/test"})
		req := &Request{Opcode: OpSecretsRotate, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		secretsSvc := &errorSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.RotateSecretRequest{})
		req := &Request{Opcode: OpSecretsRotate, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleSecretsGetVersion_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		secretsSvc := &securityTestSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.GetSecretVersionRequest{SecretID: "secret/test", Version: 1})
		req := &Request{Opcode: OpSecretsGetVersion, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		secretsSvc := &errorSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.GetSecretVersionRequest{})
		req := &Request{Opcode: OpSecretsGetVersion, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleSecretsSetRotation_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		secretsSvc := &securityTestSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SetRotationPolicyRequest{
			SecretID: "secret/test",
		})
		req := &Request{Opcode: OpSecretsSetRotation, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		secretsSvc := &errorSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SetRotationPolicyRequest{})
		req := &Request{Opcode: OpSecretsSetRotation, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleSecretsGetRotation_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		secretsSvc := &securityTestSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.GetRotationPolicyRequest{SecretID: "secret/test"})
		req := &Request{Opcode: OpSecretsGetRotation, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		secretsSvc := &errorSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.GetRotationPolicyRequest{})
		req := &Request{Opcode: OpSecretsGetRotation, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleSecretsUpdateStatus_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		secretsSvc := &securityTestSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UpdateSecretStatusRequest{SecretID: "secret/test", Status: "active"})
		req := &Request{Opcode: OpSecretsUpdateStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		secretsSvc := &errorSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UpdateSecretStatusRequest{})
		req := &Request{Opcode: OpSecretsUpdateStatus, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleSecretsGetByPrefix_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		secretsSvc := &securityTestSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.GetSecretsByPrefixRequest{Prefix: "secret/"})
		req := &Request{Opcode: OpSecretsGetByPrefix, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		secretsSvc := &errorSecretsService{}
		handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

		payload, _ := EncodePayload(&service.GetSecretsByPrefixRequest{})
		req := &Request{Opcode: OpSecretsGetByPrefix, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

// =============================================================================
// Client Options Tests - Improve NewClient coverage
// =============================================================================

func TestClientOptions_Coverage(t *testing.T) {
	t.Parallel()

	// Test all client options without actually connecting
	cfg := &clientConfig{}

	// Test WithTLSConfig
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
	WithTLSConfig(tlsCfg)(cfg)
	if cfg.tlsConfig != tlsCfg {
		t.Error("WithTLSConfig did not set tls config")
	}

	// Test WithTimeout
	WithTimeout(5 * time.Second)(cfg)
	if cfg.timeout != 5*time.Second {
		t.Error("WithTimeout did not set timeout")
	}

	// Test WithMaxIdleTimeout
	WithMaxIdleTimeout(time.Minute)(cfg)
	if cfg.maxIdleTimeout != time.Minute {
		t.Error("WithMaxIdleTimeout did not set max idle timeout")
	}

	// Test WithLogger
	log := createTestLogger()
	WithLogger(log)(cfg)
	if cfg.logger != log {
		t.Error("WithLogger did not set logger")
	}
}

// =============================================================================
// KMS Safety Operations Tests - Target remaining coverage
// =============================================================================

func TestHandleKMSUnwrapKey_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UnwrapKeyRequest{KeyID: "wrapper-key", WrappedKey: []byte("wrapped")})
		req := &Request{Opcode: OpKMSUnwrapKey, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UnwrapKeyRequest{})
		req := &Request{Opcode: OpKMSUnwrapKey, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil KMS service")
		}
	})

	t.Run("DecodeError", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		req := &Request{Opcode: OpKMSUnwrapKey, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		kmsSvc := &errorKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.UnwrapKeyRequest{})
		req := &Request{Opcode: OpKMSUnwrapKey, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleKMSSetKeySafety_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SetKeySafetyRequest{KeyID: "test-key"})
		req := &Request{Opcode: OpKMSSetKeySafety, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SetKeySafetyRequest{})
		req := &Request{Opcode: OpKMSSetKeySafety, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil KMS service")
		}
	})

	t.Run("DecodeError", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		req := &Request{Opcode: OpKMSSetKeySafety, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		kmsSvc := &errorKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SetKeySafetyRequest{})
		req := &Request{Opcode: OpKMSSetKeySafety, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleKMSClearKeySafety_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.ClearKeySafetyRequest{KeyID: "test-key"})
		req := &Request{Opcode: OpKMSClearKeySafety, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.ClearKeySafetyRequest{})
		req := &Request{Opcode: OpKMSClearKeySafety, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil KMS service")
		}
	})

	t.Run("DecodeError", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		req := &Request{Opcode: OpKMSClearKeySafety, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		kmsSvc := &errorKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.ClearKeySafetyRequest{})
		req := &Request{Opcode: OpKMSClearKeySafety, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleKMSSafetyCheck_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SafetyCheckRequest{ThresholdPercent: 80})
		req := &Request{Opcode: OpKMSSafetyCheck, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SafetyCheckRequest{})
		req := &Request{Opcode: OpKMSSafetyCheck, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil KMS service")
		}
	})

	t.Run("DecodeError", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		req := &Request{Opcode: OpKMSSafetyCheck, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		kmsSvc := &errorKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SafetyCheckRequest{})
		req := &Request{Opcode: OpKMSSafetyCheck, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

func TestHandleKMSSafetyReport_AllPaths(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SafetyReportRequest{})
		req := &Request{Opcode: OpKMSSafetyReport, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status != StatusOK {
			t.Errorf("expected StatusOK, got %d", resp.Status)
		}
	})

	t.Run("NilService", func(t *testing.T) {
		handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SafetyReportRequest{})
		req := &Request{Opcode: OpKMSSafetyReport, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with nil KMS service")
		}
	})

	t.Run("DecodeError", func(t *testing.T) {
		kmsSvc := &securityTestKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		req := &Request{Opcode: OpKMSSafetyReport, Payload: []byte("invalid")}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error with invalid payload")
		}
	})

	t.Run("ServiceError", func(t *testing.T) {
		kmsSvc := &errorKMSService{}
		handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

		payload, _ := EncodePayload(&service.SafetyReportRequest{})
		req := &Request{Opcode: OpKMSSafetyReport, Payload: payload}
		resp := handler.HandleRequest(context.Background(), req)

		if resp.Status == StatusOK {
			t.Error("expected error from service")
		}
	})
}

// --- Content moved from handler_coverage_boost_test.go ---

// =============================================================================
// KMS Handler Coverage Tests - handleKMSGenerateKey, ImportKey, GetKey, etc.
// Targets 66.7% coverage functions to reach success path
// =============================================================================

func TestHandleKMSGenerateKey_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GenerateKeyRequest{
		Name: "test-key",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpKMSGenerateKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d, payload: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleKMSGenerateKey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GenerateKeyRequest{})
	req := &Request{
		Opcode:  OpKMSGenerateKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSGenerateKey_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpKMSGenerateKey,
		Payload: []byte("invalid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSGenerateKey_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GenerateKeyRequest{})
	req := &Request{
		Opcode:  OpKMSGenerateKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSImportKey_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ImportKeyRequest{
		Name:        "test-key",
		KeyMaterial: []byte("key-material"),
	})

	req := &Request{
		Opcode:  OpKMSImportKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSImportKey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ImportKeyRequest{})
	req := &Request{
		Opcode:  OpKMSImportKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSImportKey_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpKMSImportKey,
		Payload: []byte("not json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSImportKey_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ImportKeyRequest{})
	req := &Request{
		Opcode:  OpKMSImportKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSGetKey_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetKeyRequest{
		KeyID: "test-key",
	})

	req := &Request{
		Opcode:  OpKMSGetKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSGetKey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetKeyRequest{})
	req := &Request{
		Opcode:  OpKMSGetKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSGetKey_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpKMSGetKey,
		Payload: []byte("bad payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSGetKey_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetKeyRequest{})
	req := &Request{
		Opcode:  OpKMSGetKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSListKeys_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListKeysRequest{})

	req := &Request{
		Opcode:  OpKMSListKeys,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSListKeys_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListKeysRequest{})
	req := &Request{
		Opcode:  OpKMSListKeys,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSListKeys_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpKMSListKeys,
		Payload: []byte("garbage"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSListKeys_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListKeysRequest{})
	req := &Request{
		Opcode:  OpKMSListKeys,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSRotateKey_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RotateKeyRequest{
		KeyID: "test-key",
	})

	req := &Request{
		Opcode:  OpKMSRotateKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSRotateKey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RotateKeyRequest{})
	req := &Request{
		Opcode:  OpKMSRotateKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSRotateKey_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpKMSRotateKey,
		Payload: []byte("invalid"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSRotateKey_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RotateKeyRequest{})
	req := &Request{
		Opcode:  OpKMSRotateKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleKMSDeleteKey_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteKeyRequest{
		KeyID: "test-key",
	})

	req := &Request{
		Opcode:  OpKMSDeleteKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSDeleteKey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteKeyRequest{})
	req := &Request{
		Opcode:  OpKMSDeleteKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSDeleteKey_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpKMSDeleteKey,
		Payload: []byte("not json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSDeleteKey_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteKeyRequest{})
	req := &Request{
		Opcode:  OpKMSDeleteKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Crypto Handler Coverage Tests - All handlers at 75%
// Note: NewHandler signature is (svc, kms, secrets, seal, crypto, logger, ...opts)
// =============================================================================

func TestHandleCryptoEncrypt_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.EncryptRequest{
		Plaintext: []byte("test data"),
		KeyID:     "test-key",
	})

	req := &Request{
		Opcode:  OpCryptoEncrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoEncrypt_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.EncryptRequest{})
	req := &Request{
		Opcode:  OpCryptoEncrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleCryptoDecrypt_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.DecryptRequest{
		Ciphertext: []byte("encrypted data"),
		KeyID:      "test-key",
	})

	req := &Request{
		Opcode:  OpCryptoDecrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoDecrypt_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.DecryptRequest{})
	req := &Request{
		Opcode:  OpCryptoDecrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleCryptoRewrap_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.RewrapRequest{
		KeyID:      "key-id",
		Ciphertext: []byte("encrypted"),
	})

	req := &Request{
		Opcode:  OpCryptoRewrap,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoRewrap_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.RewrapRequest{})
	req := &Request{
		Opcode:  OpCryptoRewrap,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleCryptoSign_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.SignRequest{
		Message: []byte("data to sign"),
		KeyID:   "signing-key",
	})

	req := &Request{
		Opcode:  OpCryptoSign,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoSign_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.SignRequest{})
	req := &Request{
		Opcode:  OpCryptoSign,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleCryptoVerify_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.VerifyRequest{
		Message:   []byte("data"),
		Signature: []byte("signature"),
		KeyID:     "signing-key",
	})

	req := &Request{
		Opcode:  OpCryptoVerify,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoVerify_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.VerifyRequest{})
	req := &Request{
		Opcode:  OpCryptoVerify,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleCryptoHash_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.HashRequest{
		Data:      []byte("data to hash"),
		Algorithm: "SHA256",
	})

	req := &Request{
		Opcode:  OpCryptoHash,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoHash_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.HashRequest{})
	req := &Request{
		Opcode:  OpCryptoHash,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleCryptoRandom_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.GenerateRandomRequest{
		NumBytes: 32,
	})

	req := &Request{
		Opcode:  OpCryptoRandom,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoRandom_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.GenerateRandomRequest{})
	req := &Request{
		Opcode:  OpCryptoRandom,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleCryptoDataKey_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.GenerateDataKeyRequest{
		KeyID: "master-key",
	})

	req := &Request{
		Opcode:  OpCryptoDataKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoDataKey_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.GenerateDataKeyRequest{})
	req := &Request{
		Opcode:  OpCryptoDataKey,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Secrets Handler Coverage Tests - Remaining handlers at 75%
// =============================================================================

func TestHandleSecretsStore_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.StoreSecretRequest{
		SecretID: "secret/test",
		Name:     "test-secret",
		Data:     []byte("secret-data"),
	})

	req := &Request{
		Opcode:  OpSecretsStore,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsStore_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.StoreSecretRequest{})
	req := &Request{
		Opcode:  OpSecretsStore,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleSecretsGet_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetSecretRequest{
		SecretID: "secret/test",
	})

	req := &Request{
		Opcode:  OpSecretsGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsGet_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetSecretRequest{})
	req := &Request{
		Opcode:  OpSecretsGet,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleSecretsList_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListSecretsRequest{})

	req := &Request{
		Opcode:  OpSecretsList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsList_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListSecretsRequest{})
	req := &Request{
		Opcode:  OpSecretsList,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleSecretsDelete_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteSecretRequest{
		SecretID: "secret/test",
	})

	req := &Request{
		Opcode:  OpSecretsDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsDelete_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteSecretRequest{})
	req := &Request{
		Opcode:  OpSecretsDelete,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleSecretsListVersions_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListSecretVersionsRequest{
		SecretID: "secret/test",
	})

	req := &Request{
		Opcode:  OpSecretsListVersions,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsListVersions_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListSecretVersionsRequest{})
	req := &Request{
		Opcode:  OpSecretsListVersions,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleSecretsRollback_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RollbackSecretRequest{
		SecretID:      "secret/test",
		TargetVersion: 1,
	})

	req := &Request{
		Opcode:  OpSecretsRollback,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsRollback_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RollbackSecretRequest{})
	req := &Request{
		Opcode:  OpSecretsRollback,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Error Crypto Service Mock
// =============================================================================

type errorCryptoService struct{}

var errCryptoServiceError = errors.New("crypto service error")

func (s *errorCryptoService) Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) GenerateDataKey(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) GenerateWrappedDataKey(ctx context.Context, req *service.GenerateWrappedDataKeyRequest) (*service.GenerateWrappedDataKeyResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) GenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	return nil, errCryptoServiceError
}
func (s *errorCryptoService) VerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	return nil, errCryptoServiceError
}

// =============================================================================
// Error Secrets Service Mock
// =============================================================================

type errorSecretsService struct{}

var errSecretsServiceError = errors.New("secrets service error")

func (s *errorSecretsService) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	return nil, errSecretsServiceError
}
func (s *errorSecretsService) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	return nil, errSecretsServiceError
}
