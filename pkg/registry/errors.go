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

package registry

// RegistryError wraps a backend failure during a registry operation.
type RegistryError struct {
	// Op is the registry operation that failed (grant, put, scan, ...).
	Op string

	// Err is the underlying backend error.
	Err error
}

// Error returns a human-readable description.
func (e *RegistryError) Error() string {
	return "registry: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying error.
func (e *RegistryError) Unwrap() error {
	return e.Err
}

// InvalidInstanceError is returned when a ServiceInstance is missing required
// fields.
type InvalidInstanceError struct {
	// Reason describes what is invalid.
	Reason string
}

// Error returns a human-readable description.
func (e *InvalidInstanceError) Error() string {
	return "registry: invalid service instance: " + e.Reason
}
