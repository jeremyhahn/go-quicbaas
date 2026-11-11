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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Test structs for encryption tag parsing.
type TestUser struct {
	ID       uint64 `json:"id"`
	Email    string `json:"email" encrypt:"aes256"`
	Password string `json:"password" encrypt:"aes256"`
	Name     string `json:"name"`
}

type TestDocument struct {
	ID      uint64 `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content" encrypt:"aes128"`
}

type TestMixedEncryption struct {
	ID          uint64 `json:"id"`
	PublicData  string `json:"public_data"`
	PrivateData string `json:"private_data" encrypt:"aes256"`
	SecretData  []byte `json:"secret_data" encrypt:"aes128"`
}

type TestNoEncryption struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type TestInvalidAlgorithm struct {
	ID     uint64 `json:"id"`
	Secret string `json:"secret" encrypt:"invalid-algo"`
}

type TestInvalidFieldType struct {
	ID             uint64  `json:"id"`
	EncryptedInt   int     `json:"encrypted_int" encrypt:"aes128"`
	EncryptedFloat float64 `json:"encrypted_float" encrypt:"aes256"`
}

type TestUnexportedField struct {
	ID           uint64 `json:"id"`
	PublicField  string `json:"public" encrypt:"aes128"`
	privateField string `encrypt:"aes256"`
}

type TestByteSlice struct {
	ID   uint64 `json:"id"`
	Data []byte `json:"data" encrypt:"aes256"`
}

func TestParseEncryptionTags(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "user with encrypted fields",
			input: TestUser{},
			want: map[string]string{
				"Email":    "aes256",
				"Password": "aes256",
			},
		},
		{
			name:  "pointer to struct",
			input: &TestUser{},
			want: map[string]string{
				"Email":    "aes256",
				"Password": "aes256",
			},
		},
		{
			name:  "document with single encrypted field",
			input: TestDocument{},
			want: map[string]string{
				"Content": "aes128",
			},
		},
		{
			name:  "mixed encryption algorithms",
			input: TestMixedEncryption{},
			want: map[string]string{
				"PrivateData": "aes256",
				"SecretData":  "aes128",
			},
		},
		{
			name:  "no encryption tags",
			input: TestNoEncryption{},
			want:  map[string]string{},
		},
		{
			name:  "empty tag ignored",
			input: TestEmptyTag{},
			want:  map[string]string{},
		},
		{
			name:  "whitespace around algorithm trimmed",
			input: TestWhitespaceAlgorithm{},
			want: map[string]string{
				"Secret": "aes256",
			},
		},
		{
			name:  "unexported field ignored",
			input: TestUnexportedField{privateField: "never parsed"},
			want: map[string]string{
				"PublicField": "aes128",
			},
		},
		{
			name:  "byte slice field",
			input: TestByteSlice{},
			want: map[string]string{
				"Data": "aes256",
			},
		},
		{name: "invalid algorithm", input: TestInvalidAlgorithm{}, wantErr: true},
		{name: "int field", input: TestInvalidIntField{}, wantErr: true},
		{name: "float field", input: TestInvalidFloatField{}, wantErr: true},
		{name: "bool field", input: TestInvalidBoolField{}, wantErr: true},
		{name: "struct field", input: TestInvalidStructField{}, wantErr: true},
		{name: "non-struct int", input: 42, wantErr: true},
		{name: "non-struct string", input: "hello", wantErr: true},
		{name: "non-struct slice", input: []int{1, 2, 3}, wantErr: true},
		{name: "non-struct map", input: map[string]int{"a": 1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEncryptionTags(reflect.TypeOf(tt.input))

			if tt.wantErr {
				var qbErr *errors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetFieldEncryption(t *testing.T) {
	type fieldIndexes struct {
		ID       uint64 `json:"id"`
		Name     string `json:"name"`
		Email    string `json:"email" encrypt:"aes256"`
		Age      int    `json:"age"`
		Password string `json:"password" encrypt:"aes128"`
	}

	tests := []struct {
		name    string
		input   interface{}
		want    []FieldEncryption
		wantErr bool
	}{
		{
			name:  "user with two encrypted fields",
			input: TestUser{},
			want: []FieldEncryption{
				{FieldName: "Email", Algorithm: "aes256", FieldIndex: 1},
				{FieldName: "Password", Algorithm: "aes256", FieldIndex: 2},
			},
		},
		{
			name:  "pointer to struct",
			input: &TestUser{},
			want: []FieldEncryption{
				{FieldName: "Email", Algorithm: "aes256", FieldIndex: 1},
				{FieldName: "Password", Algorithm: "aes256", FieldIndex: 2},
			},
		},
		{
			name:  "document with one encrypted field",
			input: TestDocument{},
			want: []FieldEncryption{
				{FieldName: "Content", Algorithm: "aes128", FieldIndex: 2},
			},
		},
		{
			name:  "mixed encryption",
			input: TestMixedEncryption{},
			want: []FieldEncryption{
				{FieldName: "PrivateData", Algorithm: "aes256", FieldIndex: 2},
				{FieldName: "SecretData", Algorithm: "aes128", FieldIndex: 3},
			},
		},
		{
			name:  "multiple byte slices",
			input: TestMultipleByteSlices{},
			want: []FieldEncryption{
				{FieldName: "Data1", Algorithm: "aes128", FieldIndex: 1},
				{FieldName: "Data2", Algorithm: "aes256", FieldIndex: 2},
			},
		},
		{
			name:  "field indexes skip plain fields",
			input: fieldIndexes{},
			want: []FieldEncryption{
				{FieldName: "Email", Algorithm: "aes256", FieldIndex: 2},
				{FieldName: "Password", Algorithm: "aes128", FieldIndex: 4},
			},
		},
		{
			name:  "whitespace around algorithm trimmed",
			input: TestWhitespaceAlgorithm{},
			want: []FieldEncryption{
				{FieldName: "Secret", Algorithm: "aes256", FieldIndex: 1},
			},
		},
		{
			name:  "no encrypted fields",
			input: TestNoEncryption{},
			want:  []FieldEncryption{},
		},
		{
			name:  "empty tag ignored",
			input: TestEmptyTag{},
			want:  []FieldEncryption{},
		},
		{name: "invalid algorithm", input: TestInvalidAlgorithmTag{}, wantErr: true},
		{name: "int field", input: TestInvalidIntField{}, wantErr: true},
		{name: "struct field", input: TestInvalidStructField{}, wantErr: true},
		{name: "non-struct int", input: 42, wantErr: true},
		{name: "non-struct map", input: map[string]int{"a": 1}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GetFieldEncryption(reflect.TypeOf(tt.input))

			if tt.wantErr {
				var qbErr *errors.QBError
				require.ErrorAs(t, err, &qbErr)
				assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, len(tt.want))
			for i, want := range tt.want {
				assert.Equal(t, want.FieldName, got[i].FieldName)
				assert.Equal(t, want.Algorithm, got[i].Algorithm)
				assert.Equal(t, want.FieldIndex, got[i].FieldIndex)
			}
		})
	}
}

func TestHasEncryptedFields(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  bool
	}{
		{"struct with encrypted fields", TestUser{}, true},
		{"pointer to struct with encrypted fields", &TestUser{}, true},
		{"byte slice encrypted", TestByteSlice{}, true},
		{"exported field tagged beside unexported one", TestUnexportedField{}, true},
		{"struct without encrypted fields", TestNoEncryption{}, false},
		{"empty tag", TestEmptyTag{}, false},
		{"non-struct int", 42, false},
		{"non-struct string", "hello", false},
		{"non-struct slice", []int{1, 2, 3}, false},
		{"non-struct map", map[string]int{"a": 1}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasEncryptedFields(reflect.TypeOf(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestValidateEncryptionTags(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		wantErr bool
	}{
		{name: "valid encryption tags", input: TestUser{}},
		{name: "no encryption tags", input: TestNoEncryption{}},
		{name: "invalid algorithm", input: TestInvalidAlgorithmTag{}, wantErr: true},
		{name: "invalid field type", input: TestInvalidFieldType{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEncryptionTags(reflect.TypeOf(tt.input))
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			var qbErr *errors.QBError
			require.ErrorAs(t, err, &qbErr)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
		})
	}
}

func TestIsEncryptableType(t *testing.T) {
	tests := []struct {
		name      string
		fieldType reflect.Type
		want      bool
	}{
		{
			name:      "string type",
			fieldType: reflect.TypeOf(""),
			want:      true,
		},
		{
			name:      "byte slice type",
			fieldType: reflect.TypeOf([]byte{}),
			want:      true,
		},
		{
			name:      "int type",
			fieldType: reflect.TypeOf(0),
			want:      false,
		},
		{
			name:      "int slice type",
			fieldType: reflect.TypeOf([]int{}),
			want:      false,
		},
		{
			name:      "struct type",
			fieldType: reflect.TypeOf(struct{}{}),
			want:      false,
		},
		{
			name:      "pointer type",
			fieldType: reflect.TypeOf((*string)(nil)),
			want:      false,
		},
		{
			name:      "string slice type",
			fieldType: reflect.TypeOf([]string{}),
			want:      false,
		},
		{
			name:      "float slice type",
			fieldType: reflect.TypeOf([]float64{}),
			want:      false,
		},
		{
			name:      "bool slice type",
			fieldType: reflect.TypeOf([]bool{}),
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEncryptableType(tt.fieldType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkParseEncryptionTags(b *testing.B) {
	structType := reflect.TypeOf(TestUser{})
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = ParseEncryptionTags(structType)
	}
}

func BenchmarkGetFieldEncryption(b *testing.B) {
	structType := reflect.TypeOf(TestMixedEncryption{})
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = GetFieldEncryption(structType)
	}
}

func BenchmarkHasEncryptedFields(b *testing.B) {
	structType := reflect.TypeOf(TestUser{})
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = HasEncryptedFields(structType)
	}
}

// =============================================================================
// Consolidated from metadata_additional_test.go
// =============================================================================

// Additional test structs for edge cases.
type TestInvalidIntField struct {
	ID        uint64 `json:"id"`
	SecretInt int    `json:"secret_int" encrypt:"aes128"`
}

type TestInvalidFloatField struct {
	ID          uint64  `json:"id"`
	SecretFloat float64 `json:"secret_float" encrypt:"aes256"`
}

type TestInvalidBoolField struct {
	ID         uint64 `json:"id"`
	SecretBool bool   `json:"secret_bool" encrypt:"aes128"`
}

type TestInvalidStructField struct {
	ID     uint64 `json:"id"`
	Nested struct {
		Secret string `json:"secret"`
	} `json:"nested" encrypt:"aes256"`
}

type TestMultipleByteSlices struct {
	ID     uint64 `json:"id"`
	Data1  []byte `json:"data1" encrypt:"aes128"`
	Data2  []byte `json:"data2" encrypt:"aes256"`
	Public string `json:"public"`
}

type TestWhitespaceAlgorithm struct {
	ID     uint64 `json:"id"`
	Secret string `json:"secret" encrypt:" aes256 "`
}

type TestInvalidAlgorithmTag struct {
	ID     uint64 `json:"id"`
	Secret string `json:"secret" encrypt:"aes512"`
}

type TestEmptyTag struct {
	ID     uint64 `json:"id"`
	Secret string `json:"secret" encrypt:""`
}
