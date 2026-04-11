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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Inheritance-specific error types.
var (
	// ErrHierarchyNotFound is returned when a hierarchy relationship is not found.
	ErrHierarchyNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.inheritance.hierarchy",
		Err:  &IAMMessageError{Message: "hierarchy relationship not found"},
	}

	// ErrHierarchyAlreadyExists is returned when attempting to create a duplicate hierarchy.
	ErrHierarchyAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.inheritance.hierarchy",
		Err:  &IAMMessageError{Message: "hierarchy relationship already exists"},
	}

	// ErrHierarchyCycle is returned when a hierarchy operation would create a cycle.
	ErrHierarchyCycle = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.inheritance.hierarchy",
		Err:  &IAMMessageError{Message: "hierarchy cycle detected"},
	}

	// ErrHierarchyInvalid is returned when hierarchy validation fails.
	ErrHierarchyInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.inheritance.hierarchy",
		Err:  &IAMMessageError{Message: "invalid hierarchy relationship"},
	}

	// ErrEffectivePermissionsNotFound is returned when cached permissions are not found.
	ErrEffectivePermissionsNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.inheritance.permissions",
		Err:  &IAMMessageError{Message: "effective permissions not found"},
	}
)

// RoleHierarchy represents a parent-child relationship between roles.
// Child roles inherit permissions from their parent roles.
type RoleHierarchy struct {
	// ID is the unique hierarchy relationship identifier.
	ID string `json:"id"`

	// TenantID is the owning tenant.
	TenantID string `json:"tenant_id"`

	// ParentRoleID is the parent role that provides permissions.
	ParentRoleID string `json:"parent_role_id"`

	// ChildRoleID is the child role that inherits permissions.
	ChildRoleID string `json:"child_role_id"`

	// Priority determines the order of inheritance when multiple parents exist.
	// Lower values have higher priority. Used for conflict resolution.
	Priority int `json:"priority"`

	// Created is the hierarchy creation timestamp.
	Created time.Time `json:"created"`
}

// Validate validates a RoleHierarchy for correctness.
func (h *RoleHierarchy) Validate() error {
	if h.ID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.role.validate", "hierarchy ID is required", nil)
	}
	if h.TenantID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.role.validate", "tenant ID is required", nil)
	}
	if h.ParentRoleID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.role.validate", "parent role ID is required", nil)
	}
	if h.ChildRoleID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.role.validate", "child role ID is required", nil)
	}
	if h.ParentRoleID == h.ChildRoleID {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.role.validate", "role cannot be its own parent", nil)
	}
	if h.Priority < 0 {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.role.validate", "priority must be non-negative", nil)
	}
	return nil
}

// GroupHierarchy represents a parent-child relationship between groups.
// Child groups can inherit members from their parent groups.
type GroupHierarchy struct {
	// ID is the unique hierarchy relationship identifier.
	ID string `json:"id"`

	// TenantID is the owning tenant.
	TenantID string `json:"tenant_id"`

	// ParentGroupID is the parent group.
	ParentGroupID string `json:"parent_group_id"`

	// ChildGroupID is the child group.
	ChildGroupID string `json:"child_group_id"`

	// InheritMembers indicates whether child inherits parent's members.
	// When true, all members of parent are considered members of child.
	InheritMembers bool `json:"inherit_members"`

	// Created is the hierarchy creation timestamp.
	Created time.Time `json:"created"`
}

// Validate validates a GroupHierarchy for correctness.
func (h *GroupHierarchy) Validate() error {
	if h.ID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.group.validate", "hierarchy ID is required", nil)
	}
	if h.TenantID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.group.validate", "tenant ID is required", nil)
	}
	if h.ParentGroupID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.group.validate", "parent group ID is required", nil)
	}
	if h.ChildGroupID == "" {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.group.validate", "child group ID is required", nil)
	}
	if h.ParentGroupID == h.ChildGroupID {
		return NewIAMError(ErrHierarchyInvalid, "iam.inheritance.group.validate", "group cannot be its own parent", nil)
	}
	return nil
}

// EffectivePermissions represents the computed permissions for a subject.
// This includes both directly assigned permissions and inherited ones.
type EffectivePermissions struct {
	// SubjectID is the user or role ID.
	SubjectID string `json:"subject_id"`

	// TenantID is the owning tenant.
	TenantID string `json:"tenant_id"`

	// DirectPermissions are permissions directly assigned to the subject.
	DirectPermissions []string `json:"direct_permissions"`

	// InheritedPermissions maps source role/group IDs to their permissions.
	// Key is the source ID, value is the list of permissions from that source.
	InheritedPermissions map[string][]string `json:"inherited_permissions"`

	// AllPermissions is the deduplicated union of all permissions.
	AllPermissions []string `json:"all_permissions"`

	// ComputedAt is when these permissions were calculated.
	ComputedAt time.Time `json:"computed_at"`
}

// Validate validates EffectivePermissions for correctness.
func (p *EffectivePermissions) Validate() error {
	if p.SubjectID == "" {
		return NewIAMError(ErrEffectivePermissionsNotFound, "iam.inheritance.permissions.validate", "subject ID is required", nil)
	}
	if p.TenantID == "" {
		return NewIAMError(ErrEffectivePermissionsNotFound, "iam.inheritance.permissions.validate", "tenant ID is required", nil)
	}
	return nil
}
