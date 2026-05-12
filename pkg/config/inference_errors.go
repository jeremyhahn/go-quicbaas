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

package config

import (
	"errors"
	"strconv"
)

// ErrInvalidInferenceConfig is the sentinel error returned when an
// InferenceConfig fails validation. Use errors.Is to match.
var ErrInvalidInferenceConfig = errors.New("config: invalid inference config")

// InvalidInferenceConfigError carries structured detail about which
// ProviderConfig entry failed validation and why.
type InvalidInferenceConfigError struct {
	Index  int
	Field  string
	Reason string
}

// Error implements the error interface.
func (e *InvalidInferenceConfigError) Error() string {
	msg := ErrInvalidInferenceConfig.Error()
	msg += ": providers[" + strconv.Itoa(e.Index) + "]"
	if e.Field != "" {
		msg += "." + e.Field
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidInferenceConfigError) Unwrap() error {
	return ErrInvalidInferenceConfig
}
