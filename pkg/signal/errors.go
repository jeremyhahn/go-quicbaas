//go:build quicbaas_signal

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package signal

// SignalNotEnabledError is returned when Signal messaging is not enabled
// for the requesting tenant.
type SignalNotEnabledError struct {
	TenantID string
}

// Error implements the error interface.
func (e *SignalNotEnabledError) Error() string {
	return "signal: not enabled for tenant " + e.TenantID
}

// TenantNotFoundError is returned when a tenant configuration cannot be
// located in the backing store.
type TenantNotFoundError struct {
	TenantID string
}

// Error implements the error interface.
func (e *TenantNotFoundError) Error() string {
	return "signal: tenant not found: " + e.TenantID
}

// TenantAlreadyProvisionedError is returned when attempting to provision a
// tenant that already has an active Signal configuration.
type TenantAlreadyProvisionedError struct {
	TenantID string
}

// Error implements the error interface.
func (e *TenantAlreadyProvisionedError) Error() string {
	return "signal: tenant already provisioned: " + e.TenantID
}

// AccountQuotaExceededError is returned when a tenant's account registration
// would exceed the configured maximum account limit.
type AccountQuotaExceededError struct {
	TenantID    string
	MaxAccounts int
}

// Error implements the error interface.
func (e *AccountQuotaExceededError) Error() string {
	return "signal: account quota exceeded for tenant " + e.TenantID
}

// InvalidConfigError is returned when a Signal tenant configuration fails
// validation.
type InvalidConfigError struct {
	Field  string
	Reason string
}

// Error implements the error interface.
func (e *InvalidConfigError) Error() string {
	return "signal: invalid config: " + e.Field + ": " + e.Reason
}

// KeyGenerationError is returned when cryptographic key generation fails
// during account registration.
type KeyGenerationError struct {
	KeyType string
	Err     error
}

// Error implements the error interface.
func (e *KeyGenerationError) Error() string {
	return "signal: failed to generate " + e.KeyType + " key: " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *KeyGenerationError) Unwrap() error {
	return e.Err
}
