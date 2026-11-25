// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// requireCallerTenant refuses a request whose tenant_id is not the caller's
// own tenant. The tenant_id field is caller-supplied, so it is honoured only
// as tenantctx.Target allows: a tenantless caller is refused, and the system
// tenant gets no lift into another tenant (#1150).
func requireCallerTenant(ctx context.Context, op, tenantID string) error {
	_, err := targetTenant(ctx, op, tenantID)
	return err
}

// targetTenant is requireCallerTenant for a handler that also needs the tenant
// it resolved: an empty request names the caller's own tenant, which is
// returned in its place.
func targetTenant(ctx context.Context, op, tenantID string) (string, error) {
	resolved, err := tenantctx.Target(ctx, op, tenantID)
	if err != nil {
		return "", toProtoError(err)
	}
	return resolved, nil
}

// callerTenantOf returns the tenant bound to the caller's context, refusing
// an unbound caller as tenantctx does.
func callerTenantOf(ctx context.Context, op string) (string, error) {
	caller, err := tenantctx.FromContext(ctx, op)
	if err != nil {
		return "", toProtoError(err)
	}
	return caller, nil
}

// grantableTenantIDs resolves the TenantIDs a caller may write into an
// admin assignment, mapped to a gRPC status on refusal.
func grantableTenantIDs(ctx context.Context, op string, requested []string) ([]string, error) {
	resolved, err := iam.GrantableTenantIDs(ctx, op, requested)
	if err != nil {
		return nil, toProtoError(err)
	}
	return resolved, nil
}

// ownAdminAssignment fetches an admin assignment and answers one outside the
// caller's tenant as not found.
func (s *IAMServer) ownAdminAssignment(ctx context.Context, caller, id string) (*iam.AdminAssignment, error) {
	assignment, err := s.iamService.GetAdminAssignment(ctx, id)
	if err != nil {
		return nil, convertIAMError(err)
	}
	if !iam.AssignmentInTenant(assignment, caller) {
		return nil, convertIAMError(iam.ErrAdminAssignmentNotFound)
	}
	return assignment, nil
}

// ownExport fetches a data export request and answers one outside the
// caller's tenant as not found.
func (s *IAMServer) ownExport(ctx context.Context, exportID string) (*iam.DataExportRequest, error) {
	caller, err := callerTenantOf(ctx, "grpc.IAM")
	if err != nil {
		return nil, err
	}
	exportReq, err := s.iamService.GetExportStatus(ctx, exportID)
	if err != nil {
		return nil, convertIAMError(err)
	}
	if exportReq == nil || exportReq.TenantID != caller {
		return nil, convertIAMError(iam.ErrDataExportNotFound)
	}
	return exportReq, nil
}

// ownDeletion fetches a data deletion request and answers one outside the
// caller's tenant as not found.
func (s *IAMServer) ownDeletion(ctx context.Context, deletionID string) (*iam.DataDeletionRequest, error) {
	caller, err := callerTenantOf(ctx, "grpc.IAM")
	if err != nil {
		return nil, err
	}
	deletionReq, err := s.iamService.GetDeletionStatus(ctx, deletionID)
	if err != nil {
		return nil, convertIAMError(err)
	}
	if deletionReq == nil || deletionReq.TenantID != caller {
		return nil, convertIAMError(iam.ErrDeletionNotFound)
	}
	return deletionReq, nil
}
