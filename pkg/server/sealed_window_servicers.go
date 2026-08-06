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

package server

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Sealed-window lazy KV / Admin servicer proxies
//
// The sealed-window boot phase builds an in-process qrdb SDK client over the
// embedded transport so every quicbaas transport serves the node's OWN
// barrier / ceremony / credential servicers while the node is sealed (see
// newSealedWindowClient in sealed_window.go). The qrdb embedded transport
// requires non-nil KV and Admin servicers at construction time, but a sealed
// node has no data plane yet: node.KVService() / node.AdminService() return
// nil until Unseal completes the boot.
//
// These proxies resolve the node servicer PER CALL — the exact late-binding
// pattern already used for the user servicers (qrdb_user_servicers.go). While
// the node is sealed every KV / Admin call fails closed with the typed
// SealedNodeUnavailableError; after Unseal the same proxy transparently
// reaches the live servicer. They are late-binding proxies over the real
// node, not stubs.
// =============================================================================

// SealedNodeUnavailableError is returned by the sealed-window KV / Admin
// proxies when the embedded qrdb node's data plane is not yet available
// (the node is sealed and Unseal has not completed the boot).
type SealedNodeUnavailableError struct {
	Service string
}

// Error implements the error interface.
func (e *SealedNodeUnavailableError) Error() string {
	return "sealed window: qrdb node " + e.Service + " service unavailable until the barrier is unsealed"
}

// sealedKVServicerProxy is a lazy qrdbsdk.KVServicer that resolves the
// embedded node's KV servicer per call.
type sealedKVServicerProxy struct {
	server *Server
}

// newSealedKVServicerProxy returns the lazy KV servicer proxy for the
// sealed-window embedded transport.
func newSealedKVServicerProxy(s *Server) qrdbsdk.KVServicer {
	return &sealedKVServicerProxy{server: s}
}

// resolve returns the live node KV servicer or a typed sealed error.
func (p *sealedKVServicerProxy) resolve() (qrdbsdk.KVServicer, error) {
	if svc := p.server.GetQRDBKVServicer(); svc != nil {
		return svc, nil
	}
	return nil, &SealedNodeUnavailableError{Service: "kv"}
}

func (p *sealedKVServicerProxy) Put(ctx context.Context, key string, value []byte) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.Put(ctx, key, value)
}

func (p *sealedKVServicerProxy) Get(ctx context.Context, key string) ([]byte, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.Get(ctx, key)
}

func (p *sealedKVServicerProxy) Delete(ctx context.Context, key string) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.Delete(ctx, key)
}

func (p *sealedKVServicerProxy) List(ctx context.Context, prefix string) ([]string, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.List(ctx, prefix)
}

func (p *sealedKVServicerProxy) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.Scan(ctx, prefix)
}

func (p *sealedKVServicerProxy) GetStale(ctx context.Context, key string) ([]byte, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.GetStale(ctx, key)
}

func (p *sealedKVServicerProxy) ListStale(ctx context.Context, prefix string) ([]string, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.ListStale(ctx, prefix)
}

func (p *sealedKVServicerProxy) ScanStale(ctx context.Context, prefix string) (map[string][]byte, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.ScanStale(ctx, prefix)
}

func (p *sealedKVServicerProxy) PutWithMode(ctx context.Context, key string, value []byte, mode qrdbsdk.WriteMode) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.PutWithMode(ctx, key, value, mode)
}

func (p *sealedKVServicerProxy) DeleteWithMode(ctx context.Context, key string, mode qrdbsdk.WriteMode) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.DeleteWithMode(ctx, key, mode)
}

func (p *sealedKVServicerProxy) GetWithConsistency(ctx context.Context, key string, opts qrdbsdk.ReadOptions) ([]byte, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.GetWithConsistency(ctx, key, opts)
}

func (p *sealedKVServicerProxy) ListWithConsistency(ctx context.Context, prefix string, opts qrdbsdk.ReadOptions) ([]string, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.ListWithConsistency(ctx, prefix, opts)
}

func (p *sealedKVServicerProxy) ScanWithConsistency(ctx context.Context, prefix string, opts qrdbsdk.ReadOptions) (map[string][]byte, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.ScanWithConsistency(ctx, prefix, opts)
}

func (p *sealedKVServicerProxy) ExistsWithConsistency(ctx context.Context, key string, opts qrdbsdk.ReadOptions) (bool, error) {
	svc, err := p.resolve()
	if err != nil {
		return false, err
	}
	return svc.ExistsWithConsistency(ctx, key, opts)
}

func (p *sealedKVServicerProxy) PutEntity(ctx context.Context, req *qrdbsdk.PutEntityRequest) (*qrdbsdk.PutEntityResponse, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.PutEntity(ctx, req)
}

func (p *sealedKVServicerProxy) GetEntity(ctx context.Context, req *qrdbsdk.GetEntityRequest) (*qrdbsdk.GetEntityResponse, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.GetEntity(ctx, req)
}

func (p *sealedKVServicerProxy) DeleteEntity(ctx context.Context, req *qrdbsdk.DeleteEntityRequest) (*qrdbsdk.DeleteEntityResponse, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.DeleteEntity(ctx, req)
}

func (p *sealedKVServicerProxy) ListEntities(ctx context.Context, req *qrdbsdk.ListEntitiesRequest) (*qrdbsdk.ListEntitiesResponse, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.ListEntities(ctx, req)
}

// sealedAdminServicerProxy is a lazy qrdbsdk.AdminServicer that resolves the
// embedded node's Admin servicer per call.
type sealedAdminServicerProxy struct {
	server *Server
}

// newSealedAdminServicerProxy returns the lazy Admin servicer proxy for the
// sealed-window embedded transport.
func newSealedAdminServicerProxy(s *Server) qrdbsdk.AdminServicer {
	return &sealedAdminServicerProxy{server: s}
}

// resolve returns the live node Admin servicer or a typed sealed error.
func (p *sealedAdminServicerProxy) resolve() (qrdbsdk.AdminServicer, error) {
	if svc := p.server.GetQRDBAdminServicer(); svc != nil {
		return svc, nil
	}
	return nil, &SealedNodeUnavailableError{Service: "admin"}
}

// Health reports a sealed liveness verdict while the node's admin servicer
// is unavailable instead of failing: the qrdb SDK client probes Health at
// connect time and would otherwise refuse to connect during the sealed
// window, making the unseal flow unreachable. This mirrors qrdb's own
// always-servable sealed-window health semantics ("sealed" is a documented
// HealthStatus verdict).
func (p *sealedAdminServicerProxy) Health(ctx context.Context) (*qrdbsdk.HealthStatus, error) {
	svc, err := p.resolve()
	if err != nil {
		return &qrdbsdk.HealthStatus{Status: "sealed"}, nil
	}
	return svc.Health(ctx)
}

func (p *sealedAdminServicerProxy) Status(ctx context.Context) (*qrdbsdk.StatusInfo, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.Status(ctx)
}

func (p *sealedAdminServicerProxy) Peers(ctx context.Context) (*qrdbsdk.PeersInfo, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.Peers(ctx)
}

func (p *sealedAdminServicerProxy) AddMember(ctx context.Context, shardID, replicaID uint64, raftAddr string) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.AddMember(ctx, shardID, replicaID, raftAddr)
}

func (p *sealedAdminServicerProxy) RemoveMember(ctx context.Context, shardID, replicaID uint64) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.RemoveMember(ctx, shardID, replicaID)
}

func (p *sealedAdminServicerProxy) RequestSnapshot(ctx context.Context, shardID uint64) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.RequestSnapshot(ctx, shardID)
}

func (p *sealedAdminServicerProxy) Drain(ctx context.Context) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.Drain(ctx)
}

func (p *sealedAdminServicerProxy) Capabilities(ctx context.Context) (*qrdbsdk.Capabilities, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.Capabilities(ctx)
}

func (p *sealedAdminServicerProxy) StartShard(ctx context.Context, req *qrdbsdk.StartShardRequest) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.StartShard(ctx, req)
}

func (p *sealedAdminServicerProxy) StopShard(ctx context.Context, shardID uint64) error {
	svc, err := p.resolve()
	if err != nil {
		return err
	}
	return svc.StopShard(ctx, shardID)
}

func (p *sealedAdminServicerProxy) GetShardState(ctx context.Context, shardID uint64) (*qrdbsdk.ShardStateResponse, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.GetShardState(ctx, shardID)
}

func (p *sealedAdminServicerProxy) GetTopology(ctx context.Context) (*qrdbsdk.TopologyResponse, error) {
	svc, err := p.resolve()
	if err != nil {
		return nil, err
	}
	return svc.GetTopology(ctx)
}
