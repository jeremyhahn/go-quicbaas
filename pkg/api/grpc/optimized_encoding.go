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
