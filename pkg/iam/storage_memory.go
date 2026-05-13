//go:build iam

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
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// MemoryStorage implements all IAM storage interfaces using in-memory maps.
// This implementation is thread-safe and suitable for testing.
type MemoryStorage struct {
	mu sync.RWMutex

	// Tenant storage
	tenants        map[string]*Tenant
	tenantSettings map[string]*TenantSettings
	tenantQuotas   map[string]*TenantQuota

	// User storage
	users              map[string]*User // key: tenantID:userID
	usersByEmail       map[string]*User // key: email
	usersByPIVThumbprint map[string]*User // key: piv_thumbprint

	// Group storage
	groups map[string]*Group // key: tenantID:groupID

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
func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		tenants:        make(map[string]*Tenant),
		tenantSettings: make(map[string]*TenantSettings),
		tenantQuotas:   make(map[string]*TenantQuota),
		users:                make(map[string]*User),
		usersByEmail:         make(map[string]*User),
		usersByPIVThumbprint: make(map[string]*User),
		groups:         make(map[string]*Group),
		roles:          make(map[string]*Role),
		policies:       make(map[string]*Policy),
		apps:           make(map[string]*Application),
		authCodes:      make(map[string]*AuthCodeData),
		tokens:         make(map[string]*Token),
		tokenValues:    make(map[string]*Token),
		revokedTokens:  make(map[string]bool),
		sessions:       make(map[string]*Session),
		credentials:    make(map[string]*Credential),
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

	// Always initialize quota (0 means unlimited for each resource type)
	quota := &TenantQuota{
		MaxUsers:    int64(tenant.Quotas.MaxUsers),
		MaxGroups:   int64(tenant.Quotas.MaxGroups),
		MaxRoles:    int64(tenant.Quotas.MaxRoles),
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
				delete(m.usersByEmail, user.Email)
			}
		}
	}

	// Cascade delete groups
	for key := range m.groups {
		if len(key) > len(prefix) && key[:len(prefix)] == prefix {
			delete(m.groups, key)
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

	switch resource {
	case "users":
		if quota.MaxUsers > 0 && quota.CurrentUsers+delta > quota.MaxUsers {
			return NewQuotaExceededError(tenantID, resource, quota.MaxUsers, quota.CurrentUsers)
		}
		quota.CurrentUsers += delta
	case "groups":
		if quota.MaxGroups > 0 && quota.CurrentGroups+delta > quota.MaxGroups {
			return NewQuotaExceededError(tenantID, resource, quota.MaxGroups, quota.CurrentGroups)
		}
		quota.CurrentGroups += delta
	case "roles":
		if quota.MaxRoles > 0 && quota.CurrentRoles+delta > quota.MaxRoles {
			return NewQuotaExceededError(tenantID, resource, quota.MaxRoles, quota.CurrentRoles)
		}
		quota.CurrentRoles += delta
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

	switch resource {
	case "users":
		quota.CurrentUsers -= delta
		if quota.CurrentUsers < 0 {
			quota.CurrentUsers = 0
		}
	case "groups":
		quota.CurrentGroups -= delta
		if quota.CurrentGroups < 0 {
			quota.CurrentGroups = 0
		}
	case "roles":
		quota.CurrentRoles -= delta
		if quota.CurrentRoles < 0 {
			quota.CurrentRoles = 0
		}
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
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(user.TenantID, user.ID)
	if _, exists := m.users[key]; exists {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.user.create"}
	}

	if _, exists := m.usersByEmail[user.Email]; exists {
		return ErrEmailAlreadyExists
	}

	// Create a copy
	u := *user
	m.users[key] = &u
	m.usersByEmail[user.Email] = &u
	if u.PIVCertThumbprint != "" {
		m.usersByPIVThumbprint[u.PIVCertThumbprint] = &u
	}

	return nil
}

func (m *MemoryStorage) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := makeKey(tenantID, userID)
	user, exists := m.users[key]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.user.get"}
	}

	// Return a copy
	u := *user
	return &u, nil
}

func (m *MemoryStorage) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	user, exists := m.usersByEmail[email]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.user.get"}
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
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.user.getbypivthumbprint"}
	}

	u := *user
	return &u, nil
}

func (m *MemoryStorage) UpdateUser(ctx context.Context, user *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(user.TenantID, user.ID)
	oldUser, exists := m.users[key]
	if !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.user.update"}
	}

	// Check if email changed and new email is already taken
	if oldUser.Email != user.Email {
		if _, exists := m.usersByEmail[user.Email]; exists {
			return ErrEmailAlreadyExists
		}
		delete(m.usersByEmail, oldUser.Email)
	}

	// Create a copy
	u := *user
	u.Updated = time.Now()
	m.users[key] = &u
	m.usersByEmail[user.Email] = &u

	// Maintain PIV thumbprint index when the cert binding changes.
	if oldUser.PIVCertThumbprint != u.PIVCertThumbprint {
		if oldUser.PIVCertThumbprint != "" {
			delete(m.usersByPIVThumbprint, oldUser.PIVCertThumbprint)
		}
		if u.PIVCertThumbprint != "" {
			m.usersByPIVThumbprint[u.PIVCertThumbprint] = &u
		}
	} else if u.PIVCertThumbprint != "" {
		// Refresh the pointer so the index returns the latest user state.
		m.usersByPIVThumbprint[u.PIVCertThumbprint] = &u
	}

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
	delete(m.usersByEmail, user.Email)
	if user.PIVCertThumbprint != "" {
		delete(m.usersByPIVThumbprint, user.PIVCertThumbprint)
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

// GroupStorage implementation

func (m *MemoryStorage) CreateGroup(ctx context.Context, group *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(group.TenantID, group.ID)
	if _, exists := m.groups[key]; exists {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.group.create"}
	}

	// Create a copy
	g := *group
	m.groups[key] = &g

	return nil
}

func (m *MemoryStorage) GetGroup(ctx context.Context, tenantID, groupID string) (*Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := makeKey(tenantID, groupID)
	group, exists := m.groups[key]
	if !exists {
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.group.get"}
	}

	// Return a copy
	g := *group
	return &g, nil
}

func (m *MemoryStorage) UpdateGroup(ctx context.Context, group *Group) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(group.TenantID, group.ID)
	if _, exists := m.groups[key]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.group.update"}
	}

	// Create a copy
	g := *group
	g.Updated = time.Now()
	m.groups[key] = &g

	return nil
}

func (m *MemoryStorage) DeleteGroup(ctx context.Context, tenantID, groupID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(tenantID, groupID)
	if _, exists := m.groups[key]; !exists {
		return &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.group.delete"}
	}

	delete(m.groups, key)
	return nil
}

func (m *MemoryStorage) ListGroups(ctx context.Context, tenantID string, opts ListOptions) ([]*Group, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Group
	prefix := tenantID + ":"
	for key, group := range m.groups {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			g := *group
			result = append(result, &g)
		}
	}

	// Apply pagination
	start := opts.Offset
	if start >= len(result) {
		return []*Group{}, nil
	}
	end := start + opts.Limit
	if opts.Limit == 0 || end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

// RoleStorage implementation

func (m *MemoryStorage) CreateRole(ctx context.Context, role *Role) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := makeKey(role.TenantID, role.ID)
	if _, exists := m.roles[key]; exists {
		return &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "iam.role.create"}
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
		return nil, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.role.get"}
	}

	// Return a copy
	r := *role
	return &r, nil
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
	m.mu.RLock()
	app, exists := m.apps[clientID]
	m.mu.RUnlock()

	if !exists {
		return false, &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "iam.app.validate"}
	}

	// Compare plaintext secret against bcrypt hash
	err := bcrypt.CompareHashAndPassword([]byte(app.ClientSecret), []byte(clientSecret))
	return err == nil, nil
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

	// For refresh tokens, also store by value
	if token.TokenType == TokenTypeRefresh {
		m.tokenValues[token.JTI] = &t // Using JTI as value key for simplicity
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

	token, exists := m.tokenValues[tokenValue]
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
			if token.TokenType == TokenTypeRefresh {
				delete(m.tokenValues, jti)
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
