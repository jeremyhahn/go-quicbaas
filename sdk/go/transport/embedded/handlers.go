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

package embedded

import (
	"context"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	healthclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rbac"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// KV Handlers
// =============================================================================

func handlePut(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.PutRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KV", "Put", "*service.PutRequest", typeNameOf(req))
	}
	return svc.Put(ctx, r)
}

func handleGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KV", "Get", "*service.GetRequest", typeNameOf(req))
	}
	return svc.Get(ctx, r)
}

func handleDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.DeleteRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KV", "Delete", "*service.DeleteRequest", typeNameOf(req))
	}
	return svc.Delete(ctx, r)
}

func handleListEntityTypes(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.ListEntityTypesRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KV", "ListEntityTypes", "*service.ListEntityTypesRequest", typeNameOf(req))
	}
	return svc.ListEntityTypes(ctx, r)
}

func handleList(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.ListRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.KV", "List", "*service.ListRequest", typeNameOf(req))
	}
	stream := make(chan *service.ListResponse, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- svc.List(ctx, r, stream)
	}()
	// The service sends at most one page; the caller follows NextPageToken.
	var result *service.ListResponse
	for resp := range stream {
		result = resp
	}
	if err := <-errCh; err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.ListResponse{}
	}
	return result, nil
}

// =============================================================================
// Batch Handlers
// =============================================================================

func handleBatchPut(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.BatchPutRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Batch", "Put", "*service.BatchPutRequest", typeNameOf(req))
	}
	return svc.BatchPut(ctx, r)
}

func handleBatchGet(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.BatchGetRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Batch", "Get", "*service.BatchGetRequest", typeNameOf(req))
	}
	return svc.BatchGet(ctx, r)
}

func handleBatchDelete(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.BatchDeleteRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Batch", "Delete", "*service.BatchDeleteRequest", typeNameOf(req))
	}
	return svc.BatchDelete(ctx, r)
}

// =============================================================================
// Cluster Handlers
// =============================================================================

// handleClusterStatus reports cluster status. It takes the SDK cluster
// client's request, which is what Cluster().Status sends, as well as the
// service request an embedder calling the transport directly passes.
func handleClusterStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	if r, ok := req.(*service.ClusterStatusRequest); ok {
		return svc.ClusterStatus(ctx, r)
	}
	if !isClusterStatusRequest(req) {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "Status", "*cluster.statusRequest", typeNameOf(req))
	}
	// The gRPC transport asks for details too.
	resp, err := svc.ClusterStatus(ctx, &service.ClusterStatusRequest{IncludeDetails: true})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal("embedded.ClusterStatus", "nil response from service")
	}
	return convertClusterStatusToSDK(resp), nil
}

// clusterStatusRequestType is the cluster client's unexported Status request.
// It is matched by package and name because it cannot be named here.
var clusterStatusRequestType = struct{ pkgPath, name string }{
	pkgPath: reflect.TypeOf(cluster.ClusterStatus{}).PkgPath(),
	name:    "statusRequest",
}

// isClusterStatusRequest reports whether req is the cluster client's Status
// request.
func isClusterStatusRequest(req interface{}) bool {
	t := reflect.TypeOf(req)
	if t == nil || t.Kind() != reflect.Ptr {
		return false
	}
	t = t.Elem()
	return t.PkgPath() == clusterStatusRequestType.pkgPath && t.Name() == clusterStatusRequestType.name
}

// convertClusterStatusToSDK converts a service ClusterStatusResponse to the
// SDK cluster client's ClusterStatus, as the gRPC transport does. The cluster
// is healthy when it has members and every member is healthy.
func convertClusterStatusToSDK(resp *service.ClusterStatusResponse) *cluster.ClusterStatus {
	status := &cluster.ClusterStatus{
		ClusterID: resp.ClusterID,
		Members:   make([]cluster.ClusterMember, 0, len(resp.Nodes)),
		Healthy:   len(resp.Nodes) > 0,
		Shards:    make([]cluster.ClusterShard, 0, len(resp.Shards)),
	}
	for _, node := range resp.Nodes {
		member := cluster.ClusterMember{
			ID:       strconv.FormatUint(node.NodeID, 10),
			Name:     node.Address,
			Address:  node.Address,
			IsLeader: node.IsLeader,
			Healthy:  node.HealthStatus == service.HealthStatusHealthy,
		}
		status.Members = append(status.Members, member)
		if member.IsLeader {
			status.LeaderID = member.ID
		}
		if !member.Healthy {
			status.Healthy = false
		}
	}
	for _, shard := range resp.Shards {
		status.Shards = append(status.Shards, cluster.ClusterShard{
			ShardID:  shard.ShardID,
			LeaderID: shard.LeaderID,
			Replicas: shard.Replicas,
		})
	}
	return status
}

func handleGetLeader(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetLeaderRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "GetLeader", "*service.GetLeaderRequest", typeNameOf(req))
	}
	return svc.GetLeader(ctx, r)
}

// handleClusterNodes serves the SDK cluster client's Nodes call. The verb
// takes no fields: the client sends its unexported nodesRequest, so the
// request is not inspected. The nodes come from ClusterStatus, as the gRPC
// transport serves them, converted to *cluster.NodesResponse so the SDK
// client can type-assert the result.
func handleClusterNodes(ctx context.Context, svc service.QuicBaaS, _ interface{}) (interface{}, error) {
	status, err := svc.ClusterStatus(ctx, &service.ClusterStatusRequest{})
	if err != nil {
		return nil, err
	}
	resp := &cluster.NodesResponse{Nodes: []cluster.NodeInfo{}}
	if status == nil {
		return resp, nil
	}
	for _, node := range status.Nodes {
		if node == nil {
			continue
		}
		resp.Nodes = append(resp.Nodes, cluster.NodeInfo{
			ID:      strconv.FormatUint(node.NodeID, 10),
			Name:    node.Address,
			Address: node.Address,
			State:   nodeStateOf(node.HealthStatus),
		})
	}
	return resp, nil
}

// nodeStateOf maps a service health status to the SDK cluster node state.
func nodeStateOf(status service.HealthStatus) cluster.NodeState {
	switch status {
	case service.HealthStatusHealthy:
		return cluster.NodeStateHealthy
	case service.HealthStatusDegraded, service.HealthStatusUnhealthy:
		return cluster.NodeStateUnhealthy
	default:
		return cluster.NodeStateUnknown
	}
}

// handleAddNode adds a replica. It takes the SDK cluster client's request,
// which is what Cluster().AddNode sends, as well as the service request an
// embedder calling the transport directly passes.
func handleAddNode(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *cluster.AddNodeRequest:
		// The SDK client takes a nil response as success, exactly as the
		// gRPC transport returns it.
		resp, err := svc.AddNode(ctx, &service.AddNodeRequest{ShardID: r.ShardID, NodeID: r.NodeID, Address: r.Address, Identity: r.Identity})
		if err != nil {
			return nil, err
		}
		if !resp.Success {
			return nil, errors.NewInternal("embedded.AddNode", resp.Message)
		}
		return nil, nil
	case *service.AddNodeRequest:
		return svc.AddNode(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "AddNode", "*cluster.AddNodeRequest", typeNameOf(req))
	}
}

// handleRemoveNode removes a replica. It takes the SDK cluster client's
// request, which is what Cluster().RemoveNode sends, as well as the service
// request an embedder calling the transport directly passes.
func handleRemoveNode(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *cluster.RemoveNodeRequest:
		nodeID, err := strconv.ParseUint(r.NodeID, 10, 64)
		if err != nil {
			return nil, errors.NewInvalidInput("embedded.RemoveNode", "invalid node ID format: "+r.NodeID)
		}
		// The SDK request names no shard, so ShardID stays zero exactly as
		// the gRPC transport sends it.
		resp, err := svc.RemoveNode(ctx, &service.RemoveNodeRequest{NodeID: nodeID})
		if err != nil {
			return nil, err
		}
		if resp == nil || !resp.Success {
			message := "remove node failed"
			if resp != nil {
				message = resp.Message
			}
			return nil, errors.NewInternal("embedded.RemoveNode", message)
		}
		// The SDK client takes a nil response as success, exactly as the
		// gRPC transport returns it.
		return nil, nil
	case *service.RemoveNodeRequest:
		return svc.RemoveNode(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "RemoveNode", "*cluster.RemoveNodeRequest", typeNameOf(req))
	}
}

func handleCreateShard(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.CreateShardRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "CreateShard", "*service.CreateShardRequest", typeNameOf(req))
	}
	return svc.CreateShard(ctx, r)
}

// handleRebalanceCluster starts a rebalance. It takes the SDK cluster
// client's request, which is what Cluster().RebalanceCluster sends, as well as
// the service request an embedder calling the transport directly passes.
func handleRebalanceCluster(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *cluster.RebalanceClusterRequest:
		resp, err := svc.RebalanceCluster(ctx, &service.RebalanceClusterRequest{Strategy: r.Strategy})
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, errors.NewInternal("embedded.RebalanceCluster", "nil response from service")
		}
		// The same field mapping as the gRPC server's RebalanceCluster.
		return &cluster.RebalanceClusterResponse{
			OperationID: resp.JobID,
			Started:     resp.EstimatedCompletion,
		}, nil
	case *service.RebalanceClusterRequest:
		return svc.RebalanceCluster(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "Rebalance", "*cluster.RebalanceClusterRequest", typeNameOf(req))
	}
}

// handleGetRebalanceProgress reports rebalance progress. It takes the SDK
// cluster client's request, which is what Cluster().GetRebalanceProgress
// sends, as well as the service request an embedder calling the transport
// directly passes.
func handleGetRebalanceProgress(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *cluster.GetRebalanceProgressRequest:
		resp, err := svc.GetRebalanceProgress(ctx, &service.GetRebalanceProgressRequest{OperationID: r.OperationID})
		if err != nil {
			return nil, err
		}
		if resp == nil {
			return nil, errors.NewInternal("embedded.GetRebalanceProgress", "nil response from service")
		}
		return convertRebalanceProgressToSDK(resp)
	case *service.GetRebalanceProgressRequest:
		return svc.GetRebalanceProgress(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "GetRebalanceStatus", "*cluster.GetRebalanceProgressRequest", typeNameOf(req))
	}
}

// convertRebalanceProgressToSDK converts a service GetRebalanceProgressResponse
// to the SDK cluster client's response, with the gRPC server's field mapping.
// A shard count outside the SDK's int32 range is an error, not a wrapped value.
func convertRebalanceProgressToSDK(resp *service.GetRebalanceProgressResponse) (*cluster.GetRebalanceProgressResponse, error) {
	shardsMoved, err := shardCountInt32("shards_moved", resp.ShardsMoved)
	if err != nil {
		return nil, err
	}
	totalShards, err := shardCountInt32("total_shards", resp.TotalShards)
	if err != nil {
		return nil, err
	}
	return &cluster.GetRebalanceProgressResponse{
		OperationID:     resp.OperationID,
		Status:          resp.Status,
		PercentComplete: resp.Progress,
		ShardsMoved:     shardsMoved,
		TotalShards:     totalShards,
		ElapsedTime:     int64(resp.ElapsedSeconds),
	}, nil
}

// shardCountInt32 narrows a service shard count to the SDK's int32 field.
func shardCountInt32(field string, v int) (int32, error) {
	if v < math.MinInt32 || v > math.MaxInt32 {
		return 0, errors.NewInternal("embedded.GetRebalanceProgress", field+" is out of int32 range: "+strconv.Itoa(v))
	}
	return int32(v), nil
}

// =============================================================================
// Backup Handlers
// =============================================================================

func handleCreateBackup(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.CreateBackupRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Backup", "Create", "*service.CreateBackupRequest", typeNameOf(req))
	}
	return svc.CreateBackup(ctx, r)
}

func handleListBackups(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.ListBackupsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Backup", "List", "*service.ListBackupsRequest", typeNameOf(req))
	}
	return svc.ListBackups(ctx, r)
}

func handleRestoreBackup(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.RestoreBackupRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Backup", "Restore", "*service.RestoreBackupRequest", typeNameOf(req))
	}
	return svc.RestoreBackup(ctx, r)
}

func handleDeleteBackup(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.DeleteBackupRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Backup", "Delete", "*service.DeleteBackupRequest", typeNameOf(req))
	}
	return svc.DeleteBackup(ctx, r)
}

func handleVerifyBackup(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.VerifyBackupRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Backup", "Verify", "*service.VerifyBackupRequest", typeNameOf(req))
	}
	return svc.VerifyBackup(ctx, r)
}

// =============================================================================
// Auth Handlers
// =============================================================================

func handleLogin(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	// Accept *service.ExtendedLoginRequest, the type the auth client sends and
	// the gRPC and REST transports take. Demanding *service.LoginRequest
	// failed every in-process call (go-quicbaas#1544).
	r, ok := req.(*service.ExtendedLoginRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "Login", "*service.ExtendedLoginRequest", typeNameOf(req))
	}
	// service.LoginRequest carries only the username. Refuse an MFA code or a
	// tenant rather than drop it: forwarding without them would log in
	// without the factor or outside the requested tenant.
	if r.MFACode != "" || r.TenantID != "" {
		return nil, errors.NewInvalidInput("embedded.quicbaas.Auth.Login",
			"mfa code and tenant id are not supported by the service login")
	}
	resp, err := svc.Login(ctx, &service.LoginRequest{Username: r.Username})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		// Nothing to convert; the auth client reports a nil response.
		return nil, nil
	}
	// Answer with the *service.ExtendedLoginResponse the auth client asserts.
	// ExpiresAt is Unix seconds.
	return &service.ExtendedLoginResponse{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    time.Unix(resp.ExpiresAt, 0),
	}, nil
}

func handleLogout(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.LogoutRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "Logout", "*service.LogoutRequest", typeNameOf(req))
	}
	return svc.Logout(ctx, r)
}

func handleRefreshToken(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.RefreshTokenRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "RefreshToken", "*service.RefreshTokenRequest", typeNameOf(req))
	}
	return svc.RefreshToken(ctx, r)
}

func handleValidateToken(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.ValidateTokenRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "ValidateToken", "*service.ValidateTokenRequest", typeNameOf(req))
	}
	resp, err := svc.ValidateToken(ctx, r)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		// No claims to convert; the auth client reports a nil response.
		return nil, nil
	}
	// Answer with *service.TokenClaims, the type the gRPC and REST transports
	// produce and the auth client asserts. Returning the service's
	// *service.ValidateTokenResponse unchanged failed that assertion on every
	// in-process call (go-quicbaas#955). ExpiresAt is Unix seconds.
	return &service.TokenClaims{
		Valid:     resp.Valid,
		UserID:    resp.UserID,
		Username:  resp.Username,
		TenantID:  resp.TenantID,
		Roles:     resp.Roles,
		ExpiresAt: time.Unix(resp.ExpiresAt, 0),
	}, nil
}

func handleIntrospectToken(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.IntrospectTokenRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "IntrospectToken", "*service.IntrospectTokenRequest", typeNameOf(req))
	}
	return svc.IntrospectToken(ctx, r)
}

// =============================================================================
// RBAC Handlers
// =============================================================================

// The rbac SDK client sends its own request types and reads its own response
// types, exactly as the REST and gRPC transports accept and return them. Each
// role handler maps the client's request onto the service request and the
// service's answer back; roles and decisions stay go-qrdb's.

func handleCreateAuthRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *rbac.CreateRoleRequest:
		resp, err := svc.CreateAuthRole(ctx, &service.AuthCreateRoleRequest{RoleName: r.RoleName, Permissions: r.Permissions})
		if err != nil || resp == nil {
			return nil, err
		}
		return &rbac.CreateRoleResponse{RoleID: resp.RoleID, Timestamp: resp.Timestamp}, nil
	case *service.AuthCreateRoleRequest:
		return svc.CreateAuthRole(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "CreateRole", "*rbac.CreateRoleRequest", typeNameOf(req))
	}
}

func handleDeleteAuthRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *rbac.DeleteRoleRequest:
		resp, err := svc.DeleteAuthRole(ctx, &service.AuthDeleteRoleRequest{RoleName: r.RoleName})
		if err != nil || resp == nil {
			return nil, err
		}
		return &rbac.DeleteRoleResponse{Success: resp.Success, Message: resp.Message}, nil
	case *service.AuthDeleteRoleRequest:
		return svc.DeleteAuthRole(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "DeleteRole", "*rbac.DeleteRoleRequest", typeNameOf(req))
	}
}

func handleListAuthRoles(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *rbac.ListRolesRequest:
		resp, err := svc.ListAuthRoles(ctx, &service.AuthListRolesRequest{Limit: r.Limit, Offset: r.Offset})
		if err != nil || resp == nil {
			return nil, err
		}
		roles := make([]*rbac.RoleInfo, 0, len(resp.Roles))
		for _, role := range resp.Roles {
			if role == nil {
				continue
			}
			roles = append(roles, &rbac.RoleInfo{
				RoleID:      role.RoleID,
				RoleName:    role.RoleName,
				Permissions: role.Permissions,
				CreatedAt:   role.CreatedAt,
			})
		}
		return &rbac.ListRolesResponse{Roles: roles, TotalCount: resp.TotalCount}, nil
	case *service.AuthListRolesRequest:
		return svc.ListAuthRoles(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "ListRoles", "*rbac.ListRolesRequest", typeNameOf(req))
	}
}

func handleAssignRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *rbac.AssignRoleRequest:
		resp, err := svc.AssignRole(ctx, &service.AuthAssignRoleRequest{Username: r.Username, RoleName: r.RoleName})
		if err != nil || resp == nil {
			return nil, err
		}
		return &rbac.AssignRoleResponse{Success: resp.Success, Message: resp.Message}, nil
	case *service.AuthAssignRoleRequest:
		return svc.AssignRole(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "AssignRole", "*rbac.AssignRoleRequest", typeNameOf(req))
	}
}

func handleRevokeRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *rbac.RevokeRoleRequest:
		resp, err := svc.RevokeRole(ctx, &service.AuthRevokeRoleRequest{Username: r.Username, RoleName: r.RoleName})
		if err != nil || resp == nil {
			return nil, err
		}
		return &rbac.RevokeRoleResponse{Success: resp.Success, Message: resp.Message}, nil
	case *service.AuthRevokeRoleRequest:
		return svc.RevokeRole(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "RevokeRole", "*rbac.RevokeRoleRequest", typeNameOf(req))
	}
}

// =============================================================================
// Health Handlers
// =============================================================================

// handleHealth handles the Check endpoint. This endpoint is only used by
// callers that send the service-level *service.HealthRequest directly (the
// SDK health client does not expose a Check method).
func handleHealth(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.HealthRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Health", "Check", "*service.HealthRequest", typeNameOf(req))
	}
	return svc.Health(ctx, r)
}

// handleLiveness handles the Liveness endpoint. It accepts both the service-level
// *service.LivenessRequest and the SDK client's unexported livenessRequest (which
// has no fields). The response is always converted to *healthclient.HealthResponse
// so the SDK client can type-assert successfully.
func handleLiveness(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	var svcReq *service.LivenessRequest
	if r, ok := req.(*service.LivenessRequest); ok {
		svcReq = r
	} else {
		// Accept the SDK client's unexported livenessRequest type.
		// The SDK type has no fields, so we create a zero-value service request.
		svcReq = &service.LivenessRequest{}
	}

	svcResp, err := svc.Liveness(ctx, svcReq)
	if err != nil {
		return nil, err
	}

	return convertLivenessToSDK(svcResp), nil
}

// handleReadiness handles the Readiness endpoint. It accepts both the service-level
// *service.ReadinessRequest and the SDK client's unexported readinessRequest (which
// has no fields). The response is always converted to *healthclient.HealthResponse
// so the SDK client can type-assert successfully.
func handleReadiness(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	var svcReq *service.ReadinessRequest
	if r, ok := req.(*service.ReadinessRequest); ok {
		svcReq = r
	} else {
		// Accept the SDK client's unexported readinessRequest type.
		// The SDK type has no fields, so we create a zero-value service request.
		svcReq = &service.ReadinessRequest{}
	}

	svcResp, err := svc.Readiness(ctx, svcReq)
	if err != nil {
		return nil, err
	}

	return convertReadinessToSDK(svcResp), nil
}

// convertLivenessToSDK converts a service LivenessResponse to the SDK
// health client's HealthResponse type.
func convertLivenessToSDK(resp *service.LivenessResponse) *healthclient.HealthResponse {
	status := "dead"
	if resp.Alive {
		status = "alive"
	}
	return &healthclient.HealthResponse{
		Healthy: resp.Alive,
		Status:  status,
	}
}

// convertReadinessToSDK converts a service ReadinessResponse to the SDK
// health client's HealthResponse type.
func convertReadinessToSDK(resp *service.ReadinessResponse) *healthclient.HealthResponse {
	status := "not ready"
	if resp.Ready {
		status = "ready"
	}
	details := map[string]interface{}{}
	if resp.Reason != "" {
		details["reason"] = resp.Reason
	}
	return &healthclient.HealthResponse{
		Healthy: resp.Ready,
		Status:  status,
		Details: details,
	}
}

// =============================================================================
// TimeSeries Handlers
// =============================================================================

func handleAggregate(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.AggregateRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "Aggregate", "*service.AggregateRequest", typeNameOf(req))
	}
	return svc.Aggregate(ctx, r)
}

func handleWritePoint(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.WritePointRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "WritePoint", "*service.WritePointRequest", typeNameOf(req))
	}
	return svc.WritePoint(ctx, r)
}

// handleTSWrite serves the SDK time-series client's Write call. The client
// sends *timeseries.WriteRequest, converted here to the service point the
// WritePoint verb writes; a *service.WritePointRequest is served as WritePoint.
func handleTSWrite(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *timeseries.WriteRequest:
		if r == nil {
			return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "Write", "*timeseries.WriteRequest", typeNameOf(req))
		}
		return svc.WritePoint(ctx, &service.WritePointRequest{
			Metric:    r.Metric,
			Tags:      r.Tags,
			Value:     r.Value,
			Timestamp: r.Timestamp,
		})
	case *service.WritePointRequest:
		return handleWritePoint(ctx, svc, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "Write", "*timeseries.WriteRequest", typeNameOf(req))
	}
}

func handleWriteBatch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.WriteBatchRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "WriteBatch", "*service.WriteBatchRequest", typeNameOf(req))
	}
	return svc.WriteBatch(ctx, r)
}

func handleTSQuery(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *timeseries.QueryRequest:
		if r == nil {
			return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "Query", "*timeseries.QueryRequest", typeNameOf(req))
		}
		svcReq := &service.TSQueryRequest{
			Metric:      r.Metric,
			Tags:        r.Tags,
			StartTime:   r.Start,
			EndTime:     r.End,
			IntervalSec: int64(r.Interval / time.Second),
		}
		if r.Aggregate != timeseries.AggregateNone {
			svcReq.Aggregation = strings.ToLower(r.Aggregate.String())
		}
		resp, err := svc.TSQuery(ctx, svcReq)
		if err != nil {
			return nil, err
		}
		return tsQueryResponseToSDK(resp), nil
	case *service.TSQueryRequest:
		return svc.TSQuery(ctx, r)
	default:
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "Query", "*timeseries.QueryRequest", typeNameOf(req))
	}
}

// tsQueryResponseToSDK converts a service query response into the points the
// timeseries client reads.
func tsQueryResponseToSDK(resp *service.TSQueryResponse) *timeseries.QueryResponse {
	out := &timeseries.QueryResponse{Series: []timeseries.Point{}}
	if resp == nil {
		return out
	}
	for _, p := range resp.Points {
		if p == nil {
			continue
		}
		out.Series = append(out.Series, timeseries.Point{Timestamp: p.Timestamp, Value: p.Value})
	}
	return out
}

func handleDeleteSeries(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.TSDeleteRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "DeleteSeries", "*service.TSDeleteRequest", typeNameOf(req))
	}
	return svc.DeleteSeries(ctx, r)
}

func handleGetRetentionPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.TSGetRetentionPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "GetRetentionPolicy", "*service.TSGetRetentionPolicyRequest", typeNameOf(req))
	}
	return svc.GetRetentionPolicy(ctx, r)
}

func handleSetRetentionPolicy(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.TSSetRetentionPolicyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "SetRetentionPolicy", "*service.TSSetRetentionPolicyRequest", typeNameOf(req))
	}
	return svc.SetRetentionPolicy(ctx, r)
}

func handleListMetrics(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.TSListMetricsRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "ListMetrics", "*service.TSListMetricsRequest", typeNameOf(req))
	}
	return svc.ListMetrics(ctx, r)
}

// =============================================================================
// Operations Handlers
// =============================================================================

// The operations handlers serve the SDK operations client, which sends the
// *operations.*Request types (as the gRPC transport accepts them) and
// type-asserts *operations.*Response results. A *service.*Request passed by
// an embedder calling the transport directly is served unchanged.

func handleMigrateData(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *operations.MigrateDataRequest:
		if r == nil {
			break
		}
		resp, err := svc.MigrateData(ctx, &service.MigrateDataRequest{
			SourceShard: r.SourceShard,
			TargetShard: r.TargetShard,
			EntityTypes: r.EntityTypes,
			Concurrency: r.Concurrency,
		})
		if err != nil || resp == nil {
			return nil, err
		}
		return &operations.MigrateDataResponse{
			MigrationID:       resp.MigrationID,
			Status:            resp.Status,
			EstimatedDuration: resp.EstimatedDuration,
		}, nil
	case *service.MigrateDataRequest:
		return svc.MigrateData(ctx, r)
	}
	return nil, NewInvalidRequestTypeError("quicbaas.Operations", "MigrateData", "*operations.MigrateDataRequest", typeNameOf(req))
}

func handleGetMigrationProgress(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *operations.GetMigrationProgressRequest:
		if r == nil {
			break
		}
		resp, err := svc.GetMigrationProgress(ctx, &service.GetMigrationProgressRequest{MigrationID: r.MigrationID})
		if err != nil || resp == nil {
			return nil, err
		}
		return &operations.GetMigrationProgressResponse{
			MigrationID:   resp.MigrationID,
			Progress:      resp.Progress,
			Status:        resp.Status,
			ItemsMigrated: resp.ItemsMigrated,
			TotalItems:    resp.TotalItems,
			ETA:           resp.ETA,
		}, nil
	case *service.GetMigrationProgressRequest:
		return svc.GetMigrationProgress(ctx, r)
	}
	return nil, NewInvalidRequestTypeError("quicbaas.Operations", "GetMigrationProgress", "*operations.GetMigrationProgressRequest", typeNameOf(req))
}

func handleReplaceNode(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *operations.ReplaceNodeRequest:
		if r == nil {
			break
		}
		resp, err := svc.ReplaceNode(ctx, &service.ReplaceNodeRequest{
			ShardID:    r.ShardID,
			OldNodeID:  r.OldNodeID,
			NewNodeID:  r.NewNodeID,
			NewAddress: r.NewAddress,
		})
		if err != nil || resp == nil {
			return nil, err
		}
		return &operations.ReplaceNodeResponse{JobID: resp.JobID, Status: resp.Status}, nil
	case *service.ReplaceNodeRequest:
		return svc.ReplaceNode(ctx, r)
	}
	return nil, NewInvalidRequestTypeError("quicbaas.Operations", "ReplaceNode", "*operations.ReplaceNodeRequest", typeNameOf(req))
}

func handleUpgradeNode(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *operations.UpgradeNodeRequest:
		if r == nil {
			break
		}
		// NodeID 0 is the "upgrade all nodes" sentinel and is passed through.
		resp, err := svc.UpgradeNode(ctx, &service.UpgradeNodeRequest{NodeID: r.NodeID, TargetVersion: r.TargetVersion})
		if err != nil || resp == nil {
			return nil, err
		}
		return &operations.UpgradeNodeResponse{JobID: resp.JobID, Status: resp.Status}, nil
	case *service.UpgradeNodeRequest:
		return svc.UpgradeNode(ctx, r)
	}
	return nil, NewInvalidRequestTypeError("quicbaas.Operations", "UpgradeNode", "*operations.UpgradeNodeRequest", typeNameOf(req))
}

func handleGetUpgradeStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	switch r := req.(type) {
	case *operations.GetUpgradeStatusRequest:
		if r == nil {
			break
		}
		resp, err := svc.GetUpgradeStatus(ctx, &service.GetUpgradeStatusRequest{JobID: r.JobID})
		if err != nil || resp == nil {
			return nil, err
		}
		return &operations.GetUpgradeStatusResponse{
			JobID:          resp.JobID,
			Progress:       resp.Progress,
			Status:         resp.Status,
			CurrentVersion: resp.CurrentVersion,
			TargetVersion:  resp.TargetVersion,
		}, nil
	case *service.GetUpgradeStatusRequest:
		return svc.GetUpgradeStatus(ctx, r)
	}
	return nil, NewInvalidRequestTypeError("quicbaas.Operations", "GetUpgradeStatus", "*operations.GetUpgradeStatusRequest", typeNameOf(req))
}
