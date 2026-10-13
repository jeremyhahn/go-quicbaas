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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// RotateSessionAccount over REST
// =============================================================================

// TestOrchestratorRotateSessionAccount_PostsToTheSessionSubresource pins the
// URL and body the SDK builds. The server registers the route as
// POST /api/v1/orchestrator/sessions/{sessionID}/rotate-account, and the PEP
// authorizes on that exact suffix — a path drift here silently changes which
// permission the call is checked against.
func TestOrchestratorRotateSessionAccount_PostsToTheSessionSubresource(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"s1","previous_account_id":"acct-a","account_id":"acct-b","strategy":"least_usage","mode":"resume"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)

	availableAt := time.Now().UTC().Add(25 * time.Minute).Truncate(time.Second)
	resp, err := tr.Request(context.Background(), "Orchestrator", "RotateSessionAccount",
		&orchestrator.RotateSessionAccountRequest{
			SessionID:    "s1",
			Reason:       "seat retired",
			HoldOutgoing: true,
			AvailableAt:  availableAt,
		})
	if err != nil {
		t.Fatalf("RotateSessionAccount: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/api/v1/orchestrator/sessions/s1/rotate-account" {
		t.Errorf("path = %s, want /api/v1/orchestrator/sessions/s1/rotate-account", gotPath)
	}
	if gotBody["reason"] != "seat retired" {
		t.Errorf("body reason = %v, want the operator's reason", gotBody["reason"])
	}
	if gotBody["hold_outgoing"] != true {
		t.Errorf("body hold_outgoing = %v, want true", gotBody["hold_outgoing"])
	}
	if _, ok := gotBody["available_at"]; !ok {
		t.Error("body dropped available_at")
	}

	result, ok := resp.(*orchestrator.RotateSessionAccountResponse)
	if !ok {
		t.Fatalf("response is %T, want *orchestrator.RotateSessionAccountResponse", resp)
	}
	if result.PreviousAccountID != "acct-a" || result.AccountID != "acct-b" {
		t.Errorf("rotation = %s -> %s, want acct-a -> acct-b", result.PreviousAccountID, result.AccountID)
	}
	if result.Mode != "resume" {
		t.Errorf("Mode = %q, want resume", result.Mode)
	}
}

func TestOrchestratorRotateSessionAccount_WrongRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be reached for a mistyped request, path %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "Orchestrator", "RotateSessionAccount",
		"not-the-right-type"); err == nil {
		t.Fatal("a mistyped request was accepted")
	}
}

// TestOrchestratorRotateSessionAccount_RequiresSessionID keeps the SDK from
// posting to ".../sessions//rotate-account", which the server would answer
// with a confusing 404 instead of a clear client-side error.
func TestOrchestratorRotateSessionAccount_RequiresSessionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("server must not be reached without a session id, path %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "Orchestrator", "RotateSessionAccount",
		&orchestrator.RotateSessionAccountRequest{}); err == nil {
		t.Fatal("an empty session id was accepted")
	}
}

func TestOrchestratorRotateSessionAccount_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"account: session is not rotatable"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "Orchestrator", "RotateSessionAccount",
		&orchestrator.RotateSessionAccountRequest{SessionID: "s1"}); err == nil {
		t.Fatal("a 409 from the server was reported as success")
	}
}

// TestOrchestratorSpawnSession_CarriesAccountAllow is the REST-SDK half of gap
// 2: the field has to survive the JSON encode or the enumerated rotation pool
// never reaches the server.
func TestOrchestratorSpawnSession_CarriesAccountAllow(t *testing.T) {
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"session_id":"s1","backend":"claude"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "Orchestrator", "SpawnSession",
		&orchestrator.SpawnSessionRequest{
			Backend:      "claude",
			AccountID:    "acct-a",
			AccountAllow: []string{"acct-a", "acct-b"},
		}); err != nil {
		t.Fatalf("SpawnSession: %v", err)
	}

	allow, ok := gotBody["account_allow"].([]interface{})
	if !ok {
		t.Fatalf("spawn body dropped account_allow: %v", gotBody)
	}
	if len(allow) != 2 || allow[0] != "acct-a" || allow[1] != "acct-b" {
		t.Fatalf("account_allow = %v, want [acct-a acct-b]", allow)
	}
}

func TestOrchestratorSpawnSession_CarriesKeep(t *testing.T) {
	bodies := make(chan map[string]interface{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"session_id":"s1","backend":"claude"}`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)

	for _, keep := range []bool{true, false} {
		if _, err := tr.Request(context.Background(), "Orchestrator", "SpawnSession",
			&orchestrator.SpawnSessionRequest{Backend: "claude", Keep: keep}); err != nil {
			t.Fatalf("SpawnSession(keep=%v): %v", keep, err)
		}
		body := <-bodies
		got, present := body["keep"]
		if keep && got != true {
			t.Errorf("spawn body keep = %v, want true", got)
		}
		if !keep && present {
			t.Errorf("an unkept spawn sent keep = %v, want it absent", got)
		}
	}
}
