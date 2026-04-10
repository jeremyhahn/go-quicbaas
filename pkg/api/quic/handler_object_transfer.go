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
	"fmt"
	"io"
	"sync"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// ObjectTransfer Service Types
// =============================================================================

// ObjectStoreReader provides read access to a local object store for a shard.
// This interface is implemented by ObjectStoreStateMachine or similar components.
type ObjectStoreReader interface {
	// ReadObject reads object data by its storage reference.
	ReadObject(objectRef string) ([]byte, error)
}

// ObjectTransferRegistry manages object store readers for shards.
type ObjectTransferRegistry struct {
	mu     sync.RWMutex
	stores map[uint64]ObjectStoreReader
}

// NewObjectTransferRegistry creates a new object transfer registry.
func NewObjectTransferRegistry() *ObjectTransferRegistry {
	return &ObjectTransferRegistry{
		stores: make(map[uint64]ObjectStoreReader),
	}
}

// RegisterStore registers an object store reader for a shard.
func (r *ObjectTransferRegistry) RegisterStore(shardID uint64, store ObjectStoreReader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[shardID] = store
}

// UnregisterStore removes an object store reader for a shard.
func (r *ObjectTransferRegistry) UnregisterStore(shardID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.stores, shardID)
}

// GetStore retrieves an object store reader for a shard.
func (r *ObjectTransferRegistry) GetStore(shardID uint64) (ObjectStoreReader, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	store, ok := r.stores[shardID]
	return store, ok
}

// =============================================================================
// ObjectTransfer Request/Response Types (Binary Encoding)
// =============================================================================

// FetchObjectRequest represents a request to fetch a single object from a peer.
type FetchObjectRequest struct {
	ShardID   uint64 `json:"shard_id"`
	ObjectRef string `json:"object_ref"`
	Checksum  []byte `json:"checksum,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// FetchObjectResponse represents the response containing object data.
type FetchObjectResponse struct {
	ObjectRef       string `json:"object_ref"`
	Data            []byte `json:"data,omitempty"`
	Checksum        []byte `json:"checksum,omitempty"`
	Size            int64  `json:"size"`
	Compressed      bool   `json:"compressed"`
	CompressionAlgo string `json:"compression_algo,omitempty"`
	Found           bool   `json:"found"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

// ObjectFetchItem represents a single object to fetch in a stream request.
type ObjectFetchItem struct {
	ObjectRef string `json:"object_ref"`
	Checksum  []byte `json:"checksum,omitempty"`
}

// StreamFetchObjectsRequest represents a request to fetch multiple objects.
type StreamFetchObjectsRequest struct {
	ShardID uint64            `json:"shard_id"`
	Items   []ObjectFetchItem `json:"items"`
}

// StreamFetchObjectsResponse aggregates multiple fetch responses.
type StreamFetchObjectsResponse struct {
	Objects []FetchObjectResponse `json:"objects"`
}

// =============================================================================
// Binary Encoding/Decoding for ObjectTransfer
// =============================================================================

// EncodeFetchObjectRequest encodes a FetchObjectRequest to binary format.
// Wire format: [shardID:8][refLen:2][ref:N][checksumLen:2][checksum:M][reqIDLen:2][reqID:P]
func EncodeFetchObjectRequest(req *FetchObjectRequest) ([]byte, error) {
	if req == nil {
		return nil, api.NewNilInputError("quic.EncodeFetchObjectRequest", "request")
	}

	refBytes := []byte(req.ObjectRef)
	reqIDBytes := []byte(req.RequestID)

	// Calculate total size
	size := 8 + // shardID
		2 + len(refBytes) + // refLen + ref
		2 + len(req.Checksum) + // checksumLen + checksum
		2 + len(reqIDBytes) // reqIDLen + reqID

	buf := make([]byte, size)
	offset := 0

	// ShardID (8 bytes, big-endian)
	binary.BigEndian.PutUint64(buf[offset:], req.ShardID)
	offset += 8

	// ObjectRef length and data
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(refBytes))) //nolint:gosec
	offset += 2
	copy(buf[offset:], refBytes)
	offset += len(refBytes)

	// Checksum length and data
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(req.Checksum))) //nolint:gosec
	offset += 2
	copy(buf[offset:], req.Checksum)
	offset += len(req.Checksum)

	// RequestID length and data
	binary.BigEndian.PutUint16(buf[offset:], uint16(len(reqIDBytes))) //nolint:gosec
	offset += 2
	copy(buf[offset:], reqIDBytes)

	return buf, nil
}

// DecodeFetchObjectRequest decodes binary data into a FetchObjectRequest.
func DecodeFetchObjectRequest(data []byte) (*FetchObjectRequest, error) {
	if len(data) < 14 { // minimum: 8 + 2 + 2 + 2
		return nil, api.NewDataTooShortError("quic.DecodeFetchObjectRequest", len(data))
	}

	r := bytes.NewReader(data)
	req := &FetchObjectRequest{}

	// Read ShardID
	if err := binary.Read(r, binary.BigEndian, &req.ShardID); err != nil {
		return nil, api.NewReadError("quic.DecodeFetchObjectRequest", "shard_id", err)
	}

	// Read ObjectRef
	var refLen uint16
	if err := binary.Read(r, binary.BigEndian, &refLen); err != nil {
		return nil, api.NewReadError("quic.DecodeFetchObjectRequest", "object_ref length", err)
	}
	if refLen > 0 {
		refBytes := make([]byte, refLen)
		if _, err := io.ReadFull(r, refBytes); err != nil {
			return nil, api.NewReadError("quic.DecodeFetchObjectRequest", "object_ref", err)
		}
		req.ObjectRef = string(refBytes)
	}

	// Read Checksum
	var checksumLen uint16
	if err := binary.Read(r, binary.BigEndian, &checksumLen); err != nil {
		return nil, api.NewReadError("quic.DecodeFetchObjectRequest", "checksum length", err)
	}
	if checksumLen > 0 {
		req.Checksum = make([]byte, checksumLen)
		if _, err := io.ReadFull(r, req.Checksum); err != nil {
			return nil, api.NewReadError("quic.DecodeFetchObjectRequest", "checksum", err)
		}
	}

	// Read RequestID
	var reqIDLen uint16
	if err := binary.Read(r, binary.BigEndian, &reqIDLen); err != nil {
		return nil, api.NewReadError("quic.DecodeFetchObjectRequest", "request_id length", err)
	}
	if reqIDLen > 0 {
		reqIDBytes := make([]byte, reqIDLen)
		if _, err := io.ReadFull(r, reqIDBytes); err != nil {
			return nil, api.NewReadError("quic.DecodeFetchObjectRequest", "request_id", err)
		}
		req.RequestID = string(reqIDBytes)
	}

	return req, nil
}

// EncodeStreamFetchObjectsRequest encodes a StreamFetchObjectsRequest to binary format.
// Wire format: [shardID:8][itemCount:4][items...]
// Each item: [refLen:2][ref:N][checksumLen:2][checksum:M]
func EncodeStreamFetchObjectsRequest(req *StreamFetchObjectsRequest) ([]byte, error) {
	if req == nil {
		return nil, api.NewNilInputError("quic.EncodeStreamFetchObjectsRequest", "request")
	}

	// Calculate total size
	size := 8 + 4 // shardID + itemCount
	for _, item := range req.Items {
		size += 2 + len(item.ObjectRef) + 2 + len(item.Checksum)
	}

	buf := make([]byte, size)
	offset := 0

	// ShardID (8 bytes)
	binary.BigEndian.PutUint64(buf[offset:], req.ShardID)
	offset += 8

	// Item count (4 bytes)
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(req.Items))) //nolint:gosec
	offset += 4

	// Encode each item
	for _, item := range req.Items {
		refBytes := []byte(item.ObjectRef)

		// ObjectRef length and data
		binary.BigEndian.PutUint16(buf[offset:], uint16(len(refBytes))) //nolint:gosec
		offset += 2
		copy(buf[offset:], refBytes)
		offset += len(refBytes)

		// Checksum length and data
		binary.BigEndian.PutUint16(buf[offset:], uint16(len(item.Checksum))) //nolint:gosec
		offset += 2
		copy(buf[offset:], item.Checksum)
		offset += len(item.Checksum)
	}

	return buf, nil
}

// DecodeStreamFetchObjectsRequest decodes binary data into a StreamFetchObjectsRequest.
func DecodeStreamFetchObjectsRequest(data []byte) (*StreamFetchObjectsRequest, error) {
	if len(data) < 12 { // minimum: 8 + 4
		return nil, api.NewDataTooShortError("quic.DecodeStreamFetchObjectsRequest", len(data))
	}

	r := bytes.NewReader(data)
	req := &StreamFetchObjectsRequest{}

	// Read ShardID
	if err := binary.Read(r, binary.BigEndian, &req.ShardID); err != nil {
		return nil, api.NewReadError("quic.DecodeStreamFetchObjectsRequest", "shard_id", err)
	}

	// Read item count
	var itemCount uint32
	if err := binary.Read(r, binary.BigEndian, &itemCount); err != nil {
		return nil, api.NewReadError("quic.DecodeStreamFetchObjectsRequest", "item count", err)
	}

	// Read each item
	req.Items = make([]ObjectFetchItem, 0, itemCount)
	for i := uint32(0); i < itemCount; i++ {
		item := ObjectFetchItem{}

		// Read ObjectRef
		var refLen uint16
		if err := binary.Read(r, binary.BigEndian, &refLen); err != nil {
			return nil, api.NewItemReadError("quic.DecodeStreamFetchObjectsRequest", "object_ref length", i, err)
		}
		if refLen > 0 {
			refBytes := make([]byte, refLen)
			if _, err := io.ReadFull(r, refBytes); err != nil {
				return nil, api.NewItemReadError("quic.DecodeStreamFetchObjectsRequest", "object_ref", i, err)
			}
			item.ObjectRef = string(refBytes)
		}

		// Read Checksum
		var checksumLen uint16
		if err := binary.Read(r, binary.BigEndian, &checksumLen); err != nil {
			return nil, api.NewItemReadError("quic.DecodeStreamFetchObjectsRequest", "checksum length", i, err)
		}
		if checksumLen > 0 {
			item.Checksum = make([]byte, checksumLen)
			if _, err := io.ReadFull(r, item.Checksum); err != nil {
				return nil, api.NewItemReadError("quic.DecodeStreamFetchObjectsRequest", "checksum", i, err)
			}
		}

		req.Items = append(req.Items, item)
	}

	return req, nil
}

// =============================================================================
// ObjectTransfer Handler Functions
// =============================================================================

// handleFetchObject handles single object fetch operations.
func (h *Handler) handleFetchObject(ctx context.Context, req *Request) *Response {
	registry := h.objectTransferRegistry
	if registry == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectTransfer"))
	}

	// Decode request using binary format
	fetchReq, err := DecodeFetchObjectRequest(req.Payload)
	if err != nil {
		return h.errorResponse(err)
	}

	// Validate required fields
	if fetchReq.ObjectRef == "" {
		return h.errorResponse(api.NewRequiredFieldError("object_ref"))
	}

	// Get object store for the shard
	store, ok := registry.GetStore(fetchReq.ShardID)
	if !ok {
		// Shard not found - return not found response (not an error)
		resp := &FetchObjectResponse{
			ObjectRef:    fetchReq.ObjectRef,
			Found:        false,
			ErrorMessage: fmt.Sprintf("shard %d not found on this node", fetchReq.ShardID),
		}
		payload, encErr := EncodePayload(resp)
		if encErr != nil {
			return h.errorResponse(encErr)
		}
		return &Response{Status: StatusOK, Payload: payload}
	}

	// Read object from local store
	data, readErr := store.ReadObject(fetchReq.ObjectRef)
	if readErr != nil {
		// Object not found or read error
		resp := &FetchObjectResponse{
			ObjectRef:    fetchReq.ObjectRef,
			Found:        false,
			ErrorMessage: readErr.Error(),
		}
		payload, encErr := EncodePayload(resp)
		if encErr != nil {
			return h.errorResponse(encErr)
		}
		return &Response{Status: StatusOK, Payload: payload}
	}

	// Compute checksum
	hash := sha256.Sum256(data)
	checksum := hash[:]

	// Verify checksum if provided in request
	if len(fetchReq.Checksum) > 0 && !bytes.Equal(fetchReq.Checksum, checksum) {
		resp := &FetchObjectResponse{
			ObjectRef:    fetchReq.ObjectRef,
			Found:        true,
			ErrorMessage: "checksum mismatch",
		}
		payload, encErr := EncodePayload(resp)
		if encErr != nil {
			return h.errorResponse(encErr)
		}
		return &Response{Status: StatusOK, Payload: payload}
	}

	// Build success response
	resp := &FetchObjectResponse{
		ObjectRef: fetchReq.ObjectRef,
		Data:      data,
		Checksum:  checksum,
		Size:      int64(len(data)),
		Found:     true,
	}

	payload, encErr := EncodePayload(resp)
	if encErr != nil {
		return h.errorResponse(encErr)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleStreamFetchObjects handles batch object fetch operations.
func (h *Handler) handleStreamFetchObjects(ctx context.Context, req *Request) *Response {
	registry := h.objectTransferRegistry
	if registry == nil {
		return h.errorResponse(api.NewServiceUnavailableError("ObjectTransfer"))
	}

	// Decode request using binary format
	streamReq, err := DecodeStreamFetchObjectsRequest(req.Payload)
	if err != nil {
		return h.errorResponse(err)
	}

	// Validate request
	if len(streamReq.Items) == 0 {
		return h.errorResponse(api.NewEmptyListError(
			"quic.handleStreamFetchObjects", "items list"))
	}

	// Get object store for the shard
	store, ok := registry.GetStore(streamReq.ShardID)
	if !ok {
		return h.errorResponse(api.NewShardNotFoundError(
			"quic.handleStreamFetchObjects", streamReq.ShardID))
	}

	// Fetch all objects
	responses := make([]FetchObjectResponse, 0, len(streamReq.Items))
	for _, item := range streamReq.Items {
		// Check context for cancellation
		select {
		case <-ctx.Done():
			return h.errorResponse(&errors.DragonError{
				Code: errors.ErrTimeout,
				Op:   "quic.handleStreamFetchObjects",
				Err:  ctx.Err(),
			})
		default:
		}

		resp := h.fetchSingleObject(store, &item)
		responses = append(responses, resp)
	}

	// Build aggregate response
	streamResp := &StreamFetchObjectsResponse{
		Objects: responses,
	}

	payload, encErr := EncodePayload(streamResp)
	if encErr != nil {
		return h.errorResponse(encErr)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// fetchSingleObject fetches a single object and builds the response.
func (h *Handler) fetchSingleObject(store ObjectStoreReader, item *ObjectFetchItem) FetchObjectResponse {
	data, err := store.ReadObject(item.ObjectRef)
	if err != nil {
		return FetchObjectResponse{
			ObjectRef:    item.ObjectRef,
			Found:        false,
			ErrorMessage: err.Error(),
		}
	}

	// Compute checksum
	hash := sha256.Sum256(data)
	checksum := hash[:]

	// Verify checksum if provided
	if len(item.Checksum) > 0 && !bytes.Equal(item.Checksum, checksum) {
		return FetchObjectResponse{
			ObjectRef:    item.ObjectRef,
			Found:        true,
			ErrorMessage: "checksum mismatch",
		}
	}

	return FetchObjectResponse{
		ObjectRef: item.ObjectRef,
		Data:      data,
		Checksum:  checksum,
		Size:      int64(len(data)),
		Found:     true,
	}
}
