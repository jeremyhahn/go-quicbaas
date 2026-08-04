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

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Validation-framework re-exports. QuicBaaS is the single SDK frontage for
// the stack: downstream consumers (e.g. go-trusted-platform) reach the
// shared qrdb validation framework through this SDK rather than importing
// go-qrdb directly. Pure aliases — no transport state, no behavior.

// ValidationConstraint identifies which validation rule an input violated.
type ValidationConstraint = qrdbsdk.ValidationConstraint

const (
	// ValidationConstraintEmpty rejects empty input.
	ValidationConstraintEmpty = qrdbsdk.ValidationConstraintEmpty
	// ValidationConstraintNullByte rejects embedded NUL bytes.
	ValidationConstraintNullByte = qrdbsdk.ValidationConstraintNullByte
	// ValidationConstraintTooLong rejects over-length input.
	ValidationConstraintTooLong = qrdbsdk.ValidationConstraintTooLong
	// ValidationConstraintAbsolutePath rejects absolute filesystem paths.
	ValidationConstraintAbsolutePath = qrdbsdk.ValidationConstraintAbsolutePath
	// ValidationConstraintPathTraversal rejects path-traversal sequences.
	ValidationConstraintPathTraversal = qrdbsdk.ValidationConstraintPathTraversal
	// ValidationConstraintControlChars rejects control characters.
	ValidationConstraintControlChars = qrdbsdk.ValidationConstraintControlChars
	// ValidationConstraintInvalidChars rejects disallowed characters.
	ValidationConstraintInvalidChars = qrdbsdk.ValidationConstraintInvalidChars
	// ValidationConstraintInvalidFormat rejects malformed input.
	ValidationConstraintInvalidFormat = qrdbsdk.ValidationConstraintInvalidFormat
	// ValidationConstraintInvalidKeyType rejects unknown key types.
	ValidationConstraintInvalidKeyType = qrdbsdk.ValidationConstraintInvalidKeyType
	// ValidationConstraintInvalidAlgorithm rejects unknown algorithms.
	ValidationConstraintInvalidAlgorithm = qrdbsdk.ValidationConstraintInvalidAlgorithm
)

// ValidationError is the typed validation failure (Field, Constraint,
// Detail). Domain error types may embed it to inherit errors.As matching.
type ValidationError = qrdbsdk.ValidationError

// NewValidationError constructs a ValidationError.
var NewValidationError = qrdbsdk.NewValidationError

// SanitizeForLog neutralizes log-injection characters in untrusted input.
var SanitizeForLog = qrdbsdk.SanitizeForLog
