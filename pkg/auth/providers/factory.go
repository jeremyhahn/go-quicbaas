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

package providers

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ProviderType represents the authorization provider type.
type ProviderType string

const (
	// ProviderTypeRBACFactory is the RBAC provider type for factory use.
	ProviderTypeRBACFactory ProviderType = ProviderTypeRBAC

	// ProviderTypeABACFactory is the ABAC provider type for factory use.
	ProviderTypeABACFactory ProviderType = "abac"

	// ProviderTypeHybridFactory is the hybrid provider type combining RBAC and ABAC.
	ProviderTypeHybridFactory ProviderType = ProviderTypeHybrid
)

// Sentinel errors for the factory.
var (
	// ErrABACNotAvailable is returned when ABAC is requested but the iam
	// build tag was not set during compilation.
	ErrABACNotAvailable = &errors.DragonError{
		Code: errors.ErrUnavailable,
		Op:   "providers.factory",
		Err:  errABACNotAvailable,
	}

	// ErrInvalidProviderType is returned when an unknown provider type is requested.
	ErrInvalidProviderType = &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   "providers.factory",
		Err:  errInvalidProviderType,
	}

	// ErrNilConfig is returned when the factory config is nil.
	ErrNilConfig = &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   "providers.factory",
		Err:  errNilConfig,
	}

	// ErrRoleManagerRequired is returned when RoleManager is required but not provided.
	ErrRoleManagerRequired = &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   "providers.factory",
		Err:  errRoleManagerRequired,
	}

	// Internal sentinel errors for wrapping.
	errABACNotAvailable    = &factoryError{msg: "ABAC provider not available: build without iam tag"}
	errInvalidProviderType = &factoryError{msg: "invalid provider type"}
	errNilConfig           = &factoryError{msg: "factory config cannot be nil"}
	errRoleManagerRequired = &factoryError{msg: "RoleManager is required for RBAC and Hybrid providers"}
)

// factoryError is an internal error type for factory errors.
type factoryError struct {
	msg string
}

// Error implements the error interface.
func (e *factoryError) Error() string {
	return e.msg
}

// FactoryConfig holds configuration for creating authorization providers.
type FactoryConfig struct {
	// Type specifies the provider type to create.
	Type ProviderType

	// RoleManager is required for RBAC and Hybrid providers.
	// It provides role-based permission checks.
	RoleManager interfaces.RoleManager

	// TenantID is the tenant identifier for ABAC providers in multi-tenant deployments.
	// Optional for RBAC providers.
	TenantID string

	// Logger is the logger instance for the provider.
	// If nil, a default logger will be created.
	Logger *logger.Logger
}

// NewProvider creates an authorization provider based on configuration.
//
// Provider types:
//   - ProviderTypeRBACFactory: Creates an RBAC provider using the RoleManager
//   - ProviderTypeABACFactory: Creates an ABAC provider (requires iam build tag)
//   - ProviderTypeHybridFactory: Creates a hybrid provider combining RBAC and ABAC
//
// Returns:
//   - interfaces.AuthorizationProvider: The configured provider
//   - error: ErrNilConfig, ErrInvalidProviderType, ErrRoleManagerRequired, or ErrABACNotAvailable
func NewProvider(cfg *FactoryConfig) (interfaces.AuthorizationProvider, error) {
	if cfg == nil {
		return nil, ErrNilConfig
	}

	switch cfg.Type {
	case ProviderTypeRBACFactory:
		return createRBACProvider(cfg)

	case ProviderTypeABACFactory:
		return createABACProvider(cfg)

	case ProviderTypeHybridFactory:
		return createHybridProvider(cfg)

	default:
		return nil, &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "providers.NewProvider",
			Err:  &factoryError{msg: "unknown provider type: " + string(cfg.Type)},
		}
	}
}

// createRBACProvider creates an RBAC authorization provider.
func createRBACProvider(cfg *FactoryConfig) (interfaces.AuthorizationProvider, error) {
	if cfg.RoleManager == nil {
		return nil, ErrRoleManagerRequired
	}

	return NewRBACProvider(cfg.RoleManager, cfg.Logger)
}

// createHybridProvider creates a hybrid authorization provider.
// This provider checks ABAC first, then falls back to RBAC if ABAC denies.
func createHybridProvider(cfg *FactoryConfig) (interfaces.AuthorizationProvider, error) {
	if cfg.RoleManager == nil {
		return nil, ErrRoleManagerRequired
	}

	// Check if ABAC is available
	if !IsABACAvailable() {
		return nil, ErrABACNotAvailable
	}

	// Create RBAC provider first
	rbacProvider, err := NewRBACProvider(cfg.RoleManager, cfg.Logger)
	if err != nil {
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "providers.createHybridProvider",
			Err:  err,
		}
	}

	// Create ABAC provider
	abacProvider, err := createABACProvider(cfg)
	if err != nil {
		// Close RBAC provider on failure
		_ = rbacProvider.Close()
		return nil, &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "providers.createHybridProvider",
			Err:  err,
		}
	}

	// Create hybrid provider wrapping both
	return NewHybridProvider(rbacProvider, abacProvider, cfg.Logger)
}

// ValidateConfig validates the factory configuration.
// Returns nil if the configuration is valid.
func ValidateConfig(cfg *FactoryConfig) error {
	if cfg == nil {
		return ErrNilConfig
	}

	switch cfg.Type {
	case ProviderTypeRBACFactory:
		if cfg.RoleManager == nil {
			return ErrRoleManagerRequired
		}

	case ProviderTypeABACFactory:
		if !IsABACAvailable() {
			return ErrABACNotAvailable
		}

	case ProviderTypeHybridFactory:
		if cfg.RoleManager == nil {
			return ErrRoleManagerRequired
		}
		if !IsABACAvailable() {
			return ErrABACNotAvailable
		}

	default:
		return ErrInvalidProviderType
	}

	return nil
}

// String returns the string representation of the provider type.
func (pt ProviderType) String() string {
	return string(pt)
}

// IsValid checks if the provider type is valid.
func (pt ProviderType) IsValid() bool {
	switch pt {
	case ProviderTypeRBACFactory, ProviderTypeABACFactory, ProviderTypeHybridFactory:
		return true
	default:
		return false
	}
}

// ParseProviderType parses a string into a ProviderType.
// Returns an error if the string is not a valid provider type.
func ParseProviderType(s string) (ProviderType, error) {
	pt := ProviderType(s)
	if !pt.IsValid() {
		return "", &errors.DragonError{
			Code: errors.ErrInvalidInput,
			Op:   "providers.ParseProviderType",
			Err:  &factoryError{msg: "invalid provider type: " + s},
		}
	}
	return pt, nil
}
