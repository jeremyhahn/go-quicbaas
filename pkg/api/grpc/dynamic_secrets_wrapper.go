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

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// DynamicSecretsWrapper adapts the QuicBaaS *Server to the generated
// DynamicSecretsServiceServer interface.
//
// The four role verbs are implemented on *Server under DynamicSecrets-qualified
// names (CreateDynamicSecretsRole, ...) because the bare proto names collide
// with the IAM role verbs already on that type. This wrapper restores the proto
// names so the service can actually be registered on the gRPC listener; the
// four lease/credential verbs already match and are delegated verbatim.
type DynamicSecretsWrapper struct {
	server *Server
}

// Compile-time proof that the wrapper satisfies the generated service
// interface. Without it a proto signature drift would only surface at
// registration time.
var _ pb.DynamicSecretsServiceServer = (*DynamicSecretsWrapper)(nil)

// NewDynamicSecretsWrapper creates a DynamicSecretsService adapter around the
// main server.
func NewDynamicSecretsWrapper(server *Server) *DynamicSecretsWrapper {
	return &DynamicSecretsWrapper{server: server}
}

// RegisterDynamicSecretsServiceServer registers the dynamic secrets handlers
// on the supplied gRPC listener. It mirrors the Register* helpers the
// certificate, trust and attestation services expose so the server lifecycle
// never has to know about the wrapper indirection.
func RegisterDynamicSecretsServiceServer(server *grpc.Server, srv *Server) {
	pb.RegisterDynamicSecretsServiceServer(server, NewDynamicSecretsWrapper(srv))
}

// CreateRole creates a new credential generation role.
func (w *DynamicSecretsWrapper) CreateRole(ctx context.Context, req *pb.DynSecretsCreateRoleRequest) (*pb.DynSecretsCreateRoleResponse, error) {
	return w.server.CreateDynamicSecretsRole(ctx, req)
}

// GetRole retrieves a credential generation role by name.
func (w *DynamicSecretsWrapper) GetRole(ctx context.Context, req *pb.DynSecretsGetRoleRequest) (*pb.DynSecretsGetRoleResponse, error) {
	return w.server.GetDynamicSecretsRole(ctx, req)
}

// ListRoles returns all credential generation roles.
func (w *DynamicSecretsWrapper) ListRoles(ctx context.Context, req *pb.DynSecretsListRolesRequest) (*pb.DynSecretsListRolesResponse, error) {
	return w.server.ListDynamicSecretsRoles(ctx, req)
}

// DeleteRole removes a credential generation role.
func (w *DynamicSecretsWrapper) DeleteRole(ctx context.Context, req *pb.DynSecretsDeleteRoleRequest) (*pb.DynSecretsDeleteRoleResponse, error) {
	return w.server.DeleteDynamicSecretsRole(ctx, req)
}

// GenerateCredential generates a new dynamic credential based on a role.
func (w *DynamicSecretsWrapper) GenerateCredential(ctx context.Context, req *pb.GenerateCredentialRequest) (*pb.GenerateCredentialResponse, error) {
	return w.server.GenerateCredential(ctx, req)
}

// RenewLease extends the expiration time of a credential lease.
func (w *DynamicSecretsWrapper) RenewLease(ctx context.Context, req *pb.RenewLeaseRequest) (*pb.RenewLeaseResponse, error) {
	return w.server.RenewLease(ctx, req)
}

// RevokeLease revokes a credential lease immediately.
func (w *DynamicSecretsWrapper) RevokeLease(ctx context.Context, req *pb.RevokeLeaseRequest) (*pb.RevokeLeaseResponse, error) {
	return w.server.RevokeLease(ctx, req)
}

// ListLeases returns all active leases.
func (w *DynamicSecretsWrapper) ListLeases(ctx context.Context, req *pb.ListLeasesRequest) (*pb.ListLeasesResponse, error) {
	return w.server.ListLeases(ctx, req)
}
