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
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// --------------------------------------------------------------------------
// Reserved tenant-id patterns (from the plan § "Open design questions").
// --------------------------------------------------------------------------

var (
	reservedTenantIDs = map[string]struct{}{
		"system":  {},
		"default": {},
	}
	reservedTenantIDPrefixUnderscore = regexp.MustCompile(`^_`)
	reservedTenantIDColon            = regexp.MustCompile(`^.+:`)
	nonAlphanumHyphen                = regexp.MustCompile(`[^a-z0-9-]+`)
	consecutiveHyphens               = regexp.MustCompile(`-{2,}`)
)

// isReservedTenantID returns true when the proposed id is rejected by policy.
func isReservedTenantID(id string) bool {
	if _, ok := reservedTenantIDs[id]; ok {
		return true
	}
	if reservedTenantIDPrefixUnderscore.MatchString(id) {
		return true
	}
	if reservedTenantIDColon.MatchString(id) {
		return true
	}
	return false
}

// --------------------------------------------------------------------------
// Slug helpers
// --------------------------------------------------------------------------

// slugify converts a free-form tenant name to a lowercase kebab-case id.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	// Replace whitespace with hyphens before stripping non-alphanum.
	s = strings.ReplaceAll(s, " ", "-")
	// Replace any non-alphanumeric, non-hyphen characters with a hyphen.
	s = nonAlphanumHyphen.ReplaceAllString(s, "-")
	// Collapse runs of consecutive hyphens into a single hyphen.
	s = consecutiveHyphens.ReplaceAllString(s, "-")
	// Remove leading/trailing hyphens.
	s = strings.Trim(s, "-")
	if s == "" {
		return ""
	}
	if len(s) < 3 {
		s += "org"
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

// deriveTenantID generates a slug-based tenant id, appending -2, -3, … on
// collision. Falls back to a UUID when no slug can be derived.
func deriveTenantID(
	ctx context.Context,
	tenantName string,
	tenantExists func(ctx context.Context, id string) (bool, error),
) (string, error) {
	slug := slugify(tenantName)
	if slug == "" {
		return uuid.New().String(), nil
	}
	base := slug
	for i := 0; i < 100; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}
		exists, err := tenantExists(ctx, candidate)
		if err != nil {
			return uuid.New().String(), nil
		}
		if !exists {
			return candidate, nil
		}
	}
	return uuid.New().String(), nil
}

// emailShapeRegex matches the addresses iam.validateDisplayName refuses. It is
// deliberately the same shape that validator uses rather than a stricter one:
// this endpoint exists to refuse early exactly what the store would refuse
// late, so a name this accepts and the store rejects would reintroduce the
// failure it prevents.
var emailShapeRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// isEmailShaped reports whether s would be refused as a display name.
func isEmailShaped(s string) bool {
	return emailShapeRegex.MatchString(s)
}

// deriveHandle builds a tenant-unique login handle from an email address.
//
// The handle charset is [A-Za-z0-9_-], 3-64 characters, so an address cannot be
// one: "@" is excluded, and so are the dots and plus-tags ordinary local parts
// carry. The local part is kept, stripped to the charset and lowercased — the
// form an administrator types, and stable across the case an address does not
// distinguish.
//
// It does NOT loop on collision, and does not need to: this runs only on the
// self-registration path, which creates a FRESH tenant (deriveTenantID appends
// -2, -3, … until the id is unused) and makes the registrant its first user.
// There is nothing in that tenant to collide with. A caller adding a second
// user to an existing tenant must go through the ordinary create, which takes
// an explicit handle and surfaces the store's uniqueness error.
//
// Short and empty local parts get a suffix rather than a rejection: "a.b@x.test"
// sanitizes to two characters, which is legal as an email and not as a handle,
// and refusing the registration over it would be a worse answer than naming the
// account "ab-admin".
func deriveHandle(email string) string {
	local := email
	if at := strings.Index(email, "@"); at > 0 {
		local = email[:at]
	}

	var b strings.Builder
	for _, r := range strings.ToLower(local) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	handle := b.String()

	switch {
	case handle == "":
		handle = "admin"
	case len(handle) < 3:
		handle += "-admin"
	}
	if len(handle) > 64 {
		handle = handle[:64]
	}
	return handle
}

// --------------------------------------------------------------------------
// Registration session store (Option B: in-memory for Phase 2).
// Phase 4 replaces this with the qrdb-backed persistent store.
// --------------------------------------------------------------------------

// registrationSession holds the multi-tenant context while the WebAuthn
// ceremony is in flight (begin → finish). TTL: 5 minutes.
type registrationSession struct {
	tenantID      string
	tenantName    string
	email         string
	displayName   string
	isolationMode string
	expiresAt     time.Time
}

// registrationSessionStore is a minimal in-memory store for Phase 2.
// Concurrent-safe via sync.Map.
type registrationSessionStore struct {
	sessions sync.Map // key: string → *registrationSession
}

var globalRegSessionStore = &registrationSessionStore{}

func (s *registrationSessionStore) put(sessionID string, sess *registrationSession) {
	s.sessions.Store(sessionID, sess)
}

func (s *registrationSessionStore) get(sessionID string) (*registrationSession, bool) {
	v, ok := s.sessions.Load(sessionID)
	if !ok {
		return nil, false
	}
	sess, ok := v.(*registrationSession)
	if !ok {
		return nil, false
	}
	if time.Now().After(sess.expiresAt) {
		s.sessions.Delete(sessionID)
		return nil, false
	}
	return sess, true
}

func (s *registrationSessionStore) delete(sessionID string) {
	s.sessions.Delete(sessionID)
}

// --------------------------------------------------------------------------
// Request / response types
// --------------------------------------------------------------------------

// SelfRegisterBeginRequest is the JSON body for POST /api/v1/auth/register/begin.
type SelfRegisterBeginRequest struct {
	// TenantName is the human-readable name for the new tenant.
	TenantName string `json:"tenant_name"`

	// Email is the registrant's email address.
	Email string `json:"email"`

	// DisplayName is the registrant's display name.
	DisplayName string `json:"display_name"`

	// IsolationMode selects the tenant isolation strategy: "keyspace" or "barrier".
	IsolationMode string `json:"isolation_mode"`
}

// SelfRegisterBeginResponse is returned by POST /api/v1/auth/register/begin.
type SelfRegisterBeginResponse struct {
	// PublicKeyCredentialCreationOptions is the WebAuthn challenge for the browser.
	PublicKeyCredentialCreationOptions interface{} `json:"public_key_credential_creation_options"`

	// SessionID must be echoed back in the finish request.
	SessionID string `json:"session_id"`
}

// SelfRegisterFinishRequest is the JSON body for POST /api/v1/auth/register/finish.
type SelfRegisterFinishRequest struct {
	// SessionID returned from the begin call.
	SessionID string `json:"session_id"`

	// Credential is the WebAuthn attestation response from the browser.
	Credential map[string]interface{} `json:"credential"`
}

// SelfRegisterFinishResponse is returned by POST /api/v1/auth/register/finish.
type SelfRegisterFinishResponse struct {
	// Status is "active", "pending", or "verification_required".
	Status string `json:"status"`

	// TenantID of the newly created tenant (always present).
	TenantID string `json:"tenant_id,omitempty"`

	// UserID of the newly created user (always present).
	UserID string `json:"user_id,omitempty"`

	// AccessToken is present only when Status == "active".
	AccessToken string `json:"access_token,omitempty"`

	// RefreshToken is present only when Status == "active".
	RefreshToken string `json:"refresh_token,omitempty"`

	// Message carries a human-readable explanation when Status != "active".
	Message string `json:"message,omitempty"`
}

// --------------------------------------------------------------------------
// Self-service registration errors
// --------------------------------------------------------------------------

// ErrSelfRegistrationDisabled is returned when registration is not enabled.
const ErrSelfRegistrationDisabled = "self_registration_disabled"

// ErrReservedTenantID is returned when the derived tenant id is reserved.
const ErrReservedTenantID = "reserved_tenant_id"

// ErrRegistrationSessionExpired is returned when the begin→finish session TTL elapses.
const ErrRegistrationSessionExpired = "registration_session_expired"

// ErrIsolationModeNotAllowed is returned when the requested mode is not in the allowlist.
const ErrIsolationModeNotAllowed = "isolation_mode_not_allowed"

// --------------------------------------------------------------------------
// IAMHandler: self-service registration begin
// --------------------------------------------------------------------------

// selfRegConfig returns the self-service registration config or a zero-value
// (disabled) config when the server configuration is not accessible.
func (h *IAMHandler) selfRegConfig() config.IAMSelfServiceRegistrationConfig {
	if h.server == nil || h.server.config == nil {
		return config.IAMSelfServiceRegistrationConfig{}
	}
	return h.server.config.IAMSelfServiceRegistration
}

// RegisterSelfBegin handles POST /api/v1/auth/register/begin.
//
// Validates the registration gate, derives the tenant id, launches the
// WebAuthn BeginRegistration ceremony, and stashes the multi-tenant context
// so RegisterSelfFinish can pick it up after the ceremony completes.
func (h *IAMHandler) RegisterSelfBegin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := h.selfRegConfig()

	if !cfg.Enabled {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrSelfRegistrationDisabled,
				"self-service registration is disabled on this server"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req SelfRegisterBeginRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}

	if strings.TrimSpace(req.TenantName) == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_name is required"))
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "email is required"))
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "display_name is required"))
		return
	}
	// Refused HERE rather than at the create, because the create happens after
	// the tenant has already been written: a display name the store rejects
	// would fail the ceremony at its last step and leave an orphan tenant with
	// no members and no way in. The store refuses an email-shaped display name
	// (iam.validateDisplayName), so this endpoint refuses it too, at the point
	// where refusing costs nothing.
	if isEmailShaped(strings.TrimSpace(req.DisplayName)) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "display_name must not be an email address"))
		return
	}

	// Resolve and validate isolation mode.
	allowedModes := cfg.AllowedIsolationModes
	if len(allowedModes) == 0 {
		allowedModes = []string{config.TenantIsolationKeyspace}
	}
	isolationMode := req.IsolationMode
	if isolationMode == "" {
		isolationMode = cfg.DefaultIsolationMode
		if isolationMode == "" {
			isolationMode = config.TenantIsolationKeyspace
		}
	}
	if !stringInSlice(isolationMode, allowedModes) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrIsolationModeNotAllowed,
				"isolation_mode not permitted by server configuration"))
		return
	}

	// Derive tenant ID (slug + collision suffix).
	identityStore, _ := h.getIdentityStore(ctx)
	tenantExists := func(ctx context.Context, id string) (bool, error) {
		if identityStore == nil {
			return false, nil
		}
		_, err := identityStore.GetTenant(ctx, id)
		return err == nil, nil
	}

	tenantID, err := deriveTenantID(ctx, req.TenantName, tenantExists)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	if isReservedTenantID(tenantID) {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrReservedTenantID,
				"tenant_name produces a reserved tenant id"))
		return
	}

	// Begin WebAuthn ceremony.
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

	options, err := mgr.BeginRegistration(ctx, tenantID, req.Email)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Stash registration context. SessionID correlates begin→finish.
	sessionID := uuid.New().String()
	globalRegSessionStore.put(sessionID, &registrationSession{
		tenantID:      tenantID,
		tenantName:    req.TenantName,
		email:         req.Email,
		displayName:   strings.TrimSpace(req.DisplayName),
		isolationMode: isolationMode,
		expiresAt:     time.Now().Add(5 * time.Minute),
	})

	RespondJSON(w, r, http.StatusOK, &SelfRegisterBeginResponse{
		PublicKeyCredentialCreationOptions: options,
		SessionID:                          sessionID,
	})
}

// --------------------------------------------------------------------------
// IAMHandler: self-service registration finish
// --------------------------------------------------------------------------

// RegisterSelfFinish handles POST /api/v1/auth/register/finish.
//
// Completes the WebAuthn ceremony, creates the new tenant and user record,
// grants roles, and either mints session tokens or returns a pending/
// verification_required status based on the operator configuration.
func (h *IAMHandler) RegisterSelfFinish(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	cfg := h.selfRegConfig()

	if !cfg.Enabled {
		RespondIAMError(w, r, http.StatusForbidden,
			NewIAMAPIError(ErrSelfRegistrationDisabled,
				"self-service registration is disabled on this server"))
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "failed to read request body"))
		return
	}
	defer r.Body.Close()

	var req SelfRegisterFinishRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "invalid JSON body"))
		return
	}
	if req.SessionID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "session_id is required"))
		return
	}
	if req.Credential == nil {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "credential is required"))
		return
	}

	sess, ok := globalRegSessionStore.get(req.SessionID)
	if !ok {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrRegistrationSessionExpired,
				"registration session not found or expired"))
		return
	}

	// Complete WebAuthn ceremony.
	webAuthnMgr, ok2 := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok2 {
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

	// The ceremony returns what it PROVED about this authenticator. It is
	// carried down to token issuance rather than discarded: the registrant is
	// about to be granted Admin on a brand-new tenant, the gate has just held
	// them to the hardware bar, and a session that failed to say so would make
	// a legitimate hardware login look unattested until the operator logged in
	// a second time.
	attestation, err := mgr.FinishRegistration(ctx, sess.tenantID, sess.email, req.Credential)
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}
	globalRegSessionStore.delete(req.SessionID)

	// Obtain tenant manager and identity store.
	identityStore, err := h.getIdentityStore(ctx)
	if err != nil || identityStore == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}
	tenantMgr, err := h.getTenantManager(ctx)
	if err != nil || tenantMgr == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not available"))
		return
	}

	// Create the new tenant. The isolation mode is recorded in the attributes;
	// qrdb's per-tenant barrier path is assigned internally by Phase 2.5b.
	_, err = tenantMgr.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   sess.tenantID,
		Name: sess.tenantName,
		Settings: &iam.TenantSettings{
			DisplayName: sess.tenantName,
		},
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Determine the initial user state. BOTH gates land on pending, and the
	// decision has to be made here rather than corrected afterwards: qrdb's
	// transition table has no active -> pending edge, so an account created
	// active and then "moved" to pending simply stays active — which, on the
	// email-verification path, is an account that authenticates without ever
	// verifying its address.
	userState := iam.LifecycleActive
	if cfg.RequireAdminApproval || cfg.RequireEmailVerification {
		userState = iam.LifecyclePending
	}

	// Create the user in the new tenant, IN the state decided above. It used to
	// be computed here and applied only on the branch that needed a change, so
	// the no-approval path created a user that defaulted to pending and then
	// minted it a session — an account persisted as pending holding an active
	// bearer. Passing it to the create is what makes the record and the session
	// agree.
	// The handle is DERIVED, not the email address. A username may not contain
	// "@" (iam.validateUsername), so passing the address made every
	// self-registration fail at this call — after the tenant had been created,
	// leaving an orphan tenant nobody could enter. The display name is the one
	// the registrant supplied at begin; the store refuses an email-shaped
	// display name too, which is why begin now rejects one.
	createdUser, err := identityStore.CreateUser(ctx, sess.tenantID, iam.CreateUserOptions{
		Email:       sess.email,
		Username:    deriveHandle(sess.email),
		Name:        sess.displayName,
		AuthMethods: []string{"webauthn"},
		Lifecycle:   userState,
		Attributes: map[string]string{
			"isolation_mode":    sess.isolationMode,
			"self_registered":   "true",
			"registration_time": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Grant roles: Admin@<their-tenant> always, plus the configured default_role.
	// It FAILS CLOSED. A creator without the Admin role owns a tenant nobody can
	// administer, so a failed grant leaves no account behind and mints nothing.
	// The cleanup runs on a context detached from the request, so a client that
	// disconnects cannot strand a half-provisioned account that blocks a retry.
	if err := grantSelfRegistrationRoles(ctx, identityStore, sess.tenantID, createdUser.ID, cfg.DefaultRole); err != nil {
		if delErr := identityStore.DeleteUser(context.WithoutCancel(ctx), sess.tenantID, createdUser.ID); delErr != nil {
			err = errors.Join(err, delErr)
		}
		h.server.logger.WithError(err).Error("iam.self_registration: role grant failed, registration refused")
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "registration could not grant the tenant's built-in roles"))
		return
	}

	if cfg.RequireAdminApproval {
		RespondJSON(w, r, http.StatusOK, &SelfRegisterFinishResponse{
			Status:   "pending",
			TenantID: sess.tenantID,
			UserID:   strconv.FormatUint(createdUser.ID, 10),
			Message:  "Your account is awaiting admin approval.",
		})
		return
	}

	if cfg.RequireEmailVerification {
		// The account was CREATED pending above; awaiting verification is that
		// state, not a correction applied afterwards. It used to write an inline
		// "unverified" that belonged to no vocabulary, and because the
		// may-authenticate check excluded only "disabled", an account waiting on
		// verification could log in anyway (#334).
		RespondJSON(w, r, http.StatusOK, &SelfRegisterFinishResponse{
			Status:   "verification_required",
			TenantID: sess.tenantID,
			UserID:   strconv.FormatUint(createdUser.ID, 10),
			Message:  "Please check your email to verify your account before signing in.",
		})
		return
	}

	// Mint session tokens via the OIDC provider.
	oidcGetter, ok3 := h.server.service.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	if !ok3 {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not available"))
		return
	}
	oidcProvider, err := oidcGetter.GetOIDCProvider(ctx)
	if err != nil || oidcProvider == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "OIDC provider not configured"))
		return
	}

	// The session token reports the authentication that actually happened —
	// no more and no less.
	//
	// It used to be minted with amr=["webauthn","hwk"], acr=hardware-attested
	// and attest_level=hardware UNCONDITIONALLY, for a registrant who had just
	// been granted Admin on a brand-new tenant, with a credential nothing had
	// examined. A headless-Chrome virtual authenticator produced the same token
	// a YubiKey did.
	//
	// Now the claims follow the evidence the ceremony produced. When the gate
	// proved hardware the session says so, so a first login is not spuriously
	// weaker than the credential behind it; when it proved less, the ACR is
	// omitted and a policy requiring hardware refuses the session rather than
	// being fooled by a label this handler chose.
	amr := []string{"webauthn"}
	acr := ""
	if attestation.IsHardware() {
		amr = []string{"webauthn", "hwk"}
		acr = iam.ACRHardwareAttested
	}
	tokens, err := oidcProvider.IssueHardwareTokens(ctx, iam.HardwareTokenOptions{
		User:        createdUser,
		AMR:         amr,
		ACR:         acr,
		Attestation: attestation,
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &SelfRegisterFinishResponse{
		Status:       "active",
		TenantID:     sess.tenantID,
		UserID:       strconv.FormatUint(createdUser.ID, 10),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
	})
}

// --------------------------------------------------------------------------
// Admin approval / rejection endpoints
// --------------------------------------------------------------------------

// ApproveUser handles POST /api/v1/iam/users/{user_id}/approve.
// Transitions a pending user to active. Requires tenant_id query parameter.
func (h *IAMHandler) ApproveUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id query parameter is required"))
		return
	}

	identityStore, err := h.getIdentityStore(ctx)
	if err != nil || identityStore == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	active := iam.LifecycleActive
	_, err = identityStore.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{
		Lifecycle: &active,
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]string{
		"status":  "approved",
		"user_id": strconv.FormatUint(userID, 10),
	})
}

// RejectUser handles POST /api/v1/iam/users/{user_id}/reject.
// Soft-deletes a pending user (marked disabled; retention policy enforces TTL).
func (h *IAMHandler) RejectUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID, ok := requireUserIDParam(w, r)
	if !ok {
		return
	}
	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "tenant_id query parameter is required"))
		return
	}

	var body struct {
		Reason string `json:"reason"`
	}
	if b, err := io.ReadAll(r.Body); err == nil {
		_ = json.Unmarshal(b, &body)
	}
	defer r.Body.Close()

	identityStore, err := h.getIdentityStore(ctx)
	if err != nil || identityStore == nil {
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}

	disabled := iam.LifecycleDisabled
	_, err = identityStore.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{
		Lifecycle: &disabled,
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]string{
		"status":  "rejected",
		"user_id": strconv.FormatUint(userID, 10),
	})
}

// --------------------------------------------------------------------------
// Private helpers
// --------------------------------------------------------------------------

// SelfRegistrationRoleGrantError reports that a self-registered tenant creator
// could not be granted one of the tenant's built-in roles. Registration fails
// closed on it: the account is deleted and no session is minted, because a
// creator who holds no Admin role owns a tenant nobody can administer.
type SelfRegistrationRoleGrantError struct {
	TenantID string
	Role     string
	Cause    error
}

// Error implements the error interface.
func (e *SelfRegistrationRoleGrantError) Error() string {
	return "iam.self_registration: grant role " + e.Role + " in tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap returns the cause, so a caller can still read what the store said.
func (e *SelfRegistrationRoleGrantError) Unwrap() error {
	return e.Cause
}

// builtInRoleName maps a configured role name (config.ValidRoleNames spells
// them "Admin", "User", "SO", ...) to the name qrdb seeds the built-in role
// under, which is the lower-case form ("admin", "user", "so", ...).
func builtInRoleName(configured string) string {
	return strings.ToLower(configured)
}

// grantSelfRegistrationRoles grants the tenant creator the tenant's Admin role
// and the configured default role. The first failure is returned; nothing is
// left half-granted silently, because the caller deletes the account on error.
func grantSelfRegistrationRoles(
	ctx context.Context,
	store *iam.IdentityStore,
	tenantID string,
	userID uint64,
	defaultRole string,
) error {
	if defaultRole == "" {
		defaultRole = "User"
	}
	roles := []string{qrdbsdk.RoleNameAdmin}
	if name := builtInRoleName(defaultRole); name != qrdbsdk.RoleNameAdmin {
		roles = append(roles, name)
	}
	for _, roleName := range roles {
		if err := grantBuiltInRole(ctx, store, tenantID, userID, roleName); err != nil {
			return err
		}
	}
	return nil
}

// grantBuiltInRole assigns the tenant's built-in role named roleName to the
// user. User.Roles holds role IDs, so the name is resolved to the role the
// tenant actually carries before it is assigned; a name written in its place
// would name no role, and IsAdministrativeAccount compares IDs.
// A role the user already holds is a success, so a re-grant is a no-op.
func grantBuiltInRole(
	ctx context.Context,
	store *iam.IdentityStore,
	tenantID string,
	userID uint64,
	roleName string,
) error {
	role, err := store.GetRoleByName(ctx, tenantID, roleName)
	if err != nil {
		return &SelfRegistrationRoleGrantError{TenantID: tenantID, Role: roleName, Cause: err}
	}
	err = store.AssignRole(ctx, tenantID, userID, role.ID)
	// coded-sentinel-ok: the already-exists class is the intent. AssignRole
	// raises it only for a role the user already holds: its validation and
	// lookups raise invalid-input or not-found, and its one write updates the
	// role list of an existing record, which has no uniqueness to collide with.
	if err != nil && !errors.Is(err, iam.ErrRoleAlreadyAssigned) {
		return &SelfRegistrationRoleGrantError{TenantID: tenantID, Role: roleName, Cause: err}
	}
	return nil
}

// getTenantManager retrieves the TenantManager from the service layer.
func (h *IAMHandler) getTenantManager(ctx context.Context) (iam.TenantManager, error) {
	svc, ok := h.server.service.(interface {
		GetTenantManager(ctx context.Context) (iam.TenantManager, error)
	})
	if !ok {
		return nil, nil
	}
	return svc.GetTenantManager(ctx)
}

// stringInSlice returns true when s is present in the list.
func stringInSlice(s string, list []string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
