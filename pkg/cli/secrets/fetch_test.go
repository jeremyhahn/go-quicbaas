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

package secrets

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSecretsProvider implements SecretsProvider for testing.
type mockSecretsProvider struct {
	secrets []SecretData
	err     error
}

func (m *mockSecretsProvider) GetSecretsByPrefix(ctx context.Context, prefix string) ([]SecretData, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.secrets, nil
}

// TestParseOutputFormat checks every accepted alias and that an unknown
// format is rejected with an empty result.
func TestParseOutputFormat(t *testing.T) {
	tests := []struct {
		input    string
		expected OutputFormat
		wantErr  bool
	}{
		{input: "file", expected: OutputFormatFile},
		{input: "files", expected: OutputFormatFile},
		{input: "FILE", expected: OutputFormatFile},
		{input: "env", expected: OutputFormatEnv},
		{input: ".env", expected: OutputFormatEnv},
		{input: "dotenv", expected: OutputFormatEnv},
		{input: "ENV", expected: OutputFormatEnv},
		{input: "json", expected: OutputFormatJSON},
		{input: "JSON", expected: OutputFormatJSON},
		{input: "xml", wantErr: true},
		{input: "yaml", wantErr: true},
		{input: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result, err := ParseOutputFormat(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestDefaultFetchConfig tests default configuration values.
func TestDefaultFetchConfig(t *testing.T) {
	cfg := DefaultFetchConfig()

	assert.Equal(t, "/secrets", cfg.OutputDir)
	assert.Equal(t, OutputFormatFile, cfg.Format)
	assert.True(t, cfg.CreateDirs)
	assert.Equal(t, os.FileMode(0o600), cfg.FileMode)
	assert.Equal(t, os.FileMode(0o700), cfg.DirMode)
	assert.False(t, cfg.FlattenKeys)
	assert.True(t, cfg.StripPrefix)
}

// TestNewFetcher checks that a complete config builds a fetcher and that each
// required field is rejected when missing.
func TestNewFetcher(t *testing.T) {
	tests := []struct {
		name      string
		provider  SecretsProvider
		prefix    string
		outputDir string
		wantErr   bool
	}{
		{name: "valid", provider: &mockSecretsProvider{}, prefix: "apps/myapp/", outputDir: "/tmp/secrets"},
		{name: "nil provider", provider: nil, prefix: "apps/myapp/", outputDir: "/tmp/secrets", wantErr: true},
		{name: "empty prefix", provider: &mockSecretsProvider{}, prefix: "", outputDir: "/tmp/secrets", wantErr: true},
		{name: "empty output dir", provider: &mockSecretsProvider{}, prefix: "apps/myapp/", outputDir: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := FetchConfig{Prefix: tc.prefix, OutputDir: tc.outputDir, Format: OutputFormatFile}
			fetcher, err := NewFetcher(tc.provider, config)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, fetcher)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, fetcher)
		})
	}
}

// TestFetch_WriteAsFiles tests fetching and writing secrets as individual files.
func TestFetch_WriteAsFiles(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"username": "admin",
					"password": "secret123",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600,
		DirMode:     0o700,
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 1, result.SecretsCount)
	assert.Equal(t, 2, result.KeysCount)
	assert.Len(t, result.FilesWritten, 2)

	// Verify files were created with correct content
	usernameContent, err := os.ReadFile(filepath.Join(tmpDir, "database", "username"))
	require.NoError(t, err)
	assert.Equal(t, "admin", string(usernameContent))

	passwordContent, err := os.ReadFile(filepath.Join(tmpDir, "database", "password"))
	require.NoError(t, err)
	assert.Equal(t, "secret123", string(passwordContent))
}

// TestFetch_WriteAsFiles_FlattenKeys tests writing with flattened keys.
func TestFetch_WriteAsFiles_FlattenKeys(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"username": "admin",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600,
		DirMode:     0o700,
		FlattenKeys: true, // Files go directly to output dir
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	// File should be directly in output dir, not in subdirectory
	usernameFile := filepath.Join(tmpDir, "username")
	assert.Contains(t, result.FilesWritten, usernameFile)

	content, err := os.ReadFile(usernameFile)
	require.NoError(t, err)
	assert.Equal(t, "admin", string(content))
}

// TestFetch_WriteAsEnv checks the .env output: the header is always written,
// keys become env var names, and each secret gets its own comment block.
func TestFetch_WriteAsEnv(t *testing.T) {
	tests := []struct {
		name         string
		secrets      []SecretData
		wantSecrets  int
		wantKeys     int
		wantContains []string
		wantCounts   map[string]int
	}{
		{
			name: "keys converted to env names",
			secrets: []SecretData{
				{Path: "apps/myapp/database", Data: map[string]string{"db-host": "localhost", "db.password": "secret123"}},
			},
			wantSecrets:  1,
			wantKeys:     2,
			wantContains: []string{"# Generated by quicbaas secrets fetch", "DB_HOST=localhost", "DB_PASSWORD=secret123"},
		},
		{
			name:         "no secrets writes header only",
			secrets:      []SecretData{},
			wantContains: []string{"# Generated by quicbaas secrets fetch"},
		},
		{
			name: "multiple secrets each get a comment",
			secrets: []SecretData{
				{Path: "apps/myapp/database", Data: map[string]string{"host": "db.example.com"}},
				{Path: "apps/myapp/cache", Data: map[string]string{"host": "cache.example.com"}},
			},
			wantSecrets:  2,
			wantKeys:     2,
			wantContains: []string{"# Secret: apps/myapp/database", "# Secret: apps/myapp/cache"},
			wantCounts:   map[string]int{"HOST=": 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			config := FetchConfig{
				Prefix:     "apps/myapp/",
				OutputDir:  tmpDir,
				Format:     OutputFormatEnv,
				CreateDirs: true,
				FileMode:   0o600,
			}

			fetcher, err := NewFetcher(&mockSecretsProvider{secrets: tc.secrets}, config)
			require.NoError(t, err)

			result, err := fetcher.Fetch(context.Background())
			require.NoError(t, err)

			assert.Equal(t, tc.wantSecrets, result.SecretsCount)
			assert.Equal(t, tc.wantKeys, result.KeysCount)

			envFile := filepath.Join(tmpDir, ".env")
			assert.Equal(t, []string{envFile}, result.FilesWritten)

			content, err := os.ReadFile(envFile)
			require.NoError(t, err)
			contentStr := string(content)
			for _, want := range tc.wantContains {
				assert.Contains(t, contentStr, want)
			}
			for substr, n := range tc.wantCounts {
				assert.Equal(t, n, strings.Count(contentStr, substr), substr)
			}
		})
	}
}

// TestFetch_WriteAsEnv_WithSpecialChars tests .env escaping.
func TestFetch_WriteAsEnv_WithSpecialChars(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/config",
				Data: map[string]string{
					"simple":       "value",
					"with_space":   "hello world",
					"with_quote":   `say "hello"`,
					"with_newline": "line1\nline2",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:     "apps/myapp/",
		OutputDir:  tmpDir,
		Format:     OutputFormatEnv,
		CreateDirs: true,
		FileMode:   0o600,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, ".env"))
	require.NoError(t, err)

	contentStr := string(content)

	// Simple values should not be quoted
	assert.Contains(t, contentStr, "SIMPLE=value")
	// Values with spaces should be quoted
	assert.Contains(t, contentStr, `WITH_SPACE="hello world"`)
	// Values with quotes should be escaped
	assert.Contains(t, contentStr, `WITH_QUOTE="say \"hello\""`)
	// Newlines should be escaped
	assert.Contains(t, contentStr, `WITH_NEWLINE="line1\nline2"`)

	assert.Equal(t, 4, result.KeysCount)
}

// TestFetch_WriteAsJSON tests writing secrets as a JSON file.
func TestFetch_WriteAsJSON(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host":     "localhost",
					"password": "secret123",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatJSON,
		CreateDirs:  true,
		FileMode:    0o600,
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 1, result.SecretsCount)
	assert.Equal(t, 2, result.KeysCount)
	assert.Len(t, result.FilesWritten, 1)

	// Verify JSON file was created
	jsonFile := filepath.Join(tmpDir, "secrets.json")
	assert.Contains(t, result.FilesWritten, jsonFile)

	content, err := os.ReadFile(jsonFile)
	require.NoError(t, err)

	// Parse JSON to verify structure
	var output map[string]interface{}
	err = json.Unmarshal(content, &output)
	require.NoError(t, err)

	// Should have nested structure: database -> {host, password}
	database, ok := output["database"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "localhost", database["host"])
	assert.Equal(t, "secret123", database["password"])
}

// TestFetch_WriteAsJSON_FlattenKeys tests JSON with flattened keys.
func TestFetch_WriteAsJSON_FlattenKeys(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host":     "localhost",
					"password": "secret123",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatJSON,
		CreateDirs:  true,
		FileMode:    0o600,
		FlattenKeys: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "secrets.json"))
	require.NoError(t, err)

	var output map[string]interface{}
	err = json.Unmarshal(content, &output)
	require.NoError(t, err)

	// Should be flat: {host, password} directly at root
	assert.Equal(t, "localhost", output["host"])
	assert.Equal(t, "secret123", output["password"])

	assert.Equal(t, 2, result.KeysCount)
}

// TestFetch_WriteAsJSON_NestedPaths tests JSON with deeply nested paths.
func TestFetch_WriteAsJSON_NestedPaths(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/services/api/config",
				Data: map[string]string{
					"port": "8080",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatJSON,
		CreateDirs:  true,
		FileMode:    0o600,
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "secrets.json"))
	require.NoError(t, err)

	var output map[string]interface{}
	err = json.Unmarshal(content, &output)
	require.NoError(t, err)

	// Should have nested structure: services -> api -> config -> {port}
	services := output["services"].(map[string]interface{})
	api := services["api"].(map[string]interface{})
	cfg := api["config"].(map[string]interface{})
	assert.Equal(t, "8080", cfg["port"])

	assert.Equal(t, 1, result.KeysCount)
}

// TestFetch_FilterKeys tests filtering specific keys.
func TestFetch_FilterKeys(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host":     "localhost",
					"port":     "5432",
					"username": "admin",
					"password": "secret123",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600,
		DirMode:     0o700,
		Keys:        []string{"username", "password"}, // Only these keys
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	// Should only have 2 keys, not 4
	assert.Equal(t, 2, result.KeysCount)

	// username and password should exist
	_, err = os.ReadFile(filepath.Join(tmpDir, "database", "username"))
	assert.NoError(t, err)
	_, err = os.ReadFile(filepath.Join(tmpDir, "database", "password"))
	assert.NoError(t, err)

	// host and port should NOT exist
	_, err = os.ReadFile(filepath.Join(tmpDir, "database", "host"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.ReadFile(filepath.Join(tmpDir, "database", "port"))
	assert.True(t, os.IsNotExist(err))
}

// TestFetch_FilterKeys_NoMatch tests filtering when no keys match.
func TestFetch_FilterKeys_NoMatch(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host": "localhost",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:     "apps/myapp/",
		OutputDir:  tmpDir,
		Format:     OutputFormatFile,
		CreateDirs: true,
		FileMode:   0o600,
		DirMode:    0o700,
		Keys:       []string{"nonexistent"},
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	// No keys should be written
	assert.Equal(t, 0, result.SecretsCount)
	assert.Equal(t, 0, result.KeysCount)
	assert.Empty(t, result.FilesWritten)
}

// TestFetch_EmptySecrets tests fetching when no secrets exist.
func TestFetch_EmptySecrets(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{}, // Empty
	}

	config := FetchConfig{
		Prefix:     "apps/myapp/",
		OutputDir:  tmpDir,
		Format:     OutputFormatFile,
		CreateDirs: true,
		FileMode:   0o600,
		DirMode:    0o700,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 0, result.SecretsCount)
	assert.Equal(t, 0, result.KeysCount)
	assert.Empty(t, result.FilesWritten)
}

// TestFetch_ProviderError tests handling provider errors.
func TestFetch_ProviderError(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		err: assert.AnError, // Simulated error
	}

	config := FetchConfig{
		Prefix:     "apps/myapp/",
		OutputDir:  tmpDir,
		Format:     OutputFormatFile,
		CreateDirs: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestFetch_MultipleSecrets tests fetching multiple secrets.
func TestFetch_MultipleSecrets(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host": "db.example.com",
				},
			},
			{
				Path: "apps/myapp/cache",
				Data: map[string]string{
					"host": "cache.example.com",
				},
			},
			{
				Path: "apps/myapp/api",
				Data: map[string]string{
					"key": "api-key-123",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600,
		DirMode:     0o700,
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 3, result.SecretsCount)
	assert.Equal(t, 3, result.KeysCount)
	assert.Len(t, result.FilesWritten, 3)

	// Verify each secret was written
	dbHost, _ := os.ReadFile(filepath.Join(tmpDir, "database", "host"))
	assert.Equal(t, "db.example.com", string(dbHost))

	cacheHost, _ := os.ReadFile(filepath.Join(tmpDir, "cache", "host"))
	assert.Equal(t, "cache.example.com", string(cacheHost))

	apiKey, _ := os.ReadFile(filepath.Join(tmpDir, "api", "key"))
	assert.Equal(t, "api-key-123", string(apiKey))
}

// TestFetch_NoStripPrefix tests preserving full path structure.
func TestFetch_NoStripPrefix(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host": "localhost",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600,
		DirMode:     0o700,
		StripPrefix: false, // Keep full path
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	// File should be at full path: apps/myapp/database/host
	expectedFile := filepath.Join(tmpDir, "apps", "myapp", "database", "host")
	assert.Contains(t, result.FilesWritten, expectedFile)

	content, err := os.ReadFile(expectedFile)
	require.NoError(t, err)
	assert.Equal(t, "localhost", string(content))
}

// TestToEnvVarName tests environment variable name conversion.
func TestToEnvVarName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "SIMPLE"},
		{"db-host", "DB_HOST"},
		{"db.password", "DB_PASSWORD"},
		{"api/key", "API_KEY"},
		{"mixed-style.key/path", "MIXED_STYLE_KEY_PATH"},
		{"ALREADY_UPPER", "ALREADY_UPPER"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := toEnvVarName(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestEscapeEnvValue tests .env value escaping.
func TestEscapeEnvValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple", "value", "value"},
		{"with_space", "hello world", `"hello world"`},
		{"with_tab", "hello\tworld", "\"hello\tworld\""}, // Tabs quoted but not escaped
		{"with_newline", "line1\nline2", `"line1\nline2"`},
		{"with_quote", `say "hello"`, `"say \"hello\""`},
		{"with_dollar", "$VAR", `"$VAR"`},
		{"with_backtick", "`cmd`", "\"`cmd`\""},
		{"with_backslash", `path\to\file`, `"path\\to\\file"`},
		{"empty", "", ""},
		{"numeric", "12345", "12345"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := escapeEnvValue(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// TestFetch_WriteAsJSON_EmptySecrets tests JSON with no secrets.
func TestFetch_WriteAsJSON_EmptySecrets(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{},
	}

	config := FetchConfig{
		Prefix:     "apps/myapp/",
		OutputDir:  tmpDir,
		Format:     OutputFormatJSON,
		CreateDirs: true,
		FileMode:   0o600,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	jsonFile := filepath.Join(tmpDir, "secrets.json")
	assert.Contains(t, result.FilesWritten, jsonFile)

	content, err := os.ReadFile(jsonFile)
	require.NoError(t, err)

	// Should be empty JSON object
	var output map[string]interface{}
	err = json.Unmarshal(content, &output)
	require.NoError(t, err)
	assert.Empty(t, output)
}

// TestFetch_CreateDirs_Disabled tests behavior when CreateDirs is false.
func TestFetch_CreateDirs_Disabled(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host": "localhost",
				},
			},
		},
	}

	// Create a non-existent subdirectory path
	nonExistentDir := filepath.Join(tmpDir, "nonexistent", "path")

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   nonExistentDir,
		Format:      OutputFormatFile,
		CreateDirs:  false, // Don't create dirs
		FileMode:    0o600,
		DirMode:     0o700,
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	// Should fail because directory doesn't exist
	result, err := fetcher.Fetch(context.Background())
	assert.Error(t, err)
	assert.Nil(t, result)
}

// TestFetch_FilePermissions tests that files are created with correct permissions.
func TestFetch_FilePermissions(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/secrets",
				Data: map[string]string{
					"password": "secret123",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600, // Read/write for owner only
		DirMode:     0o700,
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	_, err = fetcher.Fetch(context.Background())
	require.NoError(t, err)

	// Check file permissions
	file := filepath.Join(tmpDir, "secrets", "password")
	info, err := os.Stat(file)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestFetch_JSON_PathStripPrefix tests JSON output with different prefix handling.
func TestFetch_JSON_PathStripPrefix(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/database",
				Data: map[string]string{
					"host": "localhost",
				},
			},
		},
	}

	// Test with StripPrefix=false (keep full path)
	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatJSON,
		CreateDirs:  true,
		FileMode:    0o600,
		StripPrefix: false,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	_, err = fetcher.Fetch(context.Background())
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "secrets.json"))
	require.NoError(t, err)

	var output map[string]interface{}
	err = json.Unmarshal(content, &output)
	require.NoError(t, err)

	// Full path should be preserved: apps -> myapp -> database -> {host}
	apps := output["apps"].(map[string]interface{})
	myapp := apps["myapp"].(map[string]interface{})
	database := myapp["database"].(map[string]interface{})
	assert.Equal(t, "localhost", database["host"])
}

// TestFetch_MultipleSecretsWithEmptyPath tests secrets at root level.
func TestFetch_MultipleSecretsWithEmptyPath(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/", // Prefix matches exactly
				Data: map[string]string{
					"global_key": "global_value",
				},
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatJSON,
		CreateDirs:  true,
		FileMode:    0o600,
		StripPrefix: true, // After strip, path becomes empty
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(tmpDir, "secrets.json"))
	require.NoError(t, err)

	var output map[string]interface{}
	err = json.Unmarshal(content, &output)
	require.NoError(t, err)

	// Empty path should put keys directly at root
	assert.Equal(t, "global_value", output["global_key"])
	assert.Equal(t, 1, result.KeysCount)
}

// TestOutputFormat_String tests OutputFormat string representation.
func TestOutputFormat_String(t *testing.T) {
	assert.Equal(t, "file", string(OutputFormatFile))
	assert.Equal(t, "env", string(OutputFormatEnv))
	assert.Equal(t, "json", string(OutputFormatJSON))
}

// TestSecretData_Empty tests SecretData with empty data map.
func TestSecretData_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	provider := &mockSecretsProvider{
		secrets: []SecretData{
			{
				Path: "apps/myapp/empty",
				Data: map[string]string{}, // Empty data
			},
		},
	}

	config := FetchConfig{
		Prefix:      "apps/myapp/",
		OutputDir:   tmpDir,
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600,
		DirMode:     0o700,
		StripPrefix: true,
	}

	fetcher, err := NewFetcher(provider, config)
	require.NoError(t, err)

	result, err := fetcher.Fetch(context.Background())
	require.NoError(t, err)

	// No files should be written for empty data
	assert.Equal(t, 1, result.SecretsCount)
	assert.Equal(t, 0, result.KeysCount)
	assert.Empty(t, result.FilesWritten)
}
