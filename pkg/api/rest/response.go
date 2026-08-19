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
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

var jsonBufPool = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// RespondJSON writes a JSON response with the given status code.
func RespondJSON(w http.ResponseWriter, r *http.Request, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if data == nil {
		return
	}

	// Encode into a pooled buffer to reduce allocations.
	poolObj := jsonBufPool.Get()
	buf, ok := poolObj.(*bytes.Buffer)
	if !ok {
		return
	}
	buf.Reset()
	defer jsonBufPool.Put(buf)

	enc := json.NewEncoder(buf)
	if err := enc.Encode(data); err == nil {
		// Set Content-Length to avoid chunked encoding overhead in tests
		w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
		_, _ = w.Write(buf.Bytes())
	} else {
		// Fallback to direct encoding on error (rare)
		_ = json.NewEncoder(w).Encode(data)
	}
}

// RespondError writes an error response with the given status code.
func RespondError(w http.ResponseWriter, r *http.Request, statusCode int, err error) {
	requestID := GetRequestID(r.Context())

	var errResp ErrorResponse
	errResp.Timestamp = time.Now()
	errResp.RequestID = requestID

	// Handle QuicBaaS errors
	var qbErr *errors.QBError
	if errors.As(err, &qbErr) {
		errResp.Error = qbErr.Error()
		errResp.Code = qbErr.Code.String()
		errResp.Retryable = qbErr.IsRetryable()

		// Add context as details
		if len(qbErr.Context) > 0 {
			errResp.Details = qbErr.Context
		}
	} else {
		// Generic error
		errResp.Error = err.Error()
		errResp.Retryable = false
	}

	RespondJSON(w, r, statusCode, errResp)
}

// RespondSuccess writes a generic success response.
func RespondSuccess(w http.ResponseWriter, r *http.Request, message string, data interface{}) {
	resp := SuccessResponse{
		Success:   true,
		Message:   message,
		Data:      data,
		Timestamp: time.Now(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// RespondCreated writes a 201 Created response.
func RespondCreated(w http.ResponseWriter, r *http.Request, data interface{}) {
	RespondJSON(w, r, http.StatusCreated, data)
}

// RespondNoContent writes a 204 No Content response.
func RespondNoContent(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// RespondBadRequest writes a 400 Bad Request error response.
func RespondBadRequest(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusBadRequest, &ValidationError{
		Message: message,
	})
}

// RespondNotFound writes a 404 Not Found error response.
func RespondNotFound(w http.ResponseWriter, r *http.Request, resource string) {
	RespondError(w, r, http.StatusNotFound, &NotFoundError{
		Resource: resource,
	})
}

// RespondConflict writes a 409 Conflict error response.
func RespondConflict(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusConflict, &ConflictError{
		Message: message,
	})
}

// RespondForbidden writes a 403 Forbidden error response.
func RespondForbidden(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusForbidden, &ForbiddenError{
		Reason: message,
	})
}

// RespondPayloadTooLarge writes a 413 Payload Too Large error response.
// Use this when a request body exceeds the configured maximum size.
func RespondPayloadTooLarge(w http.ResponseWriter, r *http.Request, maxBytes int64) {
	RespondError(w, r, http.StatusRequestEntityTooLarge, &PayloadTooLargeError{
		MaxBytes: maxBytes,
	})
}

// RespondInternalError writes a 500 Internal Server Error response.
// The actual error is logged server-side for debugging and correlation via
// the request ID. A generic message is returned to the client to prevent
// leaking internal implementation details.
func RespondInternalError(w http.ResponseWriter, r *http.Request, err error) {
	requestID := GetRequestID(r.Context())

	// Log the actual error server-side for debugging correlation.
	// The message is a constant and the caller-supplied data travels only as
	// slog attribute values, which every slog handler escapes when it writes
	// them. gosec's own G706 config scopes the check to the message argument
	// (CheckArgs: []int{0}) for exactly that reason, but it does not honour
	// that scoping and flags the tainted attribute values anyway.
	// #nosec G706 -- constant message; attribute values are escaped by the slog handler.
	slog.Error("internal server error",
		slog.String("request_id", requestID),
		slog.String("error", err.Error()),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
	)

	RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
		Message: "internal server error",
	})
}

// RespondServiceUnavailable writes a 503 Service Unavailable response.
func RespondServiceUnavailable(w http.ResponseWriter, r *http.Request, message string) {
	RespondError(w, r, http.StatusServiceUnavailable, &ServiceUnavailableError{
		Message: message,
	})
}

// ConflictError represents a 409 conflict error.
type ConflictError struct {
	Message string
}

func (e *ConflictError) Error() string {
	return e.Message
}
