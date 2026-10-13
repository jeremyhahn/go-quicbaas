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
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc/codes"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestIAMHandlers_ResponseOutOfRange proves that an IAM count or setting too
// large for its int32 wire field becomes codes.Internal instead of a wrapped
// integer.
func TestIAMHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	hugeTenant := &iam.Tenant{ID: "t1", Settings: iam.TenantSettings{MaxUsers: tooBig}}
	hugePolicy := &iam.Policy{Metadata: iam.PolicyMetadata{Name: "p1", Priority: tooBig}}
	hugeDeletion := &iam.DataDeletionRequest{
		ID:                 "d1",
		DeletedDataSummary: &iam.DeletionSummary{TokensRevoked: tooBig},
	}

	tests := []struct {
		name  string
		field string
		setup func(svc *mockIAMService)
		call  func(srv *IAMServer) error
	}{
		{
			name:  "GetTenant max users",
			field: "max_users",
			setup: func(svc *mockIAMService) {
				svc.On("GetTenant", mock.Anything, "t1").Return(hugeTenant, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.GetTenant(ctx, &iampb.GetTenantRequest{Id: "t1"})
				return err
			},
		},
		{
			name:  "ListTenants max users",
			field: "max_users",
			setup: func(svc *mockIAMService) {
				svc.On("ListTenants", mock.Anything, mock.Anything, mock.Anything).
					Return([]*iam.Tenant{hugeTenant}, 1, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListTenants(ctx, &iampb.ListTenantsRequest{})
				return err
			},
		},
		{
			name:  "ListTenants total",
			field: "total",
			setup: func(svc *mockIAMService) {
				svc.On("ListTenants", mock.Anything, mock.Anything, mock.Anything).
					Return([]*iam.Tenant{}, tooBig, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListTenants(ctx, &iampb.ListTenantsRequest{})
				return err
			},
		},
		{
			name:  "ListUsers total",
			field: "total",
			setup: func(svc *mockIAMService) {
				svc.On("ListUsers", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return([]*iam.User{}, tooBig, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListUsers(ctx, &iampb.ListUsersRequest{TenantId: "t1"})
				return err
			},
		},
		{
			name:  "ListRoles total",
			field: "total",
			setup: func(svc *mockIAMService) {
				svc.On("ListRoles", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return([]*iam.Role{}, tooBig, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListRoles(ctx, &iampb.ListRolesRequest{TenantId: "t1"})
				return err
			},
		},
		{
			name:  "ListApps total",
			field: "total",
			setup: func(svc *mockIAMService) {
				svc.On("ListApps", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return([]*iam.Application{}, tooBig, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListApps(ctx, &iampb.ListAppsRequest{TenantId: "t1"})
				return err
			},
		},
		{
			name:  "GetPolicy priority",
			field: "priority",
			setup: func(svc *mockIAMService) {
				svc.On("GetPolicy", mock.Anything, "t1", "p1").Return(hugePolicy, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.GetPolicy(ctx, &iampb.GetPolicyRequest{TenantId: "t1", PolicyName: "p1"})
				return err
			},
		},
		{
			name:  "ListPolicies priority",
			field: "priority",
			setup: func(svc *mockIAMService) {
				svc.On("ListPolicies", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return([]*iam.Policy{hugePolicy}, 1, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListPolicies(ctx, &iampb.ListPoliciesRequest{TenantId: "t1"})
				return err
			},
		},
		{
			name:  "ListPolicies total",
			field: "total",
			setup: func(svc *mockIAMService) {
				svc.On("ListPolicies", mock.Anything, mock.Anything, mock.Anything, mock.Anything).
					Return([]*iam.Policy{}, tooBig, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.ListPolicies(ctx, &iampb.ListPoliciesRequest{TenantId: "t1"})
				return err
			},
		},
		{
			name:  "GetDeletionStatus summary",
			field: "tokens_revoked",
			setup: func(svc *mockIAMService) {
				svc.On("GetDeletionStatus", mock.Anything, "d1").Return(hugeDeletion, nil)
			},
			call: func(srv *IAMServer) error {
				_, err := srv.GetDeletionStatus(ctx, &iampb.GetDeletionStatusRequest{DeletionId: "d1"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, svc := newTestIAMServer(t)
			tt.setup(svc)
			requireRangeStatus(t, tt.call(srv), codes.Internal, tt.field)
		})
	}
}

// TestIAMConverters_ResponseOutOfRange proves that every narrowed field of
// the GDPR export and deletion converters is checked, including values
// nested below the top-level message.
func TestIAMConverters_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1

	tests := []struct {
		name    string
		field   string
		summary iam.DeletionSummary
	}{
		{name: "sessions deleted", field: "sessions_deleted", summary: iam.DeletionSummary{SessionsDeleted: tooBig}},
		{name: "audit logs anonymized", field: "audit_logs_anonymized", summary: iam.DeletionSummary{AuditLogsAnonymized: tooBig}},
		{name: "consents deleted", field: "consents_deleted", summary: iam.DeletionSummary{ConsentsDeleted: tooBig}},
		{name: "credentials deleted", field: "credentials_deleted", summary: iam.DeletionSummary{CredentialsDeleted: tooBig}},
		{name: "role assignments removed", field: "role_assignments_removed", summary: iam.DeletionSummary{RoleAssignmentsRemoved: tooBig}},
		{name: "tokens revoked", field: "tokens_revoked", summary: iam.DeletionSummary{TokensRevoked: tooBig}},
		{name: "additional data", field: "additional_data", summary: iam.DeletionSummary{AdditionalData: map[string]int{"x": tooBig}}},
	}

	for _, tt := range tests {
		t.Run("summary "+tt.name, func(t *testing.T) {
			result, err := convertDeletionSummaryToProto(&tt.summary)
			assert.Nil(t, result)
			requireRangeStatus(t, err, codes.Internal, tt.field)
		})
		t.Run("deletion request "+tt.name, func(t *testing.T) {
			result, err := convertDataDeletionRequestToProto(&iam.DataDeletionRequest{DeletedDataSummary: &tt.summary})
			assert.Nil(t, result)
			requireRangeStatus(t, err, codes.Internal, tt.field)
		})
	}

	t.Run("user data export backup codes", func(t *testing.T) {
		result, err := convertUserDataExportToProto(&iam.UserDataExport{
			MFAInfo: &iam.MFAInfoExport{BackupCodes: tooBig},
		})
		assert.Nil(t, result)
		requireRangeStatus(t, err, codes.Internal, "backup_codes_remaining")
	})
}
