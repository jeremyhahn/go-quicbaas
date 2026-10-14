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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/lease"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// Pooled provider account REST surface (/api/v1/agent-accounts).
//
// SECURITY INVARIANT: AccountCredentialRESTRequest is an INBOUND-ONLY shape.
// It is a field of CreateAccountRESTRequest and RotateAccountCredentialRESTRequest
// and of nothing else. AccountRESTResponse — the only account shape any handler
// writes — is built by accountToREST from the metadata record and has no
// credential field to populate. TestAccountRESTResponsesCarryNoCredential
// asserts this structurally.
// =============================================================================

// =============================================================================
// REST Request/Response Types
// =============================================================================

// AccountCredentialRESTRequest is the inbound secret envelope for an account.
// It appears ONLY on create and rotate requests and is never echoed back.
type AccountCredentialRESTRequest struct {
	Env               map[string]string `json:"env,omitempty"`
	Files             map[string][]byte `json:"files,omitempty"`
	AccessToken       string            `json:"access_token,omitempty"`
	RefreshToken      string            `json:"refresh_token,omitempty"`
	ExpiresAt         time.Time         `json:"expires_at,omitempty"`
	TokenEndpoint     string            `json:"token_endpoint,omitempty"`
	ClientID          string            `json:"client_id,omitempty"`
	Scopes            []string          `json:"scopes,omitempty"`
	AccessTokenEnvKey string            `json:"access_token_env_key,omitempty"`
}

// toOrchestration converts the inbound envelope to the orchestration type.
func (c *AccountCredentialRESTRequest) toOrchestration() *orchestration.AccountCredential {
	if c == nil {
		return nil
	}
	return &orchestration.AccountCredential{
		Env:               c.Env,
		Files:             c.Files,
		AccessToken:       c.AccessToken,
		RefreshToken:      c.RefreshToken,
		ExpiresAt:         c.ExpiresAt,
		TokenEndpoint:     c.TokenEndpoint,
		ClientID:          c.ClientID,
		Scopes:            c.Scopes,
		AccessTokenEnvKey: c.AccessTokenEnvKey,
	}
}

// CreateAccountRESTRequest is the JSON body for POST /api/v1/agent-accounts.
type CreateAccountRESTRequest struct {
	TenantID      string `json:"tenant_id"`
	ProjectID     string `json:"project_id,omitempty"`
	Provider      string `json:"provider"`
	DisplayName   string `json:"display_name,omitempty"`
	Default       bool   `json:"default,omitempty"`
	RefreshPolicy string `json:"refresh_policy,omitempty"`

	// Credential is the inline credential. Exactly one of Credential and
	// CredentialSecretRef is required.
	Credential *AccountCredentialRESTRequest `json:"credential,omitempty"`

	// CredentialSecretRef names the tenant secret the credential is read from
	// on every spawn (e.g. "agents/claude/claude1/oauth_token").
	CredentialSecretRef string `json:"credential_secret_ref,omitempty"`

	// CredentialSecretFile materializes the referenced secret as this file
	// (relative to the session home) instead of as the access token.
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`
	// MaxConcurrentSessions caps the account's live sessions. Zero is
	// unmetered.
	MaxConcurrentSessions int `json:"max_concurrent_sessions,omitempty"`

	// Billing is how the account is billed; absent leaves it not configured.
	Billing *AccountBillingREST `json:"billing,omitempty"`

	// Spend is what the account may spend and how many failures take it out
	// of service (go-quicbaas#781); absent is unmetered.
	Spend *orchestration.AccountSpendPolicy `json:"spend,omitempty"`
}

// AccountBillingREST is how a pooled account is billed: mode "subscription"
// or "api", and a subscription's monthly fee in millionths of a US dollar.
type AccountBillingREST struct {
	Mode             string `json:"mode,omitempty"`
	MonthlyFeeMicros int64  `json:"monthly_fee_micros,omitempty"`
}

// toOrchestration converts the body's billing; nil is "not configured".
func (b *AccountBillingREST) toOrchestration() orchestration.AccountBilling {
	if b == nil {
		return orchestration.AccountBilling{}
	}
	return orchestration.AccountBilling{Mode: orchestration.BillingMode(b.Mode), MonthlyFeeMicros: b.MonthlyFeeMicros}
}

// optional converts an update body's billing to the service's "nil means
// unchanged" pointer.
func (b *AccountBillingREST) optional() *orchestration.AccountBilling {
	if b == nil {
		return nil
	}
	billing := b.toOrchestration()
	return &billing
}

// derefSpendPolicy is a create body's spend policy; absent is unmetered.
func derefSpendPolicy(policy *orchestration.AccountSpendPolicy) orchestration.AccountSpendPolicy {
	if policy == nil {
		return orchestration.AccountSpendPolicy{}
	}
	return *policy
}

// accountSpendToREST renders an account's spend policy, nil when unmetered.
func accountSpendToREST(policy orchestration.AccountSpendPolicy) *orchestration.AccountSpendPolicy {
	if !policy.Metered() {
		return nil
	}
	return &policy
}

// accountBillingToREST renders an account's billing, nil when not configured.
func accountBillingToREST(b orchestration.AccountBilling) *AccountBillingREST {
	if b == (orchestration.AccountBilling{}) {
		return nil
	}
	return &AccountBillingREST{Mode: string(b.Mode), MonthlyFeeMicros: b.MonthlyFeeMicros}
}

// UpdateAccountRESTRequest is the JSON body for PUT /api/v1/agent-accounts/{id}.
type UpdateAccountRESTRequest struct {
	DisplayName    string `json:"display_name,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	ClearProjectID bool   `json:"clear_project_id,omitempty"`
	RefreshPolicy  string `json:"refresh_policy,omitempty"`
	// Billing, when present, replaces how the account is billed; an empty
	// object clears it. Absent leaves it unchanged.
	Billing *AccountBillingREST `json:"billing,omitempty"`
	// Spend, when present, replaces the account's spend policy; an empty
	// object makes it unmetered. Absent leaves it unchanged.
	Spend *orchestration.AccountSpendPolicy `json:"spend,omitempty"`

	// CredentialSecretRef switches the account to (or re-points it at) a
	// tenant secret; see service.UpdateAccountRequest.
	CredentialSecretRef  string `json:"credential_secret_ref,omitempty"`
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`
	// MaxConcurrentSessions, when present, replaces the session cap; zero
	// makes the account unmetered. Absent leaves it unchanged.
	MaxConcurrentSessions *int `json:"max_concurrent_sessions,omitempty"`
}

// RotateAccountCredentialRESTRequest is the JSON body for
// POST /api/v1/agent-accounts/{id}/rotate.
type RotateAccountCredentialRESTRequest struct {
	Credential *AccountCredentialRESTRequest `json:"credential"`
}

// RevokeAccountRESTRequest is the JSON body for
// POST /api/v1/agent-accounts/{id}/revoke.
type RevokeAccountRESTRequest struct {
	Reason string `json:"reason,omitempty"`
}

// AccountRESTResponse mirrors orchestration.Account for JSON serialization.
// It carries metadata only — deliberately no credential field.
type AccountRESTResponse struct {
	ID            string `json:"id"`
	TenantID      string `json:"tenant_id"`
	ProjectID     string `json:"project_id,omitempty"`
	Provider      string `json:"provider"`
	DisplayName   string `json:"display_name,omitempty"`
	Default       bool   `json:"default"`
	Status        string `json:"status"`
	RefreshPolicy string `json:"refresh_policy"`

	// Billing is how the account is billed; absent when not configured.
	Billing *AccountBillingREST `json:"billing,omitempty"`
	// CredentialSecretRef / CredentialSecretFile name the tenant secret the
	// credential is read from, when the account is reference-sourced. A name,
	// never the value.
	CredentialSecretRef  string `json:"credential_secret_ref,omitempty"`
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`

	// Spend is the account's spend policy; absent when unmetered.
	Spend *orchestration.AccountSpendPolicy `json:"spend,omitempty"`

	// AvailableAt is when a "limited" account rejoins the selectable pool.
	// Zero on every other status.
	AvailableAt time.Time `json:"available_at,omitempty"`

	// CredentialExpiresAt mirrors the credential envelope's expiry. Zero means
	// UNKNOWN, not "never expires" — see orchestration.Account.
	CredentialExpiresAt time.Time `json:"credential_expires_at,omitempty"`

	// LastValidatedAt is the last time the credential was successfully loaded
	// and projected into a spawn.
	LastValidatedAt time.Time `json:"last_validated_at,omitempty"`

	// CredentialHealth is the derived verdict on CredentialExpiresAt:
	// "unknown", "valid", "expiring", or "expired".
	CredentialHealth string `json:"credential_health,omitempty"`

	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastUsedAt      time.Time `json:"last_used_at,omitempty"`
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`

	// MaxConcurrentSessions caps the account's live sessions. Zero is
	// unmetered.
	MaxConcurrentSessions int `json:"max_concurrent_sessions,omitempty"`
}

// AccountUsageRESTResponse mirrors orchestration.UsageReport.
type AccountUsageRESTResponse struct {
	TotalTokens int64            `json:"total_tokens"`
	BudgetLimit int64            `json:"budget_limit"`
	PercentUsed float64          `json:"percent_used"`
	ByAgent     map[string]int64 `json:"by_agent,omitempty"`
	LastUpdated int64            `json:"last_updated"`
}

// AccountEnvelopeRESTResponse is the single-account response envelope shared by
// create / get / update / revoke.
type AccountEnvelopeRESTResponse struct {
	Account   *AccountRESTResponse `json:"account"`
	Timestamp time.Time            `json:"timestamp"`
}

// ListAccountsRESTResponse is returned on GET /api/v1/agent-accounts. It
// carries the paging envelope every list endpoint answers with; see
// docs/software-factory/api.md.
type ListAccountsRESTResponse struct {
	Accounts []*AccountRESTResponse `json:"accounts"`

	// Total is the number of accounts the filters and search left, before
	// paging.
	Total int `json:"total"`

	// Limit and Offset are the applied paging values.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`

	// NextOffset is the offset of the following page, absent on the last.
	NextOffset int `json:"next_offset,omitempty"`

	// Facets counts the accounts by provider and status; see
	// docs/software-factory/api.md.
	Facets orchestration.Facets `json:"facets,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// AccountIDRESTResponse is the acknowledgement envelope for the verbs that
// return only the affected account ID (rotate / set-default).
type AccountIDRESTResponse struct {
	AccountID string    `json:"account_id"`
	Timestamp time.Time `json:"timestamp"`
}

// DeleteAccountRESTResponse is returned on DELETE /api/v1/agent-accounts/{id}.
type DeleteAccountRESTResponse struct {
	AccountID string    `json:"account_id"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// GetAccountUsageRESTResponse is returned on GET /api/v1/agent-accounts/{id}/usage.
type GetAccountUsageRESTResponse struct {
	Usage *AccountUsageRESTResponse `json:"usage"`

	// LiveSessions is how many sessions the account is serving now.
	LiveSessions int `json:"live_sessions"`

	// Spend is the spend gate's verdict on the account; absent when the
	// deployment wires no gate. A worker daemon fails over on it.
	Spend *orchestration.SpendHeadroom `json:"spend,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Handlers
// =============================================================================

// HandleCreateAccount handles POST /api/v1/agent-accounts.
func (s *Server) HandleCreateAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req CreateAccountRESTRequest
	if !decodeAccountBody(w, r, &req) {
		return
	}

	tenantID := req.TenantID
	if tenantID == "" {
		tenantID = r.URL.Query().Get("tenant_id")
	}
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id is required")
		return
	}
	if req.Provider == "" {
		RespondBadRequest(w, r, "provider is required")
		return
	}

	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	// Which credential source was supplied — inline, reference, both or
	// neither — is the service's rule to enforce, so every transport answers
	// it identically.
	resp, err := accountSvc.CreateAccount(ctx, &service.CreateAccountRequest{
		TenantID:              tenantID,
		ProjectID:             req.ProjectID,
		Provider:              orchestration.AccountProvider(req.Provider),
		DisplayName:           req.DisplayName,
		Default:               req.Default,
		RefreshPolicy:         orchestration.AccountRefreshPolicy(req.RefreshPolicy),
		Credential:            req.Credential.toOrchestration(),
		CredentialSecretRef:   req.CredentialSecretRef,
		CredentialSecretFile:  req.CredentialSecretFile,
		MaxConcurrentSessions: req.MaxConcurrentSessions,
		Billing:               req.Billing.toOrchestration(),
		Spend:                 derefSpendPolicy(req.Spend),
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusCreated, &AccountEnvelopeRESTResponse{
		Account:   accountToREST(resp.Account),
		Timestamp: resp.Timestamp,
	})
}

// HandleGetAccount handles GET /api/v1/agent-accounts/{id}.
func (s *Server) HandleGetAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID, tenantID, ok := accountPathParams(w, r)
	if !ok {
		return
	}
	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.GetAccount(ctx, &service.GetAccountRequest{
		TenantID:  tenantID,
		AccountID: accountID,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &AccountEnvelopeRESTResponse{
		Account:   accountToREST(resp.Account),
		Timestamp: resp.Timestamp,
	})
}

// HandleListAccounts handles GET /api/v1/agent-accounts.
func (s *Server) HandleListAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return
	}
	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	page, err := pageFromRequest(r)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	resp, err := accountSvc.ListAccounts(ctx, &service.ListAccountsRequest{
		TenantID:  tenantID,
		Provider:  orchestration.AccountProvider(r.URL.Query().Get("provider")),
		ProjectID: r.URL.Query().Get("project_id"),
		Status:    orchestration.AccountStatus(r.URL.Query().Get("status")),
		Page:      page,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	accounts := make([]*AccountRESTResponse, 0, len(resp.Accounts))
	for _, a := range resp.Accounts {
		accounts = append(accounts, accountToREST(a))
	}

	RespondJSON(w, r, http.StatusOK, &ListAccountsRESTResponse{
		Accounts:   accounts,
		Total:      resp.Total,
		Limit:      resp.Limit,
		Offset:     resp.Offset,
		NextOffset: resp.NextOffset,
		Facets:     resp.Facets,
		Timestamp:  resp.Timestamp,
	})
}

// HandleUpdateAccount handles PUT /api/v1/agent-accounts/{id}.
func (s *Server) HandleUpdateAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID, tenantID, ok := accountPathParams(w, r)
	if !ok {
		return
	}

	var req UpdateAccountRESTRequest
	if !decodeAccountBody(w, r, &req) {
		return
	}

	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.UpdateAccount(ctx, &service.UpdateAccountRequest{
		TenantID:       tenantID,
		AccountID:      accountID,
		DisplayName:    req.DisplayName,
		ProjectID:      req.ProjectID,
		ClearProjectID: req.ClearProjectID,
		RefreshPolicy:  orchestration.AccountRefreshPolicy(req.RefreshPolicy),
		Billing:        req.Billing.optional(),
		Spend:          req.Spend,

		CredentialSecretRef:   req.CredentialSecretRef,
		CredentialSecretFile:  req.CredentialSecretFile,
		MaxConcurrentSessions: req.MaxConcurrentSessions,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &AccountEnvelopeRESTResponse{
		Account:   accountToREST(resp.Account),
		Timestamp: resp.Timestamp,
	})
}

// HandleDeleteAccount handles DELETE /api/v1/agent-accounts/{id}.
func (s *Server) HandleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID, tenantID, ok := accountPathParams(w, r)
	if !ok {
		return
	}
	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.DeleteAccount(ctx, &service.DeleteAccountRequest{
		TenantID:  tenantID,
		AccountID: accountID,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &DeleteAccountRESTResponse{
		AccountID: resp.AccountID,
		Deleted:   resp.Deleted,
		Timestamp: resp.Timestamp,
	})
}

// HandleRotateAccountCredential handles POST /api/v1/agent-accounts/{id}/rotate.
func (s *Server) HandleRotateAccountCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID, tenantID, ok := accountPathParams(w, r)
	if !ok {
		return
	}

	var req RotateAccountCredentialRESTRequest
	if !decodeAccountBody(w, r, &req) {
		return
	}
	if req.Credential == nil {
		RespondBadRequest(w, r, "credential is required")
		return
	}

	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.RotateAccountCredential(ctx, &service.RotateAccountCredentialRequest{
		TenantID:   tenantID,
		AccountID:  accountID,
		Credential: req.Credential.toOrchestration(),
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &AccountIDRESTResponse{
		AccountID: resp.AccountID,
		Timestamp: resp.Timestamp,
	})
}

// HandleRevokeAccount handles POST /api/v1/agent-accounts/{id}/revoke.
func (s *Server) HandleRevokeAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID, tenantID, ok := accountPathParams(w, r)
	if !ok {
		return
	}

	// The reason is optional, so an absent or empty body is accepted.
	var req RevokeAccountRESTRequest
	if !decodeAccountBody(w, r, &req) {
		return
	}

	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.RevokeAccount(ctx, &service.RevokeAccountRequest{
		TenantID:  tenantID,
		AccountID: accountID,
		Reason:    req.Reason,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &AccountEnvelopeRESTResponse{
		Account:   accountToREST(resp.Account),
		Timestamp: resp.Timestamp,
	})
}

// HandleSetDefaultAccount handles POST /api/v1/agent-accounts/{id}/default.
func (s *Server) HandleSetDefaultAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID, tenantID, ok := accountPathParams(w, r)
	if !ok {
		return
	}
	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.SetDefaultAccount(ctx, &service.SetDefaultAccountRequest{
		TenantID:  tenantID,
		AccountID: accountID,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &AccountIDRESTResponse{
		AccountID: resp.AccountID,
		Timestamp: resp.Timestamp,
	})
}

// HandleGetAccountUsage handles GET /api/v1/agent-accounts/{id}/usage.
func (s *Server) HandleGetAccountUsage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID, tenantID, ok := accountPathParams(w, r)
	if !ok {
		return
	}
	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.GetAccountUsage(ctx, &service.GetAccountUsageRequest{
		TenantID:  tenantID,
		AccountID: accountID,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &GetAccountUsageRESTResponse{
		Usage:        accountUsageToREST(resp.Usage),
		LiveSessions: resp.LiveSessions,
		Spend:        resp.Spend,
		Timestamp:    resp.Timestamp,
	})
}

// =============================================================================
// Helpers
// =============================================================================

// accountService resolves the AccountService, responding 503 when it is not
// configured. The bool result reports whether the caller may proceed.
func (s *Server) accountService(w http.ResponseWriter, r *http.Request) (service.AccountService, bool) {
	accountSvc := s.service.Account()
	if accountSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, errors.New("account service unavailable"))
		return nil, false
	}
	return accountSvc, true
}

// accountPathParams extracts the {id} path parameter and the required
// tenant_id query parameter, responding 400 when either is missing.
func accountPathParams(w http.ResponseWriter, r *http.Request) (accountID, tenantID string, ok bool) {
	accountID = chi.URLParam(r, "id")
	if accountID == "" {
		RespondBadRequest(w, r, "account id is required")
		return "", "", false
	}
	tenantID = r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant_id query parameter is required")
		return "", "", false
	}
	return accountID, tenantID, true
}

// decodeAccountBody reads and JSON-decodes the request body into target. An
// empty body decodes to the zero value, which every account body shape treats
// as "no optional fields supplied". The bool result reports whether the caller
// may proceed.
func decodeAccountBody(w http.ResponseWriter, r *http.Request, target interface{}) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return false
	}
	defer r.Body.Close()

	if len(body) == 0 {
		return true
	}
	if err := json.Unmarshal(body, target); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return false
	}
	return true
}

// accountToREST converts an orchestration.Account to its REST representation.
// It reads metadata fields only; AccountRESTResponse has no credential field.
func accountToREST(a *orchestration.Account) *AccountRESTResponse {
	if a == nil {
		return nil
	}
	return &AccountRESTResponse{
		ID:                    a.ID,
		TenantID:              a.TenantID,
		ProjectID:             a.ProjectID,
		Provider:              string(a.Provider),
		DisplayName:           a.DisplayName,
		Default:               a.Default,
		Status:                string(a.Status),
		RefreshPolicy:         string(a.RefreshPolicy),
		CredentialSecretRef:   a.CredentialSecretRef,
		CredentialSecretFile:  a.CredentialSecretFile,
		AvailableAt:           a.AvailableAt,
		CredentialExpiresAt:   a.CredentialExpiresAt,
		LastValidatedAt:       a.LastValidatedAt,
		CredentialHealth:      string(a.CredentialHealth()),
		CreatedAt:             a.CreatedAt,
		UpdatedAt:             a.UpdatedAt,
		LastUsedAt:            a.LastUsedAt,
		LastRefreshedAt:       a.LastRefreshedAt,
		LastError:             a.LastError,
		MaxConcurrentSessions: a.MaxConcurrentSessions,
		Billing:               accountBillingToREST(a.Billing),
		Spend:                 accountSpendToREST(a.Spend),
	}
}

// accountUsageToREST converts an orchestration.UsageReport to REST form.
func accountUsageToREST(u *orchestration.UsageReport) *AccountUsageRESTResponse {
	if u == nil {
		return nil
	}
	return &AccountUsageRESTResponse{
		TotalTokens: u.TotalTokens,
		BudgetLimit: u.BudgetLimit,
		PercentUsed: u.PercentUsed,
		ByAgent:     u.ByAgent,
		LastUpdated: u.LastUpdated,
	}
}

// respondAccountError writes err at statusCode, carrying its spawn
// configuration-fault class (orchestration.SpawnFaultClass) in the error
// envelope's fault_class when it has one.
//
// The class is what a fleet node daemon maps back onto the orchestration sentinel
// (pkg/fleet AccountAPIError), so a release refused because the account's
// credential is missing is PARKED uncharged on a remote spawn exactly as it is
// on an in-process one. A status code alone cannot say that: 409 also means a
// revoked delivery grant, which is not a configuration fault.
func respondAccountError(w http.ResponseWriter, r *http.Request, statusCode int, err error) {
	errResp := newErrorResponse(r, err)
	errResp.FaultClass = orchestration.SpawnFaultClass(err)
	RespondJSON(w, r, statusCode, errResp)
}

// handleAccountError maps service-layer account errors to HTTP status codes
// and writes the envelope, fault class included (respondAccountError).
func handleAccountError(w http.ResponseWriter, r *http.Request, err error) {
	respondAccountError(w, r, accountErrorStatus(err), err)
}

// accountErrorStatus is the HTTP status a service-layer account error maps to.
func accountErrorStatus(err error) int {
	var notFound *service.AccountNotFoundError
	var invalid *service.AccountInvalidError
	var noTracker *service.AccountUsageTrackerNotConfiguredError
	var notReleasable *service.AccountNotReleasableError
	var projectScope *service.AccountProjectScopeError
	var noTokens *service.AccountTokenManagerNotConfiguredError
	var noReserver *service.AccountSessionReserverNotConfiguredError
	var leaseRevoked *lease.LeaseRevokedError
	var secretUnresolved *orchestration.AccountSecretRefUnresolvedError
	var noSecretResolver *orchestration.AccountSecretResolverNotConfiguredError
	var missingTenant *tenantctx.MissingTenantError

	switch {
	case isPageInputError(err):
		return http.StatusBadRequest
	case errors.As(err, &notFound):
		return http.StatusNotFound
	case errors.As(err, &leaseRevoked):
		// 409, for the same reason AccountNotReleasableError is: the caller is
		// entitled to this account and the account exists — it is the LEASE's
		// state that refuses, and that state is one the caller can leave by
		// taking a new delivery grant.
		//
		// It used to fall through to 500, which is what made this unrecoverable
		// in practice: a routine session teardown revokes its delivery lease,
		// the daemon kept presenting that id, and an opaque server error gave it
		// nothing to act on. Every spawn then failed until the process was
		// restarted. A distinguishable status is what lets the daemon drop the
		// stale grant and ask for a fresh one (pkg/fleet account_secrets.go).
		return http.StatusConflict
	case errors.As(err, &invalid):
		return http.StatusBadRequest
	case errors.Is(err, orchestration.ErrAccountSessionCountChanged):
		// 412: a conditional reservation lost its race; the count it was
		// conditioned on no longer holds, and the caller re-selects.
		return http.StatusPreconditionFailed
	case errors.Is(err, orchestration.ErrAccountsSaturated):
		// 409: the account is at its session cap. Busy, not broken — the
		// slot frees when a session ends — and distinguishable, so a daemon
		// rebuilds the typed refusal instead of charging a failed spawn.
		return http.StatusConflict
	case errors.As(err, &notReleasable):
		// 409, not 403: the caller is entitled to this account, and the
		// account exists — it is the account's STATE that refuses, and that
		// state changes on its own for a limit hold. A 403 would tell a
		// daemon its identity was wrong and send an operator after the grant.
		return http.StatusConflict
	case errors.As(err, &secretUnresolved):
		// 409, like a not-releasable account: the caller is entitled to the
		// account and it exists — the secret it names does not resolve, and
		// the spawn path has already put the account in "error" for an
		// operator to clear by writing the secret. The envelope's
		// fault_class carries the missing-credential class so a daemon parks
		// the work.
		return http.StatusConflict
	case errors.As(err, &noSecretResolver):
		return http.StatusServiceUnavailable
	case errors.As(err, &projectScope):
		// 403: the account is fenced to another project and no retry, wait, or
		// differently-shaped request by this caller will release it.
		return http.StatusForbidden
	case errors.Is(err, orchestration.ErrAccountTenantScope), errors.As(err, &missingTenant):
		// 403: the request names a tenant other than the caller's own, or the
		// caller carries no tenant at all. Neither is lifted for the system
		// tenant (go-quicbaas#1150).
		return http.StatusForbidden
	case errors.As(err, &noTracker), errors.As(err, &noTokens), errors.As(err, &noReserver):
		return http.StatusServiceUnavailable
	case errors.Is(err, orchestration.ErrAccountVersionConflict):
		// 409: a concurrent write changed the account under this one — a
		// rewrite that kept losing, or an inline rotation overtaken by a switch
		// to a secret reference. Nothing was written; the caller may retry.
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

// =============================================================================
// Credential materialization (POST /api/v1/agent-accounts/{id}/materialize)
//
// THE ONE ACCOUNT ROUTE WHOSE RESPONSE BODY IS SECRET MATERIAL. Everything
// above this line returns metadata, and the structural leak guards enumerate
// those shapes; this route is the deliberate, separately-permissioned
// exception that lets a pooled credential reach the worker host running the
// agent (go-quicbaas#734).
//
// Tenancy is the CALLER'S OWN and is never taken from the request. The
// tenant_id query parameter the rest of this surface requires is refused here
// when it names anything but the authenticated principal's tenant, and a
// system-tenant principal — which carries no tenant binding at all — is
// refused outright: a credential release must name the tenant that owns the
// pool.
// =============================================================================

// MaterializeAccountCredentialRESTRequest is the JSON body for
// POST /api/v1/agent-accounts/{id}/materialize. Every field is optional; an
// absent body materializes a tenant-wide account for no particular project.
type MaterializeAccountCredentialRESTRequest struct {
	// ProjectID is the project the requesting spawn belongs to. An account
	// restricted to a project is released only to a request naming it.
	ProjectID string `json:"project_id,omitempty"`

	// JobID names the job the requesting spawn works for. It scopes the
	// delivery grant this release takes out.
	JobID string `json:"job_id,omitempty"`

	// LeaseID presents the delivery grant a caller already holds, and is what
	// makes a REFRESH of a copy distinguishable from a request for a new one.
	// A grant that has been revoked — because the session that held the copy
	// ended — is refused, so the caller cannot replace what it holds.
	LeaseID string `json:"lease_id,omitempty"`
}

// AccountCredentialRESTEnvelope is the released credential envelope. It is
// the OUTBOUND credential shape and is deliberately a distinct type from
// AccountCredentialRESTRequest so the inbound-only invariant that type carries
// stays literally true and the leak guards keep meaning what they say.
type AccountCredentialRESTEnvelope struct {
	Env               map[string]string `json:"env,omitempty"`
	Files             map[string][]byte `json:"files,omitempty"`
	AccessToken       string            `json:"access_token,omitempty"`
	ExpiresAt         time.Time         `json:"expires_at,omitempty"`
	Scopes            []string          `json:"scopes,omitempty"`
	AccessTokenEnvKey string            `json:"access_token_env_key,omitempty"`

	// Generation identifies which copy of the account's credential this
	// envelope is (AccountCredential.Generation). A worker host records it on
	// the session it spawns, so the control plane can tell before a turn that
	// the pool has rotated the credential under that session
	// (go-quicbaas#997). An integer naming a copy, not credential material.
	// Zero means unknown.
	Generation int64 `json:"generation,omitempty"`
}

// MaterializeAccountCredentialRESTResponse carries one released envelope plus
// the non-secret metadata a caching caller needs.
type MaterializeAccountCredentialRESTResponse struct {
	AccountID     string `json:"account_id"`
	Provider      string `json:"provider"`
	DisplayName   string `json:"display_name,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	RefreshPolicy string `json:"refresh_policy,omitempty"`

	// Credential is the released envelope.
	Credential *AccountCredentialRESTEnvelope `json:"credential"`

	// ExpiresAt bounds how long a caller may cache the envelope.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// UpdatedAt is diagnostics: it moves on any write to the record, including
	// the usage stamp the release itself makes.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// LastRefreshedAt is when the envelope was last replaced, and is the
	// cache-invalidation signal: a caller that later reads a newer value knows
	// its copy is superseded without asking for the secret again.
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitempty"`

	// LeaseID is the delivery grant this release is authorised by. A caller
	// that caches the envelope MUST present it on every refresh of that copy:
	// it is what lets the control plane refuse the refresh once the grant has
	// been revoked.
	LeaseID string `json:"lease_id,omitempty"`

	// LeaseExpiresAt is when the delivery grant stops being authorised without
	// renewal. It is NOT the credential's expiry (see ExpiresAt) — the two are
	// deliberately independent.
	LeaseExpiresAt time.Time `json:"lease_expires_at,omitempty"`

	// SingleUse says the envelope must not be reused for a later spawn: the
	// account reads its credential from a tenant secret, which rotates with no
	// account write a cache could observe.
	SingleUse bool `json:"single_use,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// HandleMaterializeAccountCredential handles
// POST /api/v1/agent-accounts/{id}/materialize.
func (s *Server) HandleMaterializeAccountCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	accountID := chi.URLParam(r, "id")
	if accountID == "" {
		RespondBadRequest(w, r, "account id is required")
		return
	}

	// The tenant is the principal's, not the request's. A tenant_id query is
	// tolerated (the rest of this surface requires one) but only when it
	// agrees with the caller.
	subject, ok := requireTenantSubject(w, r)
	if !ok {
		return
	}
	if !queryIdentityMatches(w, r, subject, s.lazyCallerAccount(w, r, subject)) {
		return
	}

	var req MaterializeAccountCredentialRESTRequest
	if !decodeAccountBody(w, r, &req) {
		return
	}

	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.MaterializeAccountCredential(ctx, &service.MaterializeAccountCredentialRequest{
		TenantID:  subject.TenantID,
		AccountID: accountID,
		ProjectID: req.ProjectID,
		JobID:     req.JobID,
		LeaseID:   req.LeaseID,
		AgentID:   subject.ID,
	})
	if err != nil {
		handleAccountError(w, r, err)
		return
	}

	// Materialized credentials must never sit in a shared cache: the response
	// body is a live provider login.
	w.Header().Set("Cache-Control", "no-store")

	RespondJSON(w, r, http.StatusOK, &MaterializeAccountCredentialRESTResponse{
		AccountID:       resp.AccountID,
		Provider:        string(resp.Provider),
		DisplayName:     resp.DisplayName,
		ProjectID:       resp.ProjectID,
		RefreshPolicy:   string(resp.RefreshPolicy),
		Credential:      accountCredentialToREST(resp.Credential),
		ExpiresAt:       resp.ExpiresAt,
		UpdatedAt:       resp.UpdatedAt,
		LastRefreshedAt: resp.LastRefreshedAt,
		LeaseID:         resp.LeaseID,
		LeaseExpiresAt:  resp.LeaseExpiresAt,
		SingleUse:       resp.SingleUse,
		Timestamp:       resp.Timestamp,
	})
}

// accountCredentialToREST converts a released envelope to its REST form.
//
// The refresh token is DELIBERATELY NOT PROJECTED. Renewal is the central's
// job — it already happened, inside the service call that produced this
// envelope — so a daemon has no use for one, and shipping it would put the
// long-lived half of the credential on a worker host that only ever needs the
// short-lived half. Same reasoning for TokenEndpoint / ClientID: they are
// refresh-grant configuration, and a party that must not refresh must not
// receive them.
func accountCredentialToREST(c *orchestration.AccountCredential) *AccountCredentialRESTEnvelope {
	if c == nil {
		return nil
	}
	return &AccountCredentialRESTEnvelope{
		Env:               c.Env,
		Files:             c.Files,
		AccessToken:       c.AccessToken,
		ExpiresAt:         c.ExpiresAt,
		Scopes:            c.Scopes,
		AccessTokenEnvKey: c.AccessTokenEnvKey,
		Generation:        c.Generation,
	}
}
