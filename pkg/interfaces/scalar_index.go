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

import "strconv"

// ScalarQueryOp represents query operators for scalar index lookups.
type ScalarQueryOp int

const (
	// ScalarQueryEqual matches exact value equality.
	ScalarQueryEqual ScalarQueryOp = iota
	// ScalarQueryNotEqual matches when values are not equal.
	ScalarQueryNotEqual
	// ScalarQueryGreaterThan matches when indexed value is greater than the query value.
	ScalarQueryGreaterThan
	// ScalarQueryGreaterEqual matches when indexed value is greater than or equal to the query value.
	ScalarQueryGreaterEqual
	// ScalarQueryLessThan matches when indexed value is less than the query value.
	ScalarQueryLessThan
	// ScalarQueryLessEqual matches when indexed value is less than or equal to the query value.
	ScalarQueryLessEqual
	// ScalarQueryIn matches when indexed value is in a set of values.
	ScalarQueryIn
	// ScalarQueryNotIn matches when indexed value is not in a set of values.
	ScalarQueryNotIn
	// ScalarQueryRange matches when indexed value is between min and max.
	ScalarQueryRange
	// ScalarQueryIsNull matches when the indexed value is empty or absent.
	ScalarQueryIsNull
	// ScalarQueryIsNotNull matches when the indexed value is non-empty.
	ScalarQueryIsNotNull
)

// scalarQueryOpNames maps scalar query operators to display names.
var scalarQueryOpNames = map[ScalarQueryOp]string{
	ScalarQueryEqual:        "=",
	ScalarQueryNotEqual:     "!=",
	ScalarQueryGreaterThan:  ">",
	ScalarQueryGreaterEqual: ">=",
	ScalarQueryLessThan:     "<",
	ScalarQueryLessEqual:    "<=",
	ScalarQueryIn:           "IN",
	ScalarQueryNotIn:        "NOT IN",
	ScalarQueryRange:        "RANGE",
	ScalarQueryIsNull:       "IS NULL",
	ScalarQueryIsNotNull:    "IS NOT NULL",
}

// String returns a human-readable name for the scalar query operator.
func (op ScalarQueryOp) String() string {
	name, ok := scalarQueryOpNames[op]
	if !ok {
		return "UNKNOWN(" + strconv.Itoa(int(op)) + ")"
	}
	return name
}

// ScalarIndexType represents the type of scalar index.
type ScalarIndexType int

const (
	// ScalarIndexBitmap uses roaring bitmaps for low-cardinality fields.
	ScalarIndexBitmap ScalarIndexType = iota
	// ScalarIndexInverted uses hash-based inverted posting lists for high-cardinality strings.
	ScalarIndexInverted
	// ScalarIndexSorted uses a B-tree for numeric range queries.
	ScalarIndexSorted
)

// scalarIndexTypeNames maps index types to display names.
var scalarIndexTypeNames = map[ScalarIndexType]string{
	ScalarIndexBitmap:   "BITMAP",
	ScalarIndexInverted: "INVERTED",
	ScalarIndexSorted:   "SORTED",
}

// String returns a human-readable name for the scalar index type.
func (t ScalarIndexType) String() string {
	name, ok := scalarIndexTypeNames[t]
	if !ok {
		return "UNKNOWN(" + strconv.Itoa(int(t)) + ")"
	}
	return name
}

// ScalarIndex defines the interface for scalar field indexes in QuicBaaS.
// These indexes accelerate metadata filtering during vector search by
// maintaining pre-computed data structures for field values.
type ScalarIndex interface {
	// Add indexes a value for the given vector ID.
	Add(vectorID uint64, value interface{}) error
	// Remove removes the index entry for the given vector ID.
	Remove(vectorID uint64) error
	// Query returns vector IDs matching the given operator and value.
	Query(op ScalarQueryOp, value interface{}) ([]uint64, error)
	// QueryRange returns vector IDs with values between min and max (inclusive).
	QueryRange(min, max interface{}) ([]uint64, error)
	// Type returns the scalar index type.
	Type() ScalarIndexType
	// Count returns the number of indexed entries.
	Count() int64
	// Close releases index resources.
	Close() error
}

// ScalarIndexConfig describes the configuration for a scalar field index.
type ScalarIndexConfig struct {
	// Field is the metadata field name to index.
	Field string
	// IndexType is the scalar index algorithm to use.
	IndexType ScalarIndexType
}
