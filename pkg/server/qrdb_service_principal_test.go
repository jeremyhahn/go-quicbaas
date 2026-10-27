//go:build !integration

package server

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
	testqrdbnode "github.com/jeremyhahn/go-quicbaas/pkg/testutil/qrdbnode"
)

// newPrincipalNode boots a standalone in-memory node that names
// go-quicbaas's production service principal, as the daemon's node does. The
// node authorizes every embedded call, so only calls bound to the principal
// reach its store.
func newPrincipalNode(t *testing.T, nodeOpts ...qrdbnode.NodeOption) *qrdbnode.Node {
	t.Helper()
	return startPrincipalNode(t, principalNodeConfig(t), nil, nodeOpts...)
}

// principalNodeConfig is the in-memory node configuration every principal
// test node starts from.
func principalNodeConfig(t *testing.T) *qrdbsdk.DaemonConfig {
	t.Helper()
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir() // vector storage requires a data dir
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Backend: "memory"},
	}
	return cfg
}

// startPrincipalNode boots cfg as a standalone node running as the
// production service principal.
func startPrincipalNode(t *testing.T, cfg *qrdbsdk.DaemonConfig, serverOpts []qrdbsdk.ServerOption, nodeOpts ...qrdbnode.NodeOption) *qrdbnode.Node {
	t.Helper()
	principal, err := qrdbservice.NodeOption()
	require.NoError(t, err)
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, serverOpts, append(nodeOpts, principal)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	return node
}

func newPrincipalServer(t *testing.T) (*Server, *qrdbnode.Node) {
	t.Helper()
	node := newPrincipalNode(t)
	return serverOverNode(node), node
}

// serverOverNode is a Server whose embedded qrdb node is node.
func serverOverNode(node *qrdbnode.Node) *Server {
	s := &Server{}
	s.setQRDBNode(node)
	s.qrdbClient = newNodeBackedShardedClient(node)
	return s
}

// The store GetKVStore hands to internal components runs as the service
// principal, keeps the guarded verbs, and so works on a node that authorizes
// every embedded call.
func TestGetKVStore_EmbeddedNodeRunsAsServicePrincipal(t *testing.T) {
	s, _ := newPrincipalServer(t)
	store := s.GetKVStore(1)
	ctx := context.Background()

	require.NoError(t, store.Put(ctx, "principal-k", []byte("v1")))
	got, err := store.Get(ctx, "principal-k")
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))

	kv, ok := store.(swapper)
	require.True(t, ok, "%T lost the guarded verbs", store)
	swapped, err := kv.CompareAndSwap(ctx, "principal-k", []byte("v1"), []byte("v2"))
	require.NoError(t, err)
	assert.True(t, swapped)
}

// A workflow saved through the server's persistor is rehydrated at the next
// boot, on a node that authorizes every embedded call.
func TestWorkflowPersistor_RehydratesAsServicePrincipal(t *testing.T) {
	s, node := newPrincipalServer(t)
	ctx := context.Background()
	def := &orchestration.WorkflowDefinition{TenantID: "acme", Name: "wf-boot"}
	require.NoError(t, s.workflowPersistor(node.Client()).Save(ctx, def))

	wl := orchestration.NewWorkflowLoader()
	wl.SetPersistor(s.workflowPersistor(node.Client()))
	n, err := wl.RehydrateAll(ctx)

	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

// A call that reaches the node without the service principal bound is
// refused with go-qrdb's typed denial and never touches the store.
func TestEmbeddedNode_RefusesACallWithoutAPrincipal(t *testing.T) {
	s, node := newPrincipalServer(t)
	err := node.Client().KV().Put(context.Background(), "unbound-k", []byte("v1"))
	require.ErrorIs(t, err, qrdbsdk.ErrUnauthenticated)

	exists, err := s.GetKVStore(1).Exists(context.Background(), "unbound-k")
	require.NoError(t, err)
	assert.False(t, exists, "the refused write reached the store")
}

// TestServiceContext_BindsTheNodePrincipal: the context ServiceContext returns
// is what an embedded SDK client acting as the system dispatches with, so the
// node's in-process client must accept it where a bare context is refused.
func TestServiceContext_BindsTheNodePrincipal(t *testing.T) {
	s, node := newPrincipalServer(t)
	ctx, err := s.ServiceContext(context.Background())
	require.NoError(t, err)

	require.NoError(t, node.Client().KV().Put(ctx, "bound-k", []byte("v1")))
	got, err := node.Client().KV().Get(ctx, "bound-k")
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))
}

// TestXKMSStorageContext_TenantRequestRunsInTheSystemTenant pins Codex review
// 7679 on PR #1841: the xkms module's storage lives in the system tenant, so
// a tenant caller's request must reach it as the service principal targeting
// the system tenant. ServiceContext alone keeps the request's tenant, which
// the node's self-scope check refuses.
func TestXKMSStorageContext_TenantRequestRunsInTheSystemTenant(t *testing.T) {
	s, node := newPrincipalServer(t)
	request := qrdbsdk.ContextWithTenant(context.Background(), "acme")

	principalOnly, err := s.ServiceContext(request)
	require.NoError(t, err)
	require.Error(t, node.Client().KV().Put(principalOnly, "xkms-k", []byte("v1")),
		"the principal reached the request tenant's storage")

	bound, err := s.xkmsStorageContext(request)
	require.NoError(t, err)
	tenantID, ok := qrdbsdk.TenantFromContext(bound)
	require.True(t, ok)
	assert.Equal(t, qrdbsdk.SystemTenantID, tenantID)
	require.NoError(t, node.Client().KV().Put(bound, "xkms-k", []byte("v1")))
	got, err := node.Client().KV().Get(bound, "xkms-k")
	require.NoError(t, err)
	assert.Equal(t, "v1", string(got))
}

// TestXKMSStorageContext_NoServicePrincipalIsRefused: a node without a
// service principal cannot bind the module's storage, and the binder says so
// rather than run the call anonymously.
func TestXKMSStorageContext_NoServicePrincipalIsRefused(t *testing.T) {
	node, err := qrdbnode.NewStandaloneNode(context.Background(), principalNodeConfig(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })

	ctx, err := serverOverNode(node).xkmsStorageContext(context.Background())
	require.ErrorIs(t, err, qrdbnode.ErrNoServicePrincipal)
	assert.Nil(t, ctx)
}

// TestDelegatedContext_RefusesAnUnverifiedChain: a chain the node cannot
// verify as a tenant-issued delegation binds no identity at all; nothing falls
// back to the service principal.
func TestDelegatedContext_RefusesAnUnverifiedChain(t *testing.T) {
	s, _ := newPrincipalServer(t)
	ctx, err := s.DelegatedContext(context.Background(), nil)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNoEmbeddedNode)
	assert.Nil(t, ctx)
}

// TestDelegatedContext_NoNodeIsRefused: with no embedded node there is nothing
// to verify the delegation, so it is refused rather than passed through.
func TestDelegatedContext_NoNodeIsRefused(t *testing.T) {
	s := &Server{}
	ctx, err := s.DelegatedContext(context.Background(), nil)
	require.ErrorIs(t, err, ErrNoEmbeddedNode)
	assert.Nil(t, ctx)
}

// TestAuthMiddleware_IsTheNodes: the server hands out its node's own
// authorization middleware, so an embedded delegation is authorized by the
// same instance the node's network transports enforce with.
func TestAuthMiddleware_IsTheNodes(t *testing.T) {
	s, node := newPrincipalServer(t)
	mw := s.AuthMiddleware()
	require.NotNil(t, mw)
	assert.Same(t, node.AuthMiddleware(), mw)
}

// TestAuthMiddleware_NoNodeIsNil: with no embedded node there is no
// middleware, and an embedded delegation over the server is refused.
func TestAuthMiddleware_NoNodeIsNil(t *testing.T) {
	s := &Server{}
	assert.Nil(t, s.AuthMiddleware())
}

// TestServiceKV_RemoteClientKeepsItsOwnIdentity pins that, with no embedded
// node, the server's clients are passed through: a remote transport
// authenticates as the server's mTLS identity, not the embedded principal.
func TestServiceKV_RemoteClientKeepsItsOwnIdentity(t *testing.T) {
	s := &Server{}
	var kv transport.KVClient = &recordingGuardedKV{}
	assert.Same(t, kv, s.serviceKV(kv))
}

func TestServiceTransaction_RemoteClientKeepsItsOwnIdentity(t *testing.T) {
	s := &Server{}
	var txn transport.TransactionClient
	assert.Nil(t, s.serviceTransaction(txn))
}

// TestServiceCluster_LeaderCheckRunsAsServicePrincipal pins the scheduler's
// leader check to the service principal. The embedded node refuses an
// anonymous membership read, and the scheduler reads that refusal as "not
// leader", which would suppress every cluster-wide job.
func TestServiceCluster_LeaderCheckRunsAsServicePrincipal(t *testing.T) {
	// A standalone node has no Raft membership view; the shared clustered
	// node leads shard 1, as replica 1, under the production grants.
	node := testqrdbnode.Shared(t).Node
	_, err := node.Client().Cluster().Members(context.Background(), qbshards.PlatformShardID)
	require.ErrorIs(t, err, qrdbsdk.ErrUnauthenticated, "an anonymous read was served; this test would prove nothing")

	cfg := &config.Config{Server: config.ServerConfig{Mode: config.ServerModeCluster}}
	cfg.Raft.NodeID = 1
	s := &Server{config: cfg, logger: buildTestLogger(t), qrdbClient: newNodeBackedShardedClient(node)}
	s.setQRDBNode(node)

	clustering := s.schedulerClustering()
	require.NotNil(t, clustering)
	require.NotNil(t, clustering.Leader)
	isLeader, err := clustering.Leader.IsLeader()
	require.NoError(t, err)
	assert.True(t, isLeader, "a single-node cluster leads its own shard")
}

func TestServiceCluster_RemoteClientKeepsItsOwnIdentity(t *testing.T) {
	s := &Server{}
	var cluster qrdbservice.Membership
	assert.Nil(t, s.serviceCluster(cluster))
}

// TestServiceVectorProvider_ClosedClientIsRefused: a shard client the server
// cannot hand out yields its typed error and no vector client.
func TestServiceVectorProvider_ClosedClientIsRefused(t *testing.T) {
	s := &Server{qrdbClient: &ShardedClient{}}
	s.qrdbClient.closed.Store(true)

	vec, err := serviceVectorProvider{server: s}.VectorForShard(qbshards.PlatformShardID)
	var closedErr *ShardedClientClosedError
	require.ErrorAs(t, err, &closedErr)
	assert.Nil(t, vec)
}

// TestIssuanceReadContext_SystemTenantReadsAsThePrincipal pins #1727: token
// issuance for a system-tenant user resolves the user's effective permissions
// before any bearer exists, so the read runs as the node's service principal,
// which holds permissions:resolve for it.
func TestIssuanceReadContext_SystemTenantReadsAsThePrincipal(t *testing.T) {
	s, node := newPrincipalServer(t)
	ctx, err := s.issuanceReadContext(context.Background(), qrdbsdk.SystemTenantID)
	require.NoError(t, err)

	_, err = node.Client().RBAC().ListPermissions(ctx, &transport.Identity{
		Subject:  "issuance-admin",
		TenantID: qrdbsdk.SystemTenantID,
	})
	require.NoError(t, err, "the node refused the principal's permission read")
}

// issuanceCallerKey marks the caller's own context in the issuance tests.
type issuanceCallerKey struct{}

// TestIssuanceReadContext_OtherTenantIsNotBound is the refusal half: go-qrdb
// lets the principal act in the system tenant only, so another tenant's
// context is returned unbound and the node refuses the read rather than answer
// it from the system tenant.
func TestIssuanceReadContext_OtherTenantIsNotBound(t *testing.T) {
	s, node := newPrincipalServer(t)
	parent := context.WithValue(context.Background(), issuanceCallerKey{}, "caller")
	ctx, err := s.issuanceReadContext(parent, "acme")
	require.NoError(t, err)
	assert.Equal(t, parent, ctx, "another tenant's read was bound to the system principal")

	_, err = node.Client().RBAC().ListPermissions(ctx, &transport.Identity{
		Subject:  "acme-user",
		TenantID: "acme",
	})
	require.ErrorIs(t, err, qrdbsdk.ErrUnauthenticated)
}
