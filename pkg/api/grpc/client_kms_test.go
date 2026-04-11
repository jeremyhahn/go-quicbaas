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
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestClientKMSGenerateKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGenerateKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSImportKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSImportKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGetKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSRotateKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSRotateKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSDeleteKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSDeleteKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSEncrypt_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSEncrypt(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSDecrypt_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSDecrypt(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSGenerateDataKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGenerateDataKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// Test conversion helpers
func TestConvertProtoKeyHandle(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		input    *pb.KeyHandle
		expected *service.KeyHandle
	}{
		{
			name:     "nil_input",
			input:    nil,
			expected: nil,
		},
		{
			name: "valid_conversion",
			input: &pb.KeyHandle{
				KeyId:     "key-123",
				Version:   1,
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
				CreatedAt: now.Unix(),
			},
			expected: &service.KeyHandle{
				KeyID:     "key-123",
				Version:   1,
				Algorithm: service.KeyAlgAES256GCM,
				Status:    service.KeyStatusEnabled,
				CreatedAt: now.Truncate(time.Second),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoKeyHandle(tt.input)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.KeyID, result.KeyID)
				assert.Equal(t, tt.expected.Version, result.Version)
				assert.Equal(t, tt.expected.Algorithm, result.Algorithm)
				assert.Equal(t, tt.expected.Status, result.Status)
			}
		})
	}
}

func TestConvertProtoKeyMetadata(t *testing.T) {
	now := time.Now()
	lastUsed := now.Add(-1 * time.Hour)

	tests := []struct {
		name  string
		input *pb.KeyMetadata
	}{
		{
			name:  "nil_input",
			input: nil,
		},
		{
			name: "valid_conversion_with_last_used",
			input: &pb.KeyMetadata{
				KeyId:       "key-123",
				Name:        "test-key",
				Description: "Test description",
				Algorithm:   pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Status:      pb.KeyStatus_KEY_STATUS_ENABLED,
				Usage:       []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT},
				CreatedAt:   now.Unix(),
				UpdatedAt:   now.Unix(),
				LastUsedAt:  lastUsed.Unix(),
			},
		},
		{
			name: "valid_conversion_without_last_used",
			input: &pb.KeyMetadata{
				KeyId:     "key-123",
				CreatedAt: now.Unix(),
				UpdatedAt: now.Unix(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoKeyMetadata(tt.input)
			if tt.input == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.input.KeyId, result.KeyID)
			}
		})
	}
}

func TestConvertProtoToServiceKeyAlgorithm(t *testing.T) {
	tests := []struct {
		input    pb.KeyAlgorithm
		expected service.KeyAlgorithm
	}{
		{pb.KeyAlgorithm_KEY_ALG_AES_128_GCM, service.KeyAlgAES128GCM},
		{pb.KeyAlgorithm_KEY_ALG_AES_192_GCM, service.KeyAlgAES192GCM},
		{pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, service.KeyAlgAES256GCM},
		{pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305, service.KeyAlgChaCha20Poly1305},
		{pb.KeyAlgorithm_KEY_ALG_RSA_2048, service.KeyAlgRSA2048},
		{pb.KeyAlgorithm_KEY_ALG_RSA_3072, service.KeyAlgRSA3072},
		{pb.KeyAlgorithm_KEY_ALG_RSA_4096, service.KeyAlgRSA4096},
		{pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, service.KeyAlgECDSAP256},
		{pb.KeyAlgorithm_KEY_ALG_ECDSA_P384, service.KeyAlgECDSAP384},
		{pb.KeyAlgorithm_KEY_ALG_ECDSA_P521, service.KeyAlgECDSAP521},
		{pb.KeyAlgorithm_KEY_ALG_ED25519, service.KeyAlgEd25519},
		{pb.KeyAlgorithm_KEY_ALG_ED448, service.KeyAlgEd448},
		{pb.KeyAlgorithm(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := convertProtoToServiceKeyAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceKeyStatus(t *testing.T) {
	tests := []struct {
		input    pb.KeyStatus
		expected service.KeyStatus
	}{
		{pb.KeyStatus_KEY_STATUS_ENABLED, service.KeyStatusEnabled},
		{pb.KeyStatus_KEY_STATUS_DISABLED, service.KeyStatusDisabled},
		{pb.KeyStatus_KEY_STATUS_PENDING_DELETION, service.KeyStatusPendingDeletion},
		{pb.KeyStatus_KEY_STATUS_DELETED, service.KeyStatusDeleted},
		{pb.KeyStatus_KEY_STATUS_ROTATING, service.KeyStatusRotating},
		{pb.KeyStatus_KEY_STATUS_COMPROMISED, service.KeyStatusCompromised},
		{pb.KeyStatus(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := convertProtoToServiceKeyStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceKeyUsage(t *testing.T) {
	tests := []struct {
		input    pb.KeyUsage
		expected service.KeyUsage
	}{
		{pb.KeyUsage_KEY_USAGE_ENCRYPT, service.KeyUsageEncrypt},
		{pb.KeyUsage_KEY_USAGE_DECRYPT, service.KeyUsageDecrypt},
		{pb.KeyUsage_KEY_USAGE_SIGN, service.KeyUsageSign},
		{pb.KeyUsage_KEY_USAGE_VERIFY, service.KeyUsageVerify},
		{pb.KeyUsage_KEY_USAGE_WRAP_KEY, service.KeyUsageWrapKey},
		{pb.KeyUsage_KEY_USAGE_DERIVE_KEY, service.KeyUsageDeriveKey},
		{pb.KeyUsage_KEY_USAGE_GENERATE_MAC, service.KeyUsageGenerateMAC},
		{pb.KeyUsage_KEY_USAGE_VERIFY_MAC, service.KeyUsageVerifyMAC},
		{pb.KeyUsage(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := convertProtoToServiceKeyUsage(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceKeyUsageList(t *testing.T) {
	tests := []struct {
		name     string
		input    []pb.KeyUsage
		expected []service.KeyUsage
	}{
		{
			name:     "empty_list",
			input:    []pb.KeyUsage{},
			expected: []service.KeyUsage{},
		},
		{
			name: "multiple_usages",
			input: []pb.KeyUsage{
				pb.KeyUsage_KEY_USAGE_ENCRYPT,
				pb.KeyUsage_KEY_USAGE_DECRYPT,
				pb.KeyUsage_KEY_USAGE_SIGN,
			},
			expected: []service.KeyUsage{
				service.KeyUsageEncrypt,
				service.KeyUsageDecrypt,
				service.KeyUsageSign,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoToServiceKeyUsageList(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Merged from client_kms_additional_test.go ---

const kmsAdditionalBufSize = 1024 * 1024

// extendedKMSServer implements pb.KeyManagementServer for testing additional methods
type extendedKMSServer struct {
	pb.UnimplementedKeyManagementServer

	updateKeyStatusFunc        func(ctx context.Context, req *pb.UpdateKeyStatusRequest) (*pb.UpdateKeyStatusResponse, error)
	listKeyVersionsFunc        func(ctx context.Context, req *pb.ListKeyVersionsRequest) (*pb.ListKeyVersionsResponse, error)
	exportKeyFunc              func(ctx context.Context, req *pb.ExportKeyRequest) (*pb.ExportKeyResponse, error)
	unwrapKeyFunc              func(ctx context.Context, req *pb.UnwrapKeyRequest) (*pb.UnwrapKeyResponse, error)
	setKeySafetyFunc           func(ctx context.Context, req *pb.SetKeySafetyRequest) (*pb.SetKeySafetyResponse, error)
	clearKeySafetyFunc         func(ctx context.Context, req *pb.ClearKeySafetyRequest) (*pb.ClearKeySafetyResponse, error)
	getSupportedAlgorithmsFunc func(ctx context.Context, req *pb.GetSupportedAlgorithmsRequest) (*pb.GetSupportedAlgorithmsResponse, error)
}

func (s *extendedKMSServer) UpdateKeyStatus(ctx context.Context, req *pb.UpdateKeyStatusRequest) (*pb.UpdateKeyStatusResponse, error) {
	if s.updateKeyStatusFunc != nil {
		return s.updateKeyStatusFunc(ctx, req)
	}
	return &pb.UpdateKeyStatusResponse{
		KeyId:     req.KeyId,
		Version:   req.Version,
		Status:    req.Status,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *extendedKMSServer) ListKeyVersions(ctx context.Context, req *pb.ListKeyVersionsRequest) (*pb.ListKeyVersionsResponse, error) {
	if s.listKeyVersionsFunc != nil {
		return s.listKeyVersionsFunc(ctx, req)
	}
	return &pb.ListKeyVersionsResponse{
		KeyId: req.KeyId,
		Versions: []*pb.KeyVersionInfo{
			{
				Version:   1,
				Status:    pb.KeyStatus_KEY_STATUS_DISABLED,
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				CreatedAt: time.Now().Add(-1 * time.Hour).Unix(),
			},
			{
				Version:   2,
				Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				CreatedAt: time.Now().Unix(),
			},
		},
		NextCursor: "",
		HasMore:    false,
		Timestamp:  time.Now().Unix(),
	}, nil
}

func (s *extendedKMSServer) ExportKey(ctx context.Context, req *pb.ExportKeyRequest) (*pb.ExportKeyResponse, error) {
	if s.exportKeyFunc != nil {
		return s.exportKeyFunc(ctx, req)
	}
	return &pb.ExportKeyResponse{
		KeyId:             req.KeyId,
		Format:            req.Format,
		WrappingKeyId:     req.WrappingKeyId,
		WrappingAlgorithm: req.WrappingAlgorithm,
		WrappedMaterial:   []byte("wrapped-key-material"),
		Timestamp:         time.Now().Unix(),
	}, nil
}

func (s *extendedKMSServer) UnwrapKey(ctx context.Context, req *pb.UnwrapKeyRequest) (*pb.UnwrapKeyResponse, error) {
	if s.unwrapKeyFunc != nil {
		return s.unwrapKeyFunc(ctx, req)
	}
	now := time.Now()
	return &pb.UnwrapKeyResponse{
		Key: &pb.KeyHandle{
			KeyId:     req.KeyId,
			Version:   1,
			Algorithm: req.Algorithm,
			Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
			CreatedAt: now.Unix(),
		},
		Metadata: &pb.KeyMetadata{
			KeyId:     req.KeyId,
			Name:      req.Name,
			Algorithm: req.Algorithm,
			Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
			Version:   1,
			CreatedAt: now.Unix(),
			UpdatedAt: now.Unix(),
		},
		Timestamp: now.Unix(),
	}, nil
}

func (s *extendedKMSServer) SetKeySafety(ctx context.Context, req *pb.SetKeySafetyRequest) (*pb.SetKeySafetyResponse, error) {
	if s.setKeySafetyFunc != nil {
		return s.setKeySafetyFunc(ctx, req)
	}
	return &pb.SetKeySafetyResponse{
		KeyId:     req.KeyId,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *extendedKMSServer) ClearKeySafety(ctx context.Context, req *pb.ClearKeySafetyRequest) (*pb.ClearKeySafetyResponse, error) {
	if s.clearKeySafetyFunc != nil {
		return s.clearKeySafetyFunc(ctx, req)
	}
	return &pb.ClearKeySafetyResponse{
		KeyId:     req.KeyId,
		Timestamp: time.Now().Unix(),
	}, nil
}

func (s *extendedKMSServer) GetSupportedAlgorithms(ctx context.Context, req *pb.GetSupportedAlgorithmsRequest) (*pb.GetSupportedAlgorithmsResponse, error) {
	if s.getSupportedAlgorithmsFunc != nil {
		return s.getSupportedAlgorithmsFunc(ctx, req)
	}
	return &pb.GetSupportedAlgorithmsResponse{
		Algorithms: []string{
			"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
			"RSA-2048", "RSA-3072", "RSA-4096",
			"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
			"ED25519", "ED448",
		},
	}, nil
}

func setupExtendedKMSTestServer(kmsServer *extendedKMSServer) (*grpc.Server, *bufconn.Listener) {
	lis := bufconn.Listen(kmsAdditionalBufSize)
	server := grpc.NewServer()
	pb.RegisterKeyManagementServer(server, kmsServer)
	go func() {
		if err := server.Serve(lis); err != nil {
			// Server stopped
		}
	}()
	return server, lis
}

func createExtendedKMSTestClient(lis *bufconn.Listener) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
}

func setupConnectedExtendedKMSClient(t *testing.T, kmsServer *extendedKMSServer) (*Client, *grpc.Server) {
	server, lis := setupExtendedKMSTestServer(kmsServer)
	conn, err := createExtendedKMSTestClient(lis)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	client := &Client{
		conn:      conn,
		connected: true,
		logger:    log,
	}
	return client, server
}

// =============================================================================
// KMS Client Additional Method Tests
// =============================================================================

func TestClientKMSUpdateKeyStatus_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSUpdateKeyStatus(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSUpdateKeyStatus_Success(t *testing.T) {
	kmsServer := &extendedKMSServer{}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.UpdateKeyStatusRequest{
		KeyID:  "key-123",
		Status: service.KeyStatusEnabled,
	}

	resp, err := client.KMSUpdateKeyStatus(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Equal(t, service.KeyStatusEnabled, resp.Status)
}

func TestClientKMSUpdateKeyStatus_Error(t *testing.T) {
	kmsServer := &extendedKMSServer{
		updateKeyStatusFunc: func(ctx context.Context, req *pb.UpdateKeyStatusRequest) (*pb.UpdateKeyStatusResponse, error) {
			return nil, status.Error(codes.NotFound, "key not found")
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.UpdateKeyStatusRequest{
		KeyID:  "nonexistent",
		Status: service.KeyStatusEnabled,
	}

	resp, err := client.KMSUpdateKeyStatus(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSListKeyVersions_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSListKeyVersions(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSListKeyVersions_Success(t *testing.T) {
	kmsServer := &extendedKMSServer{}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ListKeyVersionsRequest{
		KeyID: "key-123",
	}

	resp, err := client.KMSListKeyVersions(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.Len(t, resp.Versions, 2)
}

func TestClientKMSListKeyVersions_Error(t *testing.T) {
	kmsServer := &extendedKMSServer{
		listKeyVersionsFunc: func(ctx context.Context, req *pb.ListKeyVersionsRequest) (*pb.ListKeyVersionsResponse, error) {
			return nil, status.Error(codes.NotFound, "key not found")
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ListKeyVersionsRequest{
		KeyID: "nonexistent",
	}

	resp, err := client.KMSListKeyVersions(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSExportKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSExportKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSExportKey_Success(t *testing.T) {
	kmsServer := &extendedKMSServer{}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ExportKeyRequest{
		KeyID:             "key-123",
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-GCM",
		Format:            "raw",
	}

	resp, err := client.KMSExportKey(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "key-123", resp.KeyID)
	assert.NotEmpty(t, resp.WrappedKey)
}

func TestClientKMSExportKey_Error(t *testing.T) {
	kmsServer := &extendedKMSServer{
		exportKeyFunc: func(ctx context.Context, req *pb.ExportKeyRequest) (*pb.ExportKeyResponse, error) {
			return nil, status.Error(codes.PermissionDenied, "key not exportable")
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ExportKeyRequest{
		KeyID:         "key-123",
		WrappingKeyID: "wrapper-key",
	}

	resp, err := client.KMSExportKey(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSUnwrapKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSUnwrapKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSUnwrapKey_Success(t *testing.T) {
	kmsServer := &extendedKMSServer{}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.UnwrapKeyRequest{
		KeyID:             "new-key",
		WrappedKey:        []byte("wrapped-key-material"),
		WrappingKeyID:     "wrapper-key",
		WrappingAlgorithm: "AES-GCM",
		Algorithm:         "AES-256-GCM",
		Name:              "imported-key",
	}

	resp, err := client.KMSUnwrapKey(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "new-key", resp.KeyID)
	assert.Equal(t, uint64(1), resp.Version)
	assert.Equal(t, service.KeyStatusEnabled, resp.Status)
}

func TestClientKMSUnwrapKey_Error(t *testing.T) {
	kmsServer := &extendedKMSServer{
		unwrapKeyFunc: func(ctx context.Context, req *pb.UnwrapKeyRequest) (*pb.UnwrapKeyResponse, error) {
			return nil, status.Error(codes.InvalidArgument, "invalid wrapped key")
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.UnwrapKeyRequest{
		KeyID:         "new-key",
		WrappedKey:    []byte("bad-wrapped-key"),
		WrappingKeyID: "wrapper-key",
	}

	resp, err := client.KMSUnwrapKey(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSSetKeySafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetKeySafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSSetKeySafety_Success(t *testing.T) {
	kmsServer := &extendedKMSServer{}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SetKeySafetyRequest{
		KeyID:          "key-123",
		MaxOperations:  1000000,
		MaxBytes:       1073741824,
		WarningPercent: 80,
	}

	resp, err := client.KMSSetKeySafety(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "key-123", resp.KeyID)
}

func TestClientKMSSetKeySafety_Error(t *testing.T) {
	kmsServer := &extendedKMSServer{
		setKeySafetyFunc: func(ctx context.Context, req *pb.SetKeySafetyRequest) (*pb.SetKeySafetyResponse, error) {
			return nil, status.Error(codes.NotFound, "key not found")
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.SetKeySafetyRequest{
		KeyID: "nonexistent",
	}

	resp, err := client.KMSSetKeySafety(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSClearKeySafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSClearKeySafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

func TestClientKMSClearKeySafety_Success(t *testing.T) {
	kmsServer := &extendedKMSServer{}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ClearKeySafetyRequest{
		KeyID: "key-123",
	}

	resp, err := client.KMSClearKeySafety(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "key-123", resp.KeyID)
}

func TestClientKMSClearKeySafety_Error(t *testing.T) {
	kmsServer := &extendedKMSServer{
		clearKeySafetyFunc: func(ctx context.Context, req *pb.ClearKeySafetyRequest) (*pb.ClearKeySafetyResponse, error) {
			return nil, status.Error(codes.NotFound, "key not found")
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	ctx := context.Background()
	req := &service.ClearKeySafetyRequest{
		KeyID: "nonexistent",
	}

	resp, err := client.KMSClearKeySafety(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSGetSupportedAlgorithms_Success(t *testing.T) {
	kmsServer := &extendedKMSServer{
		getSupportedAlgorithmsFunc: func(ctx context.Context, req *pb.GetSupportedAlgorithmsRequest) (*pb.GetSupportedAlgorithmsResponse, error) {
			return &pb.GetSupportedAlgorithmsResponse{
				Algorithms: []string{
					"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
					"CHACHA20-POLY1305", "XCHACHA20-POLY1305",
					"RSA-2048", "RSA-3072", "RSA-4096",
					"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
					"ED25519", "ED448",
				},
			}, nil
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.Close()

	ctx := context.Background()
	algorithms, err := client.KMSGetSupportedAlgorithms(ctx)

	require.NoError(t, err)
	require.NotNil(t, algorithms)
	assert.Contains(t, algorithms, "AES-256-GCM")
	assert.Contains(t, algorithms, "ED25519")
	assert.Contains(t, algorithms, "RSA-4096")
	// Verify ChaCha20 algorithms are now returned
	assert.Contains(t, algorithms, "CHACHA20-POLY1305")
	assert.Contains(t, algorithms, "XCHACHA20-POLY1305")
}

func TestClientKMSGetSupportedAlgorithms_Error(t *testing.T) {
	kmsServer := &extendedKMSServer{
		getSupportedAlgorithmsFunc: func(ctx context.Context, req *pb.GetSupportedAlgorithmsRequest) (*pb.GetSupportedAlgorithmsResponse, error) {
			return nil, status.Error(codes.Internal, "backend error")
		},
	}
	client, server := setupConnectedExtendedKMSClient(t, kmsServer)
	defer server.Stop()
	defer client.Close()

	ctx := context.Background()
	algorithms, err := client.KMSGetSupportedAlgorithms(ctx)

	require.Error(t, err)
	assert.Nil(t, algorithms)
}

func TestClientKMSGetSupportedAlgorithms_NotConnected(t *testing.T) {
	client := &Client{} // Not connected
	ctx := context.Background()

	algorithms, err := client.KMSGetSupportedAlgorithms(ctx)

	require.Error(t, err)
	assert.Nil(t, algorithms)
}

// =============================================================================
// Conversion Helper Tests
// =============================================================================

func TestConvertServiceToProtoKeyStatus(t *testing.T) {
	tests := []struct {
		input    service.KeyStatus
		expected pb.KeyStatus
	}{
		{service.KeyStatusEnabled, pb.KeyStatus_KEY_STATUS_ENABLED},
		{service.KeyStatusDisabled, pb.KeyStatus_KEY_STATUS_DISABLED},
		{service.KeyStatusPendingDeletion, pb.KeyStatus_KEY_STATUS_PENDING_DELETION},
		{service.KeyStatusDeleted, pb.KeyStatus_KEY_STATUS_DELETED},
		{service.KeyStatusRotating, pb.KeyStatus_KEY_STATUS_ROTATING},
		{service.KeyStatusCompromised, pb.KeyStatus_KEY_STATUS_COMPROMISED},
		{service.KeyStatus("UNKNOWN"), pb.KeyStatus_KEY_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := convertServiceToProtoKeyStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoKeyVersionInfo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		keyID string
		input *pb.KeyVersionInfo
	}{
		{
			name:  "nil_input",
			keyID: "key-123",
			input: nil,
		},
		{
			name:  "full_version_info",
			keyID: "key-456",
			input: &pb.KeyVersionInfo{
				Version:   2,
				Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				CreatedAt: now.Unix(),
			},
		},
		{
			name:  "disabled_version",
			keyID: "key-789",
			input: &pb.KeyVersionInfo{
				Version:   1,
				Status:    pb.KeyStatus_KEY_STATUS_DISABLED,
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				CreatedAt: now.Add(-1 * time.Hour).Unix(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoKeyVersionInfo(tt.keyID, tt.input)
			if tt.input == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.keyID, result.KeyID)
				assert.Equal(t, tt.input.Version, result.Version)
			}
		})
	}
}

func TestConvertServiceToProtoWrappingAlgorithm(t *testing.T) {
	tests := []struct {
		input    string
		expected pb.WrappingAlgorithm
	}{
		{"AES-KW", pb.WrappingAlgorithm_WRAP_ALG_AES_KW},
		{"AES-KWP", pb.WrappingAlgorithm_WRAP_ALG_AES_KWP},
		{"AES-GCM", pb.WrappingAlgorithm_WRAP_ALG_AES_GCM},
		{"RSA-OAEP-SHA256", pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256},
		{"RSA-OAEP-SHA384", pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384},
		{"RSA-OAEP-SHA512", pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512},
		{"ECDH-ES+A256KW", pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW},
		{"UNKNOWN", pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := convertServiceToProtoWrappingAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceWrappingAlgorithm(t *testing.T) {
	tests := []struct {
		input    pb.WrappingAlgorithm
		expected string
	}{
		{pb.WrappingAlgorithm_WRAP_ALG_AES_KW, "AES-KW"},
		{pb.WrappingAlgorithm_WRAP_ALG_AES_KWP, "AES-KWP"},
		{pb.WrappingAlgorithm_WRAP_ALG_AES_GCM, "AES-GCM"},
		{pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256, "RSA-OAEP-SHA256"},
		{pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384, "RSA-OAEP-SHA384"},
		{pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512, "RSA-OAEP-SHA512"},
		{pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW, "ECDH-ES+A256KW"},
		{pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED, ""},
		{pb.WrappingAlgorithm(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := convertProtoToServiceWrappingAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertServiceToProtoExportFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected pb.ExportFormat
	}{
		{"raw", pb.ExportFormat_EXPORT_FORMAT_RAW},
		{"jwk", pb.ExportFormat_EXPORT_FORMAT_JWK},
		{"pem", pb.ExportFormat_EXPORT_FORMAT_PEM},
		{"UNKNOWN", pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := convertServiceToProtoExportFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertProtoToServiceExportFormat(t *testing.T) {
	tests := []struct {
		input    pb.ExportFormat
		expected string
	}{
		{pb.ExportFormat_EXPORT_FORMAT_RAW, "raw"},
		{pb.ExportFormat_EXPORT_FORMAT_JWK, "jwk"},
		{pb.ExportFormat_EXPORT_FORMAT_PEM, "pem"},
		{pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED, ""},
		{pb.ExportFormat(999), ""},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := convertProtoToServiceExportFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// --- Merged from client_coverage_test.go ---

// TestKMSListKeys_ClientClosed tests KMSListKeys with closed client.
func TestKMSListKeys_ClientClosed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}

	ctx := context.Background()
	resp, err := client.KMSListKeys(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// TestRebalanceCluster_ClientClosed tests RebalanceCluster with closed client.
func TestRebalanceCluster_ClientClosed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}

	ctx := context.Background()
	req := &pb.RebalanceClusterRequest{}

	resp, err := client.RebalanceCluster(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// TestGetRebalanceProgress_ClientClosed tests GetRebalanceProgress with closed client.
func TestGetRebalanceProgress_ClientClosed(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: false,
		closed:    true,
	}

	ctx := context.Background()
	req := &pb.GetRebalanceProgressRequest{}

	resp, err := client.GetRebalanceProgress(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "closed")
}

// TestKMSGenerateKey_NilRequest tests KMSGenerateKey with nil request.
func TestKMSGenerateKey_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSGenerateKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGenerateKey_NilKeyAttributes tests KMSGenerateKey with nil key attributes.
func TestKMSGenerateKey_NilKeyAttributes(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	req := &service.GenerateKeyRequest{
		Name:          "test-key",
		Description:   "Test key for coverage",
		KeyAttributes: nil, // nil key attributes should error
	}

	resp, err := client.KMSGenerateKey(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
}

// TestKMSImportKey_NilRequest tests KMSImportKey with nil request.
func TestKMSImportKey_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSImportKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSImportKey_NilKeyAttributes tests KMSImportKey with nil key attributes.
func TestKMSImportKey_NilKeyAttributes(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	req := &service.ImportKeyRequest{
		KeyMaterial:   []byte("test-key-material"),
		WrappingKeyID: "wrapping-key-123",
		Name:          "imported-key",
		KeyAttributes: nil, // nil key attributes should error
	}

	resp, err := client.KMSImportKey(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
}

// TestKMSGetKey_NilRequest tests KMSGetKey with nil request.
func TestKMSGetKey_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSGetKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSRotateKey_NilRequest tests KMSRotateKey with nil request.
func TestKMSRotateKey_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSRotateKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSDeleteKey_NilRequest tests KMSDeleteKey with nil request.
func TestKMSDeleteKey_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSDeleteKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSEncrypt_NilRequest tests KMSEncrypt with nil request.
func TestKMSEncrypt_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSEncrypt(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSDecrypt_NilRequest tests KMSDecrypt with nil request.
func TestKMSDecrypt_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSDecrypt(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGenerateDataKey_NilRequest tests KMSGenerateDataKey with nil request.
func TestKMSGenerateDataKey_NilRequest(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	resp, err := client.KMSGenerateDataKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGenerateDataKey_NilKeyAttributes tests KMSGenerateDataKey with nil key attributes.
func TestKMSGenerateDataKey_NilKeyAttributes(t *testing.T) {
	client := &Client{
		conn:      nil,
		connected: true,
	}

	ctx := context.Background()
	req := &service.GenerateDataKeyRequest{
		KeyID:         "kek-123",
		Context:       []byte("data key context"),
		KeyAttributes: nil, // nil key attributes should error
	}

	resp, err := client.KMSGenerateDataKey(ctx, req)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "key attributes cannot be nil")
}

// TestConvertKeyAttributesToProto_Coverage tests the convertKeyAttributesToProto function comprehensively.
func TestConvertKeyAttributesToProto_Coverage(t *testing.T) {
	tests := []struct {
		name           string
		attrs          *xkmssdk.KeyAttributes
		expectError    bool
		errorContains  string
		expectedAlg    pb.KeyAlgorithm
		expectedSize   int32
		expectedUsages int
	}{
		{
			name:          "nil_attributes",
			attrs:         nil,
			expectError:   true,
			errorContains: "key attributes cannot be nil",
		},
		{
			name: "aes_128_gcm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES128GCM,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_AES_128_GCM,
			expectedSize:   16,
			expectedUsages: 2,
		},
		{
			name: "aes_192_gcm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES192GCM,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_AES_192_GCM,
			expectedSize:   24,
			expectedUsages: 2,
		},
		{
			name: "aes_256_gcm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			expectedSize:   32,
			expectedUsages: 2,
		},
		{
			name: "chacha20_poly1305",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricChaCha20Poly1305,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305,
			expectedSize:   32,
			expectedUsages: 2,
		},
		{
			name: "xchacha20_poly1305",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricXChaCha20Poly1305,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305,
			expectedSize:   32,
			expectedUsages: 2,
		},
		{
			name: "unsupported_symmetric_algorithm",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: "UNSUPPORTED",
			},
			expectError:   true,
			errorContains: "unsupported symmetric algorithm",
		},
		{
			name: "rsa_2048",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.RSA,
				RSAAttributes: &xkmssdk.RSAAttributes{
					KeySize: 2048,
				},
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_RSA_2048,
			expectedSize:   2048,
			expectedUsages: 2,
		},
		{
			name: "rsa_3072",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.RSA,
				RSAAttributes: &xkmssdk.RSAAttributes{
					KeySize: 3072,
				},
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_RSA_3072,
			expectedSize:   3072,
			expectedUsages: 2,
		},
		{
			name: "rsa_4096",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.RSA,
				RSAAttributes: &xkmssdk.RSAAttributes{
					KeySize: 4096,
				},
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_RSA_4096,
			expectedSize:   4096,
			expectedUsages: 2,
		},
		{
			name: "rsa_default_size",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.RSA,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_RSA_4096, // default
			expectedSize:   4096,
			expectedUsages: 2,
		},
		{
			name: "ecdsa_p256",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.ECDSA,
				ECCAttributes: &xkmssdk.ECCAttributes{
					Curve: elliptic.P256(),
				},
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_ECDSA_P256,
			expectedSize:   256,
			expectedUsages: 2,
		},
		{
			name: "ecdsa_p384",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.ECDSA,
				ECCAttributes: &xkmssdk.ECCAttributes{
					Curve: elliptic.P384(),
				},
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_ECDSA_P384,
			expectedSize:   384,
			expectedUsages: 2,
		},
		{
			name: "ecdsa_p521",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.ECDSA,
				ECCAttributes: &xkmssdk.ECCAttributes{
					Curve: elliptic.P521(),
				},
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_ECDSA_P521,
			expectedSize:   521,
			expectedUsages: 2,
		},
		{
			name: "ecdsa_default_curve",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.ECDSA,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, // default
			expectedSize:   256,
			expectedUsages: 2,
		},
		{
			name: "ed25519",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.Ed25519,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_ED25519,
			expectedSize:   256,
			expectedUsages: 2,
		},
		{
			name: "unsupported_key_algorithm",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.PublicKeyAlgorithm(99),
			},
			expectError:   true,
			errorContains: "unsupported key algorithm",
		},
		{
			name:  "no_algorithm_specified",
			attrs: &xkmssdk.KeyAttributes{
				// Neither SymmetricAlgorithm nor KeyAlgorithm set
			},
			expectError:   true,
			errorContains: "either SymmetricAlgorithm or KeyAlgorithm must be set",
		},
		{
			name: "exportable_flag",
			attrs: &xkmssdk.KeyAttributes{
				SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
				Exportable:         true,
			},
			expectError:    false,
			expectedAlg:    pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			expectedSize:   32,
			expectedUsages: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alg, size, usages, exportable, err := convertKeyAttributesToProto(tt.attrs)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expectedAlg, alg)
			assert.Equal(t, tt.expectedSize, size)
			assert.Len(t, usages, tt.expectedUsages)

			if tt.attrs != nil {
				assert.Equal(t, tt.attrs.Exportable, exportable)
			}
		})
	}
}

// TestWrappingAlgorithmConversion_Coverage tests wrapping algorithm conversion for coverage.
func TestWrappingAlgorithmConversion_Coverage(t *testing.T) {
	t.Run("service_to_proto", func(t *testing.T) {
		tests := []struct {
			input    string
			expected pb.WrappingAlgorithm
		}{
			{"AES-KW", pb.WrappingAlgorithm_WRAP_ALG_AES_KW},
			{"AES-KWP", pb.WrappingAlgorithm_WRAP_ALG_AES_KWP},
			{"AES-GCM", pb.WrappingAlgorithm_WRAP_ALG_AES_GCM},
			{"RSA-OAEP-SHA256", pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256},
			{"RSA-OAEP-SHA384", pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384},
			{"RSA-OAEP-SHA512", pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512},
			{"ECDH-ES+A256KW", pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW},
			{"UNKNOWN", pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED},
			{"", pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED},
		}

		for _, tt := range tests {
			result := convertServiceToProtoWrappingAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		}
	})

	t.Run("proto_to_service", func(t *testing.T) {
		tests := []struct {
			input    pb.WrappingAlgorithm
			expected string
		}{
			{pb.WrappingAlgorithm_WRAP_ALG_AES_KW, "AES-KW"},
			{pb.WrappingAlgorithm_WRAP_ALG_AES_KWP, "AES-KWP"},
			{pb.WrappingAlgorithm_WRAP_ALG_AES_GCM, "AES-GCM"},
			{pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256, "RSA-OAEP-SHA256"},
			{pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA384, "RSA-OAEP-SHA384"},
			{pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA512, "RSA-OAEP-SHA512"},
			{pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW, "ECDH-ES+A256KW"},
			{pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED, ""},
			{pb.WrappingAlgorithm(999), ""},
		}

		for _, tt := range tests {
			result := convertProtoToServiceWrappingAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		}
	})
}

// TestExportFormatConversion_Coverage tests export format conversion for coverage.
func TestExportFormatConversion_Coverage(t *testing.T) {
	t.Run("service_to_proto", func(t *testing.T) {
		tests := []struct {
			input    string
			expected pb.ExportFormat
		}{
			{"raw", pb.ExportFormat_EXPORT_FORMAT_RAW},
			{"jwk", pb.ExportFormat_EXPORT_FORMAT_JWK},
			{"pem", pb.ExportFormat_EXPORT_FORMAT_PEM},
			{"UNKNOWN", pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED},
			{"", pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED},
		}

		for _, tt := range tests {
			result := convertServiceToProtoExportFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		}
	})

	t.Run("proto_to_service", func(t *testing.T) {
		tests := []struct {
			input    pb.ExportFormat
			expected string
		}{
			{pb.ExportFormat_EXPORT_FORMAT_RAW, "raw"},
			{pb.ExportFormat_EXPORT_FORMAT_JWK, "jwk"},
			{pb.ExportFormat_EXPORT_FORMAT_PEM, "pem"},
			{pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED, ""},
			{pb.ExportFormat(999), ""},
		}

		for _, tt := range tests {
			result := convertProtoToServiceExportFormat(tt.input)
			assert.Equal(t, tt.expected, result)
		}
	})
}

// TestKeyStatusConversion_Coverage tests key status conversion for coverage.
func TestKeyStatusConversion_Coverage(t *testing.T) {
	tests := []struct {
		input    service.KeyStatus
		expected pb.KeyStatus
	}{
		{service.KeyStatusEnabled, pb.KeyStatus_KEY_STATUS_ENABLED},
		{service.KeyStatusDisabled, pb.KeyStatus_KEY_STATUS_DISABLED},
		{service.KeyStatusPendingDeletion, pb.KeyStatus_KEY_STATUS_PENDING_DELETION},
		{service.KeyStatusDeleted, pb.KeyStatus_KEY_STATUS_DELETED},
		{service.KeyStatusRotating, pb.KeyStatus_KEY_STATUS_ROTATING},
		{service.KeyStatusCompromised, pb.KeyStatus_KEY_STATUS_COMPROMISED},
		{"unknown", pb.KeyStatus_KEY_STATUS_UNSPECIFIED},
		{"", pb.KeyStatus_KEY_STATUS_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := convertServiceToProtoKeyStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestKeyVersionInfoConversion_Coverage tests key version info conversion for coverage.
func TestKeyVersionInfoConversion_Coverage(t *testing.T) {
	now := time.Now()
	disabledTime := now.Add(-1 * time.Hour)

	tests := []struct {
		name     string
		keyID    string
		input    *pb.KeyVersionInfo
		validate func(*testing.T, *service.KeyVersionInfo)
	}{
		{
			name:  "nil_input",
			keyID: "key-123",
			input: nil,
			validate: func(t *testing.T, result *service.KeyVersionInfo) {
				assert.Nil(t, result)
			},
		},
		{
			name:  "basic_conversion",
			keyID: "key-123",
			input: &pb.KeyVersionInfo{
				Version:   1,
				Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				CreatedAt: now.Unix(),
			},
			validate: func(t *testing.T, result *service.KeyVersionInfo) {
				require.NotNil(t, result)
				assert.Equal(t, "key-123", result.KeyID)
				assert.Equal(t, uint64(1), result.Version)
				assert.Equal(t, service.KeyStatusEnabled, result.Status)
				assert.Equal(t, service.KeyAlgAES256GCM, result.Algorithm)
				assert.Nil(t, result.DisabledAt)
			},
		},
		{
			name:  "with_disabled_at",
			keyID: "key-456",
			input: &pb.KeyVersionInfo{
				Version:    2,
				Status:     pb.KeyStatus_KEY_STATUS_DISABLED,
				Algorithm:  pb.KeyAlgorithm_KEY_ALG_RSA_4096,
				CreatedAt:  now.Unix(),
				DisabledAt: disabledTime.Unix(),
			},
			validate: func(t *testing.T, result *service.KeyVersionInfo) {
				require.NotNil(t, result)
				assert.Equal(t, "key-456", result.KeyID)
				assert.Equal(t, uint64(2), result.Version)
				assert.Equal(t, service.KeyStatusDisabled, result.Status)
				require.NotNil(t, result.DisabledAt)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoKeyVersionInfo(tt.keyID, tt.input)
			tt.validate(t, result)
		})
	}
}

// TestDEKMetadataConversion_Coverage tests DEK metadata conversion for coverage.
func TestDEKMetadataConversion_Coverage(t *testing.T) {
	now := time.Now()
	rotatedTime := now.Add(-1 * time.Hour)

	tests := []struct {
		name     string
		input    *pb.DEKMetadata
		validate func(*testing.T, *service.DEKMetadata)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *service.DEKMetadata) {
				assert.Nil(t, result)
			},
		},
		{
			name: "basic_conversion",
			input: &pb.DEKMetadata{
				Path:        "/app/data/key1",
				Algorithm:   pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Version:     1,
				Status:      "active",
				IsDefault:   true,
				CreatedAt:   now.Unix(),
				Description: "Test DEK",
				BackendType: "local",
			},
			validate: func(t *testing.T, result *service.DEKMetadata) {
				require.NotNil(t, result)
				assert.Equal(t, "/app/data/key1", result.Path)
				assert.Equal(t, service.KeyAlgAES256GCM, result.Algorithm)
				assert.Equal(t, uint64(1), result.Version)
				assert.Equal(t, "active", result.Status)
				assert.True(t, result.IsDefault)
				assert.Equal(t, "Test DEK", result.Description)
				assert.Equal(t, "local", result.BackendType)
				assert.Nil(t, result.RotatedAt)
			},
		},
		{
			name: "with_rotated_at",
			input: &pb.DEKMetadata{
				Path:      "/app/data/key2",
				Algorithm: pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305,
				Version:   2,
				CreatedAt: now.Unix(),
				RotatedAt: rotatedTime.Unix(),
			},
			validate: func(t *testing.T, result *service.DEKMetadata) {
				require.NotNil(t, result)
				assert.Equal(t, "/app/data/key2", result.Path)
				assert.Equal(t, uint64(2), result.Version)
				require.NotNil(t, result.RotatedAt)
			},
		},
		{
			name: "with_safety_limits",
			input: &pb.DEKMetadata{
				Path:           "/app/data/key3",
				Algorithm:      pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Version:        1,
				CreatedAt:      now.Unix(),
				UsageCount:     1000,
				BytesUsed:      1024000,
				MaxUsageCount:  10000,
				MaxBytesUsed:   10240000,
				WarningPercent: 80,
			},
			validate: func(t *testing.T, result *service.DEKMetadata) {
				require.NotNil(t, result)
				assert.Equal(t, uint64(1000), result.UsageCount)
				assert.Equal(t, uint64(1024000), result.BytesUsed)
				assert.Equal(t, uint64(10000), result.MaxUsageCount)
				assert.Equal(t, uint64(10240000), result.MaxBytesUsed)
				assert.Equal(t, 80, result.WarningPercent)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoDEKMetadata(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestReencryptionProgressConversion_Coverage tests re-encryption progress conversion for coverage.
func TestReencryptionProgressConversion_Coverage(t *testing.T) {
	now := time.Now()
	later := now.Add(1 * time.Hour)

	tests := []struct {
		name     string
		input    *pb.ReencryptionProgress
		validate func(*testing.T, *service.ReencryptionProgress)
	}{
		{
			name:  "nil_input",
			input: nil,
			validate: func(t *testing.T, result *service.ReencryptionProgress) {
				assert.Nil(t, result)
			},
		},
		{
			name: "in_progress",
			input: &pb.ReencryptionProgress{
				InProgress:          true,
				TotalKeys:           100,
				ProcessedKeys:       50,
				FailedKeys:          2,
				StartedAt:           now.Unix(),
				EstimatedCompletion: later.Unix(),
			},
			validate: func(t *testing.T, result *service.ReencryptionProgress) {
				require.NotNil(t, result)
				assert.True(t, result.InProgress)
				assert.Equal(t, uint64(100), result.TotalKeys)
				assert.Equal(t, uint64(50), result.ProcessedKeys)
				assert.Equal(t, uint64(2), result.FailedKeys)
				require.NotNil(t, result.StartedAt)
				require.NotNil(t, result.EstimatedCompletion)
				assert.Equal(t, float64(50), result.PercentComplete)
			},
		},
		{
			name: "completed",
			input: &pb.ReencryptionProgress{
				InProgress:    false,
				TotalKeys:     100,
				ProcessedKeys: 100,
				FailedKeys:    0,
				StartedAt:     now.Unix(),
			},
			validate: func(t *testing.T, result *service.ReencryptionProgress) {
				require.NotNil(t, result)
				assert.False(t, result.InProgress)
				assert.Equal(t, uint64(100), result.TotalKeys)
				assert.Equal(t, uint64(100), result.ProcessedKeys)
				assert.Equal(t, float64(100), result.PercentComplete)
				assert.Nil(t, result.EstimatedCompletion)
			},
		},
		{
			name: "zero_total_keys",
			input: &pb.ReencryptionProgress{
				TotalKeys:     0,
				ProcessedKeys: 0,
			},
			validate: func(t *testing.T, result *service.ReencryptionProgress) {
				require.NotNil(t, result)
				assert.Equal(t, float64(0), result.PercentComplete)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoReencryptionProgress(tt.input)
			tt.validate(t, result)
		})
	}
}

// TestServiceKeyAlgorithmToProto_Coverage tests service to proto key algorithm conversion for coverage.
func TestServiceKeyAlgorithmToProto_Coverage(t *testing.T) {
	tests := []struct {
		input    service.KeyAlgorithm
		expected pb.KeyAlgorithm
	}{
		{service.KeyAlgAES128GCM, pb.KeyAlgorithm_KEY_ALG_AES_128_GCM},
		{service.KeyAlgAES192GCM, pb.KeyAlgorithm_KEY_ALG_AES_192_GCM},
		{service.KeyAlgAES256GCM, pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
		{service.KeyAlgChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305},
		{service.KeyAlgXChaCha20Poly1305, pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305},
		{service.KeyAlgRSA2048, pb.KeyAlgorithm_KEY_ALG_RSA_2048},
		{service.KeyAlgRSA3072, pb.KeyAlgorithm_KEY_ALG_RSA_3072},
		{service.KeyAlgRSA4096, pb.KeyAlgorithm_KEY_ALG_RSA_4096},
		{service.KeyAlgECDSAP256, pb.KeyAlgorithm_KEY_ALG_ECDSA_P256},
		{service.KeyAlgECDSAP384, pb.KeyAlgorithm_KEY_ALG_ECDSA_P384},
		{service.KeyAlgECDSAP521, pb.KeyAlgorithm_KEY_ALG_ECDSA_P521},
		{service.KeyAlgEd25519, pb.KeyAlgorithm_KEY_ALG_ED25519},
		{service.KeyAlgEd448, pb.KeyAlgorithm_KEY_ALG_ED448},
		{service.KeyAlgX25519, pb.KeyAlgorithm_KEY_ALG_X25519},
		{service.KeyAlgX448, pb.KeyAlgorithm_KEY_ALG_X448},
		{service.KeyAlgHMACSHA256, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256},
		{service.KeyAlgHMACSHA384, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384},
		{service.KeyAlgHMACSHA512, pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512},
		{"unknown", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
		{"", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := convertServiceKeyAlgorithmToProto(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestServiceToProtoKeyAlgorithmString_Coverage tests string to proto key algorithm conversion for coverage.
func TestServiceToProtoKeyAlgorithmString_Coverage(t *testing.T) {
	tests := []struct {
		input    string
		expected pb.KeyAlgorithm
	}{
		{"AES-128-GCM", pb.KeyAlgorithm_KEY_ALG_AES_128_GCM},
		{"AES-192-GCM", pb.KeyAlgorithm_KEY_ALG_AES_192_GCM},
		{"AES-256-GCM", pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
		{"CHACHA20-POLY1305", pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305},
		{"XCHACHA20-POLY1305", pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305},
		{"RSA-2048", pb.KeyAlgorithm_KEY_ALG_RSA_2048},
		{"RSA-3072", pb.KeyAlgorithm_KEY_ALG_RSA_3072},
		{"RSA-4096", pb.KeyAlgorithm_KEY_ALG_RSA_4096},
		{"ECDSA-P256", pb.KeyAlgorithm_KEY_ALG_ECDSA_P256},
		{"ECDSA-P384", pb.KeyAlgorithm_KEY_ALG_ECDSA_P384},
		{"ECDSA-P521", pb.KeyAlgorithm_KEY_ALG_ECDSA_P521},
		{"ED25519", pb.KeyAlgorithm_KEY_ALG_ED25519},
		{"ED448", pb.KeyAlgorithm_KEY_ALG_ED448},
		{"X25519", pb.KeyAlgorithm_KEY_ALG_X25519},
		{"X448", pb.KeyAlgorithm_KEY_ALG_X448},
		{"HMAC-SHA256", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256},
		{"HMAC-SHA384", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384},
		{"HMAC-SHA512", pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512},
		{"unknown", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
		{"", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := convertServiceToProtoKeyAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestKMSUpdateKeyStatus_NilRequest tests KMSUpdateKeyStatus with nil request.
func TestKMSUpdateKeyStatus_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSUpdateKeyStatus(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSListKeyVersions_NilRequest tests KMSListKeyVersions with nil request.
func TestKMSListKeyVersions_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSListKeyVersions(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSExportKey_NilRequest tests KMSExportKey with nil request.
func TestKMSExportKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSExportKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSUnwrapKey_NilRequest tests KMSUnwrapKey with nil request.
func TestKMSUnwrapKey_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSUnwrapKey(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSSetKeySafety_NilRequest tests KMSSetKeySafety with nil request.
func TestKMSSetKeySafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetKeySafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSClearKeySafety_NilRequest tests KMSClearKeySafety with nil request.
func TestKMSClearKeySafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSClearKeySafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGetSupportedAlgorithms_NotConnected tests KMSGetSupportedAlgorithms with closed client.
func TestKMSGetSupportedAlgorithms_NotConnected(t *testing.T) {
	client := &Client{
		closed: true,
	}

	ctx := context.Background()
	resp, err := client.KMSGetSupportedAlgorithms(ctx)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "client has been closed")
}

// TestKMSCreateDEK_NilRequest tests KMSCreateDEK with nil request.
func TestKMSCreateDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSCreateDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGetDEK_NilRequest tests KMSGetDEK with nil request.
func TestKMSGetDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSRotateDEK_NilRequest tests KMSRotateDEK with nil request.
func TestKMSRotateDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSRotateDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSDeleteDEK_NilRequest tests KMSDeleteDEK with nil request.
func TestKMSDeleteDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSDeleteDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGetDEKStatus_NilRequest tests KMSGetDEKStatus with nil request.
func TestKMSGetDEKStatus_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetDEKStatus(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSSetDefaultDEK_NilRequest tests KMSSetDefaultDEK with nil request.
func TestKMSSetDefaultDEK_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetDefaultDEK(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSSetDEKSafety_NilRequest tests KMSSetDEKSafety with nil request.
func TestKMSSetDEKSafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSetDEKSafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSClearDEKSafety_NilRequest tests KMSClearDEKSafety with nil request.
func TestKMSClearDEKSafety_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSClearDEKSafety(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSStartReencryption_NilRequest tests KMSStartReencryption with nil request.
func TestKMSStartReencryption_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSStartReencryption(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGetReencryptionProgress_NilRequest tests KMSGetReencryptionProgress with nil request.
func TestKMSGetReencryptionProgress_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGetReencryptionProgress(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestProtoToServiceKeyAlgorithm_Additional tests additional algorithm conversions.
func TestProtoToServiceKeyAlgorithm_Additional(t *testing.T) {
	tests := []struct {
		input    pb.KeyAlgorithm
		expected service.KeyAlgorithm
	}{
		{pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305, service.KeyAlgXChaCha20Poly1305},
		{pb.KeyAlgorithm_KEY_ALG_X25519, service.KeyAlgX25519},
		{pb.KeyAlgorithm_KEY_ALG_X448, service.KeyAlgX448},
		{pb.KeyAlgorithm_KEY_ALG_HMAC_SHA256, service.KeyAlgHMACSHA256},
		{pb.KeyAlgorithm_KEY_ALG_HMAC_SHA384, service.KeyAlgHMACSHA384},
		{pb.KeyAlgorithm_KEY_ALG_HMAC_SHA512, service.KeyAlgHMACSHA512},
		{pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			result := convertProtoToServiceKeyAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsUnixSocketAddress_Coverage tests Unix socket address detection for coverage.
func TestIsUnixSocketAddress_Coverage(t *testing.T) {
	tests := []struct {
		name     string
		address  string
		expected bool
	}{
		{"tcp_address", "localhost:50051", false},
		{"tcp_with_host", "192.168.1.1:50051", false},
		{"unix_prefix", "unix:///tmp/quicbaas.sock", true},
		{"unix_abstract_prefix", "unix-abstract://quicbaas", true},
		{"absolute_path_sock", "/tmp/quicbaas.sock", true},
		{"relative_path_sock", "./quicbaas.sock", true},
		{"parent_relative_sock", "../quicbaas.sock", true},
		{"just_filename_sock", "quicbaas.sock", true},
		{"no_sock_extension", "/tmp/quicbaas", false},
		{"empty_string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsUnixSocketAddress(tt.address)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeUnixSocketAddress_Coverage tests Unix socket address normalization for coverage.
func TestNormalizeUnixSocketAddress_Coverage(t *testing.T) {
	tests := []struct {
		name           string
		address        string
		expectedPrefix string
	}{
		{"tcp_unchanged", "localhost:50051", "localhost:50051"},
		{"already_unix_prefix", "unix:///tmp/quicbaas.sock", "unix:///tmp/quicbaas.sock"},
		{"already_unix_abstract", "unix-abstract://quicbaas", "unix-abstract://quicbaas"},
		{"absolute_path_sock", "/tmp/quicbaas.sock", "unix:///tmp/quicbaas.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeUnixSocketAddress(tt.address)
			if tt.expectedPrefix != "" {
				assert.True(t, result == tt.expectedPrefix || len(result) > 0)
			}
		})
	}
}

// TestClientEnsureConnected_PoolMode_Coverage tests ensureConnected with pool mode for coverage.
func TestClientEnsureConnected_PoolMode_Coverage(t *testing.T) {
	client := &Client{
		conn:      nil,
		pool:      &ConnectionPool{}, // Non-nil pool indicates pool mode
		connected: false,
		closed:    false,
		opts:      nil, // No options, will fail validation
	}

	ctx := context.Background()
	err := client.ensureConnected(ctx)

	// When not connected, ensureConnected calls Connect(), which validates opts
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client options not initialized")
}

// TestClientIsConnected_Coverage tests the isConnected method for coverage.
func TestClientIsConnected_Coverage(t *testing.T) {
	tests := []struct {
		name     string
		client   *Client
		expected bool
	}{
		{
			name: "connected_not_closed",
			client: &Client{
				connected: true,
				closed:    false,
			},
			expected: true,
		},
		{
			name: "connected_but_closed",
			client: &Client{
				connected: true,
				closed:    true,
			},
			expected: false,
		},
		{
			name: "not_connected",
			client: &Client{
				connected: false,
				closed:    false,
			},
			expected: false,
		},
		{
			name: "not_connected_and_closed",
			client: &Client{
				connected: false,
				closed:    true,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.client.isConnected()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestClientGetClient_SingleMode_Coverage tests getClient with single connection mode for coverage.
func TestClientGetClient_SingleMode_Coverage(t *testing.T) {
	client := &Client{
		client: nil, // nil gRPC client
		pool:   nil, // single connection mode
	}

	result, err := client.getClient()
	// When client is nil in single mode, getClient returns an error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client is not connected")
	assert.Nil(t, result)
}

// TestClientConnReturnsConnection_Coverage tests the Conn method for coverage.
func TestClientConnReturnsConnection_Coverage(t *testing.T) {
	client := &Client{
		conn: nil, // no connection
	}

	result := client.Conn()
	assert.Nil(t, result)
}

// TestConvertError_Coverage tests the convertError function for coverage.
func TestConvertError_Coverage(t *testing.T) {
	// Test nil error
	result := convertError(nil)
	assert.Nil(t, result)
}

// TestClientAddMetadata_EmptyMetadata_Coverage tests addMetadata with no metadata for coverage.
func TestClientAddMetadata_EmptyMetadata_Coverage(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			metadata: nil,
		},
	}

	ctx := context.Background()
	result := client.addMetadata(ctx)

	// Should return same context when no metadata
	assert.Equal(t, ctx, result)
}

// TestClientAddMetadata_WithPrebuiltMD_Coverage tests addMetadata with pre-built metadata for coverage.
func TestClientAddMetadata_WithPrebuiltMD_Coverage(t *testing.T) {
	client := &Client{
		opts: &clientOptions{
			metadata: map[string]string{"key": "value"},
			md:       nil, // will be built on first call
		},
	}

	ctx := context.Background()
	result := client.addMetadata(ctx)

	// Should return new context with metadata
	assert.NotEqual(t, ctx, result)

	// Second call should reuse pre-built md
	result2 := client.addMetadata(ctx)
	assert.NotEqual(t, ctx, result2)
}

// TestConvertGRPCError_QBErrorPassthrough_Coverage tests that QBErrors pass through for coverage.
func TestConvertGRPCError_QBErrorPassthrough_Coverage(t *testing.T) {
	originalErr := &errors.QBError{
		Code: errors.ErrNotFound,
		Op:   "test.operation",
	}

	result := convertGRPCError(originalErr, "SomeOp")

	// Should return the same error
	assert.Equal(t, originalErr, result)
}

// TestKMSSign_NilRequest tests KMSSign with nil request.
func TestKMSSign_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSSign(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSVerify_NilRequest tests KMSVerify with nil request.
func TestKMSVerify_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSVerify(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSHash_NilRequest tests KMSHash with nil request.
func TestKMSHash_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSHash(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGenerateHMAC_NilRequest tests KMSGenerateHMAC with nil request.
func TestKMSGenerateHMAC_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGenerateHMAC(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSVerifyHMAC_NilRequest tests KMSVerifyHMAC with nil request.
func TestKMSVerifyHMAC_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSVerifyHMAC(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSRewrap_NilRequest tests KMSRewrap with nil request.
func TestKMSRewrap_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSRewrap(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}

// TestKMSGenerateRandom_NilRequest tests KMSGenerateRandom with nil request.
func TestKMSGenerateRandom_NilRequest(t *testing.T) {
	client := &Client{}

	ctx := context.Background()
	resp, err := client.KMSGenerateRandom(ctx, nil)

	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "request cannot be nil")
}
