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

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
)

// =============================================================================
// Dynamic Secrets Handlers
// =============================================================================
//
// These in-process handlers delegate to the service-layer DynamicSecretsService
// and return the leaf client/dynamicsecrets types the SDK client type-asserts
// on, matching what the REST / gRPC / QUIC / MCP transports return.

// dynamicSecretsService resolves the dynamic secrets sub-service, reporting a
// typed not-configured error when the deployment does not run the engine.
func dynamicSecretsService(svc service.QuicBaaS) (service.DynamicSecretsService, error) {
	if svc == nil {
		return nil, NewServiceNotConfiguredError("DynamicSecrets")
	}
	dynSvc := svc.DynamicSecrets()
	if dynSvc == nil {
		return nil, NewServiceNotConfiguredError("DynamicSecrets")
	}
	return dynSvc, nil
}

// newDynamicSecretsRequestTypeError builds the typed mismatch error for a
// dynamic secrets verb.
func newDynamicSecretsRequestTypeError(method, expected string, req interface{}) error {
	actual := "<nil>"
	if req != nil {
		actual = typeNameOf(req)
	}
	return NewInvalidRequestTypeError("quicbaas.DynamicSecrets", method, expected, actual)
}

func handleDynamicSecretsCreateRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*dsclient.CreateRoleRequest)
	if !ok || r == nil {
		return nil, newDynamicSecretsRequestTypeError("CreateRole",
			"*dynamicsecrets.CreateRoleRequest", req)
	}

	resp, err := dynSvc.CreateRole(ctx, &service.CreateRoleRequest{
		Name:          r.Name,
		GeneratorType: r.GeneratorType,
		Config:        r.Config,
		DefaultTTL:    r.DefaultTTL,
		MaxTTL:        r.MaxTTL,
	})
	if err != nil {
		return nil, err
	}
	return &dsclient.CreateRoleResponse{
		Role:      dynamicSecretsRoleFromService(resp.Role),
		CreatedAt: resp.CreatedAt,
	}, nil
}

func handleDynamicSecretsGetRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*dsclient.GetRoleRequest)
	if !ok || r == nil {
		return nil, newDynamicSecretsRequestTypeError("GetRole",
			"*dynamicsecrets.GetRoleRequest", req)
	}

	resp, err := dynSvc.GetRole(ctx, &service.GetRoleRequest{Name: r.Name})
	if err != nil {
		return nil, err
	}
	return &dsclient.GetRoleResponse{Role: dynamicSecretsRoleFromService(resp.Role)}, nil
}

func handleDynamicSecretsListRoles(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	svcReq := &service.ListRolesRequest{}
	if r, ok := req.(*dsclient.ListRolesRequest); ok && r != nil {
		svcReq.Prefix = r.Prefix
	}

	resp, err := dynSvc.ListRoles(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	roles := make([]*dsclient.Role, 0, len(resp.Roles))
	for _, role := range resp.Roles {
		roles = append(roles, dynamicSecretsRoleFromService(role))
	}
	return &dsclient.ListRolesResponse{Roles: roles, Count: resp.Count}, nil
}

func handleDynamicSecretsDeleteRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*dsclient.DeleteRoleRequest)
	if !ok || r == nil {
		return nil, newDynamicSecretsRequestTypeError("DeleteRole",
			"*dynamicsecrets.DeleteRoleRequest", req)
	}

	resp, err := dynSvc.DeleteRole(ctx, &service.DeleteRoleRequest{Name: r.Name})
	if err != nil {
		return nil, err
	}
	return &dsclient.DeleteRoleResponse{DeletedAt: resp.DeletedAt}, nil
}

func handleDynamicSecretsGenerateCredential(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*dsclient.GenerateCredentialRequest)
	if !ok || r == nil {
		return nil, newDynamicSecretsRequestTypeError("GenerateCredential",
			"*dynamicsecrets.GenerateCredentialRequest", req)
	}

	resp, err := dynSvc.GenerateCredential(ctx, &service.GenerateCredentialRequest{
		RoleName: r.RoleName,
		TTL:      r.TTL,
	})
	if err != nil {
		return nil, err
	}
	return &dsclient.GenerateCredentialResponse{
		Credential: dynamicSecretsCredentialFromService(resp.Credential),
		Lease:      dynamicSecretsLeaseFromService(resp.Lease),
		LeaseID:    resp.LeaseID,
	}, nil
}

func handleDynamicSecretsRenewLease(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*dsclient.RenewLeaseRequest)
	if !ok || r == nil {
		return nil, newDynamicSecretsRequestTypeError("RenewLease",
			"*dynamicsecrets.RenewLeaseRequest", req)
	}

	resp, err := dynSvc.RenewLease(ctx, &service.RenewLeaseRequest{
		LeaseID:   r.LeaseID,
		Increment: r.Increment,
	})
	if err != nil {
		return nil, err
	}
	return &dsclient.RenewLeaseResponse{
		Lease:     dynamicSecretsLeaseFromService(resp.Lease),
		RenewedAt: resp.RenewedAt,
	}, nil
}

func handleDynamicSecretsRevokeLease(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	r, ok := req.(*dsclient.RevokeLeaseRequest)
	if !ok || r == nil {
		return nil, newDynamicSecretsRequestTypeError("RevokeLease",
			"*dynamicsecrets.RevokeLeaseRequest", req)
	}

	resp, err := dynSvc.RevokeLease(ctx, &service.RevokeLeaseRequest{LeaseID: r.LeaseID})
	if err != nil {
		return nil, err
	}
	return &dsclient.RevokeLeaseResponse{RevokedAt: resp.RevokedAt}, nil
}

func handleDynamicSecretsListLeases(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	dynSvc, err := dynamicSecretsService(svc)
	if err != nil {
		return nil, err
	}
	svcReq := &service.ListLeasesRequest{}
	if r, ok := req.(*dsclient.ListLeasesRequest); ok && r != nil {
		svcReq.Prefix = r.Prefix
	}

	resp, err := dynSvc.ListLeases(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	leases := make([]*dsclient.Lease, 0, len(resp.Leases))
	for _, l := range resp.Leases {
		leases = append(leases, dynamicSecretsLeaseFromService(l))
	}
	return &dsclient.ListLeasesResponse{Leases: leases, Count: resp.Count}, nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

// dynamicSecretsRoleFromService converts the service-layer role into the leaf
// SDK role type.
func dynamicSecretsRoleFromService(r *credentials.Role) *dsclient.Role {
	if r == nil {
		return nil
	}
	return &dsclient.Role{
		Name:          r.Name,
		GeneratorType: r.GeneratorType,
		Config:        r.Config,
		DefaultTTL:    r.DefaultTTL,
		MaxTTL:        r.MaxTTL,
	}
}

// dynamicSecretsCredentialFromService converts the service-layer credential
// into the leaf SDK credential type.
func dynamicSecretsCredentialFromService(c *credentials.Credential) *dsclient.Credential {
	if c == nil {
		return nil
	}
	return &dsclient.Credential{
		CredentialID: c.CredentialID,
		Type:         c.Type,
		Data:         c.Data,
		LeaseID:      c.LeaseID,
		CreatedAt:    c.CreatedAt,
		ExpiresAt:    c.ExpiresAt,
	}
}

// dynamicSecretsLeaseFromService converts the service-layer lease into the leaf
// SDK lease type.
func dynamicSecretsLeaseFromService(l *lease.Lease) *dsclient.Lease {
	if l == nil {
		return nil
	}
	return &dsclient.Lease{
		LeaseID:     l.LeaseID,
		SecretPath:  l.SecretPath,
		IssuedAt:    l.IssuedAt,
		ExpiresAt:   l.ExpiresAt,
		TTL:         l.TTL,
		RenewCount:  l.RenewCount,
		MaxRenewals: l.MaxRenewals,
		Renewable:   l.Renewable,
		RevokedAt:   l.RevokedAt,
		Metadata:    l.Metadata,
	}
}
