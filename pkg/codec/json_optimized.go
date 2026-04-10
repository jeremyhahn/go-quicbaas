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

	"github.com/jeremyhahn/go-codec/pkg/pool"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// JSONOptimizedCodec extends JSONCodec with optimized zero-allocation methods.
// It uses buffer pooling from go-codec to minimize allocations in hot paths.
type JSONOptimizedCodec struct {
	*JSONCodec
}

// NewJSONOptimized creates an optimized JSON codec with buffer pooling support.
func NewJSONOptimized() *JSONOptimizedCodec {
	return &JSONOptimizedCodec{
		JSONCodec: NewJSONCodec(),
	}
}

// EncodeTo encodes entity into the provided buffer.
// If the buffer has sufficient capacity, it reuses it. Otherwise, allocates a new buffer.
// This method minimizes allocations when buffers are reused.
func (c *JSONOptimizedCodec) EncodeTo(buf []byte, entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilEntityError("json.EncodeTo", "encode")
	}

	// Use a pooled bytes.Buffer for encoding
	byteBuf := pool.GetBytesBuffer()
	defer pool.PutBytesBuffer(byteBuf)

	encoder := json.NewEncoder(byteBuf)
	if err := encoder.Encode(entity); err != nil {
		return nil, api.NewEncodeError("json.EncodeTo", "JSON", err)
	}

	// Remove the trailing newline that json.Encoder adds
	encoded := byteBuf.Bytes()
	if len(encoded) > 0 && encoded[len(encoded)-1] == '\n' {
		encoded = encoded[:len(encoded)-1]
	}

	// If provided buffer has enough capacity, copy into it
	if cap(buf) >= len(encoded) {
		buf = buf[:len(encoded)]
		copy(buf, encoded)
		return buf, nil
	}

	// Otherwise allocate new buffer
	result := make([]byte, len(encoded))
	copy(result, encoded)
	return result, nil
}

// DecodeWith decodes data into entity using an optional scratch buffer.
// The scratch buffer is not used by JSON decoder but kept for interface compatibility.
func (c *JSONOptimizedCodec) DecodeWith(data []byte, entity interface{}, scratch []byte) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("json.DecodeWith")
	}

	if entity == nil {
		return api.NewNilEntityError("json.DecodeWith", "decode into")
	}

	// For JSON, we don't benefit from scratch buffer as json.Unmarshal doesn't use it
	if err := json.Unmarshal(data, entity); err != nil {
		return api.NewDecodeError("json.DecodeWith", "JSON", err)
	}

	return nil
}

// EncodePooled encodes entity using optimized encoding with internal buffer pooling.
// Returns the encoded data and a cleanup function for API compatibility.
//
// Example usage:
//
//	data, cleanup, err := codec.EncodePooled(entity)
//	if err != nil {
//	    return err
//	}
//	defer cleanup()
//	// use data...
//
// Note: EncodeTo already uses internal bytes.Buffer pooling from go-codec v1.0.0,
// so no additional cleanup is needed. The cleanup function is provided for API compatibility.
func (c *JSONOptimizedCodec) EncodePooled(entity interface{}) ([]byte, func(), error) {
	if entity == nil {
		return nil, nil, api.NewNilEntityError("json.EncodePooled", "encode")
	}

	// EncodeTo uses internal bytes.Buffer pooling
	result, err := c.EncodeTo(nil, entity)
	if err != nil {
		return nil, nil, err
	}

	// Return no-op cleanup function for API compatibility
	return result, func() {}, nil
}
