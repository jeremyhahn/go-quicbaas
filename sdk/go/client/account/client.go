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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
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

	// Materialize RELEASES one account's credential envelope to the caller.
	//
	// It is the only method on this client whose response carries secret
	// material, and it exists so a pooled credential can reach the worker host
	// that runs the agent rather than only the host that holds the pool
	// (go-quicbaas#734). It carries its own permission
	// (agent_accounts:materialize), it is scoped to the CALLER'S OWN tenant,
	// and the server refuses an account that is revoked, errored, or under a
	// live limit hold.
	//
	// The returned envelope must never be logged or written outside a
	// caller-controlled secret store; ExpiresAt bounds how long it may be
	// cached and UpdatedAt tells a cache when a central rotation superseded it.
	Materialize(ctx context.Context, req *MaterializeCredentialRequest) (*MaterializedCredential, error)
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

	// CredentialSecretRef names the tenant secret the credential is read from,
	// when the account is reference-sourced. A name, never the value.
	CredentialSecretRef string `json:"credential_secret_ref,omitempty"`

	// CredentialSecretFile is where a referenced secret is materialized
	// (relative to the session home) instead of the access token.
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`

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
	// Credential is the initial secret envelope, held inline; never echoed
	// back. Exactly one of Credential and CredentialSecretRef is required.
	Credential *Credential `json:"credential,omitempty"`
	// CredentialSecretRef names the tenant secret the credential is read from
	// on every spawn instead (e.g. "agents/claude/claude1/oauth_token"). Write
	// the secret first (Security().Secrets()); rotating it rotates the
	// account with no account write.
	CredentialSecretRef string `json:"credential_secret_ref,omitempty"`
	// CredentialSecretFile, with CredentialSecretRef, materializes the secret
	// as this file relative to the session home (e.g. ".codex/auth.json")
	// instead of as the access token.
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`
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
	// Status filters to one lifecycle state — active, revoked, error,
	// limited — when non-empty.
	Status string `json:"status,omitempty"`

	// Params pages, orders and searches the listing. The zero value takes
	// the server's default page; see package page.
	page.Params
}

// ListAccountsResponse contains one page of the matching accounts.
type ListAccountsResponse struct {
	Accounts []*AccountInfo `json:"accounts"`

	// Envelope carries how many records matched and where the next page
	// starts; see package page.
	page.Envelope
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

	// CredentialSecretRef switches the account's credential to (or re-points
	// it at) this tenant secret. Re-applying the current one returns an
	// account whose secret was missing to service.
	CredentialSecretRef string `json:"credential_secret_ref,omitempty"`
	// CredentialSecretFile is applied together with CredentialSecretRef.
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`
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

// MaterializeCredentialRequest asks for one account's credential envelope.
type MaterializeCredentialRequest struct {
	// TenantID owns the account. Required, and it must be the caller's own
	// tenant: the server binds tenancy from the authenticated principal and
	// refuses a value naming anything else.
	TenantID string `json:"tenant_id"`

	// AccountID names the account to release. Required — this verb never
	// selects.
	AccountID string `json:"account_id"`

	// ProjectID is the project the requesting spawn belongs to. An account
	// fenced to a project is released only to a request naming it.
	ProjectID string `json:"project_id,omitempty"`

	// JobID names the job the requesting spawn works for. It scopes the
	// DELIVERY GRANT this release takes out, so a job's releases can be listed
	// and revoked as a set through Security().DynamicSecrets().
	//
	// Name it only when this release serves ONE job. An enrolled agent daemon
	// leaves it empty because its credential cache is keyed by account, so one
	// release serves every job on that host — and a later refresh presented
	// against a job-scoped grant would then be refused its own copy.
	JobID string `json:"job_id,omitempty"`

	// LeaseID presents the delivery grant a caller already holds, and is what
	// distinguishes a REFRESH of a copy from a request for a new one.
	//
	// Leave it empty for a first release. Set it — to the LeaseID the previous
	// release returned — on every subsequent release of the SAME copy, and
	// expect the call to be refused once that grant has been revoked: the
	// session that held the copy has ended and the server will not replace it.
	// A caller that has lost its grant may always ask for a first release again.
	LeaseID string `json:"lease_id,omitempty"`
}

// ReleasedCredential is the RELEASED half of a pooled credential: what a
// spawn needs to authenticate, and nothing else.
//
// It is deliberately a DISTINCT type from Credential rather than a reuse of
// it. Credential is request-only and the SDK's structural leak guard asserts
// no response reaches it; more importantly, this shape carries no refresh
// grant (no refresh token, token endpoint or client id) because renewal is the
// server's job and a recipient that must not refresh must not receive the
// means to.
type ReleasedCredential struct {
	// Env holds static environment variables to inject into the spawned
	// subprocess verbatim.
	Env map[string]string `json:"env,omitempty"`

	// Files holds provider config file bytes keyed by a path relative to the
	// materialized per-session home directory.
	Files map[string][]byte `json:"files,omitempty"`

	// AccessToken is the bearer credential handed to the spawn.
	AccessToken string `json:"access_token,omitempty"`

	// ExpiresAt is when AccessToken stops working; zero means unknown.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// Scopes records the scopes the access token was issued with.
	Scopes []string `json:"scopes,omitempty"`

	// AccessTokenEnvKey is the environment variable AccessToken is projected
	// into at materialization time.
	AccessTokenEnvKey string `json:"access_token_env_key,omitempty"`
}

// MaterializedCredential is one released envelope plus the non-secret metadata
// a caching caller needs.
type MaterializedCredential struct {
	AccountID     string        `json:"account_id"`
	Provider      Provider      `json:"provider"`
	DisplayName   string        `json:"display_name,omitempty"`
	ProjectID     string        `json:"project_id,omitempty"`
	RefreshPolicy RefreshPolicy `json:"refresh_policy,omitempty"`

	// Credential is the released envelope.
	Credential *ReleasedCredential `json:"credential"`

	// ExpiresAt bounds how long the envelope may be cached.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// UpdatedAt is the account record's last write. Diagnostics only: it moves
	// on any write, including the usage stamp the release itself makes.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// LastRefreshedAt is when the ENVELOPE was last replaced, and is the
	// cache-invalidation signal: a caller that later reads a newer value knows
	// its cached envelope is superseded without asking for the secret again.
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitempty"`

	// LeaseID is the delivery grant this release is authorised by. Empty when
	// the deployment leases no deliveries.
	//
	// A caller that caches the envelope MUST present it on every refresh of
	// that copy (MaterializeCredentialRequest.LeaseID).
	LeaseID string `json:"lease_id,omitempty"`

	// LeaseExpiresAt is when the delivery grant stops being authorised without
	// renewal. It is NOT the credential's expiry — that is ExpiresAt, and the
	// control plane's OAuth refresher owns it. The two are deliberately
	// independent: a lapsing grant never shortens a token the refresher would
	// have kept alive, and a refreshed token never extends a grant.
	LeaseExpiresAt time.Time `json:"lease_expires_at,omitempty"`

	// SingleUse says the envelope is good for the spawn that asked for it only.
	// The account reads its credential from a tenant secret, which rotates with
	// no account write — so a caller must not cache it past that spawn, or a
	// secret write would not reach the next one.
	SingleUse bool `json:"single_use,omitempty"`
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
	// Both-supplied is the server's to refuse, with the same answer on every
	// transport; neither-supplied cannot be a valid request anywhere.
	if req.Credential == nil && req.CredentialSecretRef == "" {
		return nil, errors.NewInvalidInput(op, "credential or credential_secret_ref is required")
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

// Materialize releases one account's credential envelope.
func (c *client) Materialize(ctx context.Context, req *MaterializeCredentialRequest) (*MaterializedCredential, error) {
	const op = "account.Materialize"
	if err := requireContext(ctx, op); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, errors.NewInvalidInput(op, "request is required")
	}
	if err := requireIdentifiers(ctx, op, req.TenantID, req.AccountID); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "Materialize", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.NewInternal(op, "nil response from server")
	}
	out, ok := resp.(*MaterializedCredential)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	// A release that carried no envelope is a protocol fault, not an empty
	// answer: the caller would otherwise spawn an agent with no credential and
	// discover it as an unexplained provider rejection minutes later.
	if out.Credential == nil {
		return nil, errors.NewInternal(op, "server released no credential envelope")
	}
	return out, nil
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
