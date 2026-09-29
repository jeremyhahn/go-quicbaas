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
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	iam "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// adminRoleFromString maps CLI role name strings to proto AdminRole enum values.
var adminRoleFromString = map[string]int{
	"super_admin":  1,
	"tenant_admin": 2,
	"user_admin":   3,
	"policy_admin": 5,
	"audit_admin":  6,
	"read_only":    7,
}

// resolveAdminRole extracts the admin role from the request map, accepting
// either an integer enum value (from "admin_role" or "scope") or a string
// role name (from "role_id" or "admin_role").
func resolveAdminRole(reqMap map[string]interface{}) int {
	// Try integer first (direct enum value)
	if v, ok := getInt(reqMap, "admin_role"); ok && v > 0 {
		return v
	}
	if v, ok := getInt(reqMap, "scope"); ok && v > 0 {
		return v
	}
	// Try string role name
	for _, key := range []string{"role_id", "admin_role", "scope"} {
		if s, ok := getString(reqMap, key); ok && s != "" {
			if enumVal, found := adminRoleFromString[strings.ToLower(s)]; found {
				return enumVal
			}
		}
	}
	return 0
}

// handleCreateAdminAssignment handles CreateAdminAssignment requests.
func (h *iamHandler) handleCreateAdminAssignment(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCreateAdminAssignment", "invalid request type")
	}

	userID, _ := getString(reqMap, "user_id")
	adminRole := resolveAdminRole(reqMap)
	tenantIDs, _ := getStringSlice(reqMap, "tenant_ids")
	permissions, _ := getStringSlice(reqMap, "permissions")
	deniedPermissions, _ := getStringSlice(reqMap, "denied_permissions")
	grantedBy, _ := getString(reqMap, "granted_by")
	metadata, _ := getStringMap(reqMap, "metadata")

	pbReq := &iampb.CreateAdminAssignmentRequest{
		UserId:            userID,
		AdminRole:         iampb.AdminRole(adminRole),
		TenantIds:         tenantIDs,
		Permissions:       permissions,
		DeniedPermissions: deniedPermissions,
		GrantedBy:         grantedBy,
		Metadata:          metadata,
	}

	resp, err := client.CreateAdminAssignment(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCreateAdminAssignment")
	}

	return convertProtoAdminAssignmentToSDK(resp), nil
}

// handleGetAdminAssignment handles GetAdminAssignment requests.
func (h *iamHandler) handleGetAdminAssignment(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleGetAdminAssignment", "invalid request type")
	}

	id, _ := getString(reqMap, "id")
	if id == "" {
		id, _ = getString(reqMap, "assignment_id")
	}

	pbReq := &iampb.GetAdminAssignmentRequest{
		Id: id,
	}

	resp, err := client.GetAdminAssignment(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetAdminAssignment")
	}

	return convertProtoAdminAssignmentToSDK(resp), nil
}

// handleUpdateAdminAssignment handles UpdateAdminAssignment requests.
func (h *iamHandler) handleUpdateAdminAssignment(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleUpdateAdminAssignment", "invalid request type")
	}

	id, _ := getString(reqMap, "id")
	if id == "" {
		id, _ = getString(reqMap, "assignment_id")
	}
	adminRole, _ := getInt(reqMap, "admin_role")
	tenantIDs, _ := getStringSlice(reqMap, "tenant_ids")
	permissions, _ := getStringSlice(reqMap, "permissions")
	deniedPermissions, _ := getStringSlice(reqMap, "denied_permissions")
	active, _ := getBool(reqMap, "active")
	metadata, _ := getStringMap(reqMap, "metadata")

	// Check for nested "update" field from SDK request.
	if updateMap, ok := getMap(reqMap, "update"); ok {
		if p, ok := getStringSlice(updateMap, "permissions"); ok {
			permissions = p
		}
		if m, ok := getStringMap(updateMap, "metadata"); ok {
			metadata = m
		}
	}

	pbReq := &iampb.UpdateAdminAssignmentRequest{
		Id:                id,
		AdminRole:         iampb.AdminRole(adminRole),
		TenantIds:         tenantIDs,
		Permissions:       permissions,
		DeniedPermissions: deniedPermissions,
		Active:            active,
		Metadata:          metadata,
	}

	resp, err := client.UpdateAdminAssignment(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleUpdateAdminAssignment")
	}

	return convertProtoAdminAssignmentToSDK(resp), nil
}

// handleDeleteAdminAssignment handles DeleteAdminAssignment requests.
func (h *iamHandler) handleDeleteAdminAssignment(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleDeleteAdminAssignment", "invalid request type")
	}

	id, _ := getString(reqMap, "id")
	if id == "" {
		id, _ = getString(reqMap, "assignment_id")
	}

	pbReq := &iampb.DeleteAdminAssignmentRequest{
		Id: id,
	}

	_, err := client.DeleteAdminAssignment(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleDeleteAdminAssignment")
	}

	// Return nil to indicate success - the SDK client accepts nil response for delete.
	return nil, nil
}

// handleListAdminAssignments handles ListAdminAssignments requests.
func (h *iamHandler) handleListAdminAssignments(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleListAdminAssignments", "invalid request type")
	}

	// Check for nested "filter" field from SDK request.
	if filterMap, ok := getMap(reqMap, "filter"); ok {
		reqMap = filterMap
	}

	userID, _ := getString(reqMap, "user_id")
	if userID == "" {
		userID, _ = getString(reqMap, "admin_id")
	}
	tenantID, _ := getString(reqMap, "tenant_id")
	adminRole, _ := getInt(reqMap, "admin_role")
	activeOnly, _ := getBool(reqMap, "active_only")
	// The SDK sends a "status" field (int pointer) instead of "active_only".
	// AdminAssignmentStatusActive == 0, so when the status field is present
	// and equals 0, treat it as active-only filtering.
	if !activeOnly {
		if _, hasStatus := reqMap["status"]; hasStatus {
			statusVal, _ := getInt(reqMap, "status")
			if statusVal == 0 {
				activeOnly = true
			}
		}
	}
	pageSize, _ := getInt(reqMap, "page_size")
	if pageSize == 0 {
		pageSize, _ = getInt(reqMap, "limit")
	}

	pbReq := &iampb.ListAdminAssignmentsRequest{
		UserId:     userID,
		TenantId:   tenantID,
		AdminRole:  iampb.AdminRole(adminRole),
		ActiveOnly: activeOnly,
		Limit:      int32(pageSize),
	}

	resp, err := client.ListAdminAssignments(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleListAdminAssignments")
	}

	assignments := make([]iam.AdminAssignment, 0, len(resp.Assignments))
	for _, a := range resp.Assignments {
		if converted := convertProtoAdminAssignmentToSDK(a); converted != nil {
			assignments = append(assignments, *converted)
		}
	}

	return &iam.ListAdminAssignmentsResponse{
		Assignments: assignments,
		TotalCount:  int(resp.Total),
		HasMore:     len(assignments) > 0 && int(resp.Total) > len(assignments),
	}, nil
}

// handleCheckAdminPermission handles CheckAdminPermission requests.
func (h *iamHandler) handleCheckAdminPermission(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleCheckAdminPermission", "invalid request type")
	}

	// Check for nested "check" field from SDK request.
	if checkMap, ok := getMap(reqMap, "check"); ok {
		reqMap = checkMap
	}

	userID, _ := getString(reqMap, "user_id")
	if userID == "" {
		userID, _ = getString(reqMap, "admin_id")
	}
	permission, _ := getString(reqMap, "permission")
	tenantID, _ := getString(reqMap, "tenant_id")
	resourceType, _ := getString(reqMap, "resource_type")
	resourceID, _ := getString(reqMap, "resource_id")
	action, _ := getString(reqMap, "action")

	pbReq := &iampb.CheckAdminPermissionRequest{
		UserId:       userID,
		Permission:   permission,
		TenantId:     tenantID,
		ResourceType: resourceType,
		ResourceId:   resourceID,
	}

	resp, err := client.CheckAdminPermission(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleCheckAdminPermission")
	}

	// Map proto AdminRole to SDK AdminScope.
	scope := iam.AdminScopeGlobal
	if resp.AdminRole > 0 {
		scope = iam.AdminScope(resp.AdminRole)
	}

	denialReason := resp.Reason
	if denialReason == "" && !resp.Allowed {
		denialReason = "permission denied"
	}

	_ = action // action is used at the SDK level, not at the proto level

	return &iam.AdminCheckResponse{
		Allowed:      resp.Allowed,
		AssignmentID: resp.AssignmentId,
		Scope:        scope,
		DenialReason: denialReason,
	}, nil
}

// handleStartImpersonation handles StartImpersonation requests.
func (h *iamHandler) handleStartImpersonation(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleStartImpersonation", "invalid request type")
	}

	// Check for nested "impersonation" field from SDK request.
	if impMap, ok := getMap(reqMap, "impersonation"); ok {
		reqMap = impMap
	}

	// No admin_user_id: the acting admin is the authenticated caller,
	// resolved server-side from the verified principal, never forwarded
	// from the request (go-quicbaas#383).
	targetUserID, _ := getString(reqMap, "target_user_id")
	tenantID, _ := getString(reqMap, "tenant_id")
	reason, _ := getString(reqMap, "reason")
	durationSecs, _ := getInt(reqMap, "duration_seconds")

	// Also check for duration as a Go Duration value (nanoseconds).
	if durationSecs == 0 {
		if durationNs, ok := getInt(reqMap, "duration"); ok && durationNs > 0 {
			durationSecs = durationNs / int(time.Second)
		}
	}

	pbReq := &iampb.StartImpersonationRequest{
		TargetUserId: targetUserID,
		TenantId:     tenantID,
		Reason:       reason,
	}

	if durationSecs > 0 {
		pbReq.Duration = durationpb.New(time.Duration(durationSecs) * time.Second)
	}

	resp, err := client.StartImpersonation(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleStartImpersonation")
	}

	return convertProtoImpersonationSessionToSDK(resp), nil
}

// handleEndImpersonation handles EndImpersonation requests.
func (h *iamHandler) handleEndImpersonation(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		return nil, errors.NewInvalidInput("handleEndImpersonation", "invalid request type")
	}

	sessionID, _ := getString(reqMap, "session_id")

	pbReq := &iampb.EndImpersonationRequest{
		SessionId: sessionID,
	}

	_, err := client.EndImpersonation(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleEndImpersonation")
	}

	// Return nil to indicate success - the SDK client accepts nil response.
	return nil, nil
}

// handleGetActiveImpersonations handles GetActiveImpersonations requests.
func (h *iamHandler) handleGetActiveImpersonations(ctx context.Context, client iampb.IAMServiceClient, req interface{}) (interface{}, error) {
	reqMap, ok := toMap(req)
	if !ok {
		// Empty request is valid for listing all active impersonations.
		reqMap = make(map[string]interface{})
	}

	adminUserID, _ := getString(reqMap, "admin_user_id")

	pbReq := &iampb.GetActiveImpersonationsRequest{
		AdminUserId: adminUserID,
	}

	resp, err := client.GetActiveImpersonations(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "handleGetActiveImpersonations")
	}

	sessions := make([]iam.ImpersonationSession, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		if converted := convertProtoImpersonationSessionToSDK(s); converted != nil {
			sessions = append(sessions, *converted)
		}
	}

	return &iam.ListImpersonationsResponse{
		Sessions:   sessions,
		TotalCount: len(sessions),
	}, nil
}

// convertProtoAdminAssignmentToSDK converts a protobuf AdminAssignment to SDK AdminAssignment.
func convertProtoAdminAssignmentToSDK(a *iampb.AdminAssignment) *iam.AdminAssignment {
	if a == nil {
		return nil
	}

	resp := &iam.AdminAssignment{
		ID:          a.Id,
		AdminID:     a.UserId,
		TenantID:    "",
		Scope:       iam.AdminScope(a.AdminRole),
		Permissions: a.Permissions,
		RoleID:      "",
		Status:      iam.AdminAssignmentStatusActive,
		Active:      a.Active,
		RequiresMFA: false,
		CreatedBy:   a.GrantedBy,
		Metadata:    a.Metadata,
	}

	// Set tenant ID from tenant IDs list if available.
	if len(a.TenantIds) > 0 {
		resp.TenantID = a.TenantIds[0]
	}

	// Set status based on active flag.
	if !a.Active {
		resp.Status = iam.AdminAssignmentStatusSuspended
	}

	if a.ExpiresAt != nil {
		resp.ExpiresAt = a.ExpiresAt.AsTime()
	}
	if a.GrantedAt != nil {
		resp.CreatedAt = a.GrantedAt.AsTime()
		resp.UpdatedAt = a.GrantedAt.AsTime()
	}
	if a.LastUsedAt != nil {
		resp.LastUsedAt = a.LastUsedAt.AsTime()
	}

	return resp
}

// convertProtoImpersonationSessionToSDK converts a protobuf ImpersonationSession to SDK ImpersonationSession.
func convertProtoImpersonationSessionToSDK(s *iampb.ImpersonationSession) *iam.ImpersonationSession {
	if s == nil {
		return nil
	}

	resp := &iam.ImpersonationSession{
		ID:           s.Id,
		AdminID:      s.AdminUserId,
		TargetUserID: s.TargetUserId,
		TenantID:     s.TenantId,
		Reason:       s.Reason,
		Active:       true,
	}

	if s.StartedAt != nil {
		resp.StartedAt = s.StartedAt.AsTime()
	}
	if s.ExpiresAt != nil {
		resp.ExpiresAt = s.ExpiresAt.AsTime()
	}
	if s.EndedAt != nil {
		resp.EndedAt = s.EndedAt.AsTime()
		resp.Active = false
	}

	return resp
}

// protoTimestampPtr converts a time.Time pointer to protobuf Timestamp.
func protoTimestampPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(*t)
}
