//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// Paging, sorting and search — one contract, seven list endpoints
//
// Every endpoint is held to the same three properties:
//
//  1. limit/offset cut a page and the envelope counts the whole matching set;
//  2. `sort` orders on an allow-listed field and REFUSES an unknown one with
//     a 400 naming it;
//  3. `q` narrows, and composes with the filters the endpoint already had.
//
// See docs/software-factory/api.md for the contract these tests enforce.
// =============================================================================

// pageEnvelope is the paging half of any list response, decoded generically
// so one helper reads every endpoint's answer.
type pageEnvelope struct {
	Total      int  `json:"total"`
	Limit      int  `json:"limit"`
	Offset     int  `json:"offset"`
	NextOffset *int `json:"next_offset"`
}

// decodePageEnvelope reads the envelope and the records under itemsKey.
func decodePageEnvelope(t *testing.T, body []byte, itemsKey string) (pageEnvelope, []map[string]interface{}) {
	t.Helper()

	var envelope pageEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decoding envelope: %v (body=%s)", err, body)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decoding the response: %v (body=%s)", err, body)
	}
	raw, ok := fields[itemsKey]
	if !ok {
		t.Fatalf("response has no %q key (body=%s)", itemsKey, body)
	}
	var items []map[string]interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("decoding %q: %v (body=%s)", itemsKey, err, body)
	}
	return envelope, items
}

// assertPage checks the three envelope numbers and the page size.
func assertPage(t *testing.T, envelope pageEnvelope, items []map[string]interface{}, wantItems, wantTotal int, wantNext *int) {
	t.Helper()

	if len(items) != wantItems {
		t.Errorf("page size = %d, want %d", len(items), wantItems)
	}
	if envelope.Total != wantTotal {
		t.Errorf("total = %d, want %d", envelope.Total, wantTotal)
	}
	switch {
	case wantNext == nil && envelope.NextOffset != nil:
		t.Errorf("next_offset = %d, want it absent on the last page", *envelope.NextOffset)
	case wantNext != nil && envelope.NextOffset == nil:
		t.Errorf("next_offset absent, want %d", *wantNext)
	case wantNext != nil && *envelope.NextOffset != *wantNext:
		t.Errorf("next_offset = %d, want %d", *envelope.NextOffset, *wantNext)
	}
}

func offsetPtr(v int) *int { return &v }

// =============================================================================
// Jobs
// =============================================================================

// pagedJobs builds n jobs, oldest first, alternating runner and state.
func pagedJobs(n int) []*orchestration.JobStatus {
	jobs := make([]*orchestration.JobStatus, 0, n)
	for i := range n {
		runner := "sdlc"
		state := orchestration.JobStateCompleted
		if i%2 == 1 {
			runner = "triage"
			state = orchestration.JobStateFailed
		}
		jobs = append(jobs, &orchestration.JobStatus{
			JobID:       "job-" + strconv.Itoa(i),
			Description: "attempt " + strconv.Itoa(i),
			Runner:      runner,
			State:       state,
			StartedAt:   int64(i),
			TaskID:      "task-" + strconv.Itoa(i),
		})
	}
	return jobs
}

func TestListJobs_PagesAndCounts(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = pagedJobs(7)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&limit=3&offset=3", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "jobs")
	assertPage(t, envelope, items, 3, 7, offsetPtr(6))
	if envelope.Limit != 3 || envelope.Offset != 3 {
		t.Errorf("limit/offset = %d/%d, want 3/3", envelope.Limit, envelope.Offset)
	}
	// Newest first, so offset 3 of 7 starts at job-3.
	if items[0]["job_id"] != "job-3" {
		t.Errorf("first job = %v, want job-3", items[0]["job_id"])
	}
}

func TestListJobs_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = pagedJobs(4)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&sort=started_at", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "jobs")
	if items[0]["job_id"] != "job-0" {
		t.Errorf("first job = %v, want job-0 for an ascending sort", items[0]["job_id"])
	}

	refused := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(refused,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&sort=subject", nil, nil))

	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an unknown sort field; body=%s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "subject") {
		t.Errorf("refusal %s does not name the offending field", refused.Body.String())
	}
}

func TestListJobs_SearchComposesWithTheRunnerFilter(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = pagedJobs(6)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&runner=sdlc&q=attempt+4", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "jobs")
	if envelope.Total != 1 || items[0]["job_id"] != "job-4" {
		t.Errorf("matched %d jobs, want only job-4 (sdlc AND \"attempt 4\")", envelope.Total)
	}
	if agentSvc.listJobsFilter == nil || agentSvc.listJobsFilter.Runner != "sdlc" {
		t.Error("the runner filter never reached the service")
	}
}

func TestListJobs_RefusesAnUnreadableLimit(t *testing.T) {
	server, _, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&limit=lots", nil, nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "limit") {
		t.Errorf("refusal %s does not name the parameter", w.Body.String())
	}
}

func TestListJobs_NarrowsByStateAndProject(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listJobs = pagedJobs(6)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListJobs(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/jobs?tenant_id=acme&statuses=failed", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "jobs")
	if envelope.Total != 3 {
		t.Errorf("total = %d, want the 3 failed jobs", envelope.Total)
	}
	for _, item := range items {
		if item["state"] != string(orchestration.JobStateFailed) {
			t.Errorf("job %v is %v, want only failed jobs", item["job_id"], item["state"])
		}
	}
	if agentSvc.listJobsFilter == nil || len(agentSvc.listJobsFilter.Statuses) != 1 {
		t.Error("the statuses filter never reached the service")
	}
}

// =============================================================================
// Tasks
// =============================================================================

func pagedTasks(n int) []*orchestration.Task {
	tasks := make([]*orchestration.Task, 0, n)
	for i := range n {
		tasks = append(tasks, &orchestration.Task{
			ID:        "task-" + strconv.Itoa(i),
			Subject:   "subject " + strconv.Itoa(i),
			Status:    orchestration.TaskStatusPending,
			ProjectID: "proj-a",
			CreatedAt: int64(i),
		})
	}
	return tasks
}

func TestListTasks_PagesAndCounts(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	tasks := pagedTasks(7)
	agentSvc.listTasksFunc = func(_ context.Context, _ string, _ *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return tasks, nil
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&limit=2&offset=2", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "tasks")
	assertPage(t, envelope, items, 2, 7, offsetPtr(4))
	if items[0]["id"] != "task-4" {
		t.Errorf("first task = %v, want task-4 (newest first, offset 2)", items[0]["id"])
	}
}

func TestListTasks_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	tasks := pagedTasks(3)
	agentSvc.listTasksFunc = func(_ context.Context, _ string, _ *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		return tasks, nil
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&sort=id", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "tasks")
	if items[0]["id"] != "task-0" {
		t.Errorf("first task = %v, want task-0", items[0]["id"])
	}

	refused := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(refused,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&sort=runner", nil, nil))

	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "runner") {
		t.Errorf("refusal %s does not name the offending field", refused.Body.String())
	}
}

func TestListTasks_SearchComposesWithTheStatusFilter(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()

	var captured *orchestration.TaskFilter
	agentSvc.listTasksFunc = func(_ context.Context, _ string, filter *orchestration.TaskFilter) ([]*orchestration.Task, error) {
		captured = filter
		matched := make([]*orchestration.Task, 0, 4)
		for _, task := range pagedTasks(4) {
			if filter.Status == "" || task.Status == filter.Status {
				matched = append(matched, task)
			}
		}
		return matched, nil
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListTasks(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/tasks?tenant_id=acme&status=pending&q=subject+3", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "tasks")
	if envelope.Total != 1 || items[0]["id"] != "task-3" {
		t.Errorf("matched %d tasks, want only task-3", envelope.Total)
	}
	if captured == nil || captured.Status != orchestration.TaskStatusPending {
		t.Error("the status filter never reached the service")
	}
}

// =============================================================================
// Sessions
// =============================================================================

func pagedSessions(n int) []*orchestration.SessionInfo {
	sessions := make([]*orchestration.SessionInfo, 0, n)
	for i := range n {
		backend := "claude"
		if i%2 == 1 {
			backend = "codex"
		}
		sessions = append(sessions, &orchestration.SessionInfo{
			SessionID: "sess-" + strconv.Itoa(i),
			Backend:   backend,
			Model:     "model-" + strconv.Itoa(i),
			State:     orchestration.SessionStateActive,
			CreatedAt: int64(i),
		})
	}
	return sessions
}

func TestListSessions_PagesAndCounts(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listSessions = pagedSessions(5)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id=acme&limit=2", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "sessions")
	assertPage(t, envelope, items, 2, 5, offsetPtr(2))
	// The sessions listing keeps its oldest-first natural order.
	if items[0]["session_id"] != "sess-0" {
		t.Errorf("first session = %v, want sess-0", items[0]["session_id"])
	}
}

func TestListSessions_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listSessions = pagedSessions(3)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id=acme&sort=created_at:desc", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "sessions")
	if items[0]["session_id"] != "sess-2" {
		t.Errorf("first session = %v, want sess-2 for a descending sort", items[0]["session_id"])
	}

	refused := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(refused,
		orchReq(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id=acme&sort=runner", nil, nil))

	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "runner") {
		t.Errorf("refusal %s does not name the offending field", refused.Body.String())
	}
}

func TestListSessions_SearchComposesWithTheBackendFilter(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listSessions = pagedSessions(6)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListSessions(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/sessions?tenant_id=acme&backend=codex&q=model-3", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "sessions")
	if envelope.Total != 1 || items[0]["session_id"] != "sess-3" {
		t.Errorf("matched %d sessions, want only sess-3 (codex AND model-3)", envelope.Total)
	}
}

// =============================================================================
// Agents
// =============================================================================

func pagedAgents(n int) []*orchestration.AgentRegistration {
	agents := make([]*orchestration.AgentRegistration, 0, n)
	for i := range n {
		agents = append(agents, &orchestration.AgentRegistration{
			ID:           "agent-" + strconv.Itoa(i),
			Name:         "burnin-" + strconv.Itoa(i),
			Backend:      "claude",
			Capabilities: []string{"codex"},
			RegisteredAt: int64(i),
		})
	}
	return agents
}

func TestListAgents_PagesAndCounts(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listAgents = pagedAgents(5)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListAgents(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/agents?tenant_id=acme&limit=2", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "agents")
	assertPage(t, envelope, items, 2, 5, offsetPtr(2))
	if items[0]["id"] != "agent-4" {
		t.Errorf("first agent = %v, want agent-4 (most recently registered)", items[0]["id"])
	}

	// "count" keeps meaning "records in this response" while "total" says how
	// many matched.
	var counted struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &counted); err != nil {
		t.Fatalf("decoding count: %v", err)
	}
	if counted.Count != 2 {
		t.Errorf("count = %d, want the 2 records in this response", counted.Count)
	}
}

func TestListAgents_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listAgents = pagedAgents(3)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListAgents(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/agents?tenant_id=acme&sort=name", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "agents")
	if items[0]["name"] != "burnin-0" {
		t.Errorf("first agent = %v, want burnin-0", items[0]["name"])
	}

	refused := httptest.NewRecorder()
	server.HandleOrchestratorListAgents(refused,
		orchReq(http.MethodGet, "/api/v1/orchestrator/agents?tenant_id=acme&sort=subject", nil, nil))

	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "subject") {
		t.Errorf("refusal %s does not name the offending field", refused.Body.String())
	}
}

func TestListAgents_SearchNarrowsTheFleet(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listAgents = pagedAgents(4)

	w := httptest.NewRecorder()
	server.HandleOrchestratorListAgents(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/agents?tenant_id=acme&q=burnin-2", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "agents")
	if envelope.Total != 1 || items[0]["id"] != "agent-2" {
		t.Errorf("matched %d agents, want only agent-2", envelope.Total)
	}

	// A capability the agent serves is searchable too, and every agent here
	// serves codex.
	all := httptest.NewRecorder()
	server.HandleOrchestratorListAgents(all,
		orchReq(http.MethodGet, "/api/v1/orchestrator/agents?tenant_id=acme&q=codex", nil, nil))
	allEnvelope, _ := decodePageEnvelope(t, all.Body.Bytes(), "agents")
	if allEnvelope.Total != 4 {
		t.Errorf("q=codex matched %d agents, want all 4 that serve it", allEnvelope.Total)
	}
}

// =============================================================================
// Workflows
// =============================================================================

func TestListWorkflows_PagesAndCounts(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listWorkflows = []*orchestration.WorkflowDefinition{
		{Name: "zeta"}, {Name: "alpha"}, {Name: "mid"},
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/workflows?tenant_id=acme&limit=2", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "workflows")
	assertPage(t, envelope, items, 2, 3, offsetPtr(2))
	if items[0]["name"] != "alpha" {
		t.Errorf("first workflow = %v, want alpha (by-name order)", items[0]["name"])
	}
}

func TestListWorkflows_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listWorkflows = []*orchestration.WorkflowDefinition{{Name: "alpha"}, {Name: "zeta"}}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/workflows?tenant_id=acme&sort=name:desc", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "workflows")
	if items[0]["name"] != "zeta" {
		t.Errorf("first workflow = %v, want zeta for a descending sort", items[0]["name"])
	}

	refused := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(refused,
		orchReq(http.MethodGet, "/api/v1/orchestrator/workflows?tenant_id=acme&sort=created_at", nil, nil))

	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "created_at") {
		t.Errorf("refusal %s does not name the offending field", refused.Body.String())
	}
}

func TestListWorkflows_SearchNarrows(t *testing.T) {
	server, agentSvc, cleanup := setupOrchestratorMockServer(t)
	defer cleanup()
	agentSvc.listWorkflows = []*orchestration.WorkflowDefinition{
		{Name: "sdlc-delivery"}, {Name: "triage"}, {Name: "sdlc-review"},
	}

	w := httptest.NewRecorder()
	server.HandleOrchestratorListWorkflows(w,
		orchReq(http.MethodGet, "/api/v1/orchestrator/workflows?tenant_id=acme&q=SDLC", nil, nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "workflows")
	if envelope.Total != 2 {
		t.Errorf("matched %d workflows, want the 2 sdlc ones", envelope.Total)
	}
	if items[0]["name"] != "sdlc-delivery" {
		t.Errorf("first workflow = %v, want sdlc-delivery", items[0]["name"])
	}
}

// =============================================================================
// Projects
// =============================================================================

// pagedProjectService answers the listing by running the REAL page helper
// over canned projects, exactly as the project service does over its store.
func pagedProjectService(t *testing.T, projects []*orchestration.Project, captured **service.ListProjectsRequest) *mocks.MockProjectService {
	t.Helper()

	return &mocks.MockProjectService{
		ListProjectsFunc: func(_ context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			if captured != nil {
				*captured = req
			}
			page, err := orchestration.ApplyPage(projects, req.Page, orchestration.ProjectPageSpec())
			if err != nil {
				return nil, err
			}
			return &service.ListProjectsResponse{
				Projects:   page.Items,
				Total:      page.Total,
				Limit:      page.Limit,
				Offset:     page.Offset,
				NextOffset: page.NextOffset,
				Timestamp:  time.Now().UTC(),
			}, nil
		},
	}
}

func pagedProjects(n int) []*orchestration.Project {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	projects := make([]*orchestration.Project, 0, n)
	for i := range n {
		projects = append(projects, &orchestration.Project{
			ID:        "proj-" + strconv.Itoa(i),
			TenantID:  "tenant-1",
			Name:      "service-" + strconv.Itoa(i),
			CreatedAt: created.Add(time.Duration(i) * time.Hour),
		})
	}
	return projects
}

// listProjects issues a projects request against a server wired with svc.
func listProjects(t *testing.T, svc service.ProjectService, query string) *httptest.ResponseRecorder {
	t.Helper()

	server := newProjectTestServer(svc)
	r := httptest.NewRequest(http.MethodGet, "/api/v1/projects"+query, nil)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, chi.NewRouteContext()))
	w := httptest.NewRecorder()
	server.HandleListProjects(w, r)
	return w
}

func TestListProjects_PagesAndCounts(t *testing.T) {
	w := listProjects(t, pagedProjectService(t, pagedProjects(5), nil), "?tenant_id=tenant-1&limit=2&offset=2")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "projects")
	assertPage(t, envelope, items, 2, 5, offsetPtr(4))
	if items[0]["id"] != "proj-2" {
		t.Errorf("first project = %v, want proj-2 (newest first, offset 2)", items[0]["id"])
	}
}

func TestListProjects_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	w := listProjects(t, pagedProjectService(t, pagedProjects(3), nil), "?tenant_id=tenant-1&sort=name")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "projects")
	if items[0]["name"] != "service-0" {
		t.Errorf("first project = %v, want service-0", items[0]["name"])
	}

	refused := listProjects(t, pagedProjectService(t, pagedProjects(3), nil), "?tenant_id=tenant-1&sort=runner")
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "runner") {
		t.Errorf("refusal %s does not name the offending field", refused.Body.String())
	}
}

func TestListProjects_SearchComposesWithTheTenant(t *testing.T) {
	var captured *service.ListProjectsRequest
	w := listProjects(t, pagedProjectService(t, pagedProjects(4), &captured), "?tenant_id=tenant-1&q=service-3")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "projects")
	if envelope.Total != 1 || items[0]["id"] != "proj-3" {
		t.Errorf("matched %d projects, want only proj-3", envelope.Total)
	}
	if captured == nil || captured.TenantID != "tenant-1" {
		t.Error("the tenant never reached the service")
	}
	if captured.Page == nil || captured.Page.Query != "service-3" {
		t.Error("the search never reached the service")
	}
}

// =============================================================================
// Accounts
// =============================================================================

func pagedAccounts(n int) []*orchestration.Account {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	accounts := make([]*orchestration.Account, 0, n)
	for i := range n {
		accounts = append(accounts, &orchestration.Account{
			ID:          "acct-" + strconv.Itoa(i),
			TenantID:    "tenant-1",
			DisplayName: "alice-" + strconv.Itoa(i),
			Provider:    orchestration.AccountProviderClaude,
			CreatedAt:   created.Add(time.Duration(i) * time.Hour),
		})
	}
	return accounts
}

// pagedAccountService pages canned accounts with the real page helper, and
// narrows by provider first so a search can be shown to compose with it.
func pagedAccountService(accounts []*orchestration.Account, captured **service.ListAccountsRequest) *mocks.MockAccountService {
	return &mocks.MockAccountService{
		ListAccountsFunc: func(_ context.Context, req *service.ListAccountsRequest) (*service.ListAccountsResponse, error) {
			if captured != nil {
				*captured = req
			}
			matched := make([]*orchestration.Account, 0, len(accounts))
			for _, account := range accounts {
				if req.Provider != "" && account.Provider != req.Provider {
					continue
				}
				matched = append(matched, account)
			}
			page, err := orchestration.ApplyPage(matched, req.Page, orchestration.AccountPageSpec())
			if err != nil {
				return nil, err
			}
			return &service.ListAccountsResponse{
				Accounts:   page.Items,
				Total:      page.Total,
				Limit:      page.Limit,
				Offset:     page.Offset,
				NextOffset: page.NextOffset,
				Timestamp:  time.Now().UTC(),
			}, nil
		},
	}
}

func TestListAccounts_PagesAndCounts(t *testing.T) {
	srv := newAccountTestServer(pagedAccountService(pagedAccounts(5), nil))

	w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts?tenant_id=tenant-1&limit=2", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, items := decodePageEnvelope(t, w.Body.Bytes(), "accounts")
	assertPage(t, envelope, items, 2, 5, offsetPtr(2))
	if items[0]["id"] != "acct-4" {
		t.Errorf("first account = %v, want acct-4 (newest first)", items[0]["id"])
	}
}

func TestListAccounts_SortsOnAllowListedFieldAndRefusesOthers(t *testing.T) {
	srv := newAccountTestServer(pagedAccountService(pagedAccounts(3), nil))

	w := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts?tenant_id=tenant-1&sort=display_name", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	_, items := decodePageEnvelope(t, w.Body.Bytes(), "accounts")
	if items[0]["display_name"] != "alice-0" {
		t.Errorf("first account = %v, want alice-0", items[0]["display_name"])
	}

	refused := doAccountRequest(srv, http.MethodGet, "/api/v1/agent-accounts?tenant_id=tenant-1&sort=runner", nil)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", refused.Code, refused.Body.String())
	}
	if !strings.Contains(refused.Body.String(), "runner") {
		t.Errorf("refusal %s does not name the offending field", refused.Body.String())
	}
}

func TestListAccounts_SearchComposesWithTheProviderFilter(t *testing.T) {
	accounts := pagedAccounts(4)
	accounts[1].Provider = orchestration.AccountProviderCodex
	var captured *service.ListAccountsRequest
	srv := newAccountTestServer(pagedAccountService(accounts, &captured))

	w := doAccountRequest(srv, http.MethodGet,
		"/api/v1/agent-accounts?tenant_id=tenant-1&provider=claude&q=alice", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	envelope, _ := decodePageEnvelope(t, w.Body.Bytes(), "accounts")
	if envelope.Total != 3 {
		t.Errorf("matched %d accounts, want the 3 claude ones the search also matched", envelope.Total)
	}
	if captured == nil || captured.Provider != orchestration.AccountProviderClaude {
		t.Error("the provider filter never reached the service")
	}
	if captured.Page == nil || captured.Page.Query != "alice" {
		t.Error("the search never reached the service")
	}
}
