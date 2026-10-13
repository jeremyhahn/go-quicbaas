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
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

func TestNewCryptoCommand(t *testing.T) {
	cmd := newCryptoCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "crypto", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Check that all expected subcommands exist
	expectedCommands := []string{"encrypt", "decrypt", "rewrap", "sign", "verify", "hash", "random", "datakey"}
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

func TestCryptoEncryptCommandFlags(t *testing.T) {
	cmd := newCryptoEncryptCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	contextFlag := cmd.Flags().Lookup("context")
	assert.NotNil(t, contextFlag)

	base64Flag := cmd.Flags().Lookup("base64")
	assert.NotNil(t, base64Flag)
	assert.Equal(t, "false", base64Flag.DefValue)
}

func TestCryptoEncryptCommand(t *testing.T) {
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
			name:        "encrypt with context",
			args:        []string{"test-key", "--data", "data", "--context", "user-123"},
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
			cmd := newCryptoEncryptCommand()
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

// TestRunCryptoEncryptCommand_Success tests successful encrypt with mock client
func TestRunCryptoEncryptCommand_Success(t *testing.T) {
	// Save and restore original factory
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	// Create mock client
	mockClient := &MockCryptoClient{
		KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
			return &service.EncryptResponse{
				KeyID:      req.KeyID,
				KeyVersion: 1,
				Ciphertext: []byte("encrypted-data"),
			}, nil
		},
	}

	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	// Reset flags after test
	defer func() {
		cryptoData = ""
		cryptoContext = ""
		cryptoBase64 = false
		cryptoOutputFmt = "table"
	}()

	tests := []struct {
		name    string
		data    string
		context string
		base64  bool
		keyName string
	}{
		{
			name:    "encrypt plain text",
			data:    "sensitive data",
			context: "",
			base64:  false,
			keyName: "test-key",
		},
		{
			name:    "encrypt with context",
			data:    "data",
			context: "user-123",
			base64:  false,
			keyName: "test-key",
		},
		{
			name:    "encrypt base64 data",
			data:    base64.StdEncoding.EncodeToString([]byte("test data")),
			context: "",
			base64:  true,
			keyName: "test-key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoData = tt.data
			cryptoContext = tt.context
			cryptoBase64 = tt.base64

			cmd := newCryptoEncryptCommand()
			err := runCryptoEncryptCommand(cmd, []string{tt.keyName})
			assert.NoError(t, err)
		})
	}
}

// TestRunCryptoEncryptCommand_FactoryError tests encrypt with factory error
func TestRunCryptoEncryptCommand_FactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cryptoData = "test data"
	defer func() { cryptoData = "" }()

	cmd := newCryptoEncryptCommand()
	err := runCryptoEncryptCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestRunCryptoEncryptCommand_InvalidBase64 tests encrypt with invalid base64
func TestRunCryptoEncryptCommand_InvalidBase64(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	// Mock factory in case validation doesn't catch it
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "should not reach here")
	}

	// Create command first (this binds flags to variables with defaults)
	cmd := newCryptoEncryptCommand()

	// Set flags AFTER command creation
	cryptoData = "invalid!!!"
	cryptoBase64 = true
	defer func() {
		cryptoData = ""
		cryptoBase64 = false
	}()

	err := runCryptoEncryptCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64")
}

// TestRunCryptoEncryptCommand_OutputFormats tests different output formats
func TestRunCryptoEncryptCommand_OutputFormats(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
			return &service.EncryptResponse{
				KeyID:      "test-key",
				KeyVersion: 1,
				Ciphertext: []byte("encrypted"),
			}, nil
		},
	}

	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	defer func() {
		cryptoData = ""
		cryptoOutputFmt = "table"
	}()

	cryptoData = "test data"

	tests := []struct {
		name   string
		format string
	}{
		{"json format", "json"},
		{"yaml format", "yaml"},
		{"table format", "table"},
		{"default format", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoOutputFmt = tt.format
			cmd := newCryptoEncryptCommand()
			err := runCryptoEncryptCommand(cmd, []string{"test-key"})
			assert.NoError(t, err)
		})
	}
}

func TestCryptoDecryptCommandFlags(t *testing.T) {
	cmd := newCryptoDecryptCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	contextFlag := cmd.Flags().Lookup("context")
	assert.NotNil(t, contextFlag)

	base64Flag := cmd.Flags().Lookup("base64")
	assert.NotNil(t, base64Flag)
	assert.Equal(t, "false", base64Flag.DefValue)
}

func TestCryptoDecryptCommand(t *testing.T) {
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
			cmd := newCryptoDecryptCommand()
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

// TestRunCryptoDecryptCommand_Success tests successful decrypt with mock client
func TestRunCryptoDecryptCommand_Success(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
			return &service.DecryptResponse{
				KeyID:      req.KeyID,
				KeyVersion: 1,
				Plaintext:  []byte("decrypted-data"),
			}, nil
		},
	}

	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	defer func() {
		cryptoData = ""
		cryptoContext = ""
		cryptoBase64 = false
		cryptoOutputFmt = "table"
	}()

	tests := []struct {
		name    string
		data    string
		context string
		base64  bool
	}{
		{
			name:    "decrypt hex data",
			data:    hex.EncodeToString([]byte("encrypted")),
			context: "",
			base64:  false,
		},
		{
			name:    "decrypt with context",
			data:    hex.EncodeToString([]byte("encrypted")),
			context: "user-123",
			base64:  false,
		},
		{
			name:    "decrypt base64 data",
			data:    base64.StdEncoding.EncodeToString([]byte("encrypted")),
			context: "",
			base64:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoData = tt.data
			cryptoContext = tt.context
			cryptoBase64 = tt.base64

			cmd := newCryptoDecryptCommand()
			err := runCryptoDecryptCommand(cmd, []string{"test-key"})
			assert.NoError(t, err)
		})
	}
}

// TestRunCryptoDecryptCommand_FactoryError tests decrypt with factory error
func TestRunCryptoDecryptCommand_FactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cryptoData = hex.EncodeToString([]byte("encrypted"))
	defer func() { cryptoData = "" }()

	cmd := newCryptoDecryptCommand()
	err := runCryptoDecryptCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestRunCryptoDecryptCommand_InvalidHex tests decrypt with invalid hex
func TestRunCryptoDecryptCommand_InvalidHex(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	// Mock factory in case validation doesn't catch it
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "should not reach here")
	}

	// Create command first (this binds flags to variables with defaults)
	cmd := newCryptoDecryptCommand()

	// Set flags AFTER command creation
	cryptoData = "invalid!!!"
	cryptoBase64 = false
	defer func() {
		cryptoData = ""
		cryptoBase64 = false
	}()

	err := runCryptoDecryptCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid hex")
}

// TestRunCryptoDecryptCommand_InvalidBase64 tests decrypt with invalid base64
func TestRunCryptoDecryptCommand_InvalidBase64(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	// Mock factory in case validation doesn't catch it
	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrInternal, "test", "should not reach here")
	}

	// Create command first (this binds flags to variables with defaults)
	cmd := newCryptoDecryptCommand()

	// Set flags AFTER command creation
	cryptoData = "invalid!!!"
	cryptoBase64 = true
	defer func() {
		cryptoData = ""
		cryptoBase64 = false
	}()

	err := runCryptoDecryptCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid base64")
}

// TestRunCryptoDecryptCommand_OutputFormats tests different output formats
func TestRunCryptoDecryptCommand_OutputFormats(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
			return &service.DecryptResponse{
				KeyID:      "test-key",
				KeyVersion: 1,
				Plaintext:  []byte("decrypted"),
			}, nil
		},
	}

	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	defer func() {
		cryptoData = ""
		cryptoOutputFmt = "table"
	}()

	cryptoData = hex.EncodeToString([]byte("encrypted"))

	tests := []struct {
		name   string
		format string
	}{
		{"json format", "json"},
		{"yaml format", "yaml"},
		{"table format", "table"},
		{"default format", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoOutputFmt = tt.format
			cmd := newCryptoDecryptCommand()
			err := runCryptoDecryptCommand(cmd, []string{"test-key"})
			assert.NoError(t, err)
		})
	}
}

func TestCryptoRewrapCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "rewrap without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "rewrap without data",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "rewrap with invalid hex",
			args:        []string{"test-key", "--data", "invalid!!!"},
			expectError: true,
			errorMsg:    "invalid hex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCryptoRewrapCommand()
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

// TestRunCryptoRewrapCommand_InvalidHex tests rewrap with invalid hex
func TestRunCryptoRewrapCommand_InvalidHex(t *testing.T) {
	// Create command first (this binds flags to variables with defaults)
	cmd := newCryptoRewrapCommand()

	// Set flags AFTER command creation
	cryptoData = "invalid!!!"
	defer func() { cryptoData = "" }()

	err := runCryptoRewrapCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid hex")
}

// TestRunCryptoRewrapCommand_FactoryError tests rewrap with factory error
func TestRunCryptoRewrapCommand_FactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cryptoData = hex.EncodeToString([]byte("encrypted"))
	defer func() { cryptoData = "" }()

	cmd := newCryptoRewrapCommand()
	err := runCryptoRewrapCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestRunCryptoRewrapCommand_Unimplemented tests rewrap returns unimplemented
func TestRunCryptoRewrapCommand_Unimplemented(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSRewrapFunc: func(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
			return nil, errors.NotImplemented("crypto.rewrap", "rewrap not yet implemented")
		},
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	cryptoData = hex.EncodeToString([]byte("encrypted"))
	defer func() { cryptoData = "" }()

	cmd := newCryptoRewrapCommand()
	err := runCryptoRewrapCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

// TestRunCryptoRewrapCommand_OutputFormats tests different output formats (when implemented)
func TestRunCryptoRewrapCommand_OutputFormats(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSRewrapFunc: func(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
			return nil, errors.NotImplemented("crypto.rewrap", "rewrap not yet implemented")
		},
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	defer func() {
		cryptoData = ""
		cryptoOutputFmt = "table"
	}()

	cryptoData = hex.EncodeToString([]byte("encrypted"))

	tests := []struct {
		name   string
		format string
	}{
		{"json format", "json"},
		{"table format", "table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoOutputFmt = tt.format
			cmd := newCryptoRewrapCommand()
			err := runCryptoRewrapCommand(cmd, []string{"test-key"})
			// Will fail with unimplemented, but tests format handling path
			assert.Error(t, err)
		})
	}
}

func TestCryptoSignCommandFlags(t *testing.T) {
	cmd := newCryptoSignCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	algorithmFlag := cmd.Flags().Lookup("algorithm")
	assert.NotNil(t, algorithmFlag)
	assert.Equal(t, "SHA256", algorithmFlag.DefValue)
}

func TestCryptoSignCommand(t *testing.T) {
	// Mock the client factory for tests that need "not yet implemented" errors
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSSignFunc: func(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
			return nil, errors.NotImplemented("crypto.sign", "sign not yet implemented")
		},
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "sign without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "sign without data",
			args:        []string{"signing-key"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "sign with data",
			args:        []string{"signing-key", "--data", "message to sign"},
			expectError: true,
			errorMsg:    "not yet implemented",
		},
		{
			name:        "sign with custom algorithm",
			args:        []string{"signing-key", "--data", "message", "--algorithm", "SHA512"},
			expectError: true,
			errorMsg:    "not yet implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCryptoSignCommand()
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

func TestCryptoVerifyCommandFlags(t *testing.T) {
	cmd := newCryptoVerifyCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	signatureFlag := cmd.Flags().Lookup("signature")
	assert.NotNil(t, signatureFlag)

	algorithmFlag := cmd.Flags().Lookup("algorithm")
	assert.NotNil(t, algorithmFlag)
	assert.Equal(t, "SHA256", algorithmFlag.DefValue)
}

func TestCryptoVerifyCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "verify without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "verify without data",
			args:        []string{"signing-key"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "verify without signature",
			args:        []string{"signing-key", "--data", "message"},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "verify with invalid signature",
			args:        []string{"signing-key", "--data", "message", "--signature", "invalid!!!"},
			expectError: true,
			errorMsg:    "invalid signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCryptoVerifyCommand()
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

// TestRunCryptoVerifyCommand_InvalidSignature tests verify with invalid signature format
func TestRunCryptoVerifyCommand_InvalidSignature(t *testing.T) {
	// Create command first (this binds flags to variables with defaults)
	cmd := newCryptoVerifyCommand()

	// Set flags AFTER command creation
	cryptoData = "test message"
	cryptoSignature = "invalid!!!"
	defer func() {
		cryptoData = ""
		cryptoSignature = ""
	}()

	err := runCryptoVerifyCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signature")
}

// TestRunCryptoVerifyCommand_FactoryError tests verify with factory error
func TestRunCryptoVerifyCommand_FactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cryptoData = "test message"
	cryptoSignature = base64.StdEncoding.EncodeToString([]byte("signature"))
	defer func() {
		cryptoData = ""
		cryptoSignature = ""
	}()

	cmd := newCryptoVerifyCommand()
	err := runCryptoVerifyCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestRunCryptoVerifyCommand_Unimplemented tests verify returns unimplemented
func TestRunCryptoVerifyCommand_Unimplemented(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSVerifyFunc: func(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
			return nil, errors.NotImplemented("crypto.verify", "verify not yet implemented")
		},
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	cryptoData = "test message"
	cryptoSignature = base64.StdEncoding.EncodeToString([]byte("signature"))
	defer func() {
		cryptoData = ""
		cryptoSignature = ""
	}()

	cmd := newCryptoVerifyCommand()
	err := runCryptoVerifyCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not yet implemented")
}

// TestRunCryptoVerifyCommand_OutputFormats tests different output formats
func TestRunCryptoVerifyCommand_OutputFormats(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSVerifyFunc: func(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
			return nil, errors.NotImplemented("crypto.verify", "verify not yet implemented")
		},
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	defer func() {
		cryptoData = ""
		cryptoSignature = ""
		cryptoOutputFmt = "table"
	}()

	cryptoData = "test message"
	cryptoSignature = base64.StdEncoding.EncodeToString([]byte("signature"))

	tests := []struct {
		name   string
		format string
	}{
		{"json format", "json"},
		{"table format", "table"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoOutputFmt = tt.format
			cmd := newCryptoVerifyCommand()
			err := runCryptoVerifyCommand(cmd, []string{"test-key"})
			// Will fail with unimplemented, but tests format handling path
			assert.Error(t, err)
		})
	}
}

func TestCryptoHashCommandFlags(t *testing.T) {
	cmd := newCryptoHashCommand()
	assert.NotNil(t, cmd)

	dataFlag := cmd.Flags().Lookup("data")
	assert.NotNil(t, dataFlag)

	algorithmFlag := cmd.Flags().Lookup("algorithm")
	assert.NotNil(t, algorithmFlag)
	assert.Equal(t, "SHA256", algorithmFlag.DefValue)
}

func TestCryptoHashCommand(t *testing.T) {
	// Mock the client factory for tests that need "not yet implemented" errors
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSHashFunc: func(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
			return nil, errors.NotImplemented("crypto.hash", "hash not yet implemented")
		},
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "hash without data",
			args:        []string{},
			expectError: true,
			errorMsg:    "required flag",
		},
		{
			name:        "hash with data",
			args:        []string{"--data", "message to hash"},
			expectError: true,
			errorMsg:    "not yet implemented",
		},
		{
			name:        "hash with custom algorithm",
			args:        []string{"--data", "message", "--algorithm", "SHA512"},
			expectError: true,
			errorMsg:    "not yet implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCryptoHashCommand()
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

func TestCryptoRandomCommandFlags(t *testing.T) {
	cmd := newCryptoRandomCommand()
	assert.NotNil(t, cmd)

	bytesFlag := cmd.Flags().Lookup("bytes")
	assert.NotNil(t, bytesFlag)
	assert.Equal(t, "32", bytesFlag.DefValue)

	formatFlag := cmd.Flags().Lookup("format")
	assert.NotNil(t, formatFlag)
	assert.Equal(t, "base64", formatFlag.DefValue)
}

func TestCryptoRandomCommand(t *testing.T) {
	// Mock the client factory
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSGenerateRandomFunc: func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
			return &service.GenerateRandomResponse{
				RandomBytes: make([]byte, req.NumBytes),
				Timestamp:   time.Now(),
			}, nil
		},
	}
	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "random default",
			args:        []string{},
			expectError: false,
		},
		{
			name:        "random with custom bytes",
			args:        []string{"--bytes", "64"},
			expectError: false,
		},
		{
			name:        "random with hex format",
			args:        []string{"--bytes", "32", "--format", "hex"},
			expectError: false,
		},
		{
			name:        "random with invalid bytes (too small)",
			args:        []string{"--bytes", "0"},
			expectError: true,
			errorMsg:    "bytes must be between 1 and",
		},
		{
			name:        "random with invalid bytes (too large)",
			args:        []string{"--bytes", "2000000"},
			expectError: true,
			errorMsg:    "bytes must be between 1 and",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCryptoRandomCommand()
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

func TestCryptoDataKeyCommandFlags(t *testing.T) {
	cmd := newCryptoDataKeyCommand()
	assert.NotNil(t, cmd)

	bitsFlag := cmd.Flags().Lookup("bits")
	assert.NotNil(t, bitsFlag)
	assert.Equal(t, "256", bitsFlag.DefValue)
}

func TestCryptoDataKeyCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "datakey without key name",
			args:        []string{},
			expectError: true,
			errorMsg:    "accepts 1 arg",
		},
		{
			name:        "datakey with default bits",
			args:        []string{"test-key"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "datakey with 128 bits",
			args:        []string{"test-key", "--bits", "128"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "datakey with 192 bits",
			args:        []string{"test-key", "--bits", "192"},
			expectError: true,
			errorMsg:    "", // Connection error varies by environment
		},
		{
			name:        "datakey with invalid bits",
			args:        []string{"test-key", "--bits", "512"},
			expectError: true,
			errorMsg:    "bits must be 128, 192, or 256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newCryptoDataKeyCommand()
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

// TestRunCryptoDataKeyCommand_Success tests successful datakey generation
func TestRunCryptoDataKeyCommand_Success(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSGenerateDataKeyFunc: func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
			return &service.GenerateDataKeyResponse{
				KeyID:          req.KeyID,
				Plaintext:      []byte("plaintext-key"),
				CiphertextBlob: []byte("encrypted-key"),
				Timestamp:      time.Now(),
			}, nil
		},
	}

	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	defer func() {
		cryptoBits = 256
		cryptoOutputFmt = "table"
	}()

	tests := []struct {
		name string
		bits int
	}{
		{"128 bit key", 128},
		{"192 bit key", 192},
		{"256 bit key", 256},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoBits = tt.bits
			cmd := newCryptoDataKeyCommand()
			err := runCryptoDataKeyCommand(cmd, []string{"test-key"})
			assert.NoError(t, err)
		})
	}
}

// TestRunCryptoDataKeyCommand_InvalidBits tests datakey with invalid bits
func TestRunCryptoDataKeyCommand_InvalidBits(t *testing.T) {
	// Create command first (this binds flags to variables with defaults)
	cmd := newCryptoDataKeyCommand()

	// Set flags AFTER command creation
	cryptoBits = 512
	defer func() { cryptoBits = 256 }()

	err := runCryptoDataKeyCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bits must be 128, 192, or 256")
}

// TestRunCryptoDataKeyCommand_FactoryError tests datakey with factory error
func TestRunCryptoDataKeyCommand_FactoryError(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	cryptoClientFactory = func() (CryptoClient, error) {
		return nil, errors.Newf(errors.ErrUnavailable, "test", "connection refused")
	}

	cryptoBits = 256
	defer func() { cryptoBits = 256 }()

	cmd := newCryptoDataKeyCommand()
	err := runCryptoDataKeyCommand(cmd, []string{"test-key"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

// TestRunCryptoDataKeyCommand_OutputFormats tests different output formats
func TestRunCryptoDataKeyCommand_OutputFormats(t *testing.T) {
	origFactory := cryptoClientFactory
	defer func() { cryptoClientFactory = origFactory }()

	mockClient := &MockCryptoClient{
		KMSGenerateDataKeyFunc: func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
			return &service.GenerateDataKeyResponse{
				KeyID:          "test-key",
				Plaintext:      []byte("plaintext"),
				CiphertextBlob: []byte("ciphertext"),
				Timestamp:      time.Now(),
			}, nil
		},
	}

	cryptoClientFactory = func() (CryptoClient, error) {
		return mockClient, nil
	}

	defer func() {
		cryptoBits = 256
		cryptoOutputFmt = "table"
	}()

	tests := []struct {
		name   string
		format string
	}{
		{"json format", "json"},
		{"yaml format", "yaml"},
		{"table format", "table"},
		{"default format", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cryptoOutputFmt = tt.format
			cmd := newCryptoDataKeyCommand()
			err := runCryptoDataKeyCommand(cmd, []string{"test-key"})
			assert.NoError(t, err)
		})
	}
}

func TestFormatCryptoEncryptResponse(t *testing.T) {
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
		{
			name:         "yaml format",
			format:       "yaml",
			expectedText: []string{hex.EncodeToString(ciphertext)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatCryptoEncryptResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatCryptoDecryptResponse(t *testing.T) {
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
			expectedText: []string{string(plaintext)},
		},
		{
			name:         "yaml format",
			format:       "yaml",
			expectedText: []string{string(plaintext)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatCryptoDecryptResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatCryptoRewrapResponse(t *testing.T) {
	ciphertext := []byte("rewrapped data")
	resp := &service.RewrapResponse{
		KeyID:         "key-123",
		OldKeyVersion: 1,
		NewKeyVersion: 2,
		Ciphertext:    ciphertext,
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"key-123", "old_key_version", "new_key_version"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID", "key-123", "Old Version", "New Version"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{hex.EncodeToString(ciphertext)},
		},
		{
			name:         "yaml format",
			format:       "yaml",
			expectedText: []string{hex.EncodeToString(ciphertext)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatCryptoRewrapResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatCryptoSignResponse(t *testing.T) {
	signature := []byte("signature data")
	resp := &service.SignResponse{
		KeyID:         "key-123",
		KeyVersion:    1,
		HashAlgorithm: service.HashAlgSHA256,
		Signature:     signature,
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"key-123", "signature"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Key ID", "key-123", "Signature (base64)"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{base64.StdEncoding.EncodeToString(signature)},
		},
		{
			name:         "yaml format",
			format:       "yaml",
			expectedText: []string{base64.StdEncoding.EncodeToString(signature)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatCryptoSignResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatCryptoVerifyResponse(t *testing.T) {
	tests := []struct {
		name         string
		valid        bool
		format       string
		expectedText []string
	}{
		{
			name:         "valid signature json",
			valid:        true,
			format:       "json",
			expectedText: []string{"valid", "true"},
		},
		{
			name:         "invalid signature json",
			valid:        false,
			format:       "json",
			expectedText: []string{"valid", "false"},
		},
		{
			name:         "valid signature table",
			valid:        true,
			format:       "table",
			expectedText: []string{"Valid", "true"},
		},
		{
			name:         "valid signature default",
			valid:        true,
			format:       "",
			expectedText: []string{"Signature valid"},
		},
		{
			name:         "invalid signature default",
			valid:        false,
			format:       "",
			expectedText: []string{"Signature INVALID"},
		},
		{
			name:         "valid signature yaml",
			valid:        true,
			format:       "yaml",
			expectedText: []string{"Signature valid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &service.VerifyResponse{
				KeyID:      "key-123",
				KeyVersion: 1,
				Valid:      tt.valid,
			}

			var buf bytes.Buffer
			err := formatCryptoVerifyResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatCryptoHashResponse(t *testing.T) {
	hash := []byte("hash data")
	resp := &service.HashResponse{
		Algorithm: service.HashAlgSHA256,
		Hash:      hash,
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"algorithm", "hash"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"Algorithm", "Hash (hex)", hex.EncodeToString(hash)},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{hex.EncodeToString(hash)},
		},
		{
			name:         "yaml format",
			format:       "yaml",
			expectedText: []string{hex.EncodeToString(hash)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatCryptoHashResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatCryptoRandomResponse(t *testing.T) {
	randomBytes := []byte("random data here")
	resp := &service.GenerateRandomResponse{
		RandomBytes: randomBytes,
		Timestamp:   time.Now(),
	}

	tests := []struct {
		name         string
		outputFormat string
		format       string
		expectedText []string
	}{
		{
			name:         "hex json format",
			outputFormat: "hex",
			format:       "json",
			expectedText: []string{"random_bytes", hex.EncodeToString(randomBytes), "format"},
		},
		{
			name:         "base64 json format",
			outputFormat: "base64",
			format:       "json",
			expectedText: []string{"random_bytes", base64.StdEncoding.EncodeToString(randomBytes), "format"},
		},
		{
			name:         "hex table format",
			outputFormat: "hex",
			format:       "table",
			expectedText: []string{"Length", "Random (hex)", hex.EncodeToString(randomBytes)},
		},
		{
			name:         "base64 table format",
			outputFormat: "base64",
			format:       "table",
			expectedText: []string{"Length", "Random (base64)", base64.StdEncoding.EncodeToString(randomBytes)},
		},
		{
			name:         "hex default format",
			outputFormat: "hex",
			format:       "",
			expectedText: []string{hex.EncodeToString(randomBytes)},
		},
		{
			name:         "base64 default format",
			outputFormat: "base64",
			format:       "",
			expectedText: []string{base64.StdEncoding.EncodeToString(randomBytes)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatCryptoRandomResponse(resp, tt.outputFormat, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestFormatCryptoDataKeyResponse(t *testing.T) {
	plaintext := []byte("plaintext key data")
	ciphertext := []byte("ciphertext key data")
	resp := &service.GenerateDataKeyResponse{
		KeyID:          "key-123",
		Plaintext:      plaintext,
		CiphertextBlob: ciphertext,
		Timestamp:      time.Now(),
	}

	tests := []struct {
		name         string
		format       string
		expectedText []string
	}{
		{
			name:         "json format",
			format:       "json",
			expectedText: []string{"key_id", "plaintext", "ciphertext_blob"},
		},
		{
			name:         "table format",
			format:       "table",
			expectedText: []string{"DATA ENCRYPTION KEY GENERATED", "key-123", "Plaintext Key", "Wrapped Key"},
		},
		{
			name:         "default format",
			format:       "",
			expectedText: []string{"Plaintext:", "Wrapped:", base64.StdEncoding.EncodeToString(plaintext)},
		},
		{
			name:         "yaml format",
			format:       "yaml",
			expectedText: []string{"Plaintext:", "Wrapped:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := formatCryptoDataKeyResponse(resp, tt.format, &buf)
			require.NoError(t, err)

			output := buf.String()
			for _, expected := range tt.expectedText {
				assert.Contains(t, output, expected)
			}
		})
	}
}

func TestCryptoRandomGeneration(t *testing.T) {
	// Test that executeCryptoRandom actually generates random bytes
	req := &service.GenerateRandomRequest{
		NumBytes: 32,
	}

	// Create a mock client that generates actual random bytes
	callCount := 0
	mockClient := &MockCryptoClient{
		KMSGenerateRandomFunc: func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
			callCount++
			randomBytes := make([]byte, req.NumBytes)
			// Fill with different data for each call
			for i := range randomBytes {
				randomBytes[i] = byte((callCount * 17) + i)
			}
			return &service.GenerateRandomResponse{
				RandomBytes: randomBytes,
				Timestamp:   time.Now(),
			}, nil
		},
	}

	resp, err := executeCryptoRandom(context.Background(), mockClient, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, 32, len(resp.RandomBytes))
	assert.False(t, resp.Timestamp.IsZero())

	// Generate another set and ensure they're different
	resp2, err := executeCryptoRandom(context.Background(), mockClient, req)
	require.NoError(t, err)
	assert.NotEqual(t, resp.RandomBytes, resp2.RandomBytes)
}

func TestCryptoRandomBytesValidation(t *testing.T) {
	// Create a mock client
	mockClient := &MockCryptoClient{
		KMSGenerateRandomFunc: func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
			return &service.GenerateRandomResponse{
				RandomBytes: make([]byte, req.NumBytes),
				Timestamp:   time.Now(),
			}, nil
		},
	}

	tests := []struct {
		name      string
		numBytes  int
		expectErr bool
	}{
		{
			name:      "valid small",
			numBytes:  1,
			expectErr: false,
		},
		{
			name:      "valid medium",
			numBytes:  1024,
			expectErr: false,
		},
		{
			name:      "valid large",
			numBytes:  1024 * 1024,
			expectErr: false,
		},
		{
			name:      "zero bytes",
			numBytes:  0,
			expectErr: false, // Will fail at command level
		},
		{
			name:      "too large",
			numBytes:  1024*1024 + 1,
			expectErr: false, // Will fail at command level
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.numBytes > 0 && tt.numBytes <= 1024*1024 {
				req := &service.GenerateRandomRequest{
					NumBytes: tt.numBytes,
				}
				resp, err := executeCryptoRandom(context.Background(), mockClient, req)
				assert.NoError(t, err)
				assert.Equal(t, tt.numBytes, len(resp.RandomBytes))
			}
		})
	}
}

// TestExecuteCryptoRewrap tests that rewrap with nil client returns error
func TestExecuteCryptoRewrap(t *testing.T) {
	req := &service.RewrapRequest{
		KeyID:      "test-key",
		Ciphertext: []byte("encrypted"),
	}

	resp, err := executeCryptoRewrap(nil, nil, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid client type")
}

// TestExecuteCryptoSign tests that sign with nil client returns error
func TestExecuteCryptoSign(t *testing.T) {
	req := &service.SignRequest{
		KeyID:   "test-key",
		Message: []byte("message to sign"),
	}

	resp, err := executeCryptoSign(nil, nil, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid client type")
}

// TestExecuteCryptoVerify tests that verify with nil client returns error
func TestExecuteCryptoVerify(t *testing.T) {
	req := &service.VerifyRequest{
		KeyID:     "test-key",
		Message:   []byte("message"),
		Signature: []byte("signature"),
	}

	resp, err := executeCryptoVerify(nil, nil, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid client type")
}

// TestExecuteCryptoHash tests that hash with nil client returns error
func TestExecuteCryptoHash(t *testing.T) {
	req := &service.HashRequest{
		Data:      []byte("data to hash"),
		Algorithm: service.HashAlgSHA256,
	}

	resp, err := executeCryptoHash(nil, nil, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid client type")
}

// TestExecuteCryptoDecrypt_InvalidClientType tests decrypt with wrong client type
func TestExecuteCryptoDecrypt_InvalidClientType(t *testing.T) {
	req := &service.DecryptRequest{
		KeyID:      "test-key",
		Ciphertext: []byte("encrypted"),
	}

	// Pass nil as client - will fail on type assertion
	resp, err := executeCryptoDecrypt(nil, nil, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid client type")
}

// TestExecuteCryptoDataKey_InvalidClientType tests datakey with wrong client type
func TestExecuteCryptoDataKey_InvalidClientType(t *testing.T) {
	req := &service.GenerateDataKeyRequest{
		KeyID:         "test-key",
		KeyAttributes: &kctypes.KeyAttributes{SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
	}

	// Pass nil as client - will fail on type assertion
	resp, err := executeCryptoDataKey(nil, nil, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid client type")
}

// TestExecuteCryptoEncrypt_InvalidClientType tests encrypt with wrong client type
func TestExecuteCryptoEncrypt_InvalidClientType(t *testing.T) {
	req := &service.EncryptRequest{
		KeyID:     "test-key",
		Plaintext: []byte("data to encrypt"),
	}

	// Pass nil as client - will fail on type assertion
	resp, err := executeCryptoEncrypt(nil, nil, req)
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid client type")
}

// TestExecuteCryptoRandom_Success tests random byte generation
func TestExecuteCryptoRandom_Success(t *testing.T) {
	mockClient := &MockCryptoClient{
		KMSGenerateRandomFunc: func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
			return &service.GenerateRandomResponse{
				RandomBytes: make([]byte, req.NumBytes),
				Timestamp:   time.Now(),
			}, nil
		},
	}

	req := &service.GenerateRandomRequest{
		NumBytes: 32,
	}

	resp, err := executeCryptoRandom(context.Background(), mockClient, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.RandomBytes, 32)
	assert.False(t, resp.Timestamp.IsZero())
}

// TestExecuteCryptoRandom_ZeroBytes tests generating zero bytes
func TestExecuteCryptoRandom_ZeroBytes(t *testing.T) {
	mockClient := &MockCryptoClient{
		KMSGenerateRandomFunc: func(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
			return &service.GenerateRandomResponse{
				RandomBytes: make([]byte, req.NumBytes),
				Timestamp:   time.Now(),
			}, nil
		},
	}

	req := &service.GenerateRandomRequest{
		NumBytes: 0,
	}

	resp, err := executeCryptoRandom(context.Background(), mockClient, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Len(t, resp.RandomBytes, 0)
}

// TestExecuteCryptoEncrypt_Success tests encrypt with mock client
func TestExecuteCryptoEncrypt_Success(t *testing.T) {
	mockClient := &MockCryptoClient{
		KMSEncryptFunc: func(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error) {
			return &service.EncryptResponse{
				KeyID:      req.KeyID,
				KeyVersion: 1,
				Ciphertext: []byte("encrypted"),
			}, nil
		},
	}

	req := &service.EncryptRequest{
		KeyID:     "test-key",
		Plaintext: []byte("plaintext"),
	}

	resp, err := executeCryptoEncrypt(context.Background(), mockClient, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.KeyID)
	assert.Equal(t, []byte("encrypted"), resp.Ciphertext)
}

// TestExecuteCryptoDecrypt_Success tests decrypt with mock client
func TestExecuteCryptoDecrypt_Success(t *testing.T) {
	mockClient := &MockCryptoClient{
		KMSDecryptFunc: func(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error) {
			return &service.DecryptResponse{
				KeyID:      req.KeyID,
				KeyVersion: 1,
				Plaintext:  []byte("decrypted"),
			}, nil
		},
	}

	req := &service.DecryptRequest{
		KeyID:      "test-key",
		Ciphertext: []byte("encrypted"),
	}

	resp, err := executeCryptoDecrypt(context.Background(), mockClient, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.KeyID)
	assert.Equal(t, []byte("decrypted"), resp.Plaintext)
}

// TestExecuteCryptoDataKey_Success tests datakey generation with mock client
func TestExecuteCryptoDataKey_Success(t *testing.T) {
	mockClient := &MockCryptoClient{
		KMSGenerateDataKeyFunc: func(ctx context.Context, req *service.GenerateDataKeyRequest) (*service.GenerateDataKeyResponse, error) {
			return &service.GenerateDataKeyResponse{
				KeyID:          req.KeyID,
				Plaintext:      []byte("plaintext-key"),
				CiphertextBlob: []byte("encrypted-key"),
				Timestamp:      time.Now(),
			}, nil
		},
	}

	req := &service.GenerateDataKeyRequest{
		KeyID:         "test-key",
		KeyAttributes: &kctypes.KeyAttributes{SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
	}

	resp, err := executeCryptoDataKey(context.Background(), mockClient, req)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "test-key", resp.KeyID)
	assert.Equal(t, []byte("plaintext-key"), resp.Plaintext)
}

func TestCreateKMSClient_MCPTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientForMCP{mockTransport: &mockMCPTransport{}}, nil
	}

	client, err := createKMSClient()
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestCreateBackendClient_MCPTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientForMCP{mockTransport: &mockMCPTransport{}}, nil
	}

	client, err := createBackendClient()
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestCreateSafetyClient_MCPTransport(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClientForMCP{mockTransport: &mockMCPTransport{}}, nil
	}

	client, err := createSafetyClient()
	require.NoError(t, err)
	require.NotNil(t, client)
}

// MCP factory error path tests
func TestCreateKMSClient_MCPFactoryError(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	client, err := createKMSClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestCreateBackendClient_MCPFactoryError(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	client, err := createBackendClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

func TestCreateSafetyClient_MCPFactoryError(t *testing.T) {
	origTransport := transport
	origFactory := sdkClientFactory
	defer func() {
		transport = origTransport
		sdkClientFactory = origFactory
	}()

	transport = "mcp"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	client, err := createSafetyClient()
	assert.Error(t, err)
	assert.Nil(t, client)
}

// =============================================================================
// Secrets Transport-Aware Client QUIC Path
// =============================================================================

func TestCreateTransportAwareSecretsClient_QUICTransport(t *testing.T) {
	origTransport := transport
	defer func() { transport = origTransport }()

	transport = "quic"
	// Without the quic build tag, this returns ErrQUICNotCompiled
	_, err := createTransportAwareSecretsClient()
	assert.Error(t, err)
	// Check case-insensitively for "quic" in the error message
	assert.True(t, strings.Contains(strings.ToLower(err.Error()), "quic"),
		"expected error message to contain 'quic', got: %s", err.Error())
}

// =============================================================================
// Crypto Format Functions - Invalid Format + Default Format Paths
// =============================================================================

func TestFormatCryptoEncryptResponse_InvalidFormat(t *testing.T) {
	resp := &service.EncryptResponse{KeyID: "k1", KeyVersion: 1, Ciphertext: []byte("ct")}
	err := formatCryptoEncryptResponse(resp, "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatCryptoDecryptResponse_InvalidFormat(t *testing.T) {
	resp := &service.DecryptResponse{KeyID: "k1", KeyVersion: 1, Plaintext: []byte("pt")}
	err := formatCryptoDecryptResponse(resp, "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatCryptoRewrapResponse_InvalidFormat(t *testing.T) {
	resp := &service.RewrapResponse{KeyID: "k1", OldKeyVersion: 1, NewKeyVersion: 2, Ciphertext: []byte("ct")}
	err := formatCryptoRewrapResponse(resp, "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatCryptoSignResponse_InvalidFormat(t *testing.T) {
	resp := &service.SignResponse{KeyID: "k1", KeyVersion: 1, Signature: []byte("sig")}
	err := formatCryptoSignResponse(resp, "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatCryptoVerifyResponse_InvalidFormat(t *testing.T) {
	resp := &service.VerifyResponse{KeyID: "k1", KeyVersion: 1, Valid: true}
	err := formatCryptoVerifyResponse(resp, "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatCryptoHashResponse_InvalidFormat(t *testing.T) {
	resp := &service.HashResponse{Algorithm: "sha256", Hash: []byte("hash")}
	err := formatCryptoHashResponse(resp, "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatCryptoRandomResponse_InvalidFormat(t *testing.T) {
	resp := &service.GenerateRandomResponse{RandomBytes: []byte("random")}
	err := formatCryptoRandomResponse(resp, "hex", "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatCryptoDataKeyResponse_InvalidFormat(t *testing.T) {
	resp := &service.GenerateDataKeyResponse{KeyID: "k1", Plaintext: []byte("pt"), CiphertextBlob: []byte("ct")}
	err := formatCryptoDataKeyResponse(resp, "INVALID", &bytes.Buffer{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

// Default output format (plain text) paths for crypto
func TestFormatCryptoEncryptResponse_DefaultFormat(t *testing.T) {
	resp := &service.EncryptResponse{Ciphertext: []byte{0xab, 0xcd}}
	var buf bytes.Buffer
	err := formatCryptoEncryptResponse(resp, "raw", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "abcd")
}

func TestFormatCryptoDecryptResponse_DefaultFormat(t *testing.T) {
	resp := &service.DecryptResponse{Plaintext: []byte("hello")}
	var buf bytes.Buffer
	err := formatCryptoDecryptResponse(resp, "raw", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "hello")
}

func TestFormatCryptoRewrapResponse_DefaultFormat(t *testing.T) {
	resp := &service.RewrapResponse{Ciphertext: []byte{0x01, 0x02}}
	var buf bytes.Buffer
	err := formatCryptoRewrapResponse(resp, "raw", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "0102")
}

func TestFormatCryptoSignResponse_DefaultFormat(t *testing.T) {
	resp := &service.SignResponse{Signature: []byte("sig")}
	var buf bytes.Buffer
	err := formatCryptoSignResponse(resp, "raw", &buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func TestFormatCryptoVerifyResponse_DefaultInvalid(t *testing.T) {
	resp := &service.VerifyResponse{Valid: false}
	var buf bytes.Buffer
	err := formatCryptoVerifyResponse(resp, "raw", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "INVALID")
}

func TestFormatCryptoHashResponse_DefaultFormat(t *testing.T) {
	resp := &service.HashResponse{Hash: []byte{0xff}}
	var buf bytes.Buffer
	err := formatCryptoHashResponse(resp, "raw", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "ff")
}

func TestFormatCryptoRandomResponse_DefaultBase64(t *testing.T) {
	resp := &service.GenerateRandomResponse{RandomBytes: []byte("random")}
	var buf bytes.Buffer
	err := formatCryptoRandomResponse(resp, "base64", "raw", &buf)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func TestFormatCryptoRandomResponse_TableHex(t *testing.T) {
	resp := &service.GenerateRandomResponse{RandomBytes: []byte("random"), Timestamp: time.Now()}
	var buf bytes.Buffer
	err := formatCryptoRandomResponse(resp, "hex", "table", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "hex")
}

func TestFormatCryptoRandomResponse_JSONHex(t *testing.T) {
	resp := &service.GenerateRandomResponse{RandomBytes: []byte("random"), Timestamp: time.Now()}
	var buf bytes.Buffer
	err := formatCryptoRandomResponse(resp, "hex", "json", &buf)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "random_bytes")
}

// =============================================================================
// RAG Format Functions - Uncovered YAML and Error Paths
// =============================================================================

func TestFormatRAGQueryResponse_YAMLFormat(t *testing.T) {
	origFmt := outputFormat
	defer func() { outputFormat = origFmt }()
	outputFormat = "yaml"

	resp := &rag.QueryResponse{
		Context:     "test context",
		ResultCount: 2,
		Tokens:      100,
		Truncated:   false,
		Citations: []rag.Citation{
			{Index: 1, FilePath: "file.go", StartLine: 10, EndLine: 20, Score: 0.95},
		},
	}

	err := formatRAGQueryResponse(resp)
	assert.NoError(t, err)
}

func TestFormatRAGQueryResponse_InvalidFormat(t *testing.T) {
	origFmt := outputFormat
	defer func() { outputFormat = origFmt }()
	outputFormat = "INVALID"

	resp := &rag.QueryResponse{}
	err := formatRAGQueryResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatRAGBuildContextResponse_InvalidFormat(t *testing.T) {
	origFmt := outputFormat
	defer func() { outputFormat = origFmt }()
	outputFormat = "INVALID"

	resp := &rag.BuildContextResponse{}
	err := formatRAGBuildContextResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestFormatRAGBuildContextResponse_JSONFormat(t *testing.T) {
	origFmt := outputFormat
	defer func() { outputFormat = origFmt }()
	outputFormat = "json"

	resp := &rag.BuildContextResponse{
		Context:     "built context",
		ResultCount: 3,
		Tokens:      150,
		Truncated:   true,
		Citations: []rag.Citation{
			{Index: 1, FilePath: "main.go", StartLine: 1, EndLine: 5, Score: 0.99},
		},
	}

	err := formatRAGBuildContextResponse(resp)
	assert.NoError(t, err)
}

func TestFormatRAGBuildContextResponse_TableWithCitations(t *testing.T) {
	origFmt := outputFormat
	defer func() { outputFormat = origFmt }()
	outputFormat = "table"

	resp := &rag.BuildContextResponse{
		Context:     "context text",
		ResultCount: 1,
		Tokens:      50,
		Truncated:   false,
		Citations: []rag.Citation{
			{Index: 1, FilePath: "util.go", StartLine: 5, EndLine: 15, Score: 0.88},
			{Index: 2, FilePath: "handler.go", StartLine: 20, EndLine: 40, Score: 0.76},
		},
	}

	err := formatRAGBuildContextResponse(resp)
	assert.NoError(t, err)
}

// =============================================================================
// Backup Storage - buildLocalConfig with ProfileCredentials
// =============================================================================

func TestBuildLocalConfig_WithProfileCredentials(t *testing.T) {
	origPath := backupStoragePath
	defer func() { backupStoragePath = origPath }()

	backupStoragePath = ""
	builder := NewBackupStorageBuilder(&BackupStorageFlags{StorageType: "local"}, "", "")

	creds := &ProfileCredentials{
		Config: map[string]interface{}{
			"base_path": "/custom/backups",
		},
	}

	config := make(map[string]interface{})
	result, err := builder.buildLocalConfig(config, creds)
	require.NoError(t, err)
	assert.Equal(t, "/custom/backups", result["base_path"])
}

func TestBuildLocalConfig_WithProfileCredentialsNoBasePath(t *testing.T) {
	origPath := backupStoragePath
	defer func() { backupStoragePath = origPath }()

	backupStoragePath = ""
	builder := NewBackupStorageBuilder(&BackupStorageFlags{StorageType: "local"}, "", "")

	// ProfileCredentials with Config that has no base_path key
	creds := &ProfileCredentials{
		Config: map[string]interface{}{
			"other_key": "value",
		},
	}

	config := make(map[string]interface{})
	result, err := builder.buildLocalConfig(config, creds)
	require.NoError(t, err)
	assert.Equal(t, "/var/lib/quicbaas/backups", result["base_path"])
}

// =============================================================================
// Backup Storage - buildGCSConfig additional paths
// =============================================================================

func TestBuildGCSConfig_NoBucket(t *testing.T) {
	builder := NewBackupStorageBuilder(&BackupStorageFlags{StorageType: "gcs"}, "", "")

	config := make(map[string]interface{})
	_, err := builder.buildGCSConfig(config, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket name is required")
}

func TestBuildGCSConfig_WithProjectIDFromProfileCreds(t *testing.T) {
	builder := NewBackupStorageBuilder(&BackupStorageFlags{
		StorageType: "gcs",
		Bucket:      "test-bucket",
	}, "", "")

	creds := &ProfileCredentials{
		Config: map[string]interface{}{
			"project_id": "my-project",
		},
		Credentials: map[string]interface{}{
			"credentials_file": "/path/to/creds.json",
		},
	}

	config := make(map[string]interface{})
	result, err := builder.buildGCSConfig(config, creds)
	require.NoError(t, err)
	assert.Equal(t, "test-bucket", result["bucket"])
	assert.Equal(t, "my-project", result["project_id"])
	assert.Equal(t, "/path/to/creds.json", result["credentials_file"])
}

// =============================================================================
// Backup Storage - buildMinIOConfig additional paths
// =============================================================================

func TestBuildMinIOConfig_NoBucket(t *testing.T) {
	builder := NewBackupStorageBuilder(&BackupStorageFlags{StorageType: "minio"}, "", "")

	config := make(map[string]interface{})
	_, err := builder.buildMinIOConfig(config, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucket name is required")
}

func TestBuildMinIOConfig_NoEndpoint(t *testing.T) {
	builder := NewBackupStorageBuilder(&BackupStorageFlags{
		StorageType: "minio",
		Bucket:      "b",
	}, "", "")

	config := make(map[string]interface{})
	_, err := builder.buildMinIOConfig(config, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint is required")
}

func TestBuildMinIOConfig_WithProfileCredsAndRegion(t *testing.T) {
	builder := NewBackupStorageBuilder(&BackupStorageFlags{
		StorageType: "minio",
		Bucket:      "test-bucket",
		Endpoint:    "http://minio:9000",
		Region:      "us-east-1",
	}, "", "")

	creds := &ProfileCredentials{
		Region: "eu-west-1",
		Credentials: map[string]interface{}{
			"access_key": "mykey",
			"secret_key": "mysecret",
		},
		Config: map[string]interface{}{
			"use_ssl": true,
		},
	}

	config := make(map[string]interface{})
	result, err := builder.buildMinIOConfig(config, creds)
	require.NoError(t, err)
	assert.Equal(t, "test-bucket", result["bucket"])
	assert.Equal(t, "http://minio:9000", result["endpoint"])
	assert.Equal(t, "us-east-1", result["region"])
	assert.Equal(t, "mykey", result["access_key_id"])
	assert.Equal(t, "mysecret", result["secret_access_key"])
	assert.Equal(t, true, result["use_ssl"])
}

func TestBuildMinIOConfig_EndpointFromProfile(t *testing.T) {
	builder := NewBackupStorageBuilder(&BackupStorageFlags{
		StorageType: "minio",
		Bucket:      "b",
	}, "", "")

	creds := &ProfileCredentials{
		Config: map[string]interface{}{
			"endpoint": "http://minio-from-profile:9000",
		},
	}

	config := make(map[string]interface{})
	result, err := builder.buildMinIOConfig(config, creds)
	require.NoError(t, err)
	assert.Equal(t, "http://minio-from-profile:9000", result["endpoint"])
}

// =============================================================================
// ParseKeyToID - Numeric and String Key Paths
// =============================================================================

func TestParseKeyToID_NumericKey(t *testing.T) {
	id := ParseKeyToID("12345")
	assert.Equal(t, uint64(12345), id)
}

func TestParseKeyToID_StringKey(t *testing.T) {
	id := ParseKeyToID("user-john")
	assert.NotZero(t, id)
}

func TestFormatIDToKey_Coverage(t *testing.T) {
	key := FormatIDToKey(12345)
	assert.Equal(t, "12345", key)
}

// =============================================================================
// Data Command - runPutCommand error paths
// =============================================================================

func TestRunPutCommand_InvalidJSON(t *testing.T) {
	origFactory := sdkClientFactory
	origJSONInput := jsonInput
	origConsistency := consistency
	origOutputFmt := outputFormat
	defer func() {
		sdkClientFactory = origFactory
		jsonInput = origJSONInput
		consistency = origConsistency
		outputFormat = origOutputFmt
	}()

	jsonInput = true

	cmd := &cobra.Command{}
	err := runPutCommand(cmd, []string{"user", "john", "not-valid-json"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestRunPutCommand_InvalidConsistency(t *testing.T) {
	origFactory := sdkClientFactory
	origConsistency := consistency
	origOutputFmt := outputFormat
	origJSONInput := jsonInput
	defer func() {
		sdkClientFactory = origFactory
		consistency = origConsistency
		outputFormat = origOutputFmt
		jsonInput = origJSONInput
	}()

	jsonInput = false
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClient{kvClient: &mockKVClient{}}, nil
	}
	consistency = "INVALID_CONSISTENCY"

	cmd := &cobra.Command{}
	err := runPutCommand(cmd, []string{"user", "john", "value"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "consistency")
}

func TestRunPutCommand_InvalidOutputFormat(t *testing.T) {
	origFactory := sdkClientFactory
	origConsistency := consistency
	origOutputFmt := outputFormat
	origJSONInput := jsonInput
	defer func() {
		sdkClientFactory = origFactory
		consistency = origConsistency
		outputFormat = origOutputFmt
		jsonInput = origJSONInput
	}()

	jsonInput = false
	consistency = "linearizable"
	outputFormat = "INVALID_FORMAT"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClient{kvClient: &mockKVClient{}}, nil
	}

	cmd := &cobra.Command{}
	err := runPutCommand(cmd, []string{"user", "john", "value"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown output format")
}

func TestRunPutCommand_ClientFactoryError(t *testing.T) {
	origFactory := sdkClientFactory
	origJSONInput := jsonInput
	defer func() {
		sdkClientFactory = origFactory
		jsonInput = origJSONInput
	}()

	jsonInput = false
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, assert.AnError
	}

	cmd := &cobra.Command{}
	err := runPutCommand(cmd, []string{"user", "john", "value"})
	assert.Error(t, err)
}

func TestRunPutCommand_PutError(t *testing.T) {
	origFactory := sdkClientFactory
	origConsistency := consistency
	origJSONInput := jsonInput
	defer func() {
		sdkClientFactory = origFactory
		consistency = origConsistency
		jsonInput = origJSONInput
	}()

	jsonInput = false
	consistency = "linearizable"
	sdkClientFactory = func() (quicbaas.Client, error) {
		return &mockSDKClient{
			kvClient: &mockKVClient{
				PutFunc: func(ctx context.Context, req *service.PutRequest) (*service.PutResponse, error) {
					return nil, assert.AnError
				},
			},
		}, nil
	}

	cmd := &cobra.Command{}
	err := runPutCommand(cmd, []string{"user", "john", "value"})
	assert.Error(t, err)
}

// =============================================================================
// Secrets Parse Functions - Happy Path (covers the successful toJSONMap paths)
// =============================================================================

func TestParseSecretsStoreResponse_HappyPath(t *testing.T) {
	now := time.Now().Unix()
	input := toolResp(map[string]interface{}{
		"secret_id": "db/prod",
		"version":   float64(1),
		"timestamp": float64(now),
	})

	resp, err := parseSecretsStoreResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.Equal(t, uint64(1), resp.Version)
}

func TestParseSecretsGetResponse_HappyPath(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secret_id": "db/prod",
		"version":   float64(1),
		"data":      "c2VjcmV0LWRhdGE=", // base64("secret-data")
		"metadata": map[string]interface{}{
			"env": "production",
		},
	})

	resp, err := parseSecretsGetResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.Equal(t, []byte("secret-data"), resp.Data)
	assert.Equal(t, "production", resp.Metadata["env"])
}

func TestParseSecretsGetResponse_NonBase64Data(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secret_id": "db/prod",
		"data":      "not-base64!!!",
	})

	resp, err := parseSecretsGetResponse(input)
	require.NoError(t, err)
	assert.Equal(t, []byte("not-base64!!!"), resp.Data)
}

func TestParseSecretsGetResponse_NoData(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secret_id": "db/prod",
	})

	resp, err := parseSecretsGetResponse(input)
	require.NoError(t, err)
	assert.Nil(t, resp.Data)
}

func TestParseSecretsDeleteResponse_HappyPath(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secret_id": "db/prod",
		"deleted":   true,
	})

	resp, err := parseSecretsDeleteResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.True(t, resp.Deleted)
}

func TestParseSecretsListResponse_WithSecrets(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secrets": []interface{}{
			map[string]interface{}{
				"secret_id":       "db/prod",
				"name":            "db-password",
				"description":     "Production DB password",
				"current_version": float64(3),
			},
		},
		"total_count": float64(1),
		"has_more":    false,
	})

	resp, err := parseSecretsListResponse(input)
	require.NoError(t, err)
	require.Len(t, resp.Secrets, 1)
	assert.Equal(t, "db/prod", resp.Secrets[0].SecretID)
	assert.Equal(t, "db-password", resp.Secrets[0].Name)
	assert.Equal(t, uint64(3), resp.Secrets[0].CurrentVersion)
}

func TestParseSecretsRotateResponse_HappyPath(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secret_id":   "db/prod",
		"old_version": float64(1),
		"new_version": float64(2),
	})

	resp, err := parseSecretsRotateResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.Equal(t, uint64(1), resp.OldVersion)
	assert.Equal(t, uint64(2), resp.NewVersion)
}

func TestParseSecretsListVersionsResponse_WithVersions(t *testing.T) {
	now := time.Now().Unix()
	input := toolResp(map[string]interface{}{
		"versions": []interface{}{
			map[string]interface{}{
				"secret_id":  "db/prod",
				"version":    float64(1),
				"created_at": float64(now),
				"created_by": "admin",
			},
			map[string]interface{}{
				"secret_id":  "db/prod",
				"version":    float64(2),
				"created_at": float64(now),
				"created_by": "system",
			},
		},
		"timestamp": float64(now),
	})

	resp, err := parseSecretsListVersionsResponse(input)
	require.NoError(t, err)
	require.Len(t, resp.Versions, 2)
	assert.Equal(t, uint64(1), resp.Versions[0].Version)
	assert.Equal(t, "admin", resp.Versions[0].CreatedBy)
	assert.Equal(t, uint64(2), resp.Versions[1].Version)
}

func TestParseSecretsRollbackResponse_HappyPath(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secret_id":   "db/prod",
		"new_version": float64(3),
	})

	resp, err := parseSecretsRollbackResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.Equal(t, uint64(3), resp.NewVersion)
}

func TestParseSecretsUpdateStatusResponse_HappyPath(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secret_id":  "db/prod",
		"old_status": "active",
		"new_status": "disabled",
	})

	resp, err := parseSecretsUpdateStatusResponse(input)
	require.NoError(t, err)
	assert.Equal(t, "db/prod", resp.SecretID)
	assert.Equal(t, service.SecretStatus("active"), resp.OldStatus)
	assert.Equal(t, service.SecretStatus("disabled"), resp.NewStatus)
}

func TestParseSecretsGetByPrefixResponse_HappyPath(t *testing.T) {
	input := toolResp(map[string]interface{}{
		"secrets": map[string]interface{}{
			"db/prod": map[string]interface{}{
				"password": "c2VjcmV0", // base64("secret")
			},
		},
		"metadata": map[string]interface{}{
			"db/prod": map[string]interface{}{
				"env": "production",
			},
		},
		"total_count": float64(1),
		"has_more":    false,
	})

	resp, err := parseSecretsGetByPrefixResponse(input)
	require.NoError(t, err)
	assert.Equal(t, 1, resp.TotalCount)
	assert.Contains(t, resp.Secrets, "db/prod")
	assert.Contains(t, resp.Metadata, "db/prod")
	assert.Equal(t, "production", resp.Metadata["db/prod"]["env"])
}

func TestParseSecretsTimestamp_Zero(t *testing.T) {
	result := parseSecretsTimestamp(0)
	assert.True(t, result.IsZero())
}

func TestParseSecretsTimestamp_NonZero(t *testing.T) {
	now := time.Now().Unix()
	result := parseSecretsTimestamp(now)
	assert.Equal(t, now, result.Unix())
}

// =============================================================================
// Secrets Parse Functions - Error Paths (unmarshal failure)
// =============================================================================

func TestParseSecretsStoreResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsStoreResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsGetResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsGetResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsDeleteResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsDeleteResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsListResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsListResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsGetByPrefixResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsGetByPrefixResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsRotateResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsRotateResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsListVersionsResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsListVersionsResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsRollbackResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsRollbackResponse("not-a-map")
	assert.Error(t, err)
}

func TestParseSecretsUpdateStatusResponse_UnmarshalError(t *testing.T) {
	_, err := parseSecretsUpdateStatusResponse("not-a-map")
	assert.Error(t, err)
}

// =============================================================================
// Mock TS Client Methods - Cover nil function fallback returns
// =============================================================================

func TestMockTimeSeriesClient_DefaultReturns(t *testing.T) {
	mock := &MockTimeSeriesClient{}

	resp1, err := mock.TSWritePoint(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp1)

	resp2, err := mock.TSWriteBatch(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp2)

	resp3, err := mock.TSQuery(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp3)

	resp4, err := mock.TSDeleteSeries(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp4)

	resp5, err := mock.TSGetRetentionPolicy(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp5)

	resp6, err := mock.TSSetRetentionPolicy(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp6)

	resp7, err := mock.TSListMetrics(context.Background(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, resp7)

	err = mock.Close()
	assert.NoError(t, err)
}
