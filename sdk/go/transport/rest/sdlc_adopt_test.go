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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestSDLCREST_AdoptIssuePostsToTheProjectsAdoptRoute: the path names the
// project and the tenant, the body the rest, and the answer decodes.
func TestSDLCREST_AdoptIssuePostsToTheProjectsAdoptRoute(t *testing.T) {
	var method, path, tenant string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, tenant = r.Method, r.URL.Path, r.URL.Query().Get("tenant_id")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"task_id":"task-1","issue":141,"pull_request":194,"branch":"issue/141-x"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)

	resp, err := tr.Request(context.Background(), "quicbaas.SDLC", "AdoptIssue",
		&sdlc.AdoptIssueRequest{TenantID: "t1", ProjectID: "p1", Issue: 141, PullRequest: 194, Ruling: "go"})
	if err != nil {
		t.Fatalf("AdoptIssue: %v", err)
	}
	if method != http.MethodPost || path != "/api/v1/sdlc/projects/p1/adopt" || tenant != "t1" {
		t.Fatalf("request = %s %s tenant %q", method, path, tenant)
	}
	if body["issue"] != float64(141) || body["pull_request"] != float64(194) || body["ruling"] != "go" {
		t.Fatalf("body = %v", body)
	}
	if got := resp.(*sdlc.Adoption); got.TaskID != "task-1" || got.Branch != "issue/141-x" {
		t.Fatalf("adoption = %+v", got)
	}
}

func TestSDLCREST_AdoptIssueRefusesAWrongRequestAndSurfacesAConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"the issue is closed"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "quicbaas.SDLC", "AdoptIssue", &sdlc.ReviewerRequest{}); err == nil {
		t.Fatal("a wrong request type was sent")
	}
	if _, err := tr.Request(context.Background(), "quicbaas.SDLC", "AdoptIssue", &sdlc.AdoptIssueRequest{ProjectID: "p1", Issue: 1}); err == nil {
		t.Fatal("a 409 was not surfaced")
	}
}
