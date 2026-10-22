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
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// ExportKey Tests
// =============================================================================

func TestKMSServer_ExportKey(t *testing.T) {
	tests := []struct {
		name        string
		request     *pb.ExportKeyRequest
		mockSetup   func(m *mockKMSServiceForSafety)
		noService   bool
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.ExportKeyResponse)
	}{
		{
			name: "successful_export_raw",
			request: &pb.ExportKeyRequest{
				KeyId:             "export-key-123",
				WrappingKeyId:     "wrapping-key-456",
				WrappingAlgorithm: pb.WrappingAlgorithm_WRAP_ALG_AES_GCM,
				Format:            pb.ExportFormat_EXPORT_FORMAT_RAW,
				Version:           1,
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.exportKeyFn = func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
					assert.Equal(t, "export-key-123", req.KeyID)
					assert.Equal(t, "wrapping-key-456", req.WrappingKeyID)
					assert.Equal(t, "AES-GCM", req.WrappingAlgorithm)
					assert.Equal(t, "raw", req.Format)
					assert.Equal(t, uint64(1), req.Version)
					return &service.ExportKeyResponse{
						KeyID:         "export-key-123",
						Version:       1,
						WrappingKeyID: "wrapping-key-456",
						WrappedKey:    []byte("encrypted-key-material"),
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ExportKeyResponse) {
				assert.Equal(t, "export-key-123", resp.KeyId)
				assert.Equal(t, uint64(1), resp.Version)
				assert.Equal(t, pb.ExportFormat_EXPORT_FORMAT_RAW, resp.Format)
				assert.Equal(t, "wrapping-key-456", resp.WrappingKeyId)
				assert.Equal(t, pb.WrappingAlgorithm_WRAP_ALG_AES_GCM, resp.WrappingAlgorithm)
				assert.Equal(t, []byte("encrypted-key-material"), resp.WrappedMaterial)
				assert.Greater(t, resp.Timestamp, int64(0))
			},
		},
		{
			name: "successful_export_jwk_format",
			request: &pb.ExportKeyRequest{
				KeyId:             "jwk-key-123",
				WrappingKeyId:     "wrapping-key-789",
				WrappingAlgorithm: pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256,
				Format:            pb.ExportFormat_EXPORT_FORMAT_JWK,
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.exportKeyFn = func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
					assert.Equal(t, "jwk", req.Format)
					assert.Equal(t, "RSA-OAEP-SHA256", req.WrappingAlgorithm)
					return &service.ExportKeyResponse{
						KeyID:         "jwk-key-123",
						Version:       2,
						WrappingKeyID: "wrapping-key-789",
						WrappedKey:    []byte(`{"kty":"oct","k":"..."}`),
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ExportKeyResponse) {
				assert.Equal(t, pb.ExportFormat_EXPORT_FORMAT_JWK, resp.Format)
			},
		},
		{
			name: "successful_export_pem_format",
			request: &pb.ExportKeyRequest{
				KeyId:             "pem-key-123",
				WrappingKeyId:     "wrapping-key-abc",
				WrappingAlgorithm: pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW,
				Format:            pb.ExportFormat_EXPORT_FORMAT_PEM,
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.exportKeyFn = func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
					assert.Equal(t, "pem", req.Format)
					assert.Equal(t, "ECDH-ES+A256KW", req.WrappingAlgorithm)
					return &service.ExportKeyResponse{
						KeyID:         "pem-key-123",
						Version:       1,
						WrappingKeyID: "wrapping-key-abc",
						WrappedKey:    []byte("-----BEGIN ENCRYPTED PRIVATE KEY-----\n..."),
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.ExportKeyResponse) {
				assert.Equal(t, pb.ExportFormat_EXPORT_FORMAT_PEM, resp.Format)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.ExportKeyRequest{
				KeyId:         "",
				WrappingKeyId: "wrapping-key",
			},
			mockSetup:   func(m *mockKMSServiceForSafety) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_wrapping_key_id",
			request: &pb.ExportKeyRequest{
				KeyId:         "export-key",
				WrappingKeyId: "",
			},
			mockSetup:   func(m *mockKMSServiceForSafety) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_not_configured",
			request: &pb.ExportKeyRequest{
				KeyId:         "test-key",
				WrappingKeyId: "wrapping-key",
			},
			noService:   true,
			expectError: true,
			errorCode:   codes.Unimplemented,
		},
		{
			name: "service_error_key_not_found",
			request: &pb.ExportKeyRequest{
				KeyId:         "missing-key",
				WrappingKeyId: "wrapping-key",
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.exportKeyFn = func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
					return nil, errors.New(errors.ErrNotFound, "ExportKey", "key not found")
				}
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
		{
			name: "service_error_key_not_exportable",
			request: &pb.ExportKeyRequest{
				KeyId:         "non-exportable-key",
				WrappingKeyId: "wrapping-key",
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.exportKeyFn = func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
					return nil, errors.New(errors.ErrForbidden, "ExportKey", "key is not exportable")
				}
			},
			expectError: true,
			errorCode:   codes.PermissionDenied,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			if tt.noService {
				srv.kmsSvc = nil
			} else if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			resp, err := srv.ExportKey(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// =============================================================================
// UnwrapKey Tests
// =============================================================================

func TestKMSServer_UnwrapKey(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		request     *pb.UnwrapKeyRequest
		mockSetup   func(m *mockKMSServiceForSafety)
		noService   bool
		expectError bool
		errorCode   codes.Code
		validate    func(t *testing.T, resp *pb.UnwrapKeyResponse)
	}{
		{
			name: "successful_unwrap",
			request: &pb.UnwrapKeyRequest{
				KeyId:             "unwrapped-key-123",
				WrappedMaterial:   []byte("wrapped-key-material"),
				WrappingKeyId:     "wrapping-key-456",
				WrappingAlgorithm: pb.WrappingAlgorithm_WRAP_ALG_AES_GCM,
				Algorithm:         pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Name:              "My Unwrapped Key",
				SourceKeyId:       "original-key-789",
				SourceVersion:     3,
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.unwrapKeyFn = func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
					assert.Equal(t, "unwrapped-key-123", req.KeyID)
					assert.Equal(t, "wrapping-key-456", req.WrappingKeyID)
					assert.Equal(t, "AES-GCM", req.WrappingAlgorithm)
					assert.Equal(t, "AES-256-GCM", req.Algorithm)
					assert.Equal(t, "My Unwrapped Key", req.Name)
					assert.Equal(t, "original-key-789", req.SourceKeyID)
					assert.Equal(t, uint64(3), req.SourceVersion)
					return &service.UnwrapKeyResponse{
						KeyID:     "unwrapped-key-123",
						Algorithm: service.KeyAlgAES256GCM,
						Status:    service.KeyStatusEnabled,
						Version:   1,
						Key: &service.KeyHandle{
							KeyID:     "unwrapped-key-123",
							Version:   1,
							Algorithm: service.KeyAlgAES256GCM,
							Status:    service.KeyStatusEnabled,
							CreatedAt: now,
						},
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.UnwrapKeyResponse) {
				assert.NotNil(t, resp.Key)
				assert.Equal(t, "unwrapped-key-123", resp.Key.KeyId)
				assert.Equal(t, uint64(1), resp.Key.Version)
				assert.NotNil(t, resp.Metadata)
				assert.Equal(t, "unwrapped-key-123", resp.Metadata.KeyId)
				assert.Equal(t, "My Unwrapped Key", resp.Metadata.Name)
				assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, resp.Metadata.Algorithm)
				assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, resp.Metadata.Status)
				assert.Greater(t, resp.Timestamp, int64(0))
			},
		},
		{
			name: "successful_unwrap_rsa_oaep",
			request: &pb.UnwrapKeyRequest{
				KeyId:             "unwrapped-rsa-key",
				WrappedMaterial:   []byte("rsa-wrapped-material"),
				WrappingKeyId:     "rsa-wrapping-key",
				WrappingAlgorithm: pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256,
				Algorithm:         pb.KeyAlgorithm_KEY_ALG_RSA_4096,
				Name:              "RSA Signing Key",
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.unwrapKeyFn = func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
					assert.Equal(t, "RSA-OAEP-SHA256", req.WrappingAlgorithm)
					assert.Equal(t, "RSA-4096", req.Algorithm)
					return &service.UnwrapKeyResponse{
						KeyID:     "unwrapped-rsa-key",
						Algorithm: service.KeyAlgRSA4096,
						Status:    service.KeyStatusEnabled,
						Version:   1,
						Key: &service.KeyHandle{
							KeyID:     "unwrapped-rsa-key",
							Version:   1,
							Algorithm: service.KeyAlgRSA4096,
							Status:    service.KeyStatusEnabled,
							CreatedAt: now,
						},
					}, nil
				}
			},
			expectError: false,
			validate: func(t *testing.T, resp *pb.UnwrapKeyResponse) {
				assert.NotNil(t, resp.Key)
				assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_RSA_4096, resp.Metadata.Algorithm)
			},
		},
		{
			name: "missing_key_id",
			request: &pb.UnwrapKeyRequest{
				KeyId:           "",
				WrappedMaterial: []byte("material"),
				WrappingKeyId:   "wrapping-key",
			},
			mockSetup:   func(m *mockKMSServiceForSafety) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_wrapped_material",
			request: &pb.UnwrapKeyRequest{
				KeyId:           "test-key",
				WrappedMaterial: []byte{},
				WrappingKeyId:   "wrapping-key",
			},
			mockSetup:   func(m *mockKMSServiceForSafety) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "missing_wrapping_key_id",
			request: &pb.UnwrapKeyRequest{
				KeyId:           "test-key",
				WrappedMaterial: []byte("material"),
				WrappingKeyId:   "",
			},
			mockSetup:   func(m *mockKMSServiceForSafety) {},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_not_configured",
			request: &pb.UnwrapKeyRequest{
				KeyId:           "test-key",
				WrappedMaterial: []byte("material"),
				WrappingKeyId:   "wrapping-key",
			},
			noService:   true,
			expectError: true,
			errorCode:   codes.Unimplemented,
		},
		{
			name: "service_error_invalid_material",
			request: &pb.UnwrapKeyRequest{
				KeyId:           "test-key",
				WrappedMaterial: []byte("invalid-material"),
				WrappingKeyId:   "wrapping-key",
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.unwrapKeyFn = func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
					return nil, errors.New(errors.ErrInvalidInput, "UnwrapKey", "invalid wrapped key material")
				}
			},
			expectError: true,
			errorCode:   codes.InvalidArgument,
		},
		{
			name: "service_error_wrapping_key_not_found",
			request: &pb.UnwrapKeyRequest{
				KeyId:           "test-key",
				WrappedMaterial: []byte("material"),
				WrappingKeyId:   "missing-wrapping-key",
			},
			mockSetup: func(m *mockKMSServiceForSafety) {
				m.unwrapKeyFn = func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
					return nil, errors.New(errors.ErrNotFound, "UnwrapKey", "wrapping key not found")
				}
			},
			expectError: true,
			errorCode:   codes.NotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			if tt.noService {
				srv.kmsSvc = nil
			} else if tt.mockSetup != nil {
				tt.mockSetup(mockSvc)
			}

			resp, err := srv.UnwrapKey(context.Background(), tt.request)
			if tt.expectError {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, tt.errorCode, st.Code())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				if tt.validate != nil {
					tt.validate(t, resp)
				}
			}
		})
	}
}

// TestKMSServer_GenerateKey_KeyType checks that the key type reaching the KMS
// service is the service's default for the requested algorithm, since KeySpec
// carries no key type of its own.
func TestKMSServer_GenerateKey_KeyType(t *testing.T) {
	tests := []struct {
		name      string
		algorithm pb.KeyAlgorithm
		wantType  xkmssdk.KeyType
	}{
		{"symmetric_key_is_secret", pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, xkmssdk.KeyTypeSecret},
		{"asymmetric_key_signs", pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, xkmssdk.KeyTypeSigning},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			var got *service.GenerateKeyRequest
			mockSvc.generateKeyFn = func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				got = req
				return &service.GenerateKeyResponse{}, nil
			}

			_, err := srv.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
				Name:    "typed-key",
				KeySpec: &pb.KeySpec{Algorithm: tt.algorithm},
			})
			require.NoError(t, err)
			require.NotNil(t, got)
			require.NotNil(t, got.KeyAttributes)
			assert.Equal(t, tt.wantType, got.KeyAttributes.KeyType)
		})
	}

	t.Run("unspecified_algorithm_never_reaches_the_service", func(t *testing.T) {
		srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
		called := false
		mockSvc.generateKeyFn = func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
			called = true
			return &service.GenerateKeyResponse{}, nil
		}

		_, err := srv.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
			Name:    "typed-key",
			KeySpec: &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED},
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.False(t, called)
	})
}

func TestKMSServer_UnwrapKey_Exportable(t *testing.T) {
	for _, exportable := range []bool{true, false} {
		t.Run(fmt.Sprintf("exportable_%t", exportable), func(t *testing.T) {
			srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
			var got *service.UnwrapKeyRequest
			mockSvc.unwrapKeyFn = func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
				got = req
				return &service.UnwrapKeyResponse{KeyID: req.KeyID}, nil
			}

			_, err := srv.UnwrapKey(context.Background(), &pb.UnwrapKeyRequest{
				KeyId:           "unwrapped-key",
				WrappingKeyId:   "wrapping-key",
				WrappedMaterial: []byte("wrapped"),
				Exportable:      exportable,
			})
			require.NoError(t, err)
			require.NotNil(t, got)
			require.NotNil(t, got.KeyAttributes)
			assert.Equal(t, exportable, got.KeyAttributes.Exportable)
		})
	}

	t.Run("missing_wrapping_key_never_reaches_the_service", func(t *testing.T) {
		srv, mockSvc, _ := createTestKMSServerWithSafetyService(t)
		called := false
		mockSvc.unwrapKeyFn = func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
			called = true
			return &service.UnwrapKeyResponse{}, nil
		}

		_, err := srv.UnwrapKey(context.Background(), &pb.UnwrapKeyRequest{
			KeyId:           "unwrapped-key",
			WrappedMaterial: []byte("wrapped"),
			Exportable:      true,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.False(t, called, "the KMS service must not be called")
	})
}

// =============================================================================
// protoWrappingAlgorithmToString Tests
// =============================================================================

func TestProtoWrappingAlgorithmToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.WrappingAlgorithm
		expected string
	}{
		{"unspecified", pb.WrappingAlgorithm_WRAP_ALG_UNSPECIFIED, "AES-GCM"},
		{"aes_gcm", pb.WrappingAlgorithm_WRAP_ALG_AES_GCM, "AES-GCM"},
		{"rsa_oaep_sha256", pb.WrappingAlgorithm_WRAP_ALG_RSA_OAEP_SHA256, "RSA-OAEP-SHA256"},
		{"ecdh_es_a256kw", pb.WrappingAlgorithm_WRAP_ALG_ECDH_ES_A256KW, "ECDH-ES+A256KW"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoWrappingAlgorithmToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// protoExportFormatToString Tests
// =============================================================================

func TestProtoExportFormatToString(t *testing.T) {
	tests := []struct {
		name     string
		input    pb.ExportFormat
		expected string
	}{
		{"unspecified", pb.ExportFormat_EXPORT_FORMAT_UNSPECIFIED, "raw"},
		{"raw", pb.ExportFormat_EXPORT_FORMAT_RAW, "raw"},
		{"jwk", pb.ExportFormat_EXPORT_FORMAT_JWK, "jwk"},
		{"pem", pb.ExportFormat_EXPORT_FORMAT_PEM, "pem"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := protoExportFormatToString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
