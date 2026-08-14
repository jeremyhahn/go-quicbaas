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
	"sort"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// filterOpToScalarOp maps FilterOp to the corresponding ScalarQueryOp.
var filterOpToScalarOp = map[FilterOp]interfaces.ScalarQueryOp{
	OpEqual:        interfaces.ScalarQueryEqual,
	OpNotEqual:     interfaces.ScalarQueryNotEqual,
	OpGreaterThan:  interfaces.ScalarQueryGreaterThan,
	OpGreaterEqual: interfaces.ScalarQueryGreaterEqual,
	OpLessThan:     interfaces.ScalarQueryLessThan,
	OpLessEqual:    interfaces.ScalarQueryLessEqual,
	OpIn:           interfaces.ScalarQueryIn,
	OpNotIn:        interfaces.ScalarQueryNotIn,
	OpIsNull:       interfaces.ScalarQueryIsNull,
	OpIsNotNull:    interfaces.ScalarQueryIsNotNull,
}

// MapFilterOpToScalarOp converts a filter.FilterOp to interfaces.ScalarQueryOp.
// Returns the mapped operator and true if a mapping exists, or zero value and
// false if the operator cannot be mapped.
func MapFilterOpToScalarOp(op FilterOp) (interfaces.ScalarQueryOp, bool) {
	scalarOp, ok := filterOpToScalarOp[op]
	return scalarOp, ok
}

// IndexedEvaluator evaluates filter expressions using scalar indexes when
// available, falling back to brute-force metadata scan for unindexed fields.
type IndexedEvaluator struct {
	indexes map[string]interfaces.ScalarIndex // field name -> scalar index
}

// NewIndexedEvaluator creates a new IndexedEvaluator with the given index map.
func NewIndexedEvaluator(indexes map[string]interfaces.ScalarIndex) *IndexedEvaluator {
	return &IndexedEvaluator{indexes: indexes}
}

// EvaluateWithIndexes returns the set of vector IDs matching the expression.
// It returns:
//   - matchingIDs: the set of vector IDs that match the expression
//   - fullyResolved: true if all fields had indexes and the result is complete,
//     false if some fields need brute-force metadata scan
//   - error: any error encountered during evaluation
func (e *IndexedEvaluator) EvaluateWithIndexes(expr Expression) ([]uint64, bool, error) {
	if expr == nil {
		return nil, true, nil
	}
	return e.evalExpr(expr)
}

func (e *IndexedEvaluator) evalExpr(expr Expression) ([]uint64, bool, error) {
	switch ex := expr.(type) {
	case *TermExpr:
		return e.evalTerm(ex)
	case *LogicalExpr:
		return e.evalLogical(ex)
	case *NullExpr:
		return e.evalNull(ex)
	case *InExpr:
		return e.evalIn(ex)
	case *RangeExpr:
		return e.evalRange(ex)
	default:
		return nil, false, nil
	}
}

func (e *IndexedEvaluator) evalTerm(expr *TermExpr) ([]uint64, bool, error) {
	idx, ok := e.indexes[expr.Field]
	if !ok {
		return nil, false, nil
	}
	scalarOp, mapped := MapFilterOpToScalarOp(expr.Op)
	if !mapped {
		return nil, false, nil
	}
	ids, err := idx.Query(scalarOp, expr.Value)
	if err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

func (e *IndexedEvaluator) evalLogical(expr *LogicalExpr) ([]uint64, bool, error) {
	switch expr.Op {
	case LogicalAND:
		return e.evalLogicalAND(expr)
	case LogicalOR:
		return e.evalLogicalOR(expr)
	case LogicalNOT:
		return e.evalLogicalNOT(expr)
	default:
		return nil, false, nil
	}
}

func (e *IndexedEvaluator) evalNull(expr *NullExpr) ([]uint64, bool, error) {
	idx, ok := e.indexes[expr.Field]
	if !ok {
		return nil, false, nil
	}
	var op interfaces.ScalarQueryOp
	if expr.IsNull {
		op = interfaces.ScalarQueryIsNull
	} else {
		op = interfaces.ScalarQueryIsNotNull
	}
	ids, err := idx.Query(op, nil)
	if err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

func (e *IndexedEvaluator) evalIn(expr *InExpr) ([]uint64, bool, error) {
	idx, ok := e.indexes[expr.Field]
	if !ok {
		return nil, false, nil
	}
	var op interfaces.ScalarQueryOp
	if expr.Negate {
		op = interfaces.ScalarQueryNotIn
	} else {
		op = interfaces.ScalarQueryIn
	}
	ids, err := idx.Query(op, expr.Values)
	if err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

func (e *IndexedEvaluator) evalRange(expr *RangeExpr) ([]uint64, bool, error) {
	idx, ok := e.indexes[expr.Field]
	if !ok {
		return nil, false, nil
	}
	ids, err := idx.QueryRange(expr.Min, expr.Max)
	if err != nil {
		return nil, false, err
	}
	return ids, true, nil
}

func (e *IndexedEvaluator) evalLogicalAND(expr *LogicalExpr) ([]uint64, bool, error) {
	leftIDs, leftResolved, err := e.evalExpr(expr.Left)
	if err != nil {
		return nil, false, err
	}
	rightIDs, rightResolved, err := e.evalExpr(expr.Right)
	if err != nil {
		return nil, false, err
	}

	// Both sides resolved: compute intersection.
	if leftResolved && rightResolved {
		return intersectSorted(leftIDs, rightIDs), true, nil
	}

	// One side resolved: return partial result, not fully resolved.
	if leftResolved {
		return leftIDs, false, nil
	}
	if rightResolved {
		return rightIDs, false, nil
	}

	// Neither side resolved.
	return nil, false, nil
}

func (e *IndexedEvaluator) evalLogicalOR(expr *LogicalExpr) ([]uint64, bool, error) {
	leftIDs, leftResolved, err := e.evalExpr(expr.Left)
	if err != nil {
		return nil, false, err
	}
	rightIDs, rightResolved, err := e.evalExpr(expr.Right)
	if err != nil {
		return nil, false, err
	}

	// Both sides must be resolved for OR to be fully resolved.
	if leftResolved && rightResolved {
		return unionSorted(leftIDs, rightIDs), true, nil
	}

	// Cannot fully resolve OR without both sides.
	return nil, false, nil
}

func (e *IndexedEvaluator) evalLogicalNOT(expr *LogicalExpr) ([]uint64, bool, error) {
	// NOT cannot be fully resolved with indexes alone since we don't
	// know the full universe of vector IDs.
	_, resolved, err := e.evalExpr(expr.Left)
	if err != nil {
		return nil, false, err
	}
	if !resolved {
		return nil, false, nil
	}
	// Even if the inner expression is resolved, NOT requires knowledge
	// of the full set to compute the complement.
	return nil, false, nil
}

// intersectSorted returns the intersection of two sorted uint64 slices.
func intersectSorted(a, b []uint64) []uint64 {
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })

	var result []uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			result = append(result, a[i])
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return result
}

// unionSorted returns the union of two sorted uint64 slices.
func unionSorted(a, b []uint64) []uint64 {
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })

	var result []uint64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			result = append(result, a[i])
			i++
			j++
		case a[i] < b[j]:
			result = append(result, a[i])
			i++
		default:
			result = append(result, b[j])
			j++
		}
	}
	for ; i < len(a); i++ {
		result = append(result, a[i])
	}
	for ; j < len(b); j++ {
		result = append(result, b[j])
	}
	return result
}
