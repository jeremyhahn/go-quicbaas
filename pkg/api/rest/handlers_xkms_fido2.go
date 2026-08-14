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
	"errors"
	"net/http"

	transportTypes "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// FIDO2 compat handlers (/api/v1/xkms/fido2/*)
// =============================================================================
//
// These expose the go-xkms FIDO2Service wire shape (BeginRegistration /
// FinishRegistration / BeginAuthentication / FinishAuthentication over the
// xkms transport types) on the compat bridge. They delegate to the service
// layer's tenant-scoped FIDO2 composition service, which wraps the per-tenant
// xkms relying party in go-xkms's canonical FIDO2 servicer adapter — the same
// code path the canonical /api/v1/iam/{tenant}/webauthn mount exercises.
//
// The whole group is AUTHZ-GATED (register -> user_authfactors:add,
// authenticate -> user_authfactors:get; see xkmsOps). Pre-auth passkey login
// rides the canonical public surfaces instead: the unified ceremony at
// /api/v1/auth/login/{begin,finish} and the xkms-native mount at
// /api/v1/iam/{tenant}/webauthn/login/{begin,finish}. Keeping the compat legs
// gated adds no anonymous surface while still giving authenticated automation
// the full xkms ceremony wire.

// XKMSFIDO2TenantRequiredError is returned when neither the tenant_id query
// parameter nor the authenticated principal resolves a tenant scope.
type XKMSFIDO2TenantRequiredError struct{}

func (e *XKMSFIDO2TenantRequiredError) Error() string {
	return "tenant is required: pass ?tenant_id= or authenticate with a tenant-scoped principal"
}

// xkmsFIDO2Tenant resolves the tenant scope for a compat FIDO2 ceremony: an
// explicit tenant_id query parameter wins, otherwise the authenticated
// principal's tenant applies.
func xkmsFIDO2Tenant(r *http.Request) (string, error) {
	if tid := r.URL.Query().Get("tenant_id"); tid != "" {
		return tid, nil
	}
	if ac := AuthorizedContextFromRequest(r.Context()); ac != nil && ac.TenantID != "" {
		return ac.TenantID, nil
	}
	return "", &XKMSFIDO2TenantRequiredError{}
}

// xkmsFIDO2Service returns the tenant-scoped FIDO2 composition service, or an
// error when the service graph is not wired.
func (s *Server) xkmsFIDO2Service() (service.FIDO2Service, error) {
	if s.service == nil || s.service.FIDO2() == nil {
		return nil, service.ErrFIDO2NotConfigured
	}
	return s.service.FIDO2(), nil
}

// respondXKMSFIDO2Error maps FIDO2 service failures onto the xkms error
// envelope: configuration gaps are 503, tenant/nil-request problems are 400,
// and ceremony failures are 500 for registration but a generic 401 for
// authentication (a failed proof must not describe why it failed).
func respondXKMSFIDO2Error(w http.ResponseWriter, op string, err error, authentication bool) {
	switch {
	case errors.Is(err, service.ErrFIDO2NotConfigured):
		respondXKMSError(w, http.StatusServiceUnavailable, err)
	case errors.Is(err, service.ErrFIDO2TenantRequired), errors.Is(err, service.ErrFIDO2NilRequest):
		respondXKMSError(w, http.StatusBadRequest, err)
	default:
		var tenantRequired *XKMSFIDO2TenantRequiredError
		if errors.As(err, &tenantRequired) {
			respondXKMSError(w, http.StatusBadRequest, err)
			return
		}
		if authentication {
			respondXKMSError(w, http.StatusUnauthorized, &XKMSOperationError{
				Op:     op,
				Detail: "authentication failed",
			})
			return
		}
		respondXKMSError(w, http.StatusInternalServerError, &XKMSOperationError{
			Op:     op,
			Detail: err.Error(),
		})
	}
}

// HandleXKMSFIDO2BeginRegistration handles POST /api/v1/xkms/fido2/register/begin.
func (s *Server) HandleXKMSFIDO2BeginRegistration(w http.ResponseWriter, r *http.Request) {
	fido2, err := s.xkmsFIDO2Service()
	if err != nil {
		respondXKMSFIDO2Error(w, "BeginRegistration", err, false)
		return
	}
	tenantID, err := xkmsFIDO2Tenant(r)
	if err != nil {
		respondXKMSFIDO2Error(w, "BeginRegistration", err, false)
		return
	}

	var req transportTypes.BeginRegistrationRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := fido2.BeginRegistration(ctx, tenantID, &req)
	if err != nil {
		respondXKMSFIDO2Error(w, "BeginRegistration", err, false)
		return
	}
	respondXKMSJSON(w, http.StatusOK, resp)
}

// HandleXKMSFIDO2FinishRegistration handles POST /api/v1/xkms/fido2/register/finish.
func (s *Server) HandleXKMSFIDO2FinishRegistration(w http.ResponseWriter, r *http.Request) {
	fido2, err := s.xkmsFIDO2Service()
	if err != nil {
		respondXKMSFIDO2Error(w, "FinishRegistration", err, false)
		return
	}
	tenantID, err := xkmsFIDO2Tenant(r)
	if err != nil {
		respondXKMSFIDO2Error(w, "FinishRegistration", err, false)
		return
	}

	var req transportTypes.FinishRegistrationRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := fido2.FinishRegistration(ctx, tenantID, &req)
	if err != nil {
		respondXKMSFIDO2Error(w, "FinishRegistration", err, false)
		return
	}
	respondXKMSJSON(w, http.StatusOK, resp)
}

// HandleXKMSFIDO2BeginAuthentication handles POST /api/v1/xkms/fido2/authenticate/begin.
func (s *Server) HandleXKMSFIDO2BeginAuthentication(w http.ResponseWriter, r *http.Request) {
	fido2, err := s.xkmsFIDO2Service()
	if err != nil {
		respondXKMSFIDO2Error(w, "BeginAuthentication", err, true)
		return
	}
	tenantID, err := xkmsFIDO2Tenant(r)
	if err != nil {
		respondXKMSFIDO2Error(w, "BeginAuthentication", err, true)
		return
	}

	var req transportTypes.BeginAuthenticationRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := fido2.BeginAuthentication(ctx, tenantID, &req)
	if err != nil {
		respondXKMSFIDO2Error(w, "BeginAuthentication", err, true)
		return
	}
	respondXKMSJSON(w, http.StatusOK, resp)
}

// HandleXKMSFIDO2FinishAuthentication handles POST /api/v1/xkms/fido2/authenticate/finish.
func (s *Server) HandleXKMSFIDO2FinishAuthentication(w http.ResponseWriter, r *http.Request) {
	fido2, err := s.xkmsFIDO2Service()
	if err != nil {
		respondXKMSFIDO2Error(w, "FinishAuthentication", err, true)
		return
	}
	tenantID, err := xkmsFIDO2Tenant(r)
	if err != nil {
		respondXKMSFIDO2Error(w, "FinishAuthentication", err, true)
		return
	}

	var req transportTypes.FinishAuthenticationRequest
	if err := readXKMSBody(r, &req); err != nil {
		respondXKMSError(w, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := xkmsContextWithTimeout(r.Context())
	defer cancel()

	resp, err := fido2.FinishAuthentication(ctx, tenantID, &req)
	if err != nil {
		respondXKMSFIDO2Error(w, "FinishAuthentication", err, true)
		return
	}
	respondXKMSJSON(w, http.StatusOK, resp)
}
