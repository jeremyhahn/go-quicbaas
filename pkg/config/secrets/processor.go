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

package secrets

import (
	"context"
	"fmt"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Logger defines the logging interface for the config processor.
// Implementations should provide structured logging capabilities.
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
}

// ProcessResult contains the outcome of processing a configuration struct.
type ProcessResult struct {
	// Resolved contains successfully resolved secrets with metadata.
	Resolved []ResolvedSecret

	// Unresolved contains field paths that couldn't be resolved.
	Unresolved []string

	// Errors contains errors encountered during processing.
	Errors []error
}

// ResolvedSecret represents a successfully resolved secret with metadata.
type ResolvedSecret struct {
	// FieldPath is the dot-notation path to the field (e.g., "KMS.Backends[0].TPM2.SRKAuth").
	FieldPath string

	// Original is the original value before resolution.
	Original string

	// Strategy indicates how the secret was resolved.
	Strategy ResolvedStrategy
}

// ProcessorOption is a functional option for configuring ConfigProcessor.
type ProcessorOption func(*ConfigProcessor)

// ConfigProcessor walks configuration structs and resolves secret references.
// It uses reflection to traverse nested structures and resolves string fields
// that contain secret URIs or placeholders.
type ConfigProcessor struct {
	registry *ResolverRegistry
	logger   Logger
}

// NewConfigProcessor creates a new ConfigProcessor with the given registry.
// The registry must not be nil.
func NewConfigProcessor(registry *ResolverRegistry, opts ...ProcessorOption) *ConfigProcessor {
	p := &ConfigProcessor{
		registry: registry,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithLogger returns a ProcessorOption that sets the logger.
func WithLogger(logger Logger) ProcessorOption {
	return func(p *ConfigProcessor) {
		p.logger = logger
	}
}

// ConfigNilError represents an error when nil config is provided.
type ConfigNilError struct{}

// Error implements the error interface.
func (e *ConfigNilError) Error() string {
	return "config cannot be nil"
}

// ConfigNotPointerError represents an error when config is not a pointer.
type ConfigNotPointerError struct {
	Kind reflect.Kind
}

// Error implements the error interface.
func (e *ConfigNotPointerError) Error() string {
	return fmt.Sprintf("config must be a pointer to struct, got %s", e.Kind)
}

// ConfigNotStructError represents an error when config points to non-struct.
type ConfigNotStructError struct {
	Kind reflect.Kind
}

// Error implements the error interface.
func (e *ConfigNotStructError) Error() string {
	return fmt.Sprintf("config must point to a struct, got pointer to %s", e.Kind)
}

// RegistryNilError represents an error when registry is nil.
type RegistryNilError struct{}

// Error implements the error interface.
func (e *RegistryNilError) Error() string {
	return "resolver registry cannot be nil"
}

// Process walks the configuration struct and resolves all secret references.
// The cfg parameter must be a pointer to a struct. String fields containing
// secret URIs or placeholders are resolved in place.
//
// Process traverses nested structs, pointers, slices, and maps to find all
// string fields that may contain secrets.
//
// Returns a ProcessResult containing resolved secrets, unresolved fields, and
// any errors encountered. Processing continues even when individual fields
// fail to resolve.
func (p *ConfigProcessor) Process(ctx context.Context, cfg interface{}) (*ProcessResult, error) {
	if ctx == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ConfigProcessor.Process",
			Err:  ErrNilContext,
		}
	}

	if p.registry == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ConfigProcessor.Process",
			Err:  &RegistryNilError{},
		}
	}

	if cfg == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ConfigProcessor.Process",
			Err:  &ConfigNilError{},
		}
	}

	rv := reflect.ValueOf(cfg)
	if rv.Kind() != reflect.Ptr {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ConfigProcessor.Process",
			Err:  &ConfigNotPointerError{Kind: rv.Kind()},
		}
	}

	if rv.IsNil() {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ConfigProcessor.Process",
			Err:  &ConfigNilError{},
		}
	}

	elem := rv.Elem()
	if elem.Kind() != reflect.Struct {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "ConfigProcessor.Process",
			Err:  &ConfigNotStructError{Kind: elem.Kind()},
		}
	}

	result := &ProcessResult{
		Resolved:   make([]ResolvedSecret, 0),
		Unresolved: make([]string, 0),
		Errors:     make([]error, 0),
	}

	p.processValue(ctx, elem, "", result)

	if p.logger != nil {
		p.logger.Info("config processing completed",
			"resolved_count", len(result.Resolved),
			"unresolved_count", len(result.Unresolved),
			"error_count", len(result.Errors))
	}

	return result, nil
}

// processValue recursively processes a reflect.Value to find and resolve secrets.
func (p *ConfigProcessor) processValue(ctx context.Context, v reflect.Value, path string, result *ProcessResult) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		result.Errors = append(result.Errors, &errors.QBError{
			Code: errors.ErrCanceled,
			Op:   "ConfigProcessor.processValue",
			Err:  ctx.Err(),
			Context: map[string]interface{}{
				"path": path,
			},
		})
		return
	default:
	}

	switch v.Kind() {
	case reflect.Struct:
		p.processStruct(ctx, v, path, result)
	case reflect.Ptr:
		p.processPointer(ctx, v, path, result)
	case reflect.Slice:
		p.processSlice(ctx, v, path, result)
	case reflect.Map:
		p.processMap(ctx, v, path, result)
	case reflect.String:
		p.processString(ctx, v, path, result)
	case reflect.Interface:
		p.processInterface(ctx, v, path, result)
	}
}

// processStruct iterates over struct fields and processes each one.
func (p *ConfigProcessor) processStruct(ctx context.Context, v reflect.Value, path string, result *ProcessResult) {
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Skip unexported fields
		if !fieldType.IsExported() {
			continue
		}

		// Build field path
		fieldPath := fieldType.Name
		if path != "" {
			fieldPath = path + "." + fieldType.Name
		}

		p.processValue(ctx, field, fieldPath, result)
	}
}

// processPointer dereferences pointers and processes the underlying value.
func (p *ConfigProcessor) processPointer(ctx context.Context, v reflect.Value, path string, result *ProcessResult) {
	if v.IsNil() {
		return
	}
	p.processValue(ctx, v.Elem(), path, result)
}

// processSlice iterates over slice elements and processes each one.
func (p *ConfigProcessor) processSlice(ctx context.Context, v reflect.Value, path string, result *ProcessResult) {
	for i := 0; i < v.Len(); i++ {
		elem := v.Index(i)
		elemPath := fmt.Sprintf("%s[%d]", path, i)
		p.processValue(ctx, elem, elemPath, result)
	}
}

// processMap iterates over map values and processes each one.
func (p *ConfigProcessor) processMap(ctx context.Context, v reflect.Value, path string, result *ProcessResult) {
	iter := v.MapRange()
	for iter.Next() {
		key := iter.Key()
		val := iter.Value()

		// Build map key path
		keyPath := fmt.Sprintf("%s[%v]", path, key.Interface())

		// For string values in maps, we need to handle them specially
		// since map values are not addressable
		if val.Kind() == reflect.String {
			p.processMapString(ctx, v, key, val, keyPath, result)
		} else {
			// For non-string values, recurse if the value is addressable
			// or if it's a struct/slice/map that contains strings
			p.processMapValue(ctx, v, key, val, keyPath, result)
		}
	}
}

// processMapString handles string values in maps.
func (p *ConfigProcessor) processMapString(ctx context.Context, m reflect.Value, key, val reflect.Value, path string, result *ProcessResult) {
	raw := val.String()
	if raw == "" {
		return
	}

	if !p.registry.IsSecretURI(raw) {
		return
	}

	if p.logger != nil {
		p.logger.Debug("resolving secret in map", "path", path, "scheme", raw[:min(10, len(raw))])
	}

	resolved, err := p.registry.Resolve(ctx, raw)
	if err != nil {
		result.Errors = append(result.Errors, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "ConfigProcessor.processMapString",
			Err:  err,
			Context: map[string]interface{}{
				"path": path,
			},
		})
		result.Unresolved = append(result.Unresolved, path)
		return
	}

	// Update the map with resolved value
	m.SetMapIndex(key, reflect.ValueOf(resolved.Value))

	result.Resolved = append(result.Resolved, ResolvedSecret{
		FieldPath: path,
		Original:  raw,
		Strategy:  resolved.Strategy,
	})

	if p.logger != nil {
		p.logger.Debug("resolved secret in map", "path", path, "strategy", resolved.Strategy)
	}
}

// processMapValue handles non-string values in maps by processing nested structures.
func (p *ConfigProcessor) processMapValue(ctx context.Context, m reflect.Value, key, val reflect.Value, path string, result *ProcessResult) {
	// For complex types, we need to get addressable copies
	switch val.Kind() {
	case reflect.Struct, reflect.Slice, reflect.Map, reflect.Ptr, reflect.Interface:
		// Create an addressable copy for structs
		if val.Kind() == reflect.Struct {
			// Create a new pointer to the struct value
			newVal := reflect.New(val.Type())
			newVal.Elem().Set(val)
			p.processValue(ctx, newVal.Elem(), path, result)
			// Set the modified value back to the map
			m.SetMapIndex(key, newVal.Elem())
		} else {
			p.processValue(ctx, val, path, result)
		}
	}
}

// processInterface handles interface values by extracting and processing the underlying value.
func (p *ConfigProcessor) processInterface(ctx context.Context, v reflect.Value, path string, result *ProcessResult) {
	if v.IsNil() {
		return
	}
	elem := v.Elem()
	p.processValue(ctx, elem, path, result)
}

// processString attempts to resolve a string field if it contains a secret URI.
func (p *ConfigProcessor) processString(ctx context.Context, v reflect.Value, path string, result *ProcessResult) {
	if !v.CanSet() {
		return
	}

	raw := v.String()
	if raw == "" {
		return
	}

	// Check if this is a resolvable secret URI
	if !p.registry.IsSecretURI(raw) {
		return
	}

	if p.logger != nil {
		p.logger.Debug("resolving secret", "path", path, "scheme", raw[:min(10, len(raw))])
	}

	resolved, err := p.registry.Resolve(ctx, raw)
	if err != nil {
		result.Errors = append(result.Errors, &errors.QBError{
			Code: errors.ErrConfiguration,
			Op:   "ConfigProcessor.processString",
			Err:  err,
			Context: map[string]interface{}{
				"path": path,
			},
		})
		result.Unresolved = append(result.Unresolved, path)
		return
	}

	// Update the field with the resolved value
	v.SetString(resolved.Value)

	result.Resolved = append(result.Resolved, ResolvedSecret{
		FieldPath: path,
		Original:  raw,
		Strategy:  resolved.Strategy,
	})

	if p.logger != nil {
		p.logger.Debug("resolved secret", "path", path, "strategy", resolved.Strategy)
	}
}
