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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAES256GCMEncryptor_EncryptWriteNonceError(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	// Use a writer that fails immediately
	errorWriter := &errorWriter{}

	// Encrypt should fail when trying to write nonce
	writer, err := encryptor.Encrypt(errorWriter)
	assert.Error(t, err)
	assert.Nil(t, writer)
	assert.Contains(t, err.Error(), "nonce")
}

func TestAES256GCMEncryptor_DecryptReadNonceError(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	// Use a reader with insufficient data for nonce
	shortReader := bytes.NewReader([]byte("short"))

	reader, err := encryptor.Decrypt(shortReader)
	assert.Error(t, err)
	assert.Nil(t, reader)
	assert.Contains(t, err.Error(), "nonce")
}

func TestGCMReader_ReadDecryptError(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	// Create a valid nonce but invalid ciphertext
	block, err := aes.NewCipher(key)
	require.NoError(t, err)

	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)

	nonce := make([]byte, aead.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)

	// Create data with nonce followed by invalid ciphertext
	var buf bytes.Buffer
	buf.Write(nonce)
	buf.Write([]byte("invalid ciphertext data"))

	// Decrypt should fail when trying to read/decrypt
	reader, err := encryptor.Decrypt(&buf)
	require.NoError(t, err)

	// Error should occur on read
	_, err = io.ReadAll(reader)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

func TestGCMReader_ReadCiphertextError(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	// Create a valid nonce
	block, err := aes.NewCipher(key)
	require.NoError(t, err)

	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)

	nonce := make([]byte, aead.NonceSize())
	_, err = io.ReadFull(rand.Reader, nonce)
	require.NoError(t, err)

	// Create reader that fails after nonce
	var buf bytes.Buffer
	buf.Write(nonce)

	// Use an error reader wrapper
	errorAfterNonce := &errorAfterBytesReader{
		data:       buf.Bytes(),
		bytesRead:  0,
		errorAfter: len(nonce),
	}

	reader, err := encryptor.Decrypt(errorAfterNonce)
	require.NoError(t, err)

	// Should fail when trying to read ciphertext
	_, err = io.ReadAll(reader)
	assert.Error(t, err)
}

func TestGCMReader_MultipleReads(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("test data for multiple reads")

	// Encrypt
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(plaintext)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Decrypt with multiple small reads
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	// Read in chunks
	chunk1 := make([]byte, 5)
	n1, err := reader.Read(chunk1)
	require.NoError(t, err)
	assert.Equal(t, 5, n1)

	chunk2 := make([]byte, 10)
	n2, err := reader.Read(chunk2)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("unexpected error: %v", err)
	}
	assert.Greater(t, n2, 0)

	// Read rest
	rest, err := io.ReadAll(reader)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}

	// Combine all chunks
	result := append(chunk1[:n1], chunk2[:n2]...)
	result = append(result, rest...)

	assert.Equal(t, plaintext, result)
}

func TestGCMReader_EOF(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("test")

	// Encrypt
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(plaintext)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Decrypt
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	// Read all data
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, plaintext, data)

	// Try to read again - should get EOF
	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	assert.Equal(t, 0, n)
	assert.Equal(t, io.EOF, err)
}

func TestGCMReader_Close(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	plaintext := []byte("test")

	// Encrypt
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	_, err = writer.Write(plaintext)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Decrypt
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	// Close reader
	err = reader.Close()
	require.NoError(t, err)
}

func TestAES256GCMEncryptor_EncryptEmptyData(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	// Encrypt empty data
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	// Decrypt
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Empty(t, data)
}

func TestGCMWriter_MultipleWrites(t *testing.T) {
	key := make([]byte, 32)
	encryptor, err := NewAES256GCMEncryptor(key)
	require.NoError(t, err)

	// Encrypt with multiple writes
	var encrypted bytes.Buffer
	writer, err := encryptor.Encrypt(&encrypted)
	require.NoError(t, err)

	// Write in chunks
	chunks := [][]byte{
		[]byte("first "),
		[]byte("second "),
		[]byte("third"),
	}

	for _, chunk := range chunks {
		_, err = writer.Write(chunk)
		require.NoError(t, err)
	}

	err = writer.Close()
	require.NoError(t, err)

	// Decrypt and verify
	reader, err := encryptor.Decrypt(&encrypted)
	require.NoError(t, err)

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("first second third"), data)
}

// errorAfterBytesReader reads successfully for N bytes then fails.
type errorAfterBytesReader struct {
	data       []byte
	bytesRead  int
	errorAfter int
}

func (r *errorAfterBytesReader) Read(p []byte) (n int, err error) {
	if r.bytesRead >= r.errorAfter {
		return 0, errors.New("read error")
	}

	remaining := r.errorAfter - r.bytesRead
	if len(p) > remaining {
		p = p[:remaining]
	}

	n = copy(p, r.data[r.bytesRead:])
	r.bytesRead += n

	if r.bytesRead >= r.errorAfter {
		return n, errors.New("read error after bytes")
	}

	return n, nil
}

func TestNoneEncryptor_Encrypt_Writer(t *testing.T) {
	encryptor := &noneEncryptor{}

	var buf bytes.Buffer
	writer, err := encryptor.Encrypt(&buf)
	require.NoError(t, err)

	data := []byte("test data")
	n, err := writer.Write(data)
	require.NoError(t, err)
	assert.Equal(t, len(data), n)

	err = writer.Close()
	require.NoError(t, err)

	assert.Equal(t, data, buf.Bytes())
}

func TestNoneEncryptor_Decrypt_Reader(t *testing.T) {
	encryptor := &noneEncryptor{}

	data := []byte("test data")
	reader := bytes.NewReader(data)

	result, err := encryptor.Decrypt(reader)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(result)
	require.NoError(t, err)

	assert.Equal(t, data, decrypted)
}
