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
	"errors"
	"strings"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

func TestFilterParseError_Error(t *testing.T) {
	err := &FilterParseError{
		Expression: "bad expression",
		Position:   5,
		Message:    "unexpected token",
	}
	got := err.Error()
	if !strings.Contains(got, "position 5") || !strings.Contains(got, "unexpected token") {
		t.Errorf("FilterParseError.Error() = %q, expected position and message", got)
	}
}

func TestFilterParseError_EmptyMessage(t *testing.T) {
	err := &FilterParseError{
		Expression: "",
		Position:   0,
		Message:    "",
	}
	got := err.Error()
	if !strings.Contains(got, "position 0") {
		t.Errorf("FilterParseError.Error() = %q, expected position", got)
	}
}

func TestNewFilterParseError(t *testing.T) {
	err := NewFilterParseError("filter.Parse", "age > foo", 4, "invalid value")
	if err.Code != dberrors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err.Code)
	}
	if err.Op != "filter.Parse" {
		t.Errorf("expected op filter.Parse, got %s", err.Op)
	}
	var parseErr *FilterParseError
	if !errors.As(err, &parseErr) {
		t.Error("expected inner error to be FilterParseError")
	}
}

func TestNewFilterParseError_Context(t *testing.T) {
	err := NewFilterParseError("op", "expr", 10, "msg")
	if err.Context["expression"] != "expr" {
		t.Error("expected expression in context")
	}
	if err.Context["position"] != 10 {
		t.Error("expected position in context")
	}
}

func TestFilterEvaluationError_Error(t *testing.T) {
	err := &FilterEvaluationError{
		Field:   "age",
		Op:      OpGreaterThan,
		Message: "type mismatch",
	}
	got := err.Error()
	if !strings.Contains(got, "age") || !strings.Contains(got, "type mismatch") {
		t.Errorf("FilterEvaluationError.Error() = %q, expected field and message", got)
	}
}

func TestNewFilterEvaluationError(t *testing.T) {
	err := NewFilterEvaluationError("filter.Evaluate", "price", OpLessThan, "not a number")
	if err.Code != dberrors.ErrInternal {
		t.Errorf("expected ErrInternal, got %v", err.Code)
	}
	var evalErr *FilterEvaluationError
	if !errors.As(err, &evalErr) {
		t.Error("expected inner error to be FilterEvaluationError")
	}
}

func TestFilterUnsupportedOperatorError_Error(t *testing.T) {
	err := &FilterUnsupportedOperatorError{Op: FilterOp(999)}
	got := err.Error()
	if !strings.Contains(got, "unsupported") {
		t.Errorf("FilterUnsupportedOperatorError.Error() = %q, expected 'unsupported'", got)
	}
}

func TestNewFilterUnsupportedOperatorError(t *testing.T) {
	err := NewFilterUnsupportedOperatorError("filter.Dispatch", FilterOp(999))
	if err.Code != dberrors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err.Code)
	}
	var unsupErr *FilterUnsupportedOperatorError
	if !errors.As(err, &unsupErr) {
		t.Error("expected inner error to be FilterUnsupportedOperatorError")
	}
}

func TestFilterTypeMismatchError_Error(t *testing.T) {
	err := &FilterTypeMismatchError{
		Field:    "age",
		Expected: "int",
		Got:      "string",
	}
	got := err.Error()
	if !strings.Contains(got, "age") || !strings.Contains(got, "int") || !strings.Contains(got, "string") {
		t.Errorf("FilterTypeMismatchError.Error() = %q, expected field, expected, and got", got)
	}
}

func TestNewFilterTypeMismatchError(t *testing.T) {
	err := NewFilterTypeMismatchError("filter.Compare", "price", "float64", "[]string")
	if err.Code != dberrors.ErrInvalidInput {
		t.Errorf("expected ErrInvalidInput, got %v", err.Code)
	}
	var tmErr *FilterTypeMismatchError
	if !errors.As(err, &tmErr) {
		t.Error("expected inner error to be FilterTypeMismatchError")
	}
	if tmErr.Field != "price" {
		t.Errorf("expected field price, got %s", tmErr.Field)
	}
}

func TestNewFilterTypeMismatchError_Context(t *testing.T) {
	err := NewFilterTypeMismatchError("op", "field", "expected", "got")
	if err.Context["field"] != "field" {
		t.Error("expected field in context")
	}
	if err.Context["expected"] != "expected" {
		t.Error("expected expected in context")
	}
	if err.Context["got"] != "got" {
		t.Error("expected got in context")
	}
}
