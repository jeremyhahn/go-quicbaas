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
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ExportAll exports all IAM resources for a tenant.
func (bm *BatchManager) ExportAll(ctx context.Context, req *BatchExportRequest) (*BatchExportResult, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	startTime := time.Now()

	result := &BatchExportResult{
		TenantID: req.TenantID,
	}

	// Export users
	if req.IncludeUsers {
		users, err := bm.ExportUsers(ctx, req.TenantID, req.Filter)
		if err != nil {
			return nil, err
		}
		result.Users = users
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrBatchCanceled
	}

	// Export groups
	if req.IncludeGroups {
		groups, err := bm.ExportGroups(ctx, req.TenantID, req.Filter)
		if err != nil {
			return nil, err
		}
		result.Groups = groups
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrBatchCanceled
	}

	// Export roles
	if req.IncludeRoles {
		roles, err := bm.ExportRoles(ctx, req.TenantID, req.Filter)
		if err != nil {
			return nil, err
		}
		result.Roles = roles
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return nil, ErrBatchCanceled
	}

	// Export policies
	if req.IncludePolicies {
		policies, err := bm.ExportPolicies(ctx, req.TenantID, req.Filter)
		if err != nil {
			return nil, err
		}
		result.Policies = policies
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// ExportUsers exports users from a tenant with optional filtering.
func (bm *BatchManager) ExportUsers(ctx context.Context, tenantID string, filter ExportFilter) ([]*User, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	if bm.identityStore == nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.exportUsers",
			Err:  errBatchOperationFailed,
		}
	}

	// Build list options
	listOpts := ListOptions{
		Limit: 1000, // Export in batches
	}

	var allUsers []*User
	offset := 0

	for {
		// Check context
		if err := ctx.Err(); err != nil {
			return nil, ErrBatchCanceled
		}

		listOpts.Offset = offset
		users, err := bm.identityStore.ListUsers(ctx, tenantID, listOpts)
		if err != nil {
			return nil, err
		}

		if len(users) == 0 {
			break
		}

		// Apply filters
		for _, user := range users {
			if bm.userMatchesFilter(user, filter) {
				allUsers = append(allUsers, user)
			}
		}

		if len(users) < listOpts.Limit {
			break
		}

		offset += listOpts.Limit
	}

	return allUsers, nil
}

// ExportGroups exports groups from a tenant with optional filtering.
func (bm *BatchManager) ExportGroups(ctx context.Context, tenantID string, filter ExportFilter) ([]*Group, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	if bm.identityStore == nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.exportGroups",
			Err:  errBatchOperationFailed,
		}
	}

	// Build list options
	listOpts := ListOptions{
		Limit: 1000, // Export in batches
	}

	var allGroups []*Group
	offset := 0

	for {
		// Check context
		if err := ctx.Err(); err != nil {
			return nil, ErrBatchCanceled
		}

		listOpts.Offset = offset
		groups, err := bm.identityStore.ListGroups(ctx, tenantID, listOpts)
		if err != nil {
			return nil, err
		}

		if len(groups) == 0 {
			break
		}

		// Apply filters
		for _, group := range groups {
			if bm.groupMatchesFilter(group, filter) {
				allGroups = append(allGroups, group)
			}
		}

		if len(groups) < listOpts.Limit {
			break
		}

		offset += listOpts.Limit
	}

	return allGroups, nil
}

// ExportRoles exports roles from a tenant with optional filtering.
func (bm *BatchManager) ExportRoles(ctx context.Context, tenantID string, filter ExportFilter) ([]*Role, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	if bm.identityStore == nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.exportRoles",
			Err:  errBatchOperationFailed,
		}
	}

	// Build list options
	listOpts := ListOptions{
		Limit: 1000, // Export in batches
	}

	var allRoles []*Role
	offset := 0

	for {
		// Check context
		if err := ctx.Err(); err != nil {
			return nil, ErrBatchCanceled
		}

		listOpts.Offset = offset
		roles, err := bm.identityStore.ListRoles(ctx, tenantID, listOpts)
		if err != nil {
			return nil, err
		}

		if len(roles) == 0 {
			break
		}

		// Apply filters
		for _, role := range roles {
			if bm.roleMatchesFilter(role, filter) {
				allRoles = append(allRoles, role)
			}
		}

		if len(roles) < listOpts.Limit {
			break
		}

		offset += listOpts.Limit
	}

	return allRoles, nil
}

// ExportPolicies exports policies from a tenant with optional filtering.
func (bm *BatchManager) ExportPolicies(ctx context.Context, tenantID string, filter ExportFilter) ([]*Policy, error) {
	if tenantID == "" {
		return nil, &ValidationError{Field: "tenant_id", Message: "tenant ID is required"}
	}

	if bm.policyEngine == nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "iam.batch.exportPolicies",
			Err:  errBatchOperationFailed,
		}
	}

	// Build list options
	listOpts := ListOptions{
		Limit: 1000, // Export in batches
	}

	var allPolicies []*Policy
	offset := 0

	for {
		// Check context
		if err := ctx.Err(); err != nil {
			return nil, ErrBatchCanceled
		}

		listOpts.Offset = offset
		policies, err := bm.policyEngine.ListPolicies(ctx, tenantID, listOpts)
		if err != nil {
			return nil, err
		}

		if len(policies) == 0 {
			break
		}

		// Apply filters
		for _, policy := range policies {
			if bm.policyMatchesFilter(policy, filter) {
				allPolicies = append(allPolicies, policy)
			}
		}

		if len(policies) < listOpts.Limit {
			break
		}

		offset += listOpts.Limit
	}

	return allPolicies, nil
}

// userMatchesFilter checks if a user matches the export filter.
func (bm *BatchManager) userMatchesFilter(user *User, filter ExportFilter) bool {
	// Check ID filter
	if len(filter.UserIDs) > 0 {
		found := false
		for _, id := range filter.UserIDs {
			if user.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check created time filters
	if filter.CreatedAfter != nil && user.Created.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && user.Created.After(*filter.CreatedBefore) {
		return false
	}

	// Check updated time filters
	if filter.UpdatedAfter != nil && user.Updated.Before(*filter.UpdatedAfter) {
		return false
	}
	if filter.UpdatedBefore != nil && user.Updated.After(*filter.UpdatedBefore) {
		return false
	}

	return true
}

// groupMatchesFilter checks if a group matches the export filter.
func (bm *BatchManager) groupMatchesFilter(group *Group, filter ExportFilter) bool {
	// Check ID filter
	if len(filter.GroupIDs) > 0 {
		found := false
		for _, id := range filter.GroupIDs {
			if group.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check created time filters
	if filter.CreatedAfter != nil && group.Created.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && group.Created.After(*filter.CreatedBefore) {
		return false
	}

	// Check updated time filters
	if filter.UpdatedAfter != nil && group.Updated.Before(*filter.UpdatedAfter) {
		return false
	}
	if filter.UpdatedBefore != nil && group.Updated.After(*filter.UpdatedBefore) {
		return false
	}

	return true
}

// roleMatchesFilter checks if a role matches the export filter.
func (bm *BatchManager) roleMatchesFilter(role *Role, filter ExportFilter) bool {
	// Check ID filter
	if len(filter.RoleIDs) > 0 {
		found := false
		for _, id := range filter.RoleIDs {
			if role.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check created time filters
	if filter.CreatedAfter != nil && role.Created.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && role.Created.After(*filter.CreatedBefore) {
		return false
	}

	// Check updated time filters
	if filter.UpdatedAfter != nil && role.Updated.Before(*filter.UpdatedAfter) {
		return false
	}
	if filter.UpdatedBefore != nil && role.Updated.After(*filter.UpdatedBefore) {
		return false
	}

	return true
}

// policyMatchesFilter checks if a policy matches the export filter.
func (bm *BatchManager) policyMatchesFilter(policy *Policy, filter ExportFilter) bool {
	// Check name filter
	if len(filter.PolicyNames) > 0 {
		found := false
		for _, name := range filter.PolicyNames {
			if policy.Metadata.Name == name {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check created time filters
	if filter.CreatedAfter != nil && policy.Metadata.Created.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && policy.Metadata.Created.After(*filter.CreatedBefore) {
		return false
	}

	// Check updated time filters
	if filter.UpdatedAfter != nil && policy.Metadata.Updated.Before(*filter.UpdatedAfter) {
		return false
	}
	if filter.UpdatedBefore != nil && policy.Metadata.Updated.After(*filter.UpdatedBefore) {
		return false
	}

	return true
}

// ConvertToImportRequest converts an export result to an import request.
// This is useful for migrating data between tenants or environments.
func (result *BatchExportResult) ConvertToImportRequest(targetTenantID string) *BatchImportRequest {
	req := &BatchImportRequest{
		TenantID: targetTenantID,
	}

	// Convert users
	if len(result.Users) > 0 {
		req.Users = make([]*UserImport, 0, len(result.Users))
		for _, user := range result.Users {
			req.Users = append(req.Users, &UserImport{
				ID:         user.ID,
				Email:      user.Email,
				Username:   user.Name, // Name is used as username in this context
				Name:       user.Name,
				Attributes: user.Attributes,
				Groups:     user.Groups,
				Roles:      user.Roles,
				Enabled:    !user.Suspended && !user.Locked,
			})
		}
	}

	// Convert groups
	if len(result.Groups) > 0 {
		req.Groups = make([]*GroupImport, 0, len(result.Groups))
		for _, group := range result.Groups {
			req.Groups = append(req.Groups, &GroupImport{
				ID:          group.ID,
				Name:        group.Name,
				Description: group.Description,
				Members:     group.Members,
			})
		}
	}

	// Convert roles
	if len(result.Roles) > 0 {
		req.Roles = make([]*RoleImport, 0, len(result.Roles))
		for _, role := range result.Roles {
			req.Roles = append(req.Roles, &RoleImport{
				ID:          role.ID,
				Name:        role.Name,
				Description: role.Description,
				Permissions: role.Permissions,
			})
		}
	}

	// Convert policies
	if len(result.Policies) > 0 {
		req.Policies = make([]*PolicyImport, 0, len(result.Policies))
		for _, policy := range result.Policies {
			req.Policies = append(req.Policies, &PolicyImport{
				Name:        policy.Metadata.Name,
				Description: policy.Metadata.Description,
				Priority:    policy.Metadata.Priority,
				Subjects:    policy.Spec.Subjects,
				Rules:       policy.Spec.Rules,
				Variables:   policy.Spec.Variables,
			})
		}
	}

	return req
}
