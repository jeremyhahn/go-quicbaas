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
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// ListUsers handles GET /api/v1/tenants/{tenant_id}/users - List users in a tenant.
func (h *IAMHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Parse query parameters
	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	offset := 0
	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	// Get identity store from service
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Validate tenant exists
	_, err = store.GetTenant(ctx, tenantID)
	if err != nil {
		RespondIAMError(w, r, http.StatusNotFound,
			NewIAMAPIError(ErrNotFound, "tenant not found"))
		return
	}

	// List users
	opts := iam.ListUsersOptions{
		Limit:  limit,
		Offset: offset,
	}

	users, err := store.ListUsers(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := map[string]interface{}{
		"users":  users,
		"limit":  limit,
		"offset": offset,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// CreateUser handles POST /api/v1/tenants/{tenant_id}/users - Create a new user.
func (h *IAMHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse request
	// There is deliberately no "id": the identity is the Snowflake the store
	// mints, so an id supplied here could only name a record in some other
	// system. A caller that sent one used to get it back and then find nothing
	// under it, because the store minted its own anyway.
	var req struct {
		Email             string            `json:"email"`
		Username          string            `json:"username,omitempty"`
		Name              string            `json:"name,omitempty"`
		Attributes        map[string]string `json:"attributes,omitempty"`
		Enabled           *bool             `json:"enabled,omitempty"`
		AuthMethods       []string          `json:"auth_methods,omitempty"`
		Roles             []string          `json:"roles,omitempty"`
		Groups            []string          `json:"groups,omitempty"`
		Lifecycle         iam.Lifecycle     `json:"lifecycle,omitempty"`
		InviteExpiryHours int               `json:"invite_expiry_hours,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Validate required fields
	if req.Email == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "email is required"))
		return
	}

	// The lifecycle a create may ask for is the same on EVERY transport — the
	// requirement that IAM verbs behave alike whichever protocol carries them.
	// Refusing here what gRPC accepts would make one SDK call mean two different
	// things depending on the wire underneath it.
	//
	// What is specific to this endpoint is the INVITE, and that follows from the
	// state rather than replacing it: an invite is the thing a PENDING account
	// activates through, so it is minted exactly when the account is pending. A
	// caller asking for `active` or `disabled` is asking for an account with
	// nothing to activate, and gets no token — which is the honest answer, not a
	// missing feature.
	// Resolve the EFFECTIVE state here, applying the same precedence the store
	// does, because this handler has to decide whether to mint an invite and
	// that decision must match what gets persisted.
	//
	// Defaulting straight to `pending` was wrong for exactly that reason: the
	// store treats Enabled=false as `disabled` only when no lifecycle was
	// given, so pre-filling one meant `{"enabled": false}` created a PENDING
	// account and handed back a live activation link. The caller believed the
	// account was disabled while the response carried the way in.
	lifecycle := req.Lifecycle
	if lifecycle == "" {
		lifecycle = iam.LifecyclePending
		if req.Enabled != nil && !*req.Enabled {
			lifecycle = iam.LifecycleDisabled
		}
	}
	invited := lifecycle == iam.LifecyclePending

	// Default and cap invite expiry hours.
	if req.InviteExpiryHours <= 0 {
		req.InviteExpiryHours = 24
	}
	if req.InviteExpiryHours > 168 {
		req.InviteExpiryHours = 168
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// User quota (max users per tenant) is enforced by qrdb's TenantServicer.
	// qrdb will return a quota-exceeded error if the limit is reached; the
	// service layer propagates that as a 403 Forbidden through the normal error
	// path below. No BaaS-local count is maintained here.

	// Create user
	inviteExpiry := time.Duration(req.InviteExpiryHours) * time.Hour
	opts := iam.CreateUserOptions{
		Email:      req.Email,
		Username:   req.Username,
		Name:       req.Name,
		Attributes: req.Attributes,
		// A pointer so an omitted "enabled" means "unspecified", not false.
		// As a plain bool it meant every create without the field explicitly
		// set would have suspended the user the moment CreateUser started
		// honouring it.
		Enabled: req.Enabled,
		// Roles and Groups are validated against the tenant's role and
		// group stores by IdentityStore.CreateUser BEFORE the user record
		// is written (validate-before-create), matching the gRPC/Unix
		// adapter contract.
		Roles:       req.Roles,
		Groups:      req.Groups,
		AuthMethods: req.AuthMethods,
		Lifecycle:   lifecycle,
	}

	user, err := store.CreateUser(ctx, tenantID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// An account that is not pending has nothing to activate, so it gets no
	// invite and the response carries no token.
	if !invited {
		h.logAuditEvent(ctx, tenantID,
			iam.AuditEventUserManagement,
			iam.AuditCategoryAdministration,
			"create",
			iam.AuditOutcomeSuccess,
			&iam.AuditResource{Type: "user", ID: user.ID, Name: user.Email},
			map[string]interface{}{"email": user.Email, "lifecycle": string(lifecycle)},
		)
		RespondJSON(w, r, http.StatusCreated, user)
		return
	}

	// Issue the one-time invite through the canonical store — the SAME store
	// the activation endpoints and first-boot use.
	//
	// The token is MINTED BY go-qrdb, not here: generation, digesting and
	// single-use enforcement all belong to the layer that owns the invitation
	// record. It is returned exactly once, on this call, and is not
	// recoverable afterwards; only its digest is stored.
	//
	// A failure to issue the invite UNDOES the user. This is the one surface
	// that can complete enrollment, so a user left here without an invite is
	// the exact state every other create surface now refuses to produce: an
	// account in `pending` that nothing can activate, which a retry cannot
	// repair because the identity already exists.
	//
	// The rollback is best-effort and its error is deliberately dropped: the
	// caller is already being told the create failed, and a cleanup failure
	// must not replace the reason it failed.
	inviteStore, err := h.getInviteStore(ctx)
	if err != nil {
		h.rollbackCreatedUser(ctx, tenantID, user.ID)
		h.server.handleServiceError(w, r, err)
		return
	}
	inviteToken, inviteRecord, err := inviteStore.Create(ctx, tenantID, user.ID, inviteExpiry)
	if err != nil {
		// The canonical store returns an empty token with an error, so this
		// is a no-op for it. It is here for the store that does not: a failed
		// mint that still handed back a token would otherwise leave that token
		// live, and revokeMintedInvite already ignores an empty one. Cheap
		// insurance against an implementation detail this layer cannot see.
		h.revokeMintedInvite(ctx, inviteStore, tenantID, inviteToken)
		h.rollbackCreatedUser(ctx, tenantID, user.ID)
		h.server.handleServiceError(w, r, err)
		return
	}
	expiresAt := inviteRecord.ExpiresAt

	// Put the account in `pending` — the state PIVActivator requires before it
	// will activate. The invite itself is NOT copied onto the user record: it
	// lives in the invite store, which mints it, digests it and enforces
	// single use. A second copy on the identity record was a bearer credential
	// that read authority could reach, and it could disagree with the store
	// that actually decides whether the invite is still good.
	// The lifecycle is NOT re-asserted here: the account was created pending
	// above, and re-sending the state it already holds would be a second write
	// saying nothing. Only the auth methods need attaching.
	//
	// And they are attached from the CREATED RECORD, never from the request.
	// Two writes in a row where the second re-states a field of the first is
	// the shape that quietly widens an authentication policy, and it does so in
	// two ways this handler used to be exposed to:
	//
	//   - The store defaults an omitted auth_methods to webauthn and normalizes
	//     what it is given. Re-sending the request's own copy made this handler
	//     a SECOND source of truth for that default: the two agreed only for as
	//     long as nobody changed one of them, and the second write is the one
	//     that lands.
	//   - A non-nil pointer to an EMPTY slice does not mean "unchanged" to
	//     UpdateUser, it means "no restriction". A request that omitted the
	//     field therefore had to be defaulted before this point or the follow-up
	//     write would clear what the create had just set — a pending account
	//     that accepts any credential kind, reported to the caller as the
	//     webauthn-only account they asked for.
	//
	// Sourcing the value from the create's own result closes the first, and
	// only sending the field when the record actually carries methods closes
	// the second: this write can now re-state a restriction, never remove one,
	// whatever the store's default happens to be.
	updateOpts := iam.UpdateUserOptions{}
	if len(user.AuthMethods) > 0 {
		methods := append([]string(nil), user.AuthMethods...)
		updateOpts.AuthMethods = &methods
	}

	// This step is FATAL too, and undoes the user for the same reason the two
	// above do. It used to log and continue, on the reasoning that the user
	// existed and only the invite state was missing — but that state is what
	// puts the account in `pending`, and PIVActivator refuses to activate a
	// user that is not pending (ErrUserNotPending). Continuing therefore
	// returned "created, here is your invite" for an account whose PIV
	// activation would later be refused, with nothing in the response saying
	// so.
	//
	// Failing and rolling back makes the whole create atomic: either a user
	// with its invite attached and delivered, or no user at all. The already
	// minted token is spent as part of that, so it cannot outlive the user it
	// was issued for.
	updatedUser, updateErr := store.UpdateUser(ctx, tenantID, user.ID, updateOpts)
	if updateErr != nil {
		h.logAuditEvent(ctx, tenantID,
			iam.AuditEventUserManagement,
			iam.AuditCategoryAdministration,
			"create",
			iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "user", ID: user.ID, Name: user.Email},
			map[string]interface{}{"email": user.Email, "error": updateErr.Error()},
		)
		h.revokeMintedInvite(ctx, inviteStore, tenantID, inviteToken)
		h.rollbackCreatedUser(ctx, tenantID, user.ID)
		h.server.handleServiceError(w, r, updateErr)
		return
	}
	if updatedUser != nil {
		user = updatedUser
	}

	// Derive the invite URL. THIS IS THE LINK AN OPERATOR ACTUALLY CLICKS, so
	// every part of it has to be the working part — a link that looks right and
	// lands on an error is worse than no link, because it sends whoever follows
	// it looking for a problem with their invite rather than with the URL.
	//
	// Three things were wrong here and each broke it on its own:
	//
	//   - The path was "/#/invite/...", but the UI is mounted at "/ui/". The
	//     root answers 401 on a node whose authenticator runs before routing, so
	//     the link produced an auth error rather than the activation view.
	//   - The tenant was absent. The invite API is tenant-scoped by design —
	//     a token issued for one tenant is absent from another's store, which
	//     makes isolation structural — so a browser holding only a token cannot
	//     reach it. The link is the only thing that can carry the tenant.
	//   - The host came from r.Host, which is whatever the ADMIN's client
	//     connected to. An admin calling 127.0.0.1 minted a link naming
	//     127.0.0.1, and WebAuthn refuses a ceremony whose origin does not match
	//     the configured RP ID. The configured WebAuthn origin is the authority
	//     for what the browser must use, so it wins when it is set.
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	// HOST COMES FROM THE REQUEST, so an admin must create invites via the host
	// the invitee will use. WebAuthn refuses a ceremony whose origin does not
	// match the configured RP ID, so an invite minted through 127.0.0.1 yields a
	// link that loads and then fails at the key. The REST layer cannot see the
	// IAM WebAuthn config to override this; until it can, the constraint is
	// stated here rather than left to be discovered at the authenticator.
	inviteURL := scheme + "://" + host + "/ui/#/invite/" +
		url.PathEscape(tenantID) + "/" + url.PathEscape(inviteToken)

	// Log audit event
	h.logAuditEvent(ctx, tenantID,
		iam.AuditEventUserManagement,
		iam.AuditCategoryAdministration,
		"create",
		iam.AuditOutcomeSuccess,
		&iam.AuditResource{Type: "user", ID: user.ID, Name: user.Email},
		// The STORED methods, not the requested ones: an audit line that
		// reports the request cannot be used to answer what the account may
		// actually present.
		map[string]interface{}{"email": user.Email, "auth_methods": user.AuthMethods},
	)

	// Return the canonical user object (matches the shape returned by GetUser/
	// UpdateUser/ListUsers) with the invite appended.
	//
	// The token is rendered from inviteToken — the value the invite store
	// returned above — and NOT from the user record, which no longer carries it
	// out of the store: IdentityStore redacts it on every read so a caller with
	// user-read authority cannot redeem someone else's pending invite. This is
	// the one place the cleartext token is exposed, and sourcing it here from
	// the local makes that explicit rather than dependent on a field riding
	// along on the record.
	//
	// Both invite fields are declared at this level and sourced from the invite
	// store, which owns them. The user record carries neither any more: an
	// identity record holding a copy of a credential and its expiry was a second
	// answer to a question only the invite store can answer correctly.
	RespondJSON(w, r, http.StatusCreated, struct {
		*iam.User
		InviteToken     string    `json:"invite_token,omitempty"`
		InviteExpiresAt time.Time `json:"invite_expires_at,omitempty"`
		InviteURL       string    `json:"invite_url,omitempty"`
	}{
		User:            user,
		InviteToken:     inviteToken,
		InviteExpiresAt: expiresAt,
		InviteURL:       inviteURL,
	})
}

// revokeMintedInvite burns an invite that was minted for a create that then
// failed, so the token cannot outlive the user it was issued for.
//
// Deleting the user without this leaves a live, redeemable token pointing at
// an id that no longer exists — a credential in the wild that names nothing,
// which is the same stranded-state class the rollback exists to close. Redeem
// is the store's single-use primitive, so spending the token here is what
// makes it unusable; there is no separate revoke.
//
// Best-effort and detached for the same reasons as the user rollback: the
// caller is already reporting a failure, and the cancellation that caused it
// must not also cancel the cleanup.
func (h *IAMHandler) revokeMintedInvite(ctx context.Context, store iam.InviteStore, tenantID, token string) {
	if store == nil || token == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), userRollbackTimeout)
	defer cancel()

	if _, err := store.Redeem(ctx, tenantID, token); err != nil {
		h.logAuditEvent(ctx, tenantID, iam.AuditEventUserManagement,
			iam.AuditCategorySecurity, "create.rollback.invite", iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "invite", ID: tenantID},
			map[string]interface{}{"error": err.Error()})
	}
}

// userRollbackTimeout bounds the detached rollback below. It is generous
// because the rollback is the last chance to avoid a stranded account, and
// short enough that a wedged store cannot leak the goroutine indefinitely.
const userRollbackTimeout = 30 * time.Second

// rollbackCreatedUser removes a user whose enrollment could not be completed,
// so a failed create leaves nothing behind. Best-effort: the caller is already
// reporting the failure that triggered this, and a cleanup error must not
// replace it. The attempt is audited so an orphan that survives is visible.
func (h *IAMHandler) rollbackCreatedUser(ctx context.Context, tenantID, userID string) {
	// The rollback MUST outlive the request that triggered it.
	//
	// Every reason this is called — a store gone away, a deadline, a client
	// that hung up — can also have cancelled the request context, and a
	// cancelled context cancels the DeleteUser too. The rollback would then
	// fail for the same reason the create did, and leave behind exactly the
	// stranded, unactivatable user it exists to prevent. Worse, that is the
	// case where it is MOST likely to be needed.
	//
	// WithoutCancel keeps the context's values — tenant, audit identity,
	// request id — while dropping the cancellation, and the timeout keeps a
	// detached operation from running unbounded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), userRollbackTimeout)
	defer cancel()

	store, err := h.getIdentityStore(ctx)
	if err != nil || store == nil {
		h.logAuditEvent(ctx, tenantID, iam.AuditEventUserManagement,
			iam.AuditCategorySecurity, "create.rollback", iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "user", ID: userID},
			map[string]interface{}{"error": "identity store unavailable for rollback"})
		return
	}
	if err := store.DeleteUser(ctx, tenantID, userID); err != nil {
		h.logAuditEvent(ctx, tenantID, iam.AuditEventUserManagement,
			iam.AuditCategorySecurity, "create.rollback", iam.AuditOutcomeFailure,
			&iam.AuditResource{Type: "user", ID: userID},
			map[string]interface{}{"error": err.Error()})
	}
}

// GetUser handles GET /api/v1/tenants/{tenant_id}/users/{user_id} - Get a specific user.
func (h *IAMHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Get user
	user, err := store.GetUser(ctx, tenantID, userID)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, user)
}

// UpdateUser handles PUT /api/v1/tenants/{tenant_id}/users/{user_id} - Update a user.
func (h *IAMHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse request
	var req struct {
		Email       *string            `json:"email,omitempty"`
		Username    *string            `json:"username,omitempty"`
		Name        *string            `json:"name,omitempty"`
		Attributes  *map[string]string `json:"attributes,omitempty"`
		Enabled     *bool              `json:"enabled,omitempty"`
		Roles       *[]string          `json:"roles,omitempty"`
		Groups      *[]string          `json:"groups,omitempty"`
		AuthMethods *[]string          `json:"auth_methods,omitempty"`
		Lifecycle   *iam.Lifecycle     `json:"lifecycle,omitempty"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Update user
	opts := iam.UpdateUserOptions{
		Email:      req.Email,
		Username:   req.Username,
		Name:       req.Name,
		Attributes: req.Attributes,
		// A pointer so an omitted "enabled" means "unspecified", not false.
		// As a plain bool it meant every create without the field explicitly
		// set would have suspended the user the moment CreateUser started
		// honouring it.
		Enabled: req.Enabled,
		// Forwarded, so a caller that sends them observes the change. Accepting
		// an account-state field and answering 200 with nothing changed is the
		// fail-open shape: the caller believes the account was disabled.
		AuthMethods: req.AuthMethods,
		Lifecycle:   req.Lifecycle,
		Roles:       req.Roles,
		Groups:      req.Groups,
	}

	user, err := store.UpdateUser(ctx, tenantID, userID, opts)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, user)
}

// DeleteUser handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id} - Delete a user.
func (h *IAMHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Delete user
	if err := store.DeleteUser(ctx, tenantID, userID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// AssignRole handles POST /api/v1/tenants/{tenant_id}/users/{user_id}/roles - Assign a role to a user.
func (h *IAMHandler) AssignRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	// Parse request
	var req struct {
		RoleID string `json:"role_id"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if req.RoleID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "role_id is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Assign role
	if err := store.AssignRole(ctx, tenantID, userID, req.RoleID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}

// RevokeRole handles DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/roles/{role} - Revoke a role from a user.
func (h *IAMHandler) RevokeRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant ID is required"))
		return
	}

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user ID is required"))
		return
	}

	roleID := chi.URLParam(r, "role")
	if roleID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "role ID is required"))
		return
	}

	// Get identity store
	identityStore, ok := h.server.service.(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	if !ok {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	store, err := identityStore.GetIdentityStore(ctx)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if store == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not initialized"))
		return
	}

	// Revoke role
	if err := store.RevokeRole(ctx, tenantID, userID, roleID); err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondNoContent(w, r)
}
