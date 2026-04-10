//go:build unix

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

package objectstorage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"sync"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ObjectStoreReader provides read access to the local object store.
// This interface is implemented by the ObjectStoreStateMachine.
type ObjectStoreReader interface {
	// ReadObject reads object data by its storage reference.
	ReadObject(objectRef string) ([]byte, error)
}

// objectStoreRegistry holds registered object stores by shard ID.
type objectStoreRegistry struct {
	mu     sync.RWMutex
	stores map[uint64]ObjectStoreReader
}

// newObjectStoreRegistry creates a new object store registry.
func newObjectStoreRegistry() *objectStoreRegistry {
	return &objectStoreRegistry{
		stores: make(map[uint64]ObjectStoreReader),
	}
}

// register adds an object store for a shard.
func (r *objectStoreRegistry) register(shardID uint64, store ObjectStoreReader) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stores[shardID] = store
}

// unregister removes an object store registration.
func (r *objectStoreRegistry) unregister(shardID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.stores, shardID)
}

// get retrieves an object store for a shard.
func (r *objectStoreRegistry) get(shardID uint64) (ObjectStoreReader, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	store, ok := r.stores[shardID]
	return store, ok
}

// RegisterObjectStore registers an object store for a shard.
// This allows the handler to serve FetchObject and StreamFetchObjects requests.
func (h *Handler) RegisterObjectStore(shardID uint64, store ObjectStoreReader) {
	h.objectStores.register(shardID, store)
	h.logger.WithField("shard_id", shardID).Debug("registered object store for transfer")
}

// UnregisterObjectStore removes an object store registration.
func (h *Handler) UnregisterObjectStore(shardID uint64) {
	h.objectStores.unregister(shardID)
	h.logger.WithField("shard_id", shardID).Debug("unregistered object store for transfer")
}

// checkObjectStoreService verifies that object store functionality is available.
func (h *Handler) checkObjectStoreService(shardID uint64) (ObjectStoreReader, error) {
	store, ok := h.objectStores.get(shardID)
	if !ok {
		return nil, api.NewShardNotFoundError(
			"unix.objectstorage.checkObjectStoreService", shardID)
	}
	return store, nil
}

// handleFetchObject handles the FetchObject operation.
// It retrieves object data from the local object store for a given shard.
func (h *Handler) handleFetchObject(ctx context.Context, reqData []byte) ([]byte, error) {
	var req FetchObjectRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	if req.ObjectRef == "" {
		return nil, api.NewRequiredFieldError("object_ref")
	}

	// Get object store for the shard
	store, err := h.checkObjectStoreService(req.ShardID)
	if err != nil {
		resp := &FetchObjectResponse{
			ObjectRef:    req.ObjectRef,
			Found:        false,
			ErrorMessage: err.Error(),
		}
		return EncodePayload(resp)
	}

	// Read object from local store
	data, err := store.ReadObject(req.ObjectRef)
	if err != nil {
		h.logger.WithFields(map[string]interface{}{
			"shard_id":   req.ShardID,
			"object_ref": req.ObjectRef,
			"error":      err.Error(),
		}).Debug("object not found in local store")

		resp := &FetchObjectResponse{
			ObjectRef:    req.ObjectRef,
			Found:        false,
			ErrorMessage: err.Error(),
		}
		return EncodePayload(resp)
	}

	// Compute checksum
	hash := sha256.Sum256(data)
	checksum := hash[:]

	// Verify checksum if provided in request
	if len(req.Checksum) > 0 && !bytes.Equal(req.Checksum, checksum) {
		resp := &FetchObjectResponse{
			ObjectRef:    req.ObjectRef,
			Found:        true,
			ErrorMessage: "checksum mismatch",
		}
		return EncodePayload(resp)
	}

	h.logger.WithFields(map[string]interface{}{
		"shard_id":   req.ShardID,
		"object_ref": req.ObjectRef,
		"size":       len(data),
	}).Debug("object fetched successfully")

	resp := &FetchObjectResponse{
		ObjectRef: req.ObjectRef,
		Data:      data,
		Checksum:  checksum,
		Size:      int64(len(data)),
		Found:     true,
	}
	return EncodePayload(resp)
}

// handleStreamFetchObjects handles the StreamFetchObjects operation.
// It retrieves multiple objects in a single request for bulk recovery.
func (h *Handler) handleStreamFetchObjects(ctx context.Context, reqData []byte) ([]byte, error) {
	var req StreamFetchObjectsRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	if len(req.Items) == 0 {
		return nil, api.NewRequiredFieldError("items list")
	}

	// Get object store for the shard
	store, err := h.checkObjectStoreService(req.ShardID)
	if err != nil {
		return nil, err
	}

	results := make([]*FetchObjectResponse, 0, len(req.Items))
	succeeded := 0
	failed := 0

	for _, item := range req.Items {
		// Check context for cancellation
		select {
		case <-ctx.Done():
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrTimeout,
				Op:   "unix.objectstorage.handleStreamFetchObjects",
				Err:  ctx.Err(),
			}
		default:
		}

		result := h.fetchSingleObject(store, item)
		results = append(results, result)

		if result.Found && result.ErrorMessage == "" {
			succeeded++
		} else {
			failed++
		}
	}

	resp := &StreamFetchObjectsResponse{
		Results:   results,
		Succeeded: succeeded,
		Failed:    failed,
	}
	return EncodePayload(resp)
}

// fetchSingleObject fetches a single object and builds the response.
func (h *Handler) fetchSingleObject(store ObjectStoreReader, item *ObjectFetchItem) *FetchObjectResponse {
	if item == nil || item.ObjectRef == "" {
		return &FetchObjectResponse{
			Found:        false,
			ErrorMessage: "invalid fetch item",
		}
	}

	data, err := store.ReadObject(item.ObjectRef)
	if err != nil {
		return &FetchObjectResponse{
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
		return &FetchObjectResponse{
			ObjectRef:    item.ObjectRef,
			Found:        true,
			ErrorMessage: "checksum mismatch",
		}
	}

	return &FetchObjectResponse{
		ObjectRef: item.ObjectRef,
		Data:      data,
		Checksum:  checksum,
		Size:      int64(len(data)),
		Found:     true,
	}
}

// IsObjectTransferOperation returns true if the opcode is an object transfer operation.
func IsObjectTransferOperation(opcode byte) bool {
	return opcode == OpFetchObject || opcode == OpStreamFetchObjects
}
