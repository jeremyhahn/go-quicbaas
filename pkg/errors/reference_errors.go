// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package errors

// ---------------------------------------------------------------------------
// Reference errors — a saved field names an object that is not there
// ---------------------------------------------------------------------------
//
// Every field an operator can save that names another object the platform
// controls (a secret, an SSH key, a workflow, a fleet node, a tenant) is
// checked for existence by the service layer on save. The two outcomes a
// check can fail with are deliberately distinct types:
//
//   - ReferenceNotFoundError: the store answered, and the object is not
//     there. The request is wrong; every transport answers it as a bad
//     request naming the field.
//   - ReferenceUnavailableError: the store could not answer (sealed barrier,
//     unwired dependency, I/O failure). Nothing is known about the object, so
//     the save is refused as unavailable, never as "not found" — a sealed
//     store must not tell an operator their correct reference is wrong.
//
// Stored data is never re-validated on read; only a save runs a check.

// Reference kinds name what a reference field points at. They appear in
// error messages, so they read as prose.
const (
	// ReferenceKindSecret is a secret in the tenant's secrets store.
	ReferenceKindSecret = "secret"

	// ReferenceKindSSHKey is a KMS-held SSH key, addressed scope/owner/name.
	ReferenceKindSSHKey = "ssh key"

	// ReferenceKindWorkflow is a workflow definition registered in the tenant.
	ReferenceKindWorkflow = "workflow"

	// ReferenceKindFleetNode is a registered fleet node (agent ID, name or host).
	ReferenceKindFleetNode = "fleet node"

	// ReferenceKindTenant is a tenant known to IAM.
	ReferenceKindTenant = "tenant"
)

// ReferenceNotFoundError reports that a saved field names an object that
// does not exist.
type ReferenceNotFoundError struct {
	// Field is the wire name of the field that holds the reference.
	Field string

	// Kind is what the field references; one of the ReferenceKind constants.
	Kind string

	// Ref is the reference value as the caller wrote it.
	Ref string
}

// Error implements the error interface.
func (e *ReferenceNotFoundError) Error() string {
	return e.Field + ": " + e.Kind + " " + quoteRef(e.Ref) + " does not exist"
}

// ReferenceUnavailableError reports that a saved field's reference could not
// be verified because the store that holds the referenced object could not
// answer. It never means the object is absent.
type ReferenceUnavailableError struct {
	// Field is the wire name of the field that holds the reference.
	Field string

	// Kind is what the field references; one of the ReferenceKind constants.
	Kind string

	// Ref is the reference value as the caller wrote it.
	Ref string

	// Err is why the store could not answer. Nil when no store is attached.
	Err error
}

// Error implements the error interface.
func (e *ReferenceUnavailableError) Error() string {
	msg := e.Field + ": " + e.Kind + " " + quoteRef(e.Ref) + " cannot be verified: "
	if e.Err == nil {
		return msg + "no " + e.Kind + " store is attached"
	}
	return msg + e.Err.Error()
}

// Unwrap returns the store failure for errors.Is and errors.As.
func (e *ReferenceUnavailableError) Unwrap() error {
	return e.Err
}

// quoteRef renders a reference value for a message.
func quoteRef(ref string) string {
	return "\"" + ref + "\""
}
