// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

// richNamespaceJSON is a fully-populated namespace response used to exercise
// the parseNamespaceInfo / vectorGetStringMap / vectorGetTime helpers.
const richNamespaceJSON = `{
	"namespace": "docs",
	"dimensions": 128,
	"index_type": "hnsw",
	"index_params": {"m": "16"},
	"metadata": {"team": "search"},
	"vector_count": 5,
	"index_bytes": 1024,
	"storage_bytes": 4096,
	"created_at": "2025-01-01T00:00:00Z",
	"updated_at": "2025-01-02T00:00:00Z",
	"timestamp": "2025-01-02T00:00:00Z"
}`

// richSearchJSON exercises parseSearchResults / parseVectorEntity.
const richSearchJSON = `{
	"results": [
		{
			"score": 0.9,
			"distance": 0.1,
			"rank": 1,
			"vector": {
				"id": "v1",
				"namespace": "docs",
				"version": 3,
				"vector": [0.1, 0.2, 0.3],
				"metadata": {"k": "v"},
				"created_at": "2025-01-01T00:00:00Z"
			}
		}
	],
	"total_candidates": 10,
	"search_time_ms": 5,
	"timestamp": "2025-01-02T00:00:00Z"
}`

// TestVectorHandlerHappyPaths drives every vector method against a recording
// server, asserting the HTTP method and path each handler builds and that the
// typed response is decoded.
func TestVectorHandlerHappyPaths(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/vector/namespaces" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"namespaces":[` + richNamespaceJSON + `],"total_count":1,"timestamp":"2025-01-02T00:00:00Z"}`))
		case r.URL.Path == "/api/v1/vector/namespaces/docs" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(richNamespaceJSON))
		case r.URL.Path == "/api/v1/vector/namespaces/docs/vectors/v1" && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"v1","namespace":"docs","version":3,"vector":[0.1,0.2],"metadata":{"k":"v"}}`))
		case r.URL.Path == "/api/v1/vector/hybrid-search",
			r.URL.Path == "/api/v1/vector/search-by-metadata":
			_, _ = w.Write([]byte(richSearchJSON))
		default:
			_, _ = w.Write([]byte(`{"deleted":true,"id":"v1","version":3,"vectors_succeeded":2,"deleted_count":2,` +
				`"operation_id":"op1","status":"running","results":[],"timestamp":"2025-01-02T00:00:00Z"}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
	}{
		{"CreateNamespace", "CreateNamespace",
			&vector.CreateNamespaceRequest{Config: &vector.NamespaceConfig{
				Namespace: "docs", Dimensions: 128, IndexType: vector.IndexType("hnsw"),
				IndexParams: map[string]string{"m": "16"}, Metadata: map[string]string{"t": "x"}}},
			http.MethodPost, "/api/v1/vector/namespaces"},
		{"DeleteNamespace", "DeleteNamespace",
			&vector.DeleteNamespaceRequest{Namespace: "docs"},
			http.MethodDelete, "/api/v1/vector/namespaces/docs"},
		{"ListNamespaces", "ListNamespaces",
			&vector.ListNamespacesRequest{Prefix: "do", PageSize: 10, PageToken: "tok"},
			http.MethodGet, "/api/v1/vector/namespaces"},
		{"GetNamespaceInfo", "GetNamespaceInfo",
			&vector.GetNamespaceInfoRequest{Namespace: "docs"},
			http.MethodGet, "/api/v1/vector/namespaces/docs"},
		{"Upsert", "Upsert",
			&vector.UpsertRequest{Vector: &vector.VectorEntity{
				ID: "v1", Namespace: "docs", Vector: []float32{0.1, 0.2}, Metadata: map[string]string{"k": "v"}}},
			http.MethodPost, "/api/v1/vector/namespaces/docs/vectors"},
		{"Get", "Get",
			&vector.GetRequest{Namespace: "docs", ID: "v1"},
			http.MethodGet, "/api/v1/vector/namespaces/docs/vectors/v1"},
		{"Delete", "Delete",
			&vector.DeleteRequest{Namespace: "docs", ID: "v1"},
			http.MethodDelete, "/api/v1/vector/namespaces/docs/vectors/v1"},
		{"BatchUpsert", "BatchUpsert",
			&vector.BatchUpsertRequest{Vectors: []*vector.VectorEntity{
				{ID: "v1", Namespace: "docs", Vector: []float32{0.1}, Metadata: map[string]string{"k": "v"}}}},
			http.MethodPost, "/api/v1/vector/namespaces/docs/vectors/batch"},
		{"BatchDelete", "BatchDelete",
			&vector.BatchDeleteRequest{Namespace: "docs", IDs: []string{"v1", "v2"}},
			http.MethodDelete, "/api/v1/vector/namespaces/docs/vectors/batch"},
		{"SearchSimilar", "SearchSimilar",
			&vector.SearchSimilarRequest{Namespace: "docs", QueryVector: []float32{0.1, 0.2}, K: 5,
				EfSearch: 64, MinScore: 0.5, IncludeVectors: true, IncludeMetadata: true},
			http.MethodPost, "/api/v1/vector/namespaces/docs/search/similar"},
		{"SearchByID", "SearchByID",
			&vector.SearchByIDRequest{Namespace: "docs", ID: "v1", K: 5},
			http.MethodPost, "/api/v1/vector/namespaces/docs/search/by-id/v1"},
		{"HybridSearch", "HybridSearch",
			&vector.HybridSearchRequest{Namespace: "docs", QueryVector: []float32{0.1}, K: 5,
				MetadataFilters: []*vector.MetadataFilter{{Key: "team", Operator: vector.FilterOperator("eq"), Value: "search"}}},
			http.MethodPost, "/api/v1/vector/hybrid-search"},
		{"SearchByMetadata", "SearchByMetadata",
			&vector.SearchByMetadataRequest{Namespace: "docs", Limit: 10, Offset: 0,
				MetadataFilters: []*vector.MetadataFilter{{Key: "team", Operator: vector.FilterOperator("eq"), Value: "search"}}},
			http.MethodPost, "/api/v1/vector/search-by-metadata"},
		{"RebuildIndex", "RebuildIndex",
			&vector.RebuildIndexRequest{Namespace: "docs", IndexParams: map[string]string{"m": "16"}, Async: true},
			http.MethodPost, "/api/v1/vector/namespaces/docs/index/rebuild"},
		{"GetIndexProgress", "GetIndexProgress",
			&vector.GetIndexProgressRequest{Namespace: "docs", OperationID: "op1"},
			http.MethodGet, "/api/v1/vector/namespaces/docs/index/progress"},
		{"Compact", "Compact",
			&vector.CompactRequest{Namespace: "docs", Async: true},
			http.MethodPost, "/api/v1/vector/admin/compact"},
		{"GetCompactionProgress", "GetCompactionProgress",
			&vector.GetCompactionProgressRequest{Namespace: "docs", OperationID: "op1"},
			http.MethodGet, "/api/v1/vector/admin/compact/progress"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath = "", ""
			resp, err := tr.Request(ctx, "Vector", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path: want %s, got %s", tc.wantPath, gotPath)
			}
		})
	}
}

// TestVectorHandlerInvalidRequestType exercises the type-assertion error branch
// on every vector handler.
func TestVectorHandlerInvalidRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be reached, path %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	methods := []string{
		"CreateNamespace", "DeleteNamespace", "ListNamespaces", "GetNamespaceInfo",
		"Upsert", "Get", "Delete", "BatchUpsert", "BatchDelete", "SearchSimilar",
		"SearchByID", "HybridSearch", "SearchByMetadata", "RebuildIndex",
		"GetIndexProgress", "Compact", "GetCompactionProgress", "List",
		"UpdateMetadata", "GetStats",
	}
	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			_, err := tr.Request(ctx, "Vector", m, "wrong-type")
			if err == nil {
				t.Fatalf("expected invalid-input error for method %s", m)
			}
		})
	}
}

// TestVectorHandlerValidationErrors exercises the required-field validation
// branches that return before any HTTP request is made.
func TestVectorHandlerValidationErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be reached for validation error, path %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"CreateNamespaceNilConfig", "CreateNamespace", &vector.CreateNamespaceRequest{}},
		{"UpsertNilVector", "Upsert", &vector.UpsertRequest{}},
		{"BatchUpsertEmpty", "BatchUpsert", &vector.BatchUpsertRequest{}},
		{"ListVectorsNoNamespace", "List", &vector.ListVectorsRequest{}},
		{"UpdateMetadataNoNamespace", "UpdateMetadata", &vector.UpdateMetadataRequest{ID: "v1"}},
		{"UpdateMetadataNoID", "UpdateMetadata", &vector.UpdateMetadataRequest{Namespace: "docs"}},
		{"GetStatsNoNamespace", "GetStats", &vector.GetStatsRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, "Vector", tc.method, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestVectorHandlerUnsupportedMethod verifies the default branch returns an
// unimplemented error for unknown methods.
func TestVectorHandlerUnsupportedMethod(t *testing.T) {
	tr := newMappingTransport(t)
	_, err := tr.Request(context.Background(), "Vector", "Telepathy", nil)
	if err == nil {
		t.Fatal("expected unimplemented error for unknown vector method")
	}
}

// TestVectorHandlerGetNotFound verifies the not-found short-circuit in handleGet.
func TestVectorHandlerGetNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"missing","code":"not_found"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "Vector", "Get",
		&vector.GetRequest{Namespace: "docs", ID: "missing"})
	if err != nil {
		t.Fatalf("not-found should be handled gracefully, got error: %v", err)
	}
	getResp, ok := resp.(*vector.GetResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if getResp.Found {
		t.Error("expected Found=false for missing vector")
	}
}

// TestVectorHandlerServerError exercises the doRequest error path through a
// handler that uses vectorHandler.doRequest (not doJSONRequest).
func TestVectorHandlerServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom","code":"internal"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Vector", "DeleteNamespace",
		&vector.DeleteNamespaceRequest{Namespace: "docs"})
	if err == nil {
		t.Fatal("expected error from server 500")
	}
}
