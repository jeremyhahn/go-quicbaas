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

package security

import (
	"context"
	"os"
	"sync"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// SecretManager manages secure credential storage and retrieval.
// For application secrets, use QuicBaaS's secrets engine (pkg/secrets/).
// This manager is for internal security configuration only.
type SecretManager struct {
	backend SecretBackend
	cache   *SecretCache
	config  *SecretConfig
	logger  *logger.Logger
}

// SecretBackend defines the interface for secret storage backends.
type SecretBackend interface {
	// Get retrieves a secret by path
	Get(ctx context.Context, path string) (*Secret, error)

	// Set stores a secret at the given path
	Set(ctx context.Context, path string, secret *Secret) error

	// Delete removes a secret
	Delete(ctx context.Context, path string) error

	// List lists secrets at a given path prefix
	List(ctx context.Context, prefix string) ([]string, error)

	// Close closes the backend connection
	Close() error
}

// SecretConfig configures secret management.
type SecretConfig struct {
	CacheEnabled bool          `json:"cache_enabled"`
	CacheTTL     time.Duration `json:"cache_ttl"`
}

// Secret represents a secret value with metadata.
type Secret struct {
	Key       string            `json:"key"`
	Value     string            `json:"value"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   int               `json:"version"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

// SecretCache provides caching for secrets with TTL.
type SecretCache struct {
	entries map[string]*cachedSecret
	ttl     time.Duration
	mu      sync.RWMutex
}

type cachedSecret struct {
	secret    *Secret
	expiresAt time.Time
}

// NewSecretManager creates a new secret manager.
// This uses environment variables for internal security configuration.
// For application secrets, use QuicBaaS's secrets engine (pkg/secrets/).
func NewSecretManager(cfg *SecretConfig, log *logger.Logger) (*SecretManager, error) {
	if cfg == nil {
		cfg = &SecretConfig{}
	}

	// Set defaults
	if cfg.CacheTTL == 0 {
		cfg.CacheTTL = 5 * time.Minute
	}

	// Use environment backend for internal configuration
	backend, err := NewEnvBackend(log)
	if err != nil {
		return nil, api.NewOperationFailedError("create secret backend", err)
	}

	sm := &SecretManager{
		backend: backend,
		config:  cfg,
		logger:  log,
	}

	// Initialize cache if enabled
	if cfg.CacheEnabled {
		sm.cache = NewSecretCache(cfg.CacheTTL)
		go sm.cache.startCleanup()
	}

	log.Info("Secret manager initialized with environment backend")
	return sm, nil
}

// Get retrieves a secret by path.
func (sm *SecretManager) Get(ctx context.Context, path string) (*Secret, error) {
	// Check cache first
	if sm.cache != nil {
		if secret := sm.cache.Get(path); secret != nil {
			sm.logger.Debugf("Secret cache hit: %s", path)
			return secret, nil
		}
	}

	// Fetch from backend
	secret, err := sm.backend.Get(ctx, path)
	if err != nil {
		return nil, api.NewOperationFailedError("get secret "+path, err)
	}

	// Store in cache
	if sm.cache != nil {
		sm.cache.Set(path, secret)
	}

	return secret, nil
}

// Set stores a secret.
func (sm *SecretManager) Set(ctx context.Context, path string, value string, metadata map[string]string) error {
	secret := &Secret{
		Key:       path,
		Value:     value,
		Metadata:  metadata,
		Version:   1,
		CreatedAt: time.Now(),
	}

	if err := sm.backend.Set(ctx, path, secret); err != nil {
		return api.NewOperationFailedError("set secret "+path, err)
	}

	// Invalidate cache
	if sm.cache != nil {
		sm.cache.Delete(path)
	}

	sm.logger.Infof("Secret stored: %s", path)
	return nil
}

// Delete removes a secret.
func (sm *SecretManager) Delete(ctx context.Context, path string) error {
	if err := sm.backend.Delete(ctx, path); err != nil {
		return api.NewOperationFailedError("delete secret "+path, err)
	}

	// Remove from cache
	if sm.cache != nil {
		sm.cache.Delete(path)
	}

	sm.logger.Infof("Secret deleted: %s", path)
	return nil
}

// Close closes the secret manager.
func (sm *SecretManager) Close() error {
	if sm.cache != nil {
		sm.cache.Close()
	}
	return sm.backend.Close()
}

// EnvBackend implements environment variable-based secrets.
// This is used for internal security configuration (e.g., TLS cert paths).
// For application secrets, use QuicBaaS's secrets engine.
type EnvBackend struct {
	logger *logger.Logger
}

// NewEnvBackend creates a new environment variable backend.
func NewEnvBackend(log *logger.Logger) (*EnvBackend, error) {
	return &EnvBackend{
		logger: log,
	}, nil
}

// Get retrieves a secret from environment variables.
func (eb *EnvBackend) Get(ctx context.Context, path string) (*Secret, error) {
	value := os.Getenv(path)
	if value == "" {
		return nil, api.NewEnvVarNotFoundError("EnvBackend.Get", path)
	}

	return &Secret{
		Key:       path,
		Value:     value,
		Version:   1,
		CreatedAt: time.Now(),
	}, nil
}

// Set stores a secret (not supported for env backend).
func (eb *EnvBackend) Set(ctx context.Context, path string, secret *Secret) error {
	return api.NewUnsupportedOperationError("EnvBackend.Set", "set", "environment")
}

// Delete removes a secret (not supported for env backend).
func (eb *EnvBackend) Delete(ctx context.Context, path string) error {
	return api.NewUnsupportedOperationError("EnvBackend.Delete", "delete", "environment")
}

// List lists secrets (not supported for env backend).
func (eb *EnvBackend) List(ctx context.Context, prefix string) ([]string, error) {
	return nil, api.NewUnsupportedOperationError("EnvBackend.List", "list", "environment")
}

// Close closes the env backend.
func (eb *EnvBackend) Close() error {
	return nil
}

// NewSecretCache creates a new secret cache.
func NewSecretCache(ttl time.Duration) *SecretCache {
	return &SecretCache{
		entries: make(map[string]*cachedSecret),
		ttl:     ttl,
	}
}

// Get retrieves a secret from cache.
func (sc *SecretCache) Get(key string) *Secret {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	entry, exists := sc.entries[key]
	if !exists {
		return nil
	}

	// Check if expired
	if time.Now().After(entry.expiresAt) {
		return nil
	}

	return entry.secret
}

// Set stores a secret in cache.
func (sc *SecretCache) Set(key string, secret *Secret) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.entries[key] = &cachedSecret{
		secret:    secret,
		expiresAt: time.Now().Add(sc.ttl),
	}
}

// Delete removes a secret from cache.
func (sc *SecretCache) Delete(key string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	delete(sc.entries, key)
}

// startCleanup starts periodic cache cleanup.
func (sc *SecretCache) startCleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		sc.cleanup()
	}
}

// cleanup removes expired entries.
func (sc *SecretCache) cleanup() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	now := time.Now()
	for key, entry := range sc.entries {
		if now.After(entry.expiresAt) {
			delete(sc.entries, key)
		}
	}
}

// Close closes the cache.
func (sc *SecretCache) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	sc.entries = make(map[string]*cachedSecret)
}

// GetString is a convenience method to get a secret value as string.
func (sm *SecretManager) GetString(ctx context.Context, path string) (string, error) {
	secret, err := sm.Get(ctx, path)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

// GetWithDefault retrieves a secret or returns default value if not found.
func (sm *SecretManager) GetWithDefault(ctx context.Context, path, defaultValue string) string {
	secret, err := sm.Get(ctx, path)
	if err != nil {
		return defaultValue
	}
	return secret.Value
}
