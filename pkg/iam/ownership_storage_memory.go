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
	"sync"
	"time"
)

// MemoryOwnershipStorage is an in-memory implementation of OwnershipStorage.
type MemoryOwnershipStorage struct {
	mu sync.RWMutex

	// ownerships maps "tenantID:resourceType:resourceID" to ResourceOwnership
	ownerships map[string]*ResourceOwnership

	// ownerIndex maps "tenantID:ownerID" to list of resource keys
	ownerIndex map[string][]string
}

// NewMemoryOwnershipStorage creates a new in-memory ownership storage.
func NewMemoryOwnershipStorage() *MemoryOwnershipStorage {
	return &MemoryOwnershipStorage{
		ownerships: make(map[string]*ResourceOwnership),
		ownerIndex: make(map[string][]string),
	}
}

// CreateOwnership creates a new resource ownership record.
func (s *MemoryOwnershipStorage) CreateOwnership(ctx context.Context, ownership *ResourceOwnership) error {
	if err := ownership.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeResourceKey(ownership.TenantID, ownership.ResourceType, ownership.ResourceID)
	if _, exists := s.ownerships[key]; exists {
		return NewIAMError(ErrOwnershipAlreadyExists, "iam.ownership.create", fmt.Sprintf("ownership already exists for resource %s", key), nil)
	}

	// Set creation time if not set
	if ownership.CreatedAt.IsZero() {
		ownership.CreatedAt = time.Now()
	}

	s.ownerships[key] = ownership

	// Update owner index
	ownerKey := s.makeOwnerKey(ownership.TenantID, ownership.OwnerID)
	s.ownerIndex[ownerKey] = append(s.ownerIndex[ownerKey], key)

	return nil
}

// GetOwnership retrieves ownership for a specific resource.
func (s *MemoryOwnershipStorage) GetOwnership(ctx context.Context, tenantID, resourceType, resourceID string) (*ResourceOwnership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.makeResourceKey(tenantID, resourceType, resourceID)
	ownership, exists := s.ownerships[key]
	if !exists {
		return nil, NewIAMError(ErrOwnershipNotFound, "iam.ownership.get", fmt.Sprintf("ownership not found for resource %s", key), nil)
	}

	return ownership, nil
}

// ListOwnershipsByOwner retrieves all resources owned by a specific user.
func (s *MemoryOwnershipStorage) ListOwnershipsByOwner(ctx context.Context, tenantID, ownerID string) ([]*ResourceOwnership, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ownerKey := s.makeOwnerKey(tenantID, ownerID)
	resourceKeys, exists := s.ownerIndex[ownerKey]
	if !exists {
		return []*ResourceOwnership{}, nil
	}

	ownerships := make([]*ResourceOwnership, 0, len(resourceKeys))
	for _, key := range resourceKeys {
		if ownership, exists := s.ownerships[key]; exists {
			ownerships = append(ownerships, ownership)
		}
	}

	return ownerships, nil
}

// DeleteOwnership removes an ownership record.
func (s *MemoryOwnershipStorage) DeleteOwnership(ctx context.Context, tenantID, resourceType, resourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeResourceKey(tenantID, resourceType, resourceID)
	ownership, exists := s.ownerships[key]
	if !exists {
		return NewIAMError(ErrOwnershipNotFound, "iam.ownership.delete", fmt.Sprintf("ownership not found for resource %s", key), nil)
	}

	// Remove from owner index
	ownerKey := s.makeOwnerKey(tenantID, ownership.OwnerID)
	if resourceKeys, exists := s.ownerIndex[ownerKey]; exists {
		filtered := make([]string, 0, len(resourceKeys))
		for _, k := range resourceKeys {
			if k != key {
				filtered = append(filtered, k)
			}
		}
		if len(filtered) > 0 {
			s.ownerIndex[ownerKey] = filtered
		} else {
			delete(s.ownerIndex, ownerKey)
		}
	}

	delete(s.ownerships, key)
	return nil
}

// TransferOwnership transfers ownership of a resource to a new owner.
func (s *MemoryOwnershipStorage) TransferOwnership(ctx context.Context, tenantID, resourceType, resourceID, newOwnerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeResourceKey(tenantID, resourceType, resourceID)
	ownership, exists := s.ownerships[key]
	if !exists {
		return NewIAMError(ErrOwnershipNotFound, "iam.ownership.transfer", fmt.Sprintf("ownership not found for resource %s", key), nil)
	}

	// Remove from old owner index
	oldOwnerKey := s.makeOwnerKey(tenantID, ownership.OwnerID)
	if resourceKeys, exists := s.ownerIndex[oldOwnerKey]; exists {
		filtered := make([]string, 0, len(resourceKeys))
		for _, k := range resourceKeys {
			if k != key {
				filtered = append(filtered, k)
			}
		}
		if len(filtered) > 0 {
			s.ownerIndex[oldOwnerKey] = filtered
		} else {
			delete(s.ownerIndex, oldOwnerKey)
		}
	}

	// Update ownership
	ownership.OwnerID = newOwnerID

	// Add to new owner index
	newOwnerKey := s.makeOwnerKey(tenantID, newOwnerID)
	s.ownerIndex[newOwnerKey] = append(s.ownerIndex[newOwnerKey], key)

	return nil
}

func (s *MemoryOwnershipStorage) makeResourceKey(tenantID, resourceType, resourceID string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID, resourceType, resourceID)
}

func (s *MemoryOwnershipStorage) makeOwnerKey(tenantID, ownerID string) string {
	return fmt.Sprintf("%s:%s", tenantID, ownerID)
}

// MemoryShareStorage is an in-memory implementation of ShareStorage.
type MemoryShareStorage struct {
	mu sync.RWMutex

	// shares maps "tenantID:shareID" to ResourceShare
	shares map[string]*ResourceShare

	// resourceIndex maps "tenantID:resourceType:resourceID" to list of share IDs
	resourceIndex map[string][]string

	// granteeIndex maps "tenantID:granteeType:granteeID" to list of share IDs
	granteeIndex map[string][]string

	// ownerIndex maps "tenantID:ownerID" to list of share IDs
	ownerIndex map[string][]string
}

// NewMemoryShareStorage creates a new in-memory share storage.
func NewMemoryShareStorage() *MemoryShareStorage {
	return &MemoryShareStorage{
		shares:        make(map[string]*ResourceShare),
		resourceIndex: make(map[string][]string),
		granteeIndex:  make(map[string][]string),
		ownerIndex:    make(map[string][]string),
	}
}

// CreateShare creates a new resource share.
func (s *MemoryShareStorage) CreateShare(ctx context.Context, share *ResourceShare) error {
	if err := share.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeShareKey(share.TenantID, share.ID)
	if _, exists := s.shares[key]; exists {
		return NewIAMError(ErrShareAlreadyExists, "iam.share.create", fmt.Sprintf("share %s already exists", share.ID), nil)
	}

	// Set timestamps if not set
	now := time.Now()
	if share.CreatedAt.IsZero() {
		share.CreatedAt = now
	}
	if share.UpdatedAt.IsZero() {
		share.UpdatedAt = now
	}

	s.shares[key] = share

	// Update indexes
	resourceKey := s.makeResourceKey(share.TenantID, share.ResourceType, share.ResourceID)
	s.resourceIndex[resourceKey] = append(s.resourceIndex[resourceKey], share.ID)

	granteeKey := s.makeGranteeKey(share.TenantID, share.GranteeType, share.GranteeID)
	s.granteeIndex[granteeKey] = append(s.granteeIndex[granteeKey], share.ID)

	ownerKey := s.makeOwnerKey(share.TenantID, share.OwnerID)
	s.ownerIndex[ownerKey] = append(s.ownerIndex[ownerKey], share.ID)

	return nil
}

// GetShare retrieves a specific share by ID.
func (s *MemoryShareStorage) GetShare(ctx context.Context, tenantID, shareID string) (*ResourceShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := s.makeShareKey(tenantID, shareID)
	share, exists := s.shares[key]
	if !exists {
		return nil, NewIAMError(ErrShareNotFound, "iam.share.get", fmt.Sprintf("share %s not found", shareID), nil)
	}

	return share, nil
}

// UpdateShare updates an existing share's permissions.
func (s *MemoryShareStorage) UpdateShare(ctx context.Context, share *ResourceShare) error {
	if err := share.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeShareKey(share.TenantID, share.ID)
	existing, exists := s.shares[key]
	if !exists {
		return NewIAMError(ErrShareNotFound, "iam.share.update", fmt.Sprintf("share %s not found", share.ID), nil)
	}

	// Preserve creation time, update modification time
	share.CreatedAt = existing.CreatedAt
	share.UpdatedAt = time.Now()

	s.shares[key] = share
	return nil
}

// DeleteShare removes a share.
func (s *MemoryShareStorage) DeleteShare(ctx context.Context, tenantID, shareID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.makeShareKey(tenantID, shareID)
	share, exists := s.shares[key]
	if !exists {
		return NewIAMError(ErrShareNotFound, "iam.share.delete", fmt.Sprintf("share %s not found", shareID), nil)
	}

	// Remove from indexes
	s.removeFromIndex(s.resourceIndex, s.makeResourceKey(share.TenantID, share.ResourceType, share.ResourceID), shareID)
	s.removeFromIndex(s.granteeIndex, s.makeGranteeKey(share.TenantID, share.GranteeType, share.GranteeID), shareID)
	s.removeFromIndex(s.ownerIndex, s.makeOwnerKey(share.TenantID, share.OwnerID), shareID)

	delete(s.shares, key)
	return nil
}

// ListSharesByResource retrieves all shares for a specific resource.
func (s *MemoryShareStorage) ListSharesByResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]*ResourceShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	resourceKey := s.makeResourceKey(tenantID, resourceType, resourceID)
	shareIDs, exists := s.resourceIndex[resourceKey]
	if !exists {
		return []*ResourceShare{}, nil
	}

	return s.getSharesByIDs(tenantID, shareIDs), nil
}

// ListSharesByGrantee retrieves all shares for a specific grantee.
func (s *MemoryShareStorage) ListSharesByGrantee(ctx context.Context, tenantID string, granteeType GranteeType, granteeID string) ([]*ResourceShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	granteeKey := s.makeGranteeKey(tenantID, granteeType, granteeID)
	shareIDs, exists := s.granteeIndex[granteeKey]
	if !exists {
		return []*ResourceShare{}, nil
	}

	return s.getSharesByIDs(tenantID, shareIDs), nil
}

// ListSharesByOwner retrieves all shares created by a specific owner.
func (s *MemoryShareStorage) ListSharesByOwner(ctx context.Context, tenantID, ownerID string) ([]*ResourceShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ownerKey := s.makeOwnerKey(tenantID, ownerID)
	shareIDs, exists := s.ownerIndex[ownerKey]
	if !exists {
		return []*ResourceShare{}, nil
	}

	return s.getSharesByIDs(tenantID, shareIDs), nil
}

// GetActiveSharesForUser retrieves all active shares for a user (direct and via groups).
func (s *MemoryShareStorage) GetActiveSharesForUser(ctx context.Context, tenantID, userID string, userGroups []string) ([]*ResourceShare, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	activeShares := make([]*ResourceShare, 0)

	// Get direct user shares
	userKey := s.makeGranteeKey(tenantID, GranteeTypeUser, userID)
	if shareIDs, exists := s.granteeIndex[userKey]; exists {
		for _, shareID := range shareIDs {
			share := s.getShareByID(tenantID, shareID)
			if share != nil && !share.IsExpired() {
				activeShares = append(activeShares, share)
			}
		}
	}

	// Get group shares
	for _, groupID := range userGroups {
		groupKey := s.makeGranteeKey(tenantID, GranteeTypeGroup, groupID)
		if shareIDs, exists := s.granteeIndex[groupKey]; exists {
			for _, shareID := range shareIDs {
				share := s.getShareByID(tenantID, shareID)
				if share != nil && (share.ExpiresAt == nil || share.ExpiresAt.After(now)) {
					activeShares = append(activeShares, share)
				}
			}
		}
	}

	return activeShares, nil
}

// DeleteExpiredShares removes all expired shares.
func (s *MemoryShareStorage) DeleteExpiredShares(ctx context.Context, tenantID string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	deleted := 0

	for key, share := range s.shares {
		if share.TenantID == tenantID && share.ExpiresAt != nil && share.ExpiresAt.Before(now) {
			// Remove from indexes
			s.removeFromIndex(s.resourceIndex, s.makeResourceKey(share.TenantID, share.ResourceType, share.ResourceID), share.ID)
			s.removeFromIndex(s.granteeIndex, s.makeGranteeKey(share.TenantID, share.GranteeType, share.GranteeID), share.ID)
			s.removeFromIndex(s.ownerIndex, s.makeOwnerKey(share.TenantID, share.OwnerID), share.ID)

			delete(s.shares, key)
			deleted++
		}
	}

	return deleted, nil
}

func (s *MemoryShareStorage) makeShareKey(tenantID, shareID string) string {
	return fmt.Sprintf("%s:%s", tenantID, shareID)
}

func (s *MemoryShareStorage) makeResourceKey(tenantID, resourceType, resourceID string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID, resourceType, resourceID)
}

func (s *MemoryShareStorage) makeGranteeKey(tenantID string, granteeType GranteeType, granteeID string) string {
	return fmt.Sprintf("%s:%s:%s", tenantID, granteeType, granteeID)
}

func (s *MemoryShareStorage) makeOwnerKey(tenantID, ownerID string) string {
	return fmt.Sprintf("%s:%s", tenantID, ownerID)
}

func (s *MemoryShareStorage) getShareByID(tenantID, shareID string) *ResourceShare {
	key := s.makeShareKey(tenantID, shareID)
	return s.shares[key]
}

func (s *MemoryShareStorage) getSharesByIDs(tenantID string, shareIDs []string) []*ResourceShare {
	shares := make([]*ResourceShare, 0, len(shareIDs))
	for _, shareID := range shareIDs {
		if share := s.getShareByID(tenantID, shareID); share != nil {
			shares = append(shares, share)
		}
	}
	return shares
}

func (s *MemoryShareStorage) removeFromIndex(index map[string][]string, key, shareID string) {
	if ids, exists := index[key]; exists {
		filtered := make([]string, 0, len(ids))
		for _, id := range ids {
			if id != shareID {
				filtered = append(filtered, id)
			}
		}
		if len(filtered) > 0 {
			index[key] = filtered
		} else {
			delete(index, key)
		}
	}
}
