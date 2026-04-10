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
// +build integration

package backup

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncryption_EmptyData tests actual encryption/decryption with empty data
func TestEncryption_EmptyData(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	// Encrypt empty data
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Should still have nonce
	assert.Greater(t, encrypted.Len(), 0)

	// Decrypt
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Empty(t, decrypted)
}

// TestEncryption_LargeData tests encryption with 1MB of data
func TestEncryption_LargeData(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	// Create 1MB of data
	largeData := make([]byte, 1024*1024)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	// Encrypt
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(largeData)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Encrypted data should be slightly larger (nonce + auth tag)
	assert.Greater(t, encrypted.Len(), len(largeData))

	// Decrypt
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, largeData, decrypted)
}

// TestEncryption_MultipleWrites tests encryption with multiple writes
func TestEncryption_MultipleWrites(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	chunks := [][]byte{
		[]byte("First chunk of data "),
		[]byte("Second chunk of data "),
		[]byte("Third chunk of data"),
	}
	var expected bytes.Buffer
	for _, chunk := range chunks {
		expected.Write(chunk)
	}

	// Encrypt with multiple writes
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	for _, chunk := range chunks {
		_, err = writer.Write(chunk)
		require.NoError(t, err)
	}

	err = writer.Close()
	require.NoError(t, err)

	// Decrypt
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(reader)
	require.NoError(t, err)

	assert.Equal(t, expected.Bytes(), decrypted)
}

// TestEncryption_WrongKey tests decryption with wrong key
func TestEncryption_WrongKey(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	for i := range key2 {
		key2[i] = byte(255 - i)
	}

	encryptor1, err := NewEncryptor(EncryptionAES256GCM, key1)
	require.NoError(t, err)

	encryptor2, err := NewEncryptor(EncryptionAES256GCM, key2)
	require.NoError(t, err)

	data := []byte("secret data")

	// Encrypt with key1
	var encrypted bytes.Buffer
	writer, err := encryptor1.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(data)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Try to decrypt with key2 - should fail
	reader, err := encryptor2.Decrypt(&encrypted)
	require.NoError(t, err)

	_, err = io.ReadAll(reader)
	assert.Error(t, err, "Decryption with wrong key should fail")
}

// TestEncryption_CorruptedCiphertext tests decryption with corrupted ciphertext
func TestEncryption_CorruptedCiphertext(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	data := []byte("test data")

	// Encrypt
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(data)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Corrupt the ciphertext
	ciphertext := encrypted.Bytes()
	if len(ciphertext) > 20 {
		ciphertext[20] ^= 0xFF // Flip bits
	}

	// Try to decrypt - should fail
	reader, err := encryptor.Decrypt(bytes.NewReader(ciphertext))
	require.NoError(t, err)

	_, err = io.ReadAll(reader)
	assert.Error(t, err, "Decryption of corrupted data should fail")
}

// TestEncryption_CorruptedNonce tests decryption with corrupted nonce
func TestEncryption_CorruptedNonce(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	data := []byte("test data")

	// Encrypt
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(data)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Corrupt the nonce (first 12 bytes)
	ciphertext := encrypted.Bytes()
	if len(ciphertext) > 0 {
		ciphertext[0] ^= 0xFF
	}

	// Try to decrypt - should fail
	reader, err := encryptor.Decrypt(bytes.NewReader(ciphertext))
	require.NoError(t, err)

	_, err = io.ReadAll(reader)
	assert.Error(t, err, "Decryption with corrupted nonce should fail")
}

// TestEncryption_TruncatedData tests decryption with truncated data
func TestEncryption_TruncatedData(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewEncryptor(EncryptionAES256GCM, key)
	require.NoError(t, err)

	data := []byte("test data that will be truncated")

	// Encrypt
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(data)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Truncate the ciphertext
	truncated := encrypted.Bytes()[:encrypted.Len()/2]

	// Try to decrypt - should fail
	reader, err := encryptor.Decrypt(bytes.NewReader(truncated))
	require.NoError(t, err)

	_, err = io.ReadAll(reader)
	assert.Error(t, err, "Decryption of truncated data should fail")
}

// TestEncryption_DifferentKeys tests that different keys produce different ciphertexts
func TestEncryption_DifferentKeys(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)

	// Make keys different
	key1[0] = 1
	key2[0] = 2

	encryptor1, err := NewEncryptor(EncryptionAES256GCM, key1)
	require.NoError(t, err)

	encryptor2, err := NewEncryptor(EncryptionAES256GCM, key2)
	require.NoError(t, err)

	data := []byte("test data")

	// Encrypt with key1
	var encrypted1 bytes.Buffer
	writer1, err := encryptor1.Encrypt(&encrypted1)
	require.NoError(t, err)
	_, err = writer1.Write(data)
	require.NoError(t, err)
	err = writer1.Close()
	require.NoError(t, err)

	// Encrypt with key2
	var encrypted2 bytes.Buffer
	writer2, err := encryptor2.Encrypt(&encrypted2)
	require.NoError(t, err)
	_, err = writer2.Write(data)
	require.NoError(t, err)
	err = writer2.Close()
	require.NoError(t, err)

	// Encrypted data should be different (different nonces)
	assert.NotEqual(t, encrypted1.Bytes(), encrypted2.Bytes())
}
