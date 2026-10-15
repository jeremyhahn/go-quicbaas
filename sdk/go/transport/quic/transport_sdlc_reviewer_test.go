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

// TestRequest_ReviewerVerbsOverHTTP3 proves the three reviewer-override verbs
// reach the REST routes over HTTP/3 with the right methods and body.
func TestRequest_ReviewerVerbsOverHTTP3(t *testing.T) {
	type hit struct{ method, path, body string }
	hits := make(chan hit, 3)
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hits <- hit{r.Method, r.URL.Path, string(body)}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"project_id":"p1","pull_request":41,"profile":"qwen","overridden":true}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	ctx := context.Background()

	for _, call := range []struct {
		method, wantM string
		req           interface{}
	}{
		{"Reviewer", http.MethodGet, &sdlcclient.ReviewerRequest{ProjectID: "p1", PullRequest: 41}},
		{"SetReviewer", http.MethodPut, &sdlcclient.SetReviewerRequest{ProjectID: "p1", PullRequest: 41, Profile: "qwen"}},
		{"ClearReviewer", http.MethodDelete, &sdlcclient.ReviewerRequest{ProjectID: "p1", PullRequest: 41}},
	} {
		resp, err := tr.Request(ctx, "quicbaas.SDLC", call.method, call.req)
		require.NoError(t, err, call.method)
		selection, ok := resp.(*sdlcclient.ReviewerSelection)
		require.True(t, ok, "%s returned %T", call.method, resp)
		assert.Equal(t, "qwen", selection.Profile)
		got := <-hits
		assert.Equal(t, call.wantM, got.method, call.method)
		assert.Equal(t, "/api/v1/sdlc/projects/p1/pulls/41/reviewer", got.path, call.method)
		if call.method == "SetReviewer" {
			var body map[string]string
			require.NoError(t, json.Unmarshal([]byte(got.body), &body))
			assert.Equal(t, map[string]string{"profile": "qwen"}, body)
		}
	}
}

// TestRequest_ARebindOverHTTP3KeepsTheReviewerProfiles is the QUIC half of the
// reviewer_not_on_the_wire regression: what a read returned, a bind writes
// back whole.
func TestRequest_ARebindOverHTTP3KeepsTheReviewerProfiles(t *testing.T) {
	var mu sync.Mutex
	var held []byte
	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodPut {
			held, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(held)
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()
	ctx := context.Background()

	advisory := false
	want := sdlcclient.ProjectBinding{
		ProjectID: "p1",
		Reviewer:  sdlcclient.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "codex-tok"},
		Reviewers: []sdlcclient.ReviewerConfig{{Name: "qwen", Agent: "qwen-reviewer", Login: "q", TokenSecretRef: "q-tok", CountsTowardMerge: &advisory}},
	}
	_, err = tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlcclient.BindRequest{ProjectID: "p1", Binding: want})
	require.NoError(t, err)
	read, err := tr.Request(ctx, "quicbaas.SDLC", "Binding", &sdlcclient.GetBindingRequest{ProjectID: "p1"})
	require.NoError(t, err)
	rebound, err := tr.Request(ctx, "quicbaas.SDLC", "Bind", &sdlcclient.BindRequest{ProjectID: "p1", Binding: *read.(*sdlcclient.ProjectBinding)})
	require.NoError(t, err)
	got := rebound.(*sdlcclient.ProjectBinding)
	assert.Equal(t, want.Reviewer, got.Reviewer)
	assert.Equal(t, want.Reviewers, got.Reviewers)
}
