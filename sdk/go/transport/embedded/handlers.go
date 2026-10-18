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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/cluster"
	healthclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
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
// Transaction Handlers
// =============================================================================

func handleTransaction(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.TransactionRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Transaction", "Execute", "*service.TransactionRequest", typeNameOf(req))
	}
	return svc.Transaction(ctx, r)
}

// =============================================================================
// Cluster Handlers
// =============================================================================

func handleClusterStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.ClusterStatusRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "Status", "*service.ClusterStatusRequest", typeNameOf(req))
	}
	return svc.ClusterStatus(ctx, r)
}

func handleGetLeader(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetLeaderRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "GetLeader", "*service.GetLeaderRequest", typeNameOf(req))
	}
	return svc.GetLeader(ctx, r)
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

func handleRemoveNode(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.RemoveNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "RemoveNode", "*service.RemoveNodeRequest", typeNameOf(req))
	}
	return svc.RemoveNode(ctx, r)
}

func handleCreateShard(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.CreateShardRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "CreateShard", "*service.CreateShardRequest", typeNameOf(req))
	}
	return svc.CreateShard(ctx, r)
}

func handleRebalanceCluster(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.RebalanceClusterRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "Rebalance", "*service.RebalanceClusterRequest", typeNameOf(req))
	}
	return svc.RebalanceCluster(ctx, r)
}

func handleGetRebalanceProgress(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetRebalanceProgressRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Cluster", "GetRebalanceStatus", "*service.GetRebalanceProgressRequest", typeNameOf(req))
	}
	return svc.GetRebalanceProgress(ctx, r)
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
	r, ok := req.(*service.LoginRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Auth", "Login", "*service.LoginRequest", typeNameOf(req))
	}
	return svc.Login(ctx, r)
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

func handleCreateAuthRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.AuthCreateRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "CreateRole", "*service.AuthCreateRoleRequest", typeNameOf(req))
	}
	return svc.CreateAuthRole(ctx, r)
}

func handleDeleteAuthRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.AuthDeleteRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "DeleteRole", "*service.AuthDeleteRoleRequest", typeNameOf(req))
	}
	return svc.DeleteAuthRole(ctx, r)
}

func handleListAuthRoles(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.AuthListRolesRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "ListRoles", "*service.AuthListRolesRequest", typeNameOf(req))
	}
	return svc.ListAuthRoles(ctx, r)
}

func handleAssignRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.AuthAssignRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "AssignRole", "*service.AuthAssignRoleRequest", typeNameOf(req))
	}
	return svc.AssignRole(ctx, r)
}

func handleRevokeRole(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.AuthRevokeRoleRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.RBAC", "RevokeRole", "*service.AuthRevokeRoleRequest", typeNameOf(req))
	}
	return svc.RevokeRole(ctx, r)
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

func handleWriteBatch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.WriteBatchRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "WriteBatch", "*service.WriteBatchRequest", typeNameOf(req))
	}
	return svc.WriteBatch(ctx, r)
}

func handleTSQuery(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.TSQueryRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.TimeSeries", "Query", "*service.TSQueryRequest", typeNameOf(req))
	}
	return svc.TSQuery(ctx, r)
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

func handleMigrateData(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.MigrateDataRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Operations", "MigrateData", "*service.MigrateDataRequest", typeNameOf(req))
	}
	return svc.MigrateData(ctx, r)
}

func handleGetMigrationProgress(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetMigrationProgressRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Operations", "GetMigrationProgress", "*service.GetMigrationProgressRequest", typeNameOf(req))
	}
	return svc.GetMigrationProgress(ctx, r)
}

func handleReplaceNode(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.ReplaceNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Operations", "ReplaceNode", "*service.ReplaceNodeRequest", typeNameOf(req))
	}
	return svc.ReplaceNode(ctx, r)
}

func handleUpgradeNode(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.UpgradeNodeRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Operations", "UpgradeNode", "*service.UpgradeNodeRequest", typeNameOf(req))
	}
	return svc.UpgradeNode(ctx, r)
}

func handleGetUpgradeStatus(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	r, ok := req.(*service.GetUpgradeStatusRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Operations", "GetUpgradeStatus", "*service.GetUpgradeStatusRequest", typeNameOf(req))
	}
	return svc.GetUpgradeStatus(ctx, r)
}
