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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func abacUserCtx(sub, tenant string) context.Context {
	ctx := context.WithValue(context.Background(), ContextKeyUser, &iam.UserInfoResponse{Sub: sub})
	ctx = context.WithValue(ctx, ContextKeyTenant, tenant)
	return iam.ContextWithTenant(ctx, tenant)
}

// Unauthenticated requests are rejected by ABACMiddleware with 401.
func TestABACMiddleware_Unauthenticated(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	nextCalled := false
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { nextCalled = true })

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	handler.ABACMiddleware("doc", "read")(next).ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.False(t, nextCalled)
}

// With no matching policy the engine denies access -> 403.
func TestABACMiddleware_Denied(t *testing.T) {
	handler, cleanup := setupPolicyHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil).WithContext(abacUserCtx("u1", policyTenant))
	w := httptest.NewRecorder()
	handler.ABACMiddleware("/secret", "read")(next).ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestJWTMiddleware_NoAuthHeader(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	w := httptest.NewRecorder()
	handler.JWTMiddleware(next).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTMiddleware_BadFormat(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Token abc")
	w := httptest.NewRecorder()
	handler.JWTMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestJWTMiddleware_ProviderNotAvailable(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	handler.JWTMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestJWTMiddleware_InvalidToken(t *testing.T) {
	handler := setupWebAuthnHandler(t) // wires a real OIDC provider via GetOIDCProvider

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer garbage.token.value")
	w := httptest.NewRecorder()
	handler.JWTMiddleware(next).ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
