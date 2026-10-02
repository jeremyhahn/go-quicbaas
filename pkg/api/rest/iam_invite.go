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
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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

// respondInviteLookupError renders a failed invite lookup and reports whether
// it handled one. It returns false only when err is nil, so callers read as
// `if h.respondInviteLookupError(w, r, err) { return }`.
//
// The canonical store reports a spent or expired token as an ERROR rather than
// returning a record to inspect (qrdb's Lookup runs checkUsable and refuses),
// so this — not the callers' record.Used branch — is where those land. They
// used to fall through to handleServiceError, which renders iam.ErrInviteUsed
// by its QBError code, Forbidden: the activation UI showed "access denied:
// invite has already been used", sending the invitee to look for a permissions
// problem they do not have.
//
// Anything else is a real failure (storage, sealed tenant) and keeps its own
// status; collapsing those into the opaque answer would report an outage as a
// bad link. That distinction is why this asks IsInviteUnusable rather than
// errors.Is against the three sentinels: errors.Is compares the QBError CODE,
// so every forbidden and every not-found reaching here -- a sealed tenant, a
// suspended one, a store that answered 404 -- was collapsed into "bad link",
// which is exactly the outage-as-bad-link report this comment forbids
// (go-quicbaas#434).
func (h *IAMHandler) respondInviteLookupError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return false
	case iam.IsInviteUnusable(err):
		respondInviteUnusable(w, r)
	default:
		h.server.handleServiceError(w, r, err)
	}
	return true
}

// GetInvite handles GET /api/v1/iam/{tenant_id}/invites/{token}.
// Public endpoint — returns invite metadata without revealing the token itself.
func (h *IAMHandler) GetInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// The tenant is a path segment because invitations live in that tenant's
	// own store: a token issued for one tenant is simply absent from another,
	// which makes isolation structural rather than a check that could be
	// forgotten. See the route mount in iam.go.
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id is required"))
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token is required"))
		return
	}

	inviteStore, err := h.getInviteStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	record, err := inviteStore.Lookup(ctx, tenantID, token)
	if h.respondInviteLookupError(w, r, err) {
		return
	}

	// A store that returns a spent or expired record instead of refusing gets
	// the same opaque answer as one that refuses, so the two shapes are
	// indistinguishable from outside.
	if record.Used || time.Now().After(record.ExpiresAt) {
		respondInviteUnusable(w, r)
		return
	}

	// Fetch user metadata so the activation UI can display email/name.
	store, storeErr := h.getIdentityStore(ctx)
	resp := &InviteMetadataResponse{
		UserID:    record.UserID,
		ExpiresAt: record.ExpiresAt,
	}
	if storeErr == nil && store != nil {
		if user, err := store.GetUser(ctx, record.TenantID, record.UserID); err == nil {
			resp.Email = user.Email
			resp.Name = user.Name
			resp.AuthMethods = user.AuthMethods
		}
	}
	if len(resp.AuthMethods) == 0 {
		resp.AuthMethods = []string{"webauthn"}
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ActivateFIDO2Begin handles POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin.
// Returns the WebAuthn CredentialCreationOptions so the browser can call
// navigator.credentials.create().
func (h *IAMHandler) ActivateFIDO2Begin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// The tenant is a path segment because invitations live in that tenant's
	// own store: a token issued for one tenant is simply absent from another,
	// which makes isolation structural rather than a check that could be
	// forgotten. See the route mount in iam.go.
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id is required"))
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token is required"))
		return
	}

	inviteStore, err := h.getInviteStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	record, err := inviteStore.Lookup(ctx, tenantID, token)
	if h.respondInviteLookupError(w, r, err) {
		return
	}

	// A store that returns a spent or expired record instead of refusing gets
	// the same opaque answer as one that refuses, so the two shapes are
	// indistinguishable from outside.
	if record.Used || time.Now().After(record.ExpiresAt) {
		respondInviteUnusable(w, r)
		return
	}

	// Delegate to the WebAuthn manager.
	webAuthnMgr, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not available"))
		return
	}

	mgr, err := webAuthnMgr.GetWebAuthnManager(ctx)
	if err != nil || mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not initialized"))
		return
	}

	// xkms's WebAuthn ceremony uses the user's email as the lookup key.
	// Resolve it from the IdentityStore via the invite's user id.
	email, lookupErr := h.lookupInviteEmail(ctx, record.TenantID, record.UserID)
	if lookupErr != nil || email == "" {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "invite user has no email"))
		return
	}

	// The invite's account already exists and is pending: its first passkey
	// is enrolled through the invitation ceremony, the one unauthenticated
	// path go-xkms allows for a pre-existing account (go-xkms#1294).
	options, err := mgr.BeginInviteRegistration(ctx, record.TenantID, email)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, options)
}

// ActivateFIDO2Finish handles POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish.
// Verifies the authenticator response, transitions the user to active, and consumes the invite.
func (h *IAMHandler) ActivateFIDO2Finish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// The tenant is a path segment because invitations live in that tenant's
	// own store: a token issued for one tenant is simply absent from another,
	// which makes isolation structural rather than a check that could be
	// forgotten. See the route mount in iam.go.
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id is required"))
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token is required"))
		return
	}

	inviteStore, err := h.getInviteStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	record, err := inviteStore.Lookup(ctx, tenantID, token)
	if h.respondInviteLookupError(w, r, err) {
		return
	}

	// A store that returns a spent or expired record instead of refusing gets
	// the same opaque answer as one that refuses, so the two shapes are
	// indistinguishable from outside.
	if record.Used || time.Now().After(record.ExpiresAt) {
		respondInviteUnusable(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req struct {
		Credential map[string]interface{} `json:"credential"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Credential == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "credential is required"))
		return
	}

	webAuthnMgr, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not available"))
		return
	}

	mgr, err := webAuthnMgr.GetWebAuthnManager(ctx)
	if err != nil || mgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not initialized"))
		return
	}

	// Match the ActivateFIDO2Begin lookup key — xkms's BeginRegistration
	// keys ceremony sessions by email, so FinishRegistration must use
	// the same identifier.
	email, lookupErr := h.lookupInviteEmail(ctx, record.TenantID, record.UserID)
	if lookupErr != nil || email == "" {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "invite user has no email"))
		return
	}

	// The proven attestation is not used here: invite activation does not mint
	// a session, it transitions the account to active and the operator logs in
	// afterwards. The gate has already refused the ceremony if this account's
	// policy required hardware.
	if _, err := mgr.FinishRegistration(ctx, record.TenantID, email, req.Credential); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Transition the user to active and clear the invite. This step is FATAL
	// on failure, and it runs strictly before the invite is consumed, so a
	// one-time token is never spent on an account that stayed `pending`.
	// PIVActivator.Activate already orders its writes this way.
	//
	// Failing here does NOT make the ceremony repeatable: xkms persisted the
	// credential inside FinishRegistration above and deleted the registration
	// session, and a later begin excludes credentials the user already holds,
	// so the same authenticator cannot re-register. What it preserves is the
	// invite, which keeps the account activatable at all — by a different
	// authenticator, or by an admin. Spending the token here instead would
	// leave nothing to recover with.
	store, storeErr := h.getIdentityStore(ctx)
	if storeErr != nil {
		h.server.handleServiceError(w, r, storeErr)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Activating is a lifecycle transition and nothing else. The invite itself
	// lives in the invite store and is consumed there; there is no longer a copy
	// on the user record to clear alongside it.
	activeState := iam.LifecycleActive
	var activationWarning string
	if _, err := store.UpdateUser(ctx, record.TenantID, record.UserID, iam.UpdateUserOptions{
		Lifecycle: &activeState,
	}); err != nil {
		h.logAuditEvent(ctx, record.TenantID, iam.AuditEventUserManagement,
			iam.AuditCategorySecurity, "invite.activate", iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "user", ID: record.UserID},
			map[string]interface{}{"error": err.Error()})

		// An error here does not prove the write did not land: a commit whose
		// response was lost reports failure on a user that IS now active.
		// Refusing unconditionally on that outcome is its own defect — it would
		// leave a live one-time token on an activated account, redeemable by
		// whoever else holds the link.
		//
		// So reconcile before deciding: re-read the user and let the persisted
		// state settle it. Active means the activation happened and the invite
		// must still be spent; anything else (including a failed re-read) keeps
		// the fail-closed refusal, which leaves the invite intact and the
		// account recoverable.
		if !h.userIsActive(ctx, store, record.TenantID, record.UserID) {
			h.server.handleServiceError(w, r, err)
			return
		}
		activationWarning = "activation write reported an error but the account is active; the invite was consumed"
	}

	// Consume the invite. Redeem failure on a one-time token is
	// security-relevant: if we cannot record the token as spent it remains
	// redeemable, so we must NOT report success. The credential is already
	// registered and the user is active, so the operator's authenticator works;
	// we surface a 500 so the failure is visible and the unconsumed invite is
	// not silently left live.
	if _, err := inviteStore.Redeem(ctx, tenantID, token); err != nil {
		h.logAuditEvent(ctx, record.TenantID, iam.AuditEventUserManagement,
			iam.AuditCategorySecurity, "invite.mark_used", iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "invite", ID: record.UserID},
			map[string]interface{}{"error": err.Error()})
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &ActivationResponse{
		UserID:  record.UserID,
		Message: "activation successful",
		Warning: activationWarning,
	})
}

// userIsActive reports whether the user is persisted in the active state. It
// answers the reconciliation question after an ambiguous activation write, so a
// read failure is deliberately NOT distinguished from "not active": both mean
// the activation is unconfirmed, and the caller must then keep the invite.
func (h *IAMHandler) userIsActive(ctx context.Context, store *iam.IdentityStore, tenantID, userID string) bool {
	user, err := store.GetUser(ctx, tenantID, userID)
	if err != nil || user == nil {
		return false
	}
	return user.Lifecycle == iam.LifecycleActive
}

// ActivatePIV handles POST /api/v1/iam/{tenant_id}/invites/{token}/activate/piv.
// Accepts a PEM-encoded X.509 certificate, verifies the chain, and activates the user.
func (h *IAMHandler) ActivatePIV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// The tenant is a path segment because invitations live in that tenant's
	// own store: a token issued for one tenant is simply absent from another,
	// which makes isolation structural rather than a check that could be
	// forgotten. See the route mount in iam.go.
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id is required"))
		return
	}

	token := chi.URLParam(r, "token")
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token is required"))
		return
	}

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
	if req.CertPEM == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "cert_pem is required"))
		return
	}

	// Get the PIV activator from the service (if configured).
	pivSvc, ok := h.server.service.(interface {
		GetPIVActivator(ctx context.Context) (*iam.PIVActivator, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusNotImplemented,
			NewIAMAPIError("piv_not_configured", "PIV activation is not configured on this server"))
		return
	}

	activator, err := pivSvc.GetPIVActivator(ctx)
	if err != nil || activator == nil {
		RespondIAMError(w, r, http.StatusNotImplemented,
			NewIAMAPIError("piv_not_configured", "PIV activation is not configured on this server"))
		return
	}

	if err := activator.Activate(ctx, tenantID, token, []byte(req.CertPEM)); err != nil {
		switch {
		// Absent, expired and spent collapse to one opaque answer here for
		// exactly the reason they do on the FIDO2 routes. PIV is if anything
		// the more exposed of the two: PIVActivator resolves the invite BEFORE
		// it parses or verifies the certificate, so an unauthenticated caller
		// posting arbitrary non-empty cert_pem could probe tokens and read the
		// distinct statuses as an existence oracle.
		//
		// Each case names ITS condition rather than the condition's error code.
		// errors.Is compared codes here (QBError is go-qrdb's QRDBError, whose
		// Is looks at Code alone), and ErrPIVCertRejected carries Forbidden --
		// the same code as ErrInviteExpired and ErrInviteUsed. The first case
		// therefore matched a rejected certificate and the 422 below it could
		// never run: every failed chain verification was reported to the caller
		// as an unusable invite (go-quicbaas#434).
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
		return
	}

	// Look up the user ID from the (now-consumed) invite for the response.
	// Activation already succeeded inside activator.Activate, so a lookup
	// failure here is cosmetic: log it and return success with an empty UserID
	// rather than failing a request whose side effect already committed.
	userID := ""
	if inviteStore, storeErr := h.getInviteStore(ctx); storeErr != nil {
		h.server.logger.WithError(storeErr).Warn("piv activation: invite store unavailable for response user id lookup")
	} else if record, lookupErr := inviteStore.Lookup(ctx, tenantID, token); lookupErr != nil {
		h.server.logger.WithError(lookupErr).Warn("piv activation: failed to resolve invite for response user id")
	} else if record != nil {
		userID = record.UserID
	}

	RespondJSON(w, r, http.StatusOK, &ActivationResponse{
		UserID:  userID,
		Message: "PIV activation successful",
	})
}

// ErrInviteStoreUnavailable indicates the canonical persistent InviteStore is
// not wired into the service. There is intentionally NO in-memory fallback: the
// activation invite first-boot prints lives in the persistent store, so a
// missing store is a wiring fault that must surface, never be papered over.
var ErrInviteStoreUnavailable = errors.New("iam: invite store not available")

// getInviteStore resolves the canonical persistent InviteStore from the service.
// It mirrors getIdentityStore: the store is owned by the IAM components and wired
// after handler construction (SetIAMProvider), so it is resolved lazily per
// request rather than captured on the handler. A nil store fails closed.
func (h *IAMHandler) getInviteStore(ctx context.Context) (iam.InviteStore, error) {
	svc, ok := h.server.service.(interface {
		GetInviteStore(ctx context.Context) (iam.InviteStore, error)
	})
	if !ok {
		return nil, ErrInviteStoreUnavailable
	}
	store, err := svc.GetInviteStore(ctx)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrInviteStoreUnavailable
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

// lookupInviteEmail resolves the email for the user referenced by an
// invite. xkms's WebAuthn ceremony uses the email as the persistent
// lookup key (qrdb.Username); the invite record stores the quicbaas
// IdentityStore user id, so we round-trip through GetUser.
func (h *IAMHandler) lookupInviteEmail(ctx context.Context, tenantID, userID string) (string, error) {
	store, err := h.getIdentityStore(ctx)
	if err != nil {
		return "", err
	}
	if store == nil {
		return "", nil
	}
	user, err := store.GetUser(ctx, tenantID, userID)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", nil
	}
	return user.Email, nil
}
