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

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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
	TenantID      string                        `json:"tenant_id"`
	ProjectID     string                        `json:"project_id,omitempty"`
	Provider      string                        `json:"provider"`
	DisplayName   string                        `json:"display_name,omitempty"`
	Default       bool                          `json:"default,omitempty"`
	RefreshPolicy string                        `json:"refresh_policy,omitempty"`
	Credential    *AccountCredentialRESTRequest `json:"credential"`
}

// UpdateAccountRESTRequest is the JSON body for PUT /api/v1/agent-accounts/{id}.
type UpdateAccountRESTRequest struct {
	DisplayName    string `json:"display_name,omitempty"`
	ProjectID      string `json:"project_id,omitempty"`
	ClearProjectID bool   `json:"clear_project_id,omitempty"`
	RefreshPolicy  string `json:"refresh_policy,omitempty"`
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

// ListAccountsRESTResponse is returned on GET /api/v1/agent-accounts.
type ListAccountsRESTResponse struct {
	Accounts  []*AccountRESTResponse `json:"accounts"`
	Total     int                    `json:"total"`
	Timestamp time.Time              `json:"timestamp"`
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
	Usage     *AccountUsageRESTResponse `json:"usage"`
	Timestamp time.Time                 `json:"timestamp"`
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
	if req.Credential == nil {
		RespondBadRequest(w, r, "credential is required")
		return
	}

	accountSvc, ok := s.accountService(w, r)
	if !ok {
		return
	}

	resp, err := accountSvc.CreateAccount(ctx, &service.CreateAccountRequest{
		TenantID:      tenantID,
		ProjectID:     req.ProjectID,
		Provider:      orchestration.AccountProvider(req.Provider),
		DisplayName:   req.DisplayName,
		Default:       req.Default,
		RefreshPolicy: orchestration.AccountRefreshPolicy(req.RefreshPolicy),
		Credential:    req.Credential.toOrchestration(),
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

	resp, err := accountSvc.ListAccounts(ctx, &service.ListAccountsRequest{
		TenantID:  tenantID,
		Provider:  orchestration.AccountProvider(r.URL.Query().Get("provider")),
		ProjectID: r.URL.Query().Get("project_id"),
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
		Accounts:  accounts,
		Total:     resp.Total,
		Timestamp: resp.Timestamp,
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
		Usage:     accountUsageToREST(resp.Usage),
		Timestamp: resp.Timestamp,
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
		ID:                  a.ID,
		TenantID:            a.TenantID,
		ProjectID:           a.ProjectID,
		Provider:            string(a.Provider),
		DisplayName:         a.DisplayName,
		Default:             a.Default,
		Status:              string(a.Status),
		RefreshPolicy:       string(a.RefreshPolicy),
		AvailableAt:         a.AvailableAt,
		CredentialExpiresAt: a.CredentialExpiresAt,
		LastValidatedAt:     a.LastValidatedAt,
		CredentialHealth:    string(a.CredentialHealth()),
		CreatedAt:           a.CreatedAt,
		UpdatedAt:           a.UpdatedAt,
		LastUsedAt:          a.LastUsedAt,
		LastRefreshedAt:     a.LastRefreshedAt,
		LastError:           a.LastError,
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

// handleAccountError maps service-layer account errors to HTTP status codes.
func handleAccountError(w http.ResponseWriter, r *http.Request, err error) {
	var notFound *service.AccountNotFoundError
	var invalid *service.AccountInvalidError
	var noTracker *service.AccountUsageTrackerNotConfiguredError

	switch {
	case errors.As(err, &notFound):
		RespondError(w, r, http.StatusNotFound, err)
	case errors.As(err, &invalid):
		RespondError(w, r, http.StatusBadRequest, err)
	case errors.As(err, &noTracker):
		RespondError(w, r, http.StatusServiceUnavailable, err)
	default:
		RespondError(w, r, http.StatusInternalServerError, err)
	}
}
