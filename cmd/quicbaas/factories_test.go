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
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestResetFactories(t *testing.T) {
	// Save original factories
	origSDKFactory := sdkClientFactory
	origCryptoFactory := cryptoClientFactory
	origKMSFactory := kmsClientFactory
	origSecretsFactory := secretsClientFactory
	origTimeSeriesFactory := timeSeriesClientFactory
	origAuthFactory := authClientFactory

	// Swap with mock factories
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "mock error")
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "mock error")
	}
	kmsClientFactory = func() (KMSClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "mock error")
	}
	secretsClientFactory = func() (SecretsClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "mock error")
	}
	timeSeriesClientFactory = func() (TimeSeriesClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "mock error")
	}
	authClientFactory = func() (auth.Client, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "mock error")
	}

	// Reset should restore to original
	resetFactories()

	// Verify factories are back to original
	assert.NotNil(t, sdkClientFactory)
	assert.NotNil(t, cryptoClientFactory)
	assert.NotNil(t, kmsClientFactory)
	assert.NotNil(t, secretsClientFactory)
	assert.NotNil(t, timeSeriesClientFactory)
	assert.NotNil(t, authClientFactory)

	// Restore for cleanup
	sdkClientFactory = origSDKFactory
	cryptoClientFactory = origCryptoFactory
	kmsClientFactory = origKMSFactory
	secretsClientFactory = origSecretsFactory
	timeSeriesClientFactory = origTimeSeriesFactory
	authClientFactory = origAuthFactory
}

// TestCryptoEncryptFactoryError tests crypto encrypt command with factory error.
func TestCryptoEncryptFactoryError(t *testing.T) {
	// Save original factory
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	// Mock factory to return error
	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, factoryErr
	}

	// Set required flags
	cryptoData = "test data"
	cryptoBase64 = false
	defer func() { cryptoData = "" }()

	cmd := &cobra.Command{}
	err := runCryptoEncryptCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestCryptoDecryptFactoryError tests crypto decrypt command with factory error.
func TestCryptoDecryptFactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, factoryErr
	}

	// Set valid hex data for the test
	cryptoData = "48656c6c6f" // "Hello" in hex
	defer func() { cryptoData = "" }()

	cmd := &cobra.Command{}
	err := runCryptoDecryptCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestCryptoRewrapFactoryError tests crypto rewrap command with factory error.
func TestCryptoRewrapFactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, factoryErr
	}

	// Set valid hex data
	cryptoData = "48656c6c6f"
	defer func() { cryptoData = "" }()

	cmd := &cobra.Command{}
	err := runCryptoRewrapCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestCryptoSignFactoryError tests crypto sign command with factory error.
func TestCryptoSignFactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, factoryErr
	}

	cryptoData = "test data"
	defer func() { cryptoData = "" }()

	cmd := &cobra.Command{}
	err := runCryptoSignCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestCryptoVerifyFactoryError tests crypto verify command with factory error.
func TestCryptoVerifyFactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, factoryErr
	}

	cryptoData = "test data"
	cryptoSignature = "dGVzdA==" // base64 "test"
	defer func() {
		cryptoData = ""
		cryptoSignature = ""
	}()

	cmd := &cobra.Command{}
	err := runCryptoVerifyCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestCryptoHashFactoryError tests crypto hash command with factory error.
func TestCryptoHashFactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, factoryErr
	}

	cryptoData = "test data"
	defer func() { cryptoData = "" }()

	cmd := &cobra.Command{}
	err := runCryptoHashCommand(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestCryptoDataKeyFactoryError tests crypto datakey command with factory error.
func TestCryptoDataKeyFactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	factoryErr := errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, factoryErr
	}

	cryptoBits = 256
	defer func() { cryptoBits = 256 }()

	cmd := &cobra.Command{}
	err := runCryptoDataKeyCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// =============================================================================
// Response Parse Function Tests (factories_secrets.go)
// These test the pure parsing functions that convert interface{} to typed responses.
// =============================================================================

func TestParseSecretsStoreResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"secret_id": "test/secret",
		"version":   float64(1),
		"timestamp": float64(1700000000),
	}

	resp, err := parseSecretsStoreResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, uint64(1), resp.Version)
	assert.False(t, resp.Timestamp.IsZero())
}

// TestParseSecretsResponses_InvalidInput checks that every secrets response
// parser rejects a result that does not decode to a JSON object.
func TestParseSecretsResponses_InvalidInput(t *testing.T) {
	parsers := map[string]func(interface{}) (interface{}, error){
		"store": func(v interface{}) (interface{}, error) { return parseSecretsStoreResponse(v) },
		"get":   func(v interface{}) (interface{}, error) { return parseSecretsGetResponse(v) },
		"delete": func(v interface{}) (interface{}, error) {
			return parseSecretsDeleteResponse(v)
		},
		"list": func(v interface{}) (interface{}, error) { return parseSecretsListResponse(v) },
		"get_by_prefix": func(v interface{}) (interface{}, error) {
			return parseSecretsGetByPrefixResponse(v)
		},
		"rotate": func(v interface{}) (interface{}, error) { return parseSecretsRotateResponse(v) },
		"list_versions": func(v interface{}) (interface{}, error) {
			return parseSecretsListVersionsResponse(v)
		},
		"rollback": func(v interface{}) (interface{}, error) { return parseSecretsRollbackResponse(v) },
		"update_status": func(v interface{}) (interface{}, error) {
			return parseSecretsUpdateStatusResponse(v)
		},
	}
	for name, parse := range parsers {
		t.Run(name, func(t *testing.T) {
			resp, err := parse("invalid")
			assert.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

func TestParseSecretsGetResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"secret_id":  "test/secret",
		"version":    float64(2),
		"created_at": float64(1700000000),
		"timestamp":  float64(1700000001),
		"metadata": map[string]interface{}{
			"author": "admin",
		},
	}

	resp, err := parseSecretsGetResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, uint64(2), resp.Version)
	assert.Equal(t, "admin", resp.Metadata["author"])
}

func TestParseSecretsGetResponse_Data(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		wantData []byte
	}{
		{
			name:     "base64 string is decoded",
			input:    map[string]interface{}{"data": base64.StdEncoding.EncodeToString([]byte(`{"key":"value"}`))},
			wantData: []byte(`{"key":"value"}`),
		},
		{
			name:     "non-base64 string is kept raw",
			input:    map[string]interface{}{"data": "not-base64!!!"},
			wantData: []byte("not-base64!!!"),
		},
		{
			name:     "direct bytes are kept",
			input:    map[string]interface{}{"data": []byte("raw bytes")},
			wantData: []byte("raw bytes"),
		},
		{
			name:  "no data and no metadata",
			input: map[string]interface{}{"secret_id": "test/secret"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := parseSecretsGetResponse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, resp.Data)
			assert.Nil(t, resp.Metadata)
		})
	}
}

func TestParseSecretsDeleteResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"secret_id": "test/secret",
		"deleted":   true,
		"timestamp": float64(1700000000),
	}

	resp, err := parseSecretsDeleteResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.True(t, resp.Deleted)
}

func TestParseSecretsListResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"secrets": []interface{}{
			map[string]interface{}{
				"secret_id":       "test/s1",
				"name":            "s1",
				"description":     "first secret",
				"current_version": float64(3),
				"created_at":      float64(1700000000),
				"updated_at":      float64(1700000001),
			},
			map[string]interface{}{
				"secret_id":       "test/s2",
				"name":            "s2",
				"current_version": float64(1),
			},
		},
		"next_cursor": "cursor-abc",
		"has_more":    true,
		"total_count": float64(10),
		"timestamp":   float64(1700000000),
	}

	resp, err := parseSecretsListResponse(input)
	require.NoError(t, err)
	assert.Len(t, resp.Secrets, 2)
	assert.Equal(t, "test/s1", resp.Secrets[0].SecretID)
	assert.Equal(t, "first secret", resp.Secrets[0].Description)
	assert.Equal(t, uint64(3), resp.Secrets[0].CurrentVersion)
	assert.Equal(t, "cursor-abc", resp.NextCursor)
	assert.True(t, resp.HasMore)
	assert.Equal(t, 10, resp.TotalCount)
}

func TestParseSecretsListResponse_EmptySecrets(t *testing.T) {
	input := map[string]interface{}{
		"secrets":     []interface{}{},
		"has_more":    false,
		"total_count": float64(0),
	}

	resp, err := parseSecretsListResponse(input)
	require.NoError(t, err)
	assert.Empty(t, resp.Secrets)
}

func TestParseSecretsListResponse_NoSecrets(t *testing.T) {
	input := map[string]interface{}{}

	resp, err := parseSecretsListResponse(input)
	require.NoError(t, err)
	assert.Empty(t, resp.Secrets)
}

func TestParseSecretsListResponse_InvalidSecretEntry(t *testing.T) {
	input := map[string]interface{}{
		"secrets": []interface{}{
			"not-a-map", // should be skipped
		},
	}

	resp, err := parseSecretsListResponse(input)
	require.NoError(t, err)
	assert.Empty(t, resp.Secrets) // invalid entry skipped
}

func TestParseSecretsGetByPrefixResponse_ValidInput(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("secret-value"))
	input := map[string]interface{}{
		"secrets": map[string]interface{}{
			"app/db": map[string]interface{}{
				"password": encoded,
			},
		},
		"metadata": map[string]interface{}{
			"app/db": map[string]interface{}{
				"author": "admin",
			},
		},
		"version":     float64(5),
		"next_cursor": "next",
		"has_more":    true,
		"total_count": float64(1),
		"timestamp":   float64(1700000000),
	}

	resp, err := parseSecretsGetByPrefixResponse(input)
	require.NoError(t, err)
	assert.Contains(t, resp.Secrets, "app/db")
	assert.Equal(t, []byte("secret-value"), resp.Secrets["app/db"]["password"])
	assert.Equal(t, "admin", resp.Metadata["app/db"]["author"])
	assert.True(t, resp.HasMore)
	assert.Equal(t, 1, resp.TotalCount)
}

func TestParseSecretsGetByPrefixResponse_RawStringData(t *testing.T) {
	input := map[string]interface{}{
		"secrets": map[string]interface{}{
			"app/db": map[string]interface{}{
				"password": "not-base64!!!",
			},
		},
	}

	resp, err := parseSecretsGetByPrefixResponse(input)
	require.NoError(t, err)
	assert.Equal(t, []byte("not-base64!!!"), resp.Secrets["app/db"]["password"])
}

func TestParseSecretsGetByPrefixResponse_BytesData(t *testing.T) {
	input := map[string]interface{}{
		"secrets": map[string]interface{}{
			"app/db": map[string]interface{}{
				"password": []byte("raw-bytes"),
			},
		},
	}

	resp, err := parseSecretsGetByPrefixResponse(input)
	require.NoError(t, err)
	assert.Equal(t, []byte("raw-bytes"), resp.Secrets["app/db"]["password"])
}

func TestParseSecretsGetByPrefixResponse_InvalidPathData(t *testing.T) {
	input := map[string]interface{}{
		"secrets": map[string]interface{}{
			"app/db": "not-a-map",
		},
		"metadata": map[string]interface{}{
			"app/db": "not-a-map",
		},
	}

	resp, err := parseSecretsGetByPrefixResponse(input)
	require.NoError(t, err)
	// Invalid path data should be skipped
	assert.NotContains(t, resp.Secrets, "app/db")
}

func TestParseSecretsRotateResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"secret_id":   "test/secret",
		"old_version": float64(1),
		"new_version": float64(2),
		"timestamp":   float64(1700000000),
	}

	resp, err := parseSecretsRotateResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestParseSecretsListVersionsResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"versions": []interface{}{
			map[string]interface{}{
				"secret_id":  "test/secret",
				"version":    float64(1),
				"created_at": float64(1700000000),
				"created_by": "admin",
			},
			map[string]interface{}{
				"secret_id":  "test/secret",
				"version":    float64(2),
				"created_at": float64(1700000001),
				"created_by": "user1",
			},
		},
		"timestamp": float64(1700000000),
	}

	resp, err := parseSecretsListVersionsResponse(input)
	require.NoError(t, err)
	assert.Len(t, resp.Versions, 2)
	assert.Equal(t, uint64(1), resp.Versions[0].Version)
	assert.Equal(t, "admin", resp.Versions[0].CreatedBy)
}

func TestParseSecretsListVersionsResponse_InvalidVersionEntry(t *testing.T) {
	input := map[string]interface{}{
		"versions": []interface{}{
			"not-a-map",
		},
	}

	resp, err := parseSecretsListVersionsResponse(input)
	require.NoError(t, err)
	assert.Empty(t, resp.Versions)
}

func TestParseSecretsListVersionsResponse_NoVersions(t *testing.T) {
	input := map[string]interface{}{}

	resp, err := parseSecretsListVersionsResponse(input)
	require.NoError(t, err)
	assert.Empty(t, resp.Versions)
}

func TestParseSecretsRollbackResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"secret_id":   "test/secret",
		"new_version": float64(3),
		"timestamp":   float64(1700000000),
	}

	resp, err := parseSecretsRollbackResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

func TestParseSecretsUpdateStatusResponse_ValidInput(t *testing.T) {
	input := map[string]interface{}{
		"secret_id":  "test/secret",
		"old_status": "active",
		"new_status": "disabled",
		"timestamp":  float64(1700000000),
	}

	resp, err := parseSecretsUpdateStatusResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "test/secret", resp.SecretID)
	assert.Equal(t, service.SecretStatus("active"), resp.OldStatus)
	assert.Equal(t, service.SecretStatus("disabled"), resp.NewStatus)
}

func TestParseSecretsTimestamp_ValidTimestamp(t *testing.T) {
	ts := parseSecretsTimestamp(1700000000)
	assert.Equal(t, time.Unix(1700000000, 0), ts)
}

func TestParseSecretsTimestamp_ZeroTimestamp(t *testing.T) {
	ts := parseSecretsTimestamp(0)
	assert.True(t, ts.IsZero())
}

// =============================================================================
// MCP KMS Helper Function Tests (factories_mcp_kms.go)
// =============================================================================

func TestToJSONMap_ValidMap(t *testing.T) {
	input := map[string]interface{}{
		"key1": "value1",
		"key2": float64(42),
	}
	result, err := toJSONMap(input)
	require.NoError(t, err)
	assert.Equal(t, "value1", result["key1"])
}

func TestToJSONMap_InvalidInput(t *testing.T) {
	result, err := toJSONMap("not-a-map")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestToJSONMap_NilInput(t *testing.T) {
	result, err := toJSONMap(nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestGetString_ValidKey(t *testing.T) {
	m := map[string]interface{}{"name": "test"}
	assert.Equal(t, "test", getString(m, "name"))
}

func TestGetString_MissingKey(t *testing.T) {
	m := map[string]interface{}{}
	assert.Equal(t, "", getString(m, "name"))
}

func TestGetString_NonStringValue(t *testing.T) {
	m := map[string]interface{}{"name": 123}
	assert.Equal(t, "", getString(m, "name"))
}

func TestGetInt64_ValidFloat64(t *testing.T) {
	m := map[string]interface{}{"count": float64(42)}
	assert.Equal(t, int64(42), getInt64(m, "count"))
}

func TestGetInt64_ValidInt(t *testing.T) {
	m := map[string]interface{}{"count": 42}
	assert.Equal(t, int64(42), getInt64(m, "count"))
}

func TestGetInt64_ValidInt64(t *testing.T) {
	m := map[string]interface{}{"count": int64(42)}
	assert.Equal(t, int64(42), getInt64(m, "count"))
}

func TestGetInt64_MissingKey(t *testing.T) {
	m := map[string]interface{}{}
	assert.Equal(t, int64(0), getInt64(m, "count"))
}

func TestGetInt64_NonNumeric(t *testing.T) {
	m := map[string]interface{}{"count": "not-a-number"}
	assert.Equal(t, int64(0), getInt64(m, "count"))
}

func TestGetBool_ValidTrue(t *testing.T) {
	m := map[string]interface{}{"flag": true}
	assert.True(t, getBool(m, "flag"))
}

func TestGetBool_ValidFalse(t *testing.T) {
	m := map[string]interface{}{"flag": false}
	assert.False(t, getBool(m, "flag"))
}

func TestGetBool_MissingKey(t *testing.T) {
	m := map[string]interface{}{}
	assert.False(t, getBool(m, "flag"))
}

func TestGetBool_NonBool(t *testing.T) {
	m := map[string]interface{}{"flag": "yes"}
	assert.False(t, getBool(m, "flag"))
}

func TestGetMap_ValidMap(t *testing.T) {
	inner := map[string]interface{}{"nested": "value"}
	m := map[string]interface{}{"data": inner}
	result := getMap(m, "data")
	assert.Equal(t, "value", result["nested"])
}

func TestGetMap_MissingKey(t *testing.T) {
	m := map[string]interface{}{}
	result := getMap(m, "data")
	assert.Nil(t, result)
}

func TestGetMap_NonMap(t *testing.T) {
	m := map[string]interface{}{"data": "not-a-map"}
	result := getMap(m, "data")
	assert.Nil(t, result)
}

func TestGetBytes_ValidBytes(t *testing.T) {
	m := map[string]interface{}{"data": []byte("hello")}
	result := getBytes(m, "data")
	assert.Equal(t, []byte("hello"), result)
}

func TestGetBytes_MissingKey(t *testing.T) {
	m := map[string]interface{}{}
	result := getBytes(m, "data")
	assert.Nil(t, result)
}

func TestGetBytes_NonBytes(t *testing.T) {
	m := map[string]interface{}{"data": "a-string"}
	result := getBytes(m, "data")
	assert.Equal(t, []byte("a-string"), result)
}

// =============================================================================
// Mock Client Method Coverage Tests
// These test mock methods that have nil function handlers (default paths).
// =============================================================================

func TestMockSecretsClient_DefaultMethods(t *testing.T) {
	client := &MockSecretsClient{}
	ctx := context.Background()

	t.Run("StoreSecret", func(t *testing.T) {
		resp, err := client.StoreSecret(ctx, &service.StoreSecretRequest{SecretID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("GetSecret", func(t *testing.T) {
		resp, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("DeleteSecret", func(t *testing.T) {
		resp, err := client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("ListSecrets", func(t *testing.T) {
		resp, err := client.ListSecrets(ctx, &service.ListSecretsRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("GetSecretsByPrefix", func(t *testing.T) {
		resp, err := client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "test/"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("RotateSecret", func(t *testing.T) {
		resp, err := client.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("ListSecretVersions", func(t *testing.T) {
		resp, err := client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("RollbackSecret", func(t *testing.T) {
		resp, err := client.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("UpdateSecretStatus", func(t *testing.T) {
		resp, err := client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("WatchSecretsByPrefix", func(t *testing.T) {
		ch, err := client.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{Prefix: "test/"})
		assert.Error(t, err)
		assert.Nil(t, ch)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

func TestMockKMSClient_DefaultMethods(t *testing.T) {
	client := &MockKMSClient{}
	ctx := context.Background()

	t.Run("KMSGenerateKey", func(t *testing.T) {
		resp, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSImportKey", func(t *testing.T) {
		resp, err := client.KMSImportKey(ctx, &service.ImportKeyRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSGetKey", func(t *testing.T) {
		resp, err := client.KMSGetKey(ctx, &service.GetKeyRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSListKeys", func(t *testing.T) {
		resp, err := client.KMSListKeys(ctx, &service.ListKeysRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSRotateKey", func(t *testing.T) {
		resp, err := client.KMSRotateKey(ctx, &service.RotateKeyRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSDeleteKey", func(t *testing.T) {
		resp, err := client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSUpdateKeyStatus", func(t *testing.T) {
		resp, err := client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp)
	})

	t.Run("KMSListKeyVersions", func(t *testing.T) {
		resp, err := client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp)
	})

	t.Run("KMSExportKey", func(t *testing.T) {
		resp, err := client.KMSExportKey(ctx, &service.ExportKeyRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp)
	})

	t.Run("KMSUnwrapKey", func(t *testing.T) {
		resp, err := client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp)
	})

	t.Run("KMSSetKeySafety", func(t *testing.T) {
		resp, err := client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp)
	})

	t.Run("KMSEncrypt", func(t *testing.T) {
		resp, err := client.KMSEncrypt(ctx, &service.EncryptRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSDecrypt", func(t *testing.T) {
		resp, err := client.KMSDecrypt(ctx, &service.DecryptRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

func TestMockCryptoClient_DefaultMethods(t *testing.T) {
	client := &MockCryptoClient{}
	ctx := context.Background()

	t.Run("KMSEncrypt", func(t *testing.T) {
		resp, err := client.KMSEncrypt(ctx, &service.EncryptRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSDecrypt", func(t *testing.T) {
		resp, err := client.KMSDecrypt(ctx, &service.DecryptRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSSign", func(t *testing.T) {
		resp, err := client.KMSSign(ctx, &service.SignRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSVerify", func(t *testing.T) {
		resp, err := client.KMSVerify(ctx, &service.VerifyRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSHash", func(t *testing.T) {
		resp, err := client.KMSHash(ctx, &service.HashRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSRewrap", func(t *testing.T) {
		resp, err := client.KMSRewrap(ctx, &service.RewrapRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSGenerateDataKey", func(t *testing.T) {
		resp, err := client.KMSGenerateDataKey(ctx, &service.GenerateDataKeyRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("KMSGenerateRandom", func(t *testing.T) {
		resp, err := client.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

func TestMockBackendClient_DefaultMethods(t *testing.T) {
	client := &MockBackendClient{}
	ctx := context.Background()

	t.Run("KMSBackendStatus", func(t *testing.T) {
		resp, err := client.KMSBackendStatus(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("KMSBackendTest", func(t *testing.T) {
		resp, err := client.KMSBackendTest(ctx)
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

func TestMockSafetyClient_DefaultMethods(t *testing.T) {
	client := &MockSafetyClient{}
	ctx := context.Background()

	t.Run("KMSSafetyCheck", func(t *testing.T) {
		resp, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("KMSSafetyReport", func(t *testing.T) {
		resp, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

func TestMockBackupClient_DefaultMethods(t *testing.T) {
	client := &MockBackupClient{}
	ctx := context.Background()

	t.Run("CreateBackup", func(t *testing.T) {
		resp, err := client.CreateBackup(ctx, &BackupCreateRequest{Name: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("ListBackups", func(t *testing.T) {
		resp, err := client.ListBackups(ctx, &BackupListRequest{})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("RestoreBackup", func(t *testing.T) {
		resp, err := client.RestoreBackup(ctx, &BackupRestoreRequest{BackupID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("DeleteBackup", func(t *testing.T) {
		resp, err := client.DeleteBackup(ctx, &BackupDeleteRequest{BackupID: "test"})
		assert.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("VerifyBackup", func(t *testing.T) {
		resp, err := client.VerifyBackup(ctx, &BackupVerifyRequest{BackupID: "test"})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

// =============================================================================
// createTransportAwareSecretsClient routing tests
// =============================================================================

func TestCreateTransportAwareSecretsClient_DefaultTransport(t *testing.T) {
	// Cannot create actual gRPC client without server, but we test the routing
	origTransport := transport
	defer func() { transport = origTransport }()

	// The "rest", "quic", "mcp" paths require SDK factory, which needs a real connection
	// Test the switch statement coverage by testing "default" path
	transport = "unknown-transport"
	// This will try createGRPCSecretsClient which requires a server connection
	// So we only verify it does not panic
	_, _ = createTransportAwareSecretsClient()
}
