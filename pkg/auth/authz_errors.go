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

package auth

import "errors"

// Authorization service sentinel errors.
var (
	// ErrNilProvider is returned when a nil provider is passed to NewAuthorizationService.
	ErrNilProvider = errors.New("authorization provider cannot be nil")

	// ErrNilLogger is returned when a nil logger is passed to NewAuthorizationService.
	ErrNilLogger = errors.New("logger cannot be nil")

	// ErrNilRequest is returned when a nil request is passed to Authorize.
	ErrNilRequest = errors.New("authorization request cannot be nil")

	// ErrNilRequestInBatch is returned when a batch contains a nil request.
	ErrNilRequestInBatch = errors.New("authorization request in batch cannot be nil")

	// ErrEmptyBatch is returned when an empty batch is passed to BatchAuthorize.
	ErrEmptyBatch = errors.New("authorization request batch cannot be empty")

	// ErrServiceClosed is returned when operations are attempted on a closed service.
	ErrServiceClosed = errors.New("authorization service is closed")
)

// ReservedKeyPrefixError is returned when a key uses a reserved internal prefix.
type ReservedKeyPrefixError struct {
	Prefix string
}

// Error implements the error interface.
func (e *ReservedKeyPrefixError) Error() string {
	return "key uses reserved internal prefix '" + e.Prefix + "'"
}

// AuthCauseError wraps an auth operation error with a cause.
type AuthCauseError struct {
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *AuthCauseError) Error() string {
	return e.Message + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AuthCauseError) Unwrap() error {
	return e.Cause
}
