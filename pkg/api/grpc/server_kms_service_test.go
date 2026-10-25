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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// Service Layer Tests - GenerateKey via kmsSvc
// =============================================================================

func TestKMSServer_GenerateKey_ServiceLayer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	t.Run("successful_generation_via_service_layer", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			GenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				assert.Equal(t, "svc-key", req.Name)
				assert.Equal(t, "test description", req.Description)
				assert.Equal(t, "test-owner", req.Owner)
				return &service.GenerateKeyResponse{
					Key: &service.KeyHandle{
						KeyID:     "svc-key",
						Version:   1,
						Algorithm: service.KeyAlgAES256GCM,
						Status:    service.KeyStatusEnabled,
						CreatedAt: now,
					},
					Metadata: &service.KeyMetadata{
						KeyID:       "svc-key",
						Name:        "svc-key",
						Description: "test description",
						Algorithm:   service.KeyAlgAES256GCM,
						Status:      service.KeyStatusEnabled,
						Version:     1,
						CreatedAt:   now,
						UpdatedAt:   now,
						Tags:        map[string]string{"env": "test"},
						Owner:       "test-owner",
					},
					Timestamp: now,
				}, nil
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		resp, err := server.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
			Name:        "svc-key",
			Description: "test description",
			KeySpec: &pb.KeySpec{
				Algorithm:          pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
				Usage:              []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT},
				Exportable:         true,
				DeletionProtection: false,
			},
			Tags:  map[string]string{"env": "test"},
			Owner: "test-owner",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "svc-key", resp.Key.KeyId)
		assert.Equal(t, uint64(1), resp.Key.Version)
		assert.Equal(t, "svc-key", resp.Metadata.Name)
		assert.Equal(t, "test description", resp.Metadata.Description)
	})

	t.Run("service_layer_error_propagated", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			GenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				return nil, errors.New(errors.ErrForbidden, "GenerateKey", "insufficient permissions")
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		_, err := server.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
			Name: "forbidden-key",
			KeySpec: &pb.KeySpec{
				Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
			},
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.PermissionDenied, st.Code())
	})

	t.Run("service_layer_invalid_algorithm_returns_error", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		_, err := server.GenerateKey(context.Background(), &pb.GenerateKeyRequest{
			Name: "bad-alg-key",
			KeySpec: &pb.KeySpec{
				Algorithm: pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED,
			},
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})
}

// =============================================================================
// Service Layer Tests - GetKey via kmsSvc
// =============================================================================

func TestKMSServer_GetKey_ServiceLayer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	t.Run("successful_get_via_service_layer", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			GetKeyFunc: func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
				assert.Equal(t, "my-key", req.KeyID)
				assert.Equal(t, uint64(2), req.Version)
				return &service.GetKeyResponse{
					Key: &service.KeyHandle{
						KeyID:     "my-key",
						Version:   2,
						Algorithm: service.KeyAlgRSA2048,
						Status:    service.KeyStatusEnabled,
						CreatedAt: now,
					},
					Metadata: &service.KeyMetadata{
						KeyID:     "my-key",
						Name:      "My Key",
						Algorithm: service.KeyAlgRSA2048,
						Status:    service.KeyStatusEnabled,
						Version:   2,
						CreatedAt: now,
						UpdatedAt: now,
					},
					Timestamp: now,
				}, nil
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		resp, err := server.GetKey(context.Background(), &pb.GetKeyRequest{
			KeyId:   "my-key",
			Version: 2,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "my-key", resp.Key.KeyId)
		assert.Equal(t, uint64(2), resp.Key.Version)
	})

	t.Run("service_layer_not_found_error", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			GetKeyFunc: func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetKey", "key not found")
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		_, err := server.GetKey(context.Background(), &pb.GetKeyRequest{
			KeyId: "nonexistent",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// Service Layer Tests - ListKeys via kmsSvc
// =============================================================================

func TestKMSServer_ListKeys_ServiceLayer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	t.Run("successful_list_via_service_layer", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			ListKeysFunc: func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
				assert.Equal(t, 25, req.Limit)
				assert.Equal(t, "cursor-token", req.Cursor)
				assert.Equal(t, "app-", req.Filter["prefix"])
				return &service.ListKeysResponse{
					Keys: []*service.KeyHandle{
						{KeyID: "app-key-1", Version: 1, Algorithm: service.KeyAlgAES256GCM, Status: service.KeyStatusEnabled, CreatedAt: now},
						{KeyID: "app-key-2", Version: 3, Algorithm: service.KeyAlgRSA4096, Status: service.KeyStatusDisabled, CreatedAt: now},
					},
					NextCursor: "next-cursor",
					HasMore:    true,
					TotalCount: 50,
					Timestamp:  now,
				}, nil
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		resp, err := server.ListKeys(context.Background(), &pb.ListKeysRequest{
			Limit:  25,
			Cursor: "cursor-token",
			Filter: map[string]string{"prefix": "app-"},
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Keys, 2)
		assert.Equal(t, "app-key-1", resp.Keys[0].KeyId)
		assert.Equal(t, "app-key-2", resp.Keys[1].KeyId)
		assert.Equal(t, "next-cursor", resp.NextCursor)
		assert.True(t, resp.HasMore)
		assert.Equal(t, int32(50), resp.TotalCount)
	})

	t.Run("service_layer_error_on_list", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			ListKeysFunc: func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
				return nil, errors.New(errors.ErrUnavailable, "ListKeys", "service unavailable")
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		_, err := server.ListKeys(context.Background(), &pb.ListKeysRequest{
			Limit: 10,
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
	})
}

// =============================================================================
// Service Layer Tests - RotateKey via kmsSvc
// =============================================================================

func TestKMSServer_RotateKey_ServiceLayer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	t.Run("successful_rotation_via_service_layer", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			RotateKeyFunc: func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
				assert.Equal(t, "rotate-me", req.KeyID)
				assert.True(t, req.RetainOldVersion)
				assert.Equal(t, int64(3600), req.GracePeriodSeconds)
				return &service.RotateKeyResponse{
					KeyID:                "rotate-me",
					OldVersion:           3,
					NewVersion:           4,
					Timestamp:            now,
					GracePeriodDisableAt: now.Add(time.Hour).Unix(),
					GracePeriodJobID:     "job-123",
				}, nil
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		resp, err := server.RotateKey(context.Background(), &pb.RotateKeyRequest{
			KeyId:              "rotate-me",
			RetainOldVersion:   true,
			GracePeriodSeconds: 3600,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "rotate-me", resp.KeyId)
		assert.Equal(t, uint64(3), resp.OldVersion)
		assert.Equal(t, uint64(4), resp.NewVersion)
		assert.NotZero(t, resp.GracePeriodDisableAt)
		assert.Equal(t, "job-123", resp.GracePeriodJobId)
	})

	t.Run("service_layer_rotation_error", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			RotateKeyFunc: func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "RotateKey", "key not found")
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		_, err := server.RotateKey(context.Background(), &pb.RotateKeyRequest{
			KeyId: "missing-key",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}

// =============================================================================
// Service Layer Tests - ListKeyVersions via kmsSvc
// =============================================================================

func TestKMSServer_ListKeyVersions_ServiceLayer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()
	disabledAt := now.Add(-24 * time.Hour)

	t.Run("successful_list_versions_via_service_layer", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			ListKeyVersionsFunc: func(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
				assert.Equal(t, "versioned-key", req.KeyID)
				assert.Equal(t, 50, req.Limit)
				return &service.ListKeyVersionsResponse{
					KeyID: "versioned-key",
					Versions: []*service.KeyVersionInfo{
						{
							Version:    1,
							Status:     service.KeyStatusDisabled,
							Algorithm:  service.KeyAlgAES256GCM,
							CreatedAt:  now.Add(-48 * time.Hour),
							DisabledAt: &disabledAt,
						},
						{
							Version:   2,
							Status:    service.KeyStatusEnabled,
							Algorithm: service.KeyAlgAES256GCM,
							CreatedAt: now,
						},
					},
					NextCursor: "v-cursor",
					HasMore:    false,
					Timestamp:  now,
				}, nil
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		resp, err := server.ListKeyVersions(context.Background(), &pb.ListKeyVersionsRequest{
			KeyId: "versioned-key",
			Limit: 50,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "versioned-key", resp.KeyId)
		assert.Len(t, resp.Versions, 2)
		assert.Equal(t, uint64(1), resp.Versions[0].Version)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_DISABLED, resp.Versions[0].Status)
		assert.NotZero(t, resp.Versions[0].DisabledAt)
		assert.Equal(t, uint64(2), resp.Versions[1].Version)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, resp.Versions[1].Status)
		assert.Equal(t, "v-cursor", resp.NextCursor)
	})

	t.Run("service_layer_list_versions_error", func(t *testing.T) {
		mockMgr := &mockKMSManager{}
		mockSvc := &mocks.MockKMSService{
			ListKeyVersionsFunc: func(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
				return nil, errors.New(errors.ErrTimeout, "ListKeyVersions", "request timed out")
			},
		}
		server := NewKMSServerWithService(mockMgr, mockSvc, log)

		_, err := server.ListKeyVersions(context.Background(), &pb.ListKeyVersionsRequest{
			KeyId: "timeout-key",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.DeadlineExceeded, st.Code())
	})
}

// =============================================================================
// GetKey version validation tests (direct KMS manager path)
// =============================================================================

func TestKMSServer_GetKey_VersionValidation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{
				KeyID:     keyID,
				Name:      "versioned-key",
				Algorithm: kms.AlgAES256GCM,
				State:     kms.KeyStateEnabled,
				Version:   3,
				Created:   now,
				Updated:   now,
			}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	t.Run("requested_version_exceeds_current_returns_not_found", func(t *testing.T) {
		_, err := server.GetKey(context.Background(), &pb.GetKeyRequest{
			KeyId:   "versioned-key",
			Version: 10,
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		assert.Contains(t, st.Message(), "version 10 not found")
	})

	t.Run("older_version_returns_disabled_status", func(t *testing.T) {
		resp, err := server.GetKey(context.Background(), &pb.GetKeyRequest{
			KeyId:   "versioned-key",
			Version: 1,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(1), resp.Key.Version)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_DISABLED, resp.Key.Status)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_DISABLED, resp.Metadata.Status)
	})

	t.Run("current_version_returns_actual_status", func(t *testing.T) {
		resp, err := server.GetKey(context.Background(), &pb.GetKeyRequest{
			KeyId:   "versioned-key",
			Version: 3,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(3), resp.Key.Version)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, resp.Key.Status)
	})

	t.Run("zero_version_returns_current", func(t *testing.T) {
		resp, err := server.GetKey(context.Background(), &pb.GetKeyRequest{
			KeyId:   "versioned-key",
			Version: 0,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, uint64(3), resp.Key.Version)
		assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, resp.Key.Status)
	})
}

// =============================================================================
// DeleteKey force path
// =============================================================================

func TestKMSServer_DeleteKey_ForceTrue(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	disableCalled := false
	deleteCalled := false

	mockMgr := &mockKMSManager{
		disableKeyFunc: func(ctx context.Context, keyID string) error {
			disableCalled = true
			return nil
		},
		deleteKeyFunc: func(ctx context.Context, keyID string) error {
			deleteCalled = true
			return nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	t.Run("force_delete_skips_disable", func(t *testing.T) {
		resp, err := server.DeleteKey(context.Background(), &pb.DeleteKeyRequest{
			KeyId: "force-delete-key",
			Force: true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.False(t, disableCalled, "disable should not be called when Force=true")
		assert.True(t, deleteCalled, "delete should be called")
	})
}

func TestKMSServer_DeleteKey_NonForce(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})

	disableCalled := false
	deleteCalled := false

	mockMgr := &mockKMSManager{
		disableKeyFunc: func(ctx context.Context, keyID string) error {
			disableCalled = true
			return nil
		},
		deleteKeyFunc: func(ctx context.Context, keyID string) error {
			deleteCalled = true
			return nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	t.Run("non_force_delete_calls_disable_first", func(t *testing.T) {
		resp, err := server.DeleteKey(context.Background(), &pb.DeleteKeyRequest{
			KeyId: "graceful-delete-key",
			Force: false,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, disableCalled, "disable should be called when Force=false")
		assert.True(t, deleteCalled, "delete should be called after disable")
	})
}

// =============================================================================
// ListKeys pagination with limit truncation (direct KMS manager)
// =============================================================================

func TestKMSServer_ListKeys_PaginationTruncation(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		listKeysFunc: func(ctx context.Context, prefix string) ([]interface{}, error) {
			results := make([]interface{}, 5)
			for i := range results {
				results[i] = &kms.KeyMetadata{
					KeyID:     "key-" + string(rune('a'+i)),
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   1,
					Created:   now,
					Updated:   now,
				}
			}
			return results, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	t.Run("limit_truncates_results_and_sets_has_more", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &pb.ListKeysRequest{
			Limit: 3,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Keys, 3)
		assert.True(t, resp.HasMore)
		assert.Equal(t, int32(5), resp.TotalCount)
	})

	t.Run("limit_greater_than_results_returns_all", func(t *testing.T) {
		resp, err := server.ListKeys(context.Background(), &pb.ListKeysRequest{
			Limit: 100,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Keys, 5)
		assert.False(t, resp.HasMore)
	})

	t.Run("prefix_filter_passed_to_manager", func(t *testing.T) {
		prefixReceived := ""
		mockMgr2 := &mockKMSManager{
			listKeysFunc: func(ctx context.Context, prefix string) ([]interface{}, error) {
				prefixReceived = prefix
				return []interface{}{}, nil
			},
		}
		server2 := NewKMSServer(mockMgr2, log)

		_, err := server2.ListKeys(context.Background(), &pb.ListKeysRequest{
			Filter: map[string]string{"prefix": "app/"},
		})

		require.NoError(t, err)
		assert.Equal(t, "app/", prefixReceived)
	})
}

// =============================================================================
// Converter Tests - protoToKeyAttributes (full branch coverage)
// =============================================================================

func TestProtoToKeyAttributes(t *testing.T) {
	tests := []struct {
		name              string
		algorithm         pb.KeyAlgorithm
		expectedKeyType   xkmssdk.KeyType
		checkSymmetric    bool
		expectedSymAlg    xkmssdk.SymmetricAlgorithm
		checkAsymmetric   bool
		expectedKeyAlg    x509.PublicKeyAlgorithm
		expectedRSASize   int
		expectedCurve     elliptic.Curve
		expectError       bool
		expectedErrSubstr string
	}{
		{"aes_128_gcm", pb.KeyAlgorithm_KEY_ALG_AES_128_GCM, xkmssdk.KeyTypeEncryption, true, xkmssdk.SymmetricAES128GCM, false, 0, 0, nil, false, ""},
		{"aes_192_gcm", pb.KeyAlgorithm_KEY_ALG_AES_192_GCM, xkmssdk.KeyTypeEncryption, true, xkmssdk.SymmetricAES192GCM, false, 0, 0, nil, false, ""},
		{"aes_256_gcm", pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, xkmssdk.KeyTypeEncryption, true, xkmssdk.SymmetricAES256GCM, false, 0, 0, nil, false, ""},
		{"chacha20_poly1305", pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305, xkmssdk.KeyTypeEncryption, true, xkmssdk.SymmetricChaCha20Poly1305, false, 0, 0, nil, false, ""},
		{"xchacha20_poly1305", pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305, xkmssdk.KeyTypeEncryption, true, xkmssdk.SymmetricXChaCha20Poly1305, false, 0, 0, nil, false, ""},
		{"rsa_2048", pb.KeyAlgorithm_KEY_ALG_RSA_2048, xkmssdk.KeyTypeSigning, false, "", true, x509.RSA, 2048, nil, false, ""},
		{"rsa_3072", pb.KeyAlgorithm_KEY_ALG_RSA_3072, xkmssdk.KeyTypeSigning, false, "", true, x509.RSA, 3072, nil, false, ""},
		{"rsa_4096", pb.KeyAlgorithm_KEY_ALG_RSA_4096, xkmssdk.KeyTypeSigning, false, "", true, x509.RSA, 4096, nil, false, ""},
		{"ecdsa_p256", pb.KeyAlgorithm_KEY_ALG_ECDSA_P256, xkmssdk.KeyTypeSigning, false, "", true, x509.ECDSA, 0, elliptic.P256(), false, ""},
		{"ecdsa_p384", pb.KeyAlgorithm_KEY_ALG_ECDSA_P384, xkmssdk.KeyTypeSigning, false, "", true, x509.ECDSA, 0, elliptic.P384(), false, ""},
		{"ecdsa_p521", pb.KeyAlgorithm_KEY_ALG_ECDSA_P521, xkmssdk.KeyTypeSigning, false, "", true, x509.ECDSA, 0, elliptic.P521(), false, ""},
		{"ed25519", pb.KeyAlgorithm_KEY_ALG_ED25519, xkmssdk.KeyTypeSigning, false, "", true, x509.Ed25519, 0, nil, false, ""},
		{"unspecified_returns_error", pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED, 0, false, "", false, 0, 0, nil, true, "key algorithm"},
		{"unsupported_returns_error", pb.KeyAlgorithm(9999), 0, false, "", false, 0, 0, nil, true, "unsupported key algorithm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &pb.GenerateKeyRequest{
				KeySpec: &pb.KeySpec{
					Algorithm:  tt.algorithm,
					Exportable: true,
				},
			}

			attrs, err := protoToKeyAttributes("test-key-id", req.KeySpec)

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedErrSubstr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, attrs)
			assert.Equal(t, "test-key-id", attrs.CN)
			assert.True(t, attrs.Exportable)
			assert.Equal(t, tt.expectedKeyType, attrs.KeyType)

			if tt.checkSymmetric {
				assert.Equal(t, tt.expectedSymAlg, attrs.SymmetricAlgorithm)
			}

			if tt.checkAsymmetric {
				assert.Equal(t, tt.expectedKeyAlg, attrs.KeyAlgorithm)
				if tt.expectedRSASize > 0 {
					require.NotNil(t, attrs.RSAAttributes)
					assert.Equal(t, tt.expectedRSASize, attrs.RSAAttributes.KeySize)
				}
				if tt.expectedCurve != nil {
					require.NotNil(t, attrs.ECCAttributes)
					assert.Equal(t, tt.expectedCurve, attrs.ECCAttributes.Curve)
				}
			}
		})
	}
}

// =============================================================================
// Converter Tests - protoKeySpecToKeyAttributes (from server_crypto.go)
// =============================================================================

func TestProtoKeySpecToKeyAttributes(t *testing.T) {
	tests := []struct {
		name           string
		algorithm      pb.KeyAlgorithm
		expectedSymAlg xkmssdk.SymmetricAlgorithm
	}{
		{"aes_128_gcm", pb.KeyAlgorithm_KEY_ALG_AES_128_GCM, xkmssdk.SymmetricAES128GCM},
		{"aes_192_gcm", pb.KeyAlgorithm_KEY_ALG_AES_192_GCM, xkmssdk.SymmetricAES192GCM},
		{"aes_256_gcm", pb.KeyAlgorithm_KEY_ALG_AES_256_GCM, xkmssdk.SymmetricAES256GCM},
		{"chacha20_poly1305", pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305, xkmssdk.SymmetricChaCha20Poly1305},
		{"xchacha20_poly1305", pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305, xkmssdk.SymmetricXChaCha20Poly1305},
		{"default_unsupported", pb.KeyAlgorithm_KEY_ALG_RSA_2048, xkmssdk.SymmetricAES256GCM},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &pb.KeySpec{
				Algorithm:  tt.algorithm,
				Exportable: true,
			}
			attrs := protoKeySpecToKeyAttributes(spec)
			require.NotNil(t, attrs)
			assert.Equal(t, tt.expectedSymAlg, attrs.SymmetricAlgorithm)
			assert.Equal(t, xkmssdk.KeyTypeEncryption, attrs.KeyType)
			assert.True(t, attrs.Exportable)
		})
	}
}

// =============================================================================
// Converter Tests - missing hash algorithm branches
// =============================================================================

func TestConvertProtoHashAlgorithm_MissingBranches(t *testing.T) {
	tests := []struct {
		name     string
		proto    pb.HashAlgorithm
		expected service.HashAlgorithm
	}{
		{"sha3_384", pb.HashAlgorithm_HASH_ALG_SHA3_384, service.HashAlgSHA3_384},
		{"sha3_512", pb.HashAlgorithm_HASH_ALG_SHA3_512, service.HashAlgSHA3_512},
		{"blake2b_512", pb.HashAlgorithm_HASH_ALG_BLAKE2B_512, service.HashAlgBLAKE2b512},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertProtoHashAlgorithm(tt.proto)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertServiceHashAlgorithm_MissingBranches(t *testing.T) {
	tests := []struct {
		name     string
		svc      service.HashAlgorithm
		expected pb.HashAlgorithm
	}{
		{"sha3_256", service.HashAlgSHA3_256, pb.HashAlgorithm_HASH_ALG_SHA3_256},
		{"sha3_384", service.HashAlgSHA3_384, pb.HashAlgorithm_HASH_ALG_SHA3_384},
		{"sha3_512", service.HashAlgSHA3_512, pb.HashAlgorithm_HASH_ALG_SHA3_512},
		{"blake2b_256", service.HashAlgBLAKE2b256, pb.HashAlgorithm_HASH_ALG_BLAKE2B_256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertServiceHashAlgorithm(tt.svc)
			assert.Equal(t, tt.expected, result)
		})
	}
}
