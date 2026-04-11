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

	"github.com/jeremyhahn/go-qrdb/pkg/ceremony"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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
// This determines which backend (memory, file, pebble) is used for embedded
// mode per-shard stores.
func WithStorageConfig(cfg *config.StorageConfig) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.storageCfg = cfg
	}
}

// WithVectorStorage sets the vector storage backend for the sharded client.
// When provided, the embedded transport for the vector shard will delegate
// vector write operations to this storage so that writes are visible to
// the vectorService read path, eliminating the write-read storage mismatch.
func WithVectorStorage(vs interfaces.VectorStorage, shardID uint64) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.vectorStorage = vs
		sc.vectorShardID = shardID
	}
}

// WithTimeSeriesStorage sets the time series storage backend for the sharded
// client. When provided, the embedded transport for the timeseries shard will
// delegate timeseries operations to this storage so that writes are visible
// to the timeSeriesService read path, eliminating the write-read storage mismatch.
func WithTimeSeriesStorage(ts interfaces.TimeSeriesStorage, shardID uint64) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.timeSeriesStorage = ts
		sc.timeSeriesShardID = shardID
	}
}

// WithDatabaseService sets the database service for the sharded client.
// When provided, the embedded transport will delegate database management
// operations to this service instead of returning BaasTransportNotSupportedError.
func WithDatabaseService(dbSvc service.DatabaseService) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.dbService = dbSvc
	}
}

// WithLockService sets the lock service for the sharded client.
// When provided, the embedded transport will delegate lock operations
// to this service instead of returning BaasTransportNotSupportedError.
func WithLockService(lockSvc BaasLockService) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.lockService = lockSvc
	}
}

// WithCeremonyService sets the ceremony lifecycle service for the sharded
// client. When provided, the embedded transport will delegate ceremony
// operations to this service instead of returning BaasTransportNotSupportedError.
func WithCeremonyService(svc BasCeremonyService) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.ceremonySvc = svc
	}
}

// WithCeremonyCA sets the ceremony CA for the sharded client. The CA is
// forwarded to CeremonyInitialize calls. Both a ceremony service and a CA
// must be set for CeremonyInitialize to succeed.
func WithCeremonyCA(ca ceremony.CeremonyCA) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.ceremonyCA = ca
	}
}

// WithCredentialService sets the credential management service for the sharded
// client. When provided, the embedded transport will delegate credential
// operations to this service instead of returning BaasTransportNotSupportedError.
func WithCredentialService(svc BaasCredentialService) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.credentialSvc = svc
	}
}

// WithCustodianGroupService sets the custodian group service for the sharded
// client. When provided, the embedded transport will delegate custodian group
// operations to this service instead of returning BaasTransportNotSupportedError.
func WithCustodianGroupService(svc BaasCustodianGroupService) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.custodianGroupSvc = svc
	}
}

// WithShareService sets the Shamir share service for the sharded client.
// When provided, the embedded transport will delegate share operations to this
// service instead of returning BaasTransportNotSupportedError.
func WithShareService(svc BaasShareService) ShardedClientOption {
	return func(sc *ShardedClient) {
		sc.shareSvc = svc
	}
}

// ShardedClient manages per-shard SDK clients. QuicBaaS operates as a
// BaaS client and connects through the qrdb SDK. For embedded transport,
// each shard gets an SDK client backed by a per-shard baasTransport
// (memory, file, or PebbleDB). For network transports, each shard gets
// its own connection through the transport factory.
type ShardedClient struct {
	factory           *transportFactory
	transportCfg      *config.QRDBConfig
	storageCfg        *config.StorageConfig
	vectorStorage     interfaces.VectorStorage
	vectorShardID     uint64
	timeSeriesStorage interfaces.TimeSeriesStorage
	timeSeriesShardID uint64
	dbService         service.DatabaseService
	lockService       BaasLockService
	ceremonySvc       BasCeremonyService
	ceremonyCA        ceremony.CeremonyCA
	credentialSvc     BaasCredentialService
	custodianGroupSvc BaasCustodianGroupService
	shareSvc          BaasShareService
	transportStore    *baasTransportStore // per-shard stores for embedded mode
	clients           sync.Map            // map[uint64]*qrdbsdk.Client
	closed            atomic.Bool
}

// NewShardedClient creates a new ShardedClient. The serviceHost parameter
// provides access to the embedded service layer and may be nil for network
// transports. Options configure transport selection, TLS, and storage backend.
func NewShardedClient(serviceHost interface{}, opts ...ShardedClientOption) *ShardedClient {
	sc := &ShardedClient{}

	for _, opt := range opts {
		opt(sc)
	}

	sc.factory = newTransportFactory(sc.transportCfg, serviceHost, nil)

	// Initialize per-shard transport store for embedded mode.
	if sc.factory.isEmbedded() {
		sc.transportStore = newBaasTransportStore(sc.storageCfg)
		// Inject vector storage into the transport store so that the
		// vector shard transport delegates writes to the real storage.
		if sc.vectorStorage != nil {
			sc.transportStore.vectorStorage = sc.vectorStorage
			sc.transportStore.vectorShardID = sc.vectorShardID
		}
		// Inject timeseries storage into the transport store so that the
		// timeseries shard transport delegates operations to the real storage.
		if sc.timeSeriesStorage != nil {
			sc.transportStore.timeSeriesStorage = sc.timeSeriesStorage
			sc.transportStore.timeSeriesShardID = sc.timeSeriesShardID
		}
		if sc.dbService != nil {
			sc.transportStore.dbService = sc.dbService
		}
		if sc.lockService != nil {
			sc.transportStore.lockService = sc.lockService
		}
		if sc.ceremonySvc != nil {
			sc.transportStore.ceremonySvc = sc.ceremonySvc
		}
		if sc.ceremonyCA != nil {
			sc.transportStore.ceremonyCA = sc.ceremonyCA
		}
		if sc.credentialSvc != nil {
			sc.transportStore.credentialSvc = sc.credentialSvc
		}
		if sc.custodianGroupSvc != nil {
			sc.transportStore.custodianGroupSvc = sc.custodianGroupSvc
		}
		if sc.shareSvc != nil {
			sc.transportStore.shareSvc = sc.shareSvc
		}
	}

	return sc
}

// ForShard returns an SDK client for the given shard. For embedded
// transport, clients are backed by per-shard in-memory stores and
// created on first access. For network transports, clients are created
// via the transport factory on first access. All clients are cached for
// subsequent calls.
// Returns ShardedClientClosedError if Close has been called.
func (sc *ShardedClient) ForShard(shardID uint64) (*qrdbsdk.Client, error) {
	if sc.closed.Load() {
		return nil, &ShardedClientClosedError{}
	}

	// Check for a cached client first.
	if existing, ok := sc.clients.Load(shardID); ok {
		return existing.(*qrdbsdk.Client), nil //nolint:errcheck // type guaranteed by map contract
	}

	// For embedded transport, create an SDK client backed by the
	// per-shard baasTransport (in-memory KV store).
	if sc.IsEmbedded() {
		return sc.createEmbeddedClient(shardID)
	}

	// Create a network client via the transport factory.
	client, err := sc.factory.createNetworkClient()
	if err != nil {
		return nil, err
	}

	// Store the client for future use. If another goroutine stored one
	// first, use theirs and close ours.
	actual, loaded := sc.clients.LoadOrStore(shardID, client)
	if loaded {
		// Another goroutine created a client for this shard first.
		_ = client.Close()
		return actual.(*qrdbsdk.Client), nil //nolint:errcheck // type guaranteed by map contract
	}

	return client, nil
}

// createEmbeddedClient creates an SDK client backed by a per-shard
// baasTransport for the given shard. The transport is backed by the
// configured storage backend (memory, file, or PebbleDB). The client
// is cached in the clients map. Thread-safe via LoadOrStore.
func (sc *ShardedClient) createEmbeddedClient(shardID uint64) (*qrdbsdk.Client, error) {
	t, err := sc.transportStore.transportForShard(shardID)
	if err != nil {
		return nil, &TransportCreationError{Transport: "embedded", Cause: err}
	}
	client, err := qrdbsdk.NewClient(t)
	if err != nil {
		return nil, &TransportCreationError{Transport: "embedded", Cause: err}
	}

	// Store the client for future use. If another goroutine stored one
	// first, use theirs and close ours.
	actual, loaded := sc.clients.LoadOrStore(shardID, client)
	if loaded {
		_ = client.Close()
		return actual.(*qrdbsdk.Client), nil //nolint:errcheck // type guaranteed by map contract
	}

	return client, nil
}

// Close closes all cached SDK clients and per-shard stores, then marks
// the ShardedClient as closed. Close is idempotent; subsequent calls
// return nil.
func (sc *ShardedClient) Close() error {
	if sc.closed.Swap(true) {
		return nil
	}

	// Close SDK clients first (they reference the transports).
	sc.clients.Range(func(key, value any) bool {
		client := value.(*qrdbsdk.Client) //nolint:errcheck // type guaranteed by map contract
		_ = client.Close()
		sc.clients.Delete(key)
		return true
	})

	// Close per-shard stores (PebbleDB, File) to flush and release resources.
	if sc.transportStore != nil {
		return sc.transportStore.Close()
	}

	return nil
}

// IsEmbedded returns true if the transport configuration indicates
// embedded (in-process) mode where no real Raft consensus is running.
func (sc *ShardedClient) IsEmbedded() bool {
	return sc.factory.isEmbedded()
}
