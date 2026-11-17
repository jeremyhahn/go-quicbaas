//go:build !integration

package server

import (
	"context"
	stderrors "errors"
	"path/filepath"
	"testing"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// A put through the kv service for a barrier-isolated tenant on a composed
// node lands in that tenant's registry-scoped KV, not in the node's shared
// client keyspace (#2512).
func TestKVServiceRegistry_BarrierTenantPutIsScoped(t *testing.T) {
	// Barrier isolation needs the seal subsystem; the none strategy boots
	// the node unsealed.
	cfg := qrdbsdk.DefaultDaemonConfig()
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "pebble"}}
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "none"
	cfg.Storage.VolumePools = []qrdbsdk.DaemonVolumePoolConfig{
		{Name: "default", Paths: []string{filepath.Join(cfg.Raft.DataDir, defaultVolumePoolDirName)}, Strategy: "round_robin"},
	}
	cfg.Protocols.REST = false
	cfg.Protocols.GRPC = false
	cfg.Protocols.QUIC = false
	cfg.Protocols.Unix = false
	cfg.Protocols.MCP = false
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, qrdbnode.WithTenantIsolation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	require.False(t, node.IsSealed(), "a none-strategy node boots unsealed")

	const tenantID = "acme"
	svcs := node.Services()
	require.NotNil(t, svcs)
	require.NotNil(t, svcs.Tenant)
	_, err = svcs.Tenant.CreateTenant(context.Background(), tenantID, "Acme", qrdbsdk.TenantIsolationBarrier)
	require.NoError(t, err)

	sharded := newNodeBackedShardedClient(node)
	provider := sharded.ServiceRegistryProvider()
	require.NotNil(t, provider, "a node-backed client always carries a registry provider")
	reg := provider()
	require.NotNil(t, reg, "an unsealed node serves a registry")

	svc, err := service.NewServiceBuilder().
		WithLogger(logger.NewAdapter(buildTestLogger(t))).
		WithConfig(&service.ServiceConfig{NodeID: 1, Version: "1.0.0", Shards: []uint64{1}}).
		WithQRDBClient(sharded).
		WithQRDBServiceRegistry(provider).
		Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = svc.Close() })

	ctx := tenantctx.WithTenant(context.Background(), tenantID)
	data := []byte(`{"name":"scoped"}`)
	_, err = svc.Put(ctx, &service.PutRequest{EntityType: "User", ID: 42, Data: data})
	require.NoError(t, err)

	// The kv service's tenant key for User 42 (pkg/service keyspace).
	const key = "__t:" + tenantID + ":User:42"

	scoped, err := reg.KVService(qrdbsdk.NormalizeTenantID(tenantID), "")
	require.NoError(t, err)
	got, err := scoped.Get(context.Background(), key)
	require.NoError(t, err, "the put must be readable through the tenant's scoped KV")
	require.Equal(t, data, got)

	_, err = node.Client().KV().Get(context.Background(), key)
	require.Error(t, err, "the put must not land in the node client's shared keyspace")
	var transportKNF *qrdbtransport.KeyNotFoundError
	var serviceKNF *qrdbservice.KeyNotFoundError
	require.True(t, stderrors.As(err, &transportKNF) || stderrors.As(err, &serviceKNF),
		"expected key-not-found on the shared client, got %T: %v", err, err)

	got2, err := svc.Get(ctx, &service.GetRequest{EntityType: "User", ID: 42})
	require.NoError(t, err)
	require.True(t, got2.Found, "the kv service reads its own put back through the registry")
}
