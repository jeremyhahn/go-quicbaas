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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	grpccodes "google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// handleOrchestratorError maps each orchestration error class onto its HTTP
// status, exercising the predicate helpers (incl. the forbidden branch).
func TestHandleOrchestratorError_Dispatch(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"not_found", orchestration.ErrJobNotFound, http.StatusNotFound},
		{"session_not_found_typed", &orchestration.SessionNotFoundError{SessionID: "ses-x"}, http.StatusNotFound},
		// A "session not found" returned from the remote agent daemon arrives
		// as a gRPC codes.NotFound status. It must surface as 404, not the
		// generic 500 that masked the lifecycle bug after a just-successful spawn.
		{"session_not_found_grpc", grpcstatus.Error(grpccodes.NotFound, "acp: session not found: ses-x"), http.StatusNotFound},
		{"invalid_input", orchestration.ErrBackendRequired, http.StatusBadRequest},
		{"conflict", orchestration.ErrTaskAlreadyClaimed, http.StatusConflict},
		{"forbidden", orchestration.ErrNotLeadOfParent, http.StatusForbidden},
		{"internal", errors.New("some unexpected failure"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/x", nil)
			handleOrchestratorError(w, r, tt.err)
			assert.Equal(t, tt.want, w.Code)
		})
	}
}

func TestIsOrchForbiddenError(t *testing.T) {
	assert.True(t, isOrchForbiddenError(orchestration.ErrNotLeadOfParent))
	assert.False(t, isOrchForbiddenError(errors.New("other")))
	assert.False(t, isOrchForbiddenError(nil))
}

// RespondForbidden writes a 403 with the supplied detail.
func TestRespondForbidden(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	RespondForbidden(w, r, "no access")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "no access")
}
