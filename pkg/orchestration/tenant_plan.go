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
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// TenantPlanRecordID is the fixed key segment a tenant's TenantPlan is
// stored under, so every tenant holds exactly one plan.
const TenantPlanRecordID = "current"

// Limits that bound the size of a TenantPlan record.
const (
	// MaxTenantPlanPriorities is the most priorities one plan may hold.
	MaxTenantPlanPriorities = 256

	// MaxTenantPlanMilestones is the most milestones one plan may hold.
	MaxTenantPlanMilestones = 64

	// MaxTenantPlanNoteLen is the longest priority note or milestone title,
	// in bytes.
	MaxTenantPlanNoteLen = 1024
)

// TenantPriority ranks one of the tenant's projects within its plan.
type TenantPriority struct {
	// ProjectID names the tenant's project.
	ProjectID string `json:"project_id"`

	// Rank orders the priority: 1 is the highest; unique within a plan.
	Rank int `json:"rank"`

	// Note is optional free text explaining the priority.
	Note string `json:"note,omitempty"`
}

// TenantMilestone is one dated goal of the tenant's plan, optionally tied to
// some of the tenant's projects.
type TenantMilestone struct {
	// ID identifies the milestone within the plan.
	ID string `json:"id"`

	// Title describes the milestone.
	Title string `json:"title"`

	// Due is when the milestone is due.
	Due time.Time `json:"due"`

	// ProjectIDs names the tenant's projects the milestone covers.
	ProjectIDs []string `json:"project_ids,omitempty"`

	// Done reports that the milestone is reached.
	Done bool `json:"done,omitempty"`
}

// TenantPlan is the tenant's ranked project priorities and milestones, set
// by the tenant's appointed Tenant Agent and signed with its go-xkms key.
//
// The record holds identifiers and text only, never a credential or key
// material.
type TenantPlan struct {
	// TenantID is the tenant the plan belongs to. The store sets it from the
	// key, so it can never name another tenant.
	TenantID string `json:"tenant_id"`

	// Priorities ranks the tenant's projects.
	Priorities []TenantPriority `json:"priorities"`

	// Milestones lists the tenant's milestones.
	Milestones []TenantMilestone `json:"milestones"`

	// Revision counts the plan's writes, starting at 1.
	Revision uint64 `json:"revision"`

	// Principal is the appointed agent's go-qrdb username at signing.
	Principal string `json:"principal"`

	// SigningKeyID is the go-xkms key that signed the plan.
	SigningKeyID string `json:"signing_key_id"`

	// Signature is the signature over SigningPayload.
	Signature []byte `json:"signature"`

	// CreatedAt is when the tenant's plan was first stored.
	CreatedAt time.Time `json:"created_at,omitempty"`

	// UpdatedAt is when the plan was last written.
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Validate checks the plan's content and reports the first problem as an
// *InputValidationError. Empty Priorities and Milestones are valid: a
// cleared plan is still a plan. Validate does not check the signature
// fields.
func (p TenantPlan) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return &InputValidationError{Field: "tenant_plan.tenant_id", Message: "required"}
	}
	if len(p.Priorities) > MaxTenantPlanPriorities {
		return &InputValidationError{Field: "priorities", Message: "at most " + strconv.Itoa(MaxTenantPlanPriorities) + " allowed"}
	}
	if len(p.Milestones) > MaxTenantPlanMilestones {
		return &InputValidationError{Field: "milestones", Message: "at most " + strconv.Itoa(MaxTenantPlanMilestones) + " allowed"}
	}
	projects := make(map[string]struct{}, len(p.Priorities))
	ranks := make(map[int]struct{}, len(p.Priorities))
	for i, priority := range p.Priorities {
		field := fmt.Sprintf("priorities[%d]", i)
		if strings.TrimSpace(priority.ProjectID) == "" {
			return &InputValidationError{Field: field + ".project_id", Message: "required"}
		}
		if priority.Rank < 1 {
			return &InputValidationError{Field: field + ".rank", Message: "must be at least 1"}
		}
		if _, dup := projects[priority.ProjectID]; dup {
			return &InputValidationError{Field: field + ".project_id", Message: "duplicate project " + strconv.Quote(priority.ProjectID)}
		}
		if _, dup := ranks[priority.Rank]; dup {
			return &InputValidationError{Field: field + ".rank", Message: "duplicate rank " + strconv.Itoa(priority.Rank)}
		}
		if len(priority.Note) > MaxTenantPlanNoteLen {
			return &InputValidationError{Field: field + ".note", Message: "at most " + strconv.Itoa(MaxTenantPlanNoteLen) + " bytes"}
		}
		projects[priority.ProjectID] = struct{}{}
		ranks[priority.Rank] = struct{}{}
	}
	milestones := make(map[string]struct{}, len(p.Milestones))
	for i, milestone := range p.Milestones {
		field := fmt.Sprintf("milestones[%d]", i)
		if strings.TrimSpace(milestone.ID) == "" {
			return &InputValidationError{Field: field + ".id", Message: "required"}
		}
		if _, dup := milestones[milestone.ID]; dup {
			return &InputValidationError{Field: field + ".id", Message: "duplicate milestone " + strconv.Quote(milestone.ID)}
		}
		if strings.TrimSpace(milestone.Title) == "" {
			return &InputValidationError{Field: field + ".title", Message: "required"}
		}
		if len(milestone.Title) > MaxTenantPlanNoteLen {
			return &InputValidationError{Field: field + ".title", Message: "at most " + strconv.Itoa(MaxTenantPlanNoteLen) + " bytes"}
		}
		named := make(map[string]struct{}, len(milestone.ProjectIDs))
		for j, projectID := range milestone.ProjectIDs {
			projectField := fmt.Sprintf("%s.project_ids[%d]", field, j)
			if strings.TrimSpace(projectID) == "" {
				return &InputValidationError{Field: projectField, Message: "required"}
			}
			if _, dup := named[projectID]; dup {
				return &InputValidationError{Field: projectField, Message: "duplicate project " + strconv.Quote(projectID)}
			}
			named[projectID] = struct{}{}
		}
		milestones[milestone.ID] = struct{}{}
	}
	return nil
}

// tenantPlanPayload is the signed content of a TenantPlan, in a fixed field
// order.
type tenantPlanPayload struct {
	TenantID     string            `json:"tenant_id"`
	Revision     uint64            `json:"revision"`
	Principal    string            `json:"principal"`
	SigningKeyID string            `json:"signing_key_id"`
	Priorities   []TenantPriority  `json:"priorities"`
	Milestones   []TenantMilestone `json:"milestones"`
}

// SigningPayload returns the bytes the appointed agent signs: the JSON of
// the plan's tenant, revision, principal, signing key ID, priorities and
// milestones, in that order. Signature, CreatedAt and UpdatedAt are
// excluded, so the store's timestamps never invalidate a signature, while a
// plan can't be replayed into another tenant or revision.
func (p TenantPlan) SigningPayload() ([]byte, error) {
	return json.Marshal(tenantPlanPayload{
		TenantID:     p.TenantID,
		Revision:     p.Revision,
		Principal:    p.Principal,
		SigningKeyID: p.SigningKeyID,
		Priorities:   p.Priorities,
		Milestones:   p.Milestones,
	})
}

var (
	// ErrTenantPlanUnknown reports that a tenant has no plan.
	ErrTenantPlanUnknown = errors.New("tenant plan: none set")

	// ErrTenantPlanConflict reports a plan write refused because the held
	// revision is not the one the writer expected.
	ErrTenantPlanConflict = errors.New("tenant plan: revision conflict")
)

// UnknownTenantPlanError reports a tenant with no plan.
type UnknownTenantPlanError struct {
	TenantID string
}

// Error implements the error interface.
func (e *UnknownTenantPlanError) Error() string {
	return ErrTenantPlanUnknown.Error() + ": tenant " + strconv.Quote(e.TenantID)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *UnknownTenantPlanError) Unwrap() error { return ErrTenantPlanUnknown }

// TenantPlanConflictError reports a plan write refused because the tenant's
// held revision differs from the expected one.
type TenantPlanConflictError struct {
	TenantID string
	Expected uint64
	Held     uint64
}

// Error implements the error interface.
func (e *TenantPlanConflictError) Error() string {
	return ErrTenantPlanConflict.Error() + ": tenant " + strconv.Quote(e.TenantID) +
		" expected revision " + strconv.FormatUint(e.Expected, 10) +
		", holds " + strconv.FormatUint(e.Held, 10)
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *TenantPlanConflictError) Unwrap() error { return ErrTenantPlanConflict }
