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
	"slices"
	"sort"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Dispositions of an open task in a Triage.
const (
	// DispositionNeedsOwner is a task blocked waiting on information from
	// the owner (block kind needs_info).
	DispositionNeedsOwner = "needs_owner"
	// DispositionEscalated is a task blocked with kind escalated.
	DispositionEscalated = "escalated"
	// DispositionHeld is a task blocked with any other kind.
	DispositionHeld = "held"
	// DispositionWaiting is a pending task with a dependency that is not
	// completed or not listed.
	DispositionWaiting = "waiting"
	// DispositionReady is a pending task whose every dependency is
	// completed.
	DispositionReady = "ready"
	// DispositionInProgress is a claimed task.
	DispositionInProgress = "in_progress"
)

// TriagerOptions are the Triager's dependencies. Planner, Projects and Tasks
// are required; a nil Now means time.Now.
type TriagerOptions struct {
	// Planner reads the tenant's verified plan; its Service signs and
	// verifies triages.
	Planner  *Planner
	Projects orchestration.ProjectStore
	Tasks    orchestration.TaskStore
	Now      func() time.Time
}

// Triager builds the triage of a tenant's open tasks that the tenant's
// appointed Tenant Agent gives. A triage reads only the named tenant's plan,
// projects and tasks; it writes nothing and changes no task. It is signed as
// the tenant's appointed agent, and it is refused over a plan that does not
// verify or over a task listing that could not read every record.
type Triager struct {
	planner  *Planner
	projects orchestration.ProjectStore
	tasks    orchestration.TaskStore
	now      func() time.Time
}

// NewTriager returns a Triager, or an error wrapping ErrNotConfigured naming
// the first missing dependency.
func NewTriager(opts TriagerOptions) (*Triager, error) {
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
	return &Triager{planner: opts.Planner, projects: opts.Projects, tasks: opts.Tasks, now: now}, nil
}

// TriageItem is one open task's line in a Triage.
type TriageItem struct {
	TaskID    string `json:"task_id"`
	ProjectID string `json:"project_id,omitempty"`
	Subject   string `json:"subject"`
	Status    string `json:"status"`

	// Disposition is one of the Disposition constants.
	Disposition string `json:"disposition"`

	// ProjectRank is the plan's rank for the task's project; 0 when the
	// project is unranked, not one of the tenant's, or the task names none.
	ProjectRank int `json:"project_rank,omitempty"`

	// Priority is the task's own priority; higher comes first.
	Priority int `json:"priority,omitempty"`

	// WaitingOn lists, sorted, the dependency IDs of a waiting task that
	// are not completed or not listed.
	WaitingOn []string `json:"waiting_on,omitempty"`

	// Milestone is the earliest-due milestone not reached that covers the
	// task's project; Overdue reports that it is overdue.
	Milestone string `json:"milestone,omitempty"`
	Overdue   bool   `json:"overdue,omitempty"`

	// Reason is a blocked task's BlockedReason.
	Reason string `json:"reason,omitempty"`
}

// Triage is a signed triage of one tenant's open tasks.
type Triage struct {
	TenantID    string    `json:"tenant_id"`
	GeneratedAt time.Time `json:"generated_at"`

	// PlanRevision is the revision of the plan the triage orders by; 0 when
	// the tenant has no plan.
	PlanRevision uint64 `json:"plan_revision"`

	// Items lists the open tasks: ranked projects by rank, then the rest;
	// within that by priority (higher first), age (older first) and ID.
	Items []TriageItem `json:"items"`

	// Counts counts the items per disposition; a disposition with no item
	// is left out.
	Counts map[string]int `json:"counts,omitempty"`

	// Principal and SigningKeyID name the appointed agent that signed.
	Principal    string `json:"principal"`
	SigningKeyID string `json:"signing_key_id"`
	Signature    []byte `json:"signature"`
}

// triagePayload is every Triage field but Signature, in a fixed order.
type triagePayload struct {
	TenantID     string         `json:"tenant_id"`
	GeneratedAt  time.Time      `json:"generated_at"`
	PlanRevision uint64         `json:"plan_revision"`
	Items        []TriageItem   `json:"items"`
	Counts       map[string]int `json:"counts,omitempty"`
	Principal    string         `json:"principal"`
	SigningKeyID string         `json:"signing_key_id"`
}

// SigningPayload returns the bytes the triage's signature covers: the JSON
// encoding of every field except Signature, in a fixed field order. JSON
// encoding sorts map keys, so the payload is the same for equal triages.
func (t Triage) SigningPayload() ([]byte, error) {
	return json.Marshal(triagePayload{
		TenantID:     t.TenantID,
		GeneratedAt:  t.GeneratedAt,
		PlanRevision: t.PlanRevision,
		Items:        t.Items,
		Counts:       t.Counts,
		Principal:    t.Principal,
		SigningKeyID: t.SigningKeyID,
	})
}

// Triage builds, signs and returns the triage of tenantID's open tasks. It
// requires a standing appointment (ErrNotAppointed), since the triage is
// signed as the appointed agent. It reads the tenant's plan verified (a plan
// that does not verify stops the triage with ErrBadSignature or
// ErrForeignKey; no plan gives an unranked triage), the tenant's projects
// and the tenant's task queue, and refuses an incomplete task listing with
// the listing's error (orchestration.ErrTaskListingIncomplete), since a
// ready verdict depends on every dependency's record. It writes nothing and
// changes no task.
func (t *Triager) Triage(ctx context.Context, tenantID string) (*Triage, error) {
	ctx, tenant, err := t.planner.agent.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	held, err := t.planner.requireAppointment(ctx, tenant)
	if err != nil {
		return nil, err
	}
	plan, err := t.planner.GetPlan(ctx, tenant)
	switch {
	case errors.Is(err, orchestration.ErrTenantPlanUnknown):
		plan = nil
	case err != nil:
		return nil, err
	}
	projects, err := t.projects.ListByTenant(ctx, tenant)
	if err != nil {
		return nil, err
	}
	listing, err := t.tasks.ListTaskRecords(ctx, tenant, &orchestration.TaskFilter{})
	if err != nil {
		return nil, err
	}
	if err := listing.Err(tenant); err != nil {
		return nil, err
	}

	triage := &Triage{
		TenantID:     tenant,
		GeneratedAt:  t.now().UTC(),
		Items:        []TriageItem{},
		Principal:    held.Principal,
		SigningKeyID: held.SigningKeyID,
	}
	ranks := map[string]int{}
	milestones := map[string]orchestration.TenantMilestone{}
	if plan != nil {
		triage.PlanRevision = plan.Revision
		listed := map[string]bool{}
		for _, project := range projects {
			listed[project.ID] = true
		}
		for _, priority := range plan.Priorities {
			if listed[priority.ProjectID] {
				ranks[priority.ProjectID] = priority.Rank
			}
		}
		for _, milestone := range plan.Milestones {
			if milestone.Done {
				continue
			}
			for _, id := range milestone.ProjectIDs {
				if !listed[id] {
					continue
				}
				current, ok := milestones[id]
				if !ok || milestone.Due.Before(current.Due) ||
					(milestone.Due.Equal(current.Due) && milestone.ID < current.ID) {
					milestones[id] = milestone
				}
			}
		}
	}
	byID := make(map[string]*orchestration.Task, len(listing.Tasks))
	for _, task := range listing.Tasks {
		byID[task.ID] = task
	}
	created := map[string]int64{}
	for _, task := range listing.Tasks {
		item, ok := triageItem(task, byID)
		if !ok {
			continue
		}
		if task.ProjectID != "" {
			item.ProjectRank = ranks[task.ProjectID]
			if milestone, ok := milestones[task.ProjectID]; ok {
				item.Milestone = milestone.ID
				item.Overdue = milestoneState(milestone, triage.GeneratedAt) == MilestoneOverdue
			}
		}
		created[task.ID] = task.CreatedAt
		if triage.Counts == nil {
			triage.Counts = map[string]int{}
		}
		triage.Counts[item.Disposition]++
		triage.Items = append(triage.Items, item)
	}
	sort.Slice(triage.Items, func(i, j int) bool {
		a, b := triage.Items[i], triage.Items[j]
		switch {
		case a.ProjectRank != 0 && b.ProjectRank != 0 && a.ProjectRank != b.ProjectRank:
			return a.ProjectRank < b.ProjectRank
		case (a.ProjectRank != 0) != (b.ProjectRank != 0):
			return a.ProjectRank != 0
		case a.Priority != b.Priority:
			return a.Priority > b.Priority
		case created[a.TaskID] != created[b.TaskID]:
			return created[a.TaskID] < created[b.TaskID]
		}
		return a.TaskID < b.TaskID
	})

	payload, err := triage.SigningPayload()
	if err != nil {
		return nil, fmt.Errorf("tenantagent: encode triage: %w", err)
	}
	keyID, sig, err := t.planner.agent.Sign(ctx, tenant, payload)
	if err != nil {
		return nil, err
	}
	if keyID != held.SigningKeyID {
		return nil, fmt.Errorf("%w: signed with %q, triage names %q", ErrForeignKey, keyID, held.SigningKeyID)
	}
	triage.Signature = sig
	return triage, nil
}

// triageItem returns task's item without its plan fields, or false when
// the task is not open (completed, failed or cancelled). byID holds every
// listed task of the tenant.
func triageItem(task *orchestration.Task, byID map[string]*orchestration.Task) (TriageItem, bool) {
	item := TriageItem{
		TaskID:    task.ID,
		ProjectID: task.ProjectID,
		Subject:   task.Subject,
		Status:    string(task.Status),
		Priority:  task.Priority,
	}
	switch task.Status {
	case orchestration.TaskStatusBlocked:
		item.Reason = task.BlockedReason
		switch task.BlockKind {
		case orchestration.TaskBlockKindNeedsInfo:
			item.Disposition = DispositionNeedsOwner
		case orchestration.TaskBlockKindEscalated:
			item.Disposition = DispositionEscalated
		default:
			item.Disposition = DispositionHeld
		}
	case orchestration.TaskStatusClaimed:
		item.Disposition = DispositionInProgress
	case orchestration.TaskStatusPending:
		for _, dep := range task.Dependencies {
			if found, ok := byID[dep]; !ok || found.Status != orchestration.TaskStatusCompleted {
				item.WaitingOn = append(item.WaitingOn, dep)
			}
		}
		item.Disposition = DispositionReady
		if len(item.WaitingOn) > 0 {
			slices.Sort(item.WaitingOn)
			item.WaitingOn = slices.Compact(item.WaitingOn)
			item.Disposition = DispositionWaiting
		}
	default:
		return TriageItem{}, false
	}
	return item, true
}

// VerifyTriage checks triage's signature with its tenant's Tenant Agent key.
// A nil or changed triage gives an error wrapping ErrBadSignature, and a key
// of another tenant one wrapping ErrForeignKey. It does not require a
// standing appointment, so a triage stays verifiable after a retirement.
func (t *Triager) VerifyTriage(ctx context.Context, triage *Triage) error {
	if triage == nil {
		return fmt.Errorf("%w: no triage", ErrBadSignature)
	}
	ctx, tenant, err := t.planner.agent.scope(ctx, triage.TenantID)
	if err != nil {
		return err
	}
	payload, err := triage.SigningPayload()
	if err != nil {
		return fmt.Errorf("tenantagent: encode triage: %w", err)
	}
	return t.planner.agent.Verify(ctx, tenant, triage.SigningKeyID, payload, triage.Signature)
}
