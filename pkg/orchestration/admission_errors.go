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
// Admission errors
// =============================================================================
//
// TWO SENTINELS, AND THE SPLIT IS THE POINT. A controller built without a
// store it needs is a wiring defect that a boot must refuse; a fleet the
// controller could not measure is an outage, and the sweep that met it must
// leave every project where it is rather than admit on a number it does not
// have. A caller that could not tell them apart would either run unmetered
// through an outage or refuse to boot through one.

var (
	// ErrAdmissionConfig indicates an admission controller missing a
	// collaborator it cannot work without.
	ErrAdmissionConfig = errors.New("admission: invalid configuration")

	// ErrAdmissionUndecidable indicates the fleet's state could not be read,
	// so no plan exists. Never accompanied by a plan: see admission.go.
	ErrAdmissionUndecidable = errors.New("admission: the fleet could not be measured")
)

// AdmissionConfigError reports a controller built without a collaborator.
type AdmissionConfigError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *AdmissionConfigError) Error() string {
	if e.Message == "" {
		return ErrAdmissionConfig.Error() + ": " + e.Field + " is required"
	}
	return ErrAdmissionConfig.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *AdmissionConfigError) Unwrap() error { return ErrAdmissionConfig }

// AdmissionUndecidableError reports an input a plan could not read, naming
// which one so an operator is not left to guess which store is down.
type AdmissionUndecidableError struct {
	Input string
	Cause error
}

// Error implements the error interface.
func (e *AdmissionUndecidableError) Error() string {
	msg := ErrAdmissionUndecidable.Error() + ": could not read " + e.Input
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the sentinel and the cause for errors.Is/As support.
func (e *AdmissionUndecidableError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrAdmissionUndecidable}
	}
	return []error{ErrAdmissionUndecidable, e.Cause}
}
