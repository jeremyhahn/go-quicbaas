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
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// registerForgeIssueSourceKind registers sdlc.TaskSourceKindForgeIssue with
// the process-wide task source registry — normally done at server boot
// (pkg/server/lifecycle_sdlc.go) — so a task seeded with a forge-issue
// Source validates in this package's test binary. Idempotent: only the
// first call does anything.
var registerForgeIssueSourceKindOnce sync.Once

type nullTaskSource struct{}

func (nullTaskSource) Name() string { return "attention-test-forge-issue-source" }
func (nullTaskSource) Poll(context.Context, *orchestration.Project) ([]orchestration.TaskSourceSpec, error) {
	return nil, nil
}

func registerForgeIssueSourceKind() {
	registerForgeIssueSourceKindOnce.Do(func() {
		_ = orchestration.RegisterTaskSource(sdlc.TaskSourceKindForgeIssue, nullTaskSource{})
	})
}

// attentionCall routes a request through only the two attention endpoints,
// mirroring sdlcCall's minimal-router convention.
func attentionCall(t *testing.T, server *Server, tenant, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	// resolveOrchestratorTenant reads tenant_id from the query, not from
	// iam.ContextWithTenant (that carries the SDLC binding's tenant guard;
	// the orchestrator surface's tenant resolution is its own, see
	// resolveOrchestratorTenant in handlers_orchestrator.go).
	if tenant != "" {
		if strings.Contains(path, "?") {
			path += "&tenant_id=" + tenant
		} else {
			path += "?tenant_id=" + tenant
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if tenant != "" {
		req = req.WithContext(tenantctx.WithTenant(req.Context(), tenant))
	}
	r := chi.NewRouter()
	r.Get("/api/v1/orchestrator/attention", server.HandleListAttention)
	r.Post("/api/v1/orchestrator/attention/{taskID}/resolve", server.HandleResolveAttention)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// decodeJSON unmarshals a recorded response body, failing the test on error.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, v any) error {
	t.Helper()
	return json.Unmarshal(rec.Body.Bytes(), v)
}

// seedBlockedTask creates and parks a task through the real task service the
// rig's SDLCServiceConfig.Tasks wires (sdlcRESTTasksService), with a forge
// issue provenance so the item carries an IssueRef.
func seedBlockedTask(t *testing.T, tenant, projectID, taskID, issueRef, reason string) *orchestration.Task {
	t.Helper()
	registerForgeIssueSourceKind()
	ctx := context.Background()
	task := &orchestration.Task{
		ID: taskID, TenantID: tenant, ProjectID: projectID, Subject: "a parked task",
		Status: orchestration.TaskStatusInProgress,
		Source: &orchestration.TaskSourceRef{Kind: sdlc.TaskSourceKindForgeIssue, Ref: issueRef},
	}
	require.NoError(t, sdlcRESTTaskStore.CreateTask(ctx, task))
	require.NoError(t, sdlcRESTTasksService.BlockTask(ctx, tenant, taskID, orchestration.TaskBlockKindNeedsInfo, reason))
	got, err := sdlcRESTTasksService.GetTask(ctx, tenant, taskID)
	require.NoError(t, err)
	return got
}

func TestHandleListAttention_ReturnsBlockedTasksPagedAndFaceted(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBlockedTask(t, "tenant-a", "p1", "task-att-1", "acme/p1#5",
		"the owner must rule on this")

	rec := attentionCall(t, server, "tenant-a", http.MethodGet, "/api/v1/orchestrator/attention", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var envelope struct {
		Items []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			IssueRef string `json:"issue_ref"`
			Subject  string `json:"subject"`
		} `json:"items"`
		Total  int            `json:"total"`
		Facets map[string]any `json:"facets"`
	}
	require.NoError(t, decodeJSON(t, rec, &envelope))
	require.Len(t, envelope.Items, 1)
	assert.Equal(t, "task-att-1", envelope.Items[0].ID)
	assert.Equal(t, "parked_task", envelope.Items[0].Kind)
	assert.Equal(t, "acme/p1#5", envelope.Items[0].IssueRef)
	assert.Equal(t, 1, envelope.Total)
	assert.NotNil(t, envelope.Facets["kind"])
}

func TestHandleListAttention_FiltersByProjectAndRefusesAnUnknownKind(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBlockedTask(t, "tenant-a", "p1", "task-att-2", "acme/p1#6", "waiting")
	seedBlockedTask(t, "tenant-a", "p2", "task-att-3", "acme/p2#7", "waiting")

	rec := attentionCall(t, server, "tenant-a", http.MethodGet, "/api/v1/orchestrator/attention?project_id=p1", nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var envelope struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	require.NoError(t, decodeJSON(t, rec, &envelope))
	require.Len(t, envelope.Items, 1)
	assert.Equal(t, "task-att-2", envelope.Items[0].ID)

	rec = attentionCall(t, server, "tenant-a", http.MethodGet, "/api/v1/orchestrator/attention?kind=not-a-kind", nil)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestHandleResolveAttention_RefusesBadInput(t *testing.T) {
	server, _ := setupSDLCServer(t)
	seedBlockedTask(t, "tenant-a", "p1", "task-att-4", "acme/p1#8", "waiting")

	// Unknown task: 404.
	rec := attentionCall(t, server, "tenant-a", http.MethodPost, "/api/v1/orchestrator/attention/does-not-exist/resolve",
		AttentionResolveRequest{Action: "readmit"})
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	// Unknown action: 400.
	rec = attentionCall(t, server, "tenant-a", http.MethodPost, "/api/v1/orchestrator/attention/task-att-4/resolve",
		AttentionResolveRequest{Action: "not-a-real-action"})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	// A task that is not blocked: 409.
	require.NoError(t, sdlcRESTTasksService.ReadmitTask(context.Background(), "tenant-a", "task-att-4"))
	rec = attentionCall(t, server, "tenant-a", http.MethodPost, "/api/v1/orchestrator/attention/task-att-4/resolve",
		AttentionResolveRequest{Action: "readmit"})
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

func TestHandleAttention_UnavailableModuleAnswers503(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.SetSDLCService(nil)

	rec := attentionCall(t, server, "tenant-a", http.MethodGet, "/api/v1/orchestrator/attention", nil)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	rec = attentionCall(t, server, "tenant-a", http.MethodPost, "/api/v1/orchestrator/attention/t1/resolve",
		AttentionResolveRequest{Action: "readmit"})
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// TestHandleResolveAttention_NoteKeepsTheTaskParked is G2 over REST: a note
// with text is accepted and the task is still blocked afterwards; a note with
// none is refused as bad input.
func TestHandleResolveAttention_NoteKeepsTheTaskParked(t *testing.T) {
	server, _ := setupSDLCServer(t)
	// A task adopted from no forge issue: the note posts nothing, so the
	// test needs no bound forge, and the verb's whole effect is what the
	// task store says afterwards.
	ctx := context.Background()
	require.NoError(t, sdlcRESTTaskStore.CreateTask(ctx, &orchestration.Task{
		ID: "task-att-note", TenantID: "tenant-a", ProjectID: "p1", Subject: "a parked task",
		Status: orchestration.TaskStatusInProgress,
	}))
	require.NoError(t, sdlcRESTTasksService.BlockTask(ctx, "tenant-a", "task-att-note", orchestration.TaskBlockKindNeedsInfo, "waiting on the owner"))

	rec := attentionCall(t, server, "tenant-a", http.MethodPost, "/api/v1/orchestrator/attention/task-att-note/resolve",
		AttentionResolveRequest{Action: "note", Answer: "  "})
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())

	rec = attentionCall(t, server, "tenant-a", http.MethodPost, "/api/v1/orchestrator/attention/task-att-note/resolve",
		AttentionResolveRequest{Action: "note", Answer: "Decided: keep the v1 wire format."})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	task, err := sdlcRESTTasksService.GetTask(ctx, "tenant-a", "task-att-note")
	require.NoError(t, err)
	assert.Equal(t, orchestration.TaskStatusBlocked, task.Status)
}
