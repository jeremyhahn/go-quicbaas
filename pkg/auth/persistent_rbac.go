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

// Package auth provides persistent role-based access control (RBAC)
package auth

import (
	"encoding/json"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// PersistentRoleManager provides database-backed role management with in-memory caching.
type PersistentRoleManager struct {
	storage    interfaces.Storage
	logger     *logger.Logger
	keyBuilder *KeyBuilder
	roles      map[string]*Role // roleName -> Role (cached)
	mu         sync.RWMutex
}

// NewPersistentRoleManager creates a new persistent role manager.
func NewPersistentRoleManager(storage interfaces.Storage, logger *logger.Logger) (*PersistentRoleManager, error) {
	if storage == nil {
		return nil, errors.InvalidInput("auth.NewPersistentRoleManager", "storage cannot be nil")
	}

	if logger == nil {
		return nil, errors.InvalidInput("auth.NewPersistentRoleManager", "logger cannot be nil")
	}

	prm := &PersistentRoleManager{
		storage:    storage,
		logger:     logger,
		keyBuilder: NewKeyBuilder(),
		roles:      make(map[string]*Role),
	}

	// Load all roles from storage
	if err := prm.loadAllRoles(); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "auth.NewPersistentRoleManager", "failed to load roles")
	}

	// Initialize built-in roles if they don't exist
	if err := prm.initializeBuiltInRoles(); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "auth.NewPersistentRoleManager", "failed to initialize built-in roles")
	}

	logger.Infof("Persistent role manager initialized with %d roles", len(prm.roles))
	return prm, nil
}

// initializeBuiltInRoles creates the default roles if they don't exist.
func (prm *PersistentRoleManager) initializeBuiltInRoles() error {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	builtInRoles := []*Role{
		{
			Name:        RoleNameAdmin,
			Description: "Full administrative access to all resources",
			Permissions: []*Permission{
				{
					Resource: ResourceAll,
					Actions:  []string{ActionAll},
				},
			},
		},
		{
			Name:        RoleNameReadWrite,
			Description: "Read and write access to database operations",
			Permissions: []*Permission{
				{
					Resource: ResourceKeys,
					Actions:  []string{ActionRead, ActionWrite, ActionDelete},
				},
				{
					Resource: ResourceIndexes,
					Actions:  []string{ActionRead, ActionWrite},
				},
				{
					Resource: ResourceTransactions,
					Actions:  []string{ActionRead, ActionWrite},
				},
				{
					Resource: ResourceWatch,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceMetrics,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceHealth,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceTimeseries,
					Actions:  []string{ActionRead, ActionWrite, ActionDelete},
				},
				{
					Resource: ResourceVector,
					Actions:  []string{ActionRead, ActionWrite, ActionDelete},
				},
			},
		},
		{
			Name:        RoleNameReadOnly,
			Description: "Read-only access to database operations",
			Permissions: []*Permission{
				{
					Resource: ResourceKeys,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceIndexes,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceMetrics,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceHealth,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceTimeseries,
					Actions:  []string{ActionRead},
				},
				{
					Resource: ResourceVector,
					Actions:  []string{ActionRead},
				},
			},
		},
	}

	for _, role := range builtInRoles {
		// Only create if it doesn't exist
		if _, exists := prm.roles[role.Name]; !exists {
			if err := prm.persistRole(role); err != nil {
				return errors.Wrapf(err, errors.ErrInternal, "auth.initializeBuiltInRoles", "failed to persist built-in role %s", role.Name)
			}
			prm.roles[role.Name] = role
			prm.logger.Infof("Built-in role created: %s", role.Name)
		}
	}

	return nil
}

// AddRole adds a custom role to persistent storage.
func (prm *PersistentRoleManager) AddRole(role *Role) error {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	// Check if role already exists
	if _, exists := prm.roles[role.Name]; exists {
		return errors.AlreadyExists("auth.PersistentRoleManager.AddRole", "role: "+role.Name)
	}

	// Persist role
	if err := prm.persistRole(role); err != nil {
		return err
	}

	// Update cache
	prm.roles[role.Name] = role

	prm.logger.Infof("Role added to persistent storage: %s", role.Name)
	return nil
}

// GetRole retrieves a role by name.
func (prm *PersistentRoleManager) GetRole(name string) *Role {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	return prm.roles[name]
}

// ListRoles returns all available roles.
func (prm *PersistentRoleManager) ListRoles() []*Role {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	roles := make([]*Role, 0, len(prm.roles))
	for _, role := range prm.roles {
		roles = append(roles, role)
	}

	return roles
}

// DeleteRole removes a custom role (cannot delete built-in roles).
func (prm *PersistentRoleManager) DeleteRole(name string) error {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	// Prevent deletion of built-in roles
	if name == RoleNameAdmin || name == RoleNameReadWrite || name == RoleNameReadOnly {
		return errors.New(errors.ErrNotAllowed, "auth.PersistentRoleManager.DeleteRole", "cannot delete built-in role: "+name)
	}

	// Check if role exists
	if _, exists := prm.roles[name]; !exists {
		return errors.NotFound("auth.PersistentRoleManager.DeleteRole", "role: "+name)
	}

	// Delete from storage
	key := prm.keyBuilder.RoleKey(name)
	if err := prm.storage.Delete(key); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "auth.PersistentRoleManager.DeleteRole", "failed to delete role")
	}

	// Update cache
	delete(prm.roles, name)

	prm.logger.Infof("Role deleted from persistent storage: %s", name)
	return nil
}

// UpdateRole updates an existing role.
func (prm *PersistentRoleManager) UpdateRole(role *Role) error {
	prm.mu.Lock()
	defer prm.mu.Unlock()

	// Prevent modification of built-in roles
	if role.Name == RoleNameAdmin || role.Name == RoleNameReadWrite || role.Name == RoleNameReadOnly {
		return errors.New(errors.ErrNotAllowed, "auth.PersistentRoleManager.UpdateRole", "cannot modify built-in role: "+role.Name)
	}

	// Check if role exists
	if _, exists := prm.roles[role.Name]; !exists {
		return errors.NotFound("auth.PersistentRoleManager.UpdateRole", "role: "+role.Name)
	}

	// Persist updated role
	if err := prm.persistRole(role); err != nil {
		return err
	}

	// Update cache
	prm.roles[role.Name] = role

	prm.logger.Infof("Role updated in persistent storage: %s", role.Name)
	return nil
}

// HasPermission checks if any of the user's roles have permission for the action.
func (prm *PersistentRoleManager) HasPermission(userRoles []string, resource, action string) bool {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	for _, roleName := range userRoles {
		role, exists := prm.roles[roleName]
		if !exists {
			continue
		}

		// Check each permission in the role
		for _, perm := range role.Permissions {
			// Check if permission applies to this resource
			if !prm.matchesResource(perm.Resource, resource) {
				continue
			}

			// Check if permission includes this action
			if prm.hasAction(perm.Actions, action) {
				return true
			}
		}
	}

	return false
}

// AllowsAnonymous checks if a resource/action allows anonymous access.
func (prm *PersistentRoleManager) AllowsAnonymous(resource, action string) bool {
	// Only allow anonymous access to health checks and metrics
	if resource == ResourceHealth && action == ActionRead {
		return true
	}

	if resource == ResourceMetrics && action == ActionRead {
		return true
	}

	return false
}

// ValidateRoles checks if all role names exist.
func (prm *PersistentRoleManager) ValidateRoles(roleNames []string) bool {
	prm.mu.RLock()
	defer prm.mu.RUnlock()

	for _, name := range roleNames {
		if _, exists := prm.roles[name]; !exists {
			return false
		}
	}

	return true
}

// matchesResource checks if a permission resource pattern matches the requested resource.
func (prm *PersistentRoleManager) matchesResource(permResource, requestedResource string) bool {
	// Wildcard matches all resources
	if permResource == ResourceAll {
		return true
	}

	// Exact match
	return permResource == requestedResource
}

// hasAction checks if an action list contains the requested action.
func (prm *PersistentRoleManager) hasAction(actions []string, requestedAction string) bool {
	for _, action := range actions {
		// Wildcard matches all actions
		if action == ActionAll {
			return true
		}

		// Exact match
		if action == requestedAction {
			return true
		}

		// Admin action includes all other actions
		if action == ActionAdmin {
			return true
		}
	}

	return false
}

// persistRole writes a role to persistent storage.
func (prm *PersistentRoleManager) persistRole(role *Role) error {
	// Serialize role data
	roleData, err := json.Marshal(role)
	if err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "auth.PersistentRoleManager.persistRole", "failed to marshal role")
	}

	// Store role: __quicbaas:roles:{roleName}
	key := prm.keyBuilder.RoleKey(role.Name)
	if err := prm.storage.Set(key, roleData); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "auth.PersistentRoleManager.persistRole", "failed to persist role")
	}

	return nil
}

// loadAllRoles loads all roles from persistent storage into cache.
func (prm *PersistentRoleManager) loadAllRoles() error {
	// Create iterator for role keys: __quicbaas:roles:*
	prefix := []byte(KeyPrefixRoles)
	opts := &storage.IteratorOptions{
		LowerBound: prefix,
		UpperBound: prefixUpperBound(prefix),
	}
	iter := prm.storage.NewIterator(opts)
	defer iter.Close()

	count := 0
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		key := iter.Key()
		value := iter.Value()

		// Skip metadata keys (they contain ":meta:")
		if len(key) > len(KeyPrefixRoleMetadata) &&
			string(key[:len(KeyPrefixRoleMetadata)]) == KeyPrefixRoleMetadata {
			continue
		}

		var role Role
		if err := json.Unmarshal(value, &role); err != nil {
			prm.logger.WithError(err).Warnf("Failed to unmarshal role data for key: %s", string(key))
			continue
		}

		prm.roles[role.Name] = &role
		count++
	}

	if err := iter.Error(); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "auth.PersistentRoleManager.loadAllRoles", "iterator error")
	}

	prm.logger.Infof("Loaded %d roles from persistent storage", count)
	return nil
}

// Count returns the number of roles in the manager.
func (prm *PersistentRoleManager) Count() int {
	prm.mu.RLock()
	defer prm.mu.RUnlock()
	return len(prm.roles)
}
