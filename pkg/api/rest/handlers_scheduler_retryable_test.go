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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// postCreateSchedulerJob issues a valid create-job request against the
// handler and returns the recorded response.
func postCreateSchedulerJob(t *testing.T, server *Server) *httptest.ResponseRecorder {
	t.Helper()

	reqBody := CreateSchedulerJobRESTRequest{
		Name:    "test-job",
		Type:    "recurring",
		Handler: "test-handler",
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/scheduler/jobs", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.HandleCreateSchedulerJob(w, req)
	return w
}

// decodeErrorResponse unmarshals the recorded body into an ErrorResponse.
func decodeErrorResponse(t *testing.T, w *httptest.ResponseRecorder) ErrorResponse {
	t.Helper()

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

// TestHandleCreateSchedulerJob_NotLeaderStorageError_503Retryable pins the
// mapping of a transient Raft leadership transition — surfaced from the qrdb
// SDK as *transport.NotLeaderError wrapped inside scheduler.ErrStorageError —
// to 503 Service Unavailable with retryable:true, so clients back off and
// retry instead of treating a leader election as a permanent failure.
func TestHandleCreateSchedulerJob_NotLeaderStorageError_503Retryable(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	storageErr := &scheduler.ErrStorageError{
		Op:    "CreateJob.put",
		Cause: &qrdbtransport.NotLeaderError{ShardID: 1},
	}
	mockSvc.schedulerService.On("CreateJob", mock.Anything, mock.Anything).
		Return(nil, storageErr).Once()

	w := postCreateSchedulerJob(t, server)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	resp := decodeErrorResponse(t, w)
	assert.True(t, resp.Retryable, "not-leader must be retryable")
	assert.Equal(t, "leader_not_available", resp.Code)
	assert.Contains(t, resp.Error, "not leader for shard 1")

	mockSvc.schedulerService.AssertExpectations(t)
}

// TestHandleCreateSchedulerJob_UnknownStorageError_500 pins the mapping of a
// non-transient storage failure to 500 Internal Server Error with
// retryable:false — the not-leader classification must not widen to
// arbitrary storage errors.
func TestHandleCreateSchedulerJob_UnknownStorageError_500(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	storageErr := &scheduler.ErrStorageError{
		Op:    "CreateJob.put",
		Cause: assert.AnError,
	}
	mockSvc.schedulerService.On("CreateJob", mock.Anything, mock.Anything).
		Return(nil, storageErr).Once()

	w := postCreateSchedulerJob(t, server)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	resp := decodeErrorResponse(t, w)
	assert.False(t, resp.Retryable, "unknown storage errors are not retryable")

	mockSvc.schedulerService.AssertExpectations(t)
}

// TestHandleCreateSchedulerJob_LeaderNotAvailableQBError_503Retryable pins
// the coded-error path: a QBError carrying ErrLeaderNotAvailable must pass
// through with its code string and per-code retryability intact rather than
// being flattened into a generic non-retryable wrapper.
func TestHandleCreateSchedulerJob_LeaderNotAvailableQBError_503Retryable(t *testing.T) {
	server, mockSvc, cleanup := setupSchedulerMockServer(t)
	defer cleanup()

	qbErr := errors.New(errors.ErrLeaderNotAvailable, "CreateJob", "no raft leader elected")
	mockSvc.schedulerService.On("CreateJob", mock.Anything, mock.Anything).
		Return(nil, qbErr).Once()

	w := postCreateSchedulerJob(t, server)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	resp := decodeErrorResponse(t, w)
	assert.True(t, resp.Retryable, "leader-not-available must be retryable")
	assert.Equal(t, "leader_not_available", resp.Code)

	mockSvc.schedulerService.AssertExpectations(t)
}
