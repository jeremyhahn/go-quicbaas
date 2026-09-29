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
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
)

// sdlcRESTIssueAuditor records the filings the REST rig's service audits.
type sdlcRESTIssueAuditor struct {
	mu     sync.Mutex
	events []sdlc.IssueFiledEvent
}

func (a *sdlcRESTIssueAuditor) RecordIssueFiled(_ context.Context, event sdlc.IssueFiledEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
}

func (a *sdlcRESTIssueAuditor) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.events)
}

// sdlcRESTAudit is the auditor setupSDLCServer wires.
var sdlcRESTAudit *sdlcRESTIssueAuditor

func TestSDLCFileIssue_FilesOnTheBoundRepository(t *testing.T) {
	server, svc := setupSDLCServer(t)
	binding := restBinding("p1")
	require.NoError(t, svc.Store().PutBinding(context.Background(), "tenant-a", &binding))
	sdlcRESTForge.PutLabel("task:ready", "00ff00", "")
	sdlcRESTForge.PutIssue(12, "parent")

	rec := sdlcCall(t, server, "tenant-a", http.MethodPost, "/api/v1/sdlc/projects/p1/issues",
		SDLCFileIssueRequest{Title: "Follow-up", Body: "rest", Labels: []string{"task:ready"}, Parent: 12})
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var issue sdlc.ForgeIssue
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &issue))
	assert.Equal(t, "Follow-up", issue.Title)
	assert.Equal(t, []string{"task:ready"}, issue.Labels)
	assert.Equal(t, "rest\n\nParent: #12", issue.Body)
	assert.Equal(t, 1, sdlcRESTAudit.count())
}

func TestSDLCFileIssue_Refusals(t *testing.T) {
	server, svc := setupSDLCServer(t)
	binding := restBinding("p1")
	require.NoError(t, svc.Store().PutBinding(context.Background(), "tenant-a", &binding))

	cases := []struct {
		name, tenant, path string
		body               any
		want               int
	}{
		{"no title", "tenant-a", "/api/v1/sdlc/projects/p1/issues", SDLCFileIssueRequest{}, http.StatusBadRequest},
		{"an undefined label", "tenant-a", "/api/v1/sdlc/projects/p1/issues", SDLCFileIssueRequest{Title: "t", Labels: []string{"nope"}}, http.StatusBadRequest},
		{"a missing parent", "tenant-a", "/api/v1/sdlc/projects/p1/issues", SDLCFileIssueRequest{Title: "t", Parent: 404}, http.StatusBadRequest},
		{"an unbound project", "tenant-a", "/api/v1/sdlc/projects/p9/issues", SDLCFileIssueRequest{Title: "t"}, http.StatusNotFound},
		{"no tenant", "", "/api/v1/sdlc/projects/p1/issues", SDLCFileIssueRequest{Title: "t"}, http.StatusForbidden},
		{"a malformed body", "tenant-a", "/api/v1/sdlc/projects/p1/issues", "not an object", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := sdlcCall(t, server, tc.tenant, http.MethodPost, tc.path, tc.body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
	assert.Equal(t, 0, sdlcRESTForge.IssueCount(), "a refused filing reached the forge")
}

func TestSDLCCollaborators_ListsTheOwnerThenCollaborators(t *testing.T) {
	server, svc := setupSDLCServer(t)
	binding := restBinding("p1")
	require.NoError(t, svc.Store().PutBinding(context.Background(), "tenant-a", &binding))
	sdlcRESTForge.PutCollaborator("lead-agent")

	rec := sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/collaborators", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp SDLCCollaboratorsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 2, resp.Count)
	assert.Equal(t, "acme", resp.Users[0].Login)
	assert.Equal(t, "lead-agent", resp.Users[1].Login)
}

func TestSDLCCollaborators_Refusals(t *testing.T) {
	server, _ := setupSDLCServer(t)
	rec := sdlcCall(t, server, "tenant-a", http.MethodGet, "/api/v1/sdlc/projects/p1/collaborators", nil)
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	rec = sdlcCall(t, server, "", http.MethodGet, "/api/v1/sdlc/projects/p1/collaborators", nil)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
}

func TestSDLCBinding_RulersSurviveARebindAndAreChecked(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBindableProject("p1")
	sdlcRESTForge.PutCollaborator("lead-agent")

	binding := restBinding("p1")
	binding.Rulers = []string{"lead-agent"}
	rec := sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", binding)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var bound sdlc.ProjectBinding
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &bound))
	assert.Equal(t, []string{"lead-agent"}, bound.Rulers)

	binding.Rulers = []string{"stranger"}
	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", binding)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	binding.Rulers = []string{"not a login"}
	rec = sdlcCall(t, server, "tenant-a", http.MethodPut, "/api/v1/sdlc/projects/p1/binding", binding)
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
