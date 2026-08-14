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
	"time"
)

// =============================================================================
// AccountTokenManager — renew-before-use, single-flight-per-account token
// refresh.
//
// This mirrors two patterns already proven elsewhere in the stack rather
// than inventing a third:
//
//   - pkg/cli.TokenManager: renew when the token has less than a skew
//     window of life left, and re-check under the lock in case a racing
//     caller already renewed (the double-checked pattern below).
//   - pkg/iam (OIDCProvider session refresh): the renewal credential is
//     opaque, rotates on every use, and the rotated pair is the only copy —
//     losing the write loses the ability to renew again.
//
// The difference from pkg/cli.TokenManager is cardinality: that type guards
// one CLI-local file; this one guards N pooled accounts, so the lock is
// keyed per tenant+account (keyedMutex) instead of a single mutex — five
// agents starting against five DIFFERENT accounts proceed in parallel, and
// five agents racing to start against the SAME account serialize to exactly
// one refresh.
// =============================================================================

// DefaultAccountRefreshSkew mirrors pkg/cli.RefreshSkew: a credential with
// less than this much life left is renewed before being handed to a spawn,
// so it cannot expire mid-session.
const DefaultAccountRefreshSkew = 60 * time.Second

// TokenRefresher renews an OAuth2-style credential for one provider. It is
// registered per AccountProvider on AccountTokenManager.
//
// QuicBaaS ships OAuth2TokenRefresher, a generic RFC 6749 section 6
// refresh_token-grant implementation. It is deliberately NOT pre-wired with
// any vendor's token endpoint or client_id: the concrete values come from the
// account's own AccountCredential, or from its provider's descriptor in the
// AccountProviderRegistry (built-ins plus orchestration.account_providers
// configuration) — see account_provider_registry.go.
type TokenRefresher interface {
	// Refresh redeems cred's refresh credential and returns the renewed
	// envelope. Implementations MUST return a fresh AccountCredential (not
	// mutate cred in place) so a failed persist by the caller cannot corrupt
	// the value already in memory elsewhere.
	Refresh(ctx context.Context, cred *AccountCredential) (*AccountCredential, error)
}

// AccountTokenManager resolves a usable AccountCredential for an account,
// renewing it first when required.
type AccountTokenManager struct {
	secrets    *AccountSecretStore
	store      AccountStore
	providers  *AccountProviderRegistry
	refreshers map[AccountProvider]TokenRefresher
	skew       time.Duration
	locks      keyedMutex
}

// AccountTokenManagerOption configures an AccountTokenManager at construction.
type AccountTokenManagerOption func(*AccountTokenManager)

// WithAccountProviders selects the provider registry whose descriptors supply
// the OAuth defaults (token endpoint, client id, access-token env key) for
// accounts that did not set their own. A nil registry is ignored, leaving the
// built-in-only default in place.
func WithAccountProviders(registry *AccountProviderRegistry) AccountTokenManagerOption {
	return func(m *AccountTokenManager) {
		if registry != nil {
			m.providers = registry
		}
	}
}

// NewAccountTokenManager returns a manager backed by secrets (credential
// envelopes) and store (account bookkeeping: LastRefreshedAt / Status /
// LastError on refresh outcome). A zero skew selects
// DefaultAccountRefreshSkew, and an unconfigured provider registry selects
// DefaultAccountProviderRegistry.
func NewAccountTokenManager(secrets *AccountSecretStore, store AccountStore, skew time.Duration, opts ...AccountTokenManagerOption) *AccountTokenManager {
	if skew <= 0 {
		skew = DefaultAccountRefreshSkew
	}
	m := &AccountTokenManager{
		secrets:    secrets,
		store:      store,
		providers:  DefaultAccountProviderRegistry(),
		refreshers: make(map[AccountProvider]TokenRefresher),
		skew:       skew,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// RegisterRefresher installs the TokenRefresher used for provider's
// oauth_refresh accounts.
func (m *AccountTokenManager) RegisterRefresher(provider AccountProvider, r TokenRefresher) {
	if m.refreshers == nil {
		m.refreshers = make(map[AccountProvider]TokenRefresher)
	}
	m.refreshers[provider] = r
}

// RegisterOAuthRefreshers installs r for every oauth-capable provider in the
// manager's registry and returns the providers it was registered for.
//
// This is the one call that makes oauth_refresh accounts work at all: without
// it every renewal fails with AccountRefresherNotConfiguredError at spawn
// time. Server bring-up calls it once (see pkg/server/lifecycle_orchestration.go);
// tests call it to assert the same wiring without standing up a server.
func (m *AccountTokenManager) RegisterOAuthRefreshers(r TokenRefresher) []AccountProvider {
	if r == nil {
		return nil
	}
	providers := m.providers.OAuthCapableProviders()
	for _, provider := range providers {
		m.RegisterRefresher(provider, r)
	}
	return providers
}

// Refresher returns the TokenRefresher registered for provider, if any.
func (m *AccountTokenManager) Refresher(provider AccountProvider) (TokenRefresher, bool) {
	r, ok := m.refreshers[provider]
	return r, ok && r != nil
}

// Resolve returns a usable AccountCredential for account, refreshing it
// first when account.RefreshPolicy requires it and the stored credential is
// within the skew window of expiry (or already expired).
//
// The returned envelope is always the EFFECTIVE one: a copy of what is stored
// with each field the account omitted filled from its provider descriptor
// (see AccountProviderRegistry.Effective). That is what lets an account for a
// built-in provider omit access_token_env_key and still have its access token
// projected into the variable that provider's CLI reads.
//
// Static-token accounts skip the expiry check entirely and return the stored
// envelope unchanged apart from that projection — the manager never mutates or
// re-validates a credential it wasn't told needs renewing.
func (m *AccountTokenManager) Resolve(ctx context.Context, account *Account) (*AccountCredential, error) {
	if account == nil {
		return nil, &AccountInvalidError{Field: "account", Message: "must not be nil"}
	}
	stored, err := m.secrets.Get(ctx, account.TenantID, account.ID)
	if err != nil {
		return nil, err
	}
	effective := m.providers.Effective(account.Provider, stored)
	if !account.RefreshPolicy.IsOAuthRefresh() {
		return effective, nil
	}
	if credentialUsable(effective, m.skew) {
		return effective, nil
	}
	return m.renew(ctx, account)
}

// renew redeems the refresh credential and persists the rotated envelope.
// Single-flight per tenant+account: a second caller that arrives while a
// refresh is in flight blocks on the same lock and then re-reads the
// (already renewed) envelope instead of presenting the same refresh_token a
// second time, which an OAuth2 authorization server would reject as replay.
func (m *AccountTokenManager) renew(ctx context.Context, account *Account) (*AccountCredential, error) {
	lockKey := account.TenantID + ":" + account.ID
	unlock := m.locks.lock(lockKey)
	defer unlock()

	// Double-check under the lock: another caller may have renewed while we
	// waited.
	stored, err := m.secrets.Get(ctx, account.TenantID, account.ID)
	if err != nil {
		return nil, err
	}
	effective := m.providers.Effective(account.Provider, stored)
	if credentialUsable(effective, m.skew) {
		return effective, nil
	}

	refresher, ok := m.Refresher(account.Provider)
	if !ok {
		return nil, &AccountRefresherNotConfiguredError{Provider: account.Provider}
	}

	renewed, refreshErr := refresher.Refresh(ctx, effective)
	if refreshErr != nil {
		m.recordRefreshFailure(ctx, account, refreshErr.Error())
		return nil, &AccountRefreshFailedError{AccountID: account.ID, Cause: refreshErr}
	}
	if renewed == nil || renewed.AccessToken == "" {
		failErr := &AccountRefreshFailedError{AccountID: account.ID}
		m.recordRefreshFailure(ctx, account, "refresher returned an empty credential")
		return nil, failErr
	}

	// Persist the rotated pair BEFORE returning it. If the server rotated the
	// refresh_token and this write fails, the credential we would hand back
	// is unrecoverable on the next call (the presented refresh_token is
	// already dead server-side) — so a persist failure must fail the whole
	// renewal rather than silently handing out a token we cannot renew again.
	if putErr := m.secrets.Put(ctx, account.TenantID, account.ID, applyRenewal(stored, effective, renewed)); putErr != nil {
		m.recordRefreshFailure(ctx, account, "failed to persist renewed credential: "+putErr.Error())
		return nil, &AccountRefreshFailedError{AccountID: account.ID, Cause: putErr}
	}

	m.recordRefreshSuccess(ctx, account, renewed.ExpiresAt)
	return renewed, nil
}

// applyRenewal projects the fields a refresh actually produced back onto the
// STORED envelope.
//
// It exists so provider-registry defaults, which were injected into effective
// only for the duration of the call, are never written into the account: an
// account that omitted its token endpoint must still be omitting it after a
// renewal, so that changing the provider default in configuration keeps taking
// effect. Scopes are adopted only when the token endpoint actually reported a
// different set than the one we went in with.
func applyRenewal(stored, effective, renewed *AccountCredential) *AccountCredential {
	out := *stored
	out.AccessToken = renewed.AccessToken
	if renewed.RefreshToken != "" {
		out.RefreshToken = renewed.RefreshToken
	}
	out.ExpiresAt = renewed.ExpiresAt
	if !equalScopes(renewed.Scopes, effective.Scopes) {
		out.Scopes = renewed.Scopes
	}
	return &out
}

// equalScopes reports whether two scope lists hold the same values in the same
// order.
func equalScopes(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// recordRefreshSuccess best-effort updates the account's bookkeeping fields
// after a renewal the provider honored.
//
// expiresAt is mirrored onto the non-secret metadata record so
// Account.CredentialHealth can report the renewed credential's remaining life
// without any reader touching AccountSecretStore. A timestamp is not credential
// material. It is written even when zero — a token endpoint that reported no
// expires_in genuinely leaves the expiry unknown, and carrying the PREVIOUS
// cycle's expiry forward would describe a credential that no longer exists.
func (m *AccountTokenManager) recordRefreshSuccess(ctx context.Context, account *Account, expiresAt time.Time) {
	m.touchRefreshOutcome(ctx, account, TouchAccountOptions{
		MarkRefreshed:            true,
		MarkValidated:            true,
		SetStatusValid:           true,
		SetStatus:                AccountStatusActive,
		ClearLastError:           true,
		SetCredentialExpiresAt:   expiresAt,
		SetCredentialExpiryValid: true,
	})
}

// recordRefreshFailure best-effort records a renewal the provider refused.
// The recorded expiry is left alone: the stored credential was not replaced, so
// whatever expiry the record already carries is still the truth about it.
func (m *AccountTokenManager) recordRefreshFailure(ctx context.Context, account *Account, message string) {
	m.touchRefreshOutcome(ctx, account, TouchAccountOptions{
		MarkRefreshed:  true,
		SetStatusValid: true,
		SetStatus:      AccountStatusError,
		SetLastError:   message,
	})
}

// touchRefreshOutcome applies one refresh outcome's bookkeeping. A failure here
// never masks the refresh result itself — Resolve/renew already returned, or is
// about to return, based on the refresh outcome rather than on whether this
// write succeeded.
func (m *AccountTokenManager) touchRefreshOutcome(ctx context.Context, account *Account, opts TouchAccountOptions) {
	if m.store == nil {
		return
	}
	_ = m.store.Touch(ctx, account.TenantID, account.ID, opts)
}

// credentialUsable reports whether cred's OAuth2 access token has enough
// life left to be attached to a subprocess without needing renewal first.
func credentialUsable(cred *AccountCredential, skew time.Duration) bool {
	if cred == nil || cred.AccessToken == "" {
		return false
	}
	if cred.ExpiresAt.IsZero() {
		// No expiry recorded: treat as usable rather than needlessly
		// refreshing a credential whose provider never reported one.
		return true
	}
	return time.Now().Add(skew).Before(cred.ExpiresAt)
}
