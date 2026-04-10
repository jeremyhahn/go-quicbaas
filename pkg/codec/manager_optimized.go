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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// EncodeForStorageTo encodes an entity for storage into the provided buffer.
// This is a zero-allocation variant of EncodeForStorage when the buffer is reused.
//
// If the storage codec implements OptimizedCodec, it uses EncodeTo for efficiency.
// Otherwise, falls back to standard Encode (which allocates).
//
// Use this for:
//   - Hot paths where buffer reuse is possible
//   - Batch encoding operations
//   - Performance-critical storage operations
//
// Example:
//
//	buf := make([]byte, 0, 1024) // Reusable buffer
//	for _, entity := range entities {
//	    buf, err = manager.EncodeForStorageTo(buf[:0], entity)
//	    // use buf...
//	}
func (m *CodecManager) EncodeForStorageTo(buf []byte, entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilEntityError("codecmanager.EncodeForStorageTo", "encode")
	}

	m.mu.RLock()
	codec := m.storageCodec
	m.mu.RUnlock()

	// Try optimized path first
	if optCodec, ok := codec.(OptimizedCodec); ok {
		data, err := optCodec.EncodeTo(buf, entity)
		if err != nil {
			return nil, api.NewEncodeError("codecmanager.EncodeForStorageTo", "optimized "+codec.Name()+" codec", err)
		}
		return data, nil
	}

	// Fall back to standard encoding
	data, err := codec.Encode(entity)
	if err != nil {
		return nil, api.NewEncodeError("codecmanager.EncodeForStorageTo", "storage codec "+codec.Name(), err)
	}

	return data, nil
}

// DecodeFromStorageWith decodes storage data into an entity using an optional scratch buffer.
// This can reduce GC pressure in hot paths by reusing the scratch buffer for intermediate allocations.
//
// If the storage codec implements OptimizedCodec, it uses DecodeWith for efficiency.
// Otherwise, falls back to standard Decode.
//
// Use this for:
//   - Hot paths where scratch buffer can be reused
//   - Batch decoding operations
//   - Performance-critical read operations
//
// Example:
//
//	scratch := make([]byte, 4096) // Reusable scratch buffer
//	for _, data := range batch {
//	    entity := &MyEntity{}
//	    err = manager.DecodeFromStorageWith(data, entity, scratch)
//	    // use entity...
//	}
func (m *CodecManager) DecodeFromStorageWith(data []byte, entity interface{}, scratch []byte) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("codecmanager.DecodeFromStorageWith")
	}

	if entity == nil {
		return api.NewNilEntityError("codecmanager.DecodeFromStorageWith", "decode into")
	}

	m.mu.RLock()
	codec := m.storageCodec
	m.mu.RUnlock()

	// Try optimized path first
	if optCodec, ok := codec.(OptimizedCodec); ok {
		err := optCodec.DecodeWith(data, entity, scratch)
		if err != nil {
			return api.NewDecodeError("codecmanager.DecodeFromStorageWith", "optimized "+codec.Name()+" codec", err)
		}
		return nil
	}

	// Fall back to standard decoding
	err := codec.Decode(data, entity)
	if err != nil {
		return api.NewDecodeError("codecmanager.DecodeFromStorageWith", "storage codec "+codec.Name(), err)
	}

	return nil
}

// EncodeForStoragePooled encodes an entity for storage using a pooled buffer.
// Returns the encoded data, a cleanup function, and any error.
//
// The cleanup function MUST be called to return the buffer to the pool:
//
//	data, cleanup, err := manager.EncodeForStoragePooled(entity)
//	if err != nil {
//	    return err
//	}
//	defer cleanup()
//	// use data...
//
// This is the most efficient encoding method for one-time operations as it
// automatically manages buffer pooling without requiring caller buffer management.
func (m *CodecManager) EncodeForStoragePooled(entity interface{}) ([]byte, func(), error) {
	if entity == nil {
		return nil, nil, api.NewNilEntityError("codecmanager.EncodeForStoragePooled", "encode")
	}

	m.mu.RLock()
	codec := m.storageCodec
	m.mu.RUnlock()

	// Try optimized path first
	if optCodec, ok := codec.(OptimizedCodec); ok {
		data, cleanup, err := optCodec.EncodePooled(entity)
		if err != nil {
			return nil, nil, api.NewEncodeError("codecmanager.EncodeForStoragePooled", "optimized "+codec.Name()+" codec", err)
		}
		return data, cleanup, nil
	}

	// Fall back to standard encoding (no pooling, no cleanup needed)
	data, err := codec.Encode(entity)
	if err != nil {
		return nil, nil, api.NewEncodeError("codecmanager.EncodeForStoragePooled", "storage codec "+codec.Name(), err)
	}

	// Return noop cleanup function
	return data, func() {}, nil
}

// EncodeForWirePooled encodes an entity for wire transmission using a pooled buffer.
// Returns the encoded data, codec name used, cleanup function, and any error.
//
// The cleanup function MUST be called to return the buffer to the pool:
//
//	data, codecName, cleanup, err := manager.EncodeForWirePooled(entity)
//	if err != nil {
//	    return err
//	}
//	defer cleanup()
//	// use data...
//
// This method combines codec selection with buffer pooling for maximum efficiency.
func (m *CodecManager) EncodeForWirePooled(entity interface{}) ([]byte, string, func(), error) {
	if entity == nil {
		return nil, "", nil, api.NewNilEntityError("codecmanager.EncodeForWirePooled", "encode")
	}

	m.mu.RLock()
	dualMode := m.dualCodecMode
	m.mu.RUnlock()

	// In single-codec mode, use storage codec
	if !dualMode {
		return m.encodeForWirePooledWithCodec(entity, m.storageCodec)
	}

	// Dual-codec mode: select appropriate wire codec
	codec := m.selectWireCodec(entity)
	if codec == nil {
		return nil, "", nil, api.NewNoSuitableCodecError("codecmanager.EncodeForWirePooled", fmt.Sprintf("%T", entity))
	}

	return m.encodeForWirePooledWithCodec(entity, codec)
}

// encodeForWirePooledWithCodec is a helper that performs pooled encoding with a specific codec.
func (m *CodecManager) encodeForWirePooledWithCodec(entity interface{}, codec Codec) ([]byte, string, func(), error) {
	codecName := codec.Name()

	// Try optimized path first
	if optCodec, ok := codec.(OptimizedCodec); ok {
		data, cleanup, err := optCodec.EncodePooled(entity)
		if err != nil {
			return nil, "", nil, api.NewEncodeError("codecmanager.EncodeForWirePooled", "optimized "+codecName+" codec", err)
		}
		return data, codecName, cleanup, nil
	}

	// Fall back to standard encoding (no pooling, no cleanup needed)
	data, err := codec.Encode(entity)
	if err != nil {
		return nil, "", nil, api.NewEncodeError("codecmanager.EncodeForWirePooled", "wire codec "+codecName, err)
	}

	// Return noop cleanup function
	return data, codecName, func() {}, nil
}
