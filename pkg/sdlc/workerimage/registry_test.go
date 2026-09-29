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

package workerimage

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	softwarefactoryworker "github.com/jeremyhahn/go-quicbaas/deployments/agents/software-factory-worker"
)

// npmServer answers GET /<escaped package>/latest from a table.
func npmServer(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := answers[r.URL.EscapedPath()]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNPMRegistry_LatestReadsTheScopedPackage(t *testing.T) {
	srv := npmServer(t, map[string]string{
		"/@anthropic-ai%2Fclaude-code/latest": `{"name":"@anthropic-ai/claude-code","version":"2.1.280"}`,
		"/@openai%2Fcodex/latest":             `{"version":"0.156.0"}`,
	})
	registry, err := NewNPMRegistry(srv.URL+"/", srv.Client())
	if err != nil {
		t.Fatalf("NewNPMRegistry: %v", err)
	}
	agents, err := LatestAgents(t.Context(), registry)
	if err != nil {
		t.Fatalf("LatestAgents: %v", err)
	}
	if agents != (softwarefactoryworker.Agents{ClaudeCode: "2.1.280", Codex: "0.156.0"}) {
		t.Fatalf("agents = %+v", agents)
	}
}

func TestNPMRegistry_Errors(t *testing.T) {
	srv := npmServer(t, map[string]string{
		"/bad-json/latest":    `{`,
		"/bad-version/latest": `{"version":"1.0.0; curl evil"}`,
		"/no-version/latest":  `{}`,
	})
	registry, err := NewNPMRegistry(srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewNPMRegistry: %v", err)
	}
	for _, pkg := range []string{"missing", "bad-json", "bad-version", "no-version"} {
		t.Run(pkg, func(t *testing.T) {
			if _, err := registry.Latest(t.Context(), pkg); !errors.Is(err, ErrRegistry) {
				t.Fatalf("err = %v, want ErrRegistry", err)
			}
		})
	}
	if _, err := registry.Latest(t.Context(), " "); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("an empty package name must be refused, got %v", err)
	}
}

func TestNewNPMRegistry_RefusesANonHTTPURL(t *testing.T) {
	for _, u := range []string{"", "registry.npmjs.org", "ftp://x", "https://"} {
		if _, err := NewNPMRegistry(u, nil); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("NewNPMRegistry(%q) = %v, want ErrInvalidConfig", u, err)
		}
	}
}

func TestLatestAgents_StopsAtTheFirstFailure(t *testing.T) {
	v := newFakeVersions("2.1.280", "0.156.0")
	v.err = errFake
	if _, err := LatestAgents(t.Context(), v); !errors.Is(err, errFake) {
		t.Fatalf("err = %v", err)
	}
	if v.calls != 1 {
		t.Fatalf("calls = %d, want the second lookup skipped", v.calls)
	}
}
