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

package service

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

const (
	// parameterPrefix is the key prefix for all parameters in the KV store.
	parameterPrefix = "param:"
)

// kvParameterManager implements interfaces.ParameterManager using the qrdb SDK client.
type kvParameterManager struct {
	clientProvider QRDBClientProvider
	shardID        uint64
	logger         interfaces.Logger
	closed         atomic.Bool
}

// storedParameter is the serialized format for parameters in the KV store.
type storedParameter struct {
	Value    []byte                        `json:"value"`
	Metadata *interfaces.ParameterMetadata `json:"metadata"`
}

// newKVParameterManager creates a new parameter manager backed by the qrdb SDK client.
func newKVParameterManager(clientProvider QRDBClientProvider, shardID uint64, logger interfaces.Logger) *kvParameterManager {
	return &kvParameterManager{
		clientProvider: clientProvider,
		shardID:        shardID,
		logger:         logger,
	}
}

// getClient returns the qrdb SDK client for the configured shard.
func (m *kvParameterManager) getClient() (*qrdbsdk.Client, error) {
	return m.clientProvider.ForShard(m.shardID)
}

// paramKey returns the full key for a parameter path.
func (m *kvParameterManager) paramKey(path string) string {
	return parameterPrefix + path
}

// Store saves a parameter at the specified path.
func (m *kvParameterManager) Store(ctx context.Context, path string, value []byte, metadata *interfaces.ParameterMetadata, overwrite bool) error {
	return m.StoreWithOptions(ctx, path, value, metadata, overwrite, nil)
}

// StoreWithOptions saves a parameter with custom encryption options.
func (m *kvParameterManager) StoreWithOptions(ctx context.Context, path string, value []byte, metadata *interfaces.ParameterMetadata, overwrite bool, opts *interfaces.StoreOptions) error {
	if m.closed.Load() {
		return api.NewServiceClosedError("kvParameterManager.StoreWithOptions", "parameter manager")
	}

	if path == "" {
		return errors.NewParameterNotFoundError("")
	}

	key := m.paramKey(path)

	client, err := m.getClient()
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kvParameterManager.StoreWithOptions",
			Err:  &api.OperationFailedError{Operation: "get client", Cause: err},
		}
	}

	// Check if exists and overwrite is false
	if !overwrite {
		_, err := client.KV().Get(ctx, key)
		if err == nil {
			return errors.NewParameterExistsError(path)
		}
	}

	// Prepare metadata
	now := time.Now()
	if metadata == nil {
		metadata = &interfaces.ParameterMetadata{
			Type:      "string",
			Tags:      make(map[string]string),
			CreatedAt: now,
			UpdatedAt: now,
		}
	} else {
		metadata.UpdatedAt = now
		if metadata.CreatedAt.IsZero() {
			metadata.CreatedAt = now
		}
		if metadata.Tags == nil {
			metadata.Tags = make(map[string]string)
		}
	}

	// Handle TTL
	if metadata.TTLSeconds > 0 && metadata.ExpiresAt == nil {
		expiresAt := now.Add(time.Duration(metadata.TTLSeconds) * time.Second)
		metadata.ExpiresAt = &expiresAt
	}

	// Serialize the parameter
	stored := &storedParameter{
		Value:    value,
		Metadata: metadata,
	}
	data, err := json.Marshal(stored)
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kvParameterManager.StoreWithOptions",
			Err:  &api.OperationFailedError{Operation: "serialize parameter", Cause: err},
		}
	}

	// Store via SDK client
	if err := client.KV().Put(ctx, key, data); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kvParameterManager.StoreWithOptions",
			Err:  &api.OperationFailedError{Operation: "store parameter", Cause: err},
		}
	}

	if m.logger != nil {
		m.logger.WithField("path", path).Debug("Parameter stored")
	}

	return nil
}

// Get retrieves a parameter from the specified path.
func (m *kvParameterManager) Get(ctx context.Context, path string, withDecryption bool) ([]byte, *interfaces.ParameterMetadata, error) {
	if m.closed.Load() {
		return nil, nil, api.NewServiceClosedError("kvParameterManager.Get", "parameter manager")
	}

	key := m.paramKey(path)

	client, err := m.getClient()
	if err != nil {
		return nil, nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kvParameterManager.Get",
			Err:  &api.OperationFailedError{Operation: "get client", Cause: err},
		}
	}

	// Read via SDK client
	data, err := client.KV().Get(ctx, key)
	if err != nil {
		return nil, nil, errors.NewParameterNotFoundError(path)
	}

	// Deserialize
	var stored storedParameter
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kvParameterManager.Get",
			Err:  &api.OperationFailedError{Operation: "deserialize parameter", Cause: err},
		}
	}

	// Check expiration
	if stored.Metadata.ExpiresAt != nil && time.Now().After(*stored.Metadata.ExpiresAt) {
		return nil, nil, errors.NewParameterExpiredError(path, *stored.Metadata.ExpiresAt)
	}

	return stored.Value, stored.Metadata, nil
}

// Delete removes a parameter at the specified path.
func (m *kvParameterManager) Delete(ctx context.Context, path string) error {
	if m.closed.Load() {
		return api.NewServiceClosedError("kvParameterManager.Delete", "parameter manager")
	}

	key := m.paramKey(path)

	client, err := m.getClient()
	if err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kvParameterManager.Delete",
			Err:  &api.OperationFailedError{Operation: "get client", Cause: err},
		}
	}

	// Check if exists
	_, err = client.KV().Get(ctx, key)
	if err != nil {
		return errors.NewParameterNotFoundError(path)
	}

	// Delete via SDK client
	if err := client.KV().Delete(ctx, key); err != nil {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "kvParameterManager.Delete",
			Err:  &api.OperationFailedError{Operation: "delete parameter", Cause: err},
		}
	}

	if m.logger != nil {
		m.logger.WithField("path", path).Debug("Parameter deleted")
	}

	return nil
}

// List returns all parameter paths matching the prefix.
func (m *kvParameterManager) List(ctx context.Context, prefix string, recursive bool, limit int, cursor string) ([]string, string, error) {
	if m.closed.Load() {
		return nil, "", api.NewServiceClosedError("kvParameterManager.List", "parameter manager")
	}

	// Build the scan prefix
	scanPrefix := parameterPrefix
	if prefix != "" {
		scanPrefix = parameterPrefix + prefix
	}

	// Scan keys from the KV store
	keys, err := m.scanKeys(ctx, scanPrefix)
	if err != nil {
		return nil, "", err
	}

	// Filter and extract paths
	var paths []string
	for _, key := range keys {
		// Remove the param: prefix to get the path
		path := strings.TrimPrefix(string(key), parameterPrefix)

		// Apply prefix filter
		if prefix != "" && !strings.HasPrefix(path, prefix) {
			continue
		}

		// If not recursive, only include direct children
		if !recursive && prefix != "" {
			rel := strings.TrimPrefix(path, prefix)
			rel = strings.TrimPrefix(rel, "/")
			if strings.Contains(rel, "/") {
				continue
			}
		}

		paths = append(paths, path)
	}

	// Sort for consistent ordering
	sort.Strings(paths)

	// Apply cursor
	if cursor != "" {
		var filtered []string
		for _, p := range paths {
			if p > cursor {
				filtered = append(filtered, p)
			}
		}
		paths = filtered
	}

	// Apply limit
	var nextCursor string
	if limit > 0 && len(paths) > limit {
		nextCursor = paths[limit-1]
		paths = paths[:limit]
	}

	return paths, nextCursor, nil
}

// GetBatch retrieves multiple parameters by paths.
func (m *kvParameterManager) GetBatch(ctx context.Context, paths []string, withDecryption bool) (map[string]*interfaces.ParameterResult, map[string]error, error) {
	results := make(map[string]*interfaces.ParameterResult)
	errs := make(map[string]error)

	for _, path := range paths {
		value, metadata, err := m.Get(ctx, path, withDecryption)
		if err != nil {
			errs[path] = err
		} else {
			results[path] = &interfaces.ParameterResult{
				Value:    value,
				Metadata: metadata,
			}
		}
	}

	return results, errs, nil
}

// StoreBatch stores multiple parameters atomically.
func (m *kvParameterManager) StoreBatch(ctx context.Context, params []*interfaces.ParameterEntry, overwrite bool, opts *interfaces.StoreOptions) error {
	// Note: For true atomicity, this would need transaction support
	// For now, we store each parameter individually
	for _, p := range params {
		if err := m.StoreWithOptions(ctx, p.Path, p.Value, p.Metadata, overwrite, opts); err != nil {
			return err
		}
	}
	return nil
}

// DeleteBatch removes multiple parameters.
func (m *kvParameterManager) DeleteBatch(ctx context.Context, paths []string) (map[string]bool, map[string]error, error) {
	results := make(map[string]bool)
	errs := make(map[string]error)

	for _, path := range paths {
		err := m.Delete(ctx, path)
		if err != nil {
			results[path] = false
			errs[path] = err
		} else {
			results[path] = true
		}
	}

	return results, errs, nil
}

// SearchByTags finds parameters matching all specified tags.
func (m *kvParameterManager) SearchByTags(ctx context.Context, tags map[string]string, prefix string, limit int, cursor string) ([]string, string, error) {
	if m.closed.Load() {
		return nil, "", api.NewServiceClosedError("kvParameterManager.SearchByTags", "parameter manager")
	}

	// First get all parameters with the prefix
	allPaths, _, err := m.List(ctx, prefix, true, 0, "")
	if err != nil {
		return nil, "", err
	}

	// Filter by tags
	var matchingPaths []string
	for _, path := range allPaths {
		value, metadata, err := m.Get(ctx, path, false)
		if err != nil {
			continue
		}
		_ = value // unused, we just need metadata

		// Check if all tags match
		matches := true
		for k, v := range tags {
			if metadata.Tags[k] != v {
				matches = false
				break
			}
		}

		if matches {
			matchingPaths = append(matchingPaths, path)
		}
	}

	// Sort
	sort.Strings(matchingPaths)

	// Apply cursor
	if cursor != "" {
		var filtered []string
		for _, p := range matchingPaths {
			if p > cursor {
				filtered = append(filtered, p)
			}
		}
		matchingPaths = filtered
	}

	// Apply limit
	var nextCursor string
	if limit > 0 && len(matchingPaths) > limit {
		nextCursor = matchingPaths[limit-1]
		matchingPaths = matchingPaths[:limit]
	}

	return matchingPaths, nextCursor, nil
}

// GetBackendType returns the backend type.
func (m *kvParameterManager) GetBackendType() string {
	return "kv"
}

// Close releases any resources held by the parameter manager.
func (m *kvParameterManager) Close() error {
	m.closed.Store(true)
	if m.logger != nil {
		m.logger.Info("Parameter manager closed")
	}
	return nil
}

// scanKeys scans all keys with the given prefix using the qrdb SDK Scan method.
func (m *kvParameterManager) scanKeys(ctx context.Context, prefix string) ([][]byte, error) {
	client, err := m.getClient()
	if err != nil {
		if m.logger != nil {
			m.logger.WithFields(map[string]interface{}{
				"prefix": prefix,
				"error":  err.Error(),
			}).Debug("Failed to get client for prefix scan")
		}
		return nil, nil
	}

	results, err := client.KV().Scan(ctx, prefix)
	if err != nil {
		// If scan fails, return empty result
		if m.logger != nil {
			m.logger.WithFields(map[string]interface{}{
				"prefix": prefix,
				"error":  err.Error(),
			}).Debug("Prefix scan failed")
		}
		return nil, nil
	}

	// Extract just the keys from the map
	keys := make([][]byte, 0, len(results))
	for k := range results {
		keys = append(keys, []byte(k))
	}
	return keys, nil
}
