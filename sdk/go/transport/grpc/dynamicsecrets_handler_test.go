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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dynamicsecrets"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// fakeDynamicSecretsServer records the last request per verb and replies with
// a canned proto response so the handler's conversions can be asserted in
// isolation.
type fakeDynamicSecretsServer struct {
	lastCreate   *pb.DynSecretsCreateRoleRequest
	lastGet      *pb.DynSecretsGetRoleRequest
	lastList     *pb.DynSecretsListRolesRequest
	lastDelete   *pb.DynSecretsDeleteRoleRequest
	lastGenerate *pb.GenerateCredentialRequest
	lastRenew    *pb.RenewLeaseRequest
	lastRevoke   *pb.RevokeLeaseRequest
	lastLeases   *pb.ListLeasesRequest

	err error
}

func (f *fakeDynamicSecretsServer) CreateRole(_ context.Context, req *pb.DynSecretsCreateRoleRequest) (*pb.DynSecretsCreateRoleResponse, error) {
	f.lastCreate = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.DynSecretsCreateRoleResponse{
		Role: &pb.Role{
			Name:          req.GetName(),
			GeneratorType: req.GetGeneratorType(),
			Config:        req.GetConfig(),
			DefaultTtl:    req.GetDefaultTtl(),
			MaxTtl:        req.GetMaxTtl(),
		},
		Timestamp: timestamppb.New(time.Unix(1700000000, 0)),
	}, nil
}

func (f *fakeDynamicSecretsServer) GetRole(_ context.Context, req *pb.DynSecretsGetRoleRequest) (*pb.DynSecretsGetRoleResponse, error) {
	f.lastGet = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.DynSecretsGetRoleResponse{
		Role: &pb.Role{Name: req.GetName(), DefaultTtl: durationpb.New(time.Hour)},
	}, nil
}

func (f *fakeDynamicSecretsServer) ListRoles(_ context.Context, req *pb.DynSecretsListRolesRequest) (*pb.DynSecretsListRolesResponse, error) {
	f.lastList = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.DynSecretsListRolesResponse{
		Roles: []*pb.Role{{Name: "a"}, {Name: "b"}},
		Count: 2,
	}, nil
}

func (f *fakeDynamicSecretsServer) DeleteRole(_ context.Context, req *pb.DynSecretsDeleteRoleRequest) (*pb.DynSecretsDeleteRoleResponse, error) {
	f.lastDelete = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.DynSecretsDeleteRoleResponse{
		Success:   true,
		Timestamp: timestamppb.New(time.Unix(1700000001, 0)),
	}, nil
}

func (f *fakeDynamicSecretsServer) GenerateCredential(_ context.Context, req *pb.GenerateCredentialRequest) (*pb.GenerateCredentialResponse, error) {
	f.lastGenerate = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.GenerateCredentialResponse{
		Credential: &pb.Credential{
			CredentialId: "cred-1",
			Username:     "u",
			Password:     "p",
			Metadata:     map[string]string{"extra": "x"},
		},
		Lease: &pb.Lease{
			LeaseId:    "lease-1",
			SecretPath: "postgres/creds/" + req.GetRoleName(),
			Ttl:        durationpb.New(time.Hour),
			ExpiresAt:  timestamppb.New(time.Unix(1700003600, 0)),
			CreatedAt:  timestamppb.New(time.Unix(1700000000, 0)),
			Renewable:  true,
			RenewCount: 2,
			Metadata:   map[string]string{"k": "v"},
		},
		LeaseId: "lease-1",
	}, nil
}

func (f *fakeDynamicSecretsServer) RenewLease(_ context.Context, req *pb.RenewLeaseRequest) (*pb.RenewLeaseResponse, error) {
	f.lastRenew = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.RenewLeaseResponse{
		Lease:     &pb.Lease{LeaseId: req.GetLeaseId(), RenewCount: 3},
		Timestamp: timestamppb.New(time.Unix(1700000002, 0)),
	}, nil
}

func (f *fakeDynamicSecretsServer) RevokeLease(_ context.Context, req *pb.RevokeLeaseRequest) (*pb.RevokeLeaseResponse, error) {
	f.lastRevoke = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.RevokeLeaseResponse{
		Success:   true,
		Timestamp: timestamppb.New(time.Unix(1700000003, 0)),
	}, nil
}

func (f *fakeDynamicSecretsServer) ListLeases(_ context.Context, req *pb.ListLeasesRequest) (*pb.ListLeasesResponse, error) {
	f.lastLeases = req
	if f.err != nil {
		return nil, f.err
	}
	return &pb.ListLeasesResponse{
		Leases: []*pb.Lease{{LeaseId: "lease-1", Ttl: durationpb.New(time.Minute)}},
		Count:  1,
	}, nil
}

// newDynamicSecretsHandlerFor starts an in-memory server backed by fake and
// returns a handler wired to it.
func newDynamicSecretsHandlerFor(t *testing.T, fake *fakeDynamicSecretsServer) *dynamicSecretsHandler {
	t.Helper()
	hs := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterDynamicSecretsServiceServer(s, fake)
	})
	return newDynamicSecretsHandler(hs.dial(t))
}

func TestIsDynamicSecretsService(t *testing.T) {
	tests := []struct {
		svc  string
		want bool
	}{
		{"quicbaas.DynamicSecrets", true},
		{"quicbaas.DynamicSecrets/CreateRole", true},
		{"quicbaas.Secrets", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isDynamicSecretsService(tt.svc); got != tt.want {
			t.Errorf("isDynamicSecretsService(%q) = %v, want %v", tt.svc, got, tt.want)
		}
	}
}

func TestDynamicSecretsHandler_AllVerbs(t *testing.T) {
	fake := &fakeDynamicSecretsServer{}
	h := newDynamicSecretsHandlerFor(t, fake)
	ctx := context.Background()

	t.Run("CreateRole", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "CreateRole",
			&dsclient.CreateRoleRequest{
				Name:          "db-readonly",
				GeneratorType: "postgres",
				Config:        map[string]string{"db": "app"},
				DefaultTTL:    time.Hour,
				MaxTTL:        24 * time.Hour,
			})
		if err != nil {
			t.Fatalf("CreateRole: %v", err)
		}
		resp := got.(*dsclient.CreateRoleResponse)
		if resp.Role.Name != "db-readonly" || resp.Role.DefaultTTL != time.Hour {
			t.Errorf("role round-trip lost data: %+v", resp.Role)
		}
		if resp.Role.MaxTTL != 24*time.Hour {
			t.Errorf("MaxTTL = %v", resp.Role.MaxTTL)
		}
		if resp.CreatedAt.Unix() != 1700000000 {
			t.Errorf("CreatedAt = %v", resp.CreatedAt)
		}
		if fake.lastCreate.GetConfig()["db"] != "app" {
			t.Errorf("config not forwarded: %+v", fake.lastCreate.GetConfig())
		}
	})

	t.Run("GetRole", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "GetRole",
			&dsclient.GetRoleRequest{Name: "db-readonly"})
		if err != nil {
			t.Fatalf("GetRole: %v", err)
		}
		resp := got.(*dsclient.GetRoleResponse)
		if resp.Role.Name != "db-readonly" {
			t.Errorf("got %+v", resp.Role)
		}
	})

	t.Run("ListRoles", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "ListRoles",
			&dsclient.ListRolesRequest{Prefix: "db"})
		if err != nil {
			t.Fatalf("ListRoles: %v", err)
		}
		resp := got.(*dsclient.ListRolesResponse)
		if resp.Count != 2 || len(resp.Roles) != 2 {
			t.Errorf("got count=%d roles=%d", resp.Count, len(resp.Roles))
		}
		if fake.lastList.GetPrefix() != "db" {
			t.Errorf("prefix not forwarded: %q", fake.lastList.GetPrefix())
		}
	})

	t.Run("DeleteRole", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "DeleteRole",
			&dsclient.DeleteRoleRequest{Name: "db-readonly"})
		if err != nil {
			t.Fatalf("DeleteRole: %v", err)
		}
		resp := got.(*dsclient.DeleteRoleResponse)
		if resp.DeletedAt.Unix() != 1700000001 {
			t.Errorf("DeletedAt = %v", resp.DeletedAt)
		}
	})

	t.Run("GenerateCredential", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "GenerateCredential",
			&dsclient.GenerateCredentialRequest{RoleName: "db-readonly", TTL: 30 * time.Minute})
		if err != nil {
			t.Fatalf("GenerateCredential: %v", err)
		}
		resp := got.(*dsclient.GenerateCredentialResponse)
		if resp.LeaseID != "lease-1" {
			t.Errorf("LeaseID = %q", resp.LeaseID)
		}
		if resp.Credential.CredentialID != "cred-1" {
			t.Errorf("CredentialID = %q", resp.Credential.CredentialID)
		}
		// The typed username/password fields and the generator's own map are
		// merged into the single Data map the SDK exposes.
		for k, want := range map[string]string{"username": "u", "password": "p", "extra": "x"} {
			if resp.Credential.Data[k] != want {
				t.Errorf("Data[%q] = %q, want %q", k, resp.Credential.Data[k], want)
			}
		}
		if resp.Lease.SecretPath != "postgres/creds/db-readonly" {
			t.Errorf("SecretPath = %q", resp.Lease.SecretPath)
		}
		if resp.Lease.RenewCount != 2 || !resp.Lease.Renewable {
			t.Errorf("lease renewal fields lost: %+v", resp.Lease)
		}
		if resp.Lease.Metadata["k"] != "v" {
			t.Errorf("lease metadata lost: %+v", resp.Lease.Metadata)
		}
		if !resp.Credential.ExpiresAt.Equal(resp.Lease.ExpiresAt) {
			t.Errorf("credential expiry must mirror the lease: %v vs %v",
				resp.Credential.ExpiresAt, resp.Lease.ExpiresAt)
		}
		if fake.lastGenerate.GetTtl().AsDuration() != 30*time.Minute {
			t.Errorf("TTL not forwarded: %v", fake.lastGenerate.GetTtl())
		}
	})

	t.Run("RenewLease", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "RenewLease",
			&dsclient.RenewLeaseRequest{LeaseID: "lease-1", Increment: time.Hour})
		if err != nil {
			t.Fatalf("RenewLease: %v", err)
		}
		resp := got.(*dsclient.RenewLeaseResponse)
		if resp.Lease.RenewCount != 3 {
			t.Errorf("RenewCount = %d", resp.Lease.RenewCount)
		}
		if fake.lastRenew.GetIncrement().AsDuration() != time.Hour {
			t.Errorf("increment not forwarded: %v", fake.lastRenew.GetIncrement())
		}
	})

	t.Run("RevokeLease", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "RevokeLease",
			&dsclient.RevokeLeaseRequest{LeaseID: "lease-1"})
		if err != nil {
			t.Fatalf("RevokeLease: %v", err)
		}
		resp := got.(*dsclient.RevokeLeaseResponse)
		if resp.RevokedAt.Unix() != 1700000003 {
			t.Errorf("RevokedAt = %v", resp.RevokedAt)
		}
	})

	t.Run("ListLeases", func(t *testing.T) {
		got, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "ListLeases",
			&dsclient.ListLeasesRequest{Prefix: "postgres"})
		if err != nil {
			t.Fatalf("ListLeases: %v", err)
		}
		resp := got.(*dsclient.ListLeasesResponse)
		if resp.Count != 1 || len(resp.Leases) != 1 {
			t.Errorf("got count=%d leases=%d", resp.Count, len(resp.Leases))
		}
		if fake.lastLeases.GetPrefix() != "postgres" {
			t.Errorf("prefix not forwarded: %q", fake.lastLeases.GetPrefix())
		}
	})
}

func TestDynamicSecretsHandler_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown method", func(t *testing.T) {
		h := newDynamicSecretsHandlerFor(t, &fakeDynamicSecretsServer{})
		_, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "NotAVerb", nil)
		if err == nil {
			t.Fatal("expected an error")
		}
		if !sdkerrors.IsUnimplemented(err) {
			t.Errorf("got %v, want an Unimplemented error", err)
		}
	})

	t.Run("wrong request type", func(t *testing.T) {
		h := newDynamicSecretsHandlerFor(t, &fakeDynamicSecretsServer{})
		for _, method := range []string{
			"CreateRole", "GetRole", "DeleteRole",
			"GenerateCredential", "RenewLease", "RevokeLease",
		} {
			if _, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, method, "not-a-request"); err == nil {
				t.Errorf("%s accepted a string request", method)
			}
		}
	})

	t.Run("nil request", func(t *testing.T) {
		h := newDynamicSecretsHandlerFor(t, &fakeDynamicSecretsServer{})
		if _, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "CreateRole",
			(*dsclient.CreateRoleRequest)(nil)); err == nil {
			t.Error("CreateRole accepted a nil request")
		}
	})

	t.Run("server error is converted", func(t *testing.T) {
		fake := &fakeDynamicSecretsServer{err: status.Error(codes.PermissionDenied, "denied")}
		h := newDynamicSecretsHandlerFor(t, fake)
		_, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "ListRoles",
			&dsclient.ListRolesRequest{})
		if err == nil {
			t.Fatal("expected an error")
		}
		if !sdkerrors.IsForbidden(err) {
			t.Errorf("got %v, want a Forbidden error", err)
		}
	})

	t.Run("list verbs tolerate a nil request", func(t *testing.T) {
		h := newDynamicSecretsHandlerFor(t, &fakeDynamicSecretsServer{})
		if _, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "ListRoles", nil); err != nil {
			t.Errorf("ListRoles(nil): %v", err)
		}
		if _, err := h.handleRequest(ctx, dynamicSecretsServicePrefix, "ListLeases", nil); err != nil {
			t.Errorf("ListLeases(nil): %v", err)
		}
	})
}

// TestDSTimeFromProto covers the epoch-to-zero-time mapping callers rely on
// for IsZero() checks.
func TestDSTimeFromProto(t *testing.T) {
	if got := dsTimeFromProto(nil); !got.IsZero() {
		t.Errorf("nil timestamp = %v, want zero", got)
	}
	if got := dsTimeFromProto(timestamppb.New(time.Unix(0, 0))); !got.IsZero() {
		t.Errorf("epoch timestamp = %v, want zero", got)
	}
	if got := dsTimeFromProto(timestamppb.New(time.Unix(1700000000, 0))); got.Unix() != 1700000000 {
		t.Errorf("got %v", got)
	}
}

// TestDSCredentialData proves the generator's own map wins over the typed
// convenience fields, so a generator that emits a different username is not
// silently overwritten.
func TestDSCredentialData(t *testing.T) {
	got := dsCredentialData(&pb.Credential{
		Username: "typed",
		Password: "typed-pw",
		Host:     "db.example",
		Metadata: map[string]string{"username": "from-map"},
	})
	if got["username"] != "from-map" {
		t.Errorf("username = %q, want the generator map value", got["username"])
	}
	if got["password"] != "typed-pw" || got["host"] != "db.example" {
		t.Errorf("typed fields not folded in: %+v", got)
	}

	if dsCredentialData(&pb.Credential{}) != nil {
		t.Error("an empty credential must yield a nil data map")
	}
}

func TestDSConvertersHandleNil(t *testing.T) {
	if dsRoleFromProto(nil) != nil {
		t.Error("dsRoleFromProto(nil) must be nil")
	}
	if dsLeaseFromProto(nil) != nil {
		t.Error("dsLeaseFromProto(nil) must be nil")
	}
	if dsCredentialFromProto(nil, "lease", nil) != nil {
		t.Error("dsCredentialFromProto(nil, ...) must be nil")
	}
}
