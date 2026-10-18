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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestEmbeddedSecrets_GetVersion(t *testing.T) {
	created := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	var got *service.GetSecretVersionRequest
	client := newSecretsClient(t, &mockSecretsService{
		getVersionFunc: func(_ context.Context, req *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
			got = req
			return &service.GetSecretVersionResponse{Version: &service.SecretVersion{
				SecretID:  req.SecretID,
				Version:   req.Version,
				Data:      []byte("v2"),
				Metadata:  map[string]string{"by": "test"},
				CreatedAt: created,
				CreatedBy: "admin",
			}}, nil
		},
	})

	info, err := client.GetVersion(context.Background(), &secrets.GetSecretVersionRequest{SecretID: "app/db", Version: 2})
	require.NoError(t, err)
	assert.Equal(t, &secrets.SecretVersionInfo{
		SecretID:  "app/db",
		Version:   2,
		Data:      []byte("v2"),
		Metadata:  map[string]string{"by": "test"},
		CreatedAt: created,
		CreatedBy: "admin",
	}, info)
	require.NotNil(t, got)
	assert.Equal(t, &service.GetSecretVersionRequest{SecretID: "app/db", Version: 2}, got)
}

func TestEmbeddedSecrets_GetVersion_NotFound(t *testing.T) {
	client := newSecretsClient(t, &mockSecretsService{
		getVersionFunc: func(_ context.Context, _ *service.GetSecretVersionRequest) (*service.GetSecretVersionResponse, error) {
			return nil, errors.NewNotFound("secrets.GetVersion", "no such version")
		},
	})

	_, err := client.GetVersion(context.Background(), &secrets.GetSecretVersionRequest{SecretID: "app/db", Version: 9})
	assert.True(t, errors.IsNotFound(err), "err = %v, want NotFound", err)
}

func TestEmbeddedSecrets_GetVersion_NonPositiveVersion(t *testing.T) {
	tr, err := New(&mockQuicBaaS{secretsService: &mockSecretsService{}})
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Secrets", "GetVersion", &secrets.GetSecretVersionRequest{SecretID: "app/db", Version: -1})
	assert.True(t, errors.IsInvalidInput(err), "err = %v, want InvalidInput", err)
}

func TestEmbeddedSecrets_ListVersions(t *testing.T) {
	var got *service.ListSecretVersionsRequest
	client := newSecretsClient(t, &mockSecretsService{
		listVersionsFunc: func(_ context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
			got = req
			return &service.ListSecretVersionsResponse{Versions: []*service.SecretVersion{
				{SecretID: req.SecretID, Version: 1},
				{SecretID: req.SecretID, Version: 2},
			}}, nil
		},
	})

	resp, err := client.ListVersions(context.Background(), "app/db")
	require.NoError(t, err)
	assert.Equal(t, &secrets.ListSecretVersionsResponse{Versions: []secrets.SecretVersionInfo{
		{SecretID: "app/db", Version: 1},
		{SecretID: "app/db", Version: 2},
	}}, resp)
	require.NotNil(t, got)
	assert.Equal(t, "app/db", got.SecretID)
}

func TestEmbeddedSecrets_ListVersions_NotFound(t *testing.T) {
	client := newSecretsClient(t, &mockSecretsService{
		listVersionsFunc: func(_ context.Context, _ *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
			return nil, errors.NewNotFound("secrets.ListVersions", "no such secret")
		},
	})

	_, err := client.ListVersions(context.Background(), "missing")
	assert.True(t, errors.IsNotFound(err), "err = %v, want NotFound", err)
}

func TestEmbeddedSecrets_Rollback(t *testing.T) {
	var got *service.RollbackSecretRequest
	client := newSecretsClient(t, &mockSecretsService{
		rollbackFunc: func(_ context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
			got = req
			return &service.RollbackSecretResponse{SecretID: req.SecretID, NewVersion: 4}, nil
		},
	})

	resp, err := client.Rollback(context.Background(), &secrets.RollbackSecretRequest{SecretID: "app/db", TargetVersion: 2})
	require.NoError(t, err)
	assert.Equal(t, &secrets.RollbackSecretResponse{SecretID: "app/db", NewVersion: 4}, resp)
	require.NotNil(t, got)
	assert.Equal(t, &service.RollbackSecretRequest{SecretID: "app/db", TargetVersion: 2}, got)
}

func TestEmbeddedSecrets_Rollback_NotFound(t *testing.T) {
	client := newSecretsClient(t, &mockSecretsService{
		rollbackFunc: func(_ context.Context, _ *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
			return nil, errors.NewNotFound("secrets.Rollback", "no such version")
		},
	})

	_, err := client.Rollback(context.Background(), &secrets.RollbackSecretRequest{SecretID: "app/db", TargetVersion: 9})
	assert.True(t, errors.IsNotFound(err), "err = %v, want NotFound", err)
}

func TestEmbeddedSecrets_Rollback_NonPositiveVersion(t *testing.T) {
	tr, err := New(&mockQuicBaaS{secretsService: &mockSecretsService{}})
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Secrets", "Rollback", &secrets.RollbackSecretRequest{SecretID: "app/db", TargetVersion: 0})
	assert.True(t, errors.IsInvalidInput(err), "err = %v, want InvalidInput", err)
}
