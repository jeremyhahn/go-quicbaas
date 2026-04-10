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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockSecretsServiceWithPrefix implements the secrets service for prefix tests.
type mockSecretsServiceWithPrefix struct {
	getSecretsByPrefixResp *service.GetSecretsByPrefixResponse
	getSecretsByPrefixErr  error
	watchSecretsResp       <-chan *service.SecretChange
	watchSecretsErr        error
}

func (m *mockSecretsServiceWithPrefix) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return nil, nil
}

func (m *mockSecretsServiceWithPrefix) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if m.getSecretsByPrefixErr != nil {
		return nil, m.getSecretsByPrefixErr
	}
	return m.getSecretsByPrefixResp, nil
}

func (m *mockSecretsServiceWithPrefix) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if m.watchSecretsErr != nil {
		return nil, m.watchSecretsErr
	}
	return m.watchSecretsResp, nil
}

func TestSecretsServer_GetSecretsByPrefix_Success(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockSvc := &mockSecretsServiceWithPrefix{
		getSecretsByPrefixResp: &service.GetSecretsByPrefixResponse{
			Secrets: map[string]map[string][]byte{
				"app/db/host":     {"value": []byte("localhost")},
				"app/db/port":     {"value": []byte("5432")},
				"app/db/password": {"value": []byte("secret123")},
			},
			Metadata: map[string]map[string]string{
				"app/db/host":     {"env": "production"},
				"app/db/port":     {"env": "production"},
				"app/db/password": {"env": "production"},
			},
			Version:    5,
			NextCursor: "next-page-token",
			HasMore:    true,
			TotalCount: 100,
			Timestamp:  now,
		},
	}

	server := NewSecretsServer(mockSvc, log)

	ctx := context.Background()
	resp, err := server.GetSecretsByPrefix(ctx, &pb.GetSecretsByPrefixRequest{
		Prefix:      "app/db/",
		IncludeData: true,
		Limit:       10,
		Cursor:      "",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 3, len(resp.Secrets))
	assert.Equal(t, "next-page-token", resp.NextCursor)
	assert.True(t, resp.HasMore)
	assert.Equal(t, int32(100), resp.TotalCount)
}

func TestSecretsServer_GetSecretsByPrefix_ServiceError(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})

	mockSvc := &mockSecretsServiceWithPrefix{
		getSecretsByPrefixErr: errors.Newf(errors.ErrNotFound, "TestSecretsServer", "prefix not found"),
	}

	server := NewSecretsServer(mockSvc, log)

	ctx := context.Background()
	resp, err := server.GetSecretsByPrefix(ctx, &pb.GetSecretsByPrefixRequest{
		Prefix: "nonexistent/",
	})

	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestSecretsServer_GetSecretsByPrefix_EmptyResponse(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	now := time.Now()

	mockSvc := &mockSecretsServiceWithPrefix{
		getSecretsByPrefixResp: &service.GetSecretsByPrefixResponse{
			Secrets:    map[string]map[string][]byte{},
			Metadata:   map[string]map[string]string{},
			Version:    1,
			NextCursor: "",
			HasMore:    false,
			TotalCount: 0,
			Timestamp:  now,
		},
	}

	server := NewSecretsServer(mockSvc, log)

	ctx := context.Background()
	resp, err := server.GetSecretsByPrefix(ctx, &pb.GetSecretsByPrefixRequest{
		Prefix:      "empty/",
		IncludeData: true,
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, len(resp.Secrets))
	assert.False(t, resp.HasMore)
	assert.Equal(t, int32(0), resp.TotalCount)
}
