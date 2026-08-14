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

import "errors"

// =============================================================================
// Provider-registry and credential-mode errors.
//
// These are the write-time failures the account subsystem now rejects instead
// of accepting and failing later at spawn. They follow the same
// sentinel + typed-error pairing as account_errors.go so callers can either
// errors.Is a category or errors.As the detail.
// =============================================================================

var (
	// ErrAccountCredentialModeInvalid indicates a credential mode (spelled
	// "refresh_policy" on the transports) that is neither AccountCredentialMode
	// value nor one of their transport aliases.
	ErrAccountCredentialModeInvalid = errors.New("account: credential mode invalid")

	// ErrAccountProviderEndpointUnresolved indicates an oauth_refresh account
	// resolves to no token endpoint at all: neither the account's own
	// AccountCredential.TokenEndpoint nor its provider descriptor supplies one,
	// so the account could never be renewed.
	ErrAccountProviderEndpointUnresolved = errors.New("account: provider token endpoint unresolved")

	// ErrAccountRefreshTokenRequired indicates an oauth_refresh account was
	// written without the refresh credential that mode exists to redeem.
	ErrAccountRefreshTokenRequired = errors.New("account: refresh token required")

	// ErrAccountStaticTokenRefreshable indicates a static_token account was
	// written holding a refresh token nothing will ever redeem.
	ErrAccountStaticTokenRefreshable = errors.New("account: static token credential must not carry a refresh token")

	// ErrAccountProviderDuplicate indicates the same provider was declared more
	// than once when building an AccountProviderRegistry.
	ErrAccountProviderDuplicate = errors.New("account: duplicate provider descriptor")
)

// AccountCredentialModeInvalidError reports an unrecognized credential mode.
type AccountCredentialModeInvalidError struct {
	Mode string
}

// Error implements the error interface.
func (e *AccountCredentialModeInvalidError) Error() string {
	return ErrAccountCredentialModeInvalid.Error() + ": " + e.Mode
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountCredentialModeInvalidError) Unwrap() error { return ErrAccountCredentialModeInvalid }

// AccountProviderEndpointUnresolvedError reports an oauth_refresh account with
// no resolvable token endpoint.
type AccountProviderEndpointUnresolvedError struct {
	Provider AccountProvider
}

// Error implements the error interface.
func (e *AccountProviderEndpointUnresolvedError) Error() string {
	return ErrAccountProviderEndpointUnresolved.Error() + ": provider=" + string(e.Provider) +
		": set the account's token_endpoint or configure orchestration.account_providers for this provider"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountProviderEndpointUnresolvedError) Unwrap() error {
	return ErrAccountProviderEndpointUnresolved
}

// AccountRefreshTokenRequiredError reports an oauth_refresh account written
// without a refresh token.
type AccountRefreshTokenRequiredError struct {
	Provider AccountProvider
}

// Error implements the error interface.
func (e *AccountRefreshTokenRequiredError) Error() string {
	return ErrAccountRefreshTokenRequired.Error() + ": provider=" + string(e.Provider)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountRefreshTokenRequiredError) Unwrap() error { return ErrAccountRefreshTokenRequired }

// AccountStaticTokenRefreshableError reports a static_token account written
// with a refresh token.
type AccountStaticTokenRefreshableError struct {
	Provider AccountProvider
}

// Error implements the error interface.
func (e *AccountStaticTokenRefreshableError) Error() string {
	return ErrAccountStaticTokenRefreshable.Error() + ": provider=" + string(e.Provider)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountStaticTokenRefreshableError) Unwrap() error { return ErrAccountStaticTokenRefreshable }

// AccountProviderDuplicateError reports a provider declared twice when
// building an AccountProviderRegistry.
type AccountProviderDuplicateError struct {
	Provider AccountProvider
}

// Error implements the error interface.
func (e *AccountProviderDuplicateError) Error() string {
	return ErrAccountProviderDuplicate.Error() + ": provider=" + string(e.Provider)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AccountProviderDuplicateError) Unwrap() error { return ErrAccountProviderDuplicate }
