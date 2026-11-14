package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/batch"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TestBatchExecute_ThroughSDKClient drives SDK().Batch().Execute over the
// embedded transport: the mixed put/delete batch reaches the service as one
// call and the service result maps back to the SDK response (#1533).
func TestBatchExecute_ThroughSDKClient(t *testing.T) {
	var got *service.BatchExecuteRequest
	mock := &mockQuicBaaS{
		batchExecuteFunc: func(_ context.Context, req *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
			got = req
			return &service.BatchExecuteResponse{
				Results: []*service.BatchExecuteResult{{Succeeded: true}, {Succeeded: true}},
			}, nil
		},
	}
	tr, err := New(mock)
	require.NoError(t, err)

	resp, err := batch.NewClient(tr).Execute(context.Background(), &batch.BatchRequest{
		Operations: []batch.BatchOperation{
			{Type: batch.OpPut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
			{Type: batch.OpDelete, EntityType: "users", Key: "user-old"},
		},
	})
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, []*service.BatchExecuteOperation{
		{Type: service.BatchExecutePut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
		{Type: service.BatchExecuteDelete, EntityType: "users", Key: "user-old"},
	}, got.Operations)
	assert.Equal(t, &batch.BatchResponse{
		Results: []batch.BatchResult{{Success: true}, {Success: true}},
	}, resp)
}

// TestBatchExecute_BadRequests checks that requests the handler cannot
// convert are refused with a typed error and never reach the service.
func TestBatchExecute_BadRequests(t *testing.T) {
	tests := []struct {
		name string
		req  interface{}
	}{
		{name: "untyped nil", req: nil},
		{name: "typed nil", req: (*batch.BatchRequest)(nil)},
		{name: "wrong type", req: &service.BatchPutRequest{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			mock := &mockQuicBaaS{
				batchExecuteFunc: func(context.Context, *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
					called = true
					return &service.BatchExecuteResponse{}, nil
				},
			}
			tr, err := New(mock)
			require.NoError(t, err)

			_, err = tr.Request(context.Background(), "quicbaas.Batch", "Execute", tt.req)
			var typeErr *InvalidRequestTypeError
			assert.ErrorAs(t, err, &typeErr)
			assert.False(t, called)
		})
	}
}

// TestBatchExecute_UnknownOpType checks that an operation type the SDK does
// not define reaches the service as the zero type, which the service refuses.
// The SDK client validates types itself, so the request goes to the transport
// directly.
func TestBatchExecute_UnknownOpType(t *testing.T) {
	var got *service.BatchExecuteRequest
	mock := &mockQuicBaaS{
		batchExecuteFunc: func(_ context.Context, req *service.BatchExecuteRequest) (*service.BatchExecuteResponse, error) {
			got = req
			return nil, &pkgerrors.QBError{Code: pkgerrors.ErrInvalidInput, Op: "service.BatchExecute"}
		},
	}
	tr, err := New(mock)
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Batch", "Execute", &batch.BatchRequest{
		Operations: []batch.BatchOperation{{Type: batch.OpType(99), EntityType: "users", Key: "k"}},
	})
	assert.True(t, sdkerrors.IsInvalidInput(err), "want invalid input, got %v", err)
	require.NotNil(t, got)
	assert.Equal(t, service.BatchExecuteOpType(0), got.Operations[0].Type)
}
