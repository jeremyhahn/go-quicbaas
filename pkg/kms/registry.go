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
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// HealthStatus represents the health state of a KMS backend.
type HealthStatus string

const (
	// HealthStatusHealthy indicates the backend is operational.
	HealthStatusHealthy HealthStatus = "healthy"

	// HealthStatusDegraded indicates the backend is operational but with issues.
	HealthStatusDegraded HealthStatus = "degraded"

	// HealthStatusUnhealthy indicates the backend is not operational.
	HealthStatusUnhealthy HealthStatus = "unhealthy"

	// HealthStatusUnknown indicates the health status has not been determined.
	HealthStatusUnknown HealthStatus = "unknown"
)

// BackendHealth contains health information for a KMS backend.
type BackendHealth struct {
	// Status is the current health status.
	Status HealthStatus `json:"status"`

	// Message provides additional context about the health status.
	Message string `json:"message,omitempty"`

	// LastCheck is when the health was last checked.
	LastCheck time.Time `json:"last_check"`

	// LastHealthy is when the backend was last confirmed healthy.
	LastHealthy time.Time `json:"last_healthy,omitempty"`

	// Latency is the response time of the last health check.
	Latency time.Duration `json:"latency"`

	// ConsecutiveFailures is the number of consecutive failed health checks.
	ConsecutiveFailures int `json:"consecutive_failures"`

	// TotalChecks is the total number of health checks performed.
	TotalChecks uint64 `json:"total_checks"`

	// TotalFailures is the total number of failed health checks.
	TotalFailures uint64 `json:"total_failures"`
}

// BackendInfo contains registration information for a KMS backend.
type BackendInfo struct {
	// Name is the unique identifier for the backend.
	Name string `json:"name"`

	// Type is the backend type (e.g., BackendTypeSoftware, BackendTypePKCS11).
	Type config.BackendType `json:"type"`

	// Priority determines the order for backend selection (lower = higher priority).
	Priority int `json:"priority"`

	// Enabled indicates whether the backend is enabled for use.
	Enabled bool `json:"enabled"`

	// Algorithms lists the cryptographic algorithms supported by this backend.
	Algorithms []string `json:"algorithms"`

	// Health contains the current health status.
	Health BackendHealth `json:"health"`

	// Backend is the actual backend implementation.
	Backend Backend `json:"-"`

	// RegisteredAt is when the backend was registered.
	RegisteredAt time.Time `json:"registered_at"`
}

// RegistryConfig configures the backend registry.
type RegistryConfig struct {
	// HealthCheckInterval is how often to check backend health.
	HealthCheckInterval time.Duration

	// HealthCheckTimeout is the timeout for each health check.
	HealthCheckTimeout time.Duration

	// UnhealthyThreshold is the number of consecutive failures before marking unhealthy.
	UnhealthyThreshold int

	// RecoveryThreshold is the number of consecutive successes before marking healthy.
	RecoveryThreshold int

	// EnableFailover enables automatic failover to backup backends.
	EnableFailover bool

	// Logger for registry operations.
	Logger interfaces.Logger
}

// DefaultRegistryConfig returns the default registry configuration.
func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		UnhealthyThreshold:  3,
		RecoveryThreshold:   2,
		EnableFailover:      true,
	}
}

// BackendRegistry manages KMS backends with health tracking and failover.
type BackendRegistry struct {
	backends map[string]*BackendInfo
	mu       sync.RWMutex
	config   *RegistryConfig
	logger   interfaces.Logger

	// Health check state
	stopCh  chan struct{}
	doneCh  chan struct{}
	running atomic.Bool
	wg      sync.WaitGroup

	// Metrics
	totalHealthChecks atomic.Uint64
	totalFailedChecks atomic.Uint64
	totalFailovers    atomic.Uint64
}

// NewBackendRegistry creates a new backend registry.
func NewBackendRegistry(config *RegistryConfig) *BackendRegistry {
	if config == nil {
		config = DefaultRegistryConfig()
	}

	return &BackendRegistry{
		backends: make(map[string]*BackendInfo),
		config:   config,
		logger:   config.Logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Register adds a backend to the registry.
func (r *BackendRegistry) Register(name string, backend Backend, priority int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.backends[name]; exists {
		return &RegistryError{
			Code:    ErrBackendExists,
			Op:      "Register",
			Backend: name,
			Message: "backend already registered",
		}
	}

	// Get supported algorithms
	ctx, cancel := context.WithTimeout(context.Background(), r.config.HealthCheckTimeout)
	defer cancel()

	algorithms := backend.SupportedAlgorithms(ctx)

	r.backends[name] = &BackendInfo{
		Name:       name,
		Type:       backend.Type(),
		Priority:   priority,
		Enabled:    true,
		Algorithms: algorithms,
		Backend:    backend,
		Health: BackendHealth{
			Status: HealthStatusUnknown,
		},
		RegisteredAt: time.Now(),
	}

	if r.logger != nil {
		r.logger.WithFields(map[string]interface{}{
			"backend":    name,
			"type":       backend.Type(),
			"priority":   priority,
			"algorithms": len(algorithms),
		}).Info("Registered KMS backend")
	}

	return nil
}

// Unregister removes a backend from the registry.
func (r *BackendRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, exists := r.backends[name]
	if !exists {
		return &RegistryError{
			Code:    ErrBackendNotFound,
			Op:      "Unregister",
			Backend: name,
			Message: "backend not found",
		}
	}

	// Close the backend
	if err := info.Backend.Close(); err != nil {
		if r.logger != nil {
			r.logger.WithFields(map[string]interface{}{
				"backend": name,
				"error":   err.Error(),
			}).Warn("Error closing backend during unregister")
		}
	}

	delete(r.backends, name)

	if r.logger != nil {
		r.logger.WithFields(map[string]interface{}{
			"backend": name,
		}).Info("Unregistered KMS backend")
	}

	return nil
}

// Get returns a backend by name.
func (r *BackendRegistry) Get(name string) (Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.backends[name]
	if !exists {
		return nil, &RegistryError{
			Code:    ErrBackendNotFound,
			Op:      "Get",
			Backend: name,
			Message: "backend not found",
		}
	}

	if !info.Enabled {
		return nil, &RegistryError{
			Code:    ErrBackendDisabled,
			Op:      "Get",
			Backend: name,
			Message: "backend is disabled",
		}
	}

	return info.Backend, nil
}

// GetHealthy returns the highest priority healthy backend.
func (r *BackendRegistry) GetHealthy(ctx context.Context) (Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var selected *BackendInfo

	for _, info := range r.backends {
		if !info.Enabled {
			continue
		}

		// Check if backend is healthy (skip only explicitly unhealthy backends)
		if info.Health.Status == HealthStatusUnhealthy {
			continue
		}

		// Select the highest priority (lowest number)
		if selected == nil || info.Priority < selected.Priority {
			selected = info
		}
	}

	if selected == nil {
		return nil, &RegistryError{
			Code:    ErrNoHealthyBackend,
			Op:      "GetHealthy",
			Message: "no healthy backends available",
		}
	}

	return selected.Backend, nil
}

// GetByAlgorithm returns the highest priority healthy backend that supports the algorithm.
func (r *BackendRegistry) GetByAlgorithm(ctx context.Context, algorithm string) (Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var selected *BackendInfo

	for _, info := range r.backends {
		if !info.Enabled {
			continue
		}

		// Check if backend is healthy (skip only explicitly unhealthy backends)
		if info.Health.Status == HealthStatusUnhealthy {
			continue
		}

		// Check if backend supports the algorithm
		if !containsAlgorithm(info.Algorithms, algorithm) {
			continue
		}

		// Select the highest priority (lowest number)
		if selected == nil || info.Priority < selected.Priority {
			selected = info
		}
	}

	if selected == nil {
		return nil, &RegistryError{
			Code:    ErrNoBackendForAlgorithm,
			Op:      "GetByAlgorithm",
			Message: "no healthy backend supports algorithm: " + algorithm,
		}
	}

	return selected.Backend, nil
}

// Enable enables a backend.
func (r *BackendRegistry) Enable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, exists := r.backends[name]
	if !exists {
		return &RegistryError{
			Code:    ErrBackendNotFound,
			Op:      "Enable",
			Backend: name,
			Message: "backend not found",
		}
	}

	info.Enabled = true

	if r.logger != nil {
		r.logger.WithFields(map[string]interface{}{
			"backend": name,
		}).Info("Enabled KMS backend")
	}

	return nil
}

// Disable disables a backend.
func (r *BackendRegistry) Disable(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, exists := r.backends[name]
	if !exists {
		return &RegistryError{
			Code:    ErrBackendNotFound,
			Op:      "Disable",
			Backend: name,
			Message: "backend not found",
		}
	}

	info.Enabled = false

	if r.logger != nil {
		r.logger.WithFields(map[string]interface{}{
			"backend": name,
		}).Info("Disabled KMS backend")
	}

	return nil
}

// SetPriority updates a backend's priority.
func (r *BackendRegistry) SetPriority(name string, priority int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, exists := r.backends[name]
	if !exists {
		return &RegistryError{
			Code:    ErrBackendNotFound,
			Op:      "SetPriority",
			Backend: name,
			Message: "backend not found",
		}
	}

	info.Priority = priority

	if r.logger != nil {
		r.logger.WithFields(map[string]interface{}{
			"backend":  name,
			"priority": priority,
		}).Info("Updated KMS backend priority")
	}

	return nil
}

// GetHealth returns the health status for a backend.
func (r *BackendRegistry) GetHealth(name string) (*BackendHealth, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, exists := r.backends[name]
	if !exists {
		return nil, &RegistryError{
			Code:    ErrBackendNotFound,
			Op:      "GetHealth",
			Backend: name,
			Message: "backend not found",
		}
	}

	// Return a copy
	health := info.Health
	return &health, nil
}

// GetAllHealth returns health status for all backends.
func (r *BackendRegistry) GetAllHealth() map[string]*BackendHealth {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]*BackendHealth, len(r.backends))
	for name, info := range r.backends {
		health := info.Health
		result[name] = &health
	}
	return result
}

// List returns information about all registered backends.
func (r *BackendRegistry) List() []*BackendInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*BackendInfo, 0, len(r.backends))
	for _, info := range r.backends {
		// Create a copy without the backend reference
		infoCopy := &BackendInfo{
			Name:         info.Name,
			Type:         info.Type,
			Priority:     info.Priority,
			Enabled:      info.Enabled,
			Algorithms:   info.Algorithms,
			Health:       info.Health,
			RegisteredAt: info.RegisteredAt,
		}
		result = append(result, infoCopy)
	}
	return result
}

// ListWithBackends returns information about all registered backends including
// the backend implementations. This is intended for internal use where the
// caller needs to invoke backend methods directly (e.g., health checks).
func (r *BackendRegistry) ListWithBackends() []*BackendInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*BackendInfo, 0, len(r.backends))
	for _, info := range r.backends {
		// Create a copy with the backend reference
		infoCopy := &BackendInfo{
			Name:         info.Name,
			Type:         info.Type,
			Priority:     info.Priority,
			Enabled:      info.Enabled,
			Algorithms:   info.Algorithms,
			Health:       info.Health,
			Backend:      info.Backend,
			RegisteredAt: info.RegisteredAt,
		}
		result = append(result, infoCopy)
	}
	return result
}

// CheckHealth performs an immediate health check on a specific backend.
func (r *BackendRegistry) CheckHealth(ctx context.Context, name string) (*BackendHealth, error) {
	r.mu.Lock()
	info, exists := r.backends[name]
	if !exists {
		r.mu.Unlock()
		return nil, &RegistryError{
			Code:    ErrBackendNotFound,
			Op:      "CheckHealth",
			Backend: name,
			Message: "backend not found",
		}
	}
	backend := info.Backend
	r.mu.Unlock()

	// Perform health check
	health := r.performHealthCheck(ctx, name, backend)

	// Update stored health
	r.mu.Lock()
	if info, exists := r.backends[name]; exists {
		info.Health = *health
	}
	r.mu.Unlock()

	return health, nil
}

// performHealthCheck performs a single health check on a backend.
func (r *BackendRegistry) performHealthCheck(ctx context.Context, name string, backend Backend) *BackendHealth {
	start := time.Now()

	r.totalHealthChecks.Add(1)

	// Get existing health state
	r.mu.RLock()
	info := r.backends[name]
	previousHealth := BackendHealth{}
	if info != nil {
		previousHealth = info.Health
	}
	r.mu.RUnlock()

	// Create timeout context
	checkCtx, cancel := context.WithTimeout(ctx, r.config.HealthCheckTimeout)
	defer cancel()

	// Check availability
	available := backend.IsAvailable(checkCtx)
	latency := time.Since(start)

	health := &BackendHealth{
		LastCheck:   time.Now(),
		Latency:     latency,
		TotalChecks: previousHealth.TotalChecks + 1,
	}

	if available {
		health.LastHealthy = time.Now()
		health.TotalFailures = previousHealth.TotalFailures
		health.ConsecutiveFailures = 0

		// Check if we're recovering from unhealthy state
		switch {
		case previousHealth.Status == HealthStatusUnhealthy:
			health.Status = HealthStatusDegraded
			health.Message = "recovering from unhealthy state"
		case previousHealth.ConsecutiveFailures > 0:
			health.Status = HealthStatusDegraded
			health.Message = "recovering from failures"
		default:
			health.Status = HealthStatusHealthy
			health.Message = "backend is operational"
		}
	} else {
		r.totalFailedChecks.Add(1)
		health.TotalFailures = previousHealth.TotalFailures + 1
		health.ConsecutiveFailures = previousHealth.ConsecutiveFailures + 1
		health.LastHealthy = previousHealth.LastHealthy

		if health.ConsecutiveFailures >= r.config.UnhealthyThreshold {
			health.Status = HealthStatusUnhealthy
			health.Message = "backend unavailable after multiple failures"
		} else {
			health.Status = HealthStatusDegraded
			health.Message = "backend experiencing issues"
		}
	}

	// Log health state changes
	if r.logger != nil && previousHealth.Status != health.Status {
		r.logger.WithFields(map[string]interface{}{
			"backend":    name,
			"old_status": previousHealth.Status,
			"new_status": health.Status,
			"latency_ms": latency.Milliseconds(),
			"failures":   health.ConsecutiveFailures,
		}).Info("KMS backend health status changed")
	}

	return health
}

// StartHealthChecks starts the background health check worker.
func (r *BackendRegistry) StartHealthChecks() {
	if r.running.Swap(true) {
		return // Already running
	}

	r.wg.Add(1)
	go r.healthCheckWorker()

	if r.logger != nil {
		r.logger.WithFields(map[string]interface{}{
			"interval": r.config.HealthCheckInterval,
		}).Info("Started KMS backend health checks")
	}
}

// StopHealthChecks stops the background health check worker.
func (r *BackendRegistry) StopHealthChecks() {
	if !r.running.Swap(false) {
		return // Not running
	}

	close(r.stopCh)
	r.wg.Wait()

	// Reset for potential restart
	r.stopCh = make(chan struct{})

	if r.logger != nil {
		r.logger.Info("Stopped KMS backend health checks")
	}
}

// healthCheckWorker runs periodic health checks on all backends.
func (r *BackendRegistry) healthCheckWorker() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.config.HealthCheckInterval)
	defer ticker.Stop()

	// Perform initial health check
	r.checkAllBackends()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			r.checkAllBackends()
		}
	}
}

// checkAllBackends checks health of all registered backends.
func (r *BackendRegistry) checkAllBackends() {
	r.mu.RLock()
	names := make([]string, 0, len(r.backends))
	backends := make(map[string]Backend)
	for name, info := range r.backends {
		names = append(names, name)
		backends[name] = info.Backend
	}
	r.mu.RUnlock()

	ctx := context.Background()

	for _, name := range names {
		backend := backends[name]
		health := r.performHealthCheck(ctx, name, backend)

		r.mu.Lock()
		if info, exists := r.backends[name]; exists {
			info.Health = *health
		}
		r.mu.Unlock()
	}
}

// Close stops health checks and closes all backends.
func (r *BackendRegistry) Close() error {
	r.StopHealthChecks()

	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error
	for name, info := range r.backends {
		if err := info.Backend.Close(); err != nil {
			lastErr = err
			if r.logger != nil {
				r.logger.WithFields(map[string]interface{}{
					"backend": name,
					"error":   err.Error(),
				}).Warn("Error closing KMS backend")
			}
		}
	}

	r.backends = make(map[string]*BackendInfo)
	return lastErr
}

// Stats returns registry statistics.
func (r *BackendRegistry) Stats() *RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := &RegistryStats{
		TotalBackends:     len(r.backends),
		TotalHealthChecks: r.totalHealthChecks.Load(),
		TotalFailedChecks: r.totalFailedChecks.Load(),
		TotalFailovers:    r.totalFailovers.Load(),
	}

	for _, info := range r.backends {
		if info.Enabled {
			stats.EnabledBackends++
		}
		switch info.Health.Status {
		case HealthStatusHealthy:
			stats.HealthyBackends++
		case HealthStatusDegraded:
			stats.DegradedBackends++
		case HealthStatusUnhealthy:
			stats.UnhealthyBackends++
		}
	}

	return stats
}

// RegistryStats contains registry statistics.
type RegistryStats struct {
	TotalBackends     int    `json:"total_backends"`
	EnabledBackends   int    `json:"enabled_backends"`
	HealthyBackends   int    `json:"healthy_backends"`
	DegradedBackends  int    `json:"degraded_backends"`
	UnhealthyBackends int    `json:"unhealthy_backends"`
	TotalHealthChecks uint64 `json:"total_health_checks"`
	TotalFailedChecks uint64 `json:"total_failed_checks"`
	TotalFailovers    uint64 `json:"total_failovers"`
}

// Helper function to check if an algorithm is in the list.
func containsAlgorithm(algorithms []string, algorithm string) bool {
	for _, a := range algorithms {
		if a == algorithm {
			return true
		}
	}
	return false
}

// Registry error codes.
const (
	ErrBackendExists         = "backend_exists"
	ErrBackendNotFound       = "backend_not_found"
	ErrBackendDisabled       = "backend_disabled"
	ErrNoHealthyBackend      = "no_healthy_backend"
	ErrNoBackendForAlgorithm = "no_backend_for_algorithm"
)

// RegistryError represents an error from the backend registry.
type RegistryError struct {
	Code    string
	Op      string
	Backend string
	Message string
}

func (e *RegistryError) Error() string {
	if e.Backend != "" {
		return e.Op + ": " + e.Backend + ": " + e.Message
	}
	return e.Op + ": " + e.Message
}
