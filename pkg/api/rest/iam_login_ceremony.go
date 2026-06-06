//go:build rest && iam

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
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Unified Login Ceremony REST handlers.
//
// These handlers expose the unified login ceremony (BeginLogin / FinishLogin)
// over REST, delegating to the QuicBaaS LoginCeremony() facade. The facade
// owns the mTLS anti-spoofing model. The REST handler's security duty is to
// place the VERIFIED TLS peer certificate on the context so the facade can
// derive the mtls lookup key from it (never from the request body).
//
// This is the NEW unified path; the bespoke WebAuthn ceremony in
// iam_webauthn.go (IAMHandler.BeginLogin/FinishLogin) remains untouched and is
// retired in a later task.
// =============================================================================

// ceremonyBeginLoginRequest is the JSON body for POST /api/v1/auth/login/begin.
type ceremonyBeginLoginRequest struct {
	Type     string `json:"type"`
	Hint     string `json:"hint,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

// ceremonyBeginLoginResponse mirrors transport.BeginLoginResponse.
type ceremonyBeginLoginResponse struct {
	SessionID         string            `json:"session_id"`
	Nonce             []byte            `json:"nonce"`
	ChallengeMetadata map[string]string `json:"challenge_metadata,omitempty"`
	ExpiresAt         string            `json:"expires_at"`
}

// ceremonyFinishLoginRequest is the JSON body for POST /api/v1/auth/login/finish.
type ceremonyFinishLoginRequest struct {
	SessionID      string            `json:"session_id"`
	Type           string            `json:"type"`
	LookupKey      []byte            `json:"lookup_key,omitempty"`
	Signature      []byte            `json:"signature,omitempty"`
	MethodMetadata map[string]string `json:"method_metadata,omitempty"`
}

// ceremonyLoginIdentity mirrors transport.Identity.
type ceremonyLoginIdentity struct {
	Subject  string   `json:"subject"`
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles,omitempty"`
}

// ceremonyFinishLoginResponse mirrors transport.FinishLoginResponse.
type ceremonyFinishLoginResponse struct {
	Bearer    string                 `json:"bearer"`
	Identity  *ceremonyLoginIdentity `json:"identity,omitempty"`
	ExpiresAt string                 `json:"expires_at"`
	FactorID  string                 `json:"factor_id,omitempty"`
}

// verifiedPeerCertFromRequest extracts the leaf certificate from the request's
// VERIFIED TLS chain. Returns nil when there is no verified mTLS chain. Using
// VerifiedChains (not the raw PeerCertificates) guarantees the cert passed
// chain validation against the configured client CA.
func verifiedPeerCertFromRequest(r *http.Request) *x509.Certificate {
	if r == nil || r.TLS == nil {
		return nil
	}
	if len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0 {
		return r.TLS.VerifiedChains[0][0]
	}
	return nil
}

// loginCeremony returns the facade or nil when not wired.
func (h *IAMHandler) loginCeremony() service.LoginServicer {
	if h.server == nil || h.server.service == nil {
		return nil
	}
	return h.server.service.LoginCeremony()
}

// respondLoginCeremonyError maps facade errors onto HTTP status codes.
func respondLoginCeremonyError(w http.ResponseWriter, r *http.Request, err error) {
	var unavailable *service.LoginCeremonyUnavailableError
	if errors.As(err, &unavailable) {
		RespondIAMError(w, r, http.StatusServiceUnavailable,
			NewIAMAPIError(ErrInternalError, err.Error()))
		return
	}
	var peerRequired *service.MTLSPeerCertRequiredError
	if errors.As(err, &peerRequired) {
		RespondIAMError(w, r, http.StatusUnauthorized,
			NewIAMAPIError(ErrUnauthorized, err.Error()))
		return
	}
	var nilReq *service.LoginRequestNilError
	if errors.As(err, &nilReq) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, err.Error()))
		return
	}
	RespondIAMError(w, r, http.StatusUnauthorized,
		NewIAMAPIError(ErrUnauthorized, err.Error()))
}

// CeremonyBeginLogin handles POST /api/v1/auth/login/begin.
func (h *IAMHandler) CeremonyBeginLogin(w http.ResponseWriter, r *http.Request) {
	login := h.loginCeremony()
	if login == nil {
		RespondIAMError(w, r, http.StatusServiceUnavailable,
			NewIAMAPIError(ErrInternalError, "login ceremony not available"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req ceremonyBeginLoginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid request body"))
		return
	}

	resp, err := login.BeginLogin(r.Context(), &qrdbtransport.BeginLoginRequest{
		Type:     qrdbtransport.FactorType(req.Type),
		Hint:     req.Hint,
		TenantID: req.TenantID,
	})
	if err != nil {
		respondLoginCeremonyError(w, r, err)
		return
	}

	out := ceremonyBeginLoginResponse{
		SessionID:         resp.SessionID,
		Nonce:             resp.Nonce,
		ChallengeMetadata: resp.ChallengeMetadata,
		ExpiresAt:         resp.ExpiresAt.UTC().Format(time.RFC3339Nano),
	}
	writeCeremonyJSON(w, http.StatusOK, out)
}

// CeremonyFinishLogin handles POST /api/v1/auth/login/finish. The verified
// peer certificate is placed on the context so the facade derives the mtls
// lookup key from it.
func (h *IAMHandler) CeremonyFinishLogin(w http.ResponseWriter, r *http.Request) {
	login := h.loginCeremony()
	if login == nil {
		RespondIAMError(w, r, http.StatusServiceUnavailable,
			NewIAMAPIError(ErrInternalError, "login ceremony not available"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req ceremonyFinishLoginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid request body"))
		return
	}

	ctx := r.Context()
	if cert := verifiedPeerCertFromRequest(r); cert != nil {
		ctx = service.WithVerifiedPeerCert(ctx, cert)
	}

	// RFC 9449 DPoP: a proof presented on the login-finish request
	// sender-constrains the issued session bearer (cnf.jkt). The proof rides the
	// context to the SessionIssuer; an invalid proof fails the login closed.
	if proof := r.Header.Get("DPoP"); proof != "" {
		ctx = iam.ContextWithLoginDPoP(ctx, &iam.LoginDPoP{
			Proof:  proof,
			Method: r.Method,
			URI:    requestAbsoluteURI(r),
		})
	}

	resp, err := login.FinishLogin(ctx, &qrdbtransport.FinishLoginRequest{
		SessionID:      req.SessionID,
		Type:           qrdbtransport.FactorType(req.Type),
		LookupKey:      req.LookupKey,
		Signature:      req.Signature,
		MethodMetadata: req.MethodMetadata,
	})
	if err != nil {
		respondLoginCeremonyError(w, r, err)
		return
	}

	out := ceremonyFinishLoginResponse{
		Bearer:    resp.Bearer,
		ExpiresAt: resp.ExpiresAt.UTC().Format(time.RFC3339Nano),
		FactorID:  resp.FactorID,
	}
	if resp.Identity != nil {
		out.Identity = &ceremonyLoginIdentity{
			Subject:  resp.Identity.Subject,
			TenantID: resp.Identity.TenantID,
			Roles:    resp.Identity.Roles,
		}
	}

	// Dual-identity audit (AU-2/AU-3): record BOTH the service that presented
	// the login (verified client cert) and the user that authenticated.
	if resp.Identity != nil {
		id := iam.DualIdentity{
			Subject:         resp.Identity.Subject,
			TenantID:        resp.Identity.TenantID,
			Roles:           resp.Identity.Roles,
			ServiceIdentity: iam.ServiceIdentityFromCert(verifiedPeerCertFromRequest(r)),
		}
		h.auditAuthentication(ctx, id, "iam.login.finish", iam.AuditOutcomeSuccess, map[string]interface{}{
			"factor_id":   resp.FactorID,
			"factor_type": req.Type,
		})
	}

	writeCeremonyJSON(w, http.StatusOK, out)
}

// writeCeremonyJSON writes a JSON response with the given status code.
func writeCeremonyJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
