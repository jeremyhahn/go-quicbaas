//go:build rest

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

package rest

// Binary protocol encoding functions for FastStateMachine compatibility
//
// FastStateMachine Binary Protocol:
// - PUT:    [0x01][keyLen:2bytes][key][valueLen:4bytes][value]
// - DELETE: [0x02][keyLen:2bytes][key]
// - BATCH:  [0x03][op1][op2]...[opN]
// - READ:   Raw key bytes only (no encoding)

const (
	opPut    byte = 0x01
	opDelete byte = 0x02
	opBatch  byte = 0x03
)

// encodePutBinary encodes a PUT operation in FastStateMachine binary format.
//
// Binary format: [0x01][keyLen:2bytes][key][valueLen:4bytes][value]
//
// Parameters:
//   - key: The key bytes (e.g., "entityType:id")
//   - value: The value bytes to store
//
// Returns:
//   - Binary-encoded PUT command ready for Raft ProposeWithMode
func encodePutBinary(key, value []byte) []byte {
	// Calculate total buffer size: opcode(1) + keyLen(2) + key + valueLen(4) + value
	buf := make([]byte, 1+2+len(key)+4+len(value))

	// Opcode
	buf[0] = opPut

	// Key length (2 bytes, big-endian)
	buf[1] = byte(len(key) >> 8)
	buf[2] = byte(len(key))

	// Key bytes
	copy(buf[3:], key)

	// Value length (4 bytes, big-endian)
	valueOffset := 3 + len(key)
	buf[valueOffset] = byte(len(value) >> 24)
	buf[valueOffset+1] = byte(len(value) >> 16)
	buf[valueOffset+2] = byte(len(value) >> 8)
	buf[valueOffset+3] = byte(len(value))

	// Value bytes
	copy(buf[valueOffset+4:], value)

	return buf
}

// encodeDeleteBinary encodes a DELETE operation in FastStateMachine binary format.
//
// Binary format: [0x02][keyLen:2bytes][key]
//
// Parameters:
//   - key: The key bytes to delete (e.g., "entityType:id")
//
// Returns:
//   - Binary-encoded DELETE command ready for Raft ProposeWithMode
func encodeDeleteBinary(key []byte) []byte {
	// Calculate total buffer size: opcode(1) + keyLen(2) + key
	buf := make([]byte, 1+2+len(key))

	// Opcode
	buf[0] = opDelete

	// Key length (2 bytes, big-endian)
	buf[1] = byte(len(key) >> 8)
	buf[2] = byte(len(key))

	// Key bytes
	copy(buf[3:], key)

	return buf
}

// encodeBatchBinary encodes a batch of operations in FastStateMachine binary format.
//
// Binary format: [0x03][op1][op2]...[opN]
//
// Each operation (op1, op2, etc.) is already encoded using encodePutBinary or encodeDeleteBinary.
//
// Parameters:
//   - operations: Slice of pre-encoded operation bytes
//
// Returns:
//   - Binary-encoded BATCH command ready for Raft ProposeWithMode
func encodeBatchBinary(operations [][]byte) []byte {
	// Calculate total size: opcode(1) + sum of all operation sizes
	totalSize := 1
	for _, op := range operations {
		totalSize += len(op)
	}

	buf := make([]byte, totalSize)

	// Opcode
	buf[0] = opBatch

	// Append all operations
	offset := 1
	for _, op := range operations {
		copy(buf[offset:], op)
		offset += len(op)
	}

	return buf
}
