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

	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestDynamicSecretsHandlers_ResponseOutOfRange proves that a role or lease
// count, or a lease renewal count, too large for its int32 wire field becomes
// codes.Internal instead of a wrapped integer.
func TestDynamicSecretsHandlers_ResponseOutOfRange(t *testing.T) {
	const tooBig = math.MaxInt32 + 1
	ctx := context.Background()
	hugeLease := &lease.Lease{LeaseID: "l1", RenewCount: tooBig}

	tests := []struct {
		name  string
		field string
		svc   *mocks.MockDynamicSecretsService
		call  func(srv *Server) error
	}{
		{
			name:  "ListDynamicSecretsRoles count",
			field: "count",
			svc: &mocks.MockDynamicSecretsService{
				ListRolesFunc: func(context.Context, *service.ListRolesRequest) (*service.ListRolesResponse, error) {
					return &service.ListRolesResponse{Count: tooBig}, nil
				},
			},
			call: func(srv *Server) error {
				_, err := srv.ListDynamicSecretsRoles(ctx, &pb.DynSecretsListRolesRequest{})
				return err
			},
		},
		{
			name:  "ListLeases count",
			field: "count",
			svc: &mocks.MockDynamicSecretsService{
				ListLeasesFunc: func(context.Context, *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
					return &service.ListLeasesResponse{Count: tooBig}, nil
				},
			},
			call: func(srv *Server) error {
				_, err := srv.ListLeases(ctx, &pb.ListLeasesRequest{})
				return err
			},
		},
		{
			name:  "ListLeases renew count",
			field: "renew_count",
			svc: &mocks.MockDynamicSecretsService{
				ListLeasesFunc: func(context.Context, *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
					return &service.ListLeasesResponse{Leases: []*lease.Lease{hugeLease}, Count: 1}, nil
				},
			},
			call: func(srv *Server) error {
				_, err := srv.ListLeases(ctx, &pb.ListLeasesRequest{})
				return err
			},
		},
		{
			name:  "GenerateCredential renew count",
			field: "renew_count",
			svc: &mocks.MockDynamicSecretsService{
				GenerateCredentialFunc: func(context.Context, *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
					return &service.GenerateCredentialResponse{Lease: hugeLease, LeaseID: "l1"}, nil
				},
			},
			call: func(srv *Server) error {
				_, err := srv.GenerateCredential(ctx, &pb.GenerateCredentialRequest{RoleName: "r1"})
				return err
			},
		},
		{
			name:  "RenewLease renew count",
			field: "renew_count",
			svc: &mocks.MockDynamicSecretsService{
				RenewLeaseFunc: func(context.Context, *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
					return &service.RenewLeaseResponse{Lease: hugeLease}, nil
				},
			},
			call: func(srv *Server) error {
				_, err := srv.RenewLease(ctx, &pb.RenewLeaseRequest{LeaseId: "l1"})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestServerWithMockService(t)
			mockSvc.DynamicSecretsSvc = tt.svc
			requireRangeStatus(t, tt.call(srv), codes.Internal, tt.field)
		})
	}
}
