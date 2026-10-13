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

package prompts

import (
	"path/filepath"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
)

// MaxRevisions bounds the history a record carries inline. The oldest
// revision is dropped when a write would exceed it, so a record stays one
// key of bounded size however often it is edited.
const MaxRevisions = 20

// tenantScope is the scope segment of a tenant-wide record, the one with no
// project. It cannot be a project id: Ref.Validate refuses it as one.
const tenantScope = "_tenant"

// Scope says which layer of the inheritance chain a prompt came from.
type Scope string

const (
	// ScopeProject is a record overriding the prompt for one project.
	ScopeProject Scope = "project"

	// ScopeTenant is a tenant's own record.
	ScopeTenant Scope = "tenant"

	// ScopeSystem is the system tenant's record, the one the boot reconciler
	// writes from the embedded defaults.
	ScopeSystem Scope = "system"

	// ScopeEmbedded is the body compiled into the binary, used when no store
	// record answers.
	ScopeEmbedded Scope = "embedded"
)

// Ref addresses one prompt at one scope.
type Ref struct {
	// TenantID owns the record. qrdbsdk.SystemTenantID addresses the system
	// set.
	TenantID string

	// ProjectID is the project the record overrides the prompt for; empty
	// for the tenant-wide record.
	ProjectID string

	// Set and Name identify the prompt.
	Set  SetKind
	Name string
}

// String names the ref for errors and logs: tenant/project/set/name, with
// the tenant-wide scope spelled as such.
func (r Ref) String() string {
	return r.TenantID + "/" + r.scopeSegment() + "/" + string(r.Set) + "/" + r.Name
}

// Scope returns which layer the ref addresses.
func (r Ref) Scope() Scope {
	switch {
	case r.ProjectID != "":
		return ScopeProject
	case r.TenantID == qrdbsdk.SystemTenantID:
		return ScopeSystem
	default:
		return ScopeTenant
	}
}

// Validate refuses a ref that could not address a record.
func (r Ref) Validate() error {
	if err := validateScope(r.TenantID, r.ProjectID); err != nil {
		return err
	}
	if err := validateSegment("set", string(r.Set)); err != nil {
		return err
	}
	return validateSegment("name", r.Name)
}

// validateScope refuses a tenant or project id that could not address a
// scope.
func validateScope(tenantID, projectID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return &InvalidInputError{Field: "tenant_id", Value: tenantID, Message: "must not be empty"}
	}
	if projectID == "" {
		return nil
	}
	if projectID == tenantScope {
		return &InvalidInputError{Field: "project_id", Value: projectID, Message: "is reserved for the tenant-wide record"}
	}
	return validateSegment("project_id", projectID)
}

// scopeSegment is the ref's scope as it appears in its identity.
func (r Ref) scopeSegment() string {
	if r.ProjectID == "" {
		return tenantScope
	}
	return r.ProjectID
}

// entityID derives the record's DAO entity id from its identity within the
// tenant, so a read is one key and no secondary index exists to drift.
func (r Ref) entityID() uint64 {
	hashed := id.HashString(r.scopeSegment() + "/" + string(r.Set) + "/" + r.Name)
	if hashed == 0 {
		// Zero is reserved for "unset" on Record.ID.
		return 1
	}
	return hashed
}

// validateSegment refuses a value that cannot be one segment of an identity.
func validateSegment(field, value string) error {
	switch {
	case value == "":
		return &InvalidInputError{Field: field, Value: value, Message: "must not be empty"}
	case value == "." || value == "..":
		return &InvalidInputError{Field: field, Value: value, Message: "path-element names cannot be identifiers"}
	case strings.ContainsAny(value, "/\\\x00"):
		return &InvalidInputError{Field: field, Value: value, Message: "must not contain path separators or NUL"}
	case filepath.Clean(value) != value:
		return &InvalidInputError{Field: field, Value: value, Message: "must survive path cleaning unchanged"}
	}
	return nil
}

// Revision is one entry in a record's history. Every write — an edit, a
// rollback, a reset, a reconcile — is a new revision; history is never
// rewritten, only bounded.
type Revision struct {
	Version      int64     `json:"version"`
	ContentHash  string    `json:"content_hash"`
	Content      string    `json:"content"`
	Author       string    `json:"author"`
	Note         string    `json:"note,omitempty"`
	FollowParent bool      `json:"follow_parent"`
	CreatedAt    time.Time `json:"created_at"`
}

// Record is one prompt at one scope of one tenant: the current body and its
// bounded history.
type Record struct {
	// ID is the DAO entity id, derived from the ref. Never set by callers.
	ID uint64 `json:"-"`

	TenantID  string  `json:"tenant_id"`
	ProjectID string  `json:"project_id,omitempty"`
	Set       SetKind `json:"set"`
	Name      string  `json:"name"`

	// Content is the current body and ContentHash its Hash.
	Content     string `json:"content"`
	ContentHash string `json:"content_hash"`

	// Version is the current revision's version, starting at 1. It is the
	// compare-and-swap guard of every write.
	Version int64 `json:"version"`

	// FollowParent says the record is an unedited copy that tracks the scope
	// above it: resolution skips it. Seeded copies carry it; an edit or a
	// rollback clears it; a reset sets it again.
	FollowParent bool `json:"follow_parent"`

	// Revisions is the history, oldest first, the current revision last.
	Revisions []Revision `json:"revisions"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Ref returns the record's address.
func (r *Record) Ref() Ref {
	return Ref{TenantID: r.TenantID, ProjectID: r.ProjectID, Set: r.Set, Name: r.Name}
}

// Revision returns the revision with the given version from the bounded
// history.
func (r *Record) Revision(version int64) (Revision, bool) {
	for _, revision := range r.Revisions {
		if revision.Version == version {
			return revision, true
		}
	}
	return Revision{}, false
}

// EntityID implements the DAO entity contract.
func (r *Record) EntityID() uint64 { return r.ID }

// SetEntityID implements the DAO entity contract.
func (r *Record) SetEntityID(id uint64) { r.ID = id }

// GetVersion implements the DAO's Versioned contract.
func (r *Record) GetVersion() int64 { return r.Version }

// SetVersion implements the DAO's Versioned contract.
func (r *Record) SetVersion(version int64) { r.Version = version }

// identifies reports whether the record stored in a slot is the one ref
// addresses, rather than another identity whose digest collided with it.
func (r *Record) identifies(ref Ref) bool {
	return r.ProjectID == ref.ProjectID && r.Set == ref.Set && r.Name == ref.Name
}
