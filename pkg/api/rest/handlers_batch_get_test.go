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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestHandleBatchGet(t *testing.T) {
	t.Run("successful batch get", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("BatchGet", mock.Anything, mock.Anything).Return(&service.BatchGetResponse{
			Responses: []*service.GetResponse{
				{ID: 1, Data: []byte(`{"name":"Alice"}`), Found: true, Version: 1, Timestamp: 12345},
				{ID: 2, Data: nil, Found: false},
			},
		}, nil).Once()

		body := `{"operations":[{"entity_type":"user","id":1},{"entity_type":"user","id":2}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp BatchGetResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		require.Len(t, resp.Responses, 2)
		assert.True(t, resp.Responses[0].Found)
		assert.False(t, resp.Responses[1].Found)
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader("{invalid"))
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty operations", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"operations":[]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "batch operations cannot be empty")
	})

	t.Run("missing entity type", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"operations":[{"entity_type":"","id":1}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "entity_type is required")
	})

	t.Run("missing ID", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"operations":[{"entity_type":"user","id":0}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "id is required")
	})

	t.Run("invalid consistency level", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		body := `{"operations":[{"entity_type":"user","id":1}],"consistency_level":"invalid"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("service error", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("BatchGet", mock.Anything, mock.Anything).Return(nil,
			errors.New(errors.ErrInternal, "batch", "service error")).Once()

		body := `{"operations":[{"entity_type":"user","id":1}]}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("body read error", func(t *testing.T) {
		server, _, cleanup := setupMockServer(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", &errorReader{})
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("with valid consistency level", func(t *testing.T) {
		server, mockService, cleanup := setupMockServer(t)
		defer cleanup()

		mockService.On("BatchGet", mock.Anything, mock.Anything).Return(&service.BatchGetResponse{
			Responses: []*service.GetResponse{
				{ID: 1, Data: []byte(`{"name":"Alice"}`), Found: true, Version: 1, Timestamp: 12345},
			},
		}, nil).Once()

		body := `{"operations":[{"entity_type":"user","id":1}],"consistency_level":"strong"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/batch/get", strings.NewReader(body))
		w := httptest.NewRecorder()

		server.HandleBatchGet(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
