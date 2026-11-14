package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// realNodeProvider serves every shard from the shared real qrdb node. Close
// leaves the node's client open: the node is shared by the package's tests.
type realNodeProvider struct{ client *qrdbsdk.Client }

func (p *realNodeProvider) ForShard(uint64) (*qrdbsdk.Client, error) { return p.client, nil }
func (p *realNodeProvider) Close() error                             { return nil }
func (p *realNodeProvider) IsEmbedded() bool                         { return true }

// TestTransactionService_Transaction_MixedReadWrite_RealNode drives a
// transaction that reads and writes in both branches against a real qrdb
// node (issue #1904, Codex 7453 P1 6). The native transaction encoder of
// go-qrdb accepts only PUT and DELETE, and encodes both branches before it
// evaluates the compares, so a translation that forwards GET to it fails
// every transaction holding a GET, even in the branch not taken. A test
// transport that implements GET itself masks that; the real node does not.
func TestTransactionService_Transaction_MixedReadWrite_RealNode(t *testing.T) {
	n := qrdbnode.Shared(t)
	svc := createTestService(t, withQRDBClientProvider(&realNodeProvider{client: n.Node.Client()}))
	t.Cleanup(func() { svc.Close() })

	// A tenant of its own: the node is shared and keeps state across tests.
	ctx, cancel := context.WithTimeout(
		tenantctx.WithTenant(context.Background(), fmt.Sprintf("tx1904-%d", time.Now().UnixNano())),
		30*time.Second)
	defer cancel()

	const seeded = `{"name":"seeded"}`
	const written = `{"name":"written"}`

	// Seed user 1 with a write-only transaction.
	runTxn(t, svc, ctx, putUser(seeded))

	// Mixed transaction: the compare holds, the success branch reads user 1
	// and writes user 2; the failure branch (not taken) also holds a GET.
	resp := runTxn(t, svc, ctx, &TransactionRequest{
		Compare: []*CompareCondition{
			{EntityType: "user", ID: 1, Target: CompareTargetExists, Operator: CompareOperatorEqual},
		},
		Success: []*TransactionOp{
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 1},
			{OpType: TransactionOpTypePut, EntityType: "user", ID: 2, Data: []byte(written)},
		},
		Failure: []*TransactionOp{
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 3},
			{OpType: TransactionOpTypeDelete, EntityType: "user", ID: 1},
		},
	})
	require.True(t, resp.Succeeded, "the compare holds, so the success branch runs")
	require.Len(t, resp.Responses, 2)
	byOp := map[TransactionOpType]*TransactionOpResponse{}
	for _, r := range resp.Responses {
		byOp[r.OpType] = r
	}
	require.Contains(t, byOp, TransactionOpTypeGet)
	require.Contains(t, byOp, TransactionOpTypePut)
	assert.True(t, byOp[TransactionOpTypeGet].Success, byOp[TransactionOpTypeGet].Error)
	assert.Equal(t, seeded, string(byOp[TransactionOpTypeGet].Data))
	assert.True(t, byOp[TransactionOpTypePut].Success, byOp[TransactionOpTypePut].Error)

	// The write landed and the failure branch's delete did not run.
	assert.Equal(t, seeded, string(getUserData(t, svc, ctx)))
	got := runTxn(t, svc, ctx, &TransactionRequest{
		Success: []*TransactionOp{{OpType: TransactionOpTypeGet, EntityType: "user", ID: 2}},
	})
	require.Len(t, got.Responses, 1)
	require.True(t, got.Responses[0].Success, got.Responses[0].Error)
	assert.Equal(t, written, string(got.Responses[0].Data))

	// Mixed transaction whose compare fails: the failure branch reads and
	// writes, and the success branch (not taken) holds a GET.
	resp = runTxn(t, svc, ctx, &TransactionRequest{
		Compare: []*CompareCondition{
			{EntityType: "user", ID: 9, Target: CompareTargetExists, Operator: CompareOperatorEqual},
		},
		Success: []*TransactionOp{
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 9},
		},
		Failure: []*TransactionOp{
			{OpType: TransactionOpTypeGet, EntityType: "user", ID: 2},
			{OpType: TransactionOpTypeDelete, EntityType: "user", ID: 1},
		},
	})
	require.False(t, resp.Succeeded, "user 9 does not exist, so the failure branch runs")
	require.Len(t, resp.Responses, 2)
	for _, r := range resp.Responses {
		assert.True(t, r.Success, "%s: %s", r.OpType, r.Error)
		if r.OpType == TransactionOpTypeGet {
			assert.Equal(t, written, string(r.Data))
		}
	}
	gone := runTxn(t, svc, ctx, getUser())
	require.Len(t, gone.Responses, 1)
	assert.False(t, gone.Responses[0].Success && gone.Responses[0].Data != nil,
		"the failure branch deleted user 1")
}
