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
	"sync"
	"testing"

	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
)

// servicesRoute records one request the services server received.
type servicesRoute struct{ method, path, body string }

// servicesServer answers each request with a record named after the path,
// in the JSON shape quicbaasd's handlers write, and records the requests.
func servicesServer(t *testing.T) (*httptest.Server, func() []servicesRoute) {
	t.Helper()
	var mu sync.Mutex
	var got []servicesRoute
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, servicesRoute{r.Method, r.URL.EscapedPath(), string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/services":
			_, _ = io.WriteString(w, `{"services":[{"tenant":"acme","name":"web","state":"running","revision":1}]}`)
		case r.URL.Path == "/api/v1/services/missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found","code":"NOT_FOUND"}`)
		default:
			if r.Method == http.MethodPost && r.URL.Path == "/api/v1/services" {
				w.WriteHeader(http.StatusCreated)
			}
			_, _ = io.WriteString(w, `{"tenant":"acme","name":"web","state":"running","revision":2,"restarts":1,`+
				`"grants":{"name":"name:acme/web"},"updated_at":"2026-11-14T00:00:00Z","key_id":"k",`+
				`"manifest":{"name":"web","tenant":"acme"}}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []servicesRoute {
		mu.Lock()
		defer mu.Unlock()
		return append([]servicesRoute(nil), got...)
	}
}

func TestRESTServices_RoutesAndRecords(t *testing.T) {
	srv, routes := servicesServer(t)
	client := servicesclient.NewClient(newTestTransport(t, srv))
	ctx := context.Background()
	signed := &servicesclient.SignedManifest{Payload: []byte(`{"name":"web"}`), KeyID: "k", Signature: []byte{1, 2}}

	rec, err := client.Deploy(ctx, signed)
	if err != nil {
		t.Fatal(err)
	}
	if rec.State != "running" || rec.Revision != 2 || rec.Grants["name"] != "name:acme/web" || rec.UpdatedAt.IsZero() {
		t.Fatalf("record = %+v", rec)
	}
	var m struct{ Name string }
	if err := json.Unmarshal(rec.Manifest, &m); err != nil || m.Name != "web" {
		t.Fatalf("manifest %s, %v", rec.Manifest, err)
	}
	if _, err := client.Redeploy(ctx, signed); err != nil {
		t.Fatal(err)
	}
	for _, verb := range []func(context.Context, string) (*servicesclient.Record, error){
		client.Start, client.Stop, client.Resume, client.Restart, client.Get,
	} {
		if _, err := verb(ctx, "web"); err != nil {
			t.Fatal(err)
		}
	}
	if list, err := client.List(ctx); err != nil || len(list.Services) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := client.Undeploy(ctx, "web"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Get(ctx, "missing"); err == nil {
		t.Fatal("a 404 read succeeded")
	}

	want := []struct{ method, path string }{
		{http.MethodPost, "/api/v1/services"}, {http.MethodPut, "/api/v1/services"},
		{http.MethodPost, "/api/v1/services/web/start"}, {http.MethodPost, "/api/v1/services/web/stop"},
		{http.MethodPost, "/api/v1/services/web/resume"}, {http.MethodPost, "/api/v1/services/web/restart"},
		{http.MethodGet, "/api/v1/services/web"}, {http.MethodGet, "/api/v1/services"},
		{http.MethodDelete, "/api/v1/services/web"}, {http.MethodGet, "/api/v1/services/missing"},
	}
	got := routes()
	if len(got) != len(want) {
		t.Fatalf("got %d requests, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].method != w.method || got[i].path != w.path {
			t.Fatalf("request %d = %s %s, want %s %s", i, got[i].method, got[i].path, w.method, w.path)
		}
	}
	var body struct{ Manifest servicesclient.SignedManifest }
	if err := json.Unmarshal([]byte(got[0].body), &body); err != nil || body.Manifest.KeyID != "k" || string(body.Manifest.Payload) != `{"name":"web"}` {
		t.Fatalf("deploy body %s, %v", got[0].body, err)
	}
}

func TestRESTServices_RefusesUnknownMethodsAndWrongTypes(t *testing.T) {
	srv, _ := servicesServer(t)
	tr := newTestTransport(t, srv)
	if _, err := tr.handleServicesRequest(context.Background(), "Watch", nil); err == nil {
		t.Fatal("an unknown method was served")
	}
	if _, err := tr.handleServicesRequest(context.Background(), "Deploy", &servicesclient.NameRequest{}); err == nil {
		t.Fatal("a name request deployed")
	}
	if _, err := tr.handleServicesRequest(context.Background(), "Start", &servicesclient.DeployRequest{}); err == nil {
		t.Fatal("a deploy request started")
	}
}
