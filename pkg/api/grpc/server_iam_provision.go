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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ProvisionTenant creates a tenant and its first admin: the sysadmin-driven
// onboarding path used when public registration is off.
func (s *IAMServer) ProvisionTenant(ctx context.Context, req *iampb.ProvisionTenantRequest) (*iampb.ProvisionTenantResponse, error) {
	s.logger.WithField("tenant_id", req.TenantId).Info("Processing ProvisionTenant request")

	if req.TenantId == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id is required")
	}

	res, err := s.iamService.ProvisionTenant(ctx, &service.IAMProvisionTenantRequest{
		TenantID:          req.TenantId,
		Name:              req.Name,
		IsolationType:     qrdbsdk.TenantIsolationType(req.IsolationType),
		AdminUsername:     req.AdminUsername,
		AdminEnrollment:   service.IAMTenantAdminEnrollment(req.AdminEnrollment),
		AdminCSRPEM:       req.AdminCsrPem,
		InviteExpiryHours: int(req.InviteExpiryHours),
	})
	if err != nil {
		return nil, convertIAMError(err)
	}

	out := &iampb.ProvisionTenantResponse{
		TenantId:            res.TenantID,
		AdminUserId:         formatUserID(res.AdminUserID),
		AdminUsername:       res.AdminUsername,
		AdminRoleId:         res.AdminRoleID,
		AdminPending:        res.AdminPending,
		AdminCertPem:        res.AdminCertPEM,
		AdminCertThumbprint: res.AdminCertThumbprint,
		InviteToken:         res.InviteToken,
		Timestamp:           timestamppb.New(res.Timestamp),
	}
	if res.InviteExpiresAt != nil {
		out.InviteExpiresAt = timestamppb.New(*res.InviteExpiresAt)
	}
	return out, nil
}

// SetTenantProvisioner wires the control-plane tenant provisioner. It is
// late-bound, like SetWebAuthnCredentials, so the constructor keeps its
// signature; nil leaves ProvisionTenant reporting the service unconfigured.
func (a *IAMServiceAdapter) SetTenantProvisioner(p service.TenantProvisioner) {
	a.provisioner = p
}

// ProvisionTenant delegates to the tenant provisioner, which validates the
// request before any write.
func (a *IAMServiceAdapter) ProvisionTenant(ctx context.Context, req *service.IAMProvisionTenantRequest) (*service.IAMProvisionTenantResponse, error) {
	if a.provisioner == nil {
		return nil, api.NewServiceNotConfiguredError("tenant provisioner")
	}
	return a.provisioner.Provision(ctx, req)
}
