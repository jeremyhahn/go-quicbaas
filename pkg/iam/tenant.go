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

package iam

import (
	"context"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/database"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Resource quota names for tenant quota management.
const (
	QuotaResourceUsers    = "users"
	QuotaResourceGroups   = "groups"
	QuotaResourceRoles    = "roles"
	QuotaResourcePolicies = "policies"
	QuotaResourceApps     = "apps"
	QuotaResourceStorage  = "storage"
)

// QRDBTenantProvisioner abstracts the qrdb engine's tenant lifecycle so the
// IAM layer can fire engine-side tenant provisioning (barrier init, DEK, quota
// bootstrap) without importing go-qrdb packages directly. Implementations
// must be safe for concurrent use.
type QRDBTenantProvisioner interface {
	// CreateTenant provisions a new tenant in the qrdb engine. name is the
	// human-readable display name. isolationType selects the engine
	// isolation strategy: tenant.IsolationBarrier gives the tenant its own
	// DEK and dedicated "{tenantID}/" namespace; an empty string defers to
	// the engine default (prefix). The reserved system tenant must use
	// prefix isolation.
	CreateTenant(ctx context.Context, id, name, isolationType string) error

	// DeleteTenant removes the tenant from the qrdb engine, destroying the
	// per-tenant barrier and purging its DEK.
	DeleteTenant(ctx context.Context, id string) error

	// UpdateQuotas updates the engine-side resource quotas for the tenant
	// identified by id. These are the low-level numeric limits (max
	// databases, keys, storage bytes, etc.) that qrdb enforces
	// independently of the BaaS-layer IAM TenantQuotas.
	UpdateQuotas(ctx context.Context, id string, quotas qrdbsdk.Quotas) error

	// UpdateEntitlements updates the engine-side feature entitlements for
	// the tenant identified by id. Entitlements govern which isolation tiers
	// and dedicated-storage features the tenant's plan permits.
	UpdateEntitlements(ctx context.Context, id string, entitlements qrdbsdk.Entitlements) error
}

// TenantRoleSeeder materializes qrdb's canonical built-in roles inside a
// tenant. Every tenant needs them: a role grant is the only way a user is
// created with any authority, and the role names a token carries are resolved
// to permissions against the same store. First-boot seeds them for the system
// tenant; CreateTenant seeds them for every tenant after it, through this seam.
//
// Implementations must be idempotent — a role that already exists is success,
// not a conflict — so a re-run over a partly seeded tenant converges. They
// must also leave the SYSTEM tenant alone: first-boot creates it through this
// same manager and then seeds its roles itself, and it decides how to resolve
// the platform admin's role grant from whether it found those roles already
// present. Seeding them here would break the admin's creation on a fresh
// install.
type TenantRoleSeeder interface {
	// SeedBuiltInRoles creates the built-in roles in tenantID.
	SeedBuiltInRoles(ctx context.Context, tenantID string) error
}

// Tenant ID validation constraints.
const (
	TenantIDMinLength = 3
	TenantIDMaxLength = 63
)

var (
	// tenantIDRegex validates tenant IDs (alphanumeric lowercase, hyphens allowed).
	tenantIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)
)

// TenantSettingsObserver is notified after a tenant's settings are persisted by
// UpdateTenant so caches keyed by per-tenant settings (e.g. the per-tenant
// WebAuthn relying-party service and the per-tenant OIDC signing key) can
// rebuild on the next access. Implementations must be safe for concurrent use
// and must not block.
type TenantSettingsObserver interface {
	// OnTenantSettingsChanged is invoked with the tenant id whose settings
	// just changed.
	OnTenantSettingsChanged(tenantID string)
}

// DefaultTenantManager is the default implementation of TenantManager.
type DefaultTenantManager struct {
	storage         TenantStorage
	provisioner     database.TenantProvisioner
	dekProvisioner  TenantDEKProvisioner
	qrdbProvisioner QRDBTenantProvisioner

	// roleSeeder, when wired, materializes the built-in roles inside each
	// newly created tenant. Wired at server init via SetRoleSeeder. Nil
	// leaves CreateTenant's role-seed step a no-op, which yields a tenant
	// that cannot hold a user until an operator recreates the roles by hand.
	roleSeeder TenantRoleSeeder

	// userStorage, when wired, is the routed (qrdb-backed) user store used by
	// DeleteTenant to deprovision the tenant's users — purging the broker
	// profile, IDMap, and email/legacy-PIV index records that the legacy
	// BackendStorage cascade no longer reaches. Wired at server init via
	// SetUserStorage. Nil leaves DeleteTenant's user-deprovision step a no-op.
	userStorage UserStorage

	// webauthnCredentials, when wired, revokes each user's WebAuthn
	// credentials as the tenant is torn down. Tenant deletion deprovisions
	// users through userStorage directly rather than through
	// IdentityStore.DeleteUser, so without this the tenant's credentials would
	// survive the tenant — filed under (tenant, email) keys whose users no
	// longer exist, and ready to block those addresses if the tenant id is
	// ever reused. Wired at server init via SetWebAuthnCredentialReaper.
	webauthnCredentials WebAuthnCredentialReaper

	// settingsObservers are notified after UpdateTenant persists a change so
	// per-tenant caches can invalidate. The slice is append-only at wiring
	// time (server init) before the manager is shared, so reads during
	// UpdateTenant need no lock.
	settingsObservers []TenantSettingsObserver
}

// AddSettingsObserver registers an observer notified after each successful
// UpdateTenant. Call during server init before the manager is used
// concurrently.
func (m *DefaultTenantManager) AddSettingsObserver(obs TenantSettingsObserver) {
	if obs == nil {
		return
	}
	m.settingsObservers = append(m.settingsObservers, obs)
}

// NewDefaultTenantManager creates a new default tenant manager with the provided storage backend.
func NewDefaultTenantManager(storage TenantStorage) *DefaultTenantManager {
	return &DefaultTenantManager{
		storage: storage,
	}
}

// CreateTenant creates a new tenant with the specified options.
// It validates the tenant ID, generates one if not provided, and initializes
// default settings and quotas.
func (m *DefaultTenantManager) CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error) {
	// Validate required fields
	if opts.Name == "" {
		return nil, dberrors.InvalidInput("iam.CreateTenant", "tenant name is required")
	}

	// Generate or validate tenant ID
	tenantID := opts.ID
	if tenantID == "" {
		var err error
		tenantID, err = generateTenantIDString()
		if err != nil {
			return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.CreateTenant", "failed to generate tenant ID")
		}
	} else {
		if err := validateTenantIDPattern(tenantID); err != nil {
			return nil, err
		}
	}

	// Check if tenant already exists
	exists, err := m.storage.TenantExists(ctx, tenantID)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.CreateTenant", "failed to check tenant existence")
	}
	if exists {
		return nil, &dberrors.QBError{
			Code: dberrors.ErrAlreadyExists,
			Op:   "iam.CreateTenant",
			Err:  &IAMMessageError{Message: "tenant already exists: " + tenantID},
			Context: map[string]interface{}{
				"tenant_id": tenantID,
			},
		}
	}

	// Initialize default settings if not provided
	settings := TenantSettings{}
	if opts.Settings != nil {
		settings = *opts.Settings
	}

	// Initialize default BaaS-layer IAM quotas if not provided.
	// Engine-level quotas (users, roles, storage) are set on qrdb's TenantServicer.
	quotas := TenantQuotas{
		MaxGroups:   1000,
		MaxPolicies: 1000,
		MaxApps:     100,
	}
	if opts.Quotas != nil {
		quotas = *opts.Quotas
	}

	// Create tenant object
	now := time.Now()
	tenant := &Tenant{
		ID:       tenantID,
		Name:     opts.Name,
		Settings: settings,
		Quotas:   quotas,
		Created:  now,
		Updated:  now,
	}

	// Engine-side provisioning: create the tenant in the qrdb engine FIRST
	// so the per-tenant barrier, DEK, and quota are initialised before any
	// BaaS-layer state is written. On failure the entire creation is aborted.
	if m.qrdbProvisioner != nil {
		if err := m.qrdbProvisioner.CreateTenant(ctx, tenantID, opts.Name, string(opts.IsolationType)); err != nil {
			return nil, mapQRDBTenantError("iam.CreateTenant.provision", err)
		}

		// Apply engine-level quota overrides when provided by the caller (e.g.
		// BaaS operator wiring plan-tier limits at tenant creation time).
		if opts.EngineQuotas != nil {
			if err := m.qrdbProvisioner.UpdateQuotas(ctx, tenantID, *opts.EngineQuotas); err != nil {
				_ = m.qrdbProvisioner.DeleteTenant(ctx, tenantID)
				return nil, mapQRDBTenantError("iam.CreateTenant.quotas", err)
			}
		}

		// Apply engine-level entitlement overrides (isolation tiers, dedicated-raft,
		// WAL features, barrier modes) when provided.
		if opts.EngineEntitlements != nil {
			if err := m.qrdbProvisioner.UpdateEntitlements(ctx, tenantID, *opts.EngineEntitlements); err != nil {
				_ = m.qrdbProvisioner.DeleteTenant(ctx, tenantID)
				return nil, mapQRDBTenantError("iam.CreateTenant.entitlements", err)
			}
		}
	}

	// Store the tenant in the BaaS-layer IAM storage.
	if err := m.storage.CreateTenant(ctx, tenant); err != nil {
		// Roll back the engine-side tenant creation on BaaS storage failure.
		if m.qrdbProvisioner != nil {
			_ = m.qrdbProvisioner.DeleteTenant(ctx, tenantID)
		}
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.CreateTenant", "failed to create tenant")
	}

	// Provision tenant database infrastructure if a provisioner is configured.
	if m.provisioner != nil {
		if err := m.provisioner.ProvisionTenant(ctx, tenantID); err != nil {
			// Roll back: delete the tenant record since provisioning failed.
			_ = m.storage.DeleteTenant(ctx, tenantID)
			if m.qrdbProvisioner != nil {
				_ = m.qrdbProvisioner.DeleteTenant(ctx, tenantID)
			}
			return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.CreateTenant", "failed to provision tenant database")
		}
	}

	// Provision tenant DEK if a DEK provisioner is configured.
	if m.dekProvisioner != nil {
		if err := m.dekProvisioner.ProvisionDEK(ctx, tenantID); err != nil {
			// Roll back: deprovision database and delete tenant record.
			if m.provisioner != nil {
				_ = m.provisioner.DeprovisionTenant(ctx, tenantID)
			}
			_ = m.storage.DeleteTenant(ctx, tenantID)
			if m.qrdbProvisioner != nil {
				_ = m.qrdbProvisioner.DeleteTenant(ctx, tenantID)
			}
			return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.CreateTenant", "failed to provision tenant DEK")
		}
	}

	// Seed the built-in roles. A tenant with no roles cannot hold a user:
	// every CreateUser that asks for authority names a role, and a name that
	// resolves to nothing fails the create. Seeding is therefore part of
	// creating the tenant, not a follow-up the operator is expected to know
	// about, and a seeding failure fails the whole creation rather than
	// leaving behind a tenant nothing can be provisioned into.
	if m.roleSeeder != nil {
		if err := m.roleSeeder.SeedBuiltInRoles(ctx, tenantID); err != nil {
			// Roll back every step taken above, innermost first.
			if m.dekProvisioner != nil {
				_ = m.dekProvisioner.DeprovisionDEK(ctx, tenantID)
			}
			if m.provisioner != nil {
				_ = m.provisioner.DeprovisionTenant(ctx, tenantID)
			}
			_ = m.storage.DeleteTenant(ctx, tenantID)
			if m.qrdbProvisioner != nil {
				_ = m.qrdbProvisioner.DeleteTenant(ctx, tenantID)
			}
			return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.CreateTenant", "failed to seed tenant built-in roles")
		}
	}

	return tenant, nil
}

// GetTenant retrieves a tenant by ID.
func (m *DefaultTenantManager) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	if err := validateTenantIDPattern(id); err != nil {
		return nil, err
	}

	tenant, err := m.storage.GetTenant(ctx, id)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, NewTenantNotFoundError(id)
		}
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.GetTenant", "failed to get tenant")
	}

	return tenant, nil
}

// UpdateTenant updates an existing tenant with the specified options.
func (m *DefaultTenantManager) UpdateTenant(ctx context.Context, id string, opts UpdateTenantOptions) (*Tenant, error) {
	if err := validateTenantIDPattern(id); err != nil {
		return nil, err
	}

	// Get existing tenant
	tenant, err := m.GetTenant(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if opts.Name != nil {
		if *opts.Name == "" {
			return nil, dberrors.InvalidInput("iam.UpdateTenant", "tenant name cannot be empty")
		}
		tenant.Name = *opts.Name
	}

	if opts.Settings != nil {
		tenant.Settings = *opts.Settings
	}

	if opts.Quotas != nil {
		tenant.Quotas = *opts.Quotas
	}

	tenant.Updated = time.Now()

	// Store updated tenant
	if err := m.storage.UpdateTenant(ctx, tenant); err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.UpdateTenant", "failed to update tenant")
	}

	// Notify per-tenant settings caches (WebAuthn RP service, OIDC signing key)
	// so they rebuild from the new settings on next access.
	for _, obs := range m.settingsObservers {
		obs.OnTenantSettingsChanged(id)
	}

	return tenant, nil
}

// DeleteTenant marks a tenant as deleted (soft delete).
func (m *DefaultTenantManager) DeleteTenant(ctx context.Context, id string) error {
	if err := validateTenantIDPattern(id); err != nil {
		return err
	}

	// Verify tenant exists
	_, err := m.GetTenant(ctx, id)
	if err != nil {
		return err
	}

	// Deprovision the tenant's users through the routed (qrdb-backed) user
	// store so each user's broker profile, IDMap, and email/legacy-PIV index
	// records are purged — state the legacy BackendStorage cascade no longer
	// reaches post-C1. Page until the listing is drained (a partial page or an
	// empty page signals the end) rather than assuming a single page. Fail the
	// tenant delete on any per-user deprovision error (fail-closed, consistent
	// with the DEK / database / engine steps below).
	if m.userStorage != nil {
		const userPage = 256
		for {
			users, listErr := m.userStorage.ListUsers(ctx, id, ListOptions{Limit: userPage})
			if listErr != nil {
				return dberrors.Wrapf(listErr, dberrors.ErrStorage, "iam.DeleteTenant", "failed to list tenant users for deprovision")
			}
			if len(users) == 0 {
				break
			}
			for _, u := range users {
				// Revoke the user's WebAuthn credentials first, for the same
				// reason IdentityStore.DeleteUser does: the credential store
				// is keyed by (tenant, email), so once the user record is
				// gone the address that names its credentials is gone too.
				if m.webauthnCredentials != nil && u.Email != "" {
					if _, rErr := m.webauthnCredentials.DeleteCredentials(ctx, id, u.Email); rErr != nil {
						return dberrors.Wrapf(rErr, dberrors.ErrStorage, "iam.DeleteTenant", "failed to revoke tenant user webauthn credentials")
					}
				}
				if delErr := m.userStorage.DeleteUser(ctx, id, u.ID); delErr != nil {
					return dberrors.Wrapf(delErr, dberrors.ErrStorage, "iam.DeleteTenant", "failed to deprovision tenant user")
				}
			}
			// DeleteUser removes the listed records, so the next page reads
			// the new head of the list (Offset stays 0). A page shorter than
			// the limit means no more users remain.
			if len(users) < userPage {
				break
			}
		}
	}

	// Deprovision tenant DEK if a DEK provisioner is configured.
	if m.dekProvisioner != nil {
		if err := m.dekProvisioner.DeprovisionDEK(ctx, id); err != nil {
			return dberrors.Wrapf(err, dberrors.ErrInternal, "iam.DeleteTenant", "failed to deprovision tenant DEK")
		}
	}

	// Deprovision tenant database infrastructure if a provisioner is configured.
	if m.provisioner != nil {
		if err := m.provisioner.DeprovisionTenant(ctx, id); err != nil {
			return dberrors.Wrapf(err, dberrors.ErrInternal, "iam.DeleteTenant", "failed to deprovision tenant database")
		}
	}

	// Delete the tenant from BaaS-layer IAM storage.
	if err := m.storage.DeleteTenant(ctx, id); err != nil {
		if dberrors.IsNotFound(err) {
			return NewTenantNotFoundError(id)
		}
		return dberrors.Wrapf(err, dberrors.ErrStorage, "iam.DeleteTenant", "failed to delete tenant")
	}

	// Engine-side cleanup: delete the tenant from the qrdb engine LAST so the
	// per-tenant barrier and DEK are destroyed after BaaS-layer state is gone.
	if m.qrdbProvisioner != nil {
		if err := m.qrdbProvisioner.DeleteTenant(ctx, id); err != nil {
			return dberrors.Wrapf(err, dberrors.ErrInternal, "iam.DeleteTenant", "qrdb engine tenant deletion failed")
		}
	}

	return nil
}

// ListTenants retrieves tenants with pagination and filtering.
func (m *DefaultTenantManager) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	tenants, err := m.storage.ListTenants(ctx, opts)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.ListTenants", "failed to list tenants")
	}

	return tenants, nil
}

// CheckQuota verifies that a resource increment would not exceed quota limits.
func (m *DefaultTenantManager) CheckQuota(ctx context.Context, tenantID string, resource string) error {
	if err := validateTenantIDPattern(tenantID); err != nil {
		return err
	}

	quota, err := m.storage.GetQuota(ctx, tenantID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return NewTenantNotFoundError(tenantID)
		}
		return dberrors.Wrapf(err, dberrors.ErrStorage, "iam.CheckQuota", "failed to get quota")
	}

	// Check BaaS-layer IAM quota only. Users, roles, and storage are
	// engine-level quotas enforced by qrdb's TenantServicer — call
	// qrdb's IncrementQuotaUsage for those resources instead.
	var limit, current int64
	switch resource {
	case QuotaResourceGroups:
		limit, current = quota.MaxGroups, quota.CurrentGroups
	case QuotaResourcePolicies:
		limit, current = quota.MaxPolicies, quota.CurrentPolicies
	case QuotaResourceApps:
		limit, current = quota.MaxApps, quota.CurrentApps
	default:
		// QuotaResourceUsers, QuotaResourceRoles, QuotaResourceStorage are
		// owned by qrdb. Unknown resources are silently allowed here.
		return nil
	}

	// limit == 0 means unlimited
	if limit > 0 && current >= limit {
		return NewQuotaExceededError(tenantID, resource, limit, current)
	}

	return nil
}

// UpdateEngineQuotas updates the engine-side numeric resource limits for the
// tenant by delegating to the QRDBTenantProvisioner. Returns nil when no
// engine provisioner is wired (backwards-compatible no-op).
func (m *DefaultTenantManager) UpdateEngineQuotas(ctx context.Context, id string, quotas qrdbsdk.Quotas) error {
	if m.qrdbProvisioner == nil {
		return nil
	}
	return mapQRDBTenantError("iam.UpdateEngineQuotas", m.qrdbProvisioner.UpdateQuotas(ctx, id, quotas))
}

// UpdateEngineEntitlements updates the engine-side feature entitlements for
// the tenant by delegating to the QRDBTenantProvisioner. Returns nil when no
// engine provisioner is wired (backwards-compatible no-op).
func (m *DefaultTenantManager) UpdateEngineEntitlements(ctx context.Context, id string, entitlements qrdbsdk.Entitlements) error {
	if m.qrdbProvisioner == nil {
		return nil
	}
	return mapQRDBTenantError("iam.UpdateEngineEntitlements", m.qrdbProvisioner.UpdateEntitlements(ctx, id, entitlements))
}

// validateTenantIDPattern validates a tenant ID according to the rules:
// - Alphanumeric lowercase characters and hyphens only
// - Must start and end with alphanumeric character
// - Length between 3 and 63 characters.
func validateTenantIDPattern(id string) error {
	if id == "" {
		return NewInvalidTenantIDError(id, "ID cannot be empty")
	}

	if len(id) < TenantIDMinLength {
		return NewInvalidTenantIDError(id, fmt.Sprintf("ID must be at least %d characters", TenantIDMinLength))
	}

	if len(id) > TenantIDMaxLength {
		return NewInvalidTenantIDError(id, fmt.Sprintf("ID must not exceed %d characters", TenantIDMaxLength))
	}

	if !tenantIDPattern.MatchString(id) {
		return NewInvalidTenantIDError(id, "ID must contain only lowercase alphanumeric characters and hyphens, and must start and end with an alphanumeric character")
	}

	return nil
}

// generateTenantIDString generates a random tenant ID.
func generateTenantIDString() (string, error) {
	// Generate 8 random bytes (16 hex characters)
	b, err := xkms.Rand(8)
	if err != nil {
		return "", err
	}

	// Format as "tenant-" prefix + hex string
	return "tenant-" + hex.EncodeToString(b), nil
}
