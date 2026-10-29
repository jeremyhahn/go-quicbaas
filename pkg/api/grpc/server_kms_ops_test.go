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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
)

// =============================================================================
// ImportKey Tests
// =============================================================================

func TestKMSServer_ImportKey_Success(t *testing.T) {
	var importedID string
	mockMgr := &mockKMSManager{
		importKeyFunc: func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
			importedID = keyID
			assert.Equal(t, []byte("key-material"), material)
			assert.Equal(t, "AES-256-GCM", algorithm)
			return &kms.KeyMetadata{KeyID: keyID, Algorithm: kms.AlgAES256GCM, State: kms.KeyStateEnabled, Version: 1}, nil
		},
	}
	server := newServiceBackedKMSServer(t, mockMgr)

	resp, err := server.ImportKey(importTenantContext(), &pb.ImportKeyRequest{
		Name:        "import-key",
		Description: "imported key",
		KeyMaterial: []byte("key-material"),
		KeySpec: &pb.KeySpec{
			Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
		},
		Tags: map[string]string{"env": "test"},
	})

	require.NoError(t, err)
	assert.Equal(t, "tenant."+kmstenant.OwnerTenant+".import-key", importedID, "the manager must receive the tenant-scoped key ID")
	assert.Equal(t, "import-key", resp.GetKey().GetKeyId())
	assert.Equal(t, "imported key", resp.GetMetadata().GetDescription())
	assert.Equal(t, map[string]string{"env": "test"}, resp.GetMetadata().GetTags())
}

func TestKMSServer_ImportKey_MissingKeyMaterial(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.ImportKey(context.Background(), &pb.ImportKeyRequest{
		Name:    "import-key",
		KeySpec: &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "key_material")
}

func TestKMSServer_ImportKey_MissingKeySpec(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.ImportKey(context.Background(), &pb.ImportKeyRequest{
		Name:        "import-key",
		KeyMaterial: []byte("material"),
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "key_spec")
}

func TestKMSServer_ImportKey_ManagerError(t *testing.T) {
	mockMgr := &mockKMSManager{
		importKeyFunc: func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
			return nil, errors.New(errors.ErrAlreadyExists, "ImportKey", "key already exists")
		},
	}
	server := newServiceBackedKMSServer(t, mockMgr)

	_, err := server.ImportKey(importTenantContext(), &pb.ImportKeyRequest{
		Name:        "import-key",
		KeyMaterial: []byte("material"),
		KeySpec:     &pb.KeySpec{Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM},
	})

	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err),
		"the service keeps the manager's classified failure, so importing an existing key must answer AlreadyExists")
}

// newServiceBackedKMSServer wires mgr behind the service layer, the only
// path ImportKey takes (#1992).
func newServiceBackedKMSServer(t *testing.T, mgr *mockKMSManager) *KMSServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	// The KMS service records which tenant owns each key in this store.
	ownershipStore := qrdbsdk.NewMemoryKVStore()
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&importRaftNode{}).
		WithKMSManager(mgr).
		WithKVStoreProvider(func(uint64) interface{} { return ownershipStore }).
		Build()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, svc.Close()) })
	return NewKMSServerWithService(mgr, svc.KMS(), log)
}

// importRaftNode satisfies the service builder; the KMS service never
// touches Raft.
type importRaftNode struct {
	interfaces.RaftNode
}

// =============================================================================
// UpdateKeyMetadata Tests
// =============================================================================

func TestKMSServer_UpdateKeyMetadata_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{
				KeyID:       keyID,
				Name:        "original-name",
				Description: "original desc",
				Algorithm:   kms.AlgAES256GCM,
				State:       kms.KeyStateEnabled,
				Version:     1,
				Created:     now,
				Updated:     now,
				Tags:        map[string]string{"old-tag": "old-value"},
			}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.UpdateKeyMetadata(context.Background(), &pb.UpdateKeyMetadataRequest{
		KeyId:       "meta-key",
		Name:        "new-name",
		Description: "new description",
		Tags:        map[string]string{"new-tag": "new-value"},
		RemoveTags:  []string{"old-tag"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Metadata)
	assert.Equal(t, "new-name", resp.Metadata.Name)
	assert.Equal(t, "new description", resp.Metadata.Description)
}

func TestKMSServer_UpdateKeyMetadata_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.UpdateKeyMetadata(context.Background(), &pb.UpdateKeyMetadataRequest{
		KeyId: "",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestKMSServer_UpdateKeyMetadata_NilTags(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{
				KeyID:     keyID,
				Algorithm: kms.AlgAES256GCM,
				State:     kms.KeyStateEnabled,
				Version:   1,
				Created:   now,
				Updated:   now,
				Tags:      nil, // nil tags to trigger initialization
			}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.UpdateKeyMetadata(context.Background(), &pb.UpdateKeyMetadataRequest{
		KeyId: "meta-key",
		Tags:  map[string]string{"new-tag": "value"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKMSServer_UpdateKeyMetadata_PersistError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{
				KeyID:     keyID,
				Algorithm: kms.AlgAES256GCM,
				State:     kms.KeyStateEnabled,
				Version:   1,
				Created:   now,
				Updated:   now,
			}, nil
		},
		updateStoredMetadataFunc: func(metadata interface{}) error {
			return errors.New(errors.ErrInternal, "UpdateStoredMetadata", "storage error")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.UpdateKeyMetadata(context.Background(), &pb.UpdateKeyMetadataRequest{
		KeyId: "meta-key",
		Name:  "new-name",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// GetKeyMetadata Tests
// =============================================================================

func TestKMSServer_GetKeyMetadata_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{
				KeyID:     keyID,
				Name:      "my-key",
				Algorithm: kms.AlgAES256GCM,
				State:     kms.KeyStateEnabled,
				Version:   2,
				Created:   now,
				Updated:   now,
			}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.GetKeyMetadata(context.Background(), &pb.GetKeyMetadataRequest{
		KeyId: "my-key",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Metadata)
}

func TestKMSServer_GetKeyMetadata_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.GetKeyMetadata(context.Background(), &pb.GetKeyMetadataRequest{KeyId: ""})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestKMSServer_GetKeyMetadata_NotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return nil, errors.New(errors.ErrNotFound, "GetKey", "key not found")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.GetKeyMetadata(context.Background(), &pb.GetKeyMetadataRequest{KeyId: "missing"})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// =============================================================================
// UpdateKeyStatus Tests
// =============================================================================

func TestKMSServer_UpdateKeyStatus_Enable(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	enableCalled := false
	mockMgr := &mockKMSManager{
		enableKeyFunc: func(ctx context.Context, keyID string) error {
			enableCalled = true
			assert.Equal(t, "my-key", keyID)
			return nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.UpdateKeyStatus(context.Background(), &pb.UpdateKeyStatusRequest{
		KeyId:   "my-key",
		Status:  pb.KeyStatus_KEY_STATUS_ENABLED,
		Version: 1,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, enableCalled)
	assert.Equal(t, "my-key", resp.KeyId)
	assert.Equal(t, pb.KeyStatus_KEY_STATUS_ENABLED, resp.Status)
}

func TestKMSServer_UpdateKeyStatus_Disable(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	disableCalled := false
	mockMgr := &mockKMSManager{
		disableKeyFunc: func(ctx context.Context, keyID string) error {
			disableCalled = true
			return nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.UpdateKeyStatus(context.Background(), &pb.UpdateKeyStatusRequest{
		KeyId:  "my-key",
		Status: pb.KeyStatus_KEY_STATUS_DISABLED,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, disableCalled)
	assert.Equal(t, pb.KeyStatus_KEY_STATUS_DISABLED, resp.Status)
}

func TestKMSServer_UpdateKeyStatus_UnsupportedTransition(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.UpdateKeyStatus(context.Background(), &pb.UpdateKeyStatusRequest{
		KeyId:  "my-key",
		Status: pb.KeyStatus_KEY_STATUS_DELETED,
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "unsupported status transition")
}

func TestKMSServer_UpdateKeyStatus_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.UpdateKeyStatus(context.Background(), &pb.UpdateKeyStatusRequest{
		KeyId:  "",
		Status: pb.KeyStatus_KEY_STATUS_ENABLED,
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestKMSServer_UpdateKeyStatus_EnableError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		enableKeyFunc: func(ctx context.Context, keyID string) error {
			return errors.New(errors.ErrNotFound, "EnableKey", "key not found")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.UpdateKeyStatus(context.Background(), &pb.UpdateKeyStatusRequest{
		KeyId:  "missing-key",
		Status: pb.KeyStatus_KEY_STATUS_ENABLED,
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// =============================================================================
// RotateKey Tests (direct manager path)
// =============================================================================

func TestKMSServer_RotateKey_DirectManager(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{
				KeyID:   keyID,
				Version: 2,
				Created: now,
				Updated: now,
			}, nil
		},
		rotateKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{
				KeyID:   keyID,
				Version: 3,
				Created: now,
				Updated: now,
			}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.RotateKey(context.Background(), &pb.RotateKeyRequest{
		KeyId: "rotate-key",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "rotate-key", resp.KeyId)
	assert.Equal(t, uint64(2), resp.OldVersion)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

func TestKMSServer_RotateKey_DirectManager_GetKeyError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return nil, errors.New(errors.ErrNotFound, "GetKey", "key not found")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.RotateKey(context.Background(), &pb.RotateKeyRequest{
		KeyId: "missing-key",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestKMSServer_RotateKey_DirectManager_RotateError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{KeyID: keyID, Version: 1, Created: now, Updated: now}, nil
		},
		rotateKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return nil, errors.New(errors.ErrInternal, "RotateKey", "rotation failed")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.RotateKey(context.Background(), &pb.RotateKeyRequest{
		KeyId: "fail-rotate",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// =============================================================================
// RevokeKey Tests
// =============================================================================

func TestKMSServer_RevokeKey_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	revokeCalled := false
	mockMgr := &mockKMSManager{
		revokeKeyFunc: func(ctx context.Context, keyID string) error {
			revokeCalled = true
			assert.Equal(t, "revoke-key", keyID)
			return nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.RevokeKey(context.Background(), &pb.RevokeKeyRequest{
		KeyId: "revoke-key",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, revokeCalled)
	assert.Equal(t, "revoke-key", resp.KeyId)
}

func TestKMSServer_RevokeKey_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.RevokeKey(context.Background(), &pb.RevokeKeyRequest{KeyId: ""})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestKMSServer_RevokeKey_Error(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		revokeKeyFunc: func(ctx context.Context, keyID string) error {
			return errors.New(errors.ErrForbidden, "RevokeKey", "not permitted")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.RevokeKey(context.Background(), &pb.RevokeKeyRequest{KeyId: "my-key"})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code())
}

// =============================================================================
// GetKeyPolicy Tests
// =============================================================================

func TestKMSServer_GetKeyPolicy_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return &kms.KeyMetadata{KeyID: keyID, Version: 1, Created: now, Updated: now}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.GetKeyPolicy(context.Background(), &pb.GetKeyPolicyRequest{KeyId: "policy-key"})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "policy-key", resp.Policy.KeyId)
	assert.Equal(t, uint64(1), resp.Policy.Version)
	assert.NotEmpty(t, resp.Policy.AllowedOperations)
}

func TestKMSServer_GetKeyPolicy_EmptyKeyID(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := NewKMSServer(&mockKMSManager{}, log)

	_, err := server.GetKeyPolicy(context.Background(), &pb.GetKeyPolicyRequest{KeyId: ""})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestKMSServer_GetKeyPolicy_NotFound(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
			return nil, errors.New(errors.ErrNotFound, "GetKey", "key not found")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.GetKeyPolicy(context.Background(), &pb.GetKeyPolicyRequest{KeyId: "missing"})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

// =============================================================================
// GetSupportedAlgorithms Tests
// =============================================================================

func TestKMSServer_GetSupportedAlgorithms_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		getSupportedAlgorithms: func(ctx context.Context) ([]string, error) {
			return []string{"AES-256-GCM", "RSA-2048", "ECDSA-P256"}, nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.GetSupportedAlgorithms(context.Background(), &pb.GetSupportedAlgorithmsRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Algorithms, 3)
}

func TestKMSServer_GetSupportedAlgorithms_Error(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		getSupportedAlgorithms: func(ctx context.Context) ([]string, error) {
			return nil, errors.New(errors.ErrUnavailable, "GetSupportedAlgorithms", "backend unavailable")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.GetSupportedAlgorithms(context.Background(), &pb.GetSupportedAlgorithmsRequest{})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code())
}

// =============================================================================
// BackendStatus Tests
// =============================================================================

func TestKMSServer_BackendStatus_Healthy(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		isAvailableFunc: func(ctx context.Context) bool { return true },
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.BackendStatus(context.Background(), &pb.BackendStatusRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "healthy", resp.Status)
	assert.NotEmpty(t, resp.Capabilities)
}

func TestKMSServer_BackendStatus_Unavailable(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		isAvailableFunc: func(ctx context.Context) bool { return false },
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.BackendStatus(context.Background(), &pb.BackendStatusRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "unavailable", resp.Status)
}

// =============================================================================
// BackendTest Tests
// =============================================================================

func TestKMSServer_BackendTest_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		isAvailableFunc: func(ctx context.Context) bool { return true },
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.BackendTest(context.Background(), &pb.BackendTestRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Contains(t, resp.Message, "successfully")
}

func TestKMSServer_BackendTest_Unavailable(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		isAvailableFunc: func(ctx context.Context) bool { return false },
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.BackendTest(context.Background(), &pb.BackendTestRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Success)
	assert.Contains(t, resp.Message, "not available")
}

// =============================================================================
// DeleteKey Error Paths (manager-level)
// =============================================================================

func TestKMSServer_DeleteKey_ManagerDeleteError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		deleteKeyFunc: func(ctx context.Context, keyID string) error {
			return errors.New(errors.ErrNotFound, "DeleteKey", "key not found")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.DeleteKey(context.Background(), &pb.DeleteKeyRequest{
		KeyId: "missing-key",
		Force: true, // skip disable, go straight to delete error
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestKMSServer_DeleteKey_DisableError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		disableKeyFunc: func(ctx context.Context, keyID string) error {
			return errors.New(errors.ErrInternal, "DisableKey", "disable failed")
		},
	}
	server := NewKMSServer(mockMgr, log)

	_, err := server.DeleteKey(context.Background(), &pb.DeleteKeyRequest{
		KeyId: "fail-disable-key",
		Force: false, // triggers disable path first
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestKMSServer_DeleteKey_DefaultPendingWindow(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockMgr := &mockKMSManager{
		deleteKeyFunc: func(ctx context.Context, keyID string) error {
			return nil
		},
	}
	server := NewKMSServer(mockMgr, log)

	resp, err := server.DeleteKey(context.Background(), &pb.DeleteKeyRequest{
		KeyId:             "default-window-key",
		Force:             true,
		PendingWindowDays: 0, // triggers default 7-day pending window
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pb.KeyStatus_KEY_STATUS_PENDING_DELETION, resp.Status)
	// Deletion date should be approximately 7 days from now
	expectedMin := time.Now().AddDate(0, 0, 6).Unix()
	expectedMax := time.Now().AddDate(0, 0, 8).Unix()
	assert.Greater(t, resp.DeletionDate, expectedMin)
	assert.Less(t, resp.DeletionDate, expectedMax)
}

// =============================================================================
// ListKeyVersions Tests via Direct Manager
// =============================================================================

func TestKMSServer_ListKeyVersions_DirectManager(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		mockMgr := &mockKMSManager{
			getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return &kms.KeyMetadata{
					KeyID:     keyID,
					Algorithm: kms.AlgAES256GCM,
					State:     kms.KeyStateEnabled,
					Version:   3,
					Created:   now,
					Updated:   now,
				}, nil
			},
		}
		server := NewKMSServer(mockMgr, log)

		resp, err := server.ListKeyVersions(context.Background(), &pb.ListKeyVersionsRequest{
			KeyId: "ver-key",
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "ver-key", resp.KeyId)
		assert.Len(t, resp.Versions, 3)
	})

	t.Run("empty_key_id", func(t *testing.T) {
		server := NewKMSServer(&mockKMSManager{}, log)

		_, err := server.ListKeyVersions(context.Background(), &pb.ListKeyVersionsRequest{KeyId: ""})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("key_not_found", func(t *testing.T) {
		mockMgr := &mockKMSManager{
			getKeyFunc: func(ctx context.Context, keyID string) (interface{}, error) {
				return nil, errors.New(errors.ErrNotFound, "GetKey", "not found")
			},
		}
		server := NewKMSServer(mockMgr, log)

		_, err := server.ListKeyVersions(context.Background(), &pb.ListKeyVersionsRequest{KeyId: "missing"})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
	})
}
