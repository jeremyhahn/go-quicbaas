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

func TestMarshalWithEncryption(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	tests := []struct {
		name    string
		entity  interface{}
		wantErr bool
	}{
		{
			name: "user with encrypted fields",
			entity: &User{
				ID:       1,
				Username: "alice",
				Email:    "alice@example.com",
				Password: "secret123",
			},
			wantErr: false,
		},
		{
			name: "document with encrypted content",
			entity: &Document{
				ID:      100,
				Title:   "Secret Document",
				Content: "This is confidential information",
				Author:  "Bob",
			},
			wantErr: false,
		},
		{
			name: "mixed encrypted and plain fields",
			entity: &MixedData{
				ID:          42,
				PublicInfo:  "public data",
				PrivateInfo: "private data",
				SecretData:  []byte("secret bytes"),
			},
			wantErr: false,
		},
		{
			name: "no encrypted fields",
			entity: &NoEncryption{
				ID:   1,
				Name: "test",
				Age:  30,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := MarshalWithEncryption(tt.entity, key)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, data)

			// Verify it's valid JSON
			var jsonCheck map[string]interface{}
			err = json.Unmarshal(data, &jsonCheck)
			require.NoError(t, err)
		})
	}
}

func TestUnmarshalWithDecryption(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	tests := []struct {
		name     string
		original interface{}
		target   interface{}
		wantErr  bool
	}{
		{
			name: "user entity",
			original: &User{
				ID:       1,
				Username: "alice",
				Email:    "alice@example.com",
				Password: "secret123",
			},
			target:  &User{},
			wantErr: false,
		},
		{
			name: "document entity",
			original: &Document{
				ID:      100,
				Title:   "Secret Document",
				Content: "This is confidential information",
				Author:  "Bob",
			},
			target:  &Document{},
			wantErr: false,
		},
		{
			name: "mixed data entity",
			original: &MixedData{
				ID:          42,
				PublicInfo:  "public data",
				PrivateInfo: "private data",
				SecretData:  []byte("secret bytes"),
			},
			target:  &MixedData{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal with encryption
			data, err := MarshalWithEncryption(tt.original, key)
			require.NoError(t, err)

			// Unmarshal with decryption
			err = UnmarshalWithDecryption(data, tt.target, key)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Compare original and decrypted
			assert.Equal(t, tt.original, tt.target)
		})
	}
}

func TestMarshalUnmarshal_RoundTrip(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	original := &User{
		ID:       42,
		Username: "testuser",
		Email:    "test@example.com",
		Password: "super-secret-password-123!@#",
	}

	// Marshal with encryption
	encrypted, err := MarshalWithEncryption(original, key)
	require.NoError(t, err)

	// Verify encrypted fields are not in plaintext
	assert.NotContains(t, string(encrypted), "test@example.com")
	assert.NotContains(t, string(encrypted), "super-secret-password-123!@#")

	// Verify non-encrypted fields are in plaintext (or at least the username)
	assert.Contains(t, string(encrypted), "testuser")

	// Unmarshal with decryption
	decrypted := &User{}
	err = UnmarshalWithDecryption(encrypted, decrypted, key)
	require.NoError(t, err)

	// Verify all fields match
	assert.Equal(t, original.ID, decrypted.ID)
	assert.Equal(t, original.Username, decrypted.Username)
	assert.Equal(t, original.Email, decrypted.Email)
	assert.Equal(t, original.Password, decrypted.Password)
}

func TestMarshalWithEncryption_EmptyFields(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	// Entity with empty encrypted fields
	user := &User{
		ID:       1,
		Username: "alice",
		Email:    "",
		Password: "",
	}

	data, err := MarshalWithEncryption(user, key)
	require.NoError(t, err)

	// Unmarshal should work
	result := &User{}
	err = UnmarshalWithDecryption(data, result, key)
	require.NoError(t, err)

	assert.Equal(t, user.ID, result.ID)
	assert.Equal(t, user.Username, result.Username)
	assert.Equal(t, user.Email, result.Email)
	assert.Equal(t, user.Password, result.Password)
}

func TestUnmarshalWithDecryption_WrongKey(t *testing.T) {
	key1 := make([]byte, AES256KeySize)
	key2 := make([]byte, AES256KeySize)
	_, err := rand.Read(key1)
	require.NoError(t, err)
	_, err = rand.Read(key2)
	require.NoError(t, err)

	user := &User{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	// Encrypt with key1
	data, err := MarshalWithEncryption(user, key1)
	require.NoError(t, err)

	// Try to decrypt with key2
	result := &User{}
	err = UnmarshalWithDecryption(data, result, key2)
	assert.Error(t, err)

	var qbErr *errors.QBError
	assert.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrDecryption, qbErr.Code)
}

func TestMarshalWithEncryption_NilEntity(t *testing.T) {
	key := make([]byte, AES256KeySize)

	_, err := MarshalWithEncryption(nil, key)
	assert.Error(t, err)

	var qbErr *errors.QBError
	assert.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestMarshalWithEncryption_NilPointer(t *testing.T) {
	key := make([]byte, AES256KeySize)
	var user *User

	_, err := MarshalWithEncryption(user, key)
	assert.Error(t, err)

	var qbErr *errors.QBError
	assert.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestUnmarshalWithDecryption_NonPointer(t *testing.T) {
	key := make([]byte, AES256KeySize)
	data := []byte(`{"id":1}`)

	user := User{}
	err := UnmarshalWithDecryption(data, user, key)
	assert.Error(t, err)

	var qbErr *errors.QBError
	assert.ErrorAs(t, err, &qbErr)
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestMarshalWithEncryption_NonStruct(t *testing.T) {
	key := make([]byte, AES256KeySize)

	tests := []struct {
		name   string
		entity interface{}
	}{
		{"int", 42},
		{"string", "hello"},
		{"slice", []int{1, 2, 3}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := MarshalWithEncryption(tt.entity, key)
			assert.Error(t, err)

			var qbErr *errors.QBError
			assert.ErrorAs(t, err, &qbErr)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
		})
	}
}

func TestMarshalWithEncryption_StructValue(t *testing.T) {
	key := make([]byte, AES256KeySize)

	// Pass struct by value (not pointer)
	user := User{
		ID:       1,
		Username: "alice",
		Email:    "alice@example.com",
		Password: "secret123",
	}

	data, err := MarshalWithEncryption(user, key)
	require.NoError(t, err)

	// Should still work - unmarshal
	result := &User{}
	err = UnmarshalWithDecryption(data, result, key)
	require.NoError(t, err)

	assert.Equal(t, user.ID, result.ID)
	assert.Equal(t, user.Username, result.Username)
	assert.Equal(t, user.Email, result.Email)
	assert.Equal(t, user.Password, result.Password)
}

func TestMarshalWithEncryption_UnicodeData(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	user := &User{
		ID:       1,
		Username: "用户",
		Email:    "test@例え.jp",
		Password: "密码123🔐",
	}

	data, err := MarshalWithEncryption(user, key)
	require.NoError(t, err)

	result := &User{}
	err = UnmarshalWithDecryption(data, result, key)
	require.NoError(t, err)

	assert.Equal(t, user.Username, result.Username)
	assert.Equal(t, user.Email, result.Email)
	assert.Equal(t, user.Password, result.Password)
}

func TestMarshalWithEncryption_ByteSliceField(t *testing.T) {
	key := make([]byte, AES256KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)

	original := &MixedData{
		ID:          1,
		PublicInfo:  "public",
		PrivateInfo: "private",
		SecretData:  []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd},
	}

	data, err := MarshalWithEncryption(original, key)
	require.NoError(t, err)

	result := &MixedData{}
	err = UnmarshalWithDecryption(data, result, key)
	require.NoError(t, err)

	assert.Equal(t, original.SecretData, result.SecretData)
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
