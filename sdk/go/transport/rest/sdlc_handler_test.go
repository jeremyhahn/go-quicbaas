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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestSDLCHandler_ReachesTheServersPaths(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.URL.Path == "/api/v1/sdlc/tasks/t1/delivery":
			_, _ = w.Write([]byte(`{"task_id":"t1","attempt":1,"branch":"b","pull_request":{"number":3,"state":"open"}}`))
		default:
			_, _ = w.Write([]byte(`{"project_id":"p1","forge":{"type":"gitea","trigger_label":"task:ready"}}`))
		}
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()
	binding := sdlc.ProjectBinding{ProjectID: "p1", Forge: sdlc.ForgeConfig{Type: "gitea", Owner: "o", Repo: "r", TriggerLabel: "task:ready"}}
	cases := []struct {
		method    string
		req       interface{}
		wantM     string
		wantPath  string
		wantQuery string
	}{
		{"Bind", &sdlc.BindRequest{TenantID: "t1", ProjectID: "p1", Binding: binding}, http.MethodPut, "/api/v1/sdlc/projects/p1/binding", "tenant_id=t1"},
		{"Binding", &sdlc.GetBindingRequest{TenantID: "t1", ProjectID: "p1"}, http.MethodGet, "/api/v1/sdlc/projects/p1/binding", "tenant_id=t1"},
		{"Unbind", &sdlc.UnbindRequest{TenantID: "t1", ProjectID: "p1"}, http.MethodDelete, "/api/v1/sdlc/projects/p1/binding", "tenant_id=t1"},
		{"Delivery", &sdlc.GetDeliveryRequest{TenantID: "t1", TaskID: "t1"}, http.MethodGet, "/api/v1/sdlc/tasks/t1/delivery", "tenant_id=t1"},
		{"Binding", &sdlc.GetBindingRequest{ProjectID: "p 2"}, http.MethodGet, "/api/v1/sdlc/projects/p%202/binding", ""},
	}
	for _, tc := range cases {
		resp, err := tr.Request(ctx, "quicbaas.SDLC", tc.method, tc.req)
		if err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
		if gotMethod != tc.wantM || gotPath != tc.wantPath || gotQuery != tc.wantQuery {
			t.Fatalf("%s hit %s %s?%s, want %s %s?%s", tc.method, gotMethod, gotPath, gotQuery, tc.wantM, tc.wantPath, tc.wantQuery)
		}
		switch tc.method {
		case "Bind":
			if gotBody["project_id"] != "p1" || resp.(*sdlc.ProjectBinding).ProjectID != "p1" {
				t.Fatalf("bind body/response: %v %+v", gotBody, resp)
			}
		case "Unbind":
			if !resp.(*sdlc.UnbindResponse).Unbound {
				t.Fatal("unbind response")
			}
		case "Delivery":
			if resp.(*sdlc.Delivery).PullRequest.Number != 3 {
				t.Fatalf("delivery response: %+v", resp)
			}
		}
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "Nope", nil); err == nil {
		t.Fatal("unknown method accepted")
	}
	for _, tc := range []struct{ method string }{{"Bind"}, {"Unbind"}, {"Binding"}, {"Delivery"}} {
		if _, err := tr.Request(ctx, "quicbaas.SDLC", tc.method, "wrong type"); err == nil {
			t.Fatalf("%s accepted the wrong request type", tc.method)
		}
	}
	if !tr.SupportsSDLC() || !isSDLCService("quicbaas.SDLC") || isSDLCService("quicbaas.Project") {
		t.Fatal("service declaration")
	}
}

// TestSDLCOnboardingHandler_ReachesTheServersPaths pins the two new routes
// and their METHODS.
//
// The method is the part that matters: the read must be a GET and the write
// a POST, because the whole value of a conformance surface is that asking
// what state a repository is in cannot change it.
func TestSDLCOnboardingHandler_ReachesTheServersPaths(t *testing.T) {
	if got := sdlcOnboardingPath("p1") + tenantQuery("t"); got != "/api/v1/sdlc/projects/p1/onboarding?tenant_id=t" {
		t.Errorf("onboarding path = %q", got)
	}
	if got := sdlcConformancePath("p1") + tenantQuery("t"); got != "/api/v1/sdlc/projects/p1/conformance?tenant_id=t" {
		t.Errorf("conformance path = %q", got)
	}
	// A project id is operator input and must not reshape the path.
	if got := sdlcOnboardingPath("a/b"); got != "/api/v1/sdlc/projects/a%2Fb/onboarding" {
		t.Errorf("onboarding path with a slash = %q, want the id escaped", got)
	}
}

// TestSDLCOnboardingHandler_RefusesTheWrongRequestType is the error half.
func TestSDLCOnboardingHandler_RefusesTheWrongRequestType(t *testing.T) {
	h := &sdlcRESTHandler{}
	if _, err := h.handleOnboard(context.Background(), &sdlc.ConformanceRequest{}); err == nil {
		t.Error("Onboard accepted the wrong request type")
	}
	if _, err := h.handleConformance(context.Background(), &sdlc.OnboardRequest{}); err == nil {
		t.Error("Conformance accepted the wrong request type")
	}
}

// TestSDLCOpenWorkHandler_GetsTheServersRoute pins the open-work route as a
// GET with the tenant in the query, decoded into the SDK's own type.
func TestSDLCOpenWorkHandler_GetsTheServersRoute(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"p1","agent":"agent-api","pull_requests":[{"number":7,"ci_state":"success"}],"issues":[{"number":3,"labels":["task:blocked"]}]}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)

	resp, err := tr.Request(context.Background(), "quicbaas.SDLC", "OpenWork", &sdlc.OpenWorkRequest{TenantID: "t1", ProjectID: "p 1"})
	if err != nil {
		t.Fatalf("OpenWork: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/sdlc/projects/p%201/open-work" || gotQuery != "tenant_id=t1" {
		t.Fatalf("OpenWork hit %s %s?%s", gotMethod, gotPath, gotQuery)
	}
	work := resp.(*sdlc.OpenWork)
	if work.Agent != "agent-api" || len(work.PullRequests) != 1 || work.PullRequests[0].CIState != "success" || len(work.Issues) != 1 {
		t.Fatalf("OpenWork response: %+v", work)
	}
	if _, err := (&sdlcRESTHandler{}).handleOpenWork(context.Background(), &sdlc.ConformanceRequest{}); err == nil {
		t.Error("OpenWork accepted the wrong request type")
	}
}
