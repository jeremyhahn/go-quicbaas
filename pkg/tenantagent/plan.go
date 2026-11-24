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

package tenantagent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

var (
	// ErrUnknownProject reports a plan naming a project the tenant does not
	// have.
	ErrUnknownProject = errors.New("tenantagent: project is not one of this tenant's")
)

// PlannerOptions are the Planner's dependencies; every one is required.
type PlannerOptions struct {
	Agent    *Service
	Plans    orchestration.TenantPlanStore
	Projects orchestration.ProjectStore
}

// Planner sets and reads a tenant's plan: its ranked project priorities and
// milestones. A plan is written only through the tenant's appointed Tenant
// Agent and signed with that agent's key, and every read verifies the
// signature. Nothing here reads or writes another tenant.
type Planner struct {
	agent    *Service
	plans    orchestration.TenantPlanStore
	projects orchestration.ProjectStore
}

// NewPlanner returns a Planner, or an error wrapping ErrNotConfigured naming
// the first missing dependency.
func NewPlanner(opts PlannerOptions) (*Planner, error) {
	switch {
	case opts.Agent == nil:
		return nil, fmt.Errorf("%w: Agent is nil", ErrNotConfigured)
	case opts.Plans == nil:
		return nil, fmt.Errorf("%w: Plans is nil", ErrNotConfigured)
	case opts.Projects == nil:
		return nil, fmt.Errorf("%w: Projects is nil", ErrNotConfigured)
	}
	return &Planner{agent: opts.Agent, plans: opts.Plans, projects: opts.Projects}, nil
}

// PlanRequest is the content of a plan write.
type PlanRequest struct {
	Priorities []orchestration.TenantPriority
	Milestones []orchestration.TenantMilestone

	// ExpectedRevision is the revision the caller last read; 0 when none.
	ExpectedRevision uint64
}

// SetPlan writes tenantID's next plan revision as its appointed Tenant Agent.
// It requires a standing appointment (ErrNotAppointed), refuses a project
// the tenant does not have (ErrUnknownProject) before signing, signs the
// plan with the agent's key and stores it by revision compare-and-set, so a
// stale ExpectedRevision gives orchestration.ErrTenantPlanConflict. It
// returns the stored plan, verified.
func (p *Planner) SetPlan(ctx context.Context, tenantID string, req PlanRequest) (*orchestration.TenantPlan, error) {
	ctx, tenant, err := p.agent.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	held, err := p.requireAppointment(ctx, tenant)
	if err != nil {
		return nil, err
	}
	plan := &orchestration.TenantPlan{
		TenantID:     tenant,
		Priorities:   req.Priorities,
		Milestones:   req.Milestones,
		Revision:     req.ExpectedRevision + 1,
		Principal:    held.Principal,
		SigningKeyID: held.SigningKeyID,
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	if err := p.resolveProjects(ctx, tenant, plan); err != nil {
		return nil, err
	}
	payload, err := plan.SigningPayload()
	if err != nil {
		return nil, fmt.Errorf("tenantagent: encode plan: %w", err)
	}
	keyID, sig, err := p.agent.Sign(ctx, tenant, payload)
	if err != nil {
		return nil, err
	}
	if keyID != held.SigningKeyID {
		return nil, fmt.Errorf("%w: signed with %q, plan names %q", ErrForeignKey, keyID, held.SigningKeyID)
	}
	plan.Signature = sig
	if err := p.plans.PutTenantPlan(ctx, tenant, plan, req.ExpectedRevision); err != nil {
		return nil, err
	}
	return p.GetPlan(ctx, tenant)
}

// requireAppointment returns tenant's standing appointment, or an error
// wrapping ErrNotAppointed when there is none.
func (p *Planner) requireAppointment(ctx context.Context, tenant string) (*orchestration.TenantAgent, error) {
	held, err := p.agent.store.GetTenantAgent(ctx, tenant)
	if err != nil {
		if errors.Is(err, orchestration.ErrTenantAgentUnknown) {
			return nil, fmt.Errorf("%w: %w", ErrNotAppointed, err)
		}
		return nil, err
	}
	return held, nil
}

// resolveProjects requires every project the plan names to exist in
// tenant's ProjectStore, looking each up once.
func (p *Planner) resolveProjects(ctx context.Context, tenant string, plan *orchestration.TenantPlan) error {
	seen := map[string]struct{}{}
	check := func(id string) error {
		if _, done := seen[id]; done {
			return nil
		}
		seen[id] = struct{}{}
		if _, err := p.projects.Get(ctx, tenant, id); err != nil {
			if errors.Is(err, orchestration.ErrProjectNotFound) {
				return fmt.Errorf("%w: %q", ErrUnknownProject, id)
			}
			return err
		}
		return nil
	}
	for _, priority := range plan.Priorities {
		if err := check(priority.ProjectID); err != nil {
			return err
		}
	}
	for _, milestone := range plan.Milestones {
		for _, id := range milestone.ProjectIDs {
			if err := check(id); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetPlan returns tenantID's plan after verifying its signature with the
// tenant's Tenant Agent key. A plan that does not verify is not returned:
// the error wraps ErrBadSignature or ErrForeignKey. GetPlan does not require
// a standing appointment, so a plan stays readable after a retirement.
func (p *Planner) GetPlan(ctx context.Context, tenantID string) (*orchestration.TenantPlan, error) {
	ctx, tenant, err := p.agent.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	plan, err := p.plans.GetTenantPlan(ctx, tenant)
	if err != nil {
		return nil, err
	}
	payload, err := plan.SigningPayload()
	if err != nil {
		return nil, fmt.Errorf("tenantagent: encode plan: %w", err)
	}
	if err := p.agent.Verify(ctx, tenant, plan.SigningKeyID, payload, plan.Signature); err != nil {
		return nil, err
	}
	return plan, nil
}

// ClearPlan removes tenantID's plan as its appointed Tenant Agent. It
// requires a standing appointment (ErrNotAppointed) and is otherwise
// idempotent.
func (p *Planner) ClearPlan(ctx context.Context, tenantID string) error {
	ctx, tenant, err := p.agent.scope(ctx, tenantID)
	if err != nil {
		return err
	}
	if _, err := p.requireAppointment(ctx, tenant); err != nil {
		return err
	}
	return p.plans.DeleteTenantPlan(ctx, tenant)
}
