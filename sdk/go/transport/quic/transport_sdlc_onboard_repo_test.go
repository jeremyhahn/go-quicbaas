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

package quic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func TestRequest_OnboardRepoOverHTTP3PostsTheOnboarding(t *testing.T) {
	var mu sync.Mutex
	var method, path string
	var body map[string]any
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		method, path = r.Method, r.URL.Path
		if err := json.Unmarshal(raw, &body); err != nil {
			mu.Unlock()
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"p2","repo":"acme/web","steps":[{"step":"project","outcome":"created"}]}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(addr))
	require.NoError(t, err)
	defer tr.Close()
	resp, err := tr.Request(context.Background(), "quicbaas.SDLC", "OnboardRepo",
		&sdlcclient.RepoOnboardRequest{Repo: "web", Like: "p1", MaxConcurrency: 2, Paused: true, Preflight: true})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, http.MethodPost, method)
	assert.Equal(t, "/api/v1/sdlc/repos/web/onboarding", path)
	assert.Equal(t, "p1", body["like"])
	assert.EqualValues(t, 2, body["max_concurrency"])
	assert.Equal(t, true, body["paused"])
	assert.Equal(t, true, body["preflight"])
	report, ok := resp.(*sdlcclient.RepoOnboardingReport)
	require.True(t, ok, "response = %T", resp)
	assert.Equal(t, "acme/web", report.Repo)
	require.Len(t, report.Steps, 1)
	assert.Equal(t, sdlcclient.OnboardCreated, report.Steps[0].Outcome)
}
