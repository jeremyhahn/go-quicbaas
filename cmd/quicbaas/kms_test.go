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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestNewKMSCommand(t *testing.T) {
	cmd := newKMSCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "kms", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Check that all expected subcommands exist
	expectedCommands := []string{"key", "encrypt", "decrypt"}
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

func TestNewKMSKeyCommand(t *testing.T) {
	cmd := newKMSKeyCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "key", cmd.Use)

	// Check that all expected subcommands exist
	// Note: the actual implementation uses 'generate' not 'create'
	expectedCommands := []string{"generate", "get", "list", "rotate", "delete", "import"}
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

func TestKMSKeyCreateCommandFlags(t *testing.T) {
	cmd := newKMSKeyGenerateCommand()
	assert.NotNil(t, cmd)

	algorithmFlag := cmd.Flags().Lookup("algorithm")
	assert.NotNil(t, algorithmFlag)
	// The default is uppercase ED25519
	assert.Equal(t, "AES-256-GCM", algorithmFlag.DefValue)

	descriptionFlag := cmd.Flags().Lookup("description")
	assert.NotNil(t, descriptionFlag)

	exportableFlag := cmd.Flags().Lookup("exportable")
	assert.NotNil(t, exportableFlag)
	assert.Equal(t, "false", exportableFlag.DefValue)
}

func TestKMSKeyCreateCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "create with name",
			args:        []string{"test-key"},
			expectError: true, // Will fail because no server is running
			errorMsg:    "",   // Connection error varies by environment
		},
		{
			name:        "create with all options",
			args:        []string{"test-key", "--algorithm", "ECDSA-P256", "--exportable", "--description", "Test key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "create without name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyGenerateCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSKeyImportCommandFlags(t *testing.T) {
	cmd := newKMSKeyImportCommand()
	assert.NotNil(t, cmd)

	materialFlag := cmd.Flags().Lookup("material")
	assert.NotNil(t, materialFlag)

	algorithmFlag := cmd.Flags().Lookup("algorithm")
	assert.NotNil(t, algorithmFlag)
	assert.Equal(t, "AES-256-GCM", algorithmFlag.DefValue)

	descriptionFlag := cmd.Flags().Lookup("description")
	assert.NotNil(t, descriptionFlag)
}

func TestKMSKeyImportCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "import without name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "import without material",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "import with invalid base64",
			args:        []string{"test-key", "--material", "base64:invalid!!!", "--algorithm", "AES-256-GCM"},
			expectError: true,
			errorMsg:    "invalid base64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyImportCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSKeyGetCommandFlags(t *testing.T) {
	cmd := newKMSKeyGetCommand()
	assert.NotNil(t, cmd)

	versionFlag := cmd.Flags().Lookup("version")
	assert.NotNil(t, versionFlag)
	assert.Equal(t, "0", versionFlag.DefValue)
}

func TestKMSKeyGetCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "get without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "get with key name",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "get with version",
			args:        []string{"test-key", "--version", "2"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyGetCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSKeyListCommandFlags(t *testing.T) {
	cmd := newKMSKeyListCommand()
	assert.NotNil(t, cmd)

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "100", limitFlag.DefValue)
}

func TestKMSKeyListCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "list all keys",
			args:        []string{},
			expectError: true, // No server running during unit tests
			errorMsg:    "",   // Connection error varies by environment
		},
		{
			name:        "list with prefix",
			args:        []string{"tenant-123/"},
			expectError: true, // No server running during unit tests
			errorMsg:    "",   // Connection error varies by environment
		},
		{
			name:        "list with limit",
			args:        []string{"--limit", "50"},
			expectError: true, // No server running during unit tests
			errorMsg:    "",   // Connection error varies by environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyListCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSKeyRotateCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "rotate without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "rotate with key name",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyRotateCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSKeyDeleteCommandFlags(t *testing.T) {
	cmd := newKMSKeyDeleteCommand()
	assert.NotNil(t, cmd)

	forceFlag := cmd.Flags().Lookup("force")
	assert.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)

	purgeFlag := cmd.Flags().Lookup("purge")
	assert.NotNil(t, purgeFlag)
	assert.Equal(t, "false", purgeFlag.DefValue)
}

func TestKMSKeyDeleteCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "delete without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "delete with key name",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "delete with force",
			args:        []string{"test-key", "--force"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "delete with force and purge",
			args:        []string{"test-key", "--force", "--purge"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyDeleteCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSEncryptCommandFlags(t *testing.T) {
	cmd := newKMSEncryptCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	aadFlag := cmd.Flags().Lookup("aad")
	assert.NotNil(t, aadFlag)

	base64Flag := cmd.Flags().Lookup("base64")
	assert.NotNil(t, base64Flag)
	assert.Equal(t, "false", base64Flag.DefValue)
}

func TestKMSEncryptCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "encrypt without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "encrypt without data",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "encrypt with data",
			args:        []string{"test-key", "--data", "sensitive data"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "encrypt with aad",
			args:        []string{"test-key", "--data", "sensitive", "--aad", "context"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "encrypt with invalid base64",
			args:        []string{"test-key", "--data", "invalid!!!", "--base64"},
			expectError: true,
			errorMsg:    "invalid base64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSEncryptCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSDecryptCommandFlags(t *testing.T) {
	cmd := newKMSDecryptCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	aadFlag := cmd.Flags().Lookup("aad")
	assert.NotNil(t, aadFlag)

	base64Flag := cmd.Flags().Lookup("base64")
	assert.NotNil(t, base64Flag)
	assert.Equal(t, "false", base64Flag.DefValue)
}

func TestKMSDecryptCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "decrypt without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "decrypt without data",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "decrypt with invalid hex",
			args:        []string{"test-key", "--data", "invalid!!!"},
			expectError: true,
			errorMsg:    "invalid hex",
		},
		{
			name:        "decrypt with invalid base64",
			args:        []string{"test-key", "--data", "invalid!!!", "--base64"},
			expectError: true,
			errorMsg:    "invalid base64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSDecryptCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSKeyCreateCommand tests the KMS key create command with mocked client.
func TestRunKMSKeyCreateCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		setupFlags    func()
		args          []string
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success with table output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
						return &service.GenerateKeyResponse{
							Key: &service.KeyHandle{
								KeyID:     "test-key-123",
								Algorithm: service.KeyAlgAES256GCM,
								Version:   1,
								Status:    service.KeyStatusEnabled,
								CreatedAt: time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsAlgorithm = "ED25519"
				kmsDescription = "Test key"
				kmsExportable = true
			},
			args:         []string{"test-key"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with json output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
						return &service.GenerateKeyResponse{
							Key: &service.KeyHandle{
								KeyID:     "test-key-456",
								Algorithm: service.KeyAlgChaCha20Poly1305,
								Version:   1,
								Status:    service.KeyStatusEnabled,
								CreatedAt: time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsAlgorithm = "ECDSA-P256"
				kmsDescription = ""
				kmsExportable = false
			},
			args:         []string{"test-key"},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with default output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
						return &service.GenerateKeyResponse{
							Key: &service.KeyHandle{
								KeyID:     "test-key-789",
								Algorithm: service.KeyAlgAES256GCM,
								Version:   1,
								Status:    service.KeyStatusEnabled,
								CreatedAt: time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsAlgorithm = "ED25519"
			},
			args:         []string{"test-key"},
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			setupFlags:    func() {},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGenerateKeyFunc: func(ctx context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
						return nil, errors.Newf(errors.ErrInvalidInput, "kms.generate", "unsupported algorithm: INVALID-ALG")
					},
				}
			},
			setupFlags: func() {
				kmsAlgorithm = "INVALID-ALG"
			},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "unsupported algorithm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsAlgorithm = ""
				kmsDescription = ""
				kmsExportable = false
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			err := runKMSKeyGenerateCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSKeyImportCommand tests the KMS key import command with mocked client.
func TestRunKMSKeyImportCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	// Create temp file for testing
	tmpFile, err := os.CreateTemp("", "kms_import_test_*.bin")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	testKeyMaterial := []byte("test-key-material-32-bytes-long")
	_, err = tmpFile.Write(testKeyMaterial)
	require.NoError(t, err)
	tmpFile.Close()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		setupFlags    func()
		args          []string
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success with file material",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSImportKeyFunc: func(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
						assert.Equal(t, "imported-key", req.Name)
						assert.Equal(t, testKeyMaterial, req.KeyMaterial)
						return &service.ImportKeyResponse{
							Key: &service.KeyHandle{
								KeyID:     "imported-key-123",
								Algorithm: service.KeyAlgAES256GCM,
								Version:   1,
								Status:    service.KeyStatusEnabled,
								CreatedAt: time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsMaterial = tmpFile.Name()
				kmsAlgorithm = "AES-256-GCM"
				kmsDescription = "Imported test key"
			},
			args:         []string{"imported-key"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with base64 material",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSImportKeyFunc: func(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
						return &service.ImportKeyResponse{
							Key: &service.KeyHandle{
								KeyID:     "imported-key-456",
								Algorithm: service.KeyAlgAES256GCM,
								Version:   1,
								Status:    service.KeyStatusEnabled,
								CreatedAt: time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsMaterial = "base64:" + base64.StdEncoding.EncodeToString(testKeyMaterial)
				kmsAlgorithm = "AES-256-GCM"
			},
			args:         []string{"imported-key"},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with default output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSImportKeyFunc: func(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
						return &service.ImportKeyResponse{
							Key: &service.KeyHandle{
								KeyID:     "imported-key-789",
								Algorithm: service.KeyAlgAES256GCM,
								Version:   1,
								Status:    service.KeyStatusEnabled,
								CreatedAt: time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsMaterial = tmpFile.Name()
				kmsAlgorithm = "AES-256-GCM"
			},
			args:         []string{"imported-key"},
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "invalid base64 material",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{}
			},
			setupFlags: func() {
				kmsMaterial = "base64:invalid!!!"
				kmsAlgorithm = "AES-256-GCM"
			},
			args:          []string{"imported-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "invalid base64",
		},
		{
			name: "file not found",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{}
			},
			setupFlags: func() {
				kmsMaterial = "/nonexistent/file.bin"
				kmsAlgorithm = "AES-256-GCM"
			},
			args:          []string{"imported-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "failed to read key material file",
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			setupFlags: func() {
				kmsMaterial = tmpFile.Name()
				kmsAlgorithm = "AES-256-GCM"
			},
			args:          []string{"imported-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSImportKeyFunc: func(ctx context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
						return nil, errors.Newf(errors.ErrInvalidInput, "kms.import", "invalid key material")
					},
				}
			},
			setupFlags: func() {
				kmsMaterial = tmpFile.Name()
				kmsAlgorithm = "AES-256-GCM"
			},
			args:          []string{"imported-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "invalid key material",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsMaterial = ""
				kmsAlgorithm = ""
				kmsDescription = ""
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			err := runKMSKeyImportCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSKeyGetCommand tests the KMS key get command with mocked client.
func TestRunKMSKeyGetCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		setupFlags    func()
		args          []string
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success with table output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGetKeyFunc: func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, uint64(0), req.Version)
						return &service.GetKeyResponse{
							Metadata: &service.KeyMetadata{
								KeyID:       "test-key-123",
								Name:        "test-key",
								Algorithm:   service.KeyAlgAES256GCM,
								Version:     1,
								Status:      service.KeyStatusEnabled,
								Exportable:  false,
								Description: "Test key metadata",
								CreatedAt:   time.Now(),
								UpdatedAt:   time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsVersion = 0
			},
			args:         []string{"test-key"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with version",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGetKeyFunc: func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, uint64(2), req.Version)
						return &service.GetKeyResponse{
							Metadata: &service.KeyMetadata{
								KeyID:      "test-key-123",
								Name:       "test-key",
								Algorithm:  service.KeyAlgAES256GCM,
								Version:    2,
								Status:     service.KeyStatusEnabled,
								Exportable: true,
								CreatedAt:  time.Now(),
								UpdatedAt:  time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsVersion = 2
			},
			args:         []string{"test-key"},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with default output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGetKeyFunc: func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
						return &service.GetKeyResponse{
							Metadata: &service.KeyMetadata{
								KeyID:     "test-key-789",
								Name:      "test-key",
								Algorithm: service.KeyAlgAES256GCM,
								Version:   1,
								Status:    service.KeyStatusEnabled,
								CreatedAt: time.Now(),
								UpdatedAt: time.Now(),
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsVersion = 0
			},
			args:         []string{"test-key"},
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			setupFlags:    func() {},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSGetKeyFunc: func(ctx context.Context, req *service.GetKeyRequest) (*service.GetKeyResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "kms.get", "key not found")
					},
				}
			},
			setupFlags:    func() {},
			args:          []string{"nonexistent-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsVersion = 0
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			err := runKMSKeyGetCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSKeyListCommand tests the KMS key list command with mocked client.
func TestRunKMSKeyListCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		setupFlags    func()
		args          []string
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success with table output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSListKeysFunc: func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
						assert.Equal(t, 100, req.Limit)
						return &service.ListKeysResponse{
							Keys: []*service.KeyHandle{
								{
									KeyID:     "key-1",
									Algorithm: service.KeyAlgAES256GCM,
									Version:   1,
									Status:    service.KeyStatusEnabled,
									CreatedAt: time.Now(),
								},
								{
									KeyID:     "key-2",
									Algorithm: service.KeyAlgChaCha20Poly1305,
									Version:   2,
									Status:    service.KeyStatusDisabled,
									CreatedAt: time.Now(),
								},
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsLimit = 100
			},
			args:         []string{},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with prefix filter",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSListKeysFunc: func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
						assert.Equal(t, "tenant-123/", req.Filter["prefix"])
						assert.Equal(t, 50, req.Limit)
						return &service.ListKeysResponse{
							Keys: []*service.KeyHandle{
								{
									KeyID:     "tenant-123/key-1",
									Algorithm: service.KeyAlgAES256GCM,
									Version:   1,
									Status:    service.KeyStatusEnabled,
									CreatedAt: time.Now(),
								},
							},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsLimit = 50
			},
			args:         []string{"tenant-123/"},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with default output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSListKeysFunc: func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
						return &service.ListKeysResponse{
							Keys: []*service.KeyHandle{},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsLimit = 100
			},
			args:         []string{},
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			setupFlags: func() {
				kmsLimit = 100
			},
			args:          []string{},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSListKeysFunc: func(ctx context.Context, req *service.ListKeysRequest) (*service.ListKeysResponse, error) {
						return nil, errors.Newf(errors.ErrInternal, "kms.list", "failed to list keys")
					},
				}
			},
			setupFlags: func() {
				kmsLimit = 100
			},
			args:          []string{},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "failed to list keys",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsLimit = 0
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			err := runKMSKeyListCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSKeyRotateCommand tests the KMS key rotate command with mocked client.
func TestRunKMSKeyRotateCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		args          []string
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success with table output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSRotateKeyFunc: func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.True(t, req.RetainOldVersion)
						return &service.RotateKeyResponse{
							KeyID:      "test-key",
							OldVersion: 1,
							NewVersion: 2,
							Timestamp:  time.Now(),
						}, nil
					},
				}
			},
			args:         []string{"test-key"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with json output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSRotateKeyFunc: func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
						return &service.RotateKeyResponse{
							KeyID:      "test-key",
							OldVersion: 2,
							NewVersion: 3,
							Timestamp:  time.Now(),
						}, nil
					},
				}
			},
			args:         []string{"test-key"},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with default output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSRotateKeyFunc: func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
						return &service.RotateKeyResponse{
							KeyID:      "test-key",
							OldVersion: 1,
							NewVersion: 2,
							Timestamp:  time.Now(),
						}, nil
					},
				}
			},
			args:         []string{"test-key"},
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSRotateKeyFunc: func(ctx context.Context, req *service.RotateKeyRequest) (*service.RotateKeyResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "kms.rotate", "key not found")
					},
				}
			},
			args:          []string{"nonexistent-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			err := runKMSKeyRotateCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSKeyDeleteCommand tests the KMS key delete command with mocked client.
func TestRunKMSKeyDeleteCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		setupFlags    func()
		args          []string
		purge         bool
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success without force",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDeleteKeyFunc: func(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, 30, req.PendingWindowDays)
						assert.False(t, req.Force)
						return &service.DeleteKeyResponse{
							KeyID:        "test-key",
							Status:       service.KeyStatusDeleted,
							DeletionDate: time.Now().Add(30 * 24 * time.Hour),
							Timestamp:    time.Now(),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsForce = false
				kmsPurge = false
			},
			args:         []string{"test-key"},
			purge:        false,
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with force",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDeleteKeyFunc: func(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.True(t, req.Force)
						return &service.DeleteKeyResponse{
							KeyID:        "test-key",
							Status:       service.KeyStatusDeleted,
							DeletionDate: time.Now(),
							Timestamp:    time.Now(),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsForce = true
			},
			args:         []string{"test-key"},
			purge:        false,
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with force and purge",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDeleteKeyFunc: func(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, 0, req.PendingWindowDays)
						assert.True(t, req.Force)
						return &service.DeleteKeyResponse{
							KeyID:        "test-key",
							Status:       service.KeyStatusDeleted,
							DeletionDate: time.Now(),
							Timestamp:    time.Now(),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsForce = true
			},
			args:         []string{"test-key"},
			purge:        true,
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with default output",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDeleteKeyFunc: func(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
						return &service.DeleteKeyResponse{
							KeyID:        "test-key",
							Status:       service.KeyStatusDeleted,
							DeletionDate: time.Now().Add(30 * 24 * time.Hour),
							Timestamp:    time.Now(),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsForce = false
				kmsPurge = false
			},
			args:         []string{"test-key"},
			purge:        false,
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			setupFlags:    func() {},
			args:          []string{"test-key"},
			purge:         false,
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDeleteKeyFunc: func(ctx context.Context, req *service.DeleteKeyRequest) (*service.DeleteKeyResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "kms.delete", "key not found")
					},
				}
			},
			setupFlags:    func() {},
			args:          []string{"nonexistent-key"},
			purge:         false,
			outputFormat:  "table",
			expectError:   true,
			expectedError: "key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsForce = false
				kmsPurge = false
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			kmsPurge = tt.purge
			err := runKMSKeyDeleteCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSEncryptCommand tests the KMS encrypt command with mocked client.
func TestRunKMSEncryptCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		setupFlags    func()
		args          []string
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success with plain text",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, []byte("sensitive data"), req.Plaintext)
						assert.Nil(t, req.Context)
						return &service.EncryptResponse{
							KeyID:      "test-key",
							KeyVersion: 1,
							Ciphertext: []byte("encrypted-data"),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsData = "sensitive data"
				kmsAAD = ""
				kmsBase64 = false
			},
			args:         []string{"test-key"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with AAD",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, []byte("sensitive"), req.Plaintext)
						assert.Equal(t, []byte("context"), req.Context)
						return &service.EncryptResponse{
							KeyID:      "test-key",
							KeyVersion: 1,
							Ciphertext: []byte("encrypted-with-aad"),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsData = "sensitive"
				kmsAAD = "context"
				kmsBase64 = false
			},
			args:         []string{"test-key"},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with base64 data",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
						assert.Equal(t, []byte("Hello"), req.Plaintext)
						return &service.EncryptResponse{
							KeyID:      "test-key",
							KeyVersion: 1,
							Ciphertext: []byte("encrypted-base64"),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsData = base64.StdEncoding.EncodeToString([]byte("Hello"))
				kmsAAD = ""
				kmsBase64 = true
			},
			args:         []string{"test-key"},
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "invalid base64 data",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{}
			},
			setupFlags: func() {
				kmsData = "invalid!!!"
				kmsAAD = ""
				kmsBase64 = true
			},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "invalid base64",
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			setupFlags: func() {
				kmsData = "data"
				kmsAAD = ""
				kmsBase64 = false
			},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "kms.encrypt", "key not found")
					},
				}
			},
			setupFlags: func() {
				kmsData = "data"
				kmsAAD = ""
				kmsBase64 = false
			},
			args:          []string{"nonexistent-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsData = ""
				kmsAAD = ""
				kmsBase64 = false
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			err := runKMSEncryptCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestRunKMSDecryptCommand tests the KMS decrypt command with mocked client.
func TestRunKMSDecryptCommand(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() { kmsClientFactory = origFactory }()

	tests := []struct {
		name          string
		setupMock     func() *MockKMSClient
		setupFlags    func()
		args          []string
		outputFormat  string
		expectError   bool
		expectedError string
	}{
		{
			name: "success with hex data",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, []byte("Hello"), req.Ciphertext)
						assert.Nil(t, req.Context)
						return &service.DecryptResponse{
							KeyID:      "test-key",
							KeyVersion: 1,
							Plaintext:  []byte("decrypted data"),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsData = hex.EncodeToString([]byte("Hello"))
				kmsAAD = ""
				kmsBase64 = false
			},
			args:         []string{"test-key"},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "success with AAD",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
						assert.Equal(t, "test-key", req.KeyID)
						assert.Equal(t, []byte("context"), req.Context)
						return &service.DecryptResponse{
							KeyID:      "test-key",
							KeyVersion: 1,
							Plaintext:  []byte("decrypted with aad"),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsData = hex.EncodeToString([]byte("encrypted"))
				kmsAAD = "context"
				kmsBase64 = false
			},
			args:         []string{"test-key"},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "success with base64 data",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
						assert.Equal(t, []byte("Hello"), req.Ciphertext)
						return &service.DecryptResponse{
							KeyID:      "test-key",
							KeyVersion: 1,
							Plaintext:  []byte("decrypted base64"),
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsData = base64.StdEncoding.EncodeToString([]byte("Hello"))
				kmsAAD = ""
				kmsBase64 = true
			},
			args:         []string{"test-key"},
			outputFormat: "",
			expectError:  false,
		},
		{
			name: "invalid hex data",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{}
			},
			setupFlags: func() {
				kmsData = "invalid!!!"
				kmsAAD = ""
				kmsBase64 = false
			},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "invalid hex",
		},
		{
			name: "invalid base64 data",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{}
			},
			setupFlags: func() {
				kmsData = "invalid!!!"
				kmsAAD = ""
				kmsBase64 = true
			},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "invalid base64",
		},
		{
			name: "factory error",
			setupMock: func() *MockKMSClient {
				return nil
			},
			setupFlags: func() {
				kmsData = hex.EncodeToString([]byte("data"))
				kmsAAD = ""
				kmsBase64 = false
			},
			args:          []string{"test-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "connection failed",
		},
		{
			name: "service error",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
						return nil, errors.Newf(errors.ErrNotFound, "kms.decrypt", "key not found")
					},
				}
			},
			setupFlags: func() {
				kmsData = hex.EncodeToString([]byte("data"))
				kmsAAD = ""
				kmsBase64 = false
			},
			args:          []string{"nonexistent-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "key not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsData = ""
				kmsAAD = ""
				kmsBase64 = false
				kmsOutputFmt = ""
			}()

			// Setup factory
			if tt.name == "factory error" {
				kmsClientFactory = func() (KMSClient, error) {
					return nil, errors.Newf(errors.ErrUnavailable, "kms.factory", "connection failed")
				}
			} else {
				mock := tt.setupMock()
				kmsClientFactory = func() (KMSClient, error) {
					return mock, nil
				}
			}

			// Execute command
			err := runKMSDecryptCommand(nil, tt.args)

			// Verify
			if tt.expectError {
				require.Error(t, err)
				if tt.expectedError != "" {
					assert.Contains(t, err.Error(), tt.expectedError)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatKMSGenerateResponse(t *testing.T) {
	resp := &service.GenerateKeyResponse{
		Key: &service.KeyHandle{
			KeyID:     "key-123",
			Algorithm: service.KeyAlgAES256GCM,
			Version:   1,
			Status:    service.KeyStatusEnabled,
			CreatedAt: time.Now(),
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
			expectedText: []string{"key-123", "AES-256-GCM", "enabled"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID", "key-123", "Algorithm", "Version"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Key generated", "key-123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "kms_test_*.txt")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())
			defer tmpFile.Close()

			err = formatKMSKeyGenerateResponse(resp, tt.format, tmpFile)
			require.NoError(t, err)

			tmpFile.Seek(0, 0)
			output, _ := io.ReadAll(tmpFile)
			outputStr := string(output)

			for _, expected := range tt.expectedText {
				assert.Contains(t, outputStr, expected)
			}
		})
	}
}

func TestFormatKMSEncryptResponse(t *testing.T) {
	ciphertext := []byte("encrypted data")
	resp := &service.EncryptResponse{
		KeyID:      "key-123",
		KeyVersion: 1,
		Ciphertext: ciphertext,
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"key-123", "key_version"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID", "key-123", "Ciphertext (hex)", hex.EncodeToString(ciphertext)},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{hex.EncodeToString(ciphertext)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "kms_test_*.txt")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())
			defer tmpFile.Close()

			err = formatKMSEncryptResponse(resp, tt.format, tmpFile)
			require.NoError(t, err)

			tmpFile.Seek(0, 0)
			output, _ := io.ReadAll(tmpFile)
			outputStr := string(output)

			for _, expected := range tt.expectedText {
				assert.Contains(t, outputStr, expected)
			}
		})
	}
}

func TestFormatKMSDecryptResponse(t *testing.T) {
	plaintext := []byte("decrypted data")
	resp := &service.DecryptResponse{
		KeyID:      "key-123",
		KeyVersion: 1,
		Plaintext:  plaintext,
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"key-123", "plaintext"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID", "key-123", "Plaintext", string(plaintext)},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Plaintext", string(plaintext)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile, err := os.CreateTemp("", "kms_test_*.txt")
			require.NoError(t, err)
			defer os.Remove(tmpFile.Name())
			defer tmpFile.Close()

			err = formatKMSDecryptResponse(resp, tt.format, tmpFile)
			require.NoError(t, err)

			tmpFile.Seek(0, 0)
			output, _ := io.ReadAll(tmpFile)
			outputStr := string(output)

			for _, expected := range tt.expectedText {
				assert.Contains(t, outputStr, expected)
			}
		})
	}
}

func TestKMSOutputFormatParsing(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		expectError bool
	}{
		{
			name:        "json format",
			format:      "json",
			expectError: false,
		},
		{
			name:        "table format",
			format:      "table",
			expectError: false,
		},
		{
			name:        "yaml format",
			format:      "yaml",
			expectError: false,
		},
		{
			name:        "invalid format",
			format:      "invalid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := cli.ParseOutputFormat(tt.format)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSBase64Encoding(t *testing.T) {
	testData := []byte("test data for encryption")
	encoded := base64.StdEncoding.EncodeToString(testData)

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	assert.NoError(t, err)
	assert.Equal(t, testData, decoded)
}

func TestKMSHexEncoding(t *testing.T) {
	testData := []byte("test data for encryption")
	encoded := hex.EncodeToString(testData)

	decoded, err := hex.DecodeString(encoded)
	assert.NoError(t, err)
	assert.Equal(t, testData, decoded)
}

func TestFormatKMSImportResponse(t *testing.T) {
	resp := &service.ImportKeyResponse{
		Key: &service.KeyHandle{
			KeyID:     "imported-key-123",
			Algorithm: service.KeyAlgAES256GCM,
			Version:   1,
			Status:    service.KeyStatusEnabled,
			CreatedAt: time.Now(),
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
			expectedText: []string{"imported-key-123", "AES-256-GCM"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID:", "imported-key-123", "Algorithm:"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Key imported:", "imported-key-123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyImportResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatKMSImportResponse_InvalidFormat(t *testing.T) {
	resp := &service.ImportKeyResponse{
		Key: &service.KeyHandle{
			KeyID:     "key-123",
			Algorithm: service.KeyAlgAES256GCM,
			Status:    service.KeyStatusEnabled,
			CreatedAt: time.Now(),
		},
	}

	var buf bytes.Buffer
	err := formatKMSKeyImportResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSGetResponse(t *testing.T) {
	resp := &service.GetKeyResponse{
		Metadata: &service.KeyMetadata{
			KeyID:       "get-key-123",
			Name:        "test-key",
			Algorithm:   service.KeyAlgAES256GCM,
			Version:     2,
			Status:      service.KeyStatusEnabled,
			Exportable:  true,
			Description: "Test description",
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
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
			expectedText: []string{"get-key-123", "test-key"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID:", "get-key-123", "Algorithm:", "Version:", "2"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Key:", "get-key-123", "v2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyGetResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatKMSGetResponse_InvalidFormat(t *testing.T) {
	resp := &service.GetKeyResponse{
		Metadata: &service.KeyMetadata{
			KeyID:     "key-123",
			Algorithm: service.KeyAlgAES256GCM,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	var buf bytes.Buffer
	err := formatKMSKeyGetResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSListResponse(t *testing.T) {
	resp := &service.ListKeysResponse{
		Keys: []*service.KeyHandle{
			{
				KeyID:     "key-1",
				Algorithm: service.KeyAlgAES256GCM,
				Version:   1,
				Status:    service.KeyStatusEnabled,
				CreatedAt: time.Now(),
			},
			{
				KeyID:     "key-2",
				Algorithm: service.KeyAlgChaCha20Poly1305,
				Version:   2,
				Status:    service.KeyStatusDisabled,
				CreatedAt: time.Now(),
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
			expectedText: []string{"key-1", "key-2", "AES-256-GCM"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"KEY ID", "ALGORITHM", "VERSION", "key-1", "key-2", "key-1"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"key-1", "key-2", "key-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyListResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatKMSListResponse_InvalidFormat(t *testing.T) {
	resp := &service.ListKeysResponse{
		Keys: []*service.KeyHandle{},
	}

	var buf bytes.Buffer
	err := formatKMSKeyListResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSRotateResponse(t *testing.T) {
	resp := &service.RotateKeyResponse{
		KeyID:      "rotated-key-123",
		OldVersion: 1,
		NewVersion: 2,
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
			expectedText: []string{"rotated-key-123", "old_version", "new_version"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID:", "rotated-key-123", "Old Version:", "1", "New Version:", "2"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Key rotated:", "rotated-key-123", "v1", "v2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyRotateResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatKMSRotateResponse_InvalidFormat(t *testing.T) {
	resp := &service.RotateKeyResponse{
		KeyID:      "key-123",
		OldVersion: 1,
		NewVersion: 2,
		Timestamp:  time.Now(),
	}

	var buf bytes.Buffer
	err := formatKMSKeyRotateResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSDeleteResponse(t *testing.T) {
	resp := &service.DeleteKeyResponse{
		KeyID:        "deleted-key-123",
		Status:       service.KeyStatusDeleted,
		DeletionDate: time.Now().Add(30 * 24 * time.Hour),
		Timestamp:    time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"deleted-key-123"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID:", "deleted-key-123"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"deleted-key-123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyDeleteResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatKMSDeleteResponse_InvalidFormat(t *testing.T) {
	resp := &service.DeleteKeyResponse{
		KeyID:     "key-123",
		Status:    service.KeyStatusDeleted,
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatKMSKeyDeleteResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

// Tests for --version flag on enable/disable commands

func TestKMSKeyEnableCommandFlags(t *testing.T) {
	cmd := newKMSKeyEnableCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "enable <key-name>", cmd.Use)

	versionFlag := cmd.Flags().Lookup("version")
	assert.NotNil(t, versionFlag)
	assert.Equal(t, "0", versionFlag.DefValue)
}

func TestKMSKeyEnableCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "enable without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "enable all versions",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "enable specific version",
			args:        []string{"test-key", "--version", "2"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "enable version 0 (all)",
			args:        []string{"test-key", "--version", "0"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyEnableCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestKMSKeyDisableCommandFlags(t *testing.T) {
	cmd := newKMSKeyDisableCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "disable <key-name>", cmd.Use)

	versionFlag := cmd.Flags().Lookup("version")
	assert.NotNil(t, versionFlag)
	assert.Equal(t, "0", versionFlag.DefValue)
}

func TestKMSKeyDisableCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "disable without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "disable all versions",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "disable specific version",
			args:        []string{"test-key", "--version", "2"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "disable version 0 (all)",
			args:        []string{"test-key", "--version", "0"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyDisableCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Tests for --grace-period flag on rotate command

func TestKMSKeyRotateCommandFlags(t *testing.T) {
	cmd := newKMSKeyRotateCommand()
	assert.NotNil(t, cmd)

	gracePeriodFlag := cmd.Flags().Lookup("grace-period")
	assert.NotNil(t, gracePeriodFlag)
	assert.Equal(t, "", gracePeriodFlag.DefValue)
}

func TestKMSKeyRotateWithGracePeriod(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "rotate with 24h grace period",
			args:        []string{"test-key", "--grace-period", "24h"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "rotate with 7d grace period",
			args:        []string{"test-key", "--grace-period", "7d"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "rotate with 30m grace period",
			args:        []string{"test-key", "--grace-period", "30m"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "rotate with invalid grace period",
			args:        []string{"test-key", "--grace-period", "invalid"},
			expectError: true,
			errorMsg:    "parseGracePeriod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyRotateCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseGracePeriod(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    time.Duration
		expectError bool
	}{
		{
			name:        "parse hours",
			input:       "24h",
			expected:    24 * time.Hour,
			expectError: false,
		},
		{
			name:        "parse minutes",
			input:       "30m",
			expected:    30 * time.Minute,
			expectError: false,
		},
		{
			name:        "parse days",
			input:       "7d",
			expected:    7 * 24 * time.Hour,
			expectError: false,
		},
		{
			name:        "parse seconds",
			input:       "60s",
			expected:    60 * time.Second,
			expectError: false,
		},
		{
			name:        "parse complex duration",
			input:       "1h30m",
			expected:    90 * time.Minute,
			expectError: false,
		},
		{
			name:        "invalid duration",
			input:       "invalid",
			expected:    0,
			expectError: true,
		},
		{
			name:        "empty string",
			input:       "",
			expected:    0,
			expectError: false, // Empty string returns (0, nil)
		},
		{
			name:        "negative duration",
			input:       "-1h",
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseGracePeriod(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestKMSKeyVersionsCommandFlags(t *testing.T) {
	cmd := newKMSKeyVersionsCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "versions <key-name>", cmd.Use)

	limitFlag := cmd.Flags().Lookup("limit")
	assert.NotNil(t, limitFlag)
	assert.Equal(t, "0", limitFlag.DefValue)
}

func TestKMSKeyVersionsCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "versions without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "versions with key name",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "versions with limit",
			args:        []string{"test-key", "--limit", "10"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newKMSKeyVersionsCommand()
			cmd.SetArgs(tt.args)

			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCLIOutputFormatParsingExtended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    cli.OutputFormat
		wantErr bool
	}{
		{"json_lower", "json", cli.OutputFormatJSON, false},
		{"yaml_lower", "yaml", cli.OutputFormatYAML, false},
		{"table_lower", "table", cli.OutputFormatTable, false},
		{"JSON_upper", "JSON", cli.OutputFormatJSON, false},
		{"YAML_upper", "YAML", cli.OutputFormatYAML, false},
		{"TABLE_upper", "TABLE", cli.OutputFormatTable, false},
		{"mixed_case_Json", "Json", cli.OutputFormatJSON, false},
		{"empty_string", "", cli.OutputFormatRaw, false}, // Empty string returns Raw format
		{"raw_format", "raw", cli.OutputFormatRaw, false},
		{"invalid_xml", "xml", "", true},
		{"invalid_csv", "csv", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := cli.ParseOutputFormat(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// =============================================================================
// Mock Client Extended Coverage Tests
// =============================================================================

func TestMockKMSClientAllMethodsExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("default_implementations_extended", func(t *testing.T) {
		t.Parallel()
		client := &MockKMSClient{}

		// Test methods that return nil by default
		resp1, err := client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp1) // Returns nil by default

		resp2, err := client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp2) // Returns nil by default

		resp3, err := client.KMSExportKey(ctx, &service.ExportKeyRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp3) // Returns nil by default

		resp4, err := client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp4) // Returns nil by default

		resp5, err := client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp5) // Returns nil by default

		resp6, err := client.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{})
		assert.NoError(t, err)
		assert.Nil(t, resp6) // Returns nil by default
	})

	t.Run("custom_error_handling_extended", func(t *testing.T) {
		t.Parallel()

		testErr := errors.Newf(errors.ErrInternal, "test", "test error")

		client := &MockKMSClient{
			KMSUpdateKeyStatusFunc: func(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
				return nil, testErr
			},
			KMSListKeyVersionsFunc: func(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
				return nil, testErr
			},
			KMSExportKeyFunc: func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
				return nil, testErr
			},
		}

		_, err := client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{})
		assert.Error(t, err)
		assert.Equal(t, testErr, err)

		_, err = client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{})
		assert.Error(t, err)
		assert.Equal(t, testErr, err)

		_, err = client.KMSExportKey(ctx, &service.ExportKeyRequest{})
		assert.Error(t, err)
		assert.Equal(t, testErr, err)
	})
}

func TestMockDEKClientAllMethodsExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("custom_success_responses", func(t *testing.T) {
		t.Parallel()

		client := &MockDEKClient{
			KMSCreateDEKFunc: func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
				return &service.CreateDEKResponse{
					DEK: &service.DEKMetadata{
						Path:      req.Path,
						Version:   1,
						Algorithm: "AES-256-GCM",
					},
				}, nil
			},
			KMSGetDEKFunc: func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
				return &service.GetDEKResponse{
					DEK: &service.DEKMetadata{
						Path:    req.Path,
						Version: 1,
					},
				}, nil
			},
			KMSListDEKsFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
				return &service.ListDEKsResponse{
					DEKs: []*service.DEKMetadata{
						{Path: "/data/key1", Version: 1},
						{Path: "/data/key2", Version: 2},
					},
				}, nil
			},
			KMSRotateDEKFunc: func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
				return &service.RotateDEKResponse{
					Path:       req.Path,
					OldVersion: 1,
					NewVersion: 2,
					Timestamp:  time.Now(),
				}, nil
			},
			KMSDeleteDEKFunc: func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
				return &service.DeleteDEKResponse{}, nil
			},
			KMSGetDEKStatusFunc: func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
				return &service.GetDEKStatusResponse{
					DEK: &service.DEKMetadata{
						Path:    req.Path,
						Version: 1,
					},
				}, nil
			},
			KMSSetDefaultDEKFunc: func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
				return &service.SetDefaultDEKResponse{}, nil
			},
			KMSSetDEKSafetyFunc: func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
				return &service.SetDEKSafetyResponse{}, nil
			},
		}

		// Test create
		createResp, err := client.KMSCreateDEK(ctx, &service.CreateDEKRequest{Path: "/test/key"})
		require.NoError(t, err)
		assert.Equal(t, "/test/key", createResp.DEK.Path)
		assert.Equal(t, uint64(1), createResp.DEK.Version) // DEK version is uint64

		// Test get
		getResp, err := client.KMSGetDEK(ctx, &service.GetDEKRequest{Path: "/test/key"})
		require.NoError(t, err)
		assert.Equal(t, "/test/key", getResp.DEK.Path)

		// Test list
		listResp, err := client.KMSListDEKs(ctx, &service.ListDEKsRequest{})
		require.NoError(t, err)
		assert.Len(t, listResp.DEKs, 2)

		// Test rotate
		rotateResp, err := client.KMSRotateDEK(ctx, &service.RotateDEKRequest{Path: "/test/key"})
		require.NoError(t, err)
		assert.Equal(t, uint64(2), rotateResp.NewVersion)

		// Test delete
		deleteResp, err := client.KMSDeleteDEK(ctx, &service.DeleteDEKRequest{Path: "/test/key"})
		require.NoError(t, err)
		assert.NotNil(t, deleteResp)

		// Test status
		statusResp, err := client.KMSGetDEKStatus(ctx, &service.GetDEKStatusRequest{Path: "/test/key"})
		require.NoError(t, err)
		assert.NotNil(t, statusResp.DEK)

		// Test set default
		defaultResp, err := client.KMSSetDefaultDEK(ctx, &service.SetDefaultDEKRequest{Path: "/test/key"})
		require.NoError(t, err)
		assert.NotNil(t, defaultResp)

		// Test set safety
		safetyResp, err := client.KMSSetDEKSafety(ctx, &service.SetDEKSafetyRequest{Path: "/test/key"})
		require.NoError(t, err)
		assert.NotNil(t, safetyResp)
	})
}

func TestMockSecretsClientExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("with_custom_implementations", func(t *testing.T) {
		t.Parallel()

		client := &MockSecretsClient{
			StoreSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				return &service.StoreSecretResponse{
					SecretID: req.Name,
					Version:  1,
				}, nil
			},
			GetSecretFunc: func(ctx context.Context, req *service.GetSecretRequest) (*service.GetSecretResponse, error) {
				return &service.GetSecretResponse{
					SecretID: req.SecretID,
					Data:     []byte("secret-value"),
					Version:  1,
				}, nil
			},
			ListSecretsFunc: func(ctx context.Context, req *service.ListSecretsRequest) (*service.ListSecretsResponse, error) {
				return &service.ListSecretsResponse{
					Secrets: []*service.Secret{
						{SecretID: "secret1"},
						{SecretID: "secret2"},
					},
				}, nil
			},
			DeleteSecretFunc: func(ctx context.Context, req *service.DeleteSecretRequest) (*service.DeleteSecretResponse, error) {
				return &service.DeleteSecretResponse{}, nil
			},
			ListSecretVersionsFunc: func(ctx context.Context, req *service.ListSecretVersionsRequest) (*service.ListSecretVersionsResponse, error) {
				return &service.ListSecretVersionsResponse{
					Versions: []*service.SecretVersion{
						{Version: 1, CreatedAt: time.Now()},
						{Version: 2, CreatedAt: time.Now()},
					},
				}, nil
			},
			RollbackSecretFunc: func(ctx context.Context, req *service.RollbackSecretRequest) (*service.RollbackSecretResponse, error) {
				return &service.RollbackSecretResponse{
					NewVersion: req.TargetVersion,
				}, nil
			},
			RotateSecretFunc: func(ctx context.Context, req *service.RotateSecretRequest) (*service.RotateSecretResponse, error) {
				return &service.RotateSecretResponse{
					SecretID:   req.SecretID,
					NewVersion: 2,
				}, nil
			},
			UpdateSecretStatusFunc: func(ctx context.Context, req *service.UpdateSecretStatusRequest) (*service.UpdateSecretStatusResponse, error) {
				return &service.UpdateSecretStatusResponse{}, nil
			},
			GetSecretsByPrefixFunc: func(ctx context.Context, req *service.GetSecretsByPrefixRequest) (*service.GetSecretsByPrefixResponse, error) {
				return &service.GetSecretsByPrefixResponse{
					Secrets: map[string]map[string][]byte{
						req.Prefix + "/key1": {"data": []byte("value1")},
					},
				}, nil
			},
			WatchSecretsByPrefixFunc: func(ctx context.Context, req *service.WatchSecretsRequest) (<-chan *service.SecretChange, error) {
				ch := make(chan *service.SecretChange)
				close(ch)
				return ch, nil
			},
			CloseFunc: func() error {
				return nil
			},
		}

		// Test store
		storeResp, err := client.StoreSecret(ctx, &service.StoreSecretRequest{Name: "test-secret"})
		require.NoError(t, err)
		assert.Equal(t, "test-secret", storeResp.SecretID)
		assert.Equal(t, uint64(1), storeResp.Version)

		// Test get
		getResp, err := client.GetSecret(ctx, &service.GetSecretRequest{SecretID: "test-secret"})
		require.NoError(t, err)
		assert.Equal(t, "test-secret", getResp.SecretID)

		// Test list
		listResp, err := client.ListSecrets(ctx, &service.ListSecretsRequest{})
		require.NoError(t, err)
		assert.Len(t, listResp.Secrets, 2)

		// Test delete
		deleteResp, err := client.DeleteSecret(ctx, &service.DeleteSecretRequest{SecretID: "test-secret"})
		require.NoError(t, err)
		assert.NotNil(t, deleteResp)

		// Test list versions
		versionsResp, err := client.ListSecretVersions(ctx, &service.ListSecretVersionsRequest{SecretID: "test-secret"})
		require.NoError(t, err)
		assert.Len(t, versionsResp.Versions, 2)

		// Test rollback
		rollbackResp, err := client.RollbackSecret(ctx, &service.RollbackSecretRequest{SecretID: "test-secret", TargetVersion: 1})
		require.NoError(t, err)
		assert.Equal(t, uint64(1), rollbackResp.NewVersion)

		// Test rotate
		rotateResp, err := client.RotateSecret(ctx, &service.RotateSecretRequest{SecretID: "test-secret"})
		require.NoError(t, err)
		assert.Equal(t, uint64(2), rotateResp.NewVersion)

		// Test update status
		statusResp, err := client.UpdateSecretStatus(ctx, &service.UpdateSecretStatusRequest{SecretID: "test-secret"})
		require.NoError(t, err)
		assert.NotNil(t, statusResp)

		// Test get by prefix
		prefixResp, err := client.GetSecretsByPrefix(ctx, &service.GetSecretsByPrefixRequest{Prefix: "db/"})
		require.NoError(t, err)
		assert.Len(t, prefixResp.Secrets, 1)

		// Test watch
		watchCh, err := client.WatchSecretsByPrefix(ctx, &service.WatchSecretsRequest{Prefix: "db/"})
		require.NoError(t, err)
		assert.NotNil(t, watchCh)

		// Test close
		err = client.Close()
		assert.NoError(t, err)
	})
}

func TestMockSealClientExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("custom_implementations", func(t *testing.T) {
		t.Parallel()

		client := &MockSealClient{
			SealStatusFunc: func(ctx context.Context) (*service.SealStatusResponse, error) {
				return &service.SealStatusResponse{
					Sealed:         false,
					Initialized:    true,
					Threshold:      3,
					TotalShares:    5,
					SharesProvided: 3,
				}, nil
			},
			SealInitFunc: func(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
				// Share material is never returned (qrdb share-free
				// contract): officers claim shares out-of-band.
				return &service.SealInitResponse{
					RootToken: "root-token",
					Version:   1,
				}, nil
			},
			UnsealFunc: func(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
				return &service.UnsealResponse{
					Sealed:         false,
					SharesProvided: 3,
				}, nil
			},
			SealFunc: func(ctx context.Context) error {
				return nil
			},
			RekeyFunc: func(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
				return &service.RekeyResponse{
					Version:   2,
					Timestamp: time.Now(),
				}, nil
			},
			CloseFunc: func() error {
				return nil
			},
		}

		// Test status
		statusResp, err := client.SealStatus(ctx)
		require.NoError(t, err)
		assert.False(t, statusResp.Sealed)
		assert.True(t, statusResp.Initialized)

		// Test init (share-free: response carries no share material)
		initResp, err := client.SealInit(ctx, &service.SealInitRequest{TotalShares: 5, Threshold: 3})
		require.NoError(t, err)
		assert.Equal(t, "root-token", initResp.RootToken)
		assert.Equal(t, uint64(1), initResp.Version)

		// Test unseal
		unsealResp, err := client.Unseal(ctx, &service.UnsealRequest{Share: []byte("share")})
		require.NoError(t, err)
		assert.False(t, unsealResp.Sealed)

		// Test seal
		err = client.Seal(ctx)
		assert.NoError(t, err)

		// Test rekey (share-free: response carries no share material)
		rekeyResp, err := client.Rekey(ctx, &service.RekeyRequest{})
		require.NoError(t, err)
		assert.Equal(t, uint64(2), rekeyResp.Version)

		// Test close
		err = client.Close()
		assert.NoError(t, err)
	})
}

func TestMockCryptoClientExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("comprehensive_operations", func(t *testing.T) {
		t.Parallel()

		client := &MockCryptoClient{
			KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
				return &service.EncryptResponse{
					KeyID:      req.KeyID,
					Ciphertext: []byte("encrypted-" + string(req.Plaintext)),
				}, nil
			},
			KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
				return &service.DecryptResponse{
					KeyID:     req.KeyID,
					Plaintext: []byte("decrypted"),
				}, nil
			},
			CloseFunc: func() error {
				return nil
			},
		}

		// Test encrypt
		encResp, err := client.KMSEncrypt(ctx, &service.EncryptRequest{KeyID: "key1", Plaintext: []byte("hello")})
		require.NoError(t, err)
		assert.Equal(t, "key1", encResp.KeyID)
		assert.Contains(t, string(encResp.Ciphertext), "encrypted")

		// Test decrypt
		decResp, err := client.KMSDecrypt(ctx, &service.DecryptRequest{KeyID: "key1", Ciphertext: []byte("encrypted")})
		require.NoError(t, err)
		assert.Equal(t, "key1", decResp.KeyID)

		// Test close
		err = client.Close()
		assert.NoError(t, err)
	})

	t.Run("default_method_behavior", func(t *testing.T) {
		t.Parallel()
		client := &MockCryptoClient{}

		// Test sign with default
		signResp, err := client.KMSSign(ctx, &service.SignRequest{KeyID: "key1", Message: []byte("data")})
		require.NoError(t, err)
		assert.Equal(t, "key1", signResp.KeyID)

		// Test verify with default
		verifyResp, err := client.KMSVerify(ctx, &service.VerifyRequest{KeyID: "key1"})
		require.NoError(t, err)
		assert.True(t, verifyResp.Valid)

		// Test hash with default
		hashResp, err := client.KMSHash(ctx, &service.HashRequest{Algorithm: "sha256"})
		require.NoError(t, err)
		assert.Equal(t, service.HashAlgorithm("sha256"), hashResp.Algorithm)

		// Test generate HMAC with default
		hmacResp, err := client.KMSGenerateHMAC(ctx, &service.HMACRequest{KeyID: "key1"})
		require.NoError(t, err)
		assert.Equal(t, "key1", hmacResp.KeyID)

		// Test verify HMAC with default
		verifyHmacResp, err := client.KMSVerifyHMAC(ctx, &service.VerifyHMACRequest{KeyID: "key1"})
		require.NoError(t, err)
		assert.True(t, verifyHmacResp.Valid)

		// Test rewrap with default
		rewrapResp, err := client.KMSRewrap(ctx, &service.RewrapRequest{KeyID: "key1"})
		require.NoError(t, err)
		assert.Equal(t, "key1", rewrapResp.KeyID)

		// Test generate random with default
		randomResp, err := client.KMSGenerateRandom(ctx, &service.GenerateRandomRequest{NumBytes: 32})
		require.NoError(t, err)
		assert.Len(t, randomResp.RandomBytes, 32)
	})
}

func TestMockBackendClientExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("custom_implementations", func(t *testing.T) {
		t.Parallel()

		client := &MockBackendClient{
			KMSBackendStatusFunc: func(ctx context.Context) (*service.BackendStatusResponse, error) {
				return &service.BackendStatusResponse{
					BackendType: "pebble",
					Status:      "healthy",
				}, nil
			},
			KMSBackendTestFunc: func(ctx context.Context) (*service.BackendTestResponse, error) {
				return &service.BackendTestResponse{
					Success: true,
					Message: "Backend operational",
				}, nil
			},
			CloseFunc: func() error {
				return nil
			},
		}

		// Test status
		statusResp, err := client.KMSBackendStatus(ctx)
		require.NoError(t, err)
		assert.Equal(t, "pebble", statusResp.BackendType)
		assert.Equal(t, "healthy", statusResp.Status)

		// Test backend test
		testResp, err := client.KMSBackendTest(ctx)
		require.NoError(t, err)
		assert.True(t, testResp.Success)

		// Test close
		err = client.Close()
		assert.NoError(t, err)
	})
}

func TestMockSafetyClientExtended(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("custom_implementations", func(t *testing.T) {
		t.Parallel()

		client := &MockSafetyClient{
			KMSSafetyCheckFunc: func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
				return &service.SafetyCheckResponse{
					TotalKeysChecked: 100,
					TotalDEKsChecked: 50,
				}, nil
			},
			KMSSafetyReportFunc: func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
				return &service.SafetyReportResponse{
					TotalOperations: 1000,
				}, nil
			},
			CloseFunc: func() error {
				return nil
			},
		}

		// Test safety check
		checkResp, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		require.NoError(t, err)
		assert.Equal(t, 100, checkResp.TotalKeysChecked)

		// Test safety report
		reportResp, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		require.NoError(t, err)
		assert.Equal(t, uint64(1000), reportResp.TotalOperations)

		// Test close
		err = client.Close()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Cache Configuration Coverage Tests
// =============================================================================

func TestCacheConfigurationPathsExtended(t *testing.T) {
	t.Parallel()

	t.Run("entity_config_defaults", func(t *testing.T) {
		t.Parallel()

		cacheConfig := &config.EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities: map[string]config.EntityCacheConfig{
				"user": {
					Enabled: true,
					TTL:     10 * time.Minute,
					MaxSize: 500,
				},
			},
		}

		// Test existing entity
		userConfig := cacheConfig.GetEntityConfig("user")
		assert.True(t, userConfig.Enabled)
		assert.Equal(t, 10*time.Minute, userConfig.TTL)
		assert.Equal(t, 500, userConfig.MaxSize)

		// Test non-existing entity returns defaults
		postConfig := cacheConfig.GetEntityConfig("post")
		assert.Equal(t, cacheConfig.Enabled, postConfig.Enabled)
		assert.Equal(t, cacheConfig.DefaultTTL, postConfig.TTL)
	})
}

// =============================================================================
// Error Type Coverage Tests
// =============================================================================

func TestErrorTypeCoverageExtended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		errCode errors.ErrorCode
	}{
		{"internal", errors.ErrInternal},
		{"not_found", errors.ErrNotFound},
		{"invalid_input", errors.ErrInvalidInput},
		{"unauthorized", errors.ErrUnauthorized},
		{"unavailable", errors.ErrUnavailable},
		{"timeout", errors.ErrTimeout},
		{"conflict", errors.ErrConflict},
		{"unimplemented", errors.ErrUnimplemented},
		{"configuration", errors.ErrConfiguration},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := errors.New(tt.errCode, "test", "test message")
			assert.NotNil(t, err)
			assert.Equal(t, tt.errCode, err.Code)
			assert.Equal(t, "test", err.Op)
			assert.Contains(t, err.Error(), "test message")
		})
	}
}

func TestErrorWrappingExtended(t *testing.T) {
	t.Parallel()

	baseErr := errors.New(errors.ErrNotFound, "base", "base error")
	wrappedErr := errors.Wrapf(baseErr, errors.ErrInternal, "wrapper", "wrapping %s", "message")

	assert.NotNil(t, wrappedErr)
	assert.Contains(t, wrappedErr.Error(), "wrapping message")

	// Check that it can be cast to QBError
	var qbErr *errors.QBError
	if errors.As(wrappedErr, &qbErr) {
		assert.Equal(t, errors.ErrInternal, qbErr.Code)
		assert.Equal(t, "wrapper", qbErr.Op)
	}
}

// =============================================================================
// Logger Adapter Coverage Tests
// =============================================================================

func TestLoggerAdapterInterfaceExtended(t *testing.T) {
	t.Parallel()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stderr",
		Component: "test",
	}

	testLogger, err := logger.New(logConfig)
	require.NoError(t, err)

	// Test the adapter function
	adapted := asInterfaceLogger(testLogger)
	assert.NotNil(t, adapted)

	// Verify it satisfies the interfaces.Logger interface
	adapted.Debug("test debug message")
	adapted.Info("test info message")
	adapted.Warn("test warn message")
	adapted.Error("test error message")

	adapted.Debugf("test %s", "debugf")
	adapted.Infof("test %s", "infof")
	adapted.Warnf("test %s", "warnf")
	adapted.Errorf("test %s", "errorf")
}

// =============================================================================
// CLI Formatter Coverage Tests
// =============================================================================

func TestCLIFormatterCreationExtended(t *testing.T) {
	t.Parallel()

	t.Run("colored_formatter", func(t *testing.T) {
		t.Parallel()
		formatter := cli.NewFormatter(true)
		assert.NotNil(t, formatter)
	})

	t.Run("non_colored_formatter", func(t *testing.T) {
		t.Parallel()
		formatter := cli.NewFormatter(false)
		assert.NotNil(t, formatter)
	})
}

// =============================================================================
// Time Parsing Coverage Tests
// =============================================================================

func TestTimeParsingExtended(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"RFC3339", "2025-01-15T10:30:00Z", false},
		{"RFC3339_with_offset", "2025-01-15T10:30:00-05:00", false},
		{"invalid_format", "2025/01/15", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := time.Parse(time.RFC3339, tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Context Timeout Coverage Tests
// =============================================================================

func TestContextTimeoutHandlingExtended(t *testing.T) {
	t.Parallel()

	t.Run("normal_operation", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		select {
		case <-ctx.Done():
			t.Error("context should not be done")
		default:
			// Expected - context is not done
		}
	})

	t.Run("immediate_cancel", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cancel() // Immediately cancel

		select {
		case <-ctx.Done():
			assert.Equal(t, context.Canceled, ctx.Err())
		default:
			t.Error("context should be done")
		}
	})
}

// =============================================================================
// Mock Client Interface Verification Tests
// =============================================================================

func TestInterfaceComplianceExtended(t *testing.T) {
	t.Parallel()

	t.Run("KMSClient", func(t *testing.T) {
		t.Parallel()
		var _ KMSClient = (*MockKMSClient)(nil)
	})

	t.Run("DEKClient", func(t *testing.T) {
		t.Parallel()
		var _ DEKClient = (*MockDEKClient)(nil)
	})

	t.Run("SecretsClient", func(t *testing.T) {
		t.Parallel()
		var _ SecretsClient = (*MockSecretsClient)(nil)
	})

	t.Run("SealClient", func(t *testing.T) {
		t.Parallel()
		var _ SealClient = (*MockSealClient)(nil)
	})

	t.Run("CryptoClient", func(t *testing.T) {
		t.Parallel()
		var _ CryptoClient = (*MockCryptoClient)(nil)
	})

	t.Run("BackendClient", func(t *testing.T) {
		t.Parallel()
		var _ BackendClient = (*MockBackendClient)(nil)
	})

	t.Run("SafetyClient", func(t *testing.T) {
		t.Parallel()
		var _ SafetyClient = (*MockSafetyClient)(nil)
	})
}

func TestFormatDEKCreateResponse(t *testing.T) {
	resp := &service.CreateDEKResponse{
		DEK: &service.DEKMetadata{
			Path:      "/tenants/acme",
			Algorithm: "AES-256-GCM",
			Version:   1,
			Status:    "active",
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme", "AES-256-GCM"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Path:", "/tenants/acme", "Algorithm:", "AES-256-GCM", "Version:", "Status:"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"DEK created:", "/tenants/acme", "v1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDEKCreateResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKCreateResponse_InvalidFormat(t *testing.T) {
	resp := &service.CreateDEKResponse{
		DEK: &service.DEKMetadata{Path: "/test"},
	}
	var buf bytes.Buffer
	err := formatDEKCreateResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDEKGetResponse(t *testing.T) {
	resp := &service.GetDEKResponse{
		DEK: &service.DEKMetadata{
			Path:      "/tenants/acme",
			Algorithm: "AES-256-GCM",
			Version:   2,
			Status:    "active",
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme", "AES-256-GCM"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Path:", "/tenants/acme", "Algorithm:", "Version:"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"DEK:", "/tenants/acme", "v2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDEKGetResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKGetResponse_InvalidFormat(t *testing.T) {
	resp := &service.GetDEKResponse{
		DEK: &service.DEKMetadata{Path: "/test"},
	}
	var buf bytes.Buffer
	err := formatDEKGetResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDEKListResponse(t *testing.T) {
	resp := &service.ListDEKsResponse{
		DEKs: []*service.DEKMetadata{
			{Path: "/tenants/acme", Algorithm: "AES-256-GCM", Version: 1, Status: "active"},
			{Path: "/tenants/beta", Algorithm: "CHACHA20-POLY1305", Version: 3, Status: "active"},
		},
		Timestamp: time.Now(),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme", "/tenants/beta"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"PATH", "ALGORITHM", "VERSION", "STATUS", "/tenants/acme", "Total: 2 DEKs"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"/tenants/acme (v1)", "/tenants/beta (v3)"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDEKListResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKListResponse_InvalidFormat(t *testing.T) {
	resp := &service.ListDEKsResponse{}
	var buf bytes.Buffer
	err := formatDEKListResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDEKRotateResponse(t *testing.T) {
	resp := &service.RotateDEKResponse{
		Path:       "/tenants/acme",
		OldVersion: 1,
		NewVersion: 2,
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme", "old_version", "new_version"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Path:", "/tenants/acme", "Old Version:", "New Version:"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"DEK rotated:", "/tenants/acme", "v1", "v2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDEKRotateResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKRotateResponse_InvalidFormat(t *testing.T) {
	resp := &service.RotateDEKResponse{Path: "/test"}
	var buf bytes.Buffer
	err := formatDEKRotateResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDEKDeleteResponse(t *testing.T) {
	resp := &service.DeleteDEKResponse{
		Path:    "/tenants/acme",
		Deleted: true,
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme", "true"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Path:", "/tenants/acme", "Deleted:", "true"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"DEK deleted:", "/tenants/acme"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDEKDeleteResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKDeleteResponse_InvalidFormat(t *testing.T) {
	resp := &service.DeleteDEKResponse{Path: "/test"}
	var buf bytes.Buffer
	err := formatDEKDeleteResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDEKStatusResponse(t *testing.T) {
	resp := &service.GetDEKStatusResponse{
		DEK: &service.DEKMetadata{
			Path:       "/tenants/acme",
			Status:     "active",
			UsageCount: 500,
			BytesUsed:  1024000,
		},
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme", "active"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Path:", "/tenants/acme", "Status:", "active", "Usage Count:", "500", "Bytes Used:"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"DEK:", "/tenants/acme", "active"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDEKStatusResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKStatusResponse_InvalidFormat(t *testing.T) {
	resp := &service.GetDEKStatusResponse{
		DEK: &service.DEKMetadata{Path: "/test"},
	}
	var buf bytes.Buffer
	err := formatDEKStatusResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDEKSetDefaultResponse(t *testing.T) {
	tests := []struct {
		name            string
		format          string
		previousDefault string
		contains        []string
	}{
		{
			name:            "table_with_previous",
			format:          "table",
			previousDefault: "/old/default",
			contains:        []string{"Path:", "/tenants/acme", "Previous Default:", "/old/default"},
		},
		{
			name:     "table_without_previous",
			format:   "table",
			contains: []string{"Path:", "/tenants/acme"},
		},
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Default DEK set:", "/tenants/acme"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := &service.SetDefaultDEKResponse{
				Path:            "/tenants/acme",
				PreviousDefault: tc.previousDefault,
			}
			var buf bytes.Buffer
			err := formatDEKSetDefaultResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKSetDefaultResponse_InvalidFormat(t *testing.T) {
	resp := &service.SetDefaultDEKResponse{Path: "/test"}
	var buf bytes.Buffer
	err := formatDEKSetDefaultResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatDEKSetSafetyResponse(t *testing.T) {
	resp := &service.SetDEKSafetyResponse{
		Path: "/tenants/acme",
		Config: &service.DEKSafetyConfig{
			MaxOperations: 1000000,
			MaxBytes:      1073741824,
		},
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"/tenants/acme", "1000000", "1073741824"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Path:", "/tenants/acme", "Max Operations:", "1000000", "Max Bytes:", "1073741824"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Safety limits set for DEK:", "/tenants/acme"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatDEKSetSafetyResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatDEKSetSafetyResponse_InvalidFormat(t *testing.T) {
	resp := &service.SetDEKSafetyResponse{
		Path:   "/test",
		Config: &service.DEKSafetyConfig{},
	}
	var buf bytes.Buffer
	err := formatDEKSetSafetyResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Key Export/Unwrap/Safety Format Functions
// =============================================================================

func TestFormatKMSKeyExportResponse(t *testing.T) {
	resp := &service.ExportKeyResponse{
		KeyID:      "export-key-1",
		WrappedKey: []byte("wrapped-key-data"),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"export-key-1"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Key ID:", "export-key-1", "Wrapped Key:"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Key exported:", "export-key-1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyExportResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatKMSKeyExportResponse_InvalidFormat(t *testing.T) {
	resp := &service.ExportKeyResponse{KeyID: "test"}
	var buf bytes.Buffer
	err := formatKMSKeyExportResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSKeyUnwrapResponse(t *testing.T) {
	resp := &service.UnwrapKeyResponse{
		KeyID:     "unwrapped-key-1",
		Algorithm: "AES-256-GCM",
		Status:    "enabled",
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"unwrapped-key-1", "AES-256-GCM"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Key ID:", "unwrapped-key-1", "Algorithm:", "AES-256-GCM", "Status:", "enabled"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Key unwrapped:", "unwrapped-key-1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyUnwrapResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatKMSKeyUnwrapResponse_InvalidFormat(t *testing.T) {
	resp := &service.UnwrapKeyResponse{KeyID: "test"}
	var buf bytes.Buffer
	err := formatKMSKeyUnwrapResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSKeySetSafetyResponse(t *testing.T) {
	resp := &service.SetKeySafetyResponse{
		KeyID: "safety-key-1",
		Config: service.AEADSafetyConfigDTO{
			MaxOperations: 500000,
			MaxBytes:      536870912,
		},
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"safety-key-1", "500000", "536870912"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Key ID:", "safety-key-1", "Max Operations:", "500000", "Max Bytes:", "536870912"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Safety limits set for key:", "safety-key-1"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeySetSafetyResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatKMSKeySetSafetyResponse_InvalidFormat(t *testing.T) {
	resp := &service.SetKeySafetyResponse{KeyID: "test"}
	var buf bytes.Buffer
	err := formatKMSKeySetSafetyResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSKeyStatusResponse(t *testing.T) {
	resp := &service.UpdateKeyStatusResponse{
		KeyID:     "status-key-1",
		OldStatus: "disabled",
		NewStatus: "enabled",
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"status-key-1", "disabled", "enabled"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Key ID:", "status-key-1", "Old Status:", "disabled", "New Status:", "enabled"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Key status updated:", "status-key-1", "disabled", "enabled"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyStatusResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatKMSKeyStatusResponse_InvalidFormat(t *testing.T) {
	resp := &service.UpdateKeyStatusResponse{KeyID: "test"}
	var buf bytes.Buffer
	err := formatKMSKeyStatusResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatKMSKeyVersionsResponse(t *testing.T) {
	now := time.Now()
	resp := &service.ListKeyVersionsResponse{
		Versions: []*service.KeyVersionInfo{
			{Version: 1, Status: "enabled", CreatedAt: now},
			{Version: 2, Status: "disabled", CreatedAt: now},
		},
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "json_format",
			format:   "json",
			contains: []string{"enabled", "disabled"},
		},
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"VERSION", "STATUS", "CREATED", "enabled", "disabled"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"v1 - enabled", "v2 - disabled"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSKeyVersionsResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatKMSKeyVersionsResponse_InvalidFormat(t *testing.T) {
	resp := &service.ListKeyVersionsResponse{}
	var buf bytes.Buffer
	err := formatKMSKeyVersionsResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Backend Format Functions
// =============================================================================

func TestFormatBackendStatusResponse(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		resp     *service.BackendStatusResponse
		contains []string
	}{
		{
			name:   "json_format",
			format: "json",
			resp: &service.BackendStatusResponse{
				BackendType: "tpm2",
				Status:      "healthy",
				Device:      "/dev/tpm0",
			},
			contains: []string{"tpm2", "healthy", "/dev/tpm0"},
		},
		{
			name:   "table_with_device_and_region",
			format: "table",
			resp: &service.BackendStatusResponse{
				BackendType: "awskms",
				Status:      "healthy",
				Device:      "/dev/tpm0",
				Region:      "us-east-1",
			},
			contains: []string{"Backend:", "awskms", "Status:", "healthy", "Device:", "/dev/tpm0", "Region:", "us-east-1"},
		},
		{
			name:   "table_without_optional_fields",
			format: "table",
			resp: &service.BackendStatusResponse{
				BackendType: "software",
				Status:      "healthy",
			},
			contains: []string{"Backend:", "software", "Status:", "healthy"},
		},
		{
			name:   "raw_format",
			format: "raw",
			resp: &service.BackendStatusResponse{
				BackendType: "tpm2",
				Status:      "healthy",
			},
			contains: []string{"Backend:", "tpm2", "healthy"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatBackendStatusResponse(tc.resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatBackendStatusResponse_InvalidFormat(t *testing.T) {
	resp := &service.BackendStatusResponse{BackendType: "test"}
	var buf bytes.Buffer
	err := formatBackendStatusResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatBackendTestResponse(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		resp     *service.BackendTestResponse
		contains []string
	}{
		{
			name:   "json_format",
			format: "json",
			resp: &service.BackendTestResponse{
				Success:  true,
				Duration: 50 * time.Millisecond,
			},
			contains: []string{"true"},
		},
		{
			name:   "table_success_with_message",
			format: "table",
			resp: &service.BackendTestResponse{
				Success:  true,
				Duration: 25 * time.Millisecond,
				Message:  "All checks passed",
			},
			contains: []string{"Success:", "true", "Duration:", "Message:", "All checks passed"},
		},
		{
			name:   "table_success_without_message",
			format: "table",
			resp: &service.BackendTestResponse{
				Success:  true,
				Duration: 25 * time.Millisecond,
			},
			contains: []string{"Success:", "true"},
		},
		{
			name:   "raw_success",
			format: "raw",
			resp: &service.BackendTestResponse{
				Success:  true,
				Duration: 25 * time.Millisecond,
			},
			contains: []string{"Backend test passed"},
		},
		{
			name:   "raw_failure",
			format: "raw",
			resp: &service.BackendTestResponse{
				Success: false,
				Message: "connection refused",
			},
			contains: []string{"Backend test failed:", "connection refused"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatBackendTestResponse(tc.resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatBackendTestResponse_InvalidFormat(t *testing.T) {
	resp := &service.BackendTestResponse{}
	var buf bytes.Buffer
	err := formatBackendTestResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

// =============================================================================
// Safety Format Functions
// =============================================================================

func TestFormatSafetyCheckResponse(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		resp     *service.SafetyCheckResponse
		contains []string
	}{
		{
			name:   "json_format",
			format: "json",
			resp: &service.SafetyCheckResponse{
				Items: []service.SafetyCheckItem{
					{ID: "key-1", Type: "key", UsagePercent: 95.5, OperationCount: 950000, BytesProcessed: 1000000000},
				},
				ThresholdPercent: 80,
				TotalKeysChecked: 10,
				TotalDEKsChecked: 5,
			},
			contains: []string{"key-1", "95.5"},
		},
		{
			name:   "table_with_items",
			format: "table",
			resp: &service.SafetyCheckResponse{
				Items: []service.SafetyCheckItem{
					{ID: "key-1", Type: "key", UsagePercent: 95.5, OperationCount: 950000, BytesProcessed: 1000000000},
					{ID: "dek-1", Type: "dek", UsagePercent: 82.3, OperationCount: 823000, BytesProcessed: 500000000},
				},
				ThresholdPercent: 80,
				TotalKeysChecked: 10,
				TotalDEKsChecked: 5,
			},
			contains: []string{"ID", "TYPE", "key-1", "dek-1", "Total Checked:", "10 keys", "5 DEKs"},
		},
		{
			name:   "table_empty_items",
			format: "table",
			resp: &service.SafetyCheckResponse{
				Items:            []service.SafetyCheckItem{},
				ThresholdPercent: 80,
				TotalKeysChecked: 10,
				TotalDEKsChecked: 5,
			},
			contains: []string{"No keys or DEKs exceeding 80% usage threshold"},
		},
		{
			name:   "raw_with_items",
			format: "raw",
			resp: &service.SafetyCheckResponse{
				Items: []service.SafetyCheckItem{
					{ID: "key-1", Type: "key", UsagePercent: 95.5},
				},
			},
			contains: []string{"key-1 (key): 95.5% usage"},
		},
		{
			name:   "raw_empty_items",
			format: "raw",
			resp: &service.SafetyCheckResponse{
				Items: []service.SafetyCheckItem{},
			},
			contains: []string{"No items exceeding threshold"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSafetyCheckResponse(tc.resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatSafetyCheckResponse_InvalidFormat(t *testing.T) {
	resp := &service.SafetyCheckResponse{}
	var buf bytes.Buffer
	err := formatSafetyCheckResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

func TestFormatSafetyReportResponse(t *testing.T) {
	tests := []struct {
		name     string
		format   string
		resp     *service.SafetyReportResponse
		contains []string
	}{
		{
			name:   "json_format",
			format: "json",
			resp: &service.SafetyReportResponse{
				TotalOperations:     5000000,
				TotalBytesProcessed: 10000000000,
				ServerConfig: service.AEADServerConfig{
					Enforce:       true,
					MaxOperations: 10000000,
					MaxBytes:      50000000000,
				},
			},
			contains: []string{"5000000", "10000000000"},
		},
		{
			name:   "table_with_items",
			format: "table",
			resp: &service.SafetyReportResponse{
				TotalOperations:     5000000,
				TotalBytesProcessed: 10000000000,
				ServerConfig: service.AEADServerConfig{
					Enforce:       true,
					MaxOperations: 10000000,
					MaxBytes:      50000000000,
				},
				Items: []service.SafetyCheckItem{
					{ID: "key-1", Type: "key", UsagePercent: 50.0},
				},
			},
			contains: []string{"AEAD Safety Report", "Total Operations:", "Server Config:", "Enforce:", "true", "Items:", "key-1"},
		},
		{
			name:   "table_without_items",
			format: "table",
			resp: &service.SafetyReportResponse{
				TotalOperations:     0,
				TotalBytesProcessed: 0,
				ServerConfig: service.AEADServerConfig{
					Enforce: false,
				},
			},
			contains: []string{"AEAD Safety Report", "Total Operations:", "0"},
		},
		{
			name:   "raw_format",
			format: "raw",
			resp: &service.SafetyReportResponse{
				TotalOperations:     5000000,
				TotalBytesProcessed: 10000000000,
			},
			contains: []string{"Total operations: 5000000", "Total bytes: 10000000000"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatSafetyReportResponse(tc.resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatSafetyReportResponse_InvalidFormat(t *testing.T) {
	resp := &service.SafetyReportResponse{}
	var buf bytes.Buffer
	err := formatSafetyReportResponse(resp, "invalid-format", &buf)
	assert.Error(t, err)
}

// =============================================================================
// DEK Run Commands via Factory Swap
// =============================================================================

func TestRunKMSDEKGenerateCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSCreateDEKFunc: func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
			return &service.CreateDEKResponse{
				DEK: &service.DEKMetadata{
					Path:      req.Path,
					Algorithm: service.KeyAlgorithm(req.Algorithm),
					Version:   1,
					Status:    "active",
				},
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKGenerateCommand()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKGenerateCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKGenerateCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKGetCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSGetDEKFunc: func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
			return &service.GetDEKResponse{
				DEK: &service.DEKMetadata{
					Path:      req.Path,
					Algorithm: "AES-256-GCM",
					Version:   1,
					Status:    "active",
				},
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKGetCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKGetCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKGetCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKListCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSListDEKsFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
			return &service.ListDEKsResponse{
				DEKs: []*service.DEKMetadata{
					{Path: "/tenants/a", Algorithm: "AES-256-GCM", Version: 1, Status: "active"},
				},
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKListCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKListCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKListCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKRotateCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSRotateDEKFunc: func(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
			return &service.RotateDEKResponse{
				Path:       req.Path,
				OldVersion: 1,
				NewVersion: 2,
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKRotateCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKRotateCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKRotateCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKDeleteCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSDeleteDEKFunc: func(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
			return &service.DeleteDEKResponse{
				Path:    req.Path,
				Deleted: true,
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKDeleteCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKDeleteCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKDeleteCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKStatusCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSGetDEKStatusFunc: func(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
			return &service.GetDEKStatusResponse{
				DEK: &service.DEKMetadata{
					Path:       req.Path,
					Status:     "active",
					UsageCount: 100,
					BytesUsed:  10240,
				},
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKStatusCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKStatusCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKStatusCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKSetDefaultCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSSetDefaultDEKFunc: func(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
			return &service.SetDefaultDEKResponse{
				Path:            req.Path,
				PreviousDefault: "/old/default",
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKSetDefaultCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKSetDefaultCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKSetDefaultCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKSetSafetyCommand(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSSetDEKSafetyFunc: func(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
			return &service.SetDEKSafetyResponse{
				Path: req.Path,
				Config: &service.DEKSafetyConfig{
					MaxOperations: req.MaxOperations,
					MaxBytes:      req.MaxBytes,
				},
			}, nil
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDEKSetSafetyCommand()
	cmd.SetArgs([]string{"/tenants/test", "--max-operations", "1000000", "--max-bytes", "1073741824"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSDEKSetSafetyCommand_ClientError(t *testing.T) {
	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKSetSafetyCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// Backend and Safety Run Commands via Factory Swap
// =============================================================================

func TestRunKMSBackendStatusCommand(t *testing.T) {
	mockBackend := &MockBackendClient{
		KMSBackendStatusFunc: func(ctx context.Context) (*service.BackendStatusResponse, error) {
			return &service.BackendStatusResponse{
				BackendType: "software",
				Status:      "healthy",
			}, nil
		},
	}

	origFactory := backendClientFactory
	backendClientFactory = func() (BackendClient, error) { return mockBackend, nil }
	defer func() { backendClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSBackendStatusCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSBackendStatusCommand_ClientError(t *testing.T) {
	origFactory := backendClientFactory
	backendClientFactory = func() (BackendClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { backendClientFactory = origFactory }()

	cmd := newKMSBackendStatusCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSBackendTestCommand(t *testing.T) {
	mockBackend := &MockBackendClient{
		KMSBackendTestFunc: func(ctx context.Context) (*service.BackendTestResponse, error) {
			return &service.BackendTestResponse{
				Success:  true,
				Duration: 50 * time.Millisecond,
			}, nil
		},
	}

	origFactory := backendClientFactory
	backendClientFactory = func() (BackendClient, error) { return mockBackend, nil }
	defer func() { backendClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSBackendTestCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSBackendTestCommand_ClientError(t *testing.T) {
	origFactory := backendClientFactory
	backendClientFactory = func() (BackendClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { backendClientFactory = origFactory }()

	cmd := newKMSBackendTestCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSSafetyCheckCommand(t *testing.T) {
	mockSafety := &MockSafetyClient{
		KMSSafetyCheckFunc: func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
			return &service.SafetyCheckResponse{
				Items:            []service.SafetyCheckItem{},
				ThresholdPercent: req.ThresholdPercent,
				TotalKeysChecked: 10,
				TotalDEKsChecked: 5,
			}, nil
		},
	}

	origFactory := safetyClientFactory
	safetyClientFactory = func() (SafetyClient, error) { return mockSafety, nil }
	defer func() { safetyClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSSafetyCheckCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSSafetyCheckCommand_ClientError(t *testing.T) {
	origFactory := safetyClientFactory
	safetyClientFactory = func() (SafetyClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { safetyClientFactory = origFactory }()

	cmd := newKMSSafetyCheckCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSSafetyReportCommand(t *testing.T) {
	mockSafety := &MockSafetyClient{
		KMSSafetyReportFunc: func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
			return &service.SafetyReportResponse{
				TotalOperations: 1000,
				ServerConfig: service.AEADServerConfig{
					Enforce: true,
				},
			}, nil
		},
	}

	origFactory := safetyClientFactory
	safetyClientFactory = func() (SafetyClient, error) { return mockSafety, nil }
	defer func() { safetyClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSSafetyReportCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSSafetyReportCommand_ClientError(t *testing.T) {
	origFactory := safetyClientFactory
	safetyClientFactory = func() (SafetyClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { safetyClientFactory = origFactory }()

	cmd := newKMSSafetyReportCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// Key Export/Unwrap/Set-Safety Run Commands
// =============================================================================

func TestRunKMSKeyExportCommand(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSExportKeyFunc: func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
			return &service.ExportKeyResponse{
				KeyID:      req.KeyID,
				WrappedKey: []byte("wrapped-key-data"),
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSKeyExportCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSKeyExportCommand_ClientError(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyExportCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyUnwrapCommand(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSUnwrapKeyFunc: func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
			return &service.UnwrapKeyResponse{
				KeyID:     req.KeyID,
				Algorithm: "AES-256-GCM",
				Status:    "enabled",
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	// Need valid base64 for --material
	cmd := newKMSKeyUnwrapCommand()
	cmd.SetArgs([]string{"test-key", "--wrapping-key", "wrapping-key-1", "--material", "SGVsbG8=", "--algorithm", "AES-256-GCM"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSKeyUnwrapCommand_InvalidBase64(t *testing.T) {
	mockKMS := &MockKMSClient{}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyUnwrapCommand()
	cmd.SetArgs([]string{"test-key", "--wrapping-key", "wk-1", "--material", "not-valid-base64!!!", "--algorithm", "AES-256-GCM"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyUnwrapCommand_ClientError(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyUnwrapCommand()
	cmd.SetArgs([]string{"test-key", "--wrapping-key", "wk-1", "--material", "SGVsbG8=", "--algorithm", "AES-256-GCM"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeySetSafetyCommand(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSSetKeySafetyFunc: func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
			return &service.SetKeySafetyResponse{
				KeyID: req.KeyID,
				Config: service.AEADSafetyConfigDTO{
					MaxOperations: req.MaxOperations,
					MaxBytes:      req.MaxBytes,
				},
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSKeySetSafetyCommand()
	cmd.SetArgs([]string{"test-key", "--max-operations", "1000000"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSKeySetSafetyCommand_ClientError(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeySetSafetyCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// Key Enable/Disable Run Commands
// =============================================================================

func TestRunKMSKeyEnableCommand(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSUpdateKeyStatusFunc: func(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
			return &service.UpdateKeyStatusResponse{
				KeyID:     req.KeyID,
				OldStatus: "disabled",
				NewStatus: req.Status,
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSKeyEnableCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSKeyEnableCommand_ClientError(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyEnableCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyDisableCommand(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSUpdateKeyStatusFunc: func(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
			return &service.UpdateKeyStatusResponse{
				KeyID:     req.KeyID,
				OldStatus: "enabled",
				NewStatus: req.Status,
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSKeyDisableCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSKeyDisableCommand_ClientError(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyDisableCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyVersionsCommand(t *testing.T) {
	now := time.Now()
	mockKMS := &MockKMSClient{
		KMSListKeyVersionsFunc: func(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
			return &service.ListKeyVersionsResponse{
				Versions: []*service.KeyVersionInfo{
					{Version: 1, Status: "enabled", CreatedAt: now},
					{Version: 2, Status: "enabled", CreatedAt: now},
				},
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSKeyVersionsCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestRunKMSKeyVersionsCommand_ClientError(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: errors.New(errors.ErrInternal, "test", "connection failed")}
	}
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyVersionsCommand()
	cmd.SetArgs([]string{"test-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// DEK Service Error Propagation Tests
// =============================================================================

func TestRunKMSDEKGenerateCommand_ServiceError(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSCreateDEKFunc: func(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrAlreadyExists, Op: "dek.create", Err: errors.New(errors.ErrAlreadyExists, "dek.create", "DEK already exists")}
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKGenerateCommand()
	cmd.SetArgs([]string{"/tenants/test"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKGetCommand_ServiceError(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSGetDEKFunc: func(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "dek.get", Err: errors.New(errors.ErrNotFound, "dek.get", "DEK not found")}
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKGetCommand()
	cmd.SetArgs([]string{"/tenants/nonexistent"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSDEKListCommand_ServiceError(t *testing.T) {
	mockDEK := &MockDEKClient{
		KMSListDEKsFunc: func(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "dek.list", Err: errors.New(errors.ErrInternal, "dek.list", "internal error")}
		},
	}

	origFactory := dekClientFactory
	dekClientFactory = func() (DEKClient, error) { return mockDEK, nil }
	defer func() { dekClientFactory = origFactory }()

	cmd := newKMSDEKListCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// Backend/Safety Service Error Propagation Tests
// =============================================================================

func TestRunKMSBackendStatusCommand_ServiceError(t *testing.T) {
	mockBackend := &MockBackendClient{
		KMSBackendStatusFunc: func(ctx context.Context) (*service.BackendStatusResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "backend.status", Err: errors.New(errors.ErrInternal, "backend.status", "unavailable")}
		},
	}

	origFactory := backendClientFactory
	backendClientFactory = func() (BackendClient, error) { return mockBackend, nil }
	defer func() { backendClientFactory = origFactory }()

	cmd := newKMSBackendStatusCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSBackendTestCommand_ServiceError(t *testing.T) {
	mockBackend := &MockBackendClient{
		KMSBackendTestFunc: func(ctx context.Context) (*service.BackendTestResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "backend.test", Err: errors.New(errors.ErrInternal, "backend.test", "test failed")}
		},
	}

	origFactory := backendClientFactory
	backendClientFactory = func() (BackendClient, error) { return mockBackend, nil }
	defer func() { backendClientFactory = origFactory }()

	cmd := newKMSBackendTestCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSSafetyCheckCommand_ServiceError(t *testing.T) {
	mockSafety := &MockSafetyClient{
		KMSSafetyCheckFunc: func(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "safety.check", Err: errors.New(errors.ErrInternal, "safety.check", "check failed")}
		},
	}

	origFactory := safetyClientFactory
	safetyClientFactory = func() (SafetyClient, error) { return mockSafety, nil }
	defer func() { safetyClientFactory = origFactory }()

	cmd := newKMSSafetyCheckCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSSafetyReportCommand_ServiceError(t *testing.T) {
	mockSafety := &MockSafetyClient{
		KMSSafetyReportFunc: func(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "safety.report", Err: errors.New(errors.ErrInternal, "safety.report", "report failed")}
		},
	}

	origFactory := safetyClientFactory
	safetyClientFactory = func() (SafetyClient, error) { return mockSafety, nil }
	defer func() { safetyClientFactory = origFactory }()

	cmd := newKMSSafetyReportCommand()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// JSON Output Validation Tests
// =============================================================================

func TestFormatDEKCreateResponse_JSONStructure(t *testing.T) {
	resp := &service.CreateDEKResponse{
		DEK: &service.DEKMetadata{
			Path:      "/tenants/acme",
			Algorithm: "AES-256-GCM",
			Version:   1,
			Status:    "active",
		},
		Timestamp: time.Now(),
	}

	var buf bytes.Buffer
	err := formatDEKCreateResponse(resp, "json", &buf)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &parsed)
	require.NoError(t, err)
	assert.Contains(t, parsed, "dek")
}

func TestFormatBackendStatusResponse_JSONStructure(t *testing.T) {
	resp := &service.BackendStatusResponse{
		BackendType: "software",
		Status:      "healthy",
	}

	var buf bytes.Buffer
	err := formatBackendStatusResponse(resp, "json", &buf)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &parsed)
	require.NoError(t, err)
	assert.Equal(t, "software", parsed["backend_type"])
	assert.Equal(t, "healthy", parsed["status"])
}

func TestFormatSafetyCheckResponse_JSONStructure(t *testing.T) {
	resp := &service.SafetyCheckResponse{
		Items:            []service.SafetyCheckItem{},
		ThresholdPercent: 80,
		TotalKeysChecked: 5,
		TotalDEKsChecked: 3,
	}

	var buf bytes.Buffer
	err := formatSafetyCheckResponse(resp, "json", &buf)
	require.NoError(t, err)

	var parsed map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &parsed)
	require.NoError(t, err)
	assert.Equal(t, float64(80), parsed["threshold_percent"])
}

// =============================================================================
// KMS Encrypt/Decrypt Format Functions (ensure complete coverage)
// =============================================================================

func TestFormatKMSEncryptResponse_AllFormats(t *testing.T) {
	resp := &service.EncryptResponse{
		KeyID:      "enc-key-1",
		KeyVersion: 2,
		Ciphertext: []byte{0xde, 0xad, 0xbe, 0xef},
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Key ID:", "enc-key-1", "Key Version:", "Ciphertext (hex):", "Ciphertext (base64):"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Ciphertext (hex):"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSEncryptResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

func TestFormatKMSDecryptResponse_AllFormats(t *testing.T) {
	resp := &service.DecryptResponse{
		KeyID:      "dec-key-1",
		KeyVersion: 1,
		Plaintext:  []byte("decrypted data"),
	}

	tests := []struct {
		name     string
		format   string
		contains []string
	}{
		{
			name:     "table_format",
			format:   "table",
			contains: []string{"Key ID:", "dec-key-1", "Key Version:", "Plaintext:", "decrypted data"},
		},
		{
			name:     "raw_format",
			format:   "raw",
			contains: []string{"Plaintext:", "decrypted data"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatKMSDecryptResponse(resp, tc.format, &buf)
			require.NoError(t, err)
			output := buf.String()
			for _, s := range tc.contains {
				assert.Contains(t, output, s)
			}
		})
	}
}

// =============================================================================
// KMS DEK Command Structure Tests
// =============================================================================

func TestNewKMSDEKCommand_SubcommandRegistration(t *testing.T) {
	cmd := newKMSDEKCommand()
	assert.Equal(t, "dek", cmd.Use)

	subCmds := cmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, sub := range subCmds {
		names = append(names, sub.Name())
	}

	expected := []string{"generate", "get", "list", "rotate", "delete", "status", "set-default", "set-safety"}
	for _, exp := range expected {
		found := false
		for _, name := range names {
			if name == exp {
				found = true
				break
			}
		}
		assert.True(t, found, "expected subcommand %q not found in DEK commands", exp)
	}
}

func TestNewKMSBackendCommand_SubcommandRegistration(t *testing.T) {
	cmd := newKMSBackendCommand()
	assert.Equal(t, "backend", cmd.Use)

	subCmds := cmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, sub := range subCmds {
		names = append(names, sub.Name())
	}

	assert.Contains(t, names, "status")
	assert.Contains(t, names, "test")
}

func TestNewKMSSafetyCommand_SubcommandRegistration(t *testing.T) {
	cmd := newKMSSafetyCommand()
	assert.Equal(t, "safety", cmd.Use)

	subCmds := cmd.Commands()
	names := make([]string, 0, len(subCmds))
	for _, sub := range subCmds {
		names = append(names, sub.Name())
	}

	assert.Contains(t, names, "check")
	assert.Contains(t, names, "report")
}

// =============================================================================
// Command Flag Verification Tests
// =============================================================================

func TestKMSDEKGenerateCommandFlags(t *testing.T) {
	cmd := newKMSDEKGenerateCommand()
	assert.Equal(t, "generate <path>", cmd.Use)

	algoFlag := cmd.Flags().Lookup("algorithm")
	require.NotNil(t, algoFlag)
	assert.Equal(t, "AES-256-GCM", algoFlag.DefValue)

	descFlag := cmd.Flags().Lookup("description")
	require.NotNil(t, descFlag)
}

func TestKMSDEKDeleteCommandFlags(t *testing.T) {
	cmd := newKMSDEKDeleteCommand()
	assert.Equal(t, "delete <path>", cmd.Use)

	forceFlag := cmd.Flags().Lookup("force")
	require.NotNil(t, forceFlag)
	assert.Equal(t, "false", forceFlag.DefValue)
}

func TestKMSDEKSetSafetyCommandFlags(t *testing.T) {
	cmd := newKMSDEKSetSafetyCommand()
	assert.Equal(t, "set-safety <path>", cmd.Use)

	maxOpsFlag := cmd.Flags().Lookup("max-operations")
	require.NotNil(t, maxOpsFlag)

	maxBytesFlag := cmd.Flags().Lookup("max-bytes")
	require.NotNil(t, maxBytesFlag)

	warnFlag := cmd.Flags().Lookup("warning-percent")
	require.NotNil(t, warnFlag)
	assert.Equal(t, "80", warnFlag.DefValue)
}

func TestKMSSafetyCheckCommandFlags(t *testing.T) {
	cmd := newKMSSafetyCheckCommand()
	assert.Equal(t, "check", cmd.Use)

	thresholdFlag := cmd.Flags().Lookup("threshold")
	require.NotNil(t, thresholdFlag)
	assert.Equal(t, "80", thresholdFlag.DefValue)
}

func TestKMSKeyExportCommandFlags(t *testing.T) {
	cmd := newKMSKeyExportCommand()
	assert.Equal(t, "export <key-name>", cmd.Use)

	wrappingKeyFlag := cmd.Flags().Lookup("wrapping-key")
	require.NotNil(t, wrappingKeyFlag)

	wrappingAlgoFlag := cmd.Flags().Lookup("wrapping-algorithm")
	require.NotNil(t, wrappingAlgoFlag)

	formatFlag := cmd.Flags().Lookup("format")
	require.NotNil(t, formatFlag)
}

func TestKMSKeyUnwrapCommandFlags(t *testing.T) {
	cmd := newKMSKeyUnwrapCommand()
	assert.Equal(t, "unwrap <key-name>", cmd.Use)

	wrappingKeyFlag := cmd.Flags().Lookup("wrapping-key")
	require.NotNil(t, wrappingKeyFlag)

	materialFlag := cmd.Flags().Lookup("material")
	require.NotNil(t, materialFlag)

	algoFlag := cmd.Flags().Lookup("algorithm")
	require.NotNil(t, algoFlag)
}

func TestKMSKeySetSafetyCommandFlags(t *testing.T) {
	cmd := newKMSKeySetSafetyCommand()
	assert.Equal(t, "set-safety <key-name>", cmd.Use)

	maxOpsFlag := cmd.Flags().Lookup("max-operations")
	require.NotNil(t, maxOpsFlag)

	maxBytesFlag := cmd.Flags().Lookup("max-bytes")
	require.NotNil(t, maxBytesFlag)
}

// =============================================================================
// createKeyAttributes Extended Coverage
// =============================================================================

func TestCreateKeyAttributes_AdditionalAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algo      string
		expectErr bool
	}{
		{name: "ed25519", algo: "ED25519"},
		{name: "rsa_2048", algo: "RSA-2048"},
		{name: "rsa_3072", algo: "RSA-3072"},
		{name: "rsa_4096", algo: "RSA-4096"},
		{name: "ecdsa_p256", algo: "ECDSA-P256"},
		{name: "ecdsa_p384", algo: "ECDSA-P384"},
		{name: "ecdsa_p521", algo: "ECDSA-P521"},
		{name: "unsupported", algo: "INVALID-ALGO", expectErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs, err := createKeyAttributes("test-key", tc.algo, false)
			if tc.expectErr {
				assert.Error(t, err)
				assert.Nil(t, attrs)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, attrs)
				assert.Equal(t, "test-key", attrs.CN)
			}
		})
	}
}

func TestCreateKeyAttributes_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name string
		algo string
	}{
		{name: "lowercase_aes", algo: "aes-256-gcm"},
		{name: "mixed_case_chacha", algo: "ChaCha20-Poly1305"},
		{name: "lowercase_ed25519", algo: "ed25519"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			attrs, err := createKeyAttributes("test-key", tc.algo, true)
			assert.NoError(t, err)
			assert.NotNil(t, attrs)
			assert.True(t, attrs.Exportable)
		})
	}
}

// =============================================================================
// Key Export/Unwrap Run Command Error Propagation
// =============================================================================

func TestRunKMSKeyExportCommand_ServiceError(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSExportKeyFunc: func(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "key.export", Err: errors.New(errors.ErrNotFound, "key.export", "key not found")}
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyExportCommand()
	cmd.SetArgs([]string{"nonexistent-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyUnwrapCommand_ServiceError(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSUnwrapKeyFunc: func(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInvalidInput, Op: "key.unwrap", Err: errors.New(errors.ErrInvalidInput, "key.unwrap", "invalid wrapped key")}
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyUnwrapCommand()
	cmd.SetArgs([]string{"test-key", "--wrapping-key", "wk-1", "--material", "SGVsbG8=", "--algorithm", "AES-256-GCM"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeySetSafetyCommand_ServiceError(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSSetKeySafetyFunc: func(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "key.safety", Err: errors.New(errors.ErrNotFound, "key.safety", "key not found")}
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeySetSafetyCommand()
	cmd.SetArgs([]string{"nonexistent-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyEnableCommand_ServiceError(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSUpdateKeyStatusFunc: func(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "key.enable", Err: errors.New(errors.ErrNotFound, "key.enable", "key not found")}
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyEnableCommand()
	cmd.SetArgs([]string{"nonexistent-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyDisableCommand_ServiceError(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSUpdateKeyStatusFunc: func(ctx context.Context, req *service.UpdateKeyStatusRequest) (*service.UpdateKeyStatusResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "key.disable", Err: errors.New(errors.ErrNotFound, "key.disable", "key not found")}
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyDisableCommand()
	cmd.SetArgs([]string{"nonexistent-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestRunKMSKeyVersionsCommand_ServiceError(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSListKeyVersionsFunc: func(ctx context.Context, req *service.ListKeyVersionsRequest) (*service.ListKeyVersionsResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrNotFound, Op: "key.versions", Err: errors.New(errors.ErrNotFound, "key.versions", "key not found")}
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSKeyVersionsCommand()
	cmd.SetArgs([]string{"nonexistent-key"})
	err := cmd.Execute()
	assert.Error(t, err)
}

// =============================================================================
// KMS Encrypt/Decrypt Run Commands with Base64
// =============================================================================

func TestRunKMSEncryptCommand_Base64Data(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
			return &service.EncryptResponse{
				KeyID:      req.KeyID,
				Ciphertext: []byte("encrypted"),
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSEncryptCommand()

	// Set globals AFTER newKMSEncryptCommand() since BoolVar resets kmsBase64
	origBase64 := kmsBase64
	kmsBase64 = true
	defer func() { kmsBase64 = origBase64 }()

	origData := kmsData
	kmsData = "SGVsbG8="
	defer func() { kmsData = origData }()

	err := runKMSEncryptCommand(cmd, []string{"test-key"})
	require.NoError(t, err)
}

func TestRunKMSEncryptCommand_InvalidBase64(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return &MockKMSClient{}, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSEncryptCommand()

	// Set globals AFTER newKMSEncryptCommand() since BoolVar resets kmsBase64
	origBase64 := kmsBase64
	kmsBase64 = true
	defer func() { kmsBase64 = origBase64 }()

	origData := kmsData
	kmsData = "not-valid-base64!!!"
	defer func() { kmsData = origData }()

	err := runKMSEncryptCommand(cmd, []string{"test-key"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "base64")
}

func TestRunKMSDecryptCommand_InvalidHexData(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return &MockKMSClient{}, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSDecryptCommand()

	// Set globals AFTER newKMSDecryptCommand() since BoolVar resets kmsBase64
	origBase64 := kmsBase64
	kmsBase64 = false
	defer func() { kmsBase64 = origBase64 }()

	origData := kmsData
	kmsData = "not-valid-hex!!!"
	defer func() { kmsData = origData }()

	err := runKMSDecryptCommand(cmd, []string{"test-key"})
	assert.Error(t, err)
}

func TestRunKMSDecryptCommand_Base64Data(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
			return &service.DecryptResponse{
				KeyID:     req.KeyID,
				Plaintext: []byte("decrypted"),
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	cmd := newKMSDecryptCommand()

	// Set globals AFTER newKMSDecryptCommand() since BoolVar resets kmsBase64
	origBase64 := kmsBase64
	kmsBase64 = true
	defer func() { kmsBase64 = origBase64 }()

	origData := kmsData
	kmsData = "SGVsbG8="
	defer func() { kmsData = origData }()

	err := runKMSDecryptCommand(cmd, []string{"test-key"})
	require.NoError(t, err)
}

func TestRunKMSDecryptCommand_InvalidBase64(t *testing.T) {
	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return &MockKMSClient{}, nil }
	defer func() { kmsClientFactory = origFactory }()

	cmd := newKMSDecryptCommand()

	// Set globals AFTER newKMSDecryptCommand() since BoolVar resets kmsBase64
	origBase64 := kmsBase64
	kmsBase64 = true
	defer func() { kmsBase64 = origBase64 }()

	origData := kmsData
	kmsData = "not-valid-base64!!!"
	defer func() { kmsData = origData }()

	err := runKMSDecryptCommand(cmd, []string{"test-key"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "base64")
}

func TestRunKMSEncryptCommand_WithAAD(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
			assert.NotNil(t, req.Context)
			return &service.EncryptResponse{
				KeyID:      req.KeyID,
				Ciphertext: []byte("encrypted"),
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	origBase64 := kmsBase64
	kmsBase64 = false
	defer func() { kmsBase64 = origBase64 }()

	origData := kmsData
	kmsData = "sensitive data"
	defer func() { kmsData = origData }()

	origAAD := kmsAAD
	kmsAAD = "additional context"
	defer func() { kmsAAD = origAAD }()

	cmd := newKMSEncryptCommand()

	// Re-set globals after newKMSEncryptCommand since BoolVar resets them
	kmsBase64 = false
	kmsData = "sensitive data"
	kmsAAD = "additional context"

	err := runKMSEncryptCommand(cmd, []string{"test-key"})
	require.NoError(t, err)
}

func TestRunKMSDecryptCommand_WithAAD(t *testing.T) {
	mockKMS := &MockKMSClient{
		KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
			assert.NotNil(t, req.Context)
			return &service.DecryptResponse{
				KeyID:     req.KeyID,
				Plaintext: []byte("decrypted"),
			}, nil
		},
	}

	origFactory := kmsClientFactory
	kmsClientFactory = func() (KMSClient, error) { return mockKMS, nil }
	defer func() { kmsClientFactory = origFactory }()

	origFmt := kmsOutputFmt
	kmsOutputFmt = "json"
	defer func() { kmsOutputFmt = origFmt }()

	origBase64 := kmsBase64
	kmsBase64 = false
	defer func() { kmsBase64 = origBase64 }()

	origData := kmsData
	kmsData = "deadbeef"
	defer func() { kmsData = origData }()

	origAAD := kmsAAD
	kmsAAD = "additional context"
	defer func() { kmsAAD = origAAD }()

	cmd := newKMSDecryptCommand()

	// Re-set globals after newKMSDecryptCommand since BoolVar resets them
	kmsBase64 = false
	kmsData = "deadbeef"
	kmsAAD = "additional context"

	err := runKMSDecryptCommand(cmd, []string{"test-key"})
	require.NoError(t, err)
}
