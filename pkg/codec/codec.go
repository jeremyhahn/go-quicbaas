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
	"strings"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// Codec defines the interface for entity serialization/deserialization.
// Each codec implementation (JSON, Protobuf, MsgPack, YAML) must implement
// these methods to provide encoding and decoding capabilities.
type Codec interface {
	// Name returns the codec name (e.g., "json", "protobuf", "msgpack", "yaml")
	Name() string

	// Encode serializes an entity to bytes
	Encode(entity interface{}) ([]byte, error)

	// Decode deserializes bytes into an entity
	Decode(data []byte, entity interface{}) error

	// CanEncode determines if this codec can encode the given entity
	// based on struct tags or other metadata
	CanEncode(entity interface{}) bool

	// Priority returns the codec's priority when multiple codecs can encode
	// an entity. Higher priority codecs are preferred. Default is 0.
	// Protobuf = 100, JSON = 50, MsgPack = 40, YAML = 30
	Priority() int
}

// OptimizedCodec extends Codec with zero-allocation methods for performance-critical paths.
// Implementations should use buffer pooling and minimize allocations.
type OptimizedCodec interface {
	Codec

	// EncodeTo encodes entity into the provided buffer. If the buffer has sufficient
	// capacity, no allocation occurs. Otherwise, a new buffer is allocated.
	// Returns the buffer containing encoded data (may be the input buffer or a new one).
	EncodeTo(buf []byte, entity interface{}) ([]byte, error)

	// DecodeWith decodes data into entity using an optional scratch buffer.
	// The scratch buffer can be used for intermediate allocations to reduce GC pressure.
	// Pass nil for scratch if no scratch buffer is available.
	DecodeWith(data []byte, entity interface{}, scratch []byte) error

	// EncodePooled encodes entity using a pooled buffer and returns:
	//   - encoded data
	//   - cleanup function to return buffer to pool (must be called)
	//   - error if encoding fails
	// The cleanup function must be called to prevent buffer leaks.
	EncodePooled(entity interface{}) ([]byte, func(), error)
}

// Registry manages codec registration and selection.
// It provides automatic codec detection based on struct tags and
// manual codec selection for explicit control.
type Registry struct {
	codecs map[string]Codec
}

// NewRegistry creates a new codec registry with no codecs registered.
// Use Register() to add codecs to the registry.
func NewRegistry() *Registry {
	return &Registry{
		codecs: make(map[string]Codec),
	}
}

// Register adds a codec to the registry.
// If a codec with the same name already exists, it is replaced.
func (r *Registry) Register(codec Codec) {
	r.codecs[codec.Name()] = codec
}

// Get retrieves a codec by name.
// Returns nil if the codec is not registered.
func (r *Registry) Get(name string) Codec {
	return r.codecs[name]
}

// EncodeAuto automatically selects the best codec for the entity
// and encodes it. Returns the encoded bytes and the codec name used.
//
// Codec selection priority:
// 1. Check for explicit `quicbaas:"codec=X"` tag
// 2. Auto-detect based on struct tags (protobuf > json > msgpack > yaml)
// 3. Fall back to highest priority codec that can encode the entity.
func (r *Registry) EncodeAuto(entity interface{}) ([]byte, string, error) {
	// First, check for explicit codec tag
	codecName := r.detectExplicitCodec(entity)
	if codecName != "" {
		codec := r.Get(codecName)
		if codec == nil {
			return nil, "", api.NewCodecNotFoundError("codec.EncodeAuto", codecName)
		}
		data, err := codec.Encode(entity)
		if err != nil {
			return nil, "", api.NewEncodeError("codec.EncodeAuto", codecName, err)
		}
		return data, codecName, nil
	}

	// Auto-detect based on struct tags
	codecName = r.detectCodecFromTags(entity)
	if codecName != "" {
		codec := r.Get(codecName)
		if codec != nil {
			data, err := codec.Encode(entity)
			if err != nil {
				return nil, "", api.NewEncodeError("codec.EncodeAuto", codecName, err)
			}
			return data, codecName, nil
		}
	}

	// Fall back to highest priority codec
	var bestCodec Codec
	bestPriority := -1

	for _, codec := range r.codecs {
		if codec.CanEncode(entity) && codec.Priority() > bestPriority {
			bestCodec = codec
			bestPriority = codec.Priority()
		}
	}

	if bestCodec == nil {
		return nil, "", api.NewNoSuitableCodecError("codec.EncodeAuto", fmt.Sprintf("%T", entity))
	}

	data, err := bestCodec.Encode(entity)
	if err != nil {
		return nil, "", api.NewEncodeError("codec.EncodeAuto", bestCodec.Name(), err)
	}

	return data, bestCodec.Name(), nil
}

// Encode encodes an entity using the specified codec.
// Returns an error if the codec is not found.
func (r *Registry) Encode(entity interface{}, codecName string) ([]byte, error) {
	codec := r.Get(codecName)
	if codec == nil {
		return nil, api.NewCodecNotFoundError("codec.Encode", codecName)
	}

	data, err := codec.Encode(entity)
	if err != nil {
		return nil, api.NewEncodeError("codec.Encode", codecName, err)
	}

	return data, nil
}

// Decode decodes bytes into an entity using the specified codec.
// Returns an error if the codec is not found.
func (r *Registry) Decode(data []byte, entity interface{}, codecName string) error {
	codec := r.Get(codecName)
	if codec == nil {
		return api.NewCodecNotFoundError("codec.Decode", codecName)
	}

	err := codec.Decode(data, entity)
	if err != nil {
		return api.NewDecodeError("codec.Decode", codecName, err)
	}

	return nil
}

// detectExplicitCodec checks for an explicit `quicbaas:"codec=X"` tag on any field.
// Returns the codec name if found, empty string otherwise.
func (r *Registry) detectExplicitCodec(entity interface{}) string {
	t := reflect.TypeOf(entity)
	if t == nil {
		return ""
	}

	// Dereference pointer
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return ""
	}

	// Check struct-level tag if available
	// (Go doesn't support struct tags on the type itself, only fields)
	// So we check the first field for a special marker

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("quicbaas")
		if tag == "" {
			continue
		}

		// Parse quicbaas tag for codec=X
		parts := strings.Split(tag, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "codec=") {
				return strings.TrimPrefix(part, "codec=")
			}
		}
	}

	return ""
}

// detectCodecFromTags inspects struct field tags to determine the best codec.
// Priority: protobuf > json > msgpack > yaml.
func (r *Registry) detectCodecFromTags(entity interface{}) string {
	t := reflect.TypeOf(entity)
	if t == nil {
		return ""
	}

	// Dereference pointer
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return ""
	}

	hasProtobuf := false
	hasJSON := false
	hasMsgPack := false
	hasYAML := false

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		// Check for protobuf tags
		if _, ok := field.Tag.Lookup("protobuf"); ok {
			hasProtobuf = true
		}

		// Check for json tags
		if _, ok := field.Tag.Lookup("json"); ok {
			hasJSON = true
		}

		// Check for msgpack tags
		if _, ok := field.Tag.Lookup("msgpack"); ok {
			hasMsgPack = true
		}

		// Check for yaml tags
		if _, ok := field.Tag.Lookup("yaml"); ok {
			hasYAML = true
		}
	}

	// Return highest priority codec found
	if hasProtobuf {
		return "protobuf"
	}
	if hasJSON {
		return "json"
	}
	if hasMsgPack {
		return "msgpack"
	}
	if hasYAML {
		return "yaml"
	}

	return ""
}

// ListCodecs returns the names of all registered codecs.
func (r *Registry) ListCodecs() []string {
	names := make([]string, 0, len(r.codecs))
	for name := range r.codecs {
		names = append(names, name)
	}
	return names
}
