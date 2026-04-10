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
)

// OwnershipStorage defines the interface for storing and retrieving resource ownership.
type OwnershipStorage interface {
	// CreateOwnership creates a new resource ownership record.
	CreateOwnership(ctx context.Context, ownership *ResourceOwnership) error

	// GetOwnership retrieves ownership for a specific resource.
	GetOwnership(ctx context.Context, tenantID, resourceType, resourceID string) (*ResourceOwnership, error)

	// ListOwnershipsByOwner retrieves all resources owned by a specific user.
	ListOwnershipsByOwner(ctx context.Context, tenantID, ownerID string) ([]*ResourceOwnership, error)

	// DeleteOwnership removes an ownership record.
	DeleteOwnership(ctx context.Context, tenantID, resourceType, resourceID string) error

	// TransferOwnership transfers ownership of a resource to a new owner.
	TransferOwnership(ctx context.Context, tenantID, resourceType, resourceID, newOwnerID string) error
}

// ShareStorage defines the interface for storing and retrieving resource shares.
type ShareStorage interface {
	// CreateShare creates a new resource share.
	CreateShare(ctx context.Context, share *ResourceShare) error

	// GetShare retrieves a specific share by ID.
	GetShare(ctx context.Context, tenantID, shareID string) (*ResourceShare, error)

	// UpdateShare updates an existing share's permissions.
	UpdateShare(ctx context.Context, share *ResourceShare) error

	// DeleteShare removes a share.
	DeleteShare(ctx context.Context, tenantID, shareID string) error

	// ListSharesByResource retrieves all shares for a specific resource.
	ListSharesByResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]*ResourceShare, error)

	// ListSharesByGrantee retrieves all shares for a specific grantee.
	ListSharesByGrantee(ctx context.Context, tenantID string, granteeType GranteeType, granteeID string) ([]*ResourceShare, error)

	// ListSharesByOwner retrieves all shares created by a specific owner.
	ListSharesByOwner(ctx context.Context, tenantID, ownerID string) ([]*ResourceShare, error)

	// GetActiveSharesForUser retrieves all active shares for a user (direct and via groups).
	GetActiveSharesForUser(ctx context.Context, tenantID, userID string, userGroups []string) ([]*ResourceShare, error)

	// DeleteExpiredShares removes all expired shares.
	DeleteExpiredShares(ctx context.Context, tenantID string) (int, error)
}
