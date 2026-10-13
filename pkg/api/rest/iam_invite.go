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
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// InviteMetadataResponse is returned by GET /api/v1/iam/{tenant_id}/invites/{token}.
type InviteMetadataResponse struct {
	UserID      string    `json:"user_id"`
	Email       string    `json:"email,omitempty"`
	Name        string    `json:"name,omitempty"`
	AuthMethods []string  `json:"auth_methods"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ActivationResponse is returned on successful FIDO2 or PIV activation.
type ActivationResponse struct {
	UserID  string `json:"user_id"`
	Message string `json:"message"`

	// Warning records a non-fatal irregularity behind an otherwise successful
	// activation — currently, an activation write that reported an error on a
	// user that reconciliation then found active. The ceremony succeeded and
	// the invite was consumed, so this is not a failure, but it is the kind of
	// thing an operator should see rather than have swallowed.
	Warning string `json:"warning,omitempty"`
}

// respondInviteUnusable renders the single opaque answer every unusable
// invite gets: absent, spent, or expired are indistinguishable to the caller.
//
// These routes are public (see the mount in iam.go — an invitee has no session
// yet), and qrdb states the rule for that audience: a handler serving
// unauthenticated callers MUST collapse not-found, expired and already-redeemed
// into one response, because telling a stranger a token exists but is spent
// confirms a real user was invited — the enumeration an opaque token exists to
// prevent. Distinguishing them would turn the activation URL into an oracle for
// anyone guessing tokens.
//
// The activation UI does not need the distinction: it already renders "this
// invite link may have expired or already been used" for any load failure.
func respondInviteUnusable(w http.ResponseWriter, r *http.Request) {
	RespondIAMError(w, r, http.StatusNotFound,
		NewIAMAPIError(ErrNotFound, "invite link is not valid"))
}

// inviteErrorResponse is the REST rendering of one invite activation
// service error.
type inviteErrorResponse struct {
	err     *dberrors.QBError
	status  int
	code    string
	message string
}

// inviteErrorResponses renders the activation service's own typed errors. They
// are matched by the identity of the QBError in the chain, not errors.Is:
// errors.Is compares QBError codes, so every internal error would match the
// first internal entry.
var inviteErrorResponses = []inviteErrorResponse{
	{service.ErrInviteActivationTenantRequired, http.StatusBadRequest, ErrBadRequest, "tenant_id is required"},
	{service.ErrInviteActivationTokenRequired, http.StatusBadRequest, ErrBadRequest, "token is required"},
	{service.ErrInviteActivationCredentialRequired, http.StatusBadRequest, ErrBadRequest, "credential is required"},
	{service.ErrInviteActivationCertRequired, http.StatusBadRequest, ErrBadRequest, "cert_pem is required"},
	{service.ErrInviteActivationWebAuthnNotConfigured, http.StatusInternalServerError, ErrInternalError, "WebAuthn manager not available"},
	{service.ErrInviteActivationUserNoEmail, http.StatusInternalServerError, ErrInternalError, "invite user has no email"},
	{service.ErrInviteActivationIdentityStoreUnavailable, http.StatusInternalServerError, ErrInternalError, "identity store not initialized"},
	{service.ErrInviteActivationPIVNotConfigured, http.StatusNotImplemented, "piv_not_configured", "PIV activation is not configured on this server"},
}

// respondInviteError renders a failed invite activation verb.
//
// The canonical store reports a spent or expired token as an ERROR rather than
// returning a record to inspect (qrdb's Lookup runs checkUsable and refuses),
// and the service reports a returned spent or expired record the same way, so
// all three land in the IsInviteUnusable case. Before that case existed they
// fell through to handleServiceError, which renders iam.ErrInviteUsed by its
// QBError code, Forbidden: the activation UI showed "access denied: invite has
// already been used", sending the invitee to look for a permissions problem
// they do not have.
//
// Anything else is a real failure (storage, sealed tenant) and keeps its own
// status; collapsing those into the opaque answer would report an outage as a
// bad link. That distinction is why this asks IsInviteUnusable rather than
// errors.Is against the three sentinels: errors.Is compares the QBError CODE,
// so every forbidden and every not-found reaching here -- a sealed tenant, a
// suspended one, a store that answered 404 -- was collapsed into "bad link",
// which is exactly the outage-as-bad-link report this comment forbids
// (go-quicbaas#434). The PIV cases name their condition for the same reason:
// ErrPIVCertRejected carries Forbidden, the code of ErrInviteExpired and
// ErrInviteUsed, so a code match reported every failed chain verification as
// an unusable invite.
func (h *IAMHandler) respondInviteError(w http.ResponseWriter, r *http.Request, err error) {
	var qbErr *dberrors.QBError
	isQBErr := stderrors.As(err, &qbErr)
	for _, resp := range inviteErrorResponses {
		if isQBErr && qbErr == resp.err {
			RespondIAMError(w, r, resp.status, NewIAMAPIError(resp.code, resp.message))
			return
		}
	}

	switch {
	// Absent, expired and spent collapse to one opaque answer. PIV is if
	// anything the more exposed route: PIVActivator resolves the invite
	// BEFORE it parses or verifies the certificate, so an unauthenticated
	// caller posting arbitrary non-empty cert_pem could otherwise probe
	// tokens and read distinct statuses as an existence oracle.
	case iam.IsInviteUnusable(err):
		respondInviteUnusable(w, r)
	case iam.IsUserNotPending(err):
		RespondIAMError(w, r, http.StatusConflict,
			NewIAMAPIError(ErrConflict, "user is not in pending state"))
	case iam.IsPIVCertRejected(err):
		RespondIAMError(w, r, http.StatusUnprocessableEntity,
			NewIAMAPIError("piv_cert_rejected", "PIV certificate chain verification failed"))
	default:
		h.server.handleServiceError(w, r, err)
	}
}

// inviteActivation returns the invite activation service over this server's
// IAM components. The verbs live in the service layer so every transport
// shares one implementation.
func (h *IAMHandler) inviteActivation() service.InviteActivationService {
	return service.NewInviteActivationService(inviteActivationSource{h: h})
}

// inviteActivationSource adapts the server's service to the activation
// service's component source. Each component is an optional capability of the
// server's service; one it does not offer resolves to nil, which the
// activation service reports as that component's typed not-configured error.
type inviteActivationSource struct {
	h *IAMHandler
}

// GetInviteStore implements service.InviteActivationSource.
func (s inviteActivationSource) GetInviteStore(ctx context.Context) (iam.InviteStore, error) {
	return s.h.getInviteStore(ctx)
}

// GetIdentityStore implements service.InviteActivationSource.
func (s inviteActivationSource) GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error) {
	return s.h.getIdentityStore(ctx)
}

// GetWebAuthnManager implements service.InviteActivationSource.
func (s inviteActivationSource) GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error) {
	svc, ok := s.h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		return nil, nil
	}
	return svc.GetWebAuthnManager(ctx)
}

// GetPIVActivator implements service.InviteActivationSource.
func (s inviteActivationSource) GetPIVActivator(ctx context.Context) (*iam.PIVActivator, error) {
	svc, ok := s.h.server.service.(interface {
		GetPIVActivator(ctx context.Context) (*iam.PIVActivator, error)
	})
	if !ok {
		return nil, nil
	}
	return svc.GetPIVActivator(ctx)
}

// GetAuditLogger implements service.InviteActivationSource.
func (s inviteActivationSource) GetAuditLogger(ctx context.Context) (iam.AuditLogger, error) {
	svc, ok := s.h.server.service.(interface {
		GetAuditLogger(ctx context.Context) (iam.AuditLogger, error)
	})
	if !ok {
		return nil, nil
	}
	return svc.GetAuditLogger(ctx)
}

// GetInvite handles GET /api/v1/iam/{tenant_id}/invites/{token}.
// Public endpoint — returns invite metadata without revealing the token itself.
func (h *IAMHandler) GetInvite(w http.ResponseWriter, r *http.Request) {
	// The tenant is a path segment because invitations live in that tenant's
	// own store: a token issued for one tenant is simply absent from another,
	// which makes isolation structural rather than a check that could be
	// forgotten. See the route mount in iam.go.
	meta, err := h.inviteActivation().GetInvite(r.Context(),
		chi.URLParam(r, "tenant_id"), chi.URLParam(r, "token"))
	if err != nil {
		h.respondInviteError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &InviteMetadataResponse{
		UserID:      meta.UserID,
		Email:       meta.Email,
		Name:        meta.Name,
		AuthMethods: meta.AuthMethods,
		ExpiresAt:   meta.ExpiresAt,
	})
}

// ActivateFIDO2Begin handles POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin.
// Returns the WebAuthn CredentialCreationOptions so the browser can call
// navigator.credentials.create().
func (h *IAMHandler) ActivateFIDO2Begin(w http.ResponseWriter, r *http.Request) {
	options, err := h.inviteActivation().ActivateFIDO2Begin(r.Context(),
		chi.URLParam(r, "tenant_id"), chi.URLParam(r, "token"))
	if err != nil {
		h.respondInviteError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, options)
}

// ActivateFIDO2Finish handles POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish.
// Verifies the authenticator response, transitions the user to active, and consumes the invite.
func (h *IAMHandler) ActivateFIDO2Finish(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// An undecodable body is passed on as a missing credential: the service
	// checks the invite first, so an unusable token gets the same opaque
	// answer whatever the body carried.
	var req struct {
		Credential map[string]interface{} `json:"credential"`
	}
	if jsonErr := json.Unmarshal(body, &req); jsonErr != nil {
		req.Credential = nil
	}

	activation, err := h.inviteActivation().ActivateFIDO2Finish(r.Context(),
		chi.URLParam(r, "tenant_id"), chi.URLParam(r, "token"), req.Credential)
	if err != nil {
		h.respondInviteError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &ActivationResponse{
		UserID:  activation.UserID,
		Message: "activation successful",
		Warning: activation.Warning,
	})
}

// ActivatePIV handles POST /api/v1/iam/{tenant_id}/invites/{token}/activate/piv.
// Accepts a PEM-encoded X.509 certificate, verifies the chain, and activates the user.
func (h *IAMHandler) ActivatePIV(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req struct {
		CertPEM string `json:"cert_pem"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	activation, err := h.inviteActivation().ActivatePIV(r.Context(),
		chi.URLParam(r, "tenant_id"), chi.URLParam(r, "token"), []byte(req.CertPEM))
	if err != nil {
		h.respondInviteError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &ActivationResponse{
		UserID:  activation.UserID,
		Message: "PIV activation successful",
	})
}

// errInviteStoreNotWired reports iam.ErrInviteStoreUnavailable when the
// canonical persistent InviteStore is not wired into the service. There is
// intentionally NO in-memory fallback: the activation invite first-boot prints
// lives in the persistent store, so a missing store is a wiring fault that must
// surface, never be papered over. It is coded internal, so REST answers 500 as
// it always has for a wiring fault, and errors.Is still finds the iam sentinel.
var errInviteStoreNotWired = dberrors.Wrap(iam.ErrInviteStoreUnavailable, dberrors.ErrInternal, "rest.getInviteStore")

// getInviteStore resolves the canonical persistent InviteStore from the service.
// It mirrors getIdentityStore: the store is owned by the IAM components and wired
// after handler construction (SetIAMProvider), so it is resolved lazily per
// request rather than captured on the handler. A nil store fails closed.
func (h *IAMHandler) getInviteStore(ctx context.Context) (iam.InviteStore, error) {
	svc, ok := h.server.service.(interface {
		GetInviteStore(ctx context.Context) (iam.InviteStore, error)
	})
	if !ok {
		return nil, errInviteStoreNotWired
	}
	store, err := svc.GetInviteStore(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, errInviteStoreNotWired
	}
	return store, nil
}

// getIdentityStore is a helper that retrieves the IdentityStore from the service.
func (h *IAMHandler) getIdentityStore(ctx context.Context) (*iam.IdentityStore, error) {
	svc, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		return nil, nil
	}
	return svc.GetIdentityStore(ctx)
}
