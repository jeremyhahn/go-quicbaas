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

// Package main provides CLI-specific typed errors for the QuicBaaS CLI.
package main

import (
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// UnsupportedAlgorithmError indicates an unsupported cryptographic algorithm.
type UnsupportedAlgorithmError struct {
	Algorithm string
}

// Error implements the error interface.
func (e *UnsupportedAlgorithmError) Error() string {
	return "unsupported algorithm: " + e.Algorithm
}

// NewUnsupportedAlgorithmError creates a DragonError for an unsupported algorithm.
func NewUnsupportedAlgorithmError(op, algorithm string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &UnsupportedAlgorithmError{Algorithm: algorithm},
	}
}

// InvalidDayFormatError indicates an invalid day format in a duration string.
type InvalidDayFormatError struct {
	Value string
}

// Error implements the error interface.
func (e *InvalidDayFormatError) Error() string {
	return "invalid day format: " + e.Value
}

// NewInvalidDayFormatError creates a DragonError for an invalid day format.
func NewInvalidDayFormatError(op, value string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &InvalidDayFormatError{Value: value},
	}
}

// NegativeDurationError indicates a duration value that cannot be negative.
type NegativeDurationError struct {
	Value string
}

// Error implements the error interface.
func (e *NegativeDurationError) Error() string {
	return "grace period cannot be negative: " + e.Value
}

// NewNegativeDurationError creates a DragonError for a negative duration value.
func NewNegativeDurationError(op, value string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &NegativeDurationError{Value: value},
	}
}

// InvalidDurationFormatError indicates an invalid duration format string.
type InvalidDurationFormatError struct {
	Value string
}

// Error implements the error interface.
func (e *InvalidDurationFormatError) Error() string {
	return "invalid duration format: " + e.Value
}

// NewInvalidDurationFormatError creates a DragonError for an invalid duration format.
func NewInvalidDurationFormatError(op, value string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &InvalidDurationFormatError{Value: value},
	}
}

// UnsupportedOutputFormatError indicates an unsupported output format.
type UnsupportedOutputFormatError struct {
	Format string
}

// Error implements the error interface.
func (e *UnsupportedOutputFormatError) Error() string {
	return "unsupported output format: " + e.Format
}

// NewUnsupportedOutputFormatError creates a DragonError for an unsupported output format.
func NewUnsupportedOutputFormatError(op, format string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &UnsupportedOutputFormatError{Format: format},
	}
}

// NoActiveMigrationError indicates no active migration was found.
type NoActiveMigrationError struct{}

// Error implements the error interface.
func (e *NoActiveMigrationError) Error() string {
	return "no active migration found"
}

// NewNoActiveMigrationError creates a DragonError for a missing active migration.
func NewNoActiveMigrationError(op string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrNotFound,
		Op:   op,
		Err:  &NoActiveMigrationError{},
	}
}

// InvalidStorageURLError indicates an invalid storage URL format.
type InvalidStorageURLError struct {
	URL string
}

// Error implements the error interface.
func (e *InvalidStorageURLError) Error() string {
	return fmt.Sprintf("invalid storage URL format: %s (expected scheme://path)", e.URL)
}

// NewInvalidStorageURLError creates a DragonError for an invalid storage URL.
func NewInvalidStorageURLError(op, url string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &InvalidStorageURLError{URL: url},
	}
}

// UnsupportedStorageSchemeError indicates an unsupported storage scheme.
type UnsupportedStorageSchemeError struct {
	Scheme string
}

// Error implements the error interface.
func (e *UnsupportedStorageSchemeError) Error() string {
	return fmt.Sprintf("unsupported storage scheme: %s (supported: pebble, memory, file)", e.Scheme)
}

// NewUnsupportedStorageSchemeError creates a DragonError for an unsupported storage scheme.
func NewUnsupportedStorageSchemeError(op, scheme string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &UnsupportedStorageSchemeError{Scheme: scheme},
	}
}

// MigrationPartialFailureError indicates a migration completed with failed keys.
type MigrationPartialFailureError struct {
	FailedKeys int64
}

// Error implements the error interface.
func (e *MigrationPartialFailureError) Error() string {
	return fmt.Sprintf("migration completed with %d failed keys", e.FailedKeys)
}

// NewMigrationPartialFailureError creates a DragonError for a partial migration failure.
func NewMigrationPartialFailureError(op string, failedKeys int64) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInternal,
		Op:   op,
		Err:  &MigrationPartialFailureError{FailedKeys: failedKeys},
	}
}

// KeyNotFoundError indicates a key was not found in storage.
type KeyNotFoundError struct{}

// Error implements the error interface.
func (e *KeyNotFoundError) Error() string {
	return "key not found"
}

// NewKeyNotFoundError creates a DragonError for a missing key.
func NewKeyNotFoundError(op string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrNotFound,
		Op:   op,
		Err:  &KeyNotFoundError{},
	}
}

// InvalidFilterFormatError indicates an invalid metadata filter format.
type InvalidFilterFormatError struct {
	Filter string
}

// Error implements the error interface.
func (e *InvalidFilterFormatError) Error() string {
	return fmt.Sprintf("invalid filter format: %s (expected key=value)", e.Filter)
}

// NewInvalidFilterFormatError creates a DragonError for an invalid filter format.
func NewInvalidFilterFormatError(op, filter string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &InvalidFilterFormatError{Filter: filter},
	}
}

// NoMetadataProvidedError indicates no metadata was provided for an update operation.
type NoMetadataProvidedError struct{}

// Error implements the error interface.
func (e *NoMetadataProvidedError) Error() string {
	return "no metadata provided; use --metadata or --metadata-json"
}

// NewNoMetadataProvidedError creates a DragonError for missing metadata.
func NewNoMetadataProvidedError(op string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &NoMetadataProvidedError{},
	}
}

// FilterRequiredError indicates at least one metadata filter is required.
type FilterRequiredError struct{}

// Error implements the error interface.
func (e *FilterRequiredError) Error() string {
	return "at least one --filter is required"
}

// NewFilterRequiredError creates a DragonError for a missing required filter.
func NewFilterRequiredError(op string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &FilterRequiredError{},
	}
}

// NodeAlreadyExistsError indicates a node with the given ID already exists.
type NodeAlreadyExistsError struct {
	NodeID string
}

// Error implements the error interface.
func (e *NodeAlreadyExistsError) Error() string {
	return fmt.Sprintf("node '%s' already exists", e.NodeID)
}

// NewNodeAlreadyExistsError creates a DragonError for a duplicate node.
func NewNodeAlreadyExistsError(op, nodeID string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrAlreadyExists,
		Op:   op,
		Err:  &NodeAlreadyExistsError{NodeID: nodeID},
	}
}

// InvalidMetadataJSONError indicates invalid JSON in metadata input.
type InvalidMetadataJSONError struct {
	Cause error
}

// Error implements the error interface.
func (e *InvalidMetadataJSONError) Error() string {
	return fmt.Sprintf("invalid metadata JSON: %v", e.Cause)
}

// Unwrap returns the underlying cause for errors.Is and errors.As support.
func (e *InvalidMetadataJSONError) Unwrap() error {
	return e.Cause
}

// NewInvalidMetadataJSONError creates a DragonError for invalid metadata JSON.
func NewInvalidMetadataJSONError(op string, cause error) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInvalidInput,
		Op:   op,
		Err:  &InvalidMetadataJSONError{Cause: cause},
	}
}

// JobFailedError indicates that an orchestration job failed.
type JobFailedError struct {
	JobID   string
	Message string
}

// Error implements the error interface.
func (e *JobFailedError) Error() string {
	if e.Message != "" {
		return "job " + e.JobID + " failed: " + e.Message
	}
	return "job " + e.JobID + " failed"
}

// NewJobFailedError creates a DragonError for a failed orchestration job.
func NewJobFailedError(op, jobID, message string) *errors.DragonError {
	return &errors.DragonError{
		Code: errors.ErrInternal,
		Op:   op,
		Err:  &JobFailedError{JobID: jobID, Message: message},
	}
}
