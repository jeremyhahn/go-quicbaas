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
	"reflect"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// DeltaApplier applies delta payloads to entities of type E.
// It is a generic type that can work with any struct type.
type DeltaApplier[E any] struct {
	// jsonTagCache caches field name to struct field mappings.
	jsonTagCache map[string]string
}

// NewDeltaApplier creates a new DeltaApplier for the specified entity type.
func NewDeltaApplier[E any]() *DeltaApplier[E] {
	return &DeltaApplier[E]{
		jsonTagCache: make(map[string]string),
	}
}

// Apply applies the delta payload to the entity and returns the updated entity.
// The original entity is not modified; a copy is made and updated.
func (a *DeltaApplier[E]) Apply(entity E, delta *DeltaPayload) (E, error) {
	var zero E
	if delta == nil {
		return entity, nil
	}

	if len(delta.Changes) == 0 {
		return entity, nil
	}

	// Convert entity to map for manipulation
	entityMap, err := a.toMap(entity)
	if err != nil {
		return zero, errors.Wrap(err, errors.CodeInternal,
			"messaging.DeltaApplier.Apply", "failed to convert entity to map")
	}

	// Apply each change
	for _, change := range delta.Changes {
		if err := a.applyChange(entityMap, &change); err != nil {
			return zero, err
		}
	}

	// Convert back to entity
	result, err := a.fromMap(entityMap)
	if err != nil {
		return zero, errors.Wrap(err, errors.CodeInternal,
			"messaging.DeltaApplier.Apply", "failed to convert map to entity")
	}

	return result, nil
}

// ApplyChanges applies a list of field changes to the entity.
func (a *DeltaApplier[E]) ApplyChanges(entity E, changes []FieldChange) (E, error) {
	return a.Apply(entity, &DeltaPayload{Changes: changes})
}

// toMap converts an entity to a map representation.
func (a *DeltaApplier[E]) toMap(entity E) (map[string]interface{}, error) {
	data, err := json.Marshal(entity)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return result, nil
}

// fromMap converts a map back to an entity.
func (a *DeltaApplier[E]) fromMap(m map[string]interface{}) (E, error) {
	var entity E

	data, err := json.Marshal(m)
	if err != nil {
		return entity, err
	}

	if err := json.Unmarshal(data, &entity); err != nil {
		return entity, err
	}

	return entity, nil
}

// applyChange applies a single field change to the entity map.
func (a *DeltaApplier[E]) applyChange(entityMap map[string]interface{}, change *FieldChange) error {
	if change.Path == "" {
		return errors.NewInvalidInput("messaging.DeltaApplier.applyChange", "empty path in field change")
	}

	parts := strings.Split(change.Path, ".")
	if len(parts) == 0 {
		return errors.NewInvalidInput("messaging.DeltaApplier.applyChange", "invalid path format")
	}

	// Navigate to the parent of the target field
	current := entityMap
	for i := 0; i < len(parts)-1; i++ {
		part := parts[i]
		val, exists := current[part]
		if !exists {
			// Create intermediate map if needed for set operations
			if change.Operation == OpSet {
				current[part] = make(map[string]interface{})
				current = current[part].(map[string]interface{})
				continue
			}
			return nil // Path doesn't exist, nothing to do for other operations
		}

		nested, ok := val.(map[string]interface{})
		if !ok {
			return errors.NewInvalidInput("messaging.DeltaApplier.applyChange", "path element is not a map").
				WithContext("path_element", part)
		}
		current = nested
	}

	// Apply the operation to the final field
	field := parts[len(parts)-1]
	switch change.Operation {
	case OpSet:
		current[field] = change.Value
	case OpDelete:
		delete(current, field)
	case OpAppend:
		return a.applyAppend(current, field, change.Value)
	case OpIncrement:
		return a.applyIncrement(current, field, change.Value)
	default:
		return errors.NewInvalidInput("messaging.DeltaApplier.applyChange", "unknown operation").
			WithContext("operation", string(change.Operation))
	}

	return nil
}

// applyAppend appends a value to an array or string field.
func (a *DeltaApplier[E]) applyAppend(m map[string]interface{}, field string, value interface{}) error {
	existing, exists := m[field]
	if !exists || existing == nil {
		// Initialize as array with the value
		m[field] = []interface{}{value}
		return nil
	}

	switch v := existing.(type) {
	case []interface{}:
		m[field] = append(v, value)
	case string:
		if strVal, ok := value.(string); ok {
			m[field] = v + strVal
		} else {
			return errors.NewInvalidInput("messaging.DeltaApplier.applyAppend",
				"cannot append non-string to string field")
		}
	default:
		typeName := "nil"
		if t := reflect.TypeOf(existing); t != nil {
			typeName = t.String()
		}
		return errors.NewInvalidInput("messaging.DeltaApplier.applyAppend",
			"field is not an array or string").
			WithContext("field", field).
			WithContext("type", typeName)
	}

	return nil
}

// applyIncrement increments a numeric field.
func (a *DeltaApplier[E]) applyIncrement(m map[string]interface{}, field string, value interface{}) error {
	increment, err := a.toFloat64(value)
	if err != nil {
		return errors.Wrap(err, errors.CodeInvalidInput,
			"messaging.DeltaApplier.applyIncrement", "increment value is not numeric")
	}

	existing, exists := m[field]
	if !exists {
		m[field] = increment
		return nil
	}

	current, err := a.toFloat64(existing)
	if err != nil {
		return errors.Wrap(err, errors.CodeInvalidInput,
			"messaging.DeltaApplier.applyIncrement", "existing field is not numeric").
			WithContext("field", field)
	}

	m[field] = current + increment
	return nil
}

// toFloat64 converts a value to float64.
func (a *DeltaApplier[E]) toFloat64(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int:
		return float64(val), nil
	case int8:
		return float64(val), nil
	case int16:
		return float64(val), nil
	case int32:
		return float64(val), nil
	case int64:
		return float64(val), nil
	case uint:
		return float64(val), nil
	case uint8:
		return float64(val), nil
	case uint16:
		return float64(val), nil
	case uint32:
		return float64(val), nil
	case uint64:
		return float64(val), nil
	case json.Number:
		return val.Float64()
	default:
		typeName := "nil"
		if t := reflect.TypeOf(v); t != nil {
			typeName = t.String()
		}
		return 0, errors.NewInvalidInput("messaging.DeltaApplier.toFloat64",
			"value is not numeric").
			WithContext("type", typeName)
	}
}

// DeltaBuilder helps construct DeltaPayload instances.
type DeltaBuilder struct {
	payload *DeltaPayload
}

// NewDeltaBuilder creates a new DeltaBuilder.
func NewDeltaBuilder() *DeltaBuilder {
	return &DeltaBuilder{
		payload: &DeltaPayload{
			Changes: make([]FieldChange, 0),
		},
	}
}

// WithEntityID sets the entity ID.
func (b *DeltaBuilder) WithEntityID(id string) *DeltaBuilder {
	b.payload.EntityID = id
	return b
}

// WithEntityType sets the entity type.
func (b *DeltaBuilder) WithEntityType(entityType string) *DeltaBuilder {
	b.payload.EntityType = entityType
	return b
}

// WithVersion sets the version and previous version.
func (b *DeltaBuilder) WithVersion(prev, current uint64) *DeltaBuilder {
	b.payload.PrevVersion = prev
	b.payload.Version = current
	return b
}

// Set adds a set operation for the given path.
func (b *DeltaBuilder) Set(path string, value interface{}) *DeltaBuilder {
	b.payload.Changes = append(b.payload.Changes, FieldChange{
		Path:      path,
		Operation: OpSet,
		Value:     value,
	})
	return b
}

// SetWithOld adds a set operation with old value for the given path.
func (b *DeltaBuilder) SetWithOld(path string, oldValue, newValue interface{}) *DeltaBuilder {
	b.payload.Changes = append(b.payload.Changes, FieldChange{
		Path:      path,
		Operation: OpSet,
		Value:     newValue,
		OldValue:  oldValue,
	})
	return b
}

// Delete adds a delete operation for the given path.
func (b *DeltaBuilder) Delete(path string) *DeltaBuilder {
	b.payload.Changes = append(b.payload.Changes, FieldChange{
		Path:      path,
		Operation: OpDelete,
	})
	return b
}

// DeleteWithOld adds a delete operation with old value for the given path.
func (b *DeltaBuilder) DeleteWithOld(path string, oldValue interface{}) *DeltaBuilder {
	b.payload.Changes = append(b.payload.Changes, FieldChange{
		Path:      path,
		Operation: OpDelete,
		OldValue:  oldValue,
	})
	return b
}

// Append adds an append operation for the given path.
func (b *DeltaBuilder) Append(path string, value interface{}) *DeltaBuilder {
	b.payload.Changes = append(b.payload.Changes, FieldChange{
		Path:      path,
		Operation: OpAppend,
		Value:     value,
	})
	return b
}

// Increment adds an increment operation for the given path.
func (b *DeltaBuilder) Increment(path string, value interface{}) *DeltaBuilder {
	b.payload.Changes = append(b.payload.Changes, FieldChange{
		Path:      path,
		Operation: OpIncrement,
		Value:     value,
	})
	return b
}

// Build returns the constructed DeltaPayload.
func (b *DeltaBuilder) Build() *DeltaPayload {
	return b.payload
}
