//go:build mcp

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Test KMS tool execution error paths (service not available)
func TestKMSToolsServiceNotAvailable(t *testing.T) {
	svc := &mockService{} // Mock doesn't implement KMS interfaces
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "generate key - service not available",
			toolName: "quicbaas_kms_generate_key",
			args: map[string]interface{}{
				"algorithm": "AES-256-GCM",
				"name":      "test-key",
			},
		},
		{
			name:     "import key - service not available",
			toolName: "quicbaas_kms_import_key",
			args: map[string]interface{}{
				"key_material":    "base64data",
				"wrapping_key_id": "key-1",
				"algorithm":       "AES-256-GCM",
			},
		},
		{
			name:     "get key - service not available",
			toolName: "quicbaas_kms_get_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "list keys - service not available",
			toolName: "quicbaas_kms_list_keys",
			args:     map[string]interface{}{},
		},
		{
			name:     "rotate key - service not available",
			toolName: "quicbaas_kms_rotate_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "revoke key - service not available",
			toolName: "quicbaas_kms_revoke_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "delete key - service not available",
			toolName: "quicbaas_kms_delete_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "get metadata - service not available",
			toolName: "quicbaas_kms_get_metadata",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "update metadata - service not available",
			toolName: "quicbaas_kms_update_metadata",
			args: map[string]interface{}{
				"key_id":      "key-1",
				"description": "updated",
			},
		},
		{
			name:     "set policy - service not available",
			toolName: "quicbaas_kms_set_policy",
			args: map[string]interface{}{
				"key_id": "key-1",
				"policy": map[string]interface{}{
					"version": "1.0",
				},
			},
		},
		{
			name:     "get policy - service not available",
			toolName: "quicbaas_kms_get_policy",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// Test Seal tool execution error paths (service not available)
func TestSealToolsServiceNotAvailable(t *testing.T) {
	svc := &mockService{} // Mock doesn't implement Seal interfaces
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "seal status - service not available",
			toolName: "quicbaas_seal_status",
			args:     map[string]interface{}{},
		},
		{
			name:     "seal init - service not available",
			toolName: "quicbaas_seal_init",
			args: map[string]interface{}{
				"secret_shares":    float64(5),
				"secret_threshold": float64(3),
			},
		},
		{
			name:     "unseal - service not available",
			toolName: "quicbaas_unseal",
			args: map[string]interface{}{
				"key": "unseal-key",
			},
		},
		{
			name:     "seal - service not available",
			toolName: "quicbaas_seal",
			args:     map[string]interface{}{},
		},
		{
			name:     "seal rekey - service not available",
			toolName: "quicbaas_seal_rekey",
			args: map[string]interface{}{
				"secret_shares":    float64(5),
				"secret_threshold": float64(3),
			},
		},
		{
			name:     "generate recovery key - service not available",
			toolName: "quicbaas_seal_generate_recovery_key",
			args:     map[string]interface{}{},
		},
		{
			name:     "delete recovery key - service not available",
			toolName: "quicbaas_seal_delete_recovery_key",
			args: map[string]interface{}{
				"key_id": "recovery-1",
			},
		},
		{
			name:     "generate root token - service not available",
			toolName: "quicbaas_seal_generate_root_token",
			args: map[string]interface{}{
				"otp": "one-time-password",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// Test Secrets tool execution error paths (service not available)
func TestSecretsToolsServiceNotAvailable(t *testing.T) {
	svc := &mockService{} // Mock doesn't implement Secrets interfaces
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "store secret - service not available",
			toolName: "quicbaas_secrets_store",
			args: map[string]interface{}{
				"path": "/secret/test",
				"data": map[string]interface{}{
					"key": "value",
				},
			},
		},
		{
			name:     "get secret - service not available",
			toolName: "quicbaas_secrets_get",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "list secrets - service not available",
			toolName: "quicbaas_secrets_list",
			args: map[string]interface{}{
				"path": "/secret",
			},
		},
		{
			name:     "delete secret - service not available",
			toolName: "quicbaas_secrets_delete",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "list versions - service not available",
			toolName: "quicbaas_secrets_versions",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "rotate secret - service not available",
			toolName: "quicbaas_secrets_rotate",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "set rotation policy - service not available",
			toolName: "quicbaas_secrets_set_rotation_policy",
			args: map[string]interface{}{
				"path":            "/secret/test",
				"rotation_period": float64(86400),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// Test Crypto tool execution error paths (service not available)
func TestCryptoToolsServiceNotAvailable(t *testing.T) {
	svc := &mockService{} // Mock doesn't implement Crypto interfaces
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "encrypt - service not available",
			toolName: "quicbaas_crypto_encrypt",
			args: map[string]interface{}{
				"key_name":  "crypto-key",
				"plaintext": "SGVsbG8gV29ybGQ=",
			},
		},
		{
			name:     "decrypt - service not available",
			toolName: "quicbaas_crypto_decrypt",
			args: map[string]interface{}{
				"key_name":   "crypto-key",
				"ciphertext": "encrypted-data",
			},
		},
		{
			name:     "rewrap - service not available",
			toolName: "quicbaas_crypto_rewrap",
			args: map[string]interface{}{
				"key_name":   "crypto-key",
				"ciphertext": "encrypted-data",
			},
		},
		{
			name:     "sign - service not available",
			toolName: "quicbaas_crypto_sign",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"input":    "data-to-sign",
			},
		},
		{
			name:     "verify - service not available",
			toolName: "quicbaas_crypto_verify",
			args: map[string]interface{}{
				"key_name":  "crypto-key",
				"input":     "data-to-verify",
				"signature": "signature-value",
			},
		},
		{
			name:     "hmac - service not available",
			toolName: "quicbaas_crypto_hmac",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"input":    "data-to-hmac",
			},
		},
		{
			name:     "random - service not available",
			toolName: "quicbaas_crypto_random",
			args: map[string]interface{}{
				"bytes": float64(32),
			},
		},
		{
			name:     "hash - service not available",
			toolName: "quicbaas_crypto_hash",
			args: map[string]interface{}{
				"input": "data-to-hash",
			},
		},
		{
			name:     "datakey - service not available",
			toolName: "quicbaas_crypto_datakey",
			args: map[string]interface{}{
				"key_name": "crypto-key",
			},
		},
		{
			name:     "datakey wrapped - service not available",
			toolName: "quicbaas_crypto_datakey_wrapped",
			args: map[string]interface{}{
				"key_name": "crypto-key",
			},
		},
		{
			name:     "encrypt batch - service not available",
			toolName: "quicbaas_crypto_encrypt_batch",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"items": []interface{}{
					map[string]interface{}{"plaintext": "SGVsbG8="},
				},
			},
		},
		{
			name:     "decrypt batch - service not available",
			toolName: "quicbaas_crypto_decrypt_batch",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"items": []interface{}{
					map[string]interface{}{"ciphertext": "encrypted"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// Test edge cases in parameter extraction
func TestToolParameterExtraction(t *testing.T) {
	t.Run("KMS generate key with all parameters", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"algorithm":           "AES-256-GCM",
			"name":                "test-key",
			"description":         "test description",
			"exportable":          true,
			"deletion_protection": false,
			"usage":               []interface{}{"encrypt", "decrypt"},
			"tags": map[string]interface{}{
				"env":  "test",
				"team": "security",
			},
			"owner": "test-user",
		}

		// This will fail because service doesn't implement the interface,
		// but it will test the parameter extraction code
		_, err := registry.ExecuteTool(ctx, "quicbaas_kms_generate_key", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("KMS import key with all parameters", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_material":    "base64data",
			"wrapping_key_id": "wrapping-key-1",
			"algorithm":       "RSA-2048",
			"name":            "imported-key",
			"description":     "imported from external system",
			"exportable":      false,
			"usage":           []interface{}{"sign", "verify"},
			"tags": map[string]interface{}{
				"source": "external",
			},
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_kms_import_key", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("Secrets store with complex data", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"path": "/secret/app/db",
			"data": map[string]interface{}{
				"username": "admin",
				"password": "secret123",
				"host":     "db.example.com",
				"port":     float64(5432),
			},
			"version": float64(1),
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_secrets_store", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("Crypto encrypt with context", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_name":  "crypto-key",
			"plaintext": "SGVsbG8gV29ybGQ=",
			"context":   "dXNlci1pZDoxMjM=",
			"nonce":     "random-nonce",
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("KMS set policy with complex policy", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_id": "key-1",
			"policy": map[string]interface{}{
				"version": "1.0",
				"statements": []interface{}{
					map[string]interface{}{
						"effect":    "allow",
						"actions":   []interface{}{"encrypt", "decrypt"},
						"resources": []interface{}{"*"},
					},
				},
			},
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_kms_set_policy", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("Seal init with recovery keys", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"secret_shares":      float64(5),
			"secret_threshold":   float64(3),
			"recovery_shares":    float64(3),
			"recovery_threshold": float64(2),
			"stored_shares":      float64(1),
			"pgp_keys":           []interface{}{"key1", "key2"},
			"recovery_pgp_keys":  []interface{}{"rkey1"},
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_seal_init", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("Crypto batch operations with multiple items", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_name": "crypto-key",
			"items": []interface{}{
				map[string]interface{}{
					"plaintext": "SGVsbG8=",
					"context":   "Y3R4MQ==",
				},
				map[string]interface{}{
					"plaintext": "V29ybGQ=",
					"context":   "Y3R4Mg==",
				},
			},
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})
}

// TestSetAuthorizationService tests the SetAuthorizationService method.
func TestSetAuthorizationService(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)

	// Create mock authorization services
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Set authorization service
	server.SetAuthorizationService(mockAuthService, mockAuthManager)

	// Verify the checker was updated
	assert.NotNil(t, server.authChecker)
}

// TestHandleAuthError tests the handleAuthError method.
func TestHandleAuthError(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	server := NewServer(mockSvc, nil, log)

	testCases := []struct {
		name         string
		errorType    AuthorizationErrorType
		expectedCode int
	}{
		{
			name:         "missing token error",
			errorType:    AuthErrorMissingToken,
			expectedCode: AuthenticationRequired,
		},
		{
			name:         "invalid token error",
			errorType:    AuthErrorInvalidToken,
			expectedCode: InvalidToken,
		},
		{
			name:         "permission denied error",
			errorType:    AuthErrorPermissionDenied,
			expectedCode: PermissionDenied,
		},
		{
			name:         "unknown error type",
			errorType:    AuthorizationErrorType(99),
			expectedCode: InternalError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			authErr := &AuthorizationError{
				Type:    tc.errorType,
				Message: "test error message",
			}

			resp := server.handleAuthError(1, authErr)
			assert.NotNil(t, resp)
			assert.Equal(t, "2.0", resp.JSONRPC)
			assert.Equal(t, 1, resp.ID)
			assert.NotNil(t, resp.Error)
			assert.Equal(t, tc.expectedCode, resp.Error.Code)
			assert.Equal(t, "test error message", resp.Error.Message)
		})
	}
}

// TestExtractTokenFromHTTPRequestCoverage tests the extractTokenFromHTTPRequest function.
func TestExtractTokenFromHTTPRequestCoverage(t *testing.T) {
	testCases := []struct {
		name          string
		setupRequest  func(*http.Request)
		expectedToken string
	}{
		{
			name: "bearer token in authorization header",
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer my-secret-token")
			},
			expectedToken: "my-secret-token",
		},
		{
			name: "raw token in authorization header",
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "raw-token-value")
			},
			expectedToken: "raw-token-value",
		},
		{
			name: "token in X-Auth-Token header",
			setupRequest: func(r *http.Request) {
				r.Header.Set("X-Auth-Token", "x-auth-token-value")
			},
			expectedToken: "x-auth-token-value",
		},
		{
			name: "no token present",
			setupRequest: func(r *http.Request) {
				// No headers set
			},
			expectedToken: "",
		},
		{
			name: "authorization header takes precedence",
			setupRequest: func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer auth-token")
				r.Header.Set("X-Auth-Token", "x-auth-token")
			},
			expectedToken: "auth-token",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			tc.setupRequest(req)

			token := extractTokenFromHTTPRequest(req)
			assert.Equal(t, tc.expectedToken, token)
		})
	}
}

// TestContextWithTokenCoverage tests the context token functions.
func TestContextWithTokenCoverage(t *testing.T) {
	t.Run("add and retrieve token from context", func(t *testing.T) {
		ctx := context.Background()
		expectedToken := "test-token-123"

		ctx = contextWithToken(ctx, expectedToken)
		retrievedToken := tokenFromContext(ctx)

		assert.Equal(t, expectedToken, retrievedToken)
	})

	t.Run("retrieve token from empty context", func(t *testing.T) {
		ctx := context.Background()
		token := tokenFromContext(ctx)
		assert.Equal(t, "", token)
	})
}

// TestParseJSONObject tests the parseJSONObject function.
func TestParseJSONObject(t *testing.T) {
	t.Run("successful parsing", func(t *testing.T) {
		src := map[string]interface{}{
			"name":  "test",
			"value": float64(123),
		}

		type TestStruct struct {
			Name  string  `json:"name"`
			Value float64 `json:"value"`
		}

		var dst TestStruct
		err := parseJSONObject(src, &dst)
		assert.NoError(t, err)
		assert.Equal(t, "test", dst.Name)
		assert.Equal(t, float64(123), dst.Value)
	})

	t.Run("parsing with invalid destination", func(t *testing.T) {
		src := map[string]interface{}{
			"name": "test",
		}

		// Unmarshal into a non-pointer
		var dst string
		err := parseJSONObject(src, &dst)
		assert.Error(t, err)
	})

	t.Run("parsing with unmarshalable source", func(t *testing.T) {
		src := map[string]interface{}{
			"channel": make(chan int), // Cannot be marshaled to JSON
		}

		type TestStruct struct {
			Channel interface{} `json:"channel"`
		}

		var dst TestStruct
		err := parseJSONObject(src, &dst)
		assert.Error(t, err)
	})
}

// TestGetToolResourceActionForTest tests the exported test helper.
func TestGetToolResourceActionForTest(t *testing.T) {
	testCases := []struct {
		toolName         string
		expectedResource string
		expectedAction   string
	}{
		{"quicbaas_get", interfaces.ResourceKeys, interfaces.ActionRead},
		{"quicbaas_put", interfaces.ResourceKeys, interfaces.ActionWrite},
		{"quicbaas_delete", interfaces.ResourceKeys, interfaces.ActionDelete},
		{"quicbaas_cluster_status", interfaces.ResourceCluster, interfaces.ActionRead},
		{"unknown_tool", interfaces.ResourceCluster, interfaces.ActionAdmin},
	}

	for _, tc := range testCases {
		t.Run(tc.toolName, func(t *testing.T) {
			resource, action := GetToolResourceActionForTest(tc.toolName)
			assert.Equal(t, tc.expectedResource, resource)
			assert.Equal(t, tc.expectedAction, action)
		})
	}
}

// TestToolsCallWithAuthorizationCheck tests tools/call with authorization checking.
func TestToolsCallWithAuthorizationCheck(t *testing.T) {
	t.Run("authorization required - missing token", func(t *testing.T) {
		mockSvc := &MockQuicBaaS{}
		mockAuthService := new(MockAuthorizationService)
		mockAuthManager := new(MockAuthManager)

		log, _ := logger.New(logger.LogConfig{
			Level:     "error",
			Format:    "json",
			Output:    "stdout",
			Component: "mcp-test",
		})

		config := &Config{
			ServerName:            "Test Server",
			ServerVersion:         "1.0.0",
			Transport:             "http",
			HTTPAddr:              "127.0.0.1:0",
			AuthorizationService:  mockAuthService,
			AuthManager:           mockAuthManager,
			RequestTimeout:        5 * time.Second,
			MaxConcurrentRequests: 10,
		}

		server := NewServer(mockSvc, config, log)

		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "tools/call",
			Params: map[string]interface{}{
				"name":      "quicbaas_get", // Not a public tool
				"arguments": map[string]interface{}{},
			},
		}
		resp := server.handleRequest(context.Background(), req)

		assert.NotNil(t, resp.Error)
		assert.Equal(t, AuthenticationRequired, resp.Error.Code)
	})

	t.Run("public tool bypasses authorization", func(t *testing.T) {
		mockSvc := &MockQuicBaaS{}
		mockAuthService := new(MockAuthorizationService)
		mockAuthManager := new(MockAuthManager)

		// Mock health response
		mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
			Status:        service.HealthStatusHealthy,
			NodeID:        1,
			Version:       "1.0.0",
			UptimeSeconds: 3600,
		}, nil)

		log, _ := logger.New(logger.LogConfig{
			Level:     "error",
			Format:    "json",
			Output:    "stdout",
			Component: "mcp-test",
		})

		config := &Config{
			ServerName:            "Test Server",
			ServerVersion:         "1.0.0",
			Transport:             "http",
			HTTPAddr:              "127.0.0.1:0",
			AuthorizationService:  mockAuthService,
			AuthManager:           mockAuthManager,
			RequestTimeout:        5 * time.Second,
			MaxConcurrentRequests: 10,
		}

		server := NewServer(mockSvc, config, log)

		req := &JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "tools/call",
			Params: map[string]interface{}{
				"name":      "quicbaas_health", // Public tool
				"arguments": map[string]interface{}{},
			},
		}
		resp := server.handleRequest(context.Background(), req)

		assert.Nil(t, resp.Error)
		assert.NotNil(t, resp.Result)
	})
}

// TestHTTPRequestWithToken tests HTTP requests with authentication tokens.
func TestHTTPRequestWithToken(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Set up mock for successful token validation
	mockAuthManager.On("ValidateToken", mock.Anything, "valid-token").Return(&MockTokenValidationResult{
		Valid:    true,
		UserID:   "user-123",
		Username: "testuser",
		Roles:    []string{"admin"},
	}, nil)

	// Set up mock for successful authorization
	mockAuthService.On("Authorize", mock.Anything, mock.Anything).Return(&interfaces.AuthorizationDecision{
		Allowed: true,
	}, nil)

	// Set up mock for Get operation
	mockSvc.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
		ID:        123,
		Data:      []byte("test-data"),
		Version:   1,
		Timestamp: time.Now().Unix(),
		Found:     true,
	}, nil)

	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	config := &Config{
		ServerName:            "Test Server",
		ServerVersion:         "1.0.0",
		Transport:             "http",
		HTTPAddr:              "127.0.0.1:0",
		AuthorizationService:  mockAuthService,
		AuthManager:           mockAuthManager,
		RequestTimeout:        5 * time.Second,
		MaxConcurrentRequests: 10,
	}

	server := NewServer(mockSvc, config, log)

	reqBody := &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "quicbaas_get",
			"arguments": map[string]interface{}{
				"entityType": "user",
				"id":         float64(123),
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer valid-token")

	w := httptest.NewRecorder()
	server.handleHTTPRequest(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp JSONRPCResponse
	err = json.NewDecoder(w.Body).Decode(&resp)
	require.NoError(t, err)

	assert.Nil(t, resp.Error)
	assert.NotNil(t, resp.Result)
}

// TestExtractBoolFieldCoverage tests the extractBoolField helper with various inputs.
func TestExtractBoolFieldCoverage(t *testing.T) {
	t.Run("with IsValid method", func(t *testing.T) {
		// Create a type that implements IsValid
		type validatable struct {
			valid bool
		}
		v := &struct {
			isValid bool
		}{isValid: true}

		result := extractBoolField(v, "Valid")
		// Should return false since v doesn't implement the expected interface
		assert.False(t, result)
	})

	t.Run("with nil value", func(t *testing.T) {
		result := extractBoolField(nil, "Valid")
		assert.False(t, result)
	})
}

// TestExtractStringFieldCoverage tests the extractStringField helper.
func TestExtractStringFieldCoverage(t *testing.T) {
	t.Run("with unknown field name", func(t *testing.T) {
		v := &MockTokenValidationResult{
			UserID:   "test-user",
			Username: "testuser",
		}

		result := extractStringField(v, "UnknownField")
		assert.Equal(t, "", result)
	})

	t.Run("with nil value", func(t *testing.T) {
		result := extractStringField(nil, "UserID")
		assert.Equal(t, "", result)
	})
}

// TestExtractStringSliceFieldCoverage tests the extractStringSliceField helper.
func TestExtractStringSliceFieldCoverage(t *testing.T) {
	t.Run("with unknown field name", func(t *testing.T) {
		v := &MockTokenValidationResult{
			Roles: []string{"admin"},
		}

		result := extractStringSliceField(v, "UnknownField")
		assert.Nil(t, result)
	})

	t.Run("with nil value", func(t *testing.T) {
		result := extractStringSliceField(nil, "Roles")
		assert.Nil(t, result)
	})
}

// TestTokenValidationMapFormat tests token validation with map format result.
func TestTokenValidationMapFormat(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	// Return token validation result as a map
	mockAuthManager.On("ValidateToken", mock.Anything, "map-token").Return(map[string]interface{}{
		"Valid":    true,
		"UserID":   "user-map",
		"Username": "mapuser",
		"Roles":    []string{"reader"},
	}, nil)

	mockAuthService.On("Authorize", mock.Anything, mock.Anything).Return(&interfaces.AuthorizationDecision{
		Allowed: true,
	}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_get", "map-token")
	assert.Nil(t, err)
}

// TestResourceRegistrySuccessPaths tests successful paths in resource registry.
func TestResourceRegistrySuccessPaths(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	t.Run("read cluster nodes", func(t *testing.T) {
		mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
			ClusterID: "test-cluster",
			Nodes: []*service.NodeInfo{
				{NodeID: 1, Address: "localhost:8001"},
			},
			Shards:    []*service.ShardInfo{},
			Timestamp: time.Now().Unix(),
		}, nil).Once()

		result, err := registry.ReadResource(ctx, "quicbaas://cluster/nodes")
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("read cluster shards", func(t *testing.T) {
		mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).Return(&service.ClusterStatusResponse{
			ClusterID: "test-cluster",
			Nodes:     []*service.NodeInfo{},
			Shards: []*service.ShardInfo{
				{ShardID: 1, LeaderID: 1},
			},
			Timestamp: time.Now().Unix(),
		}, nil).Once()

		result, err := registry.ReadResource(ctx, "quicbaas://cluster/shards")
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("read metrics summary", func(t *testing.T) {
		mockSvc.On("GetCacheStats", mock.Anything, mock.Anything).Return(&service.GetCacheStatsResponse{
			HitRate: 0.95,
			Size:    1000,
			MaxSize: 10000,
		}, nil).Once()

		mockSvc.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
			Status:        service.HealthStatusHealthy,
			UptimeSeconds: 3600,
			Details:       map[string]*service.ComponentHealth{},
		}, nil).Once()

		result, err := registry.ReadResource(ctx, "quicbaas://metrics/summary")
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("read users", func(t *testing.T) {
		result, err := registry.ReadResource(ctx, "quicbaas://users")
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("read roles", func(t *testing.T) {
		result, err := registry.ReadResource(ctx, "quicbaas://roles")
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("read backups", func(t *testing.T) {
		mockSvc.On("ListBackups", mock.Anything, mock.Anything).Return(&service.ListBackupsResponse{
			Backups:    []*service.BackupMetadata{},
			TotalCount: 0,
		}, nil).Once()

		result, err := registry.ReadResource(ctx, "quicbaas://backups")
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})
}

// TestResourceRegistryErrorPaths tests error paths in resource registry.
func TestResourceRegistryErrorPaths(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	t.Run("cluster status error", func(t *testing.T) {
		mockSvc.On("ClusterStatus", mock.Anything, mock.Anything).Return(nil, &errors.MessageError{Message: "cluster error"}).Once()

		result, err := registry.ReadResource(ctx, "quicbaas://cluster/status")
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("cache stats error", func(t *testing.T) {
		mockSvc.getCacheStatsFunc = func(ctx context.Context, req *service.GetCacheStatsRequest) (*service.GetCacheStatsResponse, error) {
			return nil, &errors.MessageError{Message: "cache error"}
		}
		defer func() { mockSvc.getCacheStatsFunc = nil }()

		result, err := registry.readCacheResource(ctx, []string{"stats"})
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("health error", func(t *testing.T) {
		mockSvc.On("Health", mock.Anything, mock.Anything).Return(nil, &errors.MessageError{Message: "health error"}).Once()

		result, err := registry.readHealthResource(ctx)
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("get entity error", func(t *testing.T) {
		mockSvc.On("Get", mock.Anything, mock.Anything).Return(nil, &errors.MessageError{Message: "get error"}).Once()

		result, err := registry.readEntitiesResource(ctx, []string{"users", "123"})
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestToolRegistrySetAuthorizationChecker tests the SetAuthorizationChecker method.
func TestToolRegistrySetAuthorizationChecker(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)

	checker := NewAuthorizationChecker(nil, nil)
	registry.SetAuthorizationChecker(checker)

	assert.Equal(t, checker, registry.authChecker)
}

// TestToolPrefixMappingCoverage tests all tool prefix mappings for coverage.
func TestToolPrefixMappingCoverage(t *testing.T) {
	// Test various prefix patterns to ensure full coverage
	testCases := []struct {
		toolName         string
		expectedResource string
		expectedAction   string
	}{
		// KMS Operations - exact matches
		{"quicbaas_kms_generate_key", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"quicbaas_kms_import_key", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"quicbaas_kms_get_key", interfaces.ResourceKMS, interfaces.ActionRead},
		{"quicbaas_kms_list_keys", interfaces.ResourceKMS, interfaces.ActionRead},
		{"quicbaas_kms_rotate_key", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"quicbaas_kms_revoke_key", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"quicbaas_kms_delete_key", interfaces.ResourceKMS, interfaces.ActionDelete},
		{"quicbaas_kms_update_metadata", interfaces.ResourceKMS, interfaces.ActionWrite},
		{"quicbaas_kms_set_policy", interfaces.ResourceKMS, interfaces.ActionWrite},

		// Secrets Operations
		{"quicbaas_secrets_get_something", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"quicbaas_secrets_list_all", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"quicbaas_secrets_store_new", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"quicbaas_secrets_put_value", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"quicbaas_secrets_delete_old", interfaces.ResourceSecrets, interfaces.ActionDelete},
		{"quicbaas_secrets_version_check", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"quicbaas_secrets_versions_list", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"quicbaas_secrets_rollback_to", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"quicbaas_secrets_rotate_now", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"quicbaas_secrets_set_config", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"quicbaas_secrets_set_rotation_policy_daily", interfaces.ResourceSecrets, interfaces.ActionWrite},
		{"quicbaas_secrets_get_rotation_policy_current", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"quicbaas_secrets_get_prefix_my", interfaces.ResourceSecrets, interfaces.ActionRead},
		{"quicbaas_secrets_watch_changes", interfaces.ResourceSecrets, interfaces.ActionRead},

		// Crypto Operations
		{"quicbaas_crypto_encrypt_data", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_decrypt_data", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_sign_message", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_verify_signature", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_hmac_compute", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_hash_sha256", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_random_bytes", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_datakey_generate", interfaces.ResourceCrypto, interfaces.ActionWrite},
		{"quicbaas_crypto_rewrap_key", interfaces.ResourceCrypto, interfaces.ActionWrite},

		// Seal Operations
		{"quicbaas_seal_status", interfaces.ResourceSeal, interfaces.ActionAdmin},
		{"quicbaas_seal_init", interfaces.ResourceSeal, interfaces.ActionAdmin},
		{"quicbaas_unseal_with_key", interfaces.ResourceSeal, interfaces.ActionAdmin},

		// Timeseries Operations
		{"quicbaas_timeseries_query_range", interfaces.ResourceTimeseries, interfaces.ActionRead},
		{"quicbaas_timeseries_write_point", interfaces.ResourceTimeseries, interfaces.ActionWrite},
		{"quicbaas_timeseries_delete_old", interfaces.ResourceTimeseries, interfaces.ActionDelete},

		// Vector Operations
		{"quicbaas_vector_search_similar", interfaces.ResourceVector, interfaces.ActionRead},
		{"quicbaas_vector_get_by_id", interfaces.ResourceVector, interfaces.ActionRead},
		{"quicbaas_vector_upsert_new", interfaces.ResourceVector, interfaces.ActionWrite},
		{"quicbaas_vector_insert_batch", interfaces.ResourceVector, interfaces.ActionWrite},
		{"quicbaas_vector_delete_old", interfaces.ResourceVector, interfaces.ActionDelete},
	}

	for _, tc := range testCases {
		t.Run(tc.toolName, func(t *testing.T) {
			resource, action := getToolResourceAction(tc.toolName)
			assert.Equal(t, tc.expectedResource, resource, "resource mismatch for %s", tc.toolName)
			assert.Equal(t, tc.expectedAction, action, "action mismatch for %s", tc.toolName)
		})
	}
}

// TestAuthorizationCheckerPermissionDeniedWithEmptyReason tests permission denied with empty reason.
func TestAuthorizationCheckerPermissionDeniedWithEmptyReason(t *testing.T) {
	mockAuthService := new(MockAuthorizationService)
	mockAuthManager := new(MockAuthManager)

	mockAuthManager.On("ValidateToken", mock.Anything, "valid-token").Return(&MockTokenValidationResult{
		Valid:    true,
		UserID:   "user-123",
		Username: "testuser",
		Roles:    []string{"reader"},
	}, nil)

	// Return decision with empty reason
	mockAuthService.On("Authorize", mock.Anything, mock.Anything).Return(&interfaces.AuthorizationDecision{
		Allowed: false,
		Reason:  "", // Empty reason
	}, nil)

	checker := NewAuthorizationChecker(mockAuthService, mockAuthManager)

	err := checker.CheckToolAuthorization(context.Background(), "quicbaas_put", "valid-token")

	require.NotNil(t, err)
	assert.Equal(t, AuthErrorPermissionDenied, err.Type)
	assert.Contains(t, err.Message, "insufficient permissions")
}

// TestToolRegistryExecuteToolRouting tests all tool execution routing paths.
func TestToolRegistryExecuteToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test all seal operation routes
	sealTools := []string{
		"quicbaas_seal_status",
		"quicbaas_seal_init",
		"quicbaas_unseal",
		"quicbaas_seal",
		"quicbaas_seal_rekey",
		"quicbaas_seal_generate_recovery_key",
		"quicbaas_seal_delete_recovery_key",
		"quicbaas_seal_generate_root_token",
	}

	for _, tool := range sealTools {
		t.Run(tool, func(t *testing.T) {
			// All seal tools require seal service which mock doesn't have
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			// These should fail because seal service is not available on mock
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}

	// Test all KMS operation routes
	kmsTools := []string{
		"quicbaas_kms_generate_key",
		"quicbaas_kms_import_key",
		"quicbaas_kms_get_key",
		"quicbaas_kms_list_keys",
		"quicbaas_kms_rotate_key",
		"quicbaas_kms_revoke_key",
		"quicbaas_kms_delete_key",
		"quicbaas_kms_get_metadata",
		"quicbaas_kms_update_metadata",
		"quicbaas_kms_set_policy",
		"quicbaas_kms_get_policy",
		"quicbaas_kms_encrypt",
		"quicbaas_kms_decrypt",
		"quicbaas_kms_backend_test",
		"quicbaas_kms_key_versions",
		"quicbaas_kms_key_status_update",
		"quicbaas_kms_backend_status",
	}

	for _, tool := range kmsTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			// KMS tools require KMS service
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}

	// Test DEK operation routes
	dekTools := []string{
		"quicbaas_dek_create",
		"quicbaas_dek_get",
		"quicbaas_dek_list",
		"quicbaas_dek_rotate",
		"quicbaas_dek_delete",
		"quicbaas_dek_status",
		"quicbaas_dek_set_default",
		"quicbaas_dek_set_safety",
		"quicbaas_dek_clear_safety",
		"quicbaas_dek_start_reencryption",
		"quicbaas_dek_get_reencryption_progress",
	}

	for _, tool := range dekTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}

	// Test secrets operation routes
	secretsTools := []string{
		"quicbaas_secrets_store",
		"quicbaas_secrets_get",
		"quicbaas_secrets_list",
		"quicbaas_secrets_delete",
		"quicbaas_secrets_versions",
		"quicbaas_secrets_rollback",
		"quicbaas_secrets_rotate",
		"quicbaas_secrets_set_rotation_policy",
		"quicbaas_secrets_get_rotation_policy",
		"quicbaas_secrets_get_prefix",
		"quicbaas_secrets_watch",
	}

	for _, tool := range secretsTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}

	// Test crypto operation routes
	cryptoTools := []string{
		"quicbaas_crypto_encrypt",
		"quicbaas_crypto_decrypt",
		"quicbaas_crypto_rewrap",
		"quicbaas_crypto_sign",
		"quicbaas_crypto_verify",
		"quicbaas_crypto_hmac",
		"quicbaas_crypto_verify_hmac",
		"quicbaas_crypto_hash",
		"quicbaas_crypto_random",
		"quicbaas_crypto_datakey",
		"quicbaas_crypto_datakey_wrapped",
		"quicbaas_crypto_encrypt_batch",
		"quicbaas_crypto_decrypt_batch",
	}

	for _, tool := range cryptoTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestObjectStorageToolRouting tests object storage tool execution routing.
func TestObjectStorageToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	// Return nil for ObjectStorageService to trigger "not available" errors
	mockSvc.On("ObjectStorageService").Return(nil)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	objectStorageTools := []string{
		"quicbaas_object_put",
		"quicbaas_object_get",
		"quicbaas_object_delete",
		"quicbaas_object_head",
		"quicbaas_object_list",
		"quicbaas_object_list_by_tag",
		"quicbaas_object_list_by_tenant",
		"quicbaas_object_copy",
		"quicbaas_object_replicate",
		"quicbaas_object_migrate",
		"quicbaas_object_archive",
		"quicbaas_object_restore",
		"quicbaas_object_verify",
		"quicbaas_object_set_policy",
		"quicbaas_object_get_policy",
		"quicbaas_object_list_backends",
		"quicbaas_object_backend_health",
		"quicbaas_object_tenant_usage",
		"quicbaas_object_tenant_quotas",
	}

	for _, tool := range objectStorageTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestAlgorithmToKeyAttributes tests the algorithmToKeyAttributes function.
func TestAlgorithmToKeyAttributes(t *testing.T) {
	testCases := []struct {
		algorithm             string
		expectedSymmetricAlgo xkmssdk.SymmetricAlgorithm
		description           string
	}{
		{"AES-256-GCM", xkmssdk.SymmetricAES256GCM, "AES-256-GCM format"},
		{"AES256GCM", xkmssdk.SymmetricAES256GCM, "AES256GCM format"},
		{"AES-192-GCM", xkmssdk.SymmetricAES192GCM, "AES-192-GCM format"},
		{"AES192GCM", xkmssdk.SymmetricAES192GCM, "AES192GCM format"},
		{"AES-128-GCM", xkmssdk.SymmetricAES128GCM, "AES-128-GCM format"},
		{"AES128GCM", xkmssdk.SymmetricAES128GCM, "AES128GCM format"},
		{"CHACHA20-POLY1305", xkmssdk.SymmetricChaCha20Poly1305, "CHACHA20-POLY1305 format"},
		{"ChaCha20-Poly1305", xkmssdk.SymmetricChaCha20Poly1305, "ChaCha20-Poly1305 format"},
		{"XCHACHA20-POLY1305", xkmssdk.SymmetricXChaCha20Poly1305, "XCHACHA20-POLY1305 format"},
		{"XChaCha20-Poly1305", xkmssdk.SymmetricXChaCha20Poly1305, "XChaCha20-Poly1305 format"},
		{"unknown", xkmssdk.SymmetricAES256GCM, "unknown algorithm defaults to AES-256-GCM"},
		{"", xkmssdk.SymmetricAES256GCM, "empty algorithm defaults to AES-256-GCM"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			attrs := algorithmToKeyAttributes(tc.algorithm)
			assert.NotNil(t, attrs)
			assert.Equal(t, "data-key", attrs.CN)
			assert.Equal(t, tc.expectedSymmetricAlgo, attrs.SymmetricAlgorithm)
		})
	}
}

// TestVectorToolRouting tests vector tool execution routing.
func TestVectorToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	vectorTools := []string{
		"quicbaas_vector_search",
		"quicbaas_vector_upsert",
		"quicbaas_vector_delete",
		"quicbaas_vector_get",
		"quicbaas_vector_search_hybrid",
		"quicbaas_vector_create_index",
		"quicbaas_vector_delete_index",
		"quicbaas_vector_list_indexes",
		"quicbaas_vector_describe_index",
		"quicbaas_vector_update_metadata",
	}

	for _, tool := range vectorTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestTimeseriesToolRouting tests timeseries tool execution routing.
func TestTimeseriesToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	timeseriesTools := []string{
		"quicbaas_timeseries_write",
		"quicbaas_timeseries_query",
		"quicbaas_timeseries_delete",
		"quicbaas_timeseries_aggregate",
		"quicbaas_timeseries_create_retention_policy",
		"quicbaas_timeseries_get_retention_policy",
		"quicbaas_timeseries_delete_retention_policy",
		"quicbaas_timeseries_compact",
		"quicbaas_timeseries_get_stats",
	}

	for _, tool := range timeseriesTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestResourceReadWithSuccessfulGetEntity tests resource read with successful entity retrieval.
func TestResourceReadWithSuccessfulGetEntity(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	mockSvc.On("Get", mock.Anything, mock.MatchedBy(func(req *service.GetRequest) bool {
		return req.EntityType == "users" && req.ID == uint64(456)
	})).Return(&service.GetResponse{
		ID:        456,
		Data:      []byte(`{"name":"test user"}`),
		Version:   1,
		Timestamp: time.Now().Unix(),
		Found:     true,
	}, nil).Once()

	result, err := registry.ReadResource(ctx, "quicbaas://entities/users/456")
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestResourceReadMetricsSummaryHealthError tests error path when health fails during metrics summary.
func TestResourceReadMetricsSummaryHealthError(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	// Cache stats succeeds
	mockSvc.On("GetCacheStats", mock.Anything, mock.Anything).Return(&service.GetCacheStatsResponse{
		HitRate: 0.95,
		Size:    1000,
		MaxSize: 10000,
	}, nil).Once()

	// Health fails
	mockSvc.On("Health", mock.Anything, mock.Anything).Return(nil, &errors.MessageError{Message: "health check failed"}).Once()

	result, err := registry.ReadResource(ctx, "quicbaas://metrics/summary")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "health check failed")
}

// TestResourceReadListBackupsError tests error path when ListBackups fails.
func TestResourceReadListBackupsError(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewResourceRegistry(mockSvc, log)
	ctx := context.Background()

	mockSvc.listBackupsFunc = func(ctx context.Context, req *service.ListBackupsRequest) (*service.ListBackupsResponse, error) {
		return nil, &errors.MessageError{Message: "backup list error"}
	}
	defer func() { mockSvc.listBackupsFunc = nil }()

	result, err := registry.ReadResource(ctx, "quicbaas://backups")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "backup list error")
}

// TestExtractBoolFieldWithValidResult tests extractBoolField with a valid token result.
func TestExtractBoolFieldWithValidResult(t *testing.T) {
	result := &MockTokenValidationResult{
		Valid:    true,
		UserID:   "user-1",
		Username: "test",
		Roles:    []string{"admin"},
	}

	// Test Valid field extraction
	valid := extractBoolField(result, "Valid")
	assert.True(t, valid)
}

// TestExtractStringFieldWithValidResult tests extractStringField with a valid token result.
func TestExtractStringFieldWithValidResult(t *testing.T) {
	result := &MockTokenValidationResult{
		Valid:    true,
		UserID:   "user-123",
		Username: "testuser",
		Roles:    []string{"admin"},
	}

	// Test UserID field extraction
	userID := extractStringField(result, "UserID")
	assert.Equal(t, "user-123", userID)

	// Test Username field extraction
	username := extractStringField(result, "Username")
	assert.Equal(t, "testuser", username)
}

// TestExtractStringSliceFieldWithValidResult tests extractStringSliceField with valid roles.
func TestExtractStringSliceFieldWithValidResult(t *testing.T) {
	result := &MockTokenValidationResult{
		Valid:    true,
		UserID:   "user-123",
		Username: "testuser",
		Roles:    []string{"admin", "reader", "writer"},
	}

	// Test Roles field extraction
	roles := extractStringSliceField(result, "Roles")
	assert.Equal(t, []string{"admin", "reader", "writer"}, roles)
}
