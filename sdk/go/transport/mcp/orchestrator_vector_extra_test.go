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

// orchestrator_vector_extra_test.go covers the Orchestrator inference response
// handler and the Vector handler's List / UpdateMetadata / GetStats /
// SearchByMetadata transforms that are not exercised elsewhere.

package mcp

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// Orchestrator
// =============================================================================

func TestOrchestratorParseInferenceResponse(t *testing.T) {
	h := newOrchestratorMCPHandler()

	t.Run("from map", func(t *testing.T) {
		out, err := h.transformResponse("Inference", map[string]interface{}{
			"model":       "claude",
			"stop_reason": "end_turn",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": "hi"},
			},
		})
		if err != nil {
			t.Fatalf("transformResponse: %v", err)
		}
		resp := out.(*orchestrator.InferenceResponse)
		if resp.Model != "claude" || resp.StopReason != "end_turn" || len(resp.Content) != 1 {
			t.Errorf("inference mismatch: %+v", resp)
		}
	})

	t.Run("from JSON string", func(t *testing.T) {
		out, err := h.transformResponse("Inference", `{"model":"m","content":[{"type":"text","text":"x"}]}`)
		if err != nil {
			t.Fatalf("transformResponse: %v", err)
		}
		if out.(*orchestrator.InferenceResponse).Model != "m" {
			t.Error("model not decoded from JSON string")
		}
	})

	t.Run("nil result errors", func(t *testing.T) {
		if _, err := h.transformResponse("Inference", nil); err == nil {
			t.Error("nil inference result must error")
		}
	})

	t.Run("malformed string errors", func(t *testing.T) {
		if _, err := h.transformResponse("Inference", "not json"); err == nil {
			t.Error("malformed inference JSON must error")
		}
	})

	t.Run("other method passes through", func(t *testing.T) {
		out, err := h.transformResponse("Other", "raw")
		if err != nil {
			t.Fatalf("passthrough: %v", err)
		}
		if out != "raw" {
			t.Errorf("passthrough = %v", out)
		}
	})
}

func TestIsMCPOrchestratorService(t *testing.T) {
	if !isMCPOrchestratorService("quicbaas.Orchestrator") {
		t.Error("Orchestrator detection failed")
	}
	if isMCPOrchestratorService("quicbaas.Vector") {
		t.Error("Vector must not be Orchestrator")
	}
}

// =============================================================================
// Vector
// =============================================================================

func newVecHandler(t *testing.T) *vectorHandler {
	t.Helper()
	tr, err := New(&transport.Config{Address: "http://localhost:8080"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tr.Close() })
	return newVectorHandler(tr)
}

func TestVectorListTransform(t *testing.T) {
	h := newVecHandler(t)
	args, err := h.transformRequest("List", &vector.ListVectorsRequest{
		Namespace: "ns", PageSize: 50, PageToken: "tok",
		IncludeVectors: true, IncludeMetadata: true})
	if err != nil {
		t.Fatalf("List transform: %v", err)
	}
	if args["namespace"] != "ns" || args["page_size"] != int32(50) || args["page_token"] != "tok" {
		t.Errorf("list args = %v", args)
	}

	out, err := h.transformResponse("List", map[string]interface{}{
		"vectors": []interface{}{
			map[string]interface{}{"id": "v1"},
		},
		"total_count":     float64(1),
		"next_page_token": "next",
	})
	if err != nil {
		t.Fatalf("List response: %v", err)
	}
	resp := out.(*vector.ListVectorsResponse)
	if resp.TotalCount != 1 || resp.NextPageToken != "next" || len(resp.Vectors) != 1 {
		t.Errorf("list response mismatch: %+v", resp)
	}
}

func TestVectorUpdateMetadataTransform(t *testing.T) {
	h := newVecHandler(t)
	args, err := h.transformRequest("UpdateMetadata", &vector.UpdateMetadataRequest{
		Namespace: "ns", ID: "v1", Metadata: map[string]string{"k": "v"}, Merge: true})
	if err != nil {
		t.Fatalf("UpdateMetadata transform: %v", err)
	}
	if args["namespace"] != "ns" || args["id"] != "v1" || args["merge"] != true {
		t.Errorf("update args = %v", args)
	}

	out, err := h.transformResponse("UpdateMetadata", map[string]interface{}{"version": float64(3)})
	if err != nil {
		t.Fatalf("UpdateMetadata response: %v", err)
	}
	resp := out.(*vector.UpdateMetadataResponse)
	if !resp.Success || resp.Version != 3 {
		t.Errorf("update response mismatch: %+v", resp)
	}
}

func TestVectorGetStatsTransform(t *testing.T) {
	h := newVecHandler(t)
	args, err := h.transformRequest("GetStats", &vector.GetStatsRequest{Namespace: "ns"})
	if err != nil {
		t.Fatalf("GetStats transform: %v", err)
	}
	if args["namespace"] != "ns" {
		t.Errorf("stats args = %v", args)
	}

	out, err := h.transformResponse("GetStats", map[string]interface{}{
		"namespace": "ns", "total_vectors": float64(10),
		"total_index_bytes": float64(100), "total_storage_bytes": float64(200)})
	if err != nil {
		t.Fatalf("GetStats response: %v", err)
	}
	resp := out.(*vector.GetStatsResponse)
	if resp.VectorCount != 10 || resp.IndexSizeBytes != 100 || resp.StorageSizeBytes != 200 {
		t.Errorf("stats response mismatch: %+v", resp)
	}
}

func TestVectorSearchByMetadataTransform(t *testing.T) {
	h := newVecHandler(t)
	args, err := h.transformRequest("SearchByMetadata", &vector.SearchByMetadataRequest{
		Namespace: "ns",
		MetadataFilters: []*vector.MetadataFilter{
			{Key: "env", Operator: vector.FilterOperator("eq"), Value: "prod"},
		},
		Limit: 10, Offset: 5, IncludeVectors: true, IncludeMetadata: true,
	})
	if err != nil {
		t.Fatalf("SearchByMetadata transform: %v", err)
	}
	filters, ok := args["metadata_filters"].([]map[string]interface{})
	if !ok || len(filters) != 1 || filters[0]["key"] != "env" {
		t.Errorf("metadata_filters = %v", args["metadata_filters"])
	}
	if args["limit"] != int32(10) || args["offset"] != int32(5) {
		t.Errorf("limit/offset = %v / %v", args["limit"], args["offset"])
	}
}

func TestVectorTransformErrors(t *testing.T) {
	h := newVecHandler(t)
	// Invalid request types.
	for _, m := range []string{"List", "UpdateMetadata", "GetStats", "SearchByMetadata"} {
		if _, err := h.transformRequest(m, "wrong"); err == nil {
			t.Errorf("%s with wrong type must error", m)
		}
	}
	// Non-map (and non-nil) responses are rejected.
	for _, m := range []string{"List", "UpdateMetadata", "GetStats"} {
		if _, err := h.transformResponse(m, "scalar"); err == nil {
			t.Errorf("%s on scalar must error", m)
		}
	}
}

func TestVectorTransformNilResponses(t *testing.T) {
	h := newVecHandler(t)
	if r, err := h.transformResponse("List", nil); err != nil || r == nil {
		t.Errorf("List(nil) = %v, %v", r, err)
	}
	if r, err := h.transformResponse("UpdateMetadata", nil); err != nil || r != nil {
		t.Errorf("UpdateMetadata(nil) = %v, %v", r, err)
	}
	if r, err := h.transformResponse("GetStats", nil); err != nil || r != nil {
		t.Errorf("GetStats(nil) = %v, %v", r, err)
	}
}

func TestVectorMcpHelpers(t *testing.T) {
	m := map[string]interface{}{"s": "x", "b": true, "i": float64(7), "f": float64(1.5)}
	if mcpGetString(m, "s") != "x" || mcpGetString(m, "missing") != "" {
		t.Error("mcpGetString failed")
	}
	if !mcpGetBool(m, "b") || mcpGetBool(m, "missing") {
		t.Error("mcpGetBool failed")
	}
	if mcpGetInt64(m, "i") != 7 || mcpGetInt64(m, "missing") != 0 {
		t.Error("mcpGetInt64 failed")
	}
	if mcpGetFloat32(m, "f") != 1.5 || mcpGetFloat32(m, "missing") != 0 {
		t.Error("mcpGetFloat32 failed")
	}
	if interfaceToStringMap(map[string]interface{}{"k": "v", "n": 1})["k"] != "v" {
		t.Error("interfaceToStringMap failed")
	}
	if stringMapToInterface(nil) != nil {
		t.Error("stringMapToInterface(nil) should be nil")
	}
}

// TestVectorRequestTransformsPartial exercises the remaining request transforms
// that the primary vector test only partially covers.
func TestVectorRequestTransformsPartial(t *testing.T) {
	h := newVecHandler(t)

	nsArgs, err := h.transformRequest("ListNamespaces", &vector.ListNamespacesRequest{
		Prefix: "p", PageSize: 10, PageToken: "tok"})
	if err != nil {
		t.Fatalf("ListNamespaces: %v", err)
	}
	if nsArgs["prefix"] != "p" || nsArgs["page_size"] != int32(10) || nsArgs["page_token"] != "tok" {
		t.Errorf("ListNamespaces args = %v", nsArgs)
	}

	buArgs, err := h.transformRequest("BatchUpsert", &vector.BatchUpsertRequest{
		Vectors: []*vector.VectorEntity{
			{ID: "v1", Namespace: "ns", Vector: []float32{0.1}, Metadata: map[string]string{"k": "v"}},
		},
	})
	if err != nil {
		t.Fatalf("BatchUpsert: %v", err)
	}
	if vecs, ok := buArgs["vectors"].([]map[string]interface{}); !ok || len(vecs) != 1 {
		t.Errorf("BatchUpsert vectors = %v", buArgs["vectors"])
	}

	bdArgs, err := h.transformRequest("BatchDelete", &vector.BatchDeleteRequest{
		Namespace: "ns", IDs: []string{"a", "b"}})
	if err != nil {
		t.Fatalf("BatchDelete: %v", err)
	}
	if bdArgs["ids"] != `["a","b"]` {
		t.Errorf("BatchDelete ids = %v", bdArgs["ids"])
	}

	riArgs, err := h.transformRequest("RebuildIndex", &vector.RebuildIndexRequest{
		Namespace: "ns", Async: true, IndexParams: map[string]string{"m": "16"}})
	if err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	if riArgs["namespace"] != "ns" || riArgs["async"] != true {
		t.Errorf("RebuildIndex args = %v", riArgs)
	}
}

// TestVectorResponseTransformsBranches drives every vector response transform
// through its nil, non-map (error), and happy-path branches.
func TestVectorResponseTransformsBranches(t *testing.T) {
	h := newVecHandler(t)

	// Methods whose nil response returns a non-nil zero value.
	nonNilNil := map[string]bool{
		"ListNamespaces": true, "List": true, "Get": true,
		"SearchSimilar": true, "SearchByID": true, "HybridSearch": true,
		"SearchByMetadata": true, "CreateNamespace": false,
	}
	methods := []string{
		"CreateNamespace", "DeleteNamespace", "GetNamespaceInfo",
		"Upsert", "Get", "Delete", "BatchUpsert", "BatchDelete",
		"SearchSimilar", "SearchByID", "HybridSearch", "SearchByMetadata",
		"RebuildIndex", "GetIndexProgress", "Compact", "GetCompactionProgress",
		"UpdateMetadata", "GetStats",
	}
	for _, m := range methods {
		// nil branch should not error.
		if _, err := h.transformResponse(m, nil); err != nil {
			t.Errorf("%s(nil) error: %v", m, err)
		}
		// non-map scalar: DeleteNamespace round-trips via JSON so it does not
		// error on a scalar; the rest reject it.
		if m == "DeleteNamespace" {
			continue
		}
		if _, err := h.transformResponse(m, "scalar"); err == nil {
			t.Errorf("%s(scalar) must error", m)
		}
		// happy-path map.
		if _, err := h.transformResponse(m, map[string]interface{}{"timestamp": "2026-05-29T00:00:00Z"}); err != nil {
			t.Errorf("%s(map) error: %v", m, err)
		}
		_ = nonNilNil
	}
}

// TestVectorDeleteNamespaceResponseFallback drives the JSON round-trip branch
// (non-map input) and the explicit "deleted" flag of the delete-namespace
// response transform.
func TestVectorDeleteNamespaceResponseFallback(t *testing.T) {
	h := newVecHandler(t)

	type dnJSON struct {
		Deleted        bool  `json:"deleted"`
		VectorsDeleted int64 `json:"vectors_deleted"`
	}
	out, err := h.transformResponse("DeleteNamespace", dnJSON{Deleted: true, VectorsDeleted: 5})
	if err != nil {
		t.Fatalf("DeleteNamespace fallback: %v", err)
	}
	resp := out.(*vector.DeleteNamespaceResponse)
	if !resp.Deleted || resp.VectorsDeleted != 5 {
		t.Errorf("delete namespace fallback = %+v", resp)
	}

	// Explicit deleted:false is honored over the default true.
	out2, err := h.transformResponse("DeleteNamespace", map[string]interface{}{"deleted": false})
	if err != nil {
		t.Fatalf("DeleteNamespace map: %v", err)
	}
	if out2.(*vector.DeleteNamespaceResponse).Deleted {
		t.Error("explicit deleted:false should be honored")
	}

	// An unmarshalable value errors.
	if _, err := h.transformResponse("DeleteNamespace", make(chan int)); err == nil {
		t.Error("unmarshalable DeleteNamespace response must error")
	}
}
