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

// Package filter provides a rich filter expression system for metadata-based
// vector search queries, including parsing, evaluation, and caching.
package filter

import (
	"strconv"
	"strings"
)

// FilterOp represents a metadata filter operator.
type FilterOp int

const (
	// OpEqual matches exact string equality.
	OpEqual FilterOp = iota
	// OpNotEqual matches when values are not equal.
	OpNotEqual
	// OpGreaterThan matches when field value is greater than filter value.
	OpGreaterThan
	// OpGreaterEqual matches when field value is greater than or equal to filter value.
	OpGreaterEqual
	// OpLessThan matches when field value is less than filter value.
	OpLessThan
	// OpLessEqual matches when field value is less than or equal to filter value.
	OpLessEqual
	// OpIn matches when field value is in a set of values.
	OpIn
	// OpNotIn matches when field value is not in a set of values.
	OpNotIn
	// OpContains matches when field value contains the filter value as a substring.
	OpContains
	// OpNotContains matches when field value does not contain the filter value.
	OpNotContains
	// OpPrefixMatch matches when field value starts with the filter value.
	OpPrefixMatch
	// OpIsNull matches when the field value is an empty string.
	OpIsNull
	// OpIsNotNull matches when the field value is a non-empty string.
	OpIsNotNull
	// OpRange matches when the field value is between a min and max (inclusive).
	OpRange
	// OpTextMatch performs a case-insensitive substring match.
	OpTextMatch
)

// String returns a human-readable name for the filter operator.
func (op FilterOp) String() string {
	name, ok := opNames[op]
	if !ok {
		return "unknown(" + strconv.Itoa(int(op)) + ")"
	}
	return name
}

// opNames maps filter operators to their display names.
var opNames = map[FilterOp]string{
	OpEqual:        "=",
	OpNotEqual:     "!=",
	OpGreaterThan:  ">",
	OpGreaterEqual: ">=",
	OpLessThan:     "<",
	OpLessEqual:    "<=",
	OpIn:           "IN",
	OpNotIn:        "NOT IN",
	OpContains:     "CONTAINS",
	OpNotContains:  "NOT CONTAINS",
	OpPrefixMatch:  "STARTS WITH",
	OpIsNull:       "IS NULL",
	OpIsNotNull:    "IS NOT NULL",
	OpRange:        "RANGE",
	OpTextMatch:    "TEXT MATCH",
}

// EvalFunc evaluates a filter condition against a field value.
// The fieldValue is the actual metadata value, and filterValue is the
// comparison target provided in the filter expression.
type EvalFunc func(fieldValue string, filterValue interface{}) bool

// evaluators is the map-based dispatch table for O(1) operator lookup.
var evaluators = map[FilterOp]EvalFunc{
	OpEqual:        evalEqual,
	OpNotEqual:     evalNotEqual,
	OpGreaterThan:  evalGreaterThan,
	OpGreaterEqual: evalGreaterEqual,
	OpLessThan:     evalLessThan,
	OpLessEqual:    evalLessEqual,
	OpIn:           evalIn,
	OpNotIn:        evalNotIn,
	OpContains:     evalContains,
	OpNotContains:  evalNotContains,
	OpPrefixMatch:  evalPrefixMatch,
	OpIsNull:       evalIsNull,
	OpIsNotNull:    evalIsNotNull,
	OpRange:        evalRange,
	OpTextMatch:    evalTextMatch,
}

// Evaluate dispatches to the appropriate evaluator for the given operator.
// Returns false if the operator is not supported.
func Evaluate(op FilterOp, fieldValue string, filterValue interface{}) bool {
	fn, ok := evaluators[op]
	if !ok {
		return false
	}
	return fn(fieldValue, filterValue)
}

// evalEqual checks for exact string equality.
func evalEqual(fieldValue string, filterValue interface{}) bool {
	return fieldValue == toString(filterValue)
}

// evalNotEqual checks for string inequality.
func evalNotEqual(fieldValue string, filterValue interface{}) bool {
	return fieldValue != toString(filterValue)
}

// evalGreaterThan performs numeric comparison when possible, falling back to string comparison.
func evalGreaterThan(fieldValue string, filterValue interface{}) bool {
	fv, fvOK := parseFloat(fieldValue)
	cv, cvOK := parseFloat(filterValue)
	if fvOK && cvOK {
		return fv > cv
	}
	return fieldValue > toString(filterValue)
}

// evalGreaterEqual performs numeric comparison when possible, falling back to string comparison.
func evalGreaterEqual(fieldValue string, filterValue interface{}) bool {
	fv, fvOK := parseFloat(fieldValue)
	cv, cvOK := parseFloat(filterValue)
	if fvOK && cvOK {
		return fv >= cv
	}
	return fieldValue >= toString(filterValue)
}

// evalLessThan performs numeric comparison when possible, falling back to string comparison.
func evalLessThan(fieldValue string, filterValue interface{}) bool {
	fv, fvOK := parseFloat(fieldValue)
	cv, cvOK := parseFloat(filterValue)
	if fvOK && cvOK {
		return fv < cv
	}
	return fieldValue < toString(filterValue)
}

// evalLessEqual performs numeric comparison when possible, falling back to string comparison.
func evalLessEqual(fieldValue string, filterValue interface{}) bool {
	fv, fvOK := parseFloat(fieldValue)
	cv, cvOK := parseFloat(filterValue)
	if fvOK && cvOK {
		return fv <= cv
	}
	return fieldValue <= toString(filterValue)
}

// evalIn checks if fieldValue is contained in the given slice of values.
func evalIn(fieldValue string, filterValue interface{}) bool {
	values, ok := toStringSlice(filterValue)
	if !ok {
		return false
	}
	for _, v := range values {
		if fieldValue == v {
			return true
		}
	}
	return false
}

// evalNotIn checks if fieldValue is not contained in the given slice of values.
func evalNotIn(fieldValue string, filterValue interface{}) bool {
	return !evalIn(fieldValue, filterValue)
}

// evalContains checks for a case-sensitive substring match.
func evalContains(fieldValue string, filterValue interface{}) bool {
	return strings.Contains(fieldValue, toString(filterValue))
}

// evalNotContains checks that fieldValue does not contain the filter value as a substring.
func evalNotContains(fieldValue string, filterValue interface{}) bool {
	return !strings.Contains(fieldValue, toString(filterValue))
}

// evalPrefixMatch checks if fieldValue starts with the filter value.
func evalPrefixMatch(fieldValue string, filterValue interface{}) bool {
	return strings.HasPrefix(fieldValue, toString(filterValue))
}

// evalIsNull checks if the field value is an empty string.
func evalIsNull(fieldValue string, _ interface{}) bool {
	return fieldValue == ""
}

// evalIsNotNull checks if the field value is a non-empty string.
func evalIsNotNull(fieldValue string, _ interface{}) bool {
	return fieldValue != ""
}

// evalRange checks if fieldValue falls between min and max values.
// filterValue must be a [2]string{min, max} or [2]interface{}{min, max}.
func evalRange(fieldValue string, filterValue interface{}) bool {
	switch v := filterValue.(type) {
	case [2]string:
		return evalGreaterEqual(fieldValue, v[0]) && evalLessEqual(fieldValue, v[1])
	case [2]interface{}:
		return evalGreaterEqual(fieldValue, v[0]) && evalLessEqual(fieldValue, v[1])
	default:
		return false
	}
}

// evalTextMatch performs a case-insensitive substring match.
func evalTextMatch(fieldValue string, filterValue interface{}) bool {
	return strings.Contains(
		strings.ToLower(fieldValue),
		strings.ToLower(toString(filterValue)),
	)
}

// toString converts an interface value to its string representation.
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	default:
		return ""
	}
}

// parseFloat attempts to parse a value as a float64.
// Returns the parsed value and true on success, or 0 and false on failure.
func parseFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	default:
		return 0, false
	}
}

// toStringSlice converts an interface value to a string slice.
// Supports []string, []interface{} (converting elements to strings).
func toStringSlice(v interface{}) ([]string, bool) {
	switch val := v.(type) {
	case []string:
		return val, true
	case []interface{}:
		result := make([]string, len(val))
		for i, item := range val {
			result[i] = toString(item)
		}
		return result, true
	default:
		return nil, false
	}
}
