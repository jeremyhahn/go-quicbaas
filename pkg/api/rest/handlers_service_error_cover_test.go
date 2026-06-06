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

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// handleServiceError maps each known error class to the correct HTTP status.
func TestHandleServiceError_QBErrorCodes(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name string
		code qberrors.ErrorCode
		want int
	}{
		{"not_found", qberrors.ErrNotFound, qberrors.ErrNotFound.HTTPStatus()},
		{"invalid_input", qberrors.ErrInvalidInput, qberrors.ErrInvalidInput.HTTPStatus()},
		{"validation", qberrors.ErrValidation, qberrors.ErrValidation.HTTPStatus()},
		{"forbidden", qberrors.ErrForbidden, qberrors.ErrForbidden.HTTPStatus()},
		{"not_allowed", qberrors.ErrNotAllowed, qberrors.ErrNotAllowed.HTTPStatus()},
		{"unauthorized", qberrors.ErrUnauthorized, qberrors.ErrUnauthorized.HTTPStatus()},
		{"conflict", qberrors.ErrConflict, qberrors.ErrConflict.HTTPStatus()},
		{"already_exists", qberrors.ErrAlreadyExists, qberrors.ErrAlreadyExists.HTTPStatus()},
		{"unavailable", qberrors.ErrUnavailable, qberrors.ErrUnavailable.HTTPStatus()},
		{"storage", qberrors.ErrStorage, qberrors.ErrStorage.HTTPStatus()},
		{"internal", qberrors.ErrInternal, qberrors.ErrInternal.HTTPStatus()},
		{"unknown", qberrors.ErrUnknown, qberrors.ErrUnknown.HTTPStatus()},
		{"timeout", qberrors.ErrTimeout, qberrors.ErrTimeout.HTTPStatus()},
		{"leader_not_available", qberrors.ErrLeaderNotAvailable, qberrors.ErrLeaderNotAvailable.HTTPStatus()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			server.handleServiceError(w, r, qberrors.New(tt.code, "op", "boom"))
			assert.Equal(t, tt.want, w.Code)
		})
	}
}

func TestHandleServiceError_SchedulerErrors(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{"job_not_found", &scheduler.JobNotFoundError{JobID: 1}, http.StatusNotFound},
		{"job_already_exists", &scheduler.JobAlreadyExistsError{JobID: 1, Name: "n"}, http.StatusConflict},
		{"invalid_job", &scheduler.InvalidJobError{Field: "type", Message: "bad"}, http.StatusBadRequest},
		{"invalid_schedule", &scheduler.InvalidScheduleError{Schedule: "x"}, http.StatusBadRequest},
		{"handler_not_found", &scheduler.HandlerNotFoundError{Handler: "h"}, http.StatusNotFound},
		{"job_timeout", &scheduler.JobTimeoutError{JobID: 1}, http.StatusGatewayTimeout},
		{"max_retries", &scheduler.MaxRetriesExceededError{JobID: 1, MaxRetries: 3}, http.StatusTooManyRequests},
		{"scheduler_stopped", &scheduler.SchedulerStoppedError{}, http.StatusServiceUnavailable},
		{"unauthorized", &scheduler.UnauthorizedError{TenantID: "t", JobID: 1, Op: "x"}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			server.handleServiceError(w, r, tt.err)
			assert.Equal(t, tt.want, w.Code)
		})
	}
}

func TestHandleServiceError_GenericFallback(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	server.handleServiceError(w, r, stderrors.New("totally unexpected"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
