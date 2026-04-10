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
	"bytes"

	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/jeremyhahn/go-codec/pkg/pool"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// MsgPackOptimizedCodec extends MsgPackCodec with optimized zero-allocation methods.
// It uses buffer pooling from go-codec to minimize allocations in hot paths.
type MsgPackOptimizedCodec struct {
	*MsgPackCodec
	handle *codec.MsgpackHandle
}

// NewMsgPackOptimized creates an optimized MsgPack codec with buffer pooling support.
func NewMsgPackOptimized() *MsgPackOptimizedCodec {
	return &MsgPackOptimizedCodec{
		MsgPackCodec: NewMsgPackCodec(),
		handle:       &codec.MsgpackHandle{},
	}
}

// EncodeTo encodes entity into the provided buffer.
// If the buffer has sufficient capacity, it reuses it. Otherwise, allocates a new buffer.
// This method minimizes allocations when buffers are reused.
func (c *MsgPackOptimizedCodec) EncodeTo(buf []byte, entity interface{}) ([]byte, error) {
	if entity == nil {
		return nil, api.NewNilEntityError("msgpack.EncodeTo", "encode")
	}

	// Use a pooled bytes.Buffer for encoding
	byteBuf := pool.GetBytesBuffer()
	defer pool.PutBytesBuffer(byteBuf)

	enc := codec.NewEncoder(byteBuf, c.handle)
	if err := enc.Encode(entity); err != nil {
		return nil, api.NewEncodeError("msgpack.EncodeTo", "msgpack", err)
	}

	encoded := byteBuf.Bytes()

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
// The scratch buffer is not used by msgpack decoder but kept for interface compatibility.
func (c *MsgPackOptimizedCodec) DecodeWith(data []byte, entity interface{}, scratch []byte) error {
	if len(data) == 0 {
		return api.NewEmptyDataError("msgpack.DecodeWith")
	}

	if entity == nil {
		return api.NewNilEntityError("msgpack.DecodeWith", "decode into")
	}

	// Use bytes.Reader to avoid allocations
	reader := bytes.NewReader(data)
	dec := codec.NewDecoder(reader, c.handle)

	if err := dec.Decode(entity); err != nil {
		return api.NewDecodeError("msgpack.DecodeWith", "msgpack", err)
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
func (c *MsgPackOptimizedCodec) EncodePooled(entity interface{}) ([]byte, func(), error) {
	if entity == nil {
		return nil, nil, api.NewNilEntityError("msgpack.EncodePooled", "encode")
	}

	// EncodeTo uses internal bytes.Buffer pooling
	result, err := c.EncodeTo(nil, entity)
	if err != nil {
		return nil, nil, err
	}

	// Return no-op cleanup function for API compatibility
	return result, func() {}, nil
}
