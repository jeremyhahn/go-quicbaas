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
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// CreateDynamicSecretsRole creates a new credential generation role.
// Note: This is named differently from CreateRole to avoid conflict with IAM CreateRole in QuicBaaSServer.
// The DynamicSecretsService should be registered separately.
func (s *Server) CreateDynamicSecretsRole(ctx context.Context, req *pb.DynSecretsCreateRoleRequest) (*pb.DynSecretsCreateRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	if req.GeneratorType == "" {
		return nil, status.Error(codes.InvalidArgument, "generator_type is required")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.CreateRoleRequest{
		Name:          req.Name,
		GeneratorType: req.GeneratorType,
		Config:        req.Config,
		DefaultTTL:    req.DefaultTtl.AsDuration(),
		MaxTTL:        req.MaxTtl.AsDuration(),
	}

	// Call service
	resp, err := dynSecretsService.CreateRole(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.DynSecretsCreateRoleResponse{
		Role:      serviceRoleToProto(resp.Role),
		Timestamp: timestamppb.New(now),
	}, nil
}

// GetDynamicSecretsRole retrieves a credential generation role by name.
// Note: This is named differently from GetRole to avoid conflict with IAM methods in QuicBaaSServer.
func (s *Server) GetDynamicSecretsRole(ctx context.Context, req *pb.DynSecretsGetRoleRequest) (*pb.DynSecretsGetRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GetRoleRequest{
		Name: req.Name,
	}

	// Call service
	resp, err := dynSecretsService.GetRole(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.DynSecretsGetRoleResponse{
		Role:      serviceRoleToProto(resp.Role),
		Timestamp: timestamppb.New(now),
	}, nil
}

// ListDynamicSecretsRoles returns all credential generation roles.
// Note: This is named differently from ListRoles to avoid conflict with IAM methods in QuicBaaSServer.
func (s *Server) ListDynamicSecretsRoles(ctx context.Context, req *pb.DynSecretsListRolesRequest) (*pb.DynSecretsListRolesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.ListRolesRequest{
		Prefix: req.Prefix,
	}

	// Call service
	resp, err := dynSecretsService.ListRoles(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	// Convert service roles to proto roles
	protoRoles := make([]*pb.Role, len(resp.Roles))
	for i, role := range resp.Roles {
		protoRoles[i] = serviceRoleToProto(role)
	}

	return &pb.DynSecretsListRolesResponse{
		Roles:     protoRoles,
		Count:     int32(resp.Count), //nolint:gosec // G115: count is bounded
		Timestamp: timestamppb.New(now),
	}, nil
}

// DeleteDynamicSecretsRole removes a credential generation role.
// Note: This is named differently from DeleteRole to avoid conflict with IAM methods in QuicBaaSServer.
func (s *Server) DeleteDynamicSecretsRole(ctx context.Context, req *pb.DynSecretsDeleteRoleRequest) (*pb.DynSecretsDeleteRoleResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.DeleteRoleRequest{
		Name: req.Name,
	}

	// Call service
	_, err := dynSecretsService.DeleteRole(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.DynSecretsDeleteRoleResponse{
		Success:   true,
		Timestamp: timestamppb.New(now),
	}, nil
}

// GenerateCredential generates a new dynamic credential based on a role.
func (s *Server) GenerateCredential(ctx context.Context, req *pb.GenerateCredentialRequest) (*pb.GenerateCredentialResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.RoleName == "" {
		return nil, status.Error(codes.InvalidArgument, "role_name is required")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.GenerateCredentialRequest{
		RoleName: req.RoleName,
		TTL:      req.Ttl.AsDuration(),
	}

	// Call service
	resp, err := dynSecretsService.GenerateCredential(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	return &pb.GenerateCredentialResponse{
		Credential: serviceCredentialToProto(resp.Credential),
		Lease:      serviceLeaseToProto(resp.Lease),
		LeaseId:    resp.LeaseID,
		Timestamp:  timestamppb.New(now),
	}, nil
}

// RenewLease extends the expiration time of a credential lease.
func (s *Server) RenewLease(ctx context.Context, req *pb.RenewLeaseRequest) (*pb.RenewLeaseResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.LeaseId == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.RenewLeaseRequest{
		LeaseID:   req.LeaseId,
		Increment: req.Increment.AsDuration(),
	}

	// Call service
	resp, err := dynSecretsService.RenewLease(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RenewLeaseResponse{
		Lease:     serviceLeaseToProto(resp.Lease),
		Timestamp: timestamppb.New(resp.RenewedAt),
	}, nil
}

// RevokeLease revokes a credential lease immediately.
func (s *Server) RevokeLease(ctx context.Context, req *pb.RevokeLeaseRequest) (*pb.RevokeLeaseResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.LeaseId == "" {
		return nil, status.Error(codes.InvalidArgument, "lease_id is required")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.RevokeLeaseRequest{
		LeaseID: req.LeaseId,
	}

	// Call service
	resp, err := dynSecretsService.RevokeLease(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	return &pb.RevokeLeaseResponse{
		Success:   true,
		Timestamp: timestamppb.New(resp.RevokedAt),
	}, nil
}

// ListLeases returns all active leases.
func (s *Server) ListLeases(ctx context.Context, req *pb.ListLeasesRequest) (*pb.ListLeasesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get dynamic secrets service
	dynSecretsService := s.service.DynamicSecrets()
	if dynSecretsService == nil {
		return nil, status.Error(codes.Unimplemented, "dynamic secrets service not available")
	}

	// Convert proto request to service request
	serviceReq := &service.ListLeasesRequest{
		Prefix: req.Prefix,
	}

	// Call service
	resp, err := dynSecretsService.ListLeases(ctx, serviceReq)
	if err != nil {
		return nil, toProtoError(err)
	}

	now := time.Now()

	// Convert service leases to proto leases
	protoLeases := make([]*pb.Lease, len(resp.Leases))
	for i, l := range resp.Leases {
		protoLeases[i] = serviceLeaseToProto(l)
	}

	return &pb.ListLeasesResponse{
		Leases:    protoLeases,
		Count:     int32(resp.Count), //nolint:gosec // G115: count is bounded
		Timestamp: timestamppb.New(now),
	}, nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// serviceRoleToProto converts service credentials.Role to proto Role.
func serviceRoleToProto(role *credentials.Role) *pb.Role {
	if role == nil {
		return nil
	}

	return &pb.Role{
		Name:          role.Name,
		GeneratorType: role.GeneratorType,
		Config:        role.Config,
		DefaultTtl:    durationpb.New(role.DefaultTTL),
		MaxTtl:        durationpb.New(role.MaxTTL),
	}
}

// serviceCredentialToProto converts service credentials.Credential to proto Credential.
func serviceCredentialToProto(cred *credentials.Credential) *pb.Credential {
	if cred == nil {
		return nil
	}

	// Extract common credential fields from Data map
	protoCred := &pb.Credential{
		Metadata: cred.Data,
	}

	// Map common database credential fields
	if username, ok := cred.Data["username"]; ok {
		protoCred.Username = username
	}
	if password, ok := cred.Data["password"]; ok {
		protoCred.Password = password
	}
	if connectionString, ok := cred.Data["connection_string"]; ok {
		protoCred.ConnectionString = connectionString
	}
	if host, ok := cred.Data["host"]; ok {
		protoCred.Host = host
	}
	if portStr, ok := cred.Data["port"]; ok {
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port <= 65535 {
			protoCred.Port = int32(port) //nolint:gosec // port is bounded by check above (0-65535)
		}
	}
	if database, ok := cred.Data["database"]; ok {
		protoCred.Database = database
	}

	return protoCred
}

// serviceLeaseToProto converts service lease.Lease to proto Lease.
func serviceLeaseToProto(l *lease.Lease) *pb.Lease {
	if l == nil {
		return nil
	}

	protoLease := &pb.Lease{
		LeaseId:   l.LeaseID,
		RoleName:  l.SecretPath,
		State:     leaseStateToProto(l.State()),
		Ttl:       durationpb.New(l.RemainingTTL()),
		ExpiresAt: timestamppb.New(l.ExpiresAt),
		CreatedAt: timestamppb.New(l.IssuedAt),
		Renewable: l.Renewable,
		MaxTtl:    durationpb.New(l.TTL),
	}

	// Set renewed timestamp if available
	if l.RenewCount > 0 {
		// Approximate last renewal time
		protoLease.RenewedAt = timestamppb.New(l.IssuedAt)
	}

	return protoLease
}

// leaseStateToProto converts service lease.LeaseState to proto LeaseState.
func leaseStateToProto(state lease.LeaseState) pb.LeaseState {
	switch state {
	case lease.LeaseStateActive:
		return pb.LeaseState_LEASE_STATE_ACTIVE
	case lease.LeaseStateExpired:
		return pb.LeaseState_LEASE_STATE_EXPIRED
	case lease.LeaseStateRevoked:
		return pb.LeaseState_LEASE_STATE_REVOKED
	default:
		return pb.LeaseState_LEASE_STATE_UNSPECIFIED
	}
}
