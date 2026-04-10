//go:build quic

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

package quic

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Mock ObjectStoreReader
// =============================================================================

type mockObjectStoreReader struct {
	readObjectFn func(objectRef string) ([]byte, error)
}

func (m *mockObjectStoreReader) ReadObject(objectRef string) ([]byte, error) {
	if m.readObjectFn != nil {
		return m.readObjectFn(objectRef)
	}
	return []byte("object-data-" + objectRef), nil
}

// =============================================================================
// ObjectTransferRegistry Tests
// =============================================================================

func TestObjectTransferRegistry_RegisterAndGet(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{}

	registry.RegisterStore(1, store)

	got, ok := registry.GetStore(1)
	if !ok {
		t.Fatal("Expected to find registered store")
	}
	if got != store {
		t.Fatal("Expected same store back")
	}
}

func TestObjectTransferRegistry_GetNonExistent(t *testing.T) {
	registry := NewObjectTransferRegistry()

	_, ok := registry.GetStore(999)
	if ok {
		t.Fatal("Expected not found for non-existent shard")
	}
}

func TestObjectTransferRegistry_Unregister(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{}

	registry.RegisterStore(1, store)
	registry.UnregisterStore(1)

	_, ok := registry.GetStore(1)
	if ok {
		t.Fatal("Expected not found after unregister")
	}
}

func TestObjectTransferRegistry_MultipleStores(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store1 := &mockObjectStoreReader{}
	store2 := &mockObjectStoreReader{}

	registry.RegisterStore(1, store1)
	registry.RegisterStore(2, store2)

	got1, ok1 := registry.GetStore(1)
	got2, ok2 := registry.GetStore(2)

	if !ok1 || !ok2 {
		t.Fatal("Expected to find both stores")
	}
	if got1 != store1 || got2 != store2 {
		t.Fatal("Expected correct stores returned")
	}
}

// =============================================================================
// EncodeFetchObjectRequest / DecodeFetchObjectRequest Tests
// =============================================================================

func TestEncodeFetchObjectRequest_Success(t *testing.T) {
	req := &FetchObjectRequest{
		ShardID:   42,
		ObjectRef: "obj-ref-123",
		Checksum:  []byte{1, 2, 3, 4},
		RequestID: "req-001",
	}

	data, err := EncodeFetchObjectRequest(req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Expected non-empty encoded data")
	}

	decoded, err := DecodeFetchObjectRequest(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decoded.ShardID != req.ShardID {
		t.Fatalf("ShardID mismatch: got %d, want %d", decoded.ShardID, req.ShardID)
	}
	if decoded.ObjectRef != req.ObjectRef {
		t.Fatalf("ObjectRef mismatch: got %q, want %q", decoded.ObjectRef, req.ObjectRef)
	}
	if !bytes.Equal(decoded.Checksum, req.Checksum) {
		t.Fatal("Checksum mismatch")
	}
	if decoded.RequestID != req.RequestID {
		t.Fatalf("RequestID mismatch: got %q, want %q", decoded.RequestID, req.RequestID)
	}
}

func TestEncodeFetchObjectRequest_NilRequest(t *testing.T) {
	_, err := EncodeFetchObjectRequest(nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestDecodeFetchObjectRequest_TooShort(t *testing.T) {
	_, err := DecodeFetchObjectRequest([]byte{1, 2})
	if err == nil {
		t.Fatal("Expected error for too-short data")
	}
}

func TestEncodeFetchObjectRequest_EmptyFields(t *testing.T) {
	req := &FetchObjectRequest{
		ShardID: 1,
	}

	data, err := EncodeFetchObjectRequest(req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodeFetchObjectRequest(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decoded.ShardID != 1 {
		t.Fatal("ShardID mismatch")
	}
	if decoded.ObjectRef != "" {
		t.Fatal("Expected empty ObjectRef")
	}
}

// =============================================================================
// DecodeFetchObjectRequest - Truncated Binary Error Branches
// These tests provide data that passes the minimum length check (14 bytes)
// but is truncated at specific points to trigger read errors.
// =============================================================================

// TestDecodeFetchObjectRequest_TruncatedObjectRef provides a refLen > 0
// but not enough remaining bytes for the object ref data.
func TestDecodeFetchObjectRequest_TruncatedObjectRef(t *testing.T) {
	buf := make([]byte, 14)
	binary.BigEndian.PutUint64(buf[0:], 1)  // ShardID = 1
	binary.BigEndian.PutUint16(buf[8:], 50) // refLen = 50 (but only 4 bytes remain)
	binary.BigEndian.PutUint16(buf[10:], 0) // checksumLen = 0
	binary.BigEndian.PutUint16(buf[12:], 0) // reqIDLen = 0

	_, err := DecodeFetchObjectRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated object ref data")
	}
}

// TestDecodeFetchObjectRequest_TruncatedChecksumLen provides valid ShardID
// and objectRef but truncates before the checksum length can be read.
func TestDecodeFetchObjectRequest_TruncatedChecksumLen(t *testing.T) {
	// ShardID(8) + refLen(2, value=0) = 10 bytes, then we need checksumLen(2)
	// but provide only 10 bytes total (passes 14 min? No, 10 < 14)
	// We need >= 14 bytes. Let's write refLen=0 but end data after the ref.
	// Actually, the min check is 14, so we need at least 14 bytes total.
	// With refLen=3, we have: 8 + 2 + 3 = 13, need checksumLen but only 1 byte left
	buf := make([]byte, 14)
	binary.BigEndian.PutUint64(buf[0:], 1) // ShardID = 1
	binary.BigEndian.PutUint16(buf[8:], 3) // refLen = 3
	buf[10] = 'a'                          // ref bytes
	buf[11] = 'b'
	buf[12] = 'c'
	// Only 1 byte left at offset 13, can't read uint16 checksumLen

	_, err := DecodeFetchObjectRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated checksum length")
	}
}

// TestDecodeFetchObjectRequest_TruncatedChecksumData provides a valid checksumLen
// but not enough remaining bytes for the checksum data.
func TestDecodeFetchObjectRequest_TruncatedChecksumData(t *testing.T) {
	// ShardID(8) + refLen(2, value=0) + checksumLen(2, value=10) + reqIDLen(2)
	// Total minimum = 14, but checksumLen=10 requires 10 more bytes after it
	buf := make([]byte, 14)
	binary.BigEndian.PutUint64(buf[0:], 1)   // ShardID = 1
	binary.BigEndian.PutUint16(buf[8:], 0)   // refLen = 0
	binary.BigEndian.PutUint16(buf[10:], 10) // checksumLen = 10 (but only 2 bytes left)
	binary.BigEndian.PutUint16(buf[12:], 0)  // reqIDLen = 0

	_, err := DecodeFetchObjectRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated checksum data")
	}
}

// TestDecodeFetchObjectRequest_TruncatedRequestIDLen provides valid ShardID,
// objectRef, and checksum but truncates before requestID length.
func TestDecodeFetchObjectRequest_TruncatedRequestIDLen(t *testing.T) {
	// Build: ShardID(8) + refLen(2, value=1) + ref(1) + checksumLen(2, value=0)
	// Total so far = 13. Need reqIDLen(2) but only have 1 byte allocated in 14-byte buf
	buf := make([]byte, 14)
	binary.BigEndian.PutUint64(buf[0:], 1)  // ShardID = 1
	binary.BigEndian.PutUint16(buf[8:], 1)  // refLen = 1
	buf[10] = 'x'                           // ref data
	binary.BigEndian.PutUint16(buf[11:], 0) // checksumLen = 0
	// Only 1 byte left at offset 13, can't read uint16 reqIDLen

	_, err := DecodeFetchObjectRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated request ID length")
	}
}

// TestDecodeFetchObjectRequest_TruncatedRequestIDData provides a valid requestID
// length but not enough data for the requestID string.
func TestDecodeFetchObjectRequest_TruncatedRequestIDData(t *testing.T) {
	// ShardID(8) + refLen(2, value=0) + checksumLen(2, value=0) + reqIDLen(2, value=100)
	// = 14 bytes total. reqIDLen=100 requires 100 more bytes but we only have 14.
	buf := make([]byte, 14)
	binary.BigEndian.PutUint64(buf[0:], 1)    // ShardID = 1
	binary.BigEndian.PutUint16(buf[8:], 0)    // refLen = 0
	binary.BigEndian.PutUint16(buf[10:], 0)   // checksumLen = 0
	binary.BigEndian.PutUint16(buf[12:], 100) // reqIDLen = 100 (but no data remaining)

	_, err := DecodeFetchObjectRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated request ID data")
	}
}

// =============================================================================
// EncodeStreamFetchObjectsRequest / DecodeStreamFetchObjectsRequest Tests
// =============================================================================

func TestEncodeStreamFetchObjectsRequest_Success(t *testing.T) {
	req := &StreamFetchObjectsRequest{
		ShardID: 10,
		Items: []ObjectFetchItem{
			{ObjectRef: "ref-1", Checksum: []byte{0xAB}},
			{ObjectRef: "ref-2"},
		},
	}

	data, err := EncodeStreamFetchObjectsRequest(req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodeStreamFetchObjectsRequest(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if decoded.ShardID != req.ShardID {
		t.Fatalf("ShardID mismatch: got %d, want %d", decoded.ShardID, req.ShardID)
	}
	if len(decoded.Items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(decoded.Items))
	}
	if decoded.Items[0].ObjectRef != "ref-1" {
		t.Fatalf("Item 0 ObjectRef mismatch: got %q", decoded.Items[0].ObjectRef)
	}
	if !bytes.Equal(decoded.Items[0].Checksum, []byte{0xAB}) {
		t.Fatal("Item 0 checksum mismatch")
	}
	if decoded.Items[1].ObjectRef != "ref-2" {
		t.Fatalf("Item 1 ObjectRef mismatch: got %q", decoded.Items[1].ObjectRef)
	}
}

func TestEncodeStreamFetchObjectsRequest_NilRequest(t *testing.T) {
	_, err := EncodeStreamFetchObjectsRequest(nil)
	if err == nil {
		t.Fatal("Expected error for nil request")
	}
}

func TestDecodeStreamFetchObjectsRequest_TooShort(t *testing.T) {
	_, err := DecodeStreamFetchObjectsRequest([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("Expected error for too-short data")
	}
}

func TestEncodeStreamFetchObjectsRequest_EmptyItems(t *testing.T) {
	req := &StreamFetchObjectsRequest{
		ShardID: 5,
		Items:   []ObjectFetchItem{},
	}

	data, err := EncodeStreamFetchObjectsRequest(req)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := DecodeStreamFetchObjectsRequest(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}
	if len(decoded.Items) != 0 {
		t.Fatalf("Expected 0 items, got %d", len(decoded.Items))
	}
}

// =============================================================================
// DecodeStreamFetchObjectsRequest - Truncated Binary Error Branches
// =============================================================================

// TestDecodeStreamFetchObjectsRequest_TruncatedItemRefLen provides itemCount > 0
// but truncates before the first item's objectRef length.
func TestDecodeStreamFetchObjectsRequest_TruncatedItemRefLen(t *testing.T) {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[0:], 1) // ShardID = 1
	binary.BigEndian.PutUint32(buf[8:], 1) // itemCount = 1 (but no item data follows)

	_, err := DecodeStreamFetchObjectsRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated item ref length")
	}
}

// TestDecodeStreamFetchObjectsRequest_TruncatedItemRefData provides a valid
// refLen but not enough bytes for the object ref string.
func TestDecodeStreamFetchObjectsRequest_TruncatedItemRefData(t *testing.T) {
	buf := make([]byte, 14)
	binary.BigEndian.PutUint64(buf[0:], 1)   // ShardID = 1
	binary.BigEndian.PutUint32(buf[8:], 1)   // itemCount = 1
	binary.BigEndian.PutUint16(buf[12:], 50) // refLen = 50 (but no ref data follows)

	_, err := DecodeStreamFetchObjectsRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated item ref data")
	}
}

// TestDecodeStreamFetchObjectsRequest_TruncatedItemChecksumLen provides valid
// item objectRef but truncates before the checksum length.
func TestDecodeStreamFetchObjectsRequest_TruncatedItemChecksumLen(t *testing.T) {
	buf := make([]byte, 15)
	binary.BigEndian.PutUint64(buf[0:], 1)  // ShardID = 1
	binary.BigEndian.PutUint32(buf[8:], 1)  // itemCount = 1
	binary.BigEndian.PutUint16(buf[12:], 1) // refLen = 1
	buf[14] = 'a'                           // ref data, but no checksumLen follows

	_, err := DecodeStreamFetchObjectsRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated item checksum length")
	}
}

// TestDecodeStreamFetchObjectsRequest_TruncatedItemChecksumData provides a valid
// checksumLen but not enough bytes for the checksum data.
func TestDecodeStreamFetchObjectsRequest_TruncatedItemChecksumData(t *testing.T) {
	buf := make([]byte, 17)
	binary.BigEndian.PutUint64(buf[0:], 1)   // ShardID = 1
	binary.BigEndian.PutUint32(buf[8:], 1)   // itemCount = 1
	binary.BigEndian.PutUint16(buf[12:], 1)  // refLen = 1
	buf[14] = 'a'                            // ref data
	binary.BigEndian.PutUint16(buf[15:], 50) // checksumLen = 50 (but no checksum data)

	_, err := DecodeStreamFetchObjectsRequest(buf)
	if err == nil {
		t.Fatal("expected error for truncated item checksum data")
	}
}

// =============================================================================
// handleFetchObject Tests
// =============================================================================

func TestHandleFetchObject_RegistryNil(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{ShardID: 1, ObjectRef: "ref-1"})
	resp := h.handleFetchObject(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when registry is nil")
	}
}

func TestHandleFetchObject_ShardNotFound(t *testing.T) {
	registry := NewObjectTransferRegistry()
	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{ShardID: 999, ObjectRef: "ref-1"})
	resp := h.handleFetchObject(ctx, &Request{Payload: payload})
	// Shard not found returns StatusOK with Found=false
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFetchObject_Success(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return []byte("hello-world"), nil
		},
	}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{ShardID: 1, ObjectRef: "ref-1"})
	resp := h.handleFetchObject(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFetchObject_EmptyObjectRef(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{ShardID: 1, ObjectRef: ""})
	resp := h.handleFetchObject(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for empty ObjectRef")
	}
}

func TestHandleFetchObject_ReadError(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return nil, &errors.MessageError{Message: "disk read failure"}
		},
	}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{ShardID: 1, ObjectRef: "ref-1"})
	resp := h.handleFetchObject(ctx, &Request{Payload: payload})
	// Read error returns StatusOK with Found=false and ErrorMessage
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFetchObject_ChecksumMismatch(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return []byte("data"), nil
		},
	}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	// Provide a checksum that does not match
	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "ref-1",
		Checksum:  []byte{0xFF, 0xFE, 0xFD}, // wrong checksum
	})
	resp := h.handleFetchObject(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFetchObject_ChecksumMatch(t *testing.T) {
	data := []byte("test-data")
	hash := sha256.Sum256(data)

	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return data, nil
		},
	}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeFetchObjectRequest(&FetchObjectRequest{
		ShardID:   1,
		ObjectRef: "ref-1",
		Checksum:  hash[:],
	})
	resp := h.handleFetchObject(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleFetchObject_DecodeError(t *testing.T) {
	registry := NewObjectTransferRegistry()
	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	resp := h.handleFetchObject(ctx, &Request{Payload: []byte{1, 2}})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for malformed payload")
	}
}

// =============================================================================
// handleStreamFetchObjects Tests
// =============================================================================

func TestHandleStreamFetchObjects_RegistryNil(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodeStreamFetchObjectsRequest(&StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "ref-1"}},
	})
	resp := h.handleStreamFetchObjects(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when registry is nil")
	}
}

func TestHandleStreamFetchObjects_ShardNotFound(t *testing.T) {
	registry := NewObjectTransferRegistry()
	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeStreamFetchObjectsRequest(&StreamFetchObjectsRequest{
		ShardID: 999,
		Items:   []ObjectFetchItem{{ObjectRef: "ref-1"}},
	})
	resp := h.handleStreamFetchObjects(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when shard not found")
	}
}

func TestHandleStreamFetchObjects_EmptyItems(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeStreamFetchObjectsRequest(&StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{},
	})
	resp := h.handleStreamFetchObjects(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for empty items")
	}
}

func TestHandleStreamFetchObjects_Success(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return []byte("data-" + objectRef), nil
		},
	}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	payload, _ := EncodeStreamFetchObjectsRequest(&StreamFetchObjectsRequest{
		ShardID: 1,
		Items: []ObjectFetchItem{
			{ObjectRef: "ref-1"},
			{ObjectRef: "ref-2"},
		},
	})
	resp := h.handleStreamFetchObjects(ctx, &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("Expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleStreamFetchObjects_CancelledContext(t *testing.T) {
	registry := NewObjectTransferRegistry()
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return []byte("data"), nil
		},
	}
	registry.RegisterStore(1, store)

	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	payload, _ := EncodeStreamFetchObjectsRequest(&StreamFetchObjectsRequest{
		ShardID: 1,
		Items:   []ObjectFetchItem{{ObjectRef: "ref-1"}},
	})
	resp := h.handleStreamFetchObjects(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for cancelled context")
	}
}

func TestHandleStreamFetchObjects_DecodeError(t *testing.T) {
	registry := NewObjectTransferRegistry()
	h := createTestHandler()
	h.objectTransferRegistry = registry
	ctx := context.Background()

	resp := h.handleStreamFetchObjects(ctx, &Request{Payload: []byte{1, 2}})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for malformed payload")
	}
}

// =============================================================================
// fetchSingleObject Tests
// =============================================================================

func TestFetchSingleObject_ReadError(t *testing.T) {
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return nil, &errors.MessageError{Message: "not found"}
		},
	}
	h := createTestHandler()

	resp := h.fetchSingleObject(store, &ObjectFetchItem{ObjectRef: "ref-1"})
	if resp.Found {
		t.Fatal("Expected Found=false for read error")
	}
	if resp.ErrorMessage == "" {
		t.Fatal("Expected non-empty error message")
	}
}

func TestFetchSingleObject_ChecksumMismatch(t *testing.T) {
	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return []byte("data"), nil
		},
	}
	h := createTestHandler()

	resp := h.fetchSingleObject(store, &ObjectFetchItem{
		ObjectRef: "ref-1",
		Checksum:  []byte{0xFF},
	})
	if !resp.Found {
		t.Fatal("Expected Found=true for checksum mismatch")
	}
	if resp.ErrorMessage == "" {
		t.Fatal("Expected checksum mismatch error message")
	}
}

func TestFetchSingleObject_Success(t *testing.T) {
	data := []byte("good-data")
	hash := sha256.Sum256(data)

	store := &mockObjectStoreReader{
		readObjectFn: func(objectRef string) ([]byte, error) {
			return data, nil
		},
	}
	h := createTestHandler()

	resp := h.fetchSingleObject(store, &ObjectFetchItem{
		ObjectRef: "ref-1",
		Checksum:  hash[:],
	})
	if !resp.Found {
		t.Fatal("Expected Found=true")
	}
	if resp.ErrorMessage != "" {
		t.Fatalf("Expected no error, got %q", resp.ErrorMessage)
	}
	if resp.Size != int64(len(data)) {
		t.Fatalf("Expected size %d, got %d", len(data), resp.Size)
	}
}
