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

// Package secrets provides secret resolution for configuration values.
// It enables configuration files to reference secrets stored in KMS providers
// using URI schemes like kms://, vault://, env://, and random://.
package secrets

import (
	"errors"
	"strconv"
	"strings"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Sentinel errors for common conditions.
var (
	// ErrNilContext is returned when a nil context is provided to a method that requires one.
	ErrNilContext = errors.New("context cannot be nil")
)

// SecretResolutionError represents an error during secret resolution.
// This is a lightweight error type used internally by resolvers.
type SecretResolutionError struct {
	Scheme  string
	Message string
	Cause   error
}

// Error implements the error interface.
func (e *SecretResolutionError) Error() string {
	if e.Cause != nil {
		return e.Scheme + ": " + e.Message + ": " + e.Cause.Error()
	}
	if e.Scheme != "" {
		return e.Scheme + ": " + e.Message
	}
	return e.Message
}

// Unwrap returns the underlying cause for errors.Is/As support.
func (e *SecretResolutionError) Unwrap() error {
	return e.Cause
}

// NewSecretResolutionError creates a new secret resolution error.
func NewSecretResolutionError(scheme, message string, cause error) *SecretResolutionError {
	return &SecretResolutionError{
		Scheme:  scheme,
		Message: message,
		Cause:   cause,
	}
}

// SecretNotFoundError represents an error when a secret path doesn't exist in KMS.
type SecretNotFoundError struct {
	// Path is the secret path that was not found
	Path string

	// Provider is the KMS provider that was queried
	Provider string
}

// Error implements the error interface.
func (e *SecretNotFoundError) Error() string {
	if e.Provider != "" {
		return "secret not found: path \"" + e.Path + "\" in provider \"" + e.Provider + "\""
	}
	return "secret not found: path \"" + e.Path + "\""
}

// ResolutionFailedError represents an error when resolution of a URI fails.
type ResolutionFailedError struct {
	// URI is the secret URI that failed to resolve
	URI string

	// Reason is the reason for the failure
	Reason string

	// Cause is the underlying error that made the resolution fail (the KMS
	// client's error, or the context's error); nil when there is none
	Cause error
}

// Error implements the error interface.
func (e *ResolutionFailedError) Error() string {
	if e.Reason != "" {
		return "failed to resolve secret URI \"" + e.URI + "\": " + e.Reason
	}
	return "failed to resolve secret URI \"" + e.URI + "\""
}

// Unwrap returns the underlying error for errors.Is and errors.As support.
func (e *ResolutionFailedError) Unwrap() error {
	return e.Cause
}

// RemoteConnectionFailedError represents an error when connection to remote KMS fails.
type RemoteConnectionFailedError struct {
	// Endpoint is the remote endpoint that failed to connect
	Endpoint string

	// Provider is the KMS provider type
	Provider string

	// Cause is the underlying connection error
	Cause error
}

// Error implements the error interface.
func (e *RemoteConnectionFailedError) Error() string {
	msg := "failed to connect to remote KMS"
	if e.Provider != "" {
		msg = "failed to connect to " + e.Provider + " KMS"
	}
	if e.Endpoint != "" {
		msg = msg + " at " + e.Endpoint
	}
	if e.Cause != nil {
		msg = msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying error for errors.Is and errors.As support.
func (e *RemoteConnectionFailedError) Unwrap() error {
	return e.Cause
}

// InvalidSecretURIError represents an error when the URI format is invalid.
type InvalidSecretURIError struct {
	// URI is the invalid URI
	URI string

	// Reason is the reason the URI is invalid
	Reason string
}

// Error implements the error interface.
func (e *InvalidSecretURIError) Error() string {
	if e.Reason != "" {
		return "invalid secret URI \"" + e.URI + "\": " + e.Reason
	}
	return "invalid secret URI \"" + e.URI + "\""
}

// EntropyGenerationFailedError represents an error when random generation fails.
type EntropyGenerationFailedError struct {
	// RequestedBytes is the number of bytes that were requested
	RequestedBytes int

	// Cause is the underlying entropy generation error
	Cause error
}

// Error implements the error interface.
func (e *EntropyGenerationFailedError) Error() string {
	if e.Cause != nil {
		if e.RequestedBytes > 0 {
			return "failed to generate " + strconv.Itoa(e.RequestedBytes) + " bytes of entropy: " + e.Cause.Error()
		}
		return "failed to generate entropy: " + e.Cause.Error()
	}
	if e.RequestedBytes > 0 {
		return "failed to generate " + strconv.Itoa(e.RequestedBytes) + " bytes of entropy"
	}
	return "failed to generate entropy"
}

// Unwrap returns the underlying error for errors.Is and errors.As support.
func (e *EntropyGenerationFailedError) Unwrap() error {
	return e.Cause
}

// UnsupportedSchemeError represents an error when no resolver handles the scheme.
type UnsupportedSchemeError struct {
	// Scheme is the unsupported URI scheme
	Scheme string

	// URI is the full URI that was attempted
	URI string

	// SupportedSchemes lists the schemes that are supported
	SupportedSchemes []string
}

// Error implements the error interface.
func (e *UnsupportedSchemeError) Error() string {
	if len(e.SupportedSchemes) > 0 {
		return "unsupported scheme \"" + e.Scheme + "\" in URI \"" + e.URI + "\": supported schemes are [" + strings.Join(e.SupportedSchemes, " ") + "]"
	}
	return "unsupported scheme \"" + e.Scheme + "\" in URI \"" + e.URI + "\""
}

// NewEntropyGenerationError creates a simple entropy generation error with just a cause.
// This is a convenience wrapper for common usage patterns.
func NewEntropyGenerationError(cause error) *EntropyGenerationFailedError {
	return &EntropyGenerationFailedError{
		Cause: cause,
	}
}

// NewSecretNotFoundError creates a new error for secret not found.
func NewSecretNotFoundError(op, path, provider string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrNotFound,
		Op:   op,
		Err: &SecretNotFoundError{
			Path:     path,
			Provider: provider,
		},
		Context: map[string]interface{}{
			"path":     path,
			"provider": provider,
		},
	}
}

// NewResolutionFailedError creates a new error for resolution failure.
func NewResolutionFailedError(op, uri, reason string, cause error) *dberrors.QBError {
	err := &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err: &ResolutionFailedError{
			URI:    uri,
			Reason: reason,
			Cause:  cause,
		},
		Context: map[string]interface{}{
			"uri":    uri,
			"reason": reason,
		},
	}
	if cause != nil {
		err.Context["cause"] = cause.Error()
	}
	return err
}

// NewRemoteConnectionFailedError creates a new error for remote connection failure.
func NewRemoteConnectionFailedError(op, endpoint, provider string, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrUnavailable,
		Op:   op,
		Err: &RemoteConnectionFailedError{
			Endpoint: endpoint,
			Provider: provider,
			Cause:    cause,
		},
		Context: map[string]interface{}{
			"endpoint": endpoint,
			"provider": provider,
		},
		Retry: true,
	}
}

// NewInvalidSecretURIError creates a new error for invalid secret URI.
func NewInvalidSecretURIError(op, uri, reason string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err: &InvalidSecretURIError{
			URI:    uri,
			Reason: reason,
		},
		Context: map[string]interface{}{
			"uri":    uri,
			"reason": reason,
		},
	}
}

// NewEntropyGenerationFailedError creates a new error for entropy generation failure.
func NewEntropyGenerationFailedError(op string, requestedBytes int, cause error) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrEncryption,
		Op:   op,
		Err: &EntropyGenerationFailedError{
			RequestedBytes: requestedBytes,
			Cause:          cause,
		},
		Context: map[string]interface{}{
			"requested_bytes": requestedBytes,
		},
	}
}

// NewUnsupportedSchemeError creates a new error for unsupported URI scheme.
func NewUnsupportedSchemeError(op, scheme, uri string, supportedSchemes []string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err: &UnsupportedSchemeError{
			Scheme:           scheme,
			URI:              uri,
			SupportedSchemes: supportedSchemes,
		},
		Context: map[string]interface{}{
			"scheme":            scheme,
			"uri":               uri,
			"supported_schemes": supportedSchemes,
		},
	}
}

// IsSecretNotFound returns true if the error is a secret not found error.
func IsSecretNotFound(err error) bool {
	if err == nil {
		return false
	}
	var snf *SecretNotFoundError
	if errors.As(err, &snf) {
		return true
	}
	var de *dberrors.QBError
	if errors.As(err, &de) {
		return de.Code == dberrors.ErrNotFound
	}
	return false
}

// IsResolutionFailed returns true if the error is a resolution failed error.
func IsResolutionFailed(err error) bool {
	if err == nil {
		return false
	}
	var rf *ResolutionFailedError
	return errors.As(err, &rf)
}

// IsRemoteConnectionFailed returns true if the error is a remote connection failed error.
func IsRemoteConnectionFailed(err error) bool {
	if err == nil {
		return false
	}
	var rcf *RemoteConnectionFailedError
	if errors.As(err, &rcf) {
		return true
	}
	var de *dberrors.QBError
	if errors.As(err, &de) {
		return de.Code == dberrors.ErrUnavailable
	}
	return false
}

// IsInvalidSecretURI returns true if the error is an invalid secret URI error.
func IsInvalidSecretURI(err error) bool {
	if err == nil {
		return false
	}
	var isu *InvalidSecretURIError
	return errors.As(err, &isu)
}

// IsEntropyGenerationFailed returns true if the error is an entropy generation failed error.
func IsEntropyGenerationFailed(err error) bool {
	if err == nil {
		return false
	}
	var egf *EntropyGenerationFailedError
	if errors.As(err, &egf) {
		return true
	}
	var de *dberrors.QBError
	if errors.As(err, &de) {
		return de.Code == dberrors.ErrEncryption
	}
	return false
}

// IsUnsupportedScheme returns true if the error is an unsupported scheme error.
func IsUnsupportedScheme(err error) bool {
	if err == nil {
		return false
	}
	var use *UnsupportedSchemeError
	return errors.As(err, &use)
}
