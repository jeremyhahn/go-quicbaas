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

import (
	"context"
	"errors"
)

// =============================================================================
// Project Authorization — typed errors
// =============================================================================

// ErrProjectAccessDenied is the sentinel error for project-level access denial.
var ErrProjectAccessDenied = errors.New("project: access denied")

// ErrProjectRoleInvalid is the sentinel error for an unrecognized role value.
var ErrProjectRoleInvalid = errors.New("project: invalid role")

// ErrProjectUnresolved is the sentinel error for a project-scoped
// authorization whose project the store could not produce.
var ErrProjectUnresolved = errors.New("project: unresolved")

// ProjectAccessDeniedError is returned when an agent lacks the required role.
type ProjectAccessDeniedError struct {
	ProjectID    string
	AgentID      string
	RequiredRole ProjectRole
	ActualRole   ProjectRole
}

// Error implements the error interface.
func (e *ProjectAccessDeniedError) Error() string {
	return ErrProjectAccessDenied.Error() +
		": project=" + e.ProjectID +
		" agent=" + e.AgentID +
		" required=" + string(e.RequiredRole) +
		" actual=" + string(e.ActualRole)
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ProjectAccessDeniedError) Unwrap() error { return ErrProjectAccessDenied }

// ProjectUnresolvedError is returned when a project-scoped authorization names
// a project the store cannot produce.
//
// IT IS A REFUSAL, NOT A PASS. The role matrix lives on the project record, so
// a caller that cannot read the record cannot know what the project permits —
// and a check that answers "allowed" when it could not read the rule is not a
// check. The record is missing for exactly two reasons, and both must refuse:
// the project was deleted (its tasks are orphans no agent should still be
// dispatched or handed), or the store the authorizer reads is not the one the
// project was written to, in which case every project looks unscoped and the
// whole boundary silently disappears (go-quicbaas#634).
type ProjectUnresolvedError struct {
	TenantID  string
	ProjectID string
	AgentID   string
}

// Error implements the error interface.
func (e *ProjectUnresolvedError) Error() string {
	return ErrProjectUnresolved.Error() +
		": tenant=" + e.TenantID +
		" project=" + e.ProjectID +
		" agent=" + e.AgentID
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ProjectUnresolvedError) Unwrap() error { return ErrProjectUnresolved }

// ProjectRoleInvalidError is returned when a role value is not recognized.
type ProjectRoleInvalidError struct {
	Role string
}

// Error implements the error interface.
func (e *ProjectRoleInvalidError) Error() string {
	return ErrProjectRoleInvalid.Error() + ": " + e.Role
}

// Unwrap returns the sentinel for errors.Is support.
func (e *ProjectRoleInvalidError) Unwrap() error { return ErrProjectRoleInvalid }

// =============================================================================
// Role ordering for ≥ comparisons
// =============================================================================

// roleWeight maps each ProjectRole to a numeric weight for ordered comparison.
// Higher weight = more permissive.
var roleWeight = map[ProjectRole]int{
	ProjectRoleDenied:   0,
	ProjectRoleObserver: 1,
	ProjectRoleWorker:   2,
	ProjectRoleLead:     3,
}

// RoleAtLeast returns true when actual is at least as permissive as required.
// An unrecognized role string is treated as denied (weight 0).
func RoleAtLeast(actual, required ProjectRole) bool {
	return roleWeight[actual] >= roleWeight[required]
}

// =============================================================================
// ProjectAuthz — authorization helper
// =============================================================================

// ProjectAuthz checks per-project agent authorization against a ProjectStore.
// It is intentionally stateless; callers supply the store on construction.
type ProjectAuthz struct {
	store ProjectStore
}

// NewProjectAuthz creates a ProjectAuthz backed by the given store.
func NewProjectAuthz(store ProjectStore) *ProjectAuthz {
	return &ProjectAuthz{store: store}
}

// AuthorizeAgent verifies that agentID holds at least requiredRole in projectID.
//
// Rules:
//   - ProjectRoleDenied → always rejected with ProjectAccessDeniedError.
//   - Project has a non-empty AgentRoles map and agentID is absent → denied.
//   - Project has an empty AgentRoles map (legacy unscoped) → allowed.
//   - Actual role weight < required role weight → denied.
//   - Project cannot be read → ProjectUnresolvedError. A check that cannot
//     read the rule refuses; it never passes.
func (a *ProjectAuthz) AuthorizeAgent(ctx context.Context, projectID, agentID string, requiredRole ProjectRole) error {
	if projectID == "" || agentID == "" {
		return nil // unscoped call — legacy path
	}
	if _, ok := validProjectRoles[requiredRole]; !ok {
		return &ProjectRoleInvalidError{Role: string(requiredRole)}
	}

	project, err := a.findProject(ctx, projectID)
	if err != nil {
		return err
	}
	if project == nil {
		return &ProjectUnresolvedError{ProjectID: projectID, AgentID: agentID}
	}

	actual := a.resolveRole(project, agentID)
	if !RoleAtLeast(actual, requiredRole) {
		return &ProjectAccessDeniedError{
			ProjectID:    projectID,
			AgentID:      agentID,
			RequiredRole: requiredRole,
			ActualRole:   actual,
		}
	}
	return nil
}

// resolveRole returns the effective ProjectRole for the given agent within a project.
func (a *ProjectAuthz) resolveRole(project *Project, agentID string) ProjectRole {
	roles := project.Permissions.AgentRoles
	if len(roles) == 0 {
		// Legacy unscoped project: all agents allowed as lead.
		return ProjectRoleLead
	}
	role, ok := roles[agentID]
	if !ok {
		// Agent not in matrix for a scoped project → denied.
		return ProjectRoleDenied
	}
	return role
}

// findProject looks up a project across all tenants by iterating.
// In practice callers supply the tenantID; this helper accepts an empty
// tenantID to enable a "find by project ID" path for task-claim interception
// where only the projectID is available. Returns a nil project (not an error)
// when the record is absent; the caller turns that into a refusal.
func (a *ProjectAuthz) findProject(ctx context.Context, projectID string) (*Project, error) {
	// Attempt a direct lookup. The store interface requires a tenantID; when
	// we don't have it we use an empty string as a signal that the store may
	// return ErrProjectNotFound and we'll treat that as "unscoped".
	project, err := a.store.Get(ctx, "", projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return nil, nil
		}
		return nil, err
	}
	return project, nil
}

// AuthorizeAgentForTenant is like AuthorizeAgent but uses a tenant-scoped
// lookup which is more efficient when the tenantID is known. A project the
// store cannot produce refuses with ProjectUnresolvedError.
func (a *ProjectAuthz) AuthorizeAgentForTenant(ctx context.Context, tenantID, projectID, agentID string, requiredRole ProjectRole) error {
	if projectID == "" || agentID == "" {
		return nil
	}
	if _, ok := validProjectRoles[requiredRole]; !ok {
		return &ProjectRoleInvalidError{Role: string(requiredRole)}
	}

	project, err := a.store.Get(ctx, tenantID, projectID)
	if err != nil {
		var notFound *ProjectStoreNotFoundError
		if errors.As(err, &notFound) {
			return &ProjectUnresolvedError{TenantID: tenantID, ProjectID: projectID, AgentID: agentID}
		}
		return err
	}

	actual := a.resolveRole(project, agentID)
	if !RoleAtLeast(actual, requiredRole) {
		return &ProjectAccessDeniedError{
			ProjectID:    projectID,
			AgentID:      agentID,
			RequiredRole: requiredRole,
			ActualRole:   actual,
		}
	}
	return nil
}
