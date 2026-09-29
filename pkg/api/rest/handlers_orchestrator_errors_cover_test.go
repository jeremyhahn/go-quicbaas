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
		// A "session not found" returned from the remote fleet node daemon arrives
		// as a gRPC codes.NotFound status. It must surface as 404, not the
		// generic 500 that masked the lifecycle bug after a just-successful spawn.
		{"session_not_found_grpc", grpcstatus.Error(grpccodes.NotFound, "acp: session not found: ses-x"), http.StatusNotFound},
		{"invalid_input", orchestration.ErrBackendRequired, http.StatusBadRequest},
		// Every refusal of the request's own shape unwraps to ErrInvalidInput:
		// an oversized prompt append is the caller's to correct.
		{"prompt_append_too_large", &orchestration.PromptAppendTooLargeError{Field: "prompt_append", Size: 70000, Limit: orchestration.MaxPromptAppendBytes}, http.StatusBadRequest},
		{"input_validation", &orchestration.InputValidationError{Field: "backend", Message: "name the agent or the backend, not both"}, http.StatusBadRequest},
		// An Agent over its own spend window is exhausted, as a pool is; an
		// Agent whose limits could not be checked is unavailable.
		{"agent_over_budget", &orchestration.AgentSpendExhaustedError{AgentID: "a", Window: "daily", Axis: "tokens", Used: 2, Limit: 1}, http.StatusServiceUnavailable},
		{"agent_spend_undecidable", &orchestration.AgentSpendUndecidableError{AgentID: "a"}, http.StatusServiceUnavailable},
		{"conflict", orchestration.ErrTaskAlreadyClaimed, http.StatusConflict},
		{"forbidden", orchestration.ErrNotLeadOfParent, http.StatusForbidden},
		// Every spawn-policy refusal is a 403 carrying its actionable message.
		// These used to fall through to 500, leaving an operator unable to tell
		// a mount-allowlist misconfiguration from a server fault.
		{
			"mount_not_allowed",
			&orchestration.MountNotAllowedError{
				Field:    orchestration.MountFieldWorkDir,
				TenantID: "tenant-a",
				Source:   "/etc",
				Reason:   "outside every allowed mount root",
			},
			http.StatusForbidden,
		},
		{
			"sandbox_mode_violation",
			&orchestration.SandboxModeViolationError{
				Requested: orchestration.SandboxModeNone,
				Policy:    orchestration.SandboxPolicyRequired,
				Reason:    "raw exec forbidden under this policy",
			},
			http.StatusForbidden,
		},
		{
			"trust_level_violation",
			&orchestration.TrustLevelViolationError{
				Requested: orchestration.TrustLevelPinned,
				Policy:    orchestration.TrustPolicyAttested,
				Reason:    "below the operator's integrity floor",
			},
			http.StatusForbidden,
		},
		{
			"sandbox_network_not_allowed",
			&orchestration.SandboxNetworkNotAllowedError{
				Requested: "host",
				Effective: orchestration.SandboxNetworkDefault,
			},
			http.StatusForbidden,
		},
		// A policy denial raised on the remote fleet node daemon arrives as a gRPC
		// PermissionDenied status; the daemon is the last line of defence and
		// its verdict must not be flattened into a 500 either.
		{
			"policy_denied_grpc",
			grpcstatus.Error(grpccodes.PermissionDenied,
				`mount not allowed (work_dir) for tenant "tenant-a": "/etc": outside every allowed mount root`),
			http.StatusForbidden,
		},
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

	// A genuine server fault arriving from a remote daemon must stay a 500;
	// only PermissionDenied is re-shaped as forbidden.
	assert.False(t, isGRPCPermissionDenied(nil))
	assert.False(t, isGRPCPermissionDenied(errors.New("plain")))
	assert.False(t, isGRPCPermissionDenied(grpcstatus.Error(grpccodes.Internal, "boom")))
	assert.True(t, isGRPCPermissionDenied(grpcstatus.Error(grpccodes.PermissionDenied, "denied")))
}

// TestHandleOrchestratorError_PolicyDenialKeepsMessage asserts the 403 body
// still names the offending field, tenant and path. The whole point of not
// answering 500 is that the operator can act on the response.
func TestHandleOrchestratorError_PolicyDenialKeepsMessage(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/orchestrator/sessions", nil)
	handleOrchestratorError(w, r, &orchestration.MountNotAllowedError{
		Field:    orchestration.MountFieldWorkDir,
		TenantID: "tenant-a",
		Source:   "/etc/shadow",
		Reason:   "mount source is outside the tenant's projects root and every allowed mount root",
	})

	assert.Equal(t, http.StatusForbidden, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, orchestration.MountFieldWorkDir)
	assert.Contains(t, body, "tenant-a")
	assert.Contains(t, body, "/etc/shadow")
}

// RespondForbidden writes a 403 with the supplied detail.
func TestRespondForbidden(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	RespondForbidden(w, r, "no access")
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "no access")
}
