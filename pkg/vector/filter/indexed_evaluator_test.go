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

// opIndex returns an index whose Query answers ids and err for op only, so a
// test also proves the evaluator asked for the right scalar operation.
func opIndex(op interfaces.ScalarQueryOp, ids []uint64, err error) *mockScalarIndex {
	return &mockScalarIndex{
		queryFunc: func(got interfaces.ScalarQueryOp, _ interface{}) ([]uint64, error) {
			if got != op {
				return nil, nil
			}
			return ids, err
		},
	}
}

// rangeIndex returns an index whose QueryRange answers ids and err.
func rangeIndex(ids []uint64, err error) *mockScalarIndex {
	return &mockScalarIndex{
		queryRangeFunc: func(_, _ interface{}) ([]uint64, error) { return ids, err },
	}
}

// customExpr implements Expression for testing unknown type handling.
type customExpr struct{}

func (c *customExpr) Evaluate(_ map[string]string) bool { return false }
func (c *customExpr) String() string                    { return "custom" }

func TestIndexedEvaluator_Expressions(t *testing.T) {
	queryErr := errors.New("index failure")
	tests := []struct {
		name         string
		index        *mockScalarIndex // nil leaves field "f" unindexed
		expr         Expression
		wantIDs      []uint64
		wantResolved bool
		wantErr      error
	}{
		{name: "nil expression is resolved", wantResolved: true},
		{name: "unknown expression type", expr: &customExpr{}},
		{
			name:    "term indexed",
			index:   opIndex(interfaces.ScalarQueryEqual, []uint64{1, 3, 5}, nil),
			expr:    &TermExpr{Field: "f", Op: OpEqual, Value: "red"},
			wantIDs: []uint64{1, 3, 5}, wantResolved: true,
		},
		{name: "term unindexed", expr: &TermExpr{Field: "f", Op: OpEqual, Value: "red"}},
		{
			name:  "term op without scalar mapping",
			index: &mockScalarIndex{},
			expr:  &TermExpr{Field: "f", Op: OpContains, Value: "sub"},
		},
		{
			name:    "term query error",
			index:   opIndex(interfaces.ScalarQueryEqual, nil, queryErr),
			expr:    &TermExpr{Field: "f", Op: OpEqual, Value: "val"},
			wantErr: queryErr,
		},
		{
			name:    "null indexed",
			index:   opIndex(interfaces.ScalarQueryIsNull, []uint64{5, 10}, nil),
			expr:    &NullExpr{Field: "f", IsNull: true},
			wantIDs: []uint64{5, 10}, wantResolved: true,
		},
		{name: "null unindexed", expr: &NullExpr{Field: "f", IsNull: true}},
		{
			name:    "not-null query error",
			index:   opIndex(interfaces.ScalarQueryIsNotNull, nil, queryErr),
			expr:    &NullExpr{Field: "f", IsNull: false},
			wantErr: queryErr,
		},
		{
			name:    "in indexed",
			index:   opIndex(interfaces.ScalarQueryIn, []uint64{1, 3}, nil),
			expr:    &InExpr{Field: "f", Values: []string{"a", "b"}},
			wantIDs: []uint64{1, 3}, wantResolved: true,
		},
		{
			name:    "not-in indexed",
			index:   opIndex(interfaces.ScalarQueryNotIn, []uint64{7, 8}, nil),
			expr:    &InExpr{Field: "f", Values: []string{"a", "b"}, Negate: true},
			wantIDs: []uint64{7, 8}, wantResolved: true,
		},
		{name: "in unindexed", expr: &InExpr{Field: "f", Values: []string{"a"}}},
		{
			name:    "not-in query error",
			index:   opIndex(interfaces.ScalarQueryNotIn, nil, queryErr),
			expr:    &InExpr{Field: "f", Values: []string{"a"}, Negate: true},
			wantErr: queryErr,
		},
		{
			name:    "range indexed",
			index:   rangeIndex([]uint64{2, 4, 6}, nil),
			expr:    &RangeExpr{Field: "f", Min: "10", Max: "50", MinInclusive: true, MaxInclusive: true},
			wantIDs: []uint64{2, 4, 6}, wantResolved: true,
		},
		{name: "range unindexed", expr: &RangeExpr{Field: "f", Min: "10", Max: "50"}},
		{
			name:    "range query error",
			index:   rangeIndex(nil, queryErr),
			expr:    &RangeExpr{Field: "f", Min: "10", Max: "50"},
			wantErr: queryErr,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			indexes := map[string]interfaces.ScalarIndex{}
			if tt.index != nil {
				indexes["f"] = tt.index
			}

			ids, resolved, err := NewIndexedEvaluator(indexes).EvaluateWithIndexes(tt.expr)
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
		{name: "unknown op", op: LogicalOp(99), left: &sideIndex{ids: []uint64{1}}},
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

func TestMapFilterOpToScalarOp(t *testing.T) {
	tests := []struct {
		ops    []FilterOp
		wantOK bool
	}{
		{
			ops: []FilterOp{
				OpEqual, OpNotEqual, OpGreaterThan, OpGreaterEqual,
				OpLessThan, OpLessEqual, OpIn, OpNotIn, OpIsNull, OpIsNotNull,
			},
			wantOK: true,
		},
		{ops: []FilterOp{OpContains, OpNotContains, OpPrefixMatch, OpRange, OpTextMatch}},
	}
	for _, tt := range tests {
		for _, op := range tt.ops {
			if _, ok := MapFilterOpToScalarOp(op); ok != tt.wantOK {
				t.Errorf("MapFilterOpToScalarOp(%v) ok = %v, want %v", op, ok, tt.wantOK)
			}
		}
	}
}

func TestSortedSetOps(t *testing.T) {
	tests := []struct {
		name string
		fn   func(a, b []uint64) []uint64
		a, b []uint64
		want []uint64
	}{
		{"intersect overlap", intersectSorted, []uint64{1, 2, 3, 5}, []uint64{2, 3, 4}, []uint64{2, 3}},
		{"intersect disjoint", intersectSorted, []uint64{1, 2}, []uint64{3, 4}, nil},
		{"union overlap", unionSorted, []uint64{1, 3}, []uint64{2, 3}, []uint64{1, 2, 3}},
		{"union empty", unionSorted, nil, nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(tt.a, tt.b); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
