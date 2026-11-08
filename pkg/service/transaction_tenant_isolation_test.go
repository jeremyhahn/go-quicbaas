package service

import (
	"context"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// kvMapTransport is a qrdb transport backed by an in-memory map keyed by the
// storage key, so transactions in different tenants observe each other only
// if their keys collide. Methods it does not override come from the embedded
// svcBoostMockTransport.
type kvMapTransport struct {
	*svcBoostMockTransport
	storeMu  sync.Mutex
	store    map[string][]byte
	getKeys  []string
	batchOps int
}

func newKVMapTransport() *kvMapTransport {
	return &kvMapTransport{
		svcBoostMockTransport: &svcBoostMockTransport{},
		store:                 make(map[string][]byte),
	}
}

func (t *kvMapTransport) Get(_ context.Context, key string, _ ...transport.ReadOption) ([]byte, error) {
	t.storeMu.Lock()
	defer t.storeMu.Unlock()
	t.getKeys = append(t.getKeys, key)
	return t.store[key], nil
}

func (t *kvMapTransport) ExecuteBatch(ctx context.Context, ops []transport.Op, opts ...transport.WriteOption) (*transport.BatchResponse, error) {
	t.storeMu.Lock()
	t.batchOps += len(ops)
	for _, op := range ops {
		switch op.Type {
		case transport.OpPut:
			t.store[string(op.Key)] = op.Value
		case transport.OpDelete:
			delete(t.store, string(op.Key))
		}
	}
	t.storeMu.Unlock()
	return t.svcBoostMockTransport.ExecuteBatch(ctx, ops, opts...)
}

// accessCount reports how many reads and batched writes reached the store.
func (t *kvMapTransport) accessCount() (int, int) {
	t.storeMu.Lock()
	defer t.storeMu.Unlock()
	return len(t.getKeys), t.batchOps
}

func newKVMapTestService(t *testing.T) (QuicBaaS, *kvMapTransport) {
	t.Helper()
	kv := newKVMapTransport()
	client, err := qrdbsdk.NewClient(kv)
	require.NoError(t, err)
	provider := &svcBoostMockQRDBProvider{client: client, transport: kv.svcBoostMockTransport}
	svc := createTestService(t, withQRDBClientProvider(provider))
	t.Cleanup(func() { svc.Close() })
	return svc, kv
}

func putUser(data string) *TransactionRequest {
	return &TransactionRequest{
		Success: []*TransactionOp{
			{OpType: TransactionOpTypePut, EntityType: "user", ID: 1, Data: []byte(data)},
		},
	}
}

func getUser() *TransactionRequest {
	return &TransactionRequest{
		Success: []*TransactionOp{
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 1},
		},
	}
}

func deleteUser() *TransactionRequest {
	return &TransactionRequest{
		Success: []*TransactionOp{
			{OpType: TransactionOpTypeDelete, EntityType: "user", ID: 1},
		},
	}
}

// existsGuard succeeds when user 1 exists, otherwise takes the failure branch.
func existsGuard() *TransactionRequest {
	return &TransactionRequest{
		Compare: []*CompareCondition{
			{EntityType: "user", ID: 1, Target: CompareTargetExists, Operator: CompareOperatorEqual},
		},
		Success: []*TransactionOp{
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 1},
		},
		Failure: []*TransactionOp{
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 2},
		},
	}
}

func runTxn(t *testing.T, svc QuicBaaS, ctx context.Context, req *TransactionRequest) *TransactionResponse {
	t.Helper()
	resp, err := svc.Transaction(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp
}

func getUserData(t *testing.T, svc QuicBaaS, ctx context.Context) []byte {
	t.Helper()
	resp := runTxn(t, svc, ctx, getUser())
	require.Len(t, resp.Responses, 1)
	require.True(t, resp.Responses[0].Success)
	return resp.Responses[0].Data
}

func TestTransactionService_Transaction_TenantIsolation(t *testing.T) {
	ctxA := tenantctx.WithTenant(context.Background(), "tenant-a")
	ctxB := tenantctx.WithTenant(context.Background(), "tenant-b")
	const dataA = `{"owner":"a"}`
	const dataB = `{"owner":"b"}`

	t.Run("get in B does not see A's put", func(t *testing.T) {
		svc, _ := newKVMapTestService(t)
		runTxn(t, svc, ctxA, putUser(dataA))

		assert.Nil(t, getUserData(t, svc, ctxB))
		assert.Equal(t, dataA, string(getUserData(t, svc, ctxA)))
	})

	t.Run("compare in B does not see A's record", func(t *testing.T) {
		svc, _ := newKVMapTestService(t)
		runTxn(t, svc, ctxA, putUser(dataA))

		respA := runTxn(t, svc, ctxA, existsGuard())
		assert.True(t, respA.Succeeded)
		require.Len(t, respA.Responses, 1)
		assert.EqualValues(t, (1), respA.Responses[0].ID)
		assert.Equal(t, dataA, string(respA.Responses[0].Data))

		respB := runTxn(t, svc, ctxB, existsGuard())
		assert.False(t, respB.Succeeded)
		require.Len(t, respB.Responses, 1)
		assert.EqualValues(t, (2), respB.Responses[0].ID)
	})

	t.Run("put in B does not change A's value", func(t *testing.T) {
		svc, _ := newKVMapTestService(t)
		runTxn(t, svc, ctxA, putUser(dataA))
		runTxn(t, svc, ctxB, putUser(dataB))

		assert.Equal(t, dataA, string(getUserData(t, svc, ctxA)))
		assert.Equal(t, dataB, string(getUserData(t, svc, ctxB)))
	})

	t.Run("delete in A leaves B's record", func(t *testing.T) {
		svc, _ := newKVMapTestService(t)
		runTxn(t, svc, ctxA, putUser(dataA))
		runTxn(t, svc, ctxB, putUser(dataB))
		runTxn(t, svc, ctxA, deleteUser())

		assert.Nil(t, getUserData(t, svc, ctxA))
		assert.Equal(t, dataB, string(getUserData(t, svc, ctxB)))
	})

	t.Run("keys for A and B differ", func(t *testing.T) {
		svc, kv := newKVMapTestService(t)
		runTxn(t, svc, ctxA, putUser(dataA))
		runTxn(t, svc, ctxB, putUser(dataB))

		kv.mu.Lock()
		calls := kv.batchCalls
		kv.mu.Unlock()
		require.Len(t, calls, 2)
		keyA := string(calls[0][0].Key)
		keyB := string(calls[1][0].Key)
		assert.NotEqual(t, keyA, keyB)
		assert.Contains(t, keyA, "tenant-a")
		assert.Contains(t, keyB, "tenant-b")
	})
}

func TestTransactionService_Transaction_UnboundContextRefused(t *testing.T) {
	svc, kv := newKVMapTestService(t)
	req := &TransactionRequest{
		Compare: []*CompareCondition{
			{EntityType: "user", ID: 1, Target: CompareTargetExists, Operator: CompareOperatorNotEqual},
		},
		Success: []*TransactionOp{
			{OpType: TransactionOpTypePut, EntityType: "user", ID: 1, Data: []byte(`{"owner":"nobody"}`)},
		},
	}

	resp, err := svc.Transaction(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	reads, writes := kv.accessCount()
	assert.Zero(t, reads, "an unbound context must not read the store")
	assert.Zero(t, writes, "an unbound context must not write the store")
}
