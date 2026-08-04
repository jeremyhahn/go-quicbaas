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
// qrdb user + user auth-factor wire surface
//
// These routes mirror the qrdb wire contract (go-qrdb pkg/api/rest
// handlers_users.go + handlers_user_auth_factor.go) so a qrdb SDK REST
// client works unchanged against quicbaasd:
//
//	GET    /users/by-username/{username}?tenantID=X -> user lookup
//	POST   /v1/users/factors                        -> Add
//	GET    /v1/users/factors/list?user_id=N         -> List
//	GET    /v1/users/factors/{factorID}             -> Get
//	DELETE /v1/users/factors/{factorID}?force=true  -> Remove
//	PUT    /v1/users/factors/{factorID}/label       -> Relabel
//	POST   /v1/users/factors/rotate                 -> Rotate
//	POST   /v1/users/factors/login                  -> Login resolution
//
// The qrdb SDK's Security().Auth().Factors() surface depends on this wire:
// resolveSubjectUserID resolves the subject through the user lookup and the
// factor verbs operate on the SAME UserAuthFactor store instance the
// unified-login ceremony resolves against. Handlers delegate to the lazy
// proxies wired by lifecycle via SetUserService / SetUserAuthFactorService.
// =============================================================================

// QRDBUserServiceNotImplementedError indicates the qrdb user lookup service
// has not been wired into the REST server.
type QRDBUserServiceNotImplementedError struct{}

// Error implements the error interface.
func (*QRDBUserServiceNotImplementedError) Error() string {
	return "qrdb user service not configured"
}

// UserAuthFactorNotImplementedError indicates the qrdb user auth-factor
// service has not been wired into the REST server.
type UserAuthFactorNotImplementedError struct{}

// Error implements the error interface.
func (*UserAuthFactorNotImplementedError) Error() string {
	return "qrdb user auth-factor service not configured"
}

// UserAuthFactorMissingParameterError indicates a required path or query
// parameter was empty or missing.
type UserAuthFactorMissingParameterError struct {
	Param string
}

// Error implements the error interface.
func (e *UserAuthFactorMissingParameterError) Error() string {
	return "missing required parameter: " + e.Param
}

// UserAuthFactorReadBodyError wraps a JSON decode failure on a user
// auth-factor request body.
type UserAuthFactorReadBodyError struct {
	Err error
}

// Error implements the error interface.
func (e *UserAuthFactorReadBodyError) Error() string {
	if e.Err == nil {
		return "invalid user auth-factor request body"
	}
	return "invalid user auth-factor request body: " + e.Err.Error()
}

// Unwrap exposes the underlying decode error for errors.Is/As traversal.
func (e *UserAuthFactorReadBodyError) Unwrap() error { return e.Err }

// HandleQRDBUserByUsername handles GET /users/by-username/{username} --
// resolves a user by tenant-scoped username. The tenant is read from the
// "tenantID" query parameter (empty selects the default tenant). This is the
// lookup the qrdb SDK factor surface uses to resolve a subject to its uint64
// user id before enrolling a factor.
func (s *Server) HandleQRDBUserByUsername(w http.ResponseWriter, r *http.Request) {
	svc := s.userService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &QRDBUserServiceNotImplementedError{})
		return
	}

	username := chi.URLParam(r, "username")
	if username == "" {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorMissingParameterError{Param: "username"})
		return
	}

	u, err := svc.GetUserByUsername(r.Context(), r.URL.Query().Get("tenantID"), username)
	if err != nil {
		RespondError(w, r, mapQRDBUserHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbUserToWire(u))
}

// HandleUserAuthFactorAdd handles POST /v1/users/factors -- enrolls a new
// login factor for the given user.
func (s *Server) HandleUserAuthFactorAdd(w http.ResponseWriter, r *http.Request) {
	svc := s.userAuthFactorService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &UserAuthFactorNotImplementedError{})
		return
	}

	var req qrdbtransport.AuthFactorAddRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorReadBodyError{Err: err})
		return
	}

	factor := userAuthFactorFromWire(&req)
	if err := svc.Add(r.Context(), req.UserID, factor); err != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.AuthFactorAddResponse{
		Factor: userAuthFactorToWire(factor),
	})
}

// HandleUserAuthFactorList handles GET /v1/users/factors/list?user_id=N --
// returns every factor (active and revoked) belonging to the user.
func (s *Server) HandleUserAuthFactorList(w http.ResponseWriter, r *http.Request) {
	svc := s.userAuthFactorService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &UserAuthFactorNotImplementedError{})
		return
	}

	uid, err := parseFactorUserID(r.URL.Query().Get("user_id"))
	if err != nil {
		RespondError(w, r, http.StatusBadRequest, err)
		return
	}

	factors, listErr := svc.List(r.Context(), uid)
	if listErr != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(listErr), listErr)
		return
	}

	resp := qrdbtransport.AuthFactorListResponse{
		Factors: make([]qrdbtransport.AuthFactorRecord, len(factors)),
		Total:   len(factors),
	}
	for i, f := range factors {
		resp.Factors[i] = userAuthFactorToWire(f)
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleUserAuthFactorGet handles GET /v1/users/factors/{factorID}.
func (s *Server) HandleUserAuthFactorGet(w http.ResponseWriter, r *http.Request) {
	svc := s.userAuthFactorService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &UserAuthFactorNotImplementedError{})
		return
	}

	factorID := chi.URLParam(r, "factorID")
	if factorID == "" {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorMissingParameterError{Param: "factor_id"})
		return
	}

	factor, err := svc.Get(r.Context(), factorID)
	if err != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.AuthFactorGetResponse{
		Factor: userAuthFactorToWire(factor),
	})
}

// HandleUserAuthFactorRemove handles DELETE /v1/users/factors/{factorID} --
// soft-deletes (revokes) the factor. Removing the user's last active factor
// requires force=true (admin override), matching the qrdb wire contract.
func (s *Server) HandleUserAuthFactorRemove(w http.ResponseWriter, r *http.Request) {
	svc := s.userAuthFactorService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &UserAuthFactorNotImplementedError{})
		return
	}

	factorID := chi.URLParam(r, "factorID")
	if factorID == "" {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorMissingParameterError{Param: "factor_id"})
		return
	}

	force := r.URL.Query().Get("force") == "true"
	if err := svc.Remove(r.Context(), factorID, force); err != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.AuthFactorRemoveResponse{Status: "revoked"})
}

// HandleUserAuthFactorRelabel handles PUT /v1/users/factors/{factorID}/label
// -- updates the factor's human-readable label and returns the updated
// record.
func (s *Server) HandleUserAuthFactorRelabel(w http.ResponseWriter, r *http.Request) {
	svc := s.userAuthFactorService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &UserAuthFactorNotImplementedError{})
		return
	}

	factorID := chi.URLParam(r, "factorID")
	if factorID == "" {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorMissingParameterError{Param: "factor_id"})
		return
	}

	var req qrdbtransport.AuthFactorRelabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorReadBodyError{Err: err})
		return
	}

	if err := svc.Relabel(r.Context(), factorID, req.Label); err != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(err), err)
		return
	}

	factor, err := svc.Get(r.Context(), factorID)
	if err != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.AuthFactorRelabelResponse{
		Factor: userAuthFactorToWire(factor),
	})
}

// HandleUserAuthFactorRotate handles POST /v1/users/factors/rotate --
// atomically adds the new factor and revokes the old factor.
func (s *Server) HandleUserAuthFactorRotate(w http.ResponseWriter, r *http.Request) {
	svc := s.userAuthFactorService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &UserAuthFactorNotImplementedError{})
		return
	}

	var req qrdbtransport.AuthFactorRotateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorReadBodyError{Err: err})
		return
	}

	newFactor := userAuthFactorFromWire(&req.NewFactor)
	if err := svc.Rotate(r.Context(), req.OldFactorID, newFactor); err != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.AuthFactorRotateResponse{
		NewFactor: userAuthFactorToWire(newFactor),
	})
}

// HandleUserAuthFactorLogin handles POST /v1/users/factors/login -- resolves
// a (type, lookup key) tuple to the owning factor. This is lookup RESOLUTION
// only, mirroring the qrdb wire contract: no bearer is minted and no
// possession is proven here. Credential-minting login flows on quicbaasd go
// exclusively through the unified login ceremony, which derives the lookup
// key from the VERIFIED mTLS peer certificate (peer cert wins).
func (s *Server) HandleUserAuthFactorLogin(w http.ResponseWriter, r *http.Request) {
	svc := s.userAuthFactorService()
	if svc == nil {
		RespondError(w, r, http.StatusNotImplemented, &UserAuthFactorNotImplementedError{})
		return
	}

	var req qrdbtransport.AuthFactorLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, r, http.StatusBadRequest, &UserAuthFactorReadBodyError{Err: err})
		return
	}

	factor, err := svc.LookupByCredentialKey(r.Context(), qrdbsdk.AuthFactorType(req.Type), req.LookupKey)
	if err != nil {
		RespondError(w, r, mapUserAuthFactorHTTPError(err), err)
		return
	}

	RespondJSON(w, r, http.StatusOK, qrdbtransport.AuthFactorLoginResponse{
		UserID: factor.UserID,
		Active: true,
		Factor: userAuthFactorToWire(factor),
	})
}

// parseFactorUserID parses the user_id query parameter into the uint64 wire
// type. Zero is rejected because it is the invalid/sentinel user id.
func parseFactorUserID(raw string) (uint64, error) {
	uid, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || uid == 0 {
		return 0, &UserAuthFactorMissingParameterError{Param: "user_id"}
	}
	return uid, nil
}

// qrdbUserToWire projects a qrdb user into the qrdb wire shape the SDK REST
// transport decodes (transport.User).
func qrdbUserToWire(u *qrdbsdk.User) *qrdbtransport.User {
	if u == nil {
		return nil
	}
	roleIDs := u.RoleIDs
	if roleIDs == nil {
		roleIDs = []string{}
	}
	return &qrdbtransport.User{
		ID:        u.ID,
		Username:  u.Username,
		TenantID:  u.TenantID,
		RoleIDs:   roleIDs,
		Enabled:   u.Enabled,
		System:    u.System,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
		UpdatedAt: u.UpdatedAt.Format(time.RFC3339),
	}
}

// userAuthFactorFromWire builds a qrdb AuthFactor from the wire request,
// mirroring go-qrdb's userAuthFactorFromRequest.
func userAuthFactorFromWire(req *qrdbtransport.AuthFactorAddRequest) *qrdbsdk.AuthFactor {
	return &qrdbsdk.AuthFactor{
		UserID:         req.UserID,
		Type:           qrdbsdk.AuthFactorType(req.Type),
		Label:          req.Label,
		Credential:     req.Credential,
		SPKICredential: req.SPKICredential,
		Metadata:       req.Metadata,
		CreatedBy:      req.CreatedBy,
	}
}

// userAuthFactorToWire projects a qrdb AuthFactor into the wire record,
// mirroring go-qrdb's userAuthFactorToRecord.
func userAuthFactorToWire(f *qrdbsdk.AuthFactor) qrdbtransport.AuthFactorRecord {
	if f == nil {
		return qrdbtransport.AuthFactorRecord{}
	}
	return qrdbtransport.AuthFactorRecord{
		ID:             f.ID,
		UserID:         f.UserID,
		Type:           string(f.Type),
		Label:          f.Label,
		Credential:     f.Credential,
		SPKICredential: f.SPKICredential,
		Metadata:       f.Metadata,
		RegisteredAt:   f.RegisteredAt,
		LastUsedAt:     f.LastUsedAt,
		RevokedAt:      f.RevokedAt,
		CreatedBy:      f.CreatedBy,
	}
}

// mapQRDBUserHTTPError maps qrdb user domain errors to HTTP status codes,
// mirroring go-qrdb's mapUserError. Unknown errors (including the sealed
// window's service-unavailable error) fall through to 500.
func mapQRDBUserHTTPError(err error) int {
	var notFound *qrdbsdk.UserNotFoundError
	var validation *qrdbsdk.UserValidationError

	switch {
	case stderrors.As(err, &notFound):
		return http.StatusNotFound
	case stderrors.As(err, &validation):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// mapUserAuthFactorHTTPError maps qrdb auth-factor sentinel errors to HTTP
// status codes, mirroring go-qrdb's mapUserAuthFactorError so the qrdb SDK
// REST transport observes identical statuses against quicbaasd.
func mapUserAuthFactorHTTPError(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case stderrors.Is(err, qrdbsdk.ErrFactorNotFound):
		return http.StatusNotFound
	case stderrors.Is(err, qrdbsdk.ErrFactorRevoked):
		return http.StatusGone
	case stderrors.Is(err, qrdbsdk.ErrLastFactor):
		return http.StatusConflict
	case stderrors.Is(err, qrdbsdk.ErrFactorAlreadyExists):
		return http.StatusConflict
	case stderrors.Is(err, qrdbsdk.ErrFactorTypeUnsupported),
		stderrors.Is(err, qrdbsdk.ErrInvalidFactor):
		return http.StatusBadRequest
	case stderrors.Is(err, qrdbsdk.ErrUserHasNoFactors):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
