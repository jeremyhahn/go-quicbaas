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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestHandleSecrets_CreatedAtIsTheServiceStoredTime pins go-quicbaas#179 at
// the REST edge: GET /api/v1/secrets/<path> and GET /api/v1/secrets carry
// the stored write times the service reports, unchanged on every read.
func TestHandleSecrets_CreatedAtIsTheServiceStoredTime(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	createdAt := time.Date(2026, 8, 26, 9, 30, 0, 0, time.UTC)
	updatedAt := createdAt.Add(36 * time.Hour)
	mockService.On("GetSecret", mock.Anything, mock.Anything).Return(&service.GetSecretResponse{
		SecretID:  "app/api/key",
		Version:   1,
		Data:      []byte("api-key-value"),
		CreatedAt: createdAt,
		Timestamp: time.Now(),
	}, nil).Twice()
	mockService.On("ListSecrets", mock.Anything, mock.Anything).Return(&service.ListSecretsResponse{
		Secrets: []*service.Secret{{
			SecretID:       "app/api/key",
			Name:           "app/api/key",
			CurrentVersion: 2,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		}},
		TotalCount: 1,
		Timestamp:  time.Now(),
	}, nil).Twice()

	for read := 0; read < 2; read++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/app/api/key", nil)
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("*", "app/api/key")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()
		server.HandleGetSecret(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var got struct {
			CreatedAt time.Time `json:"created_at"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.True(t, createdAt.Equal(got.CreatedAt), "get created_at: want %s, got %s", createdAt, got.CreatedAt)

		w = httptest.NewRecorder()
		server.HandleListSecrets(w, httptest.NewRequest(http.MethodGet, "/api/v1/secrets", nil))
		require.Equal(t, http.StatusOK, w.Code)

		var list struct {
			Secrets []struct {
				CreatedAt time.Time `json:"created_at"`
				UpdatedAt time.Time `json:"updated_at"`
			} `json:"secrets"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
		require.Len(t, list.Secrets, 1)
		require.True(t, createdAt.Equal(list.Secrets[0].CreatedAt), "list created_at: want %s, got %s", createdAt, list.Secrets[0].CreatedAt)
		require.True(t, updatedAt.Equal(list.Secrets[0].UpdatedAt), "list updated_at: want %s, got %s", updatedAt, list.Secrets[0].UpdatedAt)
	}
	mockService.AssertExpectations(t)
}

// TestHandleGetSecret_UnknownWriteTimeIsAnError pins that when the service
// cannot establish a secret's stored write time, the REST read fails rather
// than answering with a substitute created_at.
func TestHandleGetSecret_UnknownWriteTimeIsAnError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetSecret", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrInternal,
		Op:   "secrets.GetSecret",
		Err:  assert.AnError,
	}).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/secrets/app/api/key", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "app/api/key")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleGetSecret(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.NotContains(t, w.Body.String(), "created_at")
	mockService.AssertExpectations(t)
}
