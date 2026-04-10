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
	stderrors "errors"
	"reflect"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Delta-specific error codes extending the base error code system.
const (
	// ErrCodeDeltaNilInput indicates a nil input was provided to delta computation.
	ErrCodeDeltaNilInput errors.ErrorCode = 1100 + iota

	// ErrCodeDeltaTypeMismatch indicates the old and new values have different types.
	ErrCodeDeltaTypeMismatch

	// ErrCodeDeltaMaxDepthExceeded indicates the maximum recursion depth was exceeded.
	ErrCodeDeltaMaxDepthExceeded

	// ErrCodeDeltaUnsupportedType indicates an unsupported type was encountered.
	ErrCodeDeltaUnsupportedType

	// ErrCodeDeltaDeserializationFailed indicates deserialization of input data failed.
	ErrCodeDeltaDeserializationFailed

	// ErrCodeDeltaDiffFailed indicates the Diffable interface method failed.
	ErrCodeDeltaDiffFailed
)

// DeltaNilInputError represents an error when nil input is provided.
type DeltaNilInputError struct {
	Parameter string
}

// Error implements the error interface.
func (e *DeltaNilInputError) Error() string {
	return "delta computation requires non-nil " + e.Parameter
}

// DeltaTypeMismatchError represents an error when types do not match.
type DeltaTypeMismatchError struct {
	OldType string
	NewType string
	Path    string
}

// Error implements the error interface.
func (e *DeltaTypeMismatchError) Error() string {
	if e.Path != "" {
		return "type mismatch at " + e.Path + ": " + e.OldType + " vs " + e.NewType
	}
	return "type mismatch: " + e.OldType + " vs " + e.NewType
}

// DeltaMaxDepthExceededError represents an error when recursion depth is exceeded.
type DeltaMaxDepthExceededError struct {
	MaxDepth int
	Path     string
}

// Error implements the error interface.
func (e *DeltaMaxDepthExceededError) Error() string {
	return "max recursion depth exceeded at " + e.Path
}

// DeltaUnsupportedTypeError represents an error for unsupported types.
type DeltaUnsupportedTypeError struct {
	Type string
	Path string
}

// Error implements the error interface.
func (e *DeltaUnsupportedTypeError) Error() string {
	return "unsupported type " + e.Type + " at " + e.Path
}

// DeltaDeserializationError represents a deserialization failure.
type DeltaDeserializationError struct {
	Cause string
}

// Error implements the error interface.
func (e *DeltaDeserializationError) Error() string {
	return "failed to deserialize delta input: " + e.Cause
}

// DeltaDiffError represents a failure in the Diffable interface.
type DeltaDiffError struct {
	Cause string
}

// Error implements the error interface.
func (e *DeltaDiffError) Error() string {
	return "diffable interface failed: " + e.Cause
}

// NewDeltaNilInputError creates a new DragonError for nil input.
func NewDeltaNilInputError(op, parameter string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeDeltaNilInput,
		Op:   op,
		Err: &DeltaNilInputError{
			Parameter: parameter,
		},
		Context: map[string]interface{}{
			"parameter": parameter,
		},
	}
}

// NewDeltaTypeMismatchError creates a new DragonError for type mismatch.
func NewDeltaTypeMismatchError(op, oldType, newType, path string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeDeltaTypeMismatch,
		Op:   op,
		Err: &DeltaTypeMismatchError{
			OldType: oldType,
			NewType: newType,
			Path:    path,
		},
		Context: map[string]interface{}{
			"old_type": oldType,
			"new_type": newType,
			"path":     path,
		},
	}
}

// NewDeltaMaxDepthExceededError creates a new DragonError for max depth exceeded.
func NewDeltaMaxDepthExceededError(op string, maxDepth int, path string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeDeltaMaxDepthExceeded,
		Op:   op,
		Err: &DeltaMaxDepthExceededError{
			MaxDepth: maxDepth,
			Path:     path,
		},
		Context: map[string]interface{}{
			"max_depth": maxDepth,
			"path":      path,
		},
	}
}

// NewDeltaUnsupportedTypeError creates a new DragonError for unsupported type.
func NewDeltaUnsupportedTypeError(op, typeName, path string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeDeltaUnsupportedType,
		Op:   op,
		Err: &DeltaUnsupportedTypeError{
			Type: typeName,
			Path: path,
		},
		Context: map[string]interface{}{
			"type": typeName,
			"path": path,
		},
	}
}

// NewDeltaDeserializationError creates a new DragonError for deserialization failure.
func NewDeltaDeserializationError(op, cause string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeDeltaDeserializationFailed,
		Op:   op,
		Err: &DeltaDeserializationError{
			Cause: cause,
		},
		Context: map[string]interface{}{
			"cause": cause,
		},
	}
}

// NewDeltaDiffError creates a new DragonError for Diffable interface failure.
func NewDeltaDiffError(op, cause string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodeDeltaDiffFailed,
		Op:   op,
		Err: &DeltaDiffError{
			Cause: cause,
		},
		Context: map[string]interface{}{
			"cause": cause,
		},
	}
}

// Default configuration values for DeltaComputer.
const (
	defaultDeltaMaxDepth = 10
)

// Diffable is an optional interface entities can implement for optimized delta detection.
type Diffable interface {
	// Diff computes the differences between this entity and the previous state.
	// Returns a list of field changes representing the delta.
	Diff(previous interface{}) ([]FieldChange, error)

	// Apply applies the given field changes to this entity.
	Apply(changes []FieldChange) error

	// Version returns the version number of this entity.
	Version() uint64
}

// DeltaComputer computes deltas between entity states.
// It uses reflection to compare struct fields recursively and supports
// the Diffable interface for optimized comparison when available.
type DeltaComputer struct {
	codec      CodecInterface
	maxDepth   int
	ignoreZero bool
	logger     *logger.Logger
}

// DeltaComputerOption configures DeltaComputer.
type DeltaComputerOption func(*DeltaComputer)

// WithDeltaMaxDepth sets the maximum recursion depth for nested struct comparison.
func WithDeltaMaxDepth(depth int) DeltaComputerOption {
	return func(dc *DeltaComputer) {
		if depth > 0 {
			dc.maxDepth = depth
		}
	}
}

// WithDeltaIgnoreZeroChanges configures whether to ignore zero-value to zero-value changes.
func WithDeltaIgnoreZeroChanges(ignore bool) DeltaComputerOption {
	return func(dc *DeltaComputer) {
		dc.ignoreZero = ignore
	}
}

// WithDeltaCodec sets the codec for deserializing byte data.
func WithDeltaCodec(codec CodecInterface) DeltaComputerOption {
	return func(dc *DeltaComputer) {
		if codec != nil {
			dc.codec = codec
		}
	}
}

// NewDeltaComputer creates a new DeltaComputer with the given options.
func NewDeltaComputer(log *logger.Logger, opts ...DeltaComputerOption) *DeltaComputer {
	dc := &DeltaComputer{
		codec:      &jsonPushCodec{},
		maxDepth:   defaultDeltaMaxDepth,
		ignoreZero: true,
		logger:     log,
	}

	for _, opt := range opts {
		opt(dc)
	}

	return dc
}

// Compute computes the delta between two entity states.
// Both old and new must be of the same type (or old can be nil for new entity creation).
// Returns a DeltaPayload containing the list of changes.
func (dc *DeltaComputer) Compute(old, new interface{}) (*DeltaPayload, error) {
	const op = "DeltaComputer.Compute"

	if new == nil {
		return nil, NewDeltaNilInputError(op, "new entity")
	}

	// Check if new implements Diffable interface for optimized comparison
	if diffable, ok := new.(Diffable); ok {
		return dc.computeWithDiffable(old, diffable)
	}

	newVal := reflect.ValueOf(new)
	if newVal.Kind() == reflect.Ptr {
		if newVal.IsNil() {
			return nil, NewDeltaNilInputError(op, "new entity (nil pointer)")
		}
		newVal = newVal.Elem()
	}

	var oldVal reflect.Value
	if old != nil {
		oldVal = reflect.ValueOf(old)
		if oldVal.Kind() == reflect.Ptr {
			if oldVal.IsNil() {
				old = nil
			} else {
				oldVal = oldVal.Elem()
			}
		}
	}

	// Handle nil old value (new entity creation)
	if old == nil {
		return dc.computeNewEntity(newVal)
	}

	// Verify types match
	if oldVal.Type() != newVal.Type() {
		return nil, NewDeltaTypeMismatchError(op, oldVal.Type().String(), newVal.Type().String(), "")
	}

	// Compute changes recursively
	changes, err := dc.computeChanges(oldVal, newVal, "", 0)
	if err != nil {
		return nil, err
	}

	// Extract version information if available
	var version, prevVersion uint64
	if v, ok := new.(interface{ Version() uint64 }); ok {
		version = v.Version()
	}
	if v, ok := old.(interface{ Version() uint64 }); ok {
		prevVersion = v.Version()
	}

	// Extract entity ID and type if available
	entityID := dc.extractEntityID(new)
	entityType := dc.extractEntityType(newVal.Type())

	return &DeltaPayload{
		EntityID:    entityID,
		EntityType:  entityType,
		Version:     version,
		PrevVersion: prevVersion,
		Timestamp:   time.Now(),
		Changes:     changes,
	}, nil
}

// ComputeBytes computes the delta from serialized data.
// The entityType parameter should be a pointer to the struct type to unmarshal into.
func (dc *DeltaComputer) ComputeBytes(oldData, newData []byte, entityType interface{}) (*DeltaPayload, error) {
	const op = "DeltaComputer.ComputeBytes"

	if len(newData) == 0 {
		return nil, NewDeltaNilInputError(op, "new data")
	}

	if entityType == nil {
		return nil, NewDeltaNilInputError(op, "entity type")
	}

	// Get the type to create new instances
	entityVal := reflect.ValueOf(entityType)
	if entityVal.Kind() != reflect.Ptr {
		return nil, NewDeltaTypeMismatchError(op, "non-pointer", "pointer", "entityType")
	}

	elemType := entityVal.Type().Elem()

	// Unmarshal new data
	newPtr := reflect.New(elemType)
	if err := dc.codec.Decode(newData, newPtr.Interface()); err != nil {
		return nil, NewDeltaDeserializationError(op, "new data: "+err.Error())
	}

	// Handle old data
	var old interface{}
	if len(oldData) > 0 {
		oldPtr := reflect.New(elemType)
		if err := dc.codec.Decode(oldData, oldPtr.Interface()); err != nil {
			return nil, NewDeltaDeserializationError(op, "old data: "+err.Error())
		}
		old = oldPtr.Interface()
	}

	return dc.Compute(old, newPtr.Interface())
}

// computeWithDiffable uses the Diffable interface for optimized comparison.
func (dc *DeltaComputer) computeWithDiffable(old interface{}, diffable Diffable) (*DeltaPayload, error) {
	const op = "DeltaComputer.computeWithDiffable"

	changes, err := diffable.Diff(old)
	if err != nil {
		return nil, NewDeltaDiffError(op, err.Error())
	}

	version := diffable.Version()
	var prevVersion uint64
	if oldDiffable, ok := old.(Diffable); ok {
		prevVersion = oldDiffable.Version()
	}

	newVal := reflect.ValueOf(diffable)
	if newVal.Kind() == reflect.Ptr {
		newVal = newVal.Elem()
	}

	return &DeltaPayload{
		EntityID:    dc.extractEntityID(diffable),
		EntityType:  dc.extractEntityType(newVal.Type()),
		Version:     version,
		PrevVersion: prevVersion,
		Timestamp:   time.Now(),
		Changes:     changes,
	}, nil
}

// computeNewEntity creates a delta payload for a new entity (all fields are set operations).
func (dc *DeltaComputer) computeNewEntity(newVal reflect.Value) (*DeltaPayload, error) {
	changes, err := dc.extractAllFields(newVal, "", 0)
	if err != nil {
		return nil, err
	}

	var version uint64
	if newVal.CanAddr() {
		if v, ok := newVal.Addr().Interface().(interface{ Version() uint64 }); ok {
			version = v.Version()
		}
	}

	return &DeltaPayload{
		EntityID:    dc.extractEntityIDFromValue(newVal),
		EntityType:  dc.extractEntityType(newVal.Type()),
		Version:     version,
		PrevVersion: 0,
		Timestamp:   time.Now(),
		Changes:     changes,
	}, nil
}

// extractAllFields extracts all non-zero fields as set operations.
func (dc *DeltaComputer) extractAllFields(val reflect.Value, path string, depth int) ([]FieldChange, error) {
	const op = "DeltaComputer.extractAllFields"

	if depth > dc.maxDepth {
		return nil, NewDeltaMaxDepthExceededError(op, dc.maxDepth, path)
	}

	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, nil
		}
		val = val.Elem()
	}

	var changes []FieldChange

	switch val.Kind() {
	case reflect.Struct:
		for i := 0; i < val.NumField(); i++ {
			field := val.Type().Field(i)
			fieldVal := val.Field(i)

			// Skip unexported fields
			if !field.IsExported() {
				continue
			}

			fieldPath := dc.getFieldPath(field)
			if fieldPath == "-" {
				continue
			}

			fullPath := dc.buildPath(path, fieldPath)

			// Handle nested structs
			if fieldVal.Kind() == reflect.Struct && !dc.isSpecialType(fieldVal.Type()) {
				nestedChanges, err := dc.extractAllFields(fieldVal, fullPath, depth+1)
				if err != nil {
					return nil, err
				}
				changes = append(changes, nestedChanges...)
				continue
			}

			// Handle pointer to struct
			if fieldVal.Kind() == reflect.Ptr && !fieldVal.IsNil() {
				elem := fieldVal.Elem()
				if elem.Kind() == reflect.Struct && !dc.isSpecialType(elem.Type()) {
					nestedChanges, err := dc.extractAllFields(elem, fullPath, depth+1)
					if err != nil {
						return nil, err
					}
					changes = append(changes, nestedChanges...)
					continue
				}
			}

			// Skip zero values if configured
			if dc.ignoreZero && dc.isZeroValue(fieldVal) {
				continue
			}

			changes = append(changes, FieldChange{
				Path:      fullPath,
				Operation: OpSet,
				Value:     dc.extractValue(fieldVal),
			})
		}

	case reflect.Map:
		iter := val.MapRange()
		for iter.Next() {
			key := iter.Key()
			mapVal := iter.Value()
			keyStr := dc.valueToString(key)
			fullPath := dc.buildPath(path, keyStr)

			if mapVal.Kind() == reflect.Struct && !dc.isSpecialType(mapVal.Type()) {
				nestedChanges, err := dc.extractAllFields(mapVal, fullPath, depth+1)
				if err != nil {
					return nil, err
				}
				changes = append(changes, nestedChanges...)
			} else {
				changes = append(changes, FieldChange{
					Path:      fullPath,
					Operation: OpSet,
					Value:     dc.extractValue(mapVal),
				})
			}
		}

	case reflect.Slice, reflect.Array:
		if !dc.isZeroValue(val) {
			changes = append(changes, FieldChange{
				Path:      path,
				Operation: OpSet,
				Value:     dc.extractValue(val),
			})
		}

	default:
		if !dc.isZeroValue(val) {
			changes = append(changes, FieldChange{
				Path:      path,
				Operation: OpSet,
				Value:     dc.extractValue(val),
			})
		}
	}

	return changes, nil
}

// computeChanges recursively compares two values and returns field changes.
func (dc *DeltaComputer) computeChanges(oldVal, newVal reflect.Value, path string, depth int) ([]FieldChange, error) {
	const op = "DeltaComputer.computeChanges"

	if depth > dc.maxDepth {
		return nil, NewDeltaMaxDepthExceededError(op, dc.maxDepth, path)
	}

	// Handle pointers
	if oldVal.Kind() == reflect.Ptr && newVal.Kind() == reflect.Ptr {
		if oldVal.IsNil() && newVal.IsNil() {
			return nil, nil
		}
		if oldVal.IsNil() {
			return dc.extractAllFields(newVal.Elem(), path, depth)
		}
		if newVal.IsNil() {
			return []FieldChange{{
				Path:      path,
				Operation: OpDelete,
				OldValue:  dc.extractValue(oldVal.Elem()),
			}}, nil
		}
		return dc.computeChanges(oldVal.Elem(), newVal.Elem(), path, depth)
	}

	// Handle interface values
	if oldVal.Kind() == reflect.Interface && newVal.Kind() == reflect.Interface {
		if oldVal.IsNil() && newVal.IsNil() {
			return nil, nil
		}
		if oldVal.IsNil() {
			return []FieldChange{{
				Path:      path,
				Operation: OpSet,
				Value:     dc.extractValue(newVal),
			}}, nil
		}
		if newVal.IsNil() {
			return []FieldChange{{
				Path:      path,
				Operation: OpDelete,
				OldValue:  dc.extractValue(oldVal),
			}}, nil
		}
		return dc.computeChanges(oldVal.Elem(), newVal.Elem(), path, depth)
	}

	var changes []FieldChange

	switch oldVal.Kind() {
	case reflect.Struct:
		structChanges, err := dc.compareStructs(oldVal, newVal, path, depth)
		if err != nil {
			return nil, err
		}
		return structChanges, nil

	case reflect.Map:
		return dc.compareMaps(oldVal, newVal, path, depth)

	case reflect.Slice, reflect.Array:
		return dc.compareSlices(oldVal, newVal, path, depth)

	default:
		// Compare primitive values
		if !reflect.DeepEqual(oldVal.Interface(), newVal.Interface()) {
			oldZero := dc.isZeroValue(oldVal)
			newZero := dc.isZeroValue(newVal)

			// Skip zero-to-zero changes if configured
			if dc.ignoreZero && oldZero && newZero {
				return nil, nil
			}

			change := FieldChange{
				Path:      path,
				Operation: OpSet,
				Value:     dc.extractValue(newVal),
				OldValue:  dc.extractValue(oldVal),
			}

			// If new value is zero, treat as delete
			if newZero && !oldZero {
				change.Operation = OpDelete
				change.Value = nil
			}

			changes = append(changes, change)
		}
	}

	return changes, nil
}

// compareStructs compares two struct values field by field.
func (dc *DeltaComputer) compareStructs(oldVal, newVal reflect.Value, path string, depth int) ([]FieldChange, error) {
	var changes []FieldChange

	for i := 0; i < oldVal.NumField(); i++ {
		field := oldVal.Type().Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		fieldPath := dc.getFieldPath(field)
		if fieldPath == "-" {
			continue
		}

		fullPath := dc.buildPath(path, fieldPath)

		oldField := oldVal.Field(i)
		newField := newVal.Field(i)

		// Handle special types (time.Time, etc.) as primitives
		if dc.isSpecialType(field.Type) {
			if !reflect.DeepEqual(oldField.Interface(), newField.Interface()) {
				oldZero := dc.isZeroValue(oldField)
				newZero := dc.isZeroValue(newField)

				if dc.ignoreZero && oldZero && newZero {
					continue
				}

				change := FieldChange{
					Path:      fullPath,
					Operation: OpSet,
					Value:     dc.extractValue(newField),
					OldValue:  dc.extractValue(oldField),
				}

				if newZero && !oldZero {
					change.Operation = OpDelete
					change.Value = nil
				}

				changes = append(changes, change)
			}
			continue
		}

		// Handle nested structs and other types recursively
		fieldChanges, err := dc.computeChanges(oldField, newField, fullPath, depth+1)
		if err != nil {
			return nil, err
		}
		changes = append(changes, fieldChanges...)
	}

	return changes, nil
}

// compareMaps compares two map values.
func (dc *DeltaComputer) compareMaps(oldVal, newVal reflect.Value, path string, depth int) ([]FieldChange, error) {
	var changes []FieldChange

	// Track keys in old map
	oldKeys := make(map[interface{}]bool)
	if !oldVal.IsNil() {
		iter := oldVal.MapRange()
		for iter.Next() {
			key := iter.Key().Interface()
			oldKeys[key] = true
		}
	}

	// Process new map keys
	if !newVal.IsNil() {
		iter := newVal.MapRange()
		for iter.Next() {
			key := iter.Key()
			keyStr := dc.valueToString(key)
			fullPath := dc.buildPath(path, keyStr)

			newMapVal := iter.Value()
			oldMapVal := reflect.Value{}

			if !oldVal.IsNil() {
				oldMapVal = oldVal.MapIndex(key)
			}

			if !oldMapVal.IsValid() {
				// New key added
				if newMapVal.Kind() == reflect.Struct && !dc.isSpecialType(newMapVal.Type()) {
					nestedChanges, err := dc.extractAllFields(newMapVal, fullPath, depth+1)
					if err != nil {
						return nil, err
					}
					changes = append(changes, nestedChanges...)
				} else {
					changes = append(changes, FieldChange{
						Path:      fullPath,
						Operation: OpSet,
						Value:     dc.extractValue(newMapVal),
					})
				}
			} else {
				// Key exists in both, compare values
				delete(oldKeys, key.Interface())

				fieldChanges, err := dc.computeChanges(oldMapVal, newMapVal, fullPath, depth+1)
				if err != nil {
					return nil, err
				}
				changes = append(changes, fieldChanges...)
			}
		}
	}

	// Process deleted keys
	if !oldVal.IsNil() {
		iter := oldVal.MapRange()
		for iter.Next() {
			key := iter.Key()
			if oldKeys[key.Interface()] {
				keyStr := dc.valueToString(key)
				fullPath := dc.buildPath(path, keyStr)
				changes = append(changes, FieldChange{
					Path:      fullPath,
					Operation: OpDelete,
					OldValue:  dc.extractValue(iter.Value()),
				})
			}
		}
	}

	return changes, nil
}

// compareSlices compares two slice values.
func (dc *DeltaComputer) compareSlices(oldVal, newVal reflect.Value, path string, depth int) ([]FieldChange, error) {
	// For slices, we do a simple comparison and report the entire slice as changed
	// if there are any differences. This is more practical than trying to compute
	// element-level diffs which would require complex matching algorithms.
	if !reflect.DeepEqual(oldVal.Interface(), newVal.Interface()) {
		oldZero := dc.isZeroValue(oldVal)
		newZero := dc.isZeroValue(newVal)

		if dc.ignoreZero && oldZero && newZero {
			return nil, nil
		}

		change := FieldChange{
			Path:      path,
			Operation: OpSet,
			Value:     dc.extractValue(newVal),
			OldValue:  dc.extractValue(oldVal),
		}

		if newZero && !oldZero {
			change.Operation = OpDelete
			change.Value = nil
		}

		return []FieldChange{change}, nil
	}

	return nil, nil
}

// getFieldPath returns the JSON field name from struct tags or falls back to field name.
func (dc *DeltaComputer) getFieldPath(field reflect.StructField) string {
	jsonTag := field.Tag.Get("json")
	if jsonTag == "" {
		return field.Name
	}

	// Handle json tags like "field_name,omitempty"
	parts := strings.Split(jsonTag, ",")
	if parts[0] == "" {
		return field.Name
	}

	return parts[0]
}

// buildPath constructs a dot-separated path.
func (dc *DeltaComputer) buildPath(base, segment string) string {
	if base == "" {
		return segment
	}
	return base + "." + segment
}

// isZeroValue checks if a reflect.Value is the zero value for its type.
func (dc *DeltaComputer) isZeroValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return v.IsNil()
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !dc.isZeroValue(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		// Special handling for time.Time.
		if v.Type() == reflect.TypeOf(time.Time{}) {
			t, ok := v.Interface().(time.Time)
			return ok && t.IsZero()
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() && !dc.isZeroValue(v.Field(i)) {
				return false
			}
		}
		return true
	default:
		return v.IsZero()
	}
}

// isSpecialType checks if a type should be treated as a primitive (not recursed into).
func (dc *DeltaComputer) isSpecialType(t reflect.Type) bool {
	// time.Time should be compared as a whole, not field by field
	if t == reflect.TypeOf(time.Time{}) {
		return true
	}
	// Pointer to time.Time
	if t.Kind() == reflect.Ptr && t.Elem() == reflect.TypeOf(time.Time{}) {
		return true
	}
	return false
}

// extractValue converts a reflect.Value to an interface{} suitable for JSON serialization.
func (dc *DeltaComputer) extractValue(v reflect.Value) interface{} {
	if !v.IsValid() {
		return nil
	}

	// Handle pointers and interfaces
	if v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		return dc.extractValue(v.Elem())
	}

	return v.Interface()
}

// valueToString converts a reflect.Value to a string for use in paths.
func (dc *DeltaComputer) valueToString(v reflect.Value) string {
	if v.Kind() == reflect.String {
		return v.String()
	}
	return strings.Trim(strings.ReplaceAll(
		strings.ReplaceAll(
			reflect.ValueOf(v.Interface()).String(),
			"<", ""),
		">", ""), " ")
}

// extractEntityID attempts to extract an ID field from the entity.
func (dc *DeltaComputer) extractEntityID(entity interface{}) string {
	val := reflect.ValueOf(entity)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return ""
		}
		val = val.Elem()
	}

	return dc.extractEntityIDFromValue(val)
}

// extractEntityIDFromValue extracts ID from a reflect.Value.
func (dc *DeltaComputer) extractEntityIDFromValue(val reflect.Value) string {
	if val.Kind() != reflect.Struct {
		return ""
	}

	// Try common ID field names
	idFieldNames := []string{"ID", "Id", "EntityID", "EntityId", "Key"}
	for _, name := range idFieldNames {
		field := val.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
	}

	return ""
}

// extractEntityType returns the type name of the entity.
func (dc *DeltaComputer) extractEntityType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Name()
}

// IsDeltaNilInput returns true if the error is a nil input error.
func IsDeltaNilInput(err error) bool {
	if err == nil {
		return false
	}
	var dni *DeltaNilInputError
	if stderrors.As(err, &dni) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeDeltaNilInput
	}
	return false
}

// IsDeltaTypeMismatch returns true if the error is a type mismatch error.
func IsDeltaTypeMismatch(err error) bool {
	if err == nil {
		return false
	}
	var dtm *DeltaTypeMismatchError
	if stderrors.As(err, &dtm) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeDeltaTypeMismatch
	}
	return false
}

// IsDeltaMaxDepthExceeded returns true if the error is a max depth exceeded error.
func IsDeltaMaxDepthExceeded(err error) bool {
	if err == nil {
		return false
	}
	var dmde *DeltaMaxDepthExceededError
	if stderrors.As(err, &dmde) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeDeltaMaxDepthExceeded
	}
	return false
}

// IsDeltaUnsupportedType returns true if the error is an unsupported type error.
func IsDeltaUnsupportedType(err error) bool {
	if err == nil {
		return false
	}
	var dut *DeltaUnsupportedTypeError
	if stderrors.As(err, &dut) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeDeltaUnsupportedType
	}
	return false
}

// IsDeltaDeserializationFailed returns true if the error is a deserialization error.
func IsDeltaDeserializationFailed(err error) bool {
	if err == nil {
		return false
	}
	var dde *DeltaDeserializationError
	if stderrors.As(err, &dde) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeDeltaDeserializationFailed
	}
	return false
}

// IsDeltaDiffFailed returns true if the error is a diff failure error.
func IsDeltaDiffFailed(err error) bool {
	if err == nil {
		return false
	}
	var ddf *DeltaDiffError
	if stderrors.As(err, &ddf) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeDeltaDiffFailed
	}
	return false
}
