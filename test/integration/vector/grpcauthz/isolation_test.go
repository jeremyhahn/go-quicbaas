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
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// tenantPair is two tenants, each with a principal holding the full vector
// grant, and the data tenant B seeded before tenant A attacks it.
type tenantPair struct {
	client pb.VectorDBClient
	a, b   principal

	// shared is a namespace name BOTH tenants created. Identical names are
	// the sharpest isolation probe: only tenant scoping keeps them apart.
	shared string
	// bOnly is a namespace only tenant B created.
	bOnly string
	// bVectorID is the ID of the vector B wrote into both namespaces.
	bVectorID string
	bVector   []float32
}

// newTenantPair provisions two tenants and seeds tenant B's data.
func newTenantPair(t *testing.T) *tenantPair {
	t.Helper()
	client := dialVector(t)

	p := &tenantPair{
		client:    client,
		a:         newPrincipal(t, newTenant(t, "vec-iso-a"), "tenant-a", fullGrant()...),
		b:         newPrincipal(t, newTenant(t, "vec-iso-b"), "tenant-b", fullGrant()...),
		shared:    uniqueName("shared"),
		bOnly:     uniqueName("b-only"),
		bVectorID: "b-secret",
		bVector:   vectorFor(7),
	}

	createNamespace(t, client, p.a, p.shared)
	// The first isolation assertion: tenant B can create a name tenant A
	// already holds. If the two tenants shared one namespace space this
	// would answer AlreadyExists.
	ctx, cancel := p.b.ctx(t)
	_, err := client.CreateNamespace(ctx, &pb.CreateNamespaceRequest{Config: namespaceConfig(p.shared)})
	cancel()
	require.NoErrorf(t, err, "tenant B could not create %q, which tenant A already holds: the tenants share one namespace space", p.shared)
	createNamespace(t, client, p.b, p.bOnly)
	upsert(t, client, p.b, p.shared, p.bVectorID, p.bVector)
	upsert(t, client, p.b, p.bOnly, p.bVectorID, p.bVector)
	return p
}

// requireBIntact asserts tenant B's seeded data survived whatever tenant A did.
func (p *tenantPair) requireBIntact(t *testing.T) {
	t.Helper()
	requireOwnVector(t, p.client, p.b, p.shared, p.bVectorID, p.bVector)
	requireOwnVector(t, p.client, p.b, p.bOnly, p.bVectorID, p.bVector)
	names := listNamespaces(t, p.client, p.b)
	assert.True(t, containsNamespace(names, p.shared), "tenant B lost namespace %q: %v", p.shared, names)
	assert.True(t, containsNamespace(names, p.bOnly), "tenant B lost namespace %q: %v", p.bOnly, names)
}

// requireNoLeak asserts a read by tenant A returned none of tenant B's data.
// Either a not-found answer or an error is an acceptable refusal; returning
// B's vector is the failure.
func requireNoLeak(t *testing.T, got *pb.VectorEntity, err error, what string) {
	t.Helper()
	if err != nil {
		requireNotAuthzDenied(t, err, what+" (tenant A holds the full vector grant; isolation must come from scoping, not from the gate)")
		return
	}
	if got != nil {
		t.Fatalf("%s: tenant A read tenant B's vector %q", what, got.GetId())
	}
}

// TestVectorGRPCAuthz_TenantCannotReadAnotherTenantsVectors proves a tenant
// holding the full vector grant still cannot read another tenant's vectors —
// not by the namespace name they share, not by a namespace only the other
// tenant owns, and not by naming the other tenant's storage path directly.
func TestVectorGRPCAuthz_TenantCannotReadAnotherTenantsVectors(t *testing.T) {
	p := newTenantPair(t)

	probes := map[string]string{
		"shared namespace name":   p.shared,
		"other tenant namespace":  p.bOnly,
		"other tenant raw prefix": "tenant/" + p.b.tenantID + "/" + p.bOnly,
	}
	for label, ns := range probes {
		t.Run(label, func(t *testing.T) {
			got, err := getVector(t, p.client, p.a, ns, p.bVectorID)
			requireNoLeak(t, got, err, "GetVector "+ns)
		})
	}

	p.requireBIntact(t)
}

// TestVectorGRPCAuthz_TenantCannotSearchAnotherTenantsVectors proves a
// similarity search (a server-streaming RPC, so the stream interceptor path)
// never returns another tenant's vectors, even when queried with the exact
// vector the other tenant stored.
func TestVectorGRPCAuthz_TenantCannotSearchAnotherTenantsVectors(t *testing.T) {
	p := newTenantPair(t)

	// Tenant A's own copy of the shared namespace holds one vector of its own,
	// so the search runs against a populated index rather than an empty one.
	upsert(t, p.client, p.a, p.shared, "a-own", vectorFor(3))

	for _, ns := range []string{p.shared, p.bOnly, "tenant/" + p.b.tenantID + "/" + p.bOnly} {
		t.Run(ns, func(t *testing.T) {
			hits, err := searchHits(t, p.client, p.a, ns, p.bVector)
			if err != nil {
				requireNotAuthzDenied(t, err, "SearchSimilar "+ns)
			}
			assert.Empty(t, foreignHits(hits, p.a.tenantID), "tenant A's search of %q returned another tenant's vectors", ns)
		})
	}

	// The positive control: tenant B's own search finds its vector, so the
	// negative result above is isolation and not a broken search.
	hits, err := searchHits(t, p.client, p.b, p.shared, p.bVector)
	require.NoError(t, err, "tenant B searches its own namespace")
	require.NotEmpty(t, hits, "tenant B must find its own vector")
	require.Equal(t, p.bVectorID, hits[0].GetId(), "tenant B's nearest hit is its own vector")

	p.requireBIntact(t)
}

// TestVectorGRPCAuthz_TenantCannotDeleteAnotherTenantsData proves deletes
// addressed at another tenant's vectors and namespaces never reach them.
func TestVectorGRPCAuthz_TenantCannotDeleteAnotherTenantsData(t *testing.T) {
	p := newTenantPair(t)

	targets := []string{p.shared, p.bOnly, "tenant/" + p.b.tenantID + "/" + p.bOnly}
	for _, ns := range targets {
		ctx, cancel := p.a.ctx(t)
		_, err := p.client.DeleteVector(ctx, &pb.DeleteVectorRequest{Namespace: ns, Id: p.bVectorID})
		cancel()
		requireNotAuthzDenied(t, err, "DeleteVector "+ns)
	}
	for _, ns := range []string{p.bOnly, "tenant/" + p.b.tenantID + "/" + p.bOnly} {
		ctx, cancel := p.a.ctx(t)
		_, err := p.client.DeleteNamespace(ctx, &pb.DeleteNamespaceRequest{Namespace: ns, Force: true})
		cancel()
		requireNotAuthzDenied(t, err, "DeleteNamespace "+ns)
	}

	p.requireBIntact(t)
}

// TestVectorGRPCAuthz_TenantCannotSeeAnotherTenantsNamespaces proves namespace
// listing and inspection are tenant-scoped.
func TestVectorGRPCAuthz_TenantCannotSeeAnotherTenantsNamespaces(t *testing.T) {
	p := newTenantPair(t)

	names := listNamespaces(t, p.client, p.a)
	assert.True(t, containsNamespace(names, p.shared), "tenant A must see its own namespace %q: %v", p.shared, names)
	assert.False(t, containsNamespace(names, p.bOnly), "tenant A listed tenant B's namespace %q: %v", p.bOnly, names)
	for _, n := range names {
		assert.NotContains(t, n, p.b.tenantID, "tenant A's listing leaked a name carrying tenant B's ID")
	}

	ctx, cancel := p.a.ctx(t)
	defer cancel()
	info, err := p.client.GetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{Namespace: p.bOnly})
	if err == nil {
		t.Fatalf("tenant A inspected tenant B's namespace %q: %v", p.bOnly, info)
	}
	requireNotAuthzDenied(t, err, "GetNamespaceInfo "+p.bOnly)

	// Tenant A's copy of the shared name is its own, empty namespace: B's
	// vector count must not show through it.
	ctx2, cancel2 := p.a.ctx(t)
	defer cancel2()
	own, err := p.client.GetNamespaceInfo(ctx2, &pb.GetNamespaceInfoRequest{Namespace: p.shared, IncludeStats: true})
	require.NoError(t, err, "tenant A inspects its own namespace")
	assert.Zero(t, own.GetInfo().GetVectorCount(), "tenant A's %q reported tenant B's vectors", p.shared)
}

// TestVectorGRPCAuthz_TenantHeaderCannotOverrideTokenTenant proves the tenant
// a request acts in is the one the authenticated token names. The live gate
// takes the tenant ONLY from the verified token (contextWithAuthorizedSubject
// stamps AuthorizedContext.TenantID); a caller-supplied tenant header naming a
// different tenant must not move the request into that tenant — neither to
// read its data nor to write into it.
func TestVectorGRPCAuthz_TenantHeaderCannotOverrideTokenTenant(t *testing.T) {
	p := newTenantPair(t)

	spoof := []string{"x-tenant-id", p.b.tenantID, "tenant-id", p.b.tenantID}

	t.Run("read with a mismatched tenant header", func(t *testing.T) {
		got, err := getVector(t, p.client, p.a, p.bOnly, p.bVectorID, spoof...)
		requireNoLeak(t, got, err, "GetVector with x-tenant-id="+p.b.tenantID)

		names := listNamespaces(t, p.client, p.a, spoof...)
		assert.False(t, containsNamespace(names, p.bOnly),
			"a mismatched tenant header exposed tenant B's namespaces: %v", names)
	})

	t.Run("write with a mismatched tenant header lands in the token's tenant", func(t *testing.T) {
		planted := uniqueName("planted")
		ctx, cancel := p.a.ctx(t, spoof...)
		_, err := p.client.CreateNamespace(ctx, &pb.CreateNamespaceRequest{Config: namespaceConfig(planted)})
		cancel()
		require.NoError(t, err, "tenant A may create in its OWN tenant")

		assert.True(t, containsNamespace(listNamespaces(t, p.client, p.a), planted),
			"the namespace must belong to the token's tenant (A)")
		assert.False(t, containsNamespace(listNamespaces(t, p.client, p.b), planted),
			"a mismatched tenant header planted a namespace in tenant B")
	})

	p.requireBIntact(t)
}

// TestVectorGRPCAuthz_ConcurrentTenantsDoNotLeak runs several tenants writing,
// reading and searching the SAME namespace name concurrently, then checks each
// tenant sees exactly its own vectors.
func TestVectorGRPCAuthz_ConcurrentTenantsDoNotLeak(t *testing.T) {
	const tenants = 4
	const perTenant = 10

	client := dialVector(t)
	ns := uniqueName("concurrent")

	// Every tenant creates the SAME namespace name; that each succeeds is
	// itself the first isolation assertion (see newTenantPair).
	principals := make([]principal, tenants)
	for i := range principals {
		principals[i] = newPrincipal(t, newTenant(t, fmt.Sprintf("vec-conc-%d", i)), fmt.Sprintf("tenant-%d", i), fullGrant()...)
		createNamespace(t, client, principals[i], ns)
	}

	// Each tenant writes vectors whose IDs are identical across tenants, so a
	// leak would surface as a foreign owner on an ID the reader also wrote.
	errs := make(chan error, tenants*perTenant*2)
	var wg sync.WaitGroup
	for i, p := range principals {
		wg.Add(1)
		go func(seed int, p principal) {
			defer wg.Done()
			for j := 0; j < perTenant; j++ {
				id := fmt.Sprintf("v-%d", j)
				ctx, cancel := p.ctx(t)
				_, err := client.UpsertVector(ctx, &pb.UpsertVectorRequest{Vector: &pb.VectorEntity{
					Id:        id,
					Namespace: ns,
					Vector:    vectorFor(seed*100 + j),
					Metadata:  map[string]string{"owner": p.tenantID},
				}})
				cancel()
				if err != nil {
					errs <- fmt.Errorf("%s upsert %s: %w", p.name, id, err)
					continue
				}
				got, gerr := getVector(t, client, p, ns, id)
				switch {
				case gerr != nil:
					errs <- fmt.Errorf("%s get %s: %w", p.name, id, gerr)
				case got == nil:
					errs <- fmt.Errorf("%s: own vector %s missing right after write", p.name, id)
				case got.GetMetadata()["owner"] != p.tenantID:
					errs <- fmt.Errorf("%s read %s owned by %q", p.name, id, got.GetMetadata()["owner"])
				}
			}
		}(i, p)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	for i, p := range principals {
		for j := 0; j < perTenant; j++ {
			id := fmt.Sprintf("v-%d", j)
			got, err := getVector(t, client, p, ns, id)
			require.NoErrorf(t, err, "%s final read %s", p.name, id)
			require.NotNilf(t, got, "%s final read %s", p.name, id)
			assert.Equalf(t, p.tenantID, got.GetMetadata()["owner"], "%s read another tenant's %s", p.name, id)
			assert.Equalf(t, vectorFor(i*100+j), got.GetVector(), "%s read another tenant's %s", p.name, id)
		}
		hits, err := searchHits(t, client, p, ns, vectorFor(i*100))
		require.NoErrorf(t, err, "%s search", p.name)
		require.NotEmptyf(t, hits, "%s search found none of its own vectors", p.name)
		assert.Emptyf(t, foreignHits(hits, p.tenantID), "%s search returned another tenant's vectors", p.name)
	}
}
