// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package kms

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// errorTransport returns a fixed transport error for every request.
func errorTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("test", "server unavailable")
		},
	}
}

// respTransport returns the provided value (and nil error) for every request.
func respTransport(resp interface{}) *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return resp, nil
		},
	}
}

// -----------------------------------------------------------------------------
// GenerateKey - service response conversion branch
// -----------------------------------------------------------------------------

func TestClient_GenerateKey_ServiceResponse(t *testing.T) {
	svcResp := &service.GenerateKeyResponse{
		Metadata: &service.KeyMetadata{KeyID: "key-svc"},
		Key: &service.KeyHandle{
			Version:   5,
			Algorithm: service.KeyAlgEd25519,
		},
	}
	client := NewClient(respTransport(svcResp))
	resp, err := client.GenerateKey(context.Background(),
		&GenerateKeyRequest{Name: "k", Algorithm: AlgorithmED25519})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.KeyID != "key-svc" {
		t.Errorf("KeyID = %q, want key-svc", resp.KeyID)
	}
	if resp.Version != 5 {
		t.Errorf("Version = %d, want 5", resp.Version)
	}
	if resp.Algorithm != AlgorithmED25519 {
		t.Errorf("Algorithm = %v, want ED25519", resp.Algorithm)
	}
}

func TestClient_GenerateKey_ServiceResponseNilKey(t *testing.T) {
	svcResp := &service.GenerateKeyResponse{
		Metadata: &service.KeyMetadata{KeyID: "key-nokey"},
	}
	client := NewClient(respTransport(svcResp))
	resp, err := client.GenerateKey(context.Background(),
		&GenerateKeyRequest{Name: "k", Algorithm: AlgorithmAES256GCM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.KeyID != "key-nokey" || resp.Version != 0 {
		t.Errorf("unexpected resp %+v", resp)
	}
}

// -----------------------------------------------------------------------------
// GetKey - service response conversion branch
// -----------------------------------------------------------------------------

func TestClient_GetKey_ServiceResponse(t *testing.T) {
	now := time.Now()
	svcResp := &service.GetKeyResponse{
		Metadata: &service.KeyMetadata{
			KeyID:     "key-svc",
			Name:      "svc-key",
			Algorithm: service.KeyAlgRSA2048,
			Version:   2,
			CreatedAt: now,
			UpdatedAt: now,
			Usage:     []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageSign},
		},
	}
	client := NewClient(respTransport(svcResp))
	info, err := client.GetKey(context.Background(), "key-svc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Name != "svc-key" {
		t.Errorf("Name = %q, want svc-key", info.Name)
	}
	if info.Algorithm != AlgorithmRSA2048 {
		t.Errorf("Algorithm = %v, want RSA2048", info.Algorithm)
	}
	if len(info.Usage) != 2 {
		t.Errorf("Usage len = %d, want 2", len(info.Usage))
	}
}

// -----------------------------------------------------------------------------
// GetPublicKey
// -----------------------------------------------------------------------------

func TestClient_GetPublicKey(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		keyID     string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&GetPublicKeyResponse{KeyID: "key-1", PublicKey: []byte("pub"), Version: 1}),
			wantErr:   false,
		},
		{
			name:  "service response",
			ctx:   context.Background(),
			keyID: "key-2",
			transport: respTransport(&service.GetPublicKeyResponse{
				KeyID:     "key-2",
				PublicKey: []byte("pub"),
				Algorithm: service.KeyAlgECDSAP256,
				Version:   3,
			}),
			wantErr: false,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-1",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response not found",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeNotFound,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(123),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetPublicKey(tt.ctx, tt.keyID)
			checkErr(t, "GetPublicKey", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ListKeys - service response conversion branch
// -----------------------------------------------------------------------------

func TestClient_ListKeys_ServiceResponse(t *testing.T) {
	svcResp := &service.ListKeysResponse{
		Keys: []*service.KeyHandle{
			{KeyID: "k1", Algorithm: service.KeyAlgAES256GCM, Version: 1, CreatedAt: time.Now()},
			{KeyID: "k2", Algorithm: service.KeyAlgEd25519, Version: 2, CreatedAt: time.Now()},
		},
	}
	client := NewClient(respTransport(svcResp))
	resp, err := client.ListKeys(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Keys) != 2 {
		t.Fatalf("Keys len = %d, want 2", len(resp.Keys))
	}
	if resp.Keys[0].KeyID != "k1" || resp.Keys[1].Algorithm != AlgorithmED25519 {
		t.Errorf("unexpected keys %+v", resp.Keys)
	}
}

func TestClient_ListKeys_TransportError(t *testing.T) {
	client := NewClient(errorTransport())
	_, err := client.ListKeys(context.Background())
	if !errors.IsUnavailable(err) {
		t.Errorf("expected unavailable error, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// RotateKey - service response + transport error
// -----------------------------------------------------------------------------

func TestClient_RotateKey_ServiceResponse(t *testing.T) {
	svcResp := &service.RotateKeyResponse{KeyID: "key-r", NewVersion: 7}
	client := NewClient(respTransport(svcResp))
	resp, err := client.RotateKey(context.Background(), "key-r")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.NewVersion != 7 {
		t.Errorf("NewVersion = %d, want 7", resp.NewVersion)
	}
}

func TestClient_RotateKey_TransportError(t *testing.T) {
	client := NewClient(errorTransport())
	_, err := client.RotateKey(context.Background(), "key-r")
	if !errors.IsUnavailable(err) {
		t.Errorf("expected unavailable error, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// DeleteKey - service response branch
// -----------------------------------------------------------------------------

func TestClient_DeleteKey_ServiceResponse(t *testing.T) {
	client := NewClient(respTransport(&service.DeleteKeyResponse{KeyID: "key-d"}))
	if err := client.DeleteKey(context.Background(), "key-d"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// -----------------------------------------------------------------------------
// ImportKey
// -----------------------------------------------------------------------------

func TestClient_ImportKey(t *testing.T) {
	validReq := &ImportKeyRequest{
		KeyMaterial: []byte("material"),
		Name:        "imported",
		Algorithm:   AlgorithmAES256GCM,
	}
	tests := []struct {
		name      string
		ctx       context.Context
		req       *ImportKeyRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(&ImportKeyResponse{KeyID: "key-i", Version: 1}),
			wantErr:   false,
		},
		{
			name: "service response",
			ctx:  context.Background(),
			req:  validReq,
			transport: respTransport(&service.ImportKeyResponse{
				Metadata: &service.KeyMetadata{KeyID: "key-i"},
				Key:      &service.KeyHandle{Version: 2, Algorithm: service.KeyAlgAES256GCM},
			}),
			wantErr: false,
		},
		{
			name: "service response nil key",
			ctx:  context.Background(),
			req:  validReq,
			transport: respTransport(&service.ImportKeyResponse{
				Metadata: &service.KeyMetadata{KeyID: "key-i"},
			}),
			wantErr: false,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty material",
			ctx:       context.Background(),
			req:       &ImportKeyRequest{Name: "x"},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty name",
			ctx:       context.Background(),
			req:       &ImportKeyRequest{KeyMaterial: []byte("m")},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       validReq,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			req:       validReq,
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ImportKey(tt.ctx, tt.req)
			checkErr(t, "ImportKey", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// ExportKey
// -----------------------------------------------------------------------------

func TestClient_ExportKey(t *testing.T) {
	validReq := &ExportKeyRequest{KeyID: "key-e", WrappingKeyID: "wrap-1"}
	tests := []struct {
		name      string
		ctx       context.Context
		req       *ExportKeyRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(&ExportKeyResponse{KeyID: "key-e", WrappedMaterial: []byte("w")}),
			wantErr:   false,
		},
		{
			name: "service response",
			ctx:  context.Background(),
			req:  validReq,
			transport: respTransport(&service.ExportKeyResponse{
				KeyID:         "key-e",
				WrappingKeyID: "wrap-1",
				WrappedKey:    []byte("w"),
				Version:       4,
			}),
			wantErr: false,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			req:       &ExportKeyRequest{WrappingKeyID: "wrap-1"},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty wrapping key ID",
			ctx:       context.Background(),
			req:       &ExportKeyRequest{KeyID: "key-e"},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       validReq,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			req:       validReq,
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(99),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ExportKey(tt.ctx, tt.req)
			checkErr(t, "ExportKey", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// RevokeKey
// -----------------------------------------------------------------------------

func TestClient_RevokeKey(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		keyID     string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&revokeKeyResponse{}),
			wantErr:   false,
		},
		{
			name:      "service response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&service.RevokeKeyResponse{KeyID: "key-1"}),
			wantErr:   false,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(nil),
			wantErr:   false,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-1",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.RevokeKey(tt.ctx, tt.keyID, "test-reason")
			checkErr(t, "RevokeKey", err, tt.wantErr, tt.errCode)
		})
	}
}

// -----------------------------------------------------------------------------
// ListKeyVersions
// -----------------------------------------------------------------------------

func TestClient_ListKeyVersions(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		keyID     string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		wantCount int
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&ListKeyVersionsResponse{KeyID: "key-1", Versions: []KeyVersionInfo{{Version: 1}}}),
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:  "service response",
			ctx:   context.Background(),
			keyID: "key-1",
			transport: respTransport(&service.ListKeyVersionsResponse{
				KeyID: "key-1",
				Versions: []*service.KeyVersionInfo{
					{Version: 1, Status: service.KeyStatusEnabled, Algorithm: service.KeyAlgAES256GCM, CreatedAt: time.Now()},
					{Version: 2, Status: service.KeyStatusDisabled, Algorithm: service.KeyAlgAES256GCM, CreatedAt: time.Now()},
				},
			}),
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:      "nil response yields empty list",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(nil),
			wantErr:   false,
			wantCount: 0,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-1",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListKeyVersions(tt.ctx, tt.keyID)
			checkErr(t, "ListKeyVersions", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && len(resp.Versions) != tt.wantCount {
				t.Errorf("Versions len = %d, want %d", len(resp.Versions), tt.wantCount)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// UpdateKeyStatus
// -----------------------------------------------------------------------------

func TestClient_UpdateKeyStatus(t *testing.T) {
	validReq := &UpdateKeyStatusRequest{KeyID: "key-1", Status: KeyStatusDisabled}
	tests := []struct {
		name      string
		ctx       context.Context
		req       *UpdateKeyStatusRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(&UpdateKeyStatusResponse{KeyID: "key-1", Status: KeyStatusDisabled}),
			wantErr:   false,
		},
		{
			name: "service response",
			ctx:  context.Background(),
			req:  validReq,
			transport: respTransport(&service.UpdateKeyStatusResponse{
				KeyID:     "key-1",
				Version:   2,
				NewStatus: service.KeyStatusDisabled,
			}),
			wantErr: false,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			req:       &UpdateKeyStatusRequest{Status: KeyStatusDisabled},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       validReq,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			req:       validReq,
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.UpdateKeyStatus(tt.ctx, tt.req)
			checkErr(t, "UpdateKeyStatus", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// GetKeyMetadata
// -----------------------------------------------------------------------------

func TestClient_GetKeyMetadata(t *testing.T) {
	now := time.Now()
	svcMeta := &service.KeyMetadata{
		KeyID:       "key-1",
		Name:        "meta-key",
		Description: "desc",
		Algorithm:   service.KeyAlgAES256GCM,
		Status:      service.KeyStatusEnabled,
		Version:     1,
		Usage:       []service.KeyUsage{service.KeyUsageEncrypt},
		CreatedAt:   now,
		UpdatedAt:   now,
		Tags:        map[string]string{"env": "prod"},
		Owner:       "alice",
	}
	tests := []struct {
		name      string
		ctx       context.Context
		keyID     string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&KeyMetadataResponse{KeyID: "key-1"}),
			wantErr:   false,
		},
		{
			name:      "service response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&service.GetKeyMetadataResponse{Metadata: svcMeta}),
			wantErr:   false,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-1",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response not found",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeNotFound,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetKeyMetadata(tt.ctx, tt.keyID)
			checkErr(t, "GetKeyMetadata", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetKeyMetadata_ServiceFields(t *testing.T) {
	now := time.Now()
	svcMeta := &service.KeyMetadata{
		KeyID:              "key-1",
		Name:               "meta-key",
		Description:        "a key",
		Algorithm:          service.KeyAlgRSA4096,
		Status:             service.KeyStatusDisabled,
		Version:            9,
		Exportable:         true,
		DeletionProtection: true,
		Usage:              []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
		CreatedAt:          now,
		UpdatedAt:          now,
		Tags:               map[string]string{"team": "crypto"},
		Owner:              "carol",
	}
	client := NewClient(respTransport(&service.GetKeyMetadataResponse{Metadata: svcMeta}))
	resp, err := client.GetKeyMetadata(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Algorithm != AlgorithmRSA4096 {
		t.Errorf("Algorithm = %v, want RSA4096", resp.Algorithm)
	}
	if resp.Status != KeyStatusDisabled {
		t.Errorf("Status = %v, want DISABLED", resp.Status)
	}
	if !resp.Exportable || !resp.DeletionProtection {
		t.Errorf("expected Exportable and DeletionProtection true")
	}
	if len(resp.Usage) != 2 {
		t.Errorf("Usage len = %d, want 2", len(resp.Usage))
	}
	if resp.Owner != "carol" || resp.Tags["team"] != "crypto" {
		t.Errorf("unexpected owner/tags: %q %v", resp.Owner, resp.Tags)
	}
}

// -----------------------------------------------------------------------------
// UpdateKeyMetadata
// -----------------------------------------------------------------------------

func TestClient_UpdateKeyMetadata(t *testing.T) {
	validReq := &UpdateKeyMetadataRequest{KeyID: "key-1", Name: "new"}
	tests := []struct {
		name      string
		ctx       context.Context
		req       *UpdateKeyMetadataRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(&KeyMetadataResponse{KeyID: "key-1"}),
			wantErr:   false,
		},
		{
			name: "service response",
			ctx:  context.Background(),
			req:  validReq,
			transport: respTransport(&service.UpdateKeyMetadataResponse{
				Metadata: &service.KeyMetadata{KeyID: "key-1", Name: "new"},
			}),
			wantErr: false,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			req:       &UpdateKeyMetadataRequest{Name: "x"},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       validReq,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			req:       validReq,
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.UpdateKeyMetadata(tt.ctx, tt.req)
			checkErr(t, "UpdateKeyMetadata", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// SetKeyPolicy
// -----------------------------------------------------------------------------

func TestClient_SetKeyPolicy(t *testing.T) {
	validReq := &SetKeyPolicyRequest{KeyID: "key-1", Policy: &KeyPolicy{KeyID: "key-1"}}
	tests := []struct {
		name      string
		ctx       context.Context
		req       *SetKeyPolicyRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(&setKeyPolicyResponse{}),
			wantErr:   false,
		},
		{
			name:      "service response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(&service.SetKeyPolicyResponse{KeyID: "key-1"}),
			wantErr:   false,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(nil),
			wantErr:   false,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			req:       &SetKeyPolicyRequest{Policy: &KeyPolicy{}},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil policy",
			ctx:       context.Background(),
			req:       &SetKeyPolicyRequest{KeyID: "key-1"},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       validReq,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			req:       validReq,
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			err := client.SetKeyPolicy(tt.ctx, tt.req)
			checkErr(t, "SetKeyPolicy", err, tt.wantErr, tt.errCode)
		})
	}
}

// -----------------------------------------------------------------------------
// GetKeyPolicy
// -----------------------------------------------------------------------------

func TestClient_GetKeyPolicy(t *testing.T) {
	now := time.Now()
	maxUsage := uint64(100)
	svcPolicy := &service.KeyPolicy{
		KeyID:             "key-1",
		Version:           2,
		AllowedOperations: []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
		AllowedPrincipals: []string{"alice"},
		DeniedPrincipals:  []string{"bob"},
		RotationRequired:  true,
		MaxUsageCount:     &maxUsage,
		RequireMFA:        true,
		UpdatedAt:         now,
	}
	tests := []struct {
		name      string
		ctx       context.Context
		keyID     string
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&KeyPolicy{KeyID: "key-1"}),
			wantErr:   false,
		},
		{
			name:      "service response",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(&service.GetKeyPolicyResponse{Policy: svcPolicy}),
			wantErr:   false,
		},
		{
			name:      "empty key ID",
			ctx:       context.Background(),
			keyID:     "",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			keyID:     "key-1",
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response not found",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeNotFound,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			keyID:     "key-1",
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetKeyPolicy(tt.ctx, tt.keyID)
			checkErr(t, "GetKeyPolicy", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_GetKeyPolicy_ServiceFields(t *testing.T) {
	maxUsage := uint64(50)
	svcPolicy := &service.KeyPolicy{
		KeyID:             "key-1",
		Version:           3,
		AllowedOperations: []service.KeyUsage{service.KeyUsageSign},
		AllowedPrincipals: []string{"alice", "bob"},
		DeniedPrincipals:  []string{"eve"},
		RotationRequired:  true,
		MaxUsageCount:     &maxUsage,
		RequireMFA:        true,
	}
	client := NewClient(respTransport(&service.GetKeyPolicyResponse{Policy: svcPolicy}))
	policy, err := client.GetKeyPolicy(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if policy.MaxUsageCount != 50 {
		t.Errorf("MaxUsageCount = %d, want 50", policy.MaxUsageCount)
	}
	if !policy.RequireMFA || !policy.RotationRequired {
		t.Errorf("expected RequireMFA and RotationRequired true")
	}
	if len(policy.AllowedOperations) != 1 || policy.AllowedOperations[0] != KeyUsageSign {
		t.Errorf("AllowedOperations = %v", policy.AllowedOperations)
	}
	if len(policy.AllowedPrincipals) != 2 || len(policy.DeniedPrincipals) != 1 {
		t.Errorf("unexpected principals")
	}
}

// -----------------------------------------------------------------------------
// GetSupportedAlgorithms
// -----------------------------------------------------------------------------

func TestClient_GetSupportedAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		wantCount int
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			transport: respTransport(&SupportedAlgorithmsResponse{Algorithms: []string{"AES-256-GCM"}}),
			wantErr:   false,
			wantCount: 1,
		},
		{
			name:      "slice response",
			ctx:       context.Background(),
			transport: respTransport([]string{"AES-256-GCM", "ED25519"}),
			wantErr:   false,
			wantCount: 2,
		},
		{
			name:      "nil response yields empty",
			ctx:       context.Background(),
			transport: respTransport(nil),
			wantErr:   false,
			wantCount: 0,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			transport: respTransport(123),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetSupportedAlgorithms(tt.ctx)
			checkErr(t, "GetSupportedAlgorithms", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && len(resp.Algorithms) != tt.wantCount {
				t.Errorf("Algorithms len = %d, want %d", len(resp.Algorithms), tt.wantCount)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// BackendStatus
// -----------------------------------------------------------------------------

func TestClient_BackendStatus(t *testing.T) {
	svcResp := &service.BackendStatusResponse{
		BackendType:  "software",
		Status:       "healthy",
		Device:       "/dev/null",
		Region:       "us-east-1",
		KeyID:        "key-1",
		PCRBanks:     []string{"sha256"},
		PCRRegisters: []int{0, 7},
		Capabilities: map[string]bool{"sign": true},
	}
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			transport: respTransport(&BackendStatusResponse{BackendType: "software", Status: "healthy"}),
			wantErr:   false,
		},
		{
			name:      "service response",
			ctx:       context.Background(),
			transport: respTransport(svcResp),
			wantErr:   false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.BackendStatus(tt.ctx)
			checkErr(t, "BackendStatus", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestClient_BackendStatus_ServiceFields(t *testing.T) {
	svcResp := &service.BackendStatusResponse{
		BackendType:  "tpm2",
		Status:       "healthy",
		PCRRegisters: []int{0, 7, 14},
		Capabilities: map[string]bool{"seal": true},
	}
	client := NewClient(respTransport(svcResp))
	resp, err := client.BackendStatus(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.PCRRegisters) != 3 || resp.PCRRegisters[2] != 14 {
		t.Errorf("PCRRegisters = %v, want [0 7 14]", resp.PCRRegisters)
	}
	if !resp.Capabilities["seal"] {
		t.Errorf("expected seal capability")
	}
}

// -----------------------------------------------------------------------------
// UnwrapKey
// -----------------------------------------------------------------------------

func TestClient_UnwrapKey(t *testing.T) {
	validReq := &UnwrapKeyRequest{
		WrappingKeyID:   "wrap-1",
		WrappedMaterial: []byte("wrapped"),
		Algorithm:       AlgorithmAES256GCM,
	}
	tests := []struct {
		name      string
		ctx       context.Context
		req       *UnwrapKeyRequest
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name:      "sdk response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(&UnwrapKeyResponse{KeyID: "key-1", Version: 1}),
			wantErr:   false,
		},
		{
			name: "service response",
			ctx:  context.Background(),
			req:  validReq,
			transport: respTransport(&service.UnwrapKeyResponse{
				KeyID:     "key-1",
				Version:   3,
				Algorithm: service.KeyAlgAES256GCM,
			}),
			wantErr: false,
		},
		{
			name:      "nil request",
			ctx:       context.Background(),
			req:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty wrapping key ID",
			ctx:       context.Background(),
			req:       &UnwrapKeyRequest{WrappedMaterial: []byte("w")},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "empty wrapped material",
			ctx:       context.Background(),
			req:       &UnwrapKeyRequest{WrappingKeyID: "wrap-1"},
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "nil context",
			ctx:       nil,
			req:       validReq,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name:      "transport error",
			ctx:       context.Background(),
			req:       validReq,
			transport: errorTransport(),
			wantErr:   true,
			errCode:   errors.CodeUnavailable,
		},
		{
			name:      "nil response",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport(nil),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
		{
			name:      "invalid response type",
			ctx:       context.Background(),
			req:       validReq,
			transport: respTransport("bad"),
			wantErr:   true,
			errCode:   errors.CodeInternal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.UnwrapKey(tt.ctx, tt.req)
			checkErr(t, "UnwrapKey", err, tt.wantErr, tt.errCode)
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Stringer methods not yet covered
// -----------------------------------------------------------------------------

func TestKeyStatus_String(t *testing.T) {
	tests := []struct {
		status   KeyStatus
		expected string
	}{
		{KeyStatusEnabled, "ENABLED"},
		{KeyStatusDisabled, "DISABLED"},
		{KeyStatusPendingDeletion, "PENDING_DELETION"},
		{KeyStatusDeleted, "DELETED"},
		{KeyStatusRotating, "ROTATING"},
		{KeyStatusCompromised, "COMPROMISED"},
		{KeyStatus(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestExportFormat_String(t *testing.T) {
	tests := []struct {
		format   ExportFormat
		expected string
	}{
		{ExportFormatRaw, "RAW"},
		{ExportFormatJWK, "JWK"},
		{ExportFormatPEM, "PEM"},
		{ExportFormat(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.format.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Conversion helpers - exercise non-default branches and nil handling
// -----------------------------------------------------------------------------

func TestServiceMetadataToSDK_Nil(t *testing.T) {
	if got := serviceMetadataToSDK(nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestServicePolicyToSDK_Nil(t *testing.T) {
	if got := servicePolicyToSDK(nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestServicePolicyToSDK_NilMaxUsage(t *testing.T) {
	p := &service.KeyPolicy{
		KeyID:             "key-1",
		AllowedOperations: []service.KeyUsage{service.KeyUsageSign},
	}
	got := servicePolicyToSDK(p)
	if got.MaxUsageCount != 0 {
		t.Errorf("MaxUsageCount = %d, want 0", got.MaxUsageCount)
	}
	if len(got.AllowedOperations) != 1 || got.AllowedOperations[0] != KeyUsageSign {
		t.Errorf("AllowedOperations = %v", got.AllowedOperations)
	}
}

func TestSdkAlgToServiceAlg(t *testing.T) {
	tests := []struct {
		sdk      KeyAlgorithm
		expected service.KeyAlgorithm
	}{
		{AlgorithmAES256GCM, service.KeyAlgAES256GCM},
		{AlgorithmChaCha20Poly1305, service.KeyAlgChaCha20Poly1305},
		{AlgorithmRSA2048, service.KeyAlgRSA2048},
		{AlgorithmRSA4096, service.KeyAlgRSA4096},
		{AlgorithmECDSAP256, service.KeyAlgECDSAP256},
		{AlgorithmECDSAP384, service.KeyAlgECDSAP384},
		{AlgorithmED25519, service.KeyAlgEd25519},
		{KeyAlgorithm(99), service.KeyAlgAES256GCM}, // default
	}
	for _, tt := range tests {
		if got := sdkAlgToServiceAlg(tt.sdk); got != tt.expected {
			t.Errorf("sdkAlgToServiceAlg(%v) = %v, want %v", tt.sdk, got, tt.expected)
		}
	}
}

func TestServiceAlgToSDKAlg(t *testing.T) {
	tests := []struct {
		svc      service.KeyAlgorithm
		expected KeyAlgorithm
	}{
		{service.KeyAlgAES256GCM, AlgorithmAES256GCM},
		{service.KeyAlgChaCha20Poly1305, AlgorithmChaCha20Poly1305},
		{service.KeyAlgRSA2048, AlgorithmRSA2048},
		{service.KeyAlgRSA4096, AlgorithmRSA4096},
		{service.KeyAlgECDSAP256, AlgorithmECDSAP256},
		{service.KeyAlgECDSAP384, AlgorithmECDSAP384},
		{service.KeyAlgEd25519, AlgorithmED25519},
		{service.KeyAlgorithm("nonsense"), AlgorithmAES256GCM}, // default
	}
	for _, tt := range tests {
		if got := serviceAlgToSDKAlg(tt.svc); got != tt.expected {
			t.Errorf("serviceAlgToSDKAlg(%v) = %v, want %v", tt.svc, got, tt.expected)
		}
	}
}

func TestServiceStatusToSDKStatus(t *testing.T) {
	tests := []struct {
		svc      service.KeyStatus
		expected KeyStatus
	}{
		{service.KeyStatusEnabled, KeyStatusEnabled},
		{service.KeyStatusDisabled, KeyStatusDisabled},
		{service.KeyStatusPendingDeletion, KeyStatusPendingDeletion},
		{service.KeyStatusDeleted, KeyStatusDeleted},
		{service.KeyStatusRotating, KeyStatusRotating},
		{service.KeyStatusCompromised, KeyStatusCompromised},
		{service.KeyStatus("nonsense"), KeyStatusEnabled}, // default
	}
	for _, tt := range tests {
		if got := serviceStatusToSDKStatus(tt.svc); got != tt.expected {
			t.Errorf("serviceStatusToSDKStatus(%v) = %v, want %v", tt.svc, got, tt.expected)
		}
	}
}

func TestSdkUsageToServiceUsage(t *testing.T) {
	tests := []struct {
		sdk      KeyUsage
		expected service.KeyUsage
	}{
		{KeyUsageEncrypt, service.KeyUsageEncrypt},
		{KeyUsageDecrypt, service.KeyUsageDecrypt},
		{KeyUsageSign, service.KeyUsageSign},
		{KeyUsageVerify, service.KeyUsageVerify},
		{KeyUsageWrap, service.KeyUsageWrapKey},
		{KeyUsageUnwrap, service.KeyUsageWrapKey},
		{KeyUsage(99), service.KeyUsageEncrypt}, // default
	}
	for _, tt := range tests {
		if got := sdkUsageToServiceUsage(tt.sdk); got != tt.expected {
			t.Errorf("sdkUsageToServiceUsage(%v) = %v, want %v", tt.sdk, got, tt.expected)
		}
	}
}

func TestServiceUsageToSDKUsage(t *testing.T) {
	tests := []struct {
		svc      service.KeyUsage
		expected KeyUsage
	}{
		{service.KeyUsageEncrypt, KeyUsageEncrypt},
		{service.KeyUsageDecrypt, KeyUsageDecrypt},
		{service.KeyUsageSign, KeyUsageSign},
		{service.KeyUsageVerify, KeyUsageVerify},
		{service.KeyUsageWrapKey, KeyUsageWrap},
		{service.KeyUsage("nonsense"), KeyUsageEncrypt}, // default
	}
	for _, tt := range tests {
		if got := serviceUsageToSDKUsage(tt.svc); got != tt.expected {
			t.Errorf("serviceUsageToSDKUsage(%v) = %v, want %v", tt.svc, got, tt.expected)
		}
	}
}

func TestDerefUint64(t *testing.T) {
	if got := derefUint64(nil); got != 0 {
		t.Errorf("derefUint64(nil) = %d, want 0", got)
	}
	v := uint64(42)
	if got := derefUint64(&v); got != 42 {
		t.Errorf("derefUint64(&42) = %d, want 42", got)
	}
}

// -----------------------------------------------------------------------------
// shared assertion helper
// -----------------------------------------------------------------------------

func checkErr(t *testing.T, op string, err error, wantErr bool, errCode errors.ErrorCode) {
	t.Helper()
	if (err != nil) != wantErr {
		t.Errorf("%s() error = %v, wantErr %v", op, err, wantErr)
		return
	}
	if wantErr && err != nil {
		if errors.Code(err) != errCode {
			t.Errorf("%s() error code = %v, want %v", op, errors.Code(err), errCode)
		}
	}
}
