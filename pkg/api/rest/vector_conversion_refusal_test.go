//go:build rest

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

package rest

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

func TestResponseInt64s(t *testing.T) {
	t.Run("converts values that fit", func(t *testing.T) {
		ints := newResponseInt64s("op")

		require.Equal(t, int64(math.MaxInt64), ints.Int64("v", math.MaxInt64))
		require.Equal(t, uint64(math.MaxUint64), ints.Add("sum", math.MaxUint64-1, 1))
		require.NoError(t, ints.Err())
	})

	tests := []struct {
		name  string
		apply func(*responseInt64s)
	}{
		{
			name:  "value beyond int64",
			apply: func(c *responseInt64s) { c.Int64("v", math.MaxInt64+1) },
		},
		{
			name:  "sum wrapping uint64",
			apply: func(c *responseInt64s) { c.Add("sum", math.MaxUint64, 1) },
		},
		{
			name: "first failure is kept and later values read zero",
			apply: func(c *responseInt64s) {
				c.Int64("v", math.MaxUint64)
				if got := c.Int64("w", 1); got != 0 {
					t.Errorf("Int64 after a failure = %d, want 0", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ints := newResponseInt64s("op")
			tt.apply(ints)

			var qbErr *errors.QBError
			require.ErrorAs(t, ints.Err(), &qbErr)
			require.Equal(t, errors.ErrInternal, qbErr.Code)
			var overflow *conv.ConversionOverflowError
			require.ErrorAs(t, ints.Err(), &overflow)
		})
	}
}

func TestVectorHandlers_RefuseCountsBeyondInt64(t *testing.T) {
	const huge = uint64(math.MaxInt64) + 1

	tests := []struct {
		name      string
		method    string
		path      string
		body      string
		setupMock func(*testutil.MockQuicBaaS, *MockVectorService)
	}{
		{
			name:   "health uptime",
			method: http.MethodGet,
			path:   "/api/v1/vector/health",
			setupMock: func(m *testutil.MockQuicBaaS, _ *MockVectorService) {
				m.On("Health", mock.Anything, mock.Anything).Return(&service.HealthResponse{
					Status:        service.HealthStatusHealthy,
					UptimeSeconds: huge,
				}, nil).Once()
			},
		},
		{
			name:   "deleted namespace vector count",
			method: http.MethodDelete,
			path:   "/api/v1/vector/namespaces/ns",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("DeleteNamespace", mock.Anything, mock.Anything).
					Return(&service.DeleteNamespaceResponse{Success: true, VectorsDeleted: huge}, nil).Once()
			},
		},
		{
			name:   "listed namespace vector count",
			method: http.MethodGet,
			path:   "/api/v1/vector/namespaces",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{{Namespace: "ns", VectorCount: huge}},
				}, nil).Once()
			},
		},
		{
			name:   "namespace info vector count",
			method: http.MethodGet,
			path:   "/api/v1/vector/namespaces/ns",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("GetNamespaceInfo", mock.Anything, mock.Anything).
					Return(&service.GetNamespaceInfoResponse{Namespace: "ns", VectorCount: huge}, nil).Once()
			},
		},
		{
			name:   "namespace stats index size",
			method: http.MethodGet,
			path:   "/api/v1/vector/namespaces/ns/stats",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("GetIndexStats", mock.Anything, mock.Anything).
					Return(&service.GetIndexStatsResponse{Namespace: "ns", IndexSizeBytes: huge}, nil).Once()
			},
		},
		{
			name:   "upserted vector version",
			method: http.MethodPut,
			path:   "/api/v1/vector/namespaces/ns/vectors/1",
			body:   `{"id":"1","vector":[0.1]}`,
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("UpsertVector", mock.Anything, mock.Anything).
					Return(&service.UpsertVectorResponse{ID: 1, Version: huge}, nil).Once()
			},
		},
		{
			name:   "fetched vector version",
			method: http.MethodGet,
			path:   "/api/v1/vector/namespaces/ns/vectors/1",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("GetVector", mock.Anything, mock.Anything).
					Return(&service.GetVectorResponse{Found: true, ID: 1, Version: huge}, nil).Once()
			},
		},
		{
			name:   "listed vector version",
			method: http.MethodGet,
			path:   "/api/v1/vector/namespaces/ns/vectors",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("SearchByMetadata", mock.Anything, mock.Anything).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{{ID: 1, Version: huge}},
				}, nil).Once()
			},
		},
		{
			name:   "listed vector total count",
			method: http.MethodGet,
			path:   "/api/v1/vector/namespaces/ns/vectors",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("SearchByMetadata", mock.Anything, mock.Anything).
					Return(&service.SearchByMetadataResponse{TotalCount: huge}, nil).Once()
			},
		},
		{
			name:   "metadata update version",
			method: http.MethodPut,
			path:   "/api/v1/vector/namespaces/ns/vectors/1/metadata",
			body:   `{"metadata":{"k":"v"}}`,
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("GetVector", mock.Anything, mock.Anything).
					Return(&service.GetVectorResponse{Found: true, ID: 1, Vector: []float32{0.1}}, nil).Once()
				v.On("UpsertVector", mock.Anything, mock.Anything).
					Return(&service.UpsertVectorResponse{ID: 1, Version: huge}, nil).Once()
			},
		},
		{
			name:   "metadata search version",
			method: http.MethodPost,
			path:   "/api/v1/vector/search-by-metadata",
			body:   `{"namespace":"ns"}`,
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("SearchByMetadata", mock.Anything, mock.Anything).Return(&service.SearchByMetadataResponse{
					Results: []*service.VectorData{{ID: 1, Version: huge}},
				}, nil).Once()
			},
		},
		{
			name:   "metadata search total count",
			method: http.MethodPost,
			path:   "/api/v1/vector/search-by-metadata",
			body:   `{"namespace":"ns"}`,
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("SearchByMetadata", mock.Anything, mock.Anything).
					Return(&service.SearchByMetadataResponse{TotalCount: huge}, nil).Once()
			},
		},
		{
			name:   "cluster stats namespace vector count",
			method: http.MethodGet,
			path:   "/api/v1/vector/cluster/stats",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{{Namespace: "ns"}},
				}, nil).Once()
				v.On("GetIndexStats", mock.Anything, mock.Anything).
					Return(&service.GetIndexStatsResponse{Namespace: "ns", VectorCount: huge}, nil).Once()
			},
		},
		{
			name:   "cluster stats total wrapping uint64",
			method: http.MethodGet,
			path:   "/api/v1/vector/cluster/stats",
			setupMock: func(_ *testutil.MockQuicBaaS, v *MockVectorService) {
				v.On("ListNamespaces", mock.Anything, mock.Anything).Return(&service.ListNamespacesResponse{
					Namespaces: []*service.NamespaceInfo{{Namespace: "a"}, {Namespace: "b"}, {Namespace: "c"}},
				}, nil).Once()
				// Each namespace fits int64, but the three sum past uint64
				// and would wrap to a total that also fits int64.
				v.On("GetIndexStats", mock.Anything, mock.Anything).
					Return(&service.GetIndexStatsResponse{IndexSizeBytes: math.MaxInt64}, nil).Times(3)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			vectorService := &MockVectorService{}
			mockService.VectorService = vectorService
			tt.setupMock(mockService, vectorService)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			requireServerFailure(t, w)
		})
	}
}

func TestHandleCreateNamespace_RefusesDimensionsBeyondUint32(t *testing.T) {
	tests := []struct {
		name       string
		dimensions string
	}{
		{name: "one past max uint32", dimensions: "4294967296"},
		{name: "far past max uint32", dimensions: "8589934592"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			vectorService := &MockVectorService{}
			mockService.VectorService = vectorService

			body := `{"namespace":"ns","dimensions":` + tt.dimensions + `,"distance_metric":"cosine"}`
			req := httptest.NewRequest(http.MethodPost, "/api/v1/vector/namespaces", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.GetRouter().ServeHTTP(w, req)

			requireInvalidField(t, w, "dimensions")
			vectorService.AssertNotCalled(t, "CreateNamespace", mock.Anything, mock.Anything)
		})
	}
}
