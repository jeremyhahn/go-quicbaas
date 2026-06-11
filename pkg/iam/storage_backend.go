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

// Package iam provides identity and access management for QuicBaaS.
package iam

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ErrAlreadyExists is a generic error for duplicate resources.
var ErrAlreadyExists = &dberrors.QBError{
	Code: dberrors.ErrAlreadyExists,
	Op:   "iam.storage",
	Err:  &ResourceAlreadyExistsError{},
}

// Key prefixes for IAM data following the architecture spec.
// All IAM data uses the reserved prefix `__iam:`.
const (
	// Root prefix for all IAM data
	KeyPrefixIAM = "__iam:"

	// Tenant keys
	KeyPrefixTenant         = "__iam:tenant:"
	KeyPrefixTenantSettings = ":settings"
	KeyPrefixTenantQuota    = ":quota"

	// Identity keys (scoped by tenant). User keys are absent: the user record
	// lives in qrdb (qrdbadapter.QRDBUserStorage), not the BaaS KVStore.
	KeyPrefixGroup  = ":group:"
	KeyPrefixRole   = ":role:"
	KeyPrefixPolicy = ":policy:"

	// OAuth2/OIDC keys (scoped by tenant)
	KeyPrefixApp      = ":app:"
	KeyPrefixToken    = ":token:"
	KeyPrefixAuthCode = ":authcode:"

	// Session keys (scoped by tenant)
	KeyPrefixSession = ":session:"

	// Global indexes. The username, email, and PIV-thumbprint lookup indexes
	// moved to qrdb with the user record (phase C1-F); only the app index
	// remains BaaS-owned.
	KeyPrefixAppIndex = "__iam:idx:app:"
)

// BackendStorage implements the BaaS-owned IAM storage interfaces using
// interfaces.KVStore: TenantStorage, GroupStorage, RoleStorage, PolicyStorage,
// AppStorage, and TokenStorage. It works with any backend (memory, pebble,
// raft) via the clean KVStore interface, providing an abstraction layer that
// hides backend implementation details.
//
// User persistence is NOT a BackendStorage responsibility: it is owned by the
// embedded qrdb node (qrdbadapter.QRDBUserStorage) so the broker and the qrdb
// policy decision point share one user record. BackendStorage therefore does
// NOT satisfy UserStorage and holds no user keys; tenant deletion deprovisions
// users through the routed UserStorage (DefaultTenantManager.DeleteTenant).
type BackendStorage struct {
	kv interfaces.KVStore
}

// NewBackendStorage creates a new IAM storage backed by a KVStore.
// The KVStore abstracts the underlying storage (memory, PebbleDB, Raft).
func NewBackendStorage(kv interfaces.KVStore) *BackendStorage {
	return &BackendStorage{
		kv: kv,
	}
}

// Compile-time interface checks. UserStorage is intentionally absent:
// BackendStorage no longer persists users (qrdb owns the user record).
var (
	_ TenantStorage = (*BackendStorage)(nil)
	_ GroupStorage  = (*BackendStorage)(nil)
	_ RoleStorage   = (*BackendStorage)(nil)
	_ AppStorage    = (*BackendStorage)(nil)
	_ TokenStorage  = (*BackendStorage)(nil)
	_ PolicyStorage = (*BackendStorage)(nil)
)

// -----------------------------------------------------------------------------
// Key helpers
// -----------------------------------------------------------------------------

// tenantKey returns the key for a tenant record.
func tenantKey(tenantID string) string {
	return KeyPrefixTenant + tenantID
}

// tenantSettingsKey returns the key for tenant settings.
func tenantSettingsKey(tenantID string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixTenantSettings
}

// tenantQuotaKey returns the key for tenant quota.
func tenantQuotaKey(tenantID string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixTenantQuota
}

// groupKey returns the key for a group record.
func groupKey(tenantID, groupID string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixGroup + groupID
}

// roleKey returns the key for a role record.
func roleKey(tenantID, roleID string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixRole + roleID
}

// policyKey returns the key for a policy record.
func policyKey(tenantID, policyName string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixPolicy + policyName
}

// appKey returns the tenant-scoped key for an OAuth2 application. The tenant
// is parsed from the clientID (format app_<tenant>_<suffix>) so two tenants
// cannot collide on a global clientID slot.
func appKey(tenantID, clientID string) string {
	return KeyPrefixIAM + "app:" + tenantID + ":" + clientID
}

// legacyAppKey returns the pre-tenant-scoping global app key (dual-read shim).
func legacyAppKey(clientID string) string {
	return KeyPrefixIAM + "app:" + clientID
}

// tenantFromClientID extracts the tenant ID from an OAuth2 clientID minted in
// the format app_<tenant>_<suffix> (see generateClientID). It returns an empty
// string when the clientID does not match that shape, so callers can fall back
// to the legacy global key.
func tenantFromClientID(clientID string) string {
	const prefix = "app_"
	if !strings.HasPrefix(clientID, prefix) {
		return ""
	}
	rest := clientID[len(prefix):]
	// rest is <tenant>_<suffix>; the tenant is everything up to the LAST "_".
	idx := strings.LastIndex(rest, "_")
	if idx <= 0 {
		return ""
	}
	return rest[:idx]
}

// appTenantKey returns the key for app lookup by tenant.
func appTenantKey(tenantID, clientID string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixApp + clientID
}

// tokenKey returns the tenant-scoped key for a token record. Scoping by tenant
// isolates per-tenant token state and prevents a forged JTI in one tenant from
// resolving a token in another.
func tokenKey(tenantID, jti string) string {
	return KeyPrefixIAM + "token:" + tenantID + ":" + jti
}

// legacyTokenKey returns the pre-tenant-scoping global token key (dual-read).
func legacyTokenKey(jti string) string {
	return KeyPrefixIAM + "token:" + jti
}

// tokenValueKey returns the key for token lookup by value.
func tokenValueKey(tokenValue string) string {
	return KeyPrefixIAM + "tokenval:" + tokenValue
}

// authCodeKey returns the key for an authorization code.
func authCodeKey(code string) string {
	return KeyPrefixIAM + "authcode:" + code
}

// sessionKey returns the key for a session record.
func sessionKey(tenantID, sessionID string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixSession + sessionID
}

// -----------------------------------------------------------------------------
// Storage operations helpers (using clean interfaces.KVStore)
// -----------------------------------------------------------------------------

// put stores a value using the KVStore.
func (s *BackendStorage) put(ctx context.Context, key string, value []byte) error {
	return s.kv.Put(ctx, key, value)
}

// get retrieves a value using the KVStore.
func (s *BackendStorage) get(ctx context.Context, key string) ([]byte, error) {
	return s.kv.Get(ctx, key)
}

// delete removes a value using the KVStore.
func (s *BackendStorage) delete(ctx context.Context, key string) error {
	return s.kv.Delete(ctx, key)
}

// list retrieves all values with a given key prefix using Scan.
func (s *BackendStorage) list(ctx context.Context, prefix string) ([][]byte, error) {
	var values [][]byte
	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		// Make a copy of the value since the callback slice may be reused
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		values = append(values, valueCopy)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return values, nil
}

// deleteByPrefix deletes all keys matching the given prefix.
func (s *BackendStorage) deleteByPrefix(ctx context.Context, prefix string) error {
	keys, err := s.kv.List(ctx, prefix)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if err := s.kv.Delete(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// deleteAppsWithIndexes deletes apps and their global indexes.
// Tenant-scoped app entries store only the clientID as value, not the full Application.
func (s *BackendStorage) deleteAppsWithIndexes(ctx context.Context, appPrefix string) error {
	// The tenant is encoded in the index prefix: __iam:tenant:<tenantID>:app:
	tenantID := strings.TrimSuffix(strings.TrimPrefix(appPrefix, KeyPrefixTenant), KeyPrefixApp)

	// Collect all client IDs from tenant-scoped app index entries
	var clientIDs []string
	err := s.kv.Scan(ctx, appPrefix, func(key string, value []byte) error {
		// The value is the clientID stored as bytes
		if len(value) > 0 {
			clientIDs = append(clientIDs, string(value))
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Delete app records and indexes for each app (tenant-scoped + legacy).
	for _, clientID := range clientIDs {
		_ = s.delete(ctx, appKey(tenantID, clientID))
		if tid := tenantFromClientID(clientID); tid != "" && tid != tenantID {
			_ = s.delete(ctx, appKey(tid, clientID))
		}
		_ = s.delete(ctx, legacyAppKey(clientID))
		// Delete app index: __iam:idx:app:<clientID>
		_ = s.delete(ctx, KeyPrefixAppIndex+clientID)
	}

	// Delete all tenant-scoped app index entries
	return s.deleteByPrefix(ctx, appPrefix)
}

// -----------------------------------------------------------------------------
// TenantStorage implementation
// -----------------------------------------------------------------------------

// CreateTenant stores a new tenant.
func (s *BackendStorage) CreateTenant(ctx context.Context, tenant *Tenant) error {
	key := tenantKey(tenant.ID)

	// Check if tenant already exists
	_, err := s.get(ctx, key)
	if err == nil {
		return ErrAlreadyExists
	}

	data, err := json.Marshal(tenant)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.create", Err: err}
	}

	return s.put(ctx, key, data)
}

// GetTenant retrieves a tenant by ID.
func (s *BackendStorage) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	data, err := s.get(ctx, tenantKey(id))
	if err != nil {
		return nil, err
	}

	var tenant Tenant
	if err := json.Unmarshal(data, &tenant); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.get", Err: err}
	}

	return &tenant, nil
}

// UpdateTenant updates an existing tenant.
func (s *BackendStorage) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	key := tenantKey(tenant.ID)

	// Verify tenant exists
	_, err := s.get(ctx, key)
	if err != nil {
		return err
	}

	data, err := json.Marshal(tenant)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.update", Err: err}
	}

	return s.put(ctx, key, data)
}

// DeleteTenant removes a tenant and all associated BaaS-owned data (cascade
// delete): groups, roles, policies, apps, sessions, settings, and quotas.
// User records live in qrdb and are deprovisioned by
// DefaultTenantManager.DeleteTenant through the routed UserStorage before this
// storage-level cascade runs.
func (s *BackendStorage) DeleteTenant(ctx context.Context, id string) error {
	key := tenantKey(id)

	// Verify tenant exists
	_, err := s.get(ctx, key)
	if err != nil {
		return err
	}

	// Cascade delete all tenant data
	tenantPrefix := KeyPrefixTenant + id

	// 1. Delete groups
	groupPrefix := tenantPrefix + KeyPrefixGroup
	if err := s.deleteByPrefix(ctx, groupPrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.groups",
			Err:  err,
		}
	}

	// 2. Delete roles
	rolePrefix := tenantPrefix + KeyPrefixRole
	if err := s.deleteByPrefix(ctx, rolePrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.roles",
			Err:  err,
		}
	}

	// 3. Delete policies
	policyPrefix := tenantPrefix + KeyPrefixPolicy
	if err := s.deleteByPrefix(ctx, policyPrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.policies",
			Err:  err,
		}
	}

	// 4. Delete apps and their global indexes
	appPrefix := tenantPrefix + KeyPrefixApp
	if err := s.deleteAppsWithIndexes(ctx, appPrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.apps",
			Err:  err,
		}
	}

	// 5. Delete sessions
	sessionPrefix := tenantPrefix + KeyPrefixSession
	if err := s.deleteByPrefix(ctx, sessionPrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.sessions",
			Err:  err,
		}
	}

	// 6. Delete tenant settings (ignore not found)
	_ = s.delete(ctx, tenantSettingsKey(id))

	// 7. Delete tenant quota (ignore not found)
	_ = s.delete(ctx, tenantQuotaKey(id))

	// 8. Finally delete the tenant record
	return s.delete(ctx, key)
}

// ListTenants retrieves tenants with pagination.
// Uses key-based filtering to only include actual tenant records, excluding
// sub-resource keys (settings, quotas, users, groups, roles, etc.) that share
// the same key prefix.
func (s *BackendStorage) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	prefix := KeyPrefixTenant

	// Scan with key filtering: only include keys that represent actual tenant
	// records. Tenant keys are "__iam:tenant:<tenantID>" with no additional
	// colon-separated segments. Sub-resource keys like settings, quotas, users,
	// groups, and roles all contain additional ":" separators after the tenant ID.
	var tenants []*Tenant
	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		// Extract the portion after the prefix (the tenant ID portion)
		suffix := strings.TrimPrefix(key, prefix)

		// Skip sub-resource keys: settings, quotas, users, groups, roles, etc.
		// These keys contain additional ":" after the tenant ID.
		// Valid tenant IDs contain only [a-z0-9-] and never ":" characters.
		if strings.Contains(suffix, ":") {
			return nil
		}

		// Skip empty suffix (should not happen but defensive)
		if suffix == "" {
			return nil
		}

		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)

		var tenant Tenant
		if err := json.Unmarshal(valueCopy, &tenant); err != nil {
			return nil
		}
		if tenant.ID != "" {
			tenants = append(tenants, &tenant)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if tenants == nil {
		tenants = []*Tenant{}
	}

	// Apply pagination
	if opts.Offset > 0 && opts.Offset < len(tenants) {
		tenants = tenants[opts.Offset:]
	} else if opts.Offset >= len(tenants) {
		return []*Tenant{}, nil
	}

	if opts.Limit > 0 && opts.Limit < len(tenants) {
		tenants = tenants[:opts.Limit]
	}

	return tenants, nil
}

// CountTenants returns the total number of tenants.
func (s *BackendStorage) CountTenants(ctx context.Context, opts ListOptions) (int64, error) {
	tenants, err := s.ListTenants(ctx, ListOptions{})
	if err != nil {
		return 0, err
	}
	return int64(len(tenants)), nil
}

// TenantExists checks if a tenant exists.
func (s *BackendStorage) TenantExists(ctx context.Context, id string) (bool, error) {
	_, err := s.get(ctx, tenantKey(id))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetSettings retrieves tenant settings.
func (s *BackendStorage) GetSettings(ctx context.Context, tenantID string) (*TenantSettings, error) {
	data, err := s.get(ctx, tenantSettingsKey(tenantID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return &TenantSettings{}, nil // Return empty settings if not set
		}
		return nil, err
	}

	if len(data) == 0 {
		return &TenantSettings{}, nil // Return empty settings if data is empty
	}

	var settings TenantSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.settings.get", Err: err}
	}

	return &settings, nil
}

// UpdateSettings updates tenant settings.
func (s *BackendStorage) UpdateSettings(ctx context.Context, tenantID string, settings *TenantSettings) error {
	// Verify tenant exists
	exists, err := s.TenantExists(ctx, tenantID)
	if err != nil {
		return err
	}
	if !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.tenant.settings.update"}
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.settings.update", Err: err}
	}

	return s.put(ctx, tenantSettingsKey(tenantID), data)
}

// GetQuota retrieves tenant quota.
func (s *BackendStorage) GetQuota(ctx context.Context, tenantID string) (*TenantQuota, error) {
	data, err := s.get(ctx, tenantQuotaKey(tenantID))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return &TenantQuota{}, nil // Return empty quota if not set
		}
		return nil, err
	}

	if len(data) == 0 {
		return &TenantQuota{}, nil // Return empty quota if data is empty
	}

	var quota TenantQuota
	if err := json.Unmarshal(data, &quota); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.quota.get", Err: err}
	}

	return &quota, nil
}

// UpdateQuota updates tenant quota.
func (s *BackendStorage) UpdateQuota(ctx context.Context, tenantID string, quota *TenantQuota) error {
	data, err := json.Marshal(quota)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.tenant.quota.update", Err: err}
	}

	return s.put(ctx, tenantQuotaKey(tenantID), data)
}

// IncrementQuotaUsage atomically increments a quota counter.
func (s *BackendStorage) IncrementQuotaUsage(ctx context.Context, tenantID, resource string, delta int64) error {
	quota, err := s.GetQuota(ctx, tenantID)
	if err != nil {
		return err
	}

	// Apply increment based on resource type. Users and roles are tracked by
	// qrdb's TenantServicer (engine-level) and are not tracked here.
	switch resource {
	case QuotaResourceGroups:
		quota.CurrentGroups += delta
	case QuotaResourcePolicies:
		quota.CurrentPolicies += delta
	case QuotaResourceApps:
		quota.CurrentApps += delta
	}

	return s.UpdateQuota(ctx, tenantID, quota)
}

// DecrementQuotaUsage atomically decrements a quota counter.
func (s *BackendStorage) DecrementQuotaUsage(ctx context.Context, tenantID, resource string, delta int64) error {
	return s.IncrementQuotaUsage(ctx, tenantID, resource, -delta)
}

// -----------------------------------------------------------------------------
// User persistence is owned by qrdb (qrdbadapter.QRDBUserStorage), not by
// BackendStorage. The user-CRUD methods that once lived here (CreateUser,
// GetUser, GetUserByUsername, GetUserByEmail, GetUserByPIVThumbprint,
// UpdateUser, DeleteUser, ListUsers) plus their lookup-index helpers were
// removed in the user-model unification (phase C1-F): every production
// transport routes user storage to the embedded qrdb node, which is the single
// source of truth for the user record. Tenant teardown deprovisions users
// through the routed UserStorage (DefaultTenantManager.DeleteTenant).
// -----------------------------------------------------------------------------

// -----------------------------------------------------------------------------
// GroupStorage implementation
// -----------------------------------------------------------------------------

// CreateGroup stores a new group.
func (s *BackendStorage) CreateGroup(ctx context.Context, group *Group) error {
	key := groupKey(group.TenantID, group.ID)

	_, err := s.get(ctx, key)
	if err == nil {
		return ErrAlreadyExists
	}

	data, err := json.Marshal(group)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.group.create", Err: err}
	}

	return s.put(ctx, key, data)
}

// GetGroup retrieves a group by ID.
func (s *BackendStorage) GetGroup(ctx context.Context, tenantID, groupID string) (*Group, error) {
	data, err := s.get(ctx, groupKey(tenantID, groupID))
	if err != nil {
		return nil, err
	}

	var group Group
	if err := json.Unmarshal(data, &group); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.group.get", Err: err}
	}

	return &group, nil
}

// UpdateGroup updates an existing group.
func (s *BackendStorage) UpdateGroup(ctx context.Context, group *Group) error {
	key := groupKey(group.TenantID, group.ID)

	_, err := s.get(ctx, key)
	if err != nil {
		return err
	}

	data, err := json.Marshal(group)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.group.update", Err: err}
	}

	return s.put(ctx, key, data)
}

// DeleteGroup removes a group.
func (s *BackendStorage) DeleteGroup(ctx context.Context, tenantID, groupID string) error {
	_, err := s.get(ctx, groupKey(tenantID, groupID))
	if err != nil {
		return err
	}
	return s.delete(ctx, groupKey(tenantID, groupID))
}

// ListGroups retrieves groups for a tenant.
func (s *BackendStorage) ListGroups(ctx context.Context, tenantID string, opts ListOptions) ([]*Group, error) {
	prefix := KeyPrefixTenant + tenantID + KeyPrefixGroup
	values, err := s.list(ctx, prefix)
	if err != nil {
		return nil, err
	}

	groups := make([]*Group, 0, len(values))
	for _, data := range values {
		var group Group
		if err := json.Unmarshal(data, &group); err != nil {
			continue
		}
		groups = append(groups, &group)
	}

	// Apply pagination
	if opts.Offset > 0 && opts.Offset < len(groups) {
		groups = groups[opts.Offset:]
	} else if opts.Offset >= len(groups) {
		return []*Group{}, nil
	}

	if opts.Limit > 0 && opts.Limit < len(groups) {
		groups = groups[:opts.Limit]
	}

	return groups, nil
}

// -----------------------------------------------------------------------------
// RoleStorage implementation
// -----------------------------------------------------------------------------

// CreateRole stores a new role.
func (s *BackendStorage) CreateRole(ctx context.Context, role *Role) error {
	key := roleKey(role.TenantID, role.ID)

	_, err := s.get(ctx, key)
	if err == nil {
		return ErrAlreadyExists
	}

	data, err := json.Marshal(role)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.role.create", Err: err}
	}

	return s.put(ctx, key, data)
}

// GetRole retrieves a role by ID.
func (s *BackendStorage) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error) {
	data, err := s.get(ctx, roleKey(tenantID, roleID))
	if err != nil {
		return nil, err
	}

	var role Role
	if err := json.Unmarshal(data, &role); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.role.get", Err: err}
	}

	return &role, nil
}

// UpdateRole updates an existing role.
func (s *BackendStorage) UpdateRole(ctx context.Context, role *Role) error {
	key := roleKey(role.TenantID, role.ID)

	_, err := s.get(ctx, key)
	if err != nil {
		return err
	}

	data, err := json.Marshal(role)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.role.update", Err: err}
	}

	return s.put(ctx, key, data)
}

// DeleteRole removes a role.
func (s *BackendStorage) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	_, err := s.get(ctx, roleKey(tenantID, roleID))
	if err != nil {
		return err
	}
	return s.delete(ctx, roleKey(tenantID, roleID))
}

// ListRoles retrieves roles for a tenant.
func (s *BackendStorage) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	prefix := KeyPrefixTenant + tenantID + KeyPrefixRole
	values, err := s.list(ctx, prefix)
	if err != nil {
		return nil, err
	}

	roles := make([]*Role, 0, len(values))
	for _, data := range values {
		var role Role
		if err := json.Unmarshal(data, &role); err != nil {
			continue
		}
		roles = append(roles, &role)
	}

	// Apply pagination
	if opts.Offset > 0 && opts.Offset < len(roles) {
		roles = roles[opts.Offset:]
	} else if opts.Offset >= len(roles) {
		return []*Role{}, nil
	}

	if opts.Limit > 0 && opts.Limit < len(roles) {
		roles = roles[:opts.Limit]
	}

	return roles, nil
}

// -----------------------------------------------------------------------------
// AppStorage implementation
// -----------------------------------------------------------------------------

// CreateApp stores a new OAuth2 application.
func (s *BackendStorage) CreateApp(ctx context.Context, app *Application) error {
	// The tenant is authoritative from the app record; the clientID also
	// encodes it (app_<tenant>_<suffix>) for read-path resolution.
	key := appKey(app.TenantID, app.ClientID)

	_, err := s.get(ctx, key)
	if err == nil {
		return ErrAlreadyExists
	}

	data, err := json.Marshal(app)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.app.create", Err: err}
	}

	if err := s.put(ctx, key, data); err != nil {
		return err
	}
	// Dual-write the legacy global app record during the migration window so
	// reads by bare clientID (no tenant in context, non app_<tenant>_ form)
	// keep resolving.
	_ = s.put(ctx, legacyAppKey(app.ClientID), data)

	// Create tenant->app index
	if app.TenantID != "" {
		_ = s.put(ctx, appTenantKey(app.TenantID, app.ClientID), []byte(app.ClientID))
	}

	return nil
}

// GetApp retrieves an application by client ID.
func (s *BackendStorage) GetApp(ctx context.Context, clientID string) (*Application, error) {
	// Resolve via the tenant-scoped key. The tenant is parsed from the
	// clientID (format app_<tenant>_<suffix>); when the clientID does not
	// encode a tenant, the context tenant is used. Both fall back to the
	// legacy global key (dual-read shim) for deployments predating scoping.
	var data []byte
	var err error = &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.get"}
	if tid := tenantFromClientID(clientID); tid != "" {
		data, err = s.get(ctx, appKey(tid, clientID))
	}
	if err != nil {
		if tid := TenantFromContext(ctx); tid != "" {
			data, err = s.get(ctx, appKey(tid, clientID))
		}
	}
	if err != nil {
		data, err = s.get(ctx, legacyAppKey(clientID))
		if err != nil {
			return nil, err
		}
	}

	var app Application
	if err := json.Unmarshal(data, &app); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.app.get", Err: err}
	}

	return &app, nil
}

// GetAppByTenant retrieves an application by tenant and client ID.
func (s *BackendStorage) GetAppByTenant(ctx context.Context, tenantID, clientID string) (*Application, error) {
	app, err := s.GetApp(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if app.TenantID != tenantID {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.getbytenant"}
	}
	return app, nil
}

// UpdateApp updates an existing application.
func (s *BackendStorage) UpdateApp(ctx context.Context, app *Application) error {
	key := appKey(app.TenantID, app.ClientID)

	_, err := s.get(ctx, key)
	if err != nil {
		return err
	}

	data, err := json.Marshal(app)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.app.update", Err: err}
	}

	if err := s.put(ctx, key, data); err != nil {
		return err
	}
	_ = s.put(ctx, legacyAppKey(app.ClientID), data)
	return nil
}

// DeleteApp removes an application.
func (s *BackendStorage) DeleteApp(ctx context.Context, clientID string) error {
	app, err := s.GetApp(ctx, clientID)
	if err != nil {
		return err
	}

	if err := s.delete(ctx, appKey(app.TenantID, clientID)); err != nil {
		return err
	}
	// Clean up any legacy global app record.
	_ = s.delete(ctx, legacyAppKey(clientID))

	// Clean up tenant index
	if app.TenantID != "" {
		_ = s.delete(ctx, appTenantKey(app.TenantID, clientID))
	}

	return nil
}

// ListApps retrieves applications for a tenant.
func (s *BackendStorage) ListApps(ctx context.Context, tenantID string, opts ListOptions) ([]*Application, error) {
	// Get app IDs from tenant index
	prefix := KeyPrefixTenant + tenantID + KeyPrefixApp
	values, err := s.list(ctx, prefix)
	if err != nil {
		return nil, err
	}

	// Resolve each app under the listing tenant so the tenant-scoped GetApp
	// key is selected even for clientIDs that do not encode a tenant.
	appCtx := ContextWithTenant(ctx, tenantID)
	apps := make([]*Application, 0, len(values))
	for _, clientIDBytes := range values {
		app, err := s.GetApp(appCtx, string(clientIDBytes))
		if err != nil {
			continue
		}
		apps = append(apps, app)
	}

	// Apply pagination
	if opts.Offset > 0 && opts.Offset < len(apps) {
		apps = apps[opts.Offset:]
	} else if opts.Offset >= len(apps) {
		return []*Application{}, nil
	}

	if opts.Limit > 0 && opts.Limit < len(apps) {
		apps = apps[:opts.Limit]
	}

	return apps, nil
}

// ValidateClientSecret validates a client secret using bcrypt.
func (s *BackendStorage) ValidateClientSecret(ctx context.Context, clientID, clientSecret string) (bool, error) {
	app, err := s.GetApp(ctx, clientID)
	if err != nil {
		return false, err
	}

	err = bcrypt.CompareHashAndPassword([]byte(app.ClientSecret), []byte(clientSecret))
	return err == nil, nil
}

// -----------------------------------------------------------------------------
// TokenStorage implementation
// -----------------------------------------------------------------------------

// StoreAuthCode stores an authorization code.
func (s *BackendStorage) StoreAuthCode(ctx context.Context, code *AuthCodeData) error {
	data, err := json.Marshal(code)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.authcode.store", Err: err}
	}

	return s.put(ctx, authCodeKey(code.Code), data)
}

// GetAuthCode retrieves an authorization code.
func (s *BackendStorage) GetAuthCode(ctx context.Context, code string) (*AuthCodeData, error) {
	data, err := s.get(ctx, authCodeKey(code))
	if err != nil {
		return nil, err
	}

	var authCode AuthCodeData
	if err := json.Unmarshal(data, &authCode); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.authcode.get", Err: err}
	}

	return &authCode, nil
}

// DeleteAuthCode removes an authorization code.
func (s *BackendStorage) DeleteAuthCode(ctx context.Context, code string) error {
	return s.delete(ctx, authCodeKey(code))
}

// StoreToken stores a token.
func (s *BackendStorage) StoreToken(ctx context.Context, token *Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.token.store", Err: err}
	}

	if err := s.put(ctx, tokenKey(token.TenantID, token.JTI), data); err != nil {
		return err
	}
	// Dual-write the legacy global token record during the migration window.
	_ = s.put(ctx, legacyTokenKey(token.JTI), data)

	// Create token value index for refresh tokens. The value carries the
	// token's tenant so GetTokenByValue can resolve the tenant-scoped token
	// key without an ambient tenant in context: value = "<tenantID>:<jti>".
	if token.TokenType == TokenTypeRefresh {
		_ = s.put(ctx, tokenValueKey(token.JTI), []byte(token.TenantID+":"+token.JTI))
	}

	return nil
}

// GetToken retrieves a token by JTI.
func (s *BackendStorage) GetToken(ctx context.Context, jti string) (*Token, error) {
	// Resolve via the tenant-scoped key (tenant from context, set by the
	// handler from the JWT tenant_id claim) with a legacy global fallback.
	tenantID := TenantFromContext(ctx)
	// Always attempt the tenant-scoped key first (an empty tenant still has a
	// distinct scoped key, e.g. "__iam:token::<jti>"), then fall back to the
	// legacy un-scoped global key (dual-read shim).
	data, err := s.get(ctx, tokenKey(tenantID, jti))
	if err != nil {
		data, err = s.get(ctx, legacyTokenKey(jti))
		if err != nil {
			return nil, err
		}
	}

	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.token.get", Err: err}
	}

	return &token, nil
}

// GetTokenByValue retrieves a token by its string value.
func (s *BackendStorage) GetTokenByValue(ctx context.Context, tokenValue string) (*Token, error) {
	jtiData, err := s.get(ctx, tokenValueKey(tokenValue))
	if err != nil {
		return nil, err
	}

	// The value index stores "<tenantID>:<jti>" for tenant-scoped resolution.
	// Legacy entries stored only the bare jti; handle both.
	raw := string(jtiData)
	jti := raw
	if idx := strings.Index(raw, ":"); idx >= 0 {
		ctx = ContextWithTenant(ctx, raw[:idx])
		jti = raw[idx+1:]
	}
	return s.GetToken(ctx, jti)
}

// RevokeToken marks a token as revoked.
func (s *BackendStorage) RevokeToken(ctx context.Context, jti string) error {
	token, err := s.GetToken(ctx, jti)
	if err != nil {
		return err
	}

	token.Revoked = true

	data, err := json.Marshal(token)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.token.revoke", Err: err}
	}

	// Persist under the token's own (authoritative) tenant-scoped key, and
	// clear any legacy global entry so the revocation is observed regardless
	// of which key a future read resolves.
	if err := s.put(ctx, tokenKey(token.TenantID, jti), data); err != nil {
		return err
	}
	// Keep the legacy global record in sync during the migration window so a
	// legacy-path read observes the revocation too.
	_ = s.put(ctx, legacyTokenKey(jti), data)
	return nil
}

// IsTokenRevoked checks if a token is revoked.
func (s *BackendStorage) IsTokenRevoked(ctx context.Context, jti string) (bool, error) {
	token, err := s.GetToken(ctx, jti)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return token.Revoked, nil
}

// DeleteExpiredTokens removes expired tokens.
func (s *BackendStorage) DeleteExpiredTokens(ctx context.Context) error {
	prefix := KeyPrefixIAM + "token:"
	values, err := s.list(ctx, prefix)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, data := range values {
		var token Token
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}
		if token.Expires.Before(now) {
			_ = s.delete(ctx, tokenKey(token.TenantID, token.JTI))
			_ = s.delete(ctx, legacyTokenKey(token.JTI))
			// Also clean up the value index for refresh tokens
			if token.TokenType == TokenTypeRefresh {
				_ = s.delete(ctx, tokenValueKey(token.JTI))
			}
		}
	}

	return nil
}

// ListTokensByUser retrieves tokens for a user.
func (s *BackendStorage) ListTokensByUser(ctx context.Context, userID string) ([]*Token, error) {
	prefix := KeyPrefixIAM + "token:"
	values, err := s.list(ctx, prefix)
	if err != nil {
		return nil, err
	}

	// The migration shim dual-writes each token under both its tenant-scoped
	// key and the legacy global key, so the prefix scan can return two copies
	// of the same token. De-duplicate by JTI.
	tokens := make([]*Token, 0)
	seen := make(map[string]struct{})
	for _, data := range values {
		var token Token
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}
		if token.Subject != userID {
			continue
		}
		if _, dup := seen[token.JTI]; dup {
			continue
		}
		seen[token.JTI] = struct{}{}
		tokens = append(tokens, &token)
	}

	return tokens, nil
}

// RevokeUserTokens revokes all tokens for a user.
func (s *BackendStorage) RevokeUserTokens(ctx context.Context, userID string) error {
	tokens, err := s.ListTokensByUser(ctx, userID)
	if err != nil {
		return err
	}

	for _, token := range tokens {
		// Revoke under the token's own tenant so GetToken inside RevokeToken
		// resolves the tenant-scoped key.
		tokenCtx := ContextWithTenant(ctx, token.TenantID)
		if err := s.RevokeToken(tokenCtx, token.JTI); err != nil {
			continue // Best effort
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// PolicyStorage implementation
// -----------------------------------------------------------------------------

// CreatePolicy creates a new policy.
func (s *BackendStorage) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	key := policyKey(tenantID, policy.Metadata.Name)

	_, err := s.get(ctx, key)
	if err == nil {
		return ErrAlreadyExists
	}

	data, err := json.Marshal(policy)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.policy.create", Err: err}
	}

	return s.put(ctx, key, data)
}

// GetPolicy retrieves a policy by name.
func (s *BackendStorage) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	data, err := s.get(ctx, policyKey(tenantID, name))
	if err != nil {
		return nil, err
	}

	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.policy.get", Err: err}
	}

	return &policy, nil
}

// UpdatePolicy updates an existing policy.
func (s *BackendStorage) UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	key := policyKey(tenantID, policy.Metadata.Name)

	_, err := s.get(ctx, key)
	if err != nil {
		return err
	}

	data, err := json.Marshal(policy)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.policy.update", Err: err}
	}

	return s.put(ctx, key, data)
}

// DeletePolicy deletes a policy.
func (s *BackendStorage) DeletePolicy(ctx context.Context, tenantID, name string) error {
	_, err := s.get(ctx, policyKey(tenantID, name))
	if err != nil {
		return err
	}
	return s.delete(ctx, policyKey(tenantID, name))
}

// ListPolicies lists policies for a tenant.
func (s *BackendStorage) ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error) {
	prefix := KeyPrefixTenant + tenantID + KeyPrefixPolicy
	values, err := s.list(ctx, prefix)
	if err != nil {
		return nil, err
	}

	policies := make([]*Policy, 0, len(values))
	for _, data := range values {
		var policy Policy
		if err := json.Unmarshal(data, &policy); err != nil {
			continue
		}
		policies = append(policies, &policy)
	}

	// Apply pagination
	if opts.Offset > 0 && opts.Offset < len(policies) {
		policies = policies[opts.Offset:]
	} else if opts.Offset >= len(policies) {
		return []*Policy{}, nil
	}

	if opts.Limit > 0 && opts.Limit < len(policies) {
		policies = policies[:opts.Limit]
	}

	return policies, nil
}
