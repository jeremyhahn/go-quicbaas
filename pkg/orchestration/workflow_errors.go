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

package orchestration

import "errors"

// =============================================================================
// Workflow Sentinel Errors
// =============================================================================

var (
	// ErrWorkflowValidation indicates a workflow definition validation failure.
	ErrWorkflowValidation = errors.New("workflow: validation failed")

	// ErrWorkflowNotFound indicates the requested workflow was not found.
	ErrWorkflowNotFound = errors.New("workflow: not found")

	// ErrWorkflowAlreadyExists indicates a workflow with the same name already exists.
	ErrWorkflowAlreadyExists = errors.New("workflow: already exists")

	// ErrWorkflowLoad indicates a workflow file could not be loaded.
	ErrWorkflowLoad = errors.New("workflow: load failed")

	// ErrWorkflowParse indicates a workflow file could not be parsed.
	ErrWorkflowParse = errors.New("workflow: parse failed")

	// ErrWorkflowRetiredField indicates a workflow definition carries a
	// field that no longer exists.
	ErrWorkflowRetiredField = errors.New("workflow: retired field")
)

// =============================================================================
// Workflow Typed Errors
// =============================================================================

// WorkflowValidationError indicates a workflow definition failed validation.
type WorkflowValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *WorkflowValidationError) Error() string {
	return ErrWorkflowValidation.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *WorkflowValidationError) Unwrap() error {
	return ErrWorkflowValidation
}

// WorkflowNotFoundError indicates the requested workflow was not found by name
// within the tenant.
type WorkflowNotFoundError struct {
	TenantID string
	Name     string
}

// Error implements the error interface.
func (e *WorkflowNotFoundError) Error() string {
	if e.TenantID != "" {
		return ErrWorkflowNotFound.Error() + ": " + e.Name + " (tenant " + e.TenantID + ")"
	}
	return ErrWorkflowNotFound.Error() + ": " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *WorkflowNotFoundError) Unwrap() error {
	return ErrWorkflowNotFound
}

// WorkflowAlreadyExistsError indicates a workflow with the same name already
// exists within the tenant.
type WorkflowAlreadyExistsError struct {
	TenantID string
	Name     string
}

// Error implements the error interface.
func (e *WorkflowAlreadyExistsError) Error() string {
	if e.TenantID != "" {
		return ErrWorkflowAlreadyExists.Error() + ": " + e.Name + " (tenant " + e.TenantID + ")"
	}
	return ErrWorkflowAlreadyExists.Error() + ": " + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *WorkflowAlreadyExistsError) Unwrap() error {
	return ErrWorkflowAlreadyExists
}

// WorkflowLoadError indicates a workflow file could not be loaded.
type WorkflowLoadError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *WorkflowLoadError) Error() string {
	return ErrWorkflowLoad.Error() + ": " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *WorkflowLoadError) Unwrap() []error {
	return []error{ErrWorkflowLoad, e.Cause}
}

// WorkflowParseError indicates a workflow file could not be parsed.
type WorkflowParseError struct {
	Path  string
	Cause error
}

// Error implements the error interface.
func (e *WorkflowParseError) Error() string {
	return ErrWorkflowParse.Error() + ": " + e.Path + ": " + e.Cause.Error()
}

// Unwrap returns both the sentinel and cause for errors.Is/As support.
func (e *WorkflowParseError) Unwrap() []error {
	return []error{ErrWorkflowParse, e.Cause}
}

// RetiredFieldError is a workflow definition that still carries a field the
// definition no longer has. It is refused rather than ignored, so a file
// written for the old field does not load as if its content were in force.
type RetiredFieldError struct {
	// Path is the file the definition was read from.
	Path string
	// Field locates the field, e.g. "agents[0].system_prompt".
	Field string
	// Successor says where what the field held lives now.
	Successor string
}

// Error implements the error interface.
func (e *RetiredFieldError) Error() string {
	return ErrWorkflowRetiredField.Error() + ": " + e.Path + ": " + e.Field + " is retired: " + e.Successor
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *RetiredFieldError) Unwrap() error {
	return ErrWorkflowRetiredField
}
