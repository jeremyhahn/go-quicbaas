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
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubTrustService is a configurable service.TrustService for the REST
// server handler tests.
type stubTrustService struct {
	rootPEMs   [][]byte
	crossErr   error
	imported   int
	importErr  error
	published  int
	publishErr error
}

func (s *stubTrustService) CrossSignedRootPool(ctx context.Context, _ *service.CrossSignedRootPoolRequest) (*service.CrossSignedRootPoolResponse, error) {
	if s.crossErr != nil {
		return nil, s.crossErr
	}
	return &service.CrossSignedRootPoolResponse{RootPEMs: s.rootPEMs}, nil
}

func (s *stubTrustService) AutoImportAIA(ctx context.Context, req *service.AutoImportAIARequest) (*service.AutoImportAIAResponse, error) {
	if s.importErr != nil {
		return nil, s.importErr
	}
	return &service.AutoImportAIAResponse{Imported: s.imported}, nil
}

func (s *stubTrustService) PublishDANE(ctx context.Context, req *service.PublishDANERequest) (*service.PublishDANEResponse, error) {
	if s.publishErr != nil {
		return nil, s.publishErr
	}
	return &service.PublishDANEResponse{Published: s.published}, nil
}

func TestHandleTrustCrossSignedRootPool(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{rootPEMs: [][]byte{[]byte("root-a"), []byte("root-b")}}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/trust/cross-signed-roots", nil)
		w := httptest.NewRecorder()
		server.HandleTrustCrossSignedRootPool(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp TrustCrossSignedRootPoolRESTResponse
		assert.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Len(t, resp.RootPEMs, 2)
	})

	t.Run("service not available", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = nil

		req := httptest.NewRequest(http.MethodGet, "/api/v1/trust/cross-signed-roots", nil)
		w := httptest.NewRecorder()
		server.HandleTrustCrossSignedRootPool(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{crossErr: stderrors.New("boom")}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/trust/cross-signed-roots", nil)
		w := httptest.NewRecorder()
		server.HandleTrustCrossSignedRootPool(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleTrustAutoImportAIA(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{imported: 2}

		body, _ := json.Marshal(TrustAutoImportAIARESTRequest{ChainDER: [][]byte{{0x30, 0x82}, {0x30, 0x82}}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/auto-import-aia", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustAutoImportAIA(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp TrustAutoImportAIARESTResponse
		assert.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, 2, resp.Imported)
	})

	t.Run("empty chain", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{}

		body, _ := json.Marshal(TrustAutoImportAIARESTRequest{})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/auto-import-aia", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustAutoImportAIA(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/auto-import-aia", bytes.NewReader([]byte("not json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustAutoImportAIA(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service not available", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = nil

		body, _ := json.Marshal(TrustAutoImportAIARESTRequest{ChainDER: [][]byte{{0x30}}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/auto-import-aia", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustAutoImportAIA(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{importErr: stderrors.New("import boom")}

		body, _ := json.Marshal(TrustAutoImportAIARESTRequest{ChainDER: [][]byte{{0x30}}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/auto-import-aia", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustAutoImportAIA(w, req)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleTrustPublishDANE(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{published: 4}

		body, _ := json.Marshal(TrustPublishDANERESTRequest{
			Name: "kms.example.com", Port: 443, CertDER: []byte{0x30, 0x82},
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/publish-dane", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustPublishDANE(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp TrustPublishDANERESTResponse
		assert.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, 4, resp.Published)
	})

	t.Run("service not available", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = nil

		body, _ := json.Marshal(TrustPublishDANERESTRequest{Name: "h", Port: 443, CertDER: []byte{0x30}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/publish-dane", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustPublishDANE(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("not configured", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{publishErr: service.ErrTrustNoDANEPublisher}

		body, _ := json.Marshal(TrustPublishDANERESTRequest{Name: "h", Port: 443, CertDER: []byte{0x30}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/publish-dane", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustPublishDANE(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("invalid json", func(t *testing.T) {
		server, mockSvc, cleanup := setupMockServer(t)
		defer cleanup()
		mockSvc.TrustSvc = &stubTrustService{}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/trust/publish-dane", bytes.NewReader([]byte("{not json")))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.HandleTrustPublishDANE(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestTrustRoutes_TrustFamilyPermissions pins the trust surface to the
// registered trust family, the same grant on every transport: the pool read
// is trust:read, and AIA import and DANE publish, which change what the
// node trusts or publishes, are trust:write. Both write routes are then
// served rather than refused.
func TestTrustRoutes_TrustFamilyPermissions(t *testing.T) {
	registered := registeredPermissionSet(t)
	cases := []struct {
		method, path, want string
	}{
		{http.MethodGet, "/api/v1/trust/cross-signed-roots", "trust:read"},
		{http.MethodPost, "/api/v1/trust/auto-import-aia", "trust:write"},
		{http.MethodPost, "/api/v1/trust/publish-dane", "trust:write"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resource, action, ok := routePermissionForPath(tc.path, tc.method)
			require.Truef(t, ok, "%s %s resolved no permission", tc.method, tc.path)
			assert.Equal(t, tc.want, resource+":"+action)
			_, found := registered[tc.want]
			assert.Truef(t, found, "%s is not registered", tc.want)
		})
	}
}

// TestTrustWriteRoutes_Served confirms AIA import and DANE publish reach the
// trust service once they carry a permission, instead of the typed refusal.
func TestTrustWriteRoutes_Served(t *testing.T) {
	for _, path := range []string{"/api/v1/trust/auto-import-aia", "/api/v1/trust/publish-dane"} {
		t.Run(path, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()
			mockSvc.TrustSvc = &stubTrustService{imported: 1, published: 1}

			req := tenantRequest("acme", http.MethodPost, path, bytes.NewReader([]byte(`{}`)))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.router.ServeHTTP(w, req)

			refusal := (&UnassignedRouteError{Method: http.MethodPost, Pattern: path}).Error()
			assert.NotContains(t, w.Body.String(), refusal)
			assert.NotEqual(t, http.StatusForbidden, w.Code)
		})
	}
}

// TestRouteSpecBind_ServesHandlerUnlessRefused proves bind leaves every
// other auth class on its own handler.
func TestRouteSpecBind_ServesHandlerUnlessRefused(t *testing.T) {
	var called bool
	spec := RouteSpec{
		Method:  http.MethodGet,
		Pattern: "/api/v1/probe",
		Auth:    RouteAuthPermission,
		Handler: func(*Server) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}
		},
	}
	w := httptest.NewRecorder()
	spec.bind(nil)(w, httptest.NewRequest(http.MethodGet, spec.Pattern, nil))
	assert.True(t, called)
	assert.Equal(t, http.StatusNoContent, w.Code)

	called = false
	spec.Auth = RouteAuthRefused
	w = httptest.NewRecorder()
	spec.bind(nil)(w, httptest.NewRequest(http.MethodGet, spec.Pattern, nil))
	assert.False(t, called)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
