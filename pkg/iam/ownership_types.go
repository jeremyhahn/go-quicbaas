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
	"fmt"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Ownership-specific error types.
var (
	// ErrOwnershipNotFound is returned when ownership is not found.
	ErrOwnershipNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.ownership",
		Err:  &IAMMessageError{Message: "ownership not found"},
	}

	// ErrOwnershipAlreadyExists is returned when ownership already exists.
	ErrOwnershipAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.ownership",
		Err:  &IAMMessageError{Message: "ownership already exists"},
	}

	// ErrOwnershipInvalid is returned when ownership validation fails.
	ErrOwnershipInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.ownership",
		Err:  &IAMMessageError{Message: "invalid ownership"},
	}

	// ErrShareNotFound is returned when a share is not found.
	ErrShareNotFound = &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   "iam.share",
		Err:  &IAMMessageError{Message: "share not found"},
	}

	// ErrShareAlreadyExists is returned when a share already exists.
	ErrShareAlreadyExists = &dberrors.QBError{
		Code: dberrors.ErrAlreadyExists,
		Op:   "iam.share",
		Err:  &IAMMessageError{Message: "share already exists"},
	}

	// ErrShareInvalid is returned when share validation fails.
	ErrShareInvalid = &dberrors.QBError{
		Code: dberrors.ErrValidation,
		Op:   "iam.share",
		Err:  &IAMMessageError{Message: "invalid share"},
	}

	// ErrShareExpired is returned when a share has expired.
	ErrShareExpired = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.share",
		Err:  &IAMMessageError{Message: "share expired"},
	}

	// ErrPermissionDenied is returned when permission check fails.
	ErrPermissionDenied = &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.ownership.permission",
		Err:  &IAMMessageError{Message: "permission denied"},
	}

	// ErrInvalidGranteeType is returned when grantee type is invalid.
	ErrInvalidGranteeType = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.share",
		Err:  &IAMMessageError{Message: "invalid grantee type"},
	}

	// ErrInvalidPermission is returned when permission is invalid.
	ErrInvalidPermission = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.share",
		Err:  &IAMMessageError{Message: "invalid permission"},
	}

	// ErrCannotShareWithSelf is returned when attempting to share with owner.
	ErrCannotShareWithSelf = &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   "iam.share",
		Err:  &IAMMessageError{Message: "cannot share resource with owner"},
	}
)

// GranteeType represents the type of entity receiving a share.
type GranteeType string

const (
	// GranteeTypeUser indicates the grantee is a user.
	GranteeTypeUser GranteeType = "user"

	// GranteeTypeGroup indicates the grantee is a group.
	GranteeTypeGroup GranteeType = "group"
)

// IsValid returns true if the GranteeType is valid.
func (g GranteeType) IsValid() bool {
	switch g {
	case GranteeTypeUser, GranteeTypeGroup:
		return true
	default:
		return false
	}
}

// SharePermission represents a permission that can be granted on a shared resource.
type SharePermission string

const (
	// PermissionRead allows reading the resource.
	PermissionRead SharePermission = "read"

	// PermissionWrite allows modifying the resource.
	PermissionWrite SharePermission = "write"

	// PermissionDelete allows deleting the resource.
	PermissionDelete SharePermission = "delete"

	// PermissionShare allows sharing the resource with others.
	PermissionShare SharePermission = "share"

	// PermissionAdmin grants all permissions including transferring ownership.
	PermissionAdmin SharePermission = "admin"
)

// IsValid returns true if the SharePermission is valid.
func (p SharePermission) IsValid() bool {
	switch p {
	case PermissionRead, PermissionWrite, PermissionDelete, PermissionShare, PermissionAdmin:
		return true
	default:
		return false
	}
}

// ResourceOwnership represents ownership of a resource.
type ResourceOwnership struct {
	// ID is the unique ownership identifier.
	ID string `json:"id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id"`

	// ResourceType is the type of resource (e.g., "database", "table", "secret").
	ResourceType string `json:"resource_type"`

	// ResourceID is the unique identifier of the resource.
	ResourceID string `json:"resource_id"`

	// OwnerID is the user ID of the owner.
	OwnerID string `json:"owner_id"`

	// CreatedAt is the ownership creation timestamp.
	CreatedAt time.Time `json:"created_at"`

	// Metadata contains additional ownership metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Validate validates a ResourceOwnership for correctness.
func (o *ResourceOwnership) Validate() error {
	if o.ID == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.validate", "ownership ID is required", nil)
	}
	if o.TenantID == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.validate", "tenant ID is required", nil)
	}
	if o.ResourceType == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.validate", "resource type is required", nil)
	}
	if o.ResourceID == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.validate", "resource ID is required", nil)
	}
	if o.OwnerID == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.validate", "owner ID is required", nil)
	}
	return nil
}

// ResourceShare represents a share grant on a resource.
type ResourceShare struct {
	// ID is the unique share identifier.
	ID string `json:"id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id"`

	// ResourceType is the type of resource being shared.
	ResourceType string `json:"resource_type"`

	// ResourceID is the unique identifier of the resource.
	ResourceID string `json:"resource_id"`

	// OwnerID is the user ID of the resource owner.
	OwnerID string `json:"owner_id"`

	// GranteeType is the type of grantee (user or group).
	GranteeType GranteeType `json:"grantee_type"`

	// GranteeID is the ID of the user or group receiving the share.
	GranteeID string `json:"grantee_id"`

	// Permissions are the granted permissions.
	Permissions []SharePermission `json:"permissions"`

	// ExpiresAt is the optional expiration time for the share.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// CreatedAt is the share creation timestamp.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is the last modification timestamp.
	UpdatedAt time.Time `json:"updated_at"`

	// CreatedBy is the user ID who created the share.
	CreatedBy string `json:"created_by"`

	// Metadata contains additional share metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Validate validates a ResourceShare for correctness.
func (s *ResourceShare) Validate() error {
	if s.ID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "share ID is required", nil)
	}
	if s.TenantID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "tenant ID is required", nil)
	}
	if s.ResourceType == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "resource type is required", nil)
	}
	if s.ResourceID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "resource ID is required", nil)
	}
	if s.OwnerID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "owner ID is required", nil)
	}
	if !s.GranteeType.IsValid() {
		return NewIAMError(ErrInvalidGranteeType, "iam.share.validate", fmt.Sprintf("invalid grantee type: %s", s.GranteeType), nil)
	}
	if s.GranteeID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "grantee ID is required", nil)
	}
	if s.OwnerID == s.GranteeID && s.GranteeType == GranteeTypeUser {
		return NewIAMError(ErrCannotShareWithSelf, "iam.share.validate", "cannot share resource with owner", nil)
	}
	if len(s.Permissions) == 0 {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "at least one permission is required", nil)
	}
	for _, perm := range s.Permissions {
		if !perm.IsValid() {
			return NewIAMError(ErrInvalidPermission, "iam.share.validate", fmt.Sprintf("invalid permission: %s", perm), nil)
		}
	}
	if s.CreatedBy == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.validate", "created by is required", nil)
	}
	return nil
}

// IsExpired returns true if the share has expired.
func (s *ResourceShare) IsExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*s.ExpiresAt)
}

// HasPermission returns true if the share includes the specified permission.
func (s *ResourceShare) HasPermission(perm SharePermission) bool {
	// Admin has all permissions
	for _, p := range s.Permissions {
		if p == PermissionAdmin || p == perm {
			return true
		}
	}
	return false
}

// OwnershipCheckRequest represents a request to check resource access.
type OwnershipCheckRequest struct {
	// TenantID is the tenant context.
	TenantID string `json:"tenant_id"`

	// ResourceType is the type of resource.
	ResourceType string `json:"resource_type"`

	// ResourceID is the unique identifier of the resource.
	ResourceID string `json:"resource_id"`

	// UserID is the user requesting access.
	UserID string `json:"user_id"`

	// RequiredPermission is the permission being checked.
	RequiredPermission SharePermission `json:"required_permission"`

	// UserGroups are the groups the user belongs to (for group share checks).
	UserGroups []string `json:"user_groups,omitempty"`
}

// Validate validates an OwnershipCheckRequest for correctness.
func (r *OwnershipCheckRequest) Validate() error {
	if r.TenantID == "" {
		return NewIAMError(ErrInvalidCheckRequest, "iam.ownership.check", "tenant ID is required", nil)
	}
	if r.ResourceType == "" {
		return NewIAMError(ErrInvalidCheckRequest, "iam.ownership.check", "resource type is required", nil)
	}
	if r.ResourceID == "" {
		return NewIAMError(ErrInvalidCheckRequest, "iam.ownership.check", "resource ID is required", nil)
	}
	if r.UserID == "" {
		return NewIAMError(ErrInvalidCheckRequest, "iam.ownership.check", "user ID is required", nil)
	}
	if !r.RequiredPermission.IsValid() {
		return NewIAMError(ErrInvalidPermission, "iam.ownership.check", fmt.Sprintf("invalid permission: %s", r.RequiredPermission), nil)
	}
	return nil
}

// OwnershipCheckResponse represents the response to an access check.
type OwnershipCheckResponse struct {
	// Allowed indicates whether access is granted.
	Allowed bool `json:"allowed"`

	// Reason provides context about the decision.
	Reason string `json:"reason,omitempty"`

	// IsOwner indicates if the user is the resource owner.
	IsOwner bool `json:"is_owner"`

	// MatchedShare is the share that granted access (if any).
	MatchedShare *ResourceShare `json:"matched_share,omitempty"`
}

// CreateShareRequest represents a request to create a resource share.
type CreateShareRequest struct {
	// TenantID is the tenant context.
	TenantID string `json:"tenant_id"`

	// ResourceType is the type of resource being shared.
	ResourceType string `json:"resource_type"`

	// ResourceID is the unique identifier of the resource.
	ResourceID string `json:"resource_id"`

	// GranteeType is the type of grantee (user or group).
	GranteeType GranteeType `json:"grantee_type"`

	// GranteeID is the ID of the user or group receiving the share.
	GranteeID string `json:"grantee_id"`

	// Permissions are the permissions to grant.
	Permissions []SharePermission `json:"permissions"`

	// ExpiresAt is the optional expiration time for the share.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`

	// CreatedBy is the user ID creating the share.
	CreatedBy string `json:"created_by"`

	// Metadata contains additional share metadata.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Validate validates a CreateShareRequest for correctness.
func (r *CreateShareRequest) Validate() error {
	if r.TenantID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.create", "tenant ID is required", nil)
	}
	if r.ResourceType == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.create", "resource type is required", nil)
	}
	if r.ResourceID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.create", "resource ID is required", nil)
	}
	if !r.GranteeType.IsValid() {
		return NewIAMError(ErrInvalidGranteeType, "iam.share.create", fmt.Sprintf("invalid grantee type: %s", r.GranteeType), nil)
	}
	if r.GranteeID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.create", "grantee ID is required", nil)
	}
	if len(r.Permissions) == 0 {
		return NewIAMError(ErrShareInvalid, "iam.share.create", "at least one permission is required", nil)
	}
	for _, perm := range r.Permissions {
		if !perm.IsValid() {
			return NewIAMError(ErrInvalidPermission, "iam.share.create", fmt.Sprintf("invalid permission: %s", perm), nil)
		}
	}
	if r.CreatedBy == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.create", "created by is required", nil)
	}
	if r.ExpiresAt != nil && r.ExpiresAt.Before(time.Now()) {
		return NewIAMError(ErrShareInvalid, "iam.share.create", "expiration time must be in the future", nil)
	}
	return nil
}
