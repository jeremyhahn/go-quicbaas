//go:build vector && qrdb_vector && !integration

package server

import (
	"context"
	"slices"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// ungrantedPrincipal is a service principal holding the production grants
// minus the vector search and compaction-progress verbs, so a test can see
// the node refuse them.
const ungrantedPrincipal = "quicbaas-test-no-vector-search"

// newUngrantedPrincipalServer boots an authorized node whose service
// principal lacks vector:search_similar and vector:get_compaction_progress.
func newUngrantedPrincipalServer(t *testing.T) *Server {
	t.Helper()
	perms, err := qrdbservice.Grants()
	require.NoError(t, err)
	search, err := qrdbsdk.ParsePermission("vector:search_similar")
	require.NoError(t, err)
	progress, err := qrdbsdk.ParsePermission("vector:get_compaction_progress")
	require.NoError(t, err)
	perms = slices.DeleteFunc(perms, func(p qrdbsdk.Permission) bool { return p == search || p == progress })
	qrdbsdk.RegisterPermissionRoleGrant(ungrantedPrincipal, perms...)

	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir() // vector storage requires a data dir
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "memory"}}
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, qrdbnode.WithServicePrincipal(ungrantedPrincipal))
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })

	s := &Server{}
	s.setQRDBNode(node)
	s.qrdbClient = newNodeBackedShardedClient(node)
	return s
}

// seedPrincipalNamespace creates a namespace with one vector as the
// server's service principal and returns its vector client.
func seedPrincipalNamespace(t *testing.T, s *Server) transport.VectorClient {
	t.Helper()
	ctx := context.Background()
	vec, err := serviceVectorProvider{server: s}.VectorForShard(qbshards.PlatformShardID)
	require.NoError(t, err)
	_, err = vec.CreateNamespace(ctx, &transport.CreateNamespaceRequest{Namespace: "principal-ns", Dimension: 2})
	require.NoError(t, err)
	_, err = vec.UpsertVector(ctx, &transport.UpsertVectorRequest{Namespace: "principal-ns", ID: 1, Vector: []float32{1, 0}})
	require.NoError(t, err)
	return vec
}

func TestServiceVector_PrincipalSearchesAndReadsCompactionProgress(t *testing.T) {
	s, _ := newPrincipalServer(t)
	vec := seedPrincipalNamespace(t, s)
	ctx := context.Background()

	resp, err := vec.SearchSimilar(ctx, &transport.SearchSimilarRequest{Namespace: "principal-ns", QueryVector: []float32{1, 0}, K: 1})
	require.NoError(t, err)
	require.NotNil(t, resp)

	compact, err := vec.CompactNamespace(ctx, &transport.CompactNamespaceRequest{Namespace: "principal-ns"})
	require.NoError(t, err)
	_, err = vec.GetCompactionProgress(ctx, &transport.GetCompactionProgressRequest{Namespace: "principal-ns", OperationID: compact.OperationID})
	require.NoError(t, err)
}

func TestServiceVector_UngrantedPrincipalIsRefused(t *testing.T) {
	s := newUngrantedPrincipalServer(t)
	vec := seedPrincipalNamespace(t, s)
	ctx := context.Background()

	_, err := vec.SearchSimilar(ctx, &transport.SearchSimilarRequest{Namespace: "principal-ns", QueryVector: []float32{1, 0}, K: 1})
	require.Error(t, err, "the node served a verb the principal does not hold")
	require.NotErrorIs(t, err, qrdbsdk.ErrUnauthenticated, "the principal was not bound")

	compact, err := vec.CompactNamespace(ctx, &transport.CompactNamespaceRequest{Namespace: "principal-ns"})
	require.NoError(t, err)
	_, err = vec.GetCompactionProgress(ctx, &transport.GetCompactionProgressRequest{Namespace: "principal-ns", OperationID: compact.OperationID})
	require.Error(t, err, "the node served a verb the principal does not hold")
	require.NotErrorIs(t, err, qrdbsdk.ErrUnauthenticated, "the principal was not bound")
}
