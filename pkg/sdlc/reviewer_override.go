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
	"errors"
	"strconv"
	"strings"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// The per-pull-request reviewer override — an operator's record, never a
// payload's
// =============================================================================
//
// AN OVERRIDE IS A RECORD THIS MODULE OWNS, written through an authorized API
// verb and read back from the store. That is the whole of its authority and
// the whole of its safety: the reasons review_config.go gives for never
// routing a review by label apply to choosing WHICH reviewer exactly as they
// apply to choosing WHETHER, so nothing here reads a label, a comment, a title
// or any field a webhook delivery carries.
//
// It is keyed by (tenant, project, pull request) and names one profile of the
// project's binding. Dispatch reads it on every review it starts and falls
// back to the binding's default when there is none. An override that names a
// profile the binding no longer carries is refused at dispatch rather than
// read as the default, so a removed reviewer surfaces as a sweep failure an
// operator can see and clear.

const (
	reviewerOverrideEntityType = "sdlc_reviewer_override"
	reviewerOverrideEntityName = "reviewer override"
)

// ReviewerOverride is an operator's choice of reviewer profile for one pull
// request.
type ReviewerOverride struct {
	// ID is the DAO entity id, derived from ProjectID and PullRequest.
	ID uint64 `json:"-"`

	ProjectID   string `json:"project_id"`
	PullRequest int    `json:"pull_request"`

	// Profile names one of the binding's reviewer profiles.
	Profile string `json:"profile"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EntityID implements the DAO entity contract.
func (o *ReviewerOverride) EntityID() uint64 { return o.ID }

// SetEntityID implements the DAO entity contract.
func (o *ReviewerOverride) SetEntityID(id uint64) { o.ID = id }

// Validate refuses an override that addresses nothing or names nothing.
func (o *ReviewerOverride) Validate() error {
	if o == nil {
		return &InvalidInputError{Field: "override", Message: "must not be nil"}
	}
	if err := validateKeySegment("project_id", o.ProjectID); err != nil {
		return err
	}
	if o.PullRequest <= 0 {
		return &InvalidInputError{Field: "pull_request", Message: "pull requests are numbered from one"}
	}
	if strings.TrimSpace(o.Profile) == "" {
		return &InvalidInputError{Field: "profile", Message: "required; clear the override to review with the default"}
	}
	return nil
}

// reviewerOverrideRef is what an override's entity id is derived from.
func reviewerOverrideRef(projectID string, pullRequest int) string {
	return projectID + "#" + strconv.Itoa(pullRequest)
}

// ReviewerOverrideReader reads the operator's per-pull-request reviewer
// choice. The Store satisfies it; so does the service that fronts the store.
type ReviewerOverrideReader interface {
	// GetReviewerOverride returns the override for one pull request, or an
	// error matching ErrNotFound when there is none.
	GetReviewerOverride(ctx context.Context, tenantID, projectID string, pullRequest int) (*ReviewerOverride, error)
}

// ReviewerSelection is which reviewer profile reviews one pull request, and
// whether an operator chose it.
type ReviewerSelection struct {
	ProjectID   string `json:"project_id"`
	PullRequest int    `json:"pull_request"`

	// Profile is the profile a review of this pull request runs as.
	Profile string `json:"profile"`

	// Overridden reports that an operator chose Profile for this pull
	// request; false is the binding's default.
	Overridden bool `json:"overridden"`

	// CountsTowardMerge reports whether Profile's verdict can land the
	// change. False is an advisory review.
	CountsTowardMerge bool `json:"counts_toward_merge"`

	// StatusContext is the commit status Profile publishes under.
	StatusContext string `json:"status_context"`

	// UpdatedAt is when the override was last written; zero with none.
	UpdatedAt time.Time `json:"updated_at,omitempty"`

	// Redispatched reports that setting the override started a review by the
	// chosen seat now. False when a review is already running — it is never
	// cancelled and finishes normally; the next dispatch is by the chosen
	// seat — or when the pull request is not reviewable right now, which the
	// next delivery or sweep asks again. Set only on the answer to setting an
	// override.
	Redispatched bool `json:"redispatched,omitempty"`
}

// SelectReviewer decides which profile reviews one pull request: the
// operator's override when one is recorded, the binding's default otherwise.
func SelectReviewer(
	ctx context.Context,
	overrides ReviewerOverrideReader,
	tenantID string,
	binding *ProjectBinding,
	pullRequest int,
) (ReviewerProfile, ReviewerSelection, error) {
	selection := ReviewerSelection{ProjectID: binding.ProjectID, PullRequest: pullRequest}
	override, err := overrides.GetReviewerOverride(ctx, tenantID, binding.ProjectID, pullRequest)
	switch {
	case errors.Is(err, ErrNotFound):
		override = nil
	case err != nil:
		return ReviewerProfile{}, selection, err
	}
	name := ""
	if override != nil {
		name = override.Profile
		selection.Overridden = true
		selection.UpdatedAt = override.UpdatedAt
	}
	profile, err := binding.ReviewerProfile(name)
	if err != nil {
		return ReviewerProfile{}, selection, err
	}
	selection.Profile = profile.Name
	selection.CountsTowardMerge = profile.Counts
	selection.StatusContext = profile.Context()
	return profile, selection, nil
}

// =============================================================================
// Store
// =============================================================================

func (s *Store) reviewerOverrideDAO(ctx context.Context, op, tenantID string) (qrdbsdk.GenericDAO[*ReviewerOverride], error) {
	return entityDAO(ctx, s, op, tenantID, reviewerOverrideEntityType, func() *ReviewerOverride { return &ReviewerOverride{} })
}

// GetReviewerOverride returns the override recorded for one pull request.
func (s *Store) GetReviewerOverride(ctx context.Context, tenantID, projectID string, pullRequest int) (*ReviewerOverride, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	ref := reviewerOverrideRef(projectID, pullRequest)
	dao, err := s.reviewerOverrideDAO(ctx, "GetReviewerOverride", tenantID)
	if err != nil {
		return nil, err
	}
	override, err := dao.Get(ctx, entityIDFor(ref))
	if err != nil {
		if qrdbsdk.IsDAONotFound(err) {
			return nil, &NotFoundError{Entity: reviewerOverrideEntityName, TenantID: tenantID, ID: ref}
		}
		return nil, backendError("GetReviewerOverride", err)
	}
	if override.ProjectID != projectID || override.PullRequest != pullRequest {
		// A digest collision: the slot holds another pull request's override.
		return nil, &NotFoundError{Entity: reviewerOverrideEntityName, TenantID: tenantID, ID: ref}
	}
	override.ID = entityIDFor(ref)
	return override, nil
}

// PutReviewerOverride creates or replaces the override for its pull request.
func (s *Store) PutReviewerOverride(ctx context.Context, tenantID string, override *ReviewerOverride) error {
	if err := override.Validate(); err != nil {
		return err
	}
	dao, err := s.reviewerOverrideDAO(ctx, "PutReviewerOverride", tenantID)
	if err != nil {
		return err
	}
	now := s.now()
	override.ID = entityIDFor(reviewerOverrideRef(override.ProjectID, override.PullRequest))
	override.UpdatedAt = now
	held, getErr := dao.Get(ctx, override.ID)
	if getErr == nil && held.ProjectID == override.ProjectID && held.PullRequest == override.PullRequest {
		override.CreatedAt = held.CreatedAt
	} else {
		override.CreatedAt = now
	}
	if err := dao.Save(ctx, override); err != nil {
		return backendError("PutReviewerOverride", err)
	}
	return nil
}

// DeleteReviewerOverride removes the override for one pull request. Deleting
// an absent override is not an error.
func (s *Store) DeleteReviewerOverride(ctx context.Context, tenantID, projectID string, pullRequest int) error {
	held, err := s.GetReviewerOverride(ctx, tenantID, projectID, pullRequest)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	dao, err := s.reviewerOverrideDAO(ctx, "DeleteReviewerOverride", tenantID)
	if err != nil {
		return err
	}
	if err := dao.Delete(ctx, held); err != nil {
		return backendError("DeleteReviewerOverride", err)
	}
	return nil
}

// DeleteReviewerOverrides removes every override recorded for one project, so
// an unbound project leaves no choice behind that a later binding with
// different profiles would inherit.
func (s *Store) DeleteReviewerOverrides(ctx context.Context, tenantID, projectID string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	dao, err := s.reviewerOverrideDAO(ctx, "DeleteReviewerOverrides", tenantID)
	if err != nil {
		return err
	}
	var doomed []*ReviewerOverride
	pageErr := dao.ForEachPage(ctx, qrdbsdk.PageQuery{PageSize: listPageSize}, func(page qrdbsdk.PageResult[*ReviewerOverride]) error {
		for _, override := range page.Entities {
			if override != nil && override.ProjectID == projectID {
				doomed = append(doomed, override)
			}
		}
		return nil
	})
	if pageErr != nil {
		return backendError("DeleteReviewerOverrides", pageErr)
	}
	for _, override := range doomed {
		override.ID = entityIDFor(reviewerOverrideRef(override.ProjectID, override.PullRequest))
		if err := dao.Delete(ctx, override); err != nil {
			return backendError("DeleteReviewerOverrides", err)
		}
	}
	return nil
}
