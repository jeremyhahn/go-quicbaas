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
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// A project's STATUS is the factory's one-line view of it: how many lanes
// (live sdlc jobs) it runs, the merge train in flight, its open pull requests
// and how many of them are red or waiting on a review, and whether its
// default branch is green. The DEPARTMENT ROLLUP sums the statuses of the
// projects a department owns.

// ProjectStatus is one project's status, read from the forge, the job store
// and the merge train ledger.
type ProjectStatus struct {
	ProjectID string `json:"project_id"`

	// Repo is the bound repository as "owner/repo".
	Repo string `json:"repo"`

	// Lanes is the number of the project's sdlc jobs not in a terminal state.
	Lanes int `json:"lanes"`

	// TrainCars is the number of cars on the train in flight, zero between
	// trains; TrainSize is the train's capacity from the binding's policy.
	TrainCars int `json:"train_cars"`
	TrainSize int `json:"train_size"`

	// TrainPhase is the phase of the train in flight, empty between trains.
	TrainPhase TrainPhase `json:"train_phase,omitempty"`

	// OpenPullRequests counts the agent's open pull requests; the others
	// count those whose head is red, awaiting a review, or carrying a
	// request for changes.
	OpenPullRequests int `json:"open_pull_requests"`
	RedPullRequests  int `json:"red_pull_requests"`
	AwaitingReview   int `json:"awaiting_review"`
	ChangesRequested int `json:"changes_requested"`

	// MainBranch is the default branch, MainSHA its tip, and Main the tip's
	// pipeline state.
	MainBranch string  `json:"main_branch"`
	MainSHA    string  `json:"main_sha"`
	Main       CIState `json:"main"`

	ReadAt time.Time `json:"read_at"`
}

// StatusJobs is the job listing a status read counts lanes from.
type StatusJobs interface {
	ListJobs(ctx context.Context, tenantID string, filter *orchestration.JobFilter) ([]*orchestration.JobStatus, error)
}

// StatusTrains is the train ledger a status read takes the train from.
type StatusTrains interface {
	GetTrainLedger(ctx context.Context, tenantID, projectID string) (*TrainLedger, error)
}

// ProjectStatusSources are the reads a project status is built from.
type ProjectStatusSources struct {
	Forge  ForgeBackend
	Jobs   StatusJobs
	Trains StatusTrains
}

// ReadProjectStatus reads one project's status. Like ReadOpenWork it is
// whole or nothing: any failed read fails the status, so a count is never
// reported from a partial read.
func ReadProjectStatus(ctx context.Context, src ProjectStatusSources, tenantID string, binding *ProjectBinding, now time.Time) (*ProjectStatus, error) {
	switch {
	case src.Forge == nil:
		return nil, &InvalidInputError{Field: "forge", Message: "a status read needs the project's forge"}
	case src.Jobs == nil:
		return nil, &InvalidInputError{Field: "jobs", Message: "a status read needs the job store"}
	case src.Trains == nil:
		return nil, &InvalidInputError{Field: "trains", Message: "a status read needs the train store"}
	case binding == nil:
		return nil, &InvalidInputError{Field: "binding", Message: "a status read needs the project's binding"}
	case strings.TrimSpace(tenantID) == "":
		return nil, &InvalidInputError{Field: "tenant_id", Message: "a status read needs the tenant"}
	}

	status := &ProjectStatus{
		ProjectID:  binding.ProjectID,
		Repo:       binding.Forge.RepoRef().String(),
		TrainSize:  binding.MergeTrain.Size(),
		MainBranch: binding.Onboarding.Branch(),
		ReadAt:     now.UTC(),
	}

	jobs, err := src.Jobs.ListJobs(ctx, tenantID, &orchestration.JobFilter{ProjectID: binding.ProjectID, Runner: JobRunnerName})
	if err != nil {
		return nil, err
	}
	for _, job := range jobs {
		if job != nil && !orchestration.IsTerminalJobState(job.State) {
			status.Lanes++
		}
	}

	ledger, err := src.Trains.GetTrainLedger(ctx, tenantID, binding.ProjectID)
	if err != nil {
		return nil, err
	}
	if ledger != nil && ledger.Active != nil {
		status.TrainCars = len(ledger.Active.Cars)
		status.TrainPhase = ledger.Active.Phase
	}

	work, err := ReadOpenWork(ctx, src.Forge, binding, now)
	if err != nil {
		return nil, err
	}
	status.OpenPullRequests = len(work.PullRequests)
	for _, pr := range work.PullRequests {
		if pr.CIState == CIFailure {
			status.RedPullRequests++
		}
		switch pr.ReviewState {
		case ReviewAwaiting:
			status.AwaitingReview++
		case ReviewChangesRequested:
			status.ChangesRequested++
		}
	}

	repo := binding.Forge.RepoRef()
	branch, err := src.Forge.GetBranch(ctx, repo, status.MainBranch)
	if err != nil {
		return nil, err
	}
	runs, err := src.Forge.ListCheckRuns(ctx, repo, branch.SHA)
	if err != nil {
		return nil, err
	}
	status.MainSHA = branch.SHA
	status.Main = ciStateOf(runs, branch.SHA, now)
	return status, nil
}

// Line renders the status as one deterministic line:
//
//	<project> lanes=<n> train=<cars>/<size>[ <phase>] prs=<n> red=<r> awaiting=<a> changes=<c> main=<state>
//
// The phase is present only while a train is in flight.
func (s ProjectStatus) Line() string {
	train := fmt.Sprintf("%d/%d", s.TrainCars, s.TrainSize)
	if s.TrainPhase != "" {
		train += " " + string(s.TrainPhase)
	}
	return fmt.Sprintf("%s lanes=%d train=%s prs=%d red=%d awaiting=%d changes=%d main=%s",
		s.ProjectID, s.Lanes, train, s.OpenPullRequests, s.RedPullRequests,
		s.AwaitingReview, s.ChangesRequested, s.Main)
}

// DepartmentRollup sums the statuses of a department's projects.
type DepartmentRollup struct {
	Projects         int `json:"projects"`
	Lanes            int `json:"lanes"`
	OpenPullRequests int `json:"open_pull_requests"`
	RedPullRequests  int `json:"red_pull_requests"`

	// RedMains are the projects whose default branch is red, sorted.
	RedMains []string `json:"red_mains"`
}

// RollupStatuses sums project statuses into a department rollup.
func RollupStatuses(statuses []ProjectStatus) DepartmentRollup {
	rollup := DepartmentRollup{Projects: len(statuses), RedMains: []string{}}
	for _, s := range statuses {
		rollup.Lanes += s.Lanes
		rollup.OpenPullRequests += s.OpenPullRequests
		rollup.RedPullRequests += s.RedPullRequests
		if s.Main == CIFailure {
			rollup.RedMains = append(rollup.RedMains, s.ProjectID)
		}
	}
	sort.Strings(rollup.RedMains)
	return rollup
}

// Line renders the rollup as one deterministic line:
//
//	projects=<n> lanes=<n> prs=<n> red=<r> red_mains=<k>[ (<id>, <id>)]
//
// The project list is present only when a main is red.
func (r DepartmentRollup) Line() string {
	line := fmt.Sprintf("projects=%d lanes=%d prs=%d red=%d red_mains=%d",
		r.Projects, r.Lanes, r.OpenPullRequests, r.RedPullRequests, len(r.RedMains))
	if len(r.RedMains) > 0 {
		line += " (" + strings.Join(r.RedMains, ", ") + ")"
	}
	return line
}
