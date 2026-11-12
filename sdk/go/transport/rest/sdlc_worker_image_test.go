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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// workerImageStatusJSON is what the server's handler encodes: the refresh
// loop's workerimage.Status.
const workerImageStatusJSON = `{"serving":true,"running":false,"queued":true,` +
	`"last":{"trigger":"schedule","outcome":"promoted","started_at":"2026-09-22T10:00:00Z","finished_at":"2026-09-22T10:20:00Z",` +
	`"latest":{"claude_code":"2.1.280","codex":"0.156.0"},"candidate":"quicbaas-software-factory-worker:0.11.0-alpha-cc2.1.280-cx0.156.0",` +
	`"reference":"reg:3000/o/w:0.11.0-alpha-cc2.1.280-cx0.156.0@sha256:abc","digest":"sha256:abc",` +
	`"probes":[{"name":"toolchain, signing and both agents","passed":true}],` +
	`"promotions":[{"tenant_id":"acme","project_id":"p1","from":["w:old"],"to":"reg:3000/o/w@sha256:abc"}]}}`

func TestWorkerImageVerbsReachTheServersRoutes(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write([]byte(workerImageStatusJSON))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	ctx := context.Background()

	for method, want := range map[string][2]string{
		"WorkerImageStatus":  {http.MethodGet, "/api/v1/sdlc/worker-image"},
		"RefreshWorkerImage": {http.MethodPost, "/api/v1/sdlc/worker-image/refresh"},
	} {
		resp, err := tr.Request(ctx, "quicbaas.SDLC", method, nil)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if gotMethod != want[0] || gotPath != want[1] || gotQuery != "" {
			t.Fatalf("%s hit %s %s?%s, want %s %s with no tenant", method, gotMethod, gotPath, gotQuery, want[0], want[1])
		}
		status, ok := resp.(*sdlc.WorkerImageStatus)
		if !ok {
			t.Fatalf("%s returned %T", method, resp)
		}
		last := status.Last
		if !status.Serving || !status.Queued || last == nil || last.Outcome != "promoted" ||
			last.Latest.ClaudeCode != "2.1.280" || last.Digest != "sha256:abc" || last.FinishedAt.IsZero() ||
			len(last.Probes) != 1 || last.Promotions[0].From[0] != "w:old" {
			t.Fatalf("%s decoded %+v / %+v", method, status, last)
		}
	}
}

func TestWorkerImageVerbsSurfaceRefusals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"worker image: a refresh is already in progress"}`))
	}))
	defer server.Close()
	tr := newTestTransport(t, server)
	for _, method := range []string{"WorkerImageStatus", "RefreshWorkerImage"} {
		if _, err := tr.Request(context.Background(), "quicbaas.SDLC", method, nil); err == nil {
			t.Errorf("%s answered on a node that refused", method)
		}
	}
}
