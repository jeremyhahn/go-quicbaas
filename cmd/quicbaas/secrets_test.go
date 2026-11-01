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
	"encoding/json"
	"strings"
	"testing"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clisecrets "github.com/jeremyhahn/go-quicbaas/pkg/cli/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestNewSecretsCommand(t *testing.T) {
	cmd := newSecretsCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "secrets", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Check that all expected subcommands exist
	expectedCommands := []string{"put", "get", "get-prefix", "list", "delete", "destroy", "versions", "rollback", "enable", "disable", "watch"}
	subCommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subCommands), len(expectedCommands))

	for _, expected := range expectedCommands {
		found := false
		for _, sub := range subCommands {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected to find subcommand '%s'", expected)
	}
}

func TestSecretsPutCommandFlags(t *testing.T) {
	cmd := newSecretsPutCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	ttlFlag := cmd.Flags().Lookup("ttl")
	assert.NotNil(t, ttlFlag)
	assert.Equal(t, "0s", ttlFlag.DefValue)
}

// TestSecretsPutCommandSuccess tests put command with mock success.
func TestSecretsPutCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		StoreSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
			return &service.StoreSecretResponse{
				SecretID:  "database/prod",
				Version:   1,
				Timestamp: time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsPutCommand()
	secretsOutputFmt = "json"
	defer func() { secretsOutputFmt = "table" }()

	cmd.SetArgs([]string{"database/prod", "--data", "password=secret123"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsPutCommandInvalidDataFormat tests put with invalid data format.
func TestSecretsPutCommandInvalidDataFormat(t *testing.T) {
	cmd := newSecretsPutCommand()
	cmd.SetArgs([]string{"test/path", "--data", "invalid"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid data format")
}

// TestSecretsPutCommandFactoryError tests put with factory error.
func TestSecretsPutCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.put", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsPutCommand()
	cmd.SetArgs([]string{"database/prod", "--data", "password=secret"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestSecretsPutCommandWithTTL tests put command with TTL.
func TestSecretsPutCommandWithTTL(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		StoreSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
			assert.NotNil(t, req.ExpiresAt)
			return &service.StoreSecretResponse{
				SecretID:  "temp/token",
				Version:   1,
				Timestamp: time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsPutCommand()
	cmd.SetArgs([]string{"temp/token", "--data", "token=abc123", "--ttl", "1h"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestSecretsGetCommandFlags(t *testing.T) {
	cmd := newSecretsGetCommand()
	assert.NotNil(t, cmd)

	versionFlag := cmd.Flags().Lookup("version")
	assert.NotNil(t, versionFlag)
	assert.Equal(t, "0", versionFlag.DefValue)
}

// TestSecretsGetCommandSuccess tests get command with mock success.
func TestSecretsGetCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	dataMap := map[string]interface{}{
		"password": "secret123",
		"username": "admin",
	}
	dataBytes, _ := json.Marshal(dataMap)

	mockClient := &MockSecretsClient{
		GetSecretFunc: func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
			return &service.GetSecretResponse{
				SecretID:  "database/prod",
				Version:   1,
				Data:      dataBytes,
				CreatedAt: time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsGetCommand()
	secretsOutputFmt = "json"
	defer func() { secretsOutputFmt = "table" }()

	cmd.SetArgs([]string{"database/prod"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsGetCommandWithVersion tests get with specific version.
func TestSecretsGetCommandWithVersion(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	dataMap := map[string]interface{}{"key": "value"}
	dataBytes, _ := json.Marshal(dataMap)

	mockClient := &MockSecretsClient{
		GetSecretFunc: func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
			assert.Equal(t, uint64(2), req.Version)
			return &service.GetSecretResponse{
				SecretID:  "database/prod",
				Version:   2,
				Data:      dataBytes,
				CreatedAt: time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsGetCommand()
	cmd.SetArgs([]string{"database/prod", "--version", "2"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsGetCommandFactoryError tests get with factory error.
func TestSecretsGetCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.get", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsGetCommand()
	cmd.SetArgs([]string{"database/prod"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestSecretsListCommandFlags(t *testing.T) {
	cmd := newSecretsListCommand()
	assert.NotNil(t, cmd)

	prefixFlag := cmd.Flags().Lookup("prefix")
	assert.NotNil(t, prefixFlag)

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "100", limitFlag.DefValue)
}

// TestSecretsListCommandSuccess tests list command with mock success.
func TestSecretsListCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	now := time.Now()
	mockClient := &MockSecretsClient{
		ListSecretsFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
			return &service.ListSecretsResponse{
				Secrets: []*service.Secret{
					{
						SecretID:       "database/prod",
						Name:           "Database Production",
						CurrentVersion: 1,
						CreatedAt:      now,
						UpdatedAt:      now,
					},
					{
						SecretID:       "app/config",
						Name:           "App Config",
						CurrentVersion: 2,
						CreatedAt:      now,
						UpdatedAt:      now,
					},
				},
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsListCommand()
	secretsOutputFmt = "json"
	defer func() { secretsOutputFmt = "table" }()

	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsListCommandWithPrefix tests list with prefix filter.
func TestSecretsListCommandWithPrefix(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		ListSecretsFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
			assert.Equal(t, "database/", req.Filter["prefix"])
			return &service.ListSecretsResponse{Secrets: []*service.Secret{}}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsListCommand()
	cmd.SetArgs([]string{"--prefix", "database/"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsListCommandFactoryError tests list with factory error.
func TestSecretsListCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.list", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsListCommand()
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestSecretsDeleteCommandFlags(t *testing.T) {
	cmd := newSecretsDeleteCommand()
	assert.NotNil(t, cmd)

	permanentFlag := cmd.Flags().Lookup("permanent")
	assert.NotNil(t, permanentFlag)
	assert.Equal(t, "false", permanentFlag.DefValue)
}

// TestSecretsDeleteCommandSuccess tests delete command with mock success.
func TestSecretsDeleteCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		DeleteSecretFunc: func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
			assert.False(t, req.PermanentlyDelete)
			return &service.DeleteSecretResponse{
				SecretID:  "database/prod",
				Deleted:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsDeleteCommand()
	secretsOutputFmt = "json"
	defer func() { secretsOutputFmt = "table" }()

	cmd.SetArgs([]string{"database/prod"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsDeleteCommandPermanent tests delete with permanent flag.
func TestSecretsDeleteCommandPermanent(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		DeleteSecretFunc: func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
			assert.True(t, req.PermanentlyDelete)
			return &service.DeleteSecretResponse{
				SecretID:  "database/prod",
				Deleted:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsDeleteCommand()
	cmd.SetArgs([]string{"database/prod", "--permanent"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsDeleteCommandFactoryError tests delete with factory error.
func TestSecretsDeleteCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.delete", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsDeleteCommand()
	cmd.SetArgs([]string{"database/prod"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestSecretsDestroyCommand tests destroy command.
func TestSecretsDestroyCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		DeleteSecretFunc: func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
			assert.True(t, req.PermanentlyDelete) // Destroy always uses permanent delete
			return &service.DeleteSecretResponse{
				SecretID:  "database/old",
				Deleted:   true,
				Timestamp: time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsDestroyCommand()
	cmd.SetArgs([]string{"database/old"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsDestroyCommandFactoryError tests destroy with factory error.
func TestSecretsDestroyCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.destroy", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsDestroyCommand()
	cmd.SetArgs([]string{"database/old"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestSecretsVersionsCommandSuccess tests versions command with mock success.
func TestSecretsVersionsCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	now := time.Now()
	deletedTime := now.Add(-1 * time.Hour)

	mockClient := &MockSecretsClient{
		ListSecretVersionsFunc: func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
			return &service.ListSecretVersionsResponse{
				Versions: []*service.SecretVersion{
					{
						Version:   3,
						CreatedAt: now,
						CreatedBy: "admin",
						DeletedAt: nil,
					},
					{
						Version:   2,
						CreatedAt: now.Add(-2 * time.Hour),
						CreatedBy: "user",
						DeletedAt: &deletedTime,
					},
					{
						Version:   1,
						CreatedAt: now.Add(-4 * time.Hour),
						CreatedBy: "",
						DeletedAt: nil,
					},
				},
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsVersionsCommand()
	secretsOutputFmt = "json"
	defer func() { secretsOutputFmt = "table" }()

	cmd.SetArgs([]string{"database/prod"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsVersionsCommandFactoryError tests versions with factory error.
func TestSecretsVersionsCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.versions", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsVersionsCommand()
	cmd.SetArgs([]string{"database/prod"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestSecretsRollbackCommandFlags(t *testing.T) {
	cmd := newSecretsRollbackCommand()
	assert.NotNil(t, cmd)

	versionFlag := cmd.Flags().Lookup("version")
	assert.NotNil(t, versionFlag)
	assert.Equal(t, "0", versionFlag.DefValue)
}

// TestSecretsRollbackCommandValidation tests rollback validation.
func TestSecretsRollbackCommandValidation(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "version 0",
			args:        []string{"database/prod", "--version", "0"},
			expectError: true,
			errorMsg:    "version must be greater than 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newSecretsRollbackCommand()
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			}
		})
	}
}

// TestSecretsRollbackCommandSuccess tests rollback with mock success.
func TestSecretsRollbackCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		RollbackSecretFunc: func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
			assert.Equal(t, uint64(2), req.TargetVersion)
			return &service.RollbackSecretResponse{
				SecretID:   "database/prod",
				NewVersion: 4,
				Timestamp:  time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsRollbackCommand()
	secretsOutputFmt = "json"
	defer func() { secretsOutputFmt = "table" }()

	cmd.SetArgs([]string{"database/prod", "--version", "2"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// TestSecretsRollbackCommandFactoryError tests rollback with factory error.
func TestSecretsRollbackCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.rollback", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsRollbackCommand()
	cmd.SetArgs([]string{"database/prod", "--version", "2"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestFormatSecretsStoreResponse(t *testing.T) {
	resp := &service.StoreSecretResponse{
		SecretID:  "database/prod",
		Version:   1,
		Timestamp: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"database/prod", "version", "timestamp"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Secret ID", "database/prod", "Version", "1"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Secret stored", "database/prod", "version 1"},
		},
		{
			name:         "raw format",
			format:       "raw",
			expectedText: []string{"Secret stored", "database/prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsStoreResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSecretsGetResponse(t *testing.T) {
	dataMap := map[string]interface{}{
		"password": "secret123",
		"username": "admin",
	}
	dataBytes, _ := json.Marshal(dataMap)

	resp := &service.GetSecretResponse{
		SecretID:  "database/prod",
		Version:   1,
		Data:      dataBytes,
		CreatedAt: time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"database/prod", "version", "data", "password", "username"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Secret ID", "database/prod", "Version", "Data", "password", "username"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"password=", "username="},
		},
		{
			name:         "raw format",
			format:       "raw",
			expectedText: []string{"admin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsGetResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSecretsListResponse(t *testing.T) {
	now := time.Now()
	resp := &service.ListSecretsResponse{
		Secrets: []*service.Secret{
			{
				SecretID:       "database/prod",
				Name:           "Database Production",
				CurrentVersion: 1,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
			{
				SecretID:       "app/config",
				Name:           "App Config",
				CurrentVersion: 2,
				CreatedAt:      now,
				UpdatedAt:      now,
			},
		},
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"database/prod", "app/config", "secrets"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"SECRET ID", "VERSION", "database/prod", "app/config", "Total: 2 secrets"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"database/prod", "app/config", "Total: 2 secrets"},
		},
		{
			name:         "raw format",
			format:       "raw",
			expectedText: []string{"database/prod", "app/config", "Total: 2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsListResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSecretsDeleteResponse(t *testing.T) {
	tests := []struct {
		name         string
		deleted      bool
		format       string
		expectedText []string
	}{
		{
			name:         "deleted json",
			deleted:      true,
			format:       "json",
			expectedText: []string{"database/prod", "deleted", "true"},
		},
		{
			name:         "not deleted json",
			deleted:      false,
			format:       "json",
			expectedText: []string{"database/prod", "deleted", "false"},
		},
		{
			name:         "deleted table",
			deleted:      true,
			format:       "table",
			expectedText: []string{"Secret ID", "database/prod", "Deleted", "true"},
		},
		{
			name:         "deleted default",
			deleted:      true,
			format:       "",
			expectedText: []string{"Secret deleted", "database/prod"},
		},
		{
			name:         "not deleted default",
			deleted:      false,
			format:       "",
			expectedText: []string{"Failed to delete secret", "database/prod"},
		},
		{
			name:         "deleted raw",
			deleted:      true,
			format:       "raw",
			expectedText: []string{"Secret deleted"},
		},
		{
			name:         "not deleted raw",
			deleted:      false,
			format:       "raw",
			expectedText: []string{"Failed to delete"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &service.DeleteSecretResponse{
				SecretID:  "database/prod",
				Deleted:   tt.deleted,
				Timestamp: time.Now(),
			}

			var buf bytes.Buffer
			err := formatSecretsDeleteResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSecretsVersionsResponse(t *testing.T) {
	now := time.Now()
	deletedTime := now.Add(-1 * time.Hour)
	resp := &service.ListSecretVersionsResponse{
		Versions: []*service.SecretVersion{
			{
				Version:   3,
				CreatedAt: now,
				CreatedBy: "admin",
				DeletedAt: nil,
			},
			{
				Version:   2,
				CreatedAt: now.Add(-2 * time.Hour),
				CreatedBy: "user",
				DeletedAt: &deletedTime,
			},
			{
				Version:   1,
				CreatedAt: now.Add(-4 * time.Hour),
				CreatedBy: "",
				DeletedAt: nil,
			},
		},
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"versions", "version", "created_by"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"VERSION", "CREATED", "admin", "user", "yes", "no", "Total: 3 versions"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"v3", "v2", "v1", "Total: 3 versions"},
		},
		{
			name:         "raw format",
			format:       "raw",
			expectedText: []string{"v3", "v2", "v1", "Total: 3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsVersionsResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSecretsRollbackResponse(t *testing.T) {
	resp := &service.RollbackSecretResponse{
		SecretID:   "database/prod",
		NewVersion: 4,
		Timestamp:  time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"database/prod", "new_version", "4"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Secret ID", "database/prod", "New Version", "4"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Secret rolled back", "database/prod", "new version 4"},
		},
		{
			name:         "raw format",
			format:       "raw",
			expectedText: []string{"Secret rolled back", "4"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsRollbackResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

// =============================================================================
// Tests for get-prefix command
// =============================================================================

func TestSecretsGetPrefixCommandFlags(t *testing.T) {
	cmd := newSecretsGetPrefixCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "get-prefix <prefix>", cmd.Use)

	includeDataFlag := cmd.Flags().Lookup("include-data")
	assert.NotNil(t, includeDataFlag)
	assert.Equal(t, "true", includeDataFlag.DefValue)

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "100", limitFlag.DefValue)
}

func TestSecretsGetPrefixCommandSuccess(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		GetSecretsByPrefixFunc: func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
			assert.Equal(t, "apps/myapp/", req.Prefix)
			assert.True(t, req.IncludeData)
			return &service.GetSecretsByPrefixResponse{
				Secrets: map[string]map[string][]byte{
					"apps/myapp/db":    {"password": []byte("secret123")},
					"apps/myapp/cache": {"host": []byte("localhost")},
				},
				Metadata: map[string]map[string]string{
					"apps/myapp/db":    {"owner": "admin"},
					"apps/myapp/cache": {"owner": "devops"},
				},
				Version:    1,
				TotalCount: 2,
				Timestamp:  time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsGetPrefixCommand()
	secretsOutputFmt = "json"
	defer func() { secretsOutputFmt = "table" }()

	cmd.SetArgs([]string{"apps/myapp/"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestSecretsGetPrefixCommandWithLimit(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		GetSecretsByPrefixFunc: func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
			assert.Equal(t, 50, req.Limit)
			return &service.GetSecretsByPrefixResponse{
				Secrets:    map[string]map[string][]byte{},
				TotalCount: 0,
				Timestamp:  time.Now(),
			}, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsGetPrefixCommand()
	cmd.SetArgs([]string{"apps/", "--limit", "50"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestSecretsGetPrefixCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.get-prefix", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsGetPrefixCommand()
	cmd.SetArgs([]string{"apps/"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestSecretsGetPrefixCommandServiceError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		GetSecretsByPrefixFunc: func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
			return nil, errors.Newf(errors.ErrForbidden, "secrets.get-prefix", "access denied")
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsGetPrefixCommand()
	cmd.SetArgs([]string{"apps/"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

// =============================================================================
// Tests for watch command
// =============================================================================

func TestSecretsWatchCommandFlags(t *testing.T) {
	cmd := newSecretsWatchCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "watch <prefix>", cmd.Use)

	timeoutFlag := cmd.Flags().Lookup("timeout")
	assert.NotNil(t, timeoutFlag)
	assert.Equal(t, "0s", timeoutFlag.DefValue)
}

func TestSecretsWatchCommandFactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "secrets.watch", "connection refused")
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, factoryErr
	}

	cmd := newSecretsWatchCommand()
	cmd.SetArgs([]string{"apps/"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestSecretsWatchCommandServiceError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	mockClient := &MockSecretsClient{
		WatchSecretsByPrefixFunc: func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return nil, errors.Newf(errors.ErrForbidden, "secrets.watch", "access denied")
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsWatchCommand()
	cmd.SetArgs([]string{"apps/"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "access denied")
}

func TestSecretsWatchCommandWithTimeout(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	changes := make(chan *service.SecretChange)
	close(changes)

	mockClient := &MockSecretsClient{
		WatchSecretsByPrefixFunc: func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return changes, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsWatchCommand()
	cmd.SetArgs([]string{"apps/", "--timeout", "100ms"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestSecretsWatchCommandStreamClose(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	changes := make(chan *service.SecretChange)
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(changes)
	}()

	mockClient := &MockSecretsClient{
		WatchSecretsByPrefixFunc: func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return changes, nil
		},
	}

	secretsClientFactory = func() (SecretsClient, error) {
		return mockClient, nil
	}

	cmd := newSecretsWatchCommand()
	cmd.SetArgs([]string{"apps/"})

	err := cmd.Execute()
	require.NoError(t, err)
}

// =============================================================================
// Tests for execute functions
// =============================================================================

// =============================================================================
// Tests for formatting functions
// =============================================================================

func TestFormatSecretsGetPrefixResponse(t *testing.T) {
	resp := &service.GetSecretsByPrefixResponse{
		Secrets: map[string]map[string][]byte{
			"apps/myapp/db":    {"password": []byte("secret123")},
			"apps/myapp/cache": {"host": []byte("localhost")},
		},
		Metadata: map[string]map[string]string{
			"apps/myapp/db":    {"owner": "admin"},
			"apps/myapp/cache": {"owner": "devops"},
		},
		Version:    1,
		TotalCount: 2,
		HasMore:    false,
		Timestamp:  time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"secrets", "apps/myapp/db", "apps/myapp/cache", "total_count"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"PATH", "KEYS", "Total: 2 secrets"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Total: 2 secrets"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsGetPrefixResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSecretsGetPrefixResponseHasMore(t *testing.T) {
	resp := &service.GetSecretsByPrefixResponse{
		Secrets:    map[string]map[string][]byte{},
		TotalCount: 100,
		HasMore:    true,
		NextCursor: "cursor123",
		Timestamp:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatSecretsGetPrefixResponse(resp, "table", &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "More results available")
}

func TestFormatSecretsWatchChange(t *testing.T) {
	change := &service.SecretChange{
		Path:       "apps/myapp/db",
		ChangeType: service.SecretChangeUpdated,
		Data:       map[string][]byte{"password": []byte("newsecret")},
		Metadata:   map[string]string{"version": "2"},
		Version:    2,
		Timestamp:  time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"apps/myapp/db", "updated", "version"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"UPDATED", "apps/myapp/db", "(v2)"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"UPDATED", "apps/myapp/db", "(v2)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsWatchChange(change, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatSecretsWatchChangeTypes(t *testing.T) {
	tests := []struct {
		name       string
		changeType service.SecretChangeType
		expected   string
	}{
		{
			name:       "created",
			changeType: service.SecretChangeCreated,
			expected:   "CREATED",
		},
		{
			name:       "updated",
			changeType: service.SecretChangeUpdated,
			expected:   "UPDATED",
		},
		{
			name:       "deleted",
			changeType: service.SecretChangeDeleted,
			expected:   "DELETED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := &service.SecretChange{
				Path:       "apps/test",
				ChangeType: tt.changeType,
				Version:    1,
				Timestamp:  time.Now(),
			}

			var buf bytes.Buffer
			err := formatSecretsWatchChange(change, "table", &buf)
			require.NoError(t, err)
			assert.Contains(t, buf.String(), tt.expected)
		})
	}
}

// =============================================================================
// Tests for mTLS and K8s auth functionality
// =============================================================================

func TestSecretsCommandTLSFlags(t *testing.T) {
	cmd := newSecretsCommand()
	assert.NotNil(t, cmd)

	// Verify TLS flags exist
	tlsFlag := cmd.PersistentFlags().Lookup("tls")
	assert.NotNil(t, tlsFlag, "Expected --tls flag")
	assert.Equal(t, "false", tlsFlag.DefValue)

	certFlag := cmd.PersistentFlags().Lookup("cert")
	assert.NotNil(t, certFlag, "Expected --cert flag")
	assert.Equal(t, "", certFlag.DefValue)

	keyFlag := cmd.PersistentFlags().Lookup("key")
	assert.NotNil(t, keyFlag, "Expected --key flag")
	assert.Equal(t, "", keyFlag.DefValue)

	caFlag := cmd.PersistentFlags().Lookup("ca")
	assert.NotNil(t, caFlag, "Expected --ca flag")
	assert.Equal(t, "", caFlag.DefValue)
}

func TestSecretsCommandAuthFlag(t *testing.T) {
	cmd := newSecretsCommand()
	assert.NotNil(t, cmd)

	// Verify auth flag exists
	authFlag := cmd.PersistentFlags().Lookup("auth")
	assert.NotNil(t, authFlag, "Expected --auth flag")
	assert.Equal(t, "token", authFlag.DefValue)
}

func TestCreateSecretsGRPCClient_TLSWithoutCerts(t *testing.T) {
	// Save original values
	origTLSEnabled := secretsTLSEnabled
	origTLSCert := secretsTLSCert
	origTLSKey := secretsTLSKey
	origTLSCA := secretsTLSCA
	origAuthMode := secretsAuthMode
	origAddress := address
	defer func() {
		secretsTLSEnabled = origTLSEnabled
		secretsTLSCert = origTLSCert
		secretsTLSKey = origTLSKey
		secretsTLSCA = origTLSCA
		secretsAuthMode = origAuthMode
		address = origAddress
	}()

	// Set TLS enabled but no certs
	secretsTLSEnabled = true
	secretsTLSCert = ""
	secretsTLSKey = ""
	secretsTLSCA = ""
	secretsAuthMode = "token"
	address = "localhost:9090"

	_, err := createSecretsGRPCClient()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS is required for gRPC connections")
}

func TestCreateSecretsGRPCClient_InvalidAuthMode(t *testing.T) {
	// Save original values
	origTLSEnabled := secretsTLSEnabled
	origAuthMode := secretsAuthMode
	origAddress := address
	defer func() {
		secretsTLSEnabled = origTLSEnabled
		secretsAuthMode = origAuthMode
		address = origAddress
	}()

	secretsTLSEnabled = false
	secretsAuthMode = "invalid-auth-mode"
	address = "localhost:9090"

	_, err := createSecretsGRPCClient()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported auth mode")
}

func TestGetK8sServiceAccountToken_FileNotFound(t *testing.T) {
	// The token file doesn't exist in unit test environment
	_, err := getK8sServiceAccountToken()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read token")
}

func TestCreateSecretsGRPCClient_K8sAuth_TokenNotFound(t *testing.T) {
	// Save original values
	origTLSEnabled := secretsTLSEnabled
	origAuthMode := secretsAuthMode
	origAddress := address
	defer func() {
		secretsTLSEnabled = origTLSEnabled
		secretsAuthMode = origAuthMode
		address = origAddress
	}()

	secretsTLSEnabled = false
	secretsAuthMode = "k8s"
	address = "localhost:9090"

	// K8s auth should fail because token file doesn't exist
	_, err := createSecretsGRPCClient()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read Kubernetes service account token")
}

func TestCreateSecretsGRPCClient_TLSRequired(t *testing.T) {
	// Save original values
	origTLSEnabled := secretsTLSEnabled
	origAuthMode := secretsAuthMode
	origAddress := address
	origTimeout := globalTimeout
	defer func() {
		secretsTLSEnabled = origTLSEnabled
		secretsAuthMode = origAuthMode
		address = origAddress
		globalTimeout = origTimeout
	}()

	secretsTLSEnabled = false
	secretsAuthMode = "token"
	address = "invalid-address-for-test:99999"
	globalTimeout = 100 * time.Millisecond

	// TLS is required for all gRPC connections
	_, err := createSecretsGRPCClient()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TLS is required for gRPC connections")
}

func TestK8sServiceAccountTokenPaths(t *testing.T) {
	// Verify the constants are correctly defined
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/token", k8sServiceAccountTokenPath)
	assert.Equal(t, "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt", k8sServiceAccountCAPath)
}

func TestParseSymmetricAlgorithm_AllCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected kctypes.SymmetricAlgorithm
	}{
		{
			name:     "AES-128-GCM uppercase",
			input:    "AES-128-GCM",
			expected: kctypes.SymmetricAES128GCM,
		},
		{
			name:     "AES-128-GCM lowercase",
			input:    "aes-128-gcm",
			expected: kctypes.SymmetricAES128GCM,
		},
		{
			name:     "AES-192-GCM uppercase",
			input:    "AES-192-GCM",
			expected: kctypes.SymmetricAES192GCM,
		},
		{
			name:     "AES-192-GCM lowercase",
			input:    "aes-192-gcm",
			expected: kctypes.SymmetricAES192GCM,
		},
		{
			name:     "AES-256-GCM uppercase",
			input:    "AES-256-GCM",
			expected: kctypes.SymmetricAES256GCM,
		},
		{
			name:     "AES-256-GCM lowercase",
			input:    "aes-256-gcm",
			expected: kctypes.SymmetricAES256GCM,
		},
		{
			name:     "CHACHA20-POLY1305 uppercase",
			input:    "CHACHA20-POLY1305",
			expected: kctypes.SymmetricChaCha20Poly1305,
		},
		{
			name:     "CHACHA20-POLY1305 lowercase",
			input:    "chacha20-poly1305",
			expected: kctypes.SymmetricChaCha20Poly1305,
		},
		{
			name:     "XCHACHA20-POLY1305 uppercase",
			input:    "XCHACHA20-POLY1305",
			expected: kctypes.SymmetricXChaCha20Poly1305,
		},
		{
			name:     "XCHACHA20-POLY1305 lowercase",
			input:    "xchacha20-poly1305",
			expected: kctypes.SymmetricXChaCha20Poly1305,
		},
		{
			name:     "Unknown algorithm returns empty",
			input:    "UNKNOWN",
			expected: "",
		},
		{
			name:     "Empty string returns empty",
			input:    "",
			expected: "",
		},
		{
			name:     "Asymmetric algorithm returns empty",
			input:    "RSA-2048",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSymmetricAlgorithm(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// createKeyAttributes Coverage Tests
// =============================================================================

func TestCreateKeyAttributes_SymmetricAlgorithms(t *testing.T) {
	tests := []struct {
		name       string
		keyName    string
		algo       string
		exportable bool
		wantErr    bool
	}{
		{
			name:       "AES-128-GCM",
			keyName:    "test-key",
			algo:       "AES-128-GCM",
			exportable: true,
			wantErr:    false,
		},
		{
			name:       "AES-256-GCM",
			keyName:    "test-key",
			algo:       "AES-256-GCM",
			exportable: false,
			wantErr:    false,
		},
		{
			name:       "CHACHA20-POLY1305",
			keyName:    "chacha-key",
			algo:       "CHACHA20-POLY1305",
			exportable: true,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := createKeyAttributes(tt.keyName, tt.algo, tt.exportable)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.keyName, attrs.CN)
				assert.Equal(t, tt.exportable, attrs.Exportable)
			}
		})
	}
}

func TestCreateKeyAttributes_AsymmetricAlgorithms(t *testing.T) {
	tests := []struct {
		name       string
		keyName    string
		algo       string
		exportable bool
		wantErr    bool
	}{
		{
			name:       "ED25519",
			keyName:    "ed-key",
			algo:       "ED25519",
			exportable: false,
			wantErr:    false,
		},
		{
			name:       "RSA-2048",
			keyName:    "rsa-key",
			algo:       "RSA-2048",
			exportable: true,
			wantErr:    false,
		},
		{
			name:       "RSA-3072",
			keyName:    "rsa-key",
			algo:       "RSA-3072",
			exportable: true,
			wantErr:    false,
		},
		{
			name:       "RSA-4096",
			keyName:    "rsa-key",
			algo:       "RSA-4096",
			exportable: true,
			wantErr:    false,
		},
		{
			name:       "ECDSA-P256",
			keyName:    "ec-key",
			algo:       "ECDSA-P256",
			exportable: false,
			wantErr:    false,
		},
		{
			name:       "ECDSA-P384",
			keyName:    "ec-key",
			algo:       "ECDSA-P384",
			exportable: false,
			wantErr:    false,
		},
		{
			name:       "ECDSA-P521",
			keyName:    "ec-key",
			algo:       "ECDSA-P521",
			exportable: false,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attrs, err := createKeyAttributes(tt.keyName, tt.algo, tt.exportable)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.keyName, attrs.CN)
				assert.Equal(t, tt.exportable, attrs.Exportable)
			}
		})
	}
}

func TestCreateKeyAttributes_InvalidAlgorithm(t *testing.T) {
	attrs, err := createKeyAttributes("test-key", "INVALID-ALGORITHM", false)
	assert.Error(t, err)
	assert.Nil(t, attrs)
	assert.Contains(t, err.Error(), "unsupported algorithm")
}

// =============================================================================
// Secrets Command Tests - Execution Functions
// =============================================================================

func TestExecuteSecretsRotate_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.RotateSecretRequest{
		SecretID: "test/secret",
		NewData:  []byte(`{"key":"value"}`),
	}

	mockClient := &MockSecretsClient{
		RotateSecretFunc: func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
			return &service.RotateSecretResponse{
				SecretID:   req.SecretID,
				NewVersion: 2,
				Timestamp:  time.Now(),
			}, nil
		},
	}

	resp, err := executeSecretsRotate(ctx, mockClient, req)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestFormatSecretsRotateResponse_AllFormats(t *testing.T) {
	resp := &service.RotateSecretResponse{
		SecretID:   "test/secret",
		NewVersion: 2,
		Timestamp:  time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "JSON format",
			format:   "json",
			contains: []string{"test/secret", "2"},
		},
		{
			name:     "table format",
			format:   "table",
			contains: []string{"test/secret", "2"},
		},
		{
			name:     "raw format",
			format:   "raw",
			contains: []string{"Secret rotated"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsRotateResponse(resp, tt.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, c := range tt.contains {
				assert.Contains(t, output, c)
			}
		})
	}
}

// =============================================================================
// Secrets Execution Function Tests
// =============================================================================

// TestExecuteSecrets_NilClient checks that every secrets verb rejects a nil
// client with the invalid client type error and returns no result.
func TestExecuteSecrets_NilClient(t *testing.T) {
	tests := []struct {
		name string
		run  func(ctx context.Context) (resultIsNil bool, err error)
	}{
		{"store", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsStore(ctx, nil, &service.StoreSecretRequest{
				SecretID: "test/secret",
				Data:     []byte(`{"key":"value"}`),
			})
			return resp == nil, err
		}},
		{"get", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsGet(ctx, nil, &service.GetSecretRequest{SecretID: "test/secret"})
			return resp == nil, err
		}},
		{"list", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsList(ctx, nil, &service.ListSecretsRequest{Prefix: "test/"})
			return resp == nil, err
		}},
		{"delete", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsDelete(ctx, nil, &service.DeleteSecretRequest{SecretID: "test/secret"})
			return resp == nil, err
		}},
		{"list versions", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsListVersions(ctx, nil, &service.ListSecretVersionsRequest{SecretID: "test/secret"})
			return resp == nil, err
		}},
		{"rollback", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsRollback(ctx, nil, &service.RollbackSecretRequest{
				SecretID:      "test/secret",
				TargetVersion: 1,
			})
			return resp == nil, err
		}},
		{"rotate", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsRotate(ctx, nil, &service.RotateSecretRequest{
				SecretID: "test/secret",
				NewData:  []byte(`{"key":"value"}`),
			})
			return resp == nil, err
		}},
		{"get by prefix", func(ctx context.Context) (bool, error) {
			resp, err := executeSecretsGetByPrefix(ctx, nil, &service.GetSecretsByPrefixRequest{Prefix: "test/"})
			return resp == nil, err
		}},
		{"watch", func(ctx context.Context) (bool, error) {
			ch, err := executeSecretsWatch(ctx, nil, &service.WatchSecretsRequest{Prefix: "test/"})
			return ch == nil, err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resultIsNil, err := tt.run(context.Background())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid client type")
			assert.True(t, resultIsNil)
		})
	}
}

// =============================================================================
// Secrets Response Formatting Tests
// =============================================================================

func TestFormatSecretsStatusResponse_AllFormats(t *testing.T) {
	resp := &service.UpdateSecretStatusResponse{
		SecretID:  "test/secret",
		Timestamp: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "JSON format",
			format:   "json",
			contains: []string{"test/secret"},
		},
		{
			name:     "table format",
			format:   "table",
			contains: []string{"test/secret"},
		},
		{
			name:     "raw format",
			format:   "raw",
			contains: []string{"test/secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsStatusResponse(resp, tt.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, c := range tt.contains {
				assert.Contains(t, output, c)
			}
		})
	}
}

// =============================================================================
// Timeseries Client Factory Stub Tests
// =============================================================================

func TestCreateTimeSeriesClient_Stub(t *testing.T) {
	// Without the timeseries build tag, the stub is compiled which returns
	// ErrTimeSeriesNotCompiled. With the timeseries build tag, the real
	// implementation is compiled. This test verifies an error is returned
	// in either case when using an unsupported/invalid transport.
	savedTransport := transport
	defer func() { transport = savedTransport }()

	transport = "unsupported_for_test"

	client, err := createTimeSeriesClient()
	require.Error(t, err)
	assert.Nil(t, client)
	// Accept either error message depending on build configuration:
	// - "timeseries support not compiled" (stub)
	// - "unsupported transport" (real implementation)
	errMsg := strings.ToLower(err.Error())
	assert.True(t,
		strings.Contains(errMsg, "timeseries") || strings.Contains(errMsg, "unsupported transport"),
		"expected timeseries or unsupported transport error, got: %s", err.Error())
}

// =============================================================================
// QUIC Client Stub Tests
// =============================================================================

func TestCreateQUICSecretsClient_Stub(t *testing.T) {
	client, err := createQUICSecretsClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

// =============================================================================
// Secrets Formatting - Store, Get, List, Delete, Versions, Rollback
// =============================================================================

func TestFormatSecretsStoreResponse_InvalidFormat(t *testing.T) {
	resp := &service.StoreSecretResponse{
		SecretID:  "test/secret",
		Timestamp: time.Now(),
	}
	var buf bytes.Buffer
	err := formatSecretsStoreResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSecretsGetResponse_InvalidData(t *testing.T) {
	resp := &service.GetSecretResponse{
		SecretID: "test/secret",
		Data:     []byte(`not-json`),
	}
	var buf bytes.Buffer
	err := formatSecretsGetResponse(resp, "json", &buf)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

// =============================================================================
// Secrets GetPrefix and Watch Formatting Tests
// =============================================================================

func TestFormatSecretsGetPrefixResponse_AllFormats(t *testing.T) {
	resp := &service.GetSecretsByPrefixResponse{
		Secrets: map[string]map[string][]byte{
			"apps/myapp/db": {
				"password": []byte("secret123"),
			},
		},
		TotalCount: 1,
		HasMore:    true,
		Timestamp:  time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{"json", "json", []string{"apps/myapp/db"}},
		{"table", "table", []string{"apps/myapp/db", "Total: 1", "More results"}},
		{"raw", "raw", []string{"apps/myapp/db", "Total: 1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsGetPrefixResponse(resp, tt.format, &buf)
			require.NoError(t, err)
			for _, c := range tt.contains {
				assert.Contains(t, buf.String(), c)
			}
		})
	}
}

func TestFormatSecretsWatchChange_AllFormats(t *testing.T) {
	change := &service.SecretChange{
		Path:       "apps/myapp/db",
		ChangeType: service.SecretChangeCreated,
		Version:    1,
		Data:       map[string][]byte{"password": []byte("secret")},
		Metadata:   map[string]string{"author": "admin"},
		Timestamp:  time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{"json", "json", []string{"apps/myapp/db", "created"}},
		{"table", "table", []string{"apps/myapp/db"}},
		{"raw", "raw", []string{"apps/myapp/db", "CREATED"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSecretsWatchChange(change, tt.format, &buf)
			require.NoError(t, err)
			for _, c := range tt.contains {
				assert.Contains(t, buf.String(), c)
			}
		})
	}
}

func TestFormatSecretsWatchChange_WithoutDataAndMetadata(t *testing.T) {
	change := &service.SecretChange{
		Path:       "apps/myapp/db",
		ChangeType: service.SecretChangeDeleted,
		Version:    2,
		Timestamp:  time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	var buf bytes.Buffer
	err := formatSecretsWatchChange(change, "json", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "deleted")
	assert.NotContains(t, buf.String(), "data")
}

// =============================================================================
// Secrets Fetch Formatting Tests
// =============================================================================

func TestFormatFetchResult_AllFormats(t *testing.T) {
	result := &clisecrets.FetchResult{
		SecretsCount: 2,
		KeysCount:    4,
		FilesWritten: []string{"/secrets/db/password", "/secrets/db/username"},
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{"json", "json", []string{"2", "4"}},
		{"table", "table", []string{"2", "4"}},
		{"raw", "raw", []string{"Fetched 2 secrets"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatFetchResult(result, tt.format, &buf)
			require.NoError(t, err)
			for _, c := range tt.contains {
				assert.Contains(t, buf.String(), c)
			}
		})
	}
}

// =============================================================================
// Secrets Watch Execution Tests
// =============================================================================

func TestExecuteSecretsWatch_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.WatchSecretsRequest{
		Prefix: "test/",
	}

	changeCh := make(chan *service.SecretChange, 1)
	mockClient := &MockSecretsClient{
		WatchSecretsByPrefixFunc: func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			return changeCh, nil
		},
	}

	ch, err := executeSecretsWatch(ctx, mockClient, req)
	require.NoError(t, err)
	assert.NotNil(t, ch)
	close(changeCh)
}

// =============================================================================
// Secrets Execution with Mock Client Tests
// =============================================================================

func TestExecuteSecretsStore_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.StoreSecretRequest{
		SecretID: "test/secret",
		Data:     []byte(`{"key":"value"}`),
	}

	mockClient := &MockSecretsClient{
		StoreSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
			return &service.StoreSecretResponse{
				SecretID:  req.SecretID,
				Version:   1,
				Timestamp: time.Now(),
			}, nil
		},
	}

	resp, err := executeSecretsStore(ctx, mockClient, req)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestExecuteSecretsGet_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.GetSecretRequest{
		SecretID: "test/secret",
	}

	mockClient := &MockSecretsClient{
		GetSecretFunc: func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
			return &service.GetSecretResponse{
				SecretID: req.SecretID,
				Version:  1,
				Data:     []byte(`{"key":"value"}`),
			}, nil
		},
	}

	resp, err := executeSecretsGet(ctx, mockClient, req)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
}

func TestExecuteSecretsList_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.ListSecretsRequest{
		Limit: 10,
	}

	mockClient := &MockSecretsClient{
		ListSecretsFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
			return &service.ListSecretsResponse{
				Secrets: []*service.Secret{
					{SecretID: "test/s1", CurrentVersion: 1},
				},
			}, nil
		},
	}

	resp, err := executeSecretsList(ctx, mockClient, req)
	require.NoError(t, err)
	assert.Len(t, resp.Secrets, 1)
}

func TestExecuteSecretsDelete_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.DeleteSecretRequest{
		SecretID: "test/secret",
	}

	mockClient := &MockSecretsClient{
		DeleteSecretFunc: func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
			return &service.DeleteSecretResponse{
				SecretID: req.SecretID,
				Deleted:  true,
			}, nil
		},
	}

	resp, err := executeSecretsDelete(ctx, mockClient, req)
	require.NoError(t, err)
	assert.True(t, resp.Deleted)
}

func TestExecuteSecretsListVersions_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.ListSecretVersionsRequest{
		SecretID: "test/secret",
	}

	mockClient := &MockSecretsClient{
		ListSecretVersionsFunc: func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
			return &service.ListSecretVersionsResponse{
				Versions: []*service.SecretVersion{
					{Version: 1},
					{Version: 2},
				},
			}, nil
		},
	}

	resp, err := executeSecretsListVersions(ctx, mockClient, req)
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 2)
}

func TestExecuteSecretsRollback_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.RollbackSecretRequest{
		SecretID:      "test/secret",
		TargetVersion: 1,
	}

	mockClient := &MockSecretsClient{
		RollbackSecretFunc: func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
			return &service.RollbackSecretResponse{
				SecretID:   req.SecretID,
				NewVersion: 3,
			}, nil
		},
	}

	resp, err := executeSecretsRollback(ctx, mockClient, req)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

func TestExecuteSecretsGetByPrefix_Success(t *testing.T) {
	ctx := context.Background()
	req := &service.GetSecretsByPrefixRequest{
		Prefix: "test/",
	}

	mockClient := &MockSecretsClient{
		GetSecretsByPrefixFunc: func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
			return &service.GetSecretsByPrefixResponse{
				Secrets: map[string]map[string][]byte{
					"test/s1": {"key": []byte("value")},
				},
				TotalCount: 1,
			}, nil
		},
	}

	resp, err := executeSecretsGetByPrefix(ctx, mockClient, req)
	require.NoError(t, err)
	assert.Equal(t, 1, resp.TotalCount)
}

// =============================================================================
// Backup Helper Function Tests
// =============================================================================

func TestParseCompressionType_AllTypes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"none", "none", false},
		{"gzip", "gzip", false},
		{"zstd", "zstd", false},
		{"invalid", "lz4", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCompressionType(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported compression")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseEncryptionType_AllTypes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"none", "none", false},
		{"aes256", "aes256", false},
		{"invalid", "rsa", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseEncryptionType(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported encryption")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Helpers Tests
// =============================================================================

func TestFormatBytes_AllRanges(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected string
	}{
		{"zero bytes", 0, "0 B"},
		{"small bytes", 512, "512 B"},
		{"one KB", 1024, "1.0 KB"},
		{"mixed KB", 1536, "1.5 KB"},
		{"one MB", 1024 * 1024, "1.0 MB"},
		{"one GB", 1024 * 1024 * 1024, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLoggerAdapter_WithError(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stderr",
	}
	l, err := logger.New(logConfig)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: l}
	result := adapter.WithError(&errors.MessageError{Message: "test error"})
	assert.NotNil(t, result)
}

func TestLoggerAdapter_WithField(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stderr",
	}
	l, err := logger.New(logConfig)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: l}
	result := adapter.WithField("key", "value")
	assert.NotNil(t, result)
}

func TestLoggerAdapter_WithFields(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:  "info",
		Format: "json",
		Output: "stderr",
	}
	l, err := logger.New(logConfig)
	require.NoError(t, err)

	adapter := &loggerAdapter{Logger: l}
	result := adapter.WithFields(map[string]interface{}{"key": "value"})
	assert.NotNil(t, result)
}

// NOTE: TestAsInterfaceLogger is defined in helpers_test.go, not duplicated here.

// =============================================================================
// initConfig Coverage Test
// =============================================================================

func TestInitConfig_DefaultNoConfigFile(t *testing.T) {
	// Save and restore
	origCfg := cfg
	origCfgFile := cfgFile
	defer func() {
		cfg = origCfg
		cfgFile = origCfgFile
	}()

	cfgFile = ""
	err := initConfig(rootCmd)
	require.NoError(t, err)
	assert.NotNil(t, cfg)
}

func TestInitConfig_WithConfigFile(t *testing.T) {
	// Save and restore
	origCfg := cfg
	origCfgFile := cfgFile
	defer func() {
		cfg = origCfg
		cfgFile = origCfgFile
	}()

	cfgFile = "/nonexistent/config.yaml"
	err := initConfig(rootCmd)
	// Will error because the file does not exist and is not ConfigFileNotFoundError
	assert.Error(t, err)
}

// =============================================================================
// Client Tests
// =============================================================================

func TestUnsupportedTransportError(t *testing.T) {
	err := &UnsupportedTransportError{Transport: "banana"}
	assert.Contains(t, err.Error(), "unsupported transport")
	assert.Contains(t, err.Error(), "banana")
}

func TestBuildSDKClientOptions_AllTransports(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		wantErr   bool
	}{
		{"grpc default", "grpc", false},
		{"empty default", "", false},
		{"rest", "rest", false},
		{"quic", "quic", false},
		{"mcp", "mcp", false},
		{"unsupported", "banana", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origTransport := transport
			defer func() { transport = origTransport }()
			transport = tt.transport

			opts, err := buildSDKClientOptions()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, opts)
			}
		})
	}
}

func TestGetSDKClient_NilWhenNotCreated(t *testing.T) {
	origHolder := sdkClientHolder.Load()
	defer func() {
		if origHolder != nil {
			sdkClientHolder.Store(origHolder)
		} else {
			sdkClientHolder.Store(nil)
		}
	}()

	resetSDKClient()
	client := getSDKClient()
	assert.Nil(t, client)
}

func TestCloseSDKClient_NilSafe(t *testing.T) {
	origHolder := sdkClientHolder.Load()
	defer func() {
		if origHolder != nil {
			sdkClientHolder.Store(origHolder)
		} else {
			sdkClientHolder.Store(nil)
		}
	}()

	resetSDKClient()
	err := closeSDKClient()
	assert.NoError(t, err)
}

// =============================================================================
// Backup Command Constructors
// =============================================================================

func TestNewBackupCommands_HasSubcommands(t *testing.T) {
	cmd := newBackupCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "backup", cmd.Use)
	assert.True(t, len(cmd.Commands()) >= 4, "backup command should have at least 4 subcommands")
}

// =============================================================================
// Secrets Command Constructors
// =============================================================================

func TestNewSecretsCommand_HasSubcommands(t *testing.T) {
	cmd := newSecretsCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "secrets", cmd.Use)
	assert.True(t, len(cmd.Commands()) >= 8, "secrets command should have at least 8 subcommands")
}

// =============================================================================
// Msg Command Constructors
// =============================================================================

func TestNewMsgCommands_HasSubcommands(t *testing.T) {
	cmd := newMsgCommands()
	assert.NotNil(t, cmd)
	assert.Equal(t, "msg", cmd.Use)
	assert.True(t, len(cmd.Commands()) >= 8, "msg command should have at least 8 subcommands")
}

// =============================================================================
// Version Command Test
// =============================================================================

func TestVersionCommand_Exists(t *testing.T) {
	assert.NotNil(t, versionCmd)
	assert.Equal(t, "version", versionCmd.Use)
}

// =============================================================================
// Root Command Test
// =============================================================================

func TestRootCmd_ErrorWithoutSubcommand(t *testing.T) {
	// The root command returns an error when no subcommand is provided
	err := rootCmd.RunE(rootCmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no command specified")
}

// =============================================================================
// FetchResult with no files
// =============================================================================

func TestFormatFetchResult_NoFiles(t *testing.T) {
	result := &clisecrets.FetchResult{
		SecretsCount: 0,
		KeysCount:    0,
		FilesWritten: nil,
	}

	var buf bytes.Buffer
	err := formatFetchResult(result, "table", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "0")
}

// =============================================================================
// Secrets Client Adapter Tests
// =============================================================================

func TestSecretsClientAdapter_GetSecretsByPrefix(t *testing.T) {
	mockClient := &MockSecretsClient{
		GetSecretsByPrefixFunc: func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
			return &service.GetSecretsByPrefixResponse{
				Secrets: map[string]map[string][]byte{
					"test/secret1": {
						"key": []byte("value"),
					},
				},
				TotalCount: 1,
			}, nil
		},
	}

	adapter := &secretsClientAdapter{
		client: mockClient,
		limit:  100,
	}

	ctx := context.Background()
	secrets, err := adapter.GetSecretsByPrefix(ctx, "test/")
	require.NoError(t, err)
	assert.Len(t, secrets, 1)
	assert.Equal(t, "test/secret1", secrets[0].Path)
	assert.Equal(t, "value", secrets[0].Data["key"])
}

func TestSecretsClientAdapter_GetSecretsByPrefix_Error(t *testing.T) {
	mockClient := &MockSecretsClient{
		GetSecretsByPrefixFunc: func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
			return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
		},
	}

	adapter := &secretsClientAdapter{
		client: mockClient,
		limit:  100,
	}

	ctx := context.Background()
	secrets, err := adapter.GetSecretsByPrefix(ctx, "test/")
	assert.Error(t, err)
	assert.Nil(t, secrets)
}

type mockSecretsClientForCoverage struct {
	storeFunc         func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error)
	getFunc           func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error)
	deleteFunc        func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error)
	listFunc          func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error)
	getByPrefixFunc   func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error)
	rotateFunc        func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error)
	listVersionsFunc  func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error)
	rollbackFunc      func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error)
	updateStatusFunc  func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error)
	watchByPrefixFunc func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error)
}

func (m *mockSecretsClientForCoverage) StoreSecret(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
	if m.storeFunc != nil {
		return m.storeFunc(ctx, req)
	}
	return &service.StoreSecretResponse{SecretID: req.SecretID, Version: 1, Timestamp: time.Now()}, nil
}

func (m *mockSecretsClientForCoverage) GetSecret(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, req)
	}
	return &service.GetSecretResponse{
		SecretID:  req.SecretID,
		Version:   1,
		Data:      []byte(`{"username":"admin","password":"secret"}`),
		CreatedAt: time.Now(),
	}, nil
}

func (m *mockSecretsClientForCoverage) DeleteSecret(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, req)
	}
	return &service.DeleteSecretResponse{SecretID: req.SecretID, Deleted: true, Timestamp: time.Now()}, nil
}

func (m *mockSecretsClientForCoverage) ListSecrets(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return &service.ListSecretsResponse{
		Secrets: []*service.Secret{
			{SecretID: "db/prod", CurrentVersion: 3, CreatedAt: time.Now(), UpdatedAt: time.Now()},
			{SecretID: "app/config", CurrentVersion: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
	}, nil
}

func (m *mockSecretsClientForCoverage) GetSecretsByPrefix(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
	if m.getByPrefixFunc != nil {
		return m.getByPrefixFunc(ctx, req)
	}
	return &service.GetSecretsByPrefixResponse{
		Secrets:    map[string]map[string][]byte{"db/prod": {"password": []byte("secret")}},
		TotalCount: 1,
		Timestamp:  time.Now(),
	}, nil
}

func (m *mockSecretsClientForCoverage) RotateSecret(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
	if m.rotateFunc != nil {
		return m.rotateFunc(ctx, req)
	}
	return &service.RotateSecretResponse{SecretID: req.SecretID, NewVersion: 2, Timestamp: time.Now()}, nil
}

func (m *mockSecretsClientForCoverage) ListSecretVersions(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
	if m.listVersionsFunc != nil {
		return m.listVersionsFunc(ctx, req)
	}
	now := time.Now()
	deleted := now.Add(-time.Hour)
	return &service.ListSecretVersionsResponse{
		Versions: []*service.SecretVersion{
			{SecretID: req.SecretID, Version: 1, CreatedAt: now, CreatedBy: "admin"},
			{SecretID: req.SecretID, Version: 2, CreatedAt: now, CreatedBy: "", DeletedAt: &deleted},
		},
	}, nil
}

func (m *mockSecretsClientForCoverage) RollbackSecret(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
	if m.rollbackFunc != nil {
		return m.rollbackFunc(ctx, req)
	}
	return &service.RollbackSecretResponse{SecretID: req.SecretID, NewVersion: 3, Timestamp: time.Now()}, nil
}

func (m *mockSecretsClientForCoverage) UpdateSecretStatus(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, req)
	}
	return &service.UpdateSecretStatusResponse{
		SecretID:  req.SecretID,
		OldStatus: service.SecretStatusEnabled,
		NewStatus: req.Status,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockSecretsClientForCoverage) WatchSecretsByPrefix(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
	if m.watchByPrefixFunc != nil {
		return m.watchByPrefixFunc(ctx, req)
	}
	ch := make(chan *service.SecretChange, 1)
	ch <- &service.SecretChange{
		Path:       "db/prod",
		ChangeType: service.SecretChangeUpdated,
		Version:    2,
		Timestamp:  time.Now(),
	}
	close(ch)
	return ch, nil
}

func (m *mockSecretsClientForCoverage) Close() error { return nil }

// setupSecretsMock replaces the secretsClientFactory with a mock and returns cleanup.
func setupSecretsMock(t *testing.T, client SecretsClient) func() {
	t.Helper()
	origFactory := secretsClientFactory
	secretsClientFactory = func() (SecretsClient, error) {
		return client, nil
	}
	return func() {
		secretsClientFactory = origFactory
	}
}

// =============================================================================
// Secrets Command Tests - Put (Store)
// =============================================================================

func TestRunSecretsPutCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	origFmt := secretsOutputFmt
	defer func() {
		secretsData = origData
		secretsOutputFmt = origFmt
	}()
	secretsData = []string{"username=admin", "password=secret123"}
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsPutCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsPutCommand_InvalidDataFormat(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	defer func() { secretsData = origData }()
	secretsData = []string{"missing-equals-sign"}

	cmd := &cobra.Command{}
	err := runSecretsPutCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

func TestRunSecretsPutCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	origData := secretsData
	defer func() { secretsData = origData }()
	secretsData = []string{"key=value"}

	cmd := &cobra.Command{}
	err := runSecretsPutCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Get
// =============================================================================

func TestRunSecretsGetCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsGetCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsGetCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsGetCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - List
// =============================================================================

func TestRunSecretsListCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	origPrefix := secretsPrefix
	origLimit := secretsLimit
	defer func() {
		secretsOutputFmt = origFmt
		secretsPrefix = origPrefix
		secretsLimit = origLimit
	}()
	secretsOutputFmt = "table"
	secretsPrefix = ""
	secretsLimit = 100

	cmd := &cobra.Command{}
	err := runSecretsListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunSecretsListCommand_WithPrefix(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	origPrefix := secretsPrefix
	defer func() {
		secretsOutputFmt = origFmt
		secretsPrefix = origPrefix
	}()
	secretsOutputFmt = "json"
	secretsPrefix = "db/"

	cmd := &cobra.Command{}
	err := runSecretsListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestRunSecretsListCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsListCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Delete
// =============================================================================

func TestRunSecretsDeleteCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsDeleteCommand(cmd, []string{"db/old"})
	assert.NoError(t, err)
}

func TestRunSecretsDeleteCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsDeleteCommand(cmd, []string{"db/old"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Destroy
// =============================================================================

func TestRunSecretsDestroyCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsDestroyCommand(cmd, []string{"db/obsolete"})
	assert.NoError(t, err)
}

// =============================================================================
// Secrets Command Tests - Versions
// =============================================================================

func TestRunSecretsVersionsCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsVersionsCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsVersionsCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsVersionsCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Rollback
// =============================================================================

func TestRunSecretsRollbackCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	origVer := secretsVersion
	defer func() {
		secretsOutputFmt = origFmt
		secretsVersion = origVer
	}()
	secretsOutputFmt = "table"
	secretsVersion = 2

	cmd := &cobra.Command{}
	err := runSecretsRollbackCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsRollbackCommand_ZeroVersion(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origVer := secretsVersion
	defer func() { secretsVersion = origVer }()
	secretsVersion = 0

	cmd := &cobra.Command{}
	err := runSecretsRollbackCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Rotate
// =============================================================================

func TestRunSecretsRotateCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	origFmt := secretsOutputFmt
	defer func() {
		secretsData = origData
		secretsOutputFmt = origFmt
	}()
	secretsData = []string{"password=newpassword123"}
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsRotateCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsRotateCommand_InvalidData(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origData := secretsData
	defer func() { secretsData = origData }()
	secretsData = []string{"bad-format"}

	cmd := &cobra.Command{}
	err := runSecretsRotateCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

func TestRunSecretsRotateCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	origData := secretsData
	defer func() { secretsData = origData }()
	secretsData = []string{"key=value"}

	cmd := &cobra.Command{}
	err := runSecretsRotateCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Enable/Disable
// =============================================================================

func TestRunSecretsEnableCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsEnableCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

func TestRunSecretsEnableCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsEnableCommand(cmd, []string{"db/prod"})
	assert.Error(t, err)
}

func TestRunSecretsDisableCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsDisableCommand(cmd, []string{"db/prod"})
	assert.NoError(t, err)
}

// =============================================================================
// Secrets Command Tests - GetPrefix
// =============================================================================

func TestRunSecretsGetPrefixCommand_Success(t *testing.T) {
	mock := &mockSecretsClientForCoverage{}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	defer func() { secretsOutputFmt = origFmt }()
	secretsOutputFmt = "table"

	cmd := &cobra.Command{}
	err := runSecretsGetPrefixCommand(cmd, []string{"apps/myapp/"})
	assert.NoError(t, err)
}

func TestRunSecretsGetPrefixCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsGetPrefixCommand(cmd, []string{"apps/myapp/"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Watch
// =============================================================================

func TestRunSecretsWatchCommand_ContextCancelled(t *testing.T) {
	mock := &mockSecretsClientForCoverage{
		watchByPrefixFunc: func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			ch := make(chan *service.SecretChange, 2)
			ch <- &service.SecretChange{
				Path:       "db/prod",
				ChangeType: service.SecretChangeUpdated,
				Version:    3,
				Timestamp:  time.Now(),
			}
			close(ch)
			return ch, nil
		},
	}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	origTTL := secretsWatchTTL
	defer func() {
		secretsOutputFmt = origFmt
		secretsWatchTTL = origTTL
	}()
	secretsOutputFmt = "table"
	secretsWatchTTL = 0

	cmd := &cobra.Command{}
	err := runSecretsWatchCommand(cmd, []string{"db/"})
	assert.NoError(t, err)
}

func TestRunSecretsWatchCommand_WithTimeout(t *testing.T) {
	mock := &mockSecretsClientForCoverage{
		watchByPrefixFunc: func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
			ch := make(chan *service.SecretChange)
			close(ch)
			return ch, nil
		},
	}
	cleanup := setupSecretsMock(t, mock)
	defer cleanup()

	origFmt := secretsOutputFmt
	origTTL := secretsWatchTTL
	defer func() {
		secretsOutputFmt = origFmt
		secretsWatchTTL = origTTL
	}()
	secretsOutputFmt = "table"
	secretsWatchTTL = 50 * time.Millisecond

	cmd := &cobra.Command{}
	err := runSecretsWatchCommand(cmd, []string{"db/"})
	assert.NoError(t, err)
}

func TestRunSecretsWatchCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	cmd := &cobra.Command{}
	err := runSecretsWatchCommand(cmd, []string{"db/"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Command Tests - Fetch
// =============================================================================

func TestRunSecretsFetchCommand_FactoryError(t *testing.T) {
	origFactory := secretsClientFactory
	defer func() { secretsClientFactory = origFactory }()

	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "unavailable")
	}

	origFmt := fetchFormat
	defer func() { fetchFormat = origFmt }()
	fetchFormat = "file"

	cmd := &cobra.Command{}
	err := runSecretsFetchCommand(cmd, []string{"apps/myapp/"})
	assert.Error(t, err)
}

func TestRunSecretsFetchCommand_InvalidFormat(t *testing.T) {
	origFmt := fetchFormat
	defer func() { fetchFormat = origFmt }()
	fetchFormat = "invalid-fmt"

	cmd := &cobra.Command{}
	err := runSecretsFetchCommand(cmd, []string{"apps/myapp/"})
	assert.Error(t, err)
}
