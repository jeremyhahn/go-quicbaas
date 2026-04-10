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
	"encoding/base64"
	"encoding/json"
	"reflect"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// MarshalWithEncryption serializes an entity to JSON with encrypted fields.
// It uses reflection to identify fields with `encrypt` tags and encrypts them
// before JSON serialization.
//
// Process:
// 1. Parse encryption tags to identify encrypted fields
// 2. Create a copy of the entity to avoid modifying the original
// 3. Encrypt specified fields in-place
// 4. Marshal to JSON
//
// Encrypted values are base64-encoded in the JSON output for safe transmission.
//
// Parameters:
//   - entity: the struct to marshal (must be a pointer or struct)
//   - encryptionKey: the encryption key (size depends on algorithm in tags)
//
// Returns JSON bytes or an error if encryption/marshaling fails.
func MarshalWithEncryption(entity interface{}, encryptionKey []byte) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilInputError("encryption.MarshalWithEncryption", "entity")
	}

	// Get the value and type
	v := reflect.ValueOf(entity)
	t := v.Type()

	// Handle pointer types
	if t.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil, api.NewNilInputError("encryption.MarshalWithEncryption", "entity pointer")
		}
		v = v.Elem()
		t = v.Type()
	}

	// Validate input is a struct
	if t.Kind() != reflect.Struct {
		return nil, api.NewExpectedStructError("encryption.MarshalWithEncryption", t.Kind().String())
	}

	// Get encryption metadata
	fieldEncryptions, err := GetFieldEncryption(t)
	if err != nil {
		return nil, err
	}

	// If no encrypted fields, just use standard JSON marshaling
	if len(fieldEncryptions) == 0 {
		data, err := json.Marshal(entity)
		if err != nil {
			return nil, api.NewJSONMarshalError("encryption.MarshalWithEncryption", err)
		}
		return data, nil
	}

	// Create a mutable copy of the struct
	copyValue := reflect.New(t).Elem()
	copyValue.Set(v)

	// Encrypt each field
	for _, fieldEnc := range fieldEncryptions {
		field := copyValue.Field(fieldEnc.FieldIndex)

		// Get field value as bytes
		var plaintext []byte
		switch field.Kind() {
		case reflect.String:
			plaintext = []byte(field.String())
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.Uint8 {
				plaintext = field.Bytes()
			}
		}

		// Skip empty values
		if len(plaintext) == 0 {
			continue
		}

		// Encrypt the field
		ciphertext, err := EncryptField(plaintext, encryptionKey, fieldEnc.Algorithm)
		if err != nil {
			return nil, api.NewFieldEncryptionError(
				"encryption.MarshalWithEncryption", fieldEnc.FieldName, "encrypt",
				errors.ErrEncryption, err)
		}

		// Encode to base64 for JSON safety
		encoded := base64.StdEncoding.EncodeToString(ciphertext)

		// Set the encrypted value back to the field
		switch field.Kind() {
		case reflect.String:
			field.SetString(encoded)
		case reflect.Slice:
			field.SetBytes([]byte(encoded))
		}
	}

	// Marshal the modified struct to JSON
	data, err := json.Marshal(copyValue.Interface())
	if err != nil {
		return nil, api.NewJSONMarshalError("encryption.MarshalWithEncryption", err)
	}

	return data, nil
}

// UnmarshalWithDecryption deserializes JSON data into an entity and decrypts
// encrypted fields. It uses reflection to identify fields with `encrypt` tags
// and decrypts them after JSON unmarshaling.
//
// Process:
// 1. Unmarshal JSON into entity
// 2. Parse encryption tags to identify encrypted fields
// 3. Decrypt and decode base64-encoded encrypted fields
// 4. Set decrypted values back to the entity
//
// Parameters:
//   - data: JSON bytes to unmarshal
//   - entity: pointer to struct to populate (must be a pointer)
//   - encryptionKey: the decryption key (must match encryption key)
//
// Returns an error if unmarshaling/decryption fails.
func UnmarshalWithDecryption(data []byte, entity interface{}, encryptionKey []byte) error {
	if entity == nil {
		return api.NewNilInputError("encryption.UnmarshalWithDecryption", "entity")
	}

	// Get the value and type
	v := reflect.ValueOf(entity)
	if v.Kind() != reflect.Ptr {
		return api.NewPointerRequiredError("encryption.UnmarshalWithDecryption")
	}

	if v.IsNil() {
		return api.NewNilInputError("encryption.UnmarshalWithDecryption", "entity pointer")
	}

	// Unmarshal JSON first
	if err := json.Unmarshal(data, entity); err != nil {
		return api.NewJSONUnmarshalError("encryption.UnmarshalWithDecryption", err)
	}

	// Get the actual struct value
	v = v.Elem()
	t := v.Type()

	// Validate it's a struct
	if t.Kind() != reflect.Struct {
		return api.NewExpectedStructError("encryption.UnmarshalWithDecryption", t.Kind().String())
	}

	// Get encryption metadata
	fieldEncryptions, err := GetFieldEncryption(t)
	if err != nil {
		return err
	}

	// If no encrypted fields, we're done
	if len(fieldEncryptions) == 0 {
		return nil
	}

	// Decrypt each field
	for _, fieldEnc := range fieldEncryptions {
		field := v.Field(fieldEnc.FieldIndex)

		// Get encrypted value as base64 string
		var encoded string
		switch field.Kind() {
		case reflect.String:
			encoded = field.String()
		case reflect.Slice:
			if field.Type().Elem().Kind() == reflect.Uint8 {
				encoded = string(field.Bytes())
			}
		}

		// Skip empty values
		if encoded == "" {
			continue
		}

		// Decode from base64
		ciphertext, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return api.NewBase64DecodeError("encryption.UnmarshalWithDecryption", fieldEnc.FieldName, err)
		}

		// Decrypt the field
		plaintext, err := DecryptField(ciphertext, encryptionKey, fieldEnc.Algorithm)
		if err != nil {
			return api.NewFieldEncryptionError(
				"encryption.UnmarshalWithDecryption", fieldEnc.FieldName, "decrypt",
				errors.ErrDecryption, err)
		}

		// Set the decrypted value back to the field
		switch field.Kind() {
		case reflect.String:
			field.SetString(string(plaintext))
		case reflect.Slice:
			field.SetBytes(plaintext)
		}
	}

	return nil
}

// MarshalWithEncryptionAndLogging is a wrapper around MarshalWithEncryption
// that adds logging for debugging and audit purposes.
func MarshalWithEncryptionAndLogging(entity interface{}, encryptionKey []byte, log *logger.Logger) ([]byte, error) {
	if log != nil {
		t := reflect.TypeOf(entity)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		log.Debugf("marshaling entity of type %s with encryption", t.Name())
	}

	data, err := MarshalWithEncryption(entity, encryptionKey)
	if err != nil {
		if log != nil {
			log.WithError(err).Error("encryption marshaling failed")
		}
		return nil, err
	}

	if log != nil {
		log.Debugf("successfully marshaled and encrypted entity (%d bytes)", len(data))
	}

	return data, nil
}

// UnmarshalWithDecryptionAndLogging is a wrapper around UnmarshalWithDecryption
// that adds logging for debugging and audit purposes.
func UnmarshalWithDecryptionAndLogging(data []byte, entity interface{}, encryptionKey []byte, log *logger.Logger) error {
	if log != nil {
		t := reflect.TypeOf(entity)
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		log.Debugf("unmarshaling entity of type %s with decryption", t.Name())
	}

	err := UnmarshalWithDecryption(data, entity, encryptionKey)
	if err != nil {
		if log != nil {
			log.WithError(err).Error("decryption unmarshaling failed")
		}
		return err
	}

	if log != nil {
		log.Debug("successfully unmarshaled and decrypted entity")
	}

	return nil
}
