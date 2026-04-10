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

// Package auth provides role-based access control (RBAC) functionality
package auth

import (
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// RoleManager manages roles and permissions.
type RoleManager struct {
	roles  map[string]*Role
	logger *logger.Logger
	mu     sync.RWMutex
}

// Role represents a role with associated permissions.
type Role struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Permissions []*Permission `json:"permissions"`
}

// GetName returns the role name.
func (r *Role) GetName() string {
	return r.Name
}

// GetDescription returns the role description.
func (r *Role) GetDescription() string {
	return r.Description
}

// GetPermissions returns the role's permissions as interfaces.
func (r *Role) GetPermissions() []interfaces.PermissionInfo {
	result := make([]interfaces.PermissionInfo, len(r.Permissions))
	for i, p := range r.Permissions {
		result[i] = p
	}
	return result
}

// Permission represents an access control permission.
type Permission struct {
	Resource string   `json:"resource"` // e.g., "keys", "cluster", "users", "indexes"
	Actions  []string `json:"actions"`  // e.g., "read", "write", "delete", "admin"
}

// GetResource returns the permission resource.
func (p *Permission) GetResource() string {
	return p.Resource
}

// GetActions returns the permission actions.
func (p *Permission) GetActions() []string {
	return p.Actions
}

// Built-in role names.
const (
	RoleNameAdmin     = "admin"
	RoleNameReadWrite = "readwrite"
	RoleNameReadOnly  = "readonly"

	// Vector-specific role names.
	RoleNameVectorReader = "vector-reader"
	RoleNameVectorWriter = "vector-writer"
	RoleNameVectorAdmin  = "vector-admin"

	// Object storage role names.
	RoleNameObjstoreAdmin    = "objstore-admin"
	RoleNameObjstoreWriter   = "objstore-writer"
	RoleNameObjstoreReader   = "objstore-reader"
	RoleNameObjstoreOperator = "objstore-operator"

	// Parameter store role names.
	RoleNameParameterReader = "parameters-reader"
	RoleNameParameterWriter = "parameters-writer"
	RoleNameParameterAdmin  = "parameters-admin"
)

// Built-in resources.
const (
	ResourceKeys         = "keys"
	ResourceIndexes      = "indexes"
	ResourceCluster      = "cluster"
	ResourceUsers        = "users"
	ResourceBackups      = "backups"
	ResourceMetrics      = "metrics"
	ResourceHealth       = "health"
	ResourceTransactions = "transactions"
	ResourceWatch        = "watch"
	ResourceVector       = "vector"
	ResourceTimeseries   = "timeseries"
	ResourceAll          = "*"

	// Object storage resources.
	ResourceObjectStore = "objstore"
	ResourceBucket      = "bucket"
	ResourceObject      = "object"
	ResourceCatalog     = "catalog"

	// Parameter store resources.
	ResourceParameters = "parameters"
)

// Built-in actions.
const (
	ActionRead   = "read"
	ActionWrite  = "write"
	ActionDelete = "delete"
	ActionAdmin  = "admin"
	ActionAll    = "*"
)

// Vector-specific actions (matching interfaces.ActionVector* constants).
const (
	ActionVectorRead      = "vector:read"
	ActionVectorWrite     = "vector:write"
	ActionVectorDelete    = "vector:delete"
	ActionVectorAdmin     = "vector:admin"
	ActionVectorNamespace = "vector:namespace"
	ActionVectorSearch    = "vector:search"
	ActionVectorStats     = "vector:stats"
)

// Object storage-specific actions (matching interfaces.ActionObject* constants).
const (
	ActionObjectPut       = "object:put"
	ActionObjectGet       = "object:get"
	ActionObjectDelete    = "object:delete"
	ActionObjectList      = "object:list"
	ActionObjectReplicate = "object:replicate"
	ActionObjectMigrate   = "object:migrate"
	ActionObjectArchive   = "object:archive"
	ActionObjectVerify    = "object:verify"
	ActionObjectSetPolicy = "object:set_policy"
	ActionObjectSetACL    = "object:set_acl"
	ActionBucketCreate    = "bucket:create"
	ActionBucketDelete    = "bucket:delete"
	ActionBucketList      = "bucket:list"
	ActionBucketSetPolicy = "bucket:set_policy"
)

// Parameter store-specific actions (matching interfaces.ActionParameter* constants).
const (
	ActionParameterRead   = "parameter:read"
	ActionParameterWrite  = "parameter:write"
	ActionParameterDelete = "parameter:delete"
	ActionParameterList   = "parameter:list"
	ActionParameterAdmin  = "parameter:admin"
)

// NewRoleManager creates a new role manager with built-in roles.
func NewRoleManager(log *logger.Logger) *RoleManager {
	rm := &RoleManager{
		roles:  make(map[string]*Role),
		logger: log,
	}

	// Initialize built-in roles
	rm.initializeBuiltInRoles()

	return rm
}

// initializeBuiltInRoles creates the default roles.
func (rm *RoleManager) initializeBuiltInRoles() {
	// Admin role - full access to everything
	rm.roles[RoleNameAdmin] = &Role{
		Name:        RoleNameAdmin,
		Description: "Full administrative access to all resources",
		Permissions: []*Permission{
			{
				Resource: ResourceAll,
				Actions:  []string{ActionAll},
			},
		},
	}

	// ReadWrite role - read and write access to data
	rm.roles[RoleNameReadWrite] = &Role{
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
	}

	// ReadOnly role - read-only access
	rm.roles[RoleNameReadOnly] = &Role{
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
		},
	}

	// Vector-reader role - read and search access to vector operations
	rm.roles[RoleNameVectorReader] = &Role{
		Name:        RoleNameVectorReader,
		Description: "Read-only access to vector database operations including search",
		Permissions: []*Permission{
			{
				Resource: ResourceVector,
				Actions:  []string{ActionVectorRead, ActionVectorSearch, ActionVectorStats},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Vector-writer role - read, write, and search access to vector operations
	rm.roles[RoleNameVectorWriter] = &Role{
		Name:        RoleNameVectorWriter,
		Description: "Read and write access to vector database operations",
		Permissions: []*Permission{
			{
				Resource: ResourceVector,
				Actions:  []string{ActionVectorRead, ActionVectorWrite, ActionVectorSearch, ActionVectorStats},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Vector-admin role - full access to all vector operations
	rm.roles[RoleNameVectorAdmin] = &Role{
		Name:        RoleNameVectorAdmin,
		Description: "Full administrative access to vector database operations",
		Permissions: []*Permission{
			{
				Resource: ResourceVector,
				Actions: []string{
					ActionVectorRead,
					ActionVectorWrite,
					ActionVectorDelete,
					ActionVectorAdmin,
					ActionVectorNamespace,
					ActionVectorSearch,
					ActionVectorStats,
				},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Object storage roles
	rm.initializeObjectStorageRoles()

	rm.logger.Info("Built-in roles initialized: admin, readwrite, readonly, vector-reader, vector-writer, vector-admin, objstore-admin, objstore-writer, objstore-reader, objstore-operator")
}

// initializeObjectStorageRoles creates the object storage roles.
func (rm *RoleManager) initializeObjectStorageRoles() {
	// Object storage admin role - full access to object storage
	rm.roles[RoleNameObjstoreAdmin] = &Role{
		Name:        RoleNameObjstoreAdmin,
		Description: "Full object storage administration",
		Permissions: []*Permission{
			{
				Resource: ResourceObjectStore,
				Actions:  []string{ActionAll},
			},
			{
				Resource: ResourceBucket,
				Actions:  []string{ActionAll},
			},
			{
				Resource: ResourceObject,
				Actions:  []string{ActionAll},
			},
			{
				Resource: ResourceCatalog,
				Actions:  []string{ActionAll},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Object storage writer role - read/write objects
	rm.roles[RoleNameObjstoreWriter] = &Role{
		Name:        RoleNameObjstoreWriter,
		Description: "Read/write objects in object storage",
		Permissions: []*Permission{
			{
				Resource: ResourceObject,
				Actions: []string{
					ActionObjectPut,
					ActionObjectGet,
					ActionObjectDelete,
					ActionObjectList,
				},
			},
			{
				Resource: ResourceBucket,
				Actions:  []string{ActionBucketList},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Object storage reader role - read-only object access
	rm.roles[RoleNameObjstoreReader] = &Role{
		Name:        RoleNameObjstoreReader,
		Description: "Read-only object access in object storage",
		Permissions: []*Permission{
			{
				Resource: ResourceObject,
				Actions:  []string{ActionObjectGet, ActionObjectList},
			},
			{
				Resource: ResourceBucket,
				Actions:  []string{ActionBucketList},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Object storage operator role - operations (replicate, migrate, archive)
	rm.roles[RoleNameObjstoreOperator] = &Role{
		Name:        RoleNameObjstoreOperator,
		Description: "Object operations (replicate, migrate, archive, verify)",
		Permissions: []*Permission{
			{
				Resource: ResourceObject,
				Actions: []string{
					ActionObjectGet,
					ActionObjectList,
					ActionObjectReplicate,
					ActionObjectMigrate,
					ActionObjectArchive,
					ActionObjectVerify,
				},
			},
			{
				Resource: ResourceBucket,
				Actions:  []string{ActionBucketList},
			},
			{
				Resource: ResourceCatalog,
				Actions:  []string{ActionRead},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Parameter store reader role - read-only access to parameters
	rm.roles[RoleNameParameterReader] = &Role{
		Name:        RoleNameParameterReader,
		Description: "Read-only access to parameter store operations",
		Permissions: []*Permission{
			{
				Resource: ResourceParameters,
				Actions:  []string{ActionRead},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Parameter store writer role - read and write access to parameters
	rm.roles[RoleNameParameterWriter] = &Role{
		Name:        RoleNameParameterWriter,
		Description: "Read and write access to parameter store operations",
		Permissions: []*Permission{
			{
				Resource: ResourceParameters,
				Actions:  []string{ActionRead, ActionWrite},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}

	// Parameter store admin role - full access to parameters
	rm.roles[RoleNameParameterAdmin] = &Role{
		Name:        RoleNameParameterAdmin,
		Description: "Full administrative access to parameter store operations",
		Permissions: []*Permission{
			{
				Resource: ResourceParameters,
				Actions:  []string{ActionRead, ActionWrite, ActionDelete, ActionAdmin},
			},
			{
				Resource: ResourceHealth,
				Actions:  []string{ActionRead},
			},
		},
	}
}

// HasPermission checks if any of the user's roles have permission for the action.
func (rm *RoleManager) HasPermission(userRoles []string, resource, action string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, roleName := range userRoles {
		role, exists := rm.roles[roleName]
		if !exists {
			continue
		}

		// Check each permission in the role
		for _, perm := range role.Permissions {
			// Check if permission applies to this resource
			if !rm.matchesResource(perm.Resource, resource) {
				continue
			}

			// Check if permission includes this action
			if rm.hasAction(perm.Actions, action) {
				return true
			}
		}
	}

	return false
}

// HasVectorNamespacePermission checks if any of the user's roles have permission
// to perform an action on a specific vector namespace.
// This is a convenience method that constructs the namespace resource string.
func (rm *RoleManager) HasVectorNamespacePermission(userRoles []string, namespace, action string) bool {
	resource := interfaces.VectorNamespaceResource(namespace)
	return rm.HasPermission(userRoles, resource, action)
}

// HasObjectBucketPermission checks if any of the user's roles have permission
// to perform an action on a specific object bucket.
// This is a convenience method that constructs the bucket resource string.
func (rm *RoleManager) HasObjectBucketPermission(userRoles []string, bucket, action string) bool {
	resource := interfaces.ObjectBucketResource(bucket)
	return rm.HasPermission(userRoles, resource, action)
}

// matchesResource checks if a permission resource pattern matches the requested resource.
func (rm *RoleManager) matchesResource(permResource, requestedResource string) bool {
	// Wildcard matches all resources
	if permResource == ResourceAll {
		return true
	}

	// Exact match
	if permResource == requestedResource {
		return true
	}

	// Check for vector namespace matching
	// If requested resource is a namespace resource (e.g., "vector:namespace:embeddings"),
	// check if permission resource grants access
	if interfaces.IsVectorNamespaceResource(requestedResource) {
		namespace := interfaces.ExtractVectorNamespace(requestedResource)
		return interfaces.MatchesVectorNamespace(permResource, namespace)
	}

	// Check for object bucket matching
	// If requested resource is a bucket resource (e.g., "object:bucket:my-bucket"),
	// check if permission resource grants access
	if interfaces.IsObjectBucketResource(requestedResource) {
		bucket := interfaces.ExtractObjectBucket(requestedResource)
		return interfaces.MatchesObjectBucket(permResource, bucket)
	}

	return false
}

// hasAction checks if an action list contains the requested action.
func (rm *RoleManager) hasAction(actions []string, requestedAction string) bool {
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

		// Vector-admin action includes all vector-specific actions
		if action == ActionVectorAdmin && isVectorAction(requestedAction) {
			return true
		}

		// Object storage action hierarchies
		if isObjectStorageAction(requestedAction) && rm.objectActionImplies(action, requestedAction) {
			return true
		}
	}

	return false
}

// isVectorAction checks if an action is a vector-specific action.
func isVectorAction(action string) bool {
	switch action {
	case ActionVectorRead, ActionVectorWrite, ActionVectorDelete,
		ActionVectorAdmin, ActionVectorNamespace, ActionVectorSearch, ActionVectorStats:
		return true
	default:
		return false
	}
}

// isObjectStorageAction checks if an action is an object storage-specific action.
func isObjectStorageAction(action string) bool {
	switch action {
	case ActionObjectPut, ActionObjectGet, ActionObjectDelete, ActionObjectList,
		ActionObjectReplicate, ActionObjectMigrate, ActionObjectArchive,
		ActionObjectVerify, ActionObjectSetPolicy, ActionObjectSetACL,
		ActionBucketCreate, ActionBucketDelete, ActionBucketList, ActionBucketSetPolicy:
		return true
	default:
		return false
	}
}

// objectActionImplies checks if a granted action implies another action.
func (rm *RoleManager) objectActionImplies(grantedAction, requestedAction string) bool {
	// Admin implies all object actions
	if grantedAction == ActionAdmin || grantedAction == ActionAll {
		return true
	}

	// Write implies get/list
	if grantedAction == ActionWrite || grantedAction == ActionObjectPut {
		switch requestedAction {
		case ActionObjectGet, ActionObjectList, ActionRead:
			return true
		}
	}

	// Replicate/archive/migrate operations imply read
	switch grantedAction {
	case ActionObjectReplicate, ActionObjectMigrate, ActionObjectArchive:
		if requestedAction == ActionObjectGet || requestedAction == ActionObjectList || requestedAction == ActionRead {
			return true
		}
	}

	return false
}

// AllowsAnonymous checks if a resource/action allows anonymous access.
func (rm *RoleManager) AllowsAnonymous(resource, action string) bool {
	// Only allow anonymous access to health checks and metrics
	if resource == ResourceHealth && action == ActionRead {
		return true
	}

	if resource == ResourceMetrics && action == ActionRead {
		return true
	}

	return false
}

// AddRole adds a custom role.
// Accepts interfaces.RoleInfo but expects *Role for internal storage.
func (rm *RoleManager) AddRole(role interfaces.RoleInfo) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Check if it's already a *Role (internal use)
	if r, ok := role.(*Role); ok {
		rm.roles[r.Name] = r
		rm.logger.Infof("Role added: %s", r.Name)
		return nil
	}

	// Convert from interface (external use)
	perms := make([]*Permission, 0)
	for _, p := range role.GetPermissions() {
		perms = append(perms, &Permission{
			Resource: p.GetResource(),
			Actions:  p.GetActions(),
		})
	}
	newRole := &Role{
		Name:        role.GetName(),
		Description: role.GetDescription(),
		Permissions: perms,
	}
	rm.roles[newRole.Name] = newRole
	rm.logger.Infof("Role added: %s", newRole.Name)
	return nil
}

// GetRole retrieves a role by name.
func (rm *RoleManager) GetRole(name string) interfaces.RoleInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	role := rm.roles[name]
	if role == nil {
		return nil
	}
	return role
}

// ListRoles returns all available roles.
func (rm *RoleManager) ListRoles() []interfaces.RoleInfo {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	roles := make([]interfaces.RoleInfo, 0, len(rm.roles))
	for _, role := range rm.roles {
		roles = append(roles, role)
	}

	return roles
}

// DeleteRole removes a custom role (cannot delete built-in roles).
func (rm *RoleManager) DeleteRole(name string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Prevent deletion of built-in roles
	if isBuiltInRole(name) {
		rm.logger.Warnf("Attempt to delete built-in role: %s", name)
		return nil
	}

	delete(rm.roles, name)
	rm.logger.Infof("Role deleted: %s", name)
	return nil
}

// isBuiltInRole checks if a role name is a built-in role.
func isBuiltInRole(name string) bool {
	switch name {
	case RoleNameAdmin, RoleNameReadWrite, RoleNameReadOnly,
		RoleNameVectorReader, RoleNameVectorWriter, RoleNameVectorAdmin,
		RoleNameObjstoreAdmin, RoleNameObjstoreWriter, RoleNameObjstoreReader, RoleNameObjstoreOperator,
		RoleNameParameterReader, RoleNameParameterWriter, RoleNameParameterAdmin:
		return true
	default:
		return false
	}
}

// UpdateRole updates an existing role.
// Accepts interfaces.RoleInfo but expects *Role for internal storage.
func (rm *RoleManager) UpdateRole(role interfaces.RoleInfo) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	roleName := role.GetName()
	// Prevent modification of built-in roles
	if isBuiltInRole(roleName) {
		rm.logger.Warnf("Attempt to modify built-in role: %s", roleName)
		return nil
	}

	// Check if it's already a *Role (internal use)
	if r, ok := role.(*Role); ok {
		rm.roles[r.Name] = r
		rm.logger.Infof("Role updated: %s", r.Name)
		return nil
	}

	// Convert from interface (external use)
	perms := make([]*Permission, 0)
	for _, p := range role.GetPermissions() {
		perms = append(perms, &Permission{
			Resource: p.GetResource(),
			Actions:  p.GetActions(),
		})
	}
	newRole := &Role{
		Name:        roleName,
		Description: role.GetDescription(),
		Permissions: perms,
	}
	rm.roles[newRole.Name] = newRole
	rm.logger.Infof("Role updated: %s", newRole.Name)
	return nil
}

// ValidateRoles checks if all role names exist.
func (rm *RoleManager) ValidateRoles(roleNames []string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	for _, name := range roleNames {
		if _, exists := rm.roles[name]; !exists {
			return false
		}
	}

	return true
}
