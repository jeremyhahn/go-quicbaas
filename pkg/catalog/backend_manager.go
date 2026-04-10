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

package catalog

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-objstore/pkg/common"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// isValidBackendTier checks if the storage tier is valid for backend configuration.
// This allows empty string (meaning "no tier specified") in addition to the standard tiers.
func isValidBackendTier(t StorageTier) bool {
	switch t {
	case TierHot, TierWarm, TierCold, "":
		return true
	default:
		return false
	}
}

// BackendConfig defines configuration for a storage backend.
type BackendConfig struct {
	// Type is the backend type (s3, gcs, local, memory, etc.).
	Type string

	// Enabled indicates whether the backend is enabled.
	Enabled bool

	// Default indicates whether this is the default backend.
	Default bool

	// Tier is the storage tier for this backend.
	Tier StorageTier

	// Region is the geographic region for this backend.
	Region string

	// MaxObjectSize is the maximum object size in bytes (0 = unlimited).
	MaxObjectSize int64

	// Settings contains backend-specific configuration.
	Settings map[string]string
}

// Clone creates a deep copy of the backend configuration.
func (c *BackendConfig) Clone() *BackendConfig {
	if c == nil {
		return nil
	}

	clone := &BackendConfig{
		Type:          c.Type,
		Enabled:       c.Enabled,
		Default:       c.Default,
		Tier:          c.Tier,
		Region:        c.Region,
		MaxObjectSize: c.MaxObjectSize,
	}

	if c.Settings != nil {
		clone.Settings = make(map[string]string, len(c.Settings))
		for k, v := range c.Settings {
			clone.Settings[k] = v
		}
	}

	return clone
}

// Validate validates the backend configuration.
func (c *BackendConfig) Validate() error {
	if c == nil {
		return ErrBackendConfigNil
	}
	if c.Type == "" {
		return ErrBackendTypeEmpty
	}
	if !isValidBackendTier(c.Tier) {
		return ErrInvalidStorageTier
	}
	if c.MaxObjectSize < 0 {
		return ErrInvalidMaxObjectSize
	}
	return nil
}

// BackendHealth represents the health status of a storage backend.
type BackendHealth struct {
	// Name is the backend name.
	Name string

	// Healthy indicates whether the backend is healthy.
	Healthy bool

	// LastCheck is the timestamp of the last health check.
	LastCheck time.Time

	// Latency is the latency of the last health check.
	Latency time.Duration

	// Error contains the error message if unhealthy.
	Error string

	// Status provides additional status information.
	Status string
}

// Clone creates a deep copy of the backend health.
func (h *BackendHealth) Clone() *BackendHealth {
	if h == nil {
		return nil
	}
	return &BackendHealth{
		Name:      h.Name,
		Healthy:   h.Healthy,
		LastCheck: h.LastCheck,
		Latency:   h.Latency,
		Error:     h.Error,
		Status:    h.Status,
	}
}

// SelectionCriteria defines criteria for backend selection.
type SelectionCriteria struct {
	// PreferredBackend is the explicitly requested backend name.
	PreferredBackend string

	// TenantID is the tenant identifier for tenant-specific selection.
	TenantID string

	// ObjectSize is the size of the object being stored.
	ObjectSize int64

	// Tier is the preferred storage tier.
	Tier StorageTier

	// RequireLocal indicates whether local storage is required.
	RequireLocal bool
}

// BackendManager manages multiple storage backends with health monitoring
// and intelligent selection.
type BackendManager interface {
	// GetBackend returns a storage backend by name.
	GetBackend(name string) (common.Storage, error)

	// ListBackends returns the names of all registered backends.
	ListBackends() []string

	// GetBackendConfig returns the configuration for a backend.
	GetBackendConfig(name string) (*BackendConfig, error)

	// RegisterBackend dynamically registers a new backend.
	RegisterBackend(name string, cfg *BackendConfig) error

	// UnregisterBackend removes a backend from the manager.
	UnregisterBackend(name string) error

	// SelectBackend selects a backend based on criteria.
	SelectBackend(criteria *SelectionCriteria) (string, common.Storage, error)

	// CheckHealth checks the health of a specific backend.
	CheckHealth(ctx context.Context, name string) (*BackendHealth, error)

	// CheckAllHealth checks the health of all backends.
	CheckAllHealth(ctx context.Context) map[string]*BackendHealth

	// Close closes the backend manager and all backends.
	Close() error
}

// BackendManagerConfig configures the backend manager.
type BackendManagerConfig struct {
	// HealthCheckTimeout is the timeout for health checks.
	HealthCheckTimeout time.Duration

	// DefaultBackend is the name of the default backend.
	DefaultBackend string
}

// DefaultBackendManagerConfig returns sensible defaults for the backend manager.
func DefaultBackendManagerConfig() *BackendManagerConfig {
	return &BackendManagerConfig{
		HealthCheckTimeout: 5 * time.Second,
	}
}

// Validate validates the backend manager configuration.
func (c *BackendManagerConfig) Validate() error {
	if c == nil {
		return ErrBackendManagerConfigNil
	}
	if c.HealthCheckTimeout < 0 {
		return ErrInvalidHealthCheckTimeout
	}
	return nil
}

// backendEntry holds a backend instance and its configuration.
type backendEntry struct {
	storage common.Storage
	config  *BackendConfig
	health  atomic.Pointer[BackendHealth]
}

// backendManager implements BackendManager.
type backendManager struct {
	config   *BackendManagerConfig
	backends map[string]*backendEntry
	mu       sync.RWMutex
	closed   atomic.Bool
}

// NewBackendManager creates a new backend manager.
func NewBackendManager(cfg *BackendManagerConfig) (BackendManager, error) {
	if cfg == nil {
		cfg = DefaultBackendManagerConfig()
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &backendManager{
		config:   cfg,
		backends: make(map[string]*backendEntry),
	}, nil
}

// GetBackend returns a storage backend by name.
func (m *backendManager) GetBackend(name string) (common.Storage, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, ErrBackendNameEmpty
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.backends[name]
	if !ok {
		return nil, NewBackendNotFoundError(name)
	}

	if !entry.config.Enabled {
		return nil, NewBackendDisabledError(name)
	}

	return entry.storage, nil
}

// ListBackends returns the names of all registered backends.
func (m *backendManager) ListBackends() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}

	sort.Strings(names)
	return names
}

// GetBackendConfig returns the configuration for a backend.
func (m *backendManager) GetBackendConfig(name string) (*BackendConfig, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, ErrBackendNameEmpty
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, ok := m.backends[name]
	if !ok {
		return nil, NewBackendNotFoundError(name)
	}

	return entry.config.Clone(), nil
}

// RegisterBackend dynamically registers a new backend.
func (m *backendManager) RegisterBackend(name string, cfg *BackendConfig) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if name == "" {
		return ErrBackendNameEmpty
	}
	if err := cfg.Validate(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if backend already exists
	if _, ok := m.backends[name]; ok {
		return NewBackendExistsError(name)
	}

	// Create the storage backend using the factory
	storage, err := config.CreateStorageBackendByType(cfg.Type, cfg.Settings)
	if err != nil {
		return NewBackendCreationError(name, err)
	}

	entry := &backendEntry{
		storage: storage,
		config:  cfg.Clone(),
	}

	// Initialize health status
	health := &BackendHealth{
		Name:      name,
		Healthy:   true,
		LastCheck: time.Now(),
		Status:    "initialized",
	}
	entry.health.Store(health)

	m.backends[name] = entry

	// Update default if this is the first backend or marked as default
	if cfg.Default || m.config.DefaultBackend == "" {
		m.config.DefaultBackend = name
	}

	return nil
}

// UnregisterBackend removes a backend from the manager.
func (m *backendManager) UnregisterBackend(name string) error {
	if err := m.checkClosed(); err != nil {
		return err
	}
	if name == "" {
		return ErrBackendNameEmpty
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	entry, ok := m.backends[name]
	if !ok {
		return NewBackendNotFoundError(name)
	}

	// Close the storage backend
	if closer, ok := entry.storage.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			return NewBackendCloseError(name, err)
		}
	}

	delete(m.backends, name)

	// Update default if we removed the default backend
	if m.config.DefaultBackend == name {
		m.config.DefaultBackend = ""
		for n, e := range m.backends {
			if e.config.Enabled {
				m.config.DefaultBackend = n
				break
			}
		}
	}

	return nil
}

// SelectBackend selects a backend based on criteria.
func (m *backendManager) SelectBackend(criteria *SelectionCriteria) (string, common.Storage, error) {
	if err := m.checkClosed(); err != nil {
		return "", nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.backends) == 0 {
		return "", nil, ErrNoBackendsAvailable
	}

	// Step 1: Check explicit request
	if criteria != nil && criteria.PreferredBackend != "" {
		entry, ok := m.backends[criteria.PreferredBackend]
		if !ok {
			return "", nil, NewBackendNotFoundError(criteria.PreferredBackend)
		}
		if !entry.config.Enabled {
			return "", nil, NewBackendDisabledError(criteria.PreferredBackend)
		}
		health := entry.health.Load()
		if health != nil && !health.Healthy {
			return "", nil, NewBackendUnhealthyError(criteria.PreferredBackend)
		}
		return criteria.PreferredBackend, entry.storage, nil
	}

	// Step 2: Filter enabled backends
	candidates := make([]string, 0, len(m.backends))
	for name, entry := range m.backends {
		if !entry.config.Enabled {
			continue
		}
		health := entry.health.Load()
		if health != nil && !health.Healthy {
			continue
		}
		candidates = append(candidates, name)
	}

	if len(candidates) == 0 {
		return "", nil, ErrNoHealthyBackends
	}

	// Step 3: Filter by tier/size/locality
	if criteria != nil {
		candidates = m.filterByCriteria(candidates, criteria)
	}

	if len(candidates) == 0 {
		return "", nil, ErrNoMatchingBackends
	}

	// Step 4: Select best match
	name := m.selectBestMatch(candidates, criteria)
	entry := m.backends[name]

	return name, entry.storage, nil
}

// filterByCriteria filters candidates based on selection criteria.
func (m *backendManager) filterByCriteria(candidates []string, criteria *SelectionCriteria) []string {
	filtered := make([]string, 0, len(candidates))

	for _, name := range candidates {
		entry := m.backends[name]

		// Filter by tier
		if criteria.Tier != "" && entry.config.Tier != "" && entry.config.Tier != criteria.Tier {
			continue
		}

		// Filter by object size
		if criteria.ObjectSize > 0 && entry.config.MaxObjectSize > 0 {
			if criteria.ObjectSize > entry.config.MaxObjectSize {
				continue
			}
		}

		// Filter by locality (local = region is empty or local)
		if criteria.RequireLocal {
			if entry.config.Region != "" && entry.config.Region != "local" {
				continue
			}
		}

		filtered = append(filtered, name)
	}

	// If no candidates match criteria, return original list
	if len(filtered) == 0 {
		return candidates
	}

	return filtered
}

// selectBestMatch selects the best backend from candidates.
func (m *backendManager) selectBestMatch(candidates []string, criteria *SelectionCriteria) string {
	// Priority order:
	// 1. Default backend if in candidates
	// 2. Backend matching tier exactly
	// 3. First enabled backend

	// Check for default backend
	if m.config.DefaultBackend != "" {
		for _, name := range candidates {
			if name == m.config.DefaultBackend {
				return name
			}
		}
	}

	// Check for tier match
	if criteria != nil && criteria.Tier != "" {
		for _, name := range candidates {
			entry := m.backends[name]
			if entry.config.Tier == criteria.Tier {
				return name
			}
		}
	}

	// Sort for deterministic selection
	sort.Strings(candidates)
	return candidates[0]
}

// CheckHealth checks the health of a specific backend.
func (m *backendManager) CheckHealth(ctx context.Context, name string) (*BackendHealth, error) {
	if err := m.checkClosed(); err != nil {
		return nil, err
	}
	if name == "" {
		return nil, ErrBackendNameEmpty
	}

	m.mu.RLock()
	entry, ok := m.backends[name]
	m.mu.RUnlock()

	if !ok {
		return nil, NewBackendNotFoundError(name)
	}

	health := m.performHealthCheck(ctx, name, entry)

	// Update stored health
	entry.health.Store(health)

	return health.Clone(), nil
}

// CheckAllHealth checks the health of all backends.
func (m *backendManager) CheckAllHealth(ctx context.Context) map[string]*BackendHealth {
	m.mu.RLock()
	names := make([]string, 0, len(m.backends))
	for name := range m.backends {
		names = append(names, name)
	}
	m.mu.RUnlock()

	results := make(map[string]*BackendHealth, len(names))
	var wg sync.WaitGroup
	var resultMu sync.Mutex

	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()

			health, err := m.CheckHealth(ctx, n)
			if err != nil {
				health = &BackendHealth{
					Name:      n,
					Healthy:   false,
					LastCheck: time.Now(),
					Error:     err.Error(),
					Status:    "error",
				}
			}

			resultMu.Lock()
			results[n] = health
			resultMu.Unlock()
		}(name)
	}

	wg.Wait()
	return results
}

// performHealthCheck performs a health check on a backend.
func (m *backendManager) performHealthCheck(ctx context.Context, name string, entry *backendEntry) *BackendHealth {
	health := &BackendHealth{
		Name:      name,
		LastCheck: time.Now(),
	}

	// Apply timeout
	timeout := m.config.HealthCheckTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()

	// Perform a simple list operation to verify connectivity
	_, err := entry.storage.ListWithContext(ctx, "__health_check_prefix_that_should_not_exist__")
	health.Latency = time.Since(start)

	if err != nil {
		// Check if error is due to context timeout/cancellation
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			health.Healthy = false
			health.Error = "health check timeout"
			health.Status = "timeout"
			return health
		}

		// Some backends return error for non-existent prefix, which is OK
		// Only mark unhealthy for real connectivity issues
		errStr := err.Error()
		if isConnectivityError(errStr) {
			health.Healthy = false
			health.Error = errStr
			health.Status = "unhealthy"
			return health
		}
	}

	health.Healthy = true
	health.Status = "healthy"
	return health
}

// isConnectivityError checks if an error indicates connectivity issues.
func isConnectivityError(errStr string) bool {
	connectivityIndicators := []string{
		"connection refused",
		"connection reset",
		"network unreachable",
		"no such host",
		"dial tcp",
		"i/o timeout",
		"TLS handshake",
		"certificate",
		"authentication",
		"access denied",
		"forbidden",
		"unauthorized",
	}

	for _, indicator := range connectivityIndicators {
		if containsIgnoreCase(errStr, indicator) {
			return true
		}
	}

	return false
}

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	sLower := make([]byte, len(s))
	substrLower := make([]byte, len(substr))

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		sLower[i] = c
	}

	for i := 0; i < len(substr); i++ {
		c := substr[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		substrLower[i] = c
	}

	return bytesContains(sLower, substrLower)
}

// bytesContains checks if b contains sub.
func bytesContains(b, sub []byte) bool {
	if len(sub) == 0 {
		return true
	}
	if len(sub) > len(b) {
		return false
	}
	for i := 0; i <= len(b)-len(sub); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if b[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// Close closes the backend manager and all backends.
func (m *backendManager) Close() error {
	if m.closed.Swap(true) {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for name, entry := range m.backends {
		if closer, ok := entry.storage.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				if firstErr == nil {
					firstErr = NewBackendCloseError(name, err)
				}
			}
		}
	}

	m.backends = nil
	return firstErr
}

// checkClosed returns an error if the manager is closed.
func (m *backendManager) checkClosed() error {
	if m.closed.Load() {
		return NewBackendManagerClosedError()
	}
	return nil
}

// Backend manager specific errors.
var (
	// ErrBackendManagerConfigNil indicates a nil configuration was provided.
	ErrBackendManagerConfigNil = errors.New("backend manager config cannot be nil")

	// ErrBackendConfigNil indicates a nil backend config was provided.
	ErrBackendConfigNil = errors.New("backend config cannot be nil")

	// ErrBackendNameEmpty indicates an empty backend name was provided.
	ErrBackendNameEmpty = errors.New("backend name cannot be empty")

	// ErrBackendTypeEmpty indicates an empty backend type was provided.
	ErrBackendTypeEmpty = errors.New("backend type cannot be empty")

	// ErrInvalidStorageTier indicates an invalid storage tier.
	ErrInvalidStorageTier = errors.New("invalid storage tier")

	// ErrInvalidMaxObjectSize indicates an invalid max object size.
	ErrInvalidMaxObjectSize = errors.New("max object size cannot be negative")

	// ErrInvalidHealthCheckTimeout indicates an invalid health check timeout.
	ErrInvalidHealthCheckTimeout = errors.New("health check timeout cannot be negative")

	// ErrNoBackendsAvailable indicates no backends are registered.
	ErrNoBackendsAvailable = errors.New("no backends available")

	// ErrNoHealthyBackends indicates no healthy backends are available.
	ErrNoHealthyBackends = errors.New("no healthy backends available")

	// ErrNoMatchingBackends indicates no backends match the selection criteria.
	ErrNoMatchingBackends = errors.New("no backends match the selection criteria")
)

// Error code constants for backend manager errors.
const (
	// ErrCodeBackendDisabled indicates a backend is disabled.
	ErrCodeBackendDisabled ErrorCode = "BACKEND_DISABLED"

	// ErrCodeBackendExists indicates a backend already exists.
	ErrCodeBackendExists ErrorCode = "BACKEND_EXISTS"

	// ErrCodeBackendCreationFailed indicates backend creation failed.
	ErrCodeBackendCreationFailed ErrorCode = "BACKEND_CREATION_FAILED"

	// ErrCodeBackendCloseFailed indicates backend close failed.
	ErrCodeBackendCloseFailed ErrorCode = "BACKEND_CLOSE_FAILED"

	// ErrCodeBackendManagerClosed indicates the backend manager is closed.
	ErrCodeBackendManagerClosed ErrorCode = "BACKEND_MANAGER_CLOSED"
)

// NewBackendDisabledError creates a new error for disabled backend.
func NewBackendDisabledError(backend string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeBackendDisabled,
		Message: "storage backend is disabled: " + backend,
	}
}

// NewBackendExistsError creates a new error for duplicate backend.
func NewBackendExistsError(backend string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeBackendExists,
		Message: "storage backend already exists: " + backend,
	}
}

// NewBackendCreationError creates a new error for backend creation failure.
func NewBackendCreationError(backend string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeBackendCreationFailed,
		Message: "failed to create storage backend: " + backend,
		Cause:   cause,
	}
}

// NewBackendCloseError creates a new error for backend close failure.
func NewBackendCloseError(backend string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeBackendCloseFailed,
		Message: "failed to close storage backend: " + backend,
		Cause:   cause,
	}
}

// NewBackendManagerClosedError creates a new error for closed backend manager.
func NewBackendManagerClosedError() *CatalogError {
	return &CatalogError{
		Code:    ErrCodeBackendManagerClosed,
		Message: "backend manager is closed",
	}
}

// IsBackendDisabled returns true if the error is a backend disabled error.
func IsBackendDisabled(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeBackendDisabled
	}
	return false
}

// IsBackendExists returns true if the error is a backend exists error.
func IsBackendExists(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeBackendExists
	}
	return false
}

// IsBackendManagerClosed returns true if the error is a backend manager closed error.
func IsBackendManagerClosed(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeBackendManagerClosed
	}
	return false
}
