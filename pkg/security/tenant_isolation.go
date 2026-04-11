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
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// errInvalidCredentials is returned when the creds argument passed to
// InitializeTenant or UnsealTenant cannot be asserted to qrdbsdk.Credentials.
var errInvalidCredentials = errors.New("tenant isolation: creds must be a qrdbsdk.Credentials value")

// TenantIsolationManager abstracts tenant isolation strategy.
// Implementations cover both keyspace-prefix isolation (zero overhead,
// backward compatible) and cryptographic barrier isolation (per-tenant
// encryption via go-qrdb's BarrierRegistry).
type TenantIsolationManager interface {
	// Mode returns the active isolation mode ("keyspace" or "barrier").
	Mode() string

	// RegisterTenant registers a new tenant with the isolation layer.
	// For keyspace mode this is a no-op. For barrier mode it creates
	// a new TenantBarrier inside the BarrierRegistry.
	RegisterTenant(ctx context.Context, tenantID string) error

	// UnregisterTenant removes a tenant from the isolation layer.
	// For barrier mode it seals and removes the tenant's barrier.
	UnregisterTenant(ctx context.Context, tenantID string) error

	// InitializeTenant initializes the tenant's cryptographic state.
	// For barrier mode this generates the tenant's root key and seals it
	// using the configured strategy. creds must be a qrdbsdk.Credentials
	// value; it is ignored in keyspace mode.
	InitializeTenant(ctx context.Context, tenantID string, creds interface{}) error

	// SealTenant transitions the tenant barrier to the sealed state.
	// No-op in keyspace mode.
	SealTenant(ctx context.Context, tenantID string) error

	// UnsealTenant unseals the tenant barrier. creds must be a
	// qrdbsdk.Credentials value; it is ignored in keyspace mode.
	UnsealTenant(ctx context.Context, tenantID string, creds interface{}) error

	// IsTenantSealed reports whether the tenant barrier is sealed.
	// Always returns false in keyspace mode.
	IsTenantSealed(ctx context.Context, tenantID string) (bool, error)
}

// =============================================================================
// KeyspaceIsolationManager — default, backward-compatible implementation
// =============================================================================

// keyspaceIsolationManager implements TenantIsolationManager using key prefixes.
// All operations are no-ops because isolation is enforced by the KeyspaceResolver.
type keyspaceIsolationManager struct{}

// Compile-time interface check.
var _ TenantIsolationManager = (*keyspaceIsolationManager)(nil)

// NewKeyspaceIsolationManager returns a TenantIsolationManager that relies on
// key-prefix isolation (__t:{tenantID}:). This is the default, zero-overhead mode.
func NewKeyspaceIsolationManager() TenantIsolationManager {
	return &keyspaceIsolationManager{}
}

func (k *keyspaceIsolationManager) Mode() string { return "keyspace" }

func (k *keyspaceIsolationManager) RegisterTenant(_ context.Context, _ string) error {
	return nil
}

func (k *keyspaceIsolationManager) UnregisterTenant(_ context.Context, _ string) error {
	return nil
}

func (k *keyspaceIsolationManager) InitializeTenant(_ context.Context, _ string, _ interface{}) error {
	return nil
}

func (k *keyspaceIsolationManager) SealTenant(_ context.Context, _ string) error {
	return nil
}

func (k *keyspaceIsolationManager) UnsealTenant(_ context.Context, _ string, _ interface{}) error {
	return nil
}

func (k *keyspaceIsolationManager) IsTenantSealed(_ context.Context, _ string) (bool, error) {
	return false, nil
}

// =============================================================================
// BarrierIsolationManager — cryptographic barrier per tenant
// =============================================================================

// barrierIsolationManager implements TenantIsolationManager using go-qrdb's
// BarrierRegistry. Each tenant gets its own StorageBarrier so all data written
// for that tenant is encrypted under a tenant-specific root key.
type barrierIsolationManager struct {
	registry *qrdbsdk.BarrierRegistry
}

// Compile-time interface check.
var _ TenantIsolationManager = (*barrierIsolationManager)(nil)

// NewBarrierIsolationManager returns a TenantIsolationManager backed by the
// supplied BarrierRegistry. The registry must not be nil.
func NewBarrierIsolationManager(registry *qrdbsdk.BarrierRegistry) TenantIsolationManager {
	return &barrierIsolationManager{registry: registry}
}

func (b *barrierIsolationManager) Mode() string { return "barrier" }

// RegisterTenant creates a new TenantBarrier for tenantID inside the registry.
// Uses RegisterTenantDefault which inherits the system barrier's strategies and
// allocates an in-memory backend scoped to the tenant.
func (b *barrierIsolationManager) RegisterTenant(_ context.Context, tenantID string) error {
	_, err := b.registry.RegisterTenantDefault(tenantID)
	return err
}

// UnregisterTenant seals and removes the tenant barrier from the registry.
func (b *barrierIsolationManager) UnregisterTenant(_ context.Context, tenantID string) error {
	return b.registry.UnregisterTenant(tenantID)
}

// InitializeTenant generates and seals the tenant's root key. creds must be a
// qrdbsdk.Credentials value.
func (b *barrierIsolationManager) InitializeTenant(ctx context.Context, tenantID string, creds interface{}) error {
	c, ok := creds.(qrdbsdk.Credentials)
	if !ok {
		return errInvalidCredentials
	}
	return b.registry.InitializeTenant(ctx, tenantID, c)
}

// SealTenant seals the tenant barrier.
func (b *barrierIsolationManager) SealTenant(_ context.Context, tenantID string) error {
	return b.registry.SealTenant(tenantID)
}

// UnsealTenant unseals the tenant barrier. creds must be a qrdbsdk.Credentials value.
func (b *barrierIsolationManager) UnsealTenant(ctx context.Context, tenantID string, creds interface{}) error {
	c, ok := creds.(qrdbsdk.Credentials)
	if !ok {
		return errInvalidCredentials
	}
	return b.registry.UnsealTenant(ctx, tenantID, c)
}

// IsTenantSealed reports whether the tenant barrier is in the sealed state.
func (b *barrierIsolationManager) IsTenantSealed(_ context.Context, tenantID string) (bool, error) {
	tb, err := b.registry.GetTenant(tenantID)
	if err != nil {
		return false, err
	}
	return tb.IsSealed(), nil
}
