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
	stderrors "errors"
	"fmt"

	"google.golang.org/grpc/status"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// toProtoError converts a QBError to a gRPC status error.
func toProtoError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a gRPC status error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Check if it's a QBError.
	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) {
		// Wrap unknown errors as internal.
		qbErr = &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "unknown",
			Err:  err,
		}
	}

	// Map QBError code to gRPC code.
	code := errorCodeToGRPCCode(qbErr.Code)
	message := qbErr.Error()

	return status.Error(code, message)
}

// Validation functions

// validatePutRequest validates a Put request.
func validatePutRequest(req *pb.PutRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validatePutRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	// Note: Empty data (len == 0) is allowed - this is valid in key-value stores
	// The existence of a key with empty value is semantically different from key not existing

	// Validate consistency level
	if req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED &&
		req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG &&
		req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE &&
		req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL {
		return api.NewInvalidFormatMessageError("consistency level", fmt.Sprintf("invalid consistency level: %v", req.Consistency))
	}

	return nil
}

// validateGetRequest validates a Get request.
func validateGetRequest(req *pb.GetRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateGetRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	if req.Id == 0 {
		return api.NewRequiredFieldError("id")
	}

	return nil
}

// validateDeleteRequest validates a Delete request.
func validateDeleteRequest(req *pb.DeleteRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateDeleteRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	if req.Id == 0 {
		return api.NewRequiredFieldError("id")
	}

	return nil
}

// validateListRequest validates a List request.
func validateListRequest(req *pb.ListRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateListRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	if req.PageSize > 10000 {
		return api.NewInvalidInputError("page_size cannot exceed 10000")
	}

	return nil
}

// validateQueryRequest validates a Query request.
func validateQueryRequest(req *pb.QueryRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateQueryRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	if req.IndexName == "" {
		return api.NewRequiredFieldError("index_name")
	}

	// Must specify either exact value or range
	hasValue := len(req.Value) > 0
	hasRange := len(req.RangeStart) > 0 || len(req.RangeEnd) > 0

	if !hasValue && !hasRange {
		return api.NewInvalidInputError("must specify either value or range")
	}

	if req.PageSize > 10000 {
		return api.NewInvalidInputError("page_size cannot exceed 10000")
	}

	return nil
}

// validateScanRequest validates a ScanIndex request.
func validateScanRequest(req *pb.ScanRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateScanRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	if req.IndexName == "" {
		return api.NewRequiredFieldError("index_name")
	}

	if req.PageSize > 10000 {
		return api.NewInvalidInputError("page_size cannot exceed 10000")
	}

	return nil
}

// validateAggregateRequest validates an Aggregate request.
func validateAggregateRequest(req *pb.AggregateRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateAggregateRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	if req.FieldName == "" {
		return api.NewRequiredFieldError("field_name")
	}

	if req.Function == pb.AggregateFunction_AGGREGATE_FUNCTION_UNSPECIFIED {
		return api.NewRequiredFieldError("aggregate function")
	}

	if req.StartTime >= req.EndTime {
		return api.NewInvalidInputError("start_time must be less than end_time")
	}

	return nil
}

// validateTimeSeriesQueryRequest validates a TimeSeriesQuery request.
func validateTimeSeriesQueryRequest(req *pb.TimeSeriesQueryRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateTimeSeriesQueryRequest", "request")
	}

	if req.EntityType == "" {
		return api.NewRequiredFieldError("entity_type")
	}

	if req.StartTime >= req.EndTime {
		return api.NewInvalidInputError("start_time must be less than end_time")
	}

	if req.PageSize > 10000 {
		return api.NewInvalidInputError("page_size cannot exceed 10000")
	}

	return nil
}

// validateTransactionRequest validates a Transaction request.
func validateTransactionRequest(req *pb.TransactionRequest) error {
	if req == nil {
		return api.NewNilInputError("grpc.validateTransactionRequest", "request")
	}

	// Must have at least one compare, success, or failure operation
	if len(req.Compare) == 0 && len(req.Success) == 0 && len(req.Failure) == 0 {
		return api.NewInvalidInputError("transaction must have at least one compare, success, or failure operation")
	}

	// Must have at least one success or failure operation
	if len(req.Success) == 0 && len(req.Failure) == 0 {
		return api.NewInvalidInputError("transaction must have at least one success or failure operation")
	}

	// Validate compare conditions
	for i, cmp := range req.Compare {
		if err := validateCompareCondition(cmp, i); err != nil {
			return err
		}
	}

	// Validate success operations
	for i, op := range req.Success {
		if err := validateTransactionOp(op, i, "success"); err != nil {
			return err
		}
	}

	// Validate failure operations
	for i, op := range req.Failure {
		if err := validateTransactionOp(op, i, "failure"); err != nil {
			return err
		}
	}

	// Validate consistency level
	if req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED &&
		req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG &&
		req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE &&
		req.Consistency != pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL {
		return api.NewInvalidFormatMessageError("consistency level", fmt.Sprintf("invalid consistency level: %v", req.Consistency))
	}

	return nil
}

// validateCompareCondition validates a single compare condition.
func validateCompareCondition(cmp *pb.CompareCondition, index int) error {
	if cmp == nil {
		return api.NewBatchItemValidationError(index, "compare condition cannot be nil")
	}

	if cmp.EntityType == "" {
		return api.NewBatchItemValidationError(index, "entity_type is required in compare")
	}

	if cmp.Id == 0 {
		return api.NewBatchItemValidationError(index, "id is required in compare")
	}

	// Validate target
	if cmp.Target == pb.CompareTarget_COMPARE_TARGET_UNSPECIFIED {
		return api.NewBatchItemValidationError(index, "target is required in compare")
	}

	// Validate operator
	if cmp.Operator == pb.CompareOperator_COMPARE_OPERATOR_UNSPECIFIED {
		return api.NewBatchItemValidationError(index, "operator is required in compare")
	}

	// Validate operator is valid for target
	switch cmp.Target {
	case pb.CompareTarget_COMPARE_TARGET_EXISTS:
		// EXISTS only supports EQUAL and NOT_EQUAL
		if cmp.Operator != pb.CompareOperator_COMPARE_OPERATOR_EQUAL &&
			cmp.Operator != pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL {
			return api.NewBatchItemValidationError(index, "EXISTS target only supports EQUAL and NOT_EQUAL operators")
		}
	case pb.CompareTarget_COMPARE_TARGET_VERSION:
		// VERSION requires version field
		if cmp.Version == 0 {
			return api.NewBatchItemValidationError(index, "version is required for VERSION target")
		}
	case pb.CompareTarget_COMPARE_TARGET_VALUE:
		// VALUE requires value field
		if len(cmp.Value) == 0 {
			return api.NewBatchItemValidationError(index, "value is required for VALUE target")
		}
	}

	return nil
}

// validateTransactionOp validates a single transaction operation.
func validateTransactionOp(op *pb.TransactionOp, index int, branch string) error {
	if op == nil {
		return api.NewBatchItemValidationError(index, branch+" operation cannot be nil")
	}

	if op.EntityType == "" {
		return api.NewBatchItemValidationError(index, "entity_type is required in "+branch)
	}

	if op.Id == 0 {
		return api.NewBatchItemValidationError(index, "id is required in "+branch)
	}

	// Validate operation type
	if op.OpType == pb.TransactionOpType_TRANSACTION_OP_TYPE_UNSPECIFIED {
		return api.NewBatchItemValidationError(index, "operation type is required in "+branch)
	}

	// PUT operations require data
	if op.OpType == pb.TransactionOpType_TRANSACTION_OP_TYPE_PUT {
		if len(op.Data) == 0 {
			return api.NewBatchItemValidationError(index, "data is required for PUT operation in "+branch)
		}
	}

	// GET and DELETE operations should not have data
	if (op.OpType == pb.TransactionOpType_TRANSACTION_OP_TYPE_GET ||
		op.OpType == pb.TransactionOpType_TRANSACTION_OP_TYPE_DELETE) &&
		len(op.Data) > 0 {
		return api.NewBatchItemValidationError(index, fmt.Sprintf("%s operation should not have data in %s", op.OpType, branch))
	}

	return nil
}

// protoToConsistencyLevel converts proto consistency level to SDK transport ConsistencyLevel.
func protoToConsistencyLevel(level pb.ConsistencyLevel) qrdbtransport.ConsistencyLevel {
	switch level {
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG:
		return qrdbtransport.ConsistencyStrong
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE:
		return qrdbtransport.ConsistencyLinearizable
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL:
		return qrdbtransport.ConsistencyEventual
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED:
		return qrdbtransport.ConsistencyLinearizable // Default to Linearizable
	default:
		return qrdbtransport.ConsistencyLinearizable
	}
}

// consistencyLevelToProto converts SDK transport ConsistencyLevel to proto type.
func consistencyLevelToProto(level qrdbtransport.ConsistencyLevel) pb.ConsistencyLevel {
	switch level {
	case qrdbtransport.ConsistencyStrong:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG
	case qrdbtransport.ConsistencyLinearizable:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE
	case qrdbtransport.ConsistencyEventual:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL
	default:
		return pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE
	}
}
