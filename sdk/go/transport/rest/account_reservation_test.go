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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/account"
)

// TestAccountHandlerSessionReservationsRoundTrip drives reserve and release
// against the routes the control plane serves.
func TestAccountHandlerSessionReservationsRoundTrip(t *testing.T) {
	var gotMethod, gotPath string
	gotExpected := -1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.Method == http.MethodPost {
			var body struct {
				ExpectedLive *int `json:"expected_live"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode body: %v", err)
			}
			if body.ExpectedLive != nil {
				gotExpected = *body.ExpectedLive
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"reservation_id":"r1"}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()
	handler := newAccountRESTHandler(newTestTransport(t, server))
	ctx := context.Background()

	three := 3
	resp, err := handler.handleRequest(ctx, "ReserveSession",
		&account.ReserveSessionRequest{TenantID: "t1", AccountID: "acct-1", ExpectedLive: &three})
	if err != nil {
		t.Fatalf("ReserveSession: %v", err)
	}
	if r, ok := resp.(*account.SessionReservation); !ok || r.ReservationID != "r1" {
		t.Fatalf("ReserveSession = %#v", resp)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/agent-accounts/acct-1/reservations" || gotExpected != 3 {
		t.Fatalf("request = %s %s expected_live=%d", gotMethod, gotPath, gotExpected)
	}

	if _, err := handler.handleRequest(ctx, "ReleaseSession",
		&account.ReleaseSessionRequest{TenantID: "t1", AccountID: "acct-1", ReservationID: "r1"}); err != nil {
		t.Fatalf("ReleaseSession: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/api/v1/agent-accounts/acct-1/reservations/r1" {
		t.Fatalf("request = %s %s", gotMethod, gotPath)
	}
}

func TestAccountHandlerSessionReservationsRejectTheWrongType(t *testing.T) {
	handler := newAccountRESTHandler(&Transport{})
	for _, method := range []string{"ReserveSession", "ReleaseSession"} {
		if _, err := handler.handleRequest(context.Background(), method, "not-a-request"); err == nil {
			t.Errorf("%s accepted a wrong-typed request", method)
		}
	}
}
