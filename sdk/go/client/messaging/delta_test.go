// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package messaging

import (
	"encoding/json"
	"testing"
)

// =============================================================================
// applyAppend branch coverage
// =============================================================================

func TestDeltaApplier_AppendToString(t *testing.T) {
	type Doc struct {
		Body string `json:"body"`
	}
	applier := NewDeltaApplier[Doc]()

	t.Run("append string to string field", func(t *testing.T) {
		doc := Doc{Body: "hello"}
		delta := &DeltaPayload{Changes: []FieldChange{
			{Path: "body", Operation: OpAppend, Value: " world"},
		}}
		result, err := applier.Apply(doc, delta)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Body != "hello world" {
			t.Errorf("expected 'hello world', got %q", result.Body)
		}
	})

	t.Run("append non-string to string field errors", func(t *testing.T) {
		doc := Doc{Body: "hello"}
		delta := &DeltaPayload{Changes: []FieldChange{
			{Path: "body", Operation: OpAppend, Value: float64(42)},
		}}
		if _, err := applier.Apply(doc, delta); err == nil {
			t.Fatal("expected error appending non-string to string field")
		}
	})
}

func TestDeltaApplier_AppendToNonAppendable(t *testing.T) {
	type Data struct {
		Count int `json:"count"`
	}
	applier := NewDeltaApplier[Data]()

	doc := Data{Count: 5}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "count", Operation: OpAppend, Value: "x"},
	}}
	if _, err := applier.Apply(doc, delta); err == nil {
		t.Fatal("expected error appending to a numeric (non-array/string) field")
	}
}

// =============================================================================
// applyChange branch coverage
// =============================================================================

func TestDeltaApplier_PathElementNotAMap(t *testing.T) {
	type Data struct {
		Name string `json:"name"`
	}
	applier := NewDeltaApplier[Data]()

	// "name" is a string, so navigating into "name.sub" should fail because
	// the intermediate path element is not a map.
	doc := Data{Name: "scalar"}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "name.sub", Operation: OpSet, Value: "v"},
	}}
	if _, err := applier.Apply(doc, delta); err == nil {
		t.Fatal("expected error when intermediate path element is not a map")
	}
}

func TestDeltaApplier_SetCreatesIntermediateMap(t *testing.T) {
	type Nested struct {
		A struct {
			B string `json:"b"`
		} `json:"a"`
	}
	applier := NewDeltaApplier[Nested]()

	var doc Nested
	// "a.b" — the "a" object marshals to an empty (but present) map, so to
	// exercise the create-intermediate-map branch we use a deeper path where
	// an interior element does not yet exist.
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "missing.deep.b", Operation: OpSet, Value: "value"},
	}}
	if _, err := applier.Apply(doc, delta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeltaApplier_NonexistentPathNonSetIsNoop(t *testing.T) {
	type Data struct {
		Name string `json:"name"`
	}
	applier := NewDeltaApplier[Data]()

	doc := Data{Name: "x"}
	// Delete on a path whose parent does not exist should be a no-op.
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "missing.field", Operation: OpDelete},
	}}
	result, err := applier.Apply(doc, delta)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "x" {
		t.Errorf("expected unchanged entity, got %q", result.Name)
	}
}

// =============================================================================
// applyIncrement branch coverage
// =============================================================================

func TestDeltaApplier_IncrementNonNumericExisting(t *testing.T) {
	type Data struct {
		Field string `json:"field"`
	}
	applier := NewDeltaApplier[Data]()

	doc := Data{Field: "not-a-number"}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "field", Operation: OpIncrement, Value: float64(1)},
	}}
	if _, err := applier.Apply(doc, delta); err == nil {
		t.Fatal("expected error incrementing a non-numeric existing field")
	}
}

func TestDeltaApplier_IncrementInvalidIncrementValue(t *testing.T) {
	type Counter struct {
		Value int `json:"value"`
	}
	applier := NewDeltaApplier[Counter]()

	counter := Counter{Value: 1}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "value", Operation: OpIncrement, Value: "not-numeric"},
	}}
	if _, err := applier.Apply(counter, delta); err == nil {
		t.Fatal("expected error when increment value is not numeric")
	}
}

// =============================================================================
// toFloat64 branch coverage
// =============================================================================

func TestDeltaApplier_ToFloat64_NumericTypes(t *testing.T) {
	type Counter struct {
		Value int64 `json:"value"`
	}
	applier := NewDeltaApplier[Counter]()

	// Each increment Value reaches toFloat64 directly via change.Value.
	cases := []struct {
		name  string
		value interface{}
		want  int64
	}{
		{"float32", float32(2), 12},
		{"int", int(3), 13},
		{"int8", int8(4), 14},
		{"int16", int16(5), 15},
		{"int32", int32(6), 16},
		{"int64", int64(7), 17},
		{"uint", uint(8), 18},
		{"uint8", uint8(9), 19},
		{"uint16", uint16(10), 20},
		{"uint32", uint32(11), 21},
		{"uint64", uint64(12), 22},
		{"json.Number", json.Number("13"), 23},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counter := Counter{Value: 10}
			delta := &DeltaPayload{Changes: []FieldChange{
				{Path: "value", Operation: OpIncrement, Value: tc.value},
			}}
			result, err := applier.Apply(counter, delta)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Value != tc.want {
				t.Errorf("expected Value=%d, got %d", tc.want, result.Value)
			}
		})
	}
}

func TestDeltaApplier_ToFloat64_InvalidJSONNumber(t *testing.T) {
	type Counter struct {
		Value int `json:"value"`
	}
	applier := NewDeltaApplier[Counter]()

	counter := Counter{Value: 1}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "value", Operation: OpIncrement, Value: json.Number("not-a-number")},
	}}
	if _, err := applier.Apply(counter, delta); err == nil {
		t.Fatal("expected error for malformed json.Number increment")
	}
}

func TestDeltaApplier_ToFloat64_NilValue(t *testing.T) {
	type Counter struct {
		Value int `json:"value"`
	}
	applier := NewDeltaApplier[Counter]()

	counter := Counter{Value: 1}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "value", Operation: OpIncrement, Value: nil},
	}}
	if _, err := applier.Apply(counter, delta); err == nil {
		t.Fatal("expected error for nil increment value")
	}
}

// =============================================================================
// toMap / fromMap error coverage
// =============================================================================

// unmarshalableEntity has a channel field, which causes json.Marshal to fail,
// exercising the toMap error branch.
type unmarshalableEntity struct {
	Ch chan int `json:"ch"`
}

func TestDeltaApplier_ToMapError(t *testing.T) {
	applier := NewDeltaApplier[unmarshalableEntity]()

	entity := unmarshalableEntity{Ch: make(chan int)}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "ch", Operation: OpSet, Value: "x"},
	}}
	if _, err := applier.Apply(entity, delta); err == nil {
		t.Fatal("expected error converting entity with channel field to map")
	}
}

func TestDeltaApplier_FromMapError(t *testing.T) {
	type Data struct {
		Name string `json:"name"`
	}
	applier := NewDeltaApplier[Data]()

	// Set the string field "name" to a numeric value. The map round-trips
	// through JSON fine, but unmarshalling a number into a string field fails,
	// exercising the fromMap error branch.
	doc := Data{Name: "ok"}
	delta := &DeltaPayload{Changes: []FieldChange{
		{Path: "name", Operation: OpSet, Value: float64(42)},
	}}
	if _, err := applier.Apply(doc, delta); err == nil {
		t.Fatal("expected error converting map back to entity")
	}
}
