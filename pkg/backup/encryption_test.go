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

package backup

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEncryptor(t *testing.T) {
	tests := []struct {
		name        string
		etype       EncryptionType
		key         []byte
		wantErr     bool
		errContains string
	}{
		{
			name:    "none encryption",
			etype:   EncryptionNone,
			key:     nil,
			wantErr: false,
		},
		{
			name:    "AES-256-GCM with valid key",
			etype:   EncryptionAES256GCM,
			key:     make([]byte, 32),
			wantErr: false,
		},
		{
			name:        "AES-256-GCM with short key",
			etype:       EncryptionAES256GCM,
			key:         make([]byte, 16),
			wantErr:     true,
			errContains: "invalid key size for AES-256",
		},
		{
			name:        "AES-256-GCM with long key",
			etype:       EncryptionAES256GCM,
			key:         make([]byte, 64),
			wantErr:     true,
			errContains: "invalid key size for AES-256",
		},
		{
			name:        "AES-256-GCM with nil key",
			etype:       EncryptionAES256GCM,
			key:         nil,
			wantErr:     true,
			errContains: "invalid key size for AES-256",
		},
		{
			name:        "AES-256-GCM with empty key",
			etype:       EncryptionAES256GCM,
			key:         []byte{},
			wantErr:     true,
			errContains: "invalid key size for AES-256",
		},
		{
			name:        "invalid encryption type",
			etype:       EncryptionType("invalid"),
			key:         make([]byte, 32),
			wantErr:     true,
			errContains: "unsupported encryption type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encryptor, err := NewEncryptor(tt.etype, tt.key)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, encryptor)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, encryptor)
		})
	}
}

func TestNewAES256GCMEncryptor(t *testing.T) {
	tests := []struct {
		name        string
		key         []byte
		wantErr     bool
		errContains string
	}{
		{
			name:    "valid 32-byte key",
			key:     make([]byte, 32),
			wantErr: false,
		},
		{
			name:        "16-byte key",
			key:         make([]byte, 16),
			wantErr:     true,
			errContains: "32 bytes",
		},
		{
			name:        "64-byte key",
			key:         make([]byte, 64),
			wantErr:     true,
			errContains: "32 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encryptor, err := NewAES256GCMEncryptor(tt.key)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, encryptor)
		})
	}
}

// TestEncryption_EmptyData moved to encryption_integration_test.go
// TestEncryption_LargeData moved to encryption_integration_test.go
// TestEncryption_MultipleWrites moved to encryption_integration_test.go
// TestEncryption_WrongKey moved to encryption_integration_test.go
// TestEncryption_CorruptedCiphertext moved to encryption_integration_test.go
// TestEncryption_CorruptedNonce moved to encryption_integration_test.go
// TestEncryption_TruncatedData moved to encryption_integration_test.go

func TestEncryption_InsufficientNonce(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	// Create data with insufficient length for nonce
	insufficientData := []byte("short")

	reader, err := encryptor.Decrypt(bytes.NewReader(insufficientData))
	if err != nil {
		// Some implementations fail immediately
		assert.Error(t, err)
		return
	}

	// Others fail on read
	_, err = io.ReadAll(reader)
	assert.Error(t, err)
}

func TestNoneEncryptor(t *testing.T) {
	encryptor := &noneEncryptor{}
	data := []byte("test data")

	// Encrypt (should be no-op)
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(data)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	assert.Equal(t, data, encrypted.Bytes())

	// Decrypt (should be no-op)
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, data, decrypted)
}

func TestGCMWriter_DoubleClose(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write([]byte("test"))
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Close again - should be idempotent
	err = writer.Close()
	require.NoError(t, err)
}

func TestGCMWriter_WriteAfterClose(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Write after close - should fail
	_, err = writer.Write([]byte("test"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestComputeChecksum(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected string
	}{
		{
			name:     "empty data",
			data:     []byte{},
			expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:     "hello world",
			data:     []byte("hello world"),
			expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
		{
			name: "binary data",
			data: []byte{0x00, 0x01, 0x02, 0x03},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checksum := ComputeChecksum(tt.data)
			assert.NotEmpty(t, checksum)
			assert.Len(t, checksum, 64) // SHA-256 hex string

			if tt.expected != "" {
				assert.Equal(t, tt.expected, checksum)
			}

			// Verify checksum is deterministic
			checksum2 := ComputeChecksum(tt.data)
			assert.Equal(t, checksum, checksum2)
		})
	}
}

func TestVerifyChecksum(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		checksum string
		expected bool
	}{
		{
			name:     "valid checksum",
			data:     []byte("test data"),
			checksum: ComputeChecksum([]byte("test data")),
			expected: true,
		},
		{
			name:     "invalid checksum",
			data:     []byte("test data"),
			checksum: "invalid",
			expected: false,
		},
		{
			name:     "empty data valid",
			data:     []byte{},
			checksum: ComputeChecksum([]byte{}),
			expected: true,
		},
		{
			name:     "different data",
			data:     []byte("test data"),
			checksum: ComputeChecksum([]byte("different data")),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := VerifyChecksum(tt.data, tt.checksum)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// errorWriterEncrypt always fails on write.
type errorWriterEncrypt struct{}

func (w *errorWriterEncrypt) Write(p []byte) (n int, err error) {
	return 0, errors.New("write error")
}

func TestEncryption_WriteError(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	writer, err := encryptor.Encrypt(&errorWriterEncrypt{})
	// Encrypt might fail immediately if it tries to write nonce
	if err != nil {
		assert.Error(t, err)
		return
	}

	// Write should succeed (buffered)
	_, err = writer.Write([]byte("test data"))
	require.NoError(t, err)

	// Close should fail when trying to write
	err = writer.Close()
	assert.Error(t, err)
}

// TestEncryption_DifferentKeys moved to encryption_integration_test.go
