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
	"errors"
	"strconv"
	"time"
)

// =============================================================================
// Account Sentinel Errors
// =============================================================================

var (
	// ErrAccountNotFound indicates the requested account does not exist.
	ErrAccountNotFound = errors.New("account: not found")

	// ErrAccountInvalid indicates an account failed validation.
	ErrAccountInvalid = errors.New("account: invalid")

	// ErrAccountStore indicates an internal account store failure.
	ErrAccountStore = errors.New("account: store error")

	// ErrAccountRevoked indicates the account cannot be used because it has
	// been revoked.
	ErrAccountRevoked = errors.New("account: revoked")

	// ErrAccountErrored indicates the account's last credential refresh failed
	// and it is held out of selection until an operator intervenes.
	ErrAccountErrored = errors.New("account: credential error")

	// ErrAccountProviderMismatch indicates a pinned account does not belong
	// to the requesting backend's provider.
	ErrAccountProviderMismatch = errors.New("account: provider mismatch")

	// ErrNoAccountAvailable indicates a selection strategy found no eligible
	// candidate account.
	ErrNoAccountAvailable = errors.New("account: no account available")

	// ErrAccountsCoolingDown indicates the pool is not empty — it holds
	// accounts for this provider — but every one of them is on a limit hold
	// that has not lapsed yet.
	//
	// A NARROWER CASE OF "no account available", AND IT IS THE OPPOSITE
	// SITUATION. An empty pool is a misconfiguration: nothing changes until an
	// operator registers an account. A pool that is entirely cooling down is
	// working exactly as designed and fixes itself at AvailableAt. Answering
	// them the same way strands work that would have been dispatchable minutes
	// later behind a human — see AccountsCoolingDownError.
	ErrAccountsCoolingDown = errors.New("account: all candidates are cooling down")

	// ErrAccountResolverNotConfigured indicates a spawn requested account
	// selection (AccountID or AccountAuto) but the backend was never wired
	// with an AccountResolver. This fails closed rather than silently
	// falling back to the host's shared identity, per policy: a caller that
	// asked for a specific account must never silently get a different one.
	ErrAccountResolverNotConfigured = errors.New("account: resolver not configured")

	// ErrAccountRefreshFailed indicates a token refresh attempt failed.
	ErrAccountRefreshFailed = errors.New("account: refresh failed")

	// ErrAccountRefresherNotConfigured indicates the account's RefreshPolicy
	// requires a TokenRefresher that was never registered for its provider.
	ErrAccountRefresherNotConfigured = errors.New("account: refresher not configured")

	// ErrAccountCredentialNotFound indicates the account's secret envelope is
	// missing from the secrets backend (metadata exists, credential does
	// not).
	ErrAccountCredentialNotFound = errors.New("account: credential not found")

	// ErrAccountLimited indicates the provider refused the account because it
	// hit a rate, quota, or usage limit.
	ErrAccountLimited = errors.New("account: rate/usage limited")

	// ErrSessionNotRotatable indicates a session cannot be moved onto a
	// different pooled account.
	ErrSessionNotRotatable = errors.New("account: session is not rotatable")

	// ErrSessionRotationUnsupported indicates the configured agent service does
	// not implement the rotation verb at all, as opposed to a particular
	// session being ineligible for it.
	ErrSessionRotationUnsupported = errors.New("account: session account rotation is not supported by this deployment")

	// ErrAccountTenantScope indicates a request for one tenant's account was
	// made under a context scoped to a DIFFERENT tenant. The durable KV routes
	// by the context's tenant, so honouring the request would read or write
	// the record in the wrong tenant's namespace — the mis-homing
	// go-quicbaas#535 measured. Refused instead.
	ErrAccountTenantScope = errors.New("account: context tenant does not match the account's tenant")
)

// AccountTenantScopeError reports a request for tenantID's account made under
// a context already scoped to ContextTenant. A namespaced store cannot serve
// it correctly: the key names one tenant and the context routes to another.
type AccountTenantScopeError struct {
	ContextTenant string
	TenantID      string
}

// Error implements the error interface.
func (e *AccountTenantScopeError) Error() string {
	return "account: context is scoped to tenant " + e.ContextTenant +
		" but the request names tenant " + e.TenantID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountTenantScopeError) Unwrap() error { return ErrAccountTenantScope }

// AccountNotFoundError reports a lookup miss, scoped to the tenant.
type AccountNotFoundError struct {
	TenantID  string
	AccountID string
}

// Error implements the error interface.
func (e *AccountNotFoundError) Error() string {
	return ErrAccountNotFound.Error() + ": tenant=" + e.TenantID + " id=" + e.AccountID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountNotFoundError) Unwrap() error { return ErrAccountNotFound }

// AccountInvalidError indicates an account failed validation.
type AccountInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *AccountInvalidError) Error() string {
	if e.Field != "" {
		return ErrAccountInvalid.Error() + ": " + e.Field + ": " + e.Message
	}
	return ErrAccountInvalid.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountInvalidError) Unwrap() error { return ErrAccountInvalid }

// AccountStoreError indicates an internal store failure (marshal, corruption, etc).
type AccountStoreError struct {
	Message string

	// Cause is the underlying failure, when the store had one to report.
	//
	// It exists because AccountSecretStore is a SEAM, not only a wrapper: a
	// deployment can back it with something other than the platform vault (the
	// agent daemon backs it with a read-through store over the control plane's
	// release endpoint, go-quicbaas#734). Flattening that store's typed refusal
	// into a message would leave every caller above it matching on substrings
	// to tell "the credential is revoked" from "the secrets backend is down".
	Cause error
}

// Error implements the error interface.
func (e *AccountStoreError) Error() string {
	return ErrAccountStore.Error() + ": " + e.Message
}

// Unwrap exposes both the sentinel — so errors.Is(err, ErrAccountStore) keeps
// working — and the underlying failure, so a caller can errors.As its way to
// the typed reason a pluggable store gave.
func (e *AccountStoreError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrAccountStore}
	}
	return []error{ErrAccountStore, e.Cause}
}

// AccountRevokedError indicates a selection or resolution attempt targeted a
// revoked account.
type AccountRevokedError struct {
	AccountID string
}

// Error implements the error interface.
func (e *AccountRevokedError) Error() string {
	return ErrAccountRevoked.Error() + ": id=" + e.AccountID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountRevokedError) Unwrap() error { return ErrAccountRevoked }

// AccountErroredError indicates a resolution attempt targeted an account whose
// last credential refresh failed (AccountStatusError).
//
// It is deliberately distinct from AccountLimitedError: a limited account has a
// working credential that is merely busy and rejoins the pool on its own, while
// an errored account has a broken credential and needs a human. Answering the
// two the same way would either strand recoverable accounts or hide real
// breakage behind an automatic retry.
type AccountErroredError struct {
	// AccountID is the account that was refused.
	AccountID string

	// LastError is the account's recorded failure reason, already redacted by
	// the refresher that stored it. Empty when none was recorded.
	LastError string
}

// Error implements the error interface.
func (e *AccountErroredError) Error() string {
	msg := ErrAccountErrored.Error() + ": id=" + e.AccountID
	if e.LastError != "" {
		msg += ": " + e.LastError
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountErroredError) Unwrap() error { return ErrAccountErrored }

// AccountProviderMismatchError indicates a pinned account's provider does not
// match the requesting backend.
type AccountProviderMismatchError struct {
	AccountID       string
	AccountProvider AccountProvider
	RequestProvider AccountProvider
}

// Error implements the error interface.
func (e *AccountProviderMismatchError) Error() string {
	return ErrAccountProviderMismatch.Error() + ": id=" + e.AccountID +
		" account_provider=" + string(e.AccountProvider) + " requested_provider=" + string(e.RequestProvider)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountProviderMismatchError) Unwrap() error { return ErrAccountProviderMismatch }

// NoAccountAvailableError indicates a selection strategy found no eligible
// candidate.
type NoAccountAvailableError struct {
	TenantID  string
	ProjectID string
	Provider  AccountProvider
	Strategy  string
}

// Error implements the error interface.
func (e *NoAccountAvailableError) Error() string {
	return ErrNoAccountAvailable.Error() + ": tenant=" + e.TenantID +
		" project=" + e.ProjectID + " provider=" + string(e.Provider) + " strategy=" + e.Strategy
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *NoAccountAvailableError) Unwrap() error { return ErrNoAccountAvailable }

// AccountsCoolingDownError reports a pool whose every candidate is on a limit
// hold that has not lapsed.
//
// IT UNWRAPS TO BOTH SENTINELS, deliberately. Every existing caller asks
// errors.Is(err, ErrNoAccountAvailable) and is still right — there is, at this
// instant, no account available. What the second sentinel adds is WHY, for the
// one caller that must tell the two apart: the SDLC module decides on its
// strength whether to park the work item for a human or leave it in the queue
// for the next sweep. A caller that does not care goes on not caring.
//
// AvailableAt is the EARLIEST moment any candidate rejoins the pool, so a
// caller with somewhere to put it can wait exactly that long. An account held
// with no stated expiry is not counted at all — see coolingDown — because a
// hold nobody promised to lift is not a condition to wait on, and a caller
// told to wait forever is worse off than one told to fetch a human.
type AccountsCoolingDownError struct {
	TenantID  string
	ProjectID string
	Provider  AccountProvider

	// Candidates is how many accounts were held out. It distinguishes "your
	// one account is busy" from "all nine are", which are different problems.
	Candidates int

	// AvailableAt is the earliest moment one of them rejoins the pool.
	//
	// NEVER ZERO on an error this package raises: a hold with no stated expiry
	// is not self-clearing at all and is excluded from the count, so an error
	// that exists always names a time. Zero is reachable only on an error
	// reconstructed from a wire message that carried none.
	AvailableAt time.Time
}

// Error implements the error interface.
func (e *AccountsCoolingDownError) Error() string {
	msg := ErrAccountsCoolingDown.Error() + ": tenant=" + e.TenantID +
		" project=" + e.ProjectID + " provider=" + string(e.Provider) +
		" candidates=" + strconv.Itoa(e.Candidates)
	if !e.AvailableAt.IsZero() {
		msg += " available_at=" + e.AvailableAt.UTC().Format(time.RFC3339)
	}
	return msg
}

// Unwrap returns both sentinels: the general condition every caller already
// tests for, and the specific one that distinguishes a cooldown from an empty
// pool.
func (e *AccountsCoolingDownError) Unwrap() []error {
	return []error{ErrNoAccountAvailable, ErrAccountsCoolingDown}
}

// AccountResolverNotConfiguredError indicates account selection was
// requested but no AccountResolver was wired into the backend.
type AccountResolverNotConfiguredError struct {
	Backend string
}

// Error implements the error interface.
func (e *AccountResolverNotConfiguredError) Error() string {
	return ErrAccountResolverNotConfigured.Error() + ": backend=" + e.Backend
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountResolverNotConfiguredError) Unwrap() error {
	return ErrAccountResolverNotConfigured
}

// AccountRefreshFailedError wraps the underlying cause of a failed token
// refresh.
type AccountRefreshFailedError struct {
	AccountID string
	Cause     error
}

// Error implements the error interface.
func (e *AccountRefreshFailedError) Error() string {
	msg := ErrAccountRefreshFailed.Error() + ": id=" + e.AccountID
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns both the sentinel and the underlying cause for errors.Is /
// errors.As support.
func (e *AccountRefreshFailedError) Unwrap() []error {
	if e.Cause != nil {
		return []error{ErrAccountRefreshFailed, e.Cause}
	}
	return []error{ErrAccountRefreshFailed}
}

// AccountRefresherNotConfiguredError indicates no TokenRefresher is
// registered for the account's provider.
type AccountRefresherNotConfiguredError struct {
	Provider AccountProvider
}

// Error implements the error interface.
func (e *AccountRefresherNotConfiguredError) Error() string {
	return ErrAccountRefresherNotConfigured.Error() + ": provider=" + string(e.Provider)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountRefresherNotConfiguredError) Unwrap() error {
	return ErrAccountRefresherNotConfigured
}

// AccountCredentialNotFoundError indicates the account's secret envelope is
// missing.
type AccountCredentialNotFoundError struct {
	TenantID  string
	AccountID string
}

// Error implements the error interface.
func (e *AccountCredentialNotFoundError) Error() string {
	return ErrAccountCredentialNotFound.Error() + ": tenant=" + e.TenantID + " id=" + e.AccountID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountCredentialNotFoundError) Unwrap() error { return ErrAccountCredentialNotFound }

// AccountLimitedError reports that a provider refused an account because it hit
// a rate, quota, or usage limit.
//
// It is the one account failure that is BOTH transient and actionable without
// an operator: the credential is fine, it is just busy, so the orchestrator
// answers it by moving the session to another account in the pool rather than
// by failing the conversation. AvailableAt is what makes that decision
// automatic — it names the moment this account rejoins the pool.
type AccountLimitedError struct {
	// AccountID is the account the provider refused. Empty when the limit was
	// classified from a signal that named no account (see
	// AccountLimitClassifier).
	AccountID string

	// Provider is the backend whose CLI reported the limit.
	Provider AccountProvider

	// RetryAfter is the provider's own hint for how long to wait, or zero when
	// it supplied none and a default backoff was applied instead.
	RetryAfter time.Duration

	// AvailableAt is when the account may be used again.
	AvailableAt time.Time

	// Signal is the evidence the classifier matched on, truncated and stripped
	// of anything credential-shaped before it is stored (see
	// redactLimitSignal). It exists so an operator can tell a genuine provider
	// limit from a heuristic false positive.
	Signal string
}

// Error implements the error interface.
func (e *AccountLimitedError) Error() string {
	msg := ErrAccountLimited.Error() + ": id=" + e.AccountID
	if e.Provider != "" {
		msg += " provider=" + string(e.Provider)
	}
	if !e.AvailableAt.IsZero() {
		msg += " available_at=" + e.AvailableAt.UTC().Format(time.RFC3339)
	}
	if e.RetryAfter > 0 {
		msg += " retry_after=" + strconv.FormatInt(int64(e.RetryAfter.Seconds()), 10) + "s"
	}
	if e.Signal != "" {
		msg += ": " + e.Signal
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountLimitedError) Unwrap() error { return ErrAccountLimited }

// SessionNotRotatableError reports a rotation request against a session that
// may not change pooled accounts.
//
// A session that never used the pool has nothing to rotate, and a session
// pinned to a named account was pinned deliberately — silently moving it would
// defeat the pin. Rotation therefore requires an explicit policy: either the
// spawn asked for strategy-based selection (AccountAuto) or it named the pool
// to rotate through (AccountAllow).
type SessionNotRotatableError struct {
	// SessionID identifies the session.
	SessionID string

	// Reason states which precondition was missing.
	Reason string
}

// Error implements the error interface.
func (e *SessionNotRotatableError) Error() string {
	return ErrSessionNotRotatable.Error() + ": session=" + e.SessionID + ": " + e.Reason
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionNotRotatableError) Unwrap() error { return ErrSessionNotRotatable }

// SessionRotationUnsupportedError reports that the AgentService wired into a
// transport does not implement SessionAccountRotationService.
//
// It is deliberately distinct from SessionNotRotatableError: that one says
// "this session may not move", this one says "no session can move here". A
// transport surfaces it as an unimplemented/501 answer rather than a client
// error, because nothing the caller changes about the request would help.
type SessionRotationUnsupportedError struct {
	// Transport names the surface that could not reach the verb, so an
	// operator reading the log knows where the gap is.
	Transport string
}

// Error implements the error interface.
func (e *SessionRotationUnsupportedError) Error() string {
	if e.Transport == "" {
		return ErrSessionRotationUnsupported.Error()
	}
	return ErrSessionRotationUnsupported.Error() + ": transport=" + e.Transport
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SessionRotationUnsupportedError) Unwrap() error { return ErrSessionRotationUnsupported }
