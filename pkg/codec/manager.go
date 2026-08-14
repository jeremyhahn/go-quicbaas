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
	"sync"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// CodecManager provides dual-codec functionality for QuicBaaS.
// It separates storage codec (optimized for disk) from wire codec (optimized for API).
//
// Architecture:
//   - Storage codec: Fixed format for on-disk persistence (typically msgpack for efficiency)
//   - Wire codec: Entity-specific format for API responses (auto-detected or configured)
//
// The manager provides:
//   - Entity-level codec selection via quicbaas:"codec=X" tags
//   - Auto-detection based on struct tags (json, yaml, protobuf, msgpack)
//   - Fallback to configured default wire codec
//   - Thread-safe codec management with internal registry
//   - Optimized codecs with buffer pooling for performance-critical paths
type CodecManager struct {
	mu               sync.RWMutex
	registry         *Registry
	storageCodec     Codec
	defaultWireCodec Codec
	autoDetect       bool
	dualCodecMode    bool
}

// Config configures the CodecManager's dual-codec behavior.
type Config struct {
	// StorageCodec is the codec name for on-disk persistence (e.g., "msgpack")
	StorageCodec string

	// DefaultWireCodec is the default codec for API responses (e.g., "json")
	DefaultWireCodec string

	// AutoDetect enables automatic wire codec detection from struct tags
	AutoDetect bool

	// DualCodecMode enables dual-codec architecture (separate storage/wire codecs)
	// When false, uses single codec for both storage and wire
	DualCodecMode bool

	// UseOptimizedCodecs enables optimized codecs with buffer pooling (default: true)
	UseOptimizedCodecs bool
}

// DefaultConfig returns a Config with sensible defaults for dual-codec mode.
func DefaultConfig() *Config {
	return &Config{
		StorageCodec:       "msgpack", // Efficient binary format for storage
		DefaultWireCodec:   "json",    // Human-readable format for APIs
		AutoDetect:         true,      // Enable auto-detection from tags
		DualCodecMode:      true,      // Enable dual-codec mode
		UseOptimizedCodecs: true,      // Enable optimized codecs by default
	}
}

// NewCodecManager creates a new dual-codec manager with the given configuration.
// It initializes the codec registry with standard codecs (json, msgpack, yaml, protobuf).
//
// When UseOptimizedCodecs is true (default), optimized codecs with buffer pooling
// are registered for json and msgpack, providing significant performance improvements.
//
// Returns an error if:
//   - Storage codec is not registered
//   - Default wire codec is not registered
//   - Configuration is invalid
func NewCodecManager(cfg *Config) (*CodecManager, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Create registry and register codecs
	registry := NewRegistry()

	// Register optimized or standard codecs based on configuration
	if cfg.UseOptimizedCodecs {
		// Register optimized codecs for json and msgpack
		registry.Register(NewJSONOptimized())
		registry.Register(NewMsgPackOptimized())
	} else {
		// Register standard codecs
		registry.Register(NewJSONCodec())
		registry.Register(NewMsgPackCodec())
	}

	// Always register yaml and protobuf with standard implementations
	registry.Register(NewYAMLCodec())
	registry.Register(NewProtobufCodec())

	// Retrieve storage codec
	storageCodec := registry.Get(cfg.StorageCodec)
	if storageCodec == nil {
		return nil, api.NewCodecNotFoundError("codecmanager.New", cfg.StorageCodec)
	}

	// Retrieve default wire codec
	defaultWireCodec := registry.Get(cfg.DefaultWireCodec)
	if defaultWireCodec == nil {
		return nil, api.NewCodecNotFoundError("codecmanager.New", cfg.DefaultWireCodec)
	}

	return &CodecManager{
		registry:         registry,
		storageCodec:     storageCodec,
		defaultWireCodec: defaultWireCodec,
		autoDetect:       cfg.AutoDetect,
		dualCodecMode:    cfg.DualCodecMode,
	}, nil
}

// EncodeForStorage encodes an entity using the configured storage codec.
// This method always uses the storage codec regardless of entity tags or auto-detection.
//
// Use this for:
//   - Persisting data to disk
//   - Storing data in PebbleDB
//   - Creating snapshots
//
// Returns encoded bytes and error if encoding fails.
func (m *CodecManager) EncodeForStorage(entity interface{}) ([]byte, error) {
	m.mu.RLock()
	codec := m.storageCodec
	m.mu.RUnlock()

	if entity == nil {
		return nil, api.NewNilEntityError("codecmanager.EncodeForStorage", "encode")
	}

	data, err := codec.Encode(entity)
	if err != nil {
		return nil, api.NewEncodeError("codecmanager.EncodeForStorage", "storage codec "+codec.Name(), err)
	}

	return data, nil
}

// DecodeFromStorage decodes data using the configured storage codec.
// This method always uses the storage codec regardless of entity tags or auto-detection.
//
// Use this for:
//   - Reading data from disk
//   - Loading data from PebbleDB
//   - Restoring from snapshots
//
// The entity parameter must be a pointer to the target struct.
// Returns error if decoding fails.
func (m *CodecManager) DecodeFromStorage(data []byte, entity interface{}) error {
	m.mu.RLock()
	codec := m.storageCodec
	m.mu.RUnlock()

	if len(data) == 0 {
		return api.NewEmptyDataError("codecmanager.DecodeFromStorage")
	}

	if entity == nil {
		return api.NewNilEntityError("codecmanager.DecodeFromStorage", "decode into")
	}

	err := codec.Decode(data, entity)
	if err != nil {
		return api.NewDecodeError("codecmanager.DecodeFromStorage", "storage codec "+codec.Name(), err)
	}

	return nil
}

// EncodeForWire encodes an entity for API wire transmission using the appropriate codec.
// In dual-codec mode, this may differ from the storage codec.
//
// Codec selection priority:
//  1. Explicit quicbaas:"codec=X" tag on entity fields
//  2. Auto-detection from struct tags (if enabled)
//  3. Default wire codec
//
// Use this for:
//   - REST API responses
//   - gRPC message encoding
//   - Client communication
//
// Returns encoded bytes, codec name used, and error if encoding fails.
func (m *CodecManager) EncodeForWire(entity interface{}) ([]byte, string, error) {
	if entity == nil {
		return nil, "", api.NewNilEntityError("codecmanager.EncodeForWire", "encode")
	}

	m.mu.RLock()
	dualMode := m.dualCodecMode
	m.mu.RUnlock()

	// In single-codec mode, just use storage codec
	if !dualMode {
		data, err := m.EncodeForStorage(entity)
		if err != nil {
			return nil, "", err
		}
		return data, m.storageCodec.Name(), nil
	}

	// Dual-codec mode: select appropriate wire codec
	codec := m.selectWireCodec(entity)
	if codec == nil {
		return nil, "", api.NewNoSuitableCodecError("codecmanager.EncodeForWire", fmt.Sprintf("%T", entity))
	}

	data, err := codec.Encode(entity)
	if err != nil {
		return nil, "", api.NewEncodeError("codecmanager.EncodeForWire", "wire codec "+codec.Name(), err)
	}

	return data, codec.Name(), nil
}

// DecodeFromWire decodes wire data into an entity using the specified codec.
//
// Use this for:
//   - Processing REST API requests
//   - Decoding gRPC messages
//   - Handling client data
//
// The entity parameter must be a pointer to the target struct.
// The codecName must match a registered codec.
// Returns error if codec not found or decoding fails.
func (m *CodecManager) DecodeFromWire(data []byte, entity interface{}, codecName string) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("codecmanager.DecodeFromWire")
	}

	if entity == nil {
		return api.NewNilEntityError("codecmanager.DecodeFromWire", "decode into")
	}

	if codecName == "" {
		return api.NewRequiredFieldError("codec name")
	}

	m.mu.RLock()
	codec := m.registry.Get(codecName)
	m.mu.RUnlock()

	if codec == nil {
		return api.NewCodecNotFoundError("codecmanager.DecodeFromWire", codecName)
	}

	err := codec.Decode(data, entity)
	if err != nil {
		return api.NewDecodeError("codecmanager.DecodeFromWire", "codec "+codecName, err)
	}

	return nil
}

// selectWireCodec selects the appropriate wire codec for an entity.
// This implements the codec selection priority logic.
func (m *CodecManager) selectWireCodec(entity interface{}) Codec {
	// Priority 1: Explicit quicbaas:"codec=X" tag
	codecName := m.detectEntityCodec(entity)
	if codecName != "" {
		m.mu.RLock()
		codec := m.registry.Get(codecName)
		m.mu.RUnlock()
		if codec != nil {
			return codec
		}
	}

	// Priority 2: Auto-detect from struct tags (if enabled)
	m.mu.RLock()
	autoDetect := m.autoDetect
	m.mu.RUnlock()

	if autoDetect {
		codecName = m.autoDetectCodec(entity)
		if codecName != "" {
			m.mu.RLock()
			codec := m.registry.Get(codecName)
			m.mu.RUnlock()
			if codec != nil {
				return codec
			}
		}
	}

	// Priority 3: Default wire codec
	m.mu.RLock()
	defaultCodec := m.defaultWireCodec
	m.mu.RUnlock()

	return defaultCodec
}

// detectEntityCodec checks for explicit quicbaas:"codec=X" tag on entity fields.
// Returns the codec name if found, empty string otherwise.
func (m *CodecManager) detectEntityCodec(entity interface{}) string {
	t := reflect.TypeOf(entity)
	if t == nil {
		return ""
	}

	// Dereference pointer
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return ""
	}

	// Check all fields for quicbaas:"codec=X" tag
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

// autoDetectCodec inspects struct field tags to determine the best codec.
// Priority: protobuf > json > msgpack > yaml
// Returns the codec name if detected, empty string otherwise.
func (m *CodecManager) autoDetectCodec(entity interface{}) string {
	t := reflect.TypeOf(entity)
	if t == nil {
		return ""
	}

	// Dereference pointer
	if t.Kind() == reflect.Pointer {
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

// GetStorageCodecName returns the name of the configured storage codec.
func (m *CodecManager) GetStorageCodecName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.storageCodec.Name()
}

// GetDefaultWireCodecName returns the name of the default wire codec.
func (m *CodecManager) GetDefaultWireCodecName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultWireCodec.Name()
}

// ListCodecs returns the names of all registered codecs.
func (m *CodecManager) ListCodecs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.registry.ListCodecs()
}

// IsDualCodecMode returns true if dual-codec mode is enabled.
func (m *CodecManager) IsDualCodecMode() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.dualCodecMode
}

// IsAutoDetectEnabled returns true if auto-detection is enabled.
func (m *CodecManager) IsAutoDetectEnabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.autoDetect
}
