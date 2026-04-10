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

package grpc

import (
	"encoding/binary"
	"math"
	"strconv"
	"sync"
)

// Buffer pool for building JSON commands - reduces GC pressure.
var cmdBufferPool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate 512 bytes - enough for most commands
		b := make([]byte, 0, 512)
		return &b
	},
}

// buildPutCommand builds a JSON Put command with minimal allocations
// Format: {"entity_type":"TYPE","id":"ID","data":"DATA"}
// This avoids:
// - map[string]interface{} allocation
// - json.Marshal() reflection overhead
// - Multiple string concatenations.
func buildPutCommand(entityType string, id uint64, data []byte) []byte {
	poolObj := cmdBufferPool.Get()
	bufPtr, ok := poolObj.(*[]byte)
	if !ok {
		return nil
	}
	buf := (*bufPtr)[:0] // Reset to zero length, keep capacity

	// Build JSON manually
	buf = append(buf, `{"entity_type":"`...)
	buf = append(buf, entityType...)
	buf = append(buf, `","id":"`...)
	buf = strconv.AppendUint(buf, id, 10)
	buf = append(buf, `","data":"`...)

	// Escape data if needed (simple JSON string escape)
	buf = appendJSONEscaped(buf, data)

	buf = append(buf, `"}`...)

	// Make a copy to return (caller owns this memory)
	result := make([]byte, len(buf))
	copy(result, buf)

	// Return buffer to pool
	cmdBufferPool.Put(bufPtr)

	return result
}

// appendJSONEscaped appends data to buf with JSON string escaping.
func appendJSONEscaped(buf, data []byte) []byte {
	for _, b := range data {
		switch b {
		case '"':
			buf = append(buf, '\\', '"')
		case '\\':
			buf = append(buf, '\\', '\\')
		case '\n':
			buf = append(buf, '\\', 'n')
		case '\r':
			buf = append(buf, '\\', 'r')
		case '\t':
			buf = append(buf, '\\', 't')
		default:
			if b < 0x20 {
				// Control characters - use unicode escape
				buf = append(buf, '\\', 'u', '0', '0')
				const hex = "0123456789abcdef"
				buf = append(buf, hex[b>>4], hex[b&0xF])
			} else {
				buf = append(buf, b)
			}
		}
	}
	return buf
}

// buildDeleteCommand builds a JSON Delete command with minimal allocations
// Format: {"operation":"delete","entity_type":"TYPE","id":"ID"}.
func buildDeleteCommand(entityType string, id uint64) []byte {
	poolObj := cmdBufferPool.Get()
	bufPtr, ok := poolObj.(*[]byte)
	if !ok {
		return nil
	}
	buf := (*bufPtr)[:0]

	buf = append(buf, `{"operation":"delete","entity_type":"`...)
	buf = append(buf, entityType...)
	buf = append(buf, `","id":"`...)
	buf = strconv.AppendUint(buf, id, 10)
	buf = append(buf, `"}`...)

	result := make([]byte, len(buf))
	copy(result, buf)

	cmdBufferPool.Put(bufPtr)
	return result
}

// ===== Binary Protocol Encoding (FastStateMachine) =====

const (
	opPut    = 0x01
	opDelete = 0x02
)

// encodePutBinary encodes a PUT operation in binary format for FastStateMachine
// Binary format: [op:1byte][keyLen:2bytes][key][valueLen:4bytes][value].
func encodePutBinary(key, value []byte) []byte {
	buf := make([]byte, 1+2+len(key)+4+len(value))
	buf[0] = opPut
	// Safe conversion: int to uint16 with bounds checking
	keyLen := len(key)
	if keyLen > math.MaxUint16 {
		keyLen = math.MaxUint16
	}
	binary.BigEndian.PutUint16(buf[1:3], uint16(keyLen)) //nolint:gosec // G115: keyLen bounded by MaxUint16 check
	copy(buf[3:], key)
	// Safe conversion: int to uint32 with bounds checking
	valueLen := len(value)
	if valueLen > math.MaxUint32 {
		valueLen = math.MaxUint32
	}
	binary.BigEndian.PutUint32(buf[3+len(key):], uint32(valueLen)) //nolint:gosec // G115: valueLen bounded by MaxUint32 check
	copy(buf[7+len(key):], value)
	return buf
}

// encodeDeleteBinary encodes a DELETE operation in binary format for FastStateMachine
// Binary format: [op:1byte][keyLen:2bytes][key].
func encodeDeleteBinary(key []byte) []byte {
	buf := make([]byte, 1+2+len(key))
	buf[0] = opDelete
	// Safe conversion: int to uint16 with bounds checking
	keyLen := len(key)
	if keyLen > math.MaxUint16 {
		keyLen = math.MaxUint16
	}
	binary.BigEndian.PutUint16(buf[1:3], uint16(keyLen)) //nolint:gosec // G115: keyLen bounded by MaxUint16 check
	copy(buf[3:], key)
	return buf
}

// encodeBatchBinary encodes multiple operations in a single binary batch for FastStateMachine
// Binary format: [0x03][op1][op2]...[opN]
// Each operation is a full encoded operation (with its opcode: 0x01 for PUT, 0x02 for DELETE)
// This matches FastStateMachine's handleBatch() expectations.
func encodeBatchBinary(operations [][]byte) []byte {
	// Calculate total size: 1 byte for batch opcode + all operations
	totalSize := 1 // 1 byte for opBatch (0x03)
	for _, op := range operations {
		totalSize += len(op)
	}

	// Allocate buffer
	buf := make([]byte, totalSize)

	// Write batch opcode
	buf[0] = 0x03 // opBatch

	// Write each operation (keeping their opcodes: 0x01 or 0x02)
	offset := 1
	for _, op := range operations {
		copy(buf[offset:], op)
		offset += len(op)
	}

	return buf
}
