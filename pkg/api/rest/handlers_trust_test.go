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
