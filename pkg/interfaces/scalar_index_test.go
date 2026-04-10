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

package interfaces

import (
	"strings"
	"testing"
)

func TestScalarQueryOp_String(t *testing.T) {
	tests := []struct {
		name     string
		op       ScalarQueryOp
		expected string
	}{
		{"equal", ScalarQueryEqual, "="},
		{"not_equal", ScalarQueryNotEqual, "!="},
		{"greater_than", ScalarQueryGreaterThan, ">"},
		{"greater_equal", ScalarQueryGreaterEqual, ">="},
		{"less_than", ScalarQueryLessThan, "<"},
		{"less_equal", ScalarQueryLessEqual, "<="},
		{"in", ScalarQueryIn, "IN"},
		{"not_in", ScalarQueryNotIn, "NOT IN"},
		{"range", ScalarQueryRange, "RANGE"},
		{"is_null", ScalarQueryIsNull, "IS NULL"},
		{"is_not_null", ScalarQueryIsNotNull, "IS NOT NULL"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.op.String()
			if result != tt.expected {
				t.Errorf("ScalarQueryOp(%d).String() = %q, want %q", tt.op, result, tt.expected)
			}
		})
	}
}

func TestScalarQueryOp_String_Unknown(t *testing.T) {
	unknown := ScalarQueryOp(255)
	result := unknown.String()
	if !strings.HasPrefix(result, "UNKNOWN(") {
		t.Errorf("expected UNKNOWN(...) prefix, got %q", result)
	}
	if !strings.Contains(result, "255") {
		t.Errorf("expected result to contain '255', got %q", result)
	}
}

func TestScalarIndexType_String(t *testing.T) {
	tests := []struct {
		name     string
		typ      ScalarIndexType
		expected string
	}{
		{"bitmap", ScalarIndexBitmap, "BITMAP"},
		{"inverted", ScalarIndexInverted, "INVERTED"},
		{"sorted", ScalarIndexSorted, "SORTED"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.typ.String()
			if result != tt.expected {
				t.Errorf("ScalarIndexType(%d).String() = %q, want %q", tt.typ, result, tt.expected)
			}
		})
	}
}

func TestScalarIndexType_String_Unknown(t *testing.T) {
	unknown := ScalarIndexType(200)
	result := unknown.String()
	if !strings.HasPrefix(result, "UNKNOWN(") {
		t.Errorf("expected UNKNOWN(...) prefix, got %q", result)
	}
	if !strings.Contains(result, "200") {
		t.Errorf("expected result to contain '200', got %q", result)
	}
}
