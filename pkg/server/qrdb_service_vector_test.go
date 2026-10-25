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

//go:build !integration && qrdb_vector

package server

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestServiceVectorProvider_WriteRunsAsServicePrincipal pins #1727: the
// vector service's writes reach the embedded node through the provider, which
// binds each shard's client to the service principal. The node refuses the
// same write from the bare client.
func TestServiceVectorProvider_WriteRunsAsServicePrincipal(t *testing.T) {
	s, node := newPrincipalServer(t)
	req := &transport.CreateNamespaceRequest{
		Namespace:      "principal-ns",
		Dimension:      3,
		DistanceMetric: "cosine",
		IndexType:      "flat",
		ShardID:        qbshards.PlatformShardID,
	}
	_, err := node.Client().Vector().CreateNamespace(context.Background(), req)
	require.ErrorIs(t, err, qrdbsdk.ErrUnauthenticated, "an anonymous write was served; this test would prove nothing")

	vec, err := serviceVectorProvider{server: s}.VectorForShard(qbshards.PlatformShardID)
	require.NoError(t, err)
	_, err = vec.CreateNamespace(context.Background(), req)
	require.NoError(t, err)

	info, err := vec.GetNamespaceInfo(context.Background(), &transport.GetNamespaceInfoRequest{
		Namespace: "principal-ns",
		ShardID:   qbshards.PlatformShardID,
	})
	require.NoError(t, err)
	assert.Equal(t, uint32(3), info.Dimension)
}

// tenantVectorCallerRole is the role the tenant callers below hold: every
// permission the vector service's calls need, as a tenant's own token would.
// tenantVectorScopes is what a tenant's platform token carries for the
// vector verbs these tests drive.
var tenantVectorScopes = []string{
	"vector:namespace_create", "vector:namespace_list", "vector:upsert", "vector:search_similar",
}

// newStrictPrincipalServer is newPrincipalServer on a node whose cluster
// isolation floor is strict, under which qrdb refuses a namespace create made
// in the system tenant.
func newStrictPrincipalServer(t *testing.T) *Server {
	t.Helper()
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "memory"}}
	// The isolation floor is read from the tenant service, which a sealed
	// node builds.
	cfg.Seal.Enabled = true
	cfg.Seal.Strategy = "none"
	principal, err := qrdbservice.NodeOption()
	require.NoError(t, err)
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, principal)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	s := &Server{}
	s.setQRDBNode(node)
	s.qrdbClient = newNodeBackedShardedClient(node)
	// qrdb applies no isolation floor from its configuration and its SDK
	// carries no setter, so the floor is written where qrdb's tenant service
	// reads it: the system tenant's cluster isolation-policy record.
	err = qrdbservice.KV(node, node.Client().KV()).Put(context.Background(),
		"__sys:cluster/isolation_policy", []byte("strict"))
	require.NoError(t, err)
	_, err = node.Client().Vector().CreateNamespace(mustServiceContext(t, node), &transport.CreateNamespaceRequest{
		Namespace: "system-probe", Dimension: 3, DistanceMetric: "cosine", IndexType: "flat",
	})
	require.Error(t, err, "the node is not strict: a system-tenant create was served")
	return s
}

// mustServiceContext binds the node's service principal in the system tenant.
func mustServiceContext(t *testing.T, node *qrdbnode.Node) context.Context {
	t.Helper()
	ctx, err := node.ServiceContext(tenantctx.WithSystem(context.Background()))
	require.NoError(t, err)
	return ctx
}

// tenantVectorCaller returns ctx as an authenticated caller of tenantID, the
// identity qrdb's authenticator chain puts on a tenant request.
func tenantVectorCaller(t *testing.T, tenantID string, scopes ...string) context.Context {
	t.Helper()
	ctx := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{
		Subject:          "alice",
		TenantID:         tenantID,
		ScopePermissions: scopes,
	})
	return tenantctx.WithTenant(ctx, tenantID)
}

// TestServiceVector_TenantCallerLandsInItsTenant pins 7156 P1 1: a tenant's
// vector calls reach qrdb in that tenant, never as the system tenant. On a
// strict-isolation node a create made as the system tenant is refused, and a
// namespace created in acme is listed for acme and for nobody else.
func TestServiceVector_TenantCallerLandsInItsTenant(t *testing.T) {
	s := newStrictPrincipalServer(t)
	acme := tenantVectorCaller(t, "acme", tenantVectorScopes...)
	globex := tenantVectorCaller(t, "globex", tenantVectorScopes...)

	vec, err := serviceVectorProvider{server: s}.VectorForShard(qbshards.PlatformShardID)
	require.NoError(t, err)
	_, err = vec.CreateNamespace(acme, &transport.CreateNamespaceRequest{
		Namespace:      "acme-docs",
		Dimension:      3,
		DistanceMetric: "cosine",
		IndexType:      "flat",
		ShardID:        qbshards.PlatformShardID,
	})
	require.NoError(t, err, "a tenant's namespace create must reach qrdb in its tenant")

	storage := newNodeVectorStorage(s.serviceVector(s.getQRDBNode().Client().Vector()))
	require.NoError(t, storage.UpsertVector(acme, "acme-docs", &interfaces.VectorValue{
		ID:        7,
		Embedding: []float32{1, 0, 0},
	}))
	found, err := vec.SearchSimilar(acme, &transport.SearchSimilarRequest{
		Namespace:   "acme-docs",
		QueryVector: []float32{1, 0, 0},
		K:           1,
		ShardID:     qbshards.PlatformShardID,
	})
	require.NoError(t, err)
	require.Len(t, found.Results, 1)
	assert.Equal(t, uint64(7), found.Results[0].ID)

	names := func(ctx context.Context) []string {
		listed, err := storage.ListNamespaces(ctx)
		require.NoError(t, err)
		out := make([]string, 0, len(listed))
		for _, ns := range listed {
			out = append(out, ns.Name)
		}
		return out
	}
	assert.Contains(t, names(acme), "acme-docs")
	assert.NotContains(t, names(globex), "acme-docs", "acme's namespace leaked to another tenant")
}

func TestServiceVector_TenantCallerWithoutTheVerbIsRefused(t *testing.T) {
	s := newStrictPrincipalServer(t)
	vec, err := serviceVectorProvider{server: s}.VectorForShard(qbshards.PlatformShardID)
	require.NoError(t, err)
	_, err = vec.CreateNamespace(tenantVectorCaller(t, "acme", tenantVectorScopes...), &transport.CreateNamespaceRequest{
		Namespace: "acme-docs", Dimension: 3, DistanceMetric: "cosine", IndexType: "flat",
		ShardID: qbshards.PlatformShardID,
	})
	require.NoError(t, err)

	// The caller holds every scope but search: the service principal's
	// grants must not stand in for the verb the caller lacks.
	reader := tenantVectorCaller(t, "acme", "vector:namespace_list", "vector:upsert")
	_, err = vec.SearchSimilar(reader, &transport.SearchSimilarRequest{
		Namespace: "acme-docs", QueryVector: []float32{1, 0, 0}, K: 1, ShardID: qbshards.PlatformShardID,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "alice cannot search_similar", "qrdb must judge the caller, not the service principal")
}

func TestServiceVector_ServerWorkRunsAsTheSystemService(t *testing.T) {
	s := newStrictPrincipalServer(t)
	storage := newNodeVectorStorage(s.serviceVector(s.getQRDBNode().Client().Vector()))

	// The startup index load carries no caller: it is bound to the service
	// principal in the system tenant, which the strict floor still serves
	// for reads.
	_, err := storage.ListNamespaces(context.Background())
	require.NoError(t, err)
}

// newNodeVectorService builds the vector service the way startVector does,
// over s's node, writing through the service vector provider.
func newNodeVectorService(t *testing.T, s *Server) service.VectorService {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	storage := newNodeVectorStorage(s.serviceVector(s.getQRDBNode().Client().Vector()))
	svc, err := service.NewVectorService(storage, logger.NewAdapter(log),
		service.WithVectorShardID(qbshards.PlatformShardID),
		service.WithVectorIndexFactory(NewVectorIndexFactory()),
		service.WithVectorLoadContext(mustServiceContext(t, s.getQRDBNode())),
		service.WithVectorSDKProvider(serviceVectorProvider{server: s}))
	require.NoError(t, err)
	return svc
}

// An upsert needs only vector:upsert. qrdb checks the namespace and its
// dimension inside the upsert, in the caller's tenant, so the service makes
// no metadata read that would also need vector:namespace_info.
func TestVectorService_UpsertNeedsOnlyTheUpsertVerb(t *testing.T) {
	s := newStrictPrincipalServer(t)
	svc := newNodeVectorService(t, s)
	_, err := svc.CreateNamespace(tenantVectorCaller(t, "acme", "vector:namespace_create"),
		&service.CreateNamespaceRequest{Namespace: "acme-docs", Dimension: 3, DistanceMetric: "cosine", IndexType: "flat"})
	require.NoError(t, err)

	upsert := &service.UpsertVectorRequest{Namespace: "acme-docs", ID: 7, Vector: []float32{1, 0, 0}}
	_, err = svc.UpsertVector(tenantVectorCaller(t, "acme", "vector:upsert"), upsert)
	require.NoError(t, err, "an own-tenant upsert must not need vector:namespace_info")

	_, err = svc.UpsertVector(tenantVectorCaller(t, "globex", "vector:upsert"), upsert)
	var refused *qberrors.QBError
	require.ErrorAs(t, err, &refused, "another tenant's namespace must stay out of reach")
	assert.Equal(t, qberrors.ErrNotFound, refused.Code)
}

// A vector whose dimension differs from the namespace's is still refused as
// invalid input, now by qrdb's check inside the upsert.
func TestVectorService_UpsertWrongDimensionIsInvalidInput(t *testing.T) {
	s := newStrictPrincipalServer(t)
	svc := newNodeVectorService(t, s)
	_, err := svc.CreateNamespace(tenantVectorCaller(t, "acme", "vector:namespace_create"),
		&service.CreateNamespaceRequest{Namespace: "acme-docs", Dimension: 3, DistanceMetric: "cosine", IndexType: "flat"})
	require.NoError(t, err)

	_, err = svc.UpsertVector(tenantVectorCaller(t, "acme", "vector:upsert"),
		&service.UpsertVectorRequest{Namespace: "acme-docs", ID: 7, Vector: []float32{1, 0}})
	var refused *qberrors.QBError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, qberrors.ErrInvalidInput, refused.Code)
}
