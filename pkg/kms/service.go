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

package kms

import (
	"context"
	"crypto/x509"
	stderrors "errors"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// KMSService provides a unified interface for key management operations.
// It wraps one or more backends and provides caching, routing, and policy enforcement.
// This is the primary entry point for all KMS operations in QuicBaaS.
//
// The KMSService always operates with a BackendRegistry and BackendRouter,
// supporting multiple backends with intelligent routing based on:
//   - Key ID prefix (routes to specific backend)
//   - Algorithm requirements (routes to backend supporting the algorithm)
//   - Health status (failover to healthy backends)
//
// The KMSService orchestrates:
//   - Backend operations (delegating to registered Backends)
//   - Multi-backend routing (via BackendRouter)
//   - Health-based failover (via BackendRegistry)
//   - Metadata caching (via KeyStore)
//   - Key material caching (via KeyCache, optional for security)
//   - Algorithm validation
//   - Operation logging
//   - Context cancellation support
type KMSService struct {
	// Backend management
	registry *BackendRegistry
	router   *BackendRouter

	// Caching
	store *KeyStore
	cache *KeyCache

	// Logging
	logger interfaces.Logger

	// Options
	cacheKeyMaterial bool
	defaultAlgorithm string
}

// KMSServiceOptions configures the KMSService behavior.
type KMSServiceOptions struct {
	// ===================================================================
	// Backend Registry (Required)
	// ===================================================================

	// Registry provides access to multiple backends with health tracking.
	// This is REQUIRED - the KMSService always operates with a registry
	// that contains one or more backends.
	// Use BackendFactory.InitializeFromXKMS() to populate the registry
	// with backends from go-xkms based on build tags.
	Registry *BackendRegistry

	// RoutingConfig configures how operations are routed to backends.
	// If nil, DefaultRoutingConfig() is used.
	RoutingConfig *RoutingConfig

	// DefaultBackend specifies the preferred backend for operations
	// when no routing rules apply. If nil, the highest priority
	// healthy backend from the registry is used.
	DefaultBackend Backend

	// ===================================================================
	// Caching
	// ===================================================================

	// KeyStore caches key metadata for performance (optional).
	// If nil, a new KeyStore is created automatically.
	KeyStore *KeyStore

	// KeyCache caches key material for performance (optional).
	// If nil, a new KeyCache is created automatically.
	// Note: Only used if CacheKeyMaterial is true.
	KeyCache *KeyCache

	// CacheKeyMaterial enables caching of key material in memory.
	// Default: false (recommended for security).
	// WARNING: Enabling this reduces backend calls but keeps key material
	// in memory longer, increasing security risk.
	CacheKeyMaterial bool

	// ===================================================================
	// Other Options
	// ===================================================================

	// Logger for operation logging (optional).
	// If nil, no logging is performed.
	Logger interfaces.Logger

	// DefaultAlgorithm is the algorithm used when none is specified.
	// Default: AES-256-GCM
	DefaultAlgorithm string
}

// NewKMSService creates a new KMSService with the specified options.
//
// The KMSService requires a BackendRegistry populated with one or more backends.
// Use BackendFactory.InitializeFromXKMS() to populate the registry with
// backends from go-xkms based on build tags.
//
// Operations are routed to backends based on:
//   - Key ID prefix (structured key IDs route to specific backends)
//   - Algorithm requirements (routes to backend supporting the algorithm)
//   - Health status (failover to healthy backends)
//
// Returns an error if Registry is nil or empty.
func NewKMSService(opts *KMSServiceOptions) (*KMSService, error) {
	if opts == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "kms.NewKMSService",
			Err:  ErrOptionsNil,
		}
	}

	// Validate: Registry is required
	if opts.Registry == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "kms.NewKMSService",
			Err:  ErrRegistryRequired,
		}
	}

	// Validate: Registry must have at least one backend
	if len(opts.Registry.List()) == 0 {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "kms.NewKMSService",
			Err:  ErrRegistryEmpty,
		}
	}

	// Create KeyStore if not provided
	store := opts.KeyStore
	if store == nil {
		store = NewKeyStore()
	}

	// Create KeyCache if not provided (only if caching is enabled)
	cache := opts.KeyCache
	if cache == nil && opts.CacheKeyMaterial {
		cache = NewKeyCache(&KeyCacheOptions{
			DefaultTTL:      5 * time.Minute,
			MaxTTL:          1 * time.Hour,
			MaxEntries:      1000,
			CleanupInterval: 1 * time.Minute,
			MaxKeySize:      1024 * 1024, // 1MB max key size
		})
	}

	// Set default algorithm
	defaultAlgorithm := opts.DefaultAlgorithm
	if defaultAlgorithm == "" {
		defaultAlgorithm = AlgAES256GCM
	}

	// Validate default algorithm
	if !IsValidAlgorithm(defaultAlgorithm) {
		return nil, api.NewInvalidAlgorithmError("kms.NewKMSService", defaultAlgorithm)
	}

	// Create router with routing config
	routingConfig := opts.RoutingConfig
	if routingConfig == nil {
		routingConfig = DefaultRoutingConfig()
	}

	routerOpts := []RouterOption{}
	if opts.Logger != nil {
		routerOpts = append(routerOpts, WithLogger(opts.Logger))
	}
	// Use DefaultBackend as fallback if provided
	if opts.DefaultBackend != nil {
		routerOpts = append(routerOpts, WithDefaultBackend(opts.DefaultBackend))
	}

	s := &KMSService{
		registry:         opts.Registry,
		router:           NewBackendRouter(opts.Registry, routingConfig, routerOpts...),
		store:            store,
		cache:            cache,
		logger:           opts.Logger,
		cacheKeyMaterial: opts.CacheKeyMaterial,
		defaultAlgorithm: defaultAlgorithm,
	}

	if s.logger != nil {
		backendNames := make([]string, 0, len(s.registry.List()))
		for _, info := range s.registry.List() {
			backendNames = append(backendNames, info.Name)
		}
		s.logger.WithFields(map[string]interface{}{
			"backends":           backendNames,
			"backends_count":     len(backendNames),
			"cache_key_material": s.cacheKeyMaterial,
			"default_algorithm":  s.defaultAlgorithm,
		}).Info("KMSService initialized with backend registry")
	}

	return s, nil
}

// GenerateKey creates a new cryptographic key with the specified parameters.
// Returns key metadata (never raw key material).
//
// In multi-backend mode, the backend is selected based on:
//  1. StoreType in attrs (if set, routes to matching backend)
//  2. Algorithm routing (if configured)
//  3. Default backend or highest priority healthy backend
func (s *KMSService) GenerateKey(ctx context.Context, attrs *kctypes.KeyAttributes) (interface{}, error) {
	if attrs == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GenerateKey",
			Err:  ErrKeyAttributesNil,
		}
	}

	if attrs.CN == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GenerateKey",
			Err:  ErrKeyCNEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.GenerateKey",
			Err:  ctx.Err(),
		}
	}

	// Get backend for this operation
	// For key generation, we route by algorithm since no key ID exists yet
	algorithm := attrsToAlgorithmString(attrs)
	backend, err := s.getBackendByAlgorithm(ctx, algorithm)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.GenerateKey", err)
	}

	// Set StoreType based on selected backend if not already set
	if attrs.StoreType == "" {
		attrs.StoreType = kctypes.StoreType(backend.Type())
	}

	// Validate key attributes
	if err := attrs.Validate(); err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GenerateKey",
			Err:  err,
		}
	}

	// Create key via backend
	metadata, err := backend.CreateKey(ctx, attrs)
	if err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.GenerateKey",
			Err:  err,
		}
	}

	// Store metadata in cache
	if err := s.store.Put(s.copyMetadata(metadata)); err != nil {
		// Log but don't fail - cache failure is not critical
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_cn": attrs.CN,
				"error":  err.Error(),
			}).Warn("Failed to cache key metadata")
		}
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"key_cn":    attrs.CN,
			"key_id":    metadata.KeyID,
			"algorithm": metadata.Algorithm,
		}).Info("Key generated")
	}

	return s.copyMetadata(metadata), nil
}

// ImportKey imports an existing key into the KMS.
// Returns key metadata.
//
// In multi-backend mode, the backend is selected based on key ID structure
// or algorithm routing.
func (s *KMSService) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
	// Use default algorithm if none specified
	if algorithm == "" {
		algorithm = s.defaultAlgorithm
	}

	// Validate algorithm
	if err := s.validateAlgorithm(algorithm); err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.ImportKey",
			Err:  err,
		}
	}

	if keyID == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.ImportKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	if len(material) == 0 {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.ImportKey",
			Err:  ErrKeyMaterialEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.ImportKey",
			Err:  ctx.Err(),
		}
	}

	// Get backend for this operation (route by key ID or algorithm)
	backend, err := s.getBackend(ctx, keyID, algorithm)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.ImportKey", err)
	}

	// Import key via backend
	metadata, err := backend.ImportKey(ctx, keyID, material, algorithm, nil)
	if err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.ImportKey",
			Err:  err,
		}
	}

	// Store metadata in cache
	if err := s.store.Put(s.copyMetadata(metadata)); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to cache key metadata")
		}
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"key_id":    keyID,
			"algorithm": algorithm,
		}).Info("Key imported")
	}

	return s.copyMetadata(metadata), nil
}

// GetKey retrieves key metadata by ID.
// Never returns raw key material.
//
// In multi-backend mode, the backend is selected based on key ID structure.
func (s *KMSService) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	if keyID == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GetKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.GetKey",
			Err:  ctx.Err(),
		}
	}

	// Try to get from cache first
	cached, err := s.store.Get(keyID)
	if err == nil {
		return cached, nil
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.GetKey", err)
	}

	// Cache miss - fetch from backend
	metadata, err := backend.GetKeyMetadata(ctx, keyID)
	if err != nil {
		if stderrors.Is(err, ErrKeyNotFound) {
			return nil, &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "KMSService.GetKey",
				Err:  err,
			}
		}
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.GetKey",
			Err:  err,
		}
	}

	// Store in cache for future requests
	serviceMeta := s.copyMetadata(metadata)
	if err := s.store.Put(serviceMeta); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to cache key metadata")
		}
	}

	return serviceMeta, nil
}

// GetPublicKey retrieves the DER-encoded public key for an asymmetric key.
// Returns an error if the key is symmetric (has no public component).
func (s *KMSService) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	if keyID == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GetPublicKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.GetPublicKey",
			Err:  ctx.Err(),
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.GetPublicKey", err)
	}

	// Get the public key from the backend
	publicKey, err := backend.ExportPublicKey(ctx, keyID)
	if err != nil {
		if stderrors.Is(err, ErrKeyNotFound) {
			return nil, &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "KMSService.GetPublicKey",
				Err:  err,
			}
		}
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.GetPublicKey",
			Err:  err,
		}
	}

	return publicKey, nil
}

// UpdateStoredMetadata updates metadata fields (Name, Description, Tags) for a key
// in the internal store. This is used to persist application-level metadata that
// isn't part of the cryptographic key itself.
func (s *KMSService) UpdateStoredMetadata(metadata interface{}) error {
	serviceMeta, ok := metadata.(*KeyMetadata)
	if !ok {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.UpdateStoredMetadata",
			Err:  ErrMetadataTypeMismatch,
		}
	}

	if serviceMeta.KeyID == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.UpdateStoredMetadata",
			Err:  ErrKeyIDRequired,
		}
	}

	// Store the metadata in the cache
	if err := s.store.Put(serviceMeta); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": serviceMeta.KeyID,
				"error":  err.Error(),
			}).Warn("Failed to update stored key metadata")
		}
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.UpdateStoredMetadata",
			Err:  err,
		}
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"key_id":      serviceMeta.KeyID,
			"name":        serviceMeta.Name,
			"description": serviceMeta.Description,
		}).Debug("Updated stored key metadata")
	}

	return nil
}

// ListKeys returns all keys matching the optional prefix.
// This aggregates keys from all registered backends in the registry.
func (s *KMSService) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.ListKeys",
			Err:  ctx.Err(),
		}
	}

	var allKeys []*KeyMetadata

	// Aggregate keys from all backends
	for _, info := range s.registry.ListWithBackends() {
		if !info.Enabled || info.Backend == nil {
			continue
		}
		keys, _, err := info.Backend.ListKeys(ctx, prefix, 0)
		if err != nil {
			// Log but continue with other backends
			if s.logger != nil {
				s.logger.WithFields(map[string]interface{}{
					"backend": info.Name,
					"error":   err.Error(),
				}).Warn("Failed to list keys from backend")
			}
			continue
		}
		allKeys = append(allKeys, keys...)
	}

	// Convert to interface slice, preferring store metadata (has Name, Description)
	// over backend metadata
	result := make([]interface{}, len(allKeys))
	for i, key := range allKeys {
		backendMeta := s.copyMetadata(key)
		// Try to get stored metadata with Name/Description from the store
		if storedMeta, err := s.store.Get(backendMeta.KeyID); err == nil {
			// Use stored metadata (has Name, Description, Tags from GenerateKey)
			result[i] = storedMeta
		} else {
			// Fall back to backend metadata
			result[i] = backendMeta
		}
	}

	return result, nil
}

// RotateKey creates a new version of an existing key.
// The old version remains available for decryption.
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	if keyID == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.RotateKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.RotateKey",
			Err:  ctx.Err(),
		}
	}

	// Validate key exists and is not disabled
	if err := s.validateKeyForOperation(keyID, "rotate"); err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.RotateKey",
			Err:  err,
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.RotateKey", err)
	}

	// Rotate key via backend
	metadata, err := backend.RotateKey(ctx, keyID)
	if err != nil {
		if stderrors.Is(err, ErrKeyNotFound) {
			return nil, &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "KMSService.RotateKey",
				Err:  err,
			}
		}
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.RotateKey",
			Err:  err,
		}
	}

	// Update cache
	serviceMeta := s.copyMetadata(metadata)
	if err := s.store.Put(serviceMeta); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to update cached key metadata")
		}
	}

	// Invalidate key material cache if enabled
	if s.cache != nil {
		s.cache.Delete(keyID)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"key_id":      keyID,
			"new_version": metadata.Version,
		}).Info("Key rotated")
	}

	return serviceMeta, nil
}

// RevokeKey marks a key as revoked (cannot be used for new operations).
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) RevokeKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.RevokeKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.RevokeKey",
			Err:  ctx.Err(),
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return api.NewBackendRoutingError("KMSService.RevokeKey", err)
	}

	// Disable key via backend
	if err := backend.DisableKey(ctx, keyID); err != nil {
		if stderrors.Is(err, ErrKeyNotFound) {
			return &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "KMSService.RevokeKey",
				Err:  err,
			}
		}
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.RevokeKey",
			Err:  err,
		}
	}

	// Update cache state
	if err := s.store.SetState(keyID, KeyStateDisabled); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to update cached key state")
		}
	}

	// Invalidate key material cache if enabled
	if s.cache != nil {
		s.cache.Delete(keyID)
	}

	if s.logger != nil {
		s.logger.WithField("key_id", keyID).Info("Key revoked")
	}

	return nil
}

// EnableKey enables a disabled key for use.
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) EnableKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.EnableKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.EnableKey",
			Err:  ctx.Err(),
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return api.NewBackendRoutingError("KMSService.EnableKey", err)
	}

	// Enable key via backend
	if err := backend.EnableKey(ctx, keyID); err != nil {
		if stderrors.Is(err, ErrKeyNotFound) {
			return &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "KMSService.EnableKey",
				Err:  err,
			}
		}
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.EnableKey",
			Err:  err,
		}
	}

	// Update cache state
	if err := s.store.SetState(keyID, KeyStateEnabled); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to update cached key state")
		}
	}

	if s.logger != nil {
		s.logger.WithField("key_id", keyID).Info("Key enabled")
	}

	return nil
}

// DisableKey disables a key, preventing its use for new operations.
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) DisableKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.DisableKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.DisableKey",
			Err:  ctx.Err(),
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return api.NewBackendRoutingError("KMSService.DisableKey", err)
	}

	// Disable key via backend
	if err := backend.DisableKey(ctx, keyID); err != nil {
		if stderrors.Is(err, ErrKeyNotFound) {
			return &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "KMSService.DisableKey",
				Err:  err,
			}
		}
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.DisableKey",
			Err:  err,
		}
	}

	// Update cache state
	if err := s.store.SetState(keyID, KeyStateDisabled); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to update cached key state")
		}
	}

	// Invalidate key material cache if enabled
	if s.cache != nil {
		s.cache.Delete(keyID)
	}

	if s.logger != nil {
		s.logger.WithField("key_id", keyID).Info("Key disabled")
	}

	return nil
}

// DeleteKey permanently removes a key.
// Most backends require the key to be disabled first.
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) DeleteKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.DeleteKey",
			Err:  ErrKeyIDEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.DeleteKey",
			Err:  ctx.Err(),
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return api.NewBackendRoutingError("KMSService.DeleteKey", err)
	}

	// Delete key via backend
	if err := backend.DeleteKey(ctx, keyID); err != nil {
		if stderrors.Is(err, ErrKeyNotFound) {
			return &errors.DragonError{
				Code: errors.ErrNotFound,
				Op:   "KMSService.DeleteKey",
				Err:  err,
			}
		}
		return &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.DeleteKey",
			Err:  err,
		}
	}

	// Remove from cache
	if err := s.store.Delete(keyID); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to remove key from cache")
		}
	}

	// Invalidate key material cache if enabled
	if s.cache != nil {
		s.cache.Delete(keyID)
	}

	if s.logger != nil {
		s.logger.WithField("key_id", keyID).Info("Key deleted")
	}

	return nil
}

// Encrypt encrypts plaintext using the specified key.
// aad is optional additional authenticated data for AEAD ciphers.
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if keyID == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Encrypt",
			Err:  ErrKeyIDEmpty,
		}
	}

	if len(plaintext) == 0 {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Encrypt",
			Err:  ErrPlaintextEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.Encrypt",
			Err:  ctx.Err(),
		}
	}

	// Validate key can be used for encryption
	if err := s.validateKeyForOperation(keyID, OpEncrypt); err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Encrypt",
			Err:  err,
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.Encrypt", err)
	}

	// Encrypt via backend
	ciphertext, err := backend.Encrypt(ctx, keyID, plaintext, aad)
	if err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrEncryption,
			Op:   "KMSService.Encrypt",
			Err:  err,
		}
	}

	// Update usage counter
	if err := s.store.IncrementUsage(keyID); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to increment usage counter")
		}
	}

	return ciphertext, nil
}

// Decrypt decrypts ciphertext using the specified key.
// aad must match the value used during encryption.
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if keyID == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Decrypt",
			Err:  ErrKeyIDEmpty,
		}
	}

	if len(ciphertext) == 0 {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Decrypt",
			Err:  ErrCiphertextEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.Decrypt",
			Err:  ctx.Err(),
		}
	}

	// Validate key can be used for decryption
	if err := s.validateKeyForOperation(keyID, OpDecrypt); err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Decrypt",
			Err:  err,
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.Decrypt", err)
	}

	// Decrypt via backend
	plaintext, err := backend.Decrypt(ctx, keyID, ciphertext, aad)
	if err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrDecryption,
			Op:   "KMSService.Decrypt",
			Err:  err,
		}
	}

	// Update usage counter
	if err := s.store.IncrementUsage(keyID); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to increment usage counter")
		}
	}

	return plaintext, nil
}

// Sign creates a digital signature of the digest.
// algorithm specifies the signature algorithm (e.g., "SHA256withRSA", "ECDSA-P256-SHA256").
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	if keyID == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Sign",
			Err:  ErrKeyIDEmpty,
		}
	}

	if len(digest) == 0 {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Sign",
			Err:  ErrDigestEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.Sign",
			Err:  ctx.Err(),
		}
	}

	// Validate key can be used for signing
	if err := s.validateKeyForOperation(keyID, OpSign); err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Sign",
			Err:  err,
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return nil, api.NewBackendRoutingError("KMSService.Sign", err)
	}

	// Sign via backend
	signature, err := backend.Sign(ctx, keyID, digest, algorithm)
	if err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.Sign",
			Err:  err,
		}
	}

	// Update usage counter
	if err := s.store.IncrementUsage(keyID); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to increment usage counter")
		}
	}

	return signature, nil
}

// Verify validates a signature against the digest.
//
// In multi-backend mode, routes to the backend that owns the key.
func (s *KMSService) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	if keyID == "" {
		return false, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Verify",
			Err:  ErrKeyIDEmpty,
		}
	}

	if len(digest) == 0 {
		return false, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Verify",
			Err:  ErrDigestEmpty,
		}
	}

	if len(signature) == 0 {
		return false, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Verify",
			Err:  ErrSignatureEmpty,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return false, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.Verify",
			Err:  ctx.Err(),
		}
	}

	// Validate key can be used for verification
	if err := s.validateKeyForOperation(keyID, OpVerify); err != nil {
		return false, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.Verify",
			Err:  err,
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return false, api.NewBackendRoutingError("KMSService.Verify", err)
	}

	// Verify via backend
	valid, err := backend.Verify(ctx, keyID, digest, signature, algorithm)
	if err != nil {
		return false, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.Verify",
			Err:  err,
		}
	}

	// Update usage counter
	if err := s.store.IncrementUsage(keyID); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to increment usage counter")
		}
	}

	return valid, nil
}

// GenerateDataKey generates a new data encryption key.
// attrs.CN specifies the KEK (key encryption key) to use for wrapping.
// keyBytes specifies the size of the DEK in bytes.
// Returns both plaintext and wrapped (encrypted) versions.
// The wrapped key can be stored; the plaintext should be used and discarded.
//
// In multi-backend mode, routes to the backend that owns the KEK.
func (s *KMSService) GenerateDataKey(ctx context.Context, attrs *kctypes.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
	if attrs == nil {
		return nil, nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GenerateDataKey",
			Err:  ErrKeyAttributesNil,
		}
	}

	// CN specifies the KEK to use for wrapping
	keyID := attrs.CN
	if keyID == "" {
		return nil, nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GenerateDataKey",
			Err:  ErrKeyCNEmpty,
		}
	}

	if keyBytes <= 0 {
		return nil, nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GenerateDataKey",
			Err:  ErrKeyBytesPositive,
		}
	}

	// Check for context cancellation
	if ctx.Err() != nil {
		return nil, nil, &errors.DragonError{
			Code: errors.ErrTimeout,
			Op:   "KMSService.GenerateDataKey",
			Err:  ctx.Err(),
		}
	}

	// Validate key can be used for wrapping
	if err := s.validateKeyForOperation(keyID, OpWrap); err != nil {
		return nil, nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "KMSService.GenerateDataKey",
			Err:  err,
		}
	}

	// Get backend for this operation
	backend, err := s.getBackendByKeyID(ctx, keyID)
	if err != nil {
		return nil, nil, api.NewBackendRoutingError("KMSService.GenerateDataKey", err)
	}

	// Generate data key via backend
	// The backend uses keyID (from attrs.CN) as the KEK for wrapping
	plaintext, wrapped, err = backend.GenerateDataKey(ctx, keyID, keyBytes)
	if err != nil {
		return nil, nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "KMSService.GenerateDataKey",
			Err:  err,
		}
	}

	// Update usage counter
	if err := s.store.IncrementUsage(keyID); err != nil {
		if s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"key_id": keyID,
				"error":  err.Error(),
			}).Warn("Failed to increment usage counter")
		}
	}

	return plaintext, wrapped, nil
}

// GetBackendType returns the type of the primary/first backend in the registry.
func (s *KMSService) GetBackendType() config.BackendType {
	infos := s.registry.List()
	if len(infos) > 0 {
		return infos[0].Type
	}
	return config.BackendTypeSoftware
}

// IsAvailable checks if the KMS service is accessible.
// Returns true if at least one backend in the registry is available.
func (s *KMSService) IsAvailable(ctx context.Context) bool {
	for _, info := range s.registry.ListWithBackends() {
		if info.Enabled && info.Backend != nil && info.Backend.IsAvailable(ctx) {
			return true
		}
	}
	return false
}

// GetSupportedAlgorithms returns the union of cryptographic algorithms
// supported by all backends in the registry.
func (s *KMSService) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	// Collect unique algorithms from all backends
	algSet := make(map[string]struct{})
	for _, info := range s.registry.List() {
		if !info.Enabled {
			continue
		}
		for _, alg := range info.Algorithms {
			algSet[alg] = struct{}{}
		}
	}

	algorithms := make([]string, 0, len(algSet))
	for alg := range algSet {
		algorithms = append(algorithms, alg)
	}
	return algorithms, nil
}

// Close releases any resources held by the KMSService.
func (s *KMSService) Close() error {
	if s.logger != nil {
		backendCount := len(s.registry.List())
		s.logger.WithField("backends_count", backendCount).Info("Closing KMSService")
	}

	// Close key material cache if enabled
	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			return api.NewCacheCloseError("KMSService.Close", err)
		}
	}

	// Close all registered backends
	for _, info := range s.registry.ListWithBackends() {
		if info.Backend == nil {
			continue
		}
		if err := info.Backend.Close(); err != nil {
			if s.logger != nil {
				s.logger.WithFields(map[string]interface{}{
					"backend": info.Name,
					"error":   err.Error(),
				}).Warn("Failed to close backend")
			}
			// Continue closing other backends
		}
	}

	return nil
}

// =============================================================================
// Backend Routing Helpers
// =============================================================================

// getBackend returns the appropriate backend for an operation.
// Routes based on keyID and algorithm using the BackendRouter.
func (s *KMSService) getBackend(ctx context.Context, keyID, algorithm string) (Backend, error) {
	return s.router.Route(ctx, keyID, algorithm)
}

// getBackendByKeyID returns the appropriate backend for a key ID.
// Uses structured key ID routing if the ID contains backend information.
func (s *KMSService) getBackendByKeyID(ctx context.Context, keyID string) (Backend, error) {
	return s.router.RouteByKeyID(ctx, keyID)
}

// getBackendByAlgorithm returns a backend that supports the specified algorithm.
// Used for key generation where no key ID exists yet.
func (s *KMSService) getBackendByAlgorithm(ctx context.Context, algorithm string) (Backend, error) {
	return s.router.RouteByAlgorithm(ctx, algorithm)
}

// GetRouter returns the backend router.
func (s *KMSService) GetRouter() *BackendRouter {
	return s.router
}

// GetRegistry returns the backend registry.
func (s *KMSService) GetRegistry() *BackendRegistry {
	return s.registry
}

// BackendCount returns the number of backends registered in the service.
func (s *KMSService) BackendCount() int {
	return len(s.registry.List())
}

// ListBackends returns information about all registered backends.
func (s *KMSService) ListBackends() []*BackendInfo {
	return s.registry.List()
}

// =============================================================================
// Validation Helpers
// =============================================================================

// attrsToAlgorithmString extracts the algorithm string from KeyAttributes.
// This is used for routing decisions when generating new keys.
func attrsToAlgorithmString(attrs *kctypes.KeyAttributes) string {
	if attrs == nil {
		return AlgAES256GCM
	}

	// Handle symmetric algorithms
	switch attrs.SymmetricAlgorithm {
	case kctypes.SymmetricAES128GCM:
		return AlgAES128GCM
	case kctypes.SymmetricAES256GCM:
		return AlgAES256GCM
	case kctypes.SymmetricChaCha20Poly1305:
		return AlgChaCha20Poly1305
	case kctypes.SymmetricAES192GCM:
		return AlgAES192GCM
	case kctypes.SymmetricXChaCha20Poly1305:
		return AlgXChaCha20Poly1305
	}

	// Handle asymmetric algorithms by checking KeyAlgorithm (x509.PublicKeyAlgorithm)
	switch attrs.KeyAlgorithm {
	case x509.RSA:
		return AlgRSA2048
	case x509.ECDSA:
		return AlgECDSAP256
	case x509.Ed25519:
		return AlgEd25519
	}

	// Default to AES-256-GCM for encryption operations
	return AlgAES256GCM
}

// validateAlgorithm validates that the algorithm is supported.
func (s *KMSService) validateAlgorithm(algorithm string) error {
	if !IsValidAlgorithm(algorithm) {
		return api.NewInvalidAlgorithmError("KMSService.validateAlgorithm", algorithm)
	}
	return nil
}

// validateKeyForOperation checks if a key exists and can be used for the specified operation.
func (s *KMSService) validateKeyForOperation(keyID, operation string) error {
	// Get key metadata from cache or backend
	metadata, err := s.store.Get(keyID)
	if err != nil {
		// Not in cache - this is OK, backend will validate
		return nil
	}

	// Check key state
	if metadata.State == KeyStateDisabled {
		return api.NewKeyDisabledError("KMSService.validateKeyForOperation", keyID)
	}
	if metadata.State == KeyStateDeleted {
		return api.NewKeyDeletedError("KMSService.validateKeyForOperation", keyID)
	}
	if metadata.State == KeyStatePendingDeletion {
		return api.NewKeyPendingDeletionError("KMSService.validateKeyForOperation", keyID)
	}

	// Validate algorithm supports the operation
	algorithmStr := string(metadata.Algorithm)
	if !CanAlgorithmPerform(algorithmStr, operation) {
		return api.NewAlgorithmOperationUnsupportedError("KMSService.validateKeyForOperation", algorithmStr, operation)
	}

	return nil
}

// copyMetadata creates a copy of KeyMetadata for cache storage.
// This ensures modifications to the returned metadata don't affect the cached version.
func (s *KMSService) copyMetadata(meta *KeyMetadata) *KeyMetadata {
	if meta == nil {
		return nil
	}

	copied := &KeyMetadata{
		KeyID:              meta.KeyID,
		Name:               meta.Name,
		Description:        meta.Description,
		Algorithm:          meta.Algorithm,
		State:              meta.State,
		Version:            meta.Version,
		Exportable:         meta.Exportable,
		DeletionProtection: meta.DeletionProtection,
		Created:            meta.Created,
		Updated:            meta.Updated,
		UsageCount:         meta.UsageCount,
		Owner:              meta.Owner,
	}

	// Copy Tags map
	if meta.Tags != nil {
		copied.Tags = make(map[string]string, len(meta.Tags))
		for k, v := range meta.Tags {
			copied.Tags[k] = v
		}
	}

	// Copy Usage slice
	if meta.Usage != nil {
		copied.Usage = make([]KeyUsage, len(meta.Usage))
		copy(copied.Usage, meta.Usage)
	}

	// Copy LastUsedAt pointer
	if meta.LastUsedAt != nil {
		t := *meta.LastUsedAt
		copied.LastUsedAt = &t
	}

	// Copy RotationPolicy
	if meta.RotationPolicy != nil {
		copied.RotationPolicy = &RotationPolicy{
			Enabled:        meta.RotationPolicy.Enabled,
			Interval:       meta.RotationPolicy.Interval,
			NextRotation:   meta.RotationPolicy.NextRotation,
			RetainVersions: meta.RotationPolicy.RetainVersions,
			AutoRotate:     meta.RotationPolicy.AutoRotate,
		}
	}

	return copied
}
