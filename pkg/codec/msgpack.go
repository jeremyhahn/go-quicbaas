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

	"github.com/hashicorp/go-msgpack/v2/codec"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// MsgPackCodec implements MessagePack serialization using hashicorp/go-msgpack.
// MessagePack provides a binary, compact encoding suitable for performance-critical
// applications. Priority is 40 (lower than JSON but higher than YAML).
type MsgPackCodec struct{}

// NewMsgPackCodec creates a new MsgPack codec.
func NewMsgPackCodec() *MsgPackCodec {
	return &MsgPackCodec{}
}

// Name returns the codec name.
func (c *MsgPackCodec) Name() string {
	return "msgpack"
}

// Priority returns the codec's priority (40 for MsgPack).
// MsgPack is more compact than JSON and YAML, but not as fast as protobuf.
func (c *MsgPackCodec) Priority() int {
	return 40
}

// CanEncode determines if this codec can encode the given entity.
// MsgPack can encode most Go types similar to JSON.
func (c *MsgPackCodec) CanEncode(entity interface{}) bool {
	if entity == nil {
		return false
	}

	t := reflect.TypeOf(entity)
	if t == nil {
		return false
	}

	// Dereference pointer
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// MsgPack handles structs, maps, slices, and basic types
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

// Encode serializes an entity to MsgPack bytes.
// Returns an error if the entity cannot be encoded.
func (c *MsgPackCodec) Encode(entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilEntityError("msgpack.Encode", "encode")
	}

	var buf []byte
	enc := codec.NewEncoderBytes(&buf, &codec.MsgpackHandle{})

	if err := enc.Encode(entity); err != nil {
		return nil, api.NewEncodeError("msgpack.Encode", "msgpack", err)
	}

	return buf, nil
}

// Decode deserializes MsgPack bytes into an entity.
// The entity parameter must be a pointer to the target struct.
// Returns an error if the data is empty, invalid, or cannot be decoded.
func (c *MsgPackCodec) Decode(data []byte, entity interface{}) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("msgpack.Decode")
	}

	if entity == nil {
		return api.NewNilEntityError("msgpack.Decode", "decode into")
	}

	// Verify entity is a pointer
	if reflect.TypeOf(entity).Kind() != reflect.Ptr {
		return api.NewNonPointerError("msgpack.Decode", fmt.Sprintf("%T", entity))
	}

	dec := codec.NewDecoderBytes(data, &codec.MsgpackHandle{})

	if err := dec.Decode(entity); err != nil {
		return api.NewDecodeError("msgpack.Decode", "msgpack", err)
	}

	return nil
}
