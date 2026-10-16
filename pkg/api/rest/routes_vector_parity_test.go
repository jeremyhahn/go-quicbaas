//go:build rest

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

package rest

import (
	"strings"
	"testing"
)

// =============================================================================
// go-quicbaas#1403 — every vector route requires the action go-qrdb declares
// for the same operation.
//
// go-qrdb owns the vector:* vocabulary: each VectorService RPC carries its
// permission as a (qrdb.v1.permission) method option. A per-vector delete is
// vector:delete, not vector:namespace_delete; reading one vector is
// vector:get, not vector:namespace_list. Where a REST verb has no qrdb RPC of
// its own (health, namespace update, export, import) the grant is the one the
// gRPC and MCP planes already use for that verb.
//
// The literals are duplicated ON PURPOSE (see
// routes_runners_forges_parity_test.go): drift in any plane fails that
// plane's own test:
//
//	gRPC: pkg/api/grpc/authz_vector_parity_test.go
//	MCP:  pkg/api/mcp/authz_vector_parity_test.go
// =============================================================================

// vectorRoutePermissions is every /api/v1/vector route and the permission it
// requires.
var vectorRoutePermissions = map[string]string{
	"DELETE /api/v1/vector/namespaces/{namespace}":                    "namespace_delete",
	"DELETE /api/v1/vector/namespaces/{namespace}/vectors/batch":      "batch_delete",
	"DELETE /api/v1/vector/namespaces/{namespace}/vectors/{id}":       "delete",
	"GET /api/v1/vector/admin/compact/progress":                       "get_compaction_progress",
	"GET /api/v1/vector/admin/stats/global":                           "get_index_stats",
	"GET /api/v1/vector/cluster/stats":                                "get_index_stats",
	"GET /api/v1/vector/health":                                       "namespace_list",
	"GET /api/v1/vector/namespaces/":                                  "namespace_list",
	"GET /api/v1/vector/namespaces/{namespace}":                       "namespace_info",
	"GET /api/v1/vector/namespaces/{namespace}/index/progress":        "get_rebuild_progress",
	"GET /api/v1/vector/namespaces/{namespace}/stats":                 "get_index_stats",
	"GET /api/v1/vector/namespaces/{namespace}/vectors/":              "list",
	"GET /api/v1/vector/namespaces/{namespace}/vectors/{id}":          "get",
	"POST /api/v1/vector/admin/compact":                               "compact",
	"POST /api/v1/vector/admin/export/{namespace}":                    "list",
	"POST /api/v1/vector/admin/import/{namespace}":                    "batch_upsert",
	"POST /api/v1/vector/hybrid-search":                               "hybrid_search",
	"POST /api/v1/vector/namespaces/":                                 "namespace_create",
	"POST /api/v1/vector/namespaces/{namespace}/index/rebuild":        "rebuild_index",
	"POST /api/v1/vector/namespaces/{namespace}/search/by-id/{id}":    "search_by_id",
	"POST /api/v1/vector/namespaces/{namespace}/search/similar":       "search_similar",
	"POST /api/v1/vector/namespaces/{namespace}/vectors/":             "upsert",
	"POST /api/v1/vector/namespaces/{namespace}/vectors/batch":        "batch_upsert",
	"POST /api/v1/vector/search-by-metadata":                          "search_by_metadata",
	"PUT /api/v1/vector/namespaces/{namespace}":                       "namespace_create",
	"PUT /api/v1/vector/namespaces/{namespace}/vectors/{id}":          "upsert",
	"PUT /api/v1/vector/namespaces/{namespace}/vectors/{id}/metadata": "update_metadata",
}

// TestVectorRoutePermissionsMatchQRDB pins each vector route to its
// vector:<action> grant.
func TestVectorRoutePermissionsMatchQRDB(t *testing.T) {
	for key, action := range vectorRoutePermissions {
		want := frozenPermission{auth: RouteAuthPermission, resource: "vector", action: action}
		got, ok := frozenRoutePermissions[key]
		if !ok {
			t.Errorf("route %q has no frozen permission entry", key)
			continue
		}
		if got != want {
			t.Errorf("route %q is gated %s:%s, but go-qrdb, gRPC and MCP gate the same verb vector:%s",
				key, got.resource, got.action, action)
		}
	}
}

// TestVectorRoutePermissionsCoverEveryVectorRoute fails when a vector route
// is added to the table without being pinned above.
func TestVectorRoutePermissionsCoverEveryVectorRoute(t *testing.T) {
	for key := range frozenRoutePermissions {
		_, path, found := strings.Cut(key, " ")
		if !found || !strings.HasPrefix(path, "/api/v1/vector/") {
			continue
		}
		if _, pinned := vectorRoutePermissions[key]; !pinned {
			t.Errorf("vector route %q is not pinned in vectorRoutePermissions", key)
		}
	}
}
