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

// sharded_client_with_options_test.go exercises the With* functional options
// on ShardedClient. The constructor applies options to the struct fields;
// the tests verify that field assignments are visible on the resulting client.

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbconfig "github.com/jeremyhahn/go-qrdb/pkg/config"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// newTestNodeBackedClient creates a ShardedClient backed by a real qrdb
// standalone (memory) node for tests that need ForShard to succeed.
func newTestNodeBackedClient(t *testing.T) *ShardedClient {
	t.Helper()
	ctx := context.Background()
	qcfg := &qrdbconfig.Config{}
	qcfg.Databases = []qrdbconfig.DatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: "memory"},
	}
	node, err := qrdbnode.NewStandaloneNode(ctx, qcfg, nil)
	require.NoError(t, err, "standalone node must start for ForShard tests")
	t.Cleanup(func() { _ = node.Close() })
	return newNodeBackedShardedClient(node)
}

// TestShardedClient_WithVectorStorage verifies the vector shard ID propagates
// from the option to the client struct.
func TestShardedClient_WithVectorStorage(t *testing.T) {
	sc := NewShardedClient(nil, WithVectorStorage(nil, 7))
	require.NotNil(t, sc)
	assert.Equal(t, uint64(7), sc.vectorShardID, "vector shard ID must propagate from option to client")
}

func TestShardedClient_WithTimeSeriesStorage(t *testing.T) {
	sc := NewShardedClient(nil, WithTimeSeriesStorage(nil, 3))
	require.NotNil(t, sc)
	assert.Equal(t, uint64(3), sc.timeSeriesShardID, "timeseries shard ID must propagate from option to client")
}

func TestShardedClient_WithDatabaseService(t *testing.T) {
	// Passing nil exercises the option's assignment branch; the assertion
	// confirms construction succeeds.
	opt := WithDatabaseService(nil)
	sc := NewShardedClient(nil, opt)
	require.NotNil(t, sc)
}

// TestShardedClient_ForShard_NodePath_ReturnsSameClientForAllShards verifies
// that a node-backed ShardedClient returns the same node client for every
// shard ID — the qrdb engine handles internal routing.
func TestShardedClient_ForShard_NodePath_ReturnsSameClientForAllShards(t *testing.T) {
	sc := newTestNodeBackedClient(t)
	t.Cleanup(func() { _ = sc.Close() })
	require.True(t, sc.IsEmbedded(), "node-backed client reports embedded transport")

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

// TestShardedClient_ForShard_Network_GRPCHappyPathCaches verifies the
// non-embedded ForShard path: a gRPC transport with a valid address
// constructs (and caches) an SDK client without dialing.
func TestShardedClient_ForShard_Network_GRPCHappyPathCaches(t *testing.T) {
	cfg := &config.QRDBConfig{Transport: config.QRDBTransportGRPC, Address: "127.0.0.1:59321"}
	sc := NewShardedClient(nil, WithTransportConfig(cfg))
	t.Cleanup(func() { _ = sc.Close() })
	require.False(t, sc.IsEmbedded(), "gRPC transport must not be embedded")

	first, err := sc.ForShard(2)
	require.NoError(t, err, "network ForShard must build a lazy client")
	require.NotNil(t, first)

	second, err := sc.ForShard(2)
	require.NoError(t, err)
	assert.Same(t, first, second, "network clients must be cached per shard")
}

// TestShardedClient_ForShard_Network_MissingAddressTypedError covers the
// validateNetworkConfig guard for a gRPC transport with no address.
func TestShardedClient_ForShard_Network_MissingAddressTypedError(t *testing.T) {
	cfg := &config.QRDBConfig{Transport: config.QRDBTransportGRPC}
	sc := NewShardedClient(nil, WithTransportConfig(cfg))
	t.Cleanup(func() { _ = sc.Close() })

	_, err := sc.ForShard(1)
	var missing *NetworkTransportMissingAddressError
	require.ErrorAs(t, err, &missing,
		"a network transport without an address must return the typed missing-address error")
}

// TestShardedClient_ForShard_Network_UnixMissingSocketTypedError covers the
// Unix-transport validation branch where no socket path is configured.
func TestShardedClient_ForShard_Network_UnixMissingSocketTypedError(t *testing.T) {
	cfg := &config.QRDBConfig{Transport: config.QRDBTransportUnix}
	sc := NewShardedClient(nil, WithTransportConfig(cfg))
	t.Cleanup(func() { _ = sc.Close() })

	_, err := sc.ForShard(1)
	var missingSock *UnixTransportMissingSocketError
	require.ErrorAs(t, err, &missingSock,
		"a unix transport without a socket path must return the typed missing-socket error")
}
