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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func newTestSecretsServerForErrors(t *testing.T, mock *mockSecretsService) *SecretsServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	return NewSecretsServer(mock, log)
}

// TestSecretsServer_ServiceErrors covers service error paths and the
// ExpiresAt conversion path in StoreSecret.
func TestSecretsServer_ServiceErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("StoreSecret_WithExpiresAt", func(t *testing.T) {
		now := time.Now()
		mock := &mockSecretsService{
			storeSecretFunc: func(_ context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				assert.NotNil(t, req.ExpiresAt)
				return &service.StoreSecretResponse{
					SecretID:  "s1",
					Version:   1,
					Timestamp: now,
				}, nil
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		resp, err := srv.StoreSecret(ctx, &pb.StoreSecretRequest{
			Name:      "test-secret",
			Data:      []byte("data"),
			ExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
		})
		require.NoError(t, err)
		assert.Equal(t, "s1", resp.SecretId)
	})

	t.Run("ListSecrets_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			listSecretsFunc: func(_ context.Context, _ *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
				return nil, errors.New(errors.ErrInternal, "ListSecrets", "storage error")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.ListSecrets(ctx, &pb.ListSecretsRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("DeleteSecret_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			deleteSecretFunc: func(_ context.Context, _ *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "DeleteSecret", "not found")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.DeleteSecret(ctx, &pb.DeleteSecretRequest{SecretId: "s1"})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("GetSecretVersion_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			getSecretVersionFunc: func(_ context.Context, _ *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetSecretVersion", "not found")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.GetSecretVersion(ctx, &pb.GetSecretVersionRequest{
			SecretId: "s1",
			Version:  1,
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("ListSecretVersions_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			listSecretVersionsFunc: func(_ context.Context, _ *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
				return nil, errors.New(errors.ErrInternal, "ListSecretVersions", "storage error")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.ListSecretVersions(ctx, &pb.ListSecretVersionsRequest{SecretId: "s1"})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("RollbackSecret_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			rollbackSecretFunc: func(_ context.Context, _ *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "RollbackSecret", "not found")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.RollbackSecret(ctx, &pb.RollbackSecretRequest{
			SecretId:      "s1",
			TargetVersion: 1,
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("RotateSecret_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			rotateSecretFunc: func(_ context.Context, _ *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "RotateSecret", "not found")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.RotateSecret(ctx, &pb.RotateSecretRequest{
			SecretId: "s1",
			NewData:  []byte("new-data"),
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("SetRotationPolicy_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			setRotationPolicyFunc: func(_ context.Context, _ *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "SetRotationPolicy", "not found")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.SetRotationPolicy(ctx, &pb.SetRotationPolicyRequest{
			SecretId: "s1",
			Policy:   &pb.SecretRotationPolicy{Enabled: true, IntervalSeconds: 3600},
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("GetRotationPolicy_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			getRotationPolicyFunc: func(_ context.Context, _ *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "GetRotationPolicy", "not found")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.GetRotationPolicy(ctx, &pb.GetRotationPolicyRequest{SecretId: "s1"})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("UpdateSecretStatus_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			updateSecretStatusFunc: func(_ context.Context, _ *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
				return nil, errors.New(errors.ErrNotFound, "UpdateSecretStatus", "not found")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.UpdateSecretStatus(ctx, &pb.UpdateSecretStatusRequest{
			SecretId: "s1",
			Status:   pb.SecretStatus_SECRET_STATUS_ENABLED,
		})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("GetSecretsByPrefix_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			getSecretsByPrefixFunc: func(_ context.Context, _ *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
				return nil, errors.New(errors.ErrInternal, "GetSecretsByPrefix", "storage error")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		_, err := srv.GetSecretsByPrefix(ctx, &pb.GetSecretsByPrefixRequest{Prefix: "/app/"})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("WatchSecretsByPrefix_ServiceError", func(t *testing.T) {
		mock := &mockSecretsService{
			watchSecretsByPrefixFunc: func(_ context.Context, _ *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
				return nil, errors.New(errors.ErrInternal, "Watch", "watch failed")
			},
		}
		srv := newTestSecretsServerForErrors(t, mock)

		stream := &stubWatchSecretsStream{ctx: ctx}
		err := srv.WatchSecretsByPrefix(&pb.WatchSecretsRequest{Prefix: "/app/"}, stream)
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})
}

// stubWatchSecretsStream implements pb.SecretsManagement_WatchSecretsByPrefixServer.
type stubWatchSecretsStream struct {
	ctx context.Context
	pb.SecretsManagement_WatchSecretsByPrefixServer
}

func (s *stubWatchSecretsStream) Context() context.Context { return s.ctx }
func (s *stubWatchSecretsStream) Send(*pb.SecretChange) error {
	return nil
}
