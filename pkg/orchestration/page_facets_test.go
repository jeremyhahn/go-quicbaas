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
	"errors"
	"reflect"
	"testing"
)

// facetTestSpec counts the test records the two ways a facet can be shaped:
// one value per record, and several.
var facetTestSpec = FacetSpec[*Task]{
	Values: map[string]func(*Task) []string{
		"status":     func(task *Task) []string { return []string{string(task.Status)} },
		"project_id": func(task *Task) []string { return []string{task.ProjectID} },
	},
}

// facetTestTasks are four tasks across two statuses and two projects, one of
// them unscoped.
func facetTestTasks() []*Task {
	return []*Task{
		{ID: "t1", Subject: "alpha", Status: TaskStatusPending, ProjectID: "proj-a", CreatedAt: 1},
		{ID: "t2", Subject: "beta", Status: TaskStatusPending, ProjectID: "proj-b", CreatedAt: 2},
		{ID: "t3", Subject: "gamma", Status: TaskStatusBlocked, ProjectID: "proj-a", CreatedAt: 3},
		{ID: "t4", Subject: "alpha again", Status: TaskStatusBlocked, CreatedAt: 4},
	}
}

// TestApplyFacetedPage_CountsBeforeNarrowingAndAfterSearch is the order of
// operations the whole contract rests on: the counts must show what every
// UNSELECTED choice would yield, while Total counts what the caller actually
// asked for.
func TestApplyFacetedPage_CountsBeforeNarrowingAndAfterSearch(t *testing.T) {
	t.Parallel()

	page, err := ApplyFacetedPage(facetTestTasks(), &Page{Query: "alpha"}, taskPageSpec, facetTestSpec,
		func(task *Task) bool { return task.Status == TaskStatusPending })
	if err != nil {
		t.Fatalf("ApplyFacetedPage() error = %v, want nil", err)
	}

	// The narrowing holds: only the pending "alpha" task survives.
	if page.Total != 1 || page.Items[0].ID != "t1" {
		t.Errorf("page = %d records (%v), want only t1", page.Total, page.Items)
	}
	// The counts saw BOTH "alpha" tasks — the one the status narrowing kept
	// and the one it removed — because selecting "blocked" instead would
	// yield that second one, and a bar that printed 0 there could not say so.
	wantStatus := map[string]int{"pending": 1, "blocked": 1}
	if !reflect.DeepEqual(page.Facets["status"], wantStatus) {
		t.Errorf("status facet = %v, want %v", page.Facets["status"], wantStatus)
	}
	// The SEARCH, unlike the narrowing, is upstream of the counts: the two
	// tasks it dropped are counted nowhere.
	wantProject := map[string]int{"proj-a": 1}
	if !reflect.DeepEqual(page.Facets["project_id"], wantProject) {
		t.Errorf("project facet = %v, want %v (t4 carries no project and is not counted)", page.Facets["project_id"], wantProject)
	}
}

// TestApplyFacetedPage_NilPageAnswersEverythingAndCountsNothing proves the
// internal callers that cannot page — the health sweep, the dispatcher — are
// not charged for a filter bar nobody renders, and still see every record.
func TestApplyFacetedPage_NilPageAnswersEverythingAndCountsNothing(t *testing.T) {
	t.Parallel()

	page, err := ApplyFacetedPage(facetTestTasks(), nil, taskPageSpec, facetTestSpec, nil)
	if err != nil {
		t.Fatalf("ApplyFacetedPage() error = %v, want nil", err)
	}
	if page.Total != 4 || len(page.Items) != 4 {
		t.Errorf("page = %d of %d, want the whole set uncut", len(page.Items), page.Total)
	}
	if page.Facets != nil {
		t.Errorf("Facets = %v, want none for a caller that asked for no page", page.Facets)
	}
}

// TestApplyFacetedPage_RefusesAnUnknownSortBeforeCountingAnything proves the
// bad-input path is still a typed refusal, and that it is decided before any
// work is done on a request that cannot be answered.
func TestApplyFacetedPage_RefusesAnUnknownSortBeforeCountingAnything(t *testing.T) {
	t.Parallel()

	page, err := ApplyFacetedPage(facetTestTasks(), &Page{Sort: "runner"}, taskPageSpec, facetTestSpec,
		func(*Task) bool { t.Error("the narrowing ran for a request that cannot be served"); return true })
	if page != nil {
		t.Error("ApplyFacetedPage() returned a page for a sort it cannot serve")
	}
	var unknown *UnknownSortFieldError
	if !errors.As(err, &unknown) || unknown.Field != "runner" {
		t.Errorf("error = %v, want *UnknownSortFieldError naming runner", err)
	}
}

// TestCountFacets_CountsARecordOncePerValue proves a record whose extractor
// names the same value twice — the daemon whose primary backend is also in
// its capabilities — is one record, not two.
func TestCountFacets_CountsARecordOncePerValue(t *testing.T) {
	t.Parallel()

	agents := []*FleetNode{
		{ID: "a1", Backend: "claude", Capabilities: []string{"claude", "codex"}},
		{ID: "a2", Backend: "codex"},
	}
	counted := countFacets(agents, fleetNodeFacetSpec)

	want := map[string]int{"claude": 1, "codex": 2}
	if !reflect.DeepEqual(counted["backend"], want) {
		t.Errorf("backend facet = %v, want %v", counted["backend"], want)
	}
}

// TestCountFacets_EmptySpecCountsNothing proves the endpoints with no facets
// answer with no facets block at all, rather than an empty one a consumer
// would have to tell apart from "nothing matched".
func TestCountFacets_EmptySpecCountsNothing(t *testing.T) {
	t.Parallel()

	if counted := countFacets(facetTestTasks(), FacetSpec[*Task]{}); counted != nil {
		t.Errorf("countFacets(no spec) = %v, want nil", counted)
	}
}

// TestFacetKeys_AreTheDocumentedContract pins the per-resource facet keys the
// UI's filter bar is written against. A key renamed here without renaming it
// there is a bar that renders nothing, so the list is asserted rather than
// described.
func TestFacetKeys_AreTheDocumentedContract(t *testing.T) {
	t.Parallel()

	cases := []struct {
		resource string
		keys     []string
	}{
		{"tasks", TaskFacetSpec().FacetKeys()},
		{"jobs", JobFacetSpec().FacetKeys()},
		{"sessions", SessionFacetSpec().FacetKeys()},
		{"agents", FleetNodeFacetSpec().FacetKeys()},
		{"workflows", WorkflowFacetSpec().FacetKeys()},
		{"projects", ProjectFacetSpec().FacetKeys()},
		{"accounts", AccountFacetSpec().FacetKeys()},
	}
	want := map[string][]string{
		"tasks":     {"agent_id", "project_id", "status"},
		"jobs":      {"agent_id", "project_id", "runner", "state"},
		"sessions":  {"agent_id", "backend", "runner", "state"},
		"agents":    {"backend", "status"},
		"workflows": {"mode", "project_id"},
		"projects":  {"autonomous"},
		"accounts":  {"provider", "status"},
	}
	for _, tc := range cases {
		if !reflect.DeepEqual(tc.keys, want[tc.resource]) {
			t.Errorf("%s facets = %v, want %v", tc.resource, tc.keys, want[tc.resource])
		}
	}
}

// TestTaskFilter_FacetBaseKeepsTheStoreNarrowingsAndDropsTheCountedOnes
// proves the split: what the store is asked for, and what the page applies.
func TestTaskFilter_FacetBaseKeepsTheStoreNarrowingsAndDropsTheCountedOnes(t *testing.T) {
	t.Parallel()

	filter := &TaskFilter{
		TeamName:  "platform",
		Status:    TaskStatusPending,
		ProjectID: "proj-a",
		AgentID:   "agent-1",
		JobID:     "job-1",
		Statuses:  []TaskStatus{TaskStatusBlocked},
	}
	base := filter.FacetBase()

	if base.TeamName != "platform" || base.Status != TaskStatusPending || base.JobID != "job-1" {
		t.Errorf("base = %+v, want the uncounted narrowings kept", base)
	}
	if base.ProjectID != "" || base.AgentID != "" || base.Statuses != nil {
		t.Errorf("base = %+v, want the counted narrowings removed", base)
	}
	if filter.ProjectID != "proj-a" {
		t.Error("facetBase mutated the caller's filter")
	}
}

// TestTaskFilter_MatchesFacets covers the faceted half on its own, including
// the two answers that are easy to get backwards: an empty filter matches
// everything, and a nil task matches nothing.
func TestTaskFilter_MatchesFacets(t *testing.T) {
	t.Parallel()

	task := &Task{Status: TaskStatusBlocked, ProjectID: "proj-a", AgentID: "agent-1"}
	cases := []struct {
		name   string
		filter *TaskFilter
		want   bool
	}{
		{"nil filter matches", nil, true},
		{"empty filter matches", &TaskFilter{}, true},
		{"status matches", &TaskFilter{Statuses: []TaskStatus{TaskStatusBlocked}}, true},
		{"status misses", &TaskFilter{Statuses: []TaskStatus{TaskStatusPending}}, false},
		{"one of several statuses matches", &TaskFilter{Statuses: []TaskStatus{TaskStatusPending, TaskStatusBlocked}}, true},
		{"project matches", &TaskFilter{ProjectID: "proj-a"}, true},
		{"project misses", &TaskFilter{ProjectID: "proj-b"}, false},
		{"agent matches", &TaskFilter{AgentID: "agent-1"}, true},
		{"agent misses", &TaskFilter{AgentID: "agent-2"}, false},
		{"all compose", &TaskFilter{Statuses: []TaskStatus{TaskStatusBlocked}, ProjectID: "proj-a", AgentID: "agent-1"}, true},
	}
	for _, tc := range cases {
		if got := tc.filter.MatchesFacets(task); got != tc.want {
			t.Errorf("%s: matchesFacets() = %v, want %v", tc.name, got, tc.want)
		}
	}
	if (&TaskFilter{}).MatchesFacets(nil) {
		t.Error("matchesFacets(nil task) = true, want false")
	}
}

// TestJobFilter_MatchesFacets covers the jobs listing's faceted half,
// including the record shape that carries the runner and project only on the
// launch config rather than on the status itself.
func TestJobFilter_MatchesFacets(t *testing.T) {
	t.Parallel()

	status := &JobStatus{
		State:   JobStateExecuting,
		AgentID: "agent-1",
		Config:  &JobConfig{ProjectID: "proj-a"},
		Runner:  "sdlc",
	}
	cases := []struct {
		name   string
		filter *JobFilter
		want   bool
	}{
		{"nil filter matches", nil, true},
		{"state matches", &JobFilter{Statuses: []JobState{JobStateExecuting}}, true},
		{"state misses", &JobFilter{Statuses: []JobState{JobStateFailed}}, false},
		{"runner matches", &JobFilter{Runner: "sdlc"}, true},
		{"runner misses", &JobFilter{Runner: "review"}, false},
		{"project matches through the config", &JobFilter{ProjectID: "proj-a"}, true},
		{"project misses", &JobFilter{ProjectID: "proj-b"}, false},
		{"agent matches", &JobFilter{AgentID: "agent-1"}, true},
		{"agent misses", &JobFilter{AgentID: "agent-2"}, false},
	}
	for _, tc := range cases {
		if got := tc.filter.MatchesFacets(status); got != tc.want {
			t.Errorf("%s: matchesFacets() = %v, want %v", tc.name, got, tc.want)
		}
	}
	if (&JobFilter{}).MatchesFacets(nil) {
		t.Error("matchesFacets(nil status) = true, want false")
	}
	// The base half keeps the bindings no facet counts, and drops the rest.
	base := (&JobFilter{TaskID: "task-1", JobIDs: []string{"job-1"}, Runner: "sdlc", AgentID: "a"}).FacetBase()
	if base.TaskID != "task-1" || len(base.JobIDs) != 1 || base.Runner != "" || base.AgentID != "" {
		t.Errorf("facetBase() = %+v, want only the task binding and the id list", base)
	}
}

// TestAgentFilter_MatchesFacetsServesBackendRatherThanEqualsIt proves the
// fleet's backend narrowing is membership, so the filter and the count that
// sits beside it agree about a daemon serving three backends.
func TestAgentFilter_MatchesFacetsServesBackendRatherThanEqualsIt(t *testing.T) {
	t.Parallel()

	agent := &FleetNode{
		ID:           "a1",
		Backend:      "acp",
		Capabilities: []string{"claude", "codex"},
		Status:       FleetNodeStatusRunning,
	}
	if !(&FleetNodeFilter{Backend: "codex"}).MatchesFacets(agent) {
		t.Error("a daemon serving codex as a capability did not match the codex filter")
	}
	if (&FleetNodeFilter{Backend: "opencode"}).MatchesFacets(agent) {
		t.Error("a daemon that serves no opencode matched the opencode filter")
	}
	if (&FleetNodeFilter{Status: FleetNodeStatusOffline}).MatchesFacets(agent) {
		t.Error("an online daemon matched the offline filter")
	}
	if (&FleetNodeFilter{}).MatchesFacets(nil) {
		t.Error("matchesFacets(nil agent) = true, want false")
	}
	base := (&FleetNodeFilter{Host: "h", Backend: "codex", Status: FleetNodeStatusRunning, Model: "m"}).FacetBase()
	if base.Host != "h" || base.Model != "m" || base.Backend != "" || base.Status != "" {
		t.Errorf("facetBase() = %+v, want the counted narrowings removed", base)
	}
}

// TestWorkflowFilter_Matches covers the workflows listing's two narrowings
// and the nil arms either side of them.
func TestWorkflowFilter_Matches(t *testing.T) {
	t.Parallel()

	definition := &WorkflowDefinition{Name: "sdlc", Mode: WorkflowModeCanned, ProjectID: "proj-a"}
	if !(&WorkflowFilter{}).Matches(definition) {
		t.Error("an empty filter did not match")
	}
	if !(&WorkflowFilter{ProjectID: "proj-a", Mode: WorkflowModeCanned}).Matches(definition) {
		t.Error("the matching project and mode did not match")
	}
	if (&WorkflowFilter{ProjectID: "proj-b"}).Matches(definition) {
		t.Error("another project's filter matched")
	}
	if (&WorkflowFilter{Mode: WorkflowModeAutonomous}).Matches(definition) {
		t.Error("another mode's filter matched")
	}
	if (&WorkflowFilter{}).Matches(nil) {
		t.Error("Matches(nil) = true, want false")
	}
}

// TestMatchesProjectAutonomy_IsTriState proves the absent parameter narrows
// nothing, which is the distinction a plain bool could not carry.
func TestMatchesProjectAutonomy_IsTriState(t *testing.T) {
	t.Parallel()

	on := &Project{Autonomous: true}
	off := &Project{}
	yes, no := true, false

	if !MatchesProjectAutonomy(on, nil) || !MatchesProjectAutonomy(off, nil) {
		t.Error("an absent autonomous filter narrowed the listing")
	}
	if !MatchesProjectAutonomy(on, &yes) || MatchesProjectAutonomy(off, &yes) {
		t.Error("autonomous=true did not select exactly the autonomous projects")
	}
	if MatchesProjectAutonomy(on, &no) || !MatchesProjectAutonomy(off, &no) {
		t.Error("autonomous=false did not select exactly the supervised projects")
	}
	if MatchesProjectAutonomy(nil, nil) {
		t.Error("MatchesProjectAutonomy(nil) = true, want false")
	}
}

// TestMatchesAccountFacets covers the accounts listing's provider and status
// narrowings, together and apart.
func TestMatchesAccountFacets(t *testing.T) {
	t.Parallel()

	account := &Account{Provider: AccountProviderClaude, Status: AccountStatusActive}
	if !MatchesAccountFacets(account, "", "") {
		t.Error("an empty filter did not match")
	}
	if !MatchesAccountFacets(account, AccountProviderClaude, AccountStatusActive) {
		t.Error("the matching provider and status did not match")
	}
	if MatchesAccountFacets(account, AccountProviderClaude, AccountStatusRevoked) {
		t.Error("a revoked filter matched an active account")
	}
	if MatchesAccountFacets(account, AccountProviderCodex, "") {
		t.Error("another provider's filter matched")
	}
	if MatchesAccountFacets(nil, "", "") {
		t.Error("MatchesAccountFacets(nil) = true, want false")
	}
}

// TestMatchesSessionFacets covers the sessions listing's four narrowings,
// including the two that are resolved rather than recorded.
func TestMatchesSessionFacets(t *testing.T) {
	t.Parallel()

	session := &SessionInfo{
		Backend: "claude",
		State:   SessionStateActive,
		Runner:  "review",
		AgentID: "agent-1",
	}
	cases := []struct {
		name string
		req  *ListSessionsRequest
		want bool
	}{
		{"nil request matches", nil, true},
		{"empty request matches", &ListSessionsRequest{}, true},
		{"backend matches", &ListSessionsRequest{Backend: "claude"}, true},
		{"backend misses", &ListSessionsRequest{Backend: "codex"}, false},
		{"state matches", &ListSessionsRequest{Status: SessionStateActive}, true},
		{"state misses", &ListSessionsRequest{Status: SessionStateClosed}, false},
		{"runner matches", &ListSessionsRequest{Runner: "review"}, true},
		{"runner misses", &ListSessionsRequest{Runner: "sdlc"}, false},
		{"agent matches", &ListSessionsRequest{AgentID: "agent-1"}, true},
		{"agent misses", &ListSessionsRequest{AgentID: "agent-2"}, false},
	}
	for _, tc := range cases {
		if got := MatchesSessionFacets(tc.req, session); got != tc.want {
			t.Errorf("%s: MatchesSessionFacets() = %v, want %v", tc.name, got, tc.want)
		}
	}
	if MatchesSessionFacets(&ListSessionsRequest{}, nil) {
		t.Error("MatchesSessionFacets(nil session) = true, want false")
	}
}
