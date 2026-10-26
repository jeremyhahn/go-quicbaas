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

package service

import (
	"bytes"
	"context"
	stderrors "errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// Permission names a delegated caller needs for each qrdb verb, as go-qrdb's
// embedded transport declares them (sdk/go/transport/embedded/
// transport_authz_registry.go: Get -> kv:get, ExecuteTransaction ->
// transaction:execute, ExecuteBatch -> batch:execute).
const (
	permKVGet              = "kv:get"
	permTransactionExecute = "transaction:execute"
	permBatchExecute       = "batch:execute"
)

// authorizingTransport is a qrdb transport that, like go-qrdb's embedded
// transport on the delegated path, authorizes each verb against the caller's
// grants and refuses with qrdbsdk.PermissionDeniedError. It keeps its data in
// memory and evaluates transaction compares as qrdb's native transaction does:
// a present key has version 1, an absent key version 0 and an empty value.
type authorizingTransport struct {
	*svcBoostMockTransport
	granted map[string]bool
	data    map[string][]byte
}

func newAuthorizingTransport(granted ...string) *authorizingTransport {
	t := &authorizingTransport{
		svcBoostMockTransport: &svcBoostMockTransport{},
		granted:               make(map[string]bool, len(granted)),
		data:                  make(map[string][]byte),
	}
	for _, perm := range granted {
		t.granted[perm] = true
	}
	return t
}

func (t *authorizingTransport) authorize(perm string) error {
	if t.granted[perm] {
		return nil
	}
	return &qrdbsdk.PermissionDeniedError{Resource: perm, Action: "execute", Subject: "caller"}
}

func (t *authorizingTransport) Get(_ context.Context, key string, _ ...transport.ReadOption) ([]byte, error) {
	if err := t.authorize(permKVGet); err != nil {
		return nil, err
	}
	value, ok := t.data[key]
	if !ok {
		return nil, &transport.KeyNotFoundError{Key: key}
	}
	return value, nil
}

func (t *authorizingTransport) ExecuteBatch(_ context.Context, ops []transport.Op, _ ...transport.WriteOption) (*transport.BatchResponse, error) {
	if err := t.authorize(permBatchExecute); err != nil {
		return nil, err
	}
	return &transport.BatchResponse{Responses: t.apply(ops)}, nil
}

func (t *authorizingTransport) ExecuteTransaction(_ context.Context, req *transport.TransactionRequest, _ ...transport.WriteOption) (*transport.TransactionResponse, error) {
	if err := t.authorize(permTransactionExecute); err != nil {
		return nil, err
	}
	succeeded := true
	for _, cmp := range req.Compares {
		if !t.holds(cmp) {
			succeeded = false
			break
		}
	}
	ops := req.SuccessOps
	if !succeeded {
		ops = req.FailureOps
	}
	return &transport.TransactionResponse{Succeeded: succeeded, Responses: t.apply(ops)}, nil
}

func (t *authorizingTransport) holds(cmp transport.Compare) bool {
	value, present := t.data[string(cmp.Key)]
	var order int
	switch cmp.Target {
	case transport.CompareValue:
		if !present {
			return cmp.Result == transport.CompareEqual && len(cmp.Value) == 0
		}
		order = bytes.Compare(value, cmp.Value)
	case transport.CompareVersion:
		var version int64
		if present {
			version = 1
		}
		switch {
		case version < cmp.Version:
			order = -1
		case version > cmp.Version:
			order = 1
		}
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
	default:
		return false
	}
}

func (t *authorizingTransport) apply(ops []transport.Op) []transport.OpResponse {
	responses := make([]transport.OpResponse, len(ops))
	for i, op := range ops {
		key := string(op.Key)
		switch op.Type {
		case transport.OpGet:
			if value, ok := t.data[key]; ok {
				responses[i].Kvs = []*transport.MVCCKeyValue{{Key: op.Key, Value: value, Version: 1}}
				responses[i].Count = 1
			}
		case transport.OpPut:
			t.data[key] = op.Value
		case transport.OpDelete:
			delete(t.data, key)
		}
		responses[i].Succeeded = true
	}
	return responses
}

func newAuthorizingService(t *testing.T, tr *authorizingTransport) QuicBaaS {
	t.Helper()
	client, err := qrdbsdk.NewClient(tr)
	require.NoError(t, err)
	svc := createTestService(t, withQRDBClientProvider(&svcBoostMockQRDBProvider{
		client:    client,
		transport: tr.svcBoostMockTransport,
	}))
	t.Cleanup(func() { require.NoError(t, svc.Close()) })
	return svc
}

func requirePermissionRefusal(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	var denied *qrdbsdk.PermissionDeniedError
	require.True(t, stderrors.As(err, &denied), "refusal must propagate as qrdb's PermissionDeniedError: %v", err)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr))
	assert.Equal(t, errors.ErrForbidden, qbErr.Code)
}

// compareAndWrite is a transaction whose Success branch writes user:1 only
// when user:1 holds "v1", and whose Failure branch writes user:2.
func compareAndWrite() *TransactionRequest {
	return &TransactionRequest{
		Compare: []*CompareCondition{{
			EntityType: "user", ID: 1,
			Target: CompareTargetValue, Operator: CompareOperatorEqual, Value: []byte("v1"),
		}},
		Success: []*TransactionOp{
			{OpType: TransactionOpTypePut, EntityType: "user", ID: 1, Data: []byte("v2")},
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 1},
		},
		Failure: []*TransactionOp{
			{OpType: TransactionOpTypePut, EntityType: "user", ID: 2, Data: []byte("else")},
		},
	}
}

func TestTransaction_DeclaredGrantAloneComparesAndWrites(t *testing.T) {
	tr := newAuthorizingTransport(permTransactionExecute)
	tr.data["user:1"] = []byte("v1")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), compareAndWrite())
	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Equal(t, []byte("v2"), tr.data["user:1"])
	assert.NotContains(t, tr.data, "user:2")
	require.Len(t, resp.Responses, 2)
	assert.True(t, resp.Responses[0].Success)
	assert.True(t, resp.Responses[1].Success)
}

func TestTransaction_ExistsCompareSelectsBranch(t *testing.T) {
	tr := newAuthorizingTransport(permTransactionExecute)
	svc := newAuthorizingService(t, tr)
	req := &TransactionRequest{
		Compare: []*CompareCondition{{
			EntityType: "user", ID: 1, Target: CompareTargetExists, Operator: CompareOperatorNotEqual,
		}},
		Success: []*TransactionOp{{OpType: TransactionOpTypePut, EntityType: "user", ID: 1, Data: []byte("created")}},
	}

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Equal(t, []byte("created"), tr.data["user:1"])

	resp, err = svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.False(t, resp.Succeeded, "user:1 now exists, so the create-if-absent compare fails")
}

func TestTransaction_WithoutDeclaredGrantIsRefusedNotElseBranch(t *testing.T) {
	// The caller holds the inner verbs' grants but not transaction:execute.
	tr := newAuthorizingTransport(permKVGet, permBatchExecute)
	tr.data["user:1"] = []byte("v1")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.Transaction(tenantctx.WithSystem(context.Background()), compareAndWrite())
	requirePermissionRefusal(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, []byte("v1"), tr.data["user:1"])
	assert.NotContains(t, tr.data, "user:2", "a refused read must never run the Failure branch")
}

func TestTransaction_UnsupportedValueOperatorIsInvalidInput(t *testing.T) {
	svc := newAuthorizingService(t, newAuthorizingTransport(permTransactionExecute))
	req := &TransactionRequest{Compare: []*CompareCondition{{
		EntityType: "user", ID: 1, Target: CompareTargetValue, Operator: CompareOperatorGreaterEqual, Value: []byte("a"),
	}}}

	_, err := svc.Transaction(tenantctx.WithSystem(context.Background()), req)
	require.Error(t, err)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr))
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestTransaction_TenantCannotReachAnotherTenantsKey(t *testing.T) {
	tr := newAuthorizingTransport(permTransactionExecute)
	tr.data["__t:tenant-b:user:1"] = []byte("v1")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.Transaction(tenantctx.WithTenant(context.Background(), "tenant-a"), compareAndWrite())
	require.NoError(t, err)
	assert.False(t, resp.Succeeded, "tenant A's compare must not see tenant B's value")
	assert.Equal(t, []byte("v1"), tr.data["__t:tenant-b:user:1"], "tenant A must not write tenant B's key")
	assert.Equal(t, []byte("else"), tr.data["__t:tenant-a:user:2"], "tenant A's branch writes its own keyspace")
	assert.NotContains(t, tr.data, "user:2", "a tenant's write must not land in the system keyspace")
}

func TestTransaction_WithoutTenantIsRefusedBeforeDispatch(t *testing.T) {
	tr := newAuthorizingTransport(permTransactionExecute)
	svc := newAuthorizingService(t, tr)

	_, err := svc.Transaction(context.Background(), compareAndWrite())
	require.Error(t, err)
	assert.Empty(t, tr.data, "a transaction without a tenant must not reach qrdb")
}

func TestBatchGet_DeclaredGrantAloneReads(t *testing.T) {
	tr := newAuthorizingTransport(permBatchExecute)
	tr.data["user:1"] = []byte("one")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), &BatchGetRequest{
		Requests: []*GetRequest{{EntityType: "user", ID: 1}, {EntityType: "user", ID: 2}},
	})
	require.NoError(t, err)
	require.Len(t, resp.Responses, 2)
	assert.True(t, resp.Responses[0].Found)
	assert.Equal(t, []byte("one"), resp.Responses[0].Data)
	assert.Equal(t, uint64(2), resp.Responses[1].ID)
	assert.False(t, resp.Responses[1].Found, "an absent key is the one case reported as not found")
}

func TestBatchGet_ReadsTenantKeysPerShardInRequestOrder(t *testing.T) {
	tr := newAuthorizingTransport(permBatchExecute)
	tr.data["__t:tenant-a:user:1"] = []byte("a1")
	tr.data["__t:tenant-a:user:3"] = []byte("a3")
	tr.data["user:2"] = []byte("system")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.BatchGet(tenantctx.WithTenant(context.Background(), "tenant-a"), &BatchGetRequest{
		Requests: []*GetRequest{
			{EntityType: "user", ID: 1},
			{EntityType: "user", ID: 2, ShardID: 2},
			{EntityType: "user", ID: 3},
		},
	})
	require.NoError(t, err)
	require.Len(t, resp.Responses, 3)
	assert.Equal(t, []byte("a1"), resp.Responses[0].Data)
	assert.False(t, resp.Responses[1].Found, "tenant A must not read the system tenant's key")
	assert.Equal(t, []byte("a3"), resp.Responses[2].Data)
}

func TestBatchGet_NilRequestEntryIsInvalidInput(t *testing.T) {
	tr := newAuthorizingTransport(permBatchExecute)
	svc := newAuthorizingService(t, tr)

	_, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), &BatchGetRequest{Requests: []*GetRequest{nil}})
	require.Error(t, err)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr))
	assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
}

func TestBatchGet_WithoutDeclaredGrantIsRefusedNotNotFound(t *testing.T) {
	// The caller holds kv:get but not the entry point's batch:execute.
	tr := newAuthorizingTransport(permKVGet)
	tr.data["user:1"] = []byte("one")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.BatchGet(tenantctx.WithSystem(context.Background()), &BatchGetRequest{
		Requests: []*GetRequest{{EntityType: "user", ID: 1}},
	})
	requirePermissionRefusal(t, err)
	assert.Nil(t, resp, "a refusal must never be reported as Found:false")
}

func TestBatchPut_WithoutDeclaredGrantIsForbidden(t *testing.T) {
	// The caller holds kv:put but not the entry point's batch:execute.
	tr := newAuthorizingTransport("kv:put")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.BatchPut(tenantctx.WithSystem(context.Background()), &BatchPutRequest{
		Requests: []*PutRequest{{EntityType: "user", ID: 1, Data: []byte("one")}},
	})
	requirePermissionRefusal(t, err)
	assert.Nil(t, resp)
	assert.Empty(t, tr.data)
}

func TestBatchDelete_WithoutDeclaredGrantIsForbidden(t *testing.T) {
	// The caller holds kv:delete but not the entry point's batch:execute.
	tr := newAuthorizingTransport("kv:delete")
	tr.data["user:1"] = []byte("one")
	svc := newAuthorizingService(t, tr)

	resp, err := svc.BatchDelete(tenantctx.WithSystem(context.Background()), &BatchDeleteRequest{
		Requests: []*DeleteRequest{{EntityType: "user", ID: 1}},
	})
	requirePermissionRefusal(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, []byte("one"), tr.data["user:1"])
}

func TestKV_RefusalIsForbiddenNotRaftOrNotFound(t *testing.T) {
	refusal := &qrdbsdk.PermissionDeniedError{Resource: "kv", Action: "execute", Subject: "caller"}
	ctx := tenantctx.WithSystem(context.Background())

	t.Run("put", func(t *testing.T) {
		tr := newAuthorizingTransport()
		tr.putErr = refusal
		svc := newAuthorizingService(t, tr)

		resp, err := svc.Put(ctx, &PutRequest{EntityType: "user", ID: 1, Data: []byte("one")})
		requirePermissionRefusal(t, err)
		assert.Nil(t, resp)
	})

	t.Run("get", func(t *testing.T) {
		// The caller lacks kv:get, so the store refuses before it looks.
		tr := newAuthorizingTransport()
		tr.data["user:1"] = []byte("one")
		svc := newAuthorizingService(t, tr)

		resp, err := svc.Get(ctx, &GetRequest{EntityType: "user", ID: 1})
		requirePermissionRefusal(t, err)
		assert.Nil(t, resp, "a refusal must never be reported as absence")
	})

	t.Run("delete", func(t *testing.T) {
		tr := newAuthorizingTransport()
		tr.deleteErr = refusal
		svc := newAuthorizingService(t, tr)

		resp, err := svc.Delete(ctx, &DeleteRequest{EntityType: "user", ID: 1})
		requirePermissionRefusal(t, err)
		assert.Nil(t, resp)
	})
}
