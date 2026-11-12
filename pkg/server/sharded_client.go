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

package server

import (
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// ShardedClient hands out the SDK client of the embedded qrdb node. go-qrdb
// under go-quicbaas is embedded only: the qrdb engine runs in-process, handles
// shard routing itself, and ForShard returns the node client for every shard
// ID. The node (or the sealed-window phase) owns the client's lifetime, so
// Close only marks the ShardedClient closed.
type ShardedClient struct {
	// nodeClient is the SDK client of the embedded qrdb node.
	nodeClient *qrdbsdk.Client

	closed atomic.Bool
}

// ForShard returns the embedded node's SDK client for any shard ID. It
// returns ShardedClientClosedError after Close, and ShardedClientNoNodeError
// when no embedded node client is attached.
func (sc *ShardedClient) ForShard(shardID uint64) (*qrdbsdk.Client, error) {
	if sc.closed.Load() {
		return nil, &ShardedClientClosedError{}
	}
	if sc.nodeClient == nil {
		return nil, &ShardedClientNoNodeError{ShardID: shardID}
	}
	return sc.nodeClient, nil
}

// Close marks the ShardedClient closed. It never closes the node client,
// whose owner closes it. Close is idempotent.
func (sc *ShardedClient) Close() error {
	sc.closed.Store(true)
	return nil
}
