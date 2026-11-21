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
	"slices"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// TestAttentionService_Resolve_AnswerHandsTheIssueBackToTheFactory is G1: a
// ruling that readmits takes every park label off the issue — the binding's
// needs-info and the portfolio hold alike — and puts the trigger on, so the
// issue reads as queued work rather than as waiting on a person.
func TestAttentionService_Resolve_AnswerHandsTheIssueBackToTheFactory(t *testing.T) {
	task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)
	f.forge.putIssue(77, "the parked issue", "factory:needs-info", HoldLabel, "area:crypto")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID,
		AttentionResolution{Action: AttentionAnswer, Answer: "Use P-256."}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	labels := f.forge.issueLabels(77)
	for _, gone := range []string{"factory:needs-info", HoldLabel} {
		if slices.Contains(labels, gone) {
			t.Errorf("label %q is still on the issue: %v", gone, labels)
		}
	}
	if !slices.Contains(labels, "task:ready") {
		t.Errorf("the trigger label was not applied: %v", labels)
	}
	if !slices.Contains(labels, "area:crypto") {
		t.Errorf("a label the module does not own was removed: %v", labels)
	}
	if task.Status != orchestration.TaskStatusPending {
		t.Errorf("task status = %q, want pending", task.Status)
	}
}

// TestAttentionService_Resolve_AHandBackThatFailsReadmitsNothing proves the
// labels move before the task does: a forge that refuses the label write
// leaves the task parked, never running under an issue that says it waits.
func TestAttentionService_Resolve_AHandBackThatFailsReadmitsNothing(t *testing.T) {
	task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)
	f.forge.putIssue(77, "the parked issue", "factory:needs-info")
	f.forge.addLabelErr = errors.New("forge refused the label")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID,
		AttentionResolution{Action: AttentionReadmit}); err == nil {
		t.Fatal("Resolve: want the label failure, got none")
	}
	if calls := f.tasks.callLog(); len(calls) != 0 {
		t.Errorf("call log = %v, want no readmit after a failed hand-back", calls)
	}
	if task.Status != orchestration.TaskStatusBlocked {
		t.Errorf("task status = %q, want it still blocked", task.Status)
	}
}

// TestAttentionService_Resolve_CloseAsDoneLeavesTheLabels proves a closing
// ruling does not dress the issue as queued work.
func TestAttentionService_Resolve_CloseAsDoneLeavesTheLabels(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindEscalated)
	f := newAttentionFixture(t, task)
	f.forge.putIssue(77, "the parked issue", "factory:needs-info")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID,
		AttentionResolution{Action: AttentionCloseAsDone}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if labels := f.forge.issueLabels(77); slices.Contains(labels, "task:ready") {
		t.Errorf("a closed issue was given the trigger label: %v", labels)
	}
}

// TestAttentionService_Resolve_NotePostsAndKeepsTheTaskParked is G2: the note
// reaches the issue under the factory marker and nothing moves — no readmit,
// no close, no label.
func TestAttentionService_Resolve_NotePostsAndKeepsTheTaskParked(t *testing.T) {
	task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)
	f.forge.putIssue(77, "the parked issue", "factory:needs-info")

	if _, err := f.service.Resolve(context.Background(), testTenant, task.ID,
		AttentionResolution{Action: AttentionNote, Answer: "Hold until the release train is cut."}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	comments := f.forge.commentsOn(77)
	if len(comments) != 1 {
		t.Fatalf("comments on issue 77 = %d, want 1", len(comments))
	}
	body := comments[0].Body
	if !strings.Contains(body, factoryCommentMarker) || !strings.Contains(body, "Hold until the release train is cut.") {
		t.Errorf("note body = %q, want the factory marker and the note text", body)
	}
	if !strings.Contains(body, "stays parked") {
		t.Errorf("note body = %q, want it to say the task stays parked", body)
	}
	if calls := f.tasks.callLog(); len(calls) != 0 {
		t.Errorf("call log = %v, want the task left alone", calls)
	}
	if task.Status != orchestration.TaskStatusBlocked {
		t.Errorf("task status = %q, want blocked", task.Status)
	}
	if labels := f.forge.issueLabels(77); !slices.Contains(labels, "factory:needs-info") || slices.Contains(labels, "task:ready") {
		t.Errorf("a note moved the labels: %v", labels)
	}
}

// TestAttentionService_Resolve_NoteRefusesEmptyText is the bad-input side: a
// note with nothing in it posts nothing.
func TestAttentionService_Resolve_NoteRefusesEmptyText(t *testing.T) {
	task := blockedFixtureTask(fixtureP256ProtoOwnerRuling, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)

	_, err := f.service.Resolve(context.Background(), testTenant, task.ID, AttentionResolution{Action: AttentionNote, Answer: "  "})
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) || invalid.Field != "answer" {
		t.Fatalf("Resolve(note, empty) = %v, want an InvalidInputError on answer", err)
	}
	if comments := f.forge.commentsOn(77); len(comments) != 0 {
		t.Errorf("an empty note posted %d comment(s)", len(comments))
	}
}

// TestAttentionService_List_OffersNoteOnEveryParkedTask pins the action list.
func TestAttentionService_List_OffersNoteOnEveryParkedTask(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindBlocked)
	f := newAttentionFixture(t, task)

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{Kind: AttentionParkedTask}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 || !slices.Contains(page.Items[0].Actions, AttentionNote) {
		t.Fatalf("parked task actions = %+v, want note among them", page.Items)
	}
}

// TestAttentionService_List_ReportsAnIdleProject is X4's throughput alert:
// an autonomous, unparked project with parked work and nothing pending or in
// progress is named as idle.
func TestAttentionService_List_ReportsAnIdleProject(t *testing.T) {
	task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindNeedsInfo)
	f := newAttentionFixture(t, task)
	f.projects.projects[testProject].Autonomous = true

	page, err := f.service.List(context.Background(), testTenant, AttentionFilter{Kind: AttentionProjectIdle}, &orchestration.Page{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("idle items = %d, want 1: %+v", len(page.Items), page.Items)
	}
	item := page.Items[0]
	if item.ID != "project_idle:"+testProject || item.ProjectName != "api" || item.Severity != "warning" {
		t.Errorf("idle item = %+v", item)
	}
	if len(item.Actions) != 0 {
		t.Errorf("an alert offers no resolution, got %v", item.Actions)
	}
}

// TestAttentionService_List_AProjectWithWorkOrClosedIntakeIsNotIdle covers the
// cases the alert must stay quiet for.
func TestAttentionService_List_AProjectWithWorkOrClosedIntakeIsNotIdle(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *attentionFixture)
	}{
		{"pending work", func(f *attentionFixture) {
			f.tasks.tasks["queued"] = &orchestration.Task{ID: "queued", TenantID: testTenant, ProjectID: testProject, Status: orchestration.TaskStatusPending}
		}},
		{"work in progress", func(f *attentionFixture) {
			f.tasks.tasks["running"] = &orchestration.Task{ID: "running", TenantID: testTenant, ProjectID: testProject, Status: orchestration.TaskStatusInProgress}
		}},
		{"parked project", func(f *attentionFixture) { f.projects.projects[testProject].Parked = true }},
		{"not autonomous", func(f *attentionFixture) { f.projects.projects[testProject].Autonomous = false }},
		{"project unreadable", func(f *attentionFixture) { delete(f.projects.projects, testProject) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := blockedFixtureTask(fixtureOwnerRulingIssueBody, orchestration.TaskBlockKindNeedsInfo)
			f := newAttentionFixture(t, task)
			f.projects.projects[testProject].Autonomous = true
			tc.setup(f)

			page, err := f.service.List(context.Background(), testTenant, AttentionFilter{Kind: AttentionProjectIdle}, &orchestration.Page{})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			if len(page.Items) != 0 {
				t.Fatalf("idle items = %+v, want none", page.Items)
			}
		})
	}
}

// TestAttentionService_Resolve_NoteReachesALiveTask pins the note on a task
// that is not parked. An operator's scope change for queued or running work
// (go-blockstore#74, 2026-09-28: the go-enclave target moved to v0.6.1-alpha
// while the task waited on the project's quota) has to reach the issue the
// worker re-reads at dispatch, without moving the task or its labels.
func TestAttentionService_Resolve_NoteReachesALiveTask(t *testing.T) {
	for _, status := range []orchestration.TaskStatus{
		orchestration.TaskStatusPending,
		orchestration.TaskStatusClaimed,
		orchestration.TaskStatusInProgress,
	} {
		t.Run(string(status), func(t *testing.T) {
			task := testTaskRecord(77)
			task.Status = status
			f := newAttentionFixture(t, task)
			f.forge.putIssue(77, "the queued issue", "task:ready")

			if _, err := f.service.Resolve(context.Background(), testTenant, task.ID,
				AttentionResolution{Action: AttentionNote, Answer: "Target v0.6.1-alpha, not v0.6.0-alpha."}); err != nil {
				t.Fatalf("Resolve(note) on a %s task: %v", status, err)
			}

			comments := f.forge.commentsOn(77)
			if len(comments) != 1 {
				t.Fatalf("comments on issue 77 = %d, want 1", len(comments))
			}
			body := comments[0].Body
			if !strings.Contains(body, factoryCommentMarker) || !strings.Contains(body, "Target v0.6.1-alpha, not v0.6.0-alpha.") {
				t.Errorf("note body = %q, want the factory marker and the note text", body)
			}
			if strings.Contains(body, "stays parked") {
				t.Errorf("note body = %q, a %s task is not parked", body, status)
			}
			if calls := f.tasks.callLog(); len(calls) != 0 {
				t.Errorf("call log = %v, want the task left alone", calls)
			}
			if task.Status != status {
				t.Errorf("task status = %q, want %q", task.Status, status)
			}
			if labels := f.forge.issueLabels(77); !slices.Contains(labels, "task:ready") || len(labels) != 1 {
				t.Errorf("a note moved the labels: %v", labels)
			}
		})
	}
}

// TestAttentionService_Resolve_OnlyNoteReachesALiveTask is the bad-input
// side: a finished task takes no note, and every action that moves a task
// still refuses one that is not parked. Nothing is posted either way.
func TestAttentionService_Resolve_OnlyNoteReachesALiveTask(t *testing.T) {
	cases := []struct {
		name   string
		status orchestration.TaskStatus
		action AttentionAction
	}{
		{"note on a completed task", orchestration.TaskStatusCompleted, AttentionNote},
		{"note on a failed task", orchestration.TaskStatusFailed, AttentionNote},
		{"answer on a running task", orchestration.TaskStatusInProgress, AttentionAnswer},
		{"close on a pending task", orchestration.TaskStatusPending, AttentionCloseAsDone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := testTaskRecord(77)
			task.Status = tc.status
			f := newAttentionFixture(t, task)
			f.forge.putIssue(77, "the issue", "task:ready")

			_, err := f.service.Resolve(context.Background(), testTenant, task.ID,
				AttentionResolution{Action: tc.action, Answer: "some ruling"})
			var invalidState *orchestration.TaskInvalidStateError
			if !errors.As(err, &invalidState) {
				t.Fatalf("Resolve = %v, want a TaskInvalidStateError", err)
			}
			if comments := f.forge.commentsOn(77); len(comments) != 0 {
				t.Errorf("a refused resolution posted %d comment(s)", len(comments))
			}
			if calls := f.tasks.callLog(); len(calls) != 0 {
				t.Errorf("call log = %v, want the task left alone", calls)
			}
		})
	}
}
