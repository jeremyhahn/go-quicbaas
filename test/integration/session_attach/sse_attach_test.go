// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration

// Package session_attach contains the Phase 9 integration tests for
// session event attach. They exercise the live daemon over HTTPS.
//
// Full replay / tail / multi-subscriber / cross-tenant semantics live in
// the in-package unit tests at pkg/api/rest/handlers_session_events_test.go
// (6 tests, all passing). Those test the handler helpers directly with
// seeded events and are the authoritative coverage for SSE wire behavior.
// This integration layer confirms the attach endpoint is reachable and
// content negotiation works against a running daemon — the cheapest
// wire-level smoke that guards against regressions like the embed-path
// bug (#1) or a route being silently dropped at startup.
package session_attach

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// quicbaasURL returns the base URL of the running daemon.
func quicbaasURL() string {
	if u := os.Getenv("QUICBAAS_URL"); u != "" {
		return u
	}
	return "https://localhost:18080"
}

// httpClient trusts the daemon's self-signed dev cert. Test-only.
func httpClient() *http.Client {
	return helpers.NewHTTPClient(15 * time.Second)
}

// waitForReady polls /api/v1/health/ready until 200 or timeout.
func waitForReady(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := httpClient().Get(quicbaasURL() + "/api/v1/health/ready")
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("daemon not ready at %s after 30s — start it with `make run-local`", quicbaasURL())
}

// TestSessionAttachSSE_JSONHistoryReachable — smoke test over the wire.
// Proves the attach endpoint responds, content negotiation works, the
// session-event log is wired in, and tenant scoping accepts `default`.
//
// A nonexistent session in a real tenant returns 200 + {events: []} —
// that's the expected behavior for an empty log, and the assertion here
// also guards against regressions where the route gets dropped or the
// handler returns 500 on an empty session.
func TestSessionAttachSSE_JSONHistoryReachable(t *testing.T) {
	waitForReady(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	u := fmt.Sprintf(
		"%s/api/v1/orchestrator/sessions/phase9-integration-probe/events?tenant_id=default",
		quicbaasURL(),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected application/json, got %q", ct)
	}

	var body struct {
		Events    []json.RawMessage `json:"events"`
		NextSeq   uint64            `json:"next_seq"`
		SessionID string            `json:"session_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.SessionID != "phase9-integration-probe" {
		t.Fatalf("session_id round-trip mismatch: got %q", body.SessionID)
	}
	if len(body.Events) != 0 {
		t.Fatalf("expected empty event list for unseeded session, got %d events", len(body.Events))
	}
}
