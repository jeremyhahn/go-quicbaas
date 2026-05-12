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

package inference

import (
	"context"
	"sort"
	"sync"
)

// Factory constructs an APIInferenceBackend from a Config. Adapter packages
// register a Factory under their provider name during init(); the daemon
// later calls Registry.Create per configured ProviderConfig.
type Factory func(ctx context.Context, cfg Config) (APIInferenceBackend, error)

// Registry is a thread-safe map of provider name to Factory. Lookup is
// O(1) via map-based dispatch — there is no big switch over provider names.
//
// Concurrency: registration happens at process init and reconfiguration time
// (rare, write-heavy bursts), while lookup happens at boot per configured
// provider (rare, read-only). A sync.RWMutex is the simplest correct fit
// here. sync.Map would be appropriate if Range happened on the hot path
// (which it does not) and would force interface{} boxing of the Factory.
// Keeping a typed map is clearer.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// NewRegistry returns an empty Registry. Adapters typically register against
// the package-level Default registry instead.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[string]Factory),
	}
}

// Register associates a Factory with the given provider name. Returns
// ProviderAlreadyRegisteredError if the name is already taken, ErrEmptyProvider
// if name is empty, and ErrNilFactory if factory is nil.
func (r *Registry) Register(provider string, factory Factory) error {
	if provider == "" {
		return ErrEmptyProvider
	}
	if factory == nil {
		return ErrNilFactory
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[provider]; exists {
		return &ProviderAlreadyRegisteredError{Provider: provider}
	}
	r.factories[provider] = factory
	return nil
}

// MustRegister is the panic-on-error variant of Register, intended for
// adapter init() functions where a programming error is unrecoverable.
func (r *Registry) MustRegister(provider string, factory Factory) {
	if err := r.Register(provider, factory); err != nil {
		panic(err)
	}
}

// Unregister removes a factory. Returns ProviderNotRegisteredError if no
// factory is registered under that name. Primarily useful for tests.
func (r *Registry) Unregister(provider string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.factories[provider]; !exists {
		return &ProviderNotRegisteredError{Provider: provider}
	}
	delete(r.factories, provider)
	return nil
}

// Create dispatches to the factory registered under cfg.Provider and returns
// the resulting backend. Returns ProviderNotRegisteredError when no factory
// matches cfg.Provider, or InvalidProviderConfigError when cfg fails the
// pre-dispatch validation enforced here.
func (r *Registry) Create(ctx context.Context, cfg Config) (APIInferenceBackend, error) {
	if cfg.Provider == "" {
		return nil, &InvalidProviderConfigError{
			Field:  "provider",
			Reason: "must not be empty",
		}
	}

	r.mu.RLock()
	factory, ok := r.factories[cfg.Provider]
	r.mu.RUnlock()

	if !ok {
		return nil, &ProviderNotRegisteredError{Provider: cfg.Provider}
	}

	return factory(ctx, cfg)
}

// Providers returns the list of registered provider names in sorted order.
// The result is a fresh slice; callers may mutate it freely.
func (r *Registry) Providers() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.factories))
	for name := range r.factories {
		names = append(names, name)
	}
	r.mu.RUnlock()

	sort.Strings(names)
	return names
}

// HasProvider reports whether a factory is registered under the given name.
func (r *Registry) HasProvider(provider string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.factories[provider]
	return ok
}

// Default is the package-level Registry that adapters should register against
// from their init() functions. Production code (the daemon) calls Default
// during boot to construct backends for the configured providers.
//
// Today Default is empty: zero adapters ship in this package.
var Default = NewRegistry()

// Register associates a Factory with a provider name in the Default registry.
// Convenience wrapper for the common adapter-init pattern.
func Register(provider string, factory Factory) error {
	return Default.Register(provider, factory)
}

// MustRegister is the panic-on-error variant of Register against the Default
// registry.
func MustRegister(provider string, factory Factory) {
	Default.MustRegister(provider, factory)
}

// Create dispatches to the Default registry.
func Create(ctx context.Context, cfg Config) (APIInferenceBackend, error) {
	return Default.Create(ctx, cfg)
}

// Providers returns the registered provider names in the Default registry.
func Providers() []string {
	return Default.Providers()
}
