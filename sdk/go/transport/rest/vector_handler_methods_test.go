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

// vector_handler_methods_test.go covers the List, UpdateMetadata and GetStats
// methods that were previously absent from the vector handler's dispatch
// switch. Before the fix those methods fell through to the default branch and
// returned an "unsupported Vector method" error instead of substituting the
// {namespace}/{id} path parameters and issuing the request. These tests assert
// the concrete URL/method/body the handler now produces.

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/vector"
)

func TestVectorHandler_List_SubstitutesNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/vector/namespaces/docs/vectors" {
			t.Errorf("expected substituted namespace path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"vectors":[{"id":"v1","version":2}],"pagination":{"total_count":1}}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Vector", "List", &vector.ListVectorsRequest{
		Namespace: "docs",
	})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	resp := result.(*vector.ListVectorsResponse)
	if len(resp.Vectors) != 1 || resp.Vectors[0].ID != "v1" || resp.TotalCount != 1 {
		t.Errorf("unexpected list response: %+v", resp)
	}
	if resp.Vectors[0].Namespace != "docs" {
		t.Errorf("expected namespace populated, got %q", resp.Vectors[0].Namespace)
	}
}

func TestVectorHandler_UpdateMetadata_SubstitutesNamespaceAndID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/vector/namespaces/docs/vectors/v1/metadata" {
			t.Errorf("expected metadata path, got %s", r.URL.Path)
		}
		var body struct {
			Metadata map[string]string `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body.Metadata["label"] != "x" {
			t.Errorf("unexpected metadata: %+v", body.Metadata)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"v1","version":3}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Vector", "UpdateMetadata", &vector.UpdateMetadataRequest{
		Namespace: "docs",
		ID:        "v1",
		Metadata:  map[string]string{"label": "x"},
	})
	if err != nil {
		t.Fatalf("UpdateMetadata failed: %v", err)
	}
	resp := result.(*vector.UpdateMetadataResponse)
	if !resp.Success || resp.Version != 3 {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestVectorHandler_GetStats_SubstitutesNamespace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/vector/namespaces/docs/stats" {
			t.Errorf("expected stats path, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"namespace":"docs","vector_count":10,"storage_bytes":2048,"index_bytes":1024}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.Vector", "GetStats", &vector.GetStatsRequest{
		Namespace: "docs",
	})
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	resp := result.(*vector.GetStatsResponse)
	if resp.VectorCount != 10 || resp.IndexSizeBytes != 1024 || resp.StorageSizeBytes != 2048 {
		t.Errorf("unexpected stats: %+v", resp)
	}
}

func TestVectorHandler_List_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom","code":"internal"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Vector", "List", &vector.ListVectorsRequest{Namespace: "docs"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestVectorHandler_GetStats_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	defer tr.Close()

	_, err := tr.Request(context.Background(), "quicbaas.Vector", "GetStats", &vector.GetStatsRequest{Namespace: "docs"})
	if err == nil {
		t.Fatal("expected decode error")
	}
}
