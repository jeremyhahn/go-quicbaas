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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

const statusTenant = "tenant-1"

// statusJobsFake answers ListJobs with its jobs and records the filter.
type statusJobsFake struct {
	jobs   []*orchestration.JobStatus
	err    error
	tenant string
	filter *orchestration.JobFilter
}

func (f *statusJobsFake) ListJobs(_ context.Context, tenantID string, filter *orchestration.JobFilter) ([]*orchestration.JobStatus, error) {
	f.tenant, f.filter = tenantID, filter
	if f.err != nil {
		return nil, f.err
	}
	return f.jobs, nil
}

// statusTrainsFake answers GetTrainLedger with its ledger, an empty one when
// unset, as the store reads back an absent ledger.
type statusTrainsFake struct {
	ledger *TrainLedger
	err    error
}

func (f *statusTrainsFake) GetTrainLedger(_ context.Context, _, projectID string) (*TrainLedger, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.ledger == nil {
		return &TrainLedger{ProjectID: projectID}, nil
	}
	return f.ledger, nil
}

func statusBinding() *ProjectBinding {
	binding := testBinding()
	binding.MergeTrain.MaxSize = 4
	return binding
}

func statusSources(forge *fakeForge) (ProjectStatusSources, *statusJobsFake, *statusTrainsFake) {
	jobs := &statusJobsFake{}
	trains := &statusTrainsFake{}
	return ProjectStatusSources{Forge: forge, Jobs: jobs, Trains: trains}, jobs, trains
}

func statusNow() time.Time { return time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC) }

func mainChecks(forge *fakeForge, conclusion string, now time.Time) {
	forge.checks[fakeForgeHeadSHA] = []ForgeCheckRun{{ID: "ci", Name: "ci", Status: "completed", Conclusion: conclusion, Completed: now}}
}

func TestReadProjectStatus_CountsLiveLanesOnly(t *testing.T) {
	now := statusNow()
	src, jobs, _ := statusSources(newFakeForge())
	jobs.jobs = []*orchestration.JobStatus{
		{JobID: "j1", State: orchestration.JobStateExecuting},
		{JobID: "j2", State: orchestration.JobStateCompleted},
		{JobID: "j3", State: orchestration.JobStateFailed},
	}

	status, err := ReadProjectStatus(context.Background(), src, statusTenant, statusBinding(), now)
	require.NoError(t, err)

	assert.Equal(t, 1, status.Lanes, "only the job not in a terminal state is a lane")
	assert.Equal(t, statusTenant, jobs.tenant)
	require.NotNil(t, jobs.filter)
	assert.Equal(t, testProject, jobs.filter.ProjectID)
	assert.Equal(t, "sdlc", jobs.filter.Runner)
	assert.Equal(t, testProject, status.ProjectID)
	assert.Equal(t, now, status.ReadAt)
}

func TestReadProjectStatus_TrainInFlight(t *testing.T) {
	src, _, trains := statusSources(newFakeForge())
	trains.ledger = &TrainLedger{ProjectID: testProject, Active: &MergeTrain{
		Phase: TrainTesting,
		Cars:  []TrainCar{{PullRequest: 11}, {PullRequest: 12}},
	}}

	status, err := ReadProjectStatus(context.Background(), src, statusTenant, statusBinding(), statusNow())
	require.NoError(t, err)

	assert.Equal(t, 2, status.TrainCars)
	assert.Equal(t, 4, status.TrainSize)
	assert.Equal(t, TrainTesting, status.TrainPhase)
}

func TestReadProjectStatus_NoTrain(t *testing.T) {
	src, _, _ := statusSources(newFakeForge())

	status, err := ReadProjectStatus(context.Background(), src, statusTenant, statusBinding(), statusNow())
	require.NoError(t, err)

	assert.Equal(t, 0, status.TrainCars)
	assert.Equal(t, TrainPhase(""), status.TrainPhase)
	assert.Equal(t, 4, status.TrainSize, "the capacity is the binding's whether or not a train runs")
}

func TestReadProjectStatus_PullRequestCounts(t *testing.T) {
	now := statusNow()
	forge := newFakeForge()
	pr := func(number int, head string) *ForgePullRequest {
		return &ForgePullRequest{Number: number, State: "open", Author: fakeForgeLogin, HeadSHA: head, Created: now}
	}
	done := func(conclusion string) []ForgeCheckRun {
		return []ForgeCheckRun{{ID: "ci", Name: "ci", Status: "completed", Conclusion: conclusion, Completed: now}}
	}
	forge.prs[1] = pr(1, "sha-red")
	forge.prs[2] = pr(2, "sha-awaiting")
	forge.prs[3] = pr(3, "sha-changes")
	forge.prs[4] = &ForgePullRequest{Number: 4, State: "open", Author: "a-person", HeadSHA: "sha-red"}
	forge.checks["sha-red"] = done("failure")
	forge.checks["sha-awaiting"] = done("success")
	forge.checks["sha-changes"] = done("success")
	forge.reviews[1] = []ForgeReview{{ID: 1, Reviewer: "codex", State: "APPROVED", HeadSHA: "sha-red", Submitted: now}}
	forge.reviews[3] = []ForgeReview{{ID: 2, Reviewer: "codex", State: "REQUEST_CHANGES", HeadSHA: "sha-changes", Submitted: now}}
	src, _, _ := statusSources(forge)

	status, err := ReadProjectStatus(context.Background(), src, statusTenant, statusBinding(), now)
	require.NoError(t, err)

	assert.Equal(t, 3, status.OpenPullRequests, "a person's pull request is not factory work")
	assert.Equal(t, 1, status.RedPullRequests)
	assert.Equal(t, 1, status.AwaitingReview)
	assert.Equal(t, 1, status.ChangesRequested)
}

func TestReadProjectStatus_RedMain(t *testing.T) {
	now := statusNow()
	forge := newFakeForge()
	mainChecks(forge, "failure", now)
	src, _, _ := statusSources(forge)

	status, err := ReadProjectStatus(context.Background(), src, statusTenant, statusBinding(), now)
	require.NoError(t, err)

	assert.Equal(t, CIFailure, status.Main)
	assert.Equal(t, fakeForgeHeadSHA, status.MainSHA)
	assert.Equal(t, statusBinding().Onboarding.Branch(), status.MainBranch)
}

func TestReadProjectStatus_GreenMain(t *testing.T) {
	now := statusNow()
	forge := newFakeForge()
	mainChecks(forge, "success", now)
	src, _, _ := statusSources(forge)

	status, err := ReadProjectStatus(context.Background(), src, statusTenant, statusBinding(), now)
	require.NoError(t, err)

	assert.Equal(t, CISuccess, status.Main)
}

func TestReadProjectStatus_ReadErrorFailsWhole(t *testing.T) {
	boom := errors.New("read failed")
	cases := []struct {
		name string
		fail func(forge *fakeForge, jobs *statusJobsFake, trains *statusTrainsFake)
	}{
		{"jobs", func(_ *fakeForge, jobs *statusJobsFake, _ *statusTrainsFake) { jobs.err = boom }},
		{"ledger", func(_ *fakeForge, _ *statusJobsFake, trains *statusTrainsFake) { trains.err = boom }},
		{"open pull requests", func(forge *fakeForge, _ *statusJobsFake, _ *statusTrainsFake) { forge.listOpenPRsErr = boom }},
		{"branch", func(forge *fakeForge, _ *statusJobsFake, _ *statusTrainsFake) { forge.getBranchErr = boom }},
		{"check runs", func(forge *fakeForge, _ *statusJobsFake, _ *statusTrainsFake) { forge.listChecksErr = boom }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forge := newFakeForge()
			src, jobs, trains := statusSources(forge)
			tc.fail(forge, jobs, trains)

			status, err := ReadProjectStatus(context.Background(), src, statusTenant, statusBinding(), statusNow())
			require.ErrorIs(t, err, boom)
			assert.Nil(t, status)
		})
	}
}

func TestReadProjectStatus_RefusesMissingSources(t *testing.T) {
	full, _, _ := statusSources(newFakeForge())
	cases := []struct {
		name    string
		src     ProjectStatusSources
		tenant  string
		binding *ProjectBinding
	}{
		{"forge", ProjectStatusSources{Jobs: full.Jobs, Trains: full.Trains}, statusTenant, statusBinding()},
		{"jobs", ProjectStatusSources{Forge: full.Forge, Trains: full.Trains}, statusTenant, statusBinding()},
		{"trains", ProjectStatusSources{Forge: full.Forge, Jobs: full.Jobs}, statusTenant, statusBinding()},
		{"binding", full, statusTenant, nil},
		{"tenant", full, " ", statusBinding()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, err := ReadProjectStatus(context.Background(), tc.src, tc.tenant, tc.binding, statusNow())
			var invalid *InvalidInputError
			require.ErrorAs(t, err, &invalid)
			assert.Nil(t, status)
		})
	}
}

func TestProjectStatus_Line(t *testing.T) {
	cases := []struct {
		name   string
		status ProjectStatus
		want   string
	}{
		{
			"no train",
			ProjectStatus{ProjectID: "proj-a", Lanes: 2, TrainSize: 4, OpenPullRequests: 3, RedPullRequests: 1,
				AwaitingReview: 1, ChangesRequested: 0, Main: CISuccess},
			"proj-a lanes=2 train=0/4 prs=3 red=1 awaiting=1 changes=0 main=success",
		},
		{
			"train in flight",
			ProjectStatus{ProjectID: "proj-b", Lanes: 1, TrainCars: 2, TrainSize: 4, TrainPhase: TrainTesting,
				OpenPullRequests: 5, RedPullRequests: 0, AwaitingReview: 2, ChangesRequested: 1, Main: CIFailure},
			"proj-b lanes=1 train=2/4 testing prs=5 red=0 awaiting=2 changes=1 main=failure",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.status.Line())
		})
	}
}

func TestRollupStatuses(t *testing.T) {
	rollup := RollupStatuses([]ProjectStatus{
		{ProjectID: "proj-c", Lanes: 1, OpenPullRequests: 2, RedPullRequests: 1, Main: CIFailure},
		{ProjectID: "proj-a", Lanes: 2, OpenPullRequests: 3, RedPullRequests: 0, Main: CISuccess},
		{ProjectID: "proj-b", Lanes: 0, OpenPullRequests: 1, RedPullRequests: 1, Main: CIFailure},
	})

	assert.Equal(t, DepartmentRollup{
		Projects:         3,
		Lanes:            3,
		OpenPullRequests: 6,
		RedPullRequests:  2,
		RedMains:         []string{"proj-b", "proj-c"},
	}, rollup)
}

func TestDepartmentRollup_Line(t *testing.T) {
	cases := []struct {
		name   string
		rollup DepartmentRollup
		want   string
	}{
		{
			"all mains green",
			DepartmentRollup{Projects: 2, Lanes: 3, OpenPullRequests: 4, RedPullRequests: 0, RedMains: []string{}},
			"projects=2 lanes=3 prs=4 red=0 red_mains=0",
		},
		{
			"red mains",
			DepartmentRollup{Projects: 3, Lanes: 3, OpenPullRequests: 6, RedPullRequests: 2, RedMains: []string{"proj-b", "proj-c"}},
			"projects=3 lanes=3 prs=6 red=2 red_mains=2 (proj-b, proj-c)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.rollup.Line())
		})
	}
}
