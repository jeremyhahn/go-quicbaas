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
	"strings"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

const (
	// EncryptTag is the struct tag key used to specify field encryption.
	EncryptTag = "encrypt"
)

// FieldEncryption represents encryption metadata for a single field.
type FieldEncryption struct {
	// FieldName is the name of the struct field
	FieldName string

	// Algorithm is the encryption algorithm (aes128 or aes256)
	Algorithm string

	// FieldIndex is the index of the field in the struct for reflection
	FieldIndex int
}

// ParseEncryptionTags analyzes a struct type and extracts encryption metadata
// from struct tags. It returns a map of field names to their encryption algorithms.
//
// Supported tag formats:
//   - `encrypt:"aes128"` - Use AES-128-GCM encryption
//   - `encrypt:"aes256"` - Use AES-256-GCM encryption
//   - No tag or empty tag - Field is not encrypted
//
// Example:
//
//	type User struct {
//	    ID       uint64 `json:"id"`
//	    Email    string `json:"email" encrypt:"aes256"`
//	    Password string `json:"password" encrypt:"aes256"`
//	    Bio      string `json:"bio" encrypt:"aes128"`
//	}
//
// Returns a map like: {"Email": "aes256", "Password": "aes256", "Bio": "aes128"}.
func ParseEncryptionTags(structType reflect.Type) (map[string]string, error) {
	// Handle pointer types
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	// Validate input is a struct
	if structType.Kind() != reflect.Struct {
		return nil, api.NewExpectedStructTypeError("encryption.ParseEncryptionTags", structType.Kind().String())
	}

	result := make(map[string]string)

	// Iterate through all fields
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check for encrypt tag
		tag := field.Tag.Get(EncryptTag)
		if tag == "" {
			continue
		}

		// Validate algorithm
		algorithm := strings.TrimSpace(tag)
		if algorithm != AlgorithmAES128 && algorithm != AlgorithmAES256 {
			return nil, api.NewInvalidEncryptionAlgorithmError(
				"encryption.ParseEncryptionTags", algorithm, field.Name,
				[]string{AlgorithmAES128, AlgorithmAES256})
		}

		// Only allow encryption on string and []byte fields for safety
		if !isEncryptableType(field.Type) {
			return nil, api.NewUnsupportedFieldTypeError(
				"encryption.ParseEncryptionTags", field.Name, field.Type.String())
		}

		result[field.Name] = algorithm
	}

	return result, nil
}

// GetFieldEncryption returns detailed encryption metadata for all encrypted fields
// in a struct type. This includes field names, algorithms, and reflection indices.
func GetFieldEncryption(structType reflect.Type) ([]FieldEncryption, error) {
	// Handle pointer types
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	// Validate input is a struct
	if structType.Kind() != reflect.Struct {
		return nil, api.NewExpectedStructTypeError("encryption.GetFieldEncryption", structType.Kind().String())
	}

	var result []FieldEncryption

	// Iterate through all fields
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check for encrypt tag
		tag := field.Tag.Get(EncryptTag)
		if tag == "" {
			continue
		}

		// Validate algorithm
		algorithm := strings.TrimSpace(tag)
		if algorithm != AlgorithmAES128 && algorithm != AlgorithmAES256 {
			return nil, api.NewInvalidEncryptionAlgorithmError(
				"encryption.GetFieldEncryption", algorithm, field.Name, nil)
		}

		// Only allow encryption on string and []byte fields
		if !isEncryptableType(field.Type) {
			return nil, api.NewUnsupportedFieldTypeError(
				"encryption.GetFieldEncryption", field.Name, "")
		}

		result = append(result, FieldEncryption{
			FieldName:  field.Name,
			Algorithm:  algorithm,
			FieldIndex: i,
		})
	}

	return result, nil
}

// isEncryptableType checks if a field type can be safely encrypted
// Currently supports: string and []byte.
func isEncryptableType(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String:
		return true
	case reflect.Slice:
		// Only allow []byte slices
		return t.Elem().Kind() == reflect.Uint8
	default:
		return false
	}
}

// HasEncryptedFields checks if a struct type has any fields with encryption tags.
func HasEncryptedFields(structType reflect.Type) bool {
	// Handle pointer types
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return false
	}

	// Check if any field has an encrypt tag
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		if field.IsExported() && field.Tag.Get(EncryptTag) != "" {
			return true
		}
	}

	return false
}

// ValidateEncryptionTags validates all encryption tags in a struct type
// without returning the actual mapping. Useful for early validation.
func ValidateEncryptionTags(structType reflect.Type) error {
	_, err := ParseEncryptionTags(structType)
	return err
}
