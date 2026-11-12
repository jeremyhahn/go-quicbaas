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
	"io"
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// KV service prefixes for routing.
const (
	kvServicePrefix          = "quicbaas.KV"
	batchServicePrefix       = "quicbaas.Batch"
	transactionServicePrefix = "quicbaas.Transaction"
	quicBaaSServicePrefix    = "quicbaas.v1.QuicBaaS"
)

// isKVService checks if the service name is a KV/Batch/Transaction service.
func isKVService(svc string) bool {
	return strings.HasPrefix(svc, kvServicePrefix) ||
		strings.HasPrefix(svc, batchServicePrefix) ||
		strings.HasPrefix(svc, transactionServicePrefix) ||
		strings.HasPrefix(svc, quicBaaSServicePrefix)
}

// kvHandler handles KV/Batch/Transaction service requests by converting between SDK types and protobuf types.
type kvHandler struct {
	conn *grpc.ClientConn
}

// newKVHandler creates a new KV handler.
func newKVHandler(conn *grpc.ClientConn) *kvHandler {
	return &kvHandler{conn: conn}
}

// kvMethodFunc converts one SDK request onto its QuicBaaS RPC and back.
type kvMethodFunc func(h *kvHandler, ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error)

// batchMethods routes the quicbaas.Batch service's verbs, as the batch client
// (sdk/go/client/batch) names them.
var batchMethods = map[string]kvMethodFunc{
	"Put":    (*kvHandler).handleBatchPut,
	"Get":    (*kvHandler).handleBatchGet,
	"Delete": (*kvHandler).handleBatchDelete,
}

// handleRequest routes KV requests to the appropriate handler based on method name.
func (h *kvHandler) handleRequest(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	client := pb.NewQuicBaaSClient(h.conn)

	// The batch client sends quicbaas.Batch with bare verbs (Put, Get,
	// Delete) that would otherwise collide with the KV verbs below.
	if strings.HasPrefix(svc, batchServicePrefix) {
		fn, ok := batchMethods[method]
		if !ok {
			return nil, errors.NewUnimplemented("kvHandler.handleRequest",
				"unsupported batch method: "+method)
		}
		return fn(h, ctx, client, req)
	}

	// Route based on method name.
	switch method {
	// KV operations
	case "Put":
		return h.handlePut(ctx, client, req)
	case "Get":
		return h.handleGet(ctx, client, req)
	case "Delete":
		return h.handleDelete(ctx, client, req)
	case "List":
		return h.handleList(ctx, client, req)
	case "ListEntityTypes":
		return h.handleListEntityTypes(ctx, client, req)
	case "CompareAndSwap":
		return h.handleCompareAndSwap(ctx, client, req)
	case "PutIfAbsent":
		return h.handlePutIfAbsent(ctx, client, req)

	// Batch operations
	case "BatchPut":
		return h.handleBatchPut(ctx, client, req)
	case "BatchGet":
		return h.handleBatchGet(ctx, client, req)
	case "BatchDelete":
		return h.handleBatchDelete(ctx, client, req)

	// Transaction operations
	case "Transaction":
		return h.handleTransaction(ctx, client, req)

	default:
		return nil, errors.NewUnimplemented("kvHandler.handleRequest",
			"unsupported method: "+method)
	}
}

// handlePut handles Put requests.
func (h *kvHandler) handlePut(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.PutRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handlePut", "invalid request type, expected *service.PutRequest")
	}

	// Convert SDK request to protobuf request.
	pbReq := &pb.PutRequest{
		EntityType:  sdkReq.EntityType,
		Id:          sdkReq.ID,
		Data:        sdkReq.Data,
		Consistency: convertConsistencyToProto(sdkReq.Consistency),
		ShardId:     sdkReq.ShardID,
	}

	// Call gRPC.
	pbResp, err := client.Put(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handlePut")
	}

	// Convert protobuf response to SDK response.
	return &service.PutResponse{
		ID:        pbResp.Id,
		Version:   pbResp.Version,
		Timestamp: pbResp.Timestamp,
		LeaderID:  pbResp.LeaderId,
	}, nil
}

func (h *kvHandler) handleCompareAndSwap(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.CompareAndSwapRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("kvHandler.handleCompareAndSwap", "invalid request type, expected *service.CompareAndSwapRequest")
	}

	pbResp, err := client.CompareAndSwap(ctx, &pb.CompareAndSwapRequest{
		EntityType: sdkReq.EntityType,
		Id:         sdkReq.ID,
		Expect:     sdkReq.Expect,
		Set:        sdkReq.Set,
		ShardId:    sdkReq.ShardID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleCompareAndSwap")
	}
	return &service.ConditionalPutResponse{Applied: pbResp.GetApplied()}, nil
}

func (h *kvHandler) handlePutIfAbsent(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.PutIfAbsentRequest)
	if !ok || sdkReq == nil {
		return nil, errors.NewInvalidInput("kvHandler.handlePutIfAbsent", "invalid request type, expected *service.PutIfAbsentRequest")
	}

	pbResp, err := client.PutIfAbsent(ctx, &pb.PutIfAbsentRequest{
		EntityType: sdkReq.EntityType,
		Id:         sdkReq.ID,
		Data:       sdkReq.Data,
		ShardId:    sdkReq.ShardID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handlePutIfAbsent")
	}
	return &service.ConditionalPutResponse{Applied: pbResp.GetApplied()}, nil
}

// handleGet handles Get requests.
func (h *kvHandler) handleGet(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.GetRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleGet", "invalid request type, expected *service.GetRequest")
	}

	// Convert SDK request to protobuf request.
	pbReq := &pb.GetRequest{
		EntityType:  sdkReq.EntityType,
		Id:          sdkReq.ID,
		Consistency: convertConsistencyToProto(sdkReq.Consistency),
		ShardId:     sdkReq.ShardID,
		TimeoutMs:   int64(sdkReq.TimeoutMs),
	}

	// Call gRPC.
	pbResp, err := client.Get(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleGet")
	}

	// Normalize data: proto3 serializes empty bytes as nil (zero value).
	// When Found is true but Data is nil, it means an empty value was stored.
	// Normalize nil to empty slice for consistent SDK behavior.
	data := pbResp.Data
	if pbResp.Found && data == nil {
		data = []byte{}
	}

	// Convert protobuf response to SDK response.
	return &service.GetResponse{
		ID:        pbResp.Id,
		Data:      data,
		Version:   pbResp.Version,
		Timestamp: pbResp.Timestamp,
		Found:     pbResp.Found,
	}, nil
}

// handleDelete handles Delete requests.
func (h *kvHandler) handleDelete(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.DeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleDelete", "invalid request type, expected *service.DeleteRequest")
	}

	// Convert SDK request to protobuf request.
	pbReq := &pb.DeleteRequest{
		EntityType:  sdkReq.EntityType,
		Id:          sdkReq.ID,
		Consistency: convertConsistencyToProto(sdkReq.Consistency),
		ShardId:     sdkReq.ShardID,
	}

	// Call gRPC.
	pbResp, err := client.Delete(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleDelete")
	}

	// Convert protobuf response to SDK response.
	return &service.DeleteResponse{
		Deleted:   pbResp.Deleted,
		Timestamp: pbResp.Timestamp,
	}, nil
}

// handleList handles List requests (streaming).
func (h *kvHandler) handleList(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.ListRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleList", "invalid request type, expected *service.ListRequest")
	}

	// Convert SDK request to protobuf request.
	pbReq := &pb.ListRequest{
		EntityType:  sdkReq.EntityType,
		PageSize:    uint32(sdkReq.PageSize),
		PageToken:   sdkReq.PageToken,
		Consistency: convertConsistencyToProto(sdkReq.Consistency),
		ShardId:     sdkReq.ShardID,
	}

	// Call gRPC streaming method.
	stream, err := client.List(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleList")
	}

	// Collect all entities from the stream.
	var allEntities []*service.Entity
	var nextPageToken string
	var totalCount uint64

	for {
		resp, err := stream.Recv()
		if err != nil {
			// Check for end of stream
			if err == io.EOF {
				break
			}
			return nil, convertGRPCError(err, "kvHandler.handleList")
		}

		// Convert protobuf entities to SDK entities.
		for _, e := range resp.Entities {
			// Normalize data: proto3 serializes empty bytes as nil.
			data := e.Data
			if data == nil {
				data = []byte{}
			}
			allEntities = append(allEntities, &service.Entity{
				EntityType: e.EntityType,
				ID:         e.Id,
				Data:       data,
				Version:    e.Version,
				Timestamp:  e.Timestamp,
			})
		}
		nextPageToken = resp.NextPageToken
		totalCount = resp.TotalCount
	}

	return &service.ListResponse{
		Entities:      allEntities,
		NextPageToken: nextPageToken,
		TotalCount:    totalCount,
	}, nil
}

// handleListEntityTypes handles ListEntityTypes requests.
func (h *kvHandler) handleListEntityTypes(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.ListEntityTypesRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleListEntityTypes", "invalid request type, expected *service.ListEntityTypesRequest")
	}

	// Convert SDK request to protobuf request.
	pbReq := &pb.ListEntityTypesRequest{
		PageSize:  uint32(sdkReq.PageSize),
		PageToken: sdkReq.PageToken,
	}

	// Call gRPC.
	pbResp, err := client.ListEntityTypes(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleListEntityTypes")
	}

	// Convert protobuf response to SDK response.
	return &service.ListEntityTypesResponse{
		EntityTypes:   pbResp.EntityTypes,
		NextPageToken: pbResp.NextPageToken,
		TotalCount:    int(pbResp.TotalCount),
	}, nil
}

// handleBatchPut handles BatchPut requests.
func (h *kvHandler) handleBatchPut(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.BatchPutRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleBatchPut", "invalid request type, expected *service.BatchPutRequest")
	}

	// Convert SDK requests to protobuf requests.
	pbRequests := make([]*pb.PutRequest, len(sdkReq.Requests))
	for i, r := range sdkReq.Requests {
		pbRequests[i] = &pb.PutRequest{
			EntityType:  r.EntityType,
			Id:          r.ID,
			Data:        r.Data,
			Consistency: convertConsistencyToProto(r.Consistency),
			ShardId:     r.ShardID,
		}
	}

	pbReq := &pb.BatchPutRequest{
		Requests: pbRequests,
		ShardId:  sdkReq.ShardID,
	}

	// Call gRPC.
	pbResp, err := client.BatchPut(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleBatchPut")
	}

	// Convert protobuf responses to SDK responses.
	sdkResponses := make([]*service.PutResponse, len(pbResp.Responses))
	for i, r := range pbResp.Responses {
		sdkResponses[i] = &service.PutResponse{
			ID:        r.Id,
			Version:   r.Version,
			Timestamp: r.Timestamp,
			LeaderID:  r.LeaderId,
		}
	}

	return &service.BatchPutResponse{
		Responses: sdkResponses,
		Timestamp: pbResp.Timestamp,
	}, nil
}

// handleBatchGet handles BatchGet requests.
func (h *kvHandler) handleBatchGet(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.BatchGetRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleBatchGet", "invalid request type, expected *service.BatchGetRequest")
	}

	// Convert SDK requests to protobuf requests.
	pbRequests := make([]*pb.GetRequest, len(sdkReq.Requests))
	for i, r := range sdkReq.Requests {
		pbRequests[i] = &pb.GetRequest{
			EntityType:  r.EntityType,
			Id:          r.ID,
			Consistency: convertConsistencyToProto(r.Consistency),
			ShardId:     r.ShardID,
			TimeoutMs:   int64(r.TimeoutMs),
		}
	}

	pbReq := &pb.BatchGetRequest{
		Requests: pbRequests,
	}

	// Call gRPC.
	pbResp, err := client.BatchGet(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleBatchGet")
	}

	// Convert protobuf responses to SDK responses.
	sdkResponses := make([]*service.GetResponse, len(pbResp.Responses))
	for i, r := range pbResp.Responses {
		// Normalize data: proto3 serializes empty bytes as nil.
		data := r.Data
		if r.Found && data == nil {
			data = []byte{}
		}
		sdkResponses[i] = &service.GetResponse{
			ID:        r.Id,
			Data:      data,
			Version:   r.Version,
			Timestamp: r.Timestamp,
			Found:     r.Found,
		}
	}

	return &service.BatchGetResponse{
		Responses: sdkResponses,
	}, nil
}

// handleBatchDelete handles BatchDelete requests.
func (h *kvHandler) handleBatchDelete(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.BatchDeleteRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleBatchDelete", "invalid request type, expected *service.BatchDeleteRequest")
	}

	// Convert SDK requests to protobuf requests.
	pbRequests := make([]*pb.DeleteRequest, len(sdkReq.Requests))
	for i, r := range sdkReq.Requests {
		pbRequests[i] = &pb.DeleteRequest{
			EntityType:  r.EntityType,
			Id:          r.ID,
			Consistency: convertConsistencyToProto(r.Consistency),
			ShardId:     r.ShardID,
		}
	}

	pbReq := &pb.BatchDeleteRequest{
		Requests: pbRequests,
		ShardId:  sdkReq.ShardID,
	}

	// Call gRPC.
	pbResp, err := client.BatchDelete(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleBatchDelete")
	}

	// Convert protobuf responses to SDK responses.
	sdkResponses := make([]*service.DeleteResponse, len(pbResp.Responses))
	for i, r := range pbResp.Responses {
		sdkResponses[i] = &service.DeleteResponse{
			Deleted:   r.Deleted,
			Timestamp: r.Timestamp,
		}
	}

	return &service.BatchDeleteResponse{
		Responses: sdkResponses,
		Timestamp: pbResp.Timestamp,
	}, nil
}

// handleTransaction handles Transaction requests.
func (h *kvHandler) handleTransaction(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	sdkReq, ok := req.(*service.TransactionRequest)
	if !ok {
		return nil, errors.NewInvalidInput("kvHandler.handleTransaction", "invalid request type, expected *service.TransactionRequest")
	}

	// Convert SDK compare conditions to protobuf.
	pbCompare := make([]*pb.CompareCondition, len(sdkReq.Compare))
	for i, c := range sdkReq.Compare {
		pbCompare[i] = &pb.CompareCondition{
			EntityType: c.EntityType,
			Id:         c.ID,
			Target:     convertCompareTargetToProto(c.Target),
			Operator:   convertCompareOperatorToProto(c.Operator),
			Version:    c.Version,
			Value:      c.Value,
		}
	}

	// Convert SDK success operations to protobuf.
	pbSuccess := make([]*pb.TransactionOp, len(sdkReq.Success))
	for i, op := range sdkReq.Success {
		pbSuccess[i] = &pb.TransactionOp{
			OpType:     convertTransactionOpTypeToProto(op.OpType),
			EntityType: op.EntityType,
			Id:         op.ID,
			Data:       op.Data,
		}
	}

	// Convert SDK failure operations to protobuf.
	pbFailure := make([]*pb.TransactionOp, len(sdkReq.Failure))
	for i, op := range sdkReq.Failure {
		pbFailure[i] = &pb.TransactionOp{
			OpType:     convertTransactionOpTypeToProto(op.OpType),
			EntityType: op.EntityType,
			Id:         op.ID,
			Data:       op.Data,
		}
	}

	pbReq := &pb.TransactionRequest{
		Compare: pbCompare,
		Success: pbSuccess,
		Failure: pbFailure,
		ShardId: sdkReq.ShardID,
	}

	// Call gRPC.
	pbResp, err := client.Transaction(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "kvHandler.handleTransaction")
	}

	// Convert protobuf responses to SDK responses.
	sdkResponses := make([]*service.TransactionOpResponse, len(pbResp.Responses))
	for i, r := range pbResp.Responses {
		// Normalize data: proto3 serializes empty bytes as nil.
		data := r.Data
		if r.Success && data == nil {
			data = []byte{}
		}
		sdkResponses[i] = &service.TransactionOpResponse{
			OpType:     convertTransactionOpTypeFromProto(r.OpType),
			EntityType: r.EntityType,
			ID:         r.Id,
			Data:       data,
			Success:    r.Success,
			Version:    r.Version,
			Timestamp:  r.Timestamp,
			Error:      r.Error,
		}
	}

	return &service.TransactionResponse{
		Succeeded: pbResp.Succeeded,
		Responses: sdkResponses,
		Timestamp: pbResp.Timestamp,
		Message:   pbResp.Message,
		Revision:  pbResp.Revision,
	}, nil
}

// convertConsistencyToProto converts SDK ConsistencyLevel to protobuf ConsistencyLevel.
func convertConsistencyToProto(level service.ConsistencyLevel) pb.ConsistencyLevel {
	switch level {
	case service.ConsistencyLevelStrong:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG
	case service.ConsistencyLevelLinearizable:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE
	case service.ConsistencyLevelEventual:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL
	default:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED
	}
}

// convertCompareTargetToProto converts SDK CompareTarget to protobuf CompareTarget.
func convertCompareTargetToProto(target service.CompareTarget) pb.CompareTarget {
	switch target {
	case service.CompareTargetVersion:
		return pb.CompareTarget_COMPARE_TARGET_VERSION
	case service.CompareTargetValue:
		return pb.CompareTarget_COMPARE_TARGET_VALUE
	case service.CompareTargetExists:
		return pb.CompareTarget_COMPARE_TARGET_EXISTS
	default:
		return pb.CompareTarget_COMPARE_TARGET_UNSPECIFIED
	}
}

// convertCompareOperatorToProto converts SDK CompareOperator to protobuf CompareOperator.
func convertCompareOperatorToProto(op service.CompareOperator) pb.CompareOperator {
	switch op {
	case service.CompareOperatorEqual:
		return pb.CompareOperator_COMPARE_OPERATOR_EQUAL
	case service.CompareOperatorNotEqual:
		return pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL
	case service.CompareOperatorGreater:
		return pb.CompareOperator_COMPARE_OPERATOR_GREATER
	case service.CompareOperatorLess:
		return pb.CompareOperator_COMPARE_OPERATOR_LESS
	case service.CompareOperatorGreaterEqual:
		return pb.CompareOperator_COMPARE_OPERATOR_GREATER_EQUAL
	case service.CompareOperatorLessEqual:
		return pb.CompareOperator_COMPARE_OPERATOR_LESS_EQUAL
	default:
		return pb.CompareOperator_COMPARE_OPERATOR_UNSPECIFIED
	}
}

// convertTransactionOpTypeToProto converts SDK TransactionOpType to protobuf TransactionOpType.
func convertTransactionOpTypeToProto(opType service.TransactionOpType) pb.TransactionOpType {
	switch opType {
	case service.TransactionOpTypePut:
		return pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT
	case service.TransactionOpTypeGet:
		return pb.TransactionOpType_TRANSACTION_OP_TYPE_GET
	case service.TransactionOpTypeDelete:
		return pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE
	default:
		return pb.TransactionOpType_TRANSACTION_OP_TYPE_UNSPECIFIED
	}
}

// convertTransactionOpTypeFromProto converts protobuf TransactionOpType to SDK TransactionOpType.
func convertTransactionOpTypeFromProto(opType pb.TransactionOpType) service.TransactionOpType {
	switch opType {
	case pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT:
		return service.TransactionOpTypePut
	case pb.TransactionOpType_TRANSACTION_OP_TYPE_GET:
		return service.TransactionOpTypeGet
	case pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE:
		return service.TransactionOpTypeDelete
	default:
		return service.TransactionOpTypeUnspecified
	}
}
