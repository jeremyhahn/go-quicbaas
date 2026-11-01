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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestNewOwnershipManager(t *testing.T) {
	tests := []struct {
		name    string
		opts    OwnershipManagerOptions
		wantErr bool
	}{
		{
			name: "valid options",
			opts: OwnershipManagerOptions{
				OwnershipStorage: NewMemoryOwnershipStorage(),
				ShareStorage:     NewMemoryShareStorage(),
			},
			wantErr: false,
		},
		{
			name: "missing ownership storage",
			opts: OwnershipManagerOptions{
				ShareStorage: NewMemoryShareStorage(),
			},
			wantErr: true,
		},
		{
			name: "missing share storage",
			opts: OwnershipManagerOptions{
				OwnershipStorage: NewMemoryOwnershipStorage(),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := NewOwnershipManager(tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, mgr)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, mgr)
			}
		})
	}
}

func TestOwnershipManager_SetOwner(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	tests := []struct {
		name         string
		tenantID     string
		resourceType string
		resourceID   string
		ownerID      string
		wantErr      bool
	}{
		{
			name:         "valid ownership",
			tenantID:     "tenant1",
			resourceType: "database",
			resourceID:   "db1",
			ownerID:      "user1",
			wantErr:      false,
		},
		{
			name:         "missing tenant ID",
			tenantID:     "",
			resourceType: "database",
			resourceID:   "db1",
			ownerID:      "user1",
			wantErr:      true,
		},
		{
			name:         "missing resource type",
			tenantID:     "tenant1",
			resourceType: "",
			resourceID:   "db1",
			ownerID:      "user1",
			wantErr:      true,
		},
		{
			name:         "missing resource ID",
			tenantID:     "tenant1",
			resourceType: "database",
			resourceID:   "",
			ownerID:      "user1",
			wantErr:      true,
		},
		{
			name:         "missing owner ID",
			tenantID:     "tenant1",
			resourceType: "database",
			resourceID:   "db1",
			ownerID:      "",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownership, err := mgr.SetOwner(ctx, tt.tenantID, tt.resourceType, tt.resourceID, tt.ownerID)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, ownership)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, ownership)
				assert.NotEmpty(t, ownership.ID)
				assert.Equal(t, tt.tenantID, ownership.TenantID)
				assert.Equal(t, tt.resourceType, ownership.ResourceType)
				assert.Equal(t, tt.resourceID, ownership.ResourceID)
				assert.Equal(t, tt.ownerID, ownership.OwnerID)
				assert.False(t, ownership.CreatedAt.IsZero())
			}
		})
	}
}

func TestOwnershipManager_GetOwner(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Create ownership
	ownership, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	tests := []struct {
		name         string
		tenantID     string
		resourceType string
		resourceID   string
		errMsg       string
	}{
		{name: "existing ownership", tenantID: "tenant1", resourceType: "database", resourceID: "db1"},
		{name: "non-existent ownership", tenantID: "tenant1", resourceType: "database", resourceID: "db2", errMsg: "not found"},
		{name: "missing tenant ID", tenantID: "", resourceType: "database", resourceID: "db1", errMsg: "tenant ID"},
		{name: "missing resource type", tenantID: "tenant1", resourceType: "", resourceID: "db1", errMsg: "resource type"},
		{name: "missing resource ID", tenantID: "tenant1", resourceType: "database", resourceID: "", errMsg: "resource ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mgr.GetOwner(ctx, tt.tenantID, tt.resourceType, tt.resourceID)
			if tt.errMsg != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				return
			}
			assert.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, ownership.ID, result.ID)
			assert.Equal(t, ownership.OwnerID, result.OwnerID)
		})
	}
}

func TestOwnershipManager_TransferOwnership(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Create ownership
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	// Transfer ownership
	err = mgr.TransferOwnership(ctx, "tenant1", "database", "db1", "user2")
	assert.NoError(t, err)

	// Verify transfer
	ownership, err := mgr.GetOwner(ctx, "tenant1", "database", "db1")
	require.NoError(t, err)
	assert.Equal(t, "user2", ownership.OwnerID)

	errorCases := []struct {
		name         string
		tenantID     string
		resourceType string
		resourceID   string
		newOwnerID   string
		errMsg       string
	}{
		{name: "missing tenant ID", tenantID: "", resourceType: "database", resourceID: "db1", newOwnerID: "user3", errMsg: "tenant ID"},
		{name: "missing resource type", tenantID: "tenant1", resourceType: "", resourceID: "db1", newOwnerID: "user3", errMsg: "resource type"},
		{name: "missing resource ID", tenantID: "tenant1", resourceType: "database", resourceID: "", newOwnerID: "user3", errMsg: "resource ID"},
		{name: "missing new owner ID", tenantID: "tenant1", resourceType: "database", resourceID: "db1", newOwnerID: "", errMsg: "owner ID"},
		{name: "non-existent ownership", tenantID: "tenant1", resourceType: "database", resourceID: "nonexistent", newOwnerID: "user3", errMsg: "not found"},
	}

	for _, tt := range errorCases {
		t.Run(tt.name, func(t *testing.T) {
			err := mgr.TransferOwnership(ctx, tt.tenantID, tt.resourceType, tt.resourceID, tt.newOwnerID)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestOwnershipManager_ShareResource(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Create ownership
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	tests := []struct {
		name    string
		req     *CreateShareRequest
		wantErr bool
	}{
		{
			name: "valid share with user",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db1",
				GranteeType:  GranteeTypeUser,
				GranteeID:    "user2",
				Permissions:  []SharePermission{PermissionRead, PermissionWrite},
				CreatedBy:    "user1",
			},
			wantErr: false,
		},
		{
			name: "valid share with group",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db1",
				GranteeType:  GranteeTypeGroup,
				GranteeID:    "group1",
				Permissions:  []SharePermission{PermissionRead},
				CreatedBy:    "user1",
			},
			wantErr: false,
		},
		{
			name: "share with expiration",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db1",
				GranteeType:  GranteeTypeUser,
				GranteeID:    "user3",
				Permissions:  []SharePermission{PermissionRead},
				ExpiresAt:    timePtrOwnership(time.Now().Add(24 * time.Hour)),
				CreatedBy:    "user1",
			},
			wantErr: false,
		},
		{
			name: "share with owner as grantee",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db1",
				GranteeType:  GranteeTypeUser,
				GranteeID:    "user1",
				Permissions:  []SharePermission{PermissionRead},
				CreatedBy:    "user1",
			},
			wantErr: true,
		},
		{
			name: "share without ownership",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db99",
				GranteeType:  GranteeTypeUser,
				GranteeID:    "user2",
				Permissions:  []SharePermission{PermissionRead},
				CreatedBy:    "user1",
			},
			wantErr: true,
		},
		{
			name: "invalid grantee type",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db1",
				GranteeType:  "invalid",
				GranteeID:    "user2",
				Permissions:  []SharePermission{PermissionRead},
				CreatedBy:    "user1",
			},
			wantErr: true,
		},
		{
			name: "invalid permission",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db1",
				GranteeType:  GranteeTypeUser,
				GranteeID:    "user2",
				Permissions:  []SharePermission{"invalid"},
				CreatedBy:    "user1",
			},
			wantErr: true,
		},
		{
			name: "expired expiration",
			req: &CreateShareRequest{
				TenantID:     "tenant1",
				ResourceType: "database",
				ResourceID:   "db1",
				GranteeType:  GranteeTypeUser,
				GranteeID:    "user2",
				Permissions:  []SharePermission{PermissionRead},
				ExpiresAt:    timePtrOwnership(time.Now().Add(-1 * time.Hour)),
				CreatedBy:    "user1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			share, err := mgr.ShareResource(ctx, tt.req)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, share)
			} else {
				assert.NoError(t, err)
				require.NotNil(t, share)
				assert.NotEmpty(t, share.ID)
				assert.Equal(t, tt.req.TenantID, share.TenantID)
				assert.Equal(t, tt.req.ResourceType, share.ResourceType)
				assert.Equal(t, tt.req.ResourceID, share.ResourceID)
				assert.Equal(t, "user1", share.OwnerID) // Ownership was set to user1
				assert.Equal(t, tt.req.GranteeType, share.GranteeType)
				assert.Equal(t, tt.req.GranteeID, share.GranteeID)
				assert.Equal(t, tt.req.Permissions, share.Permissions)
				assert.False(t, share.CreatedAt.IsZero())
				assert.False(t, share.UpdatedAt.IsZero())
			}
		})
	}
}

func TestOwnershipManager_UpdateShare(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and share
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	share, err := mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// Test update
	updated, err := mgr.UpdateShare(ctx, "tenant1", share.ID, []SharePermission{PermissionRead, PermissionWrite})
	assert.NoError(t, err)
	require.NotNil(t, updated)
	assert.Len(t, updated.Permissions, 2)
	assert.Contains(t, updated.Permissions, PermissionRead)
	assert.Contains(t, updated.Permissions, PermissionWrite)

	errorCases := []struct {
		name        string
		tenantID    string
		shareID     string
		permissions []SharePermission
		errMsg      string
	}{
		{name: "non-existent share", tenantID: "tenant1", shareID: "nonexistent", permissions: []SharePermission{PermissionRead}, errMsg: "not found"},
		{name: "missing tenant ID", tenantID: "", shareID: share.ID, permissions: []SharePermission{PermissionRead}, errMsg: "tenant ID"},
		{name: "missing share ID", tenantID: "tenant1", shareID: "", permissions: []SharePermission{PermissionRead}, errMsg: "share ID"},
		{name: "no permissions", tenantID: "tenant1", shareID: share.ID, permissions: nil, errMsg: "at least one permission"},
		{name: "invalid permission", tenantID: "tenant1", shareID: share.ID, permissions: []SharePermission{"invalid"}, errMsg: "invalid permission"},
	}

	for _, tt := range errorCases {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.UpdateShare(ctx, tt.tenantID, tt.shareID, tt.permissions)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestOwnershipManager_RevokeShare(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and share
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	share, err := mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// Revoke share
	err = mgr.RevokeShare(ctx, "tenant1", share.ID, "user1")
	assert.NoError(t, err)

	// Verify share is deleted
	_, err = mgr.shareStore.GetShare(ctx, "tenant1", share.ID)
	assert.Error(t, err)

	// Test error cases
	err = mgr.RevokeShare(ctx, "", share.ID, "user1")
	assert.Error(t, err)

	err = mgr.RevokeShare(ctx, "tenant1", "", "user1")
	assert.Error(t, err)

	err = mgr.RevokeShare(ctx, "tenant1", share.ID, "")
	assert.Error(t, err)
}

func TestOwnershipManager_CheckAccess_Owner(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Create ownership
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	// Check owner access
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user1",
		RequiredPermission: PermissionRead,
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)
	assert.True(t, resp.IsOwner)
	assert.Contains(t, resp.Reason, "owner")

	// Owner should have all permissions
	for _, perm := range []SharePermission{PermissionRead, PermissionWrite, PermissionDelete, PermissionShare, PermissionAdmin} {
		resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
			TenantID:           "tenant1",
			ResourceType:       "database",
			ResourceID:         "db1",
			UserID:             "user1",
			RequiredPermission: perm,
		})
		assert.NoError(t, err)
		assert.True(t, resp.Allowed)
		assert.True(t, resp.IsOwner)
	}
}

func TestOwnershipManager_CheckAccess_UserShare(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and share
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead, PermissionWrite},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// Check user has read permission
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionRead,
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)
	assert.False(t, resp.IsOwner)
	assert.NotNil(t, resp.MatchedShare)

	// Check user has write permission
	resp, err = mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionWrite,
	})

	assert.NoError(t, err)
	assert.True(t, resp.Allowed)

	// Check user does not have delete permission
	resp, err = mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionDelete,
	})

	assert.NoError(t, err)
	assert.False(t, resp.Allowed)
}

func TestOwnershipManager_CheckAccess_GroupShare(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and group share
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeGroup,
		GranteeID:    "group1",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// User in group should have access
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionRead,
		UserGroups:         []string{"group1"},
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)
	assert.False(t, resp.IsOwner)
	assert.NotNil(t, resp.MatchedShare)

	// User not in group should not have access
	resp, err = mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user3",
		RequiredPermission: PermissionRead,
		UserGroups:         []string{"group2"},
	})

	assert.NoError(t, err)
	assert.False(t, resp.Allowed)
}

func TestOwnershipManager_CheckAccess_ExpiredShare(t *testing.T) {
	ctx := context.Background()
	shareStore := NewMemoryShareStorage()
	mgr, err := NewOwnershipManager(OwnershipManagerOptions{
		OwnershipStorage: NewMemoryOwnershipStorage(),
		ShareStorage:     shareStore,
	})
	require.NoError(t, err)

	// Setup: Create ownership
	_, err = mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	// ShareResource rejects a past expiry, so store the expired share directly
	require.NoError(t, shareStore.CreateShare(ctx, expiredTestShare("share1", "user2")))

	// Expired share should not grant access
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionRead,
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Allowed)
}

func TestOwnershipManager_CheckAccess_AdminPermission(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and share with admin permission
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionAdmin},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// Admin should have all permissions
	for _, perm := range []SharePermission{PermissionRead, PermissionWrite, PermissionDelete, PermissionShare} {
		resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
			TenantID:           "tenant1",
			ResourceType:       "database",
			ResourceID:         "db1",
			UserID:             "user2",
			RequiredPermission: perm,
		})
		assert.NoError(t, err, "checking permission: %s", perm)
		assert.True(t, resp.Allowed, "admin should have permission: %s", perm)
	}
}

func TestOwnershipManager_CheckAccess_ShareDelegation(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and share with share permission
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead, PermissionShare},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// User2 should be able to share with share permission
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionShare,
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Allowed)

	// User2 cannot delete without delete permission
	resp, err = mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionDelete,
	})

	assert.NoError(t, err)
	assert.False(t, resp.Allowed)
}

func TestOwnershipManager_CheckAccess_NoOwnership(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Check access without ownership
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user1",
		RequiredPermission: PermissionRead,
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Allowed)
	assert.False(t, resp.IsOwner)
	assert.Contains(t, resp.Reason, "ownership not found")
}

func TestOwnershipManager_ListUserShares(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and multiple shares
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeGroup,
		GranteeID:    "group1",
		Permissions:  []SharePermission{PermissionWrite},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// List shares for user2 (direct share)
	shares, err := mgr.ListUserShares(ctx, "tenant1", "user2", nil)
	assert.NoError(t, err)
	assert.Len(t, shares, 1)

	// List shares for user2 in group1 (direct + group)
	shares, err = mgr.ListUserShares(ctx, "tenant1", "user2", []string{"group1"})
	assert.NoError(t, err)
	assert.Len(t, shares, 2)

	// A user with no shares gets an empty list, not an error
	shares, err = mgr.ListUserShares(ctx, "tenant1", "user3", nil)
	assert.NoError(t, err)
	assert.Empty(t, shares)

	// Test error cases
	_, err = mgr.ListUserShares(ctx, "", "user2", nil)
	assert.Error(t, err)

	_, err = mgr.ListUserShares(ctx, "tenant1", "", nil)
	assert.Error(t, err)
}

func TestOwnershipManager_ListResourceShares(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// A resource with no shares gets an empty list, not an error
	shares, err := mgr.ListResourceShares(ctx, "tenant1", "database", "db1")
	assert.NoError(t, err)
	assert.Empty(t, shares)

	// Setup: Create ownership and multiple shares
	_, err = mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user3",
		Permissions:  []SharePermission{PermissionWrite},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// List all shares for resource
	shares, err = mgr.ListResourceShares(ctx, "tenant1", "database", "db1")
	assert.NoError(t, err)
	assert.Len(t, shares, 2)

	// Test error cases
	_, err = mgr.ListResourceShares(ctx, "", "database", "db1")
	assert.Error(t, err)

	_, err = mgr.ListResourceShares(ctx, "tenant1", "", "db1")
	assert.Error(t, err)

	_, err = mgr.ListResourceShares(ctx, "tenant1", "database", "")
	assert.Error(t, err)
}

func TestOwnershipManager_CleanupExpiredShares(t *testing.T) {
	ctx := context.Background()
	shareStore := NewMemoryShareStorage()
	mgr, err := NewOwnershipManager(OwnershipManagerOptions{
		OwnershipStorage: NewMemoryOwnershipStorage(),
		ShareStorage:     shareStore,
	})
	require.NoError(t, err)

	// Setup: Create ownership and shares with different expiration times
	_, err = mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	// Active share
	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// ShareResource rejects a past expiry, so store the expired share directly
	require.NoError(t, shareStore.CreateShare(ctx, expiredTestShare("expired1", "user3")))

	// Cleanup expired shares
	count, err := mgr.CleanupExpiredShares(ctx, "tenant1")
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify only active share remains
	shares, err := mgr.ListResourceShares(ctx, "tenant1", "database", "db1")
	assert.NoError(t, err)
	assert.Len(t, shares, 1)
	assert.Equal(t, "user2", shares[0].GranteeID)

	// Test error case
	_, err = mgr.CleanupExpiredShares(ctx, "")
	assert.Error(t, err)
}

func TestResourceShare_IsExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt *time.Time
		want      bool
	}{
		{
			name:      "nil expiration",
			expiresAt: nil,
			want:      false,
		},
		{
			name:      "future expiration",
			expiresAt: timePtrOwnership(time.Now().Add(24 * time.Hour)),
			want:      false,
		},
		{
			name:      "past expiration",
			expiresAt: timePtrOwnership(time.Now().Add(-1 * time.Hour)),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			share := &ResourceShare{
				ExpiresAt: tt.expiresAt,
			}
			assert.Equal(t, tt.want, share.IsExpired())
		})
	}
}

func TestResourceShare_HasPermission(t *testing.T) {
	tests := []struct {
		name        string
		permissions []SharePermission
		check       SharePermission
		want        bool
	}{
		{
			name:        "has exact permission",
			permissions: []SharePermission{PermissionRead, PermissionWrite},
			check:       PermissionRead,
			want:        true,
		},
		{
			name:        "does not have permission",
			permissions: []SharePermission{PermissionRead},
			check:       PermissionWrite,
			want:        false,
		},
		{
			name:        "admin has all permissions",
			permissions: []SharePermission{PermissionAdmin},
			check:       PermissionRead,
			want:        true,
		},
		{
			name:        "admin has write permission",
			permissions: []SharePermission{PermissionAdmin},
			check:       PermissionWrite,
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			share := &ResourceShare{
				Permissions: tt.permissions,
			}
			assert.Equal(t, tt.want, share.HasPermission(tt.check))
		})
	}
}

func TestGranteeType_IsValid(t *testing.T) {
	tests := []struct {
		name string
		gt   GranteeType
		want bool
	}{
		{name: "user", gt: GranteeTypeUser, want: true},
		{name: "group", gt: GranteeTypeGroup, want: true},
		{name: "invalid", gt: "invalid", want: false},
		{name: "role", gt: "role", want: false},
		{name: "empty", gt: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.gt.IsValid())
		})
	}
}

func TestSharePermission_IsValid(t *testing.T) {
	tests := []struct {
		name string
		perm SharePermission
		want bool
	}{
		{name: "read", perm: PermissionRead, want: true},
		{name: "write", perm: PermissionWrite, want: true},
		{name: "delete", perm: PermissionDelete, want: true},
		{name: "share", perm: PermissionShare, want: true},
		{name: "admin", perm: PermissionAdmin, want: true},
		{name: "invalid", perm: "invalid", want: false},
		{name: "execute", perm: "execute", want: false},
		{name: "empty", perm: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.perm.IsValid())
		})
	}
}

func TestResourceOwnership_Validate(t *testing.T) {
	valid := func() *ResourceOwnership {
		return &ResourceOwnership{
			ID:           "id1",
			TenantID:     "tenant1",
			ResourceType: "database",
			ResourceID:   "db1",
			OwnerID:      "user1",
		}
	}

	tests := []struct {
		name   string
		mutate func(o *ResourceOwnership)
		errMsg string
	}{
		{name: "valid", mutate: func(o *ResourceOwnership) {}},
		{name: "missing ID", mutate: func(o *ResourceOwnership) { o.ID = "" }, errMsg: "ownership ID"},
		{name: "missing tenant ID", mutate: func(o *ResourceOwnership) { o.TenantID = "" }, errMsg: "tenant ID"},
		{name: "missing resource type", mutate: func(o *ResourceOwnership) { o.ResourceType = "" }, errMsg: "resource type"},
		{name: "missing resource ID", mutate: func(o *ResourceOwnership) { o.ResourceID = "" }, errMsg: "resource ID"},
		{name: "missing owner ID", mutate: func(o *ResourceOwnership) { o.OwnerID = "" }, errMsg: "owner ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ownership := valid()
			tt.mutate(ownership)
			err := ownership.Validate()
			if tt.errMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestResourceShare_Validate(t *testing.T) {
	valid := func() *ResourceShare {
		return &ResourceShare{
			ID:           "share1",
			TenantID:     "tenant1",
			ResourceType: "database",
			ResourceID:   "db1",
			OwnerID:      "user1",
			GranteeType:  GranteeTypeUser,
			GranteeID:    "user2",
			Permissions:  []SharePermission{PermissionRead},
			CreatedBy:    "user1",
		}
	}

	tests := []struct {
		name   string
		mutate func(s *ResourceShare)
		errMsg string
	}{
		{name: "valid", mutate: func(s *ResourceShare) {}},
		{name: "all fields empty", mutate: func(s *ResourceShare) { *s = ResourceShare{} }, errMsg: "share ID"},
		{name: "missing resource type", mutate: func(s *ResourceShare) { s.ResourceType = "" }, errMsg: "resource type"},
		{name: "missing grantee ID", mutate: func(s *ResourceShare) { s.GranteeID = "" }, errMsg: "grantee ID"},
		{name: "grantee is owner", mutate: func(s *ResourceShare) { s.GranteeID = s.OwnerID }, errMsg: "cannot share"},
		{name: "empty permissions", mutate: func(s *ResourceShare) { s.Permissions = []SharePermission{} }, errMsg: "at least one permission"},
		{name: "invalid permission in list", mutate: func(s *ResourceShare) { s.Permissions = []SharePermission{PermissionRead, "badperm"} }, errMsg: "invalid permission"},
		{name: "missing created by", mutate: func(s *ResourceShare) { s.CreatedBy = "" }, errMsg: "created by"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			share := valid()
			tt.mutate(share)
			err := share.Validate()
			if tt.errMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestOwnershipCheckRequest_Validate(t *testing.T) {
	valid := func() *OwnershipCheckRequest {
		return &OwnershipCheckRequest{
			TenantID:           "tenant1",
			ResourceType:       "database",
			ResourceID:         "db1",
			UserID:             "user1",
			RequiredPermission: PermissionRead,
		}
	}

	tests := []struct {
		name   string
		mutate func(r *OwnershipCheckRequest)
		errMsg string
	}{
		{name: "valid", mutate: func(r *OwnershipCheckRequest) {}},
		{name: "missing tenant ID", mutate: func(r *OwnershipCheckRequest) { r.TenantID = "" }, errMsg: "tenant ID"},
		{name: "missing resource type", mutate: func(r *OwnershipCheckRequest) { r.ResourceType = "" }, errMsg: "resource type"},
		{name: "missing resource ID", mutate: func(r *OwnershipCheckRequest) { r.ResourceID = "" }, errMsg: "resource ID"},
		{name: "missing user ID", mutate: func(r *OwnershipCheckRequest) { r.UserID = "" }, errMsg: "user ID"},
		{name: "invalid permission", mutate: func(r *OwnershipCheckRequest) { r.RequiredPermission = "invalid" }, errMsg: "invalid permission"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid()
			tt.mutate(req)
			err := req.Validate()
			if tt.errMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestCreateShareRequest_Validate(t *testing.T) {
	valid := func() *CreateShareRequest {
		return &CreateShareRequest{
			TenantID:     "tenant1",
			ResourceType: "database",
			ResourceID:   "db1",
			GranteeType:  GranteeTypeUser,
			GranteeID:    "user2",
			Permissions:  []SharePermission{PermissionRead},
			CreatedBy:    "user1",
		}
	}

	tests := []struct {
		name   string
		mutate func(r *CreateShareRequest)
		errMsg string
	}{
		{name: "valid", mutate: func(r *CreateShareRequest) {}},
		{name: "missing tenant ID", mutate: func(r *CreateShareRequest) { r.TenantID = "" }, errMsg: "tenant ID"},
		{name: "missing resource type", mutate: func(r *CreateShareRequest) { r.ResourceType = "" }, errMsg: "resource type"},
		{name: "missing resource ID", mutate: func(r *CreateShareRequest) { r.ResourceID = "" }, errMsg: "resource ID"},
		{name: "missing grantee ID", mutate: func(r *CreateShareRequest) { r.GranteeID = "" }, errMsg: "grantee ID"},
		{name: "empty permissions", mutate: func(r *CreateShareRequest) { r.Permissions = []SharePermission{} }, errMsg: "at least one permission"},
		{name: "missing created by", mutate: func(r *CreateShareRequest) { r.CreatedBy = "" }, errMsg: "created by"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := valid()
			tt.mutate(req)
			err := req.Validate()
			if tt.errMsg == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// Helper functions

func createTestOwnershipManager(t *testing.T) *OwnershipManager {
	mgr, err := NewOwnershipManager(OwnershipManagerOptions{
		OwnershipStorage: NewMemoryOwnershipStorage(),
		ShareStorage:     NewMemoryShareStorage(),
	})
	require.NoError(t, err)
	return mgr
}

// expiredTestShare returns a read share of tenant1/database/db1 from user1
// to granteeID that expired an hour ago.
func expiredTestShare(id, granteeID string) *ResourceShare {
	return &ResourceShare{
		ID:           id,
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    granteeID,
		Permissions:  []SharePermission{PermissionRead},
		ExpiresAt:    timePtrOwnership(time.Now().Add(-1 * time.Hour)),
		CreatedBy:    "user1",
	}
}

func timePtrOwnership(t time.Time) *time.Time {
	return &t
}

func TestMemoryOwnershipStorage_ListOwnershipsByOwner(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	// Create multiple ownerships for same owner
	ownership1 := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}
	err := storage.CreateOwnership(ctx, ownership1)
	require.NoError(t, err)

	ownership2 := &ResourceOwnership{
		ID:           "own2",
		TenantID:     "tenant1",
		ResourceType: "table",
		ResourceID:   "table1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}
	err = storage.CreateOwnership(ctx, ownership2)
	require.NoError(t, err)

	// Create ownership for different owner
	ownership3 := &ResourceOwnership{
		ID:           "own3",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db2",
		OwnerID:      "user2",
		CreatedAt:    time.Now(),
	}
	err = storage.CreateOwnership(ctx, ownership3)
	require.NoError(t, err)

	// List ownerships for user1
	ownerships, err := storage.ListOwnershipsByOwner(ctx, "tenant1", "user1")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 2)

	// List ownerships for user2
	ownerships, err = storage.ListOwnershipsByOwner(ctx, "tenant1", "user2")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 1)

	// List ownerships for non-existent user
	ownerships, err = storage.ListOwnershipsByOwner(ctx, "tenant1", "user3")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 0)
}

func TestMemoryOwnershipStorage_DeleteOwnership(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	// Create ownership
	ownership := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}
	err := storage.CreateOwnership(ctx, ownership)
	require.NoError(t, err)

	// Delete ownership
	err = storage.DeleteOwnership(ctx, "tenant1", "database", "db1")
	assert.NoError(t, err)

	// Verify deleted
	_, err = storage.GetOwnership(ctx, "tenant1", "database", "db1")
	assert.Error(t, err)

	// Try to delete again
	err = storage.DeleteOwnership(ctx, "tenant1", "database", "db1")
	assert.Error(t, err)

	// Verify owner index is cleaned
	ownerships, err := storage.ListOwnershipsByOwner(ctx, "tenant1", "user1")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 0)
}

func TestMemoryShareStorage_ListSharesByGrantee(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryShareStorage()

	// Create shares for same grantee
	share1 := &ResourceShare{
		ID:           "share1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "user1",
	}
	err := storage.CreateShare(ctx, share1)
	require.NoError(t, err)

	share2 := &ResourceShare{
		ID:           "share2",
		TenantID:     "tenant1",
		ResourceType: "table",
		ResourceID:   "table1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionWrite},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "user1",
	}
	err = storage.CreateShare(ctx, share2)
	require.NoError(t, err)

	// List shares for user2
	shares, err := storage.ListSharesByGrantee(ctx, "tenant1", GranteeTypeUser, "user2")
	assert.NoError(t, err)
	assert.Len(t, shares, 2)

	// List shares for non-existent grantee
	shares, err = storage.ListSharesByGrantee(ctx, "tenant1", GranteeTypeUser, "user3")
	assert.NoError(t, err)
	assert.Len(t, shares, 0)
}

func TestMemoryShareStorage_ListSharesByOwner(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryShareStorage()

	// Create shares for same owner
	share1 := &ResourceShare{
		ID:           "share1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "user1",
	}
	err := storage.CreateShare(ctx, share1)
	require.NoError(t, err)

	share2 := &ResourceShare{
		ID:           "share2",
		TenantID:     "tenant1",
		ResourceType: "table",
		ResourceID:   "table1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user3",
		Permissions:  []SharePermission{PermissionWrite},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "user1",
	}
	err = storage.CreateShare(ctx, share2)
	require.NoError(t, err)

	// Create share for different owner
	share3 := &ResourceShare{
		ID:           "share3",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db2",
		OwnerID:      "user2",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user3",
		Permissions:  []SharePermission{PermissionRead},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "user2",
	}
	err = storage.CreateShare(ctx, share3)
	require.NoError(t, err)

	// List shares for user1
	shares, err := storage.ListSharesByOwner(ctx, "tenant1", "user1")
	assert.NoError(t, err)
	assert.Len(t, shares, 2)

	// List shares for user2
	shares, err = storage.ListSharesByOwner(ctx, "tenant1", "user2")
	assert.NoError(t, err)
	assert.Len(t, shares, 1)

	// List shares for non-existent owner
	shares, err = storage.ListSharesByOwner(ctx, "tenant1", "user99")
	assert.NoError(t, err)
	assert.Len(t, shares, 0)
}

func TestMemoryOwnershipStorage_CreateDuplicate(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	ownership := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}

	// Create first time
	err := storage.CreateOwnership(ctx, ownership)
	require.NoError(t, err)

	// Try to create duplicate
	err = storage.CreateOwnership(ctx, ownership)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestMemoryShareStorage_CreateDuplicate(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryShareStorage()

	share := &ResourceShare{
		ID:           "share1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "user1",
	}

	// Create first time
	err := storage.CreateShare(ctx, share)
	require.NoError(t, err)

	// Try to create duplicate
	err = storage.CreateShare(ctx, share)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestMemoryOwnershipStorage_MissingRecords(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	tests := []struct {
		name    string
		call    func() (int, error)
		wantErr string
	}{
		{
			name: "get not found",
			call: func() (int, error) {
				_, err := storage.GetOwnership(ctx, "tenant1", "database", "nonexistent")
				return 0, err
			},
			wantErr: "not found",
		},
		{
			name: "transfer not found",
			call: func() (int, error) {
				return 0, storage.TransferOwnership(ctx, "tenant1", "database", "nonexistent", "user2")
			},
			wantErr: "not found",
		},
		{
			name: "list by owner empty",
			call: func() (int, error) {
				ownerships, err := storage.ListOwnershipsByOwner(ctx, "tenant1", "nonexistent")
				return len(ownerships), err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.call()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Zero(t, count)
		})
	}
}

func TestMemoryShareStorage_MissingRecords(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryShareStorage()

	tests := []struct {
		name    string
		call    func() (int, error)
		wantErr string
	}{
		{
			name: "get not found",
			call: func() (int, error) {
				_, err := storage.GetShare(ctx, "tenant1", "nonexistent")
				return 0, err
			},
			wantErr: "not found",
		},
		{
			name: "update not found",
			call: func() (int, error) {
				return 0, storage.UpdateShare(ctx, &ResourceShare{
					ID:           "share1",
					TenantID:     "tenant1",
					ResourceType: "database",
					ResourceID:   "db1",
					OwnerID:      "user1",
					GranteeType:  GranteeTypeUser,
					GranteeID:    "user2",
					Permissions:  []SharePermission{PermissionRead},
					CreatedBy:    "user1",
				})
			},
			wantErr: "not found",
		},
		{
			name: "delete not found",
			call: func() (int, error) {
				return 0, storage.DeleteShare(ctx, "tenant1", "nonexistent")
			},
			wantErr: "not found",
		},
		{
			name: "list by resource empty",
			call: func() (int, error) {
				shares, err := storage.ListSharesByResource(ctx, "tenant1", "database", "nonexistent")
				return len(shares), err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.call()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Zero(t, count)
		})
	}
}

func TestOwnershipManager_CheckAccess_MultipleGroupShares(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and shares for multiple groups
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	// Share with group1 (read)
	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeGroup,
		GranteeID:    "group1",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// Share with group2 (write)
	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeGroup,
		GranteeID:    "group2",
		Permissions:  []SharePermission{PermissionWrite},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// User in both groups should have both permissions
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionRead,
		UserGroups:         []string{"group1", "group2"},
	})
	assert.NoError(t, err)
	assert.True(t, resp.Allowed)

	resp, err = mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionWrite,
		UserGroups:         []string{"group1", "group2"},
	})
	assert.NoError(t, err)
	assert.True(t, resp.Allowed)
}

func TestMemoryShareStorage_UpdatePreservesCreatedAt(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryShareStorage()

	// Create a share
	originalCreated := time.Now().Add(-24 * time.Hour)
	share := &ResourceShare{
		ID:           "share1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedAt:    originalCreated,
		UpdatedAt:    originalCreated,
		CreatedBy:    "user1",
	}
	err := storage.CreateShare(ctx, share)
	require.NoError(t, err)

	// Update the share
	share.Permissions = []SharePermission{PermissionRead, PermissionWrite}
	err = storage.UpdateShare(ctx, share)
	assert.NoError(t, err)

	// Verify CreatedAt is preserved and UpdatedAt is changed
	updated, err := storage.GetShare(ctx, "tenant1", "share1")
	require.NoError(t, err)
	assert.Equal(t, originalCreated.Unix(), updated.CreatedAt.Unix())
	assert.True(t, updated.UpdatedAt.After(originalCreated))
}

func TestOwnershipManager_CheckAccess_WrongResource(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership and share for db1
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	require.NoError(t, err)

	// Check access for different resource
	resp, err := mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db2", // Different resource
		UserID:             "user2",
		RequiredPermission: PermissionRead,
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Allowed)
}

func TestMemoryOwnershipStorage_DeleteIndexCleanup(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	// Create multiple ownerships for same user
	ownership1 := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}
	err := storage.CreateOwnership(ctx, ownership1)
	require.NoError(t, err)

	ownership2 := &ResourceOwnership{
		ID:           "own2",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db2",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}
	err = storage.CreateOwnership(ctx, ownership2)
	require.NoError(t, err)

	// Delete one
	err = storage.DeleteOwnership(ctx, "tenant1", "database", "db1")
	assert.NoError(t, err)

	// Verify other ownership still exists
	ownerships, err := storage.ListOwnershipsByOwner(ctx, "tenant1", "user1")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 1)
	assert.Equal(t, "db2", ownerships[0].ResourceID)

	// Delete the last one
	err = storage.DeleteOwnership(ctx, "tenant1", "database", "db2")
	assert.NoError(t, err)

	// Verify index is cleaned
	ownerships, err = storage.ListOwnershipsByOwner(ctx, "tenant1", "user1")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 0)
}

func TestMemoryOwnershipStorage_TransferIndexUpdate(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	// Create ownership
	ownership := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}
	err := storage.CreateOwnership(ctx, ownership)
	require.NoError(t, err)

	// Transfer
	err = storage.TransferOwnership(ctx, "tenant1", "database", "db1", "user2")
	assert.NoError(t, err)

	// Verify old owner index is cleaned
	ownerships, err := storage.ListOwnershipsByOwner(ctx, "tenant1", "user1")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 0)

	// Verify new owner index is updated
	ownerships, err = storage.ListOwnershipsByOwner(ctx, "tenant1", "user2")
	assert.NoError(t, err)
	assert.Len(t, ownerships, 1)
}

func TestMemoryShareStorage_AllIndexesCleanedOnDelete(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryShareStorage()

	// Create share
	share := &ResourceShare{
		ID:           "share1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		CreatedBy:    "user1",
	}
	err := storage.CreateShare(ctx, share)
	require.NoError(t, err)

	// Verify all indexes are populated
	resourceShares, err := storage.ListSharesByResource(ctx, "tenant1", "database", "db1")
	assert.Len(t, resourceShares, 1)

	granteeShares, err := storage.ListSharesByGrantee(ctx, "tenant1", GranteeTypeUser, "user2")
	assert.Len(t, granteeShares, 1)

	ownerShares, err := storage.ListSharesByOwner(ctx, "tenant1", "user1")
	assert.Len(t, ownerShares, 1)

	// Delete share
	err = storage.DeleteShare(ctx, "tenant1", "share1")
	assert.NoError(t, err)

	// Verify all indexes are cleaned
	resourceShares, err = storage.ListSharesByResource(ctx, "tenant1", "database", "db1")
	assert.Len(t, resourceShares, 0)

	granteeShares, err = storage.ListSharesByGrantee(ctx, "tenant1", GranteeTypeUser, "user2")
	assert.Len(t, granteeShares, 0)

	ownerShares, err = storage.ListSharesByOwner(ctx, "tenant1", "user1")
	assert.Len(t, ownerShares, 0)
}

func TestMemoryOwnershipStorage_CreateWithAutoTimestamp(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	// Create ownership without timestamp
	ownership := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		// CreatedAt is zero
	}

	err := storage.CreateOwnership(ctx, ownership)
	assert.NoError(t, err)

	// Verify timestamp was set
	retrieved, err := storage.GetOwnership(ctx, "tenant1", "database", "db1")
	require.NoError(t, err)
	assert.False(t, retrieved.CreatedAt.IsZero())
}

func TestMemoryShareStorage_CreateWithAutoTimestamp(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryShareStorage()

	// Create share without timestamp
	share := &ResourceShare{
		ID:           "share1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
		// CreatedAt and UpdatedAt are zero
	}

	err := storage.CreateShare(ctx, share)
	assert.NoError(t, err)

	// Verify timestamps were set
	retrieved, err := storage.GetShare(ctx, "tenant1", "share1")
	require.NoError(t, err)
	assert.False(t, retrieved.CreatedAt.IsZero())
	assert.False(t, retrieved.UpdatedAt.IsZero())
}

func TestOwnershipManager_ShareResource_PreventSelfShare(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	// Try to share with self (via group should be allowed, only user is blocked)
	_, err = mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeGroup,
		GranteeID:    "user1", // Same as owner, but group type
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
	})
	// This should succeed because grantee type is group
	assert.NoError(t, err)
}

func TestResourceShare_WithMetadata(t *testing.T) {
	ctx := context.Background()
	mgr := createTestOwnershipManager(t)

	// Setup: Create ownership
	_, err := mgr.SetOwner(ctx, "tenant1", "database", "db1", "user1")
	require.NoError(t, err)

	// Create share with metadata
	metadata := map[string]interface{}{
		"reason": "collaboration",
		"team":   "engineering",
	}

	share, err := mgr.ShareResource(ctx, &CreateShareRequest{
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		GranteeType:  GranteeTypeUser,
		GranteeID:    "user2",
		Permissions:  []SharePermission{PermissionRead},
		CreatedBy:    "user1",
		Metadata:     metadata,
	})

	assert.NoError(t, err)
	require.NotNil(t, share)
	assert.Equal(t, metadata, share.Metadata)
}

func TestResourceOwnership_WithMetadata(t *testing.T) {
	ctx := context.Background()
	storage := NewMemoryOwnershipStorage()

	// Create ownership with metadata
	ownership := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
		Metadata: map[string]interface{}{
			"project": "alpha",
			"cost":    1000,
		},
	}

	err := storage.CreateOwnership(ctx, ownership)
	assert.NoError(t, err)

	// Verify metadata is preserved
	retrieved, err := storage.GetOwnership(ctx, "tenant1", "database", "db1")
	require.NoError(t, err)
	assert.NotNil(t, retrieved.Metadata)
	assert.Equal(t, "alpha", retrieved.Metadata["project"])
}

func TestOwnershipManager_CheckAccess_ErrorRetrievingShares(t *testing.T) {
	ctx := context.Background()

	// Create a manager with mock storage that will fail on GetActiveSharesForUser
	mgr := &OwnershipManager{
		ownershipStore: NewMemoryOwnershipStorage(),
		shareStore:     &failingShareStorage{},
	}

	// Create ownership
	ownership := &ResourceOwnership{
		ID:           "own1",
		TenantID:     "tenant1",
		ResourceType: "database",
		ResourceID:   "db1",
		OwnerID:      "user1",
		CreatedAt:    time.Now(),
	}
	err := mgr.ownershipStore.CreateOwnership(ctx, ownership)
	require.NoError(t, err)

	// Check access should fail when retrieving shares
	_, err = mgr.CheckAccess(ctx, &OwnershipCheckRequest{
		TenantID:           "tenant1",
		ResourceType:       "database",
		ResourceID:         "db1",
		UserID:             "user2",
		RequiredPermission: PermissionRead,
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to retrieve shares")
}

// failingShareStorage is a mock that fails on GetActiveSharesForUser
type failingShareStorage struct{}

func (f *failingShareStorage) CreateShare(ctx context.Context, share *ResourceShare) error {
	return nil
}

func (f *failingShareStorage) GetShare(ctx context.Context, tenantID, shareID string) (*ResourceShare, error) {
	return nil, &dberrors.MessageError{Message: "mock error"}
}

func (f *failingShareStorage) UpdateShare(ctx context.Context, share *ResourceShare) error {
	return nil
}

func (f *failingShareStorage) DeleteShare(ctx context.Context, tenantID, shareID string) error {
	return nil
}

func (f *failingShareStorage) ListSharesByResource(ctx context.Context, tenantID, resourceType, resourceID string) ([]*ResourceShare, error) {
	return nil, nil
}

func (f *failingShareStorage) ListSharesByGrantee(ctx context.Context, tenantID string, granteeType GranteeType, granteeID string) ([]*ResourceShare, error) {
	return nil, nil
}

func (f *failingShareStorage) ListSharesByOwner(ctx context.Context, tenantID, ownerID string) ([]*ResourceShare, error) {
	return nil, nil
}

func (f *failingShareStorage) GetActiveSharesForUser(ctx context.Context, tenantID, userID string, userGroups []string) ([]*ResourceShare, error) {
	return nil, &dberrors.MessageError{Message: "mock error retrieving shares"}
}

func (f *failingShareStorage) DeleteExpiredShares(ctx context.Context, tenantID string) (int, error) {
	return 0, nil
}
