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

package codec

import (
	"encoding/json"
	"fmt"
	"reflect"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// JSONCodec implements JSON serialization using the standard library encoding/json.
// It provides human-readable, flexible encoding suitable for debugging,
// configuration, and interoperability. Priority is 50 (medium).
type JSONCodec struct{}

// NewJSONCodec creates a new JSON codec.
func NewJSONCodec() *JSONCodec {
	return &JSONCodec{}
}

// Name returns the codec name.
func (c *JSONCodec) Name() string {
	return "json"
}

// Priority returns the codec's priority (50 for JSON - medium priority).
// JSON is a good default codec: human-readable, widely supported,
// but not as fast as protobuf or as compact as msgpack.
func (c *JSONCodec) Priority() int {
	return 50
}

// CanEncode determines if this codec can encode the given entity.
// JSON can encode almost anything, so this returns true for all non-nil values.
func (c *JSONCodec) CanEncode(entity interface{}) bool {
	if entity == nil {
		return false
	}

	// JSON can encode most Go types
	t := reflect.TypeOf(entity)
	if t == nil {
		return false
	}

	// Dereference pointer
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// JSON handles structs, maps, slices, and basic types
	switch t.Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice, reflect.Array:
		return true
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Float32, reflect.Float64:
		return true
	case reflect.String:
		return true
	default:
		return false
	}
}

// Encode serializes an entity to JSON bytes.
// Returns an error if the entity cannot be marshaled to JSON.
func (c *JSONCodec) Encode(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilEntityError("json.Encode", "encode")
	}

	data, err := json.Marshal(entity)
	if err != nil {
		return nil, api.NewEncodeError("json.Encode", "JSON", err)
	}

	return data, nil
}

// Decode deserializes JSON bytes into an entity.
// The entity parameter must be a pointer to the target struct.
// Returns an error if the data is empty, invalid JSON, or cannot be unmarshaled.
func (c *JSONCodec) Decode(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("json.Decode")
	}

	if entity == nil {
		return api.NewNilEntityError("json.Decode", "decode into")
	}

	// Verify entity is a pointer
	if reflect.TypeOf(entity).Kind() != reflect.Pointer {
		return api.NewNonPointerError("json.Decode", fmt.Sprintf("%T", entity))
	}

	if err := json.Unmarshal(data, entity); err != nil {
		return api.NewDecodeError("json.Decode", "JSON", err)
	}

	return nil
}
