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
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OwnershipManagerOptions contains configuration for OwnershipManager.
type OwnershipManagerOptions struct {
	// OwnershipStorage is the storage backend for ownership records.
	OwnershipStorage OwnershipStorage

	// ShareStorage is the storage backend for share records.
	ShareStorage ShareStorage
}

// OwnershipManager manages resource ownership and sharing.
type OwnershipManager struct {
	ownershipStore OwnershipStorage
	shareStore     ShareStorage
}

// NewOwnershipManager creates a new OwnershipManager with the given options.
func NewOwnershipManager(opts OwnershipManagerOptions) (*OwnershipManager, error) {
	if opts.OwnershipStorage == nil {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.manager", "ownership storage is required", nil)
	}
	if opts.ShareStorage == nil {
		return nil, NewIAMError(ErrShareInvalid, "iam.ownership.manager", "share storage is required", nil)
	}

	return &OwnershipManager{
		ownershipStore: opts.OwnershipStorage,
		shareStore:     opts.ShareStorage,
	}, nil
}

// SetOwner creates an ownership record for a resource.
func (m *OwnershipManager) SetOwner(ctx context.Context, tenantID, resourceType, resourceID, ownerID string) (*ResourceOwnership, error) {
	if tenantID == "" {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.set", "tenant ID is required", nil)
	}
	if resourceType == "" {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.set", "resource type is required", nil)
	}
	if resourceID == "" {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.set", "resource ID is required", nil)
	}
	if ownerID == "" {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.set", "owner ID is required", nil)
	}

	ownership := &ResourceOwnership{
		ID:           uuid.New().String(),
		TenantID:     tenantID,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		OwnerID:      ownerID,
		CreatedAt:    time.Now(),
	}

	if err := m.ownershipStore.CreateOwnership(ctx, ownership); err != nil {
		return nil, err
	}

	return ownership, nil
}

// GetOwner retrieves the owner of a resource.
func (m *OwnershipManager) GetOwner(ctx context.Context, tenantID, resourceType, resourceID string) (*ResourceOwnership, error) {
	if tenantID == "" {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.get", "tenant ID is required", nil)
	}
	if resourceType == "" {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.get", "resource type is required", nil)
	}
	if resourceID == "" {
		return nil, NewIAMError(ErrOwnershipInvalid, "iam.ownership.get", "resource ID is required", nil)
	}

	return m.ownershipStore.GetOwnership(ctx, tenantID, resourceType, resourceID)
}

// TransferOwnership transfers ownership of a resource to a new owner.
func (m *OwnershipManager) TransferOwnership(ctx context.Context, tenantID, resourceType, resourceID, newOwnerID string) error {
	if tenantID == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.transfer", "tenant ID is required", nil)
	}
	if resourceType == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.transfer", "resource type is required", nil)
	}
	if resourceID == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.transfer", "resource ID is required", nil)
	}
	if newOwnerID == "" {
		return NewIAMError(ErrOwnershipInvalid, "iam.ownership.transfer", "new owner ID is required", nil)
	}

	return m.ownershipStore.TransferOwnership(ctx, tenantID, resourceType, resourceID, newOwnerID)
}

// ShareResource creates a share grant for a resource.
func (m *OwnershipManager) ShareResource(ctx context.Context, req *CreateShareRequest) (*ResourceShare, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// Verify ownership exists
	ownership, err := m.ownershipStore.GetOwnership(ctx, req.TenantID, req.ResourceType, req.ResourceID)
	if err != nil {
		return nil, NewIAMError(ErrOwnershipNotFound, "iam.share.create", fmt.Sprintf("cannot share resource without ownership: %v", err), err)
	}

	// Prevent sharing with self
	if ownership.OwnerID == req.GranteeID && req.GranteeType == GranteeTypeUser {
		return nil, NewIAMError(ErrCannotShareWithSelf, "iam.share.create", "cannot share resource with owner", nil)
	}

	share := &ResourceShare{
		ID:           uuid.New().String(),
		TenantID:     req.TenantID,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceID,
		OwnerID:      ownership.OwnerID,
		GranteeType:  req.GranteeType,
		GranteeID:    req.GranteeID,
		Permissions:  req.Permissions,
		ExpiresAt:    req.ExpiresAt,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    req.CreatedBy,
		Metadata:     req.Metadata,
	}

	if err := m.shareStore.CreateShare(ctx, share); err != nil {
		return nil, err
	}

	return share, nil
}

// UpdateShare updates the permissions on an existing share.
func (m *OwnershipManager) UpdateShare(ctx context.Context, tenantID, shareID string, permissions []SharePermission) (*ResourceShare, error) {
	if tenantID == "" {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.update", "tenant ID is required", nil)
	}
	if shareID == "" {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.update", "share ID is required", nil)
	}
	if len(permissions) == 0 {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.update", "at least one permission is required", nil)
	}

	// Validate permissions
	for _, perm := range permissions {
		if !perm.IsValid() {
			return nil, NewIAMError(ErrInvalidPermission, "iam.share.update", fmt.Sprintf("invalid permission: %s", perm), nil)
		}
	}

	share, err := m.shareStore.GetShare(ctx, tenantID, shareID)
	if err != nil {
		return nil, err
	}

	share.Permissions = permissions
	share.UpdatedAt = time.Now()

	if err := m.shareStore.UpdateShare(ctx, share); err != nil {
		return nil, err
	}

	return share, nil
}

// RevokeShare removes a share grant.
func (m *OwnershipManager) RevokeShare(ctx context.Context, tenantID, shareID, revokedBy string) error {
	if tenantID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.revoke", "tenant ID is required", nil)
	}
	if shareID == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.revoke", "share ID is required", nil)
	}
	if revokedBy == "" {
		return NewIAMError(ErrShareInvalid, "iam.share.revoke", "revoked by is required", nil)
	}

	return m.shareStore.DeleteShare(ctx, tenantID, shareID)
}

// CheckAccess checks if a user has access to a resource with the required permission.
func (m *OwnershipManager) CheckAccess(ctx context.Context, req *OwnershipCheckRequest) (*OwnershipCheckResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// First, check if user is the owner
	ownership, err := m.ownershipStore.GetOwnership(ctx, req.TenantID, req.ResourceType, req.ResourceID)
	if err != nil {
		// No ownership record means no access
		return &OwnershipCheckResponse{
			Allowed: false,
			Reason:  "resource ownership not found",
			IsOwner: false,
		}, nil
	}

	// Owner has all permissions
	if ownership.OwnerID == req.UserID {
		return &OwnershipCheckResponse{
			Allowed: true,
			Reason:  "user is resource owner",
			IsOwner: true,
		}, nil
	}

	// Check for shares
	shares, err := m.shareStore.GetActiveSharesForUser(ctx, req.TenantID, req.UserID, req.UserGroups)
	if err != nil {
		return nil, NewIAMError(ErrPermissionDenied, "iam.ownership.check", fmt.Sprintf("failed to retrieve shares: %v", err), err)
	}

	// Find a matching share for this resource with the required permission
	for _, share := range shares {
		if share.ResourceType == req.ResourceType && share.ResourceID == req.ResourceID {
			if share.IsExpired() {
				continue
			}
			if share.HasPermission(req.RequiredPermission) {
				return &OwnershipCheckResponse{
					Allowed:      true,
					Reason:       fmt.Sprintf("access granted via %s share", share.GranteeType),
					IsOwner:      false,
					MatchedShare: share,
				}, nil
			}
		}
	}

	return &OwnershipCheckResponse{
		Allowed: false,
		Reason:  "no matching share with required permission",
		IsOwner: false,
	}, nil
}

// ListUserShares lists all shares for a specific user (direct and via groups).
func (m *OwnershipManager) ListUserShares(ctx context.Context, tenantID, userID string, userGroups []string) ([]*ResourceShare, error) {
	if tenantID == "" {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.list", "tenant ID is required", nil)
	}
	if userID == "" {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.list", "user ID is required", nil)
	}

	return m.shareStore.GetActiveSharesForUser(ctx, tenantID, userID, userGroups)
}

// ListResourceShares lists all shares for a specific resource.
func (m *OwnershipManager) ListResourceShares(ctx context.Context, tenantID, resourceType, resourceID string) ([]*ResourceShare, error) {
	if tenantID == "" {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.list", "tenant ID is required", nil)
	}
	if resourceType == "" {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.list", "resource type is required", nil)
	}
	if resourceID == "" {
		return nil, NewIAMError(ErrShareInvalid, "iam.share.list", "resource ID is required", nil)
	}

	return m.shareStore.ListSharesByResource(ctx, tenantID, resourceType, resourceID)
}

// CleanupExpiredShares removes expired shares for a tenant.
func (m *OwnershipManager) CleanupExpiredShares(ctx context.Context, tenantID string) (int, error) {
	if tenantID == "" {
		return 0, NewIAMError(ErrShareInvalid, "iam.share.cleanup", "tenant ID is required", nil)
	}

	return m.shareStore.DeleteExpiredShares(ctx, tenantID)
}
