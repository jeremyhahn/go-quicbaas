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

package catalog

import (
	"context"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ObjectAction represents an action that can be performed on an object.
type ObjectAction string

const (
	// ActionRead allows reading object data and metadata.
	ActionRead ObjectAction = "read"

	// ActionWrite allows creating and updating object data.
	ActionWrite ObjectAction = "write"

	// ActionDelete allows deleting objects.
	ActionDelete ObjectAction = "delete"

	// ActionAdmin allows full administrative access including ACL management.
	ActionAdmin ObjectAction = "admin"

	// ActionReplicate allows replication operations.
	ActionReplicate ObjectAction = "replicate"

	// ActionArchive allows archive operations.
	ActionArchive ObjectAction = "archive"

	// ActionList allows listing objects (namespace-level).
	ActionList ObjectAction = "list"

	// ActionVerify allows verification operations.
	ActionVerify ObjectAction = "verify"
)

// String returns the string representation of the object action.
func (a ObjectAction) String() string {
	return string(a)
}

// IsValid checks if the object action is valid.
func (a ObjectAction) IsValid() bool {
	switch a {
	case ActionRead, ActionWrite, ActionDelete, ActionAdmin,
		ActionReplicate, ActionArchive, ActionList, ActionVerify:
		return true
	default:
		return false
	}
}

// AllActions returns all valid object actions.
func AllActions() []ObjectAction {
	return []ObjectAction{
		ActionRead,
		ActionWrite,
		ActionDelete,
		ActionAdmin,
		ActionReplicate,
		ActionArchive,
		ActionList,
		ActionVerify,
	}
}

// ACLManager manages object access control lists.
// It provides centralized access control for object storage operations.
type ACLManager interface {
	// CheckAccess verifies if a principal can perform an action on an object.
	// Returns true if access is allowed, false otherwise.
	CheckAccess(ctx context.Context, principal string, objectKey string, action ObjectAction) (bool, error)

	// SetACL sets the ACL for an object.
	SetACL(ctx context.Context, objectKey string, acl *ObjectACL) error

	// GetACL gets the ACL for an object.
	GetACL(ctx context.Context, objectKey string) (*ObjectACL, error)

	// GrantAccess grants access to a principal for specified permissions.
	GrantAccess(ctx context.Context, objectKey string, principal string, permissions []ObjectAction) error

	// RevokeAccess revokes all access from a principal.
	RevokeAccess(ctx context.Context, objectKey string, principal string) error

	// ListAccessible returns objects accessible by a principal for a given action.
	ListAccessible(ctx context.Context, principal string, action ObjectAction, opts *ListOptions) (*ListResult, error)

	// GetNamespaceACL returns the default ACL for a namespace.
	GetNamespaceACL(ctx context.Context, namespace string) (*NamespaceACL, error)

	// SetNamespaceACL sets the default ACL for a namespace.
	SetNamespaceACL(ctx context.Context, namespace string, acl *NamespaceACL) error

	// TransferOwnership transfers object ownership to a new principal.
	TransferOwnership(ctx context.Context, objectKey string, newOwner string) error

	// BatchCheckAccess checks access for multiple objects efficiently.
	BatchCheckAccess(ctx context.Context, principal string, objectKeys []string, action ObjectAction) (map[string]bool, error)
}

// NamespaceACL defines default access control for a namespace.
type NamespaceACL struct {
	// Namespace is the namespace this ACL applies to.
	Namespace string `msgpack:"namespace"`

	// Owner is the namespace owner (typically the creator).
	Owner string `msgpack:"owner"`

	// DefaultReaders are principals with default read access to new objects.
	DefaultReaders []string `msgpack:"default_readers,omitempty"`

	// DefaultWriters are principals with default write access to new objects.
	DefaultWriters []string `msgpack:"default_writers,omitempty"`

	// DefaultAdmins are principals with default admin access to new objects.
	DefaultAdmins []string `msgpack:"default_admins,omitempty"`

	// PublicRead allows unauthenticated reads for all objects in namespace.
	PublicRead bool `msgpack:"public_read"`

	// InheritFromTenant indicates whether to inherit from tenant ACL.
	InheritFromTenant bool `msgpack:"inherit_from_tenant"`
}

// Clone creates a deep copy of the namespace ACL.
func (n *NamespaceACL) Clone() *NamespaceACL {
	if n == nil {
		return nil
	}

	clone := &NamespaceACL{
		Namespace:         n.Namespace,
		Owner:             n.Owner,
		PublicRead:        n.PublicRead,
		InheritFromTenant: n.InheritFromTenant,
	}

	if n.DefaultReaders != nil {
		clone.DefaultReaders = make([]string, len(n.DefaultReaders))
		copy(clone.DefaultReaders, n.DefaultReaders)
	}

	if n.DefaultWriters != nil {
		clone.DefaultWriters = make([]string, len(n.DefaultWriters))
		copy(clone.DefaultWriters, n.DefaultWriters)
	}

	if n.DefaultAdmins != nil {
		clone.DefaultAdmins = make([]string, len(n.DefaultAdmins))
		copy(clone.DefaultAdmins, n.DefaultAdmins)
	}

	return clone
}

// ACLManagerConfig configures the ACL manager.
type ACLManagerConfig struct {
	// Catalog is the catalog manager for retrieving object entries.
	Catalog CatalogManager

	// Logger is the logger instance.
	Logger interfaces.Logger

	// NamespaceACLs contains namespace-level ACLs (keyed by namespace).
	NamespaceACLs map[string]*NamespaceACL
}

// Validate validates the ACL manager configuration.
func (c *ACLManagerConfig) Validate() error {
	if c == nil {
		return ErrConfigNil
	}
	if c.Catalog == nil {
		return ErrCatalogNil
	}
	return nil
}

// aclManager implements ACLManager.
type aclManager struct {
	catalog       CatalogManager
	logger        interfaces.Logger
	namespaceACLs map[string]*NamespaceACL
	mu            sync.RWMutex
}

// NewACLManager creates a new ACL manager.
func NewACLManager(config *ACLManagerConfig) (ACLManager, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	nsACLs := make(map[string]*NamespaceACL)
	if config.NamespaceACLs != nil {
		for k, v := range config.NamespaceACLs {
			nsACLs[k] = v.Clone()
		}
	}

	return &aclManager{
		catalog:       config.Catalog,
		logger:        config.Logger,
		namespaceACLs: nsACLs,
	}, nil
}

// CheckAccess verifies if a principal can perform an action on an object.
func (m *aclManager) CheckAccess(ctx context.Context, principal string, objectKey string, action ObjectAction) (bool, error) {
	if objectKey == "" {
		return false, NewKeyEmptyError()
	}
	if !action.IsValid() {
		return false, NewACLInvalidActionError(string(action))
	}

	// Get the object entry
	entry, err := m.catalog.Get(ctx, objectKey)
	if err != nil {
		return false, err
	}

	return m.checkEntryAccess(ctx, principal, entry, action)
}

// checkEntryAccess checks access against an entry.
func (m *aclManager) checkEntryAccess(ctx context.Context, principal string, entry *CatalogEntry, action ObjectAction) (bool, error) {
	// Owner always has full access
	if entry.Owner == principal {
		return true, nil
	}

	acl := entry.ACL
	if acl == nil {
		// No ACL means only owner has access
		return false, nil
	}

	// ACL owner also has full access
	if acl.Owner == principal {
		return true, nil
	}

	// Check public read
	if action == ActionRead && acl.PublicRead {
		return true, nil
	}

	// Check admins (full access)
	if contains(acl.Admins, principal) {
		return true, nil
	}

	// Check specific permission based on action
	switch action {
	case ActionRead, ActionList:
		return contains(acl.Readers, principal) || contains(acl.Writers, principal), nil

	case ActionWrite:
		return contains(acl.Writers, principal), nil

	case ActionDelete, ActionAdmin:
		// Only owner and admins can delete or administer
		return false, nil

	case ActionReplicate, ActionArchive, ActionVerify:
		// These are typically operator actions
		// Writers can replicate/archive, but verify requires admin
		if action == ActionVerify {
			return false, nil
		}
		return contains(acl.Writers, principal), nil

	default:
		return false, nil
	}
}

// SetACL sets the ACL for an object.
func (m *aclManager) SetACL(ctx context.Context, objectKey string, acl *ObjectACL) error {
	if objectKey == "" {
		return NewKeyEmptyError()
	}
	if acl == nil {
		return ErrACLNil
	}

	return m.catalog.Update(ctx, objectKey, func(entry *CatalogEntry) error {
		entry.ACL = acl.Clone()
		return nil
	})
}

// GetACL gets the ACL for an object.
func (m *aclManager) GetACL(ctx context.Context, objectKey string) (*ObjectACL, error) {
	if objectKey == "" {
		return nil, NewKeyEmptyError()
	}

	entry, err := m.catalog.Get(ctx, objectKey)
	if err != nil {
		return nil, err
	}

	if entry.ACL == nil {
		// Return default ACL based on entry owner
		return &ObjectACL{
			Owner: entry.Owner,
		}, nil
	}

	return entry.ACL.Clone(), nil
}

// GrantAccess grants access to a principal for specified permissions.
func (m *aclManager) GrantAccess(ctx context.Context, objectKey string, principal string, permissions []ObjectAction) error {
	if objectKey == "" {
		return NewKeyEmptyError()
	}
	if principal == "" {
		return NewACLPrincipalEmptyError()
	}
	if len(permissions) == 0 {
		return NewACLPermissionsEmptyError()
	}

	// Validate all permissions
	for _, perm := range permissions {
		if !perm.IsValid() {
			return NewACLInvalidActionError(string(perm))
		}
	}

	return m.catalog.Update(ctx, objectKey, func(entry *CatalogEntry) error {
		// Initialize ACL if nil
		if entry.ACL == nil {
			entry.ACL = &ObjectACL{
				Owner: entry.Owner,
			}
		}

		// Add permissions based on granted actions
		for _, perm := range permissions {
			switch perm {
			case ActionRead, ActionList:
				entry.ACL.AddReader(principal)
			case ActionWrite, ActionReplicate, ActionArchive:
				entry.ACL.AddWriter(principal)
			case ActionDelete, ActionAdmin, ActionVerify:
				entry.ACL.AddAdmin(principal)
			}
		}

		return nil
	})
}

// RevokeAccess revokes all access from a principal.
func (m *aclManager) RevokeAccess(ctx context.Context, objectKey string, principal string) error {
	if objectKey == "" {
		return NewKeyEmptyError()
	}
	if principal == "" {
		return NewACLPrincipalEmptyError()
	}

	return m.catalog.Update(ctx, objectKey, func(entry *CatalogEntry) error {
		if entry.ACL == nil {
			return nil // Nothing to revoke
		}

		// Cannot revoke owner's access
		if entry.ACL.Owner == principal {
			return NewACLCannotRevokeOwnerError()
		}

		entry.ACL.RemoveReader(principal)
		entry.ACL.RemoveWriter(principal)
		entry.ACL.RemoveAdmin(principal)

		return nil
	})
}

// ListAccessible returns objects accessible by a principal for a given action.
func (m *aclManager) ListAccessible(ctx context.Context, principal string, action ObjectAction, opts *ListOptions) (*ListResult, error) {
	if principal == "" {
		return nil, NewACLPrincipalEmptyError()
	}
	if !action.IsValid() {
		return nil, NewACLInvalidActionError(string(action))
	}

	limit := 100
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	result := &ListResult{
		Keys:  make([]string, 0, limit),
		Total: -1, // Unknown total
	}

	// Search for all entries and filter by access
	searchResult, err := m.catalog.Search(ctx, &SearchQuery{
		Limit: 0, // No limit on search, we'll filter
	})
	if err != nil {
		return nil, err
	}

	count := 0
	for _, entry := range searchResult.Entries {
		allowed, err := m.checkEntryAccess(ctx, principal, entry, action)
		if err != nil {
			continue
		}

		if allowed {
			if count >= limit {
				result.HasMore = true
				break
			}
			result.Keys = append(result.Keys, entry.Key)
			count++
		}
	}

	return result, nil
}

// GetNamespaceACL returns the default ACL for a namespace.
func (m *aclManager) GetNamespaceACL(ctx context.Context, namespace string) (*NamespaceACL, error) {
	if namespace == "" {
		return nil, NewNamespaceEmptyError()
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if acl, ok := m.namespaceACLs[namespace]; ok {
		return acl.Clone(), nil
	}

	// Return empty namespace ACL if not configured
	return &NamespaceACL{
		Namespace: namespace,
	}, nil
}

// SetNamespaceACL sets the default ACL for a namespace.
func (m *aclManager) SetNamespaceACL(ctx context.Context, namespace string, acl *NamespaceACL) error {
	if namespace == "" {
		return NewNamespaceEmptyError()
	}
	if acl == nil {
		return ErrACLNil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.namespaceACLs[namespace] = acl.Clone()
	return nil
}

// TransferOwnership transfers object ownership to a new principal.
func (m *aclManager) TransferOwnership(ctx context.Context, objectKey string, newOwner string) error {
	if objectKey == "" {
		return NewKeyEmptyError()
	}
	if newOwner == "" {
		return NewACLPrincipalEmptyError()
	}

	return m.catalog.Update(ctx, objectKey, func(entry *CatalogEntry) error {
		oldOwner := entry.Owner
		entry.Owner = newOwner

		// Update ACL if present
		if entry.ACL != nil {
			// Transfer ACL ownership
			entry.ACL.Owner = newOwner

			// Optionally add old owner as admin
			if oldOwner != "" && oldOwner != newOwner {
				entry.ACL.AddAdmin(oldOwner)
			}
		}

		return nil
	})
}

// BatchCheckAccess checks access for multiple objects efficiently.
func (m *aclManager) BatchCheckAccess(ctx context.Context, principal string, objectKeys []string, action ObjectAction) (map[string]bool, error) {
	if principal == "" {
		return nil, NewACLPrincipalEmptyError()
	}
	if len(objectKeys) == 0 {
		return make(map[string]bool), nil
	}
	if !action.IsValid() {
		return nil, NewACLInvalidActionError(string(action))
	}

	results := make(map[string]bool, len(objectKeys))

	for _, key := range objectKeys {
		allowed, err := m.CheckAccess(ctx, principal, key, action)
		if err != nil {
			// On error, deny access
			if m.logger != nil {
				m.logger.Warnf("ACL check failed for key %s: %v", key, err)
			}
			results[key] = false
			continue
		}
		results[key] = allowed
	}

	return results, nil
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Validate validates the ObjectACL.
func (a *ObjectACL) Validate() error {
	if a == nil {
		return nil
	}

	// Owner should be set if ACL exists
	if a.Owner == "" && !a.PublicRead && !a.Inherit {
		return NewACLOwnerRequiredError()
	}

	return nil
}

// ApplyNamespaceDefaults applies namespace default permissions to an ACL.
func (a *ObjectACL) ApplyNamespaceDefaults(ns *NamespaceACL) {
	if a == nil || ns == nil {
		return
	}

	// Apply default readers
	for _, reader := range ns.DefaultReaders {
		a.AddReader(reader)
	}

	// Apply default writers
	for _, writer := range ns.DefaultWriters {
		a.AddWriter(writer)
	}

	// Apply default admins
	for _, admin := range ns.DefaultAdmins {
		a.AddAdmin(admin)
	}

	// Apply public read if namespace allows
	if ns.PublicRead {
		a.PublicRead = true
	}
}

// Merge merges another ACL into this one.
// The other ACL's principals are added to this ACL's lists.
func (a *ObjectACL) Merge(other *ObjectACL) {
	if a == nil || other == nil {
		return
	}

	for _, reader := range other.Readers {
		a.AddReader(reader)
	}

	for _, writer := range other.Writers {
		a.AddWriter(writer)
	}

	for _, admin := range other.Admins {
		a.AddAdmin(admin)
	}

	if other.PublicRead {
		a.PublicRead = true
	}
}

// IsEmpty returns true if the ACL has no principals assigned.
func (a *ObjectACL) IsEmpty() bool {
	if a == nil {
		return true
	}
	return len(a.Readers) == 0 && len(a.Writers) == 0 && len(a.Admins) == 0 && !a.PublicRead
}

// GetAllPrincipals returns all principals with any access.
func (a *ObjectACL) GetAllPrincipals() []string {
	if a == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var principals []string

	if a.Owner != "" {
		seen[a.Owner] = struct{}{}
		principals = append(principals, a.Owner)
	}

	for _, p := range a.Readers {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			principals = append(principals, p)
		}
	}

	for _, p := range a.Writers {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			principals = append(principals, p)
		}
	}

	for _, p := range a.Admins {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			principals = append(principals, p)
		}
	}

	return principals
}

// GetEffectivePermissions returns all actions a principal can perform.
func (a *ObjectACL) GetEffectivePermissions(principal string) []ObjectAction {
	if a == nil {
		return nil
	}

	var permissions []ObjectAction

	// Owner has all permissions
	if a.Owner == principal {
		return AllActions()
	}

	// Admins have all permissions
	if contains(a.Admins, principal) {
		return AllActions()
	}

	// Check public read
	if a.PublicRead {
		permissions = append(permissions, ActionRead)
	}

	// Check readers
	if contains(a.Readers, principal) {
		permissions = append(permissions, ActionRead, ActionList)
	}

	// Check writers (also implies read)
	if contains(a.Writers, principal) {
		// Dedupe read if already added
		hasRead := false
		for _, p := range permissions {
			if p == ActionRead {
				hasRead = true
				break
			}
		}
		if !hasRead {
			permissions = append(permissions, ActionRead)
		}
		permissions = append(permissions, ActionWrite, ActionReplicate, ActionArchive)
	}

	return permissions
}
