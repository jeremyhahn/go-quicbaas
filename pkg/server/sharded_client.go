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
	"sync"
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ShardedClientOption configures a ShardedClient.
type ShardedClientOption func(*ShardedClient)

// WithTransportConfig sets the transport configuration for the sharded client.
func WithTransportConfig(cfg *config.QRDBConfig) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.transportCfg = cfg
	}
}

// WithStorageConfig sets the storage configuration for the sharded client.
// Retained for call-site compatibility; the storage config is no longer used
// for embedded per-shard stores (the qrdb node owns storage for all modes).
func WithStorageConfig(cfg *config.StorageConfig) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.storageCfg = cfg
	}
}

// WithVectorStorage is a no-op retained for call-site compatibility.
// The qrdb node owns vector storage; quicbaas does not manage per-shard
// vector stores directly.
func WithVectorStorage(vs interface{}, shardID uint64) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.vectorShardID = shardID
	}
}

// WithTimeSeriesStorage is a no-op retained for call-site compatibility.
// The qrdb node owns time-series storage; quicbaas does not manage per-shard
// time-series stores directly.
func WithTimeSeriesStorage(ts interface{}, shardID uint64) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.timeSeriesShardID = shardID
	}
}

// WithDatabaseService is a no-op retained for call-site compatibility.
// Database management operations are handled by the qrdb node's service graph.
func WithDatabaseService(_ interface{}) ShardedClientOption {
	return func(_ *ShardedClient) {}
}

// ShardedClient manages per-shard SDK clients. QuicBaaS routes all
// data-plane operations through the qrdb node's SDK client. For embedded
// (standalone/clustered) mode the qrdb engine runs in-process and
// nodeClient is always set; ForShard returns nodeClient for every shard ID.
// For network transports, each shard gets its own connection through the
// transport factory on first access.
//
// When nodeClient is non-nil (standalone/clustered mode), ForShard returns
// nodeClient for every shard ID. The node owns the client's lifetime;
// Close is a no-op for the node-owned path.
type ShardedClient struct {
	factory           *transportFactory
	transportCfg      *config.QRDBConfig
	storageCfg        *config.StorageConfig
	vectorShardID     uint64
	timeSeriesShardID uint64
	clients           sync.Map // map[uint64]*qrdbsdk.Client

	// nodeClient is the SDK client from the real qrdb engine node. When
	// non-nil (standalone/clustered mode), ForShard returns this client for
	// every shard ID. The node owns the client's lifetime; Close skips
	// closing it.
	nodeClient *qrdbsdk.Client
	// nodeOwned marks that nodeClient is owned by the qrdb node, not this
	// ShardedClient — Close must not call nodeClient.Close().
	nodeOwned bool

	closed atomic.Bool
}

// NewShardedClient creates a new ShardedClient. The serviceHost parameter is
// accepted for interface compatibility but is not used on the node path.
// Options configure transport selection, TLS, and storage backend.
func NewShardedClient(serviceHost interface{}, opts ...ShardedClientOption) *ShardedClient {
	sc := &ShardedClient{}

	for _, opt := range opts {
		opt(sc)
	}

	sc.factory = newTransportFactory(sc.transportCfg, serviceHost, nil)

	return sc
}

// ForShard returns an SDK client for the given shard. When nodeClient is
// set (standalone/clustered mode), the node client is returned for every
// shard ID — the qrdb engine handles internal shard routing. For network
// transports, clients are created via the transport factory on first access
// and cached for subsequent calls.
// Returns ShardedClientClosedError if Close has been called.
func (sc *ShardedClient) ForShard(shardID uint64) (*qrdbsdk.Client, error) {
	if sc.closed.Load() {
		return nil, &ShardedClientClosedError{}
	}

	// Node mode: all shard IDs resolve to the real qrdb engine client.
	// The engine handles internal routing; quicbaas does not need per-shard
	// stores for this path.
	if sc.nodeClient != nil {
		return sc.nodeClient, nil
	}

	// Check for a cached client first.
	if existing, ok := sc.clients.Load(shardID); ok {
		if cached, isClient := existing.(*qrdbsdk.Client); isClient {
			return cached, nil
		}
	}

	// Create a network client via the transport factory.
	client, err := sc.factory.createNetworkClient()
	if err != nil {
		return nil, err
	}

	// Store the client for future use. If another goroutine stored one
	// first, use theirs and close ours.
	actual, loaded := sc.clients.LoadOrStore(shardID, client)
	if winner, isClient := actual.(*qrdbsdk.Client); loaded && isClient {
		// Another goroutine created a client for this shard first.
		_ = client.Close()
		return winner, nil
	}

	return client, nil
}

// Close closes all cached SDK clients, then marks the ShardedClient as
// closed. Close is idempotent; subsequent calls return nil. When the client
// is node-owned (standalone/clustered mode), Close is a no-op — the node
// manages the client's lifetime.
func (sc *ShardedClient) Close() error {
	if sc.closed.Swap(true) {
		return nil
	}

	// Node-owned path: the qrdb node closes its own client during node.Close().
	if sc.nodeOwned {
		return nil
	}

	// Close SDK clients (they reference the transports).
	sc.clients.Range(func(key, value any) bool {
		if client, ok := value.(*qrdbsdk.Client); ok {
			_ = client.Close()
		}
		sc.clients.Delete(key)
		return true
	})

	return nil
}

// IsEmbedded returns true if the transport configuration indicates
// embedded (in-process) mode.
func (sc *ShardedClient) IsEmbedded() bool {
	return sc.factory.isEmbedded()
}
