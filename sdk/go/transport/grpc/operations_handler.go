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
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/operations"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// operationsServicePrefix is the gRPC-style service identifier the SDK
// operations client routes through. The SDK operations client uses plain Go
// structs that do not implement proto.Message, so the bare conn.Invoke path
// cannot serialize them. They must be converted to the generated QuicBaaS
// proto types (UpgradeNodeRequest, MigrateDataRequest, ...) first. The
// operations RPCs are served by the QuicBaaS gRPC service (quicbaas.v1.QuicBaaS),
// not a dedicated OperationsService.
const operationsServicePrefix = "quicbaas.Operations"

// isOperationsService reports whether the service name targets the
// quicbaas.Operations surface so doRequest can branch into the typed
// operations handler.
func isOperationsService(svc string) bool {
	return strings.HasPrefix(svc, operationsServicePrefix)
}

// operationsHandler converts between the SDK operations client structs and the
// generated QuicBaaS proto types, then calls the gRPC QuicBaaS service
// registered by the server lifecycle.
type operationsHandler struct {
	conn *grpc.ClientConn
}

// newOperationsHandler creates a new operations handler bound to conn.
func newOperationsHandler(conn *grpc.ClientConn) *operationsHandler {
	return &operationsHandler{conn: conn}
}

// operationsHandlerMap maps method names to handler functions for O(1)
// dispatch. Every method exposed by the SDK operations.Client interface is
// wired here so no operations verb falls through to the untyped conn.Invoke
// path that produced the proto-marshal failure.
var operationsHandlerMap = map[string]func(h *operationsHandler, ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error){
	"MigrateData":          (*operationsHandler).handleMigrateData,
	"GetMigrationProgress": (*operationsHandler).handleGetMigrationProgress,
	"ReplaceNode":          (*operationsHandler).handleReplaceNode,
	"UpgradeNode":          (*operationsHandler).handleUpgradeNode,
	"GetUpgradeStatus":     (*operationsHandler).handleGetUpgradeStatus,
}

// handleRequest routes Operations requests to the appropriate handler based on
// method name.
func (h *operationsHandler) handleRequest(ctx context.Context, _ string, method string, req interface{}) (interface{}, error) {
	client := pb.NewQuicBaaSClient(h.conn)

	handler, ok := operationsHandlerMap[method]
	if !ok {
		return nil, errors.NewUnimplemented("operationsHandler.handleRequest",
			"method not exposed by QuicBaaS operations gRPC proto: "+method)
	}

	return handler(h, ctx, client, req)
}

// handleUpgradeNode converts the SDK upgrade request to proto, calls the gRPC
// QuicBaaS service, and converts the proto response back. A NodeID of 0 is the
// canonical "upgrade all nodes" sentinel and is forwarded verbatim (proto
// field node_id == 0), never rejected.
func (h *operationsHandler) handleUpgradeNode(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*operations.UpgradeNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("operationsHandler.handleUpgradeNode",
			"invalid request type, expected *operations.UpgradeNodeRequest")
	}

	pbReq := &pb.UpgradeNodeRequest{
		NodeId:        sdkReq.NodeID,
		TargetVersion: sdkReq.TargetVersion,
	}

	pbResp, err := client.UpgradeNode(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "operationsHandler.handleUpgradeNode")
	}

	// The proto UpgradeNodeResponse carries operation_id + started. The SDK
	// response exposes JobID + Status; the started timestamp has no SDK field,
	// so the operation id maps to JobID and Status is left to the caller's
	// GetUpgradeStatus follow-up (the server returns no synchronous status).
	return &operations.UpgradeNodeResponse{
		JobID:  pbResp.OperationId,
		Status: "",
	}, nil
}

// handleGetUpgradeStatus converts the SDK status request to proto, calls the
// gRPC QuicBaaS service, and converts the proto response back. The SDK JobID
// maps to the proto operation_id field.
func (h *operationsHandler) handleGetUpgradeStatus(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*operations.GetUpgradeStatusRequest)
	if !ok {
		return nil, errors.NewInvalidInput("operationsHandler.handleGetUpgradeStatus",
			"invalid request type, expected *operations.GetUpgradeStatusRequest")
	}

	pbReq := &pb.GetUpgradeStatusRequest{
		OperationId: sdkReq.JobID,
	}

	pbResp, err := client.GetUpgradeStatus(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "operationsHandler.handleGetUpgradeStatus")
	}

	return &operations.GetUpgradeStatusResponse{
		JobID:          pbResp.OperationId,
		Progress:       pbResp.PercentComplete,
		Status:         pbResp.Status,
		CurrentVersion: pbResp.CurrentVersion,
		TargetVersion:  pbResp.TargetVersion,
	}, nil
}

// handleMigrateData converts the SDK migrate request to proto, calls the gRPC
// QuicBaaS service, and converts the proto response back. The SDK shard IDs
// (SourceShard/TargetShard) map to the proto source_backend/destination_backend
// string fields via their decimal representation so the server's non-empty
// validation is satisfied.
func (h *operationsHandler) handleMigrateData(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*operations.MigrateDataRequest)
	if !ok {
		return nil, errors.NewInvalidInput("operationsHandler.handleMigrateData",
			"invalid request type, expected *operations.MigrateDataRequest")
	}

	pbReq := &pb.MigrateDataRequest{
		SourceBackend:      strconv.FormatUint(sdkReq.SourceShard, 10),
		DestinationBackend: strconv.FormatUint(sdkReq.TargetShard, 10),
		Concurrency:        int32(sdkReq.Concurrency),
	}

	pbResp, err := client.MigrateData(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "operationsHandler.handleMigrateData")
	}

	return &operations.MigrateDataResponse{
		MigrationID:       pbResp.OperationId,
		Status:            "",
		EstimatedDuration: pbResp.Started,
	}, nil
}

// handleGetMigrationProgress converts the SDK progress request to proto, calls
// the gRPC QuicBaaS service, and converts the proto response back. The SDK
// MigrationID maps to the proto operation_id field.
func (h *operationsHandler) handleGetMigrationProgress(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*operations.GetMigrationProgressRequest)
	if !ok {
		return nil, errors.NewInvalidInput("operationsHandler.handleGetMigrationProgress",
			"invalid request type, expected *operations.GetMigrationProgressRequest")
	}

	pbReq := &pb.GetMigrationProgressRequest{
		OperationId: sdkReq.MigrationID,
	}

	pbResp, err := client.GetMigrationProgress(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "operationsHandler.handleGetMigrationProgress")
	}

	return &operations.GetMigrationProgressResponse{
		MigrationID:   pbResp.OperationId,
		Progress:      pbResp.PercentComplete,
		Status:        pbResp.Status,
		ItemsMigrated: pbResp.KeysProcessed,
		TotalItems:    pbResp.EstimatedTotal,
		ETA:           pbResp.EstimatedRemaining,
	}, nil
}

// handleReplaceNode converts the SDK replace request to proto, calls the gRPC
// QuicBaaS service, and converts the proto response back. The SDK OldNodeID/
// NewNodeID/NewAddress map to the proto failed_node_id/replacement_node_id/
// replacement_address fields (matching the server's ReplaceNode mapping).
func (h *operationsHandler) handleReplaceNode(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*operations.ReplaceNodeRequest)
	if !ok {
		return nil, errors.NewInvalidInput("operationsHandler.handleReplaceNode",
			"invalid request type, expected *operations.ReplaceNodeRequest")
	}

	pbReq := &pb.ReplaceNodeRequest{
		FailedNodeId:       sdkReq.OldNodeID,
		ReplacementNodeId:  sdkReq.NewNodeID,
		ReplacementAddress: sdkReq.NewAddress,
	}

	pbResp, err := client.ReplaceNode(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "operationsHandler.handleReplaceNode")
	}

	// The proto ReplaceNodeResponse carries success + message; the server sets
	// message to the service-layer status string and success to (JobID != "").
	// Mirror that contract back onto the SDK JobID/Status fields.
	return &operations.ReplaceNodeResponse{
		JobID:  pbResp.Message,
		Status: pbResp.Message,
	}, nil
}
