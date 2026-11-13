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
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// fakeQuicBaaSServer implements the QuicBaaS service methods exercised by the
// KV, cluster, health, RBAC and auth handlers. A fail flag drives the
// error-propagation paths.
type fakeQuicBaaSServer struct {
	pb.UnimplementedQuicBaaSServer
	fail bool
}

func (f *fakeQuicBaaSServer) e() error { return failErr(f.fail) }

// KV operations.
func (f *fakeQuicBaaSServer) Put(context.Context, *pb.PutRequest) (*pb.PutResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.PutResponse{Id: 1, Version: 2, Timestamp: 3, LeaderId: 4}, nil
}
func (f *fakeQuicBaaSServer) Get(context.Context, *pb.GetRequest) (*pb.GetResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetResponse{Id: 1, Data: nil, Version: 2, Timestamp: 3, Found: true}, nil
}
func (f *fakeQuicBaaSServer) Delete(context.Context, *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.DeleteResponse{Deleted: true, Timestamp: 3}, nil
}
func (f *fakeQuicBaaSServer) List(_ *pb.ListRequest, stream grpc.ServerStreamingServer[pb.ListResponse]) error {
	if err := f.e(); err != nil {
		return err
	}
	return stream.Send(&pb.ListResponse{
		Entities:      []*pb.Entity{{EntityType: "user", Id: 1, Data: nil, Version: 2, Timestamp: 3}},
		NextPageToken: "next",
		TotalCount:    1,
	})
}
func (f *fakeQuicBaaSServer) ListEntityTypes(context.Context, *pb.ListEntityTypesRequest) (*pb.ListEntityTypesResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ListEntityTypesResponse{EntityTypes: []string{"user"}, NextPageToken: "n", TotalCount: 1}, nil
}
func (f *fakeQuicBaaSServer) BatchPut(context.Context, *pb.BatchPutRequest) (*pb.BatchPutResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.BatchPutResponse{Responses: []*pb.PutResponse{{Id: 1}}, Timestamp: 3}, nil
}
func (f *fakeQuicBaaSServer) BatchGet(context.Context, *pb.BatchGetRequest) (*pb.BatchGetResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.BatchGetResponse{Responses: []*pb.GetResponse{{Id: 1, Found: true}}}, nil
}
func (f *fakeQuicBaaSServer) BatchDelete(context.Context, *pb.BatchDeleteRequest) (*pb.BatchDeleteResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.BatchDeleteResponse{Responses: []*pb.DeleteResponse{{Deleted: true}}, Timestamp: 3}, nil
}
func (f *fakeQuicBaaSServer) BatchExecute(_ context.Context, req *pb.BatchExecuteRequest) (*pb.BatchExecuteResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	results := make([]*pb.BatchExecuteResult, len(req.Operations))
	for i, op := range req.Operations {
		results[i] = &pb.BatchExecuteResult{Succeeded: op.Type == pb.BatchExecuteOpType_BATCH_EXECUTE_OP_TYPE_PUT}
	}
	return &pb.BatchExecuteResponse{Results: results}, nil
}
func (f *fakeQuicBaaSServer) Transaction(context.Context, *pb.TransactionRequest) (*pb.TransactionResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.TransactionResponse{
		Succeeded: true,
		Responses: []*pb.TransactionOpResponse{{OpType: pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT, EntityType: "user", Id: 1, Success: true, Version: 2, Timestamp: 3}},
		Timestamp: 3, Message: "ok", Revision: 7,
	}, nil
}

// Cluster operations.
func (f *fakeQuicBaaSServer) ClusterStatus(context.Context, *pb.ClusterStatusRequest) (*pb.ClusterStatusResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ClusterStatusResponse{
		ClusterId: "c1",
		Nodes:     []*pb.NodeInfo{{NodeId: 1, Address: "addr", IsLeader: true, HealthStatus: pb.HealthStatus_HEALTH_STATUS_HEALTHY}},
		Shards:    []*pb.ShardInfo{{ShardId: 0, LeaderId: 1, Replicas: []uint64{1, 2}}},
	}, nil
}
func (f *fakeQuicBaaSServer) AddNode(context.Context, *pb.AddNodeRequest) (*pb.AddNodeResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.AddNodeResponse{Success: true}, nil
}
func (f *fakeQuicBaaSServer) RemoveNode(context.Context, *pb.RemoveNodeRequest) (*pb.RemoveNodeResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RemoveNodeResponse{Success: true}, nil
}
func (f *fakeQuicBaaSServer) RebalanceCluster(context.Context, *pb.RebalanceClusterRequest) (*pb.RebalanceClusterResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RebalanceClusterResponse{OperationId: "op1", Started: 1}, nil
}
func (f *fakeQuicBaaSServer) GetRebalanceProgress(context.Context, *pb.GetRebalanceProgressRequest) (*pb.GetRebalanceProgressResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.GetRebalanceProgressResponse{OperationId: "op1", Status: "running", PercentComplete: 50, ShardsMoved: 1, TotalShards: 2, ElapsedTime: 5}, nil
}

// Health.
func (f *fakeQuicBaaSServer) Health(context.Context, *pb.HealthRequest) (*pb.HealthResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.HealthResponse{
		Status:  pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Details: map[string]*pb.ComponentHealth{"raft": {Status: pb.HealthStatus_HEALTH_STATUS_HEALTHY, Message: "ok"}},
	}, nil
}

// Auth.
func (f *fakeQuicBaaSServer) Login(context.Context, *pb.LoginRequest) (*pb.LoginResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.LoginResponse{AccessToken: "at", RefreshToken: "rt", ExpiresAt: 1000, TokenType: "Bearer"}, nil
}
func (f *fakeQuicBaaSServer) Logout(context.Context, *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.LogoutResponse{}, nil
}
func (f *fakeQuicBaaSServer) RefreshToken(context.Context, *pb.RefreshTokenRequest) (*pb.RefreshTokenResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RefreshTokenResponse{AccessToken: "at2", ExpiresAt: 2000, TokenType: "Bearer"}, nil
}
func (f *fakeQuicBaaSServer) ValidateToken(context.Context, *pb.ValidateTokenRequest) (*pb.ValidateTokenResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ValidateTokenResponse{Valid: true, UserId: "u1", Username: "alice", TenantId: "acme", Roles: []string{"admin"}, ExpiresAt: 3000}, nil
}

// RBAC.
func (f *fakeQuicBaaSServer) CreateRole(context.Context, *pb.CreateRoleRequest) (*pb.CreateRoleResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.CreateRoleResponse{RoleName: "admin"}, nil
}
func (f *fakeQuicBaaSServer) DeleteRole(context.Context, *pb.DeleteRoleRequest) (*pb.DeleteRoleResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.DeleteRoleResponse{Success: true}, nil
}
func (f *fakeQuicBaaSServer) ListRoles(context.Context, *pb.ListRolesRequest) (*pb.ListRolesResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.ListRolesResponse{Roles: []*pb.RoleInfo{{RoleName: "admin", Permissions: []*pb.Permission{{Resource: "kv", Actions: []string{"*"}}}}}}, nil
}
func (f *fakeQuicBaaSServer) AssignRole(context.Context, *pb.AssignRoleRequest) (*pb.AssignRoleResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.AssignRoleResponse{Success: true}, nil
}
func (f *fakeQuicBaaSServer) RevokeRole(context.Context, *pb.RevokeRoleRequest) (*pb.RevokeRoleResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RevokeRoleResponse{Success: true}, nil
}

// newQuicBaaSConn dials a server with the QuicBaaS fake registered.
func newQuicBaaSConn(t *testing.T, fail bool) *grpc.ClientConn {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterQuicBaaSServer(s, &fakeQuicBaaSServer{fail: fail})
	})
	return srv.dial(t)
}
