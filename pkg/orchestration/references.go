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
	"context"
	"errors"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// =============================================================================
// Reference checks — a save never stores a name that resolves to nothing
// =============================================================================
//
// Each check answers one question for one field on save: does the object the
// field names exist? The store answering "no" is a *qerrors.
// ReferenceNotFoundError; the store failing to answer is a *qerrors.
// ReferenceUnavailableError, so a sealed or unwired store never reads as a
// wrong reference. Callers run a check only for a field the request sets:
// stored data is not re-validated.

// WorkflowGetter resolves a tenant's workflow by name; *WorkflowLoader
// satisfies it.
type WorkflowGetter interface {
	Get(tenantID, name string) (*WorkflowDefinition, error)
}

// FleetNodeLister lists a tenant's registered fleet nodes; FleetRegistry
// satisfies it.
type FleetNodeLister interface {
	ListFleetNodes(ctx context.Context, tenantID string, filter *FleetNodeFilter) ([]*FleetNode, error)
}

// TenantResolver answers whether a tenant exists.
type TenantResolver interface {
	TenantExists(ctx context.Context, tenantID string) (bool, error)
}

// RequireWorkflow refuses a workflow name the tenant does not have.
func RequireWorkflow(workflows WorkflowGetter, field, tenantID, name string) error {
	if workflows == nil {
		return &qerrors.ReferenceUnavailableError{Field: field, Kind: qerrors.ReferenceKindWorkflow, Ref: name}
	}
	_, err := workflows.Get(tenantID, name)
	if err == nil {
		return nil
	}
	var notFound *WorkflowNotFoundError
	if errors.As(err, &notFound) {
		return &qerrors.ReferenceNotFoundError{Field: field, Kind: qerrors.ReferenceKindWorkflow, Ref: name}
	}
	return &qerrors.ReferenceUnavailableError{Field: field, Kind: qerrors.ReferenceKindWorkflow, Ref: name, Err: err}
}

// RequireFleetNode refuses a fleet node reference that matches no registered
// node in the tenant. A node is named by its ID, its name or its host — the
// same three identities placement and session reconciliation match on.
func RequireFleetNode(ctx context.Context, nodes FleetNodeLister, field, tenantID, ref string) error {
	if nodes == nil {
		return &qerrors.ReferenceUnavailableError{Field: field, Kind: qerrors.ReferenceKindFleetNode, Ref: ref}
	}
	agents, err := nodes.ListFleetNodes(ctx, tenantID, nil)
	if err != nil {
		return &qerrors.ReferenceUnavailableError{Field: field, Kind: qerrors.ReferenceKindFleetNode, Ref: ref, Err: err}
	}
	for _, agent := range agents {
		if agent != nil && (agent.ID == ref || agent.Name == ref || agent.Host == ref) {
			return nil
		}
	}
	return &qerrors.ReferenceNotFoundError{Field: field, Kind: qerrors.ReferenceKindFleetNode, Ref: ref}
}

// RequireTenant refuses a tenant ID IAM does not know.
func RequireTenant(ctx context.Context, tenants TenantResolver, field, tenantID string) error {
	if tenants == nil {
		return &qerrors.ReferenceUnavailableError{Field: field, Kind: qerrors.ReferenceKindTenant, Ref: tenantID}
	}
	exists, err := tenants.TenantExists(ctx, tenantID)
	if err != nil {
		return &qerrors.ReferenceUnavailableError{Field: field, Kind: qerrors.ReferenceKindTenant, Ref: tenantID, Err: err}
	}
	if !exists {
		return &qerrors.ReferenceNotFoundError{Field: field, Kind: qerrors.ReferenceKindTenant, Ref: tenantID}
	}
	return nil
}
