package service

import (
	"context"
	stderrors "errors"
	"sync/atomic"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// conditionalPutIDs gives each test its own entity ID on the package's shared
// qrdb node.
var conditionalPutIDs atomic.Uint64

// sharedNodeProvider serves the shared qrdb test node's client for every
// shard. Close is a no-op: the node outlives each test service.
type sharedNodeProvider struct {
	client *qrdbsdk.Client
}

func (p sharedNodeProvider) ForShard(uint64) (*qrdbsdk.Client, error) { return p.client, nil }
func (p sharedNodeProvider) Close() error                             { return nil }

// newConditionalPutService builds a service whose KV writes reach a real
// in-process qrdb node, so qrdb, not a double, decides each precondition.
func newConditionalPutService(t *testing.T) (QuicBaaS, uint64) {
	t.Helper()
	n := qrdbnode.Shared(t)
	svc := createTestService(t, withQRDBClientProvider(sharedNodeProvider{client: n.Node.Client()}))
	return svc, 1_000_000 + conditionalPutIDs.Add(1)
}

func TestKVService_PutIfAbsent_StoresOnlyWhenAbsent(t *testing.T) {
	svc, id := newConditionalPutService(t)
	ctx := tenantctx.WithSystem(context.Background())

	first, err := svc.PutIfAbsent(ctx, &PutIfAbsentRequest{EntityType: "casentity", ID: id, Data: []byte("first")})
	require.NoError(t, err)
	assert.True(t, first.Applied, "an absent entity is created")

	second, err := svc.PutIfAbsent(ctx, &PutIfAbsentRequest{EntityType: "casentity", ID: id, Data: []byte("second")})
	require.NoError(t, err)
	assert.False(t, second.Applied, "a present entity is left alone")

	got, err := svc.Get(ctx, &GetRequest{EntityType: "casentity", ID: id})
	require.NoError(t, err)
	assert.Equal(t, []byte("first"), got.Data)
}

func TestKVService_CompareAndSwap_SwapsOnMatchOnly(t *testing.T) {
	svc, id := newConditionalPutService(t)
	ctx := tenantctx.WithSystem(context.Background())

	_, err := svc.PutIfAbsent(ctx, &PutIfAbsentRequest{EntityType: "casentity", ID: id, Data: []byte("v1")})
	require.NoError(t, err)

	swapped, err := svc.CompareAndSwap(ctx, &CompareAndSwapRequest{EntityType: "casentity", ID: id, Expect: []byte("v1"), Set: []byte("v2")})
	require.NoError(t, err)
	assert.True(t, swapped.Applied, "a matching expect swaps")

	stale, err := svc.CompareAndSwap(ctx, &CompareAndSwapRequest{EntityType: "casentity", ID: id, Expect: []byte("v1"), Set: []byte("v3")})
	require.NoError(t, err)
	assert.False(t, stale.Applied, "a stale expect is refused")

	got, err := svc.Get(ctx, &GetRequest{EntityType: "casentity", ID: id})
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), got.Data)
}

func TestKVService_CompareAndSwap_AbsentEntityNeverMatches(t *testing.T) {
	svc, id := newConditionalPutService(t)
	ctx := tenantctx.WithSystem(context.Background())

	// A nil Expect must not turn the swap into a create.
	for _, expect := range [][]byte{nil, {}} {
		resp, err := svc.CompareAndSwap(ctx, &CompareAndSwapRequest{EntityType: "casentity", ID: id, Expect: expect, Set: []byte("x")})
		require.NoError(t, err)
		assert.False(t, resp.Applied)
	}
}

func TestKVService_ConditionalPut_InvalidInput(t *testing.T) {
	svc := createTestService(t)
	ctx := tenantctx.WithSystem(context.Background())

	calls := map[string]func() (*ConditionalPutResponse, error){
		"CompareAndSwap nil": func() (*ConditionalPutResponse, error) { return svc.CompareAndSwap(ctx, nil) },
		"PutIfAbsent nil":    func() (*ConditionalPutResponse, error) { return svc.PutIfAbsent(ctx, nil) },
		"CompareAndSwap no type": func() (*ConditionalPutResponse, error) {
			return svc.CompareAndSwap(ctx, &CompareAndSwapRequest{ID: 1, Expect: []byte("a"), Set: []byte("b")})
		},
		"PutIfAbsent no type": func() (*ConditionalPutResponse, error) {
			return svc.PutIfAbsent(ctx, &PutIfAbsentRequest{ID: 1, Data: []byte("a")})
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			resp, err := call()
			assert.Nil(t, resp)
			var qbErr *errors.QBError
			require.True(t, stderrors.As(err, &qbErr), "want *errors.QBError, got %v", err)
			assert.Equal(t, errors.ErrInvalidInput, qbErr.Code)
		})
	}
}

func TestKVService_ConditionalPut_QRDBClientError(t *testing.T) {
	provider := newSvcBoostMockQRDBProvider(nil, nil)
	require.NoError(t, provider.client.Close())
	svc := createTestService(t, withQRDBClientProvider(provider))
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.PutIfAbsent(ctx, &PutIfAbsentRequest{EntityType: "casentity", ID: 1, Data: []byte("a")})
	assert.Nil(t, resp)
	var qbErr *errors.QBError
	require.True(t, stderrors.As(err, &qbErr), "want *errors.QBError, got %v", err)
	assert.Equal(t, errors.ErrRaft, qbErr.Code)
}
