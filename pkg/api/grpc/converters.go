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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// protoToServiceConsistency converts proto consistency level to service consistency level.
func protoToServiceConsistency(level pb.ConsistencyLevel) service.ConsistencyLevel {
	switch level {
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_STRONG:
		return service.ConsistencyLevelStrong
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_LINEARIZABLE:
		return service.ConsistencyLevelLinearizable
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_EVENTUAL:
		return service.ConsistencyLevelEventual
	case pb.ConsistencyLevel_CONSISTENCY_LEVEL_UNSPECIFIED:
		return service.ConsistencyLevelLinearizable // Default to Linearizable
	default:
		return service.ConsistencyLevelLinearizable
	}
}

// serviceHealthToProto converts service health status to proto health status.
func serviceHealthToProto(status service.HealthStatus) pb.HealthStatus {
	switch status {
	case service.HealthStatusHealthy:
		return pb.HealthStatus_HEALTH_STATUS_HEALTHY
	case service.HealthStatusDegraded:
		return pb.HealthStatus_HEALTH_STATUS_DEGRADED
	case service.HealthStatusUnhealthy:
		return pb.HealthStatus_HEALTH_STATUS_UNHEALTHY
	default:
		return pb.HealthStatus_HEALTH_STATUS_UNKNOWN
	}
}

// protoToServiceAggregateFunction converts proto aggregate function to service aggregate function.
func protoToServiceAggregateFunction(fn pb.AggregateFunction) service.AggregateFunction {
	switch fn {
	case pb.AggregateFunction_AGGREGATE_FUNCTION_SUM:
		return service.AggregateFunctionSum
	case pb.AggregateFunction_AGGREGATE_FUNCTION_AVG:
		return service.AggregateFunctionAvg
	case pb.AggregateFunction_AGGREGATE_FUNCTION_MIN:
		return service.AggregateFunctionMin
	case pb.AggregateFunction_AGGREGATE_FUNCTION_MAX:
		return service.AggregateFunctionMax
	case pb.AggregateFunction_AGGREGATE_FUNCTION_COUNT:
		return service.AggregateFunctionCount
	default:
		return service.AggregateFunctionUnspecified
	}
}

// protoToServiceCompareTarget converts proto compare target to service compare target.
func protoToServiceCompareTarget(target pb.CompareTarget) service.CompareTarget {
	switch target {
	case pb.CompareTarget_COMPARE_TARGET_VERSION:
		return service.CompareTargetVersion
	case pb.CompareTarget_COMPARE_TARGET_VALUE:
		return service.CompareTargetValue
	case pb.CompareTarget_COMPARE_TARGET_EXISTS:
		return service.CompareTargetExists
	default:
		return service.CompareTargetUnspecified
	}
}

// protoToServiceCompareOperator converts proto compare operator to service compare operator.
func protoToServiceCompareOperator(op pb.CompareOperator) service.CompareOperator {
	switch op {
	case pb.CompareOperator_COMPARE_OPERATOR_EQUAL:
		return service.CompareOperatorEqual
	case pb.CompareOperator_COMPARE_OPERATOR_NOT_EQUAL:
		return service.CompareOperatorNotEqual
	case pb.CompareOperator_COMPARE_OPERATOR_GREATER:
		return service.CompareOperatorGreater
	case pb.CompareOperator_COMPARE_OPERATOR_LESS:
		return service.CompareOperatorLess
	case pb.CompareOperator_COMPARE_OPERATOR_GREATER_EQUAL:
		return service.CompareOperatorGreaterEqual
	case pb.CompareOperator_COMPARE_OPERATOR_LESS_EQUAL:
		return service.CompareOperatorLessEqual
	default:
		return service.CompareOperatorUnspecified
	}
}

// protoToServiceTransactionOpType converts proto transaction op type to service transaction op type.
func protoToServiceTransactionOpType(opType pb.TransactionOpType) service.TransactionOpType {
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

// serviceTransactionOpTypeToProto converts service transaction op type to proto transaction op type.
func serviceTransactionOpTypeToProto(opType service.TransactionOpType) pb.TransactionOpType {
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

// toServiceError converts errors to gRPC status errors
// This wraps the existing toProtoError function for consistency.
func toServiceError(err error) error {
	if err == nil {
		return nil
	}

	// Check if it's already a gRPC status error
	if _, ok := status.FromError(err); ok {
		return err
	}

	// Check if it's a DragonError.
	var dragonErr *errors.DragonError
	if !stderrors.As(err, &dragonErr) {
		// Wrap unknown errors as internal
		dragonErr = &errors.DragonError{
			Code: errors.ErrInternal,
			Op:   "unknown",
			Err:  err,
		}
	}

	// Map DragonError code to gRPC code
	code := mapServiceErrorCode(dragonErr.Code)
	message := dragonErr.Error()

	return status.Error(code, message)
}

// mapServiceErrorCode maps QuicBaaS error codes to gRPC status codes.
func mapServiceErrorCode(errCode errors.ErrorCode) codes.Code {
	switch errCode {
	case errors.ErrNotFound:
		return codes.NotFound
	case errors.ErrConflict, errors.ErrAlreadyExists:
		return codes.AlreadyExists
	case errors.ErrTimeout, errors.ErrDeadlineExceeded:
		return codes.DeadlineExceeded
	case errors.ErrInvalidInput, errors.ErrValidation, errors.ErrOutOfRange:
		return codes.InvalidArgument
	case errors.ErrUnauthorized:
		return codes.Unauthenticated
	case errors.ErrForbidden:
		return codes.PermissionDenied
	case errors.ErrUnavailable, errors.ErrLeaderNotAvailable, errors.ErrClusterNotReady:
		return codes.Unavailable
	case errors.ErrUnimplemented:
		return codes.Unimplemented
	case errors.ErrResourceExhausted:
		return codes.ResourceExhausted
	case errors.ErrPreconditionFailed:
		return codes.FailedPrecondition
	case errors.ErrAborted:
		return codes.Aborted
	case errors.ErrCanceled:
		return codes.Canceled
	case errors.ErrDataLoss:
		return codes.DataLoss
	default:
		return codes.Internal
	}
}
