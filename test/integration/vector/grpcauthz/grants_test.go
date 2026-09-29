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
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// Grant tiers, named for the read / write / admin split.
const (
	tierRead  = "read"
	tierWrite = "write"
	tierAdmin = "admin"
)

// grantEnv is one tenant with a namespace a fully-granted seeder populated.
// Every verb acts inside this tenant, so a denial can only come from the grant.
type grantEnv struct {
	client pb.VectorDBClient
	seeder principal
	ns     string
}

// seedID is the vector every read verb targets.
const seedID = "seed"

// newGrantEnv provisions the tenant, its seeder and the seeded namespace.
func newGrantEnv(t *testing.T) (*grantEnv, string) {
	t.Helper()
	client := dialVector(t)
	tenantID := newTenant(t, "vec-grant")
	env := &grantEnv{
		client: client,
		seeder: newPrincipal(t, tenantID, "seeder", fullGrant()...),
		ns:     uniqueName("grants"),
	}
	createNamespace(t, client, env.seeder, env.ns)
	upsert(t, client, env.seeder, env.ns, seedID, vectorFor(1))
	return env, tenantID
}

// seedVector writes a fresh vector as the seeder and returns its ID.
func (e *grantEnv) seedVector(t *testing.T, prefix string) string {
	t.Helper()
	id := uniqueName(prefix)
	upsert(t, e.client, e.seeder, e.ns, id, vectorFor(2))
	return id
}

// seederSees reports whether the seeder can read id in the seeded namespace.
func (e *grantEnv) seederSees(t *testing.T, id string) *pb.VectorEntity {
	t.Helper()
	got, err := getVector(t, e.client, e.seeder, e.ns, id)
	require.NoErrorf(t, err, "seeder reads %s", id)
	return got
}

// seederHasNamespace reports whether the seeder lists name.
func (e *grantEnv) seederHasNamespace(t *testing.T, name string) bool {
	t.Helper()
	return containsNamespace(listNamespaces(t, e.client, e.seeder), name)
}

// drain reads a server stream to its end, returning the first non-EOF error.
// For a server-streaming RPC the authorization decision surfaces here.
func drain[T any](stream grpc.ServerStreamingClient[T], err error) error {
	if err != nil {
		return err
	}
	for {
		if _, rerr := stream.Recv(); rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return nil
			}
			return rerr
		}
	}
}

// verbCase is one vector verb under test.
type verbCase struct {
	name string
	tier string

	// exec seeds whatever the verb acts on (as the seeder), invokes the verb
	// as p, and returns the call error plus, for mutating verbs, a probe that
	// reports whether the verb's effect is visible to the seeder afterwards.
	exec func(t *testing.T, env *grantEnv, p principal) (applied func() bool, err error)
}

// vectorVerbs covers every tier with unary and server-streaming RPCs, so both
// the unary and the stream interceptor are on the line.
func vectorVerbs() []verbCase {
	return []verbCase{
		{name: "GetVector", tier: tierRead, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			_, err := getVector(t, env.client, p, env.ns, seedID)
			return nil, err
		}},
		{name: "ListVectors (stream)", tier: tierRead, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			ctx, cancel := p.ctx(t)
			defer cancel()
			return nil, drain(env.client.ListVectors(ctx, &pb.ListVectorsRequest{Namespace: env.ns}))
		}},
		{name: "SearchSimilar (stream)", tier: tierRead, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			_, err := searchHits(t, env.client, p, env.ns, vectorFor(1))
			return nil, err
		}},
		{name: "SearchByID (stream)", tier: tierRead, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			ctx, cancel := p.ctx(t)
			defer cancel()
			return nil, drain(env.client.SearchByID(ctx, &pb.SearchByIDRequest{Namespace: env.ns, Id: seedID, K: 3}))
		}},
		{name: "ListNamespaces", tier: tierRead, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.ListNamespaces(ctx, &pb.ListNamespacesRequest{})
			return nil, err
		}},
		{name: "GetNamespaceInfo", tier: tierRead, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.GetNamespaceInfo(ctx, &pb.GetNamespaceInfoRequest{Namespace: env.ns})
			return nil, err
		}},
		{name: "UpsertVector", tier: tierWrite, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			id := uniqueName("upsert")
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.UpsertVector(ctx, &pb.UpsertVectorRequest{Vector: &pb.VectorEntity{
				Id: id, Namespace: env.ns, Vector: vectorFor(3),
			}})
			return func() bool { return env.seederSees(t, id) != nil }, err
		}},
		{name: "BatchUpsertVectors (stream)", tier: tierWrite, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			first, second := uniqueName("batch"), uniqueName("batch")
			ctx, cancel := p.ctx(t)
			defer cancel()
			err := drain(env.client.BatchUpsertVectors(ctx, &pb.BatchUpsertVectorsRequest{Vectors: []*pb.VectorEntity{
				{Id: first, Namespace: env.ns, Vector: vectorFor(4)},
				{Id: second, Namespace: env.ns, Vector: vectorFor(5)},
			}}))
			return func() bool { return env.seederSees(t, first) != nil && env.seederSees(t, second) != nil }, err
		}},
		{name: "UpdateMetadata", tier: tierWrite, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			id := env.seedVector(t, "meta")
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.UpdateMetadata(ctx, &pb.UpdateMetadataRequest{
				Namespace: env.ns, Id: id, Metadata: map[string]string{"touched": "yes"},
			})
			return func() bool { return env.seederSees(t, id).GetMetadata()["touched"] == "yes" }, err
		}},
		{name: "DeleteVector", tier: tierWrite, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			id := env.seedVector(t, "delete")
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.DeleteVector(ctx, &pb.DeleteVectorRequest{Namespace: env.ns, Id: id})
			return func() bool { return env.seederSees(t, id) == nil }, err
		}},
		{name: "CreateNamespace", tier: tierAdmin, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			name := uniqueName("created")
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.CreateNamespace(ctx, &pb.CreateNamespaceRequest{Config: namespaceConfig(name)})
			return func() bool { return env.seederHasNamespace(t, name) }, err
		}},
		{name: "DeleteNamespace", tier: tierAdmin, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			name := uniqueName("doomed")
			createNamespace(t, env.client, env.seeder, name)
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.DeleteNamespace(ctx, &pb.DeleteNamespaceRequest{Namespace: name, Force: true})
			return func() bool { return !env.seederHasNamespace(t, name) }, err
		}},
		{name: "RebuildIndex", tier: tierAdmin, exec: func(t *testing.T, env *grantEnv, p principal) (func() bool, error) {
			// A namespace of its own: an admitted asynchronous rebuild must
			// not leave the shared namespace mid-rebuild for the verbs that
			// follow it.
			name := uniqueName("rebuild")
			createNamespace(t, env.client, env.seeder, name)
			upsert(t, env.client, env.seeder, name, seedID, vectorFor(6))
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.RebuildIndex(ctx, &pb.RebuildIndexRequest{Namespace: name, Async: true})
			return nil, err
		}},
	}
}

// grantHolder is a principal and the tiers its grant covers.
type grantHolder struct {
	label  string
	scopes []string
	tiers  map[string]bool
}

// TestVectorGRPCAuthz_GrantsAreEnforcedPerVerb proves each grant tier admits
// exactly its own verbs and that every other verb is denied at the gate with
// PermissionDenied and leaves no effect behind. It also proves two grants
// that name no live vector permission — an unrelated one, and the coarse
// vector:read / vector:write / vector:admin names the deleted interceptor
// honoured — confer nothing.
func TestVectorGRPCAuthz_GrantsAreEnforcedPerVerb(t *testing.T) {
	env, tenantID := newGrantEnv(t)

	holders := []grantHolder{
		{label: "read grant", scopes: readGrant, tiers: map[string]bool{tierRead: true}},
		{label: "write grant", scopes: writeGrant, tiers: map[string]bool{tierWrite: true}},
		{label: "admin grant", scopes: adminGrant, tiers: map[string]bool{tierAdmin: true}},
		{label: "full grant", scopes: fullGrant(), tiers: map[string]bool{tierRead: true, tierWrite: true, tierAdmin: true}},
		{label: "unrelated grant", scopes: []string{"keys:get"}, tiers: map[string]bool{}},
		{label: "coarse legacy grant", scopes: []string{"vector:read", "vector:write", "vector:admin"}, tiers: map[string]bool{}},
	}

	for _, h := range holders {
		p := newPrincipal(t, tenantID, h.label, h.scopes...)
		t.Run(h.label, func(t *testing.T) {
			for _, v := range vectorVerbs() {
				t.Run(v.name, func(t *testing.T) {
					applied, err := v.exec(t, env, p)
					if h.tiers[v.tier] {
						require.NoErrorf(t, err, "%s must be allowed %s (%s tier)", h.label, v.name, v.tier)
						if applied != nil {
							require.Truef(t, applied(), "%s: allowed %s had no effect", h.label, v.name)
						}
						return
					}
					requireCode(t, err, codes.PermissionDenied, "%s must be denied %s (%s tier)", h.label, v.name, v.tier)
					if applied != nil {
						require.Falsef(t, applied(), "%s: DENIED %s still took effect", h.label, v.name)
					}
				})
			}
		})
	}
}

// TestVectorGRPCAuthz_UnauthenticatedCallsAreRefused proves a call carrying no
// valid platform token is refused with Unauthenticated on both the unary and
// the stream path, and that a refused write leaves nothing behind.
func TestVectorGRPCAuthz_UnauthenticatedCallsAreRefused(t *testing.T) {
	env, tenantID := newGrantEnv(t)

	valid := newPrincipal(t, tenantID, "revoked", fullGrant()...)
	provider, err := iamPlane(t).GetOIDCProvider(t.Context())
	require.NoError(t, err)
	revokeCtx, cancel := systemCtx(t)
	require.NoError(t, provider.Revoke(revokeCtx, &iam.RevokeRequest{
		Token:        valid.bearer,
		ClientID:     valid.clientID,
		ClientSecret: valid.clientSecret,
	}), "revoke the bearer")
	cancel()

	callers := map[string]principal{
		// A bearer the provider signed, then revoked: the per-tenant
		// liveness check must refuse it even though its signature is valid.
		"revoked token": valid,
		// Not a JWT at all.
		"malformed token": {name: "malformed", tenantID: tenantID, bearer: "not-a-platform-token"},
		// A real token with its signature altered.
		"tampered token": {name: "tampered", tenantID: tenantID, bearer: tamper(newPrincipal(t, tenantID, "tampered", fullGrant()...).bearer)},
	}

	t.Run("no token", func(t *testing.T) {
		ctx, cancel := anonymousCtx(t)
		defer cancel()
		id := uniqueName("anon")
		_, err := env.client.UpsertVector(ctx, &pb.UpsertVectorRequest{Vector: &pb.VectorEntity{
			Id: id, Namespace: env.ns, Vector: vectorFor(9),
		}})
		requireCode(t, err, codes.Unauthenticated, "unary call without a token")
		require.Nil(t, env.seederSees(t, id), "a refused anonymous upsert took effect")

		sctx, scancel := anonymousCtx(t)
		defer scancel()
		serr := drain(env.client.SearchSimilar(sctx, &pb.SearchSimilarRequest{Namespace: env.ns, QueryVector: vectorFor(1), K: 1}))
		requireCode(t, serr, codes.Unauthenticated, "stream call without a token")
	})

	for label, p := range callers {
		t.Run(label, func(t *testing.T) {
			id := uniqueName("bad")
			ctx, cancel := p.ctx(t)
			defer cancel()
			_, err := env.client.UpsertVector(ctx, &pb.UpsertVectorRequest{Vector: &pb.VectorEntity{
				Id: id, Namespace: env.ns, Vector: vectorFor(9),
			}})
			requireCode(t, err, codes.Unauthenticated, "unary call with a %s", label)
			require.Nil(t, env.seederSees(t, id), "a refused upsert with a %s took effect", label)

			_, serr := searchHits(t, env.client, p, env.ns, vectorFor(1))
			requireCode(t, serr, codes.Unauthenticated, "stream call with a %s", label)
		})
	}
}

// tamper flips one character in a JWT's signature segment.
func tamper(jwt string) string {
	b := []byte(jwt)
	last := len(b) - 2
	if b[last] == 'A' {
		b[last] = 'B'
	} else {
		b[last] = 'A'
	}
	return string(b)
}
