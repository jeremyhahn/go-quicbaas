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
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// MemoryStorage implements all IAM storage interfaces using in-memory maps.
// This implementation is thread-safe and suitable for testing.
type MemoryStorage struct {
	mu sync.RWMutex

	// logger receives operator-facing events; nil means they are dropped.
	logger storageLogger

	// Tenant storage
	tenants        map[string]*Tenant
	tenantSettings map[string]*TenantSettings
	tenantQuotas   map[string]*TenantQuota

	// User storage
	users                map[string]*User // key: tenantID:userID
	usersByEmail         map[string]*User // key: tenantID + ":" + email
	usersByPIVThumbprint map[string]*User // key: piv_thumbprint

	// nextUserID mints the user identity. The STORE assigns it, exactly as the
	// qrdb-backed storage does: a caller cannot supply one, so there is no
	// second identifier space for anything to reconcile against.
	nextUserID atomic.Uint64

	// Role storage
	roles map[string]*Role // key: tenantID:roleID

	// Policy storage
	policies map[string]*Policy // key: tenantID:policyName

	// App storage
	apps map[string]*Application // key: clientID

	// Token storage
	authCodes     map[string]*AuthCodeData // key: code
	tokens        map[string]*Token        // key: jti
	tokenValues   map[string]*Token        // key: token value (for refresh tokens)
	revokedTokens map[string]bool          // key: jti

	// Session storage (future)
	sessions map[string]*Session // key: sessionID

	// Credential storage (future)
	credentials map[string]*Credential // key: credentialID
}

// NewMemoryStorage creates a new in-memory storage instance.
func NewMemoryStorage(opts ...StorageOption) *MemoryStorage {
	o := applyStorageOptions(opts)
	return &MemoryStorage{
		logger:               o.logger,
		tenants:              make(map[string]*Tenant),
		tenantSettings:       make(map[string]*TenantSettings),
		tenantQuotas:         make(map[string]*TenantQuota),
		users:                make(map[string]*User),
		usersByEmail:         make(map[string]*User),
		usersByPIVThumbprint: make(map[string]*User),
		roles:                make(map[string]*Role),
		policies:             make(map[string]*Policy),
		apps:                 make(map[string]*Application),
		authCodes:            make(map[string]*AuthCodeData),
		tokens:               make(map[string]*Token),
		tokenValues:          make(map[string]*Token),
		revokedTokens:        make(map[string]bool),
		sessions:             make(map[string]*Session),
		credentials:          make(map[string]*Credential),
	}
}

// Helper function to create composite keys.
func makeKey(parts ...string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ":"
		}
		result += p
	}
	return result
}

// TenantStorage implementation

func (m *MemoryStorage) CreateTenant(ctx context.Context, tenant *Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenant.ID]; exists {
		return ErrTenantAlreadyExists
	}

	// Create a copy to avoid external modifications
	t := *tenant
	m.tenants[tenant.ID] = &t

	// Initialize BaaS-layer IAM quotas (0 means unlimited).
	// Engine-level quotas (users, roles, storage) are owned by qrdb TenantServicer.
	quota := &TenantQuota{
		MaxPolicies: int64(tenant.Quotas.MaxPolicies),
		MaxApps:     int64(tenant.Quotas.MaxApps),
	}
	m.tenantQuotas[tenant.ID] = quota

	return nil
}

func (m *MemoryStorage) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tenant, exists := m.tenants[id]
	if !exists {
		return nil, NewTenantNotFoundError(id)
	}

	// Return a copy
	t := *tenant
	return &t, nil
}

func (m *MemoryStorage) UpdateTenant(ctx context.Context, tenant *Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenant.ID]; !exists {
		return NewTenantNotFoundError(tenant.ID)
	}

	// Create a copy
	t := *tenant
	t.Updated = time.Now()
	m.tenants[tenant.ID] = &t

	return nil
}

func (m *MemoryStorage) DeleteTenant(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[id]; !exists {
		return NewTenantNotFoundError(id)
	}

	prefix := id + ":"

	// Cascade delete users
	for key, user := range m.users {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(m.users, key)
			if user.Email != "" {
				delete(m.usersByEmail, makeKey(user.TenantID, user.Email))
			}
		}
	}

	// Cascade delete roles
	for key := range m.roles {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(m.roles, key)
		}
	}

	// Cascade delete policies
	for key := range m.policies {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(m.policies, key)
		}
	}

	// Cascade delete apps for this tenant
	for key, app := range m.apps {
		if app.TenantID == id {
			delete(m.apps, key)
		}
	}

	delete(m.tenants, id)
	delete(m.tenantSettings, id)
	delete(m.tenantQuotas, id)

	return nil
}

func (m *MemoryStorage) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Tenant
	for _, tenant := range m.tenants {
		t := *tenant
		result = append(result, &t)
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*Tenant{}, nil
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

func (m *MemoryStorage) CountTenants(ctx context.Context, opts ListOptions) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return int64(len(m.tenants)), nil
}

func (m *MemoryStorage) TenantExists(ctx context.Context, id string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, exists := m.tenants[id]
	return exists, nil
}

func (m *MemoryStorage) GetSettings(ctx context.Context, tenantID string) (*TenantSettings, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.tenants[tenantID]; !exists {
		return nil, NewTenantNotFoundError(tenantID)
	}

	settings, exists := m.tenantSettings[tenantID]
	if !exists {
		// Return empty settings
		return &TenantSettings{}, nil
	}

	// Return a copy
	s := *settings
	return &s, nil
}

func (m *MemoryStorage) UpdateSettings(ctx context.Context, tenantID string, settings *TenantSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenantID]; !exists {
		return NewTenantNotFoundError(tenantID)
	}

	// Create a copy
	s := *settings
	m.tenantSettings[tenantID] = &s

	return nil
}

func (m *MemoryStorage) GetQuota(ctx context.Context, tenantID string) (*TenantQuota, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, exists := m.tenants[tenantID]; !exists {
		return nil, NewTenantNotFoundError(tenantID)
	}

	quota, exists := m.tenantQuotas[tenantID]
	if !exists {
		// Return empty quota
		return &TenantQuota{}, nil
	}

	// Return a copy
	q := *quota
	return &q, nil
}

func (m *MemoryStorage) UpdateQuota(ctx context.Context, tenantID string, quota *TenantQuota) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.tenants[tenantID]; !exists {
		return NewTenantNotFoundError(tenantID)
	}

	// Create a copy
	q := *quota
	m.tenantQuotas[tenantID] = &q

	return nil
}

func (m *MemoryStorage) IncrementQuotaUsage(ctx context.Context, tenantID string, resource string, delta int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	quota, exists := m.tenantQuotas[tenantID]
	if !exists {
		quota = &TenantQuota{}
		m.tenantQuotas[tenantID] = quota
	}

	// Users and roles are engine-level quotas owned by qrdb TenantServicer.
	// Only BaaS-layer IAM quotas are tracked here.
	switch resource {
	case "policies":
		if quota.MaxPolicies > 0 && quota.CurrentPolicies+delta > quota.MaxPolicies {
			return NewQuotaExceededError(tenantID, resource, quota.MaxPolicies, quota.CurrentPolicies)
		}
		quota.CurrentPolicies += delta
	case "apps":
		if quota.MaxApps > 0 && quota.CurrentApps+delta > quota.MaxApps {
			return NewQuotaExceededError(tenantID, resource, quota.MaxApps, quota.CurrentApps)
		}
		quota.CurrentApps += delta
	}

	return nil
}

func (m *MemoryStorage) DecrementQuotaUsage(ctx context.Context, tenantID string, resource string, delta int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	quota, exists := m.tenantQuotas[tenantID]
	if !exists {
		return nil // Nothing to decrement
	}

	// Users and roles are engine-level quotas owned by qrdb TenantServicer.
	switch resource {
	case "policies":
		quota.CurrentPolicies -= delta
		if quota.CurrentPolicies < 0 {
			quota.CurrentPolicies = 0
		}
	case "apps":
		quota.CurrentApps -= delta
		if quota.CurrentApps < 0 {
			quota.CurrentApps = 0
		}
	}

	return nil
}

// UserStorage implementation

func (m *MemoryStorage) CreateUser(ctx context.Context, user *User) error {
	if err := RequireLoginKey(user); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Mint the identity when the record does not carry one. A record that
	// already has an id is being restored (an import, a test fixture standing in
	// for existing data), so its id is kept — and the allocator is advanced past
	// it, or a later mint would hand out an id the store already holds and the
	// create would fail with ErrAlreadyExists for no reason the caller can see.
	if user.ID == "" {
		user.ID = FormatUserID(m.nextUserID.Add(1))
	} else {
		// A supplied id is only ever a RESTORE of an id this store already
		// minted, so it must be a canonical qrdb Snowflake. Storing a malformed
		// one would create an account that every canonical identity operation
		// afterwards — QRDBID, factor binding, the session join — refuses,
		// which is worse than refusing the create: the caller is told it
		// succeeded and the record is unusable.
		seeded, err := ParseUserID(user.ID)
		if err != nil {
			return err
		}
		// Advance the allocator past a restored id, or a later mint would hand
		// out an id the store already holds and the create would fail with
		// ErrAlreadyExists for no reason the caller can see.
		for {
			current := m.nextUserID.Load()
			if current >= seeded || m.nextUserID.CompareAndSwap(current, seeded) {
				break
			}
		}
	}

	key := makeKey(user.TenantID, user.ID)
	if _, exists := m.users[key]; exists {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.user.create"}
	}

	if user.Email != "" {
		if _, exists := m.usersByEmail[makeKey(user.TenantID, user.Email)]; exists {
			return ErrEmailAlreadyExists
		}
	}

	// The handle is tenant-unique here because it is tenant-unique in the
	// qrdb-backed store, which answers a duplicate with UsernameExistsError.
	// A store that accepted one would let every test over it pass while
	// production refused the same write — and, worse, GetUserByUsername scans
	// and returns the FIRST match, so two accounts sharing a handle make that
	// lookup resolve to whichever the map iteration reached first.
	if err := m.usernameTaken(user.TenantID, user.Username, user.ID); err != nil {
		return err
	}

	// Create a copy
	u := *user
	m.users[key] = &u
	// A record with no email holds NO entry in the login-key index, matching
	// the qrdb-backed store: qrdb creates such a record with
	// WithoutEmailLoginKey ("mTLS- or PIV-only identities that authenticate by
	// certificate") and it cannot be resolved by GetUserByEmail. Indexing the
	// empty string instead made every tenant hold at most ONE certificate
	// identity, because the second collided with the first on an address
	// neither of them has.
	if user.Email != "" {
		m.usersByEmail[makeKey(user.TenantID, user.Email)] = &u
	}
	for _, tp := range u.PIVThumbprints() {
		m.usersByPIVThumbprint[tp] = &u
	}

	return nil
}

func (m *MemoryStorage) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := makeKey(tenantID, userID)
	user, exists := m.users[key]
	if !exists {
		// The SENTINEL, not a bare not-found code. Callers that mean "this user
		// is absent" match it with IsUserNotFound, which reads the sentinel's
		// typed inner error; a bare QBError{ErrNotFound} carries no such
		// identity and would be read as an unrelated store failure. The
		// qrdb-backed store already answers with this value, and
		// GetUserByUsername below always did (go-quicbaas#434).
		return nil, ErrUserNotFound
	}

	// Return a copy
	u := *user
	return &u, nil
}

// GetUserByEmail resolves a user by their canonical login key within a tenant.
// The index is tenant-scoped, matching qrdb's: the same address may belong to a
// different person in a different tenant, and a global index would hand the
// wrong record to whichever tenant asked second.
func (m *MemoryStorage) GetUserByEmail(ctx context.Context, tenantID, email string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.usersByEmail[makeKey(tenantID, email)]
	if !exists {
		return nil, ErrUserNotFound
	}

	// Return a copy
	u := *user
	return &u, nil
}

// GetUserByPIVThumbprint retrieves a user by PIV certificate SHA-256 thumbprint.
func (m *MemoryStorage) GetUserByPIVThumbprint(ctx context.Context, thumbprint string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.usersByPIVThumbprint[thumbprint]
	if !exists {
		return nil, ErrUserNotFound
	}

	u := *user
	return &u, nil
}

// usernameTaken reports ErrAlreadyExists when another user in the tenant already
// holds this handle. An empty handle claims nothing: a record without one is not
// addressable by username at all, so it cannot collide.
//
// It SCANS rather than consulting an index, deliberately. The comment on
// GetUserByUsername justifies scanning for lookups because an index "could
// drift on update/delete" — that argument is about maintaining a second
// structure, and it applies here too: a uniqueness check on write costs exactly
// what the read already costs and maintains nothing that can drift. The volume
// this store is built for (tests, embedded callers without a KVStore) makes the
// scan the cheaper correctness guarantee, not merely the simpler one.
//
// The caller holds m.mu, which is what makes the check and the write that
// follows it one operation rather than two racing ones.
func (m *MemoryStorage) usernameTaken(tenantID, username, selfID string) error {
	if username == "" {
		return nil
	}
	for _, existing := range m.users {
		if existing.TenantID == tenantID && existing.Username == username && existing.ID != selfID {
			return ErrAlreadyExists
		}
	}
	return nil
}

// GetUserByUsername retrieves a user by tenant-scoped username (the login Name).
// The in-memory store scans its user map; the volume is small (tests and
// embedded callers without a KVStore), so a scan is preferable to maintaining a
// separate index that could drift on update/delete.
func (m *MemoryStorage) GetUserByUsername(ctx context.Context, tenantID, username string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, user := range m.users {
		// LoginName, not Name: the handle is its own field and the qrdb-backed
		// store keys its username index by that. Matching on Name here would
		// answer a question production answers differently, and every test over
		// this store would pass while the real one failed.
		if user.TenantID == tenantID && user.LoginName() == username {
			u := *user
			return &u, nil
		}
	}
	return nil, ErrUserNotFound
}

func (m *MemoryStorage) UpdateUser(ctx context.Context, user *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(user.TenantID, user.ID)
	oldUser, exists := m.users[key]
	if !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.user.update"}
	}

	// Enforce the SAME transition table qrdb does. Without this the in-memory
	// store silently accepts moves the real one refuses — disabled -> suspended,
	// say — and every test over it passes while production rejects the write.
	// A record already terminal accepts only a re-assertion of that state.
	if user.Lifecycle != "" && user.Lifecycle != oldUser.Lifecycle {
		from := oldUser.Lifecycle
		if from == "" {
			from = LifecycleActive
		}
		if !from.CanTransitionTo(user.Lifecycle) {
			return &dberrors.QBError{
				Code: dberrors.ErrInvalidInput,
				Op:   "iam.user.update",
				Err: &IAMMessageError{Message: "illegal lifecycle transition " +
					string(from) + " -> " + string(user.Lifecycle)},
			}
		}
	}

	// Check if email changed and new email is already taken. An email-less
	// record holds no index entry (see CreateUser), so neither side of the
	// change touches the index when the address is empty.
	if oldUser.Email != user.Email {
		if user.Email != "" {
			if _, exists := m.usersByEmail[makeKey(user.TenantID, user.Email)]; exists {
				return ErrEmailAlreadyExists
			}
		}
		if oldUser.Email != "" {
			delete(m.usersByEmail, makeKey(oldUser.TenantID, oldUser.Email))
		}
	}

	// A handle change is held to the same tenant-unique rule as a create.
	// Checked only on a CHANGE, so re-writing a record does not collide with
	// itself — and passing the record's own id to the check makes that true
	// even when a caller writes the same handle back deliberately.
	if oldUser.Username != user.Username {
		if err := m.usernameTaken(user.TenantID, user.Username, user.ID); err != nil {
			return err
		}
	}

	// Create a copy
	u := *user
	u.Updated = time.Now()
	m.users[key] = &u
	if user.Email != "" {
		m.usersByEmail[makeKey(user.TenantID, user.Email)] = &u
	}

	// Maintain the PIV thumbprint index. Drop every old binding key then
	// (re)write every current one so additions, removals, and pointer refreshes
	// are all handled uniformly when a user holds multiple PIV bindings.
	for _, tp := range oldUser.PIVThumbprints() {
		delete(m.usersByPIVThumbprint, tp)
	}
	for _, tp := range u.PIVThumbprints() {
		m.usersByPIVThumbprint[tp] = &u
	}

	return nil
}

// MarkUserDeleting claims the record for deletion. Marking an already-marked
// record is a no-op, which is what makes a retried deletion resumable.
func (m *MemoryStorage) MarkUserDeleting(ctx context.Context, tenantID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	user, exists := m.users[makeKey(tenantID, userID)]
	if !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.user.mark_deleting"}
	}
	user.Lifecycle = LifecycleDeleting
	return nil
}

func (m *MemoryStorage) DeleteUser(ctx context.Context, tenantID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(tenantID, userID)
	user, exists := m.users[key]
	if !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.user.delete"}
	}

	delete(m.users, key)
	delete(m.usersByEmail, makeKey(user.TenantID, user.Email))
	for _, tp := range user.PIVThumbprints() {
		delete(m.usersByPIVThumbprint, tp)
	}

	return nil
}

func (m *MemoryStorage) ListUsers(ctx context.Context, tenantID string, opts ListOptions) ([]*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*User
	prefix := tenantID + ":"
	for key, user := range m.users {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			u := *user
			result = append(result, &u)
		}
	}

	// Map iteration order is random; sort by ID so offset pages are stable
	// across calls, as they are in the qrdb-backed store.
	slices.SortFunc(result, func(a, b *User) int { return strings.Compare(a.ID, b.ID) })

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*User{}, nil
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// RoleStorage implementation

// CreateRole stores a role, rejecting a duplicate ID or a duplicate NAME
// within the tenant.
//
// The name check matches the canonical store: qrdb's role store is name-unique
// per tenant and answers a collision with RoleNameExistsError, which is what
// callers key their idempotency off. A double that enforced only the ID would
// let a test create two roles named "admin" in one tenant — a state production
// cannot reach — and would silently skip the callers' name-collision handling.
func (m *MemoryStorage) CreateRole(ctx context.Context, role *Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(role.TenantID, role.ID)
	if _, exists := m.roles[key]; exists {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.role.create"}
	}
	for _, existing := range m.roles {
		if existing.TenantID == role.TenantID && existing.Name == role.Name {
			return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.role.create"}
		}
	}

	// Create a copy
	r := *role
	m.roles[key] = &r

	return nil
}

func (m *MemoryStorage) GetRole(ctx context.Context, tenantID, roleID string) (*Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := makeKey(tenantID, roleID)
	role, exists := m.roles[key]
	if !exists {
		// The SENTINEL, for the reason GetUser states: IsRoleNotFound reads the
		// typed inner error, which a bare not-found code does not carry.
		return nil, ErrRoleNotFound
	}

	// Return a copy
	r := *role
	return &r, nil
}

// GetRoleByName retrieves a role by its tenant-scoped name.
//
// The in-memory store SCANS its role map, for the reason GetUserByUsername
// gives for users: the volume this store is built for (tests and embedded
// callers without a KVStore) is small, so a scan is preferable to maintaining a
// second structure that can drift on update and delete. CreateRole already
// scans the same map to enforce name uniqueness, so the read costs what the
// write already costs.
func (m *MemoryStorage) GetRoleByName(ctx context.Context, tenantID, name string) (*Role, error) {
	if name == "" {
		// A role without a name is not addressable by name, so an empty query
		// must not match one. Same rule the KVStore-backed store's index
		// encodes by writing no entry for an empty name.
		return nil, ErrRoleNotFound
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, role := range m.roles {
		if role.TenantID == tenantID && role.Name == name {
			r := *role
			return &r, nil
		}
	}
	// The SENTINEL, matching GetRole: IsRoleNotFound reads the typed inner
	// error, which a bare not-found code does not carry.
	return nil, ErrRoleNotFound
}

func (m *MemoryStorage) UpdateRole(ctx context.Context, role *Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(role.TenantID, role.ID)
	if _, exists := m.roles[key]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.role.update"}
	}

	// Create a copy
	r := *role
	r.Updated = time.Now()
	m.roles[key] = &r

	return nil
}

func (m *MemoryStorage) DeleteRole(ctx context.Context, tenantID, roleID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(tenantID, roleID)
	if _, exists := m.roles[key]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.role.delete"}
	}

	delete(m.roles, key)
	return nil
}

func (m *MemoryStorage) ListRoles(ctx context.Context, tenantID string, opts ListOptions) ([]*Role, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Role
	prefix := tenantID + ":"
	for key, role := range m.roles {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			r := *role
			result = append(result, &r)
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*Role{}, nil
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// PolicyStorage implementation

func (m *MemoryStorage) CreatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(tenantID, policy.Metadata.Name)
	if _, exists := m.policies[key]; exists {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.policy.create"}
	}

	// Create a copy
	p := *policy
	p.Metadata.Created = time.Now()
	p.Metadata.Updated = time.Now()
	m.policies[key] = &p

	return nil
}

func (m *MemoryStorage) GetPolicy(ctx context.Context, tenantID, name string) (*Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := makeKey(tenantID, name)
	policy, exists := m.policies[key]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.policy.get"}
	}

	// Return a copy
	p := *policy
	return &p, nil
}

func (m *MemoryStorage) UpdatePolicy(ctx context.Context, tenantID string, policy *Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(tenantID, policy.Metadata.Name)
	if _, exists := m.policies[key]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.policy.update"}
	}

	// Create a copy
	p := *policy
	p.Metadata.Updated = time.Now()
	m.policies[key] = &p

	return nil
}

func (m *MemoryStorage) DeletePolicy(ctx context.Context, tenantID, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(tenantID, name)
	if _, exists := m.policies[key]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.policy.delete"}
	}

	delete(m.policies, key)
	return nil
}

func (m *MemoryStorage) ListPolicies(ctx context.Context, tenantID string, opts ListOptions) ([]*Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Policy
	prefix := tenantID + ":"
	for key, policy := range m.policies {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			p := *policy
			result = append(result, &p)
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*Policy{}, nil
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// AppStorage implementation

func (m *MemoryStorage) CreateApp(ctx context.Context, app *Application) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.apps[app.ClientID]; exists {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.app.create"}
	}

	// Create a copy
	a := *app
	m.apps[app.ClientID] = &a

	return nil
}

func (m *MemoryStorage) GetApp(ctx context.Context, clientID string) (*Application, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	app, exists := m.apps[clientID]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.get"}
	}

	// Return a copy
	a := *app
	return &a, nil
}

func (m *MemoryStorage) GetAppByTenant(ctx context.Context, tenantID, clientID string) (*Application, error) {
	app, err := m.GetApp(ctx, clientID)
	if err != nil {
		return nil, err
	}

	if app.TenantID != tenantID {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.get"}
	}

	return app, nil
}

func (m *MemoryStorage) UpdateApp(ctx context.Context, app *Application) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.apps[app.ClientID]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.update"}
	}

	// Create a copy
	a := *app
	a.Updated = time.Now()
	m.apps[app.ClientID] = &a

	return nil
}

func (m *MemoryStorage) DeleteApp(ctx context.Context, clientID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.apps[clientID]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.delete"}
	}

	delete(m.apps, clientID)
	return nil
}

func (m *MemoryStorage) ListApps(ctx context.Context, tenantID string, opts ListOptions) ([]*Application, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Application
	for _, app := range m.apps {
		if app.TenantID == tenantID {
			a := *app
			result = append(result, &a)
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*Application{}, nil
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

func (m *MemoryStorage) ValidateClientSecret(ctx context.Context, clientID, clientSecret string) (bool, error) {
	// Same reason as BackendStorage: the derivations below are not cancellable,
	// so a departed caller must not buy one. There is no equivalent
	// classification to make here -- this store's miss is a map lookup, so a
	// miss really is not-found and nothing else can fail.
	if err := ctx.Err(); err != nil {
		return false, err
	}

	m.mu.RLock()
	app, exists := m.apps[clientID]
	stored := ""
	if exists {
		stored = app.ClientSecret
	}
	m.mu.RUnlock()

	// An empty credential is a malformed request rather than a guess, and both
	// this check and the one it mirrors in BackendStorage are independent of
	// stored state, so neither can probe for a client ID.
	if clientSecret == "" {
		return false, ErrSecretEmpty
	}

	if !exists {
		// Pay a derivation before answering: an unknown client ID that returned
		// immediately would be distinguishable from a known one by latency, which
		// is the same enumeration channel verifyClientSecret closes for records
		// with no usable digest.
		_ = verifySecret(timingEqualizerDigest, clientSecret)
		return false, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.validate"}
	}

	// Only the current PBKDF2 form is verified. A pre-#37 bcrypt record is
	// refused with ErrSecretRotationRequired rather than migrated: since the
	// legacy path no longer authenticates anything, there is no successful
	// login left to rehash on, and the record is cleared by rotation instead
	// (owner-ruled 2026-08-25).
	ok, err := verifyClientSecret(stored, clientSecret)
	if err != nil {
		if errors.Is(err, ErrSecretRotationRequired) {
			warnRotationRequired(m.logger, clientID)
		}
		return false, err
	}
	return ok, nil
}

// TokenStorage implementation

func (m *MemoryStorage) StoreAuthCode(ctx context.Context, code *AuthCodeData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a copy
	c := *code
	m.authCodes[code.Code] = &c

	return nil
}

func (m *MemoryStorage) GetAuthCode(ctx context.Context, code string) (*AuthCodeData, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	authCode, exists := m.authCodes[code]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.authcode.get"}
	}

	// Return a copy
	c := *authCode
	return &c, nil
}

func (m *MemoryStorage) DeleteAuthCode(ctx context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.authCodes, code)
	return nil
}

func (m *MemoryStorage) StoreToken(ctx context.Context, token *Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a copy
	t := *token
	m.tokens[token.JTI] = &t

	// Opaque (refresh) tokens are presented by value, so index them under the
	// SHA-256 of that value — the only handle a refresh request carries.
	if token.TokenType == TokenTypeRefresh && token.ValueHash != "" {
		m.tokenValues[token.ValueHash] = &t
	}

	return nil
}

func (m *MemoryStorage) GetToken(ctx context.Context, jti string) (*Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	token, exists := m.tokens[jti]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.token.get"}
	}

	// Return a copy
	t := *token
	return &t, nil
}

func (m *MemoryStorage) GetTokenByValue(ctx context.Context, tokenValue string) (*Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	token, exists := m.tokenValues[HashTokenValue(tokenValue)]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.token.get"}
	}

	// Return a copy
	t := *token
	return &t, nil
}

func (m *MemoryStorage) RevokeToken(ctx context.Context, jti string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.revokedTokens[jti] = true
	// Mark the record itself too, so a caller that re-reads the token (rather
	// than querying IsTokenRevoked) observes the same state the persistent
	// backend reports.
	if token, ok := m.tokens[jti]; ok {
		token.Revoked = true
	}
	return nil
}

func (m *MemoryStorage) IsTokenRevoked(ctx context.Context, jti string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.revokedTokens[jti], nil
}

func (m *MemoryStorage) DeleteExpiredTokens(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for jti, token := range m.tokens {
		if now.After(token.Expires) {
			delete(m.tokens, jti)
			if token.TokenType == TokenTypeRefresh && token.ValueHash != "" {
				delete(m.tokenValues, token.ValueHash)
			}
			delete(m.revokedTokens, jti)
		}
	}

	return nil
}

func (m *MemoryStorage) ListTokensByUser(ctx context.Context, userID string) ([]*Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Token
	for _, token := range m.tokens {
		if token.Subject == userID {
			t := *token
			result = append(result, &t)
		}
	}

	return result, nil
}

func (m *MemoryStorage) RevokeUserTokens(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for jti, token := range m.tokens {
		if token.Subject == userID {
			m.revokedTokens[jti] = true
		}
	}

	return nil
}
