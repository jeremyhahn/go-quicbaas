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
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Routing Types
// =============================================================================

// RoutingConfig configures how operations are routed to backends.
type RoutingConfig struct {
	// DefaultBackend is the backend name to use when no routing rule applies.
	// If empty, the highest priority healthy backend is used.
	DefaultBackend string

	// FailoverEnabled enables automatic failover to backup backends on failure.
	// Default: true
	FailoverEnabled bool

	// MaxFailoverAttempts limits failover attempts before returning an error.
	// Default: 3
	MaxFailoverAttempts int

	// AlgorithmRouting maps algorithms to preferred backend names.
	// Example: {"CHACHA20-POLY1305": "software", "RSA-4096": "hsm"}
	AlgorithmRouting map[string]string

	// BackendTypeRouting maps backend types to preferred backend names.
	// Example: {BackendTypePKCS11: "hsm-primary"}
	BackendTypeRouting map[config.BackendType]string
}

// DefaultRoutingConfig returns the default routing configuration.
func DefaultRoutingConfig() *RoutingConfig {
	return &RoutingConfig{
		FailoverEnabled:     true,
		MaxFailoverAttempts: 3,
		AlgorithmRouting:    make(map[string]string),
		BackendTypeRouting:  make(map[config.BackendType]string),
	}
}

// ParsedKeyID contains the parsed components of a structured key ID.
// Key ID format: "backend:keytype:algorithm:keyname"
// Examples:
//   - "hsm:signing:ecdsa-p256:my-signing-key"
//   - "software:encryption:aes-256-gcm:data-key"
//   - "vault:master:rsa-4096:root-key"
//   - "my-simple-key" (unstructured, uses default backend)
type ParsedKeyID struct {
	// Backend is the target backend name (e.g., "hsm", "software", "vault").
	// Empty if the key ID is unstructured.
	Backend string

	// KeyType is the purpose of the key (e.g., "signing", "encryption", "master").
	// Empty if the key ID is unstructured.
	KeyType string

	// Algorithm is the cryptographic algorithm (e.g., "ecdsa-p256", "aes-256-gcm").
	// Empty if the key ID is unstructured.
	Algorithm string

	// KeyName is the user-provided key name.
	KeyName string

	// Raw is the original key ID string.
	Raw string

	// IsStructured indicates whether the key ID follows the structured format.
	IsStructured bool
}

// ParseKeyID parses a key ID into its components.
// Supports both structured format "backend:keytype:algorithm:keyname"
// and simple unstructured key names.
func ParseKeyID(keyID string) *ParsedKeyID {
	parsed := &ParsedKeyID{
		Raw:     keyID,
		KeyName: keyID, // Default: entire ID is the key name
	}

	parts := strings.Split(keyID, ":")
	if len(parts) >= 4 {
		// Structured format: backend:keytype:algorithm:keyname
		parsed.Backend = parts[0]
		parsed.KeyType = parts[1]
		parsed.Algorithm = parts[2]
		// Key name is everything after the third colon (may contain colons)
		parsed.KeyName = strings.Join(parts[3:], ":")
		parsed.IsStructured = true
	}

	return parsed
}

// =============================================================================
// Backend Router
// =============================================================================

// FailoverExhaustedError is returned when all failover attempts have been exhausted.
type FailoverExhaustedError struct {
	Attempts int
	LastErr  error
}

// Error implements the error interface.
func (e *FailoverExhaustedError) Error() string {
	msg := "all failover attempts failed after " + strconv.Itoa(e.Attempts) + " tries"
	if e.LastErr != nil {
		msg += ": " + e.LastErr.Error()
	}
	return msg
}

// Unwrap returns the last error encountered during failover.
func (e *FailoverExhaustedError) Unwrap() error {
	return e.LastErr
}

// BackendRouter routes KMS operations to appropriate backends.
// It supports:
//   - Key ID prefix routing (structured key IDs)
//   - Algorithm-based routing
//   - Priority-based selection
//   - Automatic failover on backend failures
type BackendRouter struct {
	registry       *BackendRegistry
	config         *RoutingConfig
	defaultBackend Backend // Fallback when registry is not used
	logger         interfaces.Logger
}

// RouterOption configures the BackendRouter.
type RouterOption func(*BackendRouter)

// WithDefaultBackend sets a default backend for fallback.
func WithDefaultBackend(backend Backend) RouterOption {
	return func(r *BackendRouter) {
		r.defaultBackend = backend
	}
}

// WithLogger sets the logger for the router.
func WithLogger(logger interfaces.Logger) RouterOption {
	return func(r *BackendRouter) {
		r.logger = logger
	}
}

// NewBackendRouter creates a new backend router.
func NewBackendRouter(registry *BackendRegistry, config *RoutingConfig, opts ...RouterOption) *BackendRouter {
	if config == nil {
		config = DefaultRoutingConfig()
	}

	r := &BackendRouter{
		registry: registry,
		config:   config,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Route selects a backend for an operation based on routing rules.
// The routing priority is:
//  1. Key ID prefix (if structured key ID)
//  2. Algorithm routing (if algorithm specified)
//  3. Default backend (if configured)
//  4. Highest priority healthy backend from registry
//  5. Fallback default backend
func (r *BackendRouter) Route(ctx context.Context, keyID string, algorithm string) (Backend, error) {
	// Parse the key ID to check for structured routing
	parsed := ParseKeyID(keyID)

	// 1. Route by key ID prefix if structured
	if parsed.IsStructured && parsed.Backend != "" {
		backend, err := r.routeByBackendName(ctx, parsed.Backend)
		if err == nil {
			return backend, nil
		}
		// Log but continue to try other routing methods
		if r.logger != nil {
			r.logger.WithFields(map[string]interface{}{
				"key_id":  keyID,
				"backend": parsed.Backend,
				"error":   err.Error(),
			}).Debug("Backend from key ID not available, trying fallback")
		}
	}

	// 2. Route by algorithm if specified
	if algorithm != "" {
		// Check algorithm routing config first
		if backendName, ok := r.config.AlgorithmRouting[algorithm]; ok {
			backend, err := r.routeByBackendName(ctx, backendName)
			if err == nil {
				return backend, nil
			}
		}

		// Try registry's algorithm-based routing
		if r.registry != nil {
			backend, err := r.registry.GetByAlgorithm(ctx, algorithm)
			if err == nil {
				return backend, nil
			}
		}
	}

	// 3. Use default backend if configured
	if r.config.DefaultBackend != "" {
		backend, err := r.routeByBackendName(ctx, r.config.DefaultBackend)
		if err == nil {
			return backend, nil
		}
	}

	// 4. Get highest priority healthy backend from registry
	if r.registry != nil {
		backend, err := r.registry.GetHealthy(ctx)
		if err == nil {
			return backend, nil
		}
	}

	// 5. Use fallback default backend
	if r.defaultBackend != nil {
		return r.defaultBackend, nil
	}

	return nil, &errors.QBError{
		Code: errors.ErrUnavailable,
		Op:   "kms.BackendRouter.Route",
		Err:  ErrNoBackendAvailable,
	}
}

// RouteByKeyID routes an operation based solely on the key ID.
// This is useful for operations that don't specify an algorithm.
func (r *BackendRouter) RouteByKeyID(ctx context.Context, keyID string) (Backend, error) {
	return r.Route(ctx, keyID, "")
}

// RouteByAlgorithm routes an operation based solely on the algorithm.
// This is useful for key generation where no key ID exists yet.
func (r *BackendRouter) RouteByAlgorithm(ctx context.Context, algorithm string) (Backend, error) {
	return r.Route(ctx, "", algorithm)
}

// RouteWithFailover routes with automatic failover on failure.
// If the primary backend fails, it tries alternative backends.
func (r *BackendRouter) RouteWithFailover(ctx context.Context, keyID, algorithm string, operation func(Backend) error) error {
	if !r.config.FailoverEnabled {
		// No failover, just route and execute
		backend, err := r.Route(ctx, keyID, algorithm)
		if err != nil {
			return err
		}
		return operation(backend)
	}

	// Get all eligible backends for failover
	backends := r.getFailoverCandidates(ctx, algorithm)
	if len(backends) == 0 {
		return &errors.QBError{
			Code: errors.ErrUnavailable,
			Op:   "kms.BackendRouter.RouteWithFailover",
			Err:  ErrNoFailoverBackends,
		}
	}

	var lastErr error
	attempts := 0
	maxAttempts := r.config.MaxFailoverAttempts
	if maxAttempts <= 0 {
		maxAttempts = len(backends)
	}

	for _, backend := range backends {
		if attempts >= maxAttempts {
			break
		}
		attempts++

		err := operation(backend)
		if err == nil {
			return nil
		}

		lastErr = err
		if r.logger != nil {
			r.logger.WithFields(map[string]interface{}{
				"backend": backend.Type(),
				"attempt": attempts,
				"error":   err.Error(),
			}).Warn("Backend operation failed, trying failover")
		}
	}

	return &errors.QBError{
		Code: errors.ErrUnavailable,
		Op:   "kms.BackendRouter.RouteWithFailover",
		Err:  &FailoverExhaustedError{Attempts: attempts, LastErr: lastErr},
	}
}

// routeByBackendName gets a backend by name from the registry.
func (r *BackendRouter) routeByBackendName(ctx context.Context, name string) (Backend, error) {
	if r.registry == nil {
		return nil, ErrRegistryUnavailable
	}
	return r.registry.Get(name)
}

// getFailoverCandidates returns backends suitable for failover, sorted by priority.
func (r *BackendRouter) getFailoverCandidates(ctx context.Context, algorithm string) []Backend {
	if r.registry == nil {
		if r.defaultBackend != nil {
			return []Backend{r.defaultBackend}
		}
		return nil
	}

	// Get all enabled and healthy backends (with backend references)
	infos := r.registry.ListWithBackends()
	var candidates []Backend

	for _, info := range infos {
		if !info.Enabled || info.Backend == nil {
			continue
		}
		// Accept healthy, degraded, or unknown (newly registered) backends
		if info.Health.Status == HealthStatusUnhealthy {
			continue
		}
		// If algorithm specified, filter by support
		if algorithm != "" && !containsAlgorithm(info.Algorithms, algorithm) {
			continue
		}
		candidates = append(candidates, info.Backend)
	}

	// Add default backend as last resort if not already included
	if r.defaultBackend != nil {
		found := false
		for _, b := range candidates {
			if b == r.defaultBackend {
				found = true
				break
			}
		}
		if !found {
			candidates = append(candidates, r.defaultBackend)
		}
	}

	return candidates
}

// GetRegistry returns the underlying backend registry.
func (r *BackendRouter) GetRegistry() *BackendRegistry {
	return r.registry
}

// GetConfig returns the routing configuration.
func (r *BackendRouter) GetConfig() *RoutingConfig {
	return r.config
}
