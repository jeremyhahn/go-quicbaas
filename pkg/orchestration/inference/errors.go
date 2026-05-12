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

package inference

import "errors"

// Sentinel errors. Typed errors (defined below) wrap these so callers can use
// errors.Is for stable matching while still extracting structured detail with
// errors.As.
var (
	// ErrProviderNotRegistered is returned when a Config references a
	// provider name with no factory registered.
	ErrProviderNotRegistered = errors.New("inference: provider not registered")

	// ErrProviderAlreadyRegistered is returned when Register is called twice
	// with the same provider name.
	ErrProviderAlreadyRegistered = errors.New("inference: provider already registered")

	// ErrInvalidProviderConfig is returned when a Config fails validation.
	ErrInvalidProviderConfig = errors.New("inference: invalid provider config")

	// ErrNilFactory is returned when Register is called with a nil factory.
	ErrNilFactory = errors.New("inference: factory must not be nil")

	// ErrEmptyProvider is returned when an empty provider name is passed to
	// Register or Create.
	ErrEmptyProvider = errors.New("inference: provider name must not be empty")
)

// ProviderNotRegisteredError indicates a Create call referenced a provider
// that has no factory registered.
type ProviderNotRegisteredError struct {
	Provider string
}

// Error implements the error interface.
func (e *ProviderNotRegisteredError) Error() string {
	return ErrProviderNotRegistered.Error() + ": " + e.Provider
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProviderNotRegisteredError) Unwrap() error {
	return ErrProviderNotRegistered
}

// ProviderAlreadyRegisteredError indicates a duplicate Register call.
type ProviderAlreadyRegisteredError struct {
	Provider string
}

// Error implements the error interface.
func (e *ProviderAlreadyRegisteredError) Error() string {
	return ErrProviderAlreadyRegistered.Error() + ": " + e.Provider
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProviderAlreadyRegisteredError) Unwrap() error {
	return ErrProviderAlreadyRegistered
}

// InvalidProviderConfigError indicates a Config failed validation. Field
// names the offending field; Reason gives a human-readable explanation.
type InvalidProviderConfigError struct {
	Provider string
	Field    string
	Reason   string
}

// Error implements the error interface.
func (e *InvalidProviderConfigError) Error() string {
	msg := ErrInvalidProviderConfig.Error()
	if e.Provider != "" {
		msg += ": provider=" + e.Provider
	}
	if e.Field != "" {
		msg += " field=" + e.Field
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidProviderConfigError) Unwrap() error {
	return ErrInvalidProviderConfig
}
