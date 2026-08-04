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

//go:build integration

package secretsresolver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Test Configuration Structs
// =============================================================================

// TestDatabaseConfig represents a database configuration with secret fields.
type TestDatabaseConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	SSLMode  string `json:"ssl_mode"`
}

// TestAuthConfig represents authentication configuration.
type TestAuthConfig struct {
	JWTSecret    string `json:"jwt_secret"`
	APIKeyHash   string `json:"api_key_hash"`
	SessionKey   string `json:"session_key"`
	RefreshToken string `json:"refresh_token"`
}

// TestEncryptionConfig represents encryption configuration.
type TestEncryptionConfig struct {
	MasterKey    string `json:"master_key"`
	DataKey      string `json:"data_key"`
	SigningKey   string `json:"signing_key"`
	EncryptionIV string `json:"encryption_iv"`
}

// TestServiceConfig represents a single service configuration.
type TestServiceConfig struct {
	Name       string `json:"name"`
	Endpoint   string `json:"endpoint"`
	APIKey     string `json:"api_key"`
	SecretKey  string `json:"secret_key"`
	MaxRetries int    `json:"max_retries"`
}

// TestNestedConfig represents a deeply nested configuration.
type TestNestedConfig struct {
	Database   TestDatabaseConfig   `json:"database"`
	Auth       TestAuthConfig       `json:"auth"`
	Encryption TestEncryptionConfig `json:"encryption"`
}

// TestConfigWithSlices represents configuration with slice fields.
type TestConfigWithSlices struct {
	APIKeys  []string            `json:"api_keys"`
	Services []TestServiceConfig `json:"services"`
	Tokens   []string            `json:"tokens"`
}

// TestConfigWithMaps represents configuration with map fields.
type TestConfigWithMaps struct {
	Credentials map[string]string            `json:"credentials"`
	Endpoints   map[string]TestServiceConfig `json:"endpoints"`
	Settings    map[string]string            `json:"settings"`
}

// TestMixedConfig represents configuration with mixed resolution types.
type TestMixedConfig struct {
	Database TestDatabaseConfig `json:"database"`
	APIKey   string             `json:"api_key"`
	AutoKey  string             `json:"auto_key"`
	Literal  string             `json:"literal"`
}

// TestRealConfig represents a realistic quicbaas-like configuration.
type TestRealConfig struct {
	Server   TestServerConfig   `json:"server"`
	Database TestDatabaseConfig `json:"database"`
	KMS      TestKMSConfig      `json:"kms"`
	Logging  TestLoggingConfig  `json:"logging"`
}

// TestServerConfig represents server configuration.
type TestServerConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	TLSCertPath    string `json:"tls_cert_path"`
	TLSKeyPath     string `json:"tls_key_path"`
	TLSKeyPassword string `json:"tls_key_password"`
}

// TestKMSConfig represents KMS configuration.
type TestKMSConfig struct {
	Provider     string              `json:"provider"`
	MasterKeyID  string              `json:"master_key_id"`
	SealKey      string              `json:"seal_key"`
	Backends     []TestBackendConfig `json:"backends"`
	WrappingKeys map[string]string   `json:"wrapping_keys"`
}

// TestBackendConfig represents a KMS backend configuration.
type TestBackendConfig struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	AuthKey  string `json:"auth_key"`
	SignKey  string `json:"sign_key"`
	Enabled  bool   `json:"enabled"`
	Priority int    `json:"priority"`
}

// TestLoggingConfig represents logging configuration.
type TestLoggingConfig struct {
	Level   string `json:"level"`
	Format  string `json:"format"`
	APIKey  string `json:"api_key"`
	Enabled bool   `json:"enabled"`
}

// =============================================================================
// Helper Functions
// =============================================================================

// storeProcessorTestSecret stores a secret via REST API for processor testing.
func storeProcessorTestSecret(t *testing.T, endpoint, path, value string) {
	t.Helper()

	reqBody := map[string]interface{}{
		"data": map[string]interface{}{
			"value": value,
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	url := fmt.Sprintf("%s/api/v1/secrets/%s", endpoint, path)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(bodyBytes))
	require.NoError(t, err)

	req.Header.Set("Content-Type", "application/json")

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("Failed to store secret at %s (status %d): %s", path, resp.StatusCode, string(respBody))
	}
}

// deleteProcessorTestSecret deletes a secret via REST API for cleanup.
func deleteProcessorTestSecret(t *testing.T, endpoint, path string) {
	t.Helper()

	url := fmt.Sprintf("%s/api/v1/secrets/%s?destroy=true", endpoint, path)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Logf("Failed to create delete request for %s: %v", path, err)
		return
	}

	client := helpers.NewHTTPClient(10 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("Failed to delete secret %s: %v", path, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		t.Logf("Unexpected status deleting secret %s: %d, body: %s", path, resp.StatusCode, string(respBody))
	}
}

// createProcessorTestRegistry creates a registry with both KMS and AutoGen resolvers.
func createProcessorTestRegistry(t *testing.T, endpoint string) *secrets.ResolverRegistry {
	t.Helper()

	kmsClient := newRESTKMSClient(endpoint)
	kmsResolver := secrets.NewLocalKMSResolver(kmsClient)
	require.NotNil(t, kmsResolver)

	autoGenResolver := secrets.NewAutoGenResolver()
	require.NotNil(t, autoGenResolver)

	registry := secrets.NewResolverRegistry()
	registry.Register(kmsResolver)
	registry.Register(autoGenResolver)

	return registry
}

// =============================================================================
// ConfigProcessor Integration Tests
// =============================================================================

// TestConfigProcessor_NestedStruct tests processing a deeply nested config struct
// with kms:// references at various levels.
func TestConfigProcessor_NestedStruct(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/nested/%d", time.Now().UnixNano())

	// Store test secrets
	secretPaths := map[string]string{
		testPrefix + "/db/password":     "super-secret-db-password",
		testPrefix + "/auth/jwt":        "jwt-secret-key-12345",
		testPrefix + "/auth/apikey":     "api-key-hash-abcdef",
		testPrefix + "/auth/session":    "session-key-xyz789",
		testPrefix + "/auth/refresh":    "refresh-token-qwerty",
		testPrefix + "/encrypt/master":  "master-encryption-key",
		testPrefix + "/encrypt/data":    "data-encryption-key",
		testPrefix + "/encrypt/signing": "signing-key-rsa256",
		testPrefix + "/encrypt/iv":      "initialization-vector-16",
	}

	for path, value := range secretPaths {
		storeProcessorTestSecret(t, endpoint, path, value)
	}

	// Cleanup after test
	defer func() {
		for path := range secretPaths {
			deleteProcessorTestSecret(t, endpoint, path)
		}
	}()

	// Create registry and processor
	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	// Create nested config with kms:// references
	cfg := &TestNestedConfig{
		Database: TestDatabaseConfig{
			Host:     "db.example.com",
			Port:     5432,
			Username: "dbuser",
			Password: fmt.Sprintf("kms://%s/db/password", testPrefix),
			SSLMode:  "require",
		},
		Auth: TestAuthConfig{
			JWTSecret:    fmt.Sprintf("kms://%s/auth/jwt", testPrefix),
			APIKeyHash:   fmt.Sprintf("kms://%s/auth/apikey", testPrefix),
			SessionKey:   fmt.Sprintf("kms://%s/auth/session", testPrefix),
			RefreshToken: fmt.Sprintf("kms://%s/auth/refresh", testPrefix),
		},
		Encryption: TestEncryptionConfig{
			MasterKey:    fmt.Sprintf("kms://%s/encrypt/master", testPrefix),
			DataKey:      fmt.Sprintf("kms://%s/encrypt/data", testPrefix),
			SigningKey:   fmt.Sprintf("kms://%s/encrypt/signing", testPrefix),
			EncryptionIV: fmt.Sprintf("kms://%s/encrypt/iv", testPrefix),
		},
	}

	// Process the configuration
	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors, "Should have no errors")
	assert.Empty(t, result.Unresolved, "Should have no unresolved fields")
	assert.Equal(t, 9, result.Resolved, "Should have 9 resolved secrets")

	// Verify resolved values contain expected secrets
	assertSecretContains(t, cfg.Database.Password, "super-secret-db-password")
	assertSecretContains(t, cfg.Auth.JWTSecret, "jwt-secret-key-12345")
	assertSecretContains(t, cfg.Auth.APIKeyHash, "api-key-hash-abcdef")
	assertSecretContains(t, cfg.Auth.SessionKey, "session-key-xyz789")
	assertSecretContains(t, cfg.Auth.RefreshToken, "refresh-token-qwerty")
	assertSecretContains(t, cfg.Encryption.MasterKey, "master-encryption-key")
	assertSecretContains(t, cfg.Encryption.DataKey, "data-encryption-key")
	assertSecretContains(t, cfg.Encryption.SigningKey, "signing-key-rsa256")
	assertSecretContains(t, cfg.Encryption.EncryptionIV, "initialization-vector-16")

	// Verify non-secret fields are unchanged
	assert.Equal(t, "db.example.com", cfg.Database.Host)
	assert.Equal(t, 5432, cfg.Database.Port)
	assert.Equal(t, "dbuser", cfg.Database.Username)
	assert.Equal(t, "require", cfg.Database.SSLMode)

	t.Logf("Successfully processed nested config with %d secrets resolved", result.Resolved)
}

// TestConfigProcessor_Slices tests processing config with slices containing secret references.
func TestConfigProcessor_Slices(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/slices/%d", time.Now().UnixNano())

	// Store test secrets for slice elements
	secretPaths := map[string]string{
		testPrefix + "/apikey/1":         "api-key-first",
		testPrefix + "/apikey/2":         "api-key-second",
		testPrefix + "/apikey/3":         "api-key-third",
		testPrefix + "/service/auth/key": "auth-service-key",
		testPrefix + "/service/auth/sec": "auth-service-secret",
		testPrefix + "/service/data/key": "data-service-key",
		testPrefix + "/service/data/sec": "data-service-secret",
		testPrefix + "/token/1":          "token-one",
		testPrefix + "/token/2":          "token-two",
	}

	for path, value := range secretPaths {
		storeProcessorTestSecret(t, endpoint, path, value)
	}

	defer func() {
		for path := range secretPaths {
			deleteProcessorTestSecret(t, endpoint, path)
		}
	}()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	cfg := &TestConfigWithSlices{
		APIKeys: []string{
			fmt.Sprintf("kms://%s/apikey/1", testPrefix),
			fmt.Sprintf("kms://%s/apikey/2", testPrefix),
			fmt.Sprintf("kms://%s/apikey/3", testPrefix),
		},
		Services: []TestServiceConfig{
			{
				Name:       "auth-service",
				Endpoint:   "https://auth.example.com",
				APIKey:     fmt.Sprintf("kms://%s/service/auth/key", testPrefix),
				SecretKey:  fmt.Sprintf("kms://%s/service/auth/sec", testPrefix),
				MaxRetries: 3,
			},
			{
				Name:       "data-service",
				Endpoint:   "https://data.example.com",
				APIKey:     fmt.Sprintf("kms://%s/service/data/key", testPrefix),
				SecretKey:  fmt.Sprintf("kms://%s/service/data/sec", testPrefix),
				MaxRetries: 5,
			},
		},
		Tokens: []string{
			fmt.Sprintf("kms://%s/token/1", testPrefix),
			"literal-token-value",
			fmt.Sprintf("kms://%s/token/2", testPrefix),
		},
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Unresolved)
	assert.Equal(t, 9, result.Resolved, "Should resolve 9 secrets (3 APIKeys + 4 Services + 2 Tokens)")

	// Verify slice elements resolved
	assertSecretContains(t, cfg.APIKeys[0], "api-key-first")
	assertSecretContains(t, cfg.APIKeys[1], "api-key-second")
	assertSecretContains(t, cfg.APIKeys[2], "api-key-third")

	// Verify struct slice elements
	assertSecretContains(t, cfg.Services[0].APIKey, "auth-service-key")
	assertSecretContains(t, cfg.Services[0].SecretKey, "auth-service-secret")
	assertSecretContains(t, cfg.Services[1].APIKey, "data-service-key")
	assertSecretContains(t, cfg.Services[1].SecretKey, "data-service-secret")

	// Verify mixed tokens slice
	assertSecretContains(t, cfg.Tokens[0], "token-one")
	assert.Equal(t, "literal-token-value", cfg.Tokens[1], "Literal should be unchanged")
	assertSecretContains(t, cfg.Tokens[2], "token-two")

	// Verify non-secret fields unchanged
	assert.Equal(t, "auth-service", cfg.Services[0].Name)
	assert.Equal(t, "https://auth.example.com", cfg.Services[0].Endpoint)
	assert.Equal(t, 3, cfg.Services[0].MaxRetries)

	t.Logf("Successfully processed slice config with %d secrets resolved", result.Resolved)
}

// TestConfigProcessor_Maps tests processing config with maps containing secret references.
func TestConfigProcessor_Maps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/maps/%d", time.Now().UnixNano())

	secretPaths := map[string]string{
		testPrefix + "/creds/admin":     "admin-password-secret",
		testPrefix + "/creds/user":      "user-password-secret",
		testPrefix + "/creds/readonly":  "readonly-password-secret",
		testPrefix + "/endpoint/api":    "api-endpoint-key",
		testPrefix + "/endpoint/api/s":  "api-endpoint-secret",
		testPrefix + "/endpoint/web":    "web-endpoint-key",
		testPrefix + "/endpoint/web/s":  "web-endpoint-secret",
		testPrefix + "/settings/secret": "settings-secret-value",
	}

	for path, value := range secretPaths {
		storeProcessorTestSecret(t, endpoint, path, value)
	}

	defer func() {
		for path := range secretPaths {
			deleteProcessorTestSecret(t, endpoint, path)
		}
	}()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	cfg := &TestConfigWithMaps{
		Credentials: map[string]string{
			"admin":    fmt.Sprintf("kms://%s/creds/admin", testPrefix),
			"user":     fmt.Sprintf("kms://%s/creds/user", testPrefix),
			"readonly": fmt.Sprintf("kms://%s/creds/readonly", testPrefix),
		},
		Endpoints: map[string]TestServiceConfig{
			"api": {
				Name:       "API Service",
				Endpoint:   "https://api.example.com",
				APIKey:     fmt.Sprintf("kms://%s/endpoint/api", testPrefix),
				SecretKey:  fmt.Sprintf("kms://%s/endpoint/api/s", testPrefix),
				MaxRetries: 3,
			},
			"web": {
				Name:       "Web Service",
				Endpoint:   "https://web.example.com",
				APIKey:     fmt.Sprintf("kms://%s/endpoint/web", testPrefix),
				SecretKey:  fmt.Sprintf("kms://%s/endpoint/web/s", testPrefix),
				MaxRetries: 5,
			},
		},
		Settings: map[string]string{
			"host":   "localhost",
			"port":   "8080",
			"secret": fmt.Sprintf("kms://%s/settings/secret", testPrefix),
		},
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Unresolved)
	assert.Equal(t, 8, result.Resolved, "Should resolve 8 secrets")

	// Verify credentials map
	assertSecretContains(t, cfg.Credentials["admin"], "admin-password-secret")
	assertSecretContains(t, cfg.Credentials["user"], "user-password-secret")
	assertSecretContains(t, cfg.Credentials["readonly"], "readonly-password-secret")

	// Verify endpoints map with struct values
	assertSecretContains(t, cfg.Endpoints["api"].APIKey, "api-endpoint-key")
	assertSecretContains(t, cfg.Endpoints["api"].SecretKey, "api-endpoint-secret")
	assertSecretContains(t, cfg.Endpoints["web"].APIKey, "web-endpoint-key")
	assertSecretContains(t, cfg.Endpoints["web"].SecretKey, "web-endpoint-secret")

	// Verify settings map with mixed values
	assert.Equal(t, "localhost", cfg.Settings["host"], "Literal should be unchanged")
	assert.Equal(t, "8080", cfg.Settings["port"], "Literal should be unchanged")
	assertSecretContains(t, cfg.Settings["secret"], "settings-secret-value")

	t.Logf("Successfully processed map config with %d secrets resolved", result.Resolved)
}

// TestConfigProcessor_MixedResolution tests config with both kms:// and auto placeholders.
func TestConfigProcessor_MixedResolution(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/mixed/%d", time.Now().UnixNano())

	secretPaths := map[string]string{
		testPrefix + "/db/password": "database-secret-password",
		testPrefix + "/api/key":     "api-secret-key-value",
	}

	for path, value := range secretPaths {
		storeProcessorTestSecret(t, endpoint, path, value)
	}

	defer func() {
		for path := range secretPaths {
			deleteProcessorTestSecret(t, endpoint, path)
		}
	}()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	cfg := &TestMixedConfig{
		Database: TestDatabaseConfig{
			Host:     "db.example.com",
			Port:     5432,
			Username: "dbuser",
			Password: fmt.Sprintf("kms://%s/db/password", testPrefix),
			SSLMode:  "require",
		},
		APIKey:  fmt.Sprintf("kms://%s/api/key", testPrefix),
		AutoKey: "auto", // Should trigger auto-generation
		Literal: "plain-text-value",
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Unresolved)
	assert.Equal(t, 3, result.Resolved, "Should resolve 3 secrets (2 KMS + 1 auto)")

	// Verify KMS-resolved secrets
	assertSecretContains(t, cfg.Database.Password, "database-secret-password")
	assertSecretContains(t, cfg.APIKey, "api-secret-key-value")

	// Verify auto-generated secret
	assert.NotEqual(t, "auto", cfg.AutoKey, "Auto placeholder should be replaced")
	assert.NotEmpty(t, cfg.AutoKey, "Auto-generated key should not be empty")
	assert.Greater(t, len(cfg.AutoKey), 20, "Auto-generated key should be substantial")

	// Verify literal is unchanged
	assert.Equal(t, "plain-text-value", cfg.Literal)

	t.Logf("Successfully processed mixed config with KMS and auto-gen resolution")
}

// TestConfigProcessor_Result tests that ProcessResult contains resolved paths and strategies.
func TestConfigProcessor_Result(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/result/%d", time.Now().UnixNano())

	secretPaths := map[string]string{
		testPrefix + "/secret1": "value-one",
		testPrefix + "/secret2": "value-two",
		testPrefix + "/secret3": "value-three",
	}

	for path, value := range secretPaths {
		storeProcessorTestSecret(t, endpoint, path, value)
	}

	defer func() {
		for path := range secretPaths {
			deleteProcessorTestSecret(t, endpoint, path)
		}
	}()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	type TestResultConfig struct {
		Secret1 string
		Secret2 string
		Secret3 string
		AutoGen string
		Literal string
	}

	cfg := &TestResultConfig{
		Secret1: fmt.Sprintf("kms://%s/secret1", testPrefix),
		Secret2: fmt.Sprintf("kms://%s/secret2", testPrefix),
		Secret3: fmt.Sprintf("kms://%s/secret3", testPrefix),
		AutoGen: "changeme",
		Literal: "not-a-secret",
	}

	// Store original auto-gen value for comparison
	originalAutoGen := cfg.AutoGen

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Unresolved)
	assert.Equal(t, 4, result.Resolved)

	// Verify KMS-resolved secrets contain the expected values
	assertSecretContains(t, cfg.Secret1, "value-one")
	assertSecretContains(t, cfg.Secret2, "value-two")
	assertSecretContains(t, cfg.Secret3, "value-three")

	// Verify auto-generated secret was replaced
	assert.NotEqual(t, originalAutoGen, cfg.AutoGen, "Auto-gen placeholder should be replaced")
	assert.NotEmpty(t, cfg.AutoGen, "Auto-generated value should not be empty")

	// Verify literal is not resolved
	assert.Equal(t, "not-a-secret", cfg.Literal)

	t.Logf("Successfully verified ProcessResult with %d resolved secrets", result.Resolved)
}

// TestConfigProcessor_PartialFailure tests partial resolution when some secrets exist and some don't.
func TestConfigProcessor_PartialFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/partial/%d", time.Now().UnixNano())

	// Only store some secrets - others will fail
	secretPaths := map[string]string{
		testPrefix + "/exists/1": "existing-secret-one",
		testPrefix + "/exists/2": "existing-secret-two",
	}

	for path, value := range secretPaths {
		storeProcessorTestSecret(t, endpoint, path, value)
	}

	defer func() {
		for path := range secretPaths {
			deleteProcessorTestSecret(t, endpoint, path)
		}
	}()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	type TestPartialConfig struct {
		Exists1  string
		Exists2  string
		Missing1 string
		Missing2 string
		AutoGen  string
		Literal  string
	}

	cfg := &TestPartialConfig{
		Exists1:  fmt.Sprintf("kms://%s/exists/1", testPrefix),
		Exists2:  fmt.Sprintf("kms://%s/exists/2", testPrefix),
		Missing1: fmt.Sprintf("kms://%s/missing/1", testPrefix),
		Missing2: fmt.Sprintf("kms://%s/missing/2", testPrefix),
		AutoGen:  "placeholder",
		Literal:  "not-modified",
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err, "Process should not return error even with partial failures")
	require.NotNil(t, result)

	// Should have 3 resolved (2 existing + 1 autogen)
	assert.Equal(t, 3, result.Resolved, "Should resolve 3 secrets")

	// Should have 2 errors (for missing secrets)
	assert.Len(t, result.Errors, 2, "Should have 2 errors for missing secrets")

	// Should have 2 unresolved
	assert.Len(t, result.Unresolved, 2, "Should have 2 unresolved fields")
	assert.Contains(t, result.Unresolved, "Missing1")
	assert.Contains(t, result.Unresolved, "Missing2")

	// Verify existing secrets were resolved
	assertSecretContains(t, cfg.Exists1, "existing-secret-one")
	assertSecretContains(t, cfg.Exists2, "existing-secret-two")

	// Verify missing secrets retain their original values
	assert.Contains(t, cfg.Missing1, "kms://", "Missing1 should retain kms:// URI")
	assert.Contains(t, cfg.Missing2, "kms://", "Missing2 should retain kms:// URI")

	// Verify autogen still worked
	assert.NotEqual(t, "placeholder", cfg.AutoGen)
	assert.NotEmpty(t, cfg.AutoGen)

	// Verify literal unchanged
	assert.Equal(t, "not-modified", cfg.Literal)

	t.Logf("Successfully handled partial failure: %d resolved, %d errors, %d unresolved",
		result.Resolved, len(result.Errors), len(result.Unresolved))
}

// TestConfigProcessor_NoSecrets tests processing config with no secret references (all literals).
func TestConfigProcessor_NoSecrets(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	type TestLiteralConfig struct {
		Host        string
		Port        int
		Username    string
		Password    string
		Description string
		Enabled     bool
		Tags        []string
		Settings    map[string]string
	}

	cfg := &TestLiteralConfig{
		Host:        "localhost",
		Port:        8080,
		Username:    "admin",
		Password:    "literal-password",
		Description: "A test configuration",
		Enabled:     true,
		Tags:        []string{"tag1", "tag2", "tag3"},
		Settings: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Unresolved)
	assert.Zero(t, result.Resolved, "Should have no resolved secrets")

	// Verify all values unchanged
	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "admin", cfg.Username)
	assert.Equal(t, "literal-password", cfg.Password)
	assert.Equal(t, "A test configuration", cfg.Description)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, []string{"tag1", "tag2", "tag3"}, cfg.Tags)
	assert.Equal(t, "value1", cfg.Settings["key1"])
	assert.Equal(t, "value2", cfg.Settings["key2"])

	t.Logf("Successfully processed config with no secrets (all literals)")
}

// TestConfigProcessor_RealConfig tests with a realistic config struct similar to quicbaas.yaml.
func TestConfigProcessor_RealConfig(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/real/%d", time.Now().UnixNano())

	// Store realistic secrets
	secretPaths := map[string]string{
		testPrefix + "/tls/key/password": "tls-key-password-secure",
		testPrefix + "/db/password":      "database-password-secret",
		testPrefix + "/kms/seal":         "kms-seal-key-value",
		testPrefix + "/kms/tpm/auth":     "tpm-auth-key",
		testPrefix + "/kms/tpm/sign":     "tpm-sign-key",
		testPrefix + "/kms/pkcs11/auth":  "pkcs11-auth-key",
		testPrefix + "/kms/pkcs11/sign":  "pkcs11-sign-key",
		testPrefix + "/kms/wrap/dek":     "dek-wrapping-key",
		testPrefix + "/kms/wrap/kek":     "kek-wrapping-key",
		testPrefix + "/logging/apikey":   "observability-api-key",
	}

	for path, value := range secretPaths {
		storeProcessorTestSecret(t, endpoint, path, value)
	}

	defer func() {
		for path := range secretPaths {
			deleteProcessorTestSecret(t, endpoint, path)
		}
	}()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	cfg := &TestRealConfig{
		Server: TestServerConfig{
			Host:           "0.0.0.0",
			Port:           8443,
			TLSCertPath:    "/etc/quicbaas/certs/server.crt",
			TLSKeyPath:     "/etc/quicbaas/certs/server.key",
			TLSKeyPassword: fmt.Sprintf("kms://%s/tls/key/password", testPrefix),
		},
		Database: TestDatabaseConfig{
			Host:     "db.internal.cluster",
			Port:     5432,
			Username: "quicbaas",
			Password: fmt.Sprintf("kms://%s/db/password", testPrefix),
			SSLMode:  "verify-full",
		},
		KMS: TestKMSConfig{
			Provider:    "local",
			MasterKeyID: "master-key-001",
			SealKey:     fmt.Sprintf("kms://%s/kms/seal", testPrefix),
			Backends: []TestBackendConfig{
				{
					Name:     "tpm2",
					Type:     "tpm",
					AuthKey:  fmt.Sprintf("kms://%s/kms/tpm/auth", testPrefix),
					SignKey:  fmt.Sprintf("kms://%s/kms/tpm/sign", testPrefix),
					Enabled:  true,
					Priority: 1,
				},
				{
					Name:     "pkcs11",
					Type:     "pkcs11",
					AuthKey:  fmt.Sprintf("kms://%s/kms/pkcs11/auth", testPrefix),
					SignKey:  fmt.Sprintf("kms://%s/kms/pkcs11/sign", testPrefix),
					Enabled:  true,
					Priority: 2,
				},
			},
			WrappingKeys: map[string]string{
				"dek": fmt.Sprintf("kms://%s/kms/wrap/dek", testPrefix),
				"kek": fmt.Sprintf("kms://%s/kms/wrap/kek", testPrefix),
			},
		},
		Logging: TestLoggingConfig{
			Level:   "info",
			Format:  "json",
			APIKey:  fmt.Sprintf("kms://%s/logging/apikey", testPrefix),
			Enabled: true,
		},
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors, "Should have no errors")
	assert.Empty(t, result.Unresolved, "Should have no unresolved fields")
	assert.Equal(t, 10, result.Resolved, "Should resolve 10 secrets")

	// Verify server secrets
	assertSecretContains(t, cfg.Server.TLSKeyPassword, "tls-key-password-secure")

	// Verify database secrets
	assertSecretContains(t, cfg.Database.Password, "database-password-secret")

	// Verify KMS secrets
	assertSecretContains(t, cfg.KMS.SealKey, "kms-seal-key-value")
	assertSecretContains(t, cfg.KMS.Backends[0].AuthKey, "tpm-auth-key")
	assertSecretContains(t, cfg.KMS.Backends[0].SignKey, "tpm-sign-key")
	assertSecretContains(t, cfg.KMS.Backends[1].AuthKey, "pkcs11-auth-key")
	assertSecretContains(t, cfg.KMS.Backends[1].SignKey, "pkcs11-sign-key")
	assertSecretContains(t, cfg.KMS.WrappingKeys["dek"], "dek-wrapping-key")
	assertSecretContains(t, cfg.KMS.WrappingKeys["kek"], "kek-wrapping-key")

	// Verify logging secrets
	assertSecretContains(t, cfg.Logging.APIKey, "observability-api-key")

	// Verify non-secret fields unchanged
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 8443, cfg.Server.Port)
	assert.Equal(t, "/etc/quicbaas/certs/server.crt", cfg.Server.TLSCertPath)
	assert.Equal(t, "db.internal.cluster", cfg.Database.Host)
	assert.Equal(t, "quicbaas", cfg.Database.Username)
	assert.Equal(t, "local", cfg.KMS.Provider)
	assert.Equal(t, "tpm2", cfg.KMS.Backends[0].Name)
	assert.True(t, cfg.KMS.Backends[0].Enabled)
	assert.Equal(t, "info", cfg.Logging.Level)
	assert.True(t, cfg.Logging.Enabled)

	t.Logf("Successfully processed realistic config with %d secrets resolved", result.Resolved)
}

// TestConfigProcessor_ConcurrentProcessing tests concurrent processing of multiple configs.
func TestConfigProcessor_ConcurrentProcessing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/concurrent/%d", time.Now().UnixNano())

	// Store a shared secret
	storeProcessorTestSecret(t, endpoint, testPrefix+"/shared/secret", "shared-secret-value")
	defer deleteProcessorTestSecret(t, endpoint, testPrefix+"/shared/secret")

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	type SimpleConfig struct {
		Secret  string
		AutoGen string
	}

	const numConfigs = 10
	results := make(chan struct {
		index  int
		result *secrets.ProcessResult
		err    error
		cfg    *SimpleConfig
	}, numConfigs)

	for i := 0; i < numConfigs; i++ {
		go func(index int) {
			cfg := &SimpleConfig{
				Secret:  fmt.Sprintf("kms://%s/shared/secret", testPrefix),
				AutoGen: "auto",
			}

			result, err := processor.Process(ctx, cfg)
			results <- struct {
				index  int
				result *secrets.ProcessResult
				err    error
				cfg    *SimpleConfig
			}{index, result, err, cfg}
		}(i)
	}

	autoGenValues := make(map[string]bool)
	for i := 0; i < numConfigs; i++ {
		r := <-results
		require.NoError(t, r.err, "Config %d should process successfully", r.index)
		require.NotNil(t, r.result)
		assert.Equal(t, 2, r.result.Resolved)
		assert.Empty(t, r.result.Errors)

		// Verify shared secret resolved consistently
		assertSecretContains(t, r.cfg.Secret, "shared-secret-value")

		// Verify auto-generated values are unique
		assert.NotEqual(t, "auto", r.cfg.AutoGen)
		assert.False(t, autoGenValues[r.cfg.AutoGen],
			"Auto-generated value should be unique across configs")
		autoGenValues[r.cfg.AutoGen] = true
	}

	assert.Len(t, autoGenValues, numConfigs, "All auto-generated values should be unique")

	t.Logf("Successfully processed %d configs concurrently with unique auto-generated values", numConfigs)
}

// TestConfigProcessor_ContextCancellation tests proper handling of context cancellation.
func TestConfigProcessor_ContextCancellation(t *testing.T) {
	endpoint := getRESTEndpoint()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	type SlowConfig struct {
		Secret1 string
		Secret2 string
		Secret3 string
	}

	t.Run("CancelledContext", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		cfg := &SlowConfig{
			Secret1: "kms://nonexistent/secret1",
			Secret2: "kms://nonexistent/secret2",
			Secret3: "kms://nonexistent/secret3",
		}

		result, err := processor.Process(ctx, cfg)

		// Process should still return, but with errors
		require.NoError(t, err, "Process should not return top-level error")
		require.NotNil(t, result)

		// Should have cancellation-related errors
		assert.NotEmpty(t, result.Errors, "Should have errors from cancelled context")

		t.Logf("Correctly handled cancelled context with %d errors", len(result.Errors))
	})

	t.Run("ExpiredContext", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		// Small delay to ensure context expires
		time.Sleep(1 * time.Millisecond)

		cfg := &SlowConfig{
			Secret1: "kms://nonexistent/secret1",
			Secret2: "kms://nonexistent/secret2",
			Secret3: "kms://nonexistent/secret3",
		}

		result, err := processor.Process(ctx, cfg)

		require.NoError(t, err, "Process should not return top-level error")
		require.NotNil(t, result)
		assert.NotEmpty(t, result.Errors, "Should have errors from expired context")

		t.Logf("Correctly handled expired context with %d errors", len(result.Errors))
	})
}

// TestConfigProcessor_EmptyCollections tests processing with empty slices and maps.
func TestConfigProcessor_EmptyCollections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	type EmptyCollectionsConfig struct {
		EmptySlice   []string
		EmptyMap     map[string]string
		NilSlice     []string
		NilMap       map[string]string
		EmptyStructs []TestServiceConfig
	}

	cfg := &EmptyCollectionsConfig{
		EmptySlice:   []string{},
		EmptyMap:     map[string]string{},
		NilSlice:     nil,
		NilMap:       nil,
		EmptyStructs: []TestServiceConfig{},
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Unresolved)
	assert.Zero(t, result.Resolved, "Should have no resolved secrets")

	t.Logf("Successfully processed config with empty collections")
}

// TestConfigProcessor_DeeplyNestedMaps tests processing deeply nested map structures.
func TestConfigProcessor_DeeplyNestedMaps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	endpoint := getRESTEndpoint()
	testPrefix := fmt.Sprintf("test/processor/deepmaps/%d", time.Now().UnixNano())

	storeProcessorTestSecret(t, endpoint, testPrefix+"/deep/secret", "deeply-nested-secret-value")
	defer deleteProcessorTestSecret(t, endpoint, testPrefix+"/deep/secret")

	registry := createProcessorTestRegistry(t, endpoint)
	processor := secrets.NewConfigProcessor(registry)

	type DeepMapConfig struct {
		Level1 map[string]map[string]map[string]string
	}

	cfg := &DeepMapConfig{
		Level1: map[string]map[string]map[string]string{
			"a": {
				"b": {
					"c":     fmt.Sprintf("kms://%s/deep/secret", testPrefix),
					"plain": "literal-value",
				},
			},
		},
	}

	result, err := processor.Process(ctx, cfg)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Errors)
	assert.Empty(t, result.Unresolved)
	assert.Equal(t, 1, result.Resolved)

	assertSecretContains(t, cfg.Level1["a"]["b"]["c"], "deeply-nested-secret-value")
	assert.Equal(t, "literal-value", cfg.Level1["a"]["b"]["plain"])

	t.Logf("Successfully processed deeply nested map structure")
}

// =============================================================================
// Helper Assertions
// =============================================================================

// assertSecretContains verifies that a resolved secret contains the expected value.
// It handles the case where secrets are stored as JSON with a "value" field.
func assertSecretContains(t *testing.T, resolved, expected string) {
	t.Helper()

	// Try to parse as JSON first (secrets may be stored as {"value": "..."})
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(resolved), &data); err == nil {
		if val, ok := data["value"].(string); ok {
			assert.Equal(t, expected, val, "Secret value should match expected")
			return
		}
	}

	// If not JSON, check if resolved contains expected
	assert.Contains(t, resolved, expected,
		"Resolved value %q should contain expected %q", resolved, expected)
}
