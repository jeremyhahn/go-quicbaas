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

package orchestration

import (
	"errors"
	"strconv"
	"strings"
)

// =============================================================================
// Paging Sentinel Errors
// =============================================================================

var (
	// ErrPageSortFieldUnknown indicates the caller asked to sort on a field
	// the endpoint does not order by.
	ErrPageSortFieldUnknown = errors.New("page: unknown sort field")

	// ErrPageParamInvalid indicates a paging parameter could not be read as
	// the number it must be.
	ErrPageParamInvalid = errors.New("page: invalid parameter")

	// ErrFilterParamInvalid indicates a list FILTER parameter could not be
	// read as the value its type requires — a tri-state boolean sent as
	// "maybe". It is distinct from ErrPageParamInvalid because the two name
	// different halves of a list request, and a caller reading the message
	// must not be told its paging is wrong when its filter is.
	ErrFilterParamInvalid = errors.New("filter: invalid parameter")
)

// =============================================================================
// Paging Typed Errors
// =============================================================================

// UnknownSortFieldError names the field the caller asked to sort on and the
// fields the endpoint actually orders by.
//
// IT NAMES THE ALLOW-LIST BECAUSE A REFUSAL WITHOUT ONE IS UNACTIONABLE. The
// caller is a UI column header or a script; told only "unknown sort field" it
// cannot tell a typo from a field this endpoint never had.
type UnknownSortFieldError struct {
	Field   string
	Allowed []string
}

// Error implements the error interface.
func (e *UnknownSortFieldError) Error() string {
	msg := ErrPageSortFieldUnknown.Error() + ": " + e.Field
	if len(e.Allowed) > 0 {
		msg += " (allowed: " + strings.Join(e.Allowed, ", ") + ")"
	}
	return msg
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownSortFieldError) Unwrap() error { return ErrPageSortFieldUnknown }

// InvalidPageParamError names the paging parameter that could not be parsed
// and the value that was sent.
type InvalidPageParamError struct {
	Param string
	Value string
}

// Error implements the error interface.
func (e *InvalidPageParamError) Error() string {
	return ErrPageParamInvalid.Error() + ": " + e.Param + "=" + strconv.Quote(e.Value)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidPageParamError) Unwrap() error { return ErrPageParamInvalid }

// InvalidFilterParamError names the list filter parameter that could not be
// parsed and the value that was sent.
type InvalidFilterParamError struct {
	Param string
	Value string
}

// Error implements the error interface.
func (e *InvalidFilterParamError) Error() string {
	return ErrFilterParamInvalid.Error() + ": " + e.Param + "=" + strconv.Quote(e.Value)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidFilterParamError) Unwrap() error { return ErrFilterParamInvalid }
