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
	"reflect"

	"google.golang.org/protobuf/encoding/protojson"
	protobuf "google.golang.org/protobuf/proto"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/codec/proto"
)

// ProtobufCodec implements the Codec interface using Protocol Buffers.
//
// This implementation uses a hybrid approach:
// 1. Entities are first serialized to JSON (for flexibility)
// 2. The JSON is wrapped in a protobuf GenericMessage
// 3. The protobuf message is serialized to binary
//
// This approach provides:
// - Protocol buffer efficiency for wire format
// - Flexibility to encode any Go type without defining schemas
// - Better performance than pure JSON
// - Backward compatibility
//
// Priority: 100 (highest - most efficient binary format).
type ProtobufCodec struct{}

// NewProtobufCodec creates a new protobuf codec.
func NewProtobufCodec() *ProtobufCodec {
	return &ProtobufCodec{}
}

// Name returns the codec name.
func (c *ProtobufCodec) Name() string {
	return "protobuf"
}

// Priority returns the codec priority (highest = 100).
func (c *ProtobufCodec) Priority() int {
	return 100
}

// CanEncode checks if this codec can encode the given entity.
// Protobuf codec can encode any type that has protobuf tags or
// any type that can be marshaled to JSON.
func (c *ProtobufCodec) CanEncode(entity interface{}) bool {
	if entity == nil {
		return false
	}

	// Check if it's already a protobuf message
	if _, ok := entity.(protobuf.Message); ok {
		return true
	}

	// Check for protobuf struct tags
	t := reflect.TypeOf(entity)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() == reflect.Struct {
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if _, ok := field.Tag.Lookup("protobuf"); ok {
				return true
			}
		}
	}

	// If no protobuf tags, we can still encode via JSON wrapper
	// (any JSON-serializable type can be encoded)
	return true
}

// Encode serializes an entity using protobuf.
//
// For native protobuf messages, uses protobuf.Marshal directly.
// For other types, wraps them in GenericMessage after JSON encoding.
func (c *ProtobufCodec) Encode(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilEntityError("ProtobufCodec.Encode", "encode")
	}

	// Check if it's a native protobuf message
	if msg, ok := entity.(protobuf.Message); ok {
		data, err := protobuf.Marshal(msg)
		if err != nil {
			return nil, api.NewEncodeError("ProtobufCodec.Encode", "protobuf message", err)
		}
		return data, nil
	}

	// For non-protobuf types, encode via JSON wrapper
	// First serialize to JSON
	jsonData, err := json.Marshal(entity)
	if err != nil {
		return nil, api.NewEncodeError("ProtobufCodec.Encode", "entity to JSON", err)
	}

	// Get type name for debugging
	typeName := reflect.TypeOf(entity).String()

	// Wrap in GenericMessage
	wrapper := &proto.GenericMessage{
		JsonData: jsonData,
		TypeName: typeName,
	}

	// Marshal the wrapper
	data, err := protobuf.Marshal(wrapper)
	if err != nil {
		return nil, api.NewEncodeError("ProtobufCodec.Encode", "wrapper", err)
	}

	return data, nil
}

// Decode deserializes data into an entity using protobuf.
//
// For native protobuf messages, uses protobuf.Unmarshal directly.
// For other types, unwraps GenericMessage and decodes JSON.
func (c *ProtobufCodec) Decode(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("ProtobufCodec.Decode")
	}

	if entity == nil {
		return api.NewNilEntityError("ProtobufCodec.Decode", "decode into")
	}

	// Check if entity is a native protobuf message
	if msg, ok := entity.(protobuf.Message); ok {
		if err := protobuf.Unmarshal(data, msg); err != nil {
			return api.NewDecodeError("ProtobufCodec.Decode", "protobuf message", err)
		}
		return nil
	}

	// Try to unwrap as GenericMessage
	wrapper := &proto.GenericMessage{}
	if err := protobuf.Unmarshal(data, wrapper); err != nil {
		return api.NewDecodeError("ProtobufCodec.Decode", "wrapper", err)
	}

	// Decode the JSON data
	if err := json.Unmarshal(wrapper.JsonData, entity); err != nil {
		return api.NewDecodeError("ProtobufCodec.Decode", "JSON data", err)
	}

	return nil
}

// EncodeJSON is a convenience method that encodes a protobuf message to JSON.
// This is useful for debugging and human-readable output.
func (c *ProtobufCodec) EncodeJSON(entity interface{}) ([]byte, error) {
	msg, ok := entity.(protobuf.Message)
	if !ok {
		return nil, api.NewInvalidTypeError("entity", "a protobuf message")
	}

	data, err := protojson.Marshal(msg)
	if err != nil {
		return nil, api.NewEncodeError("ProtobufCodec.EncodeJSON", "protobuf to JSON", err)
	}

	return data, nil
}

// DecodeJSON is a convenience method that decodes JSON into a protobuf message.
func (c *ProtobufCodec) DecodeJSON(data []byte, entity interface{}) error {
	msg, ok := entity.(protobuf.Message)
	if !ok {
		return api.NewInvalidTypeError("entity", "a protobuf message")
	}

	if err := protojson.Unmarshal(data, msg); err != nil {
		return api.NewDecodeError("ProtobufCodec.DecodeJSON", "JSON to protobuf", err)
	}

	return nil
}
