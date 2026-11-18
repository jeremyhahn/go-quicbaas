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

package fleet

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestHTTPHeartbeatSender_404NoReregisterer covers handleNotFound's early
// return when no Reregisterer hook is configured: a 404 surfaces a typed
// HeartbeatSendError with status 404 rather than attempting recovery.
func TestHTTPHeartbeatSender_404NoReregisterer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: srv.URL,
		TenantID:   "tenant-a",
		HTTPClient: tlsClientFromServer(srv),
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
		// No Reregisterer.
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	hbErr := sender.SendHeartbeat(context.Background(),
		captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	if hbErr == nil {
		t.Fatal("expected 404 error")
	}
	var se *HeartbeatSendError
	if !errors.As(hbErr, &se) {
		t.Fatalf("want HeartbeatSendError, got %T: %v", hbErr, hbErr)
	}
	if se.Status != http.StatusNotFound {
		t.Fatalf("status=%d want 404", se.Status)
	}
}

// TestHTTPHeartbeatSender_BuildEndpoint_NoIdentity covers buildEndpoint's
// empty-agent-id guard via SendHeartbeat with an identity lacking an
// AgentID.
func TestHTTPHeartbeatSender_BuildEndpoint_NoIdentity(t *testing.T) {
	t.Parallel()
	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: "https://control.test",
		TenantID:   "tenant-a",
		HTTPClient: http.DefaultClient,
		Tokens:     &staticToken{tokens: []string{"tok-1"}},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	hbErr := sender.SendHeartbeat(context.Background(),
		&orchestration.AgentIdentity{TenantID: "tenant-a"}, HeartbeatStatus{})
	if !errors.Is(hbErr, ErrHeartbeatNoIdentity) {
		t.Fatalf("want ErrHeartbeatNoIdentity, got %v", hbErr)
	}
}
