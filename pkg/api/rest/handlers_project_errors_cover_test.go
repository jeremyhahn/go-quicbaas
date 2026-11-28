//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// handleProjectError maps each project error class to its HTTP status.
func TestHandleProjectError_Dispatch(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not_found", &service.ProjectNotFoundError{TenantID: "t", ProjectID: "p"}, http.StatusNotFound},
		{"already_exists", &service.ProjectAlreadyExistsError{TenantID: "t", Name: "n"}, http.StatusConflict},
		{"bound", &service.ProjectBoundError{TenantID: "t", ProjectID: "p"}, http.StatusConflict},
		// Another delete, or another bind or unbind, of the project is in
		// flight: a conflict the caller retries once it settles.
		{"delete_in_progress", &sdlc.DeleteInProgressError{TenantID: "t", ProjectID: "p"}, http.StatusConflict},
		{"bind_in_progress", &sdlc.BindInProgressError{TenantID: "t", ProjectID: "p"}, http.StatusConflict},
		// The project is deleted but its tombstone was not written: a server
		// fault, not a conflict.
		{"delete_unsettled", &service.ProjectDeleteUnsettledError{TenantID: "t", ProjectID: "p", Cause: stderrors.New("kv down")}, http.StatusInternalServerError},
		{"invalid", &service.ProjectInvalidError{Field: "name", Message: "bad"}, http.StatusBadRequest},
		{"access_denied", &service.ProjectAccessDeniedError{ProjectID: "p", FleetNodeID: "a"}, http.StatusForbidden},
		// A project the authorizer could not read is a refusal, not a server
		// fault: it could not read the rule, so it refuses (go-quicbaas#634).
		{"unresolved", &service.ProjectUnresolvedError{TenantID: "t", ProjectID: "p", FleetNodeID: "a"}, http.StatusForbidden},
		{"role_invalid", &service.ProjectRoleInvalidError{Role: "x"}, http.StatusBadRequest},
		{"internal", stderrors.New("boom"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			handleProjectError(w, r, tt.err)
			assert.Equal(t, tt.want, w.Code)
		})
	}
}
