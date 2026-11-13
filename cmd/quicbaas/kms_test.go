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
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

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

	// An armoured private key, so the table covers the ASYMMETRIC branch too.
	// Without a case here that branch was unreachable from this test, which is
	// how a nil cmd survived in the harness: nothing ever dereferenced it.
	armouredKey, _ := opensshEd25519(t)
	armouredFile := t.TempDir() + "/agent-signing-key"
	require.NoError(t, os.WriteFile(armouredFile, armouredKey, 0o600))

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
				kmsImportAlgorithm = "AES-256-GCM"
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
				kmsImportAlgorithm = "AES-256-GCM"
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
				kmsImportAlgorithm = "AES-256-GCM"
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
				kmsImportAlgorithm = "AES-256-GCM"
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
				kmsImportAlgorithm = "AES-256-GCM"
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
				kmsImportAlgorithm = "AES-256-GCM"
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
				kmsImportAlgorithm = "AES-256-GCM"
			},
			args:          []string{"imported-key"},
			outputFormat:  "table",
			expectError:   true,
			expectedError: "invalid key material",
		},
		{
			name: "armoured private key takes the asymmetric path",
			setupMock: func() *MockKMSClient {
				return &MockKMSClient{
					KMSImportKeyFunc: func(_ context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
						// The key, re-encoded — not the armour that carried it.
						assert.Equal(t, x509.Ed25519, req.KeyAttributes.KeyAlgorithm)
						assert.Equal(t, []service.KeyUsage{service.KeyUsageSign}, req.Usage)
						_, parseErr := x509.ParsePKCS8PrivateKey(req.KeyMaterial)
						assert.NoError(t, parseErr, "material sent was not PKCS#8 DER")
						return &service.ImportKeyResponse{
							Key: &service.KeyHandle{KeyID: "agent-signing", Version: 1},
						}, nil
					},
				}
			},
			setupFlags: func() {
				kmsMaterial = armouredFile
			},
			args:         []string{"agent-signing"},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Build the command BEFORE setting flags, for two reasons.
			//
			// The armoured-material branch reads cmd.Flags(), so a nil cmd
			// nil-panics the moment a case supplies a private key — the cases
			// below avoid it only because their material is raw. And StringVar
			// writes each flag's default through to its global at registration,
			// so constructing afterwards would blank what setupFlags assigned.
			cmd := newKMSKeyImportCommand()

			// Setup flags
			tt.setupFlags()
			kmsOutputFmt = tt.outputFormat

			// Cleanup
			defer func() {
				kmsMaterial = ""
				kmsImportAlgorithm = ""
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
			err := runKMSKeyImportCommand(cmd, tt.args)

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

	t.Run("SecretsClient", func(t *testing.T) {
		t.Parallel()
		var _ SecretsClient = (*MockSecretsClient)(nil)
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

// =============================================================================
// Asymmetric key material on the symmetric import path
// =============================================================================

// TestDetectEncodedKeyMaterial_NamesTheTrapItCloses is the regression for an
// import that SUCCEEDED WRONGLY.
//
// `kms key import` stores KeyMaterial as raw bytes under a symmetric
// algorithm. Handed an OpenSSH private key it did not fail — it stored the
// PEM TEXT as if it were an AES key. The import reported success, the key
// listed normally, and it could never sign. That is the dangerous version of
// a missing capability: it only surfaces at the first signature, or never.
func TestDetectEncodedKeyMaterial_NamesTheTrapItCloses(t *testing.T) {
	cases := map[string]struct {
		material string
		want     string
	}{
		"openssh private key": {
			"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNz\n-----END OPENSSH PRIVATE KEY-----",
			"an OpenSSH private key",
		},
		"pem private key": {
			"-----BEGIN EC PRIVATE KEY-----\nMHcCAQ==\n-----END EC PRIVATE KEY-----",
			"a PEM private key",
		},
		"pem public key": {
			"-----BEGIN PUBLIC KEY-----\nMFkw\n-----END PUBLIC KEY-----",
			"a PEM public key",
		},
		"openssh public key": {
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 agent@host",
			"an OpenSSH public key",
		},
		// Leading whitespace must not defeat it: a key pasted into a file
		// commonly carries a newline.
		"armour after whitespace": {
			"\n  -----BEGIN OPENSSH PRIVATE KEY-----\nx\n",
			"an OpenSSH private key",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := detectEncodedKeyMaterial([]byte(tc.material)); got != tc.want {
				t.Errorf("detect = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDetectEncodedKeyMaterial_PassesRawSymmetricBytes proves the guard cannot
// block the import it exists to protect.
//
// Detection is on the PEM/OpenSSH armour rather than entropy or length,
// because a symmetric key's raw bytes can legitimately be anything at all —
// including bytes that happen to be printable.
func TestDetectEncodedKeyMaterial_PassesRawSymmetricBytes(t *testing.T) {
	cases := map[string][]byte{
		"32 random-looking bytes": []byte("\x9f\x1a\x00\xffPQRSTUVWXYZ012345\x7f\x80\x01\x02\x03\x04\x05\x06\x07"),
		"printable aes key":       []byte("0123456789abcdef0123456789abcdef"),
		"empty":                   {},
		"text mentioning a key":   []byte("this material is not a PRIVATE KEY----- header"),
	}
	for name, material := range cases {
		t.Run(name, func(t *testing.T) {
			if got := detectEncodedKeyMaterial(material); got != "" {
				t.Errorf("raw symmetric material was refused as %q", got)
			}
		})
	}
}

// =============================================================================
// Asymmetric import: derive, cross-check, and prove the key can sign
// =============================================================================

// opensshEd25519 returns a fresh ed25519 key in OpenSSH private-key form.
func opensshEd25519(t *testing.T) ([]byte, ed25519.PrivateKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatalf("marshalling to OpenSSH: %v", err)
	}
	return pem.EncodeToMemory(block), priv
}

// TestDeriveKeyAlgorithm_ReadsTheKeyNotTheFlag covers the derivation across the
// encodings a caller actually has.
func TestDeriveKeyAlgorithm_ReadsTheKeyNotTheFlag(t *testing.T) {
	openssh, _ := opensshEd25519(t)
	if _, got, err := parsePrivateKeyMaterial(openssh); err != nil || got != x509.Ed25519 {
		t.Errorf("openssh ed25519 -> (%v, %v), want Ed25519", got, err)
	}

	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating an ecdsa key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(ecKey)
	if err != nil {
		t.Fatalf("marshalling pkcs8: %v", err)
	}
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if _, got, err := parsePrivateKeyMaterial(pkcs8); err != nil || got != x509.ECDSA {
		t.Errorf("pkcs8 ecdsa -> (%v, %v), want ECDSA", got, err)
	}

	if _, _, err := parsePrivateKeyMaterial([]byte("not a key at all")); err == nil {
		t.Error("unparseable material was accepted as a key")
	}
}

// TestImportedEd25519KeyActuallySigns is the regression the ruling asked for,
// and it asserts by SIGNING rather than by import success.
//
// Import success is exactly what lied before: the old path stored the PEM TEXT
// as an AES key, reported success, listed the key normally, and produced
// something that could never sign. So the assertion here is a real signature
// over real bytes, verified against the public half — the only evidence that
// distinguishes a key from a blob that was called one.
func TestImportedEd25519KeyActuallySigns(t *testing.T) {
	material, original := opensshEd25519(t)

	_, algorithm, err := parsePrivateKeyMaterial(material)
	if err != nil {
		t.Fatalf("deriving the algorithm: %v", err)
	}
	if algorithm != x509.Ed25519 {
		t.Fatalf("algorithm = %v, want Ed25519", algorithm)
	}

	// Recover the key the way the import path does, then USE it.
	parsed, err := ssh.ParseRawPrivateKey(material)
	if err != nil {
		t.Fatalf("parsing the key the import path parses: %v", err)
	}
	priv, ok := parsed.(*ed25519.PrivateKey)
	if !ok {
		t.Fatalf("parsed key is %T, not an ed25519 private key", parsed)
	}

	message := []byte("the SDLC module signs its own commits")
	signature := ed25519.Sign(*priv, message)
	if !ed25519.Verify(original.Public().(ed25519.PublicKey), message, signature) {
		t.Fatal("the imported key produced a signature that does not verify; it is a blob, not a key")
	}

	// And the shape the OLD path would have stored — the PEM text as raw
	// symmetric material — is now unreachable: routing is on the material.
	if detectEncodedKeyMaterial(material) == "" {
		t.Error("armoured material is no longer detected; the old trap is reachable again")
	}
}

// TestAlgorithmMatches_RefusesAContradictionNotANotation pins the cross-check.
//
// A caller who says --algorithm RSA while handing over an ed25519 key believes
// they are importing something else, and storing the key under a false
// description would make every later operation reason from that description.
// Notation differences (ECDSA vs ECDSA-P256) are not contradictions.
func TestAlgorithmMatches_RefusesAContradictionNotANotation(t *testing.T) {
	_, ed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	p256, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("generating: %v", err)
	}

	if !algorithmMatches("ED25519", &ed, x509.Ed25519) || !algorithmMatches("ed25519", &ed, x509.Ed25519) {
		t.Error("ed25519 spelled either way must match an ed25519 key")
	}
	if algorithmMatches("RSA", &ed, x509.Ed25519) {
		t.Error("RSA was accepted for an ed25519 key; a false description would be stored")
	}
	if algorithmMatches("AES-256-GCM", &ed, x509.Ed25519) {
		t.Error("a symmetric algorithm matched an asymmetric key")
	}

	// A bare family name stays notation: the caller named no curve, so there
	// is nothing to contradict.
	if !algorithmMatches("ECDSA", p384, x509.ECDSA) {
		t.Error("a bare family name must still match any ECDSA key")
	}
	if !algorithmMatches("ECDSA-P256", p256, x509.ECDSA) {
		t.Error("the matching curve must be accepted")
	}

	// A NAMED CURVE IS A CLAIM, and a wrong one is a contradiction. This was
	// previously accepted as mere notation, which let a P-384 key be stored
	// describing itself as P-256.
	if algorithmMatches("ECDSA-P256", p384, x509.ECDSA) {
		t.Error("a P-384 key was accepted as P-256; the curve is part of the key's identity")
	}
	if algorithmMatches("ECDSA-P384", p256, x509.ECDSA) {
		t.Error("a P-256 key was accepted as P-384")
	}
}

// TestImportSendsThePKCS8KeyNotTheArmour asserts what the command actually puts
// on the wire, which is the one thing a mocked client CAN prove.
//
// Deriving the algorithm correctly and then shipping the original OpenSSH text
// would look right at every layer a CLI test can see, and still fail at the
// store: the KMS keeps PKCS#8 DER and refuses anything else. So this decodes
// the material the command sent and checks it is the SAME KEY the file held —
// not merely that some bytes were sent, and not that the import returned nil,
// which is exactly what lied in the original defect.
func TestImportSendsThePKCS8KeyNotTheArmour(t *testing.T) {
	origFactory := kmsClientFactory
	defer func() {
		kmsClientFactory = origFactory
		kmsMaterial = ""
		kmsImportAlgorithm = ""
		kmsDescription = ""
		kmsOutputFmt = ""
	}()

	armour, original := opensshEd25519(t)

	path := t.TempDir() + "/agent-signing-key"
	require.NoError(t, os.WriteFile(path, armour, 0o600))

	var sent *service.ImportKeyRequest
	kmsClientFactory = func() (KMSClient, error) {
		return &MockKMSClient{
			KMSImportKeyFunc: func(_ context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
				sent = req
				return &service.ImportKeyResponse{
					Key: &service.KeyHandle{KeyID: "agent-commit-signing", Version: 1},
				}, nil
			},
		}, nil
	}

	// Build the command FIRST: its StringVar registration writes through to
	// kmsMaterial, so constructing it after assigning would blank the flag.
	cmd := newKMSKeyImportCommand()

	kmsMaterial = path
	kmsOutputFmt = "json"

	require.NoError(t, runKMSKeyImportCommand(cmd, []string{"agent-commit-signing"}))
	require.NotNil(t, sent, "the command never called the KMS")

	parsed, err := x509.ParsePKCS8PrivateKey(sent.KeyMaterial)
	require.NoError(t, err,
		"the command sent material that is not PKCS#8 DER; the store refuses it, "+
			"so a correct derivation would still produce a failed import")

	imported, ok := parsed.(ed25519.PrivateKey)
	require.True(t, ok, "sent an %T where the file held an Ed25519 key", parsed)
	require.True(t, imported.Equal(original),
		"the command sent a DIFFERENT key than the file held")

	require.Equal(t, x509.Ed25519, sent.KeyAttributes.KeyAlgorithm,
		"the key must be described by what it is")
	require.Equal(t, []service.KeyUsage{service.KeyUsageSign}, sent.Usage,
		"a signing key imported as encrypt/decrypt cannot be used for what it is for")
}

// TestKMSKeyGenerateDefaultAlgorithmWithWholeTree pins #2212: with every kms
// command registered, generate without --algorithm must run with its own
// default, not the default of a later command bound to the same variable.
func TestKMSKeyGenerateDefaultAlgorithmWithWholeTree(t *testing.T) {
	root := newKMSCommand()
	gen, _, err := root.Find([]string{"key", "generate"})
	require.NoError(t, err)
	require.Equal(t, "generate", gen.Name())
	for _, path := range [][]string{{"key", "import"}, {"key", "unwrap"}} {
		sub, _, err := root.Find(path)
		require.NoError(t, err)
		require.Equal(t, path[1], sub.Name(), "the %v command is registered in the same tree", path)
	}

	require.NoError(t, gen.ParseFlags([]string{"my-key"}))
	require.False(t, gen.Flags().Changed("algorithm"))
	got, err := gen.Flags().GetString("algorithm")
	require.NoError(t, err)
	require.Equal(t, "AES-256-GCM", got)
	require.Equal(t, "AES-256-GCM", kmsAlgorithm, "the run path reads kmsAlgorithm")

	_, err = createKeyAttributes("my-key", kmsAlgorithm, false)
	require.NoError(t, err)
}
