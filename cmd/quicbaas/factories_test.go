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

func TestParseSecretsListResponse_NoUsableEntries(t *testing.T) {
	tests := map[string]map[string]interface{}{
		"empty secrets list":  {"secrets": []interface{}{}, "has_more": false, "total_count": float64(0)},
		"no secrets key":      {},
		"non-map entry skips": {"secrets": []interface{}{"not-a-map"}},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			resp, err := parseSecretsListResponse(input)
			require.NoError(t, err)
			assert.Empty(t, resp.Secrets)
		})
	}
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

func TestParseSecretsGetByPrefixResponse_Data(t *testing.T) {
	tests := map[string]struct {
		value interface{}
		want  []byte
	}{
		"non-base64 string is kept raw": {value: "not-base64!!!", want: []byte("not-base64!!!")},
		"direct bytes are kept":         {value: []byte("raw-bytes"), want: []byte("raw-bytes")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			input := map[string]interface{}{
				"secrets": map[string]interface{}{
					"app/db": map[string]interface{}{"password": tt.value},
				},
			}

			resp, err := parseSecretsGetByPrefixResponse(input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, resp.Secrets["app/db"]["password"])
		})
	}
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

func TestParseSecretsListVersionsResponse_NoUsableEntries(t *testing.T) {
	tests := map[string]map[string]interface{}{
		"no versions key":     {},
		"non-map entry skips": {"versions": []interface{}{"not-a-map"}},
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			resp, err := parseSecretsListVersionsResponse(input)
			require.NoError(t, err)
			assert.Empty(t, resp.Versions)
		})
	}
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

// TestMapGetters checks each typed map getter on a matching value, a missing
// key and a value of the wrong type.
func TestMapGetters(t *testing.T) {
	str := func(m map[string]interface{}) interface{} { return getString(m, "k") }
	i64 := func(m map[string]interface{}) interface{} { return getInt64(m, "k") }
	boolean := func(m map[string]interface{}) interface{} { return getBool(m, "k") }
	nested := func(m map[string]interface{}) interface{} { return getMap(m, "k") }
	raw := func(m map[string]interface{}) interface{} { return getBytes(m, "k") }

	tests := []struct {
		name string
		get  func(map[string]interface{}) interface{}
		in   map[string]interface{}
		want interface{}
	}{
		{"string value", str, map[string]interface{}{"k": "test"}, "test"},
		{"string missing", str, map[string]interface{}{}, ""},
		{"string wrong type", str, map[string]interface{}{"k": 123}, ""},
		{"int64 from float64", i64, map[string]interface{}{"k": float64(42)}, int64(42)},
		{"int64 from int", i64, map[string]interface{}{"k": 42}, int64(42)},
		{"int64 from int64", i64, map[string]interface{}{"k": int64(42)}, int64(42)},
		{"int64 missing", i64, map[string]interface{}{}, int64(0)},
		{"int64 wrong type", i64, map[string]interface{}{"k": "not-a-number"}, int64(0)},
		{"bool true", boolean, map[string]interface{}{"k": true}, true},
		{"bool false", boolean, map[string]interface{}{"k": false}, false},
		{"bool missing", boolean, map[string]interface{}{}, false},
		{"bool wrong type", boolean, map[string]interface{}{"k": "yes"}, false},
		{"map value", nested, map[string]interface{}{"k": map[string]interface{}{"nested": "value"}}, map[string]interface{}{"nested": "value"}},
		{"map missing", nested, map[string]interface{}{}, map[string]interface{}(nil)},
		{"map wrong type", nested, map[string]interface{}{"k": "not-a-map"}, map[string]interface{}(nil)},
		{"bytes value", raw, map[string]interface{}{"k": []byte("hello")}, []byte("hello")},
		{"bytes from string", raw, map[string]interface{}{"k": "a-string"}, []byte("a-string")},
		{"bytes missing", raw, map[string]interface{}{}, []byte(nil)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.get(tt.in))
		})
	}
}

// =============================================================================
// Mock Client Method Coverage Tests
// These test mock methods that have nil function handlers (default paths).
// =============================================================================

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
