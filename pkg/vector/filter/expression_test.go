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
	"strings"
	"testing"
)

func TestTermExpr_Evaluate(t *testing.T) {
	metadata := map[string]string{
		"age":      "30",
		"name":     "alice",
		"category": "tech",
	}

	tests := []struct {
		name string
		expr *TermExpr
		want bool
	}{
		{"equal match", &TermExpr{Field: "name", Op: OpEqual, Value: "alice"}, true},
		{"equal mismatch", &TermExpr{Field: "name", Op: OpEqual, Value: "bob"}, false},
		{"greater than", &TermExpr{Field: "age", Op: OpGreaterThan, Value: "25"}, true},
		{"less than", &TermExpr{Field: "age", Op: OpLessThan, Value: "25"}, false},
		{"contains", &TermExpr{Field: "category", Op: OpContains, Value: "tec"}, true},
		{"missing field defaults to empty", &TermExpr{Field: "missing", Op: OpEqual, Value: ""}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.expr.Evaluate(metadata)
			if got != tt.want {
				t.Errorf("%s.Evaluate() = %v, want %v", tt.expr.String(), got, tt.want)
			}
		})
	}
}

func TestTermExpr_Evaluate_MissingField(t *testing.T) {
	metadata := map[string]string{"age": "30"}
	expr := &TermExpr{Field: "nonexistent", Op: OpGreaterThan, Value: "0"}
	// Empty string "" is not > "0" numerically (0 > 0 is false), so this should be false.
	got := expr.Evaluate(metadata)
	if got {
		t.Error("missing field should evaluate to empty string, which is not > 0")
	}
}

func TestTermExpr_String(t *testing.T) {
	expr := &TermExpr{Field: "age", Op: OpGreaterThan, Value: "25"}
	s := expr.String()
	if !strings.Contains(s, "age") || !strings.Contains(s, ">") || !strings.Contains(s, "25") {
		t.Errorf("TermExpr.String() = %q, expected to contain field, op, and value", s)
	}
}

func TestLogicalExpr_AND(t *testing.T) {
	metadata := map[string]string{"age": "30", "name": "alice"}

	expr := &LogicalExpr{
		Op:    LogicalAND,
		Left:  &TermExpr{Field: "age", Op: OpGreaterThan, Value: "25"},
		Right: &TermExpr{Field: "name", Op: OpEqual, Value: "alice"},
	}

	if !expr.Evaluate(metadata) {
		t.Error("AND: both conditions true, expected true")
	}

	expr.Right = &TermExpr{Field: "name", Op: OpEqual, Value: "bob"}
	if expr.Evaluate(metadata) {
		t.Error("AND: right condition false, expected false")
	}
}

func TestLogicalExpr_OR(t *testing.T) {
	metadata := map[string]string{"age": "30", "name": "alice"}

	expr := &LogicalExpr{
		Op:    LogicalOR,
		Left:  &TermExpr{Field: "age", Op: OpLessThan, Value: "10"},
		Right: &TermExpr{Field: "name", Op: OpEqual, Value: "alice"},
	}

	if !expr.Evaluate(metadata) {
		t.Error("OR: right condition true, expected true")
	}

	expr.Right = &TermExpr{Field: "name", Op: OpEqual, Value: "bob"}
	if expr.Evaluate(metadata) {
		t.Error("OR: both conditions false, expected false")
	}
}

func TestLogicalExpr_NOT(t *testing.T) {
	metadata := map[string]string{"deleted": "true"}

	expr := &LogicalExpr{
		Op:   LogicalNOT,
		Left: &TermExpr{Field: "deleted", Op: OpEqual, Value: "true"},
	}

	if expr.Evaluate(metadata) {
		t.Error("NOT: inner condition true, expected false")
	}

	metadata["deleted"] = "false"
	if !expr.Evaluate(metadata) {
		t.Error("NOT: inner condition false, expected true")
	}
}

func TestLogicalExpr_UnknownOp(t *testing.T) {
	expr := &LogicalExpr{
		Op:   LogicalOp(99),
		Left: &TermExpr{Field: "x", Op: OpEqual, Value: "1"},
	}
	if expr.Evaluate(map[string]string{"x": "1"}) {
		t.Error("unknown logical op should return false")
	}
}

func TestLogicalExpr_NestedAND_OR(t *testing.T) {
	metadata := map[string]string{"a": "1", "b": "2", "c": "3"}

	// (a = 1 AND b = 2) OR c = 99
	expr := &LogicalExpr{
		Op: LogicalOR,
		Left: &LogicalExpr{
			Op:    LogicalAND,
			Left:  &TermExpr{Field: "a", Op: OpEqual, Value: "1"},
			Right: &TermExpr{Field: "b", Op: OpEqual, Value: "2"},
		},
		Right: &TermExpr{Field: "c", Op: OpEqual, Value: "99"},
	}

	if !expr.Evaluate(metadata) {
		t.Error("nested (a=1 AND b=2) OR c=99: left AND is true, expected true")
	}
}

func TestLogicalExpr_String(t *testing.T) {
	expr := &LogicalExpr{
		Op:    LogicalAND,
		Left:  &TermExpr{Field: "a", Op: OpEqual, Value: "1"},
		Right: &TermExpr{Field: "b", Op: OpEqual, Value: "2"},
	}
	s := expr.String()
	if !strings.Contains(s, "AND") {
		t.Errorf("LogicalExpr.String() = %q, expected AND", s)
	}

	notExpr := &LogicalExpr{
		Op:   LogicalNOT,
		Left: &TermExpr{Field: "x", Op: OpEqual, Value: "1"},
	}
	s = notExpr.String()
	if !strings.Contains(s, "NOT") {
		t.Errorf("LogicalExpr.String() = %q, expected NOT", s)
	}
}

func TestLogicalOp_String(t *testing.T) {
	tests := []struct {
		op   LogicalOp
		want string
	}{
		{LogicalAND, "AND"},
		{LogicalOR, "OR"},
		{LogicalNOT, "NOT"},
		{LogicalOp(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.op.String(); got != tt.want {
				t.Errorf("LogicalOp(%d).String() = %q, want %q", tt.op, got, tt.want)
			}
		})
	}
}

func TestNullExpr_Evaluate(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		field    string
		isNull   bool
		want     bool
	}{
		{"field missing is null", map[string]string{}, "name", true, true},
		{"field empty is null", map[string]string{"name": ""}, "name", true, true},
		{"field present is not null", map[string]string{"name": "alice"}, "name", true, false},
		{"field missing is not null", map[string]string{}, "name", false, false},
		{"field present is not null check", map[string]string{"name": "alice"}, "name", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := &NullExpr{Field: tt.field, IsNull: tt.isNull}
			got := expr.Evaluate(tt.metadata)
			if got != tt.want {
				t.Errorf("NullExpr{%s, isNull=%v}.Evaluate() = %v, want %v", tt.field, tt.isNull, got, tt.want)
			}
		})
	}
}

func TestNullExpr_String(t *testing.T) {
	isNullExpr := &NullExpr{Field: "name", IsNull: true}
	if s := isNullExpr.String(); s != "name IS NULL" {
		t.Errorf("NullExpr.String() = %q, want %q", s, "name IS NULL")
	}

	isNotNullExpr := &NullExpr{Field: "name", IsNull: false}
	if s := isNotNullExpr.String(); s != "name IS NOT NULL" {
		t.Errorf("NullExpr.String() = %q, want %q", s, "name IS NOT NULL")
	}
}

func TestInExpr_Evaluate(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		expr     *InExpr
		want     bool
	}{
		{"found", map[string]string{"cat": "tech"}, &InExpr{Field: "cat", Values: []string{"tech", "science"}, Negate: false}, true},
		{"not found", map[string]string{"cat": "art"}, &InExpr{Field: "cat", Values: []string{"tech", "science"}, Negate: false}, false},
		{"negated found", map[string]string{"cat": "tech"}, &InExpr{Field: "cat", Values: []string{"tech", "science"}, Negate: true}, false},
		{"negated not found", map[string]string{"cat": "art"}, &InExpr{Field: "cat", Values: []string{"tech", "science"}, Negate: true}, true},
		{"empty values", map[string]string{"cat": "tech"}, &InExpr{Field: "cat", Values: []string{}, Negate: false}, false},
		{"missing field", map[string]string{}, &InExpr{Field: "cat", Values: []string{"", "tech"}, Negate: false}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.expr.Evaluate(tt.metadata)
			if got != tt.want {
				t.Errorf("%s.Evaluate() = %v, want %v", tt.expr.String(), got, tt.want)
			}
		})
	}
}

func TestInExpr_String(t *testing.T) {
	expr := &InExpr{Field: "cat", Values: []string{"a", "b"}, Negate: false}
	s := expr.String()
	if !strings.Contains(s, "IN") || !strings.Contains(s, "cat") {
		t.Errorf("InExpr.String() = %q, expected field and IN", s)
	}

	negExpr := &InExpr{Field: "cat", Values: []string{"a"}, Negate: true}
	s = negExpr.String()
	if !strings.Contains(s, "NOT IN") {
		t.Errorf("InExpr.String() = %q, expected NOT IN", s)
	}
}

func TestRangeExpr_Evaluate(t *testing.T) {
	tests := []struct {
		name     string
		metadata map[string]string
		expr     *RangeExpr
		want     bool
	}{
		{"in range inclusive", map[string]string{"price": "50"}, &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: true, MaxInclusive: true}, true},
		{"at min inclusive", map[string]string{"price": "10"}, &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: true, MaxInclusive: true}, true},
		{"at min exclusive", map[string]string{"price": "10"}, &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: false, MaxInclusive: true}, false},
		{"at max inclusive", map[string]string{"price": "100"}, &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: true, MaxInclusive: true}, true},
		{"at max exclusive", map[string]string{"price": "100"}, &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: true, MaxInclusive: false}, false},
		{"below range", map[string]string{"price": "5"}, &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: true, MaxInclusive: true}, false},
		{"above range", map[string]string{"price": "200"}, &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: true, MaxInclusive: true}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.expr.Evaluate(tt.metadata)
			if got != tt.want {
				t.Errorf("%s.Evaluate() = %v, want %v", tt.expr.String(), got, tt.want)
			}
		})
	}
}

func TestRangeExpr_String(t *testing.T) {
	expr := &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: true, MaxInclusive: true}
	s := expr.String()
	if !strings.Contains(s, "price") || !strings.Contains(s, "RANGE") {
		t.Errorf("RangeExpr.String() = %q, expected field and RANGE", s)
	}

	expr2 := &RangeExpr{Field: "price", Min: "10", Max: "100", MinInclusive: false, MaxInclusive: false}
	s2 := expr2.String()
	if !strings.Contains(s2, "min exclusive") || !strings.Contains(s2, "max exclusive") {
		t.Errorf("RangeExpr.String() = %q, expected exclusive annotations", s2)
	}
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want string
	}{
		{"nil", nil, "null"},
		{"string", "hello", `"hello"`},
		{"string slice", []string{"a", "b"}, `["a", "b"]`},
		{"int", 42, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatValue(tt.val)
			if got != tt.want {
				t.Errorf("formatValue(%v) = %q, want %q", tt.val, got, tt.want)
			}
		})
	}
}
