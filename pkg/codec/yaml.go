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
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// YAMLCodec implements YAML serialization using gopkg.in/yaml.v3.
// YAML provides human-readable encoding suitable for configuration files
// and documentation. Priority is 30 (lowest among the implemented codecs).
type YAMLCodec struct{}

// NewYAMLCodec creates a new YAML codec.
func NewYAMLCodec() *YAMLCodec {
	return &YAMLCodec{}
}

// Name returns the codec name.
func (c *YAMLCodec) Name() string {
	return "yaml"
}

// Priority returns the codec's priority (30 for YAML - lowest priority).
// YAML is very human-readable but verbose and slower than JSON, MsgPack, or Protobuf.
func (c *YAMLCodec) Priority() int {
	return 30
}

// CanEncode determines if this codec can encode the given entity.
// YAML can encode most Go types similar to JSON.
func (c *YAMLCodec) CanEncode(entity interface{}) bool {
	if entity == nil {
		return false
	}

	t := reflect.TypeOf(entity)
	if t == nil {
		return false
	}

	// Dereference pointer
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// YAML handles structs, maps, slices, and basic types
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

// Encode serializes an entity to YAML bytes.
// Returns an error if the entity cannot be marshaled to YAML.
func (c *YAMLCodec) Encode(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilEntityError("yaml.Encode", "encode")
	}

	data, err := yaml.Marshal(entity)
	if err != nil {
		return nil, api.NewEncodeError("yaml.Encode", "YAML", err)
	}

	return data, nil
}

// Decode deserializes YAML bytes into an entity.
// The entity parameter must be a pointer to the target struct.
// Returns an error if the data is empty, invalid YAML, or cannot be unmarshaled.
func (c *YAMLCodec) Decode(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("yaml.Decode")
	}

	if entity == nil {
		return api.NewNilEntityError("yaml.Decode", "decode into")
	}

	// Verify entity is a pointer
	if reflect.TypeOf(entity).Kind() != reflect.Pointer {
		return api.NewNonPointerError("yaml.Decode", fmt.Sprintf("%T", entity))
	}

	if err := yaml.Unmarshal(data, entity); err != nil {
		return api.NewDecodeError("yaml.Decode", "YAML", err)
	}

	return nil
}
