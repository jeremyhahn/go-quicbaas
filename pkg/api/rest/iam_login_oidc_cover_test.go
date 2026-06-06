//go:build rest && iam

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
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestRefreshToken_Success(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()

	mockSvc := server.service.(*testutil.MockQuicBaaS)

	mockSvc.On("RefreshToken", mock.Anything, mock.MatchedBy(func(r *service.RefreshTokenRequest) bool {
		return r.RefreshToken == "rt"
	})).Return(&service.RefreshTokenResponse{AccessToken: "new-at", ExpiresAt: 1 << 40}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh",
		bytes.NewReader([]byte(`{"refresh_token":"rt"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.RefreshToken(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "new-at")
}

func TestRefreshToken_Validation(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	// Invalid JSON
	w := httptest.NewRecorder()
	handler.RefreshToken(w, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte("{bad"))))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Missing refresh_token
	w = httptest.NewRecorder()
	handler.RefreshToken(w, httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{}`))))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRefreshToken_ConfigError(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()

	mockSvc := server.service.(*testutil.MockQuicBaaS)
	mockSvc.On("RefreshToken", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrConfiguration, "auth", "not configured")).Once()

	w := httptest.NewRecorder()
	handler.RefreshToken(w, httptest.NewRequest(http.MethodPost, "/x",
		bytes.NewReader([]byte(`{"refresh_token":"rt"}`))))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestRefreshToken_InvalidToken(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()

	mockSvc := server.service.(*testutil.MockQuicBaaS)
	mockSvc.On("RefreshToken", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrUnauthorized, "auth", "expired")).Once()

	w := httptest.NewRecorder()
	handler.RefreshToken(w, httptest.NewRequest(http.MethodPost, "/x",
		bytes.NewReader([]byte(`{"refresh_token":"rt"}`))))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
