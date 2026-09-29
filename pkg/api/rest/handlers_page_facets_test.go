//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// The faceted filter contract, on the wire
//
// THE SPELLING HERE IS THE API. A web UI's filter bar is written against these
// exact parameter names and these exact facet keys; a rename that compiles is
// still a broken bar, so the names are asserted rather than described.
// =============================================================================

// decodeFacets reads the facets block off a list response.
func decodeFacets(t *testing.T, body []byte) map[string]map[string]int {
	t.Helper()

	var envelope struct {
		Facets map[string]map[string]int `json:"facets"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decoding facets: %v (body=%s)", err, body)
	}
	return envelope.Facets
}

// facetTestJobs are six jobs across two runners, two states and two daemons.
func facetTestJobs() []*orchestration.JobStatus {
	return []*orchestration.JobStatus{
		{JobID: "job-0", Description: "first", Runner: "sdlc", State: orchestration.JobStateCompleted, AgentID: "agent-1", StartedAt: 0, Config: &orchestration.JobConfig{ProjectID: "proj-a"}},
		{JobID: "job-1", Description: "second", Runner: "sdlc", State: orchestration.JobStateFailed, AgentID: "agent-2", StartedAt: 1, Config: &orchestration.JobConfig{ProjectID: "proj-a"}},
		{JobID: "job-2", Description: "third", Runner: "triage", State: orchestration.JobStateCompleted, AgentID: "agent-1", StartedAt: 2, Config: &orchestration.JobConfig{ProjectID: "proj-b"}},
		{JobID: "job-3", Description: "fourth", Runner: "triage", State: orchestration.JobStateFailed, StartedAt: 3},
	}
}

// TestListJobs_NarrowsOnTheDispatchedAgent proves ?agent_id= reaches the
// service and narrows the answer.
func TestListJobs_NarrowsOnTheDispatchedAgent(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = facetTestJobs()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&agent_id=agent-1", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "jobs")
	if envelope.Total != 2 {
		t.Errorf("total = %d, want the 2 jobs agent-1 ran", envelope.Total)
	}
	for _, item := range items {
		if item["agent_id"] != "agent-1" {
			t.Errorf("job %v ran on %v, want only agent-1", item["job_id"], item["agent_id"])
		}
	}
	if agentSvc.listJobsPageFilter == nil || agentSvc.listJobsPageFilter.AgentID != "agent-1" {
		t.Error("the agent filter never reached the service")
	}
}

// TestListJobs_AnswersTheFacetCounts proves every documented key is present
// and counted over the set the chosen facet was NOT applied to.
func TestListJobs_AnswersTheFacetCounts(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = facetTestJobs()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&runner=sdlc", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	facets := decodeFacets(t, w.Body.Bytes())
	for _, key := range []string{"state", "runner", "project_id", "agent_id"} {
		if _, ok := facets[key]; !ok {
			t.Errorf("facets = %v, want a %q key", facets, key)
		}
	}
	if facets["runner"]["triage"] != 2 {
		t.Errorf("runner facet = %v, want triage still counted while sdlc is selected", facets["runner"])
	}
	if facets["state"]["failed"] != 2 || facets["state"]["completed"] != 2 {
		t.Errorf("state facet = %v, want both states counted over the whole tenant", facets["state"])
	}
	if facets["agent_id"]["agent-1"] != 2 || facets["agent_id"]["agent-2"] != 1 {
		t.Errorf("agent facet = %v, want the per-daemon counts", facets["agent_id"])
	}
}

// TestListJobs_RefusesAStateOutsideTheEnum proves the one filter value that
// is a 400 rather than an empty page.
func TestListJobs_RefusesAStateOutsideTheEnum(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = facetTestJobs()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&statuses=failed,fialed", nil, nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "fialed") {
		t.Errorf("refusal %s does not name the offending value", w.Body.String())
	}
}

// TestListJobs_UnknownAgentMatchesNothingRatherThanRefusing proves the OTHER
// half of the rule: a value that names a record rather than an enum member is
// an empty page, because it may simply not exist.
func TestListJobs_UnknownAgentMatchesNothingRatherThanRefusing(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = facetTestJobs()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&agent_id=no-such-daemon", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "jobs")
	if envelope.Total != 0 || len(items) != 0 {
		t.Errorf("matched %d jobs, want none", envelope.Total)
	}
}

// facetTestTasks are five tasks across three statuses, two projects and two
// daemons.
func facetTestTasks() []*orchestration.Task {
	return []*orchestration.Task{
		{ID: "task-0", Subject: "alpha", Status: orchestration.TaskStatusPending, ProjectID: "proj-a", CreatedAt: 0},
		{ID: "task-1", Subject: "beta", Status: orchestration.TaskStatusBlocked, ProjectID: "proj-a", AgentID: "agent-1", CreatedAt: 1},
		{ID: "task-2", Subject: "gamma", Status: orchestration.TaskStatusBlocked, ProjectID: "proj-b", AgentID: "agent-2", CreatedAt: 2},
		{ID: "task-3", Subject: "delta", Status: orchestration.TaskStatusCompleted, ProjectID: "proj-b", AgentID: "agent-1", CreatedAt: 3},
	}
}

// TestListTasks_NarrowsOnStatusesProjectAndAgent walks each parameter the
// Tasks view sends, one at a time.
func TestListTasks_NarrowsOnStatusesProjectAndAgent(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	tasks := facetTestTasks()
	agentSvc.listTasksFunc = func(_ context.Context, _ string, _ *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return tasks, nil
	}

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"statuses", "statuses=blocked", []string{"task-2", "task-1"}},
		{"several statuses", "statuses=blocked,completed", []string{"task-3", "task-2", "task-1"}},
		{"project", "project_id=proj-b", []string{"task-3", "task-2"}},
		{"agent", "agent_id=agent-1", []string{"task-3", "task-1"}},
		{"compose", "agent_id=agent-1&statuses=blocked", []string{"task-1"}},
		{"unknown agent matches nothing", "agent_id=nobody", nil},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		server.HandleOrchestratorListTasks(w,
			orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&"+tc.query, nil, nil))

		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200; body=%s", tc.name, w.Code, w.Body.String())
			continue
		}
		envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "tasks")
		if envelope.Total != len(tc.want) {
			t.Errorf("%s: total = %d, want %d", tc.name, envelope.Total, len(tc.want))
			continue
		}
		for i, want := range tc.want {
			if items[i]["id"] != want {
				t.Errorf("%s: listed %v, want %v", tc.name, items, tc.want)
				break
			}
		}
	}
}

// TestListTasks_AnswersTheFacetCountsAndComposesWithSearch proves the counts
// are taken over the searched-but-unfaceted set, which is what lets a bar
// offer a choice the current selection excludes.
func TestListTasks_AnswersTheFacetCountsAndComposesWithSearch(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	tasks := facetTestTasks()
	agentSvc.listTasksFunc = func(_ context.Context, _ string, _ *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return tasks, nil
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&statuses=blocked&limit=1", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "tasks")
	if envelope.Total != 2 || len(items) != 1 {
		t.Errorf("page = %d of %d, want 1 of 2", len(items), envelope.Total)
	}
	facets := decodeFacets(t, w.Body.Bytes())
	for _, key := range []string{"status", "project_id", "agent_id"} {
		if _, ok := facets[key]; !ok {
			t.Errorf("facets = %v, want a %q key", facets, key)
		}
	}
	if facets["status"]["pending"] != 1 || facets["status"]["completed"] != 1 {
		t.Errorf("status facet = %v, want the unselected statuses still counted", facets["status"])
	}
	if facets["agent_id"]["agent-1"] != 2 {
		t.Errorf("agent facet = %v, want agent-1 counted twice", facets["agent_id"])
	}
}

// TestListTasks_RefusesAStatusOutsideTheEnum proves the tasks listing refuses
// the same class of value the jobs listing does.
func TestListTasks_RefusesAStatusOutsideTheEnum(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&statuses=blocked,blokced", nil, nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "blokced") {
		t.Errorf("refusal %s does not name the offending value", w.Body.String())
	}
}

// TestListTasks_SortsOnTheAttemptCount proves the sort field the Tasks view
// added is served, and that an unknown one is still refused.
func TestListTasks_SortsOnTheAttemptCount(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listTasksFunc = func(_ context.Context, _ string, _ *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return []*orchestration.Task{
			{ID: "task-0", Attempts: 1, CreatedAt: 0},
			{ID: "task-1", Attempts: 5, CreatedAt: 1},
			{ID: "task-2", Attempts: 3, CreatedAt: 2},
		}, nil
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&sort=attempts:desc", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "tasks")
	if items[0]["id"] != "task-1" {
		t.Errorf("first task = %v, want the most-attempted one", items[0]["id"])
	}

	refused := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(refused,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&sort=runner", nil, nil))
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a field the tasks listing has no order for", refused.Code)
	}
}

// facetTestSessions are four sessions across two backends, two states, two
// roles and two daemons.
func facetTestSessions() []*orchestration.SessionInfo {
	return []*orchestration.SessionInfo{
		{SessionID: "sess-0", ProjectID: "proj-a", JobID: "job-0", Backend: "claude", State: orchestration.SessionStateActive, Runner: "sdlc", AgentID: "agent-1", CreatedAt: 0},
		{SessionID: "sess-1", ProjectID: "proj-a", JobID: "job-1", Backend: "codex", State: orchestration.SessionStateClosed, Runner: "review", AgentID: "agent-2", CreatedAt: 1},
		{SessionID: "sess-2", ProjectID: "proj-b", JobID: "job-1", Backend: "claude", State: orchestration.SessionStateActive, Runner: "review", AgentID: "agent-1", CreatedAt: 2},
	}
}

// TestListSessions_NarrowsOnProjectJobRunnerAndAgent walks the four
// parameters the Sessions view gained.
func TestListSessions_NarrowsOnProjectJobRunnerAndAgent(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listSessions = facetTestSessions()

	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"project", "project_id=proj-a", []string{"sess-0", "sess-1"}},
		{"job", "job_id=job-1", []string{"sess-1", "sess-2"}},
		{"runner", "runner=review", []string{"sess-1", "sess-2"}},
		{"agent", "agent_id=agent-1", []string{"sess-0", "sess-2"}},
		{"compose", "runner=review&agent_id=agent-1", []string{"sess-2"}},
		{"unknown runner matches nothing", "runner=nobody", nil},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		server.HandleOrchestratorListSessions(w,
			orchReq(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id=acme&"+tc.query, nil, nil))

		if w.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200; body=%s", tc.name, w.Code, w.Body.String())
			continue
		}
		envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "sessions")
		if envelope.Total != len(tc.want) {
			t.Errorf("%s: total = %d, want %d", tc.name, envelope.Total, len(tc.want))
			continue
		}
		for i, want := range tc.want {
			if items[i]["session_id"] != want {
				t.Errorf("%s: listed %v, want %v", tc.name, items, tc.want)
				break
			}
		}
	}
}

// TestListSessions_AnswersTheFacetCounts proves the sessions envelope carries
// the four documented keys, counted over the unfaceted set.
func TestListSessions_AnswersTheFacetCounts(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listSessions = facetTestSessions()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id=acme&backend=claude", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	facets := decodeFacets(t, w.Body.Bytes())
	for _, key := range []string{"backend", "state", "runner", "agent_id"} {
		if _, ok := facets[key]; !ok {
			t.Errorf("facets = %v, want a %q key", facets, key)
		}
	}
	if facets["backend"]["codex"] != 1 {
		t.Errorf("backend facet = %v, want codex still counted while claude is selected", facets["backend"])
	}
	if facets["runner"]["sdlc"] != 1 || facets["runner"]["review"] != 2 {
		t.Errorf("runner facet = %v, want a count per role", facets["runner"])
	}
}

// TestListAgents_NarrowsOnServedBackendAndCountsThem proves the fleet's
// backend filter is membership on the wire too, and that the counts agree.
func TestListAgents_NarrowsOnServedBackendAndCountsThem(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listAgents = []*orchestration.FleetNode{
		{ID: "a1", Name: "one", Backend: "acp", Capabilities: []string{"claude", "codex"}, Status: orchestration.FleetNodeStatusRunning},
		{ID: "a2", Name: "two", Backend: "codex", Status: orchestration.FleetNodeStatusOffline},
	}

	w := httptest.NewRecorder()
	server.HandleListFleetNodes(w,
		orchReq(http.MethodGet, "/api/v1/fleet?tenant_id=acme&backend=claude", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "nodes")
	if envelope.Total != 1 || items[0]["id"] != "a1" {
		t.Errorf("matched %v, want the daemon that serves claude as a capability", items)
	}
	facets := decodeFacets(t, w.Body.Bytes())
	if facets["backend"]["codex"] != 2 {
		t.Errorf("backend facet = %v, want both codex-serving daemons counted", facets["backend"])
	}
	if facets["status"]["offline"] != 1 || facets["status"]["running"] != 1 {
		t.Errorf("status facet = %v, want one of each", facets["status"])
	}
}

// TestListWorkflows_NarrowsOnProjectAndMode proves the workflows listing
// honours the two parameters its filter bar sends.
func TestListWorkflows_NarrowsOnProjectAndMode(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listWorkflows = []*orchestration.WorkflowDefinition{
		{Name: "alpha", Mode: orchestration.WorkflowModeCanned, ProjectID: "proj-a"},
		{Name: "beta", Mode: orchestration.WorkflowModeAutonomous, ProjectID: "proj-a"},
		{Name: "gamma", Mode: orchestration.WorkflowModeCanned, ProjectID: "proj-b"},
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/workflows?tenant_id=acme&project_id=proj-a&mode=canned", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "workflows")
	if envelope.Total != 1 || items[0]["name"] != "alpha" {
		t.Errorf("matched %v, want only alpha", items)
	}
	facets := decodeFacets(t, w.Body.Bytes())
	if facets["mode"]["autonomous"] != 1 || facets["project_id"]["proj-b"] != 1 {
		t.Errorf("facets = %v, want the unselected choices counted", facets)
	}
}
