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

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
)

// =============================================================================
// Object Transfer REST Types
// =============================================================================

// FetchObjectRESTRequest is the JSON request for fetching a single object.
type FetchObjectRESTRequest struct {
	ShardID   uint64 `json:"shard_id"`
	ObjectRef string `json:"object_ref"`
	Checksum  string `json:"checksum,omitempty"` // Base64 encoded SHA-256
	RequestID string `json:"request_id,omitempty"`
}

// FetchObjectRESTResponse is the JSON response for a fetched object.
type FetchObjectRESTResponse struct {
	ObjectRef       string `json:"object_ref"`
	Data            string `json:"data,omitempty"`     // Base64 encoded bytes
	Checksum        string `json:"checksum,omitempty"` // Base64 encoded SHA-256
	Size            int64  `json:"size"`
	Compressed      bool   `json:"compressed"`
	CompressionAlgo string `json:"compression_algo,omitempty"`
	Found           bool   `json:"found"`
	ErrorMessage    string `json:"error_message,omitempty"`
}

// ObjectFetchItemREST represents a single object to fetch in a batch request.
type ObjectFetchItemREST struct {
	ObjectRef string `json:"object_ref"`
	Checksum  string `json:"checksum,omitempty"` // Base64 encoded SHA-256
}

// StreamFetchObjectsRESTRequest is the JSON request for fetching multiple objects.
type StreamFetchObjectsRESTRequest struct {
	ShardID uint64                `json:"shard_id"`
	Items   []ObjectFetchItemREST `json:"items"`
}

// =============================================================================
// Object Store Reader Interface
// =============================================================================

// ObjectStoreReader provides read access to the local object store.
// This interface mirrors the gRPC ObjectStoreReader for consistency.
type ObjectStoreReader interface {
	// ReadObject reads object data by its storage reference.
	ReadObject(objectRef string) ([]byte, error)
}

// objectTransferRegistry manages object store readers for the REST API.
type objectTransferRegistry struct {
	mu     sync.RWMutex
	stores map[uint64]ObjectStoreReader // shardID -> object store
}

var transferRegistry = &objectTransferRegistry{
	stores: make(map[uint64]ObjectStoreReader),
}

// RegisterObjectStore registers an object store for a shard in the REST API.
func RegisterObjectStore(shardID uint64, store ObjectStoreReader) {
	transferRegistry.mu.Lock()
	defer transferRegistry.mu.Unlock()
	transferRegistry.stores[shardID] = store
}

// UnregisterObjectStore removes an object store registration from the REST API.
func UnregisterObjectStore(shardID uint64) {
	transferRegistry.mu.Lock()
	defer transferRegistry.mu.Unlock()
	delete(transferRegistry.stores, shardID)
}

// getObjectStore retrieves an object store by shard ID.
func getObjectStore(shardID uint64) (ObjectStoreReader, bool) {
	transferRegistry.mu.RLock()
	defer transferRegistry.mu.RUnlock()
	store, ok := transferRegistry.stores[shardID]
	return store, ok
}

// =============================================================================
// Object Transfer Handlers
// =============================================================================

// HandleFetchObject fetches a single object from the local object store.
// POST /api/v1/object-transfer/fetch.
func (s *Server) HandleFetchObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = ctx // Available for future use with context deadline/cancellation

	var req FetchObjectRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	// Validate request
	if req.ObjectRef == "" {
		RespondBadRequest(w, r, "object_ref is required")
		return
	}

	// Get object store for the shard
	store, ok := getObjectStore(req.ShardID)
	if !ok {
		resp := FetchObjectRESTResponse{
			ObjectRef:    req.ObjectRef,
			Found:        false,
			ErrorMessage: "shard not found on this node",
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	// Read object from local store
	data, err := store.ReadObject(req.ObjectRef)
	if err != nil {
		s.logger.WithFields(map[string]interface{}{
			"shard_id":   req.ShardID,
			"object_ref": req.ObjectRef,
			"error":      err.Error(),
		}).Debug("object not found in local store")

		resp := FetchObjectRESTResponse{
			ObjectRef:    req.ObjectRef,
			Found:        false,
			ErrorMessage: err.Error(),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	// Compute checksum
	hash := sha256.Sum256(data)
	checksum := hash[:]

	// Verify checksum if provided in request
	if req.Checksum != "" {
		expectedChecksum, decodeErr := base64.StdEncoding.DecodeString(req.Checksum)
		if decodeErr != nil {
			RespondBadRequest(w, r, "invalid checksum encoding: "+decodeErr.Error())
			return
		}
		if !bytesEqual(expectedChecksum, checksum) {
			resp := FetchObjectRESTResponse{
				ObjectRef:    req.ObjectRef,
				Found:        true,
				ErrorMessage: "checksum mismatch",
			}
			RespondJSON(w, r, http.StatusOK, resp)
			return
		}
	}

	s.logger.WithFields(map[string]interface{}{
		"shard_id":   req.ShardID,
		"object_ref": req.ObjectRef,
		"size":       len(data),
	}).Debug("object fetched successfully")

	resp := FetchObjectRESTResponse{
		ObjectRef: req.ObjectRef,
		Data:      base64.StdEncoding.EncodeToString(data),
		Checksum:  base64.StdEncoding.EncodeToString(checksum),
		Size:      int64(len(data)),
		Found:     true,
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleStreamFetchObjects fetches multiple objects and streams them as NDJSON.
// POST /api/v1/object-transfer/stream-fetch.
func (s *Server) HandleStreamFetchObjects(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req StreamFetchObjectsRESTRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}
	defer r.Body.Close()

	// Validate request
	if len(req.Items) == 0 {
		RespondBadRequest(w, r, "items array cannot be empty")
		return
	}

	// Get object store for the shard
	store, ok := getObjectStore(req.ShardID)
	if !ok {
		RespondError(w, r, http.StatusNotFound, &ShardNotFoundError{ShardID: req.ShardID})
		return
	}

	// Check if streaming is supported
	flusher, ok := w.(http.Flusher)
	if !ok {
		RespondInternalError(w, r, &StreamingNotSupportedError{})
		return
	}

	// Set NDJSON headers for streaming response
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)

	for _, item := range req.Items {
		// Check context for cancellation
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp := s.fetchSingleObjectREST(store, item)
		if err := encoder.Encode(resp); err != nil {
			s.logger.WithFields(map[string]interface{}{
				"object_ref": item.ObjectRef,
				"error":      err.Error(),
			}).Debug("failed to write response")
			return
		}
		flusher.Flush()
	}
}

// fetchSingleObjectREST fetches a single object and builds the REST response.
func (s *Server) fetchSingleObjectREST(store ObjectStoreReader, item ObjectFetchItemREST) FetchObjectRESTResponse {
	data, err := store.ReadObject(item.ObjectRef)
	if err != nil {
		return FetchObjectRESTResponse{
			ObjectRef:    item.ObjectRef,
			Found:        false,
			ErrorMessage: err.Error(),
		}
	}

	// Compute checksum
	hash := sha256.Sum256(data)
	checksum := hash[:]

	// Verify checksum if provided
	if item.Checksum != "" {
		expectedChecksum, decodeErr := base64.StdEncoding.DecodeString(item.Checksum)
		if decodeErr == nil && !bytesEqual(expectedChecksum, checksum) {
			return FetchObjectRESTResponse{
				ObjectRef:    item.ObjectRef,
				Found:        true,
				ErrorMessage: "checksum mismatch",
			}
		}
	}

	return FetchObjectRESTResponse{
		ObjectRef: item.ObjectRef,
		Data:      base64.StdEncoding.EncodeToString(data),
		Checksum:  base64.StdEncoding.EncodeToString(checksum),
		Size:      int64(len(data)),
		Found:     true,
	}
}

// =============================================================================
// Error Types
// =============================================================================

// ShardNotFoundError indicates the requested shard is not on this node.
type ShardNotFoundError struct {
	ShardID uint64
}

func (e *ShardNotFoundError) Error() string {
	return "shard not found on this node"
}

// StreamingNotSupportedError indicates streaming is not supported.
type StreamingNotSupportedError struct{}

func (e *StreamingNotSupportedError) Error() string {
	return "streaming not supported"
}

// =============================================================================
// Helper Functions
// =============================================================================

// bytesEqual compares two byte slices for equality.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// =============================================================================
// Route Registration
// =============================================================================

// registerObjectTransferRoutes registers object transfer routes.
func registerObjectTransferRoutes(s *Server, r chi.Router) {
	r.Route("/object-transfer", func(r chi.Router) {
		r.Post("/fetch", s.HandleFetchObject)
		r.Post("/stream-fetch", s.HandleStreamFetchObjects)
	})
}
