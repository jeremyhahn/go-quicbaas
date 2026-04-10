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

// Package main provides the QuicBaaS command-line interface.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// BackupStorageFlags contains CLI flags for storage configuration.
type BackupStorageFlags struct {
	StorageType string
	Bucket      string
	Region      string
	Endpoint    string
	Container   string
}

// BackupStorageBuilder builds storage configuration from CLI flags and credentials.
type BackupStorageBuilder struct {
	flags           *BackupStorageFlags
	credentialsFile string
	profile         string
}

// NewBackupStorageBuilder creates a new BackupStorageBuilder.
func NewBackupStorageBuilder(flags *BackupStorageFlags, credentialsFile, profile string) *BackupStorageBuilder {
	return &BackupStorageBuilder{
		flags:           flags,
		credentialsFile: credentialsFile,
		profile:         profile,
	}
}

// Build builds the storage configuration by resolving credentials from multiple sources.
// Priority for credential resolution:
// 1. CLI flags (highest priority)
// 2. Environment variables (AWS_*, AZURE_*, GOOGLE_*)
// 3. Credentials file (~/.quicbaas/backup-credentials.yaml)
// 4. Cloud provider defaults (AWS chain, Azure DefaultCredential, GCP ADC).
func (b *BackupStorageBuilder) Build(ctx context.Context) (string, map[string]interface{}, error) {
	storageType := b.flags.StorageType
	if storageType == "" {
		storageType = "local"
	}

	// Validate storage type
	if err := b.validateStorageType(storageType); err != nil {
		return "", nil, err
	}

	// Start with base configuration from flags
	config := make(map[string]interface{})

	// Load credentials from file if specified
	var profileCreds *ProfileCredentials
	if b.credentialsFile != "" || b.profile != "" {
		var err error
		profileCreds, err = b.loadProfileCredentials(ctx)
		if err != nil {
			return "", nil, &BackupStorageError{
				Op:      "build",
				Message: "failed to load credentials from file",
				Cause:   err,
			}
		}
	}

	// Build configuration based on storage type
	var finalConfig map[string]interface{}
	var err error

	switch storageType {
	case "local":
		finalConfig, err = b.buildLocalConfig(config, profileCreds)
	case "s3":
		finalConfig, err = b.buildS3Config(config, profileCreds)
	case "azure":
		finalConfig, err = b.buildAzureConfig(config, profileCreds)
	case "gcs":
		finalConfig, err = b.buildGCSConfig(config, profileCreds)
	case "minio":
		finalConfig, err = b.buildMinIOConfig(config, profileCreds)
	default:
		return "", nil, &BackupStorageError{
			Op:      "build",
			Message: fmt.Sprintf("unsupported storage type: %s", storageType),
		}
	}

	if err != nil {
		return "", nil, err
	}

	return storageType, finalConfig, nil
}

// validateStorageType validates the storage type.
func (b *BackupStorageBuilder) validateStorageType(storageType string) error {
	validTypes := map[string]bool{
		"local": true,
		"s3":    true,
		"azure": true,
		"gcs":   true,
		"minio": true,
	}

	if !validTypes[storageType] {
		return &BackupStorageError{
			Op:      "validate",
			Message: fmt.Sprintf("invalid storage type: %s (valid: local, s3, azure, gcs, minio)", storageType),
		}
	}

	return nil
}

// buildLocalConfig builds configuration for local filesystem storage.
func (b *BackupStorageBuilder) buildLocalConfig(config map[string]interface{}, profileCreds *ProfileCredentials) (map[string]interface{}, error) {
	// For local storage, we use the storage path (base_path in go-objstore)
	basePath := backupStoragePath
	if basePath == "" && profileCreds != nil && profileCreds.Config != nil {
		if path, ok := profileCreds.Config["base_path"].(string); ok {
			basePath = path
		}
	}
	if basePath == "" {
		basePath = "/var/lib/quicbaas/backups"
	}

	config["base_path"] = basePath
	return config, nil
}

// buildS3Config builds configuration for S3 storage.
func (b *BackupStorageBuilder) buildS3Config(config map[string]interface{}, profileCreds *ProfileCredentials) (map[string]interface{}, error) {
	// Priority: CLI flags > Environment variables > Profile credentials > Defaults

	// Bucket
	bucket := b.flags.Bucket
	if bucket == "" {
		bucket = os.Getenv("AWS_BUCKET")
	}
	if bucket == "" && profileCreds != nil {
		bucket = profileCreds.Bucket
	}
	if bucket == "" {
		return nil, &BackupStorageError{
			Op:      "build_s3",
			Message: "bucket name is required for S3 storage",
		}
	}
	config["bucket"] = bucket

	// Region
	region := b.flags.Region
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" && profileCreds != nil {
		region = profileCreds.Region
	}
	if region == "" {
		region = "us-east-1" // Default AWS region
	}
	config["region"] = region

	// Endpoint (optional for custom S3-compatible endpoints)
	endpoint := b.flags.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("AWS_ENDPOINT")
	}
	if endpoint == "" && profileCreds != nil && profileCreds.Config != nil {
		if ep, ok := profileCreds.Config["endpoint"].(string); ok {
			endpoint = ep
		}
	}
	if endpoint != "" {
		config["endpoint"] = endpoint
	}

	// Credentials
	accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
	secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	sessionToken := os.Getenv("AWS_SESSION_TOKEN")

	// Override with profile credentials if available
	if profileCreds != nil && profileCreds.Credentials != nil {
		if ak, ok := profileCreds.Credentials["access_key"].(string); ok && ak != "" {
			accessKey = b.expandEnvVars(ak)
		}
		if sk, ok := profileCreds.Credentials["secret_key"].(string); ok && sk != "" {
			secretKey = b.expandEnvVars(sk)
		}
		if st, ok := profileCreds.Credentials["session_token"].(string); ok && st != "" {
			sessionToken = b.expandEnvVars(st)
		}
	}

	// Add credentials if provided
	if accessKey != "" {
		config["access_key_id"] = accessKey
	}
	if secretKey != "" {
		config["secret_access_key"] = secretKey
	}
	if sessionToken != "" {
		config["session_token"] = sessionToken
	}

	return config, nil
}

// buildAzureConfig builds configuration for Azure Blob Storage.
func (b *BackupStorageBuilder) buildAzureConfig(config map[string]interface{}, profileCreds *ProfileCredentials) (map[string]interface{}, error) {
	// Container
	container := b.flags.Container
	if container == "" {
		container = os.Getenv("AZURE_STORAGE_CONTAINER")
	}
	if container == "" && profileCreds != nil {
		container = profileCreds.Container
	}
	if container == "" {
		return nil, &BackupStorageError{
			Op:      "build_azure",
			Message: "container name is required for Azure storage",
		}
	}
	config["container"] = container

	// Credentials
	accountName := os.Getenv("AZURE_STORAGE_ACCOUNT")
	accountKey := os.Getenv("AZURE_STORAGE_KEY")
	connectionString := os.Getenv("AZURE_STORAGE_CONNECTION_STRING")

	// Override with profile credentials if available
	if profileCreds != nil && profileCreds.Credentials != nil {
		if an, ok := profileCreds.Credentials["account_name"].(string); ok && an != "" {
			accountName = b.expandEnvVars(an)
		}
		if ak, ok := profileCreds.Credentials["account_key"].(string); ok && ak != "" {
			accountKey = b.expandEnvVars(ak)
		}
		if cs, ok := profileCreds.Credentials["connection_string"].(string); ok && cs != "" {
			connectionString = b.expandEnvVars(cs)
		}
	}

	// Add credentials if provided
	if connectionString != "" {
		config["connection_string"] = connectionString
	} else {
		if accountName != "" {
			config["account_name"] = accountName
		}
		if accountKey != "" {
			config["account_key"] = accountKey
		}
	}

	return config, nil
}

// buildGCSConfig builds configuration for Google Cloud Storage.
func (b *BackupStorageBuilder) buildGCSConfig(config map[string]interface{}, profileCreds *ProfileCredentials) (map[string]interface{}, error) {
	// Bucket
	bucket := b.flags.Bucket
	if bucket == "" {
		bucket = os.Getenv("GCS_BUCKET")
	}
	if bucket == "" {
		bucket = os.Getenv("GOOGLE_CLOUD_BUCKET")
	}
	if bucket == "" && profileCreds != nil {
		bucket = profileCreds.Bucket
	}
	if bucket == "" {
		return nil, &BackupStorageError{
			Op:      "build_gcs",
			Message: "bucket name is required for GCS storage",
		}
	}
	config["bucket"] = bucket

	// Project ID
	projectID := os.Getenv("GCS_PROJECT_ID")
	if projectID == "" {
		projectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}
	if projectID == "" && profileCreds != nil && profileCreds.Config != nil {
		if pid, ok := profileCreds.Config["project_id"].(string); ok {
			projectID = pid
		}
	}
	if projectID != "" {
		config["project_id"] = projectID
	}

	// Credentials file path
	credPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS")
	if credPath == "" && profileCreds != nil && profileCreds.Credentials != nil {
		if cp, ok := profileCreds.Credentials["credentials_file"].(string); ok && cp != "" {
			credPath = b.expandEnvVars(cp)
		}
	}
	if credPath != "" {
		config["credentials_file"] = credPath
	}

	return config, nil
}

// buildMinIOConfig builds configuration for MinIO storage.
func (b *BackupStorageBuilder) buildMinIOConfig(config map[string]interface{}, profileCreds *ProfileCredentials) (map[string]interface{}, error) {
	// Bucket
	bucket := b.flags.Bucket
	if bucket == "" {
		bucket = os.Getenv("MINIO_BUCKET")
	}
	if bucket == "" && profileCreds != nil {
		bucket = profileCreds.Bucket
	}
	if bucket == "" {
		return nil, &BackupStorageError{
			Op:      "build_minio",
			Message: "bucket name is required for MinIO storage",
		}
	}
	config["bucket"] = bucket

	// Endpoint
	endpoint := b.flags.Endpoint
	if endpoint == "" {
		endpoint = os.Getenv("MINIO_ENDPOINT")
	}
	if endpoint == "" && profileCreds != nil && profileCreds.Config != nil {
		if ep, ok := profileCreds.Config["endpoint"].(string); ok {
			endpoint = ep
		}
	}
	if endpoint == "" {
		return nil, &BackupStorageError{
			Op:      "build_minio",
			Message: "endpoint is required for MinIO storage",
		}
	}
	config["endpoint"] = endpoint

	// Region (optional for MinIO)
	region := b.flags.Region
	if region == "" {
		region = os.Getenv("MINIO_REGION")
	}
	if region == "" && profileCreds != nil {
		region = profileCreds.Region
	}
	if region != "" {
		config["region"] = region
	}

	// Credentials
	accessKey := os.Getenv("MINIO_ACCESS_KEY")
	if accessKey == "" {
		accessKey = os.Getenv("MINIO_ROOT_USER")
	}
	secretKey := os.Getenv("MINIO_SECRET_KEY")
	if secretKey == "" {
		secretKey = os.Getenv("MINIO_ROOT_PASSWORD")
	}

	// Override with profile credentials if available
	if profileCreds != nil && profileCreds.Credentials != nil {
		if ak, ok := profileCreds.Credentials["access_key"].(string); ok && ak != "" {
			accessKey = b.expandEnvVars(ak)
		}
		if sk, ok := profileCreds.Credentials["secret_key"].(string); ok && sk != "" {
			secretKey = b.expandEnvVars(sk)
		}
	}

	// Add credentials if provided
	if accessKey != "" {
		config["access_key_id"] = accessKey
	}
	if secretKey != "" {
		config["secret_access_key"] = secretKey
	}

	// SSL/TLS (optional)
	useSSL := os.Getenv("MINIO_USE_SSL")
	if useSSL == "" && profileCreds != nil && profileCreds.Config != nil {
		if ssl, ok := profileCreds.Config["use_ssl"].(bool); ok {
			useSSL = fmt.Sprintf("%t", ssl)
		}
	}
	if useSSL == "true" || useSSL == "1" {
		config["use_ssl"] = true
	}

	return config, nil
}

// loadProfileCredentials loads credentials from the credentials file.
func (b *BackupStorageBuilder) loadProfileCredentials(ctx context.Context) (*ProfileCredentials, error) {
	credFile := b.credentialsFile
	if credFile == "" {
		// Default to ~/.quicbaas/backup-credentials.yaml
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, &BackupStorageError{
				Op:      "load_credentials",
				Message: "failed to get user home directory",
				Cause:   err,
			}
		}
		credFile = filepath.Join(homeDir, ".quicbaas", "backup-credentials.yaml")
	}

	// Check if file exists
	if _, err := os.Stat(credFile); os.IsNotExist(err) {
		// File doesn't exist, return nil (fall back to other credential sources)
		return nil, nil
	}

	// Read credentials file
	data, err := os.ReadFile(credFile)
	if err != nil {
		return nil, &BackupStorageError{
			Op:      "load_credentials",
			Message: fmt.Sprintf("failed to read credentials file: %s", credFile),
			Cause:   err,
		}
	}

	// Parse YAML
	var credsFile CredentialsFile
	if err := yaml.Unmarshal(data, &credsFile); err != nil {
		return nil, &BackupStorageError{
			Op:      "load_credentials",
			Message: fmt.Sprintf("failed to parse credentials file: %s", credFile),
			Cause:   err,
		}
	}

	// Determine which profile to use
	profileName := b.profile
	if profileName == "" {
		profileName = credsFile.DefaultProfile
	}
	if profileName == "" {
		return nil, &BackupStorageError{
			Op:      "load_credentials",
			Message: "no profile specified and no default_profile defined in credentials file",
		}
	}

	// Get profile credentials
	profileCreds, ok := credsFile.Profiles[profileName]
	if !ok {
		return nil, &BackupStorageError{
			Op:      "load_credentials",
			Message: fmt.Sprintf("profile '%s' not found in credentials file", profileName),
		}
	}

	return &profileCreds, nil
}

// expandEnvVars expands environment variables in the format ${VAR} or $VAR.
func (b *BackupStorageBuilder) expandEnvVars(s string) string {
	return os.Expand(s, func(key string) string {
		// Check if the key is wrapped in braces
		key = strings.TrimPrefix(key, "{")
		key = strings.TrimSuffix(key, "}")
		return os.Getenv(key)
	})
}

// CredentialsFile represents the structure of ~/.quicbaas/backup-credentials.yaml.
type CredentialsFile struct {
	Profiles       map[string]ProfileCredentials `yaml:"profiles"`
	DefaultProfile string                        `yaml:"default_profile,omitempty"`
}

// ProfileCredentials represents credentials for a specific storage profile.
type ProfileCredentials struct {
	Type        string                 `yaml:"type"`
	Bucket      string                 `yaml:"bucket,omitempty"`
	Container   string                 `yaml:"container,omitempty"`
	Region      string                 `yaml:"region,omitempty"`
	Credentials map[string]interface{} `yaml:"credentials,omitempty"`
	Config      map[string]interface{} `yaml:"config,omitempty"`
}

// BackupStorageError represents an error in storage configuration.
type BackupStorageError struct {
	Op      string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *BackupStorageError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("backup storage %s: %s: %v", e.Op, e.Message, e.Cause)
	}
	return fmt.Sprintf("backup storage %s: %s", e.Op, e.Message)
}

// Unwrap returns the underlying error.
func (e *BackupStorageError) Unwrap() error {
	return e.Cause
}
