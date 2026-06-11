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
	stderrors "errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// IAMServer implements the IAM gRPC service.
type IAMServer struct {
	iampb.UnimplementedIAMServiceServer
	iamService IAMService
	logger     *logger.Logger
}

// IAMService defines the interface for IAM operations.
// This will be implemented by the service layer.
type IAMService interface {
	// Tenant operations
	CreateTenant(ctx context.Context, tenant *iam.Tenant) (*iam.Tenant, error)
	GetTenant(ctx context.Context, id string) (*iam.Tenant, error)
	UpdateTenant(ctx context.Context, tenant *iam.Tenant) (*iam.Tenant, error)
	DeleteTenant(ctx context.Context, id string) error
	ListTenants(ctx context.Context, limit, offset int) ([]*iam.Tenant, int, error)

	// User operations
	CreateUser(ctx context.Context, user *iam.User) (*iam.User, error)
	GetUser(ctx context.Context, tenantID, userID string) (*iam.User, error)
	UpdateUser(ctx context.Context, user *iam.User) (*iam.User, error)
	DeleteUser(ctx context.Context, tenantID, userID string) error
	ListUsers(ctx context.Context, tenantID string, limit, offset int, filters map[string]string) ([]*iam.User, int, error)

	// Group operations
	CreateGroup(ctx context.Context, group *iam.Group) (*iam.Group, error)
	GetGroup(ctx context.Context, tenantID, groupID string) (*iam.Group, error)
	UpdateGroup(ctx context.Context, group *iam.Group) (*iam.Group, error)
	DeleteGroup(ctx context.Context, tenantID, groupID string) error
	ListGroups(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Group, int, error)
	AddUserToGroup(ctx context.Context, tenantID, groupID, userID string) error
	RemoveUserFromGroup(ctx context.Context, tenantID, groupID, userID string) error

	// Role operations
	CreateRole(ctx context.Context, role *iam.Role) (*iam.Role, error)
	GetRole(ctx context.Context, tenantID, roleID string) (*iam.Role, error)
	UpdateRole(ctx context.Context, role *iam.Role) (*iam.Role, error)
	DeleteRole(ctx context.Context, tenantID, roleID string) error
	ListRoles(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Role, int, error)
	AssignRole(ctx context.Context, tenantID, userID, role string) error
	RevokeRole(ctx context.Context, tenantID, userID, role string) error

	// Application operations
	RegisterApp(ctx context.Context, app *iam.Application) (*iam.Application, error)
	GetApp(ctx context.Context, tenantID, clientID string) (*iam.Application, error)
	UpdateApp(ctx context.Context, app *iam.Application) (*iam.Application, error)
	DeleteApp(ctx context.Context, tenantID, clientID string) error
	ListApps(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Application, int, error)
	RotateAppSecret(ctx context.Context, tenantID, clientID string) (*iam.Application, error)

	// Policy operations
	CreatePolicy(ctx context.Context, policy *iam.Policy) (*iam.Policy, error)
	GetPolicy(ctx context.Context, tenantID, policyName string) (*iam.Policy, error)
	UpdatePolicy(ctx context.Context, policy *iam.Policy) (*iam.Policy, error)
	DeletePolicy(ctx context.Context, tenantID, policyName string) error
	ListPolicies(ctx context.Context, tenantID string, limit, offset int) ([]*iam.Policy, int, error)
	ValidatePolicy(ctx context.Context, tenantID string, specYAML []byte) (bool, []string, error)
	TestPolicy(ctx context.Context, tenantID string, req *iam.CheckRequest) (*iam.CheckResponse, error)

	// Delegated Administration operations
	CreateAdminAssignment(ctx context.Context, opts iam.CreateAdminAssignmentOptions) (*iam.AdminAssignment, error)
	GetAdminAssignment(ctx context.Context, id string) (*iam.AdminAssignment, error)
	UpdateAdminAssignment(ctx context.Context, id string, opts iam.UpdateAdminAssignmentOptions) (*iam.AdminAssignment, error)
	DeleteAdminAssignment(ctx context.Context, id string) error
	ListAdminAssignments(ctx context.Context, opts iam.ListAdminAssignmentsOptions) ([]*iam.AdminAssignment, error)
	CheckAdminPermission(ctx context.Context, req *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error)
	StartImpersonation(ctx context.Context, opts *iam.StartImpersonationOptions) (*iam.ImpersonationSession, error)
	EndImpersonation(ctx context.Context, sessionID string) error
	GetActiveImpersonations(ctx context.Context, adminUserID string) ([]*iam.ImpersonationSession, error)

	// GDPR Consent operations
	GrantConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, version string, expiresAt *time.Time, ipAddress, userAgent string) (*iam.Consent, error)
	WithdrawConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose, ipAddress, userAgent string) error
	GetConsent(ctx context.Context, tenantID, userID string, purpose iam.ConsentPurpose) (*iam.Consent, error)
	ListUserConsents(ctx context.Context, tenantID, userID string) ([]*iam.Consent, error)

	// GDPR Data Export operations
	RequestExport(ctx context.Context, tenantID, userID, requestedBy string, format iam.DataExportFormat, includeCategories, excludeCategories []string) (*iam.DataExportRequest, error)
	GetExportStatus(ctx context.Context, exportID string) (*iam.DataExportRequest, error)
	GenerateExport(ctx context.Context, exportID string) (*iam.UserDataExport, error)

	// GDPR Data Deletion operations
	RequestDeletion(ctx context.Context, tenantID, userID, requestedBy, reason string, deleteCategories, preserveCategories []string, gracePeriodDays int) (*iam.DataDeletionRequest, error)
	CancelDeletion(ctx context.Context, deletionID, cancelledBy string) error
	ProcessDeletion(ctx context.Context, deletionID string) (*iam.DeletionSummary, error)
	GetDeletionStatus(ctx context.Context, deletionID string) (*iam.DataDeletionRequest, error)
}

// NewIAMServer creates a new IAM gRPC server.
func NewIAMServer(iamService IAMService, log *logger.Logger) *IAMServer {
	return &IAMServer{
		iamService: iamService,
		logger:     log.WithField("component", "iam-grpc-server"),
	}
}

// =============================================================================
// Tenant Operations
// =============================================================================

// CreateTenant creates a new tenant.
func (s *IAMServer) CreateTenant(ctx context.Context, req *iampb.CreateTenantRequest) (*iampb.Tenant, error) {
	s.logger.WithField("tenant_id", req.Id).Debug("Processing CreateTenant request")

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant id is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant name is required")
	}

	tenant := &iam.Tenant{
		ID:   req.Id,
		Name: req.Name,
	}

	if req.Settings != nil {
		tenant.Settings = iam.TenantSettings{
			MaxUsers:      int(req.Settings.MaxUsers),
			MFARequired:   req.Settings.MfaRequired,
			NamedNetworks: make(map[string][]string),
		}

		for name, cidr := range req.Settings.NamedNetworks {
			tenant.Settings.NamedNetworks[name] = []string{cidr}
		}
	}

	created, err := s.iamService.CreateTenant(ctx, tenant)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertTenantToProto(created), nil
}

// GetTenant retrieves a tenant by ID.
func (s *IAMServer) GetTenant(ctx context.Context, req *iampb.GetTenantRequest) (*iampb.Tenant, error) {
	s.logger.WithField("tenant_id", req.Id).Debug("Processing GetTenant request")

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant id is required")
	}

	tenant, err := s.iamService.GetTenant(ctx, req.Id)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertTenantToProto(tenant), nil
}

// UpdateTenant updates an existing tenant.
func (s *IAMServer) UpdateTenant(ctx context.Context, req *iampb.UpdateTenantRequest) (*iampb.Tenant, error) {
	s.logger.WithField("tenant_id", req.Id).Debug("Processing UpdateTenant request")

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant id is required")
	}

	tenant := &iam.Tenant{
		ID:   req.Id,
		Name: req.Name,
	}

	if req.Settings != nil {
		tenant.Settings = iam.TenantSettings{
			MaxUsers:      int(req.Settings.MaxUsers),
			MFARequired:   req.Settings.MfaRequired,
			NamedNetworks: make(map[string][]string),
		}

		for name, cidr := range req.Settings.NamedNetworks {
			tenant.Settings.NamedNetworks[name] = []string{cidr}
		}
	}

	updated, err := s.iamService.UpdateTenant(ctx, tenant)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertTenantToProto(updated), nil
}

// DeleteTenant deletes a tenant.
func (s *IAMServer) DeleteTenant(ctx context.Context, req *iampb.DeleteTenantRequest) (*iampb.DeleteTenantResponse, error) {
	s.logger.WithField("tenant_id", req.Id).Info("Processing DeleteTenant request")

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant id is required")
	}

	err := s.iamService.DeleteTenant(ctx, req.Id)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.DeleteTenantResponse{Success: true}, nil
}

// ListTenants lists all tenants with pagination.
func (s *IAMServer) ListTenants(ctx context.Context, req *iampb.ListTenantsRequest) (*iampb.ListTenantsResponse, error) {
	s.logger.Debug("Processing ListTenants request")

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	offset := int(req.Offset)

	tenants, total, err := s.iamService.ListTenants(ctx, limit, offset)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbTenants := make([]*iampb.Tenant, 0, len(tenants))
	for _, tenant := range tenants {
		pbTenants = append(pbTenants, convertTenantToProto(tenant))
	}

	return &iampb.ListTenantsResponse{
		Tenants: pbTenants,
		Total:   int32(total), //nolint:gosec // G115: Total count from pagination
	}, nil
}

// =============================================================================
// User Operations
// =============================================================================

// CreateUser creates a new user.
func (s *IAMServer) CreateUser(ctx context.Context, req *iampb.CreateUserRequest) (*iampb.User, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"email":     req.Email,
	}).Debug("Processing CreateUser request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Email == "" {
		return nil, status.Error(codes.InvalidArgument, "email is required")
	}

	user := &iam.User{
		TenantID: req.TenantId,
		Email:    req.Email,
		Name:     req.Name,
		Roles:    req.Roles,
		Groups:   req.Groups,
	}

	if len(req.Attributes) > 0 {
		user.Attributes = make(map[string]interface{}, len(req.Attributes))
		for k, v := range req.Attributes {
			user.Attributes[k] = v
		}
	}

	created, err := s.iamService.CreateUser(ctx, user)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertUserToProto(created), nil
}

// GetUser retrieves a user by ID.
func (s *IAMServer) GetUser(ctx context.Context, req *iampb.GetUserRequest) (*iampb.User, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing GetUser request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	user, err := s.iamService.GetUser(ctx, req.TenantId, req.UserId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertUserToProto(user), nil
}

// UpdateUser updates an existing user.
func (s *IAMServer) UpdateUser(ctx context.Context, req *iampb.UpdateUserRequest) (*iampb.User, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing UpdateUser request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	user := &iam.User{
		ID:       req.UserId,
		TenantID: req.TenantId,
		Email:    req.Email,
		Name:     req.Name,
		Roles:    req.Roles,
		Groups:   req.Groups,
	}

	if len(req.Attributes) > 0 {
		user.Attributes = make(map[string]interface{}, len(req.Attributes))
		for k, v := range req.Attributes {
			user.Attributes[k] = v
		}
	}

	updated, err := s.iamService.UpdateUser(ctx, user)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertUserToProto(updated), nil
}

// DeleteUser deletes a user.
func (s *IAMServer) DeleteUser(ctx context.Context, req *iampb.DeleteUserRequest) (*iampb.DeleteUserResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Info("Processing DeleteUser request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	err := s.iamService.DeleteUser(ctx, req.TenantId, req.UserId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.DeleteUserResponse{Success: true}, nil
}

// ListUsers lists users with pagination and optional filters.
func (s *IAMServer) ListUsers(ctx context.Context, req *iampb.ListUsersRequest) (*iampb.ListUsersResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListUsers request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	offset := int(req.Offset)

	filters := make(map[string]string)
	if req.Email != "" {
		filters["email"] = req.Email
	}
	if req.Role != "" {
		filters["role"] = req.Role
	}
	if req.Group != "" {
		filters["group"] = req.Group
	}

	users, total, err := s.iamService.ListUsers(ctx, req.TenantId, limit, offset, filters)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbUsers := make([]*iampb.User, 0, len(users))
	for _, user := range users {
		pbUsers = append(pbUsers, convertUserToProto(user))
	}

	return &iampb.ListUsersResponse{
		Users: pbUsers,
		Total: int32(total), //nolint:gosec // G115: Total count from pagination
	}, nil
}

// =============================================================================
// Group Operations
// =============================================================================

// CreateGroup creates a new group.
func (s *IAMServer) CreateGroup(ctx context.Context, req *iampb.CreateGroupRequest) (*iampb.Group, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"group_id":  req.Id,
	}).Debug("Processing CreateGroup request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "group id is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "group name is required")
	}

	group := &iam.Group{
		ID:          req.Id,
		TenantID:    req.TenantId,
		Name:        req.Name,
		Description: req.Description,
	}

	created, err := s.iamService.CreateGroup(ctx, group)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertGroupToProto(created), nil
}

// GetGroup retrieves a group by ID.
func (s *IAMServer) GetGroup(ctx context.Context, req *iampb.GetGroupRequest) (*iampb.Group, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"group_id":  req.GroupId,
	}).Debug("Processing GetGroup request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.GroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "group_id is required")
	}

	group, err := s.iamService.GetGroup(ctx, req.TenantId, req.GroupId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertGroupToProto(group), nil
}

// UpdateGroup updates an existing group.
func (s *IAMServer) UpdateGroup(ctx context.Context, req *iampb.UpdateGroupRequest) (*iampb.Group, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"group_id":  req.GroupId,
	}).Debug("Processing UpdateGroup request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.GroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "group_id is required")
	}

	group := &iam.Group{
		ID:          req.GroupId,
		TenantID:    req.TenantId,
		Name:        req.Name,
		Description: req.Description,
	}

	updated, err := s.iamService.UpdateGroup(ctx, group)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertGroupToProto(updated), nil
}

// DeleteGroup deletes a group.
func (s *IAMServer) DeleteGroup(ctx context.Context, req *iampb.DeleteGroupRequest) (*iampb.DeleteGroupResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"group_id":  req.GroupId,
	}).Info("Processing DeleteGroup request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.GroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "group_id is required")
	}

	err := s.iamService.DeleteGroup(ctx, req.TenantId, req.GroupId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.DeleteGroupResponse{Success: true}, nil
}

// ListGroups lists groups with pagination.
func (s *IAMServer) ListGroups(ctx context.Context, req *iampb.ListGroupsRequest) (*iampb.ListGroupsResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListGroups request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	offset := int(req.Offset)

	groups, total, err := s.iamService.ListGroups(ctx, req.TenantId, limit, offset)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbGroups := make([]*iampb.Group, 0, len(groups))
	for _, group := range groups {
		pbGroups = append(pbGroups, convertGroupToProto(group))
	}

	return &iampb.ListGroupsResponse{
		Groups: pbGroups,
		Total:  int32(total), //nolint:gosec // G115: Total count from pagination
	}, nil
}

// AddUserToGroup adds a user to a group.
func (s *IAMServer) AddUserToGroup(ctx context.Context, req *iampb.AddUserToGroupRequest) (*iampb.AddUserToGroupResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"group_id":  req.GroupId,
		"user_id":   req.UserId,
	}).Debug("Processing AddUserToGroup request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.GroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "group_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	err := s.iamService.AddUserToGroup(ctx, req.TenantId, req.GroupId, req.UserId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.AddUserToGroupResponse{Success: true}, nil
}

// RemoveUserFromGroup removes a user from a group.
func (s *IAMServer) RemoveUserFromGroup(ctx context.Context, req *iampb.RemoveUserFromGroupRequest) (*iampb.RemoveUserFromGroupResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"group_id":  req.GroupId,
		"user_id":   req.UserId,
	}).Debug("Processing RemoveUserFromGroup request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.GroupId == "" {
		return nil, status.Error(codes.InvalidArgument, "group_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	err := s.iamService.RemoveUserFromGroup(ctx, req.TenantId, req.GroupId, req.UserId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.RemoveUserFromGroupResponse{Success: true}, nil
}

// =============================================================================
// Role Operations
// =============================================================================

// CreateRole creates a new role.
func (s *IAMServer) CreateRole(ctx context.Context, req *iampb.CreateRoleRequest) (*iampb.Role, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"role_id":   req.Id,
	}).Debug("Processing CreateRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "role id is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "role name is required")
	}

	role := &iam.Role{
		ID:          req.Id,
		TenantID:    req.TenantId,
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
	}

	created, err := s.iamService.CreateRole(ctx, role)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertRoleToProto(created), nil
}

// GetRole retrieves a role by ID.
func (s *IAMServer) GetRole(ctx context.Context, req *iampb.GetRoleRequest) (*iampb.Role, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"role_id":   req.RoleId,
	}).Debug("Processing GetRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.RoleId == "" {
		return nil, status.Error(codes.InvalidArgument, "role_id is required")
	}

	role, err := s.iamService.GetRole(ctx, req.TenantId, req.RoleId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertRoleToProto(role), nil
}

// UpdateRole updates an existing role.
func (s *IAMServer) UpdateRole(ctx context.Context, req *iampb.UpdateRoleRequest) (*iampb.Role, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"role_id":   req.RoleId,
	}).Debug("Processing UpdateRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.RoleId == "" {
		return nil, status.Error(codes.InvalidArgument, "role_id is required")
	}

	role := &iam.Role{
		ID:          req.RoleId,
		TenantID:    req.TenantId,
		Name:        req.Name,
		Description: req.Description,
		Permissions: req.Permissions,
	}

	updated, err := s.iamService.UpdateRole(ctx, role)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertRoleToProto(updated), nil
}

// DeleteRole deletes a role.
func (s *IAMServer) DeleteRole(ctx context.Context, req *iampb.DeleteRoleRequest) (*iampb.DeleteRoleResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"role_id":   req.RoleId,
	}).Info("Processing DeleteRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.RoleId == "" {
		return nil, status.Error(codes.InvalidArgument, "role_id is required")
	}

	err := s.iamService.DeleteRole(ctx, req.TenantId, req.RoleId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.DeleteRoleResponse{Success: true}, nil
}

// ListRoles lists roles with pagination.
func (s *IAMServer) ListRoles(ctx context.Context, req *iampb.ListRolesRequest) (*iampb.ListRolesResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListRoles request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	offset := int(req.Offset)

	roles, total, err := s.iamService.ListRoles(ctx, req.TenantId, limit, offset)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbRoles := make([]*iampb.Role, 0, len(roles))
	for _, role := range roles {
		pbRoles = append(pbRoles, convertRoleToProto(role))
	}

	return &iampb.ListRolesResponse{
		Roles: pbRoles,
		Total: int32(total), //nolint:gosec // G115: Total count from pagination
	}, nil
}

// AssignRole assigns a role to a user.
func (s *IAMServer) AssignRole(ctx context.Context, req *iampb.AssignRoleRequest) (*iampb.AssignRoleResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
		"role":      req.Role,
	}).Debug("Processing AssignRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Role == "" {
		return nil, status.Error(codes.InvalidArgument, "role is required")
	}

	err := s.iamService.AssignRole(ctx, req.TenantId, req.UserId, req.Role)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.AssignRoleResponse{Success: true}, nil
}

// RevokeRole revokes a role from a user.
func (s *IAMServer) RevokeRole(ctx context.Context, req *iampb.RevokeRoleRequest) (*iampb.RevokeRoleResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
		"role":      req.Role,
	}).Debug("Processing RevokeRole request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Role == "" {
		return nil, status.Error(codes.InvalidArgument, "role is required")
	}

	err := s.iamService.RevokeRole(ctx, req.TenantId, req.UserId, req.Role)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.RevokeRoleResponse{Success: true}, nil
}

// =============================================================================
// Application Operations
// =============================================================================

// RegisterApp registers a new OAuth application.
func (s *IAMServer) RegisterApp(ctx context.Context, req *iampb.RegisterAppRequest) (*iampb.Application, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"app_name":  req.Name,
	}).Debug("Processing RegisterApp request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "app name is required")
	}

	app := &iam.Application{
		TenantID:       req.TenantId,
		Name:           req.Name,
		RedirectURIs:   req.RedirectUris,
		ResourcePrefix: req.ResourcePrefix,
		GrantTypes:     req.GrantTypes,
		ResponseTypes:  req.ResponseTypes,
	}

	created, err := s.iamService.RegisterApp(ctx, app)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertApplicationToProto(created), nil
}

// GetApp retrieves an application by client ID.
func (s *IAMServer) GetApp(ctx context.Context, req *iampb.GetAppRequest) (*iampb.Application, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"client_id": req.ClientId,
	}).Debug("Processing GetApp request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ClientId == "" {
		return nil, status.Error(codes.InvalidArgument, "client_id is required")
	}

	app, err := s.iamService.GetApp(ctx, req.TenantId, req.ClientId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	// Don't return client secret in GET operations
	appProto := convertApplicationToProto(app)
	appProto.ClientSecret = ""

	return appProto, nil
}

// UpdateApp updates an existing application.
func (s *IAMServer) UpdateApp(ctx context.Context, req *iampb.UpdateAppRequest) (*iampb.Application, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"client_id": req.ClientId,
	}).Debug("Processing UpdateApp request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ClientId == "" {
		return nil, status.Error(codes.InvalidArgument, "client_id is required")
	}

	app := &iam.Application{
		ClientID:       req.ClientId,
		TenantID:       req.TenantId,
		Name:           req.Name,
		RedirectURIs:   req.RedirectUris,
		ResourcePrefix: req.ResourcePrefix,
		GrantTypes:     req.GrantTypes,
		ResponseTypes:  req.ResponseTypes,
	}

	updated, err := s.iamService.UpdateApp(ctx, app)
	if err != nil {
		return nil, convertIAMError(err)
	}

	// Don't return client secret in update operations
	appProto := convertApplicationToProto(updated)
	appProto.ClientSecret = ""

	return appProto, nil
}

// DeleteApp deletes an application.
func (s *IAMServer) DeleteApp(ctx context.Context, req *iampb.DeleteAppRequest) (*iampb.DeleteAppResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"client_id": req.ClientId,
	}).Info("Processing DeleteApp request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ClientId == "" {
		return nil, status.Error(codes.InvalidArgument, "client_id is required")
	}

	err := s.iamService.DeleteApp(ctx, req.TenantId, req.ClientId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.DeleteAppResponse{Success: true}, nil
}

// ListApps lists applications with pagination.
func (s *IAMServer) ListApps(ctx context.Context, req *iampb.ListAppsRequest) (*iampb.ListAppsResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListApps request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	offset := int(req.Offset)

	apps, total, err := s.iamService.ListApps(ctx, req.TenantId, limit, offset)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbApps := make([]*iampb.Application, 0, len(apps))
	for _, app := range apps {
		appProto := convertApplicationToProto(app)
		// Don't return client secrets in list operations
		appProto.ClientSecret = ""
		pbApps = append(pbApps, appProto)
	}

	return &iampb.ListAppsResponse{
		Apps:  pbApps,
		Total: int32(total), //nolint:gosec // G115: Total count from pagination
	}, nil
}

// RotateAppSecret rotates an application's client secret.
func (s *IAMServer) RotateAppSecret(ctx context.Context, req *iampb.RotateAppSecretRequest) (*iampb.Application, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"client_id": req.ClientId,
	}).Info("Processing RotateAppSecret request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.ClientId == "" {
		return nil, status.Error(codes.InvalidArgument, "client_id is required")
	}

	app, err := s.iamService.RotateAppSecret(ctx, req.TenantId, req.ClientId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	// Return the new client secret (only time it's returned)
	return convertApplicationToProto(app), nil
}

// =============================================================================
// Policy Operations
// =============================================================================

// CreatePolicy creates a new policy.
func (s *IAMServer) CreatePolicy(ctx context.Context, req *iampb.CreatePolicyRequest) (*iampb.Policy, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"policy_name": req.Name,
	}).Debug("Processing CreatePolicy request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "policy name is required")
	}
	if len(req.SpecYaml) == 0 {
		return nil, status.Error(codes.InvalidArgument, "spec_yaml is required")
	}

	// Parse the policy YAML - try full policy format first, then spec-only format
	var policy *iam.Policy
	var fullPolicy iam.Policy
	if err := yaml.Unmarshal(req.SpecYaml, &fullPolicy); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid spec_yaml: %v", err)
	}

	// Check if this is a full policy document (has apiVersion and kind)
	if fullPolicy.APIVersion != "" && fullPolicy.Kind != "" {
		// Full policy format - use parsed policy with metadata from YAML
		policy = &fullPolicy
		// Override with request values if provided
		if req.Name != "" {
			policy.Metadata.Name = req.Name
		}
		if req.TenantId != "" {
			policy.Metadata.Tenant = req.TenantId
		}
		if req.Description != "" {
			policy.Metadata.Description = req.Description
		}
		if len(req.Labels) > 0 {
			policy.Metadata.Labels = req.Labels
		}
		if req.Priority > 0 {
			policy.Metadata.Priority = int(req.Priority)
		}
	} else {
		// Spec-only format - build policy from request fields
		var spec iam.PolicySpec
		if err := yaml.Unmarshal(req.SpecYaml, &spec); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid spec_yaml: %v", err)
		}
		policy = &iam.Policy{
			APIVersion: "iam.quicbaas.io/v1",
			Kind:       "Policy",
			Metadata: iam.PolicyMetadata{
				Name:        req.Name,
				Tenant:      req.TenantId,
				Description: req.Description,
				Labels:      req.Labels,
				Priority:    int(req.Priority),
			},
			Spec: spec,
		}
	}

	created, err := s.iamService.CreatePolicy(ctx, policy)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbPolicy := convertPolicyToProto(created)
	pbPolicy.SpecYaml = req.SpecYaml

	return pbPolicy, nil
}

// GetPolicy retrieves a policy by name.
func (s *IAMServer) GetPolicy(ctx context.Context, req *iampb.GetPolicyRequest) (*iampb.Policy, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"policy_name": req.PolicyName,
	}).Debug("Processing GetPolicy request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.PolicyName == "" {
		return nil, status.Error(codes.InvalidArgument, "policy_name is required")
	}

	policy, err := s.iamService.GetPolicy(ctx, req.TenantId, req.PolicyName)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertPolicyToProto(policy), nil
}

// UpdatePolicy updates an existing policy.
func (s *IAMServer) UpdatePolicy(ctx context.Context, req *iampb.UpdatePolicyRequest) (*iampb.Policy, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"policy_name": req.PolicyName,
	}).Debug("Processing UpdatePolicy request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.PolicyName == "" {
		return nil, status.Error(codes.InvalidArgument, "policy_name is required")
	}

	policy := &iam.Policy{
		APIVersion: "iam.quicbaas.io/v1",
		Kind:       "Policy",
		Metadata: iam.PolicyMetadata{
			Name:        req.PolicyName,
			Tenant:      req.TenantId,
			Description: req.Description,
			Labels:      req.Labels,
			Priority:    int(req.Priority),
		},
		// Spec would be parsed from spec_yaml by the service layer
	}

	updated, err := s.iamService.UpdatePolicy(ctx, policy)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbPolicy := convertPolicyToProto(updated)
	if len(req.SpecYaml) > 0 {
		pbPolicy.SpecYaml = req.SpecYaml
	}

	return pbPolicy, nil
}

// DeletePolicy deletes a policy.
func (s *IAMServer) DeletePolicy(ctx context.Context, req *iampb.DeletePolicyRequest) (*iampb.DeletePolicyResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id":   req.TenantId,
		"policy_name": req.PolicyName,
	}).Info("Processing DeletePolicy request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.PolicyName == "" {
		return nil, status.Error(codes.InvalidArgument, "policy_name is required")
	}

	err := s.iamService.DeletePolicy(ctx, req.TenantId, req.PolicyName)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.DeletePolicyResponse{Success: true}, nil
}

// ListPolicies lists policies with pagination.
func (s *IAMServer) ListPolicies(ctx context.Context, req *iampb.ListPoliciesRequest) (*iampb.ListPoliciesResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ListPolicies request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	limit := int(req.Limit)
	if limit == 0 {
		limit = 100
	}
	offset := int(req.Offset)

	policies, total, err := s.iamService.ListPolicies(ctx, req.TenantId, limit, offset)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbPolicies := make([]*iampb.Policy, 0, len(policies))
	for _, policy := range policies {
		pbPolicies = append(pbPolicies, convertPolicyToProto(policy))
	}

	return &iampb.ListPoliciesResponse{
		Policies: pbPolicies,
		Total:    int32(total), //nolint:gosec // G115: Total count from pagination
	}, nil
}

// ValidatePolicy validates a policy YAML without creating it.
func (s *IAMServer) ValidatePolicy(ctx context.Context, req *iampb.ValidatePolicyRequest) (*iampb.ValidatePolicyResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing ValidatePolicy request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if len(req.SpecYaml) == 0 {
		return nil, status.Error(codes.InvalidArgument, "spec_yaml is required")
	}

	valid, validationErrors, err := s.iamService.ValidatePolicy(ctx, req.TenantId, req.SpecYaml)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.ValidatePolicyResponse{
		Valid:  valid,
		Errors: validationErrors,
	}, nil
}

// TestPolicy tests a policy against a specific request.
func (s *IAMServer) TestPolicy(ctx context.Context, req *iampb.TestPolicyRequest) (*iampb.TestPolicyResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Debug("Processing TestPolicy request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Subject == nil {
		return nil, status.Error(codes.InvalidArgument, "subject is required")
	}
	if req.Action == "" {
		return nil, status.Error(codes.InvalidArgument, "action is required")
	}
	if req.Resource == "" {
		return nil, status.Error(codes.InvalidArgument, "resource is required")
	}

	checkReq := &iam.CheckRequest{
		Subject:  *convertProtoToSubject(req.Subject),
		Action:   req.Action,
		Resource: req.Resource,
		Context:  make(map[string]interface{}),
	}

	for k, v := range req.Context {
		checkReq.Context[k] = v
	}

	result, err := s.iamService.TestPolicy(ctx, req.TenantId, checkReq)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.TestPolicyResponse{
		Allowed:          result.Allowed,
		MatchedPolicy:    result.MatchedPolicy,
		MatchedRule:      result.MatchedRule,
		EvaluationTimeNs: result.EvaluationTimeNs,
	}, nil
}

// =============================================================================
// Delegated Administration Operations
// =============================================================================

// CreateAdminAssignment creates a new admin assignment.
func (s *IAMServer) CreateAdminAssignment(ctx context.Context, req *iampb.CreateAdminAssignmentRequest) (*iampb.AdminAssignment, error) {
	s.logger.WithFields(map[string]interface{}{
		"user_id":    req.UserId,
		"admin_role": req.AdminRole.String(),
	}).Debug("Processing CreateAdminAssignment request")

	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.AdminRole == iampb.AdminRole_ADMIN_ROLE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "admin_role is required")
	}
	if req.GrantedBy == "" {
		return nil, status.Error(codes.InvalidArgument, "granted_by is required")
	}

	// Convert permissions from proto to domain
	permissions := make([]iam.AdminPermission, 0, len(req.Permissions))
	for _, p := range req.Permissions {
		permissions = append(permissions, iam.AdminPermission(p))
	}

	deniedPermissions := make([]iam.AdminPermission, 0, len(req.DeniedPermissions))
	for _, p := range req.DeniedPermissions {
		deniedPermissions = append(deniedPermissions, iam.AdminPermission(p))
	}

	// Convert resource scopes
	resourceScopes := make([]iam.ResourceScope, 0, len(req.ResourceScopes))
	for _, rs := range req.ResourceScopes {
		resourceScopes = append(resourceScopes, iam.ResourceScope{
			ResourceType:    rs.ResourceType,
			ResourcePattern: rs.ResourcePattern,
		})
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t := req.ExpiresAt.AsTime()
		expiresAt = &t
	}

	opts := iam.CreateAdminAssignmentOptions{
		UserID:            req.UserId,
		AdminRole:         convertProtoAdminRole(req.AdminRole),
		TenantIDs:         req.TenantIds,
		Permissions:       permissions,
		DeniedPermissions: deniedPermissions,
		ResourceScopes:    resourceScopes,
		ExpiresAt:         expiresAt,
		GrantedBy:         req.GrantedBy,
		Metadata:          req.Metadata,
	}

	assignment, err := s.iamService.CreateAdminAssignment(ctx, opts)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertAdminAssignmentToProto(assignment), nil
}

// GetAdminAssignment retrieves an admin assignment by ID.
func (s *IAMServer) GetAdminAssignment(ctx context.Context, req *iampb.GetAdminAssignmentRequest) (*iampb.AdminAssignment, error) {
	s.logger.WithField("assignment_id", req.Id).Debug("Processing GetAdminAssignment request")

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	assignment, err := s.iamService.GetAdminAssignment(ctx, req.Id)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertAdminAssignmentToProto(assignment), nil
}

// UpdateAdminAssignment updates an existing admin assignment.
func (s *IAMServer) UpdateAdminAssignment(ctx context.Context, req *iampb.UpdateAdminAssignmentRequest) (*iampb.AdminAssignment, error) {
	s.logger.WithField("assignment_id", req.Id).Debug("Processing UpdateAdminAssignment request")

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	// Convert permissions from proto to domain
	var permissions []iam.AdminPermission
	if len(req.Permissions) > 0 {
		permissions = make([]iam.AdminPermission, 0, len(req.Permissions))
		for _, p := range req.Permissions {
			permissions = append(permissions, iam.AdminPermission(p))
		}
	}

	var deniedPermissions []iam.AdminPermission
	if len(req.DeniedPermissions) > 0 {
		deniedPermissions = make([]iam.AdminPermission, 0, len(req.DeniedPermissions))
		for _, p := range req.DeniedPermissions {
			deniedPermissions = append(deniedPermissions, iam.AdminPermission(p))
		}
	}

	// Convert resource scopes
	var resourceScopes []iam.ResourceScope
	if len(req.ResourceScopes) > 0 {
		resourceScopes = make([]iam.ResourceScope, 0, len(req.ResourceScopes))
		for _, rs := range req.ResourceScopes {
			resourceScopes = append(resourceScopes, iam.ResourceScope{
				ResourceType:    rs.ResourceType,
				ResourcePattern: rs.ResourcePattern,
			})
		}
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t := req.ExpiresAt.AsTime()
		expiresAt = &t
	}

	var adminRole *iam.AdminRole
	if req.AdminRole != iampb.AdminRole_ADMIN_ROLE_UNSPECIFIED {
		role := convertProtoAdminRole(req.AdminRole)
		adminRole = &role
	}

	opts := iam.UpdateAdminAssignmentOptions{
		AdminRole:         adminRole,
		TenantIDs:         req.TenantIds,
		Permissions:       permissions,
		DeniedPermissions: deniedPermissions,
		ResourceScopes:    resourceScopes,
		ExpiresAt:         expiresAt,
		Active:            &req.Active,
		Metadata:          req.Metadata,
	}

	assignment, err := s.iamService.UpdateAdminAssignment(ctx, req.Id, opts)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertAdminAssignmentToProto(assignment), nil
}

// DeleteAdminAssignment deletes an admin assignment.
func (s *IAMServer) DeleteAdminAssignment(ctx context.Context, req *iampb.DeleteAdminAssignmentRequest) (*iampb.DeleteAdminAssignmentResponse, error) {
	s.logger.WithField("assignment_id", req.Id).Info("Processing DeleteAdminAssignment request")

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	err := s.iamService.DeleteAdminAssignment(ctx, req.Id)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.DeleteAdminAssignmentResponse{Success: true}, nil
}

// ListAdminAssignments lists admin assignments with filters.
func (s *IAMServer) ListAdminAssignments(ctx context.Context, req *iampb.ListAdminAssignmentsRequest) (*iampb.AdminAssignmentList, error) {
	s.logger.Debug("Processing ListAdminAssignments request")

	opts := iam.ListAdminAssignmentsOptions{
		UserID:     req.UserId,
		TenantID:   req.TenantId,
		AdminRole:  convertProtoAdminRole(req.AdminRole),
		ActiveOnly: req.ActiveOnly,
		Limit:      int(req.Limit),
		Offset:     int(req.Offset),
	}

	if opts.Limit == 0 {
		opts.Limit = 100
	}

	assignments, err := s.iamService.ListAdminAssignments(ctx, opts)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbAssignments := make([]*iampb.AdminAssignment, 0, len(assignments))
	for _, assignment := range assignments {
		pbAssignments = append(pbAssignments, convertAdminAssignmentToProto(assignment))
	}

	return &iampb.AdminAssignmentList{
		Assignments: pbAssignments,
		Total:       int32(len(assignments)), //nolint:gosec // G115: Count from list
	}, nil
}

// CheckAdminPermission checks if an admin has a specific permission.
func (s *IAMServer) CheckAdminPermission(ctx context.Context, req *iampb.CheckAdminPermissionRequest) (*iampb.CheckAdminPermissionResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"user_id":    req.UserId,
		"permission": req.Permission,
	}).Debug("Processing CheckAdminPermission request")

	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Permission == "" {
		return nil, status.Error(codes.InvalidArgument, "permission is required")
	}

	checkReq := &iam.AdminCheckRequest{
		UserID:       req.UserId,
		Permission:   iam.AdminPermission(req.Permission),
		TenantID:     req.TenantId,
		ResourceType: req.ResourceType,
		ResourceID:   req.ResourceId,
	}

	result, err := s.iamService.CheckAdminPermission(ctx, checkReq)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.CheckAdminPermissionResponse{
		Allowed:      result.Allowed,
		AssignmentId: result.AssignmentID,
		AdminRole:    convertAdminRoleToProto(result.AdminRole),
		Reason:       result.Reason,
	}, nil
}

// StartImpersonation starts an impersonation session.
func (s *IAMServer) StartImpersonation(ctx context.Context, req *iampb.StartImpersonationRequest) (*iampb.ImpersonationSession, error) {
	s.logger.WithFields(map[string]interface{}{
		"admin_user_id":  req.AdminUserId,
		"target_user_id": req.TargetUserId,
		"tenant_id":      req.TenantId,
	}).Info("Processing StartImpersonation request")

	if req.AdminUserId == "" {
		return nil, status.Error(codes.InvalidArgument, "admin_user_id is required")
	}
	if req.TargetUserId == "" {
		return nil, status.Error(codes.InvalidArgument, "target_user_id is required")
	}
	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.Reason == "" {
		return nil, status.Error(codes.InvalidArgument, "reason is required")
	}

	var duration time.Duration
	if req.Duration != nil {
		duration = req.Duration.AsDuration()
	}

	opts := &iam.StartImpersonationOptions{
		AdminUserID:  req.AdminUserId,
		TargetUserID: req.TargetUserId,
		TenantID:     req.TenantId,
		Reason:       req.Reason,
		Duration:     duration,
	}

	session, err := s.iamService.StartImpersonation(ctx, opts)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertImpersonationSessionToProto(session), nil
}

// EndImpersonation ends an impersonation session.
func (s *IAMServer) EndImpersonation(ctx context.Context, req *iampb.EndImpersonationRequest) (*iampb.EndImpersonationResponse, error) {
	s.logger.WithField("session_id", req.SessionId).Info("Processing EndImpersonation request")

	if req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	err := s.iamService.EndImpersonation(ctx, req.SessionId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.EndImpersonationResponse{Success: true}, nil
}

// GetActiveImpersonations retrieves active impersonation sessions.
func (s *IAMServer) GetActiveImpersonations(ctx context.Context, req *iampb.GetActiveImpersonationsRequest) (*iampb.ImpersonationSessionList, error) {
	s.logger.WithField("admin_user_id", req.AdminUserId).Debug("Processing GetActiveImpersonations request")

	if req.AdminUserId == "" {
		return nil, status.Error(codes.InvalidArgument, "admin_user_id is required")
	}

	sessions, err := s.iamService.GetActiveImpersonations(ctx, req.AdminUserId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbSessions := make([]*iampb.ImpersonationSession, 0, len(sessions))
	for _, session := range sessions {
		pbSessions = append(pbSessions, convertImpersonationSessionToProto(session))
	}

	return &iampb.ImpersonationSessionList{Sessions: pbSessions}, nil
}

// =============================================================================
// GDPR Consent Operations
// =============================================================================

// GrantConsent grants consent for a specific purpose.
func (s *IAMServer) GrantConsent(ctx context.Context, req *iampb.GrantConsentRequest) (*iampb.Consent, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
		"purpose":   req.Purpose.String(),
	}).Debug("Processing GrantConsent request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Purpose == iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "purpose is required")
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil {
		t := req.ExpiresAt.AsTime()
		expiresAt = &t
	}

	consent, err := s.iamService.GrantConsent(
		ctx,
		req.TenantId,
		req.UserId,
		convertProtoConsentPurpose(req.Purpose),
		req.Version,
		expiresAt,
		req.IpAddress,
		req.UserAgent,
	)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertConsentToProto(consent), nil
}

// WithdrawConsent withdraws a previously granted consent.
func (s *IAMServer) WithdrawConsent(ctx context.Context, req *iampb.WithdrawConsentRequest) (*iampb.WithdrawConsentResponse, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
		"purpose":   req.Purpose.String(),
	}).Info("Processing WithdrawConsent request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Purpose == iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "purpose is required")
	}

	err := s.iamService.WithdrawConsent(
		ctx,
		req.TenantId,
		req.UserId,
		convertProtoConsentPurpose(req.Purpose),
		req.IpAddress,
		req.UserAgent,
	)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.WithdrawConsentResponse{Success: true}, nil
}

// GetConsent retrieves a specific consent.
func (s *IAMServer) GetConsent(ctx context.Context, req *iampb.GetConsentRequest) (*iampb.Consent, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
		"purpose":   req.Purpose.String(),
	}).Debug("Processing GetConsent request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.Purpose == iampb.ConsentPurpose_CONSENT_PURPOSE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "purpose is required")
	}

	consent, err := s.iamService.GetConsent(ctx, req.TenantId, req.UserId, convertProtoConsentPurpose(req.Purpose))
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertConsentToProto(consent), nil
}

// ListUserConsents lists all consents for a user.
func (s *IAMServer) ListUserConsents(ctx context.Context, req *iampb.ListUserConsentsRequest) (*iampb.ConsentList, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing ListUserConsents request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	consents, err := s.iamService.ListUserConsents(ctx, req.TenantId, req.UserId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	pbConsents := make([]*iampb.Consent, 0, len(consents))
	for _, consent := range consents {
		pbConsents = append(pbConsents, convertConsentToProto(consent))
	}

	return &iampb.ConsentList{
		Consents: pbConsents,
		Total:    int32(len(consents)), //nolint:gosec // G115: Count from list
	}, nil
}

// =============================================================================
// GDPR Data Export Operations
// =============================================================================

// RequestExport creates a new data export request.
func (s *IAMServer) RequestExport(ctx context.Context, req *iampb.RequestExportRequest) (*iampb.DataExportRequest, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Debug("Processing RequestExport request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.RequestedBy == "" {
		return nil, status.Error(codes.InvalidArgument, "requested_by is required")
	}

	exportReq, err := s.iamService.RequestExport(
		ctx,
		req.TenantId,
		req.UserId,
		req.RequestedBy,
		convertProtoDataExportFormat(req.Format),
		req.IncludeCategories,
		req.ExcludeCategories,
	)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertDataExportRequestToProto(exportReq), nil
}

// GetExportStatus retrieves the status of an export request.
func (s *IAMServer) GetExportStatus(ctx context.Context, req *iampb.GetExportStatusRequest) (*iampb.DataExportRequest, error) {
	s.logger.WithField("export_id", req.ExportId).Debug("Processing GetExportStatus request")

	if req.ExportId == "" {
		return nil, status.Error(codes.InvalidArgument, "export_id is required")
	}

	exportReq, err := s.iamService.GetExportStatus(ctx, req.ExportId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertDataExportRequestToProto(exportReq), nil
}

// GenerateExport generates a user data export.
func (s *IAMServer) GenerateExport(ctx context.Context, req *iampb.GenerateExportRequest) (*iampb.UserDataExport, error) {
	s.logger.WithField("export_id", req.ExportId).Info("Processing GenerateExport request")

	if req.ExportId == "" {
		return nil, status.Error(codes.InvalidArgument, "export_id is required")
	}

	export, err := s.iamService.GenerateExport(ctx, req.ExportId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertUserDataExportToProto(export), nil
}

// =============================================================================
// GDPR Data Deletion Operations
// =============================================================================

// RequestDeletion creates a new data deletion request.
func (s *IAMServer) RequestDeletion(ctx context.Context, req *iampb.RequestDeletionRequest) (*iampb.DataDeletionRequest, error) {
	s.logger.WithFields(map[string]interface{}{
		"tenant_id": req.TenantId,
		"user_id":   req.UserId,
	}).Info("Processing RequestDeletion request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if req.RequestedBy == "" {
		return nil, status.Error(codes.InvalidArgument, "requested_by is required")
	}

	deletionReq, err := s.iamService.RequestDeletion(
		ctx,
		req.TenantId,
		req.UserId,
		req.RequestedBy,
		req.Reason,
		req.DeleteCategories,
		req.PreserveCategories,
		int(req.GracePeriodDays),
	)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertDataDeletionRequestToProto(deletionReq), nil
}

// CancelDeletion cancels a pending deletion request.
func (s *IAMServer) CancelDeletion(ctx context.Context, req *iampb.CancelDeletionRequest) (*iampb.CancelDeletionResponse, error) {
	s.logger.WithField("deletion_id", req.DeletionId).Info("Processing CancelDeletion request")

	if req.DeletionId == "" {
		return nil, status.Error(codes.InvalidArgument, "deletion_id is required")
	}
	if req.CancelledBy == "" {
		return nil, status.Error(codes.InvalidArgument, "cancelled_by is required")
	}

	err := s.iamService.CancelDeletion(ctx, req.DeletionId, req.CancelledBy)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return &iampb.CancelDeletionResponse{Success: true}, nil
}

// ProcessDeletion processes a deletion request.
func (s *IAMServer) ProcessDeletion(ctx context.Context, req *iampb.ProcessDeletionRequest) (*iampb.DeletionSummary, error) {
	s.logger.WithField("deletion_id", req.DeletionId).Info("Processing ProcessDeletion request")

	if req.DeletionId == "" {
		return nil, status.Error(codes.InvalidArgument, "deletion_id is required")
	}

	summary, err := s.iamService.ProcessDeletion(ctx, req.DeletionId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertDeletionSummaryToProto(summary), nil
}

// GetDeletionStatus retrieves the status of a deletion request.
func (s *IAMServer) GetDeletionStatus(ctx context.Context, req *iampb.GetDeletionStatusRequest) (*iampb.DataDeletionRequest, error) {
	s.logger.WithField("deletion_id", req.DeletionId).Debug("Processing GetDeletionStatus request")

	if req.DeletionId == "" {
		return nil, status.Error(codes.InvalidArgument, "deletion_id is required")
	}

	deletionReq, err := s.iamService.GetDeletionStatus(ctx, req.DeletionId)
	if err != nil {
		return nil, convertIAMError(err)
	}

	return convertDataDeletionRequestToProto(deletionReq), nil
}

// =============================================================================
// Error Conversion
// =============================================================================

// convertIAMError converts IAM domain errors to gRPC status errors.
func convertIAMError(err error) error {
	if err == nil {
		return nil
	}

	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		return status.Error(codes.Internal, err.Error())
	}

	switch dErr.Code {
	case errors.ErrNotFound:
		return status.Error(codes.NotFound, dErr.Error())
	case errors.ErrAlreadyExists:
		return status.Error(codes.AlreadyExists, dErr.Error())
	case errors.ErrInvalidInput, errors.ErrValidation:
		return status.Error(codes.InvalidArgument, dErr.Error())
	case errors.ErrUnauthorized:
		return status.Error(codes.Unauthenticated, dErr.Error())
	case errors.ErrForbidden:
		return status.Error(codes.PermissionDenied, dErr.Error())
	case errors.ErrTimeout, errors.ErrDeadlineExceeded:
		return status.Error(codes.DeadlineExceeded, dErr.Error())
	case errors.ErrResourceExhausted:
		return status.Error(codes.ResourceExhausted, dErr.Error())
	case errors.ErrUnavailable:
		return status.Error(codes.Unavailable, dErr.Error())
	case errors.ErrConflict:
		return status.Error(codes.FailedPrecondition, dErr.Error())
	case errors.ErrUnimplemented, errors.ErrNotImplemented:
		return status.Error(codes.Unimplemented, dErr.Error())
	default:
		return status.Error(codes.Internal, dErr.Error())
	}
}
