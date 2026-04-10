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

// Package credentials provides dynamic credential generation for databases and services.
package credentials

import (
	"context"
	stderrors "errors"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var (
	// ErrGeneratorNotFound is returned when a generator type is not registered.
	ErrGeneratorNotFound = stderrors.New("credential generator not found")

	// ErrGeneratorAlreadyRegistered is returned when attempting to register a duplicate generator.
	ErrGeneratorAlreadyRegistered = stderrors.New("credential generator already registered")
)

// CredentialGenerator is the interface for generating, revoking, and rotating credentials.
// Implementations provide generator-specific logic for different backends (database, API, etc.).
type CredentialGenerator interface {
	// Type returns the generator type identifier (e.g., "postgres", "mysql", "certificate").
	Type() string

	// ValidateConfig validates the role configuration for this generator.
	// Returns an error if required configuration fields are missing or invalid.
	ValidateConfig(config map[string]string) error

	// Generate creates a new credential based on the role configuration.
	// The credential should be immediately usable and persisted in the backend system.
	Generate(ctx context.Context, role *Role) (*Credential, error)

	// Revoke removes the credential from the backend system.
	// After revocation, the credential should no longer be valid.
	Revoke(ctx context.Context, credential *Credential) error

	// Rotate creates a new credential and revokes the old one atomically.
	// This enables zero-downtime credential rotation.
	Rotate(ctx context.Context, old *Credential, role *Role) (*Credential, error)
}

// GeneratorRegistry manages registered credential generators.
// It provides a central registry for looking up generators by type.
type GeneratorRegistry struct {
	generators map[string]CredentialGenerator
	mu         sync.RWMutex
}

// NewGeneratorRegistry creates a new credential generator registry.
func NewGeneratorRegistry() *GeneratorRegistry {
	return &GeneratorRegistry{
		generators: make(map[string]CredentialGenerator),
	}
}

// Register adds a credential generator to the registry.
// Returns an error if a generator with the same type is already registered.
func (r *GeneratorRegistry) Register(gen CredentialGenerator) error {
	if gen == nil {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "GeneratorRegistry.Register",
			Err:  stderrors.New("generator cannot be nil"),
		}
	}

	genType := gen.Type()
	if genType == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "GeneratorRegistry.Register",
			Err:  stderrors.New("generator type cannot be empty"),
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.generators[genType]; exists {
		return &errors.DragonError{
			Code: errors.ErrAlreadyExists,
			Op:   "GeneratorRegistry.Register",
			Err:  &api.WrappedMessageError{Message: ErrGeneratorAlreadyRegistered.Error(), Cause: ErrGeneratorAlreadyRegistered},
		}
	}

	r.generators[genType] = gen
	return nil
}

// Get retrieves a credential generator by type.
// Returns an error if no generator is registered for the type.
func (r *GeneratorRegistry) Get(genType string) (CredentialGenerator, error) {
	if genType == "" {
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "GeneratorRegistry.Get",
			Err:  stderrors.New("generator type cannot be empty"),
		}
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	gen, exists := r.generators[genType]
	if !exists {
		return nil, &errors.DragonError{
			Code: errors.ErrNotFound,
			Op:   "GeneratorRegistry.Get",
			Err:  ErrGeneratorNotFound,
		}
	}

	return gen, nil
}

// List returns all registered generator types.
func (r *GeneratorRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.generators))
	for genType := range r.generators {
		types = append(types, genType)
	}

	return types
}

// Unregister removes a credential generator from the registry.
func (r *GeneratorRegistry) Unregister(genType string) error {
	if genType == "" {
		return &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "GeneratorRegistry.Unregister",
			Err:  stderrors.New("generator type cannot be empty"),
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.generators[genType]; !exists {
		return &errors.DragonError{
			Code: errors.ErrNotFound,
			Op:   "GeneratorRegistry.Unregister",
			Err:  ErrGeneratorNotFound,
		}
	}

	delete(r.generators, genType)
	return nil
}

// Count returns the number of registered generators.
func (r *GeneratorRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.generators)
}
