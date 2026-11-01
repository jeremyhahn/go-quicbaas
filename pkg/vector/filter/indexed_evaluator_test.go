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
	"slices"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockScalarIndex is a test double for interfaces.ScalarIndex.
type mockScalarIndex struct {
	queryFunc      func(op interfaces.ScalarQueryOp, value interface{}) ([]uint64, error)
	queryRangeFunc func(min, max interface{}) ([]uint64, error)
	indexType      interfaces.ScalarIndexType
	count          int64
}

func (m *mockScalarIndex) Add(_ uint64, _ interface{}) error { return nil }
func (m *mockScalarIndex) Remove(_ uint64) error             { return nil }
func (m *mockScalarIndex) Query(op interfaces.ScalarQueryOp, value interface{}) ([]uint64, error) {
	if m.queryFunc != nil {
		return m.queryFunc(op, value)
	}
	return nil, nil
}
func (m *mockScalarIndex) QueryRange(min, max interface{}) ([]uint64, error) {
	if m.queryRangeFunc != nil {
		return m.queryRangeFunc(min, max)
	}
	return nil, nil
}
func (m *mockScalarIndex) Type() interfaces.ScalarIndexType { return m.indexType }
func (m *mockScalarIndex) Count() int64                     { return m.count }
func (m *mockScalarIndex) Close() error                     { return nil }

func TestIndexedEvaluator_TermExpr_Indexed(t *testing.T) {
	idx := &mockScalarIndex{
		queryFunc: func(op interfaces.ScalarQueryOp, value interface{}) ([]uint64, error) {
			if op == interfaces.ScalarQueryEqual {
				return []uint64{1, 3, 5}, nil
			}
			return nil, nil
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"color": idx})

	expr := &TermExpr{Field: "color", Op: OpEqual, Value: "red"}
	ids, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Error("expected fullyResolved=true")
	}
	if len(ids) != 3 || ids[0] != 1 || ids[1] != 3 || ids[2] != 5 {
		t.Errorf("ids = %v, want [1, 3, 5]", ids)
	}
}

func TestIndexedEvaluator_TermExpr_Unindexed(t *testing.T) {
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{})

	expr := &TermExpr{Field: "color", Op: OpEqual, Value: "red"}
	ids, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("expected fullyResolved=false for unindexed field")
	}
	if ids != nil {
		t.Errorf("ids = %v, want nil", ids)
	}
}

func TestIndexedEvaluator_TermExpr_UnmappedOp(t *testing.T) {
	idx := &mockScalarIndex{}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"field": idx})

	// OpContains has no scalar mapping.
	expr := &TermExpr{Field: "field", Op: OpContains, Value: "sub"}
	ids, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("expected fullyResolved=false for unmapped op")
	}
	if ids != nil {
		t.Errorf("ids = %v, want nil", ids)
	}
}

func TestIndexedEvaluator_TermExpr_QueryError(t *testing.T) {
	queryErr := errors.New("index failure")
	idx := &mockScalarIndex{
		queryFunc: func(_ interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
			return nil, queryErr
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"field": idx})

	expr := &TermExpr{Field: "field", Op: OpEqual, Value: "val"}
	_, _, err := eval.EvaluateWithIndexes(expr)
	if err == nil {
		t.Fatal("expected error from query")
	}
	if !errors.Is(err, queryErr) {
		t.Errorf("err = %v, want %v", err, queryErr)
	}
}

func TestIndexedEvaluator_NilExpr(t *testing.T) {
	eval := NewIndexedEvaluator(nil)

	ids, resolved, err := eval.EvaluateWithIndexes(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Error("expected fullyResolved=true for nil expression")
	}
	if ids != nil {
		t.Errorf("ids = %v, want nil", ids)
	}
}

// sideIndex is what the index of one side of a logical expression returns.
// A nil *sideIndex leaves that side's field unindexed.
type sideIndex struct {
	ids []uint64
	err error
}

func TestIndexedEvaluator_Logical(t *testing.T) {
	leftErr := errors.New("left error")
	rightErr := errors.New("right error")
	tests := []struct {
		name         string
		op           LogicalOp
		left, right  *sideIndex
		wantIDs      []uint64
		wantResolved bool
		wantErr      error
	}{
		{
			name: "AND both resolved intersects", op: LogicalAND,
			left: &sideIndex{ids: []uint64{1, 2, 3, 5}}, right: &sideIndex{ids: []uint64{2, 3, 4}},
			wantIDs: []uint64{2, 3}, wantResolved: true,
		},
		{
			name: "AND left only resolved returns left", op: LogicalAND,
			left: &sideIndex{ids: []uint64{1, 2, 3}}, wantIDs: []uint64{1, 2, 3},
		},
		{
			name: "AND right only resolved returns right", op: LogicalAND,
			right: &sideIndex{ids: []uint64{4, 5}}, wantIDs: []uint64{4, 5},
		},
		{name: "AND neither resolved", op: LogicalAND},
		{
			name: "AND left error", op: LogicalAND,
			left: &sideIndex{err: leftErr}, wantErr: leftErr,
		},
		{
			name: "AND right error", op: LogicalAND,
			left: &sideIndex{ids: []uint64{1}}, right: &sideIndex{err: rightErr}, wantErr: rightErr,
		},
		{
			name: "OR both resolved unions", op: LogicalOR,
			left: &sideIndex{ids: []uint64{1, 2}}, right: &sideIndex{ids: []uint64{2, 3}},
			wantIDs: []uint64{1, 2, 3}, wantResolved: true,
		},
		{
			name: "OR one side unresolved", op: LogicalOR,
			left: &sideIndex{ids: []uint64{1, 2}},
		},
		{
			name: "OR left error", op: LogicalOR,
			left: &sideIndex{err: leftErr}, wantErr: leftErr,
		},
		{
			name: "OR right error", op: LogicalOR,
			left: &sideIndex{ids: []uint64{1}}, right: &sideIndex{err: rightErr}, wantErr: rightErr,
		},
		{
			name: "NOT resolved inner is not resolved", op: LogicalNOT,
			left: &sideIndex{ids: []uint64{1, 2}},
		},
		{name: "NOT unresolved inner", op: LogicalNOT},
		{
			name: "NOT inner error", op: LogicalNOT,
			left: &sideIndex{err: leftErr}, wantErr: leftErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexes := map[string]interfaces.ScalarIndex{}
			for field, side := range map[string]*sideIndex{"left": tt.left, "right": tt.right} {
				if side == nil {
					continue
				}
				indexes[field] = &mockScalarIndex{
					queryFunc: func(_ interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
						return side.ids, side.err
					},
				}
			}
			expr := &LogicalExpr{Op: tt.op, Left: &TermExpr{Field: "left", Op: OpEqual, Value: "x"}}
			if tt.op != LogicalNOT {
				expr.Right = &TermExpr{Field: "right", Op: OpEqual, Value: "y"}
			}

			ids, resolved, err := NewIndexedEvaluator(indexes).EvaluateWithIndexes(expr)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if resolved != tt.wantResolved {
				t.Errorf("fullyResolved = %v, want %v", resolved, tt.wantResolved)
			}
			if (tt.wantIDs == nil) != (ids == nil) || !slices.Equal(ids, tt.wantIDs) {
				t.Errorf("ids = %v, want %v", ids, tt.wantIDs)
			}
		})
	}
}

func TestIndexedEvaluator_NullExpr_IsNull(t *testing.T) {
	idx := &mockScalarIndex{
		queryFunc: func(op interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
			if op == interfaces.ScalarQueryIsNull {
				return []uint64{5, 10}, nil
			}
			return nil, nil
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"field": idx})

	expr := &NullExpr{Field: "field", IsNull: true}
	ids, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Error("expected fullyResolved=true")
	}
	if len(ids) != 2 || ids[0] != 5 || ids[1] != 10 {
		t.Errorf("ids = %v, want [5, 10]", ids)
	}
}

func TestIndexedEvaluator_NullExpr_Unindexed(t *testing.T) {
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{})

	expr := &NullExpr{Field: "field", IsNull: true}
	_, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("expected fullyResolved=false for unindexed field")
	}
}

func TestIndexedEvaluator_NullExpr_QueryError(t *testing.T) {
	queryErr := errors.New("null query error")
	idx := &mockScalarIndex{
		queryFunc: func(_ interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
			return nil, queryErr
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"field": idx})

	expr := &NullExpr{Field: "field", IsNull: false}
	_, _, err := eval.EvaluateWithIndexes(expr)
	if !errors.Is(err, queryErr) {
		t.Errorf("err = %v, want %v", err, queryErr)
	}
}

func TestIndexedEvaluator_InExpr_Indexed(t *testing.T) {
	idx := &mockScalarIndex{
		queryFunc: func(op interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
			if op == interfaces.ScalarQueryIn {
				return []uint64{1, 3}, nil
			}
			return nil, nil
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"field": idx})

	expr := &InExpr{Field: "field", Values: []string{"a", "b"}, Negate: false}
	ids, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Error("expected fullyResolved=true")
	}
	if len(ids) != 2 {
		t.Errorf("ids = %v, want 2 results", ids)
	}
}

func TestIndexedEvaluator_InExpr_Unindexed(t *testing.T) {
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{})

	expr := &InExpr{Field: "field", Values: []string{"a"}, Negate: false}
	_, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("expected fullyResolved=false")
	}
}

func TestIndexedEvaluator_InExpr_QueryError(t *testing.T) {
	queryErr := errors.New("in query error")
	idx := &mockScalarIndex{
		queryFunc: func(_ interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
			return nil, queryErr
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"field": idx})

	expr := &InExpr{Field: "field", Values: []string{"a"}, Negate: true}
	_, _, err := eval.EvaluateWithIndexes(expr)
	if !errors.Is(err, queryErr) {
		t.Errorf("err = %v, want %v", err, queryErr)
	}
}

func TestIndexedEvaluator_RangeExpr_Indexed(t *testing.T) {
	idx := &mockScalarIndex{
		queryRangeFunc: func(_, _ interface{}) ([]uint64, error) {
			return []uint64{2, 4, 6}, nil
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"price": idx})

	expr := &RangeExpr{Field: "price", Min: "10", Max: "50", MinInclusive: true, MaxInclusive: true}
	ids, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Error("expected fullyResolved=true")
	}
	if len(ids) != 3 {
		t.Errorf("ids = %v, want 3 results", ids)
	}
}

func TestIndexedEvaluator_RangeExpr_Unindexed(t *testing.T) {
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{})

	expr := &RangeExpr{Field: "price", Min: "10", Max: "50"}
	_, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("expected fullyResolved=false")
	}
}

func TestIndexedEvaluator_RangeExpr_QueryError(t *testing.T) {
	queryErr := errors.New("range error")
	idx := &mockScalarIndex{
		queryRangeFunc: func(_, _ interface{}) ([]uint64, error) {
			return nil, queryErr
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"price": idx})

	expr := &RangeExpr{Field: "price", Min: "10", Max: "50"}
	_, _, err := eval.EvaluateWithIndexes(expr)
	if !errors.Is(err, queryErr) {
		t.Errorf("err = %v, want %v", err, queryErr)
	}
}

func TestMapFilterOpToScalarOp_Mapped(t *testing.T) {
	mapped := []FilterOp{
		OpEqual, OpNotEqual, OpGreaterThan, OpGreaterEqual,
		OpLessThan, OpLessEqual, OpIn, OpNotIn, OpIsNull, OpIsNotNull,
	}
	for _, op := range mapped {
		_, ok := MapFilterOpToScalarOp(op)
		if !ok {
			t.Errorf("MapFilterOpToScalarOp(%v) returned false, want true", op)
		}
	}
}

func TestMapFilterOpToScalarOp_Unmapped(t *testing.T) {
	unmapped := []FilterOp{OpContains, OpNotContains, OpPrefixMatch, OpRange, OpTextMatch}
	for _, op := range unmapped {
		_, ok := MapFilterOpToScalarOp(op)
		if ok {
			t.Errorf("MapFilterOpToScalarOp(%v) returned true, want false", op)
		}
	}
}

func TestIntersectSorted(t *testing.T) {
	result := intersectSorted([]uint64{1, 2, 3, 5}, []uint64{2, 3, 4})
	if len(result) != 2 || result[0] != 2 || result[1] != 3 {
		t.Errorf("intersectSorted = %v, want [2, 3]", result)
	}
}

func TestIntersectSorted_Empty(t *testing.T) {
	result := intersectSorted([]uint64{1, 2}, []uint64{3, 4})
	if len(result) != 0 {
		t.Errorf("intersectSorted = %v, want []", result)
	}
}

func TestUnionSorted(t *testing.T) {
	result := unionSorted([]uint64{1, 3}, []uint64{2, 3})
	if len(result) != 3 || result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("unionSorted = %v, want [1, 2, 3]", result)
	}
}

func TestUnionSorted_Empty(t *testing.T) {
	result := unionSorted(nil, nil)
	if len(result) != 0 {
		t.Errorf("unionSorted(nil, nil) = %v, want []", result)
	}
}

func TestIndexedEvaluator_UnknownExprType(t *testing.T) {
	eval := NewIndexedEvaluator(nil)

	// Use a custom expression type that the evaluator doesn't know about.
	unknownExpr := &customExpr{}
	_, resolved, err := eval.EvaluateWithIndexes(unknownExpr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("expected fullyResolved=false for unknown expression type")
	}
}

// customExpr implements Expression for testing unknown type handling.
type customExpr struct{}

func (c *customExpr) Evaluate(_ map[string]string) bool { return false }
func (c *customExpr) String() string                    { return "custom" }

func TestIndexedEvaluator_UnknownLogicalOp(t *testing.T) {
	eval := NewIndexedEvaluator(nil)

	expr := &LogicalExpr{
		Op:   LogicalOp(99),
		Left: &TermExpr{Field: "f", Op: OpEqual, Value: "v"},
	}
	_, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved {
		t.Error("expected fullyResolved=false for unknown logical op")
	}
}

func TestIndexedEvaluator_InExpr_Negated(t *testing.T) {
	idx := &mockScalarIndex{
		queryFunc: func(op interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
			if op == interfaces.ScalarQueryNotIn {
				return []uint64{7, 8}, nil
			}
			return nil, nil
		},
	}
	eval := NewIndexedEvaluator(map[string]interfaces.ScalarIndex{"field": idx})

	expr := &InExpr{Field: "field", Values: []string{"a", "b"}, Negate: true}
	ids, resolved, err := eval.EvaluateWithIndexes(expr)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resolved {
		t.Error("expected fullyResolved=true")
	}
	if len(ids) != 2 || ids[0] != 7 || ids[1] != 8 {
		t.Errorf("ids = %v, want [7, 8]", ids)
	}
}
