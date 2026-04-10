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

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	secrets "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// =============================================================================
// Mock Secrets Service
// =============================================================================

type mockSecretsService struct {
	storeFunc  func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	getFunc    func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
	listFunc   func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	deleteFunc func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
	ensureFunc func(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error)
}

func (m *mockSecretsService) EnsureSecret(ctx context.Context, req *service.EnsureSecretRequest) (*service.EnsureSecretResponse, error) {
	if m.ensureFunc != nil {
		return m.ensureFunc(ctx, req)
	}
	return &service.EnsureSecretResponse{
		SecretID:  req.SecretID,
		Data:      []byte("MOCKED"),
		Generated: true,
		Version:   1,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSecretsService) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, req)
	}
	return &service.StoreSecretResponse{
		SecretID:  req.Name,
		Version:   1,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSecretsService) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &service.GetSecretResponse{
		SecretID:  req.SecretID,
		Version:   1,
		Data:      []byte(`{"key":"value"}`),
		Metadata:  map[string]string{"env": "test"},
		CreatedAt: time.Now(),
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSecretsService) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return &service.ListSecretsResponse{
		Secrets: []*service.Secret{
			{SecretID: "secret/one"},
			{SecretID: "secret/two"},
		},
		TotalCount: 2,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockSecretsService) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &service.DeleteSecretResponse{
		SecretID:  req.SecretID,
		Deleted:   true,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSecretsService) GetSecretVersion(ctx context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
	return &service.GetSecretVersionResponse{}, nil
}

func (m *mockSecretsService) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return &service.ListSecretVersionsResponse{}, nil
}

func (m *mockSecretsService) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return &service.RollbackSecretResponse{}, nil
}

func (m *mockSecretsService) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return &service.RotateSecretResponse{}, nil
}

func (m *mockSecretsService) SetRotationPolicy(ctx context.Context, req *service.SetRotationPolicyRequest) (*service.SetRotationPolicyResponse, error) {
	return &service.SetRotationPolicyResponse{}, nil
}

func (m *mockSecretsService) GetRotationPolicy(ctx context.Context, req *service.GetRotationPolicyRequest) (*service.GetRotationPolicyResponse, error) {
	return &service.GetRotationPolicyResponse{}, nil
}

func (m *mockSecretsService) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return &service.UpdateSecretStatusResponse{}, nil
}

func (m *mockSecretsService) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	return &service.GetSecretsByPrefixResponse{}, nil
}

func (m *mockSecretsService) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	ch := make(chan *service.SecretChange)
	close(ch)
	return ch, nil
}

// =============================================================================
// Secrets Handler Tests
// =============================================================================

func TestHandleSecretsStore_Success(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	req := &secrets.StoreSecretRequest{
		Path: "app/database/password",
		Data: map[string]interface{}{
			"password": "s3cret",
		},
		Metadata: map[string]string{"env": "prod"},
	}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Store", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	storeResp, ok := resp.(*secrets.StoreSecretResponse)
	require.True(t, ok)
	assert.Equal(t, 1, storeResp.Version)
}

func TestHandleSecretsStore_ServiceNotConfigured(t *testing.T) {
	mock := &mockQuicBaaS{} // secretsService is nil
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	req := &secrets.StoreSecretRequest{
		Path: "app/database/password",
		Data: map[string]interface{}{
			"password": "s3cret",
		},
	}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Store", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
	assert.Equal(t, "Secrets", svcErr.Service)
}

func TestHandleSecretsStore_InvalidRequestType(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	// Send wrong type
	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Store", "not-a-request")
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "quicbaas.Secrets", typeErr.Service)
	assert.Equal(t, "Store", typeErr.Method)
}

func TestHandleSecretsGet_Success(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	// Simulate the SDK client's unexported getSecretRequest by creating a struct
	// with matching field names.
	type getSecretRequest struct {
		Path    string
		Version int
	}

	req := &getSecretRequest{
		Path:    "app/database/password",
		Version: 0,
	}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Get", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	secret, ok := resp.(*secrets.Secret)
	require.True(t, ok)
	assert.Equal(t, "app/database/password", secret.Path)
	assert.Equal(t, 1, secret.Version)
	assert.NotNil(t, secret.Data)
}

func TestHandleSecretsGet_ServiceNotConfigured(t *testing.T) {
	mock := &mockQuicBaaS{} // secretsService is nil
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	type getSecretRequest struct {
		Path    string
		Version int
	}

	req := &getSecretRequest{Path: "secret/path", Version: 0}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Get", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

func TestHandleSecretsGet_InvalidRequestType(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	// Send a type without the expected fields
	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Get", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

func TestHandleSecretsList_Success(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	type listSecretsRequest struct {
		Prefix string
	}

	req := &listSecretsRequest{Prefix: "secret/"}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "List", req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	listResp, ok := resp.(*secrets.ListSecretsResponse)
	require.True(t, ok)
	assert.Len(t, listResp.Paths, 2)
	assert.Contains(t, listResp.Paths, "secret/one")
	assert.Contains(t, listResp.Paths, "secret/two")
}

func TestHandleSecretsList_ServiceNotConfigured(t *testing.T) {
	mock := &mockQuicBaaS{} // secretsService is nil
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	type listSecretsRequest struct {
		Prefix string
	}

	req := &listSecretsRequest{Prefix: "secret/"}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "List", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

func TestHandleSecretsList_InvalidRequestType(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	// Send a type without the Prefix field
	resp, err := tr.Request(ctx, "quicbaas.Secrets", "List", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

func TestHandleSecretsDelete_Success(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	type deleteSecretRequest struct {
		Path string
	}

	req := &deleteSecretRequest{Path: "app/database/password"}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Delete", req)
	require.NoError(t, err)
	// Delete handler returns nil on success (SDK expects nil response)
	assert.Nil(t, resp)
}

func TestHandleSecretsDelete_ServiceNotConfigured(t *testing.T) {
	mock := &mockQuicBaaS{} // secretsService is nil
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	type deleteSecretRequest struct {
		Path string
	}

	req := &deleteSecretRequest{Path: "app/database/password"}

	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Delete", req)
	assert.Nil(t, resp)
	require.Error(t, err)

	var svcErr *ServiceNotConfiguredError
	assert.ErrorAs(t, err, &svcErr)
}

func TestHandleSecretsDelete_InvalidRequestType(t *testing.T) {
	secretsMock := &mockSecretsService{}
	mock := &mockQuicBaaS{secretsService: secretsMock}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	// Send a type without the Path field
	resp, err := tr.Request(ctx, "quicbaas.Secrets", "Delete", 42)
	assert.Nil(t, resp)
	require.Error(t, err)

	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}

// =============================================================================
// Health Ping Handler Tests
// =============================================================================

func TestHandlePing_Success(t *testing.T) {
	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	// Simulate the SDK client's unexported pingRequest
	type pingRequest struct{}

	resp, err := tr.Request(ctx, "quicbaas.Health", "Ping", &pingRequest{})
	require.NoError(t, err)
	// Ping returns nil response on success. The SDK health client's Ping method
	// only returns error and accepts nil at health/client.go line 85-87.
	assert.Nil(t, resp)
}

func TestHandlePing_ServiceError(t *testing.T) {
	mock := &mockQuicBaaS{
		livenessFunc: func(ctx context.Context, req *service.LivenessRequest) (*service.LivenessResponse, error) {
			return nil, &ServiceNotConfiguredError{Service: "Health"}
		},
	}
	tr, err := New(mock)
	require.NoError(t, err)

	ctx := context.Background()

	type pingRequest struct{}

	resp, err := tr.Request(ctx, "quicbaas.Health", "Ping", &pingRequest{})
	assert.Nil(t, resp)
	require.Error(t, err)
}

// =============================================================================
// Registry Tests - Verify handlers are registered
// =============================================================================

func TestUnaryHandlers_SecretsRegistered(t *testing.T) {
	secretsMethods := []string{
		"quicbaas.Secrets/Store",
		"quicbaas.Secrets/Get",
		"quicbaas.Secrets/List",
		"quicbaas.Secrets/Delete",
	}

	for _, method := range secretsMethods {
		t.Run(method, func(t *testing.T) {
			_, ok := unaryHandlers[method]
			assert.True(t, ok, "handler not registered for %s", method)
		})
	}
}

func TestUnaryHandlers_PingRegistered(t *testing.T) {
	_, ok := unaryHandlers["quicbaas.Health/Ping"]
	assert.True(t, ok, "handler not registered for quicbaas.Health/Ping")
}
