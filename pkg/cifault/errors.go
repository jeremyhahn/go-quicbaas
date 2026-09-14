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

package cifault

import "errors"

// ErrClassify is the sentinel every error this package returns unwraps to, so
// a caller can tell "the classifier refused the question" from "the forge
// call failed" without naming each type.
var ErrClassify = errors.New("cifault: cannot classify")

// InvalidInputError reports a question the classifier will not answer.
//
// A REFUSAL, NEVER A DEFAULT VERDICT. Handing Classify a green check or a
// conclusion it has never heard of is a programming error at the call site,
// and answering it with VerdictGenuine would hide that call site forever
// behind a plausible-looking verdict.
type InvalidInputError struct {
	// Field names the input that was wrong, in the caller's vocabulary.
	Field string

	// Message says what was wrong with it.
	Message string
}

// Error implements the error interface.
func (e *InvalidInputError) Error() string {
	return ErrClassify.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel for errors.Is support.
func (e *InvalidInputError) Unwrap() error { return ErrClassify }
