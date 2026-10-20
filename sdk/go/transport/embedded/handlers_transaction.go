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
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/transaction"
)

// txnCompareTargets maps the transaction client's compare targets to the
// service's. The service has no modification or creation revision target, so
// CompareModRevision and CompareCreateRevision are absent and refused.
var txnCompareTargets = map[transaction.CompareTarget]service.CompareTarget{
	transaction.CompareVersion: service.CompareTargetVersion,
	transaction.CompareValue:   service.CompareTargetValue,
}

// txnCompareOperators maps the transaction client's compare results to the
// service's compare operators.
var txnCompareOperators = map[transaction.CompareResult]service.CompareOperator{
	transaction.CompareEqual:    service.CompareOperatorEqual,
	transaction.CompareNotEqual: service.CompareOperatorNotEqual,
	transaction.CompareLess:     service.CompareOperatorLess,
	transaction.CompareGreater:  service.CompareOperatorGreater,
}

// txnOpTypesToService maps the transaction client's operation types to the
// service's.
var txnOpTypesToService = map[transaction.TxnOpType]service.TransactionOpType{
	transaction.TxnOpPut:    service.TransactionOpTypePut,
	transaction.TxnOpGet:    service.TransactionOpTypeGet,
	transaction.TxnOpDelete: service.TransactionOpTypeDelete,
}

// txnOpTypesFromService maps the service's operation types back to the
// transaction client's.
var txnOpTypesFromService = map[service.TransactionOpType]transaction.TxnOpType{
	service.TransactionOpTypePut:    transaction.TxnOpPut,
	service.TransactionOpTypeGet:    transaction.TxnOpGet,
	service.TransactionOpTypeDelete: transaction.TxnOpDelete,
}

// handleTransaction serves the transaction client's Execute verb. It converts
// the client's TxnRequest to the service's TransactionRequest, and the
// service's reply back to the client's TxnResponse.
func handleTransaction(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	txnReq, ok := req.(*transaction.TxnRequest)
	if !ok || txnReq == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.Transaction", "Execute", "*transaction.TxnRequest", typeNameOf(req))
	}

	svcReq, err := txnRequestToService(txnReq)
	if err != nil {
		return nil, err
	}
	// Each branch is checked before the service runs, so an ambiguous
	// branch is refused without any write.
	successPositions, err := txnOpPositions("success", svcReq.Success)
	if err != nil {
		return nil, err
	}
	failurePositions, err := txnOpPositions("failure", svcReq.Failure)
	if err != nil {
		return nil, err
	}

	svcResp, err := svc.Transaction(ctx, svcReq)
	if err != nil {
		return nil, err
	}
	if svcResp == nil {
		return nil, nil
	}
	positions := failurePositions
	if svcResp.Succeeded {
		positions = successPositions
	}
	return txnResponseFromService(svcResp, positions)
}

// UnrepresentableTransactionCompareError indicates a transaction compare the
// service cannot evaluate as the client meant it. The service reads a
// compare's key as "<entity type>:<id>", and the client's Compare carries no
// entity type, so the compare is refused before either branch can execute.
type UnrepresentableTransactionCompareError struct {
	Index int
	Key   string
}

// Error implements error.
func (e *UnrepresentableTransactionCompareError) Error() string {
	return "INVALID_INPUT: embedded: quicbaas.Transaction/Execute: compare " + strconv.Itoa(e.Index) +
		" on key " + strconv.Quote(e.Key) + " carries no entity type, so the service cannot evaluate it"
}

// TransactionOperationFailedError indicates the service ran a transaction but
// one of its operations failed. The client TxnOpResponse cannot carry an
// operation error, so the failure is returned instead of an empty result.
type TransactionOperationFailedError struct {
	Index  int
	Reason string
}

// Error implements error.
func (e *TransactionOperationFailedError) Error() string {
	return "ABORTED: embedded: quicbaas.Transaction/Execute: operation " + strconv.Itoa(e.Index) +
		" failed: " + e.Reason
}

// AmbiguousTransactionOperationError is returned, before the service runs,
// when two operations in one branch have the same type on the same entity
// type and key. The service names each result by entity and ID, not by
// position, so the two results could not be told apart.
type AmbiguousTransactionOperationError struct {
	Branch     string
	Index      int
	FirstIndex int
	EntityType string
	Key        string
}

func (e *AmbiguousTransactionOperationError) Error() string {
	return "INVALID_INPUT: embedded: quicbaas.Transaction/Execute: " + e.Branch + " operation " + strconv.Itoa(e.Index) +
		" repeats operation " + strconv.Itoa(e.FirstIndex) + " on " + e.EntityType + " key " + strconv.Quote(e.Key) +
		", so their results cannot be told apart"
}

// txnRequestToService converts the transaction client's request to the
// service's. The service keys a compare by entity type and ID, and the
// client's compares carry no entity type, so any compare is refused here,
// before the service runs a branch: the service itself would read the key
// with an empty entity type and take the wrong branch.
func txnRequestToService(req *transaction.TxnRequest) (*service.TransactionRequest, error) {
	compare := make([]*service.CompareCondition, len(req.Compare))
	for i, c := range req.Compare {
		target, ok := txnCompareTargets[c.Target]
		if !ok {
			return nil, &UnsupportedTransactionFieldError{Field: "compare target", Value: c.Target.String()}
		}
		operator, ok := txnCompareOperators[c.Result]
		if !ok {
			return nil, &UnsupportedTransactionFieldError{Field: "compare result", Value: c.Result.String()}
		}
		id, err := txnKeyToID(c.Key)
		if err != nil {
			return nil, err
		}
		cond := &service.CompareCondition{
			ID:       id,
			Target:   target,
			Operator: operator,
			Version:  c.Revision,
			Value:    c.Value,
		}
		// The client's Compare has no entity type to map, so cond.EntityType
		// stays empty and the service could not read the key the client meant.
		if cond.EntityType == "" {
			return nil, &UnrepresentableTransactionCompareError{Index: i, Key: c.Key}
		}
		compare[i] = cond
	}

	success, err := txnOpsToService(req.Success)
	if err != nil {
		return nil, err
	}
	failure, err := txnOpsToService(req.Failure)
	if err != nil {
		return nil, err
	}

	return &service.TransactionRequest{
		Compare: compare,
		Success: success,
		Failure: failure,
	}, nil
}

// txnOpsToService converts the transaction client's operations to the
// service's.
func txnOpsToService(ops []transaction.TxnOp) ([]*service.TransactionOp, error) {
	out := make([]*service.TransactionOp, len(ops))
	for i, op := range ops {
		opType, ok := txnOpTypesToService[op.Type]
		if !ok {
			return nil, &UnsupportedTransactionFieldError{Field: "operation type", Value: op.Type.String()}
		}
		id, err := txnKeyToID(op.Key)
		if err != nil {
			return nil, err
		}
		out[i] = &service.TransactionOp{
			OpType:     opType,
			EntityType: op.EntityType,
			ID:         id,
			Data:       op.Value,
		}
	}
	return out, nil
}

// txnKeyToID parses a transaction client key as the decimal entity id the
// QuicBaaS service addresses entities by.
func txnKeyToID(key string) (uint64, error) {
	id, err := strconv.ParseUint(key, 10, 64)
	if err != nil {
		return 0, &InvalidTransactionKeyError{Key: key, Err: err}
	}
	return id, nil
}

// txnResponseFromService converts the service's transaction reply to the
// transaction client's response.
func txnResponseFromService(resp *service.TransactionResponse, positions map[txnOpKey]int) (*transaction.TxnResponse, error) {
	// The service returns every Get result before the write results, so each
	// result is placed by the operation it names, not by where it arrived.
	results := make([]*service.TransactionOpResponse, len(positions))
	for i, r := range resp.Responses {
		if r == nil {
			return nil, &UnsupportedTransactionFieldError{Field: "response operation", Value: strconv.Itoa(i)}
		}
		if _, ok := txnOpTypesFromService[r.OpType]; !ok {
			return nil, &UnsupportedTransactionFieldError{
				Field: "response operation " + strconv.Itoa(i) + " type",
				Value: strconv.Itoa(int(r.OpType)),
			}
		}
		pos, ok := positions[txnOpKey{opType: r.OpType, entityType: r.EntityType, id: r.ID}]
		if !ok || results[pos] != nil {
			return nil, &UnsupportedTransactionFieldError{
				Field: "response operation " + strconv.Itoa(i),
				Value: r.EntityType + " " + strconv.FormatUint(r.ID, 10),
			}
		}
		results[pos] = r
	}

	out := &transaction.TxnResponse{
		Succeeded: resp.Succeeded,
		Revision:  resp.Revision,
		Responses: make([]transaction.TxnOpResponse, len(results)),
	}
	for pos, r := range results {
		if r == nil {
			return nil, &UnsupportedTransactionFieldError{Field: "missing result for operation", Value: strconv.Itoa(pos)}
		}
		if !r.Success {
			return nil, &TransactionOperationFailedError{Index: pos, Reason: r.Error}
		}
		out.Responses[pos] = transaction.TxnOpResponse{Type: txnOpTypesFromService[r.OpType], Value: r.Data}
	}
	return out, nil
}

// txnOpKey identifies the operation a service result belongs to: the service
// echoes an operation's type, entity type and ID on its result.
type txnOpKey struct {
	opType     service.TransactionOpType
	entityType string
	id         uint64
}

// txnOpPositions maps each operation of one branch to its request position.
// It refuses a branch in which two operations share a txnOpKey, because their
// results could not be told apart.
func txnOpPositions(branch string, ops []*service.TransactionOp) (map[txnOpKey]int, error) {
	positions := make(map[txnOpKey]int, len(ops))
	for i, op := range ops {
		key := txnOpKey{opType: op.OpType, entityType: op.EntityType, id: op.ID}
		if first, seen := positions[key]; seen {
			return nil, &AmbiguousTransactionOperationError{
				Branch:     branch,
				Index:      i,
				FirstIndex: first,
				EntityType: op.EntityType,
				Key:        strconv.FormatUint(op.ID, 10),
			}
		}
		positions[key] = i
	}
	return positions, nil
}
