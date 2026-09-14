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

package orchestration

import (
	"context"
	"encoding/json"
	"time"
)

// =============================================================================
// Account — a pooled provider credential (e.g. one Anthropic/Claude login,
// one OpenAI/Codex API key, one OpenCode provider config) that an ACP agent
// session can be spawned against.
//
// An Account is metadata only: identity, provider, lifecycle status, and
// usage bookkeeping timestamps. The secret material itself (OAuth tokens,
// API keys, provider config file bytes) NEVER lives on this struct or in the
// AccountStore's KV record — it is held exclusively behind
// interfaces.SecretsManager (the same encrypted-at-rest, barrier-backed
// subsystem pkg/service.SecretsService already uses) and addressed only by
// Account.ID. This mirrors the split already used for named credentials
// elsewhere in the stack: a cheap, listable metadata record plus an opaque,
// separately-encrypted secret blob.
// =============================================================================

// AccountProvider identifies which CLI/backend a pooled account authenticates.
// It is derived from the ACP backend's command basename (see ACPBackend.Type
// / filepath.Base(config.Command)) so an Account is naturally scoped to the
// backend that will consume its credential.
type AccountProvider string

const (
	// AccountProviderClaude pools Anthropic/Claude Code accounts.
	AccountProviderClaude AccountProvider = "claude"

	// AccountProviderCodex pools OpenAI/Codex accounts.
	AccountProviderCodex AccountProvider = "codex"

	// AccountProviderOpenCode pools OpenCode provider configurations.
	AccountProviderOpenCode AccountProvider = "opencode"
)

// AccountStatus is the lifecycle state of a pooled account.
type AccountStatus string

const (
	// AccountStatusActive indicates the account is eligible for selection.
	AccountStatusActive AccountStatus = "active"

	// AccountStatusRevoked indicates the account has been explicitly revoked
	// by an operator. A revoked account is never selected, and any session
	// that already captured its credential is not killed retroactively (see
	// docs/orchestration/accounts.md "Lifecycle" for the accepted blast
	// radius) but no NEW spawn or refresh will use it again.
	AccountStatusRevoked AccountStatus = "revoked"

	// AccountStatusError indicates the account's last token refresh attempt
	// failed. It is excluded from selection until an operator investigates
	// and either the next refresh succeeds (auto-recovers to Active) or the
	// operator revokes it.
	AccountStatusError AccountStatus = "error"

	// AccountStatusLimited indicates the provider refused the account because
	// it hit a rate, quota, or usage limit. Unlike Revoked and Error it is a
	// SELF-HEALING state: Account.AvailableAt names the moment the hold
	// lapses, after which the account returns to the selectable pool with no
	// operator action at all (see Account.Selectable and
	// Account.expireCooldown).
	//
	// It is deliberately distinct from Error. An errored account has a broken
	// credential and needs a human; a limited account has a perfectly good
	// credential that is simply busy, and burying the two in one state would
	// either strand recoverable accounts or hide real breakage behind an
	// automatic retry.
	AccountStatusLimited AccountStatus = "limited"
)

// AccountCredentialHealth is the operator-facing verdict on how much life an
// account's credential has left. It is DERIVED from Account.CredentialExpiresAt
// (never from the credential itself, which this record never sees), so it can
// be reported over every transport without the account subsystem handing out —
// or the API echoing back — any secret material.
type AccountCredentialHealth string

const (
	// AccountCredentialHealthUnknown means no expiry was ever recorded. For a
	// static-token account that is the honest answer: QuicBaaS never contacts a
	// provider to discover a long-lived token's expiry, so an operator who
	// declared none at import time gets "unknown", not a fabricated "valid".
	AccountCredentialHealthUnknown AccountCredentialHealth = "unknown"

	// AccountCredentialHealthValid means the credential expires further out
	// than AccountCredentialExpiryWarning.
	AccountCredentialHealthValid AccountCredentialHealth = "valid"

	// AccountCredentialHealthExpiring means the credential expires within
	// AccountCredentialExpiryWarning. For a static-token account this is the
	// signal to renew out-of-band BEFORE spawns start failing, which is the
	// whole reason the expiry is mirrored onto this record.
	AccountCredentialHealthExpiring AccountCredentialHealth = "expiring"

	// AccountCredentialHealthExpired means the recorded expiry has passed.
	AccountCredentialHealthExpired AccountCredentialHealth = "expired"
)

// AccountCredentialExpiryWarning is how far ahead of a recorded expiry an
// account starts reporting AccountCredentialHealthExpiring.
//
// Twenty-four hours is chosen for the mode that actually needs the warning:
// AccountCredentialModeStaticToken, whose renewal is a manual operator action
// that cannot be scheduled in the seconds an oauth_refresh account needs (that
// mode renews itself within DefaultAccountRefreshSkew and never reaches this
// accessor in a degraded state).
const AccountCredentialExpiryWarning = 24 * time.Hour

// AccountCredentialMode is the per-account, explicitly-chosen contract for how
// a pooled credential is kept usable. It is selected independently for every
// account in the pool: an operator holding five Anthropic seats may run two of
// them on AccountCredentialModeOAuthRefresh and three on
// AccountCredentialModeStaticToken.
//
// There is no implicit mode. An account whose mode is absent or unrecognized
// is rejected at write time and refuses to resolve at spawn time
// (AccountCredentialModeInvalidError) rather than being guessed at — guessing
// "refreshable" for an account that holds no refresh token would produce a
// spurious refresh failure against the provider, and guessing "static" for an
// account that does hold one would silently stop renewing it.
type AccountCredentialMode string

const (
	// AccountCredentialModeOAuthRefresh marks a credential renewed by
	// AccountTokenManager via a standard RFC 6749 section 6 refresh_token
	// grant against AccountCredential.TokenEndpoint.
	//
	// The account stores an access_token + refresh_token pair obtained
	// out-of-band by the operator (see docs/orchestration/accounts.md
	// "Importing an account"). Both TokenEndpoint and ClientID are per-account
	// configuration on AccountCredential; this package hardcodes no vendor's
	// endpoint or client identifier.
	//
	// For providers whose refresh endpoint is not a documented, supported API,
	// this mode can stop working without notice. It therefore degrades LOUDLY:
	// a failed refresh marks the account AccountStatusError and fails the
	// spawn with AccountRefreshFailedError naming the account. It never falls
	// back to the host's shared identity and never reroutes to another
	// account.
	AccountCredentialModeOAuthRefresh AccountCredentialMode = "oauth_refresh" //nolint:gosec // G101: a mode discriminator, not a credential

	// AccountCredentialModeStaticToken marks a long-lived credential that
	// QuicBaaS never renews: an officially-issued long-lived token
	// (e.g. CLAUDE_CODE_OAUTH_TOKEN from `claude setup-token`) or a plain API
	// key. AccountTokenManager passes it through untouched — it is never
	// treated as refreshable, so it can never produce a refresh failure or
	// refresh-related log noise.
	//
	// Renewal is a manual, out-of-band operator action, so the expiry the
	// operator declares at import time (AccountCredential.ExpiresAt) is
	// mirrored onto the account's non-secret metadata
	// (Account.CredentialExpiresAt) and reported by Account.CredentialHealth
	// so a lapse is visible BEFORE spawns start failing.
	AccountCredentialModeStaticToken AccountCredentialMode = "static_token"
)

// Transport-facing input aliases for the two modes.
//
// The REST/gRPC/QUIC/MCP/CLI surfaces and the SDK currently spell the field
// "refresh_policy" with the values "oauth2"/"none". Those spellings are
// accepted as INPUT by ParseAccountCredentialMode and normalized to the
// canonical mode above before anything is persisted, so storage and every
// behavioral decision use exactly one vocabulary. They are aliases, not a
// second mode set: "oauth2" and AccountCredentialModeOAuthRefresh are the same
// mode, and an account can never be stored holding the alias spelling.
const (
	// AccountRefreshOAuth2 is the transport alias for
	// AccountCredentialModeOAuthRefresh.
	AccountRefreshOAuth2 AccountCredentialMode = "oauth2"

	// AccountRefreshNone is the transport alias for
	// AccountCredentialModeStaticToken.
	AccountRefreshNone AccountCredentialMode = "none"
)

// AccountRefreshPolicy is the transport-facing name of AccountCredentialMode.
// The two are the same type; the API/SDK layers still say "refresh policy"
// while the domain says "credential mode".
type AccountRefreshPolicy = AccountCredentialMode

// Account is the durable, non-secret metadata record for one pooled provider
// credential.
type Account struct {
	// ID is the opaque, server-generated identifier. It is the only handle
	// used to address the account's secret material — never its DisplayName
	// or Provider — so log lines and key names never carry anything
	// human-identifying about the underlying login.
	ID string `json:"id"`

	// TenantID owns this account. Enforced by key construction in
	// AccountStore; see store_keys.go.
	TenantID string `json:"tenant_id"`

	// ProjectID optionally scopes the account to one project. Empty means the
	// account is available tenant-wide to every project.
	ProjectID string `json:"project_id,omitempty"`

	// Provider identifies which backend consumes this account's credential.
	Provider AccountProvider `json:"provider"`

	// DisplayName is an operator-chosen label (e.g. "alice-work",
	// "team-max-3"). It is metadata for humans only: never used as a
	// storage key and never logged alongside credential material.
	DisplayName string `json:"display_name"`

	// Default marks this account as the tenant+provider (or
	// tenant+project+provider, when ProjectID is set) fallback the
	// StaticAccountStrategy selects when no AccountID is pinned.
	//
	// DERIVED, NEVER PERSISTED. The durable per-scope default pointer
	// AccountStore keeps is the sole authority; this field is computed from it
	// on every read and is zeroed on write (see marshalAccount). Exactly one
	// account per scope can therefore report it, because "which account" is
	// answered by a single key rather than by N records that could disagree —
	// the same treatment CredentialHealth gets, and for the same reason.
	//
	// Setting it on a Create or Update is a REQUEST to claim the scope, which
	// the store honors by moving the pointer. It is not a value the caller can
	// store.
	//
	// An account created into a scope that has no default becomes the default,
	// so the very first Anthropic (or OpenAI, or OpenCode) account an operator
	// adds is immediately what an unpinned spawn uses — the alternative is a
	// pool of exactly one account that still refuses to resolve. Every later
	// account in that scope is created non-default and only becomes the default
	// through an explicit AccountStore.SetDefault.
	Default bool `json:"default"`

	// Status is the current lifecycle state.
	Status AccountStatus `json:"status"`

	// RefreshPolicy is this account's AccountCredentialMode. It is required:
	// AccountStore rejects a create or update that leaves it absent or
	// unrecognized, and the stored value is always one of the canonical
	// AccountCredentialMode constants (never a transport alias).
	RefreshPolicy AccountCredentialMode `json:"refresh_policy"`

	// CreatedAt / UpdatedAt / LastUsedAt / LastRefreshedAt are audit-visible
	// timestamps. None of them ever appear inside the encrypted credential
	// envelope; they live only on this metadata record.
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	LastUsedAt      time.Time `json:"last_used_at,omitempty"`
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitempty"`

	// CredentialExpiresAt mirrors the credential envelope's expiry onto this
	// non-secret record so an operator can see a lapse coming without the
	// account subsystem ever handing out (or the API ever returning) the
	// credential itself. A timestamp is not credential material.
	//
	// It is written from the renewed envelope after every successful
	// AccountCredentialModeOAuthRefresh renewal, and from the operator-declared
	// AccountCredential.ExpiresAt for AccountCredentialModeStaticToken.
	//
	// Zero means UNKNOWN, not "never expires": QuicBaaS never contacts a
	// provider to discover a static token's expiry, so if the operator did not
	// declare one at import time there is nothing honest to report. See
	// Account.CredentialHealth and docs/orchestration/accounts.md.
	CredentialExpiresAt time.Time `json:"credential_expires_at,omitempty"`

	// LastValidatedAt is the last time QuicBaaS successfully loaded this
	// account's credential and projected it into a spawn. For
	// AccountCredentialModeOAuthRefresh it is additionally advanced by a
	// successful renewal, which IS provider-confirmed proof of life. For
	// AccountCredentialModeStaticToken it is NOT provider confirmation — it
	// only means the envelope was present and well-formed.
	LastValidatedAt time.Time `json:"last_validated_at,omitempty"`

	// AvailableAt is the moment an AccountStatusLimited account returns to the
	// selectable pool. It is set from the provider's own Retry-After / reset
	// hint when one could be parsed out of the failure, and otherwise from a
	// configured default backoff (see AccountLimitClassifier and
	// DefaultAccountLimitBackoff).
	//
	// Zero means the account is not under a limit hold. A timestamp in the PAST
	// on a limited account means the hold has lapsed but nothing has written
	// the record since: reads normalize that back to Active in memory
	// (Account.expireCooldown) and the next write persists it, so no background
	// sweeper is needed to return an account to the pool.
	AvailableAt time.Time `json:"available_at,omitempty"`

	// Spend is what this account may spend and how many failures take it out
	// of service. The zero value meters nothing, which is the honest state
	// for an account nobody has configured a limit for; see
	// AccountSpendPolicy and account_spend_authority.go for where a metered
	// account is actually refused.
	Spend AccountSpendPolicy `json:"spend,omitempty"`

	// LastError carries the message of the most recent failed refresh, when
	// Status is AccountStatusError. Cleared on the next successful refresh.
	// It is redacted (see redactAccountCredential) before it is stored, so a
	// provider that echoes a presented token in its error body cannot turn
	// this metadata record into a copy of the credential.
	LastError string `json:"last_error,omitempty"`
}

// AccountCredential is the secret envelope for one Account. It is stored and
// retrieved exclusively through AccountSecretStore (interfaces.SecretsManager
// backed) — never through AccountStore — and must never be logged, returned
// in an API response, or embedded in a session transcript.
type AccountCredential struct {
	// Env holds static environment variables to inject into the spawned
	// subprocess verbatim (e.g. CODEX_API_KEY for the codex provider). Safe
	// for providers whose credential is a single opaque value.
	Env map[string]string `json:"env,omitempty"`

	// Files holds provider config file bytes keyed by a path relative to the
	// materialized per-session home directory (e.g.
	// ".config/opencode/auth.json"). Used by providers that read credentials
	// from disk rather than the environment.
	Files map[string][]byte `json:"files,omitempty"`

	// AccessToken is the bearer credential handed to the spawn. Required for
	// both modes unless the account carries its whole credential in Env/Files.
	AccessToken string `json:"access_token,omitempty"`

	// RefreshToken is redeemed by OAuth2TokenRefresher. It is REQUIRED for
	// AccountCredentialModeOAuthRefresh and FORBIDDEN for
	// AccountCredentialModeStaticToken — an account that stores one but is
	// never going to redeem it is a mode mistake, not a harmless extra field,
	// and is rejected at write time with AccountStaticTokenRefreshableError.
	RefreshToken string `json:"refresh_token,omitempty"`

	// ExpiresAt is when AccessToken stops working.
	//
	//   - AccountCredentialModeOAuthRefresh: set by the token endpoint's
	//     expires_in on every renewal, so it is always accurate.
	//   - AccountCredentialModeStaticToken: OPERATOR-DECLARED at import time.
	//     A long-lived provider token is typically an opaque string with no
	//     machine-readable expiry, and QuicBaaS does not call the provider to
	//     discover one. Left zero, the account simply reports an unknown
	//     expiry rather than a fabricated countdown.
	ExpiresAt time.Time `json:"expires_at,omitempty"`

	// TokenEndpoint is the RFC 6749 token endpoint the refresh_token grant is
	// POSTed to. Per-account configuration, REQUIRED for
	// AccountCredentialModeOAuthRefresh. No vendor endpoint is hardcoded
	// anywhere in this package.
	TokenEndpoint string `json:"token_endpoint,omitempty"`

	// ClientID is the OAuth2 client identifier sent with the refresh grant.
	// Per-account configuration; optional (some servers key the grant off the
	// refresh token alone).
	ClientID string `json:"client_id,omitempty"`

	// Scopes records the scopes the current access token was issued with.
	Scopes []string `json:"scopes,omitempty"`

	// AccessTokenEnvKey, when non-empty, is the environment variable name the
	// current AccessToken is projected into at materialization time (e.g.
	// "CLAUDE_CODE_OAUTH_TOKEN"). Left empty for providers that carry their
	// full credential in Env or Files already.
	AccessTokenEnvKey string `json:"access_token_env_key,omitempty"`
}

// AccountStore defines the persistence contract for Account metadata
// records. It never sees AccountCredential — callers pair it with an
// AccountSecretStore for the secret half of an account.
type AccountStore interface {
	// Create persists a new account.
	//
	// When the account's tenant+provider(+project) scope has no default yet,
	// Create claims it: account.Default is set to true in place and the scope's
	// default pointer is written, atomically against any concurrent Create or
	// SetDefault for the same scope. Passing Default already true requests the
	// default outright and displaces whichever account currently holds it.
	Create(ctx context.Context, account *Account) error

	// Get retrieves an account by ID within the given tenant, with Default
	// derived from its scope's default pointer.
	Get(ctx context.Context, tenantID, accountID string) (*Account, error)

	// Update replaces the stored account. Returns AccountNotFoundError if
	// the account does not exist.
	//
	// Changing ProjectID (or Provider) moves the account to a different default
	// scope. The move is resolved like a Create: it claims the destination when
	// that scope has no default, arrives non-default when it does, and releases
	// the scope it left.
	Update(ctx context.Context, account *Account) error

	// Delete removes an account by ID. Deleting the scope's default leaves the
	// scope with no default rather than promoting a survivor; see
	// accountStore.releaseDefaultLocked for why.
	Delete(ctx context.Context, tenantID, accountID string) error

	// ListByTenant returns every account owned by tenantID, optionally
	// filtered to one provider and/or one project. An empty provider or
	// projectID skips that filter. Default is derived per account from its
	// scope's default pointer, so at most one result per scope reports it.
	//
	// Note the empty-projectID case means "do not filter by project", NOT "the
	// tenant-wide scope": the result may span several default scopes.
	ListByTenant(ctx context.Context, tenantID string, provider AccountProvider, projectID string) ([]*Account, error)

	// SetDefault marks accountID as the default for its tenant+provider
	// (+project, when scoped). It moves the scope's default pointer, which is a
	// single-key write: whoever previously held the scope stops being the
	// default the instant it lands, with no per-record fan-out.
	SetDefault(ctx context.Context, tenantID, accountID string) error

	// Touch records that accountID was just selected for a spawn (LastUsedAt)
	// or refreshed (LastRefreshedAt/LastError/Status), without requiring the
	// caller to read-modify-write the full record.
	Touch(ctx context.Context, tenantID, accountID string, opts TouchAccountOptions) error
}

// TouchAccountOptions describes a partial, best-effort update to an
// account's bookkeeping fields. Zero-value fields are left unchanged, except
// where an explicit Set flag says otherwise.
type TouchAccountOptions struct {
	MarkUsed       bool
	MarkRefreshed  bool
	SetStatus      AccountStatus
	SetStatusValid bool
	SetLastError   string
	ClearLastError bool

	// MarkValidated advances Account.LastValidatedAt to now.
	MarkValidated bool

	// SetCredentialExpiresAt mirrors the credential envelope's expiry onto
	// the metadata record. It is applied only when SetCredentialExpiryValid
	// is true, so a caller can distinguish "expiry is unknown, clear it" (a
	// zero time with the flag set) from "leave whatever is there alone" (the
	// flag unset) — the same explicit-set convention SetStatus uses.
	SetCredentialExpiresAt   time.Time
	SetCredentialExpiryValid bool

	// SetAvailableAt records when an AccountStatusLimited account returns to
	// the pool. Applied only when SetAvailableAtValid is true, so a caller can
	// distinguish "clear the hold" (a zero time with the flag set) from "leave
	// it alone" (the flag unset) — the same explicit-set convention SetStatus
	// uses.
	SetAvailableAt      time.Time
	SetAvailableAtValid bool
}

// =============================================================================
// Derived state
// =============================================================================

// CredentialHealth reports how much life the account's credential has left,
// derived from the expiry mirrored onto this metadata record.
//
// It answers the question an operator actually has — "is one of my pooled
// logins about to stop working?" — without the caller ever touching
// AccountSecretStore. A nil receiver reports Unknown rather than panicking, so
// it is safe to call on a list entry that failed to decode.
func (a *Account) CredentialHealth() AccountCredentialHealth {
	if a == nil || a.CredentialExpiresAt.IsZero() {
		return AccountCredentialHealthUnknown
	}
	now := time.Now().UTC()
	switch {
	case !a.CredentialExpiresAt.After(now):
		return AccountCredentialHealthExpired
	case a.CredentialExpiresAt.Before(now.Add(AccountCredentialExpiryWarning)):
		return AccountCredentialHealthExpiring
	default:
		return AccountCredentialHealthValid
	}
}

// MarshalJSON projects the derived credential health onto every JSON
// representation of an account.
//
// It lives here rather than being restated in each transport's response type
// so REST, QUIC, MCP and the embedded transport — which all serialize this
// record directly — agree with the two that convert field by field (gRPC's
// proto message and the SDK's AccountInfo) instead of drifting apart. The
// health is DERIVED, never stored: accountStore marshals through a type alias
// that bypasses this method (see marshalAccount), so a stale verdict can never
// be written into a durable record and read back as fact.
func (a Account) MarshalJSON() ([]byte, error) {
	type wireAccount Account
	return json.Marshal(struct {
		wireAccount
		CredentialHealth AccountCredentialHealth `json:"credential_health,omitempty"`
	}{
		wireAccount:      wireAccount(a),
		CredentialHealth: a.CredentialHealth(),
	})
}

// Selectable reports whether the account may be handed to a spawn at time now.
//
// Active accounts always are. Limited accounts are once their hold has lapsed —
// that is the lazy return-to-pool: no sweeper marks them available again, the
// next selection simply stops excluding them. Revoked and Error accounts never
// are; both need an operator.
func (a *Account) Selectable(now time.Time) bool {
	if a == nil {
		return false
	}
	switch a.Status {
	case AccountStatusActive:
		return true
	case AccountStatusLimited:
		return !a.LimitHoldActive(now)
	default:
		return false
	}
}

// LimitHoldActive reports whether a limit hold is still in force at now. A
// limited account with a zero AvailableAt is held indefinitely: nothing
// promised when it would come back, so guessing "immediately" would put it
// straight back into the rotation that just failed.
func (a *Account) LimitHoldActive(now time.Time) bool {
	if a == nil || a.Status != AccountStatusLimited {
		return false
	}
	if a.AvailableAt.IsZero() {
		return true
	}
	return now.Before(a.AvailableAt)
}

// expireCooldown returns a limited account whose hold has lapsed to the active
// pool, IN MEMORY, and reports whether it changed anything.
//
// It is called on every decode path (accountStore.load and ListByTenant) so a
// reader — the resolver, a REST list, an operator's CLI — never sees an account
// described as limited when its own AvailableAt says otherwise. The durable
// record catches up on the next write that touches it (Touch reloads through
// the same normalization before persisting), which is why this feature needs no
// background goroutine and no sweeper lifecycle to reason about.
func (a *Account) expireCooldown(now time.Time) bool {
	if a == nil || a.Status != AccountStatusLimited || a.LimitHoldActive(now) {
		return false
	}
	a.Status = AccountStatusActive
	a.AvailableAt = time.Time{}
	return true
}
