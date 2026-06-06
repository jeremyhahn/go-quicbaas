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
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Crypto HMAC Handler Tests (0% coverage)
// =============================================================================

func TestHandleCryptoGenerateHMAC_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, err := EncodePayload(&service.HMACRequest{
		KeyID:   "hmac-key",
		Message: []byte("test data"),
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoHMAC,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoGenerateHMAC_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.HMACRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoHMAC,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil crypto service")
	}
}

func TestHandleCryptoGenerateHMAC_DecodeError(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoHMAC,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCryptoGenerateHMAC_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.HMACRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoHMAC,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from crypto service")
	}
}

func TestHandleCryptoVerifyHMAC_Success(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, err := EncodePayload(&service.VerifyHMACRequest{
		KeyID:   "hmac-key",
		Message: []byte("test data"),
		HMAC:    []byte("hmac-value"),
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoVerifyHMAC,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCryptoVerifyHMAC_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.VerifyHMACRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoVerifyHMAC,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil crypto service")
	}
}

func TestHandleCryptoVerifyHMAC_DecodeError(t *testing.T) {
	cryptoSvc := &securityTestCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoVerifyHMAC,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleCryptoVerifyHMAC_ServiceError(t *testing.T) {
	cryptoSvc := &errorCryptoService{}
	handler := NewHandler(&mockService{}, nil, nil, cryptoSvc, createTestLogger())

	payload, _ := EncodePayload(&service.VerifyHMACRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCryptoVerifyHMAC,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from crypto service")
	}
}

// =============================================================================
// KMS DEK Reencryption Handler Tests (0% coverage)
// =============================================================================

func TestHandleKMSDEKStartReencryption_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.StartReencryptionRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKStartReencryption,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSDEKStartReencryption_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.StartReencryptionRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKStartReencryption,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSDEKStartReencryption_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKStartReencryption,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSDEKStartReencryption_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.StartReencryptionRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKStartReencryption,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from KMS service")
	}
}

func TestHandleKMSDEKGetReencryptProgress_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetReencryptionProgressRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKGetReencryptProgress,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSDEKGetReencryptProgress_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetReencryptionProgressRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKGetReencryptProgress,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSDEKGetReencryptProgress_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKGetReencryptProgress,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSDEKGetReencryptProgress_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetReencryptionProgressRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSDEKGetReencryptProgress,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from KMS service")
	}
}

// =============================================================================
// KMS Key Policy Handler Tests (0% coverage)
// =============================================================================

func TestHandleKMSSetKeyPolicy_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.SetKeyPolicyRequest{
		KeyID: "key-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSSetKeyPolicy,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSSetKeyPolicy_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.SetKeyPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSSetKeyPolicy,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSSetKeyPolicy_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSSetKeyPolicy,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSSetKeyPolicy_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.SetKeyPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSSetKeyPolicy,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from KMS service")
	}
}

func TestHandleKMSGetKeyPolicy_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetKeyPolicyRequest{
		KeyID: "key-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyPolicy,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSGetKeyPolicy_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetKeyPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyPolicy,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSGetKeyPolicy_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyPolicy,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSGetKeyPolicy_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetKeyPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyPolicy,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from KMS service")
	}
}

// =============================================================================
// KMS Key Metadata Handler Tests (0% coverage)
// =============================================================================

func TestHandleKMSGetKeyMetadata_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetKeyMetadataRequest{
		KeyID: "key-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyMetadata,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSGetKeyMetadata_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetKeyMetadataRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyMetadata,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSGetKeyMetadata_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyMetadata,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSGetKeyMetadata_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetKeyMetadataRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSGetKeyMetadata,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from KMS service")
	}
}

func TestHandleKMSUpdateKeyMetadata_Success(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.UpdateKeyMetadataRequest{
		KeyID: "key-1",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSUpdateKeyMetadata,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleKMSUpdateKeyMetadata_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.UpdateKeyMetadataRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSUpdateKeyMetadata,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil KMS service")
	}
}

func TestHandleKMSUpdateKeyMetadata_DecodeError(t *testing.T) {
	kmsSvc := &securityTestKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSUpdateKeyMetadata,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleKMSUpdateKeyMetadata_ServiceError(t *testing.T) {
	kmsSvc := &errorKMSService{}
	handler := NewHandler(&mockService{}, kmsSvc, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.UpdateKeyMetadataRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpKMSUpdateKeyMetadata,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from KMS service")
	}
}

// =============================================================================
// Backup Handler Tests (0% coverage for handleCreateBackup)
// =============================================================================

func TestHandleCreateBackup_Success(t *testing.T) {
	svc := &mockService{
		createBackupFn: func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
			return &service.CreateBackupResponse{BackupID: "backup-1"}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.CreateBackupRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCreateBackup,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleCreateBackup_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.CreateBackupRequest{})
	resp := handler.handleCreateBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleCreateBackup_DecodeError(t *testing.T) {
	svc := &mockService{
		createBackupFn: func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
			return &service.CreateBackupResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	resp := handler.handleCreateBackup(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleCreateBackup_ServiceError(t *testing.T) {
	svc := &mockService{
		createBackupFn: func(ctx context.Context, req *service.CreateBackupRequest) (*service.CreateBackupResponse, error) {
			return nil, errors.New("backup failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.CreateBackupRequest{})
	resp := handler.handleCreateBackup(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// TimeSeries Query Handler Tests (0% coverage)
// =============================================================================

func TestHandleTimeSeriesQuery_Success(t *testing.T) {
	svc := &mockService{
		timeSeriesQueryFn: func(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
			stream <- &service.TimeSeriesQueryResponse{}
			close(stream)
			return nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.TimeSeriesQueryRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpTimeSeriesQuery,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleTimeSeriesQuery_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.TimeSeriesQueryRequest{})
	resp := handler.handleTimeSeriesQuery(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleTimeSeriesQuery_DecodeError(t *testing.T) {
	svc := &mockService{
		timeSeriesQueryFn: func(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
			close(stream)
			return nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	resp := handler.handleTimeSeriesQuery(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleTimeSeriesQuery_ServiceError(t *testing.T) {
	svc := &mockService{
		timeSeriesQueryFn: func(ctx context.Context, req *service.TimeSeriesQueryRequest, stream chan<- *service.TimeSeriesQueryResponse) error {
			close(stream)
			return errors.New("query failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.TimeSeriesQueryRequest{})
	resp := handler.handleTimeSeriesQuery(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Secrets Extended Handler Tests (75% coverage functions)
// =============================================================================

func TestHandleSecretsGetVersion_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetSecretVersionRequest{
		SecretID: "secret-test",
		Version:  1,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetVersion,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsGetVersion_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetSecretVersionRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetVersion,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsGetVersion_DecodeError(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetVersion,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleSecretsGetVersion_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetSecretVersionRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetVersion,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from secrets service")
	}
}

func TestHandleSecretsSetRotation_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, err := EncodePayload(&service.SetRotationPolicyRequest{
		SecretID: "secret-test",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsSetRotation,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsSetRotation_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.SetRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsSetRotation,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsSetRotation_DecodeError(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsSetRotation,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleSecretsSetRotation_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.SetRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsSetRotation,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from secrets service")
	}
}

func TestHandleSecretsGetRotation_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetRotationPolicyRequest{
		SecretID: "secret-test",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetRotation,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsGetRotation_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetRotation,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsGetRotation_DecodeError(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetRotation,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleSecretsGetRotation_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetRotationPolicyRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetRotation,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from secrets service")
	}
}

func TestHandleSecretsUpdateStatus_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, err := EncodePayload(&service.UpdateSecretStatusRequest{
		SecretID: "secret-test",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsUpdateStatus,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsUpdateStatus_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.UpdateSecretStatusRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsUpdateStatus,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsUpdateStatus_DecodeError(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsUpdateStatus,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleSecretsUpdateStatus_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.UpdateSecretStatusRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsUpdateStatus,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from secrets service")
	}
}

func TestHandleSecretsGetByPrefix_Success(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetSecretsByPrefixRequest{
		Prefix: "secret/",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetByPrefix,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsGetByPrefix_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetSecretsByPrefixRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetByPrefix,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsGetByPrefix_DecodeError(t *testing.T) {
	secretsSvc := &securityTestSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetByPrefix,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleSecretsGetByPrefix_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetSecretsByPrefixRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGetByPrefix,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from secrets service")
	}
}

// =============================================================================
// Secrets Watch Handler Tests (0% coverage)
// =============================================================================

// watchableSecretsService extends securityTestSecretsService to provide
// a channel-based WatchSecretsByPrefix for testing the watch handler.
type watchableSecretsService struct {
	securityTestSecretsService
}

func (s *watchableSecretsService) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	ch := make(chan *service.SecretChange, 1)
	ch <- &service.SecretChange{
		Path:       "secret/test",
		ChangeType: service.SecretChangeCreated,
	}
	close(ch)
	return ch, nil
}

func TestHandleSecretsWatch_Success(t *testing.T) {
	secretsSvc := &watchableSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, err := EncodePayload(&service.WatchSecretsRequest{
		Prefix: "secret/",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsWatch,
		Payload: payload,
	})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleSecretsWatch_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.WatchSecretsRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsWatch,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsWatch_DecodeError(t *testing.T) {
	secretsSvc := &watchableSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsWatch,
		Payload: []byte("{invalid"),
	})
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleSecretsWatch_ServiceError(t *testing.T) {
	secretsSvc := &errorSecretsService{}
	handler := NewHandler(&mockService{}, nil, secretsSvc, nil, createTestLogger())

	payload, _ := EncodePayload(&service.WatchSecretsRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsWatch,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error from secrets service")
	}
}

// =============================================================================
// Operations Management - Success Path Tests (58.3% -> 100%)
// =============================================================================

func TestHandleMigrateData_Success(t *testing.T) {
	svc := &mockService{
		migrateDataFn: func(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
			return &service.MigrateDataResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.MigrateDataRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.handleMigrateData(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleMigrateData_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.MigrateDataRequest{})
	resp := handler.handleMigrateData(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleGetMigrationProgress_Success(t *testing.T) {
	svc := &mockService{
		getMigrationProgressFn: func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
			return &service.GetMigrationProgressResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetMigrationProgressRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.handleGetMigrationProgress(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleGetMigrationProgress_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetMigrationProgressRequest{})
	resp := handler.handleGetMigrationProgress(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleReplaceNode_Success(t *testing.T) {
	svc := &mockService{
		replaceNodeFn: func(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
			return &service.ReplaceNodeResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.ReplaceNodeRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.handleReplaceNode(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleReplaceNode_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ReplaceNodeRequest{})
	resp := handler.handleReplaceNode(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleUpgradeNode_Success(t *testing.T) {
	svc := &mockService{
		upgradeNodeFn: func(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
			return &service.UpgradeNodeResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.UpgradeNodeRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.handleUpgradeNode(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleUpgradeNode_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.UpgradeNodeRequest{})
	resp := handler.handleUpgradeNode(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleGetUpgradeStatus_Success(t *testing.T) {
	svc := &mockService{
		getUpgradeStatusFn: func(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
			return &service.GetUpgradeStatusResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.GetUpgradeStatusRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	resp := handler.handleGetUpgradeStatus(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleGetUpgradeStatus_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetUpgradeStatusRequest{})
	resp := handler.handleGetUpgradeStatus(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}
func TestWithObjectTransferRegistry(t *testing.T) {
	registry := NewObjectTransferRegistry()
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger(), WithObjectTransferRegistry(registry))
	if handler.objectTransferRegistry == nil {
		t.Error("expected objectTransferRegistry to be set")
	}
}

// =============================================================================
// KV Handler Tests - Service Error paths (to improve 75% -> higher)
// =============================================================================

func TestHandleGet_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetRequest{EntityType: "test", ID: 1})
	resp := handler.handleGet(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleGet_ServiceError(t *testing.T) {
	svc := &mockService{
		getFn: func(ctx context.Context, req *service.GetRequest) (*service.GetResponse, error) {
			return nil, errors.New("get failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetRequest{EntityType: "test", ID: 1})
	resp := handler.handleGet(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleDelete_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteRequest{EntityType: "test", ID: 1})
	resp := handler.handleDelete(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleDelete_ServiceError(t *testing.T) {
	svc := &mockService{
		deleteFn: func(ctx context.Context, req *service.DeleteRequest) (*service.DeleteResponse, error) {
			return nil, errors.New("delete failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteRequest{EntityType: "test", ID: 1})
	resp := handler.handleDelete(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleRebalanceCluster_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RebalanceClusterRequest{})
	resp := handler.handleRebalanceCluster(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandleRebalanceCluster_DecodeError(t *testing.T) {
	svc := &mockService{
		rebalanceClusterFn: func(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
			return &service.RebalanceClusterResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	resp := handler.handleRebalanceCluster(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleRebalanceCluster_ServiceError(t *testing.T) {
	svc := &mockService{
		rebalanceClusterFn: func(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
			return nil, errors.New("rebalance failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RebalanceClusterRequest{})
	resp := handler.handleRebalanceCluster(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Barrier / Ceremony Dispatcher Error Paths (replaces legacy Seal tests).
// =============================================================================

func TestHandleBarrierInitialize_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	body, _ := EncodePayload(&barrierInitializeWireRequest{Secret: "pw"})
	payload := append([]byte{SubOpBarrierInitialize}, body...)
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpBarrier,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil barrier service")
	}
}

func TestHandleBarrierUnseal_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	body, _ := EncodePayload(&barrierUnsealWireRequest{Secret: "pw"})
	payload := append([]byte{SubOpBarrierUnseal}, body...)
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpBarrier,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil barrier service")
	}
}

func TestHandleBarrierRekey_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	body, _ := EncodePayload(&barrierRekeyWireRequest{Threshold: 1, Total: 1})
	payload := append([]byte{SubOpBarrierRekey}, body...)
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpBarrier,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil barrier service")
	}
}

func TestHandleCeremonyInitialize_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	body, _ := EncodePayload(&ceremonyInitializeWireRequest{SOPin: "1234"})
	payload := append([]byte{SubOpCeremonyInitialize}, body...)
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCeremony,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil ceremony service")
	}
}

func TestHandleBarrierDispatch_MissingSubOpcode(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpBarrier})
	if resp.Status == StatusOK {
		t.Error("expected error for missing barrier sub-opcode")
	}
}

func TestHandleCeremonyDispatch_MissingSubOpcode(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpCeremony})
	if resp.Status == StatusOK {
		t.Error("expected error for missing ceremony sub-opcode")
	}
}

func TestHandleBarrierDispatch_UnknownSubOpcode(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpBarrier,
		Payload: []byte{0xFE},
	})
	if resp.Status == StatusOK {
		t.Error("expected error for unknown barrier sub-opcode")
	}
}

func TestHandleCeremonyDispatch_UnknownSubOpcode(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpCeremony,
		Payload: []byte{0xFE},
	})
	if resp.Status == StatusOK {
		t.Error("expected error for unknown ceremony sub-opcode")
	}
}

// =============================================================================
// Put Handler - nil service path (83.3% -> higher)
// =============================================================================

func TestHandlePut_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.PutRequest{EntityType: "test", ID: 1, Data: []byte("v")})
	resp := handler.handlePut(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error with nil service")
	}
}

func TestHandlePut_ServiceError(t *testing.T) {
	svc := &mockService{
		putFn: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
			return nil, errors.New("put failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.PutRequest{EntityType: "test", ID: 1, Data: []byte("v")})
	resp := handler.handlePut(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Secrets Handler - Nil Service Paths (83.3% -> higher)
// =============================================================================

func TestHandleSecretsStore_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.StoreSecretRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsStore,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsGet_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetSecretRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsGet,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsList_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListSecretsRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsList,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsDelete_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.DeleteSecretRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsDelete,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsListVersions_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ListSecretVersionsRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsListVersions,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}

func TestHandleSecretsRollback_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.RollbackSecretRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpSecretsRollback,
		Payload: payload,
	})
	if resp.Status == StatusOK {
		t.Error("expected error with nil secrets service")
	}
}
