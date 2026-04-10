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

// Package service provides the unified service facade for QuicBaaS.
package service

import "context"

// =============================================================================
// PIN Service - PIN Management Operations
// =============================================================================

// PINService handles Security Officer (SO) and User PIN management for
// hardware security tokens and smart cards. PINs control access to
// cryptographic operations and key material stored in secure hardware.
//
// PIN operations follow PKCS#11 and PIV conventions:
//   - Security Officer PIN (SO PIN) provides administrative control
//   - User PIN provides operational access to keys and certificates
//   - PIN lockout protects against brute-force attacks
//   - SO PIN authorization is required for User PIN management
//
// The lockout mechanism tracks failed authentication attempts and
// temporarily disables PIN verification after too many failures.
// The SO PIN can be used to reset the lockout counter.
//
// All operations delegate to the underlying go-xkms PINService, with
// QuicBaaS-specific tenant context injection and access control enforcement.
type PINService interface {
	// SetSOPIN sets the Security Officer PIN. If the SO PIN is already set,
	// the current SO PIN must be provided for authorization. This is typically
	// called during initial device provisioning.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the current SO PIN (if set) and the new SO PIN
	// Returns:
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrUnavailable, ErrTimeout
	SetSOPIN(ctx context.Context, req *PINSetSOPINRequest) error

	// SetUserPIN sets the user PIN. Requires SO PIN authorization to prevent
	// unauthorized PIN changes. This is typically called during device provisioning
	// or after a lockout reset.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the SO PIN for authorization and the new user PIN
	// Returns:
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrUnavailable, ErrTimeout
	SetUserPIN(ctx context.Context, req *PINSetUserPINRequest) error

	// ChangeSOPIN changes the Security Officer PIN. The current SO PIN must be
	// provided for authentication before the change is applied.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the current SO PIN and the new SO PIN
	// Returns:
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrUnavailable, ErrTimeout
	ChangeSOPIN(ctx context.Context, req *PINChangeSOPINRequest) error

	// ChangeUserPIN changes the user PIN. The current user PIN must be
	// provided for authentication before the change is applied.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the current user PIN and the new user PIN
	// Returns:
	//   - error: ErrInvalidInput, ErrUnauthorized, ErrUnavailable, ErrTimeout
	ChangeUserPIN(ctx context.Context, req *PINChangeUserPINRequest) error

	// VerifySOPIN verifies the Security Officer PIN without performing any
	// other operation. This can be used to check PIN correctness before
	// sensitive operations.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the SO PIN to verify
	// Returns:
	//   - error: ErrUnauthorized (wrong PIN), ErrUnavailable, ErrTimeout
	VerifySOPIN(ctx context.Context, req *PINVerifySOPINRequest) error

	// VerifyUserPIN verifies the user PIN without performing any other operation.
	// A failed verification increments the lockout counter.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the user PIN to verify
	// Returns:
	//   - error: ErrUnauthorized (wrong PIN), ErrUnavailable, ErrTimeout
	VerifyUserPIN(ctx context.Context, req *PINVerifyUserPINRequest) error

	// GetLockoutStatus returns the current PIN lockout status including
	// the number of failed attempts, maximum allowed attempts, and whether
	// the device is currently locked out.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request for lockout status (supports tenant scoping)
	// Returns:
	//   - PINLockoutStatusResponse: Contains lockout state and counters
	//   - error: ErrUnavailable, ErrUnauthorized, ErrTimeout
	GetLockoutStatus(ctx context.Context, req *PINGetLockoutStatusRequest) (*PINLockoutStatusResponse, error)

	// ResetLockout resets the PIN lockout counter using SO PIN authorization.
	// This unlocks the device after a lockout caused by too many failed PIN
	// verification attempts.
	// Parameters:
	//   - ctx: Request context with timeout, tenant, and authentication metadata
	//   - req: Request containing the SO PIN for authorization
	// Returns:
	//   - error: ErrUnauthorized (wrong SO PIN), ErrUnavailable, ErrTimeout
	ResetLockout(ctx context.Context, req *PINResetLockoutRequest) error
}

// =============================================================================
// PIN Service Request/Response Types
// =============================================================================

// PINSetSOPINRequest contains parameters for setting the Security Officer PIN.
type PINSetSOPINRequest struct {
	// CurrentSOPIN is the current SO PIN for authorization. Empty string if the
	// SO PIN has not been set yet (first-time provisioning).
	CurrentSOPIN string

	// NewSOPIN is the new Security Officer PIN to set.
	NewSOPIN string
}

// PINSetUserPINRequest contains parameters for setting the user PIN.
type PINSetUserPINRequest struct {
	// SOPIN is the Security Officer PIN for authorization.
	SOPIN string

	// NewUserPIN is the new user PIN to set.
	NewUserPIN string
}

// PINChangeSOPINRequest contains parameters for changing the Security Officer PIN.
type PINChangeSOPINRequest struct {
	// CurrentSOPIN is the current Security Officer PIN for authentication.
	CurrentSOPIN string

	// NewSOPIN is the new Security Officer PIN.
	NewSOPIN string
}

// PINChangeUserPINRequest contains parameters for changing the user PIN.
type PINChangeUserPINRequest struct {
	// CurrentUserPIN is the current user PIN for authentication.
	CurrentUserPIN string

	// NewUserPIN is the new user PIN.
	NewUserPIN string
}

// PINVerifySOPINRequest contains parameters for verifying the Security Officer PIN.
type PINVerifySOPINRequest struct {
	// SOPIN is the Security Officer PIN to verify.
	SOPIN string
}

// PINVerifyUserPINRequest contains parameters for verifying the user PIN.
type PINVerifyUserPINRequest struct {
	// UserPIN is the user PIN to verify.
	UserPIN string
}

// PINGetLockoutStatusRequest contains parameters for retrieving lockout status.
type PINGetLockoutStatusRequest struct {
	// TenantID optionally scopes this operation to a specific tenant.
	TenantID string
}

// PINLockoutStatusResponse contains the current PIN lockout status.
type PINLockoutStatusResponse struct {
	// FailedAttempts is the number of consecutive failed PIN verification attempts.
	FailedAttempts int

	// MaxAttempts is the maximum number of failed attempts before lockout.
	MaxAttempts int

	// IsLocked indicates whether the device is currently locked out.
	IsLocked bool

	// LockoutUntil is the timestamp when the lockout expires, in RFC 3339 format.
	// Empty if the device is not locked or if lockout is permanent until SO reset.
	LockoutUntil string

	// RecoverySeconds is the number of seconds remaining until the lockout
	// automatically expires. Zero if not locked or requires SO PIN reset.
	RecoverySeconds int
}

// PINResetLockoutRequest contains parameters for resetting the PIN lockout.
type PINResetLockoutRequest struct {
	// SOPIN is the Security Officer PIN for authorization.
	SOPIN string
}

// =============================================================================
// PIN Service Error Types
// =============================================================================

// PINNotAvailableError indicates that the PIN service is not configured
// or the underlying hardware backend does not support PIN operations.
type PINNotAvailableError struct{}

// Error implements the error interface.
func (e *PINNotAvailableError) Error() string {
	return "PIN service not available"
}

// Is supports errors.Is matching by type.
func (e *PINNotAvailableError) Is(target error) bool {
	_, ok := target.(*PINNotAvailableError)
	return ok
}

// PINVerificationError indicates that a PIN verification attempt failed
// because the provided PIN was incorrect.
type PINVerificationError struct {
	PINType string // "SO" or "User"
}

// Error implements the error interface.
func (e *PINVerificationError) Error() string {
	return e.PINType + " PIN verification failed"
}

// Is supports errors.Is matching by type.
func (e *PINVerificationError) Is(target error) bool {
	_, ok := target.(*PINVerificationError)
	return ok
}

// PINLockedOutError indicates that the device is locked out due to too many
// failed PIN verification attempts.
type PINLockedOutError struct {
	// RecoverySeconds is the number of seconds until the lockout expires.
	// Zero indicates the lockout requires SO PIN reset.
	RecoverySeconds int
}

// Error implements the error interface.
func (e *PINLockedOutError) Error() string {
	if e.RecoverySeconds > 0 {
		return "PIN locked out, automatic recovery pending"
	}
	return "PIN locked out, SO PIN reset required"
}

// Is supports errors.Is matching by type.
func (e *PINLockedOutError) Is(target error) bool {
	_, ok := target.(*PINLockedOutError)
	return ok
}

// PINChangeError indicates that a PIN change operation failed.
type PINChangeError struct {
	PINType string // "SO" or "User"
	Cause   error
}

// Error implements the error interface.
func (e *PINChangeError) Error() string {
	return e.PINType + " PIN change failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *PINChangeError) Unwrap() error {
	return e.Cause
}

// PINPolicyViolationError indicates that a PIN does not meet the required
// complexity or length policy.
type PINPolicyViolationError struct {
	Detail string
}

// Error implements the error interface.
func (e *PINPolicyViolationError) Error() string {
	if e.Detail != "" {
		return "PIN policy violation: " + e.Detail
	}
	return "PIN does not meet policy requirements"
}

// Is supports errors.Is matching by type.
func (e *PINPolicyViolationError) Is(target error) bool {
	_, ok := target.(*PINPolicyViolationError)
	return ok
}
