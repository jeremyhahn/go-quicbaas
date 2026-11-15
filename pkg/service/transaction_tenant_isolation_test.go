package service

import (
	"bytes"
	"context"
	"errors"
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

// ExecuteTransaction evaluates the compares against the map and applies the
// selected branch, as qrdb does: a present key has version 1, an absent key
// version 0, and a Get of an absent key returns no Kvs. Every request is
// recorded in txCalls; compare and Get keys count as reads, writes as
// batched writes.
func (t *kvMapTransport) ExecuteTransaction(_ context.Context, req *transport.TransactionRequest, _ ...transport.WriteOption) (*transport.TransactionResponse, error) {
	t.svcBoostMockTransport.mu.Lock()
	t.svcBoostMockTransport.txCalls = append(t.svcBoostMockTransport.txCalls, req)
	t.svcBoostMockTransport.mu.Unlock()

	t.storeMu.Lock()
	defer t.storeMu.Unlock()
	succeeded := true
	for _, cmp := range req.Compares {
		t.getKeys = append(t.getKeys, string(cmp.Key))
		if !kvMapCompare(t.store, cmp) {
			succeeded = false
			break
		}
	}
	ops := req.SuccessOps
	if !succeeded {
		ops = req.FailureOps
	}
	resp := &transport.TransactionResponse{Succeeded: succeeded, Responses: make([]transport.OpResponse, 0, len(ops))}
	for _, op := range ops {
		result := transport.OpResponse{Succeeded: true}
		switch op.Type {
		case transport.OpGet:
			// go-qrdb's transaction encoder encodes only Put and Delete, and
			// refuses a Get with UnsupportedTransactionOpError, which the SDK
			// does not re-export.
			return nil, errors.New("unsupported transaction op: get")
		case transport.OpPut:
			t.batchOps++
			t.store[string(op.Key)] = op.Value
		case transport.OpDelete:
			t.batchOps++
			delete(t.store, string(op.Key))
		}
		resp.Responses = append(resp.Responses, result)
	}
	return resp, nil
}

// txKeys returns the first key of each recorded transaction's success branch.
func (t *kvMapTransport) txKeys() []string {
	t.svcBoostMockTransport.mu.Lock()
	defer t.svcBoostMockTransport.mu.Unlock()
	keys := make([]string, 0, len(t.svcBoostMockTransport.txCalls))
	for _, call := range t.svcBoostMockTransport.txCalls {
		if len(call.SuccessOps) > 0 {
			keys = append(keys, string(call.SuccessOps[0].Key))
		}
	}
	return keys
}

// kvMapCompare evaluates one qrdb compare against the map.
func kvMapCompare(store map[string][]byte, cmp transport.Compare) bool {
	value, present := store[string(cmp.Key)]
	order := 0
	switch cmp.Target {
	case transport.CompareVersion:
		var version int64
		if present {
			version = 1
		}
		order = cmpInt64(version, cmp.Version)
	case transport.CompareValue:
		order = bytes.Compare(value, cmp.Value)
	default:
		return false
	}
	switch cmp.Result {
	case transport.CompareEqual:
		return order == 0
	case transport.CompareNotEqual:
		return order != 0
	case transport.CompareGreater:
		return order > 0
	case transport.CompareLess:
		return order < 0
	}
	return false
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
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
			{OpType: TransactionOpTypePut, EntityType: "user", ID: 3, Data: []byte(`{}`)},
		},
		Failure: []*TransactionOp{
			{OpType: TransactionOpTypePut, EntityType: "user", ID: 4, Data: []byte(`{}`)},
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
	// A transaction cannot carry a Get, so the read goes through the KV verb.
	resp, err := svc.Get(ctx, &GetRequest{EntityType: "user", ID: 1})
	if err != nil {
		assert.Contains(t, err.Error(), "not found")
		return nil
	}
	if resp == nil || len(resp.Data) == 0 {
		return nil
	}
	return resp.Data
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
		assert.EqualValues(t, (3), respA.Responses[0].ID)

		respB := runTxn(t, svc, ctxB, existsGuard())
		assert.False(t, respB.Succeeded)
		require.Len(t, respB.Responses, 1)
		assert.EqualValues(t, (4), respB.Responses[0].ID)
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

		keys := kv.txKeys()
		require.Len(t, keys, 2)
		keyA, keyB := keys[0], keys[1]
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

// The pinned go-qrdb transaction encoder encodes only Put and Delete, so a
// transaction carrying a Get, in either branch, is refused before qrdb is
// called.
func TestTransactionService_Transaction_GetRefused(t *testing.T) {
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	get := &TransactionOp{OpType: TransactionOpTypeGet, EntityType: "user", ID: 1}
	put := &TransactionOp{OpType: TransactionOpTypePut, EntityType: "user", ID: 2, Data: []byte(`{}`)}
	cases := map[string]*TransactionRequest{
		"get in success": {Success: []*TransactionOp{get}},
		"get only in failure": {
			Compare: []*CompareCondition{
				{EntityType: "user", ID: 1, Target: CompareTargetExists, Operator: CompareOperatorNotEqual},
			},
			Success: []*TransactionOp{put},
			Failure: []*TransactionOp{get},
		},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			svc, kv := newKVMapTestService(t)

			resp, err := svc.Transaction(ctx, req)
			require.Error(t, err)
			assert.Nil(t, resp)

			reads, writes := kv.accessCount()
			assert.Zero(t, reads, "a refused transaction must not read the store")
			assert.Zero(t, writes, "a refused transaction must not write the store")
		})
	}
}
