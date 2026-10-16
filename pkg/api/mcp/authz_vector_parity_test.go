//go:build mcp

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package mcp

import "testing"

// =============================================================================
// go-quicbaas#1403 — every vector tool requires the action go-qrdb declares
// for the same operation.
//
// go-qrdb owns the vector:* vocabulary: each VectorService RPC carries its
// permission as a (qrdb.v1.permission) method option. Tools resolved by the
// longest name prefix picked the wrong verb (qrdb_vector_delete_namespace
// matched qrdb_vector_delete), so every vector tool is pinned here. Where a
// tool has no qrdb RPC of its own (health, namespace update, export, import)
// the grant is the one the gRPC and REST planes use for that verb.
//
// The literals are duplicated ON PURPOSE (see
// routes_runners_forges_parity_test.go in pkg/api/rest): drift in any plane
// fails that plane's own test:
//
//	REST: pkg/api/rest/routes_vector_parity_test.go
//	gRPC: pkg/api/grpc/authz_vector_parity_test.go
// =============================================================================

// vectorToolPermissions is every vector MCP tool and the vector action it
// requires.
var vectorToolPermissions = map[string]string{
	"qrdb_vector_upsert":                            "upsert",
	"qrdb_vector_batch_upsert":                      "batch_upsert",
	"qrdb_vector_get":                               "get",
	"qrdb_vector_delete":                            "delete",
	"qrdb_vector_update_metadata":                   "update_metadata",
	"qrdb_vector_batch_delete":                      "batch_delete",
	"qrdb_vector_search_similar":                    "search_similar",
	"quicbaas_vector_search":                        "search_similar",
	"qrdb_vector_search_by_id":                      "search_by_id",
	"qrdb_vector_list":                              "list",
	"qrdb_vector_list_namespaces":                   "namespace_list",
	"qrdb_vector_create_namespace":                  "namespace_create",
	"quicbaas_vector_update_namespace":              "namespace_create",
	"qrdb_vector_delete_namespace":                  "namespace_delete",
	"qrdb_vector_get_namespace_info":                "namespace_info",
	"quicbaas_vector_get_stats":                     "get_index_stats",
	"quicbaas_vector_health":                        "namespace_list",
	"qrdb_vector_rebuild_index":                     "rebuild_index",
	"quicbaas_vector_get_index_progress":            "get_rebuild_progress",
	"quicbaas_vector_admin_get_cluster_stats":       "get_index_stats",
	"quicbaas_vector_admin_compact_namespace":       "compact",
	"quicbaas_vector_admin_get_compaction_progress": "get_compaction_progress",
	"quicbaas_vector_admin_export_vectors":          "list",
	"quicbaas_vector_admin_import_vectors":          "batch_upsert",
}

// TestVectorToolPermissionsMatchQRDB fails when a vector tool resolves to
// any permission other than the one go-qrdb declares for its verb.
func TestVectorToolPermissionsMatchQRDB(t *testing.T) {
	for tool, action := range vectorToolPermissions {
		resource, got, ok := getToolQRDBPermission(tool)
		if !ok {
			t.Errorf("tool %q resolves to no permission", tool)
			continue
		}
		if resource != "vector" || got != action {
			t.Errorf("tool %q is gated %s:%s, but go-qrdb, gRPC and REST gate the same verb vector:%s",
				tool, resource, got, action)
		}
	}
}

// TestVectorToolPermissionsCoverEveryVectorTool fails when a vector tool is
// registered without a pinned permission, so a new tool cannot inherit a
// prefix match unnoticed.
func TestVectorToolPermissionsCoverEveryVectorTool(t *testing.T) {
	registry := &ToolRegistry{}
	registry.registerVectorTools()
	for _, tool := range registry.tools {
		if _, ok := vectorToolPermissions[tool.Name]; !ok {
			t.Errorf("vector tool %q has no pinned permission in vectorToolPermissions", tool.Name)
		}
	}
	if len(registry.tools) != len(vectorToolPermissions) {
		t.Errorf("registered %d vector tools, pinned %d", len(registry.tools), len(vectorToolPermissions))
	}
}
