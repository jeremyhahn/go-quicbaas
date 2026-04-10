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

package database

import "strconv"

// TenantProvisionError is returned when provisioning a tenant's system
// database fails.
type TenantProvisionError struct {
	TenantID string
	Err      error
}

// Error returns a human-readable description.
func (e *TenantProvisionError) Error() string {
	return "database: failed to provision tenant " + strconv.Quote(e.TenantID) + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *TenantProvisionError) Unwrap() error {
	return e.Err
}

// TenantDeprovisionError is returned when deprovisioning a tenant fails.
type TenantDeprovisionError struct {
	TenantID string
	Err      error
}

// Error returns a human-readable description.
func (e *TenantDeprovisionError) Error() string {
	return "database: failed to deprovision tenant " + strconv.Quote(e.TenantID) + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *TenantDeprovisionError) Unwrap() error {
	return e.Err
}

// DatabaseLimitExceededError is returned when a tenant has reached the
// maximum number of databases.
type DatabaseLimitExceededError struct {
	TenantID string
	Max      int
	Current  int
}

// Error returns a human-readable description.
func (e *DatabaseLimitExceededError) Error() string {
	return "database: tenant " + strconv.Quote(e.TenantID) +
		" has reached the maximum of " + strconv.Itoa(e.Max) + " databases"
}

// TenantAlreadyProvisionedError is returned when attempting to provision
// a tenant that already has a system database.
type TenantAlreadyProvisionedError struct {
	TenantID string
}

// Error returns a human-readable description.
func (e *TenantAlreadyProvisionedError) Error() string {
	return "database: tenant " + strconv.Quote(e.TenantID) + " is already provisioned"
}

// InvalidDatabasePurposeError is returned when an invalid database purpose
// is specified.
type InvalidDatabasePurposeError struct {
	Purpose string
}

// Error returns a human-readable description.
func (e *InvalidDatabasePurposeError) Error() string {
	return "database: invalid database purpose: " + strconv.Quote(e.Purpose)
}
