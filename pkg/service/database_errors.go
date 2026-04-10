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

package service

import "strconv"

// DatabaseNotFoundError is returned when a database is not found.
type DatabaseNotFoundError struct {
	Name string
}

// Error returns a human-readable description.
func (e *DatabaseNotFoundError) Error() string {
	return "service: database not found: " + strconv.Quote(e.Name)
}

// DatabaseCreateError is returned when creating a database fails.
type DatabaseCreateError struct {
	Name string
	Err  error
}

// Error returns a human-readable description.
func (e *DatabaseCreateError) Error() string {
	return "service: failed to create database " + strconv.Quote(e.Name) + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *DatabaseCreateError) Unwrap() error {
	return e.Err
}

// DatabaseDropError is returned when dropping a database fails.
type DatabaseDropError struct {
	Name string
	Err  error
}

// Error returns a human-readable description.
func (e *DatabaseDropError) Error() string {
	return "service: failed to drop database " + strconv.Quote(e.Name) + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *DatabaseDropError) Unwrap() error {
	return e.Err
}

// DatabaseUpdateError is returned when updating a database configuration fails.
type DatabaseUpdateError struct {
	Name      string
	Operation string
	Err       error
}

// Error returns a human-readable description.
func (e *DatabaseUpdateError) Error() string {
	return "service: failed to update " + e.Operation + " for database " + strconv.Quote(e.Name) + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *DatabaseUpdateError) Unwrap() error {
	return e.Err
}

// DatabaseServiceNilError is returned when the qrdb DatabaseServicer is nil.
type DatabaseServiceNilError struct{}

// Error returns a human-readable description.
func (e *DatabaseServiceNilError) Error() string {
	return "service: qrdb database servicer is nil"
}

// InvalidRequestError is returned when a request is nil or otherwise invalid.
type InvalidRequestError struct {
	Detail string
}

// Error returns a human-readable description.
func (e *InvalidRequestError) Error() string {
	return "service: invalid request: " + e.Detail
}
