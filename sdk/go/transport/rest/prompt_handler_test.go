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
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/prompt"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// promptAnswers are the bodies the fake server answers, by the last path
// segment.
var promptAnswers = map[string]string{
	"effective": `{"set":"s","name":"n","content":"body","origin":{"scope":"tenant","version":2},"chain":[{"scope":"tenant","present":true,"answered":true}]}`,
	"diff":      `{"set":"s","name":"n","same":false,"lines":[{"op":"insert","text":"added"}]}`,
	"history":   `{"set":"s","name":"n","revisions":[{"version":1,"author":"alice"}]}`,
	"s":         `{"set":"s","scope":"tenant","prompts":[{"name":"n","overridden":true}]}`,
}

const promptRecordAnswer = `{"set":"s","name":"n","version":2,"content":"body","follow_parent":false}`

// promptCall is what the fake server saw.
type promptCall struct {
	method, path, query string
	body                map[string]interface{}
}

func newPromptServer(t *testing.T) (*Transport, *promptCall) {
	t.Helper()
	seen := &promptCall{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.method, seen.path, seen.query = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		seen.body = nil
		if r.Body != nil && r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&seen.body); err != nil {
				t.Errorf("request body: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		segment := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		answer, ok := promptAnswers[segment]
		if !ok {
			answer = promptRecordAnswer
		}
		if _, err := w.Write([]byte(answer)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return newTestTransport(t, server), seen
}

func TestPromptRESTHandler_RoutesEveryVerb(t *testing.T) {
	tr, seen := newPromptServer(t)
	ctx := context.Background()
	addr := prompt.Address{TenantID: "t1", Scope: "project", ProjectID: "p 1", Set: "s", Name: "n"}
	bare := prompt.Address{Set: "s", Name: "n"}
	cases := []struct {
		verb      string
		req       interface{}
		wantM     string
		wantPath  string
		wantQuery string
		wantBody  map[string]interface{}
	}{
		{"List", &prompt.ListRequest{Set: "s"}, http.MethodGet, "/api/v1/prompts/s", "", nil},
		{"List", &prompt.ListRequest{TenantID: "t1", Scope: "project", ProjectID: "p1", Set: "s"}, http.MethodGet, "/api/v1/prompts/s", "project_id=p1&scope=project&tenant_id=t1", nil},
		{"Get", &prompt.GetRequest{Address: bare}, http.MethodGet, "/api/v1/prompts/s/n", "", nil},
		{"Get", &prompt.GetRequest{Address: addr, Version: 3}, http.MethodGet, "/api/v1/prompts/s/n", "project_id=p+1&scope=project&tenant_id=t1&version=3", nil},
		{"Effective", &addr, http.MethodGet, "/api/v1/prompts/s/n/effective", "project_id=p+1&scope=project&tenant_id=t1", nil},
		{"Diff", &bare, http.MethodGet, "/api/v1/prompts/s/n/diff", "", nil},
		{"History", &bare, http.MethodGet, "/api/v1/prompts/s/n/history", "", nil},
		{"Put", &prompt.PutRequest{Address: bare, Content: "body", Note: "why", ExpectedVersion: 1}, http.MethodPut, "/api/v1/prompts/s/n", "",
			map[string]interface{}{"content": "body", "note": "why", "expected_version": float64(1)}},
		{"Rollback", &prompt.RollbackRequest{Address: bare, ToVersion: 1, Note: "why", ExpectedVersion: 2}, http.MethodPost, "/api/v1/prompts/s/n/rollback", "",
			map[string]interface{}{"to_version": float64(1), "note": "why", "expected_version": float64(2)}},
		{"Reset", &prompt.ResetRequest{Address: bare, ExpectedVersion: 3}, http.MethodDelete, "/api/v1/prompts/s/n", "expected_version=3", nil},
		{"Reset", &prompt.ResetRequest{Address: bare, Note: "back to default", ExpectedVersion: 3}, http.MethodDelete, "/api/v1/prompts/s/n", "expected_version=3&note=back+to+default", nil},
	}
	for _, tc := range cases {
		resp, err := tr.Request(ctx, "quicbaas.Prompts", tc.verb, tc.req)
		if err != nil {
			t.Fatalf("%s: %v", tc.verb, err)
		}
		if seen.method != tc.wantM || seen.path != tc.wantPath || seen.query != tc.wantQuery {
			t.Fatalf("%s hit %s %s?%s, want %s %s?%s", tc.verb, seen.method, seen.path, seen.query, tc.wantM, tc.wantPath, tc.wantQuery)
		}
		if tc.wantBody != nil && !reflect.DeepEqual(seen.body, tc.wantBody) {
			t.Fatalf("%s body = %v, want %v", tc.verb, seen.body, tc.wantBody)
		}
		if resp == nil {
			t.Fatalf("%s: no answer", tc.verb)
		}
	}

	resp, err := tr.Request(ctx, "quicbaas.Prompts", "Effective", &addr)
	if err != nil {
		t.Fatal(err)
	}
	effective := resp.(*prompt.EffectivePrompt)
	if effective.Content != "body" || effective.Origin.Version != 2 || !effective.Chain[0].Answered {
		t.Fatalf("effective = %+v", effective)
	}
	resp, err = tr.Request(ctx, "quicbaas.Prompts", "Put", &prompt.PutRequest{Address: bare, Content: "body", Note: "why"})
	if err != nil || resp.(*prompt.Record).Version != 2 {
		t.Fatalf("put = %+v %v", resp, err)
	}
}

func TestPromptRESTHandler_RefusesUnknownMethodsAndRequestTypes(t *testing.T) {
	tr, seen := newPromptServer(t)
	ctx := context.Background()
	if _, err := tr.Request(ctx, "quicbaas.Prompts", "Delete", &prompt.Address{}); !errors.IsInvalidInput(err) {
		t.Fatalf("unknown method: %v", err)
	}
	for verb := range promptMethods {
		if _, err := tr.Request(ctx, "quicbaas.Prompts", verb, "wrong"); !errors.IsInvalidInput(err) {
			t.Fatalf("%s with a wrong request type: %v", verb, err)
		}
	}
	var nilAddr *prompt.Address
	if _, err := tr.Request(ctx, "quicbaas.Prompts", "Diff", nilAddr); !errors.IsInvalidInput(err) {
		t.Fatalf("nil request: %v", err)
	}
	if seen.method != "" {
		t.Fatal("a refused call reached the server")
	}
}

func TestPromptRESTHandler_MapsServerRefusals(t *testing.T) {
	statuses := map[int]func(error) bool{
		http.StatusBadRequest:         errors.IsInvalidInput,
		http.StatusForbidden:          errors.IsForbidden,
		http.StatusNotFound:           errors.IsNotFound,
		http.StatusConflict:           errors.IsConflict,
		http.StatusServiceUnavailable: errors.IsUnavailable,
	}
	for status, is := range statuses {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			if _, err := w.Write([]byte(`{"error":"refused"}`)); err != nil {
				t.Errorf("write: %v", err)
			}
		}))
		tr := newTestTransport(t, server)
		_, err := tr.Request(context.Background(), "quicbaas.Prompts", "Get", &prompt.GetRequest{Address: prompt.Address{Set: "s", Name: "n"}})
		server.Close()
		if !is(err) {
			t.Errorf("status %d answered %v", status, err)
		}
	}
}

func TestIsPromptsService(t *testing.T) {
	if !isPromptsService("quicbaas.Prompts") || !isPromptsService("Prompts") {
		t.Fatal("the Prompts service must route")
	}
	if isPromptsService("quicbaas.SDLC") || isPromptsService("quicbaas.Prompt") {
		t.Fatal("another service must not route to prompts")
	}
}

// TestPromptRESTHandler_CarriesEveryVerb, in both directions: a verb the
// transport does not route answers a routing miss, and a route the SDK
// cannot reach is dead code that looks like surface.
func TestPromptRESTHandler_CarriesEveryVerb(t *testing.T) {
	iface := reflect.TypeOf((*prompt.Client)(nil)).Elem()
	verbs := make([]string, 0, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		verbs = append(verbs, iface.Method(i).Name)
	}
	sort.Strings(verbs)
	declared := make(map[string]bool, len(verbs))
	for _, verb := range verbs {
		declared[verb] = true
		if _, ok := promptMethods[verb]; !ok {
			t.Errorf("the REST transport does not route prompt.%s", verb)
		}
	}
	for method := range promptMethods {
		if !declared[method] {
			t.Errorf("the REST transport routes %q, which the SDK interface does not declare", method)
		}
	}
	if !(&Transport{}).SupportsPrompts() {
		t.Fatal("the REST transport must declare the Prompts service")
	}
}
