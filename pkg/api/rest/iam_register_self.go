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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
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

	// userID is the pending account begin created for the ceremony to enrol.
	userID uint64

	// reaper discards the tenant and account when the session expires
	// unfinished; a finish that takes the session stops it.
	reaper *time.Timer
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

// take removes the session and returns it, expired or not, and stops its
// reaper. Exactly one caller — a finish or the reaper — wins a session, so the
// discard that follows an expiry runs once.
func (s *registrationSessionStore) take(sessionID string) (*registrationSession, bool) {
	v, ok := s.sessions.LoadAndDelete(sessionID)
	if !ok {
		return nil, false
	}
	sess, ok := v.(*registrationSession)
	if !ok {
		return nil, false
	}
	if sess.reaper != nil {
		sess.reaper.Stop()
	}
	return sess, true
}

func (s *registrationSessionStore) delete(sessionID string) {
	s.sessions.Delete(sessionID)
}

// holdsTenant reports whether a session begun by this process registers the
// tenant. That session's own reaper discards it, so the restart sweeper leaves
// it alone.
func (s *registrationSessionStore) holdsTenant(tenantID string) bool {
	held := false
	s.sessions.Range(func(_, value any) bool {
		if sess, ok := value.(*registrationSession); ok && sess.tenantID == tenantID {
			held = true
			return false
		}
		return true
	})
	return held
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
	UserID uint64 `json:"user_id,omitempty,string"`

	// AccessToken is present only when Status == "active".
	AccessToken string `json:"access_token,omitempty"`

	// RefreshToken is present only when Status == "active".
	RefreshToken string `json:"refresh_token,omitempty"`

	// Message carries a human-readable explanation when Status != "active".
	Message string `json:"message,omitempty"`
}

// selfRegisterDecisionResponse is returned when an admin approves or rejects
// a pending self-registration.
type selfRegisterDecisionResponse struct {
	// Status is "approved" or "rejected".
	Status string `json:"status"`

	// UserID of the registration the decision applies to.
	UserID uint64 `json:"user_id,string"`
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
	// retired holds the ids the engine refused as deleted during this begin.
	// A deleted tenant id is retired for good, and the broker's own record of
	// it is gone, so only the engine's refusal tells the derivation to move on.
	retired := map[string]bool{}
	tenantExists := func(ctx context.Context, id string) (bool, error) {
		if retired[id] {
			return true, nil
		}
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

	if identityStore == nil {
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

	sess := &registrationSession{
		tenantID:      tenantID,
		tenantName:    req.TenantName,
		email:         req.Email,
		displayName:   strings.TrimSpace(req.DisplayName),
		isolationMode: isolationMode,
		expiresAt:     time.Now().Add(selfRegistrationSessionTTL),
	}

	// The BROKER creates the account; the ceremony only binds a credential to
	// it (ruling 189, #482). The ceremony used to be asked to create the
	// account itself — go-xkms's public-registration path, which files the
	// email as the username — and the broker's own create at finish then
	// collided on the email, so no self-registration could complete and each
	// attempt left an orphan tenant. Now the tenant and a PENDING account with
	// the broker's derived handle exist before the ceremony opens, and the
	// ceremony is the invite path, which enrols only an existing pending
	// account. Every failure from here on removes both again.
	err = h.provisionRegistration(ctx, tenantMgr, identityStore, sess, cfg.DefaultRole)
	// An id a discarded registration once held is retired by the engine, so a
	// retry under the same tenant name advances to the next candidate.
	for skips := 0; isRetiredTenantID(err) && skips < maxRetiredTenantSkips; skips++ {
		retired[sess.tenantID] = true
		next, deriveErr := deriveTenantID(ctx, req.TenantName, tenantExists)
		if deriveErr != nil {
			err = deriveErr
			break
		}
		if isReservedTenantID(next) {
			RespondIAMError(w, r, http.StatusBadRequest,
				NewIAMAPIError(ErrReservedTenantID,
					"tenant_name produces a reserved tenant id"))
			return
		}
		sess.tenantID = next
		err = h.provisionRegistration(ctx, tenantMgr, identityStore, sess, cfg.DefaultRole)
	}
	if err != nil {
		var grantErr *SelfRegistrationRoleGrantError
		if errors.As(err, &grantErr) {
			RespondIAMError(w, r, http.StatusInternalServerError,
				NewIAMAPIError(ErrInternalError, "registration could not grant the tenant's built-in roles"))
			return
		}
		h.server.handleServiceError(w, r, err)
		return
	}
	options, err := mgr.BeginInviteRegistration(ctx, sess.tenantID, req.Email)
	if err != nil {
		h.discardRegistration(ctx, sess)
		h.server.handleServiceError(w, r, err)
		return
	}

	// Stash registration context. SessionID correlates begin→finish. A begin
	// that is never finished is discarded when its session expires, so an
	// abandoned ceremony leaves no tenant and no account behind.
	sessionID := uuid.New().String()
	sess.reaper = time.AfterFunc(selfRegistrationSessionTTL, func() {
		h.reapRegistration(context.WithoutCancel(ctx), sessionID)
	})
	globalRegSessionStore.put(sessionID, sess)

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

	// The session is TAKEN, not read: a finish spends it whatever the outcome,
	// so a refused ceremony cannot be replayed against an account that has
	// already been discarded.
	sess, ok := globalRegSessionStore.take(req.SessionID)
	if !ok {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrRegistrationSessionExpired,
				"registration session not found or expired"))
		return
	}
	if time.Now().After(sess.expiresAt) {
		h.discardRegistration(ctx, sess)
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
		h.discardRegistration(ctx, sess)
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not available"))
		return
	}
	mgr, err := webAuthnMgr.GetWebAuthnManager(ctx)
	if err != nil || mgr == nil {
		h.discardRegistration(ctx, sess)
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "WebAuthn manager not initialized"))
		return
	}

	// The ceremony binds the credential to the pending account begin created,
	// and returns what it PROVED about this authenticator. It is carried down
	// to token issuance rather than discarded: the registrant is about to be
	// granted Admin on a brand-new tenant, the gate has just held them to the
	// hardware bar, and a session that failed to say so would make a
	// legitimate hardware login look unattested until the operator logged in
	// a second time.
	attestation, err := mgr.FinishInviteRegistration(ctx, sess.tenantID, sess.email, req.Credential)
	if err != nil {
		h.discardRegistration(ctx, sess)
		h.server.handleServiceError(w, r, err)
		return
	}

	identityStore, err := h.getIdentityStore(ctx)
	if err != nil || identityStore == nil {
		h.discardRegistration(ctx, sess)
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "identity store not available"))
		return
	}
	tenantMgr, err := h.getTenantManager(ctx)
	if err != nil || tenantMgr == nil {
		h.discardRegistration(ctx, sess)
		RespondIAMError(w, r, http.StatusInternalServerError,
			NewIAMAPIError(ErrInternalError, "tenant manager not available"))
		return
	}

	// The roles were granted at begin, before the ceremony opened, so the
	// ceremony's attestation gate held this administrator to the hardware bar.
	// An account a gate holds pending has the tenant's registration record
	// removed now, so the sweep never discards an account that is held for
	// admin approval or email verification. An account no gate holds keeps
	// the record until it is active: a crash before activation leaves a
	// pending account the sweep still discards, never one nothing recovers.
	if cfg.RequireAdminApproval || cfg.RequireEmailVerification {
		if _, err := tenantMgr.UpdateTenant(ctx, sess.tenantID, iam.UpdateTenantOptions{
			FinishSelfRegistration: true,
		}); err != nil {
			h.discardRegistration(ctx, sess)
			h.server.logger.WithError(err).Error("iam.self_registration: recording the enrolment failed, registration refused")
			RespondIAMError(w, r, http.StatusInternalServerError,
				NewIAMAPIError(ErrInternalError, "registration could not record the enrolled account"))
			return
		}
	}

	if cfg.RequireAdminApproval {
		RespondJSON(w, r, http.StatusOK, &SelfRegisterFinishResponse{
			Status:   "pending",
			TenantID: sess.tenantID,
			UserID:   sess.userID,
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
			UserID:   sess.userID,
			Message:  "Please check your email to verify your account before signing in.",
		})
		return
	}

	// Neither gate holds the account, so the credential just bound activates
	// it. The account was created pending so the ceremony would enrol it; the
	// pending -> active move is the one qrdb's transition table allows.
	active := iam.LifecycleActive
	createdUser, err := identityStore.UpdateUser(ctx, sess.tenantID, sess.userID, iam.UpdateUserOptions{
		Lifecycle: &active,
	})
	if err != nil {
		h.discardRegistration(ctx, sess)
		h.server.handleServiceError(w, r, err)
		return
	}
	// The account is active, so the registration is finished whatever happens
	// to its record: a failure to remove the record is logged, and the sweep
	// keeps a tenant whose account is active.
	if _, err := tenantMgr.UpdateTenant(ctx, sess.tenantID, iam.UpdateTenantOptions{
		FinishSelfRegistration: true,
	}); err != nil {
		h.server.logger.WithError(err).Error("iam.self_registration: removing the registration record of an activated account failed")
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
		UserID:       createdUser.ID,
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

	RespondJSON(w, r, http.StatusOK, &selfRegisterDecisionResponse{
		Status: "approved",
		UserID: userID,
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

	RespondJSON(w, r, http.StatusOK, &selfRegisterDecisionResponse{
		Status: "rejected",
		UserID: userID,
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

// selfRegistrationSessionTTL bounds a begin→finish ceremony. A begin that is
// not finished within it is discarded.
const selfRegistrationSessionTTL = 5 * time.Minute

// provisionRegistration creates the registrant's tenant and their PENDING
// account, the one the ceremony then enrols, and records the account id on
// the session. The account is pending whatever the gates say: the ceremony
// enrols only a pending account, and finish activates it when no gate holds
// it. The handle is DERIVED, not the email address — a username may not
// contain "@" (iam.validateUsername). A failure leaves nothing behind.
func (h *IAMHandler) provisionRegistration(ctx context.Context, tenants iam.TenantManager, store *iam.IdentityStore, sess *registrationSession, defaultRole string) error {
	// The isolation mode is recorded in the attributes; qrdb's per-tenant
	// barrier path is assigned internally by Phase 2.5b. The tenant carries
	// the broker's record of the unfinished registration, so a restart that
	// loses the session still finds the tenant to discard at its expiry.
	if _, err := tenants.CreateTenant(ctx, iam.CreateTenantOptions{
		ID:   sess.tenantID,
		Name: sess.tenantName,
		Settings: &iam.TenantSettings{
			DisplayName: sess.tenantName,
		},
		SelfRegistration: &iam.TenantSelfRegistration{Expires: sess.expiresAt},
	}); err != nil {
		return err
	}

	user, err := store.CreateUser(ctx, sess.tenantID, iam.CreateUserOptions{
		Email:       sess.email,
		Username:    deriveHandle(sess.email),
		Name:        sess.displayName,
		AuthMethods: []string{"webauthn"},
		Lifecycle:   iam.LifecyclePending,
		Attributes: map[string]string{
			"isolation_mode":    sess.isolationMode,
			"self_registered":   "true",
			"registration_time": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		h.discardRegistration(ctx, sess)
		return err
	}
	sess.userID = user.ID

	// Grant roles: Admin@<their-tenant> always, plus the configured
	// default_role. They are granted HERE, before the ceremony opens, because
	// the ceremony's attestation gate asks whether the account is
	// administrative: an account with no roles yet reads as a regular user, so
	// under a permissive tenant assurance a software authenticator would enrol
	// and only then be made Admin. The account is still PENDING, so the roles
	// grant nothing until finish activates it. It FAILS CLOSED: a creator
	// without the Admin role owns a tenant nobody can administer, so a failed
	// grant leaves no account and no tenant behind.
	if err := grantSelfRegistrationRoles(ctx, store, sess.tenantID, sess.userID, defaultRole); err != nil {
		h.discardRegistration(ctx, sess)
		h.server.logger.WithError(err).Error("iam.self_registration: role grant failed, registration refused")
		return err
	}
	return nil
}

// discardRegistration removes what provisionRegistration created — the
// account, then the tenant — so a registration that does not complete leaves
// no orphan behind to block a retry. It runs on a context detached from the
// request, so a client that disconnects cannot strand a half-provisioned
// tenant. A failure to discard is logged; the caller's own error is what the
// registrant is told.
func (h *IAMHandler) discardRegistration(ctx context.Context, sess *registrationSession) {
	store, storeErr := h.getIdentityStore(ctx)
	tenants, tenantErr := h.getTenantManager(ctx)
	err := errors.Join(storeErr, tenantErr, discardOwnedRegistration(ctx, tenants, store, sess))
	if err != nil {
		h.server.logger.WithError(err).Error("iam.self_registration: discarding an incomplete registration failed")
	}
}

// discardOwnedRegistration deletes the registration's account and tenant only
// while they are still the registration's own: the tenant still records the
// unfinished registration, and it holds nothing but the registrant's
// still-pending account. An account an operator approved, or a tenant another
// user joined, while the ceremony was open is reported and kept.
func discardOwnedRegistration(ctx context.Context, tenants iam.TenantManager, store *iam.IdentityStore, sess *registrationSession) error {
	if tenants == nil || store == nil {
		return removeRegistration(ctx, tenants, store, sess)
	}
	ctx = context.WithoutCancel(ctx)
	tenant, err := tenants.GetTenant(ctx, sess.tenantID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if tenant.SelfRegistration == nil {
		return &SelfRegistrationTenantInUseError{TenantID: sess.tenantID}
	}
	return discardUnfinishedTenant(ctx, tenants, store, sess.tenantID, sess.userID)
}

// isRetiredTenantID reports whether err is the engine's refusal of a tenant id
// that was used and deleted.
func isRetiredTenantID(err error) bool {
	var deleted *qrdbtenant.TenantDeletedError
	return errors.As(err, &deleted)
}

// maxRetiredTenantSkips bounds how many retired ids one begin steps past.
const maxRetiredTenantSkips = 20

// removeRegistration deletes the registration's account and tenant, and
// returns what failed.
func removeRegistration(ctx context.Context, tenants iam.TenantManager, store *iam.IdentityStore, sess *registrationSession) error {
	ctx = context.WithoutCancel(ctx)
	var err error
	if sess.userID != 0 && store != nil {
		err = store.DeleteUser(ctx, sess.tenantID, sess.userID)
	}
	// A server without a tenant manager provisioned no tenant to remove.
	if tenants != nil {
		err = errors.Join(err, tenants.DeleteTenant(ctx, sess.tenantID))
	}
	return err
}

// reapRegistration discards a registration whose session expired unfinished.
// It does nothing when a finish has already taken the session.
func (h *IAMHandler) reapRegistration(ctx context.Context, sessionID string) {
	sess, ok := globalRegSessionStore.take(sessionID)
	if !ok {
		return
	}
	h.discardRegistration(ctx, sess)
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
