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

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clisecrets "github.com/jeremyhahn/go-quicbaas/pkg/cli/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestSecretsFetchCommandStructure tests that the fetch command is properly created.
func TestSecretsFetchCommandStructure(t *testing.T) {
	cmd := newSecretsFetchCommand()

	assert.Equal(t, "fetch <prefix>", cmd.Use)
	assert.Equal(t, "Fetch secrets and write to filesystem", cmd.Short)
	assert.NotEmpty(t, cmd.Long)
}

// TestSecretsFetchCommandFlags tests that all flags are properly defined.
func TestSecretsFetchCommandFlags(t *testing.T) {
	cmd := newSecretsFetchCommand()

	// Check all expected flags exist
	flags := []string{
		"output-dir",
		"format",
		"create-dirs",
		"file-mode",
		"dir-mode",
		"flatten",
		"strip-prefix",
		"keys",
	}

	for _, flag := range flags {
		f := cmd.Flags().Lookup(flag)
		assert.NotNil(t, f, "flag %s should exist", flag)
	}

	// Check shorthand flags
	assert.NotNil(t, cmd.Flags().ShorthandLookup("o"), "output-dir should have -o shorthand")
	assert.NotNil(t, cmd.Flags().ShorthandLookup("f"), "format should have -f shorthand")
}

// TestSecretsFetchCommandFlagDefaults tests default flag values.
func TestSecretsFetchCommandFlagDefaults(t *testing.T) {
	cmd := newSecretsFetchCommand()

	assert.Equal(t, "/secrets", cmd.Flags().Lookup("output-dir").DefValue)
	assert.Equal(t, "file", cmd.Flags().Lookup("format").DefValue)
	assert.Equal(t, "true", cmd.Flags().Lookup("create-dirs").DefValue)
	assert.Equal(t, "384", cmd.Flags().Lookup("file-mode").DefValue) // 0o600 = 384
	assert.Equal(t, "448", cmd.Flags().Lookup("dir-mode").DefValue)  // 0o700 = 448
	assert.Equal(t, "false", cmd.Flags().Lookup("flatten").DefValue)
	assert.Equal(t, "true", cmd.Flags().Lookup("strip-prefix").DefValue)
}

// TestSecretsClientAdapter tests the adapter that converts SecretsClient to SecretsProvider.
func TestSecretsClientAdapter(t *testing.T) {
	// Create mock client
	mockClient := &mockSecretsClientForFetch{
		secrets: map[string]map[string][]byte{
			"apps/myapp/database": {
				"host":     []byte("localhost"),
				"password": []byte("secret123"),
			},
			"apps/myapp/cache": {
				"host": []byte("redis.local"),
			},
		},
	}

	adapter := &secretsClientAdapter{
		client: mockClient,
		limit:  100,
	}

	// Test GetSecretsByPrefix
	secrets, err := adapter.GetSecretsByPrefix(context.Background(), "apps/myapp/")
	require.NoError(t, err)
	assert.Len(t, secrets, 2)

	// Verify data was converted from []byte to string
	for _, secret := range secrets {
		for k, v := range secret.Data {
			assert.IsType(t, "", v, "value for key %s should be string", k)
		}
	}
}

// TestSecretsClientAdapter_Error tests adapter error handling.
func TestSecretsClientAdapter_Error(t *testing.T) {
	mockClient := &mockSecretsClientForFetch{
		err: assert.AnError,
	}

	adapter := &secretsClientAdapter{
		client: mockClient,
		limit:  100,
	}

	secrets, err := adapter.GetSecretsByPrefix(context.Background(), "apps/myapp/")
	assert.Error(t, err)
	assert.Nil(t, secrets)
}

// TestFormatFetchResult_Table tests table output formatting.
func TestFormatFetchResult_Table(t *testing.T) {
	result := &clisecrets.FetchResult{
		SecretsCount: 2,
		KeysCount:    5,
		FilesWritten: []string{"/secrets/db/host", "/secrets/db/password"},
	}

	var buf bytes.Buffer
	err := formatFetchResult(result, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Secrets fetched:")
	assert.Contains(t, output, "2")
	assert.Contains(t, output, "Keys written:")
	assert.Contains(t, output, "5")
	assert.Contains(t, output, "Files written:")
	assert.Contains(t, output, "/secrets/db/host")
}

// TestFormatFetchResult_JSON tests JSON output formatting.
func TestFormatFetchResult_JSON(t *testing.T) {
	result := &clisecrets.FetchResult{
		SecretsCount: 2,
		KeysCount:    5,
		FilesWritten: []string{"/secrets/db/host"},
	}

	var buf bytes.Buffer
	err := formatFetchResult(result, "json", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, `"SecretsCount"`)
	assert.Contains(t, output, `"KeysCount"`)
	assert.Contains(t, output, `"FilesWritten"`)
}

// TestFormatFetchResult_Raw tests raw output formatting (default case).
func TestFormatFetchResult_Raw(t *testing.T) {
	result := &clisecrets.FetchResult{
		SecretsCount: 3,
		KeysCount:    7,
		FilesWritten: []string{"/secrets/a", "/secrets/b"},
	}

	var buf bytes.Buffer
	err := formatFetchResult(result, "raw", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "Fetched 3 secrets")
	assert.Contains(t, output, "7 keys")
	assert.Contains(t, output, "2 files")
}

// TestFormatFetchResult_EmptyResult tests formatting with no results.
func TestFormatFetchResult_EmptyResult(t *testing.T) {
	result := &clisecrets.FetchResult{
		SecretsCount: 0,
		KeysCount:    0,
		FilesWritten: []string{},
	}

	var buf bytes.Buffer
	err := formatFetchResult(result, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "0")
}

// mockSecretsClientForFetch implements SecretsClient for testing fetch functionality.
type mockSecretsClientForFetch struct {
	secrets map[string]map[string][]byte
	err     error
}

func (m *mockSecretsClientForFetch) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &service.GetSecretsByPrefixResponse{
		Secrets: m.secrets,
	}, nil
}

func (m *mockSecretsClientForFetch) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	return nil, nil
}

func (m *mockSecretsClientForFetch) Close() error {
	return nil
}
