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
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Milestone states in an OwnerReport.
const (
	// MilestoneDone is a milestone the plan marks reached.
	MilestoneDone = "done"
	// MilestoneOverdue is a milestone not reached whose Due is before the
	// report's GeneratedAt.
	MilestoneOverdue = "overdue"
	// MilestoneOpen is a milestone not reached and not yet due.
	MilestoneOpen = "open"
)

// ReporterOptions are the Reporter's dependencies. Planner, Projects and
// Tasks are required; a nil Now means time.Now.
type ReporterOptions struct {
	// Planner reads the tenant's verified plan; its Service signs and
	// verifies reports.
	Planner  *Planner
	Projects orchestration.ProjectStore
	Tasks    orchestration.TaskStore
	Now      func() time.Time
}

// Reporter builds the status report a tenant's appointed Tenant Agent gives
// the tenant's owner. A report reads only the named tenant's plan, projects
// and tasks and writes nothing. It is signed as the tenant's appointed agent,
// and it is refused over a plan that does not verify or over a task listing
// that could not read every record.
type Reporter struct {
	planner  *Planner
	projects orchestration.ProjectStore
	tasks    orchestration.TaskStore
	now      func() time.Time
}

// NewReporter returns a Reporter, or an error wrapping ErrNotConfigured
// naming the first missing dependency.
func NewReporter(opts ReporterOptions) (*Reporter, error) {
	switch {
	case opts.Planner == nil:
		return nil, fmt.Errorf("%w: Planner is nil", ErrNotConfigured)
	case opts.Projects == nil:
		return nil, fmt.Errorf("%w: Projects is nil", ErrNotConfigured)
	case opts.Tasks == nil:
		return nil, fmt.Errorf("%w: Tasks is nil", ErrNotConfigured)
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Reporter{planner: opts.Planner, projects: opts.Projects, tasks: opts.Tasks, now: now}, nil
}

// ProjectStatus is one project's line in an OwnerReport.
type ProjectStatus struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name,omitempty"`

	// Rank is the plan's rank for the project; 0 when it is unranked.
	Rank int `json:"rank,omitempty"`

	// Missing reports a project the plan ranks that is no longer one of
	// the tenant's projects.
	Missing bool `json:"missing,omitempty"`

	// Tasks counts the project's tasks per status name; a status with no
	// task is left out.
	Tasks map[string]int `json:"tasks,omitempty"`

	// NeedsOwner counts the project's tasks blocked waiting on information
	// from the owner (block kind needs_info).
	NeedsOwner int `json:"needs_owner"`

	// Escalated counts the project's tasks blocked with kind escalated.
	Escalated int `json:"escalated"`
}

// MilestoneStatus is one plan milestone's line in an OwnerReport.
type MilestoneStatus struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Due        time.Time `json:"due"`
	ProjectIDs []string  `json:"project_ids,omitempty"`

	// State is MilestoneDone, MilestoneOverdue or MilestoneOpen.
	State string `json:"state"`
}

// OwnerReport is a signed status report of one tenant for its owner.
type OwnerReport struct {
	TenantID    string    `json:"tenant_id"`
	GeneratedAt time.Time `json:"generated_at"`

	// PlanRevision is the revision of the plan the report ranks by; 0 when
	// the tenant has no plan.
	PlanRevision uint64 `json:"plan_revision"`

	// Projects lists ranked projects by rank, then unranked ones by ID.
	Projects []ProjectStatus `json:"projects"`

	// Milestones lists the plan's milestones in the plan's order.
	Milestones []MilestoneStatus `json:"milestones"`

	// Unscoped counts per status the tenant's tasks that name no project or
	// a project not listed in Projects.
	Unscoped map[string]int `json:"unscoped,omitempty"`

	// Principal and SigningKeyID name the appointed agent that signed.
	Principal    string `json:"principal"`
	SigningKeyID string `json:"signing_key_id"`
	Signature    []byte `json:"signature"`
}

// ownerReportPayload is every OwnerReport field but Signature, in a fixed
// order.
type ownerReportPayload struct {
	TenantID     string            `json:"tenant_id"`
	GeneratedAt  time.Time         `json:"generated_at"`
	PlanRevision uint64            `json:"plan_revision"`
	Projects     []ProjectStatus   `json:"projects"`
	Milestones   []MilestoneStatus `json:"milestones"`
	Unscoped     map[string]int    `json:"unscoped,omitempty"`
	Principal    string            `json:"principal"`
	SigningKeyID string            `json:"signing_key_id"`
}

// SigningPayload returns the bytes the report's signature covers: the JSON
// encoding of every field except Signature, in a fixed field order. JSON
// encoding sorts map keys, so the payload is the same for equal reports.
func (r OwnerReport) SigningPayload() ([]byte, error) {
	return json.Marshal(ownerReportPayload{
		TenantID:     r.TenantID,
		GeneratedAt:  r.GeneratedAt,
		PlanRevision: r.PlanRevision,
		Projects:     r.Projects,
		Milestones:   r.Milestones,
		Unscoped:     r.Unscoped,
		Principal:    r.Principal,
		SigningKeyID: r.SigningKeyID,
	})
}

// Report builds, signs and returns tenantID's owner report. It requires a
// standing appointment (ErrNotAppointed), since the report is signed as the
// appointed agent. It reads the tenant's plan verified (a plan that does not
// verify stops the report with ErrBadSignature or ErrForeignKey; no plan
// gives an unranked report), the tenant's projects and the tenant's task
// queue, and refuses an incomplete task listing with the listing's error
// (orchestration.ErrTaskListingIncomplete). It writes nothing.
func (r *Reporter) Report(ctx context.Context, tenantID string) (*OwnerReport, error) {
	ctx, tenant, err := r.planner.agent.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	held, err := r.planner.requireAppointment(ctx, tenant)
	if err != nil {
		return nil, err
	}
	plan, err := r.planner.GetPlan(ctx, tenant)
	switch {
	case errors.Is(err, orchestration.ErrTenantPlanUnknown):
		plan = nil
	case err != nil:
		return nil, err
	}
	projects, err := r.projects.ListByTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	listing, err := r.tasks.ListTaskRecords(ctx, tenant, &orchestration.TaskFilter{})
	if err != nil {
		return nil, err
	}
	if err := listing.Err(tenant); err != nil {
		return nil, err
	}

	report := &OwnerReport{
		TenantID:     tenant,
		GeneratedAt:  r.now().UTC(),
		Milestones:   []MilestoneStatus{},
		Principal:    held.Principal,
		SigningKeyID: held.SigningKeyID,
	}
	byID := map[string]*ProjectStatus{}
	for _, project := range projects {
		byID[project.ID] = &ProjectStatus{ProjectID: project.ID, Name: project.Name}
	}
	if plan != nil {
		report.PlanRevision = plan.Revision
		for _, priority := range plan.Priorities {
			status, ok := byID[priority.ProjectID]
			if !ok {
				status = &ProjectStatus{ProjectID: priority.ProjectID, Missing: true}
				byID[priority.ProjectID] = status
			}
			status.Rank = priority.Rank
		}
		for _, milestone := range plan.Milestones {
			report.Milestones = append(report.Milestones, MilestoneStatus{
				ID:         milestone.ID,
				Title:      milestone.Title,
				Due:        milestone.Due,
				ProjectIDs: milestone.ProjectIDs,
				State:      milestoneState(milestone, report.GeneratedAt),
			})
		}
	}
	for _, task := range listing.Tasks {
		status := string(task.Status)
		entry, ok := byID[task.ProjectID]
		if !ok || task.ProjectID == "" {
			if report.Unscoped == nil {
				report.Unscoped = map[string]int{}
			}
			report.Unscoped[status]++
			continue
		}
		if entry.Tasks == nil {
			entry.Tasks = map[string]int{}
		}
		entry.Tasks[status]++
		if task.Status == orchestration.TaskStatusBlocked {
			switch task.BlockKind {
			case orchestration.TaskBlockKindNeedsInfo:
				entry.NeedsOwner++
			case orchestration.TaskBlockKindEscalated:
				entry.Escalated++
			}
		}
	}
	report.Projects = make([]ProjectStatus, 0, len(byID))
	for _, entry := range byID {
		report.Projects = append(report.Projects, *entry)
	}
	sort.Slice(report.Projects, func(i, j int) bool {
		a, b := report.Projects[i], report.Projects[j]
		switch {
		case a.Rank != 0 && b.Rank != 0 && a.Rank != b.Rank:
			return a.Rank < b.Rank
		case (a.Rank != 0) != (b.Rank != 0):
			return a.Rank != 0
		}
		return a.ProjectID < b.ProjectID
	})

	payload, err := report.SigningPayload()
	if err != nil {
		return nil, fmt.Errorf("tenantagent: encode report: %w", err)
	}
	keyID, sig, err := r.planner.agent.Sign(ctx, tenant, payload)
	if err != nil {
		return nil, err
	}
	if keyID != held.SigningKeyID {
		return nil, fmt.Errorf("%w: signed with %q, report names %q", ErrForeignKey, keyID, held.SigningKeyID)
	}
	report.Signature = sig
	return report, nil
}

// milestoneState is milestone's state at now.
func milestoneState(milestone orchestration.TenantMilestone, now time.Time) string {
	switch {
	case milestone.Done:
		return MilestoneDone
	case milestone.Due.Before(now):
		return MilestoneOverdue
	}
	return MilestoneOpen
}

// VerifyReport checks report's signature with its tenant's Tenant Agent
// key. A nil or changed report gives an error wrapping ErrBadSignature, and
// a key of another tenant one wrapping ErrForeignKey. It does not require a
// standing appointment, so a report stays verifiable after a retirement.
func (r *Reporter) VerifyReport(ctx context.Context, report *OwnerReport) error {
	if report == nil {
		return fmt.Errorf("%w: no report", ErrBadSignature)
	}
	ctx, tenant, err := r.planner.agent.scope(ctx, report.TenantID)
	if err != nil {
		return err
	}
	payload, err := report.SigningPayload()
	if err != nil {
		return fmt.Errorf("tenantagent: encode report: %w", err)
	}
	return r.planner.agent.Verify(ctx, tenant, report.SigningKeyID, payload, report.Signature)
}
