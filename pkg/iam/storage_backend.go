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
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

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
	// Root prefix for all IAM data.
	KeyPrefixIAM = "__iam:"

	// Tenant keys.
	KeyPrefixTenant         = "__iam:tenant:"
	KeyPrefixTenantSettings = ":settings"
	KeyPrefixTenantQuota    = ":quota"

	// Identity keys (scoped by tenant). User keys are absent: the user record
	// lives in qrdb (qrdbadapter.QRDBUserStorage), not the BaaS KVStore.
	KeyPrefixRole   = ":role:"
	KeyPrefixPolicy = ":policy:"

	// OAuth2/OIDC keys (scoped by tenant).
	KeyPrefixApp      = ":app:"
	KeyPrefixToken    = ":token:"
	KeyPrefixAuthCode = ":authcode:"

	// Session keys (scoped by tenant).
	KeyPrefixSession = ":session:"

	// Tenant-scoped secondary indexes. The segment deliberately sits OUTSIDE
	// the record prefixes above (`:idx:` rather than under `:role:`) so a
	// record listing — ListRoles scans `__iam:tenant:<id>:role:` — never sweeps
	// up index entries and tries to decode one as a record.
	KeyPrefixRoleNameIndex = ":idx:rolename:"

	// Global indexes. The username, email, and PIV-thumbprint lookup indexes
	// moved to qrdb with the user record (phase C1-F); only the app index
	// remains BaaS-owned.
	KeyPrefixAppIndex = "__iam:idx:app:"
)

// BackendStorage implements the BaaS-owned IAM storage interfaces using
// interfaces.KVStore: TenantStorage, RoleStorage, PolicyStorage,
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

	// logger receives operator-facing events; nil means they are dropped.
	logger storageLogger
}

// NewBackendStorage creates a new IAM storage backed by a KVStore.
// The KVStore abstracts the underlying storage (memory, PebbleDB, Raft).
func NewBackendStorage(kv interfaces.KVStore, opts ...StorageOption) *BackendStorage {
	o := applyStorageOptions(opts)
	return &BackendStorage{
		kv:     kv,
		logger: o.logger,
	}
}

// Compile-time interface checks. UserStorage is intentionally absent:
// BackendStorage no longer persists users (qrdb owns the user record).
var (
	_ TenantStorage = (*BackendStorage)(nil)
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

// roleKey returns the key for a role record.
func roleKey(tenantID, roleID string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixRole + roleID
}

// roleNameIndexKey returns the key of the tenant-scoped role-name index entry.
// The value stored under it is the role ID, mirroring the tenant->app index
// (appTenantKey), whose value is likewise the record's own identifier.
func roleNameIndexKey(tenantID, name string) string {
	return KeyPrefixTenant + tenantID + KeyPrefixRoleNameIndex + name
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

// appTenantContext scopes ctx to the tenant an application record names.
//
// The IAM KVStore routes every read and write by the tenant bound to the
// CONTEXT, never by the tenant segment in the key
// (pkg/server/tenant_kvstore.go tenantFromAnyContext), and a barrier-isolated
// tenant's records live in that tenant's own store. An application therefore
// has to be addressed under its OWN tenant on both sides, or the write and the
// read pick different namespaces.
//
// That divergence is go-quicbaas#644: POST /api/v1/fleet/enroll is a public
// route, so it carries no identity and its context named no tenant, while POST
// /oauth2/token scopes its context to the tenant encoded in the client id. The
// record was written into the shared store and looked for in the tenant's, so
// every credential the register endpoint issued was refused with
// invalid_client on its first use. The same shape was fixed for pooled
// accounts in #535 and for projects in #634.
//
// The record's tenant overrides whatever the context carried: the record is the
// authority, and a caller allowed to name another tenant was admitted by the
// management surface (AppManager.GetAppInTenant / UpdateAppInTenant /
// DeleteAppInTenant) before reaching this store.
func appTenantContext(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return ContextWithTenant(ctx, tenantID)
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

// tokenValueIndexContext strips the ambient tenant so the refresh-token value
// index is written to, and read from, the SHARED store on both sides.
//
// The index is the ONLY handle a refresh carries. A client presents an opaque
// secret and nothing else, so POST /api/v1/iam/refresh runs with no bearer and
// therefore no tenant in context — while issuance runs inside the login's
// tenant-scoped context. Since the IAM KVStore routes by that context, writing
// the index under the issuing tenant buried it in that tenant's own keyspace
// (its barrier for a dedicated tenant, its namespace within the shared store
// for a prefix tenant), where the untenanted redemption read can never reach
// it: every non-system-tenant session was unrenewable and its first refresh
// 401'd (go-quicbaas #274).
//
// The empty tenant is SAFE to write under, and is not a new keyspace: qrdb's
// store resolver treats "" and the system tenant id as the same tenant and
// canonicalizes both to one namespace (tenant.IdentityNamespace), so this
// reaches exactly the store the system tenant already used on both sides —
// which is why the defect stayed invisible until the first tenant login, and
// why system-tenant credentials issued before this fix keep resolving.
//
// Only this POINTER is global. Its value is "<tenantID>:<jti>" and
// GetTokenByValue re-scopes the context from it before reading the token
// record, so the record itself — subject, scopes, frozen session context —
// stays inside the tenant's own store. The index key is the SHA-256 of a
// 256-bit secret, so resolving an entry requires already holding the
// credential, and a sealed barrier tenant still fails closed on the record read
// that follows.
//
// This assumes no ORCHESTRATION tenant is bound to a token-storage context:
// the routing store reads the orchestration context key when the IAM one is
// empty (pkg/server tenantFromAnyContext), and pkg/iam cannot clear a key it
// must not depend on. Token issuance and refresh are IAM request paths, and
// the orchestration key is stamped only by the orchestrator handlers, so the
// two never meet — a future path that stamps both would have to re-check this.
func tokenValueIndexContext(ctx context.Context) context.Context {
	return ContextWithTenant(ctx, "")
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
	// The tenant's own store holds both the index entries and the records.
	ctx = appTenantContext(ctx, tenantID)

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
		if tid := TenantFromClientID(clientID); tid != "" && tid != tenantID {
			_ = s.delete(appTenantContext(ctx, tid), appKey(tid, clientID))
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
// delete): roles, policies, apps, sessions, settings, and quotas.
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

	// 1. Delete roles, and the name index that points at them — an index entry
	// left behind would make a recreated tenant's role name look taken.
	rolePrefix := tenantPrefix + KeyPrefixRole
	if err := s.deleteByPrefix(ctx, rolePrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.roles",
			Err:  err,
		}
	}
	if err := s.deleteByPrefix(ctx, tenantPrefix+KeyPrefixRoleNameIndex); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.role_name_index",
			Err:  err,
		}
	}

	// 2. Delete policies
	policyPrefix := tenantPrefix + KeyPrefixPolicy
	if err := s.deleteByPrefix(ctx, policyPrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.policies",
			Err:  err,
		}
	}

	// 3. Delete apps and their global indexes
	appPrefix := tenantPrefix + KeyPrefixApp
	if err := s.deleteAppsWithIndexes(ctx, appPrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.apps",
			Err:  err,
		}
	}

	// 4. Delete sessions
	sessionPrefix := tenantPrefix + KeyPrefixSession
	if err := s.deleteByPrefix(ctx, sessionPrefix); err != nil {
		return &dberrors.QBError{
			Code: dberrors.ErrInternal,
			Op:   "iam.tenant.delete.sessions",
			Err:  err,
		}
	}

	// 5. Delete tenant settings (ignore not found)
	_ = s.delete(ctx, tenantSettingsKey(id))

	// 6. Delete tenant quota (ignore not found)
	_ = s.delete(ctx, tenantQuotaKey(id))

	// 8. Finally delete the tenant record
	return s.delete(ctx, key)
}

// ListTenants retrieves tenants with pagination.
// Uses key-based filtering to only include actual tenant records, excluding
// sub-resource keys (settings, quotas, users, roles, etc.) that share
// the same key prefix.
func (s *BackendStorage) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	prefix := KeyPrefixTenant

	// Scan with key filtering: only include keys that represent actual tenant
	// records. Tenant keys are "__iam:tenant:<tenantID>" with no additional
	// colon-separated segments. Sub-resource keys like settings, quotas, users
	// and roles all contain additional ":" separators after the tenant ID.
	var tenants []*Tenant
	err := s.kv.Scan(ctx, prefix, func(key string, value []byte) error {
		// Extract the portion after the prefix (the tenant ID portion)
		suffix := strings.TrimPrefix(key, prefix)

		// Skip sub-resource keys: settings, quotas, users, roles, etc.
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
// RoleStorage implementation
// -----------------------------------------------------------------------------

// CreateRole stores a new role and its tenant-scoped name index entry.
//
// A role whose NAME is already taken in the tenant is refused with
// ErrAlreadyExists, not just one whose ID is. That matches the canonical qrdb
// role store (name-unique per tenant, RoleNameExistsError) and the in-memory
// double, and it is what makes the name index answerable: an index maps one
// name to one role, so a second role wearing the name would either overwrite
// the entry or be invisible to GetRoleByName. Uniqueness is the invariant the
// index encodes, so the store has to hold it.
func (s *BackendStorage) CreateRole(ctx context.Context, role *Role) error {
	key := roleKey(role.TenantID, role.ID)

	_, err := s.get(ctx, key)
	if err == nil {
		return ErrAlreadyExists
	}

	if err := s.roleNameAvailable(ctx, role.TenantID, role.Name, role.ID); err != nil {
		return err
	}

	data, err := json.Marshal(role)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.role.create", Err: err}
	}

	if err := s.put(ctx, key, data); err != nil {
		return err
	}
	return s.putRoleNameIndex(ctx, role.TenantID, role.Name, role.ID)
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

// GetRoleByName retrieves a role by its tenant-scoped name through the name
// index — one index read plus one record read, independent of how many roles
// the tenant holds.
//
// An index entry that names a missing record surfaces the record read's error
// rather than being reported as "no such role". A dangling pointer is a store
// inconsistency, and answering it as an ordinary absence would let a caller
// that keys idempotency off not-found take the create path against a store
// that still holds the name. This is the same choice GetTokenByValue makes for
// its own value index.
func (s *BackendStorage) GetRoleByName(ctx context.Context, tenantID, name string) (*Role, error) {
	if name == "" {
		return nil, ErrRoleNotFound
	}

	roleID, err := s.get(ctx, roleNameIndexKey(tenantID, name))
	if err != nil {
		if dberrors.IsNotFound(err) {
			// The SENTINEL, matching GetRole on the in-memory store:
			// IsRoleNotFound — and qrdb's role.NameResolver contract, which
			// distinguishes "no such role" from "could not ask" — read the
			// typed inner error, which a bare not-found code does not carry.
			return nil, ErrRoleNotFound
		}
		// A store failure is NOT an absent role. Reporting it as one would let
		// first-boot bind a grant to a derived identifier while the tenant
		// holds the role under another, so it fails closed instead.
		return nil, err
	}
	return s.GetRole(ctx, tenantID, string(roleID))
}

// UpdateRole updates an existing role, moving the name index entry when the
// update RENAMES the role: the old name stops resolving and the new one starts,
// in that order, so no two names ever point at this role at once.
//
// A rename onto a name another role already holds is refused, for the reason
// CreateRole states — the index cannot represent two roles of one name, and
// silently overwriting the entry would make the displaced role unresolvable by
// its own name.
func (s *BackendStorage) UpdateRole(ctx context.Context, role *Role) error {
	key := roleKey(role.TenantID, role.ID)

	existing, err := s.GetRole(ctx, role.TenantID, role.ID)
	if err != nil {
		return err
	}

	renamed := existing.Name != role.Name
	if renamed {
		if err := s.roleNameAvailable(ctx, role.TenantID, role.Name, role.ID); err != nil {
			return err
		}
	}

	data, err := json.Marshal(role)
	if err != nil {
		return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "iam.role.update", Err: err}
	}

	if err := s.put(ctx, key, data); err != nil {
		return err
	}
	// Written unconditionally: on a rename it establishes the new name, and on
	// an ordinary update it RE-ASSERTS the entry, so a record left unindexed by
	// an earlier partial write is repaired by the next update.
	if err := s.putRoleNameIndex(ctx, role.TenantID, role.Name, role.ID); err != nil {
		return err
	}
	if !renamed {
		return nil
	}
	return s.deleteRoleNameIndex(ctx, role.TenantID, existing.Name)
}

// DeleteRole removes a role and its name index entry, so the name is free for
// a later create and cannot resolve to a record that is gone.
func (s *BackendStorage) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	role, err := s.GetRole(ctx, tenantID, roleID)
	if err != nil {
		return err
	}
	if err := s.delete(ctx, roleKey(tenantID, roleID)); err != nil {
		return err
	}
	return s.deleteRoleNameIndex(ctx, tenantID, role.Name)
}

// roleNameAvailable reports ErrAlreadyExists when a role OTHER than selfID
// already holds name in the tenant. An empty name indexes nothing, so it
// cannot collide.
func (s *BackendStorage) roleNameAvailable(ctx context.Context, tenantID, name, selfID string) error {
	if name == "" {
		return nil
	}
	holder, err := s.get(ctx, roleNameIndexKey(tenantID, name))
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil
		}
		// A store failure must not be read as "the name is free": that would
		// admit the duplicate the index cannot represent.
		return err
	}
	if string(holder) == selfID {
		return nil
	}
	return ErrAlreadyExists
}

// putRoleNameIndex writes the name -> role ID entry. An empty name is not
// addressable and gets no entry.
func (s *BackendStorage) putRoleNameIndex(ctx context.Context, tenantID, name, roleID string) error {
	if name == "" {
		return nil
	}
	return s.put(ctx, roleNameIndexKey(tenantID, name), []byte(roleID))
}

// deleteRoleNameIndex removes the entry for name. A name that was never
// indexed has nothing to remove.
func (s *BackendStorage) deleteRoleNameIndex(ctx context.Context, tenantID, name string) error {
	if name == "" {
		return nil
	}
	return s.delete(ctx, roleNameIndexKey(tenantID, name))
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
	// The tenant on the record is authoritative for BOTH the key and the store
	// the key is written to (appTenantContext, go-quicbaas#644). The clientID
	// encodes the same tenant when it was minted by generateClientID, which is
	// how the read path resolves it when no tenant is on the context.
	ctx = appTenantContext(ctx, app.TenantID)
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
	// Resolve the owning tenant, then read from THAT tenant's store — the key
	// segment alone is not enough, because the KVStore routes by the context
	// (appTenantContext, go-quicbaas#644). The tenant is parsed from the
	// clientID (format app_<tenant>_<suffix>) first; when the clientID encodes
	// no tenant, the context tenant is used. Both fall back to the legacy
	// global key (dual-read shim) for deployments predating scoping.
	//
	// This seam is deliberately NOT an authorization boundary: it answers "who
	// owns this client id", and callers holding a tenant must go through
	// GetAppByTenant (AppManager.GetAppInTenant) so a client id from one tenant
	// cannot address a client in another.
	var data []byte
	var err error = &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.get"}
	if tid := TenantFromClientID(clientID); tid != "" {
		data, err = s.get(appTenantContext(ctx, tid), appKey(tid, clientID))
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

// GetAppByTenant retrieves an application by tenant and client ID. The named
// tenant is authoritative: the read is scoped to that tenant's store, and a
// record whose own TenantID disagrees is reported as not found. This is the
// seam every management surface uses, so a client id minted for one tenant can
// never address a client in another.
func (s *BackendStorage) GetAppByTenant(ctx context.Context, tenantID, clientID string) (*Application, error) {
	app, err := s.GetApp(appTenantContext(ctx, tenantID), clientID)
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
	ctx = appTenantContext(ctx, app.TenantID)
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
	ctx = appTenantContext(ctx, app.TenantID)

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
	// The listed tenant owns both the index and the records, so the scan and
	// every resolution run against that tenant's store (go-quicbaas#644).
	appCtx := appTenantContext(ctx, tenantID)

	// Get app IDs from tenant index
	prefix := KeyPrefixTenant + tenantID + KeyPrefixApp
	values, err := s.list(appCtx, prefix)
	if err != nil {
		return nil, err
	}
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

// ValidateClientSecret validates a client secret against the stored digest.
//
// Only the current PBKDF2-HMAC-SHA256 form is verified; a pre-#37 bcrypt
// record is refused with ErrSecretRotationRequired (owner-ruled 2026-08-25).
//
// This path does not rehash, and with the legacy form refused there is nothing
// left to rehash from -- a stored digest changes only when the secret is
// rotated. The previous note about deferring an online upgrade to #119 no
// longer applies and has been removed rather than left to mislead: it
// described a rehash-on-auth this function never performed.
func (s *BackendStorage) ValidateClientSecret(ctx context.Context, clientID, clientSecret string) (bool, error) {
	// Cancellation is checked before any work: the derivation below is not
	// cancellable, so a caller that has already gone away must not buy one.
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if clientSecret == "" {
		// Independent of stored state, so it probes nothing and costs nothing.
		s.warnClientAuthFailed(clientID, TenantFromContext(ctx), clientAuthReasonEmptySecret)
		return false, ErrSecretEmpty
	}

	app, err := s.GetApp(ctx, clientID)
	if err != nil {
		// ONLY a genuine not-found is equalized. A lookup miss that answered
		// without deriving would leak which client IDs exist, so that one pays
		// the same cost as a real verification.
		//
		// Every other GetApp error -- cancellation, deadline, a backend
		// outage -- is propagated immediately and unequalized. Deriving for
		// those would burn 600,000 uncancellable iterations for a caller that
		// has already left, and during a storage incident it would add that
		// cost to every failing authentication, amplifying the outage it is
		// reacting to. Nor is there anything to hide: a backend failure is not
		// a per-client signal, so answering fast reveals nothing about which
		// client IDs exist (Codex review of #118).
		if dberrors.IsNotFound(err) {
			_ = verifySecret(timingEqualizerDigest, clientSecret)
			s.warnClientAuthFailed(clientID, TenantFromContext(ctx), clientAuthReasonUnknownClient)
		}
		return false, err
	}

	// A record resolved out of a different tenant than the request was scoped
	// to is reported, not refused: enforcement belongs to the tenant-named read
	// seam (GetAppByTenant, which every management surface goes through), and
	// the token endpoints derive their scope from the credential itself, so a
	// mismatch here means the deployment holds a record whose namespace and
	// tenant field disagree. That is the state go-quicbaas#644 created, and an
	// operator who sees this line knows to re-enroll rather than to hunt for a
	// broken token endpoint.
	if ctxTenant := TenantFromContext(ctx); ctxTenant != "" && app.TenantID != "" && app.TenantID != ctxTenant {
		s.warnClientAuthTenantMismatch(clientID, ctxTenant, app.TenantID)
	}

	ok, err := verifyClientSecret(app.ClientSecret, clientSecret)
	if err != nil {
		// A pre-KDF digest already names itself, and names the remedy; a
		// second, vaguer line would only bury it.
		if errors.Is(err, ErrSecretRotationRequired) {
			warnRotationRequired(s.logger, clientID)
		} else {
			s.warnClientAuthFailed(clientID, app.TenantID, clientAuthReasonSecretUnusable)
		}
		return false, err
	}
	if !ok {
		s.warnClientAuthFailed(clientID, app.TenantID, clientAuthReasonSecretMismatch)
	}
	return ok, nil
}

// Reasons a client authentication was refused. They name the check that
// refused, never the material presented, and they are written to the operator
// log only -- the OAuth2 response stays the single opaque invalid_client for
// every one of them.
const (
	clientAuthReasonEmptySecret    = "empty client_secret"
	clientAuthReasonUnknownClient  = "unknown client_id"
	clientAuthReasonSecretMismatch = "client_secret mismatch"
	clientAuthReasonSecretUnusable = "stored client_secret unusable"
	clientAuthReasonTenantMismatch = "tenant mismatch"
)

// warnClientAuthFailed records which check refused a client authentication.
//
// Before this, POST /oauth2/token answered 401 invalid_client with NO server
// log line at all, and the grant handler collapsed the lookup error and the
// verdict into one condition -- so an unknown client, a wrong secret and a
// record the store could not reach were indistinguishable to an operator and
// in the code alike (go-quicbaas#644). Diagnosing a fleet-wide enrollment
// outage needed a hand-built reproduction against a live node.
//
// This does NOT weaken the non-enumeration guarantees the response side keeps:
// the OAuth2 error body and status are identical for every reason, and every
// refusal still performs exactly one key derivation, so neither the answer nor
// the time it took distinguishes a client that exists from one that does not.
// The reason exists only in the operator's log, which an unauthenticated caller
// cannot read. No secret, presented or stored, is ever an argument here.
func (s *BackendStorage) warnClientAuthFailed(clientID, tenantID, reason string) {
	if s.logger == nil {
		return
	}
	s.logger.Warnf("oauth2 client authentication failed: client_id=%s tenant=%s reason=%s",
		clientID, tenantID, reason)
}

// warnClientAuthTenantMismatch records a record whose stored tenant disagrees
// with the tenant the request was scoped to. See warnClientAuthFailed for why
// the reason never reaches the caller.
func (s *BackendStorage) warnClientAuthTenantMismatch(clientID, ctxTenant, appTenant string) {
	if s.logger == nil {
		return
	}
	s.logger.Warnf("oauth2 client authentication resolved a mis-homed record: client_id=%s tenant=%s app_tenant=%s reason=%s",
		clientID, ctxTenant, appTenant, clientAuthReasonTenantMismatch)
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

	// Create the token value index for opaque (refresh) tokens. The index is
	// keyed by the SHA-256 of the token's VALUE — the secret the client
	// presents — because that is the only handle a refresh request carries.
	// (Keying it by JTI, as an earlier revision did, made GetTokenByValue
	// unresolvable and silently broke every refresh grant.) The stored value
	// carries the token's tenant so GetTokenByValue can resolve the
	// tenant-scoped token key without an ambient tenant in context:
	// value = "<tenantID>:<jti>". It is written untenanted for the same
	// reason — see tokenValueIndexContext.
	//
	// A failed index write is FATAL, unlike the legacy dual-write above. The
	// index is the credential's only resolution path, so swallowing the error
	// hands the client a refresh token that is already dead and defers the
	// failure to a 401 fifteen minutes later.
	if token.TokenType == TokenTypeRefresh && token.ValueHash != "" {
		if err := s.put(tokenValueIndexContext(ctx), tokenValueKey(token.ValueHash), []byte(token.TenantID+":"+token.JTI)); err != nil {
			return err
		}
		// Dual-write the tenant-scoped entry for the migration window, so a
		// ROLLBACK to the tenant-routed reader still resolves credentials
		// minted while this version was deployed. Best-effort, unlike the
		// shared write above: this copy is only insurance for a downgrade, and
		// the shared entry is what every reader on this version uses.
		_ = s.put(ctx, tokenValueKey(token.ValueHash), []byte(token.TenantID+":"+token.JTI))
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

// GetTokenByValue retrieves a token by its opaque string value. The presented
// secret is hashed to derive the index key (see StoreToken).
//
// The index is a POINTER, and a pointer is a claim, not a proof. The record it
// names is therefore re-checked against the presented secret before it is
// returned: the resolved Token.ValueHash must equal the hash of what the caller
// actually holds. Without that check, anything able to write the (now shared)
// index — a corrupted entry, a stale one left by an earlier scheme, a writer
// with shared-store access — could point an attacker-known secret hash at a
// victim's refresh JTI and have RefreshSession redeem that session. The
// comparison is constant-time and the mismatch is reported as not-found, so a
// probe cannot distinguish a bad pointer from an unknown secret.
func (s *BackendStorage) GetTokenByValue(ctx context.Context, tokenValue string) (*Token, error) {
	wantHash := HashTokenValue(tokenValue)

	jtiData, err := s.get(tokenValueIndexContext(ctx), tokenValueKey(wantHash))
	if err != nil {
		// Dual-read for the migration window: entries written before the index
		// moved to the shared store live in the ISSUING tenant's keyspace, and
		// a tenant-scoped caller (the OAuth2 token endpoint scopes itself from
		// the client id) is the only one that can still reach them. Without
		// this, upgrading would strand every non-system-tenant OAuth refresh
		// token that worked before it.
		if TenantFromContext(ctx) == "" {
			return nil, err
		}
		jtiData, err = s.get(ctx, tokenValueKey(wantHash))
		if err != nil {
			return nil, err
		}
	}

	// The value index stores "<tenantID>:<jti>" for tenant-scoped resolution.
	// Legacy entries stored only the bare jti; handle both.
	raw := string(jtiData)
	jti := raw
	if idx := strings.Index(raw, ":"); idx >= 0 {
		ctx = ContextWithTenant(ctx, raw[:idx])
		jti = raw[idx+1:]
	}
	token, err := s.GetToken(ctx, jti)
	if err != nil {
		return nil, err
	}
	if subtle.ConstantTimeCompare([]byte(token.ValueHash), []byte(wantHash)) != 1 {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.token.get_by_value"}
	}
	return token, nil
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
			// Also clean up the value index for refresh tokens. It is keyed by
			// the token's ValueHash — deleting tokenValueKey(JTI), as this once
			// did, matched nothing and leaked an entry per expired token. BOTH
			// copies StoreToken writes are removed: the shared one every reader
			// on this version uses, and the tenant-scoped rollback copy. Leaving
			// the second behind would reintroduce the same leak one store over.
			if token.TokenType == TokenTypeRefresh && token.ValueHash != "" {
				indexKey := tokenValueKey(token.ValueHash)
				_ = s.delete(tokenValueIndexContext(ctx), indexKey)
				_ = s.delete(withTokenTenant(ctx, token.TenantID), indexKey)
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
