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
	"reflect"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// TestSDLCREST_ARebindKeepsTheReviewerProfiles drives bind, read, rebind
// against a server that stores the body it was PUT, and proves the alternates
// — and the presence of counts_toward_merge — survive the round trip.
func TestSDLCREST_ARebindKeepsTheReviewerProfiles(t *testing.T) {
	var mu sync.Mutex
	var held []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			held = body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(held)
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()

	advisory := false
	want := sdlc.ProjectBinding{
		ProjectID: "p1",
		Reviewer:  sdlc.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "codex-tok"},
		Reviewers: []sdlc.ReviewerConfig{
			{Name: "qwen", Agent: "qwen-reviewer", Login: "qwen-reviewer", TokenSecretRef: "qwen-tok", CountsTowardMerge: &advisory},
			{Name: "unset", Agent: "codex-reviewer", Login: "unset-reviewer", TokenSecretRef: "unset-tok"},
		},
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlc.BindRequest{ProjectID: "p1", Binding: want}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	read, err := tr.Request(ctx, "quicbaas.SDLC", "Binding", &sdlc.GetBindingRequest{ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	rebound, err := tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlc.BindRequest{ProjectID: "p1", Binding: *read.(*sdlc.ProjectBinding)})
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	got := rebound.(*sdlc.ProjectBinding)
	if !reflect.DeepEqual(got.Reviewer, want.Reviewer) || !reflect.DeepEqual(got.Reviewers, want.Reviewers) {
		t.Fatalf("after a rebind: %+v / %+v, want %+v / %+v", got.Reviewer, got.Reviewers, want.Reviewer, want.Reviewers)
	}
	var wire map[string]any
	mu.Lock()
	err = json.Unmarshal(held, &wire)
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	alternates, ok := wire["reviewers"].([]any)
	if !ok || len(alternates) != 2 {
		t.Fatalf("the server was PUT %v reviewers", wire["reviewers"])
	}
	if _, present := alternates[1].(map[string]any)["counts_toward_merge"]; present {
		t.Fatal("an unset counts_toward_merge was written as a value")
	}
	if v, present := alternates[0].(map[string]any)["counts_toward_merge"]; !present || v != false {
		t.Fatalf("an explicit false was not written: %v", alternates[0])
	}
}

func TestSDLCREST_ReviewerVerbsReachTheServersPaths(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"p1","pull_request":41,"profile":"qwen","overridden":true,"counts_toward_merge":false,"status_context":"quicbaas/review/qwen"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		method, wantM string
		req           interface{}
	}{
		{"Reviewer", http.MethodGet, &sdlc.ReviewerRequest{TenantID: "t1", ProjectID: "p1", PullRequest: 41}},
		{"SetReviewer", http.MethodPut, &sdlc.SetReviewerRequest{TenantID: "t1", ProjectID: "p1", PullRequest: 41, Profile: "qwen"}},
		{"ClearReviewer", http.MethodDelete, &sdlc.ReviewerRequest{TenantID: "t1", ProjectID: "p1", PullRequest: 41}},
	}
	for _, tc := range cases {
		resp, err := tr.Request(ctx, "quicbaas.SDLC", tc.method, tc.req)
		if err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
		if gotMethod != tc.wantM || gotPath != "/api/v1/sdlc/projects/p1/pulls/41/reviewer" || gotQuery != "tenant_id=t1" {
			t.Fatalf("%s hit %s %s?%s", tc.method, gotMethod, gotPath, gotQuery)
		}
		if selection := resp.(*sdlc.ReviewerSelection); selection.Profile != "qwen" || !selection.Overridden {
			t.Fatalf("%s decoded %+v", tc.method, selection)
		}
		if tc.method == "SetReviewer" && (gotBody["profile"] != "qwen" || len(gotBody) != 1) {
			t.Fatalf("the set body was %v, want the profile alone", gotBody)
		}
	}
	for _, method := range []string{"Reviewer", "SetReviewer", "ClearReviewer"} {
		if _, err := tr.Request(ctx, "quicbaas.SDLC", method, "wrong type"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
}
