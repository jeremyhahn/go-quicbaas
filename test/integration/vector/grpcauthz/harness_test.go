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

//go:build integration

package grpcauthz

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// callTimeout bounds a single RPC against the in-process server.
const callTimeout = 30 * time.Second

// vectorDims is the dimensionality every namespace in this suite uses.
const vectorDims = 8

// The grant tiers below are the live expression of the read / write / admin
// split the deleted RBAC suite asserted. The live gate has no coarse
// "vector:read" permission: each gRPC method maps to one qrdb-canonical
// per-verb permission (pkg/api/grpc/authz_mapping.go), and a tier is the set
// of those verbs a principal is granted.
var (
	// readGrant covers every VectorDB verb that only observes data.
	readGrant = []string{
		"vector:get",
		"vector:list",
		"vector:search_similar",
		"vector:search_by_id",
		"vector:search_by_metadata",
		"vector:hybrid_search",
		"vector:namespace_list",
		"vector:namespace_info",
	}

	// writeGrant covers every VectorDB verb that mutates vectors inside an
	// existing namespace.
	writeGrant = []string{
		"vector:upsert",
		"vector:batch_upsert",
		"vector:update_metadata",
		"vector:delete",
		"vector:batch_delete",
	}

	// adminGrant covers namespace lifecycle and index management.
	adminGrant = []string{
		"vector:namespace_create",
		"vector:namespace_delete",
		"vector:rebuild_index",
		"vector:get_index_stats",
		"vector:get_rebuild_progress",
		"vector:compact",
		"vector:get_compaction_progress",
	}
)

// fullGrant is the union of every tier: a tenant operator's vector authority.
func fullGrant() []string {
	all := make([]string, 0, len(readGrant)+len(writeGrant)+len(adminGrant))
	all = append(all, readGrant...)
	all = append(all, writeGrant...)
	return append(all, adminGrant...)
}

// iamComponents is the slice of the running service this suite provisions
// identities through. The concrete service exposes all three; asserting the
// interface keeps the suite on the server's own IAM plane rather than a copy.
type iamComponents interface {
	GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	GetAppManager(ctx context.Context) (*iam.AppManager, error)
	GetTenantManager(ctx context.Context) (iam.TenantManager, error)
}

// nameSeq makes every tenant, app and namespace name in a run unique.
var nameSeq atomic.Uint64

// uniqueName returns prefix plus a run-unique suffix, restricted to the
// characters tenant IDs and namespace names both accept.
func uniqueName(prefix string) string {
	return prefix + "-" + strconv.FormatUint(nameSeq.Add(1), 10) + "-" +
		strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 10)
}

// iamPlane returns the server's IAM components.
func iamPlane(t *testing.T) iamComponents {
	t.Helper()
	components, ok := harness.srv.Service().(iamComponents)
	require.True(t, ok, "the running service must expose its OIDC provider, app manager and tenant manager")
	return components
}

// systemCtx is the daemon-internal identity used ONLY to provision tenants and
// applications — the control-plane work an administrator does before any
// tenant principal exists. It never reaches the gRPC surface under test.
func systemCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	return auth.NewSystemContext(ctx), cancel
}

// newTenant provisions a real tenant through the server's tenant manager and
// returns its ID.
func newTenant(t *testing.T, prefix string) string {
	t.Helper()
	ctx, cancel := systemCtx(t)
	defer cancel()

	tm, err := iamPlane(t).GetTenantManager(ctx)
	require.NoError(t, err, "tenant manager")
	require.NotNil(t, tm, "tenant manager")

	id := uniqueName(prefix)
	tenant, err := tm.CreateTenant(ctx, iam.CreateTenantOptions{ID: id, Name: id})
	require.NoErrorf(t, err, "create tenant %s", id)
	require.Equal(t, id, tenant.ID)
	return id
}

// principal is an authenticated caller: a bearer the server's own OIDC
// provider minted for an application in one tenant.
type principal struct {
	name     string
	tenantID string
	bearer   string

	// clientID and clientSecret are the application's OAuth2 credentials,
	// kept so a test can revoke the bearer through the provider.
	clientID     string
	clientSecret string
}

// newPrincipal registers an OAuth2 client_credentials application in tenantID
// whose grant is exactly scopes, then obtains its access token from the
// server's OIDC provider. The provider stamps the grant into the token's scp
// claim, which is the only authority the live gRPC gate reads.
func newPrincipal(t *testing.T, tenantID, name string, scopes ...string) principal {
	t.Helper()
	ctx, cancel := systemCtx(t)
	defer cancel()

	plane := iamPlane(t)
	apps, err := plane.GetAppManager(ctx)
	require.NoError(t, err, "app manager")
	require.NotNil(t, apps, "app manager")

	app, err := apps.RegisterApp(ctx, tenantID, iam.RegisterAppOptions{
		Name:       uniqueName(name),
		GrantTypes: []string{iam.GrantTypeClientCredentials},
		Scopes:     scopes,
	})
	require.NoErrorf(t, err, "register app %s in tenant %s", name, tenantID)

	provider, err := plane.GetOIDCProvider(ctx)
	require.NoError(t, err, "oidc provider")
	require.NotNil(t, provider, "oidc provider")

	tok, err := provider.Token(ctx, &iam.TokenRequest{
		GrantType:    iam.GrantTypeClientCredentials,
		ClientID:     app.ClientID,
		ClientSecret: app.ClientSecret,
	})
	require.NoErrorf(t, err, "client_credentials token for %s", name)
	require.NotEmpty(t, tok.AccessToken)

	return principal{
		name:         name,
		tenantID:     tenantID,
		bearer:       tok.AccessToken,
		clientID:     app.ClientID,
		clientSecret: app.ClientSecret,
	}
}

// ctx returns a per-call context carrying the principal's bearer, plus any
// extra metadata pairs (used to present a spoofed tenant header).
func (p principal) ctx(t *testing.T, extra ...string) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	pairs := append([]string{"authorization", "Bearer " + p.bearer}, extra...)
	return metadata.AppendToOutgoingContext(ctx, pairs...), cancel
}

// anonymousCtx returns a per-call context with no credential at all.
func anonymousCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), callTimeout)
}

// dialVector returns a VectorDB client over the harness's connection to the
// daemon's Unix-socket gRPC listener.
func dialVector(t *testing.T) pb.VectorDBClient {
	t.Helper()
	require.NotNil(t, harness.conn, "harness connection")
	return pb.NewVectorDBClient(harness.conn)
}

// namespaceConfig is the configuration every namespace in this suite uses.
func namespaceConfig(name string) *pb.NamespaceConfig {
	return &pb.NamespaceConfig{
		Namespace:      name,
		Dimensions:     vectorDims,
		DistanceMetric: pb.DistanceMetric_DISTANCE_METRIC_COSINE,
		IndexType:      pb.IndexType_INDEX_TYPE_HNSW,
	}
}

// vectorFor returns a deterministic, non-zero vector derived from seed, so
// each tenant's data is distinguishable by value as well as by ID.
func vectorFor(seed int) []float32 {
	v := make([]float32, vectorDims)
	for i := range v {
		v[i] = float32((seed+1)*(i+3)%17) + 1
	}
	return v
}

// createNamespace creates name as principal p and requires success.
func createNamespace(t *testing.T, client pb.VectorDBClient, p principal, name string) {
	t.Helper()
	ctx, cancel := p.ctx(t)
	defer cancel()
	_, err := client.CreateNamespace(ctx, &pb.CreateNamespaceRequest{Config: namespaceConfig(name)})
	require.NoErrorf(t, err, "%s: create namespace %q", p.name, name)
}

// upsert writes one vector as principal p and requires success.
func upsert(t *testing.T, client pb.VectorDBClient, p principal, namespace, id string, vec []float32) {
	t.Helper()
	ctx, cancel := p.ctx(t)
	defer cancel()
	_, err := client.UpsertVector(ctx, &pb.UpsertVectorRequest{Vector: &pb.VectorEntity{
		Id:        id,
		Namespace: namespace,
		Vector:    vec,
		Metadata:  map[string]string{"owner": p.tenantID},
	}})
	require.NoErrorf(t, err, "%s: upsert %s/%s", p.name, namespace, id)
}

// getVector reads one vector as principal p. It returns the entity when the
// server found it, nil when the server answered "not found" in either of the
// two shapes the API uses (Found=false or a NotFound status), and the raw
// error for anything else so callers can assert on it.
func getVector(t *testing.T, client pb.VectorDBClient, p principal, namespace, id string, extra ...string) (*pb.VectorEntity, error) {
	t.Helper()
	ctx, cancel := p.ctx(t, extra...)
	defer cancel()
	resp, err := client.GetVector(ctx, &pb.GetVectorRequest{
		Namespace:       namespace,
		Id:              id,
		IncludeVector:   true,
		IncludeMetadata: true,
	})
	if status.Code(err) == codes.NotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !resp.GetFound() {
		return nil, nil
	}
	return resp.GetVector(), nil
}

// requireOwnVector asserts principal p can still read its own vector intact.
func requireOwnVector(t *testing.T, client pb.VectorDBClient, p principal, namespace, id string, want []float32) {
	t.Helper()
	got, err := getVector(t, client, p, namespace, id)
	require.NoErrorf(t, err, "%s: read own vector %s/%s", p.name, namespace, id)
	require.NotNilf(t, got, "%s: own vector %s/%s must still exist", p.name, namespace, id)
	require.Equalf(t, want, got.GetVector(), "%s: own vector %s/%s must be unmodified", p.name, namespace, id)
	require.Equal(t, p.tenantID, got.GetMetadata()["owner"])
}

// listNamespaces returns the namespace names visible to principal p.
func listNamespaces(t *testing.T, client pb.VectorDBClient, p principal, extra ...string) []string {
	t.Helper()
	ctx, cancel := p.ctx(t, extra...)
	defer cancel()
	resp, err := client.ListNamespaces(ctx, &pb.ListNamespacesRequest{})
	require.NoErrorf(t, err, "%s: list namespaces", p.name)
	names := make([]string, 0, len(resp.GetNamespaces()))
	for _, ns := range resp.GetNamespaces() {
		names = append(names, ns.GetConfig().GetNamespace())
	}
	return names
}

// searchHits runs a similarity search as principal p and returns the matched
// entities with their metadata. SearchSimilar is server-streaming, so this
// exercises the STREAM interceptor; an authorization failure surfaces on the
// first Recv.
func searchHits(t *testing.T, client pb.VectorDBClient, p principal, namespace string, query []float32) ([]*pb.VectorEntity, error) {
	t.Helper()
	ctx, cancel := p.ctx(t)
	defer cancel()
	stream, err := client.SearchSimilar(ctx, &pb.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     query,
		K:               10,
		IncludeMetadata: true,
	})
	if err != nil {
		return nil, err
	}
	var hits []*pb.VectorEntity
	for {
		resp, rerr := stream.Recv()
		if errors.Is(rerr, io.EOF) {
			return hits, nil
		}
		if rerr != nil {
			return hits, rerr
		}
		for _, r := range resp.GetResults() {
			hits = append(hits, r.GetVector())
		}
	}
}

// foreignHits returns the hits whose owner is not tenantID.
func foreignHits(hits []*pb.VectorEntity, tenantID string) []string {
	var foreign []string
	for _, h := range hits {
		if owner := h.GetMetadata()["owner"]; owner != tenantID {
			foreign = append(foreign, h.GetId()+"@"+owner)
		}
	}
	return foreign
}

// requireCode asserts err is a gRPC status carrying want.
func requireCode(t *testing.T, err error, want codes.Code, msgAndArgs ...interface{}) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
	st, ok := status.FromError(err)
	require.Truef(t, ok, "error is not a gRPC status: %v", err)
	require.Equalf(t, want, st.Code(), "unexpected status %v: %s", st.Code(), st.Message())
}

// requireNotAuthzDenied asserts err is NOT an authentication or authorization
// rejection. It is the allow-direction twin of requireCode: an allowed verb
// may still fail for a data reason, but never at the gate.
func requireNotAuthzDenied(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		return
	}
	code := status.Code(err)
	require.NotEqualf(t, codes.PermissionDenied, code, "%s was denied at the gate: %v", what, err)
	require.NotEqualf(t, codes.Unauthenticated, code, "%s was refused authentication: %v", what, err)
}

// containsNamespace reports whether names holds want. A name that leaks its
// storage prefix ("tenant/<id>/want") still counts as a hit.
func containsNamespace(names []string, want string) bool {
	for _, n := range names {
		if n == want || strings.HasSuffix(n, "/"+want) {
			return true
		}
	}
	return false
}
