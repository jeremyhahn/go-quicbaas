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

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/metadata"
)

// qrdbBearerTransport wraps the qrdb sub-client's gRPC or Unix transport and
// adds the platform bearer as outgoing authorization metadata on every call.
// go-qrdb's transports take no auth-token option, so this is how the
// cluster and membership clients, which go-qrdb builds over a transport's
// Admin, KV, MVCC, Watch and Lease surfaces, carry WithAuthToken.
//
// It implements only those five surfaces. Any other capability is absent,
// and go-qrdb's capability helper panics on it by contract; no accessor that
// uses this decorator reaches one.
type qrdbBearerTransport struct {
	inner  qrdbtransport.Transport
	bearer string
}

var (
	_ qrdbtransport.Transport   = (*qrdbBearerTransport)(nil)
	_ qrdbtransport.AdminClient = (*qrdbBearerTransport)(nil)
	_ qrdbtransport.KVClient    = (*qrdbBearerTransport)(nil)
	_ qrdbtransport.MVCCClient  = (*qrdbBearerTransport)(nil)
	_ qrdbtransport.WatchClient = (*qrdbBearerTransport)(nil)
	_ qrdbtransport.LeaseClient = (*qrdbBearerTransport)(nil)
)

func (b *qrdbBearerTransport) ctx(ctx context.Context) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer)
}

func (b *qrdbBearerTransport) admin() qrdbtransport.AdminClient {
	return b.inner.(qrdbtransport.AdminClient)
}

func (b *qrdbBearerTransport) kv() qrdbtransport.KVClient {
	return b.inner.(qrdbtransport.KVClient)
}

func (b *qrdbBearerTransport) mvcc() qrdbtransport.MVCCClient {
	return b.inner.(qrdbtransport.MVCCClient)
}

func (b *qrdbBearerTransport) watch() qrdbtransport.WatchClient {
	return b.inner.(qrdbtransport.WatchClient)
}

func (b *qrdbBearerTransport) lease() qrdbtransport.LeaseClient {
	return b.inner.(qrdbtransport.LeaseClient)
}

// Transport.

// Connect forwards to the inner transport.
func (b *qrdbBearerTransport) Connect(ctx context.Context) error { return b.inner.Connect(b.ctx(ctx)) }

// Close is a no-op: the main qrdb client owns and closes the inner transport.
func (b *qrdbBearerTransport) Close() error { return nil }

// Healthy forwards to the inner transport.
func (b *qrdbBearerTransport) Healthy(ctx context.Context) bool { return b.inner.Healthy(b.ctx(ctx)) }

// Request forwards to the inner transport with the bearer attached.
func (b *qrdbBearerTransport) Request(ctx context.Context, method string, req, resp any) error {
	return b.inner.Request(b.ctx(ctx), method, req, resp)
}

// AdminClient.

func (b *qrdbBearerTransport) Health(ctx context.Context) (*qrdbtransport.HealthResponse, error) {
	return b.admin().Health(b.ctx(ctx))
}

func (b *qrdbBearerTransport) Status(ctx context.Context) (*qrdbtransport.StatusResponse, error) {
	return b.admin().Status(b.ctx(ctx))
}

func (b *qrdbBearerTransport) Peers(ctx context.Context) (*qrdbtransport.PeersResponse, error) {
	return b.admin().Peers(b.ctx(ctx))
}

func (b *qrdbBearerTransport) AddMember(ctx context.Context, shardID, replicaID uint64, raftAddr, identity string) error {
	return b.admin().AddMember(b.ctx(ctx), shardID, replicaID, raftAddr, identity)
}

func (b *qrdbBearerTransport) RemoveMember(ctx context.Context, shardID, replicaID uint64) error {
	return b.admin().RemoveMember(b.ctx(ctx), shardID, replicaID)
}

func (b *qrdbBearerTransport) RequestSnapshot(ctx context.Context, shardID uint64) error {
	return b.admin().RequestSnapshot(b.ctx(ctx), shardID)
}

func (b *qrdbBearerTransport) Drain(ctx context.Context) error {
	return b.admin().Drain(b.ctx(ctx))
}

func (b *qrdbBearerTransport) Capabilities(ctx context.Context) (*qrdbtransport.CapabilitiesResponse, error) {
	return b.admin().Capabilities(b.ctx(ctx))
}

func (b *qrdbBearerTransport) StartShard(ctx context.Context, req *qrdbtransport.StartShardRequest) error {
	return b.admin().StartShard(b.ctx(ctx), req)
}

func (b *qrdbBearerTransport) StopShard(ctx context.Context, shardID uint64) error {
	return b.admin().StopShard(b.ctx(ctx), shardID)
}

func (b *qrdbBearerTransport) GetShardState(ctx context.Context, shardID uint64) (*qrdbtransport.ShardStateResponse, error) {
	return b.admin().GetShardState(b.ctx(ctx), shardID)
}

func (b *qrdbBearerTransport) GetTopology(ctx context.Context) (*qrdbtransport.TopologyResponse, error) {
	return b.admin().GetTopology(b.ctx(ctx))
}

// KVClient.

func (b *qrdbBearerTransport) Put(ctx context.Context, key string, value []byte, opts ...qrdbtransport.WriteOption) error {
	return b.kv().Put(b.ctx(ctx), key, value, opts...)
}

func (b *qrdbBearerTransport) Get(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) ([]byte, error) {
	return b.kv().Get(b.ctx(ctx), key, opts...)
}

func (b *qrdbBearerTransport) Delete(ctx context.Context, key string, opts ...qrdbtransport.WriteOption) error {
	return b.kv().Delete(b.ctx(ctx), key, opts...)
}

func (b *qrdbBearerTransport) List(ctx context.Context, prefix string, opts ...qrdbtransport.ReadOption) ([]string, error) {
	return b.kv().List(b.ctx(ctx), prefix, opts...)
}

func (b *qrdbBearerTransport) Scan(ctx context.Context, prefix string, opts ...qrdbtransport.ReadOption) (map[string][]byte, error) {
	return b.kv().Scan(b.ctx(ctx), prefix, opts...)
}

func (b *qrdbBearerTransport) Exists(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) (bool, error) {
	return b.kv().Exists(b.ctx(ctx), key, opts...)
}

// MVCCClient.

func (b *qrdbBearerTransport) PutWithLease(ctx context.Context, key string, value []byte, leaseID int64, opts ...qrdbtransport.WriteOption) (*qrdbtransport.MVCCKeyValue, error) {
	return b.mvcc().PutWithLease(b.ctx(ctx), key, value, leaseID, opts...)
}

func (b *qrdbBearerTransport) GetRevision(ctx context.Context, key string, opts ...qrdbtransport.ReadOption) (*qrdbtransport.MVCCKeyValue, error) {
	return b.mvcc().GetRevision(b.ctx(ctx), key, opts...)
}

func (b *qrdbBearerTransport) GetAtRevision(ctx context.Context, key string, revision int64, opts ...qrdbtransport.ReadOption) (*qrdbtransport.MVCCKeyValue, error) {
	return b.mvcc().GetAtRevision(b.ctx(ctx), key, revision, opts...)
}

func (b *qrdbBearerTransport) Range(ctx context.Context, start, end []byte, rangeOpts qrdbtransport.RangeOptions, opts ...qrdbtransport.ReadOption) (*qrdbtransport.RangeResponse, error) {
	return b.mvcc().Range(b.ctx(ctx), start, end, rangeOpts, opts...)
}

func (b *qrdbBearerTransport) Compact(ctx context.Context, revision int64) error {
	return b.mvcc().Compact(b.ctx(ctx), revision)
}

func (b *qrdbBearerTransport) CurrentRevision(ctx context.Context) (int64, error) {
	return b.mvcc().CurrentRevision(b.ctx(ctx))
}

func (b *qrdbBearerTransport) RestoreToRevision(ctx context.Context, req *qrdbtransport.RestoreRevisionRequest) (*qrdbtransport.RestoreRevisionResult, error) {
	return b.mvcc().RestoreToRevision(b.ctx(ctx), req)
}

// WatchClient.

func (b *qrdbBearerTransport) Watch(ctx context.Context, key string, opts qrdbtransport.WatchOptions) (<-chan qrdbtransport.WatchResponse, error) {
	return b.watch().Watch(b.ctx(ctx), key, opts)
}

func (b *qrdbBearerTransport) WatchPrefix(ctx context.Context, prefix string, opts qrdbtransport.WatchOptions) (<-chan qrdbtransport.WatchResponse, error) {
	return b.watch().WatchPrefix(b.ctx(ctx), prefix, opts)
}

// LeaseClient.

func (b *qrdbBearerTransport) LeaseGrant(ctx context.Context, ttl int64) (*qrdbtransport.LeaseInfo, error) {
	return b.lease().LeaseGrant(b.ctx(ctx), ttl)
}

func (b *qrdbBearerTransport) LeaseRevoke(ctx context.Context, leaseID int64) error {
	return b.lease().LeaseRevoke(b.ctx(ctx), leaseID)
}

func (b *qrdbBearerTransport) LeaseKeepAlive(ctx context.Context, leaseID int64) (*qrdbtransport.LeaseInfo, error) {
	return b.lease().LeaseKeepAlive(b.ctx(ctx), leaseID)
}

func (b *qrdbBearerTransport) LeaseTimeToLive(ctx context.Context, leaseID int64) (*qrdbtransport.LeaseInfo, error) {
	return b.lease().LeaseTimeToLive(b.ctx(ctx), leaseID)
}

func (b *qrdbBearerTransport) LeaseList(ctx context.Context) ([]*qrdbtransport.LeaseInfo, error) {
	return b.lease().LeaseList(b.ctx(ctx))
}
