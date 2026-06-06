// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package vector

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// transportErr is a reusable transport-level error used to assert propagation.
var transportErr = errors.NewUnavailable("test.transport", "backend unavailable")

// newOKTransport returns a mock transport that replies to every (service, method)
// with the supplied canned response and no error.
func newOKTransport(resp interface{}) *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return resp, nil
		},
	}
}

// newErrTransport returns a mock transport that fails every request with err.
func newErrTransport(err error) *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, err
		},
	}
}

// newNilTransport returns a mock transport that returns (nil, nil) for every request.
func newNilTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
}

// newTypeMismatchTransport returns a mock transport whose response is the wrong
// concrete type, exercising the type-assertion failure branch.
func newTypeMismatchTransport() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "wrong-type", nil
		},
	}
}

func assertErrCode(t *testing.T, err error, want errors.ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with code %v, got nil", want)
	}
	if got := errors.Code(err); got != want {
		t.Fatalf("expected error code %v, got %v (err=%v)", want, got, err)
	}
}

// =============================================================================
// Namespace Management
// =============================================================================

func TestClient_CreateNamespace(t *testing.T) {
	validReq := &CreateNamespaceRequest{Config: &NamespaceConfig{
		Namespace:  "docs",
		Dimensions: 128,
		IndexType:  IndexTypeHNSW,
	}}

	t.Run("success", func(t *testing.T) {
		c := NewClient(newOKTransport(&CreateNamespaceResponse{Namespace: "docs", Success: true}))
		resp, err := c.CreateNamespace(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Namespace != "docs" || !resp.Success {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("verifies service and method routing", func(t *testing.T) {
		mt := &mockTransport{
			requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != serviceName {
					t.Errorf("expected service %q, got %q", serviceName, service)
				}
				if method != "CreateNamespace" {
					t.Errorf("expected method CreateNamespace, got %q", method)
				}
				return &CreateNamespaceResponse{Namespace: "docs"}, nil
			},
		}
		if _, err := NewClient(mt).CreateNamespace(context.Background(), validReq); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).CreateNamespace(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).CreateNamespace(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).CreateNamespace(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&CreateNamespaceResponse{})).CreateNamespace(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&CreateNamespaceResponse{}))
		cases := []struct {
			name string
			req  *CreateNamespaceRequest
		}{
			{"nil request", nil},
			{"nil config", &CreateNamespaceRequest{}},
			{"empty namespace", &CreateNamespaceRequest{Config: &NamespaceConfig{Dimensions: 8}}},
			{"non-positive dimensions", &CreateNamespaceRequest{Config: &NamespaceConfig{Namespace: "n", Dimensions: 0}}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.CreateNamespace(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_DeleteNamespace(t *testing.T) {
	validReq := &DeleteNamespaceRequest{Namespace: "docs"}

	t.Run("success", func(t *testing.T) {
		c := NewClient(newOKTransport(&DeleteNamespaceResponse{Deleted: true, VectorsDeleted: 7}))
		resp, err := c.DeleteNamespace(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Deleted || resp.VectorsDeleted != 7 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).DeleteNamespace(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).DeleteNamespace(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).DeleteNamespace(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&DeleteNamespaceResponse{})).DeleteNamespace(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&DeleteNamespaceResponse{}))
		for _, tc := range []struct {
			name string
			req  *DeleteNamespaceRequest
		}{
			{"nil request", nil},
			{"empty namespace", &DeleteNamespaceRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.DeleteNamespace(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_ListNamespaces(t *testing.T) {
	validReq := &ListNamespacesRequest{Prefix: "doc"}

	t.Run("success", func(t *testing.T) {
		want := &ListNamespacesResponse{
			Namespaces: []*NamespaceInfo{{Config: &NamespaceConfig{Namespace: "docs"}, VectorCount: 3}},
			TotalCount: 1,
		}
		resp, err := NewClient(newOKTransport(want)).ListNamespaces(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Namespaces) != 1 || resp.TotalCount != 1 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("nil response yields empty slice", func(t *testing.T) {
		resp, err := NewClient(newNilTransport()).ListNamespaces(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp.Namespaces) != 0 {
			t.Fatalf("expected empty namespaces, got %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).ListNamespaces(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).ListNamespaces(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&ListNamespacesResponse{})).ListNamespaces(context.Background(), nil)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&ListNamespacesResponse{})).ListNamespaces(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})
}

func TestClient_GetNamespaceInfo(t *testing.T) {
	validReq := &GetNamespaceInfoRequest{Namespace: "docs", IncludeStats: true}

	t.Run("success", func(t *testing.T) {
		want := &GetNamespaceInfoResponse{Info: &NamespaceInfo{VectorCount: 42}}
		resp, err := NewClient(newOKTransport(want)).GetNamespaceInfo(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Info.VectorCount != 42 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).GetNamespaceInfo(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).GetNamespaceInfo(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).GetNamespaceInfo(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&GetNamespaceInfoResponse{})).GetNamespaceInfo(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&GetNamespaceInfoResponse{}))
		for _, tc := range []struct {
			name string
			req  *GetNamespaceInfoRequest
		}{
			{"nil request", nil},
			{"empty namespace", &GetNamespaceInfoRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.GetNamespaceInfo(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

// =============================================================================
// Vector CRUD
// =============================================================================

func TestClient_BatchUpsert(t *testing.T) {
	validReq := &BatchUpsertRequest{Vectors: []*VectorEntity{
		{Namespace: "docs", ID: "a", Vector: []float32{0.1}},
	}}

	t.Run("success", func(t *testing.T) {
		want := &BatchUpsertResponse{VectorsSucceeded: 1}
		resp, err := NewClient(newOKTransport(want)).BatchUpsert(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.VectorsSucceeded != 1 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).BatchUpsert(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).BatchUpsert(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).BatchUpsert(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&BatchUpsertResponse{})).BatchUpsert(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&BatchUpsertResponse{}))
		for _, tc := range []struct {
			name string
			req  *BatchUpsertRequest
		}{
			{"nil request", nil},
			{"empty vectors", &BatchUpsertRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.BatchUpsert(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_Get(t *testing.T) {
	validReq := &GetRequest{Namespace: "docs", ID: "a", IncludeVector: true}

	t.Run("success", func(t *testing.T) {
		want := &GetResponse{Vector: &VectorEntity{ID: "a"}, Found: true}
		resp, err := NewClient(newOKTransport(want)).Get(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Found || resp.Vector.ID != "a" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("nil response yields not found", func(t *testing.T) {
		resp, err := NewClient(newNilTransport()).Get(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || resp.Found {
			t.Fatalf("expected not-found response, got %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).Get(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).Get(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&GetResponse{})).Get(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&GetResponse{}))
		for _, tc := range []struct {
			name string
			req  *GetRequest
		}{
			{"nil request", nil},
			{"empty namespace", &GetRequest{ID: "a"}},
			{"empty ID", &GetRequest{Namespace: "docs"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.Get(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_BatchDelete(t *testing.T) {
	validReq := &BatchDeleteRequest{Namespace: "docs", IDs: []string{"a", "b"}}

	t.Run("success", func(t *testing.T) {
		want := &BatchDeleteResponse{DeletedCount: 2}
		resp, err := NewClient(newOKTransport(want)).BatchDelete(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.DeletedCount != 2 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).BatchDelete(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).BatchDelete(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).BatchDelete(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&BatchDeleteResponse{})).BatchDelete(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&BatchDeleteResponse{}))
		for _, tc := range []struct {
			name string
			req  *BatchDeleteRequest
		}{
			{"nil request", nil},
			{"empty namespace", &BatchDeleteRequest{IDs: []string{"a"}}},
			{"empty IDs", &BatchDeleteRequest{Namespace: "docs"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.BatchDelete(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_List(t *testing.T) {
	validReq := &ListVectorsRequest{Namespace: "docs", PageSize: 10}

	t.Run("success", func(t *testing.T) {
		want := &ListVectorsResponse{Vectors: []*VectorEntity{{ID: "a"}}, TotalCount: 1}
		resp, err := NewClient(newOKTransport(want)).List(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Vectors) != 1 || resp.TotalCount != 1 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("nil response yields empty slice", func(t *testing.T) {
		resp, err := NewClient(newNilTransport()).List(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp.Vectors) != 0 {
			t.Fatalf("expected empty vectors, got %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).List(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).List(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&ListVectorsResponse{})).List(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&ListVectorsResponse{}))
		for _, tc := range []struct {
			name string
			req  *ListVectorsRequest
		}{
			{"nil request", nil},
			{"empty namespace", &ListVectorsRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.List(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_UpdateMetadata(t *testing.T) {
	validReq := &UpdateMetadataRequest{
		Namespace: "docs", ID: "a", Metadata: map[string]string{"k": "v"}, Merge: true,
	}

	t.Run("success", func(t *testing.T) {
		want := &UpdateMetadataResponse{Success: true, Version: 3}
		resp, err := NewClient(newOKTransport(want)).UpdateMetadata(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Success || resp.Version != 3 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).UpdateMetadata(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).UpdateMetadata(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).UpdateMetadata(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&UpdateMetadataResponse{})).UpdateMetadata(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&UpdateMetadataResponse{}))
		for _, tc := range []struct {
			name string
			req  *UpdateMetadataRequest
		}{
			{"nil request", nil},
			{"empty namespace", &UpdateMetadataRequest{ID: "a", Metadata: map[string]string{"k": "v"}}},
			{"empty ID", &UpdateMetadataRequest{Namespace: "docs", Metadata: map[string]string{"k": "v"}}},
			{"empty metadata", &UpdateMetadataRequest{Namespace: "docs", ID: "a"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.UpdateMetadata(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

// =============================================================================
// Search Operations
// =============================================================================

func TestClient_SearchByID(t *testing.T) {
	validReq := &SearchByIDRequest{Namespace: "docs", ID: "a", K: 5}

	t.Run("success", func(t *testing.T) {
		want := &SearchByIDResponse{Results: []*SearchResult{{Score: 0.9}}, TotalCandidates: 10}
		resp, err := NewClient(newOKTransport(want)).SearchByID(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 1 || resp.TotalCandidates != 10 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("nil response yields empty results", func(t *testing.T) {
		resp, err := NewClient(newNilTransport()).SearchByID(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp.Results) != 0 {
			t.Fatalf("expected empty results, got %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).SearchByID(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).SearchByID(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&SearchByIDResponse{})).SearchByID(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&SearchByIDResponse{}))
		for _, tc := range []struct {
			name string
			req  *SearchByIDRequest
		}{
			{"nil request", nil},
			{"empty namespace", &SearchByIDRequest{ID: "a", K: 1}},
			{"empty ID", &SearchByIDRequest{Namespace: "docs", K: 1}},
			{"zero K", &SearchByIDRequest{Namespace: "docs", ID: "a", K: 0}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.SearchByID(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_HybridSearch(t *testing.T) {
	validReq := &HybridSearchRequest{
		Namespace:   "docs",
		QueryVector: []float32{0.1, 0.2},
		K:           5,
		MetadataFilters: []*MetadataFilter{
			{Key: "cat", Operator: FilterOperatorEquals, Value: "tech"},
		},
	}

	t.Run("success", func(t *testing.T) {
		want := &HybridSearchResponse{Results: []*SearchResult{{Score: 0.8}}, RerankingApplied: true}
		resp, err := NewClient(newOKTransport(want)).HybridSearch(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 1 || !resp.RerankingApplied {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("nil response yields empty results", func(t *testing.T) {
		resp, err := NewClient(newNilTransport()).HybridSearch(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp.Results) != 0 {
			t.Fatalf("expected empty results, got %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).HybridSearch(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).HybridSearch(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&HybridSearchResponse{})).HybridSearch(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&HybridSearchResponse{}))
		for _, tc := range []struct {
			name string
			req  *HybridSearchRequest
		}{
			{"nil request", nil},
			{"empty namespace", &HybridSearchRequest{QueryVector: []float32{0.1}, K: 1}},
			{"empty query vector", &HybridSearchRequest{Namespace: "docs", K: 1}},
			{"zero K", &HybridSearchRequest{Namespace: "docs", QueryVector: []float32{0.1}, K: 0}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.HybridSearch(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_SearchByMetadata(t *testing.T) {
	validReq := &SearchByMetadataRequest{
		Namespace: "docs",
		MetadataFilters: []*MetadataFilter{
			{Key: "cat", Operator: FilterOperatorEquals, Value: "tech"},
		},
		Limit: 20,
	}

	t.Run("success", func(t *testing.T) {
		want := &SearchByMetadataResponse{Results: []*VectorEntity{{ID: "a"}}, TotalCount: 1}
		resp, err := NewClient(newOKTransport(want)).SearchByMetadata(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Results) != 1 || resp.TotalCount != 1 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("nil response yields empty results", func(t *testing.T) {
		resp, err := NewClient(newNilTransport()).SearchByMetadata(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp.Results) != 0 {
			t.Fatalf("expected empty results, got %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).SearchByMetadata(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).SearchByMetadata(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&SearchByMetadataResponse{})).SearchByMetadata(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&SearchByMetadataResponse{}))
		for _, tc := range []struct {
			name string
			req  *SearchByMetadataRequest
		}{
			{"nil request", nil},
			{"empty namespace", &SearchByMetadataRequest{MetadataFilters: []*MetadataFilter{{Key: "k"}}}},
			{"empty filters", &SearchByMetadataRequest{Namespace: "docs"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.SearchByMetadata(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

// =============================================================================
// Index Management
// =============================================================================

func TestClient_RebuildIndex(t *testing.T) {
	validReq := &RebuildIndexRequest{Namespace: "docs", Async: true}

	t.Run("success", func(t *testing.T) {
		want := &RebuildIndexResponse{OperationID: "op-1", Status: "running"}
		resp, err := NewClient(newOKTransport(want)).RebuildIndex(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.OperationID != "op-1" || resp.Status != "running" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).RebuildIndex(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).RebuildIndex(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).RebuildIndex(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&RebuildIndexResponse{})).RebuildIndex(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&RebuildIndexResponse{}))
		for _, tc := range []struct {
			name string
			req  *RebuildIndexRequest
		}{
			{"nil request", nil},
			{"empty namespace", &RebuildIndexRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.RebuildIndex(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_GetIndexProgress(t *testing.T) {
	validReq := &GetIndexProgressRequest{OperationID: "op-1"}

	t.Run("success", func(t *testing.T) {
		want := &GetIndexProgressResponse{OperationID: "op-1", PercentComplete: 50}
		resp, err := NewClient(newOKTransport(want)).GetIndexProgress(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.PercentComplete != 50 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("valid by namespace only", func(t *testing.T) {
		want := &GetIndexProgressResponse{Namespace: "docs"}
		_, err := NewClient(newOKTransport(want)).GetIndexProgress(
			context.Background(), &GetIndexProgressRequest{Namespace: "docs"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).GetIndexProgress(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).GetIndexProgress(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).GetIndexProgress(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&GetIndexProgressResponse{})).GetIndexProgress(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&GetIndexProgressResponse{}))
		for _, tc := range []struct {
			name string
			req  *GetIndexProgressRequest
		}{
			{"nil request", nil},
			{"empty operation ID and namespace", &GetIndexProgressRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.GetIndexProgress(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

// =============================================================================
// Admin Operations
// =============================================================================

func TestClient_Export(t *testing.T) {
	validReq := &ExportRequest{Namespace: "docs", Format: "json"}

	t.Run("success single response wrapped in slice", func(t *testing.T) {
		want := &ExportResponse{VectorsExported: 5, IsFinal: true}
		resp, err := NewClient(newOKTransport(want)).Export(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp) != 1 || resp[0].VectorsExported != 5 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("success slice response", func(t *testing.T) {
		want := []*ExportResponse{{VectorsExported: 2}, {VectorsExported: 3, IsFinal: true}}
		resp, err := NewClient(newOKTransport(want)).Export(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp) != 2 {
			t.Fatalf("expected 2 batches, got %d", len(resp))
		}
	})

	t.Run("nil response yields empty slice", func(t *testing.T) {
		resp, err := NewClient(newNilTransport()).Export(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp) != 0 {
			t.Fatalf("expected empty slice, got %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).Export(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).Export(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&ExportResponse{})).Export(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&ExportResponse{}))
		for _, tc := range []struct {
			name string
			req  *ExportRequest
		}{
			{"nil request", nil},
			{"empty namespace", &ExportRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.Export(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_Import(t *testing.T) {
	validBatches := []*ImportRequest{
		{Namespace: "docs", Format: "json", BatchData: []byte("data")},
	}

	t.Run("success", func(t *testing.T) {
		want := &ImportResponse{VectorsImported: 3}
		resp, err := NewClient(newOKTransport(want)).Import(context.Background(), validBatches)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.VectorsImported != 3 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).Import(context.Background(), validBatches)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).Import(context.Background(), validBatches)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).Import(context.Background(), validBatches)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&ImportResponse{})).Import(nil, validBatches)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&ImportResponse{}))
		for _, tc := range []struct {
			name    string
			batches []*ImportRequest
		}{
			{"empty batches", nil},
			{"nil batch element", []*ImportRequest{nil}},
			{"empty namespace", []*ImportRequest{{Format: "json", BatchData: []byte("x")}}},
			{"empty batch data", []*ImportRequest{{Namespace: "docs", Format: "json"}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.Import(context.Background(), tc.batches)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_Compact(t *testing.T) {
	validReq := &CompactRequest{Namespace: "docs", Async: true}

	t.Run("success", func(t *testing.T) {
		want := &CompactResponse{OperationID: "op-9"}
		resp, err := NewClient(newOKTransport(want)).Compact(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.OperationID != "op-9" {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).Compact(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).Compact(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).Compact(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&CompactResponse{})).Compact(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&CompactResponse{}))
		for _, tc := range []struct {
			name string
			req  *CompactRequest
		}{
			{"nil request", nil},
			{"empty namespace", &CompactRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.Compact(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_GetCompactionProgress(t *testing.T) {
	validReq := &GetCompactionProgressRequest{OperationID: "op-9"}

	t.Run("success", func(t *testing.T) {
		want := &GetCompactionProgressResponse{OperationID: "op-9", PercentComplete: 75}
		resp, err := NewClient(newOKTransport(want)).GetCompactionProgress(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.PercentComplete != 75 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("valid by namespace only", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&GetCompactionProgressResponse{})).GetCompactionProgress(
			context.Background(), &GetCompactionProgressRequest{Namespace: "docs"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).GetCompactionProgress(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).GetCompactionProgress(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).GetCompactionProgress(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&GetCompactionProgressResponse{})).GetCompactionProgress(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&GetCompactionProgressResponse{}))
		for _, tc := range []struct {
			name string
			req  *GetCompactionProgressRequest
		}{
			{"nil request", nil},
			{"empty operation ID and namespace", &GetCompactionProgressRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.GetCompactionProgress(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}

func TestClient_GetStats(t *testing.T) {
	validReq := &GetStatsRequest{Namespace: "docs"}

	t.Run("success", func(t *testing.T) {
		want := &GetStatsResponse{Namespace: "docs", VectorCount: 100}
		resp, err := NewClient(newOKTransport(want)).GetStats(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.VectorCount != 100 {
			t.Fatalf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error propagates", func(t *testing.T) {
		_, err := NewClient(newErrTransport(transportErr)).GetStats(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeUnavailable)
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(newNilTransport()).GetStats(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("type mismatch", func(t *testing.T) {
		_, err := NewClient(newTypeMismatchTransport()).GetStats(context.Background(), validReq)
		assertErrCode(t, err, errors.CodeInternal)
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := NewClient(newOKTransport(&GetStatsResponse{})).GetStats(nil, validReq)
		assertErrCode(t, err, errors.CodeInvalidInput)
	})

	t.Run("validation", func(t *testing.T) {
		c := NewClient(newOKTransport(&GetStatsResponse{}))
		for _, tc := range []struct {
			name string
			req  *GetStatsRequest
		}{
			{"nil request", nil},
			{"empty namespace", &GetStatsRequest{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, err := c.GetStats(context.Background(), tc.req)
				assertErrCode(t, err, errors.CodeInvalidInput)
			})
		}
	})
}
