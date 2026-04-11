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

package filter

import (
	"strconv"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// FilterParseError indicates a filter expression parsing failure.
type FilterParseError struct {
	Expression string
	Position   int
	Message    string
}

// Error implements the error interface.
func (e *FilterParseError) Error() string {
	return "parse error at position " + strconv.Itoa(e.Position) + ": " + e.Message
}

// NewFilterParseError creates a QBError for a filter expression parsing failure.
func NewFilterParseError(op, expression string, position int, message string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err: &FilterParseError{
			Expression: expression,
			Position:   position,
			Message:    message,
		},
		Context: map[string]interface{}{
			"expression": expression,
			"position":   position,
		},
	}
}

// FilterEvaluationError indicates a filter evaluation failure.
type FilterEvaluationError struct {
	Field   string
	Op      FilterOp
	Message string
}

// Error implements the error interface.
func (e *FilterEvaluationError) Error() string {
	return "evaluation error on field " + e.Field + ": " + e.Message
}

// NewFilterEvaluationError creates a QBError for a filter evaluation failure.
func NewFilterEvaluationError(op string, field string, filterOp FilterOp, message string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInternal,
		Op:   op,
		Err: &FilterEvaluationError{
			Field:   field,
			Op:      filterOp,
			Message: message,
		},
		Context: map[string]interface{}{
			"field":    field,
			"operator": filterOp.String(),
		},
	}
}

// FilterUnsupportedOperatorError indicates an unsupported filter operator.
type FilterUnsupportedOperatorError struct {
	Op FilterOp
}

// Error implements the error interface.
func (e *FilterUnsupportedOperatorError) Error() string {
	return "unsupported filter operator: " + e.Op.String()
}

// NewFilterUnsupportedOperatorError creates a QBError for an unsupported filter operator.
func NewFilterUnsupportedOperatorError(op string, filterOp FilterOp) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err:  &FilterUnsupportedOperatorError{Op: filterOp},
		Context: map[string]interface{}{
			"operator": filterOp.String(),
		},
	}
}

// FilterTypeMismatchError indicates a type mismatch during filter evaluation.
type FilterTypeMismatchError struct {
	Field    string
	Expected string
	Got      string
}

// Error implements the error interface.
func (e *FilterTypeMismatchError) Error() string {
	return "type mismatch on field " + e.Field + ": expected " + e.Expected + ", got " + e.Got
}

// NewFilterTypeMismatchError creates a QBError for a type mismatch during filter evaluation.
func NewFilterTypeMismatchError(op, field, expected, got string) *dberrors.QBError {
	return &dberrors.QBError{
		Code: dberrors.ErrInvalidInput,
		Op:   op,
		Err: &FilterTypeMismatchError{
			Field:    field,
			Expected: expected,
			Got:      got,
		},
		Context: map[string]interface{}{
			"field":    field,
			"expected": expected,
			"got":      got,
		},
	}
}
