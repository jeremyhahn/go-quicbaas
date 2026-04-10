//go:build rest

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

package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HandleTransaction handles POST requests for transactions with compare-and-swap semantics.
func (s *Server) HandleTransaction(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "failed to read request body",
		})
		return
	}
	defer r.Body.Close()

	// Parse transaction request
	var txnReq TransactionRequest
	if err := json.Unmarshal(body, &txnReq); err != nil {
		RespondError(w, r, http.StatusBadRequest, &ValidationError{
			Message: "invalid JSON body",
		})
		return
	}

	// Validate request
	if err := txnReq.Validate(); err != nil {
		RespondError(w, r, http.StatusBadRequest, err)
		return
	}

	// Log transaction
	s.logger.Debugf("Transaction: compare_count=%d, success_count=%d, failure_count=%d",
		len(txnReq.Compare), len(txnReq.Success), len(txnReq.Failure))

	// Convert to service transaction request
	serviceReq := &service.TransactionRequest{
		Compare: convertCompareConditions(txnReq.Compare),
		Success: convertTransactionOps(txnReq.Success),
		Failure: convertTransactionOps(txnReq.Failure),
	}

	// Call service
	serviceResp, err := s.service.Transaction(ctx, serviceReq)
	if err != nil {
		s.logger.WithError(err).Error("Failed to execute transaction")
		s.handleServiceError(w, r, err)
		return
	}

	// Convert service response to REST response
	responses := make([]TransactionOpResponse, 0, len(serviceResp.Responses))
	for _, opResp := range serviceResp.Responses {
		responses = append(responses, TransactionOpResponse{
			Type:       convertServiceOpTypeToREST(opResp.OpType),
			EntityType: opResp.EntityType,
			ID:         opResp.ID,
			Data:       json.RawMessage(opResp.Data),
			Success:    opResp.Success,
		})
	}

	// Build response
	resp := TransactionResponse{
		Succeeded: serviceResp.Succeeded,
		Responses: responses,
		Timestamp: time.Unix(0, serviceResp.Timestamp),
		Message:   serviceResp.Message,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// Conversion helpers for transaction types

// convertCompareConditions converts REST compare conditions to service compare conditions.
func convertCompareConditions(conditions []CompareCondition) []*service.CompareCondition {
	result := make([]*service.CompareCondition, 0, len(conditions))
	for _, cond := range conditions {
		result = append(result, &service.CompareCondition{
			EntityType: cond.EntityType,
			ID:         cond.ID,
			Target:     convertCompareTarget(cond.Target),
			Operator:   convertCompareOperator(cond.Operator),
			Version:    uint64(cond.Version), //nolint:gosec // G115: Version field conversion for transaction
			Value:      cond.Value,
		})
	}
	return result
}

// convertTransactionOps converts REST transaction ops to service transaction ops.
func convertTransactionOps(ops []TransactionOp) []*service.TransactionOp {
	result := make([]*service.TransactionOp, 0, len(ops))
	for _, op := range ops {
		result = append(result, &service.TransactionOp{
			OpType:     convertTxnOpType(op.Type),
			EntityType: op.EntityType,
			ID:         op.ID,
			Data:       op.Data,
		})
	}
	return result
}

// convertCompareTarget converts REST compare target to service compare target.
func convertCompareTarget(target CompareTarget) service.CompareTarget {
	switch target {
	case CompareTargetVersion:
		return service.CompareTargetVersion
	case CompareTargetValue:
		return service.CompareTargetValue
	case CompareTargetExists:
		return service.CompareTargetExists
	default:
		return service.CompareTargetUnspecified
	}
}

// convertCompareOperator converts REST compare operator to service compare operator.
func convertCompareOperator(op CompareOperator) service.CompareOperator {
	switch op {
	case CompareOpEqual:
		return service.CompareOperatorEqual
	case CompareOpNotEqual:
		return service.CompareOperatorNotEqual
	case CompareOpGreater:
		return service.CompareOperatorGreater
	case CompareOpLess:
		return service.CompareOperatorLess
	case CompareOpGreaterEq:
		return service.CompareOperatorGreaterEqual
	case CompareOpLessEq:
		return service.CompareOperatorLessEqual
	default:
		return service.CompareOperatorUnspecified
	}
}

// convertTxnOpType converts REST transaction op type to service transaction op type.
func convertTxnOpType(opType TransactionOpType) service.TransactionOpType {
	switch opType {
	case TxnOpPut:
		return service.TransactionOpTypePut
	case TxnOpGet:
		return service.TransactionOpTypeGet
	case TxnOpDelete:
		return service.TransactionOpTypeDelete
	default:
		return service.TransactionOpTypeUnspecified
	}
}

// convertServiceOpTypeToREST converts service transaction op type to REST transaction op type.
func convertServiceOpTypeToREST(opType service.TransactionOpType) TransactionOpType {
	switch opType {
	case service.TransactionOpTypePut:
		return TxnOpPut
	case service.TransactionOpTypeGet:
		return TxnOpGet
	case service.TransactionOpTypeDelete:
		return TxnOpDelete
	default:
		return TransactionOpType("")
	}
}
