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
				assert.Equal(t, tc.expected, result)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, result)

			// The canonical name of a format parses back to itself.
			roundTrip, err := ParseOutputFormat(string(result))
			assert.NoError(t, err)
			assert.Equal(t, result, roundTrip)
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

// TestFetch_WriteAsFiles checks that each key is written as its own file,
// under the stripped secret path or directly in the output dir when
// FlattenKeys is set.
func TestFetch_WriteAsFiles(t *testing.T) {
	database := []SecretData{
		{Path: "apps/myapp/database", Data: map[string]string{"username": "admin", "password": "secret123"}},
	}

	tests := []struct {
		name        string
		secrets     []SecretData
		keys        []string
		stripPrefix bool
		flatten     bool
		wantSecrets int
		wantKeys    int
		want        map[string]string // path relative to the output dir -> content
		wantAbsent  []string
	}{
		{
			name:        "per-secret directory",
			secrets:     database,
			stripPrefix: true,
			wantSecrets: 1,
			wantKeys:    2,
			want: map[string]string{
				filepath.Join("database", "username"): "admin",
				filepath.Join("database", "password"): "secret123",
			},
		},
		{
			name:        "full path kept without strip prefix",
			secrets:     database,
			wantSecrets: 1,
			wantKeys:    2,
			want: map[string]string{
				filepath.Join("apps", "myapp", "database", "username"): "admin",
				filepath.Join("apps", "myapp", "database", "password"): "secret123",
			},
		},
		{
			name:        "flattened into output dir",
			secrets:     database,
			flatten:     true,
			wantSecrets: 1,
			wantKeys:    2,
			want:        map[string]string{"username": "admin", "password": "secret123"},
		},
		{
			name: "multiple secrets",
			secrets: []SecretData{
				{Path: "apps/myapp/database", Data: map[string]string{"host": "db.example.com"}},
				{Path: "apps/myapp/cache", Data: map[string]string{"host": "cache.example.com"}},
				{Path: "apps/myapp/api", Data: map[string]string{"key": "api-key-123"}},
			},
			stripPrefix: true,
			wantSecrets: 3,
			wantKeys:    3,
			want: map[string]string{
				filepath.Join("database", "host"): "db.example.com",
				filepath.Join("cache", "host"):    "cache.example.com",
				filepath.Join("api", "key"):       "api-key-123",
			},
		},
		{
			name: "key filter keeps only the listed keys",
			secrets: []SecretData{
				{Path: "apps/myapp/database", Data: map[string]string{
					"host": "localhost", "port": "5432", "username": "admin", "password": "secret123",
				}},
			},
			keys:        []string{"username", "password"},
			stripPrefix: true,
			wantSecrets: 1,
			wantKeys:    2,
			want: map[string]string{
				filepath.Join("database", "username"): "admin",
				filepath.Join("database", "password"): "secret123",
			},
			wantAbsent: []string{filepath.Join("database", "host"), filepath.Join("database", "port")},
		},
		{
			name:    "key filter matching nothing skips the secret",
			secrets: database,
			keys:    []string{"nonexistent"},
		},
		{
			name:        "secret with no data is counted but writes nothing",
			secrets:     []SecretData{{Path: "apps/myapp/empty", Data: map[string]string{}}},
			stripPrefix: true,
			wantSecrets: 1,
		},
		{
			name:    "no secrets writes nothing",
			secrets: []SecretData{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			config := FetchConfig{
				Prefix:      "apps/myapp/",
				OutputDir:   tmpDir,
				Format:      OutputFormatFile,
				CreateDirs:  true,
				FileMode:    0o600,
				DirMode:     0o700,
				Keys:        tc.keys,
				StripPrefix: tc.stripPrefix,
				FlattenKeys: tc.flatten,
			}

			fetcher, err := NewFetcher(&mockSecretsProvider{secrets: tc.secrets}, config)
			require.NoError(t, err)

			result, err := fetcher.Fetch(context.Background())
			require.NoError(t, err)

			assert.Equal(t, tc.wantSecrets, result.SecretsCount)
			assert.Equal(t, tc.wantKeys, result.KeysCount)
			assert.Len(t, result.FilesWritten, len(tc.want))

			for rel, wantContent := range tc.want {
				path := filepath.Join(tmpDir, rel)
				assert.Contains(t, result.FilesWritten, path)
				content, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, wantContent, string(content))
			}
			for _, rel := range tc.wantAbsent {
				_, err := os.Stat(filepath.Join(tmpDir, rel))
				assert.True(t, os.IsNotExist(err), rel)
			}
		})
	}
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

// TestFetch_WriteAsJSON checks the secrets.json layout: nested by the secret
// path (with or without the prefix), flat when FlattenKeys is set, and an
// empty object when there are no secrets.
func TestFetch_WriteAsJSON(t *testing.T) {
	database := []SecretData{
		{Path: "apps/myapp/database", Data: map[string]string{"host": "localhost", "password": "secret123"}},
	}

	tests := []struct {
		name        string
		secrets     []SecretData
		stripPrefix bool
		flatten     bool
		wantSecrets int
		wantKeys    int
		want        map[string]interface{}
	}{
		{
			name:        "nested under stripped path",
			secrets:     database,
			stripPrefix: true,
			wantSecrets: 1,
			wantKeys:    2,
			want: map[string]interface{}{
				"database": map[string]interface{}{"host": "localhost", "password": "secret123"},
			},
		},
		{
			name:        "full path kept without strip prefix",
			secrets:     database,
			wantSecrets: 1,
			wantKeys:    2,
			want: map[string]interface{}{
				"apps": map[string]interface{}{
					"myapp": map[string]interface{}{
						"database": map[string]interface{}{"host": "localhost", "password": "secret123"},
					},
				},
			},
		},
		{
			name:        "flattened keys at root",
			secrets:     database,
			flatten:     true,
			wantSecrets: 1,
			wantKeys:    2,
			want:        map[string]interface{}{"host": "localhost", "password": "secret123"},
		},
		{
			name: "deeply nested path",
			secrets: []SecretData{
				{Path: "apps/myapp/services/api/config", Data: map[string]string{"port": "8080"}},
			},
			stripPrefix: true,
			wantSecrets: 1,
			wantKeys:    1,
			want: map[string]interface{}{
				"services": map[string]interface{}{
					"api": map[string]interface{}{
						"config": map[string]interface{}{"port": "8080"},
					},
				},
			},
		},
		{
			name: "secret at the prefix itself lands at root",
			secrets: []SecretData{
				{Path: "apps/myapp/", Data: map[string]string{"global_key": "global_value"}},
			},
			stripPrefix: true,
			wantSecrets: 1,
			wantKeys:    1,
			want:        map[string]interface{}{"global_key": "global_value"},
		},
		{
			name:    "no secrets writes empty object",
			secrets: []SecretData{},
			want:    map[string]interface{}{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			config := FetchConfig{
				Prefix:      "apps/myapp/",
				OutputDir:   tmpDir,
				Format:      OutputFormatJSON,
				CreateDirs:  true,
				FileMode:    0o600,
				StripPrefix: tc.stripPrefix,
				FlattenKeys: tc.flatten,
			}

			fetcher, err := NewFetcher(&mockSecretsProvider{secrets: tc.secrets}, config)
			require.NoError(t, err)

			result, err := fetcher.Fetch(context.Background())
			require.NoError(t, err)

			assert.Equal(t, tc.wantSecrets, result.SecretsCount)
			assert.Equal(t, tc.wantKeys, result.KeysCount)

			jsonFile := filepath.Join(tmpDir, "secrets.json")
			assert.Equal(t, []string{jsonFile}, result.FilesWritten)

			content, err := os.ReadFile(jsonFile)
			require.NoError(t, err)

			var output map[string]interface{}
			require.NoError(t, json.Unmarshal(content, &output))
			assert.Equal(t, tc.want, output)
		})
	}
}

// TestFetch_Errors checks that a provider failure and a missing output dir
// with CreateDirs off both fail the fetch without a result.
func TestFetch_Errors(t *testing.T) {
	tests := []struct {
		name       string
		provider   *mockSecretsProvider
		createDirs bool
		subdir     string
	}{
		{
			name:       "provider error",
			provider:   &mockSecretsProvider{err: assert.AnError},
			createDirs: true,
		},
		{
			name: "output dir missing and CreateDirs off",
			provider: &mockSecretsProvider{secrets: []SecretData{
				{Path: "apps/myapp/database", Data: map[string]string{"host": "localhost"}},
			}},
			subdir: filepath.Join("nonexistent", "path"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			config := FetchConfig{
				Prefix:      "apps/myapp/",
				OutputDir:   filepath.Join(t.TempDir(), tc.subdir),
				Format:      OutputFormatFile,
				CreateDirs:  tc.createDirs,
				FileMode:    0o600,
				DirMode:     0o700,
				StripPrefix: true,
			}

			fetcher, err := NewFetcher(tc.provider, config)
			require.NoError(t, err)

			result, err := fetcher.Fetch(context.Background())
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
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
