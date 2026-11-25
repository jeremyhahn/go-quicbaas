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

// Package kms provides key management service functionality.
package kms

import (
	"context"
	"fmt"
	"sync"
	"time"

	goxkms "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// BackendFactory creates and registers KMS backends with a registry.
// It integrates with go-xkms to create backends based on the XKMS configuration.
type BackendFactory struct {
	logger   interfaces.Logger
	registry *BackendRegistry
	mu       sync.RWMutex

	keyNameStore    goxkms.StorageBackend
	keyNameStoreFor func(backendName string) (goxkms.StorageBackend, error)
}

// BackendFactoryConfig configures the backend factory.
type BackendFactoryConfig struct {
	// Logger for logging backend creation events
	Logger interfaces.Logger

	// EnabledBackends is an explicit list of backend types to enable.
	// If empty, uses all backends registered in go-xkms.
	EnabledBackends []config.BackendType

	// HealthCheckInterval for the registry in seconds (default: 30)
	HealthCheckInterval int

	// KeyNameStore is the persistent go-xkms key store. When set,
	// InitializeFromXKMS refuses a store holding a legacy name that reads as
	// an encoded key ID and marks a clean store; nil skips the check. It also
	// holds the ownership records of the keys the XKMS backends write under
	// encoded names, one per full go-xkms key identity (backend, type,
	// algorithm, name), not per name (xkms_key_owner.go); nil keeps those in
	// memory per backend.
	KeyNameStore goxkms.StorageBackend

	// KeyNameStoreFor, when set, is asked once for each backend the factory
	// registers, and its answer replaces KeyNameStore for that backend. This
	// lets a node-local backend keep its ownership records on the node while
	// a shared backend keeps them on the replicated store. An error fails
	// InitializeFromXKMS.
	KeyNameStoreFor func(backendName string) (goxkms.StorageBackend, error)
}

// NewBackendFactory creates a new backend factory.
// The factory uses go-xkms's initialized backends to populate the registry.
func NewBackendFactory(cfg *BackendFactoryConfig) (*BackendFactory, error) {
	if cfg == nil {
		cfg = &BackendFactoryConfig{}
	}

	// Create registry with health check interval
	healthInterval := cfg.HealthCheckInterval
	if healthInterval <= 0 {
		healthInterval = 30
	}

	registry := NewBackendRegistry(&RegistryConfig{
		HealthCheckInterval: time.Duration(healthInterval) * time.Second,
		Logger:              cfg.Logger,
	})

	factory := &BackendFactory{
		logger:          cfg.Logger,
		registry:        registry,
		keyNameStore:    cfg.KeyNameStore,
		keyNameStoreFor: cfg.KeyNameStoreFor,
	}

	return factory, nil
}

// Registry returns the backend registry.
func (f *BackendFactory) Registry() *BackendRegistry {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.registry
}

// InitializeFromXKMS creates KMS backends from go-xkms's initialized backends.
// This should be called AFTER xkms.Initialize() has been called.
// The enabledBackends list filters which backends to use; if empty, all are used.
func (f *BackendFactory) InitializeFromXKMS(ctx context.Context, enabledBackends []config.BackendType) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if xkms is initialized
	if !goxkms.ServiceIsInitialized() {
		return api.NewXKMSNotInitializedError("BackendFactory.InitializeFromXKMS")
	}

	// Get all backends from go-xkms
	xkmsBackends := goxkms.ServiceBackends()
	if len(xkmsBackends) == 0 {
		return api.NewNoBackendsAvailableError("BackendFactory.InitializeFromXKMS", "go-xkms")
	}

	if f.logger != nil {
		f.logger.WithFields(map[string]interface{}{
			"xkms_backends":  xkmsBackends,
			"enabled_filter": enabledBackends,
		}).Info("Initializing KMS backends from go-xkms")
	}

	// Create enabled set for filtering
	enabledSet := make(map[string]bool)
	if len(enabledBackends) > 0 {
		for _, b := range enabledBackends {
			enabledSet[string(b)] = true
		}
	}

	// Create and register backends
	priority := 1
	var registered []string
	var stores []goxkms.StorageBackend
	for _, backendName := range xkmsBackends {
		// Apply filter if specified
		if len(enabledSet) > 0 && !enabledSet[backendName] {
			if f.logger != nil {
				f.logger.WithFields(map[string]interface{}{
					"backend": backendName,
				}).Debug("Skipping backend (not in enabled list)")
			}
			continue
		}

		// Resolve the ownership store this backend keeps its records in. A
		// chooser error fails closed: the backend is never skipped like a
		// wrapper failure, nor left on an in-memory store.
		store := f.keyNameStore
		if f.keyNameStoreFor != nil {
			var err error
			store, err = f.keyNameStoreFor(backendName)
			if err != nil {
				return fmt.Errorf("ownership store for KMS backend %q: %w", backendName, err)
			}
		}

		// Create XKMSBackend wrapper
		backendType := config.BackendType(backendName)
		backend, err := NewXKMSBackend(&XKMSBackendConfig{
			BackendType:  backendType,
			Logger:       f.logger,
			KeyNameStore: store,
		})
		if err != nil {
			if f.logger != nil {
				f.logger.WithFields(map[string]interface{}{
					"backend": backendName,
					"error":   err.Error(),
				}).Warn("Failed to create XKMSBackend wrapper, skipping")
			}
			continue
		}

		// Register with the registry
		if err := f.registry.Register(backendName, backend, priority); err != nil {
			if f.logger != nil {
				f.logger.WithFields(map[string]interface{}{
					"backend": backendName,
					"error":   err.Error(),
				}).Warn("Failed to register backend, skipping")
			}
			continue
		}

		if f.logger != nil {
			f.logger.WithFields(map[string]interface{}{
				"backend":  backendName,
				"priority": priority,
			}).Info("Registered KMS backend")
		}

		registered = append(registered, backendName)
		stores = append(stores, store)
		priority++
	}

	// Verify at least one backend was registered
	backends := f.registry.List()
	if len(backends) == 0 {
		return api.NewNoBackendsRegisteredError("BackendFactory.InitializeFromXKMS")
	}

	if f.keyNameStoreFor == nil {
		if err := checkXKMSKeyNames(ctx, f.keyNameStore, registered); err != nil {
			return err
		}
		return nil
	}

	// Each resolved store is checked over the full registered list, so its
	// marker means what it means with a single store. A store resolved for
	// several backends returns at once on its marker after the first check.
	for _, s := range stores {
		if err := checkXKMSKeyNames(ctx, s, registered); err != nil {
			return err
		}
	}

	return nil
}

// RegisterBackend manually registers a backend with the registry.
func (f *BackendFactory) RegisterBackend(name string, backend Backend, priority int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if err := f.registry.Register(name, backend, priority); err != nil {
		return api.NewBackendRegistrationError("BackendFactory.RegisterBackend", name, err)
	}

	if f.logger != nil {
		f.logger.WithFields(map[string]interface{}{
			"backend":  name,
			"type":     backend.Type(),
			"priority": priority,
		}).Info("Manually registered KMS backend")
	}

	return nil
}

// Close shuts down the backend factory and its registry.
func (f *BackendFactory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.registry != nil {
		if err := f.registry.Close(); err != nil {
			return api.NewRegistryCloseError("BackendFactory.Close", err)
		}
	}

	return nil
}
