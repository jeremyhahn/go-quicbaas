package qrdbservice

import (
	"context"
	"errors"
	"reflect"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ctxKey struct{}

// fakeBinder marks the context it binds, or fails.
type fakeBinder struct{ err error }

func (f fakeBinder) ServiceContext(ctx context.Context) (context.Context, error) {
	if f.err != nil {
		return nil, f.err
	}
	return context.WithValue(ctx, ctxKey{}, PrincipalName), nil
}

var errBind = errors.New("no service principal")

// recorder is a KV and transaction client that records whether each call
// arrived bound to the principal.
type recorder struct {
	calls int
	bound int
}

func (r *recorder) see(ctx context.Context) {
	r.calls++
	if ctx.Value(ctxKey{}) == PrincipalName {
		r.bound++
	}
}

func (r *recorder) Put(ctx context.Context, _ string, _ []byte, _ ...transport.WriteOption) error {
	r.see(ctx)
	return nil
}

func (r *recorder) Get(ctx context.Context, _ string, _ ...transport.ReadOption) ([]byte, error) {
	r.see(ctx)
	return []byte("v"), nil
}

func (r *recorder) Delete(ctx context.Context, _ string, _ ...transport.WriteOption) error {
	r.see(ctx)
	return nil
}

func (r *recorder) List(ctx context.Context, _ string, _ ...transport.ReadOption) ([]string, error) {
	r.see(ctx)
	return []string{"k"}, nil
}

func (r *recorder) Scan(ctx context.Context, _ string, _ ...transport.ReadOption) (map[string][]byte, error) {
	r.see(ctx)
	return map[string][]byte{"k": nil}, nil
}

func (r *recorder) Exists(ctx context.Context, _ string, _ ...transport.ReadOption) (bool, error) {
	r.see(ctx)
	return true, nil
}

func (r *recorder) ExecuteTransaction(ctx context.Context, _ *transport.TransactionRequest, _ ...transport.WriteOption) (*transport.TransactionResponse, error) {
	r.see(ctx)
	return &transport.TransactionResponse{Succeeded: true}, nil
}

// kvCalls drives every KVClient method once and returns the errors.
func kvCalls(kv transport.KVClient) []error {
	ctx := context.Background()
	_, getErr := kv.Get(ctx, "k")
	_, listErr := kv.List(ctx, "p")
	_, scanErr := kv.Scan(ctx, "p")
	_, existsErr := kv.Exists(ctx, "k")
	return []error{kv.Put(ctx, "k", nil), getErr, kv.Delete(ctx, "k"), listErr, scanErr, existsErr}
}

func TestGrants_ResolveEveryNameAgainstQRDB(t *testing.T) {
	perms, err := Grants()
	require.NoError(t, err)
	require.Len(t, perms, len(grantNames))
	for i, p := range perms {
		assert.Equal(t, grantNames[i], p.String())
	}
}

// TestGrants_HoldNoAdministrativeAuthority pins the least grant: the
// principal creates no tenant, reads no plaintext credential, and holds no
// seal verb the CLI does not call nor any tenant-only barrier verb.
func TestGrants_HoldNoAdministrativeAuthority(t *testing.T) {
	perms, err := Grants()
	require.NoError(t, err)
	for _, forbidden := range []qrdbsdk.Permission{
		qrdbsdk.PermissionTenantsCreate, qrdbsdk.PermissionCredentialsRetrieve,
		"seal:initialize_shamir", "tenant_barrier:seal", "tenant_barrier:officer_list",
	} {
		assert.NotContains(t, perms, forbidden)
	}
}

// TestGrants_HoldTheCLISealOperations pins ruling 179: the CLI's embedded
// client acts as this principal, so it holds the seal verbs the CLI calls.
func TestGrants_HoldTheCLISealOperations(t *testing.T) {
	perms, err := Grants()
	require.NoError(t, err)
	assert.Contains(t, perms, qrdbsdk.PermissionSealUnseal)
	assert.Contains(t, perms, qrdbsdk.PermissionSealRekey)
}

func TestParseGrants_RejectsAnUnknownName(t *testing.T) {
	_, err := parseGrants([]string{"kv:get", "kv:teleport"})
	var grantErr *GrantError
	require.ErrorAs(t, err, &grantErr)
	assert.Equal(t, "kv:teleport", grantErr.Name)
	assert.Contains(t, err.Error(), "kv:teleport")
	assert.ErrorAs(t, err, &UnregisteredPermissionError{})
}

func TestParseGrants_RejectsAMalformedName(t *testing.T) {
	_, err := parseGrants([]string{"kvget"})
	var grantErr *GrantError
	require.ErrorAs(t, err, &grantErr)
	assert.Equal(t, "kvget", grantErr.Name)
	assert.NotNil(t, errors.Unwrap(err))
}

func TestRegister_GrantsThePrincipalAndIsIdempotent(t *testing.T) {
	require.NoError(t, Register())
	require.NoError(t, Register())

	opt, err := NodeOption()
	require.NoError(t, err)
	assert.NotNil(t, opt)
}

func TestKV_BindsEveryCall(t *testing.T) {
	rec := &recorder{}
	for _, err := range kvCalls(KV(fakeBinder{}, rec)) {
		require.NoError(t, err)
	}
	assert.Equal(t, 6, rec.calls)
	assert.Equal(t, 6, rec.bound)
}

func TestKV_UnboundContextNeverReachesTheNode(t *testing.T) {
	rec := &recorder{}
	for _, err := range kvCalls(KV(fakeBinder{err: errBind}, rec)) {
		assert.ErrorIs(t, err, errBind)
	}
	assert.Zero(t, rec.calls)
}

func TestTransaction_BindsTheCall(t *testing.T) {
	rec := &recorder{}
	resp, err := Transaction(fakeBinder{}, rec).ExecuteTransaction(context.Background(), &transport.TransactionRequest{})
	require.NoError(t, err)
	assert.True(t, resp.Succeeded)
	assert.Equal(t, 1, rec.bound)
}

func TestTransaction_UnboundContextNeverReachesTheNode(t *testing.T) {
	rec := &recorder{}
	resp, err := Transaction(fakeBinder{err: errBind}, rec).ExecuteTransaction(context.Background(), &transport.TransactionRequest{})
	assert.ErrorIs(t, err, errBind)
	assert.Nil(t, resp)
	assert.Zero(t, rec.calls)
}

func (r *recorder) Members(ctx context.Context, _ uint64) ([]qrdbsdk.Member, error) {
	r.see(ctx)
	return []qrdbsdk.Member{{ReplicaID: 1, IsLeader: true}}, nil
}

func TestCluster_BindsTheCall(t *testing.T) {
	rec := &recorder{}
	members, err := Cluster(fakeBinder{}, rec).Members(context.Background(), 1)
	require.NoError(t, err)
	assert.Len(t, members, 1)
	assert.Equal(t, 1, rec.bound)
}

func TestCluster_UnboundContextNeverReachesTheNode(t *testing.T) {
	rec := &recorder{}
	members, err := Cluster(fakeBinder{err: errBind}, rec).Members(context.Background(), 1)
	assert.ErrorIs(t, err, errBind)
	assert.Nil(t, members)
	assert.Zero(t, rec.calls)
}

// TestGrants_IncludeThePeersRead pins the grant the scheduler's leader check
// needs: without admin:peers the bound membership read is still refused.
func TestGrants_IncludeThePeersRead(t *testing.T) {
	perms, err := Grants()
	require.NoError(t, err)
	assert.Contains(t, perms, qrdbsdk.PermissionAdminPeers)
}

// guardedRecorder is a recorder that also carries the guarded verbs.
type guardedRecorder struct{ recorder }

func (g *guardedRecorder) PutIfAbsent(ctx context.Context, _ string, _ []byte) (bool, error) {
	g.see(ctx)
	return true, nil
}

func (g *guardedRecorder) CompareAndDelete(ctx context.Context, _ string, _ []byte) (bool, error) {
	g.see(ctx)
	return true, nil
}

func (g *guardedRecorder) CompareAndSwap(ctx context.Context, _ string, _, _ []byte) (bool, error) {
	g.see(ctx)
	return true, nil
}

// guardedCalls drives every guarded verb once and returns the errors.
func guardedCalls(kv transport.GuardedKVClient) []error {
	ctx := context.Background()
	_, putErr := kv.PutIfAbsent(ctx, "k", nil)
	_, delErr := kv.CompareAndDelete(ctx, "k", nil)
	_, swapErr := kv.CompareAndSwap(ctx, "k", nil, nil)
	return []error{putErr, delErr, swapErr}
}

// A guarded client stays guarded once bound, so arbitration still happens in
// the store, and its guarded verbs run as the principal too.
func TestKV_KeepsAndBindsTheGuardedVerbs(t *testing.T) {
	rec := &guardedRecorder{}

	guarded, ok := KV(fakeBinder{}, rec).(transport.GuardedKVClient)
	require.True(t, ok, "binding dropped the guarded verbs")
	for _, err := range guardedCalls(guarded) {
		require.NoError(t, err)
	}
	assert.Equal(t, 3, rec.bound)
}

func TestKV_GuardedUnboundContextNeverReachesTheNode(t *testing.T) {
	rec := &guardedRecorder{}

	guarded, ok := KV(fakeBinder{err: errBind}, rec).(transport.GuardedKVClient)
	require.True(t, ok)
	for _, err := range guardedCalls(guarded) {
		assert.ErrorIs(t, err, errBind)
	}
	assert.Zero(t, rec.calls)
}

// A client without the guarded verbs is not promoted to one.
func TestKV_PlainClientIsNotPromoted(t *testing.T) {
	_, ok := KV(fakeBinder{}, &recorder{}).(transport.GuardedKVClient)
	assert.False(t, ok)
}

func (r *recorder) IndexCreate(ctx context.Context, _, _ string, _ transport.IndexConfig) error {
	r.see(ctx)
	return nil
}

func (r *recorder) IndexDrop(ctx context.Context, _ string) error {
	r.see(ctx)
	return nil
}

func (r *recorder) IndexQuery(ctx context.Context, _, _ string) (*transport.IndexQueryResponse, error) {
	r.see(ctx)
	return &transport.IndexQueryResponse{}, nil
}

func (r *recorder) IndexScan(ctx context.Context, _ string, _, _ *string) (*transport.IndexScanResponse, error) {
	r.see(ctx)
	return &transport.IndexScanResponse{}, nil
}

func (r *recorder) IndexList(ctx context.Context) (*transport.IndexListResponse, error) {
	r.see(ctx)
	return &transport.IndexListResponse{}, nil
}

func indexCalls(idx transport.IndexClient) []error {
	ctx := context.Background()
	_, queryErr := idx.IndexQuery(ctx, "i", "v")
	_, scanErr := idx.IndexScan(ctx, "i", nil, nil)
	_, listErr := idx.IndexList(ctx)
	return []error{
		idx.IndexCreate(ctx, "i", "f", transport.IndexConfig{}),
		idx.IndexDrop(ctx, "i"),
		queryErr,
		scanErr,
		listErr,
	}
}

// go-qrdb's DAO registers its entity indexes with a context of its own, so
// the index client, not the caller, must carry the identity.
func TestIndex_BindsEveryCall(t *testing.T) {
	rec := &recorder{}
	for _, err := range indexCalls(Index(fakeBinder{}, rec)) {
		require.NoError(t, err)
	}
	assert.Equal(t, 5, rec.calls)
	assert.Equal(t, 5, rec.bound)
}

func TestIndex_UnboundContextNeverReachesTheNode(t *testing.T) {
	rec := &recorder{}
	for _, err := range indexCalls(Index(fakeBinder{err: errBind}, rec)) {
		assert.ErrorIs(t, err, errBind)
	}
	assert.Zero(t, rec.calls)
}

func TestGrants_IncludeTheDAOIndexVerbs(t *testing.T) {
	assert.Subset(t, grantNames, []string{"index:create", "index:query", "index:scan"})
}

// The entity service's BatchPut and BatchDelete run ExecuteBatch as the
// service principal; without batch:execute qrdb refuses them (#1727).
func TestGrants_IncludeTheBatchExecute(t *testing.T) {
	perms, err := Grants()
	require.NoError(t, err)
	assert.Contains(t, perms, qrdbsdk.Permission("batch:execute"))
}

// vectorRecorder is a vector client that records whether each call arrived
// bound to the principal.
type vectorRecorder struct{ recorder }

func (r *vectorRecorder) CreateNamespace(ctx context.Context, _ *transport.CreateNamespaceRequest) (*transport.CreateNamespaceResponse, error) {
	r.see(ctx)
	return &transport.CreateNamespaceResponse{}, nil
}

func (r *vectorRecorder) DeleteNamespace(ctx context.Context, _ *transport.DeleteNamespaceRequest) (*transport.DeleteNamespaceResponse, error) {
	r.see(ctx)
	return &transport.DeleteNamespaceResponse{}, nil
}

func (r *vectorRecorder) ListNamespaces(ctx context.Context, _ *transport.ListNamespacesRequest) (*transport.ListNamespacesResponse, error) {
	r.see(ctx)
	return &transport.ListNamespacesResponse{}, nil
}

func (r *vectorRecorder) GetNamespaceInfo(ctx context.Context, _ *transport.GetNamespaceInfoRequest) (*transport.GetNamespaceInfoResponse, error) {
	r.see(ctx)
	return &transport.GetNamespaceInfoResponse{}, nil
}

func (r *vectorRecorder) UpsertVector(ctx context.Context, _ *transport.UpsertVectorRequest) (*transport.UpsertVectorResponse, error) {
	r.see(ctx)
	return &transport.UpsertVectorResponse{}, nil
}

func (r *vectorRecorder) GetVector(ctx context.Context, _ *transport.GetVectorRequest) (*transport.GetVectorResponse, error) {
	r.see(ctx)
	return &transport.GetVectorResponse{}, nil
}

func (r *vectorRecorder) DeleteVector(ctx context.Context, _ *transport.DeleteVectorRequest) (*transport.DeleteVectorResponse, error) {
	r.see(ctx)
	return &transport.DeleteVectorResponse{}, nil
}

func (r *vectorRecorder) BatchUpsert(ctx context.Context, _ *transport.BatchUpsertRequest) (*transport.BatchUpsertResponse, error) {
	r.see(ctx)
	return &transport.BatchUpsertResponse{}, nil
}

func (r *vectorRecorder) BatchDeleteVectors(ctx context.Context, _ *transport.BatchDeleteVectorsRequest) (*transport.BatchDeleteVectorsResponse, error) {
	r.see(ctx)
	return &transport.BatchDeleteVectorsResponse{}, nil
}

func (r *vectorRecorder) ListVectors(ctx context.Context, _ *transport.ListVectorsRequest) (*transport.ListVectorsResponse, error) {
	r.see(ctx)
	return &transport.ListVectorsResponse{}, nil
}

func (r *vectorRecorder) UpdateMetadata(ctx context.Context, _ *transport.UpdateVectorMetadataRequest) (*transport.UpdateVectorMetadataResponse, error) {
	r.see(ctx)
	return &transport.UpdateVectorMetadataResponse{}, nil
}

func (r *vectorRecorder) SearchSimilar(ctx context.Context, _ *transport.SearchSimilarRequest) (*transport.SearchSimilarResponse, error) {
	r.see(ctx)
	return &transport.SearchSimilarResponse{}, nil
}

func (r *vectorRecorder) SearchByMetadata(ctx context.Context, _ *transport.SearchByMetadataRequest) (*transport.SearchByMetadataResponse, error) {
	r.see(ctx)
	return &transport.SearchByMetadataResponse{}, nil
}

func (r *vectorRecorder) HybridSearch(ctx context.Context, _ *transport.HybridSearchRequest) (*transport.HybridSearchResponse, error) {
	r.see(ctx)
	return &transport.HybridSearchResponse{}, nil
}

func (r *vectorRecorder) SearchByID(ctx context.Context, _ *transport.SearchByIDRequest) (*transport.SearchByIDResponse, error) {
	r.see(ctx)
	return &transport.SearchByIDResponse{}, nil
}

func (r *vectorRecorder) RebuildIndex(ctx context.Context, _ *transport.RebuildIndexRequest) (*transport.RebuildIndexResponse, error) {
	r.see(ctx)
	return &transport.RebuildIndexResponse{}, nil
}

func (r *vectorRecorder) GetRebuildProgress(ctx context.Context, _ *transport.GetRebuildProgressRequest) (*transport.GetRebuildProgressResponse, error) {
	r.see(ctx)
	return &transport.GetRebuildProgressResponse{}, nil
}

func (r *vectorRecorder) GetIndexStats(ctx context.Context, _ *transport.GetIndexStatsRequest) (*transport.GetIndexStatsResponse, error) {
	r.see(ctx)
	return &transport.GetIndexStatsResponse{}, nil
}

func (r *vectorRecorder) CompactNamespace(ctx context.Context, _ *transport.CompactNamespaceRequest) (*transport.CompactNamespaceResponse, error) {
	r.see(ctx)
	return &transport.CompactNamespaceResponse{}, nil
}

func (r *vectorRecorder) GetCompactionProgress(ctx context.Context, _ *transport.GetCompactionProgressRequest) (*transport.GetCompactionProgressResponse, error) {
	r.see(ctx)
	return &transport.GetCompactionProgressResponse{}, nil
}

// vectorCalls makes every VectorClient call on vec, read from the interface
// itself so a verb qrdb adds is covered without editing this list, and
// returns each call's error.
func vectorCalls(t *testing.T, vec transport.VectorClient) []error {
	t.Helper()
	return vectorCallsAs(t, context.Background(), vec)
}

// vectorCallsAs makes every VectorClient call with ctx.
func vectorCallsAs(t *testing.T, ctx context.Context, vec transport.VectorClient) []error {
	t.Helper()
	iface := reflect.TypeOf((*transport.VectorClient)(nil)).Elem()
	v := reflect.ValueOf(vec)
	errs := make([]error, 0, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		m := v.MethodByName(iface.Method(i).Name)
		req := reflect.New(m.Type().In(1).Elem())
		out := m.Call([]reflect.Value{reflect.ValueOf(ctx), req})
		var err error
		if !out[1].IsNil() {
			err = out[1].Interface().(error)
		}
		errs = append(errs, err)
	}
	return errs
}

// The vector service's calls reach the embedded node, which refuses an
// unauthenticated context (#1727): every verb must carry the principal.
func TestVector_BindsEveryCall(t *testing.T) {
	rec := &vectorRecorder{}
	errs := vectorCalls(t, Vector(fakeBinder{}, rec))
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, len(errs), rec.calls)
	assert.Equal(t, len(errs), rec.bound)
}

func TestVector_UnboundContextNeverReachesTheNode(t *testing.T) {
	rec := &vectorRecorder{}
	for _, err := range vectorCalls(t, Vector(fakeBinder{err: errBind}, rec)) {
		assert.ErrorIs(t, err, errBind)
	}
	assert.Zero(t, rec.calls)
}

// A request's authenticated caller reaches the node as itself: rebinding it
// to the system-tenant principal would run a tenant's request as the system
// tenant (#1814).
func TestVector_CallerReachesTheNodeUnbound(t *testing.T) {
	rec := &vectorRecorder{}
	caller := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "alice", TenantID: "acme"})
	errs := vectorCallsAs(t, caller, Vector(fakeBinder{}, rec))
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, len(errs), rec.calls)
	assert.Zero(t, rec.bound)
}

// A caller never depends on the service principal, so a binder that cannot
// bind does not stop it.
func TestVector_CallerDoesNotNeedTheBinder(t *testing.T) {
	rec := &vectorRecorder{}
	caller := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "alice", TenantID: "acme"})
	for _, err := range vectorCallsAs(t, caller, Vector(fakeBinder{err: errBind}, rec)) {
		require.NoError(t, err)
	}
	assert.NotZero(t, rec.calls)
}

// Each VectorClient call nodeVectorStorage makes needs its own qrdb grant,
// and every name must be one qrdb registers.
func TestGrants_IncludeTheVectorServiceVerbs(t *testing.T) {
	want := []string{
		"vector:namespace_create", "vector:namespace_delete", "vector:namespace_list",
		"vector:namespace_info", "vector:upsert", "vector:get", "vector:delete",
		"vector:batch_upsert", "vector:batch_delete", "vector:list",
		"vector:search_by_metadata", "vector:get_index_stats", "vector:compact",
	}
	assert.Subset(t, grantNames, want)
	_, err := parseGrants(want)
	require.NoError(t, err)
}

// TestGrants_HoldEveryVectorVerbTheServerCalls pins, per call site, that the
// principal holds the permission the pinned go-qrdb declares on each vector
// verb a service-principal-bound path calls.
func TestGrants_HoldEveryVectorVerbTheServerCalls(t *testing.T) {
	cases := []struct {
		site string
		perm string
	}{
		{"pkg/server nodeVectorStorage CreateNamespace", "vector:namespace_create"},
		{"pkg/server nodeVectorStorage DeleteNamespace", "vector:namespace_delete"},
		{"pkg/server nodeVectorStorage ListNamespaces", "vector:namespace_list"},
		{"pkg/server nodeVectorStorage GetNamespaceInfo", "vector:namespace_info"},
		{"pkg/server nodeVectorStorage UpsertVector", "vector:upsert"},
		{"pkg/server nodeVectorStorage GetVector", "vector:get"},
		{"pkg/server nodeVectorStorage DeleteVector", "vector:delete"},
		{"pkg/server nodeVectorStorage BatchUpsert", "vector:batch_upsert"},
		{"pkg/server nodeVectorStorage BatchDeleteVectors", "vector:batch_delete"},
		{"pkg/server nodeVectorStorage ListVectors", "vector:list"},
		{"pkg/server nodeVectorStorage SearchByMetadata", "vector:search_by_metadata"},
		{"pkg/server nodeVectorStorage GetIndexStats", "vector:get_index_stats"},
		{"pkg/server nodeVectorStorage CompactNamespace", "vector:compact"},
		{"pkg/server nodeVectorStorage GetCompactionProgress", "vector:get_compaction_progress"},
		{"pkg/service vectorService SearchSimilar", "vector:search_similar"},
		{"pkg/service distributed SearchSimilar", "vector:search_similar"},
		{"pkg/service vectorService RebuildIndex", "vector:rebuild_index"},
		{"pkg/service vectorService GetRebuildProgress", "vector:get_rebuild_progress"},
		// serviceVector hands pkg/service the principal's whole client.
		{"pkg/qrdbservice serviceVector UpdateMetadata", "vector:update_metadata"},
		{"pkg/qrdbservice serviceVector HybridSearch", "vector:hybrid_search"},
		{"pkg/qrdbservice serviceVector SearchByID", "vector:search_by_id"},
	}
	for _, tc := range cases {
		t.Run(tc.site, func(t *testing.T) {
			assert.Contains(t, grantNames, tc.perm)
			_, err := parseGrants([]string{tc.perm})
			require.NoError(t, err)
		})
	}
}

func TestParseGrants_RejectsAnUnknownVectorVerb(t *testing.T) {
	_, err := parseGrants([]string{"vector:get", "vector:teleport"})
	var grantErr *GrantError
	require.ErrorAs(t, err, &grantErr)
	assert.Equal(t, "vector:teleport", grantErr.Name)
	assert.ErrorAs(t, err, &UnregisteredPermissionError{})
}
