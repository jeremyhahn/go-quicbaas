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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ggrpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/credentials"
	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// dynamicSecretsServiceFullName is the fully qualified proto service name the
// authz mapping and the registration guard both key on.
const dynamicSecretsServiceFullName = "quicbaas.dynamicsecrets.v1.DynamicSecretsService"

// TestRegisterDynamicSecretsServiceServer_ActuallyRegisters is the guard for
// the class of bug that left this service unreachable: the handlers were fully
// implemented but never bound to a listener, so every gRPC caller got
// codes.Unimplemented. Asserting the service name appears in GetServiceInfo
// fails loudly if the registration call is ever dropped again.
func TestRegisterDynamicSecretsServiceServer_ActuallyRegisters(t *testing.T) {
	gs := ggrpc.NewServer()
	t.Cleanup(gs.Stop)
	srv, _ := createTestServerWithMockService(t)

	RegisterDynamicSecretsServiceServer(gs, srv)

	info := gs.GetServiceInfo()
	svc, ok := info[dynamicSecretsServiceFullName]
	require.True(t, ok, "DynamicSecretsService must be registered on the listener; got %v", keysOf(info))

	got := make([]string, 0, len(svc.Methods))
	for _, m := range svc.Methods {
		got = append(got, m.Name)
	}
	assert.ElementsMatch(t, []string{
		"CreateRole", "GetRole", "ListRoles", "DeleteRole",
		"GenerateCredential", "RenewLease", "RevokeLease", "ListLeases",
	}, got, "all eight dynamic secrets RPCs must be reachable")
}

// TestDynamicSecretsPermissions_EveryMethodMapped pins the authz tuple for each
// RPC. gRPC is default-deny, so an unmapped method is unreachable; a mismapped
// one is a privilege bug.
func TestDynamicSecretsPermissions_EveryMethodMapped(t *testing.T) {
	want := map[string][2]string{
		"CreateRole":         {"dynamic_secrets", "role_create"},
		"GetRole":            {"dynamic_secrets", "role_read"},
		"ListRoles":          {"dynamic_secrets", "role_list"},
		"DeleteRole":         {"dynamic_secrets", "role_delete"},
		"GenerateCredential": {"dynamic_secrets", "generate"},
		"RenewLease":         {"dynamic_secrets", "lease_renew"},
		"RevokeLease":        {"dynamic_secrets", "lease_revoke"},
		"ListLeases":         {"dynamic_secrets", "lease_list"},
	}
	for _, m := range pb.DynamicSecretsService_ServiceDesc.Methods {
		full := "/" + dynamicSecretsServiceFullName + "/" + m.MethodName
		perm, ok := grpcMethodPermissions[full]
		require.True(t, ok, "%s has no permission mapping", full)

		expected, known := want[m.MethodName]
		require.True(t, known, "%s is not in the expected permission table", m.MethodName)
		assert.Equal(t, expected[0], perm.resource, "%s resource", full)
		assert.Equal(t, expected[1], perm.action, "%s action", full)
	}
}

// TestDynamicSecretsWrapper_DelegatesRoleVerbs proves the four renamed role
// verbs reach the *Server implementations. Without the wrapper these proto
// names would collide with the IAM role verbs and the service could not be
// registered at all.
func TestDynamicSecretsWrapper_DelegatesRoleVerbs(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
		CreateRoleFunc: func(_ context.Context, req *service.CreateRoleRequest) (*service.CreateRoleResponse, error) {
			return &service.CreateRoleResponse{
				Role: &credentials.Role{Name: req.Name, GeneratorType: req.GeneratorType},
			}, nil
		},
		GetRoleFunc: func(_ context.Context, req *service.GetRoleRequest) (*service.GetRoleResponse, error) {
			return &service.GetRoleResponse{Role: &credentials.Role{Name: req.Name}}, nil
		},
		ListRolesFunc: func(_ context.Context, _ *service.ListRolesRequest) (*service.ListRolesResponse, error) {
			return &service.ListRolesResponse{
				Roles: []*credentials.Role{{Name: "a"}, {Name: "b"}},
				Count: 2,
			}, nil
		},
		DeleteRoleFunc: func(_ context.Context, _ *service.DeleteRoleRequest) (*service.DeleteRoleResponse, error) {
			return &service.DeleteRoleResponse{DeletedAt: time.Unix(1700000000, 0)}, nil
		},
	}

	w := NewDynamicSecretsWrapper(srv)
	ctx := context.Background()

	createResp, err := w.CreateRole(ctx, &pb.DynSecretsCreateRoleRequest{
		Name:          "db-readonly",
		GeneratorType: "postgres",
		DefaultTtl:    durationpb.New(time.Hour),
		MaxTtl:        durationpb.New(24 * time.Hour),
	})
	require.NoError(t, err)
	assert.Equal(t, "db-readonly", createResp.GetRole().GetName())

	getResp, err := w.GetRole(ctx, &pb.DynSecretsGetRoleRequest{Name: "db-readonly"})
	require.NoError(t, err)
	assert.Equal(t, "db-readonly", getResp.GetRole().GetName())

	listResp, err := w.ListRoles(ctx, &pb.DynSecretsListRolesRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(2), listResp.GetCount())

	delResp, err := w.DeleteRole(ctx, &pb.DynSecretsDeleteRoleRequest{Name: "db-readonly"})
	require.NoError(t, err)
	assert.True(t, delResp.GetSuccess())
}

// TestDynamicSecretsWrapper_DelegatesLeaseVerbs covers the four
// credential/lease verbs, whose names already match the proto.
func TestDynamicSecretsWrapper_DelegatesLeaseVerbs(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	now := time.Unix(1700000000, 0)
	mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
		GenerateCredentialFunc: func(_ context.Context, req *service.GenerateCredentialRequest) (*service.GenerateCredentialResponse, error) {
			return &service.GenerateCredentialResponse{
				Credential: &credentials.Credential{
					CredentialID: "cred-1",
					Data:         map[string]string{"username": "u", "password": "p"},
				},
				Lease: &lease.Lease{
					LeaseID:    "lease-1",
					SecretPath: "postgres/creds/" + req.RoleName,
					IssuedAt:   now,
					ExpiresAt:  now.Add(time.Hour),
					TTL:        time.Hour,
					RenewCount: 2,
					Renewable:  true,
					Metadata:   map[string]string{"k": "v"},
				},
				LeaseID: "lease-1",
			}, nil
		},
		RenewLeaseFunc: func(_ context.Context, _ *service.RenewLeaseRequest) (*service.RenewLeaseResponse, error) {
			return &service.RenewLeaseResponse{
				Lease:     &lease.Lease{LeaseID: "lease-1", ExpiresAt: now.Add(2 * time.Hour)},
				RenewedAt: now,
			}, nil
		},
		RevokeLeaseFunc: func(_ context.Context, _ *service.RevokeLeaseRequest) (*service.RevokeLeaseResponse, error) {
			return &service.RevokeLeaseResponse{RevokedAt: now}, nil
		},
		ListLeasesFunc: func(_ context.Context, _ *service.ListLeasesRequest) (*service.ListLeasesResponse, error) {
			return &service.ListLeasesResponse{
				Leases: []*lease.Lease{{LeaseID: "lease-1"}},
				Count:  1,
			}, nil
		},
	}

	w := NewDynamicSecretsWrapper(srv)
	ctx := context.Background()

	genResp, err := w.GenerateCredential(ctx, &pb.GenerateCredentialRequest{RoleName: "db-readonly"})
	require.NoError(t, err)
	assert.Equal(t, "lease-1", genResp.GetLeaseId())

	// The three fields the proto gained so gRPC matches what REST already
	// returned: without them the CLI printed an empty credential ID and a
	// zero renew count.
	assert.Equal(t, "cred-1", genResp.GetCredential().GetCredentialId())
	assert.Equal(t, "postgres/creds/db-readonly", genResp.GetLease().GetSecretPath())
	assert.Equal(t, int32(2), genResp.GetLease().GetRenewCount())
	assert.Equal(t, map[string]string{"k": "v"}, genResp.GetLease().GetMetadata())

	renewResp, err := w.RenewLease(ctx, &pb.RenewLeaseRequest{LeaseId: "lease-1"})
	require.NoError(t, err)
	assert.Equal(t, "lease-1", renewResp.GetLease().GetLeaseId())

	revokeResp, err := w.RevokeLease(ctx, &pb.RevokeLeaseRequest{LeaseId: "lease-1"})
	require.NoError(t, err)
	assert.True(t, revokeResp.GetSuccess())

	listResp, err := w.ListLeases(ctx, &pb.ListLeasesRequest{})
	require.NoError(t, err)
	assert.Equal(t, int32(1), listResp.GetCount())
}

// TestDynamicSecretsWrapper_PropagatesErrors confirms the wrapper is a pure
// pass-through: validation and service failures surface with their original
// gRPC codes rather than being swallowed by the adapter.
func TestDynamicSecretsWrapper_PropagatesErrors(t *testing.T) {
	srv, mockSvc := createTestServerWithMockService(t)
	w := NewDynamicSecretsWrapper(srv)
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		_, err := w.CreateRole(ctx, nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("missing required field", func(t *testing.T) {
		_, err := w.GetRole(ctx, &pb.DynSecretsGetRoleRequest{Name: ""})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("engine not configured", func(t *testing.T) {
		_, err := w.ListLeases(ctx, &pb.ListLeasesRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Unimplemented, status.Code(err),
			"an absent dynamic secrets engine must report Unimplemented, not panic")
	})

	t.Run("service failure", func(t *testing.T) {
		mockSvc.DynamicSecretsSvc = &mocks.MockDynamicSecretsService{
			ListRolesFunc: func(_ context.Context, _ *service.ListRolesRequest) (*service.ListRolesResponse, error) {
				return nil, errors.New("backend down")
			},
		}
		_, err := w.ListRoles(ctx, &pb.DynSecretsListRolesRequest{})
		require.Error(t, err)
	})
}

// keysOf lists the registered service names for a readable failure message.
func keysOf(info map[string]ggrpc.ServiceInfo) []string {
	out := make([]string, 0, len(info))
	for k := range info {
		out = append(out, k)
	}
	return out
}
