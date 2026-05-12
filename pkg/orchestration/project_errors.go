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
// Project Store Sentinel Errors
// =============================================================================

var (
	// ErrProjectAlreadyExists indicates a project with the same tenant+name already exists.
	ErrProjectAlreadyExists = errors.New("project: already exists")

	// ErrProjectInvalid indicates a project failed validation.
	ErrProjectInvalid = errors.New("project: invalid")

	// ErrProjectStore indicates an internal project store failure.
	ErrProjectStore = errors.New("project: store error")
)

// Note: ErrProjectNotFound and ProjectNotFoundError are declared in
// orchestrator_types.go (agent domain). The project store uses a richer
// ProjectStoreNotFoundError that carries tenant and name context.

// =============================================================================
// Project Store Typed Errors
// =============================================================================

// ProjectStoreNotFoundError is returned by the ProjectStore when a project
// cannot be found. It carries both tenant and optional name context in addition
// to the project ID to distinguish lookup-by-name failures.
type ProjectStoreNotFoundError struct {
	TenantID  string
	ProjectID string
	Name      string
}

// Error implements the error interface.
func (e *ProjectStoreNotFoundError) Error() string {
	if e.Name != "" {
		return ErrProjectNotFound.Error() + ": tenant=" + e.TenantID + " name=" + e.Name
	}
	return ErrProjectNotFound.Error() + ": tenant=" + e.TenantID + " id=" + e.ProjectID
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProjectStoreNotFoundError) Unwrap() error { return ErrProjectNotFound }

// ProjectAlreadyExistsError indicates a project with the same tenant+name already exists.
type ProjectAlreadyExistsError struct {
	TenantID string
	Name     string
}

// Error implements the error interface.
func (e *ProjectAlreadyExistsError) Error() string {
	return ErrProjectAlreadyExists.Error() + ": tenant=" + e.TenantID + " name=" + e.Name
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProjectAlreadyExistsError) Unwrap() error { return ErrProjectAlreadyExists }

// ProjectInvalidError indicates a project failed validation.
type ProjectInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ProjectInvalidError) Error() string {
	if e.Field != "" {
		return ErrProjectInvalid.Error() + ": " + e.Field + ": " + e.Message
	}
	return ErrProjectInvalid.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProjectInvalidError) Unwrap() error { return ErrProjectInvalid }

// ProjectStoreError indicates an internal store failure (marshal, corruption, etc).
type ProjectStoreError struct {
	Message string
}

// Error implements the error interface.
func (e *ProjectStoreError) Error() string {
	return ErrProjectStore.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ProjectStoreError) Unwrap() error { return ErrProjectStore }
