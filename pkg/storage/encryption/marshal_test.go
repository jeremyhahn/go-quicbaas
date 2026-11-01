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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Test entities for marshal/unmarshal testing.
type User struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email" encrypt:"aes256"`
	Password string `json:"password" encrypt:"aes256"`
}

type Document struct {
	ID      uint64 `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content" encrypt:"aes128"`
	Author  string `json:"author"`
}

type MixedData struct {
	ID          uint64 `json:"id"`
	PublicInfo  string `json:"public_info"`
	PrivateInfo string `json:"private_info" encrypt:"aes256"`
	SecretData  []byte `json:"secret_data" encrypt:"aes128"`
}

type NoEncryption struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	tests := []struct {
		name     string
		original interface{}
		target   interface{}
		// secrets are the encrypted values that must not appear in the output.
		secrets []string
	}{
		{
			name:     "user with encrypted fields",
			original: &User{ID: 42, Username: "testuser", Email: "test@example.com", Password: "super-secret-password-123!@#"},
			target:   &User{},
			secrets:  []string{"test@example.com", "super-secret-password-123!@#"},
		},
		{
			name:     "document with encrypted content",
			original: &Document{ID: 100, Title: "Secret Document", Content: "This is confidential information", Author: "Bob"},
			target:   &Document{},
			secrets:  []string{"This is confidential information"},
		},
		{
			name:     "mixed fields with binary byte slice",
			original: &MixedData{ID: 1, PublicInfo: "public", PrivateInfo: "private data", SecretData: []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd}},
			target:   &MixedData{},
			secrets:  []string{"private data"},
		},
		{
			name:     "unicode data",
			original: &User{ID: 1, Username: "用户", Email: "test@例え.jp", Password: "密码123🔐"},
			target:   &User{},
		},
		{
			name:     "empty encrypted fields",
			original: &User{ID: 1, Username: "alice"},
			target:   &User{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalWithEncryption(tt.original, key)
			require.NoError(t, err)
			for _, secret := range tt.secrets {
				assert.NotContains(t, string(data), secret)
			}

			err = UnmarshalWithDecryption(data, tt.target, key)
			require.NoError(t, err)
			assert.Equal(t, tt.original, tt.target)
		})
	}

	t.Run("struct passed by value", func(t *testing.T) {
		user := User{ID: 1, Username: "alice", Email: "alice@example.com", Password: "secret123"}

		data, err := MarshalWithEncryption(user, key)
		require.NoError(t, err)

		result := &User{}
		require.NoError(t, UnmarshalWithDecryption(data, result, key))
		assert.Equal(t, user, *result)
	})
}

func TestMarshalWithEncryption_InvalidInput(t *testing.T) {
	key := make([]byte, AES256KeySize)
	var nilUser *User

	tests := []struct {
		name   string
		entity interface{}
	}{
		{"nil entity", nil},
		{"nil pointer", nilUser},
		{"int", 42},
		{"string", "hello"},
		{"slice", []int{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarshalWithEncryption(tt.entity, key)
			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
		})
	}
}

func TestUnmarshalWithDecryption_Errors(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	otherKey := make([]byte, AES256KeySize)
	_, err = rand.Read(otherKey)
	require.NoError(t, err)

	encrypted, err := MarshalWithEncryption(&User{ID: 1, Username: "alice", Email: "alice@example.com", Password: "secret123"}, key)
	require.NoError(t, err)

	tests := []struct {
		name     string
		data     []byte
		target   interface{}
		key      []byte
		wantCode errors.ErrorCode
	}{
		{"wrong key", encrypted, &User{}, otherKey, errors.ErrDecryption},
		{"non-pointer target", []byte(`{"id":1}`), User{}, key, errors.ErrInvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := UnmarshalWithDecryption(tt.data, tt.target, tt.key)
			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, tt.wantCode, qbErr.Code)
		})
	}
}

func TestMarshalWithEncryption_NoEncryptedFields(t *testing.T) {
	key := make([]byte, AES256KeySize)

	entity := &NoEncryption{
		ID:   1,
		Name: "test",
		Age:  30,
	}

	// Should use standard JSON marshaling
	data, err := MarshalWithEncryption(entity, key)
	require.NoError(t, err)

	// Verify it's standard JSON
	expected, err := json.Marshal(entity)
	require.NoError(t, err)
	assert.Equal(t, expected, data)

	// Unmarshal should also work
	result := &NoEncryption{}
	err = UnmarshalWithDecryption(data, result, key)
	require.NoError(t, err)
	assert.Equal(t, entity, result)
}

func BenchmarkMarshalWithEncryption(b *testing.B) {
	key := make([]byte, AES256KeySize)
	rand.Read(key)

	user := &User{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalWithEncryption(user, key)
	}
}

func BenchmarkUnmarshalWithDecryption(b *testing.B) {
	key := make([]byte, AES256KeySize)
	rand.Read(key)

	user := &User{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	data, _ := MarshalWithEncryption(user, key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result := &User{}
		_ = UnmarshalWithDecryption(data, result, key)
	}
}

func BenchmarkMarshalWithEncryption_NoEncryption(b *testing.B) {
	key := make([]byte, AES256KeySize)
	rand.Read(key)

	entity := &NoEncryption{
		ID:   1,
		Name: "test",
		Age:  30,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = MarshalWithEncryption(entity, key)
	}
}
