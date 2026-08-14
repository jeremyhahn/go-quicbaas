//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestHandleGetOptimized(t *testing.T) {
	tests := []struct {
		name           string
		entityType     string
		id             string
		queryParams    string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
		checkResponse  func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name:       "successful optimized get",
			entityType: "kv",
			id:         "123",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Get", mock.Anything, mock.MatchedBy(func(req *service.GetRequest) bool {
					return req.EntityType == "kv" && req.ID == 123
				})).Return(&service.GetResponse{
					Found: true,
					Data:  []byte("test-value"),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing entity type",
			entityType:     "",
			id:             "123",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "entity type and ID are required")
			},
		},
		{
			name:           "missing ID",
			entityType:     "kv",
			id:             "",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "entity type and ID are required")
			},
		},
		{
			name:           "invalid ID format",
			entityType:     "kv",
			id:             "not-a-number",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), "invalid entity ID")
			},
		},
		{
			name:        "with consistency level",
			entityType:  "kv",
			id:          "123",
			queryParams: "?consistency=strong",
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("Get", mock.Anything, mock.Anything).Return(&service.GetResponse{
					Found: true,
					Data:  []byte("test-value"),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid consistency level",
			entityType:     "kv",
			id:             "123",
			queryParams:    "?consistency=invalid",
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()

			if tt.setupMock != nil {
				tt.setupMock(mockService)
			}

			url := "/api/v1/optimized/" + tt.entityType + "/" + tt.id
			if tt.queryParams != "" {
				url += tt.queryParams
			}

			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("type", tt.entityType)
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleGetOptimized(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.checkResponse != nil {
				tt.checkResponse(t, w)
			}
		})
	}
}
