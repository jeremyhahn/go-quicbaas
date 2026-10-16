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

package grpc

import (
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// go-quicbaas#1403 — every vector RPC requires the action go-qrdb declares
// for the same operation.
//
// go-qrdb owns the vector:* vocabulary: each qrdb.v1.QRDBService Vector* RPC
// carries its permission as a (qrdb.v1.permission) method option. Where a
// quicbaas RPC has no qrdb RPC of its own (Health, UpdateNamespace, the
// scalar-index and export/import admin verbs) the grant is the one the REST
// and MCP planes use for that verb.
//
// The literals are duplicated ON PURPOSE (see
// authz_runners_forges_parity_test.go): drift in any plane fails that plane's
// own test:
//
//	REST: pkg/api/rest/routes_vector_parity_test.go
//	MCP:  pkg/api/mcp/authz_vector_parity_test.go
// =============================================================================

// vectorRPCPermissions is every VectorDB and VectorAdmin RPC and the vector
// action it requires.
var vectorRPCPermissions = map[string]string{
	"/quicbaas.vector.v1.VectorDB/CreateNamespace":    "namespace_create",
	"/quicbaas.vector.v1.VectorDB/DeleteNamespace":    "namespace_delete",
	"/quicbaas.vector.v1.VectorDB/ListNamespaces":     "namespace_list",
	"/quicbaas.vector.v1.VectorDB/GetNamespaceInfo":   "namespace_info",
	"/quicbaas.vector.v1.VectorDB/UpdateMetadata":     "update_metadata",
	"/quicbaas.vector.v1.VectorDB/SearchSimilar":      "search_similar",
	"/quicbaas.vector.v1.VectorDB/SearchByMetadata":   "search_by_metadata",
	"/quicbaas.vector.v1.VectorDB/HybridSearch":       "hybrid_search",
	"/quicbaas.vector.v1.VectorDB/UpsertVector":       "upsert",
	"/quicbaas.vector.v1.VectorDB/GetVector":          "get",
	"/quicbaas.vector.v1.VectorDB/DeleteVector":       "delete",
	"/quicbaas.vector.v1.VectorDB/BatchDeleteVectors": "batch_delete",
	"/quicbaas.vector.v1.VectorDB/BatchUpsertVectors": "batch_upsert",
	"/quicbaas.vector.v1.VectorDB/SearchByID":         "search_by_id",
	"/quicbaas.vector.v1.VectorDB/ListVectors":        "list",
	"/quicbaas.vector.v1.VectorDB/UpdateNamespace":    "namespace_create",
	"/quicbaas.vector.v1.VectorDB/GetVectorStats":     "get_index_stats",
	"/quicbaas.vector.v1.VectorDB/GetIndexProgress":   "get_rebuild_progress",
	"/quicbaas.vector.v1.VectorDB/Health":             "namespace_list",
	"/quicbaas.vector.v1.VectorDB/RebuildIndex":       "rebuild_index",

	"/quicbaas.vector.v1.VectorAdmin/CompactNamespace":      "compact",
	"/quicbaas.vector.v1.VectorAdmin/GetCompactionProgress": "get_compaction_progress",
	"/quicbaas.vector.v1.VectorAdmin/GetClusterStats":       "get_index_stats",
	"/quicbaas.vector.v1.VectorAdmin/CreateScalarIndex":     "rebuild_index",
	"/quicbaas.vector.v1.VectorAdmin/DropScalarIndex":       "rebuild_index",
	"/quicbaas.vector.v1.VectorAdmin/ListScalarIndexes":     "get_index_stats",
	"/quicbaas.vector.v1.VectorAdmin/ExportVectors":         "list",
	"/quicbaas.vector.v1.VectorAdmin/ImportVectors":         "batch_upsert",
}

// TestVectorRPCPermissionsMatchQRDB fails when a vector RPC requires any
// permission other than the one go-qrdb declares for its verb.
func TestVectorRPCPermissionsMatchQRDB(t *testing.T) {
	for method, action := range vectorRPCPermissions {
		resource, got, ok := grpcResourceAndAction(method)
		if !ok {
			t.Errorf("RPC %q has no permission entry", method)
			continue
		}
		if resource != "vector" || got != action {
			t.Errorf("RPC %q is gated %s:%s, but go-qrdb, REST and MCP gate the same verb vector:%s",
				method, resource, got, action)
		}
	}
}

// TestVectorRPCPermissionsCoverEveryVectorRPC fails when a VectorDB or
// VectorAdmin RPC exists on the wire without a pinned permission.
func TestVectorRPCPermissionsCoverEveryVectorRPC(t *testing.T) {
	descs := []grpc.ServiceDesc{pb.VectorDB_ServiceDesc, pb.VectorAdmin_ServiceDesc}
	wire := 0
	for _, desc := range descs {
		for _, name := range methodNamesOf(desc) {
			wire++
			method := "/" + desc.ServiceName + "/" + name
			if _, ok := vectorRPCPermissions[method]; !ok {
				t.Errorf("vector RPC %q has no pinned permission in vectorRPCPermissions", method)
			}
		}
	}
	if wire != len(vectorRPCPermissions) {
		t.Errorf("wire carries %d vector RPCs, pinned %d", wire, len(vectorRPCPermissions))
	}
}
