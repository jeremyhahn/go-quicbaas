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
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// putProject drives one REST project update against srv.
func putProject(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/projects/proj-1?tenant_id=tenant-1", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "proj-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	srv.HandleUpdateProject(w, req)
	return w
}

// TestHandleUpdateProject_RetiresASigningKey: the flat retirement fields reach
// the service as one retirement, and the history comes back on the project.
func TestHandleUpdateProject_RetiresASigningKey(t *testing.T) {
	at := time.Date(2026, 9, 24, 22, 15, 0, 0, time.UTC)
	var got *service.UpdateProjectRequest
	srv := newProjectTestServer(&mocks.MockProjectService{
		UpdateProjectFunc: func(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			got = req
			project := &orchestration.Project{ID: "proj-1", TenantID: "tenant-1", Name: "api"}
			project.Git.Signing.Retired = []orchestration.RetiredSigningKey{{PublicKey: "ssh-ed25519 AAAA", Name: "old", RetiredAt: at}}
			return &service.UpdateProjectResponse{Project: project, Timestamp: time.Now()}, nil
		},
	})
	w := putProject(t, srv, `{"git_signing_retire_key_name":"old","git_signing_retire_key_scope":"service",`+
		`"git_signing_retire_key_owner":"agent","git_signing_retired_at":"2026-09-24T22:15:00Z"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	want := orchestration.KMSKeyRef{Name: "old", Scope: "service", Owner: "agent"}
	if got.GitSigningRetire == nil || got.GitSigningRetire.Key != want || !got.GitSigningRetire.RetiredAt.Equal(at) {
		t.Fatalf("retirement = %+v, want %+v at %s", got.GitSigningRetire, want, at)
	}
	var resp struct {
		Project *ProjectRESTResponse `json:"project"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Project.GitSigningRetired) != 1 || resp.Project.GitSigningRetired[0].Name != "old" {
		t.Fatalf("history = %+v", resp.Project.GitSigningRetired)
	}
}

// TestHandleUpdateProject_RefusesAnUnreadableRetirement: a time that does not
// parse is a 400 and the service is never asked.
func TestHandleUpdateProject_RefusesAnUnreadableRetirement(t *testing.T) {
	called := false
	srv := newProjectTestServer(&mocks.MockProjectService{
		UpdateProjectFunc: func(ctx context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			called = true
			return nil, nil
		},
	})
	w := putProject(t, srv, `{"git_signing_retire_key_name":"old","git_signing_retired_at":"yesterday"}`)
	if w.Code != http.StatusBadRequest || called {
		t.Fatalf("got %d (service called: %v), want 400 before the service", w.Code, called)
	}
}
