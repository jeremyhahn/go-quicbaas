//go:build rest && iam

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

// InviteMetadataResponse is returned by GET /api/v1/iam/invites/{token}.
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
}

// GetInvite handles GET /api/v1/iam/invites/{token}.
// Public endpoint — returns invite metadata without revealing the token itself.
func (h *IAMHandler) GetInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := chi.URLParam(r, "token")
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token is required"))
		return
	}

	record, err := h.inviteStore.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, iam.ErrInviteNotFound) {
			RespondIAMError(w, r, http.StatusNotFound,
				NewIAMAPIError(ErrNotFound, "invite not found"))
			return
		}
		h.server.handleServiceError(w, r, err)
		return
	}

	if record.Used {
		RespondIAMError(w, r, http.StatusGone,
			NewIAMAPIError("invite_used", "invite has already been used"))
		return
	}

	if time.Now().After(record.ExpiresAt) {
		RespondIAMError(w, r, http.StatusGone,
			NewIAMAPIError("invite_expired", "invite has expired"))
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

// ActivateFIDO2Begin handles POST /api/v1/iam/invites/{token}/activate/fido2/begin.
// Returns the WebAuthn CredentialCreationOptions so the browser can call
// navigator.credentials.create().
func (h *IAMHandler) ActivateFIDO2Begin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := chi.URLParam(r, "token")
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token is required"))
		return
	}

	record, err := h.inviteStore.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, iam.ErrInviteNotFound) {
			RespondIAMError(w, r, http.StatusNotFound,
				NewIAMAPIError(ErrNotFound, "invite not found"))
			return
		}
		h.server.handleServiceError(w, r, err)
		return
	}

	if record.Used {
		RespondIAMError(w, r, http.StatusGone,
			NewIAMAPIError("invite_used", "invite has already been used"))
		return
	}

	if time.Now().After(record.ExpiresAt) {
		RespondIAMError(w, r, http.StatusGone,
			NewIAMAPIError("invite_expired", "invite has expired"))
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

	options, err := mgr.BeginRegistration(ctx, record.TenantID, email)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, options)
}

// ActivateFIDO2Finish handles POST /api/v1/iam/invites/{token}/activate/fido2/finish.
// Verifies the authenticator response, transitions the user to active, and consumes the invite.
func (h *IAMHandler) ActivateFIDO2Finish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token := chi.URLParam(r, "token")
	if token == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "token is required"))
		return
	}

	record, err := h.inviteStore.GetByToken(ctx, token)
	if err != nil {
		if errors.Is(err, iam.ErrInviteNotFound) {
			RespondIAMError(w, r, http.StatusNotFound,
				NewIAMAPIError(ErrNotFound, "invite not found"))
			return
		}
		h.server.handleServiceError(w, r, err)
		return
	}

	if record.Used {
		RespondIAMError(w, r, http.StatusGone,
			NewIAMAPIError("invite_used", "invite has already been used"))
		return
	}

	if time.Now().After(record.ExpiresAt) {
		RespondIAMError(w, r, http.StatusGone,
			NewIAMAPIError("invite_expired", "invite has expired"))
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

	if err := mgr.FinishRegistration(ctx, record.TenantID, email, req.Credential); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Transition user to active and clear invite.
	store, storeErr := h.getIdentityStore(ctx)
	if storeErr == nil && store != nil {
		activeState := iam.UserStateActive
		emptyToken := ""
		var nilTime *time.Time
		_, _ = store.UpdateUser(ctx, record.TenantID, record.UserID, iam.UpdateUserOptions{
			State:           &activeState,
			InviteToken:     &emptyToken,
			InviteExpiresAt: &nilTime,
		})
	}

	// Consume the invite.
	_ = h.inviteStore.MarkUsed(ctx, token)

	RespondJSON(w, r, http.StatusOK, &ActivationResponse{
		UserID:  record.UserID,
		Message: "activation successful",
	})
}

// ActivatePIV handles POST /api/v1/iam/invites/{token}/activate/piv.
// Accepts a PEM-encoded X.509 certificate, verifies the chain, and activates the user.
func (h *IAMHandler) ActivatePIV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	if err := activator.Activate(ctx, token, []byte(req.CertPEM)); err != nil {
		switch {
		case errors.Is(err, iam.ErrInviteNotFound):
			RespondIAMError(w, r, http.StatusNotFound,
				NewIAMAPIError(ErrNotFound, "invite not found"))
		case errors.Is(err, iam.ErrInviteExpired):
			RespondIAMError(w, r, http.StatusGone,
				NewIAMAPIError("invite_expired", "invite has expired"))
		case errors.Is(err, iam.ErrInviteUsed):
			RespondIAMError(w, r, http.StatusGone,
				NewIAMAPIError("invite_used", "invite has already been used"))
		case errors.Is(err, iam.ErrUserNotPending):
			RespondIAMError(w, r, http.StatusConflict,
				NewIAMAPIError(ErrConflict, "user is not in pending state"))
		case errors.Is(err, iam.ErrPIVCertRejected):
			RespondIAMError(w, r, http.StatusUnprocessableEntity,
				NewIAMAPIError("piv_cert_rejected", "PIV certificate chain verification failed"))
		default:
			h.server.handleServiceError(w, r, err)
		}
		return
	}

	// Look up user ID from the invite for the response.
	record, _ := h.inviteStore.GetByToken(ctx, token)
	userID := ""
	if record != nil {
		userID = record.UserID
	}

	RespondJSON(w, r, http.StatusOK, &ActivationResponse{
		UserID:  userID,
		Message: "PIV activation successful",
	})
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
