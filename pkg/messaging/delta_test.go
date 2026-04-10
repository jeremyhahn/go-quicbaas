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

package messaging

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// testEntity is a simple test struct for delta computation.
type testEntity struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Age       int       `json:"age"`
	Active    bool      `json:"active"`
	Score     float64   `json:"score"`
	Tags      []string  `json:"tags"`
	UpdatedAt time.Time `json:"updated_at"`
}

// testNestedEntity has nested structs for testing recursion.
type testNestedEntity struct {
	ID      string       `json:"id"`
	Profile *testProfile `json:"profile,omitempty"`
	Config  testConfig   `json:"config"`
}

type testProfile struct {
	Bio   string `json:"bio"`
	Email string `json:"email"`
}

type testConfig struct {
	Theme    string `json:"theme"`
	Language string `json:"language"`
}

// testDiffable implements the Diffable interface for optimized comparison.
type testDiffable struct {
	ID      string `json:"id"`
	Value   int    `json:"value"`
	version uint64
}

func (d *testDiffable) Diff(previous interface{}) ([]FieldChange, error) {
	if previous == nil {
		return []FieldChange{
			{Path: "id", Operation: OpSet, Value: d.ID},
			{Path: "value", Operation: OpSet, Value: d.Value},
		}, nil
	}
	prev, ok := previous.(*testDiffable)
	if !ok {
		return nil, &DeltaTypeMismatchError{OldType: "unknown", NewType: "testDiffable"}
	}
	var changes []FieldChange
	if d.ID != prev.ID {
		changes = append(changes, FieldChange{Path: "id", Operation: OpSet, Value: d.ID, OldValue: prev.ID})
	}
	if d.Value != prev.Value {
		changes = append(changes, FieldChange{Path: "value", Operation: OpSet, Value: d.Value, OldValue: prev.Value})
	}
	return changes, nil
}

func (d *testDiffable) Apply(changes []FieldChange) error {
	for _, c := range changes {
		switch c.Path {
		case "id":
			d.ID = c.Value.(string)
		case "value":
			d.Value = c.Value.(int)
		}
	}
	return nil
}

func (d *testDiffable) Version() uint64 {
	return d.version
}

// entityWithMapKeys is a test struct with map fields for new entity extraction.
type entityWithMapKeys struct {
	ID   string         `json:"id"`
	Data map[int]string `json:"data"`
}

func newDeltaTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	return log
}

func TestNewDeltaComputer(t *testing.T) {
	t.Parallel()

	t.Run("creates with default options", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		if dc == nil {
			t.Fatal("expected non-nil DeltaComputer")
		}
		if dc.maxDepth != defaultDeltaMaxDepth {
			t.Errorf("expected maxDepth %d, got %d", defaultDeltaMaxDepth, dc.maxDepth)
		}
		if !dc.ignoreZero {
			t.Error("expected ignoreZero to be true by default")
		}
	})

	t.Run("creates with custom max depth", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log, WithDeltaMaxDepth(5))

		if dc.maxDepth != 5 {
			t.Errorf("expected maxDepth 5, got %d", dc.maxDepth)
		}
	})

	t.Run("ignores invalid max depth", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log, WithDeltaMaxDepth(-1))

		if dc.maxDepth != defaultDeltaMaxDepth {
			t.Errorf("expected default maxDepth, got %d", dc.maxDepth)
		}
	})

	t.Run("creates with ignoreZero disabled", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log, WithDeltaIgnoreZeroChanges(false))

		if dc.ignoreZero {
			t.Error("expected ignoreZero to be false")
		}
	})

	t.Run("creates with custom codec", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		codec := &jsonPushCodec{}
		dc := NewDeltaComputer(log, WithDeltaCodec(codec))
		require.NotNil(t, dc)
		assert.Equal(t, codec, dc.codec)
	})

	t.Run("ignores nil codec", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log, WithDeltaCodec(nil))
		require.NotNil(t, dc)
		// Default codec should still be set
		assert.NotNil(t, dc.codec)
	})
}

func TestDeltaComputer_Compute(t *testing.T) {
	t.Parallel()

	t.Run("detects simple field changes", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testEntity{ID: "1", Name: "Alice", Age: 25}
		newVal := &testEntity{ID: "1", Name: "Bob", Age: 25}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if delta == nil {
			t.Fatal("expected non-nil delta")
		}

		found := false
		for _, c := range delta.Changes {
			if c.Path == "name" && c.Value == "Bob" {
				found = true
			}
		}
		if !found {
			t.Error("expected name change in delta")
		}
	})

	t.Run("nil old value creates new entity", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		newVal := &testEntity{ID: "1", Name: "Alice", Age: 30}
		delta, err := dc.Compute(nil, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) == 0 {
			t.Error("expected changes for new entity")
		}
	})

	t.Run("nil new value returns error", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		_, err := dc.Compute(&testEntity{}, nil)
		if err == nil {
			t.Error("expected error for nil new value")
		}
	})

	t.Run("both nil returns error", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		_, err := dc.Compute(nil, nil)
		if err == nil {
			t.Error("expected error for both nil")
		}
	})

	t.Run("type mismatch returns error", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		_, err := dc.Compute(&testEntity{}, &testConfig{})
		if err == nil {
			t.Error("expected error for type mismatch")
		}
	})

	t.Run("no changes when equal", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		entity := &testEntity{ID: "1", Name: "Alice", Age: 25}
		delta, err := dc.Compute(entity, entity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) != 0 {
			t.Errorf("expected no changes, got %d", len(delta.Changes))
		}
	})

	t.Run("detects boolean change", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testEntity{ID: "1", Active: false}
		newVal := &testEntity{ID: "1", Active: true}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, c := range delta.Changes {
			if c.Path == "active" {
				found = true
			}
		}
		if !found {
			t.Error("expected active change in delta")
		}
	})

	t.Run("detects float change", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testEntity{ID: "1", Score: 1.0}
		newVal := &testEntity{ID: "1", Score: 2.5}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, c := range delta.Changes {
			if c.Path == "score" {
				found = true
			}
		}
		if !found {
			t.Error("expected score change in delta")
		}
	})

	t.Run("detects slice change", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testEntity{ID: "1", Tags: []string{"a"}}
		newVal := &testEntity{ID: "1", Tags: []string{"a", "b"}}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, c := range delta.Changes {
			if c.Path == "tags" {
				found = true
			}
		}
		if !found {
			t.Error("expected tags change in delta")
		}
	})

	t.Run("handles nested struct changes", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testNestedEntity{
			ID:     "1",
			Config: testConfig{Theme: "dark", Language: "en"},
		}
		newVal := &testNestedEntity{
			ID:     "1",
			Config: testConfig{Theme: "light", Language: "en"},
		}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, c := range delta.Changes {
			if c.Path == "config.theme" {
				found = true
			}
		}
		if !found {
			t.Error("expected config.theme change in delta")
		}
	})

	t.Run("handles pointer to nested struct", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testNestedEntity{
			ID:      "1",
			Profile: &testProfile{Bio: "old bio", Email: "old@test.com"},
		}
		newVal := &testNestedEntity{
			ID:      "1",
			Profile: &testProfile{Bio: "new bio", Email: "old@test.com"},
		}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		found := false
		for _, c := range delta.Changes {
			if c.Path == "profile.bio" {
				found = true
			}
		}
		if !found {
			t.Error("expected profile.bio change in delta")
		}
	})

	t.Run("handles pointer nil to non-nil", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testNestedEntity{ID: "1", Profile: nil}
		newVal := &testNestedEntity{
			ID:      "1",
			Profile: &testProfile{Bio: "new bio"},
		}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) == 0 {
			t.Error("expected changes")
		}
	})

	t.Run("handles pointer non-nil to nil", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testNestedEntity{
			ID:      "1",
			Profile: &testProfile{Bio: "old bio"},
		}
		newVal := &testNestedEntity{ID: "1", Profile: nil}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) == 0 {
			t.Error("expected changes")
		}
	})

	t.Run("uses Diffable interface", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testDiffable{ID: "1", Value: 10, version: 1}
		newVal := &testDiffable{ID: "1", Value: 20, version: 2}

		delta, err := dc.Compute(old, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) == 0 {
			t.Error("expected changes")
		}
	})

	t.Run("uses Diffable with nil old", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		newVal := &testDiffable{ID: "1", Value: 10}
		delta, err := dc.Compute(nil, newVal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) == 0 {
			t.Error("expected changes")
		}
	})

	t.Run("max depth exceeded", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		// WithDeltaMaxDepth only accepts values > 0, so use 1.
		// With maxDepth=1, the nested Config struct causes recursion
		// to exceed the depth limit when comparing inner fields.
		dc := NewDeltaComputer(log, WithDeltaMaxDepth(1))

		old := &testNestedEntity{
			ID:     "1",
			Config: testConfig{Theme: "dark"},
		}
		newVal := &testNestedEntity{
			ID:     "1",
			Config: testConfig{Theme: "light"},
		}

		_, err := dc.Compute(old, newVal)
		if err == nil {
			t.Error("expected max depth exceeded error")
		}
		assert.True(t, IsDeltaMaxDepthExceeded(err), "expected DeltaMaxDepthExceeded error type")
	})

	t.Run("handles non-string map keys", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		newEntity := &entityWithMapKeys{
			ID: "test-1",
			Data: map[int]string{
				1: "one",
				2: "two",
			},
		}

		delta, err := dc.Compute(nil, newEntity)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(delta.Changes) == 0 {
			t.Error("expected changes")
		}
	})

	t.Run("slice cleared to nil reports OpDelete", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		dc := NewDeltaComputer(log)

		old := &testEntity{ID: "1", Tags: []string{"a", "b"}}
		newVal := &testEntity{ID: "1", Tags: nil}

		delta, err := dc.Compute(old, newVal)
		require.NoError(t, err)

		found := false
		for _, c := range delta.Changes {
			if c.Path == "tags" && c.Operation == OpDelete {
				found = true
			}
		}
		assert.True(t, found, "expected OpDelete when slice cleared to nil")
	})

	t.Run("primitive value set to zero reports OpDelete", func(t *testing.T) {
		t.Parallel()
		log := newDeltaTestLogger(t)
		// Disable ignoreZero to see the zero-value change as OpDelete
		dc := NewDeltaComputer(log, WithDeltaIgnoreZeroChanges(false))

		old := &testEntity{ID: "1", Name: "Alice"}
		newVal := &testEntity{ID: "1", Name: ""}

		delta, err := dc.Compute(old, newVal)
		require.NoError(t, err)

		found := false
		for _, c := range delta.Changes {
			if c.Path == "name" && c.Operation == OpDelete {
				found = true
			}
		}
		assert.True(t, found, "expected OpDelete when string cleared to zero value")
	})
}

// TestDeltaComputer_ComputeBytes tests JSON-based delta computation.
func TestDeltaComputer_ComputeBytes(t *testing.T) {
	t.Parallel()
	log := newDeltaTestLogger(t)
	dc := NewDeltaComputer(log)

	t.Run("computes delta from JSON bytes", func(t *testing.T) {
		t.Parallel()
		oldJSON, _ := json.Marshal(&testEntity{ID: "1", Name: "Alice"})
		newJSON, _ := json.Marshal(&testEntity{ID: "1", Name: "Bob"})

		delta, err := dc.ComputeBytes(oldJSON, newJSON, &testEntity{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) == 0 {
			t.Error("expected changes")
		}
	})

	t.Run("nil old JSON creates new entity", func(t *testing.T) {
		t.Parallel()
		newJSON, _ := json.Marshal(&testEntity{ID: "1", Name: "Alice"})

		delta, err := dc.ComputeBytes(nil, newJSON, &testEntity{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(delta.Changes) == 0 {
			t.Error("expected changes")
		}
	})

	t.Run("nil new JSON returns error", func(t *testing.T) {
		t.Parallel()
		oldJSON, _ := json.Marshal(&testEntity{ID: "1"})

		_, err := dc.ComputeBytes(oldJSON, nil, &testEntity{})
		if err == nil {
			t.Error("expected error for nil new JSON")
		}
	})

	t.Run("invalid old JSON returns error", func(t *testing.T) {
		t.Parallel()
		newJSON, _ := json.Marshal(&testEntity{ID: "1"})

		_, err := dc.ComputeBytes([]byte("invalid json"), newJSON, &testEntity{})
		if err == nil {
			t.Error("expected error for invalid old JSON")
		}
	})

	t.Run("invalid new JSON returns error", func(t *testing.T) {
		t.Parallel()
		oldJSON, _ := json.Marshal(&testEntity{ID: "1"})

		_, err := dc.ComputeBytes(oldJSON, []byte("invalid json"), &testEntity{})
		if err == nil {
			t.Error("expected error for invalid new JSON")
		}
	})

	t.Run("nil entity type returns error", func(t *testing.T) {
		t.Parallel()
		oldJSON, _ := json.Marshal(&testEntity{ID: "1"})
		newJSON, _ := json.Marshal(&testEntity{ID: "1", Name: "Bob"})

		_, err := dc.ComputeBytes(oldJSON, newJSON, nil)
		if err == nil {
			t.Error("expected error for nil entity type")
		}
	})
}

// TestDeltaComputer_NewEntityWithMapField tests extractAllFields for a new entity
// with map and slice fields. For struct-level extraction, map fields are treated
// as single values (the entire map), not individual entries.
func TestDeltaComputer_NewEntityWithMapField(t *testing.T) {
	t.Parallel()
	log := newDeltaTestLogger(t)
	dc := NewDeltaComputer(log)

	type entityWithMap struct {
		ID     string            `json:"id"`
		Labels map[string]string `json:"labels"`
		Scores []int             `json:"scores"`
	}

	newVal := &entityWithMap{
		ID:     "entity-1",
		Labels: map[string]string{"env": "prod", "tier": "web"},
		Scores: []int{95, 87, 92},
	}

	delta, err := dc.Compute(nil, newVal)
	require.NoError(t, err)
	require.NotEmpty(t, delta.Changes)

	// extractAllFields treats map struct fields as a single value with OpSet,
	// not as individual map entries. Verify the whole map and slice are present.
	foundLabels := false
	foundScores := false
	for _, c := range delta.Changes {
		if c.Path == "labels" && c.Operation == OpSet {
			foundLabels = true
		}
		if c.Path == "scores" && c.Operation == OpSet {
			foundScores = true
		}
	}
	assert.True(t, foundLabels, "expected labels extracted from new entity")
	assert.True(t, foundScores, "expected scores extracted from new entity")
}

// TestDeltaErrorTypes_ErrorMessages verifies the Error() method output for each
// delta error type, covering the six Error() methods at 0% coverage.
func TestDeltaErrorTypes_ErrorMessages(t *testing.T) {
	t.Parallel()

	t.Run("DeltaNilInputError", func(t *testing.T) {
		t.Parallel()
		err := &DeltaNilInputError{Parameter: "old entity"}
		msg := err.Error()
		assert.Contains(t, msg, "old entity")
		assert.Contains(t, msg, "non-nil")
	})

	t.Run("DeltaTypeMismatchError without path", func(t *testing.T) {
		t.Parallel()
		err := &DeltaTypeMismatchError{OldType: "string", NewType: "int"}
		msg := err.Error()
		assert.Contains(t, msg, "type mismatch")
		assert.Contains(t, msg, "string")
		assert.Contains(t, msg, "int")
	})

	t.Run("DeltaTypeMismatchError with path", func(t *testing.T) {
		t.Parallel()
		err := &DeltaTypeMismatchError{OldType: "string", NewType: "int", Path: "config.value"}
		msg := err.Error()
		assert.Contains(t, msg, "config.value")
		assert.Contains(t, msg, "string")
		assert.Contains(t, msg, "int")
	})

	t.Run("DeltaMaxDepthExceededError", func(t *testing.T) {
		t.Parallel()
		err := &DeltaMaxDepthExceededError{MaxDepth: 5, Path: "deep.nested.path"}
		msg := err.Error()
		assert.Contains(t, msg, "max recursion depth exceeded")
		assert.Contains(t, msg, "deep.nested.path")
	})

	t.Run("DeltaUnsupportedTypeError", func(t *testing.T) {
		t.Parallel()
		err := &DeltaUnsupportedTypeError{Type: "chan", Path: "config.stream"}
		msg := err.Error()
		assert.Contains(t, msg, "unsupported type")
		assert.Contains(t, msg, "chan")
		assert.Contains(t, msg, "config.stream")
	})

	t.Run("DeltaDeserializationError", func(t *testing.T) {
		t.Parallel()
		err := &DeltaDeserializationError{Cause: "invalid JSON at offset 42"}
		msg := err.Error()
		assert.Contains(t, msg, "deserialize")
		assert.Contains(t, msg, "invalid JSON at offset 42")
	})

	t.Run("DeltaDiffError", func(t *testing.T) {
		t.Parallel()
		err := &DeltaDiffError{Cause: "incompatible versions"}
		msg := err.Error()
		assert.Contains(t, msg, "diffable interface failed")
		assert.Contains(t, msg, "incompatible versions")
	})
}

// TestDeltaErrorConstructors verifies the factory functions that create DragonError
// wrappers for unsupported type and diff failure (both at 0% coverage).
func TestDeltaErrorConstructors(t *testing.T) {
	t.Parallel()

	t.Run("NewDeltaUnsupportedTypeError", func(t *testing.T) {
		t.Parallel()
		err := NewDeltaUnsupportedTypeError("test.op", "chan", "config.stream")
		require.NotNil(t, err)
		assert.Equal(t, ErrCodeDeltaUnsupportedType, err.Code)
		assert.Equal(t, "test.op", err.Op)
		assert.True(t, IsDeltaUnsupportedType(err))
	})

	t.Run("NewDeltaDiffError", func(t *testing.T) {
		t.Parallel()
		err := NewDeltaDiffError("test.op", "diff computation failed")
		require.NotNil(t, err)
		assert.Equal(t, ErrCodeDeltaDiffFailed, err.Code)
		assert.Equal(t, "test.op", err.Op)
		assert.True(t, IsDeltaDiffFailed(err))
	})
}

// TestDeltaErrorCheckers_DragonErrorWrapping tests the DragonError code path
// in each IsDelta* error checker function.
func TestDeltaErrorCheckers_DragonErrorWrapping(t *testing.T) {
	tests := []struct {
		name    string
		checker func(error) bool
		code    errors.ErrorCode
	}{
		{"IsDeltaNilInput via DragonError", IsDeltaNilInput, ErrCodeDeltaNilInput},
		{"IsDeltaTypeMismatch via DragonError", IsDeltaTypeMismatch, ErrCodeDeltaTypeMismatch},
		{"IsDeltaMaxDepthExceeded via DragonError", IsDeltaMaxDepthExceeded, ErrCodeDeltaMaxDepthExceeded},
		{"IsDeltaUnsupportedType via DragonError", IsDeltaUnsupportedType, ErrCodeDeltaUnsupportedType},
		{"IsDeltaDeserializationFailed via DragonError", IsDeltaDeserializationFailed, ErrCodeDeltaDeserializationFailed},
		{"IsDeltaDiffFailed via DragonError", IsDeltaDiffFailed, ErrCodeDeltaDiffFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dragonErr := &errors.DragonError{
				Code: tc.code,
				Op:   "test",
			}
			assert.True(t, tc.checker(dragonErr), "expected checker to return true for DragonError with matching code")
		})
	}
}

// TestDeltaErrorCheckers_NonMatchingError tests that error checkers return false
// for errors that don't match their expected type or code.
func TestDeltaErrorCheckers_NonMatchingError(t *testing.T) {
	nonMatchingErr := &errors.DragonError{
		Code: errors.ErrorCode(9999),
		Op:   "test",
	}

	assert.False(t, IsDeltaNilInput(nonMatchingErr))
	assert.False(t, IsDeltaTypeMismatch(nonMatchingErr))
	assert.False(t, IsDeltaMaxDepthExceeded(nonMatchingErr))
	assert.False(t, IsDeltaUnsupportedType(nonMatchingErr))
	assert.False(t, IsDeltaDeserializationFailed(nonMatchingErr))
	assert.False(t, IsDeltaDiffFailed(nonMatchingErr))
}

// TestDeltaComputer_CompareStructsSpecialType tests the compareStructs function
// when comparing time.Time fields (special type) that change from non-zero to zero.
func TestDeltaComputer_CompareStructsSpecialType(t *testing.T) {
	t.Parallel()
	log := newDeltaTestLogger(t)
	dc := NewDeltaComputer(log)

	now := time.Now()

	old := &testEntity{ID: "1", Name: "test", UpdatedAt: now}
	newVal := &testEntity{ID: "1", Name: "test", UpdatedAt: time.Time{}}

	delta, err := dc.Compute(old, newVal)
	require.NoError(t, err)

	found := false
	for _, c := range delta.Changes {
		if c.Path == "updated_at" && c.Operation == OpDelete {
			found = true
		}
	}
	assert.True(t, found, "expected OpDelete for cleared time field")
}

// TestDeltaComputer_MapFieldDelta tests delta computation for entities with map fields.
func TestDeltaComputer_MapFieldDelta(t *testing.T) {
	t.Parallel()
	log := newDeltaTestLogger(t)
	dc := NewDeltaComputer(log)

	type withMapField struct {
		ID   string            `json:"id"`
		Tags map[string]string `json:"tags"`
	}

	t.Run("detects added map entries", func(t *testing.T) {
		t.Parallel()
		old := &withMapField{ID: "1", Tags: map[string]string{"a": "1"}}
		newVal := &withMapField{ID: "1", Tags: map[string]string{"a": "1", "b": "2"}}

		delta, err := dc.Compute(old, newVal)
		require.NoError(t, err)

		found := false
		for _, c := range delta.Changes {
			if c.Path == "tags.b" && c.Operation == OpSet {
				found = true
			}
		}
		assert.True(t, found, "expected added map entry tags.b")
	})

	t.Run("detects removed map entries", func(t *testing.T) {
		t.Parallel()
		old := &withMapField{ID: "1", Tags: map[string]string{"a": "1", "b": "2"}}
		newVal := &withMapField{ID: "1", Tags: map[string]string{"a": "1"}}

		delta, err := dc.Compute(old, newVal)
		require.NoError(t, err)

		found := false
		for _, c := range delta.Changes {
			if c.Path == "tags.b" && c.Operation == OpDelete {
				found = true
			}
		}
		assert.True(t, found, "expected deleted map entry tags.b")
	})

	t.Run("detects changed map values", func(t *testing.T) {
		t.Parallel()
		old := &withMapField{ID: "1", Tags: map[string]string{"a": "1"}}
		newVal := &withMapField{ID: "1", Tags: map[string]string{"a": "2"}}

		delta, err := dc.Compute(old, newVal)
		require.NoError(t, err)

		found := false
		for _, c := range delta.Changes {
			if c.Path == "tags.a" && c.Operation == OpSet {
				found = true
			}
		}
		assert.True(t, found, "expected changed map entry tags.a")
	})
}
