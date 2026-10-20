package embedded

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/transaction"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestTransactionExecute_SDKClientReachesTheService(t *testing.T) {
	var got *service.TransactionRequest
	tr, err := New(&mockQuicBaaS{transactionFunc: func(_ context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
		got = req
		return &service.TransactionResponse{
			Succeeded: true,
			Revision:  9,
			Responses: []*service.TransactionOpResponse{
				{OpType: service.TransactionOpTypeGet, EntityType: "user", ID: 7, Data: []byte("new"), Success: true},
				{OpType: service.TransactionOpTypePut, EntityType: "user", ID: 7, Success: true},
				{OpType: service.TransactionOpTypeDelete, EntityType: "user", ID: 8, Success: true},
			},
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "user", Key: "7", Value: []byte("new")},
			{Type: transaction.TxnOpDelete, EntityType: "user", Key: "8"},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "7"},
		},
		Failure: []transaction.TxnOp{{Type: transaction.TxnOpGet, EntityType: "user", Key: "7"}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got == nil || len(got.Compare) != 0 || len(got.Success) != 3 || len(got.Failure) != 1 {
		t.Fatalf("service received %+v", got)
	}
	put, del := got.Success[0], got.Success[1]
	if put.OpType != service.TransactionOpTypePut || put.EntityType != "user" || put.ID != 7 || !bytes.Equal(put.Data, []byte("new")) {
		t.Errorf("put = %+v", put)
	}
	if del.OpType != service.TransactionOpTypeDelete || del.EntityType != "user" || del.ID != 8 {
		t.Errorf("delete = %+v", del)
	}
	if get := got.Failure[0]; get.OpType != service.TransactionOpTypeGet || get.ID != 7 {
		t.Errorf("failure get = %+v", get)
	}

	if !resp.Succeeded || resp.Revision != 9 || len(resp.Responses) != 3 ||
		resp.Responses[0].Type != transaction.TxnOpPut || resp.Responses[1].Type != transaction.TxnOpDelete ||
		resp.Responses[2].Type != transaction.TxnOpGet || !bytes.Equal(resp.Responses[2].Value, []byte("new")) {
		t.Fatalf("response = %+v", resp)
	}
}

func TestTransactionExecute_ServiceErrorIsReturned(t *testing.T) {
	conflict := sdkerrors.NewConflict("transaction.Execute", "entity 7 was written concurrently")
	tr, err := New(&mockQuicBaaS{transactionFunc: func(context.Context, *service.TransactionRequest) (*service.TransactionResponse, error) {
		return nil, conflict
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Success: []transaction.TxnOp{{Type: transaction.TxnOpPut, EntityType: "user", Key: "7"}},
	})
	if !errors.Is(err, conflict) {
		t.Fatalf("Execute error = %v, want %v", err, conflict)
	}
}

func TestTransactionExecute_UnmappedResponseIsRefused(t *testing.T) {
	for name, result := range map[string]*service.TransactionOpResponse{
		"unknown type": {OpType: service.TransactionOpType(99)},
		"nil result":   nil,
	} {
		tr, err := New(&mockQuicBaaS{transactionFunc: func(context.Context, *service.TransactionRequest) (*service.TransactionResponse, error) {
			return &service.TransactionResponse{Responses: []*service.TransactionOpResponse{result}}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}

		_, err = transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
			Success: []transaction.TxnOp{{Type: transaction.TxnOpGet, EntityType: "user", Key: "7"}},
		})
		var unsupported *UnsupportedTransactionFieldError
		if !errors.As(err, &unsupported) {
			t.Errorf("%s: Execute error = %v, want *UnsupportedTransactionFieldError", name, err)
		}
	}
}

func TestTransactionExecute_FailedOperationReturnsTypedError(t *testing.T) {
	tr, err := New(&mockQuicBaaS{transactionFunc: func(context.Context, *service.TransactionRequest) (*service.TransactionResponse, error) {
		return &service.TransactionResponse{
			Succeeded: true,
			Responses: []*service.TransactionOpResponse{
				{OpType: service.TransactionOpTypeGet, EntityType: "user", ID: 8, Success: false, Error: "entity not found"},
				{OpType: service.TransactionOpTypePut, EntityType: "user", ID: 7, Success: true},
			},
		}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "user", Key: "7", Value: []byte("new")},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "8"},
		},
	})
	var failed *TransactionOperationFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("Execute error = %v, want *TransactionOperationFailedError", err)
	}
	if failed.Index != 1 || failed.Reason != "entity not found" {
		t.Errorf("error = %+v, want index 1 and the service's error text", failed)
	}
	if resp != nil {
		t.Errorf("response = %+v, want nil", resp)
	}
}

// serviceOrderResults answers a transaction the way the service does: every
// Get result first, then the write results, each naming its entity and ID.
func serviceOrderResults(ops []*service.TransactionOp, failID uint64) []*service.TransactionOpResponse {
	var reads, writes []*service.TransactionOpResponse
	for _, op := range ops {
		r := &service.TransactionOpResponse{OpType: op.OpType, EntityType: op.EntityType, ID: op.ID, Success: true}
		if op.ID == failID {
			r.Success, r.Error = false, "entity not found"
		}
		if op.OpType == service.TransactionOpTypeGet {
			r.Data = []byte("value-" + strconv.FormatUint(op.ID, 10))
			reads = append(reads, r)
			continue
		}
		writes = append(writes, r)
	}
	return append(reads, writes...)
}

func TestTransactionExecute_ResultsComeBackInRequestOrder(t *testing.T) {
	tr, err := New(&mockQuicBaaS{transactionFunc: func(_ context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
		return &service.TransactionResponse{Succeeded: true, Responses: serviceOrderResults(req.Success, 0)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "user", Key: "1", Value: []byte("v")},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "2"},
			{Type: transaction.TxnOpDelete, EntityType: "user", Key: "3"},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "4"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	want := []transaction.TxnOpResponse{
		{Type: transaction.TxnOpPut},
		{Type: transaction.TxnOpGet, Value: []byte("value-2")},
		{Type: transaction.TxnOpDelete},
		{Type: transaction.TxnOpGet, Value: []byte("value-4")},
	}
	if len(resp.Responses) != len(want) {
		t.Fatalf("responses = %+v, want %d in request order", resp.Responses, len(want))
	}
	for i, w := range want {
		got := resp.Responses[i]
		if got.Type != w.Type || !bytes.Equal(got.Value, w.Value) {
			t.Errorf("response %d = %+v, want %+v", i, got, w)
		}
	}
}

func TestTransactionExecute_FailedOperationIndexIsItsRequestPosition(t *testing.T) {
	tr, err := New(&mockQuicBaaS{transactionFunc: func(_ context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
		// Fail the Get on user 4: the service reports it second, after the
		// other Get and before the writes, but the request put it last.
		return &service.TransactionResponse{Succeeded: true, Responses: serviceOrderResults(req.Success, 4)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Success: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "user", Key: "1", Value: []byte("v")},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "2"},
			{Type: transaction.TxnOpDelete, EntityType: "user", Key: "3"},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "4"},
		},
	})
	var failed *TransactionOperationFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("Execute error = %v, want *TransactionOperationFailedError", err)
	}
	if failed.Index != 3 {
		t.Errorf("failed index = %d, want 3 (the Get on user 4)", failed.Index)
	}
}

func TestTransactionExecute_FailureBranchResultsMapToFailureOps(t *testing.T) {
	tr, err := New(&mockQuicBaaS{transactionFunc: func(_ context.Context, req *service.TransactionRequest) (*service.TransactionResponse, error) {
		return &service.TransactionResponse{Succeeded: false, Responses: serviceOrderResults(req.Failure, 0)}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Success: []transaction.TxnOp{{Type: transaction.TxnOpGet, EntityType: "user", Key: "1"}},
		Failure: []transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "user", Key: "5", Value: []byte("v")},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "6"},
		},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if resp.Succeeded || len(resp.Responses) != 2 ||
		resp.Responses[0].Type != transaction.TxnOpPut || resp.Responses[1].Type != transaction.TxnOpGet {
		t.Fatalf("response = %+v, want the failure branch in request order", resp)
	}
}

func TestTransactionExecute_ResultForNoRequestedOperationIsRefused(t *testing.T) {
	tr, err := New(&mockQuicBaaS{transactionFunc: func(context.Context, *service.TransactionRequest) (*service.TransactionResponse, error) {
		return &service.TransactionResponse{Succeeded: true, Responses: []*service.TransactionOpResponse{
			{OpType: service.TransactionOpTypeGet, EntityType: "user", ID: 99, Success: true},
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Success: []transaction.TxnOp{{Type: transaction.TxnOpGet, EntityType: "user", Key: "7"}},
	})
	var unsupported *UnsupportedTransactionFieldError
	if !errors.As(err, &unsupported) {
		t.Errorf("Execute error = %v, want *UnsupportedTransactionFieldError", err)
	}
}

func TestTransactionExecute_AmbiguousOperationsAreRefusedBeforeTheService(t *testing.T) {
	for name, branch := range map[string]func(ops []transaction.TxnOp) *transaction.TxnRequest{
		"success": func(ops []transaction.TxnOp) *transaction.TxnRequest { return &transaction.TxnRequest{Success: ops} },
		"failure": func(ops []transaction.TxnOp) *transaction.TxnRequest { return &transaction.TxnRequest{Failure: ops} },
	} {
		called := false
		tr, err := New(&mockQuicBaaS{transactionFunc: func(context.Context, *service.TransactionRequest) (*service.TransactionResponse, error) {
			called = true
			return &service.TransactionResponse{Succeeded: true}, nil
		}})
		if err != nil {
			t.Fatal(err)
		}

		_, err = transaction.NewClient(tr).Execute(context.Background(), branch([]transaction.TxnOp{
			{Type: transaction.TxnOpPut, EntityType: "user", Key: "7", Value: []byte("a")},
			{Type: transaction.TxnOpGet, EntityType: "user", Key: "7"},
			{Type: transaction.TxnOpPut, EntityType: "user", Key: "7", Value: []byte("b")},
		}))
		var ambiguous *AmbiguousTransactionOperationError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("%s: Execute error = %v, want *AmbiguousTransactionOperationError", name, err)
		}
		if ambiguous.Branch != name || ambiguous.Index != 2 || ambiguous.FirstIndex != 0 {
			t.Errorf("%s: error = %+v, want operation 2 of the %s branch, clashing with 0", name, ambiguous, name)
		}
		if called {
			t.Errorf("%s: the service ran an ambiguous transaction", name)
		}
	}
}

func TestTransactionExecute_UnmappedRequestIsRefusedBeforeTheService(t *testing.T) {
	called := false
	tr, err := New(&mockQuicBaaS{transactionFunc: func(context.Context, *service.TransactionRequest) (*service.TransactionResponse, error) {
		called = true
		return &service.TransactionResponse{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	for name, req := range map[string]interface{}{
		"string":          "txn",
		"nil request":     (*transaction.TxnRequest)(nil),
		"service request": &service.TransactionRequest{},
	} {
		_, err := tr.Request(ctx, "quicbaas.Transaction", "Execute", req)
		var invalidReqErr *InvalidRequestTypeError
		if !errors.As(err, &invalidReqErr) {
			t.Errorf("%s: error = %v, want *InvalidRequestTypeError", name, err)
		}
	}

	client := transaction.NewClient(tr)
	for name, req := range map[string]*transaction.TxnRequest{
		"op key":      {Success: []transaction.TxnOp{{Type: transaction.TxnOpPut, EntityType: "user", Key: "user-7"}}},
		"compare key": {Compare: []transaction.Compare{{Key: "-1", Target: transaction.CompareVersion}}},
	} {
		_, err := client.Execute(ctx, req)
		var keyErr *InvalidTransactionKeyError
		if !errors.As(err, &keyErr) {
			t.Errorf("%s: error = %v, want *InvalidTransactionKeyError", name, err)
		}
	}

	for name, req := range map[string]*transaction.TxnRequest{
		"mod revision target":    {Compare: []transaction.Compare{{Key: "7", Target: transaction.CompareModRevision}}},
		"create revision target": {Compare: []transaction.Compare{{Key: "7", Target: transaction.CompareCreateRevision}}},
	} {
		_, err := client.Execute(ctx, req)
		var unsupported *UnsupportedTransactionFieldError
		if !errors.As(err, &unsupported) {
			t.Errorf("%s: error = %v, want *UnsupportedTransactionFieldError", name, err)
		}
	}

	if called {
		t.Fatal("the service was called for a request the transport must refuse")
	}
}

// The client's Compare carries no entity type, and the service reads a
// compare's key as "<entity type>:<id>". A converted compare would read ":7"
// instead of "user:7" and take the wrong branch, so the transport must refuse
// it before the service runs either branch's writes.
func TestTransactionExecute_CompareWithoutEntityTypeIsRefused(t *testing.T) {
	called := false
	tr, err := New(&mockQuicBaaS{transactionFunc: func(context.Context, *service.TransactionRequest) (*service.TransactionResponse, error) {
		called = true
		return &service.TransactionResponse{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = transaction.NewClient(tr).Execute(context.Background(), &transaction.TxnRequest{
		Compare: []transaction.Compare{
			{Key: "7", Target: transaction.CompareValue, Result: transaction.CompareEqual, Value: []byte("old")},
		},
		Success: []transaction.TxnOp{{Type: transaction.TxnOpPut, EntityType: "user", Key: "7", Value: []byte("new")}},
		Failure: []transaction.TxnOp{{Type: transaction.TxnOpDelete, EntityType: "user", Key: "7"}},
	})

	var compareErr *UnrepresentableTransactionCompareError
	if !errors.As(err, &compareErr) {
		t.Fatalf("Execute error = %v, want *UnrepresentableTransactionCompareError", err)
	}
	if compareErr.Index != 0 || compareErr.Key != "7" {
		t.Errorf("error = %+v, want index 0 and key \"7\"", compareErr)
	}
	if called {
		t.Fatal("the service ran a transaction whose compare it cannot evaluate; a branch's writes may have executed")
	}
}
