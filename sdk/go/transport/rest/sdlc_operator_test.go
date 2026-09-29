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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCREST_OperatorVerbs(t *testing.T) {
	type hit struct{ method, path, query, body string }
	hits := make(chan hit, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hits <- hit{r.Method, r.URL.Path, r.URL.RawQuery, string(body)}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/sdlc/projects/p1/issues" {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"number":88,"title":"Follow-up","labels":["task:ready"]}`))
			return
		}
		_, _ = w.Write([]byte(`{"users":[{"login":"acme"},{"login":"lead"}],"count":2}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()

	resp, err := tr.Request(ctx, "quicbaas.SDLC", "FileIssue", &sdlc.FileIssueRequest{
		TenantID: "t", ProjectID: "p1", Title: "Follow-up", Labels: []string{"task:ready"}, Parent: 12,
	})
	if err != nil || resp.(*sdlc.ForgeIssue).Number != 88 {
		t.Fatalf("FileIssue: %+v %v", resp, err)
	}
	got := <-hits
	if got.method != http.MethodPost || got.path != "/api/v1/sdlc/projects/p1/issues" || got.query != "tenant_id=t" {
		t.Fatalf("FileIssue hit %+v", got)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(got.body), &sent); err != nil || sent["parent"] != float64(12) || sent["title"] != "Follow-up" {
		t.Fatalf("FileIssue body = %s (%v)", got.body, err)
	}
	if _, projected := sent["project_id"]; projected {
		t.Fatal("the body repeats the project the path already names")
	}

	resp, err = tr.Request(ctx, "quicbaas.SDLC", "Collaborators", &sdlc.CollaboratorsRequest{ProjectID: "p1"})
	if err != nil || resp.(*sdlc.CollaboratorsResponse).Count != 2 {
		t.Fatalf("Collaborators: %+v %v", resp, err)
	}
	if got := <-hits; got.method != http.MethodGet || got.path != "/api/v1/sdlc/projects/p1/collaborators" {
		t.Fatalf("Collaborators hit %+v", got)
	}
}

func TestSDLCREST_OperatorVerbsRefuseBadTypesAndSurfaceErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"sdlc: invalid title"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()
	for _, method := range []string{"FileIssue", "Collaborators"} {
		if _, err := tr.Request(ctx, "quicbaas.SDLC", method, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "FileIssue", &sdlc.FileIssueRequest{ProjectID: "p1", Title: "t"}); err == nil {
		t.Fatal("a 400 came back as a success")
	}
}
