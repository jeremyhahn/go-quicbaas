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

// newTestLogger returns a debug-level JSON logger for the logging variants.
func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "json",
	})
	require.NoError(t, err)
	return log
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

	tests := []struct {
		name string
		log  *logger.Logger
	}{
		{"with logger", newTestLogger(t)},
		{"nil logger", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalWithEncryptionAndLogging(user, key, tt.log)
			require.NoError(t, err)

			result := &TestUserForLogging{}
			require.NoError(t, UnmarshalWithDecryption(data, result, key))
			assert.Equal(t, user, result)
		})
	}
}

func TestMarshalWithEncryptionAndLogging_Error(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	log := newTestLogger(t)

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
	data, err := MarshalWithEncryption(user, key)
	require.NoError(t, err)

	tests := []struct {
		name string
		log  *logger.Logger
	}{
		{"with logger", newTestLogger(t)},
		{"nil logger", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &TestUserForLogging{}
			require.NoError(t, UnmarshalWithDecryptionAndLogging(data, result, key, tt.log))
			assert.Equal(t, user, result)
		})
	}
}

func TestUnmarshalWithDecryptionAndLogging_Error(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	log := newTestLogger(t)

	// Test with invalid data (should log error)
	result := &TestUserForLogging{}
	err = UnmarshalWithDecryptionAndLogging([]byte("invalid"), result, key, log)
	assert.Error(t, err)
}
