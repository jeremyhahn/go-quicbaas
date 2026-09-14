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
	"encoding/json"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	adminServiceName = "quicbaas.IAM.Admin"
)

// AdminClient provides delegated administration operations.
//
// The client supports:
//   - Scoped administrative assignments
//   - Permission checks for delegated admins
//   - Admin impersonation with audit trail
//   - Multi-level delegation hierarchies
type AdminClient interface {
	// CreateAssignment creates a new administrative assignment.
	CreateAssignment(ctx context.Context, req *CreateAdminAssignmentRequest) (*AdminAssignment, error)

	// GetAssignment retrieves an administrative assignment by ID.
	GetAssignment(ctx context.Context, assignmentID string) (*AdminAssignment, error)

	// UpdateAssignment modifies an existing administrative assignment.
	UpdateAssignment(ctx context.Context, assignmentID string, req *UpdateAdminAssignmentRequest) (*AdminAssignment, error)

	// DeleteAssignment removes an administrative assignment.
	DeleteAssignment(ctx context.Context, assignmentID string) error

	// ListAssignments returns administrative assignments matching the filter.
	ListAssignments(ctx context.Context, req *ListAdminAssignmentsRequest) (*ListAdminAssignmentsResponse, error)

	// CheckPermission checks if an admin has a specific permission.
	CheckPermission(ctx context.Context, req *AdminCheckRequest) (*AdminCheckResponse, error)

	// StartImpersonation begins an impersonation session.
	StartImpersonation(ctx context.Context, req *ImpersonationRequest) (*ImpersonationSession, error)

	// EndImpersonation ends an impersonation session.
	EndImpersonation(ctx context.Context, sessionID string) error

	// ListActiveImpersonations returns all active impersonation sessions.
	ListActiveImpersonations(ctx context.Context, adminUserID string) (*ListImpersonationsResponse, error)
}

// AdminScope represents the scope of an administrative assignment.
type AdminScope int

const (
	// AdminScopeGlobal grants permissions across all tenants.
	AdminScopeGlobal AdminScope = iota
	// AdminScopeTenant grants permissions within a specific tenant.
	AdminScopeTenant
	// AdminScopeOrganization grants permissions within an organization.
	AdminScopeOrganization
	// AdminScopeGroup grants permissions for a specific group.
	AdminScopeGroup
	// AdminScopeProject grants permissions for a specific project.
	AdminScopeProject
	// AdminScopeResource grants permissions for a specific resource.
	AdminScopeResource
)

// String returns the string representation of the admin scope.
func (s AdminScope) String() string {
	switch s {
	case AdminScopeGlobal:
		return "global"
	case AdminScopeTenant:
		return "tenant"
	case AdminScopeOrganization:
		return "organization"
	case AdminScopeGroup:
		return "group"
	case AdminScopeProject:
		return "project"
	case AdminScopeResource:
		return "resource"
	default:
		return "unknown"
	}
}

// AdminAssignmentStatus represents the status of an admin assignment.
type AdminAssignmentStatus int

const (
	// AdminAssignmentStatusActive means the assignment is active.
	AdminAssignmentStatusActive AdminAssignmentStatus = iota
	// AdminAssignmentStatusPending means the assignment is pending approval.
	AdminAssignmentStatusPending
	// AdminAssignmentStatusSuspended means the assignment is suspended.
	AdminAssignmentStatusSuspended
	// AdminAssignmentStatusExpired means the assignment has expired.
	AdminAssignmentStatusExpired
	// AdminAssignmentStatusRevoked means the assignment was revoked.
	AdminAssignmentStatusRevoked
)

// String returns the string representation of the assignment status.
func (s AdminAssignmentStatus) String() string {
	switch s {
	case AdminAssignmentStatusActive:
		return "active"
	case AdminAssignmentStatusPending:
		return "pending"
	case AdminAssignmentStatusSuspended:
		return "suspended"
	case AdminAssignmentStatusExpired:
		return "expired"
	case AdminAssignmentStatusRevoked:
		return "revoked"
	default:
		return "unknown"
	}
}

// CreateAdminAssignmentRequest contains parameters for creating an admin assignment.
type CreateAdminAssignmentRequest struct {
	// AdminID is the user being granted admin privileges.
	AdminID string `json:"user_id"`

	// TenantID is the tenant context (if applicable).
	TenantID string `json:"tenant_id,omitempty"`

	// Scope is the administrative scope.
	Scope AdminScope `json:"admin_role"`

	// ScopeID is the ID of the scoped resource (tenant, org, group, etc.).
	ScopeID string `json:"scope_id,omitempty"`

	// Permissions are the specific permissions being granted.
	Permissions []string `json:"permissions,omitempty"`

	// RoleID is an optional predefined admin role.
	RoleID string `json:"role_id,omitempty"`

	// Constraints are optional constraints on the permissions.
	Constraints *AdminConstraints `json:"constraints,omitempty"`

	// ExpiresAt is when the assignment expires (optional).
	ExpiresAt time.Time `json:"expires_at"`

	// RequiresMFA indicates if MFA is required for this assignment.
	RequiresMFA bool `json:"requires_mfa,omitempty"`

	// RequiresApproval indicates if approval is needed.
	RequiresApproval bool `json:"requires_approval,omitempty"`

	// ApproverIDs are the IDs of users who can approve.
	ApproverIDs []string `json:"approver_ids,omitempty"`

	// GrantedBy is the user ID of the admin granting this assignment.
	GrantedBy string `json:"granted_by,omitempty"`

	// Justification is the reason for the assignment.
	Justification string `json:"justification,omitempty"`

	// Metadata contains additional assignment metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdateAdminAssignmentRequest contains parameters for updating an admin assignment.
type UpdateAdminAssignmentRequest struct {
	// Permissions are the new permissions (replaces existing).
	Permissions []string `json:"permissions,omitempty"`

	// Constraints are the new constraints.
	Constraints *AdminConstraints `json:"constraints,omitempty"`

	// ExpiresAt is the new expiration time.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// RequiresMFA is the new MFA requirement.
	RequiresMFA *bool `json:"requires_mfa,omitempty"`

	// Status is the new status.
	Status *AdminAssignmentStatus `json:"status,omitempty"`

	// Metadata contains updated metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// AdminConstraints defines constraints on administrative permissions.
type AdminConstraints struct {
	// TimeWindows limits when permissions are active.
	TimeWindows []TimeWindow `json:"time_windows,omitempty"`

	// IPAllowlist limits which IPs can use the permissions.
	IPAllowlist []string `json:"ip_allowlist,omitempty"`

	// MaxDailyOperations limits the number of daily operations.
	MaxDailyOperations int `json:"max_daily_operations,omitempty"`

	// RequireTicket requires a support ticket reference.
	RequireTicket bool `json:"require_ticket,omitempty"`

	// AllowedOperations limits to specific operations.
	AllowedOperations []string `json:"allowed_operations,omitempty"`

	// DeniedOperations explicitly denies specific operations.
	DeniedOperations []string `json:"denied_operations,omitempty"`

	// ResourceFilters limits which resources can be accessed.
	ResourceFilters []ResourceFilter `json:"resource_filters,omitempty"`
}

// TimeWindow defines a time window for permission validity.
type TimeWindow struct {
	// Start is the start time (in 24-hour format, e.g., "09:00").
	Start string `json:"start"`

	// End is the end time (in 24-hour format, e.g., "17:00").
	End string `json:"end"`

	// Days are the days of the week (0=Sunday, 6=Saturday).
	Days []int `json:"days,omitempty"`

	// Timezone is the timezone for the window.
	Timezone string `json:"timezone,omitempty"`
}

// ResourceFilter defines a filter for accessible resources.
type ResourceFilter struct {
	// Type is the resource type.
	Type string `json:"type"`

	// Pattern is a pattern to match resource IDs.
	Pattern string `json:"pattern"`

	// Tags are required resource tags.
	Tags map[string]string `json:"tags,omitempty"`
}

// AdminAssignment represents an administrative permission assignment.
type AdminAssignment struct {
	// ID is the unique identifier for this assignment.
	ID string `json:"id"`

	// AdminID is the user with admin privileges.
	AdminID string `json:"user_id"`

	// AdminName is the admin user's display name.
	AdminName string `json:"admin_name,omitempty"`

	// AdminEmail is the admin user's email.
	AdminEmail string `json:"admin_email,omitempty"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id,omitempty"`

	// Scope is the administrative scope.
	Scope AdminScope `json:"scope"`

	// ScopeID is the ID of the scoped resource.
	ScopeID string `json:"scope_id,omitempty"`

	// ScopeName is the name of the scoped resource.
	ScopeName string `json:"scope_name,omitempty"`

	// Permissions are the granted permissions.
	Permissions []string `json:"permissions,omitempty"`

	// RoleID is the predefined admin role (if used).
	RoleID string `json:"role_id,omitempty"`

	// RoleName is the admin role name.
	RoleName string `json:"role_name,omitempty"`

	// Constraints are the permission constraints.
	Constraints *AdminConstraints `json:"constraints,omitempty"`

	// Status is the assignment status.
	Status AdminAssignmentStatus `json:"status"`

	// Active indicates whether the assignment is currently active.
	Active bool `json:"active"`

	// RequiresMFA indicates if MFA is required.
	RequiresMFA bool `json:"requires_mfa,omitempty"`

	// Justification is the reason for the assignment.
	Justification string `json:"justification,omitempty"`

	// CreatedAt is when the assignment was created.
	CreatedAt time.Time `json:"created_at"`

	// CreatedBy is who created the assignment.
	CreatedBy string `json:"created_by,omitempty"`

	// UpdatedAt is when the assignment was last updated.
	UpdatedAt time.Time `json:"updated_at"`

	// UpdatedBy is who last updated the assignment.
	UpdatedBy string `json:"updated_by,omitempty"`

	// ExpiresAt is when the assignment expires.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// ApprovedAt is when the assignment was approved.
	ApprovedAt time.Time `json:"approved_at,omitempty"`

	// ApprovedBy is who approved the assignment.
	ApprovedBy string `json:"approved_by,omitempty"`

	// LastUsedAt is when the assignment was last used.
	LastUsedAt time.Time `json:"last_used_at,omitempty"`

	// Metadata contains assignment metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ListAdminAssignmentsRequest contains filter criteria for listing assignments.
type ListAdminAssignmentsRequest struct {
	// AdminID filters by admin user.
	AdminID string `json:"admin_id,omitempty"`

	// TenantID filters by tenant.
	TenantID string `json:"tenant_id,omitempty"`

	// Scope filters by scope type.
	Scope *AdminScope `json:"scope,omitempty"`

	// ScopeID filters by scope ID.
	ScopeID string `json:"scope_id,omitempty"`

	// Status filters by status.
	Status *AdminAssignmentStatus `json:"status,omitempty"`

	// IncludeExpired includes expired assignments.
	IncludeExpired bool `json:"include_expired,omitempty"`

	// Limit is the maximum number of results.
	Limit int `json:"limit,omitempty"`

	// Offset is the number of results to skip.
	Offset int `json:"offset,omitempty"`
}

// ListAdminAssignmentsResponse contains the list of admin assignments.
type ListAdminAssignmentsResponse struct {
	// Assignments is the list of admin assignments.
	Assignments []AdminAssignment `json:"assignments"`

	// TotalCount is the total number of assignments.
	TotalCount int `json:"total_count"`

	// HasMore indicates if there are more results.
	HasMore bool `json:"has_more"`
}

// AdminCheckRequest contains parameters for checking admin permissions.
type AdminCheckRequest struct {
	// AdminID is the admin user to check.
	AdminID string `json:"user_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id,omitempty"`

	// Permission is the permission to check.
	Permission string `json:"permission,omitempty"`

	// ResourceType is the resource type.
	ResourceType string `json:"resource_type,omitempty"`

	// ResourceID is the specific resource.
	ResourceID string `json:"resource_id,omitempty"`

	// Action is the action being performed.
	Action string `json:"action,omitempty"`
}

// AdminCheckResponse contains the permission check result.
type AdminCheckResponse struct {
	// Allowed indicates if the permission is granted.
	Allowed bool `json:"allowed"`

	// AssignmentID is the assignment that grants the permission.
	AssignmentID string `json:"assignment_id,omitempty"`

	// Scope is the scope that grants the permission.
	Scope AdminScope `json:"scope"`

	// ScopeID is the scope ID.
	ScopeID string `json:"scope_id,omitempty"`

	// Constraints are any applicable constraints.
	Constraints *AdminConstraints `json:"constraints,omitempty"`

	// RequiresMFA indicates if MFA is required.
	RequiresMFA bool `json:"requires_mfa,omitempty"`

	// DenialReason explains why permission was denied (if applicable).
	DenialReason string `json:"denial_reason,omitempty"`
}

// ImpersonationRequest contains parameters for starting impersonation.
//
// It carries no acting-admin identity: the actor is the authenticated
// caller, resolved server-side from the verified principal, never named by
// the request (go-quicbaas#383). A client cannot choose the admin it acts
// as.
type ImpersonationRequest struct {
	// TargetUserID is the user to impersonate.
	TargetUserID string `json:"target_user_id"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id,omitempty"`

	// Reason is the reason for impersonation.
	Reason string `json:"reason"`

	// TicketID is an optional support ticket reference.
	TicketID string `json:"ticket_id,omitempty"`

	// Duration is how long the impersonation should last.
	Duration time.Duration `json:"duration"`

	// Permissions limits the permissions during impersonation.
	Permissions []string `json:"permissions,omitempty"`

	// NotifyUser notifies the target user of impersonation.
	NotifyUser bool `json:"notify_user,omitempty"`
}

// ImpersonationSession represents an active impersonation session.
type ImpersonationSession struct {
	// ID is the unique session identifier.
	ID string `json:"id"`

	// AdminID is the admin performing impersonation.
	AdminID string `json:"user_id"`

	// AdminName is the admin's display name.
	AdminName string `json:"admin_name,omitempty"`

	// AdminEmail is the admin's email.
	AdminEmail string `json:"admin_email,omitempty"`

	// TargetUserID is the user being impersonated.
	TargetUserID string `json:"target_user_id"`

	// TargetUserName is the target user's display name.
	TargetUserName string `json:"target_user_name,omitempty"`

	// TargetUserEmail is the target user's email.
	TargetUserEmail string `json:"target_user_email,omitempty"`

	// TenantID is the tenant context.
	TenantID string `json:"tenant_id,omitempty"`

	// Reason is the reason for impersonation.
	Reason string `json:"reason"`

	// TicketID is the support ticket reference.
	TicketID string `json:"ticket_id,omitempty"`

	// Permissions are the limited permissions.
	Permissions []string `json:"permissions,omitempty"`

	// StartedAt is when impersonation started.
	StartedAt time.Time `json:"started_at"`

	// ExpiresAt is when impersonation expires.
	ExpiresAt time.Time `json:"expires_at"`

	// EndedAt is when impersonation ended (if ended).
	EndedAt time.Time `json:"ended_at,omitempty"`

	// IPAddress is the admin's IP address.
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent is the admin's user agent.
	UserAgent string `json:"user_agent,omitempty"`

	// Active indicates if the session is active.
	Active bool `json:"active"`
}

// UnmarshalJSON accepts the acting admin under either JSON key. The gRPC
// path maps the actor onto AdminID programmatically, but the REST and MCP
// transports return the server's domain iam.ImpersonationSession, which
// serializes the actor as "admin_user_id" rather than the SDK field's
// "user_id" tag. Without this alias the actor would be dropped on those
// transports, so the acting admin (the authenticated caller resolved by
// go-quicbaas#383) is populated consistently on every transport.
func (s *ImpersonationSession) UnmarshalJSON(data []byte) error {
	type alias ImpersonationSession
	aux := &struct {
		AdminUserIDAlias string `json:"admin_user_id"`
		*alias
	}{alias: (*alias)(s)}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if s.AdminID == "" && aux.AdminUserIDAlias != "" {
		s.AdminID = aux.AdminUserIDAlias
	}
	return nil
}

// ListImpersonationsResponse contains the list of impersonation sessions.
type ListImpersonationsResponse struct {
	// Sessions is the list of impersonation sessions.
	Sessions []ImpersonationSession `json:"sessions"`

	// TotalCount is the total number of sessions.
	TotalCount int `json:"total_count"`
}

// Internal request/response types for transport.

type createAdminAssignmentRequest struct {
	*CreateAdminAssignmentRequest
}

type getAdminAssignmentRequest struct {
	AssignmentID string `json:"assignment_id"`
}

type updateAdminAssignmentRequest struct {
	AssignmentID string                        `json:"assignment_id"`
	Update       *UpdateAdminAssignmentRequest `json:"update"`
}

type deleteAdminAssignmentRequest struct {
	AssignmentID string `json:"assignment_id"`
}

type deleteAdminAssignmentResponse struct{}

type listAdminAssignmentsRequest struct {
	Filter *ListAdminAssignmentsRequest `json:"filter"`
}

type adminCheckRequest struct {
	Check *AdminCheckRequest `json:"check"`
}

type startImpersonationRequest struct {
	Impersonation *ImpersonationRequest `json:"impersonation"`
}

type endImpersonationRequest struct {
	SessionID string `json:"session_id"`
}

type endImpersonationResponse struct{}

type listImpersonationsRequest struct {
	AdminUserID string `json:"admin_user_id"`
}

// adminClient implements the AdminClient interface.
type adminClient struct {
	transport transport.Transport
}

// NewAdminClient creates a new delegated administration client.
//
// Returns nil if the transport is nil.
func NewAdminClient(t transport.Transport) AdminClient {
	if t == nil {
		return nil
	}
	return &adminClient{
		transport: t,
	}
}

// CreateAssignment creates a new administrative assignment.
func (c *adminClient) CreateAssignment(ctx context.Context, req *CreateAdminAssignmentRequest) (*AdminAssignment, error) {
	if err := validateCreateAdminAssignmentRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.admin.CreateAssignment", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "CreateAssignment", &createAdminAssignmentRequest{
		CreateAdminAssignmentRequest: req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.admin.CreateAssignment", "nil response from server")
	}

	// If already the expected type, return directly.
	if assignment, ok := resp.(*AdminAssignment); ok {
		return assignment, nil
	}

	// Handle map[string]interface{} response from REST transport.
	return convertMapToAdminAssignment(resp, "iam.admin.CreateAssignment")
}

// GetAssignment retrieves an administrative assignment by ID.
func (c *adminClient) GetAssignment(ctx context.Context, assignmentID string) (*AdminAssignment, error) {
	if assignmentID == "" {
		return nil, errors.NewInvalidInput("iam.admin.GetAssignment", "assignment ID is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.admin.GetAssignment", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "GetAssignment", &getAdminAssignmentRequest{
		AssignmentID: assignmentID,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewNotFound("iam.admin.GetAssignment", "assignment not found")
	}

	// If already the expected type, return directly.
	if assignment, ok := resp.(*AdminAssignment); ok {
		return assignment, nil
	}

	// Handle map[string]interface{} response from REST transport.
	return convertMapToAdminAssignment(resp, "iam.admin.GetAssignment")
}

// UpdateAssignment modifies an existing administrative assignment.
func (c *adminClient) UpdateAssignment(ctx context.Context, assignmentID string, req *UpdateAdminAssignmentRequest) (*AdminAssignment, error) {
	if assignmentID == "" {
		return nil, errors.NewInvalidInput("iam.admin.UpdateAssignment", "assignment ID is required")
	}
	if req == nil {
		return nil, errors.NewInvalidInput("iam.admin.UpdateAssignment", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.admin.UpdateAssignment", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "UpdateAssignment", &updateAdminAssignmentRequest{
		AssignmentID: assignmentID,
		Update:       req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.admin.UpdateAssignment", "nil response from server")
	}

	// If already the expected type, return directly.
	if assignment, ok := resp.(*AdminAssignment); ok {
		return assignment, nil
	}

	// Handle map[string]interface{} response from REST transport.
	return convertMapToAdminAssignment(resp, "iam.admin.UpdateAssignment")
}

// DeleteAssignment removes an administrative assignment.
func (c *adminClient) DeleteAssignment(ctx context.Context, assignmentID string) error {
	if assignmentID == "" {
		return errors.NewInvalidInput("iam.admin.DeleteAssignment", "assignment ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.admin.DeleteAssignment", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "DeleteAssignment", &deleteAdminAssignmentRequest{
		AssignmentID: assignmentID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	// Accept *deleteAdminAssignmentResponse or map response from REST transport.
	if _, ok := resp.(*deleteAdminAssignmentResponse); ok {
		return nil
	}
	if _, ok := resp.(map[string]interface{}); ok {
		return nil
	}

	return errors.NewInternal("iam.admin.DeleteAssignment", "invalid response type")
}

// ListAssignments returns administrative assignments matching the filter.
func (c *adminClient) ListAssignments(ctx context.Context, req *ListAdminAssignmentsRequest) (*ListAdminAssignmentsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.admin.ListAssignments", "context is required")
	}

	// Set reasonable defaults
	if req == nil {
		req = &ListAdminAssignmentsRequest{}
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "ListAssignments", &listAdminAssignmentsRequest{
		Filter: req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListAdminAssignmentsResponse{Assignments: []AdminAssignment{}}, nil
	}

	if listResp, ok := resp.(*ListAdminAssignmentsResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[ListAdminAssignmentsResponse](resp, "iam.admin.ListAssignments")
}

// CheckPermission checks if an admin has a specific permission.
func (c *adminClient) CheckPermission(ctx context.Context, req *AdminCheckRequest) (*AdminCheckResponse, error) {
	if err := validateAdminCheckRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.admin.CheckPermission", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "CheckPermission", &adminCheckRequest{
		Check: req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.admin.CheckPermission", "nil response from server")
	}

	if checkResp, ok := resp.(*AdminCheckResponse); ok {
		return checkResp, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[AdminCheckResponse](resp, "iam.admin.CheckPermission")
}

// StartImpersonation begins an impersonation session.
func (c *adminClient) StartImpersonation(ctx context.Context, req *ImpersonationRequest) (*ImpersonationSession, error) {
	if err := validateImpersonationRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.admin.StartImpersonation", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "StartImpersonation", &startImpersonationRequest{
		Impersonation: req,
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("iam.admin.StartImpersonation", "nil response from server")
	}

	if session, ok := resp.(*ImpersonationSession); ok {
		return session, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[ImpersonationSession](resp, "iam.admin.StartImpersonation")
}

// EndImpersonation ends an impersonation session.
func (c *adminClient) EndImpersonation(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return errors.NewInvalidInput("iam.admin.EndImpersonation", "session ID is required")
	}

	if ctx == nil {
		return errors.NewInvalidInput("iam.admin.EndImpersonation", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "EndImpersonation", &endImpersonationRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	// Accept *endImpersonationResponse or map response from REST transport.
	if _, ok := resp.(*endImpersonationResponse); ok {
		return nil
	}
	if _, ok := resp.(map[string]interface{}); ok {
		return nil
	}

	return errors.NewInternal("iam.admin.EndImpersonation", "invalid response type")
}

// ListActiveImpersonations returns all active impersonation sessions.
func (c *adminClient) ListActiveImpersonations(ctx context.Context, adminUserID string) (*ListImpersonationsResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("iam.admin.ListActiveImpersonations", "context is required")
	}

	resp, err := c.transport.Request(ctx, adminServiceName, "ListActiveImpersonations", &listImpersonationsRequest{AdminUserID: adminUserID})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListImpersonationsResponse{Sessions: []ImpersonationSession{}}, nil
	}

	if listResp, ok := resp.(*ListImpersonationsResponse); ok {
		return listResp, nil
	}

	// Handle map[string]interface{} response from REST/MCP transports.
	return convertMapResponse[ListImpersonationsResponse](resp, "iam.admin.ListActiveImpersonations")
}

// convertMapToAdminAssignment converts a map response to an AdminAssignment
// using JSON round-trip. This handles responses from the REST transport where
// doRequest returns map[string]interface{} from JSON unmarshaling.
func convertMapToAdminAssignment(resp interface{}, context string) (*AdminAssignment, error) {
	respMap, ok := resp.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal(context, "invalid response type")
	}

	data, err := json.Marshal(respMap)
	if err != nil {
		return nil, errors.NewInternal(context, "failed to marshal response map")
	}

	assignment := &AdminAssignment{}
	if err := json.Unmarshal(data, assignment); err != nil {
		return nil, errors.NewInternal(context, "failed to unmarshal response to AdminAssignment")
	}

	return assignment, nil
}

// Validation functions.

func validateCreateAdminAssignmentRequest(req *CreateAdminAssignmentRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.admin.CreateAssignment", "request is required")
	}
	if req.AdminID == "" {
		return errors.NewInvalidInput("iam.admin.CreateAssignment", "admin ID is required")
	}
	if len(req.Permissions) == 0 && req.RoleID == "" {
		return errors.NewInvalidInput("iam.admin.CreateAssignment", "permissions or role ID is required")
	}
	if req.Scope != AdminScopeGlobal && req.ScopeID == "" {
		return errors.NewInvalidInput("iam.admin.CreateAssignment", "scope ID is required for non-global scope")
	}
	return nil
}

func validateAdminCheckRequest(req *AdminCheckRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.admin.CheckPermission", "request is required")
	}
	if req.AdminID == "" {
		return errors.NewInvalidInput("iam.admin.CheckPermission", "admin ID is required")
	}
	if req.Permission == "" && req.Action == "" {
		return errors.NewInvalidInput("iam.admin.CheckPermission", "permission or action is required")
	}
	return nil
}

func validateImpersonationRequest(req *ImpersonationRequest) error {
	if req == nil {
		return errors.NewInvalidInput("iam.admin.StartImpersonation", "request is required")
	}
	if req.TargetUserID == "" {
		return errors.NewInvalidInput("iam.admin.StartImpersonation", "target user ID is required")
	}
	if req.Reason == "" {
		return errors.NewInvalidInput("iam.admin.StartImpersonation", "reason is required")
	}
	if req.Duration <= 0 {
		return errors.NewInvalidInput("iam.admin.StartImpersonation", "duration must be positive")
	}
	return nil
}
