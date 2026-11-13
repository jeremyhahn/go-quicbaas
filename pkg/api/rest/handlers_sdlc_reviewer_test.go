//go:build rest

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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
)

// profiledRESTBinding is restBinding with a codex default and an advisory qwen
// alternate.
func profiledRESTBinding(id string) sdlc.ProjectBinding {
	binding := restBinding(id)
	binding.Reviewer = sdlc.ReviewerConfig{Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "codex-tok"}
	binding.Reviewers = []sdlc.ReviewerConfig{{
		Name: "qwen", Agent: "qwen-reviewer", Login: "qwen-reviewer", TokenSecretRef: "qwen-tok",
	}}
	return binding
}

// seedBindableProject gives the rig a project Bind will accept: an
// allowlisted remote and signed commits.
func seedBindableProject(id string) {
	sdlcRESTProjectRecords[id] = &orchestration.Project{
		ID: id, TenantID: "tenant-a", Name: id,
		Git: orchestration.ProjectGit{
			RepoURL: "https://forge.test/acme/" + id + ".git",
			Author:  orchestration.GitAuthor{Name: "agent", Email: "agent@example.test"},
			Signing: orchestration.CommitSigning{Enabled: true, Key: orchestration.KMSKeyRef{Name: "agent"}},
		},
	}
}

// TestSDLCBinding_ARebindKeepsTheReviewerProfiles is the REST half of the
// regression the reviewer_not_on_the_wire note records: a bind replaces the
// record, so every field it carries has to survive a read-modify-write.
func TestSDLCBinding_ARebindKeepsTheReviewerProfiles(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBindableProject("p1")

	rec := sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", profiledRESTBinding("p1"))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// Read it back and write it back unchanged, as every client editor does.
	rec = sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/binding", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var read sdlc.ProjectBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &read))
	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", read)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var rebound sdlc.ProjectBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rebound))
	want := profiledRESTBinding("p1")
	assert.Equal(t, want.Reviewer, rebound.Reviewer)
	assert.Equal(t, want.Reviewers, rebound.Reviewers)
}

func TestSDLCReviewer_Refusals(t *testing.T) {
	server, svc := setupSDLCServer(t)
	binding := profiledRESTBinding("p1")
	require.NoError(t, svc.Store().PutBinding(context.Background(), "tenant-a", &binding))

	cases := []struct {
		name, tenant, method, path string
		body                       any
		want                       int
	}{
		{"an unknown profile", "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/pulls/41/reviewer", SDLCReviewerOverrideRequest{Profile: "gpt-9"}, http.StatusBadRequest},
		{"no profile", "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/pulls/41/reviewer", SDLCReviewerOverrideRequest{}, http.StatusBadRequest},
		{"a body that is not JSON", "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/pulls/41/reviewer", "not an object", http.StatusBadRequest},
		{"a pull request that is not a number", "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/pulls/abc/reviewer", nil, http.StatusBadRequest},
		{"pull request zero", "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/pulls/0/reviewer", nil, http.StatusBadRequest},
		{"an unbound project", "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/absent/pulls/41/reviewer", nil, http.StatusNotFound},
		{"another tenant's project", "tenant-b", http.MethodPut, "/api/v1/sdlc/projects/p1/pulls/41/reviewer", SDLCReviewerOverrideRequest{Profile: "qwen"}, http.StatusNotFound},
		{"another tenant named in the query", "tenant-b", http.MethodGet, "/api/v1/sdlc/projects/p1/pulls/41/reviewer?tenant_id=tenant-a", nil, http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := sdlcCall(t, server, c.tenant, c.method, c.path, c.body)
			assert.Equal(t, c.want, rec.Code, rec.Body.String())
		})
	}
}

func TestSDLCReviewer_UnavailableModuleAnswers503(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.SetSDLCService(nil)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := sdlcCall(t, server, "tenant-a", method, "/api/v1/sdlc/projects/p1/pulls/41/reviewer", nil)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, method)
	}
}
