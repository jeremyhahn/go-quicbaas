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
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func newWebSocketTestServer(t *testing.T) *Server {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(testutil.NewMockQuicBaaS(), DefaultConfig(), log)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// TestHandleWebSocket_NotRunning: with no WebSocket handler set, the upgrade
// route answers 503 to a caller authenticated with a verified tenant.
func TestHandleWebSocket_NotRunning(t *testing.T) {
	srv := newWebSocketTestServer(t)

	req := asTenantCaller(httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil), "tenant-a")
	rec := httptest.NewRecorder()
	srv.HandleWebSocket(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// closeRecordingHandler is an http.Handler and io.Closer that records Close.
type closeRecordingHandler struct {
	closed atomic.Int32
}

func (h *closeRecordingHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

func (h *closeRecordingHandler) Close() error {
	h.closed.Add(1)
	return nil
}

// TestHandleWebSocket_ClosedOnShutdown: Shutdown closes the WebSocket handler,
// and a set handler serves the route to a request that carries a verified
// identity and tenant, as go-qrdb's AuthMiddleware leaves it.
func TestHandleWebSocket_ClosedOnShutdown(t *testing.T) {
	srv := newWebSocketTestServer(t)
	h := &closeRecordingHandler{}
	srv.SetWebSocketHandler(h)

	rec := httptest.NewRecorder()
	srv.HandleWebSocket(rec, asTenantCaller(httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil), "tenant-a"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	_ = srv.Shutdown(context.Background())
	if got := h.closed.Load(); got != 1 {
		t.Fatalf("Close called %d times, want 1", got)
	}
}

// TestHandleWebSocket_NoTenantIsForbidden: a request with no verified tenant
// is refused with 403 and never reaches the WebSocket handler.
func TestHandleWebSocket_NoTenantIsForbidden(t *testing.T) {
	srv := newWebSocketTestServer(t)
	h := &closeRecordingHandler{}
	srv.SetWebSocketHandler(h)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	rec := httptest.NewRecorder()
	srv.HandleWebSocket(rec, httptest.NewRequest(http.MethodGet, "/api/v1/ws", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

// TestHandleWebSocket_UpgradeThroughRouter: an upgrade through the complete
// router, with a nonzero request timeout, reaches the WebSocket handler,
// switches protocols and echoes.
func TestHandleWebSocket_UpgradeThroughRouter(t *testing.T) {
	srv := newWebSocketTestServer(t)
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	if srv.config.RequestTimeout <= 0 {
		t.Fatalf("RequestTimeout = %v, want a nonzero timeout", srv.config.RequestTimeout)
	}
	srv.SetWebSocketHandler(wsEchoHandler(t))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.GetRouter().ServeHTTP(w, asTenantCaller(r, "tenant-a"))
	}))
	defer ts.Close()

	dialWSEcho(t, "ws"+strings.TrimPrefix(ts.URL, "http")+"/api/v1/ws")
}
