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

package sdlc

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Boot migration: a binding's agent tuples become Agent references
// =============================================================================
//
// A binding written before Agents existed stored its worker and triage as
// {backend, model, image} and each review seat with a backend, model, image
// and reasoning effort. The current record type has no such fields, so a legacy
// binding decodes with those slots EMPTY; this migration reads the same
// records through legacyBinding, which still has them, seeds one Agent per
// distinct tuple, and writes the binding back with references.
//
// IDEMPOTENT BY CONSTRUCTION. A migrated binding stores references, which
// legacyBinding reads as empty tuples, so a second boot finds nothing to do.

// legacyTuple is a slot as a pre-Agents binding stored it.
type legacyTuple struct {
	Backend         string `json:"backend,omitempty"`
	Model           string `json:"model,omitempty"`
	Image           string `json:"image,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

func (t legacyTuple) tuple() orchestration.AgentTuple {
	return orchestration.AgentTuple{Backend: t.Backend, Model: t.Model, Image: t.Image, ReasoningEffort: t.ReasoningEffort}
}

// legacyBinding is the part of a pre-Agents binding record the migration
// reads. It shares the binding's entity type, so it decodes the same records.
type legacyBinding struct {
	ID        uint64        `json:"-"`
	ProjectID string        `json:"project_id"`
	Worker    legacyTuple   `json:"worker"`
	Triage    legacyTuple   `json:"triage"`
	Reviewer  legacyTuple   `json:"reviewer"`
	Reviewers []legacyTuple `json:"reviewers"`
}

// EntityID implements the DAO entity contract.
func (b *legacyBinding) EntityID() uint64 { return b.ID }

// SetEntityID implements the DAO entity contract.
func (b *legacyBinding) SetEntityID(id uint64) { b.ID = id }

// hasTuples reports whether any slot still holds a tuple.
func (b *legacyBinding) hasTuples() bool {
	if !b.Worker.tuple().IsZero() || !b.Triage.tuple().IsZero() || !b.Reviewer.tuple().IsZero() {
		return true
	}
	for _, seat := range b.Reviewers {
		if !seat.tuple().IsZero() {
			return true
		}
	}
	return false
}

// BindingAgentMigration is what migrating one tenant's bindings did.
type BindingAgentMigration struct {
	// Migrated are the projects whose bindings now reference Agents.
	Migrated []string

	// Failed maps a project to why its binding could not be migrated; the
	// binding is left for an operator to rebind.
	Failed map[string]error
}

// MigrateBindingAgents migrates every binding of tenantID that still stores
// agent tuples, seeding Agents through seeder.
func MigrateBindingAgents(ctx context.Context, store *Store, seeder *orchestration.AgentSeeder, tenantID string) (BindingAgentMigration, error) {
	report := BindingAgentMigration{Failed: map[string]error{}}
	legacy, err := store.legacyBindings(ctx, tenantID)
	if err != nil {
		return report, err
	}
	for _, old := range legacy {
		if !old.hasTuples() {
			continue
		}
		if err := migrateBinding(ctx, store, seeder, tenantID, old); err != nil {
			report.Failed[old.ProjectID] = err
			continue
		}
		report.Migrated = append(report.Migrated, old.ProjectID)
	}
	return report, nil
}

// migrateBinding points one binding's slots at the Agents its tuples become.
func migrateBinding(ctx context.Context, store *Store, seeder *orchestration.AgentSeeder, tenantID string, old *legacyBinding) error {
	binding, err := store.GetBinding(ctx, tenantID, old.ProjectID)
	if err != nil {
		return err
	}
	seed := func(tuple legacyTuple, role orchestration.AgentRole) (string, error) {
		if tuple.tuple().IsZero() {
			return "", nil
		}
		return seeder.Seed(ctx, tuple.tuple(), role)
	}
	worker, err := seed(old.Worker, orchestration.AgentRoleCoding)
	if err != nil {
		return err
	}
	if worker != "" {
		binding.Worker = orchestration.AgentRefTo(worker)
	}
	triage, err := seed(old.Triage, orchestration.AgentRoleTriage)
	if err != nil {
		return err
	}
	if triage != "" {
		binding.Triage = orchestration.AgentRefTo(triage)
	}
	if binding.Reviewer.Agent, err = seed(old.Reviewer, orchestration.AgentRoleReview); err != nil {
		return err
	}
	for i := range binding.Reviewers {
		if i >= len(old.Reviewers) {
			break
		}
		if binding.Reviewers[i].Agent, err = seed(old.Reviewers[i], orchestration.AgentRoleReview); err != nil {
			return err
		}
	}
	return store.UpdateBinding(ctx, tenantID, binding)
}

// legacyBindings reads every binding record of tenantID as legacyBinding.
func (s *Store) legacyBindings(ctx context.Context, tenantID string) ([]*legacyBinding, error) {
	dao, err := entityDAO(ctx, s, "legacyBindings", tenantID, bindingEntityType, func() *legacyBinding { return &legacyBinding{} })
	if err != nil {
		return nil, err
	}
	var out []*legacyBinding
	pageErr := dao.ForEachPage(ctx, qrdbsdk.PageQuery{PageSize: listPageSize}, func(page qrdbsdk.PageResult[*legacyBinding]) error {
		out = append(out, page.Entities...)
		return nil
	})
	if pageErr != nil {
		return nil, backendError("legacyBindings", pageErr)
	}
	return out, nil
}
