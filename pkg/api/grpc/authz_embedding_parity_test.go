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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

const embeddingServiceMethodPrefix = "/quicbaas.embedding.v1.EmbeddingService/"

// embeddingGRPCGrants is the permission each EmbeddingService RPC requires.
// It is the same verb the REST rule for the matching /api/v1/embedding route
// (pkg/api/rest/testdata/route_permissions.golden) and the matching MCP tool
// (pkg/api/mcp/authz_qrdb.go) require, so the transports agree.
var embeddingGRPCGrants = []struct {
	method string
	perm   string
	other  string // the sibling embedding verb, which must not admit the call
}{
	{"EmbeddingGenerate", "embedding:embed", "embedding:list_models"},           // POST /generate
	{"EmbeddingBatchGenerate", "embedding:embed", "embedding:list_models"},      // POST /batch
	{"EmbeddingClearCache", "embedding:embed", "embedding:list_models"},         // DELETE /cache
	{"EmbeddingListProviders", "embedding:list_models", "embedding:embed"},      // GET /providers
	{"EmbeddingGetProviderInfo", "embedding:list_models", "embedding:embed"},    // GET /providers/{provider}
	{"EmbeddingSetDefaultProvider", "embedding:list_models", "embedding:embed"}, // PUT /providers/default
	{"EmbeddingGetCacheStats", "embedding:list_models", "embedding:embed"},      // GET /cache/stats
}

// TestEmbeddingGRPCPermissions_CoverEveryRPC pins the grant table to the
// generated service: a new EmbeddingService RPC must be added here, with the
// verb its REST and MCP counterparts use.
func TestEmbeddingGRPCPermissions_CoverEveryRPC(t *testing.T) {
	methods := map[string]bool{}
	for _, m := range pb.EmbeddingService_ServiceDesc.Methods {
		methods[m.MethodName] = true
	}
	require.Len(t, embeddingGRPCGrants, len(methods))
	for _, c := range embeddingGRPCGrants {
		assert.True(t, methods[c.method], "%s is not an EmbeddingService RPC", c.method)
	}
}

func TestUnaryAuthorizationInterceptor_EmbeddingGrant(t *testing.T) {
	for _, c := range embeddingGRPCGrants {
		fullMethod := embeddingServiceMethodPrefix + c.method

		t.Run(c.method+"_admits_"+c.perm, func(t *testing.T) {
			authorizer := &fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: c.perm,
				authzCtx: &platformtoken.AuthorizedContext{
					Subject:  "u1",
					TenantID: "automatethethings",
					Scopes:   []string{c.perm},
				},
			}
			interceptor := UnaryAuthorizationInterceptor(authorizer)

			resp, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: fullMethod},
				func(context.Context, interface{}) (interface{}, error) { return "ok", nil })
			require.NoError(t, err, fullMethod)
			assert.Equal(t, "ok", resp)
			assert.Equal(t, c.perm, authorizer.lastPerm, "required permission for %s", fullMethod)
		})

		for _, granted := range []string{c.other, ""} {
			t.Run(c.method+"_denies_grant_"+granted, func(t *testing.T) {
				authorizer := &fakeValidatorAuthorizer{
					token:       "good",
					grantedPerm: granted,
					authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "automatethethings"},
				}
				interceptor := UnaryAuthorizationInterceptor(authorizer)

				_, err := interceptor(ctxWithBearer("good"), nil,
					&grpc.UnaryServerInfo{FullMethod: fullMethod},
					func(context.Context, interface{}) (interface{}, error) {
						t.Errorf("handler ran for %s on grant %q; it requires %s", fullMethod, granted, c.perm)
						return nil, nil
					})
				require.Error(t, err)
				assert.Equal(t, codes.PermissionDenied, status.Code(err), "%s on grant %q", fullMethod, granted)
			})
		}
	}
}
