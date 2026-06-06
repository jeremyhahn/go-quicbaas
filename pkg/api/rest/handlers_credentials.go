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
	"encoding/json"
	stderrors "errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// /v1/credentials/* — credential management surface
//
// These routes mirror the qrdb /v1/credentials/* wire contract so a CLI/SDK
// that talks to go-qrdb directly works unchanged against quicbaasd's REST
// listener. The handlers delegate to the credential management facade wired
// by lifecycle via SetCredentialService.
// =============================================================================

// CredentialNotImplementedError indicates the credential service has not
// been wired into the REST server (lifecycle SetCredentialService was not
// called or returned nil).
type CredentialNotImplementedError struct{}

// Error implements the error interface.
func (*CredentialNotImplementedError) Error() string {
	return "credential service not configured"
}

// CredentialMissingNameError indicates a required {name} path parameter
// was empty or missing.
type CredentialMissingNameError struct{}

// Error implements the error interface.
func (*CredentialMissingNameError) Error() string {
	return "credential name is required"
}

// CredentialInvalidVersionError indicates the {version} path parameter
// could not be parsed as an unsigned integer.
type CredentialInvalidVersionError struct {
	Value string
}

// Error implements the error interface.
func (e *CredentialInvalidVersionError) Error() string {
	return "credential version must be a non-negative integer: " + e.Value
}

// CredentialReadBodyError wraps a JSON decode failure on a credential
// request body.
type CredentialReadBodyError struct {
	Err error
}

// Error implements the error interface.
func (e *CredentialReadBodyError) Error() string {
	if e.Err == nil {
		return "invalid credential request body"
	}
	return "invalid credential request body: " + e.Err.Error()
}

// Unwrap exposes the underlying decode error for errors.Is/As traversal.
func (e *CredentialReadBodyError) Unwrap() error { return e.Err }

// credentialServiceOrFail writes a 501 response when the credential
// service is not configured and returns false to signal the caller should
// abort. Returns true when the service is available.
func (s *Server) credentialServiceOrFail(w http.ResponseWriter, r *http.Request) bool {
	if s.credentialSvc == nil {
		RespondError(w, r, http.StatusNotImplemented, &CredentialNotImplementedError{})
		return false
	}
	return true
}

// HandleCredentialSubmit handles POST /v1/credentials/submit -- submits a
// credential value for the named credential.
func (s *Server) HandleCredentialSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	var req qrdbtransport.CredentialSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &CredentialReadBodyError{Err: err})
		return
	}

	if err := s.credentialSvc.CredentialSubmit(r.Context(), req.Name, []byte(req.Value)); err != nil {
		RespondError(w, r, mapCredentialHTTPError(err), err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleCredentialStrategy handles GET /v1/credentials/strategy -- returns
// the active credential management strategy name.
func (s *Server) HandleCredentialStrategy(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.CredentialStrategyResponse{
		Strategy: s.credentialSvc.CredentialStrategy(),
	})
}

// HandleCredentialList handles GET /v1/credentials -- returns the sorted
// list of every stored credential name. Values are never returned.
func (s *Server) HandleCredentialList(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	names, err := s.credentialSvc.CredentialList(r.Context())
	if err != nil {
		RespondError(w, r, mapCredentialHTTPError(err), err)
		return
	}
	if names == nil {
		names = []string{}
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.CredentialListResponse{Names: names})
}

// HandleCredentialDelete handles DELETE /v1/credentials/{name} -- removes
// the credential with the given name along with any non-current historical
// versions.
func (s *Server) HandleCredentialDelete(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondError(w, r, http.StatusBadRequest, &CredentialMissingNameError{})
		return
	}

	if err := s.credentialSvc.CredentialDelete(r.Context(), name); err != nil {
		RespondError(w, r, mapCredentialHTTPError(err), err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleCredentialRotate handles POST /v1/credentials/{name}/rotate --
// atomically replaces the current credential value, optionally keeping the
// previous version retrievable for a grace period.
func (s *Server) HandleCredentialRotate(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondError(w, r, http.StatusBadRequest, &CredentialMissingNameError{})
		return
	}

	var req qrdbtransport.CredentialRotateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &CredentialReadBodyError{Err: err})
		return
	}
	// The path-parameter name is authoritative; override any conflicting
	// name supplied in the body so a single source of truth controls
	// which credential is rotated.
	req.Name = name

	opts := qrdbsdk.RotateOptions{
		NewValue:    []byte(req.NewValue),
		GracePeriod: time.Duration(req.GracePeriodSeconds) * time.Second,
	}

	result, err := s.credentialSvc.CredentialRotate(r.Context(), name, opts)
	if err != nil {
		RespondError(w, r, mapCredentialHTTPError(err), err)
		return
	}

	resp := qrdbtransport.CredentialRotateResponse{
		Name:            result.Name,
		PreviousVersion: result.PreviousVersion,
		NewVersion:      result.NewVersion,
	}
	if !result.GraceUntil.IsZero() {
		resp.GraceUntilUnix = result.GraceUntil.Unix()
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCredentialListVersions handles GET /v1/credentials/{name}/versions
// -- returns the version history (current + non-expired grace versions)
// for the named credential. Values are never returned.
func (s *Server) HandleCredentialListVersions(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondError(w, r, http.StatusBadRequest, &CredentialMissingNameError{})
		return
	}

	versions, err := s.credentialSvc.CredentialListVersions(r.Context(), name)
	if err != nil {
		RespondError(w, r, mapCredentialHTTPError(err), err)
		return
	}

	out := qrdbtransport.CredentialListVersionsResponse{
		Name:     name,
		Versions: make([]qrdbtransport.CredentialVersionInfo, 0, len(versions)),
	}
	for _, v := range versions {
		entry := qrdbtransport.CredentialVersionInfo{
			Version:   v.Version,
			IsCurrent: v.IsCurrent,
		}
		if !v.CreatedAt.IsZero() {
			entry.CreatedAtUnix = v.CreatedAt.Unix()
		}
		if !v.GraceUntil.IsZero() {
			entry.GraceUntilUnix = v.GraceUntil.Unix()
		}
		out.Versions = append(out.Versions, entry)
	}
	RespondJSON(w, r, http.StatusOK, out)
}

// HandleCredentialRetrieveVersion handles
// GET /v1/credentials/{name}/versions/{version} -- returns the credential
// value for the requested version. Version "0" means the current version.
func (s *Server) HandleCredentialRetrieveVersion(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondError(w, r, http.StatusBadRequest, &CredentialMissingNameError{})
		return
	}
	versionStr := chi.URLParam(r, "version")
	version, err := strconv.ParseUint(versionStr, 10, 64)
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, &CredentialInvalidVersionError{Value: versionStr})
		return
	}

	value, err := s.credentialSvc.CredentialRetrieveVersion(r.Context(), name, version)
	if err != nil {
		RespondError(w, r, mapCredentialHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.CredentialRetrieveVersionResponse{
		Value: string(value),
	})
}

// HandleCredentialGCExpired handles POST /v1/credentials/{name}/gc --
// reaps any historical versions whose grace period has elapsed.
func (s *Server) HandleCredentialGCExpired(w http.ResponseWriter, r *http.Request) {
	if !s.credentialServiceOrFail(w, r) {
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondError(w, r, http.StatusBadRequest, &CredentialMissingNameError{})
		return
	}

	reaped, err := s.credentialSvc.CredentialGCExpired(r.Context(), name)
	if err != nil {
		RespondError(w, r, mapCredentialHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.CredentialGCResponse{Reaped: reaped})
}

// mapCredentialHTTPError maps credential-package sentinel errors to HTTP
// status codes. Unknown errors fall through to 500 Internal Server Error
// to avoid leaking implementation details.
func mapCredentialHTTPError(err error) int {
	switch {
	case stderrors.Is(err, qrdbsdk.ErrCredentialNotFound):
		return http.StatusNotFound
	case stderrors.Is(err, qrdbsdk.ErrEnumerationNotSupported):
		return http.StatusNotImplemented
	case stderrors.Is(err, qrdbsdk.ErrVersionNotFound),
		stderrors.Is(err, qrdbsdk.ErrVersionExpired):
		return http.StatusGone
	case stderrors.Is(err, qrdbsdk.ErrInvalidGracePeriod),
		stderrors.Is(err, qrdbsdk.ErrEmptyCredentialName),
		stderrors.Is(err, qrdbsdk.ErrEmptyCredentialValue):
		return http.StatusBadRequest
	case stderrors.Is(err, qrdbsdk.ErrInvalidStrategy):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}
