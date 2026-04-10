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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestHandleOpenAPISpec(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	t.Run("returns openapi spec with yaml content type", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil)
		w := httptest.NewRecorder()

		server.HandleOpenAPISpec(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/x-yaml", w.Header().Get("Content-Type"))

		// CORS headers should NOT be set per-handler; they are
		// managed by the central CORSMiddleware instead.
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))

		// Body should contain openapi spec (fallback or real)
		body := w.Body.String()
		assert.Contains(t, body, "openapi")
		assert.Contains(t, body, "QuicBaaS")
	})

	t.Run("returns fallback spec when file not found", func(t *testing.T) {
		// The default spec file path likely won't exist in test environment
		req := httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil)
		w := httptest.NewRecorder()

		server.HandleOpenAPISpec(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		// Should contain the fallback minimal spec
		assert.Contains(t, body, "openapi: 3.0.3")
		assert.Contains(t, body, "/health")
	})
}

func TestHandleSwaggerUI(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	t.Run("returns swagger UI html page", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
		w := httptest.NewRecorder()

		server.HandleSwaggerUI(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))

		body := w.Body.String()
		// Check for essential Swagger UI elements
		assert.Contains(t, body, "<!DOCTYPE html>")
		assert.Contains(t, body, "QuicBaaS API Documentation")
		assert.Contains(t, body, "swagger-ui")
		assert.Contains(t, body, "SwaggerUIBundle")
		assert.Contains(t, body, "/docs/openapi.yaml")
	})

	t.Run("swagger UI contains required scripts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
		w := httptest.NewRecorder()

		server.HandleSwaggerUI(w, req)

		body := w.Body.String()
		// Check for CDN script references
		assert.Contains(t, body, "swagger-ui-bundle.js")
		assert.Contains(t, body, "swagger-ui-standalone-preset.js")
		assert.Contains(t, body, "swagger-ui.css")
	})

	t.Run("swagger UI has try it out enabled", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
		w := httptest.NewRecorder()

		server.HandleSwaggerUI(w, req)

		body := w.Body.String()
		assert.Contains(t, body, "tryItOutEnabled: true")
	})
}

func TestHandleDocsRedirect(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	t.Run("redirects /docs to /docs/", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs", nil)
		w := httptest.NewRecorder()

		server.HandleDocsRedirect(w, req)

		assert.Equal(t, http.StatusMovedPermanently, w.Code)
		location := w.Header().Get("Location")
		assert.Equal(t, "/docs/", location)
	})

	t.Run("redirect preserves method", func(t *testing.T) {
		// Even though a redirect, let's verify behavior with HEAD
		req := httptest.NewRequest(http.MethodHead, "/docs", nil)
		w := httptest.NewRecorder()

		server.HandleDocsRedirect(w, req)

		assert.Equal(t, http.StatusMovedPermanently, w.Code)
	})
}

func TestSwaggerIntegrationWithRouter(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	server, err := NewServer(mockSvc, DefaultConfig(), log)
	require.NoError(t, err)
	defer server.Close()

	t.Run("openapi spec via router", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/openapi.yaml", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "openapi")
	})

	t.Run("swagger ui via router", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/docs/", nil)
		w := httptest.NewRecorder()

		server.GetRouter().ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.True(t, strings.Contains(w.Header().Get("Content-Type"), "text/html"))
	})
}
