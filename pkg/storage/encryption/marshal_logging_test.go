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

package encryption

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Test logging wrapper functions.
type TestUserForLogging struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email" encrypt:"aes256"`
	Password string `json:"password" encrypt:"aes256"`
}

type TestInvalidMarshalType struct {
	Invalid int `json:"invalid" encrypt:"aes128"`
}

func TestMarshalWithEncryptionAndLogging_Success(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	user := &TestUserForLogging{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	// Create a test logger
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	require.NoError(t, err)

	// Test with logger
	data, err := MarshalWithEncryptionAndLogging(user, key, log)
	require.NoError(t, err)
	assert.NotNil(t, data)

	// Verify we can unmarshal it back
	result := &TestUserForLogging{}
	err = UnmarshalWithDecryption(data, result, key)
	require.NoError(t, err)
	assert.Equal(t, user.Email, result.Email)
	assert.Equal(t, user.Password, result.Password)
}

func TestMarshalWithEncryptionAndLogging_NilLogger(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	user := &TestUserForLogging{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	// Test with nil logger (should not crash)
	data, err := MarshalWithEncryptionAndLogging(user, key, nil)
	require.NoError(t, err)
	assert.NotNil(t, data)
}

func TestMarshalWithEncryptionAndLogging_Error(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	require.NoError(t, err)

	// Test with invalid entity type (should log error)
	invalid := &TestInvalidMarshalType{
		Invalid: 123,
	}
	data, err := MarshalWithEncryptionAndLogging(invalid, key, log)
	assert.Error(t, err)
	assert.Nil(t, data)
}

func TestUnmarshalWithDecryptionAndLogging_Success(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	user := &TestUserForLogging{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	require.NoError(t, err)

	// Marshal first
	data, err := MarshalWithEncryption(user, key)
	require.NoError(t, err)

	// Test unmarshal with logging
	result := &TestUserForLogging{}
	err = UnmarshalWithDecryptionAndLogging(data, result, key, log)
	require.NoError(t, err)
	assert.Equal(t, user.Email, result.Email)
	assert.Equal(t, user.Password, result.Password)
}

func TestUnmarshalWithDecryptionAndLogging_NilLogger(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	user := &TestUserForLogging{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	// Marshal first
	data, err := MarshalWithEncryption(user, key)
	require.NoError(t, err)

	// Test unmarshal with nil logger (should not crash)
	result := &TestUserForLogging{}
	err = UnmarshalWithDecryptionAndLogging(data, result, key, nil)
	require.NoError(t, err)
	assert.Equal(t, user.Email, result.Email)
}

func TestUnmarshalWithDecryptionAndLogging_Error(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	require.NoError(t, err)

	// Test with invalid data (should log error)
	result := &TestUserForLogging{}
	err = UnmarshalWithDecryptionAndLogging([]byte("invalid"), result, key, log)
	assert.Error(t, err)
}

func TestMarshalWithEncryptionAndLogging_PointerType(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	user := &TestUserForLogging{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	require.NoError(t, err)

	// Test with pointer (should handle type name correctly)
	data, err := MarshalWithEncryptionAndLogging(user, key, log)
	require.NoError(t, err)
	assert.NotNil(t, data)
}

func TestUnmarshalWithDecryptionAndLogging_PointerType(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	user := &TestUserForLogging{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	require.NoError(t, err)

	// Marshal first
	data, err := MarshalWithEncryption(user, key)
	require.NoError(t, err)

	// Test unmarshal with pointer type logging
	result := &TestUserForLogging{}
	err = UnmarshalWithDecryptionAndLogging(data, result, key, log)
	require.NoError(t, err)
	assert.Equal(t, user.Email, result.Email)
}
