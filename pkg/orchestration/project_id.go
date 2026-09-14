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

package orchestration

import (
	"errors"

	"github.com/google/uuid"
)

// =============================================================================
// What a project id is
// =============================================================================
//
// A PROJECT HAS TWO IDENTIFIERS AND ONLY ONE OF THEM IS AN ID. The ID is a
// UUID minted when the project is created; the NAME is free text the operator
// chose. They are not interchangeable, and a caller that passes the name where
// the id belongs is not making a typo that surfaces — it is making a record
// scoped to a project that does not exist (#613).
//
// Nothing downstream re-checks it. A project listing filters on the id, a
// project's concurrency quota counts the job leases under the id's key
// subtree, and a lease's project segment is written verbatim from whatever
// the record carried. Each of those quietly finds nothing rather than failing,
// so the only place the mistake can still be reported to the caller who made
// it is where the value is accepted.

// ErrInvalidProjectID indicates a project identifier that is not a project id.
var ErrInvalidProjectID = errors.New("project: identifier is not a project id")

// InvalidProjectIDError reports a project identifier that is set but is not a
// UUID. It names the value so the caller can see what it sent — most often
// the project's name.
type InvalidProjectIDError struct {
	// ProjectID is the value as it was supplied.
	ProjectID string

	// Field names the request field it was supplied in.
	Field string
}

// Error implements the error interface.
func (e *InvalidProjectIDError) Error() string {
	msg := ErrInvalidProjectID.Error() + ": " + e.ProjectID
	if e.Field != "" {
		msg = ErrInvalidProjectID.Error() + ": " + e.Field + ": " + e.ProjectID
	}
	return msg + " (a project id is the UUID minted when the project was created, not its name)"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *InvalidProjectIDError) Unwrap() error { return ErrInvalidProjectID }

// ValidateProjectID refuses a project identifier that is set but is not a
// UUID. An EMPTY identifier is accepted and means unscoped, which is a real
// state: not every task or job belongs to a project.
//
// The value is checked as supplied, not trimmed or lower-cased: an id is
// stored, keyed and compared verbatim, so accepting a value this package
// would then have to normalise would put two spellings of one project into
// the keyspace.
func ValidateProjectID(field, projectID string) error {
	if projectID == "" {
		return nil
	}
	if err := uuid.Validate(projectID); err != nil {
		return &InvalidProjectIDError{ProjectID: projectID, Field: field}
	}
	return nil
}
