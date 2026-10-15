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

package grpc

import (
	"context"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// handleCreateUser handles CreateUser requests.
func (h *iamHandler) handleCreateUser(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCreateUser", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	email, _ := getString(requestData, "email")
	username, _ := getString(requestData, "username")
	displayName, _ := getString(requestData, "display_name")
	roles, _ := getStringSlice(requestData, "roles")
	metadata, _ := getStringMap(requestData, "metadata")
	authMethods, _ := getStringSlice(requestData, "auth_methods")
	lifecycle, _ := getString(requestData, "lifecycle")
	inviteExpiryHours, _ := getInt(requestData, "invite_expiry_hours")

	pbReq := &iampb.CreateUserRequest{
		TenantId:          tenantID,
		Email:             email,
		Username:          username,
		Name:              displayName,
		Roles:             roles,
		Attributes:        metadata,
		AuthMethods:       authMethods,
		Lifecycle:         lifecycle,
		InviteExpiryHours: int32(inviteExpiryHours),
	}

	// Use the handle as the display name when none was given. The handle
	// itself now travels in its own field; folding it into Name and sending
	// nothing else is what used to discard it, so a create of ("alice",
	// "Alice Smith") stored the display name as the handle and could not be
	// found as "alice".
	if pbReq.Name == "" {
		pbReq.Name = username
	}

	resp, err := client.CreateUser(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCreateUser")
	}

	return convertProtoUserToSDK(resp), nil
}

// handleGetUser handles GetUser requests.
func (h *iamHandler) handleGetUser(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetUser", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.GetUserRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	resp, err := client.GetUser(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetUser")
	}

	return convertProtoUserToSDK(resp), nil
}

// handleUpdateUser handles UpdateUser requests.
func (h *iamHandler) handleUpdateUser(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUpdateUser", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	requestData, _ := getMap(reqMap, "request")

	email, _ := getString(requestData, "email")
	username, _ := getString(requestData, "username")
	displayName, _ := getString(requestData, "display_name")
	roles, _ := getStringSlice(requestData, "roles")
	metadata, _ := getStringMap(requestData, "metadata")
	lifecycle, _ := getString(requestData, "lifecycle")

	pbReq := &iampb.UpdateUserRequest{
		TenantId:   tenantID,
		UserId:     userID,
		Email:      email,
		Username:   username,
		Name:       displayName,
		Roles:      roles,
		Attributes: metadata,
		Lifecycle:  lifecycle,
	}
	// The wrapper is sent only when the caller supplied the field, so an
	// explicit empty list reaches the server as a clear rather than as silence.
	if raw, present := requestData["auth_methods"]; present && raw != nil {
		values, _ := getStringSlice(requestData, "auth_methods")
		if values == nil {
			values = []string{}
		}
		pbReq.AuthMethods = &iampb.AuthMethodsUpdate{Values: values}
	}

	resp, err := client.UpdateUser(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUpdateUser")
	}

	return convertProtoUserToSDK(resp), nil
}

// handleDeleteUser handles DeleteUser requests.
func (h *iamHandler) handleDeleteUser(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteUser", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")

	pbReq := &iampb.DeleteUserRequest{
		TenantId: tenantID,
		UserId:   userID,
	}

	_, err := client.DeleteUser(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteUser")
	}

	// Return nil to indicate success.
	return nil, nil
}

// handleListUsers handles ListUsers requests.
func (h *iamHandler) handleListUsers(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListUsers", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	pageSize, _ := getInt(requestData, "page_size")
	roleID, _ := getString(requestData, "role_id")

	pbReq := &iampb.ListUsersRequest{
		TenantId: tenantID,
		Limit:    int32(pageSize),
		Role:     roleID,
	}

	resp, err := client.ListUsers(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListUsers")
	}

	users := make([]iam.User, 0, len(resp.Users))
	for _, u := range resp.Users {
		if converted := convertProtoUserToSDK(u); converted != nil {
			users = append(users, *converted)
		}
	}

	return &iam.ListUsersResponse{
		Users: users,
		Pagination: iam.PaginationInfo{
			TotalCount: int(resp.Total),
		},
	}, nil
}

// handleAssignRole handles AssignRole requests.
func (h *iamHandler) handleAssignRole(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleAssignRole", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	roleID, _ := getString(reqMap, "role_id")

	pbReq := &iampb.AssignRoleRequest{
		TenantId: tenantID,
		UserId:   userID,
		Role:     roleID,
	}

	_, err := client.AssignRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleAssignRole")
	}

	return nil, nil
}

// handleRevokeRole handles RevokeRole requests.
func (h *iamHandler) handleRevokeRole(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleRevokeRole", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	userID, _ := getString(reqMap, "user_id")
	roleID, _ := getString(reqMap, "role_id")

	pbReq := &iampb.RevokeRoleRequest{
		TenantId: tenantID,
		UserId:   userID,
		Role:     roleID,
	}

	_, err := client.RevokeRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleRevokeRole")
	}

	return nil, nil
}

// handleCreateTenant handles CreateTenant requests.
func (h *iamHandler) handleCreateTenant(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCreateTenant", "invalid request type")
	}

	id, _ := getString(reqMap, "id")
	name, _ := getString(reqMap, "name")
	settings, _ := getStringMap(reqMap, "settings")
	isolationType, _ := getString(reqMap, "isolation_type")

	engineQuotas, err := engineQuotasToProto(reqMap["engine_quotas"])
	if err != nil {
		return nil, err
	}
	engineEntitlements, err := engineEntitlementsToProto(reqMap["engine_entitlements"])
	if err != nil {
		return nil, err
	}

	pbReq := &iampb.CreateTenantRequest{
		Id:                 id,
		Name:               name,
		IsolationType:      isolationType,
		EngineQuotas:       engineQuotas,
		EngineEntitlements: engineEntitlements,
	}

	if len(settings) > 0 {
		pbReq.Settings = &iampb.TenantSettings{
			NamedNetworks: settings,
		}
	}

	resp, err := client.CreateTenant(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCreateTenant")
	}

	return convertProtoTenantToSDK(resp), nil
}

// handleGetTenant handles GetTenant requests.
func (h *iamHandler) handleGetTenant(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetTenant", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")

	pbReq := &iampb.GetTenantRequest{
		Id: tenantID,
	}

	resp, err := client.GetTenant(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetTenant")
	}

	return convertProtoTenantToSDK(resp), nil
}

// handleUpdateTenant handles UpdateTenant requests.
func (h *iamHandler) handleUpdateTenant(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUpdateTenant", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	name, _ := getString(requestData, "display_name")
	settings, _ := getStringMap(requestData, "settings")

	pbReq := &iampb.UpdateTenantRequest{
		Id:   tenantID,
		Name: name,
	}

	if len(settings) > 0 {
		pbReq.Settings = &iampb.TenantSettings{
			NamedNetworks: settings,
		}
	}

	resp, err := client.UpdateTenant(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUpdateTenant")
	}

	return convertProtoTenantToSDK(resp), nil
}

// handleDeleteTenant handles DeleteTenant requests.
func (h *iamHandler) handleDeleteTenant(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteTenant", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")

	pbReq := &iampb.DeleteTenantRequest{
		Id: tenantID,
	}

	_, err := client.DeleteTenant(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteTenant")
	}

	return nil, nil
}

func (h *iamHandler) handleProvisionTenant(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleProvisionTenant", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	if tenantID == "" {
		return nil, errors.NewInvalidInput("handleProvisionTenant", "tenant_id is required")
	}
	name, _ := getString(reqMap, "name")
	isolationType, _ := getString(reqMap, "isolation_type")
	adminUsername, _ := getString(reqMap, "admin_username")
	adminEnrollment, _ := getString(reqMap, "admin_enrollment")
	adminCSRPEM, _ := getString(reqMap, "admin_csr_pem")
	inviteExpiryHours, _ := getInt(reqMap, "invite_expiry_hours")

	resp, err := client.ProvisionTenant(ctx, &iampb.ProvisionTenantRequest{
		TenantId:          tenantID,
		Name:              name,
		IsolationType:     isolationType,
		AdminUsername:     adminUsername,
		AdminEnrollment:   adminEnrollment,
		AdminCsrPem:       adminCSRPEM,
		InviteExpiryHours: int32(inviteExpiryHours),
	})
	if err != nil {
		return nil, convertGRPCError(err, "handleProvisionTenant")
	}

	out := &iam.ProvisionTenantResponse{
		TenantID:            resp.TenantId,
		AdminUserID:         resp.AdminUserId,
		AdminUsername:       resp.AdminUsername,
		AdminRoleID:         resp.AdminRoleId,
		AdminPending:        resp.AdminPending,
		AdminCertPEM:        resp.AdminCertPem,
		AdminCertThumbprint: resp.AdminCertThumbprint,
		InviteToken:         resp.InviteToken,
	}
	if resp.InviteExpiresAt != nil {
		expiresAt := resp.InviteExpiresAt.AsTime()
		out.InviteExpiresAt = &expiresAt
	}
	if resp.Timestamp != nil {
		out.Timestamp = resp.Timestamp.AsTime()
	}
	return out, nil
}

// handleListTenants handles ListTenants requests.
func (h *iamHandler) handleListTenants(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListTenants", "invalid request type")
	}

	pageSize, _ := getInt(reqMap, "page_size")

	pbReq := &iampb.ListTenantsRequest{
		Limit: int32(pageSize),
	}

	resp, err := client.ListTenants(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListTenants")
	}

	tenants := make([]iam.Tenant, 0, len(resp.Tenants))
	for _, t := range resp.Tenants {
		if converted := convertProtoTenantToSDK(t); converted != nil {
			tenants = append(tenants, *converted)
		}
	}

	return &iam.ListTenantsResponse{
		Tenants: tenants,
		Pagination: iam.PaginationInfo{
			TotalCount: int(resp.Total),
		},
	}, nil
}

// handleCreateRole handles CreateRole requests.
func (h *iamHandler) handleCreateRole(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCreateRole", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	id, _ := getString(requestData, "id")
	name, _ := getString(requestData, "name")
	description, _ := getString(requestData, "description")
	permissions, _ := getStringSlice(requestData, "permissions")

	pbReq := &iampb.CreateRoleRequest{
		TenantId:    tenantID,
		Id:          id,
		Name:        name,
		Description: description,
		Permissions: permissions,
	}

	resp, err := client.CreateRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCreateRole")
	}

	return convertProtoRoleToSDK(resp), nil
}

// handleGetRole handles GetRole requests.
func (h *iamHandler) handleGetRole(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetRole", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	roleID, _ := getString(reqMap, "role_id")

	pbReq := &iampb.GetRoleRequest{
		TenantId: tenantID,
		RoleId:   roleID,
	}

	resp, err := client.GetRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetRole")
	}

	return convertProtoRoleToSDK(resp), nil
}

// handleUpdateRole handles UpdateRole requests.
func (h *iamHandler) handleUpdateRole(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUpdateRole", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	roleID, _ := getString(reqMap, "role_id")
	requestData, _ := getMap(reqMap, "request")

	name, _ := getString(requestData, "name")
	if name == "" {
		name, _ = getString(requestData, "display_name")
	}
	description, _ := getString(requestData, "description")
	permissions, _ := getStringSlice(requestData, "permissions")

	pbReq := &iampb.UpdateRoleRequest{
		TenantId:    tenantID,
		RoleId:      roleID,
		Name:        name,
		Description: description,
		Permissions: permissions,
	}

	resp, err := client.UpdateRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUpdateRole")
	}

	return convertProtoRoleToSDK(resp), nil
}

// handleDeleteRole handles DeleteRole requests.
func (h *iamHandler) handleDeleteRole(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteRole", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	roleID, _ := getString(reqMap, "role_id")

	pbReq := &iampb.DeleteRoleRequest{
		TenantId: tenantID,
		RoleId:   roleID,
	}

	_, err := client.DeleteRole(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteRole")
	}

	return nil, nil
}

// handleListRoles handles ListRoles requests.
func (h *iamHandler) handleListRoles(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListRoles", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")
	requestData, _ := getMap(reqMap, "request")

	pageSize, _ := getInt(requestData, "page_size")

	pbReq := &iampb.ListRolesRequest{
		TenantId: tenantID,
		Limit:    int32(pageSize),
	}

	resp, err := client.ListRoles(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListRoles")
	}

	roles := make([]iam.Role, 0, len(resp.Roles))
	for _, r := range resp.Roles {
		if converted := convertProtoRoleToSDK(r); converted != nil {
			roles = append(roles, *converted)
		}
	}

	return &iam.ListRolesResponse{
		Roles: roles,
		Pagination: iam.PaginationInfo{
			TotalCount: int(resp.Total),
		},
	}, nil
}

// handleReconcileRoles handles ReconcileRoles requests.
func (h *iamHandler) handleReconcileRoles(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleReconcileRoles", "invalid request type")
	}

	tenantID, _ := getString(reqMap, "tenant_id")

	pbReq := &iampb.ReconcileRolesRequest{
		TenantId: tenantID,
	}

	resp, err := client.ReconcileRoles(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleReconcileRoles")
	}

	return convertProtoReconcileRolesToSDK(resp), nil
}

// Conversion helpers.

// convertProtoUserToSDK converts a protobuf User to SDK User.
func convertProtoUserToSDK(u *iampb.User) *iam.User {
	if u == nil {
		return nil
	}

	resp := &iam.User{
		ID:          u.Id,
		TenantID:    u.TenantId,
		Email:       u.Email,
		Username:    u.Username,
		DisplayName: u.Name,
		Roles:       u.Roles,
		AuthMethods: u.AuthMethods,
		Lifecycle:   u.Lifecycle,
		InviteToken: u.InviteToken,
		InviteURL:   u.InviteUrl,
	}

	if u.InviteExpiresAt != nil {
		t := u.InviteExpiresAt.AsTime()
		resp.InviteExpiresAt = &t
	}

	if len(u.Attributes) > 0 {
		resp.Metadata = u.Attributes
	}

	if u.CreatedAt != nil {
		resp.CreatedAt = u.CreatedAt.AsTime()
	}
	if u.UpdatedAt != nil {
		resp.UpdatedAt = u.UpdatedAt.AsTime()
	}
	if u.LastAuth != nil {
		t := u.LastAuth.AsTime()
		resp.LastLoginAt = &t
	}

	return resp
}

// convertProtoTenantToSDK converts a protobuf Tenant to SDK Tenant.
func convertProtoTenantToSDK(t *iampb.Tenant) *iam.Tenant {
	if t == nil {
		return nil
	}

	resp := &iam.Tenant{
		ID:          t.Id,
		Name:        t.Name,
		DisplayName: t.Name,
	}

	if t.Settings != nil && len(t.Settings.NamedNetworks) > 0 {
		resp.Settings = t.Settings.NamedNetworks
	}

	if t.CreatedAt != nil {
		resp.CreatedAt = t.CreatedAt.AsTime()
	}
	if t.UpdatedAt != nil {
		resp.UpdatedAt = t.UpdatedAt.AsTime()
	}

	return resp
}

// convertProtoRoleToSDK converts a protobuf Role to SDK Role.
func convertProtoRoleToSDK(r *iampb.Role) *iam.Role {
	if r == nil {
		return nil
	}

	resp := &iam.Role{
		ID:                 r.Id,
		TenantID:           r.TenantId,
		Name:               r.Name,
		Description:        r.Description,
		Permissions:        r.Permissions,
		RetiredPermissions: r.RetiredPermissions,
	}

	if r.CreatedAt != nil {
		resp.CreatedAt = r.CreatedAt.AsTime()
	}
	if r.UpdatedAt != nil {
		resp.UpdatedAt = r.UpdatedAt.AsTime()
	}

	return resp
}

// convertProtoReconcileRolesToSDK converts a protobuf ReconcileRolesResponse
// to its SDK form. Only the roles that changed are carried, so an empty roles
// slice with Changed false is the ordinary no-drift answer.
func convertProtoReconcileRolesToSDK(r *iampb.ReconcileRolesResponse) *iam.ReconcileRolesResponse {
	if r == nil {
		return nil
	}

	roles := make([]iam.RoleReconcileResult, 0, len(r.Roles))
	for _, result := range r.Roles {
		if result == nil {
			continue
		}
		roles = append(roles, iam.RoleReconcileResult{
			RoleID:  result.RoleId,
			Name:    result.Name,
			Created: result.Created,
			Added:   result.Added,
			Removed: result.Removed,
		})
	}

	return &iam.ReconcileRolesResponse{
		TenantID: r.TenantId,
		Roles:    roles,
		Changed:  r.Changed,
	}
}

// protoTimestamp converts a time.Time to protobuf Timestamp.
func protoTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
