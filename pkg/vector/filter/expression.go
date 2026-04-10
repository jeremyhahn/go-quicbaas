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
	"strings"
)

// Expression is the interface for all expression nodes in the filter AST.
type Expression interface {
	// Evaluate tests the expression against a metadata map.
	Evaluate(metadata map[string]string) bool
	// String returns a human-readable representation of the expression.
	String() string
}

// LogicalOp represents a logical combining operator.
type LogicalOp int

const (
	// LogicalAND requires both operands to be true.
	LogicalAND LogicalOp = iota
	// LogicalOR requires at least one operand to be true.
	LogicalOR
	// LogicalNOT negates the left operand (Right is nil).
	LogicalNOT
)

// String returns the display name for the logical operator.
func (op LogicalOp) String() string {
	name, ok := logicalOpNames[op]
	if !ok {
		return "UNKNOWN"
	}
	return name
}

// logicalOpNames maps logical operators to display names.
var logicalOpNames = map[LogicalOp]string{
	LogicalAND: "AND",
	LogicalOR:  "OR",
	LogicalNOT: "NOT",
}

// TermExpr matches a field against a value using an operator.
type TermExpr struct {
	// Field is the metadata field name.
	Field string
	// Op is the comparison operator.
	Op FilterOp
	// Value is the comparison target.
	Value interface{}
}

// Evaluate tests this term expression against the given metadata.
func (e *TermExpr) Evaluate(metadata map[string]string) bool {
	fieldValue := metadata[e.Field]
	return Evaluate(e.Op, fieldValue, e.Value)
}

// String returns a human-readable representation.
func (e *TermExpr) String() string {
	return e.Field + " " + e.Op.String() + " " + formatValue(e.Value)
}

// LogicalExpr combines expressions with AND/OR/NOT logic.
type LogicalExpr struct {
	// Op is the logical operator.
	Op LogicalOp
	// Left is the left operand (always set).
	Left Expression
	// Right is the right operand (nil for NOT).
	Right Expression
}

// Evaluate tests this logical expression against the given metadata.
func (e *LogicalExpr) Evaluate(metadata map[string]string) bool {
	handler, ok := logicalEvaluators[e.Op]
	if !ok {
		return false
	}
	return handler(e, metadata)
}

// logicalEvalFunc is the function signature for logical evaluators.
type logicalEvalFunc func(e *LogicalExpr, metadata map[string]string) bool

// logicalEvaluators provides O(1) dispatch for logical operations.
var logicalEvaluators = map[LogicalOp]logicalEvalFunc{
	LogicalAND: evalLogicalAND,
	LogicalOR:  evalLogicalOR,
	LogicalNOT: evalLogicalNOT,
}

func evalLogicalAND(e *LogicalExpr, metadata map[string]string) bool {
	return e.Left.Evaluate(metadata) && e.Right.Evaluate(metadata)
}

func evalLogicalOR(e *LogicalExpr, metadata map[string]string) bool {
	return e.Left.Evaluate(metadata) || e.Right.Evaluate(metadata)
}

func evalLogicalNOT(e *LogicalExpr, metadata map[string]string) bool {
	return !e.Left.Evaluate(metadata)
}

// String returns a human-readable representation.
func (e *LogicalExpr) String() string {
	if e.Op == LogicalNOT {
		return "NOT (" + e.Left.String() + ")"
	}
	return "(" + e.Left.String() + " " + e.Op.String() + " " + e.Right.String() + ")"
}

// NullExpr checks if a field is null (empty) or not null (non-empty).
type NullExpr struct {
	// Field is the metadata field name.
	Field string
	// IsNull is true for IS NULL, false for IS NOT NULL.
	IsNull bool
}

// Evaluate tests this null expression against the given metadata.
func (e *NullExpr) Evaluate(metadata map[string]string) bool {
	val, exists := metadata[e.Field]
	if e.IsNull {
		return !exists || val == ""
	}
	return exists && val != ""
}

// String returns a human-readable representation.
func (e *NullExpr) String() string {
	if e.IsNull {
		return e.Field + " IS NULL"
	}
	return e.Field + " IS NOT NULL"
}

// InExpr checks if a field value is in a set of values.
type InExpr struct {
	// Field is the metadata field name.
	Field string
	// Values is the set of allowed values.
	Values []string
	// Negate is true for NOT IN.
	Negate bool
}

// Evaluate tests this IN expression against the given metadata.
func (e *InExpr) Evaluate(metadata map[string]string) bool {
	fieldValue := metadata[e.Field]
	for _, v := range e.Values {
		if fieldValue == v {
			return !e.Negate
		}
	}
	return e.Negate
}

// String returns a human-readable representation.
func (e *InExpr) String() string {
	var b strings.Builder
	b.WriteString(e.Field)
	if e.Negate {
		b.WriteString(" NOT IN [")
	} else {
		b.WriteString(" IN [")
	}
	for i, v := range e.Values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteByte('"')
		b.WriteString(v)
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return b.String()
}

// RangeExpr checks if a field value is in a range.
type RangeExpr struct {
	// Field is the metadata field name.
	Field string
	// Min is the range lower bound.
	Min string
	// Max is the range upper bound.
	Max string
	// MinInclusive indicates whether the lower bound is inclusive.
	MinInclusive bool
	// MaxInclusive indicates whether the upper bound is inclusive.
	MaxInclusive bool
}

// Evaluate tests this range expression against the given metadata.
func (e *RangeExpr) Evaluate(metadata map[string]string) bool {
	fieldValue := metadata[e.Field]

	var minOK, maxOK bool

	if e.MinInclusive {
		minOK = evalGreaterEqual(fieldValue, e.Min)
	} else {
		minOK = evalGreaterThan(fieldValue, e.Min)
	}

	if e.MaxInclusive {
		maxOK = evalLessEqual(fieldValue, e.Max)
	} else {
		maxOK = evalLessThan(fieldValue, e.Max)
	}

	return minOK && maxOK
}

// String returns a human-readable representation.
func (e *RangeExpr) String() string {
	var b strings.Builder
	b.WriteString(e.Field)
	b.WriteString(" RANGE [")
	b.WriteString(e.Min)
	b.WriteString(", ")
	b.WriteString(e.Max)
	b.WriteByte(']')
	if !e.MinInclusive {
		b.WriteString(" (min exclusive)")
	}
	if !e.MaxInclusive {
		b.WriteString(" (max exclusive)")
	}
	return b.String()
}

// formatValue converts a filter value to a display string.
func formatValue(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch val := v.(type) {
	case string:
		return strconv.Quote(val)
	case []string:
		var b strings.Builder
		b.WriteByte('[')
		for i, s := range val {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(s))
		}
		b.WriteByte(']')
		return b.String()
	default:
		return toString(v)
	}
}
