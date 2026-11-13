package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestBatchService_BatchExecute_MixedOpsOneCommit checks that a batch of puts
// and deletes reaches qrdb as one ExecuteBatch call, in order, with each key
// in its entity type's keyspace and a value on puts only.
func TestBatchService_BatchExecute_MixedOpsOneCommit(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	resp, err := svc.BatchExecute(tenantctx.WithSystem(context.Background()), &BatchExecuteRequest{
		Operations: []*BatchExecuteOperation{
			{Type: BatchExecutePut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
			{Type: BatchExecuteDelete, EntityType: "users", Key: "user-old", Value: []byte("ignored")},
			{Type: BatchExecutePut, EntityType: "orders", Key: "order-9", Value: []byte("{}")},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Results, 3)
	for _, result := range resp.Results {
		assert.True(t, result.Succeeded)
	}

	calls := mockQRDB.capturedBatchCalls()
	require.Len(t, calls, 1, "expected exactly one ExecuteBatch call")
	assert.Equal(t, []qrdbtransport.Op{
		{Type: qrdbtransport.OpPut, Key: []byte("users:user-1"), Value: []byte("Alice")},
		{Type: qrdbtransport.OpDelete, Key: []byte("users:user-old")},
		{Type: qrdbtransport.OpPut, Key: []byte("orders:order-9"), Value: []byte("{}")},
	}, calls[0])
}

func TestBatchService_BatchExecute_InvalidatesCache(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	db := svc.(*quicBaaS)
	cacheMock := newKVAuthCacheMock()
	db.cacheManager = cacheMock
	cacheMock.data[kvCacheKey(1, []byte("users:user-1"))] = []byte("stale")
	cacheMock.data[kvCacheKey(1, []byte("users:user-old"))] = []byte("stale")

	_, err := svc.BatchExecute(tenantctx.WithSystem(context.Background()), &BatchExecuteRequest{
		Operations: []*BatchExecuteOperation{
			{Type: BatchExecutePut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
			{Type: BatchExecuteDelete, EntityType: "users", Key: "user-old"},
			{Type: BatchExecutePut, EntityType: "orders", Key: "order-9", Value: []byte("{}")},
		},
	})
	require.NoError(t, err)

	require.NotContains(t, cacheMock.data, kvCacheKey(1, []byte("users:user-1")))
	require.NotContains(t, cacheMock.data, kvCacheKey(1, []byte("users:user-old")))
	require.NotContains(t, cacheMock.data, "orders:order-9")
}

// TestBatchService_BatchExecute_BadRequests checks that each malformed
// request is refused as invalid input before anything reaches qrdb.
func TestBatchService_BatchExecute_BadRequests(t *testing.T) {
	put := func(key string) *BatchExecuteOperation {
		return &BatchExecuteOperation{Type: BatchExecutePut, EntityType: "users", Key: key}
	}
	tests := []struct {
		name      string
		req       *BatchExecuteRequest
		wantIndex int // -1 when the whole request, not one item, is refused
	}{
		{name: "nil request", req: nil, wantIndex: -1},
		{name: "nil operations", req: &BatchExecuteRequest{}, wantIndex: -1},
		{name: "empty operations", req: &BatchExecuteRequest{Operations: []*BatchExecuteOperation{}}, wantIndex: -1},
		{name: "nil operation", req: &BatchExecuteRequest{Operations: []*BatchExecuteOperation{put("a"), nil}}, wantIndex: 1},
		{name: "zero operation type", req: &BatchExecuteRequest{Operations: []*BatchExecuteOperation{
			{EntityType: "users", Key: "a"},
		}}, wantIndex: 0},
		{name: "unknown operation type", req: &BatchExecuteRequest{Operations: []*BatchExecuteOperation{
			put("a"), {Type: BatchExecuteOpType(99), EntityType: "users", Key: "b"},
		}}, wantIndex: 1},
		{name: "empty key", req: &BatchExecuteRequest{Operations: []*BatchExecuteOperation{put("")}}, wantIndex: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockQRDB := newSvcBoostMockQRDBProvider(nil, nil)
			svc := createTestService(t, withQRDBClientProvider(mockQRDB))
			defer svc.Close()

			resp, err := svc.BatchExecute(tenantctx.WithSystem(context.Background()), tt.req)
			require.Error(t, err)
			assert.Nil(t, resp)

			var qbErr *errors.QBError
			require.True(t, stderrors.As(err, &qbErr), "want *errors.QBError, got %T", err)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
			if tt.wantIndex >= 0 {
				var itemErr *api.BatchItemValidationError
				require.True(t, stderrors.As(err, &itemErr), "want *api.BatchItemValidationError, got %v", err)
				assert.Equal(t, tt.wantIndex, itemErr.Index)
			}
			assert.Empty(t, mockQRDB.capturedBatchCalls())
		})
	}
}

// TestBatchService_BatchExecute_QRDBFailure checks that a failed commit is
// returned as an error and no partial response.
func TestBatchService_BatchExecute_QRDBFailure(t *testing.T) {
	mockQRDB := newSvcBoostMockQRDBProviderWithErrors(nil, nil, stderrors.New("raft proposal failed"))
	svc := createTestService(t, withQRDBClientProvider(mockQRDB))
	defer svc.Close()

	resp, err := svc.BatchExecute(tenantctx.WithSystem(context.Background()), &BatchExecuteRequest{
		Operations: []*BatchExecuteOperation{
			{Type: BatchExecutePut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
			{Type: BatchExecuteDelete, EntityType: "users", Key: "user-old"},
		},
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Len(t, mockQRDB.capturedBatchCalls(), 1)
}

// TestBatchExecuteResponse checks the per-operation result mapping, including
// a qrdb response shorter than the request.
func TestBatchExecuteResponse(t *testing.T) {
	resp := batchExecuteResponse(&qrdbtransport.BatchResponse{
		Responses: []qrdbtransport.OpResponse{{Succeeded: true}, {Succeeded: false}},
	}, 3)
	require.Len(t, resp.Results, 3)
	assert.True(t, resp.Results[0].Succeeded)
	assert.False(t, resp.Results[1].Succeeded)
	assert.False(t, resp.Results[2].Succeeded)

	empty := batchExecuteResponse(nil, 2)
	require.Len(t, empty.Results, 2)
	assert.False(t, empty.Results[0].Succeeded)
}
