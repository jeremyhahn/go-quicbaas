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

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/transaction"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/mock"
)

// =============================================================================
// Batch Operation Tests
// =============================================================================

func TestBatchExecute_MultiplePutOperations(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	expectedRevision := uint64(100)
	expectedResults := []batch.BatchResult{
		{Success: true},
		{Success: true},
		{Success: true},
	}

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: expectedRevision,
		Results:  expectedResults,
	}, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte(`{"name":"Alice"}`)},
			{Type: batch.OpPut, EntityType: "users", Key: "user2", Value: []byte(`{"name":"Bob"}`)},
			{Type: batch.OpPut, EntityType: "config", Key: "setting1", Value: []byte(`{"enabled":true}`)},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Revision != expectedRevision {
		t.Errorf("expected revision %d, got %d", expectedRevision, resp.Revision)
	}

	if len(resp.Results) != len(expectedResults) {
		t.Errorf("expected %d results, got %d", len(expectedResults), len(resp.Results))
	}

	for i, result := range resp.Results {
		if !result.Success {
			t.Errorf("expected result %d to be successful", i)
		}
	}

	// Verify request structure
	requests := transport.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}

	record := requests[0]
	if record.Service != "quicbaas.Batch" {
		t.Errorf("expected service 'quicbaas.Batch', got '%s'", record.Service)
	}
	if record.Method != "Execute" {
		t.Errorf("expected method 'Execute', got '%s'", record.Method)
	}

	batchReq, ok := record.Payload.(*batch.BatchRequest)
	if !ok {
		t.Fatalf("expected payload to be *batch.BatchRequest")
	}
	if len(batchReq.Operations) != 3 {
		t.Errorf("expected 3 operations in payload, got %d", len(batchReq.Operations))
	}
}

func TestBatchExecute_MultipleDeleteOperations(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 200,
		Results: []batch.BatchResult{
			{Success: true},
			{Success: true},
		},
	}, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpDelete, EntityType: "users", Key: "user1"},
			{Type: batch.OpDelete, EntityType: "users", Key: "user2"},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Revision != 200 {
		t.Errorf("expected revision 200, got %d", resp.Revision)
	}

	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}

	// Verify delete operations in request
	record := transport.LastRequest()
	batchReq := record.Payload.(*batch.BatchRequest)
	for i, op := range batchReq.Operations {
		if op.Type != batch.OpDelete {
			t.Errorf("expected operation %d to be DELETE, got %s", i, op.Type)
		}
	}
}

func TestBatchExecute_MixedPutAndDelete(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 300,
		Results: []batch.BatchResult{
			{Success: true},
			{Success: true},
			{Success: true},
		},
	}, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte(`{"name":"Alice"}`)},
			{Type: batch.OpDelete, EntityType: "users", Key: "user2"},
			{Type: batch.OpPut, EntityType: "config", Key: "setting1", Value: []byte(`{"enabled":false}`)},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Revision != 300 {
		t.Errorf("expected revision 300, got %d", resp.Revision)
	}

	// Verify mixed operation types in request
	record := transport.LastRequest()
	batchReq := record.Payload.(*batch.BatchRequest)

	if batchReq.Operations[0].Type != batch.OpPut {
		t.Errorf("expected first operation to be PUT")
	}
	if batchReq.Operations[1].Type != batch.OpDelete {
		t.Errorf("expected second operation to be DELETE")
	}
	if batchReq.Operations[2].Type != batch.OpPut {
		t.Errorf("expected third operation to be PUT")
	}
}

func TestBatchExecute_AtomicExecution_AllOrNothing(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	// Simulate a batch where one operation fails - all should be marked as failed
	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 0, // No revision on failure
		Results: []batch.BatchResult{
			{Success: false, Error: "batch failed due to conflict"},
			{Success: false, Error: "batch failed due to conflict"},
			{Success: false, Error: "batch failed due to conflict"},
		},
	}, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte(`{"name":"Alice"}`)},
			{Type: batch.OpPut, EntityType: "users", Key: "user2", Value: []byte(`{"name":"Bob"}`)},
			{Type: batch.OpPut, EntityType: "users", Key: "user3", Value: []byte(`{"name":"Charlie"}`)},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All results should indicate failure for atomic semantics
	for i, result := range resp.Results {
		if result.Success {
			t.Errorf("expected result %d to fail in atomic batch", i)
		}
		if result.Error == "" {
			t.Errorf("expected result %d to have error message", i)
		}
	}
}

func TestBatchExecute_ResultsMatchOperations(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	operations := []batch.BatchOperation{
		{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte(`{"id":1}`)},
		{Type: batch.OpPut, EntityType: "users", Key: "user2", Value: []byte(`{"id":2}`)},
		{Type: batch.OpDelete, EntityType: "sessions", Key: "sess1"},
		{Type: batch.OpPut, EntityType: "config", Key: "app", Value: []byte(`{}`)},
		{Type: batch.OpDelete, EntityType: "cache", Key: "entry1"},
	}

	expectedResults := make([]batch.BatchResult, len(operations))
	for i := range operations {
		expectedResults[i] = batch.BatchResult{Success: true}
	}

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 500,
		Results:  expectedResults,
	}, nil)

	req := &batch.BatchRequest{Operations: operations}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != len(operations) {
		t.Errorf("expected %d results to match %d operations, got %d",
			len(operations), len(operations), len(resp.Results))
	}
}

// =============================================================================
// Transaction Operation Tests
// =============================================================================

func TestTransactionExecute_CompareAndSwapWithVersionCheck(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  100,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpPut},
		},
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{
				Key:      "users/user1",
				Target:   transaction.CompareVersion,
				Result:   transaction.CompareEqual,
				Revision: 5, // Expect version 5
			},
		},
		Success: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "users",
				Key:        "user1",
				Value:      []byte(`{"name":"Updated"}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction to succeed")
	}
	if resp.Revision != 100 {
		t.Errorf("expected revision 100, got %d", resp.Revision)
	}

	// Verify request structure
	record := transport.LastRequest()
	if record.Service != "quicbaas.Transaction" {
		t.Errorf("expected service 'quicbaas.Transaction', got '%s'", record.Service)
	}
	if record.Method != "Execute" {
		t.Errorf("expected method 'Execute', got '%s'", record.Method)
	}

	txnReq := record.Payload.(*transaction.TxnRequest)
	if len(txnReq.Compare) != 1 {
		t.Errorf("expected 1 compare, got %d", len(txnReq.Compare))
	}
	if txnReq.Compare[0].Target != transaction.CompareVersion {
		t.Errorf("expected compare target VERSION, got %s", txnReq.Compare[0].Target)
	}
}

func TestTransactionExecute_CompareAndSwapWithValueCheck(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  200,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpPut},
		},
	}, nil)

	expectedValue := []byte(`{"status":"active"}`)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{
				Key:    "users/user1",
				Target: transaction.CompareValue,
				Result: transaction.CompareEqual,
				Value:  expectedValue,
			},
		},
		Success: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "users",
				Key:        "user1",
				Value:      []byte(`{"status":"inactive"}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction to succeed")
	}

	// Verify compare value in request
	record := transport.LastRequest()
	txnReq := record.Payload.(*transaction.TxnRequest)
	if string(txnReq.Compare[0].Value) != string(expectedValue) {
		t.Errorf("expected compare value '%s', got '%s'",
			string(expectedValue), string(txnReq.Compare[0].Value))
	}
}

func TestTransactionExecute_SuccessBranchExecution(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  300,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpPut},
			{Type: transaction.TxnOpDelete},
		},
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{
				Key:      "lock/resource1",
				Target:   transaction.CompareVersion,
				Result:   transaction.CompareEqual,
				Revision: 0, // Key doesn't exist
			},
		},
		Success: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "lock",
				Key:        "resource1",
				Value:      []byte(`{"holder":"client1"}`),
			},
			{
				Type:       transaction.TxnOpDelete,
				EntityType: "queue",
				Key:        "pending/resource1",
			},
		},
		Failure: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "queue",
				Key:        "pending/resource1",
				Value:      []byte(`{"requester":"client1"}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction to succeed and execute success branch")
	}

	// Response should contain success branch operations
	if len(resp.Responses) != 2 {
		t.Errorf("expected 2 responses from success branch, got %d", len(resp.Responses))
	}
}

func TestTransactionExecute_FailureBranchExecution(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: false, // Compare failed, failure branch executed
		Revision:  400,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpPut}, // From failure branch
		},
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{
				Key:      "lock/resource1",
				Target:   transaction.CompareVersion,
				Result:   transaction.CompareEqual,
				Revision: 0, // Expecting key doesn't exist, but it does
			},
		},
		Success: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "lock",
				Key:        "resource1",
				Value:      []byte(`{"holder":"client1"}`),
			},
		},
		Failure: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "queue",
				Key:        "pending/resource1",
				Value:      []byte(`{"requester":"client1"}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Succeeded {
		t.Error("expected transaction to fail and execute failure branch")
	}

	// Response should contain failure branch operations
	if len(resp.Responses) != 1 {
		t.Errorf("expected 1 response from failure branch, got %d", len(resp.Responses))
	}
}

func TestTransactionExecute_MultipleConditions(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  500,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpPut},
		},
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{
				Key:      "account/user1/balance",
				Target:   transaction.CompareModRevision,
				Result:   transaction.CompareGreater,
				Revision: 10, // Modified after revision 10
			},
			{
				Key:    "account/user1/status",
				Target: transaction.CompareValue,
				Result: transaction.CompareEqual,
				Value:  []byte("active"),
			},
			{
				Key:      "account/user1/locked",
				Target:   transaction.CompareVersion,
				Result:   transaction.CompareEqual,
				Revision: 0, // Key doesn't exist
			},
		},
		Success: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "account",
				Key:        "user1/transfer",
				Value:      []byte(`{"amount":100}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction with multiple conditions to succeed")
	}

	// Verify all conditions in request
	record := transport.LastRequest()
	txnReq := record.Payload.(*transaction.TxnRequest)
	if len(txnReq.Compare) != 3 {
		t.Errorf("expected 3 compare conditions, got %d", len(txnReq.Compare))
	}

	// Verify different compare targets
	if txnReq.Compare[0].Target != transaction.CompareModRevision {
		t.Errorf("expected first compare to be MOD_REVISION")
	}
	if txnReq.Compare[1].Target != transaction.CompareValue {
		t.Errorf("expected second compare to be VALUE")
	}
	if txnReq.Compare[2].Target != transaction.CompareVersion {
		t.Errorf("expected third compare to be VERSION")
	}

	// Verify different compare results
	if txnReq.Compare[0].Result != transaction.CompareGreater {
		t.Errorf("expected first compare result to be GREATER")
	}
	if txnReq.Compare[1].Result != transaction.CompareEqual {
		t.Errorf("expected second compare result to be EQUAL")
	}
}

func TestTransactionExecute_GetInSuccessBranch(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	retrievedValue := []byte(`{"balance":1000}`)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  600,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpGet, Value: retrievedValue},
			{Type: transaction.TxnOpPut},
		},
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{
				Key:    "account/user1/status",
				Target: transaction.CompareValue,
				Result: transaction.CompareEqual,
				Value:  []byte("active"),
			},
		},
		Success: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpGet,
				EntityType: "account",
				Key:        "user1/balance",
			},
			{
				Type:       transaction.TxnOpPut,
				EntityType: "audit",
				Key:        "user1/access",
				Value:      []byte(`{"timestamp":"2025-01-01T00:00:00Z"}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction to succeed")
	}

	// Verify GET operation returned value
	if len(resp.Responses) < 1 {
		t.Fatal("expected at least 1 response")
	}
	if resp.Responses[0].Type != transaction.TxnOpGet {
		t.Errorf("expected first response to be GET")
	}
	if string(resp.Responses[0].Value) != string(retrievedValue) {
		t.Errorf("expected value '%s', got '%s'",
			string(retrievedValue), string(resp.Responses[0].Value))
	}
}

func TestTransactionExecute_CompareResultTypes(t *testing.T) {
	testCases := []struct {
		name   string
		result transaction.CompareResult
	}{
		{"Equal", transaction.CompareEqual},
		{"NotEqual", transaction.CompareNotEqual},
		{"Less", transaction.CompareLess},
		{"Greater", transaction.CompareGreater},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			transport := mock.NewTransport()
			client := transaction.NewClient(transport)

			transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
				Succeeded: true,
				Revision:  100,
				Responses: []transaction.TxnOpResponse{},
			}, nil)

			req := &transaction.TxnRequest{
				Compare: []transaction.Compare{
					{
						Key:      "test/key",
						Target:   transaction.CompareVersion,
						Result:   tc.result,
						Revision: 1,
					},
				},
				Success: []transaction.TxnOp{
					{
						Type:       transaction.TxnOpPut,
						EntityType: "test",
						Key:        "key",
						Value:      []byte("value"),
					},
				},
			}

			ctx := context.Background()
			_, err := client.Execute(ctx, req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Verify compare result type
			record := transport.LastRequest()
			txnReq := record.Payload.(*transaction.TxnRequest)
			if txnReq.Compare[0].Result != tc.result {
				t.Errorf("expected compare result %s, got %s", tc.result, txnReq.Compare[0].Result)
			}
		})
	}
}

func TestTransactionExecute_CreateRevisionCompare(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  700,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpPut},
		},
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{
				Key:      "settings/app/config",
				Target:   transaction.CompareCreateRevision,
				Result:   transaction.CompareNotEqual,
				Revision: 0, // Key exists (create revision != 0)
			},
		},
		Success: []transaction.TxnOp{
			{
				Type:       transaction.TxnOpPut,
				EntityType: "settings",
				Key:        "app/config",
				Value:      []byte(`{"version":"2.0"}`),
			},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction to succeed")
	}

	// Verify compare target
	record := transport.LastRequest()
	txnReq := record.Payload.(*transaction.TxnRequest)
	if txnReq.Compare[0].Target != transaction.CompareCreateRevision {
		t.Errorf("expected CREATE_REVISION compare target")
	}
}

// =============================================================================
// Error Handling Tests
// =============================================================================

func TestBatchExecute_TransportError(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	expectedErr := errors.NewConnection("test", "connection refused")
	transport.SetResponse("quicbaas.Batch", "Execute", nil, expectedErr)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response on error")
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsRetryable(err) && errors.Code(err) != errors.CodeConnection {
		// Connection errors should be identifiable
		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Errorf("expected QBError, got %T", err)
		} else if qbErr.Code != errors.CodeConnection {
			t.Errorf("expected connection error code, got %v", qbErr.Code)
		}
	}
}

func TestBatchExecute_ContextCancellation(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 100,
		Results:  []batch.BatchResult{{Success: true}},
	}, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response on canceled context")
	}
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
	if !errors.IsCanceled(err) {
		t.Errorf("expected canceled error, got: %v", err)
	}
}

func TestBatchExecute_EmptyBatchRequest(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for empty batch")
	}
	if err == nil {
		t.Fatal("expected error for empty batch, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestBatchExecute_NilRequest(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	ctx := context.Background()
	resp, err := client.Execute(ctx, nil)

	if resp != nil {
		t.Error("expected nil response for nil request")
	}
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestBatchExecute_NilContext(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	//nolint:staticcheck // Testing nil context intentionally
	resp, err := client.Execute(nil, req)

	if resp != nil {
		t.Error("expected nil response for nil context")
	}
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestBatchExecute_MissingEntityType(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "", Key: "user1", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for missing entity type")
	}
	if err == nil {
		t.Fatal("expected error for missing entity type, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestBatchExecute_MissingKey(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for missing key")
	}
	if err == nil {
		t.Fatal("expected error for missing key, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_TransportError(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	expectedErr := errors.NewUnavailable("test", "service unavailable")
	transport.SetResponse("quicbaas.Transaction", "Execute", nil, expectedErr)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response on error")
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected unavailable error, got: %v", err)
	}
}

func TestTransactionExecute_ContextCancellation(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  100,
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response on canceled context")
	}
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
	if !errors.IsCanceled(err) {
		t.Errorf("expected canceled error, got: %v", err)
	}
}

func TestTransactionExecute_EmptyRequest(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{},
		Success: []transaction.TxnOp{},
		Failure: []transaction.TxnOp{},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for empty transaction")
	}
	if err == nil {
		t.Fatal("expected error for empty transaction, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_NilRequest(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	ctx := context.Background()
	resp, err := client.Execute(ctx, nil)

	if resp != nil {
		t.Error("expected nil response for nil request")
	}
	if err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_NilContext(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	//nolint:staticcheck // Testing nil context intentionally
	resp, err := client.Execute(nil, req)

	if resp != nil {
		t.Error("expected nil response for nil context")
	}
	if err == nil {
		t.Fatal("expected error for nil context, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_MissingCompareKey(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for missing compare key")
	}
	if err == nil {
		t.Fatal("expected error for missing compare key, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_MissingOperationEntityType(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for missing entity type")
	}
	if err == nil {
		t.Fatal("expected error for missing entity type, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_MissingOperationKey(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for missing operation key")
	}
	if err == nil {
		t.Fatal("expected error for missing operation key, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_FailureBranchValidation(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Failure: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "", Key: "key", Value: []byte("data")}, // Missing entity type
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for invalid failure branch")
	}
	if err == nil {
		t.Fatal("expected error for invalid failure branch, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

// =============================================================================
// Transport Closed Tests
// =============================================================================

func TestBatchExecute_TransportClosed(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 100,
		Results:  []batch.BatchResult{{Success: true}},
	}, nil)

	// Close the transport
	if err := transport.Close(); err != nil {
		t.Fatalf("failed to close transport: %v", err)
	}

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for closed transport")
	}
	if err == nil {
		t.Fatal("expected error for closed transport, got nil")
	}
}

func TestTransactionExecute_TransportClosed(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  100,
	}, nil)

	// Close the transport
	if err := transport.Close(); err != nil {
		t.Fatalf("failed to close transport: %v", err)
	}

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for closed transport")
	}
	if err == nil {
		t.Fatal("expected error for closed transport, got nil")
	}
}

// =============================================================================
// Nil Transport Tests
// =============================================================================

func TestBatchNewClient_NilTransport(t *testing.T) {
	client := batch.NewClient(nil)
	if client != nil {
		t.Error("expected nil client for nil transport")
	}
}

func TestTransactionNewClient_NilTransport(t *testing.T) {
	client := transaction.NewClient(nil)
	if client != nil {
		t.Error("expected nil client for nil transport")
	}
}

// =============================================================================
// Response Parsing Tests
// =============================================================================

func TestBatchExecute_NilResponse(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	transport.SetResponse("quicbaas.Batch", "Execute", nil, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response")
	}
	if err == nil {
		t.Fatal("expected error for nil response, got nil")
	}
}

func TestBatchExecute_InvalidResponseType(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	// Return wrong response type
	transport.SetResponse("quicbaas.Batch", "Execute", "invalid string response", nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for invalid type")
	}
	if err == nil {
		t.Fatal("expected error for invalid response type, got nil")
	}
}

func TestTransactionExecute_NilResponse(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", nil, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response")
	}
	if err == nil {
		t.Fatal("expected error for nil response, got nil")
	}
}

func TestTransactionExecute_InvalidResponseType(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	// Return wrong response type
	transport.SetResponse("quicbaas.Transaction", "Execute", 12345, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for invalid type")
	}
	if err == nil {
		t.Fatal("expected error for invalid response type, got nil")
	}
}

// =============================================================================
// Context Timeout Tests
// =============================================================================

func TestBatchExecute_ContextTimeout(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 100,
		Results:  []batch.BatchResult{{Success: true}},
	}, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	// Create already expired context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure timeout

	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response on timeout")
	}
	if err == nil {
		t.Fatal("expected error for timeout, got nil")
	}
}

func TestTransactionExecute_ContextTimeout(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  100,
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	// Create already expired context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond) // Ensure timeout

	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response on timeout")
	}
	if err == nil {
		t.Fatal("expected error for timeout, got nil")
	}
}

// =============================================================================
// Operation Type String Tests
// =============================================================================

func TestBatchOpType_String(t *testing.T) {
	testCases := []struct {
		opType   batch.OpType
		expected string
	}{
		{batch.OpPut, "PUT"},
		{batch.OpDelete, "DELETE"},
		{batch.OpType(99), "UNKNOWN"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.opType.String() != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, tc.opType.String())
			}
		})
	}
}

func TestTransactionOpType_String(t *testing.T) {
	testCases := []struct {
		opType   transaction.TxnOpType
		expected string
	}{
		{transaction.TxnOpPut, "PUT"},
		{transaction.TxnOpGet, "GET"},
		{transaction.TxnOpDelete, "DELETE"},
		{transaction.TxnOpType(99), "UNKNOWN"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.opType.String() != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, tc.opType.String())
			}
		})
	}
}

func TestTransactionCompareTarget_String(t *testing.T) {
	testCases := []struct {
		target   transaction.CompareTarget
		expected string
	}{
		{transaction.CompareVersion, "VERSION"},
		{transaction.CompareValue, "VALUE"},
		{transaction.CompareModRevision, "MOD_REVISION"},
		{transaction.CompareCreateRevision, "CREATE_REVISION"},
		{transaction.CompareTarget(99), "UNKNOWN"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.target.String() != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, tc.target.String())
			}
		})
	}
}

func TestTransactionCompareResult_String(t *testing.T) {
	testCases := []struct {
		result   transaction.CompareResult
		expected string
	}{
		{transaction.CompareEqual, "EQUAL"},
		{transaction.CompareNotEqual, "NOT_EQUAL"},
		{transaction.CompareLess, "LESS"},
		{transaction.CompareGreater, "GREATER"},
		{transaction.CompareResult(99), "UNKNOWN"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			if tc.result.String() != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, tc.result.String())
			}
		})
	}
}

// =============================================================================
// Transport Request Verification Tests
// =============================================================================

func TestBatchExecute_RequestCountVerification(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 100,
		Results:  []batch.BatchResult{{Success: true}},
	}, nil)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	ctx := context.Background()

	// Execute multiple times
	for i := 0; i < 5; i++ {
		_, err := client.Execute(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
	}

	if transport.RequestCount() != 5 {
		t.Errorf("expected 5 requests, got %d", transport.RequestCount())
	}
}

func TestTransactionExecute_TransportReset(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  100,
	}, nil)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()

	// Execute first request
	_, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if transport.RequestCount() != 1 {
		t.Errorf("expected 1 request before reset, got %d", transport.RequestCount())
	}

	// Reset and verify
	transport.Reset()

	if transport.RequestCount() != 0 {
		t.Errorf("expected 0 requests after reset, got %d", transport.RequestCount())
	}

	// Need to set response again after reset
	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  200,
	}, nil)

	// Execute second request
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Revision != 200 {
		t.Errorf("expected revision 200 after reset, got %d", resp.Revision)
	}
}

// =============================================================================
// Transaction Only Operations Tests
// =============================================================================

func TestTransactionExecute_OnlySuccessOperations(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  100,
		Responses: []transaction.TxnOpResponse{
			{Type: transaction.TxnOpPut},
			{Type: transaction.TxnOpDelete},
		},
	}, nil)

	// Transaction with only success operations, no compare
	req := &transaction.TxnRequest{
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "users", Key: "user1", Value: []byte("data")},
			{Type: transaction.TxnOpDelete, EntityType: "users", Key: "user2"},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction to succeed")
	}
	if len(resp.Responses) != 2 {
		t.Errorf("expected 2 responses, got %d", len(resp.Responses))
	}
}

func TestTransactionExecute_OnlyCompare(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	transport.SetResponse("quicbaas.Transaction", "Execute", &transaction.TxnResponse{
		Succeeded: true,
		Revision:  100,
		Responses: []transaction.TxnOpResponse{},
	}, nil)

	// Transaction with only compare, no operations
	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected compare-only transaction to succeed")
	}
}

// =============================================================================
// Large Batch Tests
// =============================================================================

func TestBatchExecute_LargeBatch(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	operationCount := 100
	operations := make([]batch.BatchOperation, operationCount)
	results := make([]batch.BatchResult, operationCount)

	for i := 0; i < operationCount; i++ {
		operations[i] = batch.BatchOperation{
			Type:       batch.OpPut,
			EntityType: "items",
			Key:        "item" + string(rune('0'+i%10)) + string(rune('0'+i/10)),
			Value:      []byte(`{"index":` + string(rune('0'+i%10)) + `}`),
		}
		results[i] = batch.BatchResult{Success: true}
	}

	transport.SetResponse("quicbaas.Batch", "Execute", &batch.BatchResponse{
		Revision: 1000,
		Results:  results,
	}, nil)

	req := &batch.BatchRequest{Operations: operations}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != operationCount {
		t.Errorf("expected %d results, got %d", operationCount, len(resp.Results))
	}

	// Verify request had all operations
	record := transport.LastRequest()
	batchReq := record.Payload.(*batch.BatchRequest)
	if len(batchReq.Operations) != operationCount {
		t.Errorf("expected %d operations in request, got %d", operationCount, len(batchReq.Operations))
	}
}

// =============================================================================
// Invalid Operation Type Tests
// =============================================================================

func TestBatchExecute_InvalidOperationType(t *testing.T) {
	transport := mock.NewTransport()
	client := batch.NewClient(transport)

	req := &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpType(99), EntityType: "users", Key: "user1", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for invalid operation type")
	}
	if err == nil {
		t.Fatal("expected error for invalid operation type, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_InvalidOperationType(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpType(99), EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for invalid operation type")
	}
	if err == nil {
		t.Fatal("expected error for invalid operation type, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_InvalidCompareTarget(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareTarget(99), Result: transaction.CompareEqual, Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for invalid compare target")
	}
	if err == nil {
		t.Fatal("expected error for invalid compare target, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}

func TestTransactionExecute_InvalidCompareResult(t *testing.T) {
	transport := mock.NewTransport()
	client := transaction.NewClient(transport)

	req := &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "test/key", Target: transaction.CompareVersion, Result: transaction.CompareResult(99), Revision: 0},
		},
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "test", Key: "key", Value: []byte("data")},
		},
	}

	ctx := context.Background()
	resp, err := client.Execute(ctx, req)

	if resp != nil {
		t.Error("expected nil response for invalid compare result")
	}
	if err == nil {
		t.Fatal("expected error for invalid compare result, got nil")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected invalid input error, got: %v", err)
	}
}
