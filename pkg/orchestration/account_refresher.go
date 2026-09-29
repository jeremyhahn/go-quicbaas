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
	"errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/metrics"
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
	logger     interfaces.Logger

	// secretRefs reads the credential of an account that names a
	// CredentialSecretRef. Optional; see WithAccountSecretResolver.
	secretRefs AccountSecretResolver
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

// WithAccountTokenLogger installs the logger a failed refresh-outcome stamp is
// reported on. A nil logger is ignored; the failure is still counted and
// returned to the caller.
func WithAccountTokenLogger(logger interfaces.Logger) AccountTokenManagerOption {
	return func(m *AccountTokenManager) {
		if logger != nil {
			m.logger = logger
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
// An account that names a CredentialSecretRef is read from that tenant secret
// instead of its own envelope; see account_secret_ref.go.
//
// Static-token accounts skip the expiry check entirely and return the stored
// envelope unchanged apart from that projection — the manager never mutates or
// re-validates a credential it wasn't told needs renewing.
func (m *AccountTokenManager) Resolve(ctx context.Context, account *Account) (*AccountCredential, error) {
	if account == nil {
		return nil, &AccountInvalidError{Field: "account", Message: "must not be nil"}
	}
	// A referenced credential is read fresh from the tenant secret every
	// time and is never renewed here (write-time validation keeps a reference
	// off every oauth_refresh account), so writing the secret is the rotation.
	if account.UsesSecretRef() {
		return m.resolveSecretRef(ctx, account)
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
		stampErr := m.recordRefreshFailure(ctx, account, refreshErr.Error())
		return nil, &AccountRefreshFailedError{AccountID: account.ID, Cause: withStampFailure(refreshErr, stampErr)}
	}
	if renewed == nil || renewed.AccessToken == "" {
		stampErr := m.recordRefreshFailure(ctx, account, "refresher returned an empty credential")
		return nil, &AccountRefreshFailedError{AccountID: account.ID, Cause: stampErr}
	}

	// Persist the rotated pair BEFORE returning it. If the server rotated the
	// refresh_token and this write fails, the credential we would hand back
	// is unrecoverable on the next call (the presented refresh_token is
	// already dead server-side) — so a persist failure must fail the whole
	// renewal rather than silently handing out a token we cannot renew again.
	generation, putErr := m.secrets.Replace(ctx, account.TenantID, account.ID, applyRenewal(stored, effective, renewed))
	if putErr != nil {
		stampErr := m.recordRefreshFailure(ctx, account, "failed to persist renewed credential: "+putErr.Error())
		return nil, &AccountRefreshFailedError{AccountID: account.ID, Cause: withStampFailure(putErr, stampErr)}
	}

	// THE CALLER LEAVES HOLDING THE IDENTITY OF WHAT WAS STORED, not just its
	// bytes. recordRefreshSuccess below mirrors the same generation onto the
	// metadata record; carrying the stamp out on the envelope is what lets the
	// resolution above this one record it on the session it spawns
	// (go-quicbaas#997).
	renewed.Generation = generation

	// The success stamp is conditional on the account STILL holding its
	// credential inline. A switch to a secret reference can commit while the
	// provider round trip is in flight; it destroys the inline envelope, which
	// the Replace above has then re-written. Stamping regardless would put the
	// retired expiry back on a reference account and leave the envelope
	// behind, so a refused stamp removes the envelope instead and the renewed
	// credential is not used: the account now reads its secret.
	//
	// Any other stamp failure is not reported as a success either: the record
	// would otherwise go on describing the previous credential (its expiry,
	// its generation, and any error status from an earlier failed refresh)
	// with no trace of why. The renewed envelope IS persisted, so the next
	// Resolve finds it usable and hands it out without another round trip to
	// the token endpoint, and reconciles the generation mirror from it.
	if stampErr := m.recordRefreshSuccess(ctx, account, renewed.ExpiresAt, generation); stampErr != nil {
		var switched *AccountCredentialSourceChangedError
		if errors.As(stampErr, &switched) {
			if cleanupErr := m.secrets.Delete(ctx, account.TenantID, account.ID); cleanupErr != nil {
				return nil, &AccountStoreError{
					Message: "renewal overtaken by a switch to a secret reference left its envelope behind: " +
						cleanupErr.Error(),
					Cause: switched,
				}
			}
			return nil, switched
		}
		return nil, stampErr
	}
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

// recordRefreshSuccess updates the account's bookkeeping fields
// after a renewal the provider honored.
//
// expiresAt is mirrored onto the non-secret metadata record so
// Account.CredentialHealth can report the renewed credential's remaining life
// without any reader touching AccountSecretStore. A timestamp is not credential
// material. It is written even when zero — a token endpoint that reported no
// expires_in genuinely leaves the expiry unknown, and carrying the PREVIOUS
// cycle's expiry forward would describe a credential that no longer exists.
//
// generation is the stamp AccountSecretStore.Replace wrote onto the envelope
// that is now stored; mirroring it is what lets SessionAccountFreshness see
// the rotation without reading secret material.
//
// It is conditional on the account still being inline (see renew) and not
// revoked. A switch to a secret reference is returned as the store's
// *AccountCredentialSourceChangedError so renew can act on it; a refusal
// because the account was revoked meanwhile is the intended outcome and is not
// an error; any other failure is an *AccountRefreshStampFailedError.
func (m *AccountTokenManager) recordRefreshSuccess(
	ctx context.Context, account *Account, expiresAt time.Time, generation int64,
) error {
	return m.touchRefreshOutcome(ctx, account, AccountRefreshOutcomeSuccess, TouchAccountOptions{
		MarkRefreshed:            true,
		MarkCredentialRotated:    true,
		MarkValidated:            true,
		SetStatusValid:           true,
		SetStatus:                AccountStatusActive,
		ClearLastError:           true,
		SetCredentialGeneration:  generation,
		SetCredentialExpiresAt:   expiresAt,
		SetCredentialExpiryValid: true,
		RequireInlineCredential:  true,
		PreserveRevoked:          true,
	})
}

// recordRefreshFailure records a renewal the provider refused.
// The recorded expiry is left alone: the stored credential was not replaced, so
// whatever expiry the record already carries is still the truth about it.
//
// Like the success stamp it lands only on an account that is still inline and
// not revoked: a failure of the OLD credential says nothing about an account
// that has since moved to a secret reference, so either refusal is the
// intended outcome and not an error. Any other failure is returned as an
// *AccountRefreshStampFailedError for renew to join to the refresh error.
func (m *AccountTokenManager) recordRefreshFailure(ctx context.Context, account *Account, message string) error {
	err := m.touchRefreshOutcome(ctx, account, AccountRefreshOutcomeFailure, TouchAccountOptions{
		MarkRefreshed:           true,
		SetStatusValid:          true,
		SetStatus:               AccountStatusError,
		SetLastError:            message,
		RequireInlineCredential: true,
		PreserveRevoked:         true,
	})
	var switched *AccountCredentialSourceChangedError
	if errors.As(err, &switched) {
		return nil
	}
	return err
}

// touchRefreshOutcome applies one refresh outcome's bookkeeping. A write the
// store refused on a precondition the caller asked for — the credential
// source changed (*AccountCredentialSourceChangedError, returned as is so the
// caller can act on it) or the account was revoked (not an error: the stamp
// must never replace revoked) — is not a failure. Any other failure is
// logged with the account it concerns and counted, then returned as an
// *AccountRefreshStampFailedError, so it is visible even to a caller that only
// reports the refresh error it wraps.
//
// A failed stamp never masks the refresh outcome itself: renew joins it to the
// refresh failure it was recording, and turns a renewal it was recording into
// a failure rather than a success.
func (m *AccountTokenManager) touchRefreshOutcome(ctx context.Context, account *Account, outcome string, opts TouchAccountOptions) error {
	if m.store == nil {
		return nil
	}
	touchErr := m.store.Touch(ctx, account.TenantID, account.ID, opts)
	if touchErr == nil {
		return nil
	}
	var switched *AccountCredentialSourceChangedError
	if errors.As(touchErr, &switched) {
		return switched
	}
	var revoked *AccountRevokedError
	if opts.PreserveRevoked && errors.As(touchErr, &revoked) {
		return nil
	}
	if m.logger != nil {
		m.logger.Warnf("orchestration: failed to record refresh %s on account %s (tenant %s): %s",
			outcome, account.ID, account.TenantID, touchErr.Error())
	}
	metrics.GetMetrics().RecordOrchAccountRefreshStampFailure(account.TenantID, outcome)
	return &AccountRefreshStampFailedError{AccountID: account.ID, Outcome: outcome, Cause: touchErr}
}

// withStampFailure returns cause unchanged when the outcome stamp succeeded,
// and cause joined with the stamp failure when it did not, so a refresh error
// keeps its exact shape unless there is a second failure to report.
func withStampFailure(cause, stampErr error) error {
	if stampErr == nil {
		return cause
	}
	return errors.Join(cause, stampErr)
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
