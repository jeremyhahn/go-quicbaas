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

// Package secrets provides secret resolution capabilities for configuration files.
// It supports multiple resolution strategies including literal values, local/remote KMS,
// auto-generated secrets, and encrypted values.
package secrets

import (
	"context"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ResolvedStrategy represents the strategy used to resolve a secret value.
type ResolvedStrategy string

const (
	// StrategyLiteral indicates the value was used as-is without transformation.
	StrategyLiteral ResolvedStrategy = "literal"

	// StrategyLocalKMS indicates the value was resolved using local KMS decryption.
	StrategyLocalKMS ResolvedStrategy = "local_kms"

	// StrategyRemoteKMS indicates the value was resolved using remote KMS decryption.
	StrategyRemoteKMS ResolvedStrategy = "remote_kms"

	// StrategyAutoGen indicates the value was auto-generated.
	StrategyAutoGen ResolvedStrategy = "autogen"

	// StrategyEncrypted indicates the value was resolved from an encrypted source.
	StrategyEncrypted ResolvedStrategy = "encrypted"
)

// SecretValue represents a resolved secret with metadata about its resolution.
type SecretValue struct {
	// Raw is the original value before resolution (e.g., "kms://key-id/encrypted-data").
	Raw string

	// Value is the resolved plaintext value.
	Value string

	// Strategy indicates how the secret was resolved.
	Strategy ResolvedStrategy
}

// SecretResolver defines the interface for resolving secret values from various sources.
// Implementations handle specific URI schemes (e.g., kms://, env://, file://).
type SecretResolver interface {
	// Resolve attempts to resolve the raw secret reference to its plaintext value.
	// Returns a SecretValue containing the resolved value and metadata.
	// Returns an error if resolution fails.
	Resolve(ctx context.Context, raw string) (*SecretValue, error)

	// CanResolve returns true if this resolver can handle the given raw value.
	// Typically checks for a specific URI scheme prefix.
	CanResolve(raw string) bool

	// Scheme returns the URI scheme this resolver handles (e.g., "kms", "env", "file").
	Scheme() string
}

// ResolverRegistry manages a collection of secret resolvers and dispatches
// resolution requests to the appropriate resolver based on the input format.
type ResolverRegistry struct {
	resolvers []SecretResolver
	mu        sync.RWMutex
}

// NewResolverRegistry creates a new empty ResolverRegistry.
func NewResolverRegistry() *ResolverRegistry {
	return &ResolverRegistry{
		resolvers: make([]SecretResolver, 0),
	}
}

// Register adds a SecretResolver to the registry.
// Resolvers are checked in registration order when resolving secrets.
// Thread-safe for concurrent use.
func (r *ResolverRegistry) Register(resolver SecretResolver) {
	if resolver == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolvers = append(r.resolvers, resolver)
}

// Resolve attempts to resolve a raw secret value using registered resolvers.
// It iterates through resolvers in registration order and uses the first one
// that can handle the input. If no resolver matches, the raw value is returned
// as a literal with StrategyLiteral.
//
// Returns an error if a matching resolver fails during resolution.
// Thread-safe for concurrent use.
func (r *ResolverRegistry) Resolve(ctx context.Context, raw string) (*SecretValue, error) {
	if ctx == nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "ResolverRegistry.Resolve",
			Err:  ErrNilContext,
		}
	}

	r.mu.RLock()
	resolvers := make([]SecretResolver, len(r.resolvers))
	copy(resolvers, r.resolvers)
	r.mu.RUnlock()

	for _, resolver := range resolvers {
		if resolver.CanResolve(raw) {
			value, err := resolver.Resolve(ctx, raw)
			if err != nil {
				return nil, &errors.DragonError{
					Code: errors.ErrConfiguration,
					Op:   "ResolverRegistry.Resolve",
					Err:  err,
					Context: map[string]interface{}{
						"scheme": resolver.Scheme(),
						"raw":    raw,
					},
				}
			}
			return value, nil
		}
	}

	// No resolver matched, treat as literal value
	return &SecretValue{
		Raw:      raw,
		Value:    raw,
		Strategy: StrategyLiteral,
	}, nil
}

// IsSecretURI returns true if any registered resolver can handle the given raw value.
// This is useful for determining if a configuration value needs resolution.
// Thread-safe for concurrent use.
func (r *ResolverRegistry) IsSecretURI(raw string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, resolver := range r.resolvers {
		if resolver.CanResolve(raw) {
			return true
		}
	}
	return false
}

// Resolvers returns a copy of all registered resolvers.
// Thread-safe for concurrent use.
func (r *ResolverRegistry) Resolvers() []SecretResolver {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]SecretResolver, len(r.resolvers))
	copy(result, r.resolvers)
	return result
}
