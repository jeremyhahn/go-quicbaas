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

// Package secrets provides CLI functionality for secrets management.
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// OutputFormat represents the output format for fetched secrets.
type OutputFormat string

const (
	// OutputFormatFile writes one file per secret key.
	OutputFormatFile OutputFormat = "file"

	// OutputFormatEnv writes a single .env file.
	OutputFormatEnv OutputFormat = "env"

	// OutputFormatJSON writes a single JSON file.
	OutputFormatJSON OutputFormat = "json"
)

// ParseOutputFormat parses a string into an OutputFormat.
func ParseOutputFormat(s string) (OutputFormat, error) {
	switch strings.ToLower(s) {
	case "file", "files":
		return OutputFormatFile, nil
	case "env", ".env", "dotenv":
		return OutputFormatEnv, nil
	case "json":
		return OutputFormatJSON, nil
	default:
		return "", dragonerrors.InvalidInputf("secrets.ParseOutputFormat", "unknown output format: %s (valid: file, env, json)", s)
	}
}

// SecretData represents a secret with its key-value data.
type SecretData struct {
	// Path is the secret path (e.g., "apps/myapp/database")
	Path string

	// Data contains the key-value pairs of the secret
	Data map[string]string
}

// SecretsProvider defines the interface for fetching secrets.
type SecretsProvider interface {
	// GetSecretsByPrefix fetches all secrets matching a prefix.
	GetSecretsByPrefix(ctx context.Context, prefix string) ([]SecretData, error)
}

// FetchConfig holds configuration for the fetch operation.
type FetchConfig struct {
	// Prefix is the secret path prefix to fetch.
	Prefix string

	// OutputDir is the directory to write secrets to.
	OutputDir string

	// Format is the output format.
	Format OutputFormat

	// CreateDirs creates the output directory if it doesn't exist.
	CreateDirs bool

	// FileMode is the permission mode for created files.
	FileMode os.FileMode

	// DirMode is the permission mode for created directories.
	DirMode os.FileMode

	// FlattenKeys if true, flattens nested paths into single-level keys.
	// e.g., "apps/myapp/database.password" becomes "database.password"
	FlattenKeys bool

	// StripPrefix removes the prefix from secret paths when creating filenames.
	StripPrefix bool

	// Keys filters to only fetch specific keys (nil means all keys).
	Keys []string
}

// DefaultFetchConfig returns a FetchConfig with sensible defaults.
func DefaultFetchConfig() FetchConfig {
	return FetchConfig{
		OutputDir:   "/secrets",
		Format:      OutputFormatFile,
		CreateDirs:  true,
		FileMode:    0o600, // Read/write for owner only
		DirMode:     0o700, // Read/write/execute for owner only
		FlattenKeys: false,
		StripPrefix: true,
	}
}

// FetchResult contains the result of a fetch operation.
type FetchResult struct {
	// SecretsCount is the number of secrets fetched.
	SecretsCount int

	// KeysCount is the total number of keys written.
	KeysCount int

	// FilesWritten is the list of files created.
	FilesWritten []string
}

// Fetcher fetches secrets and writes them to the filesystem.
type Fetcher struct {
	provider SecretsProvider
	config   FetchConfig
}

// NewFetcher creates a new secrets fetcher.
func NewFetcher(provider SecretsProvider, config FetchConfig) (*Fetcher, error) {
	if provider == nil {
		return nil, dragonerrors.InvalidInput("secrets.NewFetcher", "provider is required")
	}

	if config.Prefix == "" {
		return nil, dragonerrors.InvalidInput("secrets.NewFetcher", "prefix is required")
	}

	if config.OutputDir == "" {
		return nil, dragonerrors.InvalidInput("secrets.NewFetcher", "output directory is required")
	}

	return &Fetcher{
		provider: provider,
		config:   config,
	}, nil
}

// Fetch fetches secrets and writes them to the configured output directory.
func (f *Fetcher) Fetch(ctx context.Context) (*FetchResult, error) {
	// Create output directory if needed
	if f.config.CreateDirs {
		if err := os.MkdirAll(f.config.OutputDir, f.config.DirMode); err != nil {
			return nil, dragonerrors.Wrapf(err, dragonerrors.ErrInternal, "secrets.Fetch", "failed to create output directory")
		}
	}

	// Fetch secrets from provider
	secrets, err := f.provider.GetSecretsByPrefix(ctx, f.config.Prefix)
	if err != nil {
		return nil, dragonerrors.Wrapf(err, dragonerrors.ErrInternal, "secrets.Fetch", "failed to fetch secrets")
	}

	// Filter keys if specified
	if len(f.config.Keys) > 0 {
		secrets = f.filterKeys(secrets)
	}

	// Write secrets based on format
	var result *FetchResult
	switch f.config.Format {
	case OutputFormatFile:
		result, err = f.writeAsFiles(secrets)
	case OutputFormatEnv:
		result, err = f.writeAsEnv(secrets)
	case OutputFormatJSON:
		result, err = f.writeAsJSON(secrets)
	default:
		return nil, dragonerrors.InvalidInputf("secrets.Fetch", "unknown output format: %s", f.config.Format)
	}

	if err != nil {
		return nil, err
	}

	result.SecretsCount = len(secrets)
	return result, nil
}

// filterKeys filters secrets to only include specified keys.
func (f *Fetcher) filterKeys(secrets []SecretData) []SecretData {
	keySet := make(map[string]bool)
	for _, k := range f.config.Keys {
		keySet[k] = true
	}

	filtered := make([]SecretData, 0, len(secrets))
	for _, secret := range secrets {
		filteredData := make(map[string]string)
		for k, v := range secret.Data {
			if keySet[k] {
				filteredData[k] = v
			}
		}
		if len(filteredData) > 0 {
			filtered = append(filtered, SecretData{
				Path: secret.Path,
				Data: filteredData,
			})
		}
	}
	return filtered
}

// writeAsFiles writes each secret key as a separate file.
func (f *Fetcher) writeAsFiles(secrets []SecretData) (*FetchResult, error) {
	result := &FetchResult{
		FilesWritten: make([]string, 0),
	}

	for _, secret := range secrets {
		// Determine the base path for this secret's files
		basePath := f.config.OutputDir
		if !f.config.FlattenKeys {
			// Create subdirectory structure based on secret path
			relativePath := secret.Path
			if f.config.StripPrefix && strings.HasPrefix(relativePath, f.config.Prefix) {
				relativePath = strings.TrimPrefix(relativePath, f.config.Prefix)
				relativePath = strings.TrimPrefix(relativePath, "/")
			}

			if relativePath != "" {
				basePath = filepath.Join(f.config.OutputDir, relativePath)
				if f.config.CreateDirs {
					if err := os.MkdirAll(basePath, f.config.DirMode); err != nil {
						return nil, dragonerrors.Wrapf(err, dragonerrors.ErrInternal, "secrets.writeAsFiles", "failed to create directory %s", basePath)
					}
				}
			}
		}

		// Write each key-value pair as a file
		for key, value := range secret.Data {
			filename := filepath.Join(basePath, key)
			if err := os.WriteFile(filename, []byte(value), f.config.FileMode); err != nil {
				return nil, dragonerrors.Wrapf(err, dragonerrors.ErrInternal, "secrets.writeAsFiles", "failed to write file %s", filename)
			}
			result.FilesWritten = append(result.FilesWritten, filename)
			result.KeysCount++
		}
	}

	return result, nil
}

// writeAsEnv writes all secrets as a single .env file.
func (f *Fetcher) writeAsEnv(secrets []SecretData) (*FetchResult, error) {
	result := &FetchResult{
		FilesWritten: make([]string, 0, 1),
	}

	var builder strings.Builder
	builder.WriteString("# Generated by quicbaas secrets fetch\n")
	builder.WriteString("# DO NOT EDIT - This file is managed automatically\n\n")

	for _, secret := range secrets {
		// Add comment with secret path
		builder.WriteString(fmt.Sprintf("# Secret: %s\n", secret.Path))

		for key, value := range secret.Data {
			// Convert key to env var format (uppercase, underscores)
			envKey := toEnvVarName(key)

			// Escape value if needed
			escapedValue := escapeEnvValue(value)

			builder.WriteString(fmt.Sprintf("%s=%s\n", envKey, escapedValue))
			result.KeysCount++
		}
		builder.WriteString("\n")
	}

	// Write the .env file
	filename := filepath.Join(f.config.OutputDir, ".env")
	if err := os.WriteFile(filename, []byte(builder.String()), f.config.FileMode); err != nil {
		return nil, dragonerrors.Wrapf(err, dragonerrors.ErrInternal, "secrets.writeAsEnv", "failed to write .env file")
	}
	result.FilesWritten = append(result.FilesWritten, filename)

	return result, nil
}

// writeAsJSON writes all secrets as a single JSON file.
func (f *Fetcher) writeAsJSON(secrets []SecretData) (*FetchResult, error) {
	result := &FetchResult{
		FilesWritten: make([]string, 0, 1),
	}

	// Build nested JSON structure
	output := make(map[string]interface{})

	for _, secret := range secrets {
		// Determine the key path
		keyPath := secret.Path
		if f.config.StripPrefix && strings.HasPrefix(keyPath, f.config.Prefix) {
			keyPath = strings.TrimPrefix(keyPath, f.config.Prefix)
			keyPath = strings.TrimPrefix(keyPath, "/")
		}

		if f.config.FlattenKeys || keyPath == "" {
			// Merge directly into root
			for k, v := range secret.Data {
				output[k] = v
				result.KeysCount++
			}
		} else {
			// Create nested structure
			parts := strings.Split(keyPath, "/")
			current := output
			for i, part := range parts {
				if i == len(parts)-1 {
					// Last part - set the data
					current[part] = secret.Data
					result.KeysCount += len(secret.Data)
				} else {
					// Intermediate part - ensure map exists
					if _, exists := current[part]; !exists {
						current[part] = make(map[string]interface{})
					}
					nested, ok := current[part].(map[string]interface{})
					if !ok {
						return nil, dragonerrors.Internalf("secrets.writeAsJSON", "path conflict: %s is not a map", part)
					}
					current = nested
				}
			}
		}
	}

	// Marshal to JSON with indentation
	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return nil, dragonerrors.Wrapf(err, dragonerrors.ErrInternal, "secrets.writeAsJSON", "failed to marshal JSON")
	}

	// Write the JSON file
	filename := filepath.Join(f.config.OutputDir, "secrets.json")
	if err := os.WriteFile(filename, jsonData, f.config.FileMode); err != nil {
		return nil, dragonerrors.Wrapf(err, dragonerrors.ErrInternal, "secrets.writeAsJSON", "failed to write JSON file")
	}
	result.FilesWritten = append(result.FilesWritten, filename)

	return result, nil
}

// toEnvVarName converts a key to a valid environment variable name.
func toEnvVarName(key string) string {
	// Replace dots and dashes with underscores
	result := strings.ReplaceAll(key, ".", "_")
	result = strings.ReplaceAll(result, "-", "_")
	result = strings.ReplaceAll(result, "/", "_")

	// Convert to uppercase
	return strings.ToUpper(result)
}

// escapeEnvValue escapes a value for use in .env file.
func escapeEnvValue(value string) string {
	// If value contains special characters, quote it
	needsQuotes := strings.ContainsAny(value, " \t\n\"'$`\\")

	if needsQuotes {
		// Escape backslashes and double quotes
		escaped := strings.ReplaceAll(value, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
		escaped = strings.ReplaceAll(escaped, "\n", "\\n")
		return fmt.Sprintf("\"%s\"", escaped)
	}

	return value
}
