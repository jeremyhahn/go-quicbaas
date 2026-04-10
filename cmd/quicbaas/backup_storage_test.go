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
	"os"
	"path/filepath"
	"testing"
)

func TestBackupStorageBuilder_BuildLocal(t *testing.T) {
	tests := []struct {
		name           string
		flags          *BackupStorageFlags
		setStoragePath string
		expectedPath   string
		expectError    bool
	}{
		{
			name: "local storage with default path",
			flags: &BackupStorageFlags{
				StorageType: "local",
			},
			setStoragePath: "",
			expectedPath:   "/var/lib/quicbaas/backups",
			expectError:    false,
		},
		{
			name: "local storage with custom path",
			flags: &BackupStorageFlags{
				StorageType: "local",
			},
			setStoragePath: "/custom/backup/path",
			expectedPath:   "/custom/backup/path",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore the global backupStoragePath
			originalPath := backupStoragePath
			defer func() { backupStoragePath = originalPath }()

			// Set the custom path if specified
			backupStoragePath = tt.setStoragePath

			builder := NewBackupStorageBuilder(tt.flags, "", "")
			storageType, config, err := builder.Build(context.Background())

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if storageType != "local" {
				t.Errorf("expected storage type 'local', got '%s'", storageType)
			}

			basePath, ok := config["base_path"].(string)
			if !ok {
				t.Error("base_path not found in config")
				return
			}

			if basePath != tt.expectedPath {
				t.Errorf("expected base_path '%s', got '%s'", tt.expectedPath, basePath)
			}
		})
	}
}

func TestBackupStorageBuilder_BuildS3(t *testing.T) {
	tests := []struct {
		name         string
		flags        *BackupStorageFlags
		envVars      map[string]string
		expectError  bool
		expectedConf map[string]interface{}
	}{
		{
			name: "s3 with all flags",
			flags: &BackupStorageFlags{
				StorageType: "s3",
				Bucket:      "test-bucket",
				Region:      "us-west-2",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket": "test-bucket",
				"region": "us-west-2",
			},
		},
		{
			name: "s3 with environment variables",
			flags: &BackupStorageFlags{
				StorageType: "s3",
			},
			envVars: map[string]string{
				"AWS_BUCKET": "env-bucket",
				"AWS_REGION": "eu-west-1",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket": "env-bucket",
				"region": "eu-west-1",
			},
		},
		{
			name: "s3 with custom endpoint",
			flags: &BackupStorageFlags{
				StorageType: "s3",
				Bucket:      "test-bucket",
				Region:      "us-east-1",
				Endpoint:    "https://custom-s3.example.com",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":   "test-bucket",
				"region":   "us-east-1",
				"endpoint": "https://custom-s3.example.com",
			},
		},
		{
			name: "s3 missing bucket",
			flags: &BackupStorageFlags{
				StorageType: "s3",
			},
			expectError: true,
		},
		{
			name: "s3 with credentials from env",
			flags: &BackupStorageFlags{
				StorageType: "s3",
				Bucket:      "test-bucket",
				Region:      "us-east-1",
			},
			envVars: map[string]string{
				"AWS_ACCESS_KEY_ID":     "test-key",
				"AWS_SECRET_ACCESS_KEY": "test-secret",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":            "test-bucket",
				"region":            "us-east-1",
				"access_key_id":     "test-key",
				"secret_access_key": "test-secret",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			builder := NewBackupStorageBuilder(tt.flags, "", "")
			storageType, config, err := builder.Build(context.Background())

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if storageType != "s3" {
				t.Errorf("expected storage type 's3', got '%s'", storageType)
			}

			for key, expectedVal := range tt.expectedConf {
				actualVal, ok := config[key]
				if !ok {
					t.Errorf("expected key '%s' not found in config", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("for key '%s': expected '%v', got '%v'", key, expectedVal, actualVal)
				}
			}
		})
	}
}

func TestBackupStorageBuilder_BuildAzure(t *testing.T) {
	tests := []struct {
		name         string
		flags        *BackupStorageFlags
		envVars      map[string]string
		expectError  bool
		expectedConf map[string]interface{}
	}{
		{
			name: "azure with container flag",
			flags: &BackupStorageFlags{
				StorageType: "azure",
				Container:   "test-container",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"container": "test-container",
			},
		},
		{
			name: "azure with environment variables",
			flags: &BackupStorageFlags{
				StorageType: "azure",
			},
			envVars: map[string]string{
				"AZURE_STORAGE_CONTAINER": "env-container",
				"AZURE_STORAGE_ACCOUNT":   "test-account",
				"AZURE_STORAGE_KEY":       "test-key",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"container":    "env-container",
				"account_name": "test-account",
				"account_key":  "test-key",
			},
		},
		{
			name: "azure missing container",
			flags: &BackupStorageFlags{
				StorageType: "azure",
			},
			expectError: true,
		},
		{
			name: "azure with connection string",
			flags: &BackupStorageFlags{
				StorageType: "azure",
				Container:   "test-container",
			},
			envVars: map[string]string{
				"AZURE_STORAGE_CONNECTION_STRING": "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=key",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"container":         "test-container",
				"connection_string": "DefaultEndpointsProtocol=https;AccountName=test;AccountKey=key",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			builder := NewBackupStorageBuilder(tt.flags, "", "")
			storageType, config, err := builder.Build(context.Background())

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if storageType != "azure" {
				t.Errorf("expected storage type 'azure', got '%s'", storageType)
			}

			for key, expectedVal := range tt.expectedConf {
				actualVal, ok := config[key]
				if !ok {
					t.Errorf("expected key '%s' not found in config", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("for key '%s': expected '%v', got '%v'", key, expectedVal, actualVal)
				}
			}
		})
	}
}

func TestBackupStorageBuilder_BuildGCS(t *testing.T) {
	tests := []struct {
		name         string
		flags        *BackupStorageFlags
		envVars      map[string]string
		expectError  bool
		expectedConf map[string]interface{}
	}{
		{
			name: "gcs with bucket flag",
			flags: &BackupStorageFlags{
				StorageType: "gcs",
				Bucket:      "test-bucket",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket": "test-bucket",
			},
		},
		{
			name: "gcs with environment variables",
			flags: &BackupStorageFlags{
				StorageType: "gcs",
			},
			envVars: map[string]string{
				"GCS_BUCKET":     "env-bucket",
				"GCS_PROJECT_ID": "test-project",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":     "env-bucket",
				"project_id": "test-project",
			},
		},
		{
			name: "gcs missing bucket",
			flags: &BackupStorageFlags{
				StorageType: "gcs",
			},
			expectError: true,
		},
		{
			name: "gcs with credentials file",
			flags: &BackupStorageFlags{
				StorageType: "gcs",
				Bucket:      "test-bucket",
			},
			envVars: map[string]string{
				"GOOGLE_APPLICATION_CREDENTIALS": "/path/to/creds.json",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":           "test-bucket",
				"credentials_file": "/path/to/creds.json",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			builder := NewBackupStorageBuilder(tt.flags, "", "")
			storageType, config, err := builder.Build(context.Background())

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if storageType != "gcs" {
				t.Errorf("expected storage type 'gcs', got '%s'", storageType)
			}

			for key, expectedVal := range tt.expectedConf {
				actualVal, ok := config[key]
				if !ok {
					t.Errorf("expected key '%s' not found in config", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("for key '%s': expected '%v', got '%v'", key, expectedVal, actualVal)
				}
			}
		})
	}
}

func TestBackupStorageBuilder_BuildMinIO(t *testing.T) {
	tests := []struct {
		name         string
		flags        *BackupStorageFlags
		envVars      map[string]string
		expectError  bool
		expectedConf map[string]interface{}
	}{
		{
			name: "minio with all flags",
			flags: &BackupStorageFlags{
				StorageType: "minio",
				Bucket:      "test-bucket",
				Endpoint:    "localhost:9000",
				Region:      "us-east-1",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":   "test-bucket",
				"endpoint": "localhost:9000",
				"region":   "us-east-1",
			},
		},
		{
			name: "minio with environment variables",
			flags: &BackupStorageFlags{
				StorageType: "minio",
			},
			envVars: map[string]string{
				"MINIO_BUCKET":     "env-bucket",
				"MINIO_ENDPOINT":   "minio.example.com:9000",
				"MINIO_ACCESS_KEY": "minioadmin",
				"MINIO_SECRET_KEY": "minioadmin",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":            "env-bucket",
				"endpoint":          "minio.example.com:9000",
				"access_key_id":     "minioadmin",
				"secret_access_key": "minioadmin",
			},
		},
		{
			name: "minio missing bucket",
			flags: &BackupStorageFlags{
				StorageType: "minio",
				Endpoint:    "localhost:9000",
			},
			expectError: true,
		},
		{
			name: "minio missing endpoint",
			flags: &BackupStorageFlags{
				StorageType: "minio",
				Bucket:      "test-bucket",
			},
			expectError: true,
		},
		{
			name: "minio with ssl enabled",
			flags: &BackupStorageFlags{
				StorageType: "minio",
				Bucket:      "test-bucket",
				Endpoint:    "minio.example.com:9000",
			},
			envVars: map[string]string{
				"MINIO_USE_SSL": "true",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":   "test-bucket",
				"endpoint": "minio.example.com:9000",
				"use_ssl":  true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			builder := NewBackupStorageBuilder(tt.flags, "", "")
			storageType, config, err := builder.Build(context.Background())

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if storageType != "minio" {
				t.Errorf("expected storage type 'minio', got '%s'", storageType)
			}

			for key, expectedVal := range tt.expectedConf {
				actualVal, ok := config[key]
				if !ok {
					t.Errorf("expected key '%s' not found in config", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("for key '%s': expected '%v', got '%v'", key, expectedVal, actualVal)
				}
			}
		})
	}
}

func TestBackupStorageBuilder_CredentialsFile(t *testing.T) {
	// Create temporary credentials file
	tmpDir := t.TempDir()
	credFile := filepath.Join(tmpDir, "backup-credentials.yaml")

	credContent := `profiles:
  production-s3:
    type: s3
    bucket: prod-bucket
    region: us-west-2
    credentials:
      access_key: ${AWS_ACCESS_KEY_ID}
      secret_key: ${AWS_SECRET_ACCESS_KEY}
  staging-azure:
    type: azure
    container: staging-container
    credentials:
      account_name: ${AZURE_STORAGE_ACCOUNT}
      account_key: ${AZURE_STORAGE_KEY}
default_profile: production-s3
`

	if err := os.WriteFile(credFile, []byte(credContent), 0o600); err != nil {
		t.Fatalf("failed to create test credentials file: %v", err)
	}

	tests := []struct {
		name         string
		profile      string
		envVars      map[string]string
		expectError  bool
		expectedConf map[string]interface{}
	}{
		{
			name:    "load default profile with env var expansion",
			profile: "",
			envVars: map[string]string{
				"AWS_ACCESS_KEY_ID":     "test-access-key",
				"AWS_SECRET_ACCESS_KEY": "test-secret-key",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"bucket":            "prod-bucket",
				"region":            "us-west-2",
				"access_key_id":     "test-access-key",
				"secret_access_key": "test-secret-key",
			},
		},
		{
			name:    "load specific profile",
			profile: "staging-azure",
			envVars: map[string]string{
				"AZURE_STORAGE_ACCOUNT": "teststorage",
				"AZURE_STORAGE_KEY":     "testkey",
			},
			expectError: false,
			expectedConf: map[string]interface{}{
				"container":    "staging-container",
				"account_name": "teststorage",
				"account_key":  "testkey",
			},
		},
		{
			name:        "profile not found",
			profile:     "nonexistent",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			flags := &BackupStorageFlags{
				StorageType: "s3",
			}
			if tt.name == "load specific profile" {
				flags.StorageType = "azure"
			}

			builder := NewBackupStorageBuilder(flags, credFile, tt.profile)
			_, config, err := builder.Build(context.Background())

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			for key, expectedVal := range tt.expectedConf {
				actualVal, ok := config[key]
				if !ok {
					t.Errorf("expected key '%s' not found in config", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("for key '%s': expected '%v', got '%v'", key, expectedVal, actualVal)
				}
			}
		})
	}
}

func TestBackupStorageBuilder_InvalidStorageType(t *testing.T) {
	flags := &BackupStorageFlags{
		StorageType: "invalid-type",
	}

	builder := NewBackupStorageBuilder(flags, "", "")
	_, _, err := builder.Build(context.Background())

	if err == nil {
		t.Error("expected error for invalid storage type but got none")
	}

	if _, ok := err.(*BackupStorageError); !ok {
		t.Errorf("expected BackupStorageError, got %T", err)
	}
}

func TestBackupStorageError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *BackupStorageError
		expected string
	}{
		{
			name: "error without cause",
			err: &BackupStorageError{
				Op:      "test_op",
				Message: "test message",
			},
			expected: "backup storage test_op: test message",
		},
		{
			name: "error with cause",
			err: &BackupStorageError{
				Op:      "test_op",
				Message: "test message",
				Cause:   os.ErrNotExist,
			},
			expected: "backup storage test_op: test message: file does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errStr := tt.err.Error()
			if errStr != tt.expected {
				t.Errorf("expected error string '%s', got '%s'", tt.expected, errStr)
			}
		})
	}
}

func TestBackupStorageError_Unwrap(t *testing.T) {
	cause := os.ErrNotExist
	err := &BackupStorageError{
		Op:      "test",
		Message: "test",
		Cause:   cause,
	}

	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Errorf("expected unwrapped error to be %v, got %v", cause, unwrapped)
	}
}

func TestExpandEnvVars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		envVars  map[string]string
		expected string
	}{
		{
			name:  "expand ${VAR} format",
			input: "${TEST_VAR}",
			envVars: map[string]string{
				"TEST_VAR": "test-value",
			},
			expected: "test-value",
		},
		{
			name:  "expand $VAR format",
			input: "$TEST_VAR",
			envVars: map[string]string{
				"TEST_VAR": "test-value",
			},
			expected: "test-value",
		},
		{
			name:  "expand multiple variables",
			input: "${VAR1}-${VAR2}",
			envVars: map[string]string{
				"VAR1": "hello",
				"VAR2": "world",
			},
			expected: "hello-world",
		},
		{
			name:     "undefined variable",
			input:    "${UNDEFINED_VAR}",
			envVars:  map[string]string{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			builder := &BackupStorageBuilder{}
			result := builder.expandEnvVars(tt.input)

			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}
