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
	privateField string `encrypt:"aes256"` //nolint:unused
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
			wantErr: false,
		},
		{
			name:  "document with single encrypted field",
			input: TestDocument{},
			want: map[string]string{
				"Content": "aes128",
			},
			wantErr: false,
		},
		{
			name:  "mixed encryption algorithms",
			input: TestMixedEncryption{},
			want: map[string]string{
				"PrivateData": "aes256",
				"SecretData":  "aes128",
			},
			wantErr: false,
		},
		{
			name:    "no encryption tags",
			input:   TestNoEncryption{},
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name:    "invalid algorithm",
			input:   TestInvalidAlgorithm{},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid field type",
			input:   TestInvalidFieldType{},
			want:    nil,
			wantErr: true,
		},
		{
			name:  "unexported field ignored",
			input: TestUnexportedField{},
			want: map[string]string{
				"PublicField": "aes128",
			},
			wantErr: false,
		},
		{
			name:  "byte slice field",
			input: TestByteSlice{},
			want: map[string]string{
				"Data": "aes256",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			structType := reflect.TypeOf(tt.input)
			got, err := ParseEncryptionTags(structType)

			if tt.wantErr {
				assert.Error(t, err)
				var dragonErr *errors.DragonError
				assert.ErrorAs(t, err, &dragonErr)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseEncryptionTags_PointerType(t *testing.T) {
	// Test with pointer to struct
	user := &TestUser{}
	tags, err := ParseEncryptionTags(reflect.TypeOf(user))
	require.NoError(t, err)

	expected := map[string]string{
		"Email":    "aes256",
		"Password": "aes256",
	}
	assert.Equal(t, expected, tags)
}

func TestParseEncryptionTags_NonStruct(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"int", 42},
		{"string", "hello"},
		{"slice", []int{1, 2, 3}},
		{"map", map[string]int{"a": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEncryptionTags(reflect.TypeOf(tt.input))
			assert.Error(t, err)

			var dragonErr *errors.DragonError
			assert.ErrorAs(t, err, &dragonErr)
			assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
		})
	}
}

func TestGetFieldEncryption(t *testing.T) {
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
			wantErr: false,
		},
		{
			name:  "document with one encrypted field",
			input: TestDocument{},
			want: []FieldEncryption{
				{FieldName: "Content", Algorithm: "aes128", FieldIndex: 2},
			},
			wantErr: false,
		},
		{
			name:  "mixed encryption",
			input: TestMixedEncryption{},
			want: []FieldEncryption{
				{FieldName: "PrivateData", Algorithm: "aes256", FieldIndex: 2},
				{FieldName: "SecretData", Algorithm: "aes128", FieldIndex: 3},
			},
			wantErr: false,
		},
		{
			name:    "no encrypted fields",
			input:   TestNoEncryption{},
			want:    []FieldEncryption{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			structType := reflect.TypeOf(tt.input)
			got, err := GetFieldEncryption(structType)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.want), len(got))

				// Compare each field encryption
				for i, want := range tt.want {
					assert.Equal(t, want.FieldName, got[i].FieldName)
					assert.Equal(t, want.Algorithm, got[i].Algorithm)
					assert.Equal(t, want.FieldIndex, got[i].FieldIndex)
				}
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
		{
			name:  "struct with encrypted fields",
			input: TestUser{},
			want:  true,
		},
		{
			name:  "struct without encrypted fields",
			input: TestNoEncryption{},
			want:  false,
		},
		{
			name:  "non-struct type",
			input: 42,
			want:  false,
		},
		{
			name:  "pointer to struct with encrypted fields",
			input: &TestUser{},
			want:  true,
		},
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
		{
			name:    "valid encryption tags",
			input:   TestUser{},
			wantErr: false,
		},
		{
			name:    "invalid algorithm",
			input:   TestInvalidAlgorithm{},
			wantErr: true,
		},
		{
			name:    "invalid field type",
			input:   TestInvalidFieldType{},
			wantErr: true,
		},
		{
			name:    "no encryption tags",
			input:   TestNoEncryption{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEncryptionTags(reflect.TypeOf(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
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

// Test GetFieldEncryption with non-struct types.
func TestGetFieldEncryption_NonStruct(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"int", 42},
		{"string", "hello"},
		{"slice", []int{1, 2, 3}},
		{"map", map[string]int{"a": 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetFieldEncryption(reflect.TypeOf(tt.input))
			assert.Error(t, err)

			var dragonErr *errors.DragonError
			assert.ErrorAs(t, err, &dragonErr)
			assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
		})
	}
}

// Test GetFieldEncryption with pointer type.
func TestGetFieldEncryption_PointerType(t *testing.T) {
	user := &TestUser{}
	fields, err := GetFieldEncryption(reflect.TypeOf(user))
	require.NoError(t, err)

	assert.Equal(t, 2, len(fields))
	assert.Equal(t, "Email", fields[0].FieldName)
	assert.Equal(t, "aes256", fields[0].Algorithm)
	assert.Equal(t, "Password", fields[1].FieldName)
	assert.Equal(t, "aes256", fields[1].Algorithm)
}

// Test GetFieldEncryption with invalid algorithm.
func TestGetFieldEncryption_InvalidAlgorithm(t *testing.T) {
	_, err := GetFieldEncryption(reflect.TypeOf(TestInvalidAlgorithmTag{}))
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
}

// Test GetFieldEncryption with invalid field types.
func TestGetFieldEncryption_InvalidFieldTypes(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"int field", TestInvalidIntField{}},
		{"float field", TestInvalidFloatField{}},
		{"bool field", TestInvalidBoolField{}},
		{"struct field", TestInvalidStructField{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetFieldEncryption(reflect.TypeOf(tt.input))
			assert.Error(t, err)

			var dragonErr *errors.DragonError
			assert.ErrorAs(t, err, &dragonErr)
			assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
		})
	}
}

// Test GetFieldEncryption with multiple byte slices.
func TestGetFieldEncryption_MultipleByteSlices(t *testing.T) {
	fields, err := GetFieldEncryption(reflect.TypeOf(TestMultipleByteSlices{}))
	require.NoError(t, err)

	assert.Equal(t, 2, len(fields))
	assert.Equal(t, "Data1", fields[0].FieldName)
	assert.Equal(t, "aes128", fields[0].Algorithm)
	assert.Equal(t, "Data2", fields[1].FieldName)
	assert.Equal(t, "aes256", fields[1].Algorithm)
}

// Test GetFieldEncryption with whitespace in algorithm tag.
func TestGetFieldEncryption_WhitespaceAlgorithm(t *testing.T) {
	fields, err := GetFieldEncryption(reflect.TypeOf(TestWhitespaceAlgorithm{}))
	require.NoError(t, err)

	assert.Equal(t, 1, len(fields))
	assert.Equal(t, "Secret", fields[0].FieldName)
	assert.Equal(t, "aes256", fields[0].Algorithm)
}

// Test GetFieldEncryption with empty tag.
func TestGetFieldEncryption_EmptyTag(t *testing.T) {
	fields, err := GetFieldEncryption(reflect.TypeOf(TestEmptyTag{}))
	require.NoError(t, err)

	// Empty tag should be ignored
	assert.Equal(t, 0, len(fields))
}

// Test ParseEncryptionTags with invalid field types.
func TestParseEncryptionTags_InvalidFieldTypes(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
	}{
		{"int field", TestInvalidIntField{}},
		{"float field", TestInvalidFloatField{}},
		{"bool field", TestInvalidBoolField{}},
		{"struct field", TestInvalidStructField{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseEncryptionTags(reflect.TypeOf(tt.input))
			assert.Error(t, err)

			var dragonErr *errors.DragonError
			assert.ErrorAs(t, err, &dragonErr)
			assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
		})
	}
}

// Test ParseEncryptionTags with whitespace algorithm.
func TestParseEncryptionTags_WhitespaceAlgorithm(t *testing.T) {
	tags, err := ParseEncryptionTags(reflect.TypeOf(TestWhitespaceAlgorithm{}))
	require.NoError(t, err)

	assert.Equal(t, 1, len(tags))
	assert.Equal(t, "aes256", tags["Secret"])
}

// Test ParseEncryptionTags with empty tag.
func TestParseEncryptionTags_EmptyTag(t *testing.T) {
	tags, err := ParseEncryptionTags(reflect.TypeOf(TestEmptyTag{}))
	require.NoError(t, err)

	// Empty tag should be ignored
	assert.Equal(t, 0, len(tags))
}

// Test HasEncryptedFields with various types.
func TestHasEncryptedFields_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  bool
	}{
		{"with encrypted fields", TestUser{}, true},
		{"without encrypted fields", TestNoEncryption{}, false},
		{"with empty tag", TestEmptyTag{}, false},
		{"int type", 42, false},
		{"string type", "hello", false},
		{"slice type", []int{1, 2, 3}, false},
		{"map type", map[string]int{"a": 1}, false},
		{"byte slice encrypted", TestByteSlice{}, true},
		{"unexported fields only", TestUnexportedField{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasEncryptedFields(reflect.TypeOf(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test ValidateEncryptionTags with invalid algorithms.
func TestValidateEncryptionTags_InvalidAlgorithms(t *testing.T) {
	err := ValidateEncryptionTags(reflect.TypeOf(TestInvalidAlgorithmTag{}))
	assert.Error(t, err)

	var dragonErr *errors.DragonError
	assert.ErrorAs(t, err, &dragonErr)
	assert.Equal(t, errors.ErrInvalidInput, dragonErr.Code)
}

// Test ValidateEncryptionTags with empty tags.
func TestValidateEncryptionTags_EmptyTag(t *testing.T) {
	err := ValidateEncryptionTags(reflect.TypeOf(TestEmptyTag{}))
	assert.NoError(t, err)
}

// Test ValidateEncryptionTags with whitespace.
func TestValidateEncryptionTags_WhitespaceAlgorithm(t *testing.T) {
	err := ValidateEncryptionTags(reflect.TypeOf(TestWhitespaceAlgorithm{}))
	assert.NoError(t, err)
}

// Test isEncryptableType with various slice types.
func TestIsEncryptableType_SliceVariations(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  bool
	}{
		{"byte slice", []byte{}, true},
		{"int slice", []int{}, false},
		{"string slice", []string{}, false},
		{"float slice", []float64{}, false},
		{"bool slice", []bool{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isEncryptableType(reflect.TypeOf(tt.input))
			assert.Equal(t, tt.want, got)
		})
	}
}

// Test field index correctness.
func TestGetFieldEncryption_FieldIndexes(t *testing.T) {
	type TestFieldIndexes struct {
		ID       uint64 `json:"id"`                        // index 0
		Name     string `json:"name"`                      // index 1
		Email    string `json:"email" encrypt:"aes256"`    // index 2
		Age      int    `json:"age"`                       // index 3
		Password string `json:"password" encrypt:"aes128"` // index 4
	}

	fields, err := GetFieldEncryption(reflect.TypeOf(TestFieldIndexes{}))
	require.NoError(t, err)

	assert.Equal(t, 2, len(fields))

	// Email should be at index 2
	assert.Equal(t, "Email", fields[0].FieldName)
	assert.Equal(t, 2, fields[0].FieldIndex)
	assert.Equal(t, "aes256", fields[0].Algorithm)

	// Password should be at index 4
	assert.Equal(t, "Password", fields[1].FieldName)
	assert.Equal(t, 4, fields[1].FieldIndex)
	assert.Equal(t, "aes128", fields[1].Algorithm)
}
