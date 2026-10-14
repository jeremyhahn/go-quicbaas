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

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// WebAuthn credential administration (tenant-nested, user-scoped)
//
// Before these routes the only WebAuthn endpoints were ceremony endpoints:
// begin/finish for registration and login. An operator could watch a passkey
// be registered and could watch it fail, but could not see what was stored or
// remove it. When a credential outlived the user it was registered for, the
// email it was registered under became permanently unregistrable — the
// authenticator refuses the ceremony because the credential is listed in
// excludeCredentials — and the only recovery was to give the person a
// different email address.
//
// These are administrative verbs over another user's authenticators, so they
// are authorized exactly as the neighbouring user-admin verbs are (the frozen
// tenants:read / tenants:delete grants in routes_permissions_generated.go),
// not as the self-service ceremony routes.
// =============================================================================

// webAuthnCredentialsResponse is the list payload. The credentials are wrapped
// in an object rather than returned as a bare array so the response can grow
// (paging, a per-user summary) without breaking clients.
type webAuthnCredentialsResponse struct {
	// Credentials are the user's registered authenticators.
	Credentials []iam.WebAuthnCredentialSummary `json:"credentials"`

	// Total is len(Credentials), for caller convenience.
	Total int `json:"total"`
}

// webAuthnCredentialsDeletedResponse reports how many credentials a bulk
// delete removed, so an operator clearing a stuck account can see whether
// anything was actually there.
type webAuthnCredentialsDeletedResponse struct {
	// Deleted is the number of credentials removed.
	Deleted int `json:"deleted"`
}

// ListUserWebAuthnCredentials handles
// GET /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials —
// list the WebAuthn credentials registered to a user.
func (h *IAMHandler) ListUserWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID, userID, svc, ok := h.resolveWebAuthnCredentialSubject(w, r)
	if !ok {
		return
	}

	credentials, err := svc.ListCredentials(ctx, tenantID, userID)
	if err != nil {
		h.respondWebAuthnCredentialError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, webAuthnCredentialsResponse{
		Credentials: credentials,
		Total:       len(credentials),
	})
}

// DeleteUserWebAuthnCredential handles
// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/{credential_id} —
// remove one of a user's registered authenticators. The credential id is the
// base64url value the list verb returns.
func (h *IAMHandler) DeleteUserWebAuthnCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	credentialID := chi.URLParam(r, "credential_id")
	if credentialID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "credential ID is required"))
		return
	}

	tenantID, userID, svc, ok := h.resolveWebAuthnCredentialSubject(w, r)
	if !ok {
		return
	}

	if err := svc.DeleteCredential(ctx, tenantID, userID, credentialID); err != nil {
		h.respondWebAuthnCredentialError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// DeleteUserWebAuthnCredentials handles
// DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials —
// remove every authenticator registered to a user. This is the operator
// recovery verb for an account whose registration ceremony is being refused by
// the authenticator, and the same operation user deletion cascades through.
func (h *IAMHandler) DeleteUserWebAuthnCredentials(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID, userID, svc, ok := h.resolveWebAuthnCredentialSubject(w, r)
	if !ok {
		return
	}

	deleted, err := svc.DeleteCredentials(ctx, tenantID, userID)
	if err != nil {
		h.respondWebAuthnCredentialError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, webAuthnCredentialsDeletedResponse{Deleted: deleted})
}

// resolveWebAuthnCredentialSubject resolves the tenant, the subject user id and
// the shared credential administration service, writing the error response and
// reporting false when any of them is unavailable.
//
// The subject is named by IAM user id on the wire, matching every neighbouring
// user-nested route. Translating that id to the email the credential store is
// keyed by happens inside service.WebAuthnCredentialService, which is the same
// implementation the gRPC, embedded and MCP transports resolve through — the
// translation is the part that must not differ per transport.
func (h *IAMHandler) resolveWebAuthnCredentialSubject(
	w http.ResponseWriter,
	r *http.Request,
) (string, uint64, service.WebAuthnCredentialService, bool) {
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return "", 0, nil, false
	}

	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return "", 0, nil, false
	}

	if h.server == nil || h.server.service == nil {
		RespondIAMError(w, r, http.StatusServiceUnavailable,
			NewIAMAPIError(ErrInternalError, "WebAuthn is not available on this node"))
		return "", 0, nil, false
	}
	provider, ok := h.server.service.(interface {
		WebAuthnCredentials() service.WebAuthnCredentialService
	})
	if !ok {
		RespondIAMError(w, r, http.StatusServiceUnavailable,
			NewIAMAPIError(ErrInternalError, "WebAuthn is not available on this node"))
		return "", 0, nil, false
	}
	svc := provider.WebAuthnCredentials()
	if svc == nil {
		RespondIAMError(w, r, http.StatusServiceUnavailable,
			NewIAMAPIError(ErrInternalError, "WebAuthn is not available on this node"))
		return "", 0, nil, false
	}

	return tenantID, userID, svc, true
}

// respondWebAuthnCredentialError maps the credential surface's typed errors
// onto HTTP status codes. An unknown credential id is a 404; a malformed one
// and a truncated one are both 400 — a fragment of a real id is a malformed
// input, not a missing resource, and answering 404 for it is what sent
// go-quicbaas#574 looking for an encoding bug. Everything else is a genuine
// store failure.
func (h *IAMHandler) respondWebAuthnCredentialError(w http.ResponseWriter, r *http.Request, err error) {
	var truncated *iam.WebAuthnCredentialTruncatedIDError
	if errors.As(err, &truncated) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, truncated.Error()))
		return
	}
	var notFound *iam.WebAuthnCredentialNotFoundError
	if errors.As(err, &notFound) {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, notFound.Error()))
		return
	}
	var badID *iam.WebAuthnCredentialIDError
	if errors.As(err, &badID) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, badID.Error()))
		return
	}
	h.server.handleServiceError(w, r, err)
}
