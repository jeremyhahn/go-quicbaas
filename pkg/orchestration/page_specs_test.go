// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"slices"
	"strings"
	"testing"
	"time"
)

// TestPageSpecs_DefaultSortIsAllowListed proves every endpoint's natural
// order is a field it will also sort on by request.
//
// A DEFAULT OUTSIDE THE ALLOW-LIST IS AN ENDPOINT THAT SILENTLY STOPS
// SORTING: ApplyPage would find no comparator for it and answer in store
// order, which is the exact failure the allow-list exists to make impossible.
func TestPageSpecs_DefaultSortIsAllowListed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		defaultSort string
		fields      []string
	}{
		{name: "jobs", defaultSort: jobPageSpec.DefaultSort, fields: JobPageSpec().SortFields()},
		{name: "tasks", defaultSort: taskPageSpec.DefaultSort, fields: TaskPageSpec().SortFields()},
		{name: "sessions", defaultSort: sessionPageSpec.DefaultSort, fields: SessionPageSpec().SortFields()},
		{name: "backends", defaultSort: backendPageSpec.DefaultSort, fields: BackendPageSpec().SortFields()},
		{name: "runners", defaultSort: runnerPageSpec.DefaultSort, fields: RunnerPageSpec().SortFields()},
		{name: "artifacts", defaultSort: artifactPageSpec.DefaultSort, fields: ArtifactPageSpec().SortFields()},
		{name: "agents", defaultSort: fleetNodePageSpec.DefaultSort, fields: FleetNodePageSpec().SortFields()},
		{name: "workflows", defaultSort: workflowPageSpec.DefaultSort, fields: WorkflowPageSpec().SortFields()},
		{name: "projects", defaultSort: projectPageSpec.DefaultSort, fields: ProjectPageSpec().SortFields()},
		{name: "accounts", defaultSort: accountPageSpec.DefaultSort, fields: AccountPageSpec().SortFields()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if tc.defaultSort == "" {
				t.Fatal("endpoint has no natural order")
			}
			if !slices.Contains(tc.fields, tc.defaultSort) {
				t.Errorf("default sort %q is not in the allow-list %v", tc.defaultSort, tc.fields)
			}
		})
	}
}

// TestJobPageSpec_OrdersAndSearchesTheDocumentedFields covers the jobs
// listing's contract: newest first by default, and `q` reaching job id,
// description, runner, state and task id.
func TestJobPageSpec_OrdersAndSearchesTheDocumentedFields(t *testing.T) {
	t.Parallel()

	jobs := []*JobStatus{
		{JobID: "job-old", State: JobStateCompleted, StartedAt: 100, Description: "trunk red", Runner: "sdlc", TaskID: "task-1"},
		{JobID: "job-new", State: JobStateFailed, StartedAt: 300, Description: "triage the queue", Runner: "triage", TaskID: "task-2"},
		{JobID: "job-mid", State: JobStateExecuting, StartedAt: 200, Description: "review the PR", Runner: "review", TaskID: "task-3"},
	}

	page, err := ApplyPage(jobs, nil, jobPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if page.Items[0].JobID != "job-new" {
		t.Errorf("first job = %q, want the newest (job-new)", page.Items[0].JobID)
	}

	searches := map[string]string{
		"job-mid":  "job id",
		"TRIAGE":   "runner, case-insensitively",
		"failed":   "state",
		"the PR":   "description",
		"task-1":   "task id",
		"sdlc":     "runner",
		"no-match": "",
	}
	for query, what := range searches {
		result, searchErr := ApplyPage(jobs, &Page{Query: query}, jobPageSpec)
		if searchErr != nil {
			t.Fatalf("ApplyPage(q=%q) error = %v", query, searchErr)
		}
		if what == "" {
			if result.Total != 0 {
				t.Errorf("q=%q matched %d jobs, want none", query, result.Total)
			}
			continue
		}
		if result.Total != 1 {
			t.Errorf("q=%q matched %d jobs, want 1 by %s", query, result.Total, what)
		}
	}
}

// TestJobPageSpec_ReadsTheRunnerAndTaskOffTheConfig proves a job record that
// carries its runner and task only on the launch config still sorts and
// searches by them — the shape a record persisted before those fields were
// promoted onto JobStatus has.
func TestJobPageSpec_ReadsTheRunnerAndTaskOffTheConfig(t *testing.T) {
	t.Parallel()

	jobs := []*JobStatus{
		{JobID: "job-1", Config: &JobConfig{Runner: "sdlc", TaskID: "task-9"}},
		{JobID: "job-2", Runner: "triage", TaskID: "task-8"},
	}

	result, err := ApplyPage(jobs, &Page{Query: "task-9"}, jobPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if result.Total != 1 || result.Items[0].JobID != "job-1" {
		t.Errorf("q=task-9 matched %d jobs, want only job-1", result.Total)
	}

	ordered, err := ApplyPage(jobs, &Page{Sort: "runner"}, jobPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if ordered.Items[0].JobID != "job-1" {
		t.Errorf("first job = %q, want job-1 (sdlc sorts before triage)", ordered.Items[0].JobID)
	}
}

// TestTaskPageSpec_OrdersAndSearchesTheDocumentedFields covers the tasks
// listing's contract.
func TestTaskPageSpec_OrdersAndSearchesTheDocumentedFields(t *testing.T) {
	t.Parallel()

	tasks := []*Task{
		{ID: "task-old", Subject: "wire the gate", Status: TaskStatusCompleted, ProjectID: "proj-a", CreatedAt: 100},
		{ID: "task-new", Subject: "page the listings", Status: TaskStatusPending, ProjectID: "proj-b", CreatedAt: 300},
	}

	page, err := ApplyPage(tasks, nil, taskPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if page.Items[0].ID != "task-new" {
		t.Errorf("first task = %q, want the newest (task-new)", page.Items[0].ID)
	}

	for _, query := range []string{"task-old", "wire the", "completed", "proj-a"} {
		result, searchErr := ApplyPage(tasks, &Page{Query: query}, taskPageSpec)
		if searchErr != nil {
			t.Fatalf("ApplyPage(q=%q) error = %v", query, searchErr)
		}
		if result.Total != 1 || result.Items[0].ID != "task-old" {
			t.Errorf("q=%q matched %d tasks, want only task-old", query, result.Total)
		}
	}
}

// TestSessionPageSpec_KeepsItsOldestFirstOrder is the regression guard on the
// one listing whose natural order is NOT newest first: it has always answered
// in creation order and paging must not quietly reverse it.
func TestSessionPageSpec_KeepsItsOldestFirstOrder(t *testing.T) {
	t.Parallel()

	sessions := []*SessionInfo{
		{SessionID: "sess-new", Backend: "codex", Model: "gpt", State: SessionStateActive, CreatedAt: 300},
		{SessionID: "sess-old", Backend: "claude", Model: "opus", State: SessionStateClosed, CreatedAt: 100},
	}

	page, err := ApplyPage(sessions, nil, sessionPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if page.Items[0].SessionID != "sess-old" {
		t.Errorf("first session = %q, want the oldest (sess-old)", page.Items[0].SessionID)
	}

	reversed, err := ApplyPage(sessions, &Page{Sort: "created_at:desc"}, sessionPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if reversed.Items[0].SessionID != "sess-new" {
		t.Errorf("first session = %q, want sess-new when newest is asked for", reversed.Items[0].SessionID)
	}

	for _, query := range []string{"sess-old", "claude", "opus", "closed"} {
		result, searchErr := ApplyPage(sessions, &Page{Query: query}, sessionPageSpec)
		if searchErr != nil {
			t.Fatalf("ApplyPage(q=%q) error = %v", query, searchErr)
		}
		if result.Total != 1 || result.Items[0].SessionID != "sess-old" {
			t.Errorf("q=%q matched %d sessions, want only sess-old", query, result.Total)
		}
	}
}

// TestAgentPageSpec_SearchesEveryBackendItServes proves the fleet search
// matches the capability list too, so an agent that serves codex as a
// secondary backend is found by "codex" — the same rule dispatch applies.
func TestBackendRunnerAndArtifactPageSpecs(t *testing.T) {
	t.Parallel()

	backends, err := ApplyPage([]*AgentBackendInfo{
		{Name: "zeta", Type: "acp", DefaultModel: "opus"},
		{Name: "alpha", Type: "ollama", DefaultModel: "llama"},
	}, &Page{Limit: 1, Sort: "name", Query: "LLAMA"}, backendPageSpec)
	if err != nil {
		t.Fatalf("backend ApplyPage() error = %v, want nil", err)
	}
	if backends.Total != 1 || backends.Items[0].Name != "alpha" || backends.HasNext {
		t.Fatalf("backend page = total %d first %q hasNext %v, want alpha on last filtered page", backends.Total, backends.Items[0].Name, backends.HasNext)
	}
	if _, err := ApplyPage([]*AgentBackendInfo{{Name: "alpha"}}, &Page{Sort: "state"}, backendPageSpec); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("backend bad sort error = %v, want typed allow-list refusal", err)
	}

	runners, err := ApplyPage([]*JobRunnerInfo{
		{Name: "zeta", States: []JobStateSpec{{State: JobStateExecuting, Label: "Working"}}},
		{Name: "alpha", States: []JobStateSpec{{State: JobStateReviewing, Label: "Reviewing"}}},
	}, &Page{Sort: "name:desc", Query: "reviewing"}, runnerPageSpec)
	if err != nil {
		t.Fatalf("runner ApplyPage() error = %v, want nil", err)
	}
	if runners.Total != 1 || runners.Items[0].Name != "alpha" {
		t.Fatalf("runner page = total %d first %q, want alpha", runners.Total, runners.Items[0].Name)
	}
	if _, err := ApplyPage([]*JobRunnerInfo{{Name: "alpha"}}, &Page{Sort: "state"}, runnerPageSpec); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("runner bad sort error = %v, want typed allow-list refusal", err)
	}

	artifacts, err := ApplyPage([]Artifact{
		{Name: "report", Kind: ArtifactKindInline, MIMEType: "text/plain", Size: 12},
		{Name: "patch", Kind: ArtifactKindCommit, MIMEType: "text/x-diff", Size: 4},
	}, &Page{Limit: 1, Offset: 1, Sort: "size:desc", Query: "text"}, artifactPageSpec)
	if err != nil {
		t.Fatalf("artifact ApplyPage() error = %v, want nil", err)
	}
	if artifacts.Total != 2 || artifacts.Items[0].Name != "patch" || artifacts.HasNext {
		t.Fatalf("artifact page = total %d first %q hasNext %v, want second sorted item and last page", artifacts.Total, artifacts.Items[0].Name, artifacts.HasNext)
	}
	if _, err := ApplyPage([]Artifact{{Name: "report"}}, &Page{Sort: "created_at"}, artifactPageSpec); err == nil || !strings.Contains(err.Error(), "mime_type") {
		t.Fatalf("artifact bad sort error = %v, want typed allow-list refusal", err)
	}
}

type defaultModelBackend struct {
	testMinimalBackend
	model string
}

func (b *defaultModelBackend) DefaultModel() string { return b.model }

func TestListBackendsPopulatesDefaultModelForPageSpec(t *testing.T) {
	t.Parallel()

	svc := newTestService(t)
	ctx := context.Background()
	for name, model := range map[string]string{
		"alpha": "llama3",
		"zeta":  "opus",
	} {
		if err := svc.RegisterBackend(ctx, &RegisterBackendRequest{
			Name:    name,
			Backend: &defaultModelBackend{model: model},
		}); err != nil {
			t.Fatalf("RegisterBackend(%q): %v", name, err)
		}
	}

	result, err := svc.ListBackendsPage(ctx, &Page{Sort: "default_model", Query: "llama"})
	if err != nil {
		t.Fatalf("ListBackendsPage() error = %v, want nil", err)
	}
	if result.Total != 1 || len(result.Items) != 1 {
		t.Fatalf("ListBackendsPage() returned total %d len %d, want one model match", result.Total, len(result.Items))
	}
	if got := result.Items[0].DefaultModel; got != "llama3" {
		t.Fatalf("DefaultModel = %q, want llama3", got)
	}
}

func TestAgentPageSpec_SearchesEveryBackendItServes(t *testing.T) {
	t.Parallel()

	agents := []*FleetNode{
		{ID: "agent-1", Name: "burnin-01", Backend: "claude", Capabilities: []string{"codex", "gpu"}, RegisteredAt: 100},
		{ID: "agent-2", Name: "burnin-02", Backend: "ollama", RegisteredAt: 300},
	}

	page, err := ApplyPage(agents, nil, fleetNodePageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if page.Items[0].ID != "agent-2" {
		t.Errorf("first agent = %q, want the most recently registered (agent-2)", page.Items[0].ID)
	}

	for _, query := range []string{"codex", "agent-1", "burnin-01", "claude"} {
		result, searchErr := ApplyPage(agents, &Page{Query: query}, fleetNodePageSpec)
		if searchErr != nil {
			t.Fatalf("ApplyPage(q=%q) error = %v", query, searchErr)
		}
		if result.Total != 1 || result.Items[0].ID != "agent-1" {
			t.Errorf("q=%q matched %d agents, want only agent-1", query, result.Total)
		}
	}
}

// TestWorkflowPageSpec_OrdersByName proves the workflow listing keeps the
// by-name order it has always had, and that `q` matches the name that is also
// its identifier.
func TestWorkflowPageSpec_OrdersByName(t *testing.T) {
	t.Parallel()

	workflows := []*WorkflowDefinition{
		{Name: "zeta", Mode: WorkflowModeAutonomous},
		{Name: "alpha", Mode: WorkflowModeConstrained},
	}

	page, err := ApplyPage(workflows, nil, workflowPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if page.Items[0].Name != "alpha" {
		t.Errorf("first workflow = %q, want alpha", page.Items[0].Name)
	}

	result, err := ApplyPage(workflows, &Page{Query: "ZET"}, workflowPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if result.Total != 1 || result.Items[0].Name != "zeta" {
		t.Errorf("q=ZET matched %d workflows, want only zeta", result.Total)
	}
}

// TestProjectPageSpec_OrdersAndSearches covers the projects listing.
func TestProjectPageSpec_OrdersAndSearches(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	projects := []*Project{
		{ID: "proj-old", Name: "quicbaas", CreatedAt: older},
		{ID: "proj-new", Name: "handbook", CreatedAt: older.Add(time.Hour)},
	}

	page, err := ApplyPage(projects, nil, projectPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if page.Items[0].ID != "proj-new" {
		t.Errorf("first project = %q, want the newest (proj-new)", page.Items[0].ID)
	}

	for _, query := range []string{"proj-old", "QUICBAAS"} {
		result, searchErr := ApplyPage(projects, &Page{Query: query}, projectPageSpec)
		if searchErr != nil {
			t.Fatalf("ApplyPage(q=%q) error = %v", query, searchErr)
		}
		if result.Total != 1 || result.Items[0].ID != "proj-old" {
			t.Errorf("q=%q matched %d projects, want only proj-old", query, result.Total)
		}
	}
}

// TestAccountPageSpec_OrdersAndSearches covers the accounts listing,
// including the provider and display name the contract documents.
func TestAccountPageSpec_OrdersAndSearches(t *testing.T) {
	t.Parallel()

	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	accounts := []*Account{
		{ID: "acct-old", DisplayName: "alice-work", Provider: AccountProvider("anthropic"), CreatedAt: older},
		{ID: "acct-new", DisplayName: "team-max", Provider: AccountProvider("openai"), CreatedAt: older.Add(time.Hour)},
	}

	page, err := ApplyPage(accounts, nil, accountPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if page.Items[0].ID != "acct-new" {
		t.Errorf("first account = %q, want the newest (acct-new)", page.Items[0].ID)
	}

	for _, query := range []string{"acct-old", "alice", "anthropic"} {
		result, searchErr := ApplyPage(accounts, &Page{Query: query}, accountPageSpec)
		if searchErr != nil {
			t.Fatalf("ApplyPage(q=%q) error = %v", query, searchErr)
		}
		if result.Total != 1 || result.Items[0].ID != "acct-old" {
			t.Errorf("q=%q matched %d accounts, want only acct-old", query, result.Total)
		}
	}

	byName, err := ApplyPage(accounts, &Page{Sort: "display_name"}, accountPageSpec)
	if err != nil {
		t.Fatalf("ApplyPage() error = %v, want nil", err)
	}
	if byName.Items[0].DisplayName != "alice-work" {
		t.Errorf("first account = %q, want alice-work", byName.Items[0].DisplayName)
	}
}

// TestPageSpecs_RefuseAnUnknownFieldNamingTheirOwnAllowList proves each
// endpoint's refusal quotes ITS fields, not a shared list — the message is
// what tells a UI which column headers it may offer.
func TestPageSpecs_RefuseAnUnknownFieldNamingTheirOwnAllowList(t *testing.T) {
	t.Parallel()

	_, err := ApplyPage([]*JobStatus{{JobID: "j"}}, &Page{Sort: "subject"}, jobPageSpec)
	if err == nil || !strings.Contains(err.Error(), "runner") {
		t.Errorf("jobs refusal = %v, want it to name the jobs allow-list", err)
	}
	_, err = ApplyPage([]*Task{{ID: "t"}}, &Page{Sort: "runner"}, taskPageSpec)
	if err == nil || !strings.Contains(err.Error(), "subject") {
		t.Errorf("tasks refusal = %v, want it to name the tasks allow-list", err)
	}
}
