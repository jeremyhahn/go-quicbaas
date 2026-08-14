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

// Package account is the SDK client for pooled provider accounts — the
// per-tenant pool of Anthropic/Claude, OpenAI/Codex, and OpenCode logins an
// agent session can be spawned against.
//
// SECURITY INVARIANT: Credential is a REQUEST-ONLY type. It is a field of
// CreateAccountRequest and RotateCredentialRequest and of no response type.
// No verb on this client returns credential material, because no transport
// ever sends any back.
package account

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// serviceName is the transport-level service identifier for account calls.
const serviceName = "quicbaas.Account"

// Client provides pooled provider account lifecycle management. Every verb is
// tenant-scoped and every transport (rest, grpc, quic, mcp, unix, embedded)
// implements the full set.
type Client interface {
	// Create provisions a new pooled account and stores its initial
	// credential envelope.
	Create(ctx context.Context, req *CreateAccountRequest) (*AccountInfo, error)

	// Get retrieves account metadata by ID.
	Get(ctx context.Context, tenantID, accountID string) (*AccountInfo, error)

	// List returns the tenant's accounts, optionally filtered by provider
	// and/or project.
	List(ctx context.Context, req *ListAccountsRequest) (*ListAccountsResponse, error)

	// Update changes an account's non-secret metadata.
	Update(ctx context.Context, req *UpdateAccountRequest) (*AccountInfo, error)

	// Delete permanently removes an account and its credential envelope.
	Delete(ctx context.Context, tenantID, accountID string) (*DeleteAccountResponse, error)

	// RotateCredential replaces an account's stored credential envelope.
	RotateCredential(ctx context.Context, req *RotateCredentialRequest) (*AccountAckResponse, error)

	// Revoke marks an account revoked, excluding it from every selection
	// strategy.
	Revoke(ctx context.Context, req *RevokeAccountRequest) (*AccountInfo, error)

	// SetDefault marks an account the tenant (or tenant+project) default for
	// its provider.
	SetDefault(ctx context.Context, tenantID, accountID string) (*AccountAckResponse, error)

	// Usage reports cumulative token usage recorded against an account.
	Usage(ctx context.Context, tenantID, accountID string) (*UsageReport, error)
}

// =============================================================================
// Vocabulary
// =============================================================================

// Provider identifies which CLI/backend a pooled account authenticates.
type Provider string

const (
	// ProviderClaude pools Anthropic/Claude Code accounts.
	ProviderClaude Provider = "claude"
	// ProviderCodex pools OpenAI/Codex accounts.
	ProviderCodex Provider = "codex"
	// ProviderOpenCode pools OpenCode provider configurations.
	ProviderOpenCode Provider = "opencode"
)

// Status is the lifecycle state of a pooled account.
type Status string

const (
	// StatusActive marks an account eligible for selection.
	StatusActive Status = "active"
	// StatusRevoked marks an account explicitly revoked by an operator.
	StatusRevoked Status = "revoked"
	// StatusError marks an account whose last credential refresh failed.
	StatusError Status = "error"
	// StatusLimited marks an account the provider refused for rate, quota, or
	// usage reasons. Unlike the other non-active states it heals itself:
	// AccountInfo.AvailableAt names the moment it rejoins the pool.
	StatusLimited Status = "limited"
)

// CredentialHealth is the derived verdict on how much life an account's
// credential has left. It is computed by the server from the account's
// recorded expiry; no credential material is involved.
type CredentialHealth string

const (
	// CredentialHealthUnknown means no expiry was ever recorded for the
	// account. For a static token that is the honest answer, not a problem.
	CredentialHealthUnknown CredentialHealth = "unknown"
	// CredentialHealthValid means the credential is not close to expiring.
	CredentialHealthValid CredentialHealth = "valid"
	// CredentialHealthExpiring means the credential expires soon and, for a
	// static token, needs an out-of-band renewal before spawns start failing.
	CredentialHealthExpiring CredentialHealth = "expiring"
	// CredentialHealthExpired means the recorded expiry has passed.
	CredentialHealthExpired CredentialHealth = "expired"
)

// RefreshPolicy selects how a pooled account's credential is kept usable.
type RefreshPolicy string

const (
	// RefreshNone marks a credential that never needs refreshing.
	RefreshNone RefreshPolicy = "none"
	// RefreshOAuth2 marks a credential renewed via an RFC 6749 refresh grant.
	RefreshOAuth2 RefreshPolicy = "oauth2"
)

// =============================================================================
// Types
// =============================================================================

// Credential is the secret envelope for one account. REQUEST-ONLY: it is
// accepted by Create and RotateCredential and is never returned.
type Credential struct {
	// Env holds static environment variables injected into the spawned
	// subprocess verbatim.
	Env map[string]string `json:"env,omitempty"`
	// Files holds provider config file bytes keyed by a path relative to the
	// materialized per-session home directory.
	Files map[string][]byte `json:"files,omitempty"`
	// AccessToken is the current OAuth2 access token.
	AccessToken string `json:"access_token,omitempty"`
	// RefreshToken is the OAuth2 refresh token.
	RefreshToken string `json:"refresh_token,omitempty"`
	// ExpiresAt is when AccessToken stops being valid.
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	// TokenEndpoint is the RFC 6749 endpoint used to refresh AccessToken.
	TokenEndpoint string `json:"token_endpoint,omitempty"`
	// ClientID is the OAuth2 client identifier.
	ClientID string `json:"client_id,omitempty"`
	// Scopes are the OAuth2 scopes bound to the credential.
	Scopes []string `json:"scopes,omitempty"`
	// AccessTokenEnvKey is the environment variable AccessToken is projected
	// into at session materialization time.
	AccessTokenEnvKey string `json:"access_token_env_key,omitempty"`
}

// AccountInfo is the non-secret metadata record for one pooled account.
type AccountInfo struct {
	ID            string        `json:"id"`
	TenantID      string        `json:"tenant_id"`
	ProjectID     string        `json:"project_id,omitempty"`
	Provider      Provider      `json:"provider"`
	DisplayName   string        `json:"display_name,omitempty"`
	Default       bool          `json:"default"`
	Status        Status        `json:"status"`
	RefreshPolicy RefreshPolicy `json:"refresh_policy"`

	// AvailableAt is when a StatusLimited account rejoins the selectable pool.
	// Zero on every other status.
	AvailableAt time.Time `json:"available_at,omitempty"`

	// CredentialExpiresAt is when the stored credential stops working. Zero
	// means UNKNOWN, not "never expires".
	CredentialExpiresAt time.Time `json:"credential_expires_at,omitempty"`

	// LastValidatedAt is the last time the credential was successfully loaded
	// and projected into a spawn.
	LastValidatedAt time.Time `json:"last_validated_at,omitempty"`

	// CredentialHealth is the server's derived verdict on CredentialExpiresAt.
	CredentialHealth CredentialHealth `json:"credential_health,omitempty"`

	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastUsedAt      time.Time `json:"last_used_at,omitempty"`
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
}

// UsageReport is the cumulative token usage recorded against an account.
type UsageReport struct {
	TotalTokens int64            `json:"total_tokens"`
	BudgetLimit int64            `json:"budget_limit"`
	PercentUsed float64          `json:"percent_used"`
	ByAgent     map[string]int64 `json:"by_agent,omitempty"`
	LastUpdated int64            `json:"last_updated"`
}

// CreateAccountRequest provisions a new pooled account.
type CreateAccountRequest struct {
	// TenantID owns the account. Required.
	TenantID string `json:"tenant_id"`
	// ProjectID optionally scopes the account to one project.
	ProjectID string `json:"project_id,omitempty"`
	// Provider identifies the backend that consumes the credential. Required.
	Provider Provider `json:"provider"`
	// DisplayName is an operator-chosen label.
	DisplayName string `json:"display_name,omitempty"`
	// Default marks the account as the tenant+provider fallback.
	Default bool `json:"default,omitempty"`
	// RefreshPolicy selects how the credential is renewed.
	RefreshPolicy RefreshPolicy `json:"refresh_policy,omitempty"`
	// Credential is the initial secret envelope. Required; never echoed back.
	Credential *Credential `json:"credential"`
}

// GetAccountRequest identifies an account for transport dispatch.
type GetAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// ListAccountsRequest selects which accounts to list.
type ListAccountsRequest struct {
	// TenantID whose accounts to list. Required.
	TenantID string `json:"tenant_id"`
	// Provider filters to one provider when non-empty.
	Provider Provider `json:"provider,omitempty"`
	// ProjectID filters to one project when non-empty.
	ProjectID string `json:"project_id,omitempty"`
}

// ListAccountsResponse contains the matching accounts.
type ListAccountsResponse struct {
	Accounts []*AccountInfo `json:"accounts"`
	Total    int            `json:"total"`
}

// UpdateAccountRequest changes non-secret metadata. Empty string fields are
// left unchanged.
type UpdateAccountRequest struct {
	TenantID       string        `json:"tenant_id"`
	AccountID      string        `json:"account_id"`
	DisplayName    string        `json:"display_name,omitempty"`
	ProjectID      string        `json:"project_id,omitempty"`
	ClearProjectID bool          `json:"clear_project_id,omitempty"`
	RefreshPolicy  RefreshPolicy `json:"refresh_policy,omitempty"`
}

// DeleteAccountRequest identifies an account to delete.
type DeleteAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// DeleteAccountResponse confirms deletion.
type DeleteAccountResponse struct {
	AccountID string `json:"account_id"`
	Deleted   bool   `json:"deleted"`
}

// RotateCredentialRequest supplies a replacement credential envelope.
type RotateCredentialRequest struct {
	TenantID   string      `json:"tenant_id"`
	AccountID  string      `json:"account_id"`
	Credential *Credential `json:"credential"`
}

// RevokeAccountRequest identifies an account to revoke.
type RevokeAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
	Reason    string `json:"reason,omitempty"`
}

// SetDefaultAccountRequest identifies an account to mark default.
type SetDefaultAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// GetAccountUsageRequest identifies an account whose usage to report.
type GetAccountUsageRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// AccountAckResponse is the acknowledgement shape for the verbs that return
// only the affected account ID.
type AccountAckResponse struct {
	AccountID string `json:"account_id"`
}

// =============================================================================
// Implementation
// =============================================================================

// client implements the Client interface over any transport.
type client struct {
	transport transport.Transport
}

// NewClient creates a new pooled provider account client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{transport: t}
}

// Create provisions a new pooled account.
func (c *client) Create(ctx context.Context, req *CreateAccountRequest) (*AccountInfo, error) {
	const op = "account.Create"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput(op, "tenant ID is required")
	}
	if req.Provider == "" {
		return nil, errors.NewInvalidInput(op, "provider is required")
	}
	if req.Credential == nil {
		return nil, errors.NewInvalidInput(op, "credential is required")
	}
	return c.accountInfo(ctx, op, "Create", req)
}

// Get retrieves account metadata by ID.
func (c *client) Get(ctx context.Context, tenantID, accountID string) (*AccountInfo, error) {
	const op = "account.Get"
	if err := requireIdentifiers(ctx, op, tenantID, accountID); err != nil {
		return nil, err
	}
	return c.accountInfo(ctx, op, "Get", &GetAccountRequest{TenantID: tenantID, AccountID: accountID})
}

// List returns the tenant's accounts.
func (c *client) List(ctx context.Context, req *ListAccountsRequest) (*ListAccountsResponse, error) {
	const op = "account.List"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if req.TenantID == "" {
		return nil, errors.NewInvalidInput(op, "tenant ID is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "List", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &ListAccountsResponse{Accounts: []*AccountInfo{}}, nil
	}
	list, ok := resp.(*ListAccountsResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return list, nil
}

// Update changes an account's non-secret metadata.
func (c *client) Update(ctx context.Context, req *UpdateAccountRequest) (*AccountInfo, error) {
	const op = "account.Update"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := requireIdentifiers(ctx, op, req.TenantID, req.AccountID); err != nil {
		return nil, err
	}
	return c.accountInfo(ctx, op, "Update", req)
}

// Delete permanently removes an account and its credential envelope.
func (c *client) Delete(ctx context.Context, tenantID, accountID string) (*DeleteAccountResponse, error) {
	const op = "account.Delete"
	if err := requireIdentifiers(ctx, op, tenantID, accountID); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Delete",
		&DeleteAccountRequest{TenantID: tenantID, AccountID: accountID})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &DeleteAccountResponse{AccountID: accountID, Deleted: true}, nil
	}
	out, ok := resp.(*DeleteAccountResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}

// RotateCredential replaces an account's stored credential envelope.
func (c *client) RotateCredential(ctx context.Context, req *RotateCredentialRequest) (*AccountAckResponse, error) {
	const op = "account.RotateCredential"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := requireIdentifiers(ctx, op, req.TenantID, req.AccountID); err != nil {
		return nil, err
	}
	if req.Credential == nil {
		return nil, errors.NewInvalidInput(op, "credential is required")
	}
	return c.accountAck(ctx, op, "RotateCredential", req)
}

// Revoke marks an account revoked.
func (c *client) Revoke(ctx context.Context, req *RevokeAccountRequest) (*AccountInfo, error) {
	const op = "account.Revoke"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := requireIdentifiers(ctx, op, req.TenantID, req.AccountID); err != nil {
		return nil, err
	}
	return c.accountInfo(ctx, op, "Revoke", req)
}

// SetDefault marks an account the default for its provider.
func (c *client) SetDefault(ctx context.Context, tenantID, accountID string) (*AccountAckResponse, error) {
	const op = "account.SetDefault"
	if err := requireIdentifiers(ctx, op, tenantID, accountID); err != nil {
		return nil, err
	}
	return c.accountAck(ctx, op, "SetDefault",
		&SetDefaultAccountRequest{TenantID: tenantID, AccountID: accountID})
}

// Usage reports cumulative token usage recorded against an account.
func (c *client) Usage(ctx context.Context, tenantID, accountID string) (*UsageReport, error) {
	const op = "account.Usage"
	if err := requireIdentifiers(ctx, op, tenantID, accountID); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Usage",
		&GetAccountUsageRequest{TenantID: tenantID, AccountID: accountID})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal(op, "nil response from server")
	}
	report, ok := resp.(*UsageReport)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return report, nil
}

// =============================================================================
// Shared plumbing
// =============================================================================

// accountInfo issues a request whose response is a single AccountInfo.
func (c *client) accountInfo(ctx context.Context, op, method string, req interface{}) (*AccountInfo, error) {
	resp, err := c.transport.Request(ctx, serviceName, method, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewNotFound(op, "account not found")
	}
	info, ok := resp.(*AccountInfo)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return info, nil
}

// accountAck issues a request whose response is an AccountAckResponse.
func (c *client) accountAck(ctx context.Context, op, method string, req interface{}) (*AccountAckResponse, error) {
	resp, err := c.transport.Request(ctx, serviceName, method, req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal(op, "nil response from server")
	}
	ack, ok := resp.(*AccountAckResponse)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return ack, nil
}

// requireContext rejects a nil context, matching every other SDK client.
func requireContext(ctx context.Context, op string) error {
	if ctx == nil {
		return errors.NewInvalidInput(op, "context is required")
	}
	return nil
}

// requireIdentifiers validates the context plus the tenant/account pair every
// item-scoped verb needs.
func requireIdentifiers(ctx context.Context, op, tenantID, accountID string) error {
	if err := requireContext(ctx, op); err != nil {
		return err
	}
	if tenantID == "" {
		return errors.NewInvalidInput(op, "tenant ID is required")
	}
	if accountID == "" {
		return errors.NewInvalidInput(op, "account ID is required")
	}
	return nil
}
