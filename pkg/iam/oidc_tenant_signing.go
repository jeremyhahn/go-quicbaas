// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package iam

import (
	"context"
	"sync"

	"github.com/go-jose/go-jose/v4"
)

// OIDCSigningKeyCN is the common name under which the OIDC token signing key is
// stored in the xkms keystore. Both the global key (tenant "") and per-tenant
// dedicated keys use this CN; they are namespaced by the KeyAttributes.TenantID
// so each tenant's dedicated key is a distinct ES256 key with its own derived
// kid under the SAME single broker issuer.
const OIDCSigningKeyCN = "oidc-signing"

// TenantOIDCSettingsProvider resolves a tenant's OIDC issuance overrides so the
// signing-key resolver can decide whether a tenant gets a dedicated per-tenant
// signing key. It is satisfied by the IAM TenantManager.
type TenantOIDCSettingsProvider interface {
	// OIDCSettingsFor returns the tenant's OIDC override, or nil when the
	// tenant configured none. A non-nil error fails issuance closed for a
	// dedicated tenant (the resolver must not silently fall back to the global
	// key, which would weaken the tenant's isolation).
	OIDCSettingsFor(ctx context.Context, tenantID string) (*TenantOIDCConfig, error)

	// DedicatedSigningTenants returns the ids of all tenants that have opted
	// in to a dedicated signing key. The resolver uses it to publish every
	// active dedicated kid in the aggregated JWKS even before a tenant's first
	// token is issued.
	DedicatedSigningTenants(ctx context.Context) ([]string, error)
}

// TenantManagerOIDCSettings adapts a TenantManager into a
// TenantOIDCSettingsProvider.
type TenantManagerOIDCSettings struct {
	tenants TenantManager
}

// NewTenantManagerOIDCSettings wraps a TenantManager as a
// TenantOIDCSettingsProvider.
func NewTenantManagerOIDCSettings(tenants TenantManager) *TenantManagerOIDCSettings {
	return &TenantManagerOIDCSettings{tenants: tenants}
}

// OIDCSettingsFor returns the tenant's OIDC override.
func (a *TenantManagerOIDCSettings) OIDCSettingsFor(ctx context.Context, tenantID string) (*TenantOIDCConfig, error) {
	if a == nil || a.tenants == nil {
		return nil, nil
	}
	tenant, err := a.tenants.GetTenant(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return tenant.Settings.OIDC, nil
}

// DedicatedSigningTenants lists every tenant whose OIDC override opts in to a
// dedicated signing key.
func (a *TenantManagerOIDCSettings) DedicatedSigningTenants(ctx context.Context) ([]string, error) {
	if a == nil || a.tenants == nil {
		return nil, nil
	}
	tenants, err := a.tenants.ListTenants(ctx, ListOptions{})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, t := range tenants {
		if t != nil && t.Settings.OIDC != nil && t.Settings.OIDC.DedicatedSigningKey {
			ids = append(ids, t.ID)
		}
	}
	return ids, nil
}

// tenantKeyBuilder constructs a per-tenant SigningKeyManager. It is a function
// seam so tests can inject a deterministic builder; production uses
// NewSigningKeyManagerFromXKMS bound to the dedicated tenant's namespace.
type tenantKeyBuilder func(tenantID string) (*SigningKeyManager, error)

// TenantSigningKeyResolver maps a tenant id to the SigningKeyManager its tokens
// are signed with. The single broker issuer + tenant_id claim is the default:
// a tenant that has not opted in to a dedicated signing key uses the global
// SigningKeyManager (its tokens carry the global kid). A tenant whose
// TenantOIDCConfig.DedicatedSigningKey is true is signed by its OWN ES256 key
// (own kid) under the SAME issuer, preserving the single trust anchor — the
// aggregated JWKS publishes every active kid so the single-anchor validator
// verifies any of them by kid.
//
// Per-tenant managers are cached in a sync.Map so resolution is lock-free on
// the hot path after the first build. A dedicated tenant whose key cannot be
// built (e.g. a sealed tenant keystore) FAILS CLOSED at issuance: the resolver
// returns the build error rather than silently falling back to the global key,
// which would weaken the tenant's isolation.
type TenantSigningKeyResolver struct {
	global   *SigningKeyManager
	settings TenantOIDCSettingsProvider
	build    tenantKeyBuilder

	// managers caches one *SigningKeyManager per dedicated tenant id.
	managers sync.Map // map[tenantID]*SigningKeyManager

	// buildLocks serialises concurrent builds for the same tenant id so a
	// dedicated tenant's key is constructed once.
	buildLocks sync.Map // map[tenantID]*sync.Mutex
}

// NewTenantSigningKeyResolver constructs a resolver over a required global
// SigningKeyManager and an optional settings provider. When settings is nil
// every tenant uses the global key (the single-key behavior). The production
// per-tenant key builder calls NewSigningKeyManagerFromXKMS with the tenant's
// namespace.
func NewTenantSigningKeyResolver(global *SigningKeyManager, settings TenantOIDCSettingsProvider) (*TenantSigningKeyResolver, error) {
	if global == nil {
		return nil, &SigningKeyUnavailableError{}
	}
	return &TenantSigningKeyResolver{
		global:   global,
		settings: settings,
		build: func(tenantID string) (*SigningKeyManager, error) {
			// Dedicated tenant keys live on the global key's backend (the
			// replicated store on a cluster node); a from-signer global
			// manager has no backend, so it falls back to the default.
			if global.storeType == "" {
				return NewSigningKeyManagerFromXKMS(OIDCSigningKeyCN, tenantID)
			}
			m, err := NewSigningKeyManagerFromXKMSBackend(OIDCSigningKeyCN, tenantID, global.storeType)
			if err != nil {
				return nil, err
			}
			// A tenant migrated to the replicated store keeps its
			// pre-upgrade default-backend key, so its outstanding tokens
			// keep verifying.
			if err := m.AdoptDefaultBackendKey(); err != nil {
				return nil, err
			}
			return m, nil
		},
	}, nil
}

// Global returns the global signing key manager (the default for tenants that
// have not opted in to a dedicated key).
func (r *TenantSigningKeyResolver) Global() *SigningKeyManager {
	return r.global
}

// ManagerFor returns the SigningKeyManager whose key signs tokens for tenantID.
// It returns the global manager for a non-dedicated tenant (the default) and a
// per-tenant manager for a dedicated tenant, building and caching it on first
// use. A dedicated tenant whose key cannot be built fails closed (the build
// error is returned) so issuance does NOT silently downgrade to the global key.
func (r *TenantSigningKeyResolver) ManagerFor(ctx context.Context, tenantID string) (*SigningKeyManager, error) {
	if r.settings == nil || tenantID == "" {
		return r.global, nil
	}

	cfg, err := r.settings.OIDCSettingsFor(ctx, tenantID)
	if err != nil {
		// Settings resolution failed. We cannot prove the tenant is
		// non-dedicated, so default to the global key only when the tenant
		// has no cached dedicated manager; otherwise this is a transient read
		// error for a known-dedicated tenant and we must not weaken it.
		if _, ok := r.managers.Load(tenantID); !ok {
			return r.global, nil
		}
		return r.managerForDedicated(tenantID)
	}
	if cfg == nil || !cfg.DedicatedSigningKey {
		return r.global, nil
	}
	return r.managerForDedicated(tenantID)
}

// loadTypedManager returns the cached *SigningKeyManager for a tenant. A value
// of any other type is reported as absent so the caller rebuilds it.
func loadTypedManager(m *sync.Map, tenantID string) (*SigningKeyManager, bool) {
	v, ok := m.Load(tenantID)
	if !ok {
		return nil, false
	}
	mgr, ok := v.(*SigningKeyManager)
	return mgr, ok
}

// managerForDedicated returns the cached per-tenant manager, building it once
// under a per-tenant lock. A build failure is returned to the caller so the
// dedicated tenant fails closed.
func (r *TenantSigningKeyResolver) managerForDedicated(tenantID string) (*SigningKeyManager, error) {
	if mgr, ok := loadTypedManager(&r.managers, tenantID); ok {
		return mgr, nil
	}

	lockI, _ := r.buildLocks.LoadOrStore(tenantID, &sync.Mutex{})
	lock, _ := lockI.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if mgr, ok := loadTypedManager(&r.managers, tenantID); ok {
		return mgr, nil
	}

	mgr, err := r.build(tenantID)
	if err != nil {
		return nil, &TenantSigningKeyUnavailableError{TenantID: tenantID, Cause: err}
	}
	r.managers.Store(tenantID, mgr)
	return mgr, nil
}

// OnTenantSettingsChanged implements TenantSettingsObserver: it evicts the
// tenant's cached signing-key manager so the next issuance re-resolves whether
// the tenant is dedicated and rebuilds its manager. Evicting a non-dedicated
// (uncached) tenant is a no-op.
func (r *TenantSigningKeyResolver) OnTenantSettingsChanged(tenantID string) {
	r.managers.Delete(tenantID)
}

// JWKS returns the aggregated JSON Web Key Set the OIDC provider publishes
// under its single issuer: the global manager's keys plus every active
// dedicated tenant's keys, each addressable by its derived kid. The resolver
// eagerly builds managers for all dedicated tenants (via the settings provider)
// so a dedicated tenant's kid is published before its first token is issued —
// the single-anchor validator can then verify any tenant's token by kid.
//
// A dedicated tenant whose key cannot be built is skipped (logged by the
// caller, not published) rather than failing the whole JWKS; its issuance path
// still fails closed via ManagerFor.
func (r *TenantSigningKeyResolver) JWKS(ctx context.Context) *jose.JSONWebKeySet {
	out := &jose.JSONWebKeySet{}
	seen := make(map[string]struct{})

	appendUnique := func(set *jose.JSONWebKeySet) {
		if set == nil {
			return
		}
		for i := range set.Keys {
			kid := set.Keys[i].KeyID
			if _, dup := seen[kid]; dup {
				continue
			}
			seen[kid] = struct{}{}
			out.Keys = append(out.Keys, set.Keys[i])
		}
	}

	appendUnique(r.global.JWKS())

	// Ensure every dedicated tenant's manager is built so its kid is published.
	if r.settings != nil {
		if ids, err := r.settings.DedicatedSigningTenants(ctx); err == nil {
			for _, id := range ids {
				mgr, mErr := r.managerForDedicated(id)
				if mErr != nil {
					continue
				}
				appendUnique(mgr.JWKS())
			}
		}
	}

	// Include any already-cached dedicated managers (covers a tenant built via
	// issuance whose listing momentarily failed above).
	r.managers.Range(func(_, v any) bool {
		if mgr, ok := v.(*SigningKeyManager); ok {
			appendUnique(mgr.JWKS())
		}
		return true
	})

	return out
}

// TenantSigningKeyUnavailableError is returned when a dedicated tenant's signing
// key cannot be built (for example because the tenant's keystore is sealed). It
// is the fail-closed signal for per-tenant token issuance: the issuer must not
// downgrade to the global key for a dedicated tenant.
type TenantSigningKeyUnavailableError struct {
	TenantID string
	Cause    error
}

// Error implements the error interface.
func (e *TenantSigningKeyUnavailableError) Error() string {
	msg := "iam: signing key unavailable for dedicated tenant " + e.TenantID
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause.
func (e *TenantSigningKeyUnavailableError) Unwrap() error {
	return e.Cause
}
