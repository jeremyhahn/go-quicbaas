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
	"testing"
)

func TestEvaluate_Equal(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"exact match", "hello", "hello", true},
		{"mismatch", "hello", "world", false},
		{"empty match", "", "", true},
		{"empty vs non-empty", "", "x", false},
		{"numeric string", "42", "42", true},
		{"int value", "42", 42, true},
		{"float64 value", "3.14", float64(3.14), true},
		{"nil value", "hello", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpEqual, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpEqual, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_NotEqual(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"different values", "hello", "world", true},
		{"same values", "hello", "hello", false},
		{"empty vs non-empty", "", "x", true},
		{"both empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpNotEqual, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpNotEqual, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_GreaterThan(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"numeric greater", "100", "50", true},
		{"numeric equal", "50", "50", false},
		{"numeric less", "10", "50", false},
		{"float greater", "3.15", "3.14", true},
		{"string fallback", "banana", "apple", true},
		{"string fallback less", "apple", "banana", false},
		{"int filter value", "100", 50, true},
		{"float64 filter value", "3.15", float64(3.14), true},
		{"non-numeric strings", "xyz", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpGreaterThan, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpGreaterThan, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_GreaterEqual(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"numeric greater", "100", "50", true},
		{"numeric equal", "50", "50", true},
		{"numeric less", "10", "50", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpGreaterEqual, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpGreaterEqual, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_LessThan(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"numeric less", "10", "50", true},
		{"numeric equal", "50", "50", false},
		{"numeric greater", "100", "50", false},
		{"string fallback", "apple", "banana", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpLessThan, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpLessThan, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_LessEqual(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"numeric less", "10", "50", true},
		{"numeric equal", "50", "50", true},
		{"numeric greater", "100", "50", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpLessEqual, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpLessEqual, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_In(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"found in string slice", "tech", []string{"tech", "science"}, true},
		{"not found in string slice", "art", []string{"tech", "science"}, false},
		{"found in interface slice", "tech", []interface{}{"tech", "science"}, true},
		{"not found in interface slice", "art", []interface{}{"tech", "science"}, false},
		{"empty slice", "tech", []string{}, false},
		{"invalid type", "tech", "not-a-slice", false},
		{"nil value", "tech", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpIn, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpIn, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_NotIn(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"not in slice", "art", []string{"tech", "science"}, true},
		{"in slice", "tech", []string{"tech", "science"}, false},
		{"invalid type returns true", "tech", "not-a-slice", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpNotIn, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpNotIn, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_Contains(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"substring found", "hello world", "world", true},
		{"substring not found", "hello world", "xyz", false},
		{"empty substring", "hello", "", true},
		{"case sensitive", "Hello", "hello", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpContains, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpContains, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_NotContains(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"substring not found", "hello world", "xyz", true},
		{"substring found", "hello world", "world", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpNotContains, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpNotContains, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_PrefixMatch(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"prefix matches", "golang-pro", "golang", true},
		{"prefix does not match", "python-pro", "golang", false},
		{"empty prefix", "anything", "", true},
		{"exact match", "hello", "hello", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpPrefixMatch, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpPrefixMatch, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_IsNull(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		want       bool
	}{
		{"empty is null", "", true},
		{"non-empty is not null", "value", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpIsNull, tt.fieldValue, nil)
			if got != tt.want {
				t.Errorf("Evaluate(OpIsNull, %q, nil) = %v, want %v", tt.fieldValue, got, tt.want)
			}
		})
	}
}

func TestEvaluate_IsNotNull(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		want       bool
	}{
		{"non-empty is not null", "value", true},
		{"empty is null", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpIsNotNull, tt.fieldValue, nil)
			if got != tt.want {
				t.Errorf("Evaluate(OpIsNotNull, %q, nil) = %v, want %v", tt.fieldValue, got, tt.want)
			}
		})
	}
}

func TestEvaluate_Range(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"in range string pair", "50", [2]string{"10", "100"}, true},
		{"below range string pair", "5", [2]string{"10", "100"}, false},
		{"above range string pair", "200", [2]string{"10", "100"}, false},
		{"at min boundary", "10", [2]string{"10", "100"}, true},
		{"at max boundary", "100", [2]string{"10", "100"}, true},
		{"in range interface pair", "50", [2]interface{}{"10", "100"}, true},
		{"invalid type", "50", "not-a-range", false},
		{"nil value", "50", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpRange, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpRange, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_TextMatch(t *testing.T) {
	tests := []struct {
		name       string
		fieldValue string
		filterVal  interface{}
		want       bool
	}{
		{"case insensitive match", "Hello World", "hello", true},
		{"exact case match", "golang", "golang", true},
		{"no match", "hello", "xyz", false},
		{"empty filter", "hello", "", true},
		{"mixed case", "GoLang Pro", "golang", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(OpTextMatch, tt.fieldValue, tt.filterVal)
			if got != tt.want {
				t.Errorf("Evaluate(OpTextMatch, %q, %v) = %v, want %v", tt.fieldValue, tt.filterVal, got, tt.want)
			}
		})
	}
}

func TestEvaluate_UnsupportedOperator(t *testing.T) {
	got := Evaluate(FilterOp(999), "value", "test")
	if got {
		t.Error("Evaluate with unsupported operator should return false")
	}
}

func TestFilterOp_String(t *testing.T) {
	tests := []struct {
		op   FilterOp
		want string
	}{
		{OpEqual, "="},
		{OpNotEqual, "!="},
		{OpGreaterThan, ">"},
		{OpIn, "IN"},
		{OpContains, "CONTAINS"},
		{OpIsNull, "IS NULL"},
		{OpTextMatch, "TEXT MATCH"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("FilterOp(%d).String() = %q, want %q", tt.op, got, tt.want)
			}
		})
	}
}

func TestFilterOp_String_Unknown(t *testing.T) {
	op := FilterOp(999)
	got := op.String()
	if got != "unknown(999)" {
		t.Errorf("FilterOp(999).String() = %q, want %q", got, "unknown(999)")
	}
}

func TestToString(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want string
	}{
		{"string", "hello", "hello"},
		{"int", 42, "42"},
		{"int64", int64(100), "100"},
		{"float64", float64(3.14), "3.14"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, ""},
		{"unsupported type", struct{}{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toString(tt.val)
			if got != tt.want {
				t.Errorf("toString(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestParseFloat(t *testing.T) {
	tests := []struct {
		name    string
		val     interface{}
		wantVal float64
		wantOK  bool
	}{
		{"string number", "3.14", 3.14, true},
		{"string int", "42", 42.0, true},
		{"non-numeric string", "hello", 0, false},
		{"float64", float64(2.71), 2.71, true},
		{"int", 42, 42.0, true},
		{"int64", int64(100), 100.0, true},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotOK := parseFloat(tt.val)
			if gotOK != tt.wantOK {
				t.Errorf("parseFloat(%v) ok = %v, want %v", tt.val, gotOK, tt.wantOK)
			}
			if gotOK && gotVal != tt.wantVal {
				t.Errorf("parseFloat(%v) = %v, want %v", tt.val, gotVal, tt.wantVal)
			}
		})
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name   string
		val    interface{}
		want   []string
		wantOK bool
	}{
		{"string slice", []string{"a", "b"}, []string{"a", "b"}, true},
		{"interface slice", []interface{}{"a", 42}, []string{"a", "42"}, true},
		{"invalid type", "not-a-slice", nil, false},
		{"nil", nil, nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotOK := toStringSlice(tt.val)
			if gotOK != tt.wantOK {
				t.Errorf("toStringSlice(%v) ok = %v, want %v", tt.val, gotOK, tt.wantOK)
			}
			if gotOK && len(got) != len(tt.want) {
				t.Errorf("toStringSlice(%v) len = %d, want %d", tt.val, len(got), len(tt.want))
			}
		})
	}
}

func TestEvaluate_NumericFallbackToString(t *testing.T) {
	// When one side is non-numeric, comparison should fall back to string ordering.
	// "abc" > "50" is true in string comparison (a > 5 in ASCII).
	got := Evaluate(OpGreaterThan, "abc", "50")
	if !got {
		t.Error("expected string fallback: 'abc' > '50' should be true in lexicographic order")
	}

	// "10x" cannot be parsed as float, so string fallback.
	got = Evaluate(OpLessThan, "9y", "10x")
	if got {
		t.Error("expected string fallback: '9y' < '10x' should be false ('9' > '1' in ASCII)")
	}
}
