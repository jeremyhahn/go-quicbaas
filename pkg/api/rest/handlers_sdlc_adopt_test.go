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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// adoptCall drives one POST .../adopt against server.
func adoptCall(t *testing.T, server *Server, tenant, project, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sdlc/projects/"+project+"/adopt", bytes.NewBufferString(body))
	if tenant != "" {
		req = req.WithContext(tenantctx.WithTenant(req.Context(), tenant))
	}
	r := chi.NewRouter()
	r.Post("/api/v1/sdlc/projects/{projectID}/adopt", server.HandleAdoptSDLCIssue)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestHandleAdoptSDLCIssue_RefusalsAreStatuses: a body that does not parse,
// a request naming no issue, a project with no binding and a tenant the
// caller is not in each answer their own status, and no task is created.
func TestHandleAdoptSDLCIssue_RefusalsAreStatuses(t *testing.T) {
	server, _ := setupSDLCServer(t)
	for name, tc := range map[string]struct {
		tenant, body string
		want         int
	}{
		"bad json":         {"tenant-a", "{", http.StatusBadRequest},
		"no issue":         {"tenant-a", `{"pull_request":3}`, http.StatusBadRequest},
		"unbound project":  {"tenant-a", `{"issue":141}`, http.StatusNotFound},
		"no tenant at all": {"", `{"issue":141}`, http.StatusForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			rec := adoptCall(t, server, tc.tenant, "p1", tc.body)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
	tasks, err := sdlcRESTTaskStore.ListTasks(context.Background(), "tenant-a", &orchestration.TaskFilter{})
	if err == nil {
		assert.Empty(t, tasks, "a refused adoption created a task")
	}
}

// TestRespondSDLCError_AdoptionOutcomes: a refused adoption and a source a
// live task holds are conflicts; a forge write that failed after the task
// was created is the forge's failure.
func TestRespondSDLCError_AdoptionOutcomes(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"refused":    {&sdlc.AdoptionRefusedError{Subject: "acme/p1#1", Reason: "closed"}, http.StatusConflict},
		"held":       {&orchestration.TaskSourceAlreadyAdoptedError{TaskID: "live"}, http.StatusConflict},
		"incomplete": {&sdlc.AdoptionIncompleteError{TaskID: "t1", Step: "posting the ruling", Cause: errors.New("503")}, http.StatusBadGateway},
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			respondSDLCError(rec, httptest.NewRequest(http.MethodPost, "/", nil), tc.err)
			assert.Equal(t, tc.want, rec.Code)
		})
	}
}

// TestRespondSDLCError_OversizedSlotAppendIsBadInput: a binding whose slot
// appends more than the bound is the caller's to correct.
func TestRespondSDLCError_OversizedSlotAppendIsBadInput(t *testing.T) {
	rec := httptest.NewRecorder()
	respondSDLCError(rec, httptest.NewRequest(http.MethodPut, "/", nil),
		&orchestration.PromptAppendTooLargeError{Field: "worker_prompt_append", Size: 70000, Limit: orchestration.MaxPromptAppendBytes})
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
