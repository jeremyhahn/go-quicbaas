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
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestLogout_HeaderToken(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()

	mockSvc := server.service.(*testutil.MockQuicBaaS)
	mockSvc.On("Logout", mock.Anything, mock.MatchedBy(func(r *service.LogoutRequest) bool {
		return r.AccessToken == "tok"
	})).Return(&service.LogoutResponse{}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	handler.Logout(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLogout_BodyToken(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()

	mockSvc := server.service.(*testutil.MockQuicBaaS)
	mockSvc.On("Logout", mock.Anything, mock.Anything).Return(&service.LogoutResponse{}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout",
		bytes.NewReader([]byte(`{"access_token":"body-tok"}`)))
	w := httptest.NewRecorder()
	handler.Logout(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLogout_MissingToken(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.Logout(w, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLogout_ConfigError(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()

	mockSvc := server.service.(*testutil.MockQuicBaaS)
	mockSvc.On("Logout", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrConfiguration, "auth", "nope")).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	handler.Logout(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestLogout_GenericError(t *testing.T) {
	handler, server, cleanup := setupIAMHandler(t)
	defer cleanup()

	mockSvc := server.service.(*testutil.MockQuicBaaS)
	mockSvc.On("Logout", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrInternal, "auth", "boom")).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	handler.Logout(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
