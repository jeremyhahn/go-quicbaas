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
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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

	if err := mgr.FinishRegistration(ctx, sess.tenantID, sess.email, req.Credential); err != nil {
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

	// Determine initial user state.
	userState := iam.UserStateActive
	if cfg.RequireAdminApproval {
		userState = iam.UserStatePending
	}

	// Create the user in the new tenant.
	createdUser, err := identityStore.CreateUser(ctx, sess.tenantID, iam.CreateUserOptions{
		Email:       sess.email,
		Username:    sess.email,
		Name:        sess.email,
		AuthMethods: []string{"webauthn"},
		Attributes: map[string]interface{}{
			"isolation_mode":    sess.isolationMode,
			"self_registered":   true,
			"registration_time": time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	// Apply initial state when not active.
	if userState != iam.UserStateActive {
		st := userState
		_, _ = identityStore.UpdateUser(ctx, sess.tenantID, createdUser.ID, iam.UpdateUserOptions{
			State: &st,
		})
	}

	// Grant roles: Admin@<their-tenant> always, plus the configured default_role.
	h.grantRoleIfMissing(ctx, identityStore, sess.tenantID, createdUser.ID, "Admin")
	defaultRole := cfg.DefaultRole
	if defaultRole == "" {
		defaultRole = "User"
	}
	if defaultRole != "Admin" {
		h.grantRoleIfMissing(ctx, identityStore, sess.tenantID, createdUser.ID, defaultRole)
	}

	if cfg.RequireAdminApproval {
		RespondJSON(w, r, http.StatusOK, &SelfRegisterFinishResponse{
			Status:   "pending",
			TenantID: sess.tenantID,
			UserID:   createdUser.ID,
			Message:  "Your account is awaiting admin approval.",
		})
		return
	}

	if cfg.RequireEmailVerification {
		unverified := iam.UserState("unverified")
		_, _ = identityStore.UpdateUser(ctx, sess.tenantID, createdUser.ID, iam.UpdateUserOptions{
			State: &unverified,
		})
		RespondJSON(w, r, http.StatusOK, &SelfRegisterFinishResponse{
			Status:   "verification_required",
			TenantID: sess.tenantID,
			UserID:   createdUser.ID,
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

	tokens, err := oidcProvider.IssueHardwareTokens(ctx, iam.HardwareTokenOptions{
		User:        createdUser,
		AMR:         []string{"webauthn", "hwk"},
		ACR:         iam.ACRHardwareAttested,
		AttestLevel: "hardware",
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

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user_id is required"))
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

	active := iam.UserStateActive
	_, err = identityStore.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{
		State: &active,
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]string{
		"status":  "approved",
		"user_id": userID,
	})
}

// RejectUser handles POST /api/v1/iam/users/{user_id}/reject.
// Soft-deletes a pending user (marked disabled; retention policy enforces TTL).
func (h *IAMHandler) RejectUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		RespondIAMError(w, r, http.StatusBadRequest,
			NewIAMAPIError(ErrBadRequest, "user_id is required"))
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

	disabled := iam.UserStateDisabled
	_, err = identityStore.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{
		State: &disabled,
	})
	if err != nil {
		h.server.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]string{
		"status":  "rejected",
		"user_id": userID,
	})
}

// --------------------------------------------------------------------------
// Private helpers
// --------------------------------------------------------------------------

// grantRoleIfMissing appends a role to the user when not already present.
func (h *IAMHandler) grantRoleIfMissing(
	ctx context.Context,
	store *iam.IdentityStore,
	tenantID, userID, roleName string,
) {
	user, err := store.GetUser(ctx, tenantID, userID)
	if err != nil {
		return
	}
	for _, r := range user.Roles {
		if r == roleName {
			return
		}
	}
	roles := make([]string, 0, len(user.Roles)+1)
	roles = append(roles, user.Roles...)
	roles = append(roles, roleName)
	_, _ = store.UpdateUser(ctx, tenantID, userID, iam.UpdateUserOptions{Roles: &roles})
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
