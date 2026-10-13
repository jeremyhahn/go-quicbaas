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

package service

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// AccountService — Interface
//
// AccountService is the API-facing boundary for the pooled provider account
// feature (see pkg/orchestration/account_types.go). It never touches
// credential material directly: CreateAccount accepts it once, forwards it to
// orchestration.AccountSecretStore, and every other method operates purely on
// metadata.
//
// EXACTLY ONE method returns an AccountCredential:
// MaterializeAccountCredential, which releases one envelope to an enrolled
// fleet node daemon so a pooled credential can reach the worker host that runs the
// agent. It carries its own permission, its own refusal rules and its own
// audit event; see account_materialize.go. Every other method still returns
// metadata only, and the transports' structural leak guards enumerate them.
// =============================================================================

// AccountService manages the lifecycle of pooled provider accounts.
type AccountService interface {
	// CreateAccount provisions a new pooled account and stores its initial
	// credential. Returns AccountInvalidError if required fields are missing.
	CreateAccount(ctx context.Context, req *CreateAccountRequest) (*CreateAccountResponse, error)

	// GetAccount retrieves account metadata by ID. Never returns credential
	// material.
	GetAccount(ctx context.Context, req *GetAccountRequest) (*GetAccountResponse, error)

	// ListAccounts returns every account for the tenant, optionally filtered
	// to one provider and/or one project.
	ListAccounts(ctx context.Context, req *ListAccountsRequest) (*ListAccountsResponse, error)

	// UpdateAccount changes an account's non-secret metadata (display name,
	// project scope, status).
	UpdateAccount(ctx context.Context, req *UpdateAccountRequest) (*UpdateAccountResponse, error)

	// RotateAccountCredential replaces an account's stored credential
	// envelope (e.g. an operator re-running `claude setup-token` and
	// supplying the new value). Metadata (DisplayName, Default, ...) is
	// unchanged.
	RotateAccountCredential(ctx context.Context, req *RotateAccountCredentialRequest) (*RotateAccountCredentialResponse, error)

	// RevokeAccount marks an account AccountStatusRevoked. A revoked account
	// is immediately excluded from every selection strategy; sessions
	// already spawned with its credential are unaffected (see
	// docs/orchestration/accounts.md "Lifecycle").
	RevokeAccount(ctx context.Context, req *RevokeAccountRequest) (*RevokeAccountResponse, error)

	// DeleteAccount permanently removes an account's metadata AND credential
	// envelope.
	DeleteAccount(ctx context.Context, req *DeleteAccountRequest) (*DeleteAccountResponse, error)

	// SetDefaultAccount marks accountID as the tenant/project+provider
	// default StaticAccountStrategy falls back to when no account is pinned.
	SetDefaultAccount(ctx context.Context, req *SetDefaultAccountRequest) (*SetDefaultAccountResponse, error)

	// GetAccountUsage returns the cumulative token usage recorded against an
	// account.
	GetAccountUsage(ctx context.Context, req *GetAccountUsageRequest) (*GetAccountUsageResponse, error)

	// MaterializeAccountCredential releases ONE account's credential envelope
	// to the caller. It is the single exception to the no-credential-out rule
	// stated above, and it exists so a pooled credential can reach the worker
	// host that runs the agent rather than only the host that holds the pool.
	//
	// It refuses an account that is revoked, errored, or under a live limit
	// hold, honours the account's project restriction, renews an oauth_refresh
	// credential before releasing it, and records one audit event per attempt.
	// See account_materialize.go.
	MaterializeAccountCredential(ctx context.Context, req *MaterializeAccountCredentialRequest) (*MaterializeAccountCredentialResponse, error)

	// ReserveAccountSession claims one session slot on a capped account in
	// the control plane's reservation table, refusing with
	// orchestration.AccountsSaturatedError when the account is at its cap.
	// See account_reservation.go.
	ReserveAccountSession(ctx context.Context, req *ReserveAccountSessionRequest) (*ReserveAccountSessionResponse, error)

	// ReleaseAccountSession gives a reservation back, for a spawn that failed.
	ReleaseAccountSession(ctx context.Context, req *ReleaseAccountSessionRequest) (*ReleaseAccountSessionResponse, error)
}

// =============================================================================
// Request / Response types
// =============================================================================

// CreateAccountRequest provisions a new pooled account.
type CreateAccountRequest struct {
	TenantID      string                             `json:"tenant_id"`
	ProjectID     string                             `json:"project_id,omitempty"`
	Provider      orchestration.AccountProvider      `json:"provider"`
	DisplayName   string                             `json:"display_name,omitempty"`
	Default       bool                               `json:"default,omitempty"`
	RefreshPolicy orchestration.AccountRefreshPolicy `json:"refresh_policy,omitempty"`

	// MaxConcurrentSessions caps how many live sessions the account serves at
	// once. Zero is unmetered.
	MaxConcurrentSessions int `json:"max_concurrent_sessions,omitempty"`

	// Billing is how the account is billed — a flat subscription or a
	// metered API key — which decides the billed figure every turn on it
	// carries. The zero value is "not configured" and bills unknown.
	Billing orchestration.AccountBilling `json:"billing,omitzero"`

	// Spend is what the account may spend — sliding windows in tokens and/or
	// list-price dollars — and how many failures take it out of service
	// (go-quicbaas#781). The zero value is unmetered.
	Spend orchestration.AccountSpendPolicy `json:"spend,omitzero"`

	// Credential is the account's initial secret envelope, held inline. Never
	// echoed back by any AccountService method. Exactly one of Credential and
	// CredentialSecretRef is required.
	Credential *orchestration.AccountCredential `json:"credential,omitempty"`

	// CredentialSecretRef names the tenant secret the credential is read from
	// on every resolution instead (e.g. "agents/claude/claude1/oauth_token"),
	// in TenantID's keyspace. It must resolve when the account is created.
	CredentialSecretRef string `json:"credential_secret_ref,omitempty"`

	// CredentialSecretFile materializes the referenced secret as this file
	// (relative to the session home) rather than as the access token. Only
	// with CredentialSecretRef.
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`
}

// CreateAccountResponse confirms account creation. Account never carries
// credential material.
type CreateAccountResponse struct {
	Account   *orchestration.Account `json:"account"`
	Timestamp time.Time              `json:"timestamp"`
}

// GetAccountRequest identifies an account by ID within a tenant.
type GetAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// GetAccountResponse wraps the retrieved account metadata.
type GetAccountResponse struct {
	Account   *orchestration.Account `json:"account"`
	Timestamp time.Time              `json:"timestamp"`
}

// ListAccountsRequest specifies which tenant's accounts to list, optionally
// filtered.
type ListAccountsRequest struct {
	TenantID  string                        `json:"tenant_id"`
	Provider  orchestration.AccountProvider `json:"provider,omitempty"`
	ProjectID string                        `json:"project_id,omitempty"`

	// Status narrows the listing to one lifecycle state — active, revoked,
	// error, limited. Empty lists every state.
	Status orchestration.AccountStatus `json:"status,omitempty"`

	// Page narrows the answer to one page of the matching accounts and
	// orders it per orchestration.AccountPageSpec. Nil asks for the first
	// orchestration.DefaultPageLimit, newest first.
	Page *orchestration.Page `json:"page,omitempty"`
}

// ListAccountsResponse contains one page of the matching accounts.
type ListAccountsResponse struct {
	Accounts []*orchestration.Account `json:"accounts"`

	// Total is the number of accounts the filters and search left, BEFORE
	// the page was cut.
	Total int `json:"total"`

	// Limit and Offset are the applied paging values.
	Limit  int `json:"limit"`
	Offset int `json:"offset"`

	// NextOffset is the offset of the following page, absent on the last.
	NextOffset int `json:"next_offset,omitempty"`

	// Facets counts the accounts by provider and status over the set the
	// search left, before those two narrowings. Absent when no page was
	// asked for.
	Facets orchestration.Facets `json:"facets,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// UpdateAccountRequest specifies which metadata fields to change. Empty
// string fields are left unchanged.
type UpdateAccountRequest struct {
	TenantID       string                             `json:"tenant_id"`
	AccountID      string                             `json:"account_id"`
	DisplayName    string                             `json:"display_name,omitempty"`
	ProjectID      string                             `json:"project_id,omitempty"`
	ClearProjectID bool                               `json:"clear_project_id,omitempty"`
	RefreshPolicy  orchestration.AccountRefreshPolicy `json:"refresh_policy,omitempty"`

	// Billing, when present, replaces how the account is billed; a present
	// zero value clears it back to "not configured". Absent (nil) leaves it
	// unchanged.
	Billing *orchestration.AccountBilling `json:"billing,omitempty"`

	// Spend, when present, replaces the account's spend policy; a present
	// zero value makes it unmetered. Absent (nil) leaves it unchanged.
	Spend *orchestration.AccountSpendPolicy `json:"spend,omitempty"`
	// CredentialSecretRef, when set, points the account's credential at this
	// tenant secret: an inline account SWITCHES to the reference (its inline
	// envelope is destroyed), a reference account is re-pointed. The secret
	// must resolve. Re-applying an account's current reference is also how an
	// operator returns an account the spawn path put in "error" (because the
	// secret was missing) to service once the secret is written.
	CredentialSecretRef string `json:"credential_secret_ref,omitempty"`

	// CredentialSecretFile is applied together with CredentialSecretRef; see
	// CreateAccountRequest.CredentialSecretFile. An update that sets a
	// reference without it projects the secret as the access token.
	CredentialSecretFile string `json:"credential_secret_file,omitempty"`
	// MaxConcurrentSessions, when non-nil, replaces the account's session cap;
	// zero makes it unmetered. Nil leaves it unchanged, which is why it is a
	// pointer: zero is a value an operator sets on purpose.
	MaxConcurrentSessions *int `json:"max_concurrent_sessions,omitempty"`
}

// UpdateAccountResponse wraps the updated account.
type UpdateAccountResponse struct {
	Account   *orchestration.Account `json:"account"`
	Timestamp time.Time              `json:"timestamp"`
}

// RotateAccountCredentialRequest supplies a replacement credential envelope.
type RotateAccountCredentialRequest struct {
	TenantID   string                           `json:"tenant_id"`
	AccountID  string                           `json:"account_id"`
	Credential *orchestration.AccountCredential `json:"credential,omitempty"`
}

// RotateAccountCredentialResponse confirms rotation.
type RotateAccountCredentialResponse struct {
	AccountID string    `json:"account_id"`
	Timestamp time.Time `json:"timestamp"`
}

// RevokeAccountRequest identifies the account to revoke.
type RevokeAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
	Reason    string `json:"reason,omitempty"`
}

// RevokeAccountResponse confirms revocation.
type RevokeAccountResponse struct {
	Account   *orchestration.Account `json:"account"`
	Timestamp time.Time              `json:"timestamp"`
}

// DeleteAccountRequest identifies the account to delete.
type DeleteAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// DeleteAccountResponse confirms deletion.
type DeleteAccountResponse struct {
	AccountID string    `json:"account_id"`
	Deleted   bool      `json:"deleted"`
	Timestamp time.Time `json:"timestamp"`
}

// SetDefaultAccountRequest identifies the account to mark default.
type SetDefaultAccountRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// SetDefaultAccountResponse confirms the change.
type SetDefaultAccountResponse struct {
	AccountID string    `json:"account_id"`
	Timestamp time.Time `json:"timestamp"`
}

// GetAccountUsageRequest identifies the account whose usage to report.
type GetAccountUsageRequest struct {
	TenantID  string `json:"tenant_id"`
	AccountID string `json:"account_id"`
}

// GetAccountUsageResponse wraps the usage report.
type GetAccountUsageResponse struct {
	Usage *orchestration.UsageReport `json:"usage"`

	// LiveSessions is how many sessions the account is serving now: the
	// figure balancing ranks on and the account's session cap is checked
	// against. A worker daemon reads it from here to balance on the same
	// count the control plane does.
	LiveSessions int `json:"live_sessions"`

	// Spend is the spend gate's read-only verdict on the account: whether its
	// own spend policy admits more work and, if not, why and until when
	// (go-quicbaas#1058, ruling F6). A worker daemon reads it here so its
	// resolver fails over past a spent account exactly as the control plane's
	// does. Nil when the deployment wires no spend gate: nothing is metered.
	Spend *orchestration.SpendHeadroom `json:"spend,omitempty"`

	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Typed Errors
// =============================================================================

// AccountInvalidError indicates an account request failed validation.
type AccountInvalidError struct {
	Field   string
	Message string

	// Cause is the underlying domain error, when this error was produced by
	// translating one (see convertAccountStoreError). It keeps errors.Is
	// against the orchestration sentinels working through the service
	// boundary while transports keep matching on this one type to choose a
	// 400.
	Cause error
}

// Error implements the error interface.
func (e *AccountInvalidError) Error() string {
	if e.Field != "" {
		return "account invalid: " + e.Field + ": " + e.Message
	}
	return "account invalid: " + e.Message
}

// Unwrap returns the underlying domain error, if any.
func (e *AccountInvalidError) Unwrap() error { return e.Cause }

// AccountNotFoundError is returned when an account cannot be located.
type AccountNotFoundError struct {
	TenantID  string
	AccountID string
}

// Error implements the error interface.
func (e *AccountNotFoundError) Error() string {
	return "account not found: tenant=" + e.TenantID + " id=" + e.AccountID
}

// AccountCredentialCleanupError reports that an account's metadata settled —
// its create was rolled back, or its record was deleted — but destroying its
// credential envelope failed, so the envelope is orphaned in the secrets store
// until an operator removes it. Cause is the error that triggered the cleanup
// (nil for a delete, whose metadata removal succeeded); Cleanup is the
// failure destroying the envelope.
type AccountCredentialCleanupError struct {
	TenantID  string
	AccountID string
	Cause     error
	Cleanup   error
}

// Error implements the error interface.
func (e *AccountCredentialCleanupError) Error() string {
	msg := "account credential cleanup failed: tenant=" + e.TenantID + " id=" + e.AccountID +
		": envelope orphaned: " + e.Cleanup.Error()
	if e.Cause != nil {
		msg += " (after: " + e.Cause.Error() + ")"
	}
	return msg
}

// Unwrap exposes both the triggering error and the cleanup failure, so a
// transport keeps mapping the triggering error's type to its status code.
func (e *AccountCredentialCleanupError) Unwrap() []error {
	if e.Cause == nil {
		return []error{e.Cleanup}
	}
	return []error{e.Cause, e.Cleanup}
}

// AccountRotationIncompleteError reports that a replacement credential
// envelope was stored but the account record could not be updated to reflect
// it (expiry, status, last-refresh). The stored envelope is the live one;
// re-running the rotation is safe and completes the record.
type AccountRotationIncompleteError struct {
	TenantID  string
	AccountID string
	Cause     error
}

// Error implements the error interface.
func (e *AccountRotationIncompleteError) Error() string {
	return "account rotation incomplete: tenant=" + e.TenantID + " id=" + e.AccountID +
		": credential stored but record not updated: " + e.Cause.Error()
}

// Unwrap returns the record update failure.
func (e *AccountRotationIncompleteError) Unwrap() error { return e.Cause }

// accountTenantContext scopes ctx to the tenant an account request names.
//
// The orchestration KV routes every read and write by the tenant on the
// CONTEXT, never by the tenant segment in the key
// (pkg/server/tenant_kvstore.go tenantFromAnyContext), and a
// prefix-isolation tenant's records live in that tenant's own namespace
// (go-qrdb tenant.StoreResolver). A request that names tenant T while its
// context carries the caller's tenant — the system admin provisioning T's
// pooled account, the shape go-quicbaas#535 measured — would otherwise write
// T's record into the CALLER's namespace, where T's SDLC module, which
// resolves under T's context (sdlc.runtimeContext), can never find it.
//
// Both keys the router honours are set: pkg/iam's, which the REST path stamps
// and the secrets manager reads, and pkg/orchestration's, which the module's
// spawn path stamps. Setting one and not the other is the silent failure
// go-quicbaas#531 documented — a value nothing downstream reads. The request's
// tenant overrides whatever the context carried: the request is the
// authority, and a caller allowed to name another tenant was admitted by RBAC
// before reaching this service.
func accountTenantContext(ctx context.Context, tenantID string) context.Context {
	return orchestration.ContextWithTenant(tenantctx.WithTenant(ctx, tenantID), tenantID)
}

// =============================================================================
// accountServiceImpl — concrete implementation
// =============================================================================

// accountServiceImpl implements AccountService using an
// orchestration.AccountStore for metadata and an
// orchestration.AccountSecretStore for credential material. It never lets a
// credential envelope reach a response type — see the CreateAccount /
// UpdateAccount bodies below, which construct orchestration.Account values
// directly rather than round-tripping through anything that could carry
// Credential.
type accountServiceImpl struct {
	store     orchestration.AccountStore
	secrets   *orchestration.AccountSecretStore
	usage     orchestration.CostTracker              // optional: see SetUsageTracker
	providers *orchestration.AccountProviderRegistry // never nil: see NewAccountService

	// tokens renews and resolves a credential envelope for release. Optional:
	// MaterializeAccountCredential is the only method that needs it and fails
	// closed without it (AccountTokenManagerNotConfiguredError). See
	// SetTokenManager.
	tokens *orchestration.AccountTokenManager

	// secretRefs reads the tenant secret a reference-sourced account names,
	// so a create or update can refuse a reference that does not resolve.
	// Optional; a reference write without it fails closed with
	// orchestration.AccountSecretResolverNotConfiguredError. See
	// SetSecretResolver.
	secretRefs orchestration.AccountSecretResolver

	// auditor records each credential release. Optional; nil disables the
	// record. See SetAuditor.
	auditor orchestration.StoreAuditor

	// logger reports a non-fatal bookkeeping failure that must not fail the
	// operation in flight but must not vanish either. Optional.
	logger interfaces.Logger

	// spend is the cap a credential release is checked against. Optional
	// for the same reason tokens is — it is built later in bring-up than
	// this service — and an unwired one leaves the release guarded only by
	// the account's lifecycle state. See SetSpendAuthority.
	spend orchestration.SpendAuthority

	// leases governs the DELIVERY of a released envelope: the verb issues a
	// grant on a first release and refuses a re-release whose grant has been
	// revoked or has lapsed. Optional for the same reason spend is — it is
	// built over the lease manager during bring-up — and an unwired one leaves
	// releases unleased, which is the behaviour every deployment had before
	// delivery grants existed. See SetAccountCredentialLeaser.
	leases orchestration.AccountCredentialLeaser

	// sessions answers GetAccountUsage's live-session count and is the
	// reservation table the reserve/release verbs write. Optional for the
	// reason usage is; see SetSessionReserver.
	sessions orchestration.AccountSessionReserver
}

// NewAccountService creates a new AccountService backed by store and
// secrets. Usage queries (GetAccountUsage) return
// AccountUsageTrackerNotConfiguredError until SetUsageTracker is called —
// orchestration's CostTracker is wired up later in server bring-up than the
// account store in some deployments, so this is a settable dependency rather
// than a required constructor argument.
//
// Credential validation starts out against the built-in provider descriptors;
// server bring-up replaces them with the configuration-merged registry via
// SetProviderRegistry so operator-defined providers and overrides are enforced
// at write time too.
func NewAccountService(store orchestration.AccountStore, secrets *orchestration.AccountSecretStore) AccountService {
	return &accountServiceImpl{
		store:     store,
		secrets:   secrets,
		providers: orchestration.DefaultAccountProviderRegistry(),
	}
}

// SetUsageTracker wires the CostTracker GetAccountUsage reports from.
func (s *accountServiceImpl) SetUsageTracker(tracker orchestration.CostTracker) {
	s.usage = tracker
}

// SetSessionReserver wires the live-session count GetAccountUsage reports and
// the reservation table ReserveAccountSession writes: the SAME table the
// control plane's resolver, balancing strategy and admission read, so a worker
// daemon balances on, and reserves against, one count.
func (s *accountServiceImpl) SetSessionReserver(sessions orchestration.AccountSessionReserver) {
	s.sessions = sessions
}

// SetLogger wires the logger non-fatal bookkeeping failures are reported on.
// A nil logger is ignored, leaving those reports discarded.
func (s *accountServiceImpl) SetLogger(logger interfaces.Logger) {
	if logger != nil {
		s.logger = logger
	}
}

// SetProviderRegistry wires the provider registry write-time credential
// validation resolves effective OAuth defaults from. A nil registry is
// ignored, leaving the built-in-only default in place.
func (s *accountServiceImpl) SetProviderRegistry(registry *orchestration.AccountProviderRegistry) {
	if registry != nil {
		s.providers = registry
	}
}

// SetSecretResolver wires the resolver a credential_secret_ref is checked
// against at write time. A nil resolver is ignored.
func (s *accountServiceImpl) SetSecretResolver(resolver orchestration.AccountSecretResolver) {
	if resolver != nil {
		s.secretRefs = resolver
	}
}

// CreateAccount validates, persists, and stores the credential for a new
// account.
func (s *accountServiceImpl) CreateAccount(ctx context.Context, req *CreateAccountRequest) (*CreateAccountResponse, error) {
	if err := s.validateCreate(req); err != nil {
		return nil, err
	}
	ctx = accountTenantContext(ctx, req.TenantID)

	account := &orchestration.Account{
		ID:                   uuid.New().String(),
		TenantID:             req.TenantID,
		ProjectID:            req.ProjectID,
		Provider:             req.Provider,
		DisplayName:          req.DisplayName,
		Default:              req.Default,
		Status:               orchestration.AccountStatusActive,
		RefreshPolicy:        effectiveRefreshPolicy(req.RefreshPolicy),
		CredentialSecretRef:  req.CredentialSecretRef,
		CredentialSecretFile: req.CredentialSecretFile,
		// The operator's statement of how many sessions the subscription
		// serves at once. Zero is unmetered; the store rejects a negative.
		MaxConcurrentSessions: req.MaxConcurrentSessions,
		Billing:               req.Billing,
		Spend:                 req.Spend,
	}

	if account.UsesSecretRef() {
		return s.createWithSecretRef(ctx, account)
	}

	// Mirror the operator-declared expiry onto the non-secret metadata
	// record. For a static token that declaration is the ONLY expiry
	// QuicBaaS will ever know — it does not call the provider to discover
	// one — and it is what makes Account.CredentialHealth report "expiring"
	// ahead of the lapse instead of a permanent "unknown".
	account.CredentialExpiresAt = req.Credential.ExpiresAt

	// Store the credential BEFORE the metadata record, so a crash between
	// the two writes never leaves a listable account with no credential —
	// the opposite ordering (metadata first) is the one that could hand
	// AccountResolver an account whose Get(secrets) then fails.
	generation, err := s.secrets.Replace(ctx, req.TenantID, account.ID, req.Credential)
	if err != nil {
		return nil, err
	}
	// The import IS the account's first generation, mirrored onto the record
	// it is about to be created with. Writing it here rather than through a
	// follow-up Touch keeps the create atomic: there is no window in which a
	// listable account describes material whose identity nobody recorded.
	account.CredentialGeneration = generation
	if err := s.store.Create(ctx, account); err != nil {
		createErr := convertAccountStoreError(err)
		if cleanupErr := s.secrets.Delete(ctx, req.TenantID, account.ID); cleanupErr != nil {
			return nil, &AccountCredentialCleanupError{
				TenantID: req.TenantID, AccountID: account.ID, Cause: createErr, Cleanup: cleanupErr,
			}
		}
		return nil, createErr
	}

	return &CreateAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
}

// GetAccount retrieves account metadata.
func (s *accountServiceImpl) GetAccount(ctx context.Context, req *GetAccountRequest) (*GetAccountResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	account, err := s.store.Get(ctx, req.TenantID, req.AccountID)
	if err != nil {
		return nil, convertAccountStoreError(err)
	}
	return &GetAccountResponse{Account: account, Timestamp: time.Now().UTC()}, nil
}

// ListAccounts returns matching accounts for the tenant.
func (s *accountServiceImpl) ListAccounts(ctx context.Context, req *ListAccountsRequest) (*ListAccountsResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	// THE PROVIDER IS NOT PASSED TO THE STORE ANY MORE, and that is not an
	// oversight: it is a COUNTED facet, and a filter bar's count has to see
	// the records its own choice removes. The store read is a scan that
	// filtered in Go either way (accountStore.ListByTenant), so nothing is
	// lost but the place the narrowing happens. The project scope stays: no
	// facet counts it.
	accounts, err := s.store.ListByTenant(ctx, req.TenantID, "", req.ProjectID)
	if err != nil {
		return nil, convertAccountStoreError(err)
	}
	page, err := orchestration.ApplyFacetedPage(
		accounts, req.Page, orchestration.AccountPageSpec(), orchestration.AccountFacetSpec(),
		func(account *orchestration.Account) bool {
			return orchestration.MatchesAccountFacets(account, req.Provider, req.Status)
		})
	if err != nil {
		return nil, err
	}
	return &ListAccountsResponse{
		Accounts:   page.Items,
		Total:      page.Total,
		Limit:      page.Limit,
		Offset:     page.Offset,
		NextOffset: page.NextOffset,
		Facets:     page.Facets,
		Timestamp:  time.Now().UTC(),
	}, nil
}

// UpdateAccount applies field-level metadata updates.
func (s *accountServiceImpl) UpdateAccount(ctx context.Context, req *UpdateAccountRequest) (*UpdateAccountResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	// A mode nobody recognizes is rejected here rather than being written and
	// silently treated as "not refreshable" by every later decision.
	if req.RefreshPolicy != "" {
		if _, ok := req.RefreshPolicy.Canonical(); !ok {
			return nil, convertAccountStoreError(&orchestration.AccountCredentialModeInvalidError{Mode: string(req.RefreshPolicy)})
		}
	}

	if req.CredentialSecretFile != "" && req.CredentialSecretRef == "" {
		return nil, convertAccountStoreError(&orchestration.AccountCredentialSourceError{
			Field:   "credential_secret_file",
			Message: "set it together with credential_secret_ref",
		})
	}

	existing, err := s.rewriteAccount(ctx, req.TenantID, req.AccountID, func(account *orchestration.Account) error {
		return s.applyAccountUpdate(ctx, account, req)
	})
	if err != nil {
		return nil, err
	}

	// A reference account reads its credential from the secret, so any inline
	// envelope it still has describes nothing. Destroyed AFTER the record
	// moved: a crash in between leaves an orphaned envelope, never an account
	// with no credential.
	//
	// ON EVERY UPDATE OF A REFERENCE ACCOUNT, not only the one that switched
	// it. A switch whose cleanup failed has already committed the record, so
	// its retry no longer looks like a switch; keying the cleanup on the
	// transition left that envelope behind for good. Delete is idempotent (an
	// absent envelope is not an error), so the repeat is a no-op once it has
	// succeeded. It cannot reach the referenced secret: that lives under the
	// tenant's own secrets namespace, the inline envelope under the account
	// store's (orchestration.AccountSecretPath).
	if existing.UsesSecretRef() && s.secrets != nil {
		if cleanupErr := s.secrets.Delete(ctx, req.TenantID, req.AccountID); cleanupErr != nil {
			return nil, &AccountCredentialCleanupError{TenantID: req.TenantID, AccountID: req.AccountID, Cleanup: cleanupErr}
		}
	}
	return &UpdateAccountResponse{Account: existing, Timestamp: time.Now().UTC()}, nil
}

// accountUpdateAttempts bounds how many times a record rewrite re-reads and
// re-applies a change whose write lost a race to a concurrent change.
const accountUpdateAttempts = 5

// rewriteAccount applies apply to the stored account and writes it
// CONDITIONALLY: the write lands only if the account is unchanged since it was
// read (a guarded edit in the store), and a lost race re-reads and re-applies
// the change to the new state. Every service-level read-modify-write of an
// account record goes through here.
//
// Unconditional writes let a rename that read an inline account, and was then
// overtaken by a switch to a secret reference (which destroys the inline
// envelope), save its stale snapshot with no reference, leaving an account with
// no credential source at all while both requests reported success.
func (s *accountServiceImpl) rewriteAccount(
	ctx context.Context, tenantID, accountID string, apply func(*orchestration.Account) error,
) (*orchestration.Account, error) {
	for attempt := 1; ; attempt++ {
		existing, err := s.store.Get(ctx, tenantID, accountID)
		if err != nil {
			return nil, convertAccountStoreError(err)
		}
		readAt := existing.UpdatedAt
		if err := apply(existing); err != nil {
			return nil, err
		}
		err = s.store.UpdateIfUnchanged(ctx, existing, readAt)
		if err == nil {
			return existing, nil
		}
		if !stderrors.Is(err, orchestration.ErrAccountVersionConflict) || attempt >= accountUpdateAttempts {
			return nil, convertAccountStoreError(err)
		}
	}
}

// applyAccountUpdate applies req's changes to account in memory.
func (s *accountServiceImpl) applyAccountUpdate(ctx context.Context, existing *orchestration.Account, req *UpdateAccountRequest) error {
	if req.DisplayName != "" {
		existing.DisplayName = req.DisplayName
	}
	if req.ClearProjectID {
		existing.ProjectID = ""
	} else if req.ProjectID != "" {
		existing.ProjectID = req.ProjectID
	}
	if req.RefreshPolicy != "" {
		existing.RefreshPolicy = req.RefreshPolicy
	}
	if req.MaxConcurrentSessions != nil {
		existing.MaxConcurrentSessions = *req.MaxConcurrentSessions
	}
	if req.Billing != nil {
		existing.Billing = *req.Billing
	}
	if req.Spend != nil {
		existing.Spend = *req.Spend
	}
	if req.CredentialSecretRef != "" {
		return s.applySecretRef(ctx, existing, req.CredentialSecretRef, req.CredentialSecretFile)
	}
	if existing.UsesSecretRef() {
		// A mode change on a reference account is held to the same rules a
		// reference write is: a reference is never renewed.
		if err := s.providers.ValidateAccountCredentialSource(existing.Provider,
			effectiveRefreshPolicy(existing.RefreshPolicy), false,
			existing.CredentialSecretRef, existing.CredentialSecretFile); err != nil {
			return convertAccountStoreError(err)
		}
	}
	return nil
}

// RotateAccountCredential replaces the stored credential envelope.
func (s *accountServiceImpl) RotateAccountCredential(ctx context.Context, req *RotateAccountCredentialRequest) (*RotateAccountCredentialResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	if req.Credential == nil {
		return nil, &AccountInvalidError{Field: "credential", Message: "credential is required"}
	}
	// Confirm the account exists (and belongs to this tenant) before writing
	// its credential — otherwise a caller could probe for account IDs across
	// tenants by observing whether a rotate call succeeds.
	existing, err := s.store.Get(ctx, req.TenantID, req.AccountID)
	if err != nil {
		return nil, convertAccountStoreError(err)
	}
	// A reference account has no inline envelope to replace, and writing one
	// would leave two sources that disagree. Its rotation is a write of the
	// secret it names.
	if existing.UsesSecretRef() {
		return nil, convertAccountStoreError(&orchestration.AccountCredentialSourceError{
			Field: "credential",
			Message: "this account reads its credential from secret " + existing.CredentialSecretRef +
				"; rotate it by writing a new version of that secret",
		})
	}
	// The replacement must satisfy the account's mode against its provider's
	// effective descriptor, exactly as the original did at creation: a rotate
	// is the other way an unusable credential gets into the pool.
	if validateErr := s.providers.ValidateCredential(
		existing.Provider, effectiveRefreshPolicy(existing.RefreshPolicy), req.Credential); validateErr != nil {
		return nil, convertAccountStoreError(validateErr)
	}
	// THE REPLACEMENT IS A NEW GENERATION, stamped into the same encrypted
	// write as the material. Every live session that materialized the previous
	// one is now holding a credential the provider will stop honouring, and
	// the generation is how each of them finds that out before spending a turn
	// on it (SessionAccountFreshness, go-quicbaas#997).
	generation, err := s.secrets.Replace(ctx, req.TenantID, req.AccountID, req.Credential)
	if err != nil {
		return nil, err
	}
	// The replacement envelope's expiry supersedes the old one, including when
	// the operator declared none: carrying the retired credential's expiry
	// forward would describe a credential that no longer exists.
	//
	// RequireInlineCredential closes the race with a switch to a reference.
	// The check above read the account BEFORE the envelope write; a switch can
	// commit in between and destroy the inline envelope, which the Put above
	// has then recreated. The store decides "still inline" atomically with
	// this record write, so the rotation either lands on an inline account or
	// writes nothing to the record — and then removes the envelope it wrote.
	touchErr := s.store.Touch(ctx, req.TenantID, req.AccountID, orchestration.TouchAccountOptions{
		MarkRefreshed:            true,
		MarkCredentialRotated:    true,
		SetCredentialGeneration:  generation,
		SetStatusValid:           true,
		SetStatus:                orchestration.AccountStatusActive,
		ClearLastError:           true,
		SetCredentialExpiresAt:   req.Credential.ExpiresAt,
		SetCredentialExpiryValid: true,
		RequireInlineCredential:  true,
	})
	var switched *orchestration.AccountCredentialSourceChangedError
	if stderrors.As(touchErr, &switched) {
		if cleanupErr := s.secrets.Delete(ctx, req.TenantID, req.AccountID); cleanupErr != nil {
			return nil, &AccountCredentialCleanupError{
				TenantID: req.TenantID, AccountID: req.AccountID, Cause: touchErr, Cleanup: cleanupErr,
			}
		}
		return nil, touchErr
	}
	if touchErr != nil {
		return nil, &AccountRotationIncompleteError{
			TenantID: req.TenantID, AccountID: req.AccountID, Cause: convertAccountStoreError(touchErr),
		}
	}
	return &RotateAccountCredentialResponse{AccountID: req.AccountID, Timestamp: time.Now().UTC()}, nil
}

// RevokeAccount marks an account revoked.
func (s *accountServiceImpl) RevokeAccount(ctx context.Context, req *RevokeAccountRequest) (*RevokeAccountResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	existing, err := s.rewriteAccount(ctx, req.TenantID, req.AccountID, func(account *orchestration.Account) error {
		account.Status = orchestration.AccountStatusRevoked
		account.LastError = req.Reason
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &RevokeAccountResponse{Account: existing, Timestamp: time.Now().UTC()}, nil
}

// DeleteAccount removes both the metadata record and the credential
// envelope.
func (s *accountServiceImpl) DeleteAccount(ctx context.Context, req *DeleteAccountRequest) (*DeleteAccountResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	if err := s.store.Delete(ctx, req.TenantID, req.AccountID); err != nil {
		return nil, convertAccountStoreError(err)
	}
	// The metadata record is already gone (the authoritative "does this
	// account exist" answer), so a failure destroying the credential is
	// reported as an orphaned envelope, not a rolled-back delete — reversing
	// the metadata delete would reintroduce a TOCTOU window where
	// AccountResolver could hand out an orphaned credential.
	if cleanupErr := s.secrets.Delete(ctx, req.TenantID, req.AccountID); cleanupErr != nil {
		return nil, &AccountCredentialCleanupError{TenantID: req.TenantID, AccountID: req.AccountID, Cleanup: cleanupErr}
	}
	return &DeleteAccountResponse{AccountID: req.AccountID, Deleted: true, Timestamp: time.Now().UTC()}, nil
}

// SetDefaultAccount marks accountID as the tenant/project+provider default.
func (s *accountServiceImpl) SetDefaultAccount(ctx context.Context, req *SetDefaultAccountRequest) (*SetDefaultAccountResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	if err := s.store.SetDefault(ctx, req.TenantID, req.AccountID); err != nil {
		return nil, convertAccountStoreError(err)
	}
	return &SetDefaultAccountResponse{AccountID: req.AccountID, Timestamp: time.Now().UTC()}, nil
}

// GetAccountUsage returns the cumulative usage report recorded against an
// account via the wired CostTracker (see SetUsageTracker).
func (s *accountServiceImpl) GetAccountUsage(ctx context.Context, req *GetAccountUsageRequest) (*GetAccountUsageResponse, error) {
	if req == nil || req.TenantID == "" {
		return nil, &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.AccountID == "" {
		return nil, &AccountInvalidError{Field: "account_id", Message: "account ID is required"}
	}
	ctx = accountTenantContext(ctx, req.TenantID)
	// Confirm the account exists within the tenant first so usage cannot be
	// probed for an account ID belonging to another tenant.
	account, err := s.store.Get(ctx, req.TenantID, req.AccountID)
	if err != nil {
		return nil, convertAccountStoreError(err)
	}
	if s.usage == nil {
		return nil, &AccountUsageTrackerNotConfiguredError{}
	}
	report, err := s.usage.GetAccountUsage(ctx, req.TenantID, req.AccountID)
	if err != nil {
		return nil, err
	}
	live, err := s.liveSessions(ctx, req.TenantID, req.AccountID)
	if err != nil {
		return nil, err
	}
	spend, err := s.spendHeadroom(ctx, account)
	if err != nil {
		return nil, err
	}
	return &GetAccountUsageResponse{Usage: report, LiveSessions: live, Spend: spend, Timestamp: time.Now().UTC()}, nil
}

// spendHeadroom asks the spend gate about the account without claiming a
// half-open trial, or answers nil when no gate is wired. A gate that could
// not establish its answer fails the call: a verdict of "admits" invented
// over an unreadable ledger is the headroom the gate exists to refuse.
func (s *accountServiceImpl) spendHeadroom(ctx context.Context, account *orchestration.Account) (*orchestration.SpendHeadroom, error) {
	if s.spend == nil {
		return nil, nil
	}
	headroom, err := orchestration.SpendHeadroomOf(
		s.spend.Check(ctx, orchestration.SpendAuthorizeRequest{Account: account}, time.Now().UTC()))
	if err != nil {
		return nil, err
	}
	return &headroom, nil
}

// liveSessions counts the account's live sessions, or zero when no session
// lookup is wired (a deployment with no session store has none to count).
// A lookup that fails is an error, not a zero: a daemon balancing on a zero it
// was never told was a guess would put every session on this account.
func (s *accountServiceImpl) liveSessions(ctx context.Context, tenantID, accountID string) (int, error) {
	if s.sessions == nil {
		return 0, nil
	}
	counts, err := s.sessions.LiveSessions(ctx, tenantID, []string{accountID})
	if err != nil {
		return 0, err
	}
	return counts[accountID], nil
}

// AccountUsageTrackerNotConfiguredError indicates this AccountService
// instance was constructed without a CostTracker wired in for usage queries.
type AccountUsageTrackerNotConfiguredError struct{}

// Error implements the error interface.
func (e *AccountUsageTrackerNotConfiguredError) Error() string {
	return "account: usage tracker not configured"
}

func (s *accountServiceImpl) validateCreate(req *CreateAccountRequest) error {
	if req == nil {
		return &AccountInvalidError{Message: "request must not be nil"}
	}
	if req.TenantID == "" {
		return &AccountInvalidError{Field: "tenant_id", Message: "tenant ID is required"}
	}
	if req.Provider == "" {
		return &AccountInvalidError{Field: "provider", Message: "provider is required"}
	}
	if err := s.providers.ValidateAccountCredentialSource(req.Provider, effectiveRefreshPolicy(req.RefreshPolicy),
		req.Credential != nil, req.CredentialSecretRef, req.CredentialSecretFile); err != nil {
		return convertAccountStoreError(err)
	}
	if req.Credential == nil {
		return nil
	}
	// Fail closed at write time on a credential that could never be renewed:
	// an oauth_refresh account with no refresh token, or none whose account
	// nor provider descriptor supplies a token endpoint. Left to spawn time
	// this surfaces as a failed agent session against a live workload.
	if err := s.providers.ValidateCredential(
		req.Provider, effectiveRefreshPolicy(req.RefreshPolicy), req.Credential); err != nil {
		return convertAccountStoreError(err)
	}
	return nil
}

// effectiveRefreshPolicy normalizes an absent credential mode to the static
// (never-renewed) one, matching what accountStore.create persists for an
// account that declares none.
func effectiveRefreshPolicy(policy orchestration.AccountRefreshPolicy) orchestration.AccountRefreshPolicy {
	if policy == "" {
		return orchestration.AccountRefreshNone
	}
	return policy
}

// convertAccountStoreError translates orchestration-layer account errors into
// the service-layer vocabulary transports map to status codes, mirroring
// projectServiceImpl.convertStoreError. Each translated error keeps the domain
// error as its Cause so errors.Is against the orchestration sentinels still
// works for callers that care about the specific reason.
func convertAccountStoreError(err error) error {
	var (
		notFound      *orchestration.AccountNotFoundError
		invalid       *orchestration.AccountInvalidError
		modeInvalid   *orchestration.AccountCredentialModeInvalidError
		refreshNeeded *orchestration.AccountRefreshTokenRequiredError
		staticRefresh *orchestration.AccountStaticTokenRefreshableError
		emptyCred     *orchestration.AccountCredentialEmptyError
		endpointUnres *orchestration.AccountProviderEndpointUnresolvedError
		billing       *orchestration.AccountBillingInvalidError
		quota         *orchestration.QuotaInvalidError
		breaker       *orchestration.BreakerInvalidError
		sourceInvalid *orchestration.AccountCredentialSourceError
	)
	switch {
	case stderrors.As(err, &sourceInvalid):
		return &AccountInvalidError{Field: sourceInvalid.Field, Message: sourceInvalid.Message, Cause: err}
	case stderrors.As(err, &notFound):
		return &AccountNotFoundError{TenantID: notFound.TenantID, AccountID: notFound.AccountID}
	case stderrors.As(err, &invalid):
		return &AccountInvalidError{Field: invalid.Field, Message: invalid.Message, Cause: err}
	case stderrors.As(err, &modeInvalid):
		return &AccountInvalidError{
			Field:   "refresh_policy",
			Message: "unrecognized credential mode " + modeInvalid.Mode + " (valid: oauth2, none)",
			Cause:   err,
		}
	case stderrors.As(err, &refreshNeeded):
		return &AccountInvalidError{
			Field:   "credential.refresh_token",
			Message: "an oauth2 account must supply a refresh token",
			Cause:   err,
		}
	case stderrors.As(err, &staticRefresh):
		return &AccountInvalidError{
			Field:   "credential.refresh_token",
			Message: "a static (none) account must not supply a refresh token",
			Cause:   err,
		}
	case stderrors.As(err, &emptyCred):
		return &AccountInvalidError{
			Field:   "credential",
			Message: "a static (none) account must supply an access token, env or files",
			Cause:   err,
		}
	case stderrors.As(err, &quota):
		return &AccountInvalidError{Field: "spend.quota." + quota.Field, Message: quota.Message, Cause: err}
	case stderrors.As(err, &breaker):
		return &AccountInvalidError{Field: "spend.breaker." + breaker.Field, Message: breaker.Message, Cause: err}
	case stderrors.As(err, &billing):
		return &AccountInvalidError{Field: billing.Field, Message: billing.Message, Cause: err}
	case stderrors.As(err, &endpointUnres):
		return &AccountInvalidError{
			Field:   "credential.token_endpoint",
			Message: "no token endpoint for provider " + string(endpointUnres.Provider) + ": set it on the account or configure orchestration.account_providers",
			Cause:   err,
		}
	default:
		return err
	}
}

// Compile-time interface satisfaction check.
var _ AccountService = (*accountServiceImpl)(nil)
