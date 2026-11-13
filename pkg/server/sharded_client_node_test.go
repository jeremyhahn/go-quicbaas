//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// sharded_client_node_test.go exercises ShardedClient. go-qrdb under
// go-quicbaas is embedded only, so every ShardedClient resolves shards to the
// embedded qrdb node's SDK client.

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
)

// newTestNodeBackedClient creates a ShardedClient backed by a real qrdb
// standalone (memory) node for tests that need ForShard to succeed.
func newTestNodeBackedClient(t *testing.T) *ShardedClient {
	t.Helper()
	ctx := context.Background()
	qcfg := &qrdbsdk.DaemonConfig{}
	// Vector storage is PebbleDB-backed and requires a data directory even
	// when the KV data plane runs in memory.
	qcfg.Raft.DataDir = t.TempDir()
	qcfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	node, err := qrdbnode.NewStandaloneNode(ctx, qcfg, nil)
	require.NoError(t, err, "standalone node must start for ForShard tests")
	t.Cleanup(func() { _ = node.Close() })
	return newNodeBackedShardedClient(node)
}

// newDetachedShardedClient returns a ShardedClient with no embedded node
// client attached, for tests that exercise server wiring without a node.
func newDetachedShardedClient() *ShardedClient {
	return &ShardedClient{}
}

// TestShardedClient_ForShard_NoNodeReturnsTypedError verifies that a
// ShardedClient with no embedded node client refuses every shard: there is no
// network transport to fall back to.
func TestShardedClient_ForShard_NoNodeReturnsTypedError(t *testing.T) {
	sc := newDetachedShardedClient()

	client, err := sc.ForShard(4)
	require.Nil(t, client)
	var noNode *ShardedClientNoNodeError
	require.ErrorAs(t, err, &noNode)
	assert.Equal(t, uint64(4), noNode.ShardID)
	assert.ErrorIs(t, err, ErrShardedClientNoNode)
	assert.Contains(t, err.Error(), "shard 4")
}

// TestShardedClient_ForShard_ClosedBeforeNoNode verifies the closed guard
// fires before the node check.
func TestShardedClient_ForShard_ClosedBeforeNoNode(t *testing.T) {
	sc := newDetachedShardedClient()
	require.NoError(t, sc.Close())

	_, err := sc.ForShard(1)
	assert.ErrorIs(t, err, ErrShardedClientClosed)
}

// TestShardedClient_ForShard_NodePath_ReturnsSameClientForAllShards verifies
// that a node-backed ShardedClient returns the same node client for every
// shard ID — the qrdb engine handles internal routing.
func TestShardedClient_ForShard_NodePath_ReturnsSameClientForAllShards(t *testing.T) {
	sc := newTestNodeBackedClient(t)
	t.Cleanup(func() { _ = sc.Close() })

	first, err := sc.ForShard(7)
	require.NoError(t, err)
	require.NotNil(t, first, "node ForShard must return a usable client")

	second, err := sc.ForShard(7)
	require.NoError(t, err)
	// Node path always returns nodeClient — same pointer for every shard.
	assert.Same(t, first, second, "node-backed ForShard must return the same client per shard")
}

// TestShardedClient_ForShard_Embedded_ClosedReturnsTypedError verifies the
// closed-guard fires before any client lookup.
func TestShardedClient_ForShard_Embedded_ClosedReturnsTypedError(t *testing.T) {
	sc := newTestNodeBackedClient(t)
	require.NoError(t, sc.Close())

	_, err := sc.ForShard(1)
	var closedErr *ShardedClientClosedError
	require.ErrorAs(t, err, &closedErr,
		"ForShard after Close must return ShardedClientClosedError")
}

// TestShardedClient_Close_NodeOwned_IsNoOp verifies that Close on a node-owned
// ShardedClient is a no-op (the node manages the client lifetime) and is
// idempotent.
func TestShardedClient_Close_NodeOwned_IsNoOp(t *testing.T) {
	sc := newTestNodeBackedClient(t)
	// Access a shard to confirm the client is usable.
	_, err := sc.ForShard(3)
	require.NoError(t, err, "must resolve a shard before Close")

	require.NoError(t, sc.Close(), "first Close must succeed")
	require.NoError(t, sc.Close(), "Close must be idempotent")
}
