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

package iam

import (
	"errors"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// AppRecordCorruptError reports a stored application record that does not
// decode. It is the client's record that is broken, not the store, so client
// authentication treats it like an unknown client: one derivation, then
// invalid_client.
type AppRecordCorruptError struct {
	ClientID string
	Cause    error
}

// Error implements the error interface. It names the client id, never a secret.
func (e *AppRecordCorruptError) Error() string {
	return "application record for client " + e.ClientID + " does not decode: " + errString(e.Cause)
}

// Unwrap returns the decode failure.
func (e *AppRecordCorruptError) Unwrap() error { return e.Cause }

// ClientStoreUnavailableError reports that client authentication could not
// read the client's record because the store failed. It says nothing about
// the client, so the token endpoint answers server_error, not invalid_client.
type ClientStoreUnavailableError struct {
	ClientID string
	Cause    error
}

// Error implements the error interface. It names the client id, never a secret.
func (e *ClientStoreUnavailableError) Error() string {
	return "client store unavailable while authenticating client " + e.ClientID + ": " + errString(e.Cause)
}

// Unwrap returns the store failure.
func (e *ClientStoreUnavailableError) Unwrap() error { return e.Cause }

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// clientAuthOAuthError is the OAuth error for a failed client authentication:
// server_error when the client store could not be read, so a client is never
// told its credentials are wrong during an outage, and invalid_client for
// every client-attributable failure (unknown, corrupt, empty, mismatched or
// rotation-required), which stay indistinguishable from each other.
func clientAuthOAuthError(err error) *OAuthError {
	if errors.As(err, new(*ClientStoreUnavailableError)) {
		return NewOAuthError(ErrOAuthServerError, "client authentication is temporarily unavailable")
	}
	return NewOAuthError(ErrOAuthInvalidClient, "client authentication failed")
}

// clientLoadOAuthError is the OAuth error for a failed read of a client that
// has already authenticated: invalid_client when the record is gone or does
// not decode, server_error when the store could not be read.
func clientLoadOAuthError(err error) *OAuthError {
	if dberrors.IsNotFound(err) || errors.As(err, new(*AppRecordCorruptError)) {
		return NewOAuthError(ErrOAuthInvalidClient, "invalid client")
	}
	return NewOAuthError(ErrOAuthServerError, "failed to load client")
}
