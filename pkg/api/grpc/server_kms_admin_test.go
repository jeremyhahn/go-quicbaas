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

// server_kms_admin_test.go covers the KMS key-administration surface added for
// full transport parity: the KMSServer.GetPublicKey handler and the Client
// wrappers for public-key retrieval, revocation, metadata, access policy and
// rotation policy.

package grpc

import (
	"context"
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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// KMSServer.GetPublicKey
// =============================================================================

func TestKMSServer_GetPublicKey(t *testing.T) {
	log := newTestLogger()
	now := time.Now()

	t.Run("missing_key_id_invalid_argument", func(t *testing.T) {
		server := NewKMSServerWithService(&mockKMSManager{}, &mocks.MockKMSService{}, log)
		_, err := server.GetPublicKey(context.Background(), &pb.GetPublicKeyRequest{})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("nil_service_unimplemented", func(t *testing.T) {
		server := NewKMSServer(&mockKMSManager{}, log)
		_, err := server.GetPublicKey(context.Background(), &pb.GetPublicKeyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})

	t.Run("service_error_propagated", func(t *testing.T) {
		mockSvc := &mocks.MockKMSService{
			GetPublicKeyFunc: func(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetPublicKey", "no such key")
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		_, err := server.GetPublicKey(context.Background(), &pb.GetPublicKeyRequest{KeyId: "k1"})
		st, _ := status.FromError(err)
		assert.Equal(t, codes.NotFound, st.Code())
	})

	t.Run("success", func(t *testing.T) {
		der := []byte{0x30, 0x82, 0x01, 0x22}
		mockSvc := &mocks.MockKMSService{
			GetPublicKeyFunc: func(ctx context.Context, req *service.GetPublicKeyRequest) (*service.GetPublicKeyResponse, error) {
				assert.Equal(t, "k1", req.KeyID)
				assert.Equal(t, uint64(2), req.Version)
				return &service.GetPublicKeyResponse{
					KeyID:     "k1",
					PublicKey: der,
					Algorithm: service.KeyAlgRSA2048,
					Version:   2,
					Timestamp: now,
				}, nil
			},
		}
		server := NewKMSServerWithService(&mockKMSManager{}, mockSvc, log)
		resp, err := server.GetPublicKey(context.Background(), &pb.GetPublicKeyRequest{KeyId: "k1", Version: 2})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyId)
		assert.Equal(t, der, resp.PublicKey)
		assert.Equal(t, pb.KeyAlgorithm_KEY_ALG_RSA_2048, resp.Algorithm)
		assert.Equal(t, uint64(2), resp.Version)
		assert.Equal(t, now.Unix(), resp.Timestamp)
	})
}

// =============================================================================
// Client wrapper test harness
// =============================================================================

// kmsAdminTestServer implements the KeyManagement proto surface for the key
// administration verbs under test. Unset funcs fall back to the embedded
// Unimplemented server.
type kmsAdminTestServer struct {
	pb.UnimplementedKeyManagementServer

	getPublicKeyFunc            func(ctx context.Context, req *pb.GetPublicKeyRequest) (*pb.GetPublicKeyResponse, error)
	revokeKeyFunc               func(ctx context.Context, req *pb.RevokeKeyRequest) (*pb.RevokeKeyResponse, error)
	getKeyMetadataFunc          func(ctx context.Context, req *pb.GetKeyMetadataRequest) (*pb.GetKeyMetadataResponse, error)
	updateKeyMetadataFunc       func(ctx context.Context, req *pb.UpdateKeyMetadataRequest) (*pb.UpdateKeyMetadataResponse, error)
	setKeyPolicyFunc            func(ctx context.Context, req *pb.SetKeyPolicyRequest) (*pb.SetKeyPolicyResponse, error)
	getKeyPolicyFunc            func(ctx context.Context, req *pb.GetKeyPolicyRequest) (*pb.GetKeyPolicyResponse, error)
	setKeyRotationPolicyFunc    func(ctx context.Context, req *pb.SetKeyRotationPolicyRequest) (*pb.SetKeyRotationPolicyResponse, error)
	getKeyRotationPolicyFunc    func(ctx context.Context, req *pb.GetKeyRotationPolicyRequest) (*pb.GetKeyRotationPolicyResponse, error)
	deleteKeyRotationPolicyFunc func(ctx context.Context, req *pb.DeleteKeyRotationPolicyRequest) (*pb.DeleteKeyRotationPolicyResponse, error)
}

func (s *kmsAdminTestServer) GetPublicKey(ctx context.Context, req *pb.GetPublicKeyRequest) (*pb.GetPublicKeyResponse, error) {
	if s.getPublicKeyFunc != nil {
		return s.getPublicKeyFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.GetPublicKey(ctx, req)
}

func (s *kmsAdminTestServer) RevokeKey(ctx context.Context, req *pb.RevokeKeyRequest) (*pb.RevokeKeyResponse, error) {
	if s.revokeKeyFunc != nil {
		return s.revokeKeyFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.RevokeKey(ctx, req)
}

func (s *kmsAdminTestServer) GetKeyMetadata(ctx context.Context, req *pb.GetKeyMetadataRequest) (*pb.GetKeyMetadataResponse, error) {
	if s.getKeyMetadataFunc != nil {
		return s.getKeyMetadataFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.GetKeyMetadata(ctx, req)
}

func (s *kmsAdminTestServer) UpdateKeyMetadata(ctx context.Context, req *pb.UpdateKeyMetadataRequest) (*pb.UpdateKeyMetadataResponse, error) {
	if s.updateKeyMetadataFunc != nil {
		return s.updateKeyMetadataFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.UpdateKeyMetadata(ctx, req)
}

func (s *kmsAdminTestServer) SetKeyPolicy(ctx context.Context, req *pb.SetKeyPolicyRequest) (*pb.SetKeyPolicyResponse, error) {
	if s.setKeyPolicyFunc != nil {
		return s.setKeyPolicyFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.SetKeyPolicy(ctx, req)
}

func (s *kmsAdminTestServer) GetKeyPolicy(ctx context.Context, req *pb.GetKeyPolicyRequest) (*pb.GetKeyPolicyResponse, error) {
	if s.getKeyPolicyFunc != nil {
		return s.getKeyPolicyFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.GetKeyPolicy(ctx, req)
}

func (s *kmsAdminTestServer) SetKeyRotationPolicy(ctx context.Context, req *pb.SetKeyRotationPolicyRequest) (*pb.SetKeyRotationPolicyResponse, error) {
	if s.setKeyRotationPolicyFunc != nil {
		return s.setKeyRotationPolicyFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.SetKeyRotationPolicy(ctx, req)
}

func (s *kmsAdminTestServer) GetKeyRotationPolicy(ctx context.Context, req *pb.GetKeyRotationPolicyRequest) (*pb.GetKeyRotationPolicyResponse, error) {
	if s.getKeyRotationPolicyFunc != nil {
		return s.getKeyRotationPolicyFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.GetKeyRotationPolicy(ctx, req)
}

func (s *kmsAdminTestServer) DeleteKeyRotationPolicy(ctx context.Context, req *pb.DeleteKeyRotationPolicyRequest) (*pb.DeleteKeyRotationPolicyResponse, error) {
	if s.deleteKeyRotationPolicyFunc != nil {
		return s.deleteKeyRotationPolicyFunc(ctx, req)
	}
	return s.UnimplementedKeyManagementServer.DeleteKeyRotationPolicy(ctx, req)
}

const kmsAdminTestBufSize = 1024 * 1024

func setupConnectedKMSAdminClient(t *testing.T, kmsServer *kmsAdminTestServer) (*Client, *grpc.Server) {
	t.Helper()

	lis := bufconn.Listen(kmsAdminTestBufSize)
	server := grpc.NewServer()
	pb.RegisterKeyManagementServer(server, kmsServer)
	go func() {
		_ = server.Serve(lis)
	}()

	conn, err := grpc.DialContext(
		context.Background(),
		"bufnet",
		grpc.WithContextDialer(func(ctx context.Context, s string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	return &Client{conn: conn, connected: true, logger: log}, server
}

// =============================================================================
// Client.KMSGetPublicKey
// =============================================================================

func TestClientKMSGetPublicKey_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSGetPublicKey(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSGetPublicKey_Success(t *testing.T) {
	der := []byte{0x30, 0x03, 0x01, 0x00, 0x01}
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		getPublicKeyFunc: func(ctx context.Context, req *pb.GetPublicKeyRequest) (*pb.GetPublicKeyResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			assert.Equal(t, uint64(3), req.Version)
			return &pb.GetPublicKeyResponse{
				KeyId:     "k1",
				PublicKey: der,
				Algorithm: pb.KeyAlgorithm_KEY_ALG_RSA_2048,
				Version:   3,
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSGetPublicKey(context.Background(), &service.GetPublicKeyRequest{KeyID: "k1", Version: 3})
	require.NoError(t, err)
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, der, resp.PublicKey)
	assert.Equal(t, uint64(3), resp.Version)
}

func TestClientKMSGetPublicKey_Error(t *testing.T) {
	kmsServer := &kmsAdminTestServer{
		getPublicKeyFunc: func(ctx context.Context, req *pb.GetPublicKeyRequest) (*pb.GetPublicKeyResponse, error) {
			return nil, status.Error(codes.NotFound, "no such key")
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSGetPublicKey(context.Background(), &service.GetPublicKeyRequest{KeyID: "missing"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Client.KMSRevokeKey
// =============================================================================

func TestClientKMSRevokeKey_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSRevokeKey(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSRevokeKey_SuccessWithReason(t *testing.T) {
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		revokeKeyFunc: func(ctx context.Context, req *pb.RevokeKeyRequest) (*pb.RevokeKeyResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			assert.Equal(t, "compromised", req.Reason)
			return &pb.RevokeKeyResponse{
				KeyId:     "k1",
				Status:    pb.KeyStatus_KEY_STATUS_COMPROMISED,
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSRevokeKey(context.Background(), &service.RevokeKeyRequest{KeyID: "k1", Reason: "compromised"})
	require.NoError(t, err)
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, service.KeyStatusCompromised, resp.Status)
}

func TestClientKMSRevokeKey_Error(t *testing.T) {
	kmsServer := &kmsAdminTestServer{
		revokeKeyFunc: func(ctx context.Context, req *pb.RevokeKeyRequest) (*pb.RevokeKeyResponse, error) {
			return nil, status.Error(codes.NotFound, "no such key")
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSRevokeKey(context.Background(), &service.RevokeKeyRequest{KeyID: "missing"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// Client.KMSGetKeyMetadata / KMSUpdateKeyMetadata
// =============================================================================

func TestClientKMSGetKeyMetadata_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSGetKeyMetadata(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSGetKeyMetadata_Success(t *testing.T) {
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		getKeyMetadataFunc: func(ctx context.Context, req *pb.GetKeyMetadataRequest) (*pb.GetKeyMetadataResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			return &pb.GetKeyMetadataResponse{
				Metadata: &pb.KeyMetadata{
					KeyId:     "k1",
					Name:      "my-key",
					Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
					Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
					Version:   2,
					CreatedAt: now.Unix(),
					UpdatedAt: now.Unix(),
				},
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSGetKeyMetadata(context.Background(), &service.GetKeyMetadataRequest{KeyID: "k1"})
	require.NoError(t, err)
	require.NotNil(t, resp.Metadata)
	assert.Equal(t, "k1", resp.Metadata.KeyID)
	assert.Equal(t, "my-key", resp.Metadata.Name)
	assert.Equal(t, uint64(2), resp.Metadata.Version)
}

func TestClientKMSUpdateKeyMetadata_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSUpdateKeyMetadata(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSUpdateKeyMetadata_Success(t *testing.T) {
	now := time.Now()
	name := "renamed"
	kmsServer := &kmsAdminTestServer{
		updateKeyMetadataFunc: func(ctx context.Context, req *pb.UpdateKeyMetadataRequest) (*pb.UpdateKeyMetadataResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			assert.Equal(t, "renamed", req.Name)
			assert.Empty(t, req.Description)
			assert.Equal(t, []string{"stale"}, req.RemoveTags)
			return &pb.UpdateKeyMetadataResponse{
				Metadata: &pb.KeyMetadata{
					KeyId:     "k1",
					Name:      "renamed",
					Algorithm: pb.KeyAlgorithm_KEY_ALG_AES_256_GCM,
					Status:    pb.KeyStatus_KEY_STATUS_ENABLED,
					CreatedAt: now.Unix(),
					UpdatedAt: now.Unix(),
				},
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSUpdateKeyMetadata(context.Background(), &service.UpdateKeyMetadataRequest{
		KeyID:      "k1",
		Name:       &name,
		RemoveTags: []string{"stale"},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Metadata)
	assert.Equal(t, "renamed", resp.Metadata.Name)
}

// =============================================================================
// Client.KMSSetKeyPolicy / KMSGetKeyPolicy
// =============================================================================

func TestClientKMSSetKeyPolicy_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSSetKeyPolicy(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSSetKeyPolicy_Success(t *testing.T) {
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		setKeyPolicyFunc: func(ctx context.Context, req *pb.SetKeyPolicyRequest) (*pb.SetKeyPolicyResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			require.NotNil(t, req.Policy)
			return &pb.SetKeyPolicyResponse{
				KeyId:         "k1",
				PolicyVersion: 2,
				Timestamp:     now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSSetKeyPolicy(context.Background(), &service.SetKeyPolicyRequest{
		KeyID: "k1",
		Policy: &service.KeyPolicy{
			KeyID:             "k1",
			AllowedOperations: []service.KeyUsage{service.KeyUsageEncrypt},
			UpdatedAt:         now,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, uint64(2), resp.PolicyVersion)
}

func TestClientKMSGetKeyPolicy_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSGetKeyPolicy(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSGetKeyPolicy_Success(t *testing.T) {
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		getKeyPolicyFunc: func(ctx context.Context, req *pb.GetKeyPolicyRequest) (*pb.GetKeyPolicyResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			return &pb.GetKeyPolicyResponse{
				Policy: &pb.KeyPolicy{
					KeyId:             "k1",
					Version:           1,
					AllowedOperations: []pb.KeyUsage{pb.KeyUsage_KEY_USAGE_ENCRYPT},
					UpdatedAt:         now.Unix(),
				},
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSGetKeyPolicy(context.Background(), &service.GetKeyPolicyRequest{KeyID: "k1"})
	require.NoError(t, err)
	require.NotNil(t, resp.Policy)
	assert.Equal(t, "k1", resp.Policy.KeyID)
	assert.Equal(t, uint64(1), resp.Policy.Version)
}

// =============================================================================
// Client rotation-policy wrappers
// =============================================================================

func TestClientKMSSetKeyRotationPolicy_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSSetKeyRotationPolicy(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSSetKeyRotationPolicy_Success(t *testing.T) {
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		setKeyRotationPolicyFunc: func(ctx context.Context, req *pb.SetKeyRotationPolicyRequest) (*pb.SetKeyRotationPolicyResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			require.NotNil(t, req.Policy)
			assert.Equal(t, int64(86400), req.Policy.IntervalSeconds)
			assert.True(t, req.Policy.AutoRotate)
			return &pb.SetKeyRotationPolicyResponse{
				KeyId:     "k1",
				Policy:    req.Policy,
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSSetKeyRotationPolicy(context.Background(), &service.SetKeyRotationPolicyRequest{
		KeyID: "k1",
		Policy: &service.RotationPolicy{
			Enabled:        true,
			Interval:       24 * time.Hour,
			RetainVersions: 3,
			AutoRotate:     true,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "k1", resp.KeyID)
	require.NotNil(t, resp.Policy)
	assert.Equal(t, 24*time.Hour, resp.Policy.Interval)
}

func TestClientKMSGetKeyRotationPolicy_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSGetKeyRotationPolicy(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSGetKeyRotationPolicy_Success(t *testing.T) {
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		getKeyRotationPolicyFunc: func(ctx context.Context, req *pb.GetKeyRotationPolicyRequest) (*pb.GetKeyRotationPolicyResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			return &pb.GetKeyRotationPolicyResponse{
				KeyId: "k1",
				Policy: &pb.RotationPolicy{
					Enabled:         true,
					IntervalSeconds: 3600,
					RetainVersions:  2,
				},
				Timestamp: now.Unix(),
			}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSGetKeyRotationPolicy(context.Background(), &service.GetKeyRotationPolicyRequest{KeyID: "k1"})
	require.NoError(t, err)
	require.NotNil(t, resp.Policy)
	assert.Equal(t, time.Hour, resp.Policy.Interval)
	assert.Equal(t, 2, resp.Policy.RetainVersions)
}

func TestClientKMSDeleteKeyRotationPolicy_NilRequest(t *testing.T) {
	client := &Client{}
	resp, err := client.KMSDeleteKeyRotationPolicy(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestClientKMSDeleteKeyRotationPolicy_Success(t *testing.T) {
	now := time.Now()
	kmsServer := &kmsAdminTestServer{
		deleteKeyRotationPolicyFunc: func(ctx context.Context, req *pb.DeleteKeyRotationPolicyRequest) (*pb.DeleteKeyRotationPolicyResponse, error) {
			assert.Equal(t, "k1", req.KeyId)
			return &pb.DeleteKeyRotationPolicyResponse{KeyId: "k1", Timestamp: now.Unix()}, nil
		},
	}
	client, server := setupConnectedKMSAdminClient(t, kmsServer)
	defer server.Stop()
	defer client.conn.Close()

	resp, err := client.KMSDeleteKeyRotationPolicy(context.Background(), &service.DeleteKeyRotationPolicyRequest{KeyID: "k1"})
	require.NoError(t, err)
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, now.Unix(), resp.Timestamp.Unix())
}

// =============================================================================
// Authz mapping coverage for the new RPCs
// =============================================================================

func TestAuthzMapping_KMSAdminRPCs(t *testing.T) {
	tests := []struct {
		method       string
		wantResource string
		wantAction   string
	}{
		{"/quicbaas.kms.v1.KeyManagement/GetPublicKey", "keys", "get"},
		{"/quicbaas.kms.v1.KeyManagement/SetKeyRotationPolicy", "keys", "rotate"},
		{"/quicbaas.kms.v1.KeyManagement/GetKeyRotationPolicy", "keys", "get"},
		{"/quicbaas.kms.v1.KeyManagement/DeleteKeyRotationPolicy", "keys", "delete"},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			entry, ok := grpcMethodPermissions[tt.method]
			require.True(t, ok, "missing authz mapping for %s", tt.method)
			assert.Equal(t, tt.wantResource, entry.resource)
			assert.Equal(t, tt.wantAction, entry.action)
		})
	}
}
