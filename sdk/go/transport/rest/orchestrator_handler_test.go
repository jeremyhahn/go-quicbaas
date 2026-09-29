// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestOrchestratorHandlerHappyPaths drives every orchestrator method through
// the REST handler against a recording server, asserting the HTTP method and
// URL path each handler builds.
func TestOrchestratorHandlerHappyPaths(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		// A prompt must answer with a real turn: an empty body is a failed
		// turn and is rejected with a typed error, not an empty success.
		if strings.HasSuffix(r.URL.Path, "/prompt") {
			_, _ = w.Write([]byte(`{"session_id":"s1","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`))
			return
		}
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
	}{
		{"Health", "Health", nil, http.MethodGet, "/api/v1/orchestrator/health"},
		{"ListBackends", "ListBackends", nil, http.MethodGet, "/api/v1/orchestrator/backends"},
		{"ListRunners", "ListRunners", nil, http.MethodGet, "/api/v1/orchestrator/runners"},
		{"ListBackendModels", "ListBackendModels",
			&orchestrator.ListBackendModelsRequest{NodeID: "a1", BackendName: "ollama", TenantID: "t1"},
			http.MethodGet, "/api/v1/fleet/a1/backends/ollama/models"},
		{"ListBackendModelsDefaultAgent", "ListBackendModels",
			&orchestrator.ListBackendModelsRequest{BackendName: "ollama"},
			http.MethodGet, "/api/v1/fleet/any/backends/ollama/models"},
		{"SpawnSession", "SpawnSession",
			&orchestrator.SpawnSessionRequest{Backend: "ollama"},
			http.MethodPost, "/api/v1/orchestrator/sessions"},
		{"PromptSession", "PromptSession",
			&orchestrator.PromptSessionRequest{SessionID: "s1", Prompt: "hi"},
			http.MethodPost, "/api/v1/orchestrator/sessions/s1/prompt"},
		{"SessionStatus", "SessionStatus",
			&orchestrator.SessionStatusRequest{SessionID: "s1"},
			http.MethodGet, "/api/v1/orchestrator/sessions/s1"},
		{"CloseSession", "CloseSession",
			&orchestrator.CloseSessionRequest{SessionID: "s1"},
			http.MethodDelete, "/api/v1/orchestrator/sessions/s1"},
		{"ListSessions", "ListSessions",
			&orchestrator.ListSessionsRequest{Backend: "ollama", Status: "active"},
			http.MethodGet, "/api/v1/orchestrator/sessions"},
		{"ListSessionsNoFilter", "ListSessions",
			&orchestrator.ListSessionsRequest{},
			http.MethodGet, "/api/v1/orchestrator/sessions"},
		{"CreateTask", "CreateTask",
			&orchestrator.CreateTaskRequest{TenantID: "t1"},
			http.MethodPost, "/api/v1/orchestrator/tasks"},
		{"GetTask", "GetTask",
			&orchestrator.GetTaskRequest{TenantID: "t1", TaskID: "task1"},
			http.MethodGet, "/api/v1/orchestrator/tasks/task1"},
		{"ListTasks", "ListTasks",
			&orchestrator.ListTasksRequest{TenantID: "t1", Status: "open", TeamName: "team", AssignedTo: "agent"},
			http.MethodGet, "/api/v1/orchestrator/tasks"},
		{"ClaimTask", "ClaimTask",
			&orchestrator.ClaimTaskRequest{TenantID: "t1", TaskID: "task1", AgentID: "a1"},
			http.MethodPost, "/api/v1/orchestrator/tasks/task1/claim"},
		{"CompleteTask", "CompleteTask",
			&orchestrator.CompleteTaskRequest{TenantID: "t1", TaskID: "task1"},
			http.MethodPost, "/api/v1/orchestrator/tasks/task1/complete"},
		{"BlockTask", "BlockTask",
			&orchestrator.BlockTaskRequest{TenantID: "t1", TaskID: "task1", Kind: "blocked", Reason: "r"},
			http.MethodPost, "/api/v1/orchestrator/tasks/task1/block"},
		{"ReadmitTask", "ReadmitTask",
			&orchestrator.ReadmitTaskRequest{TenantID: "t1", TaskID: "task1"},
			http.MethodPost, "/api/v1/orchestrator/tasks/task1/readmit"},
		{"DeleteTask", "DeleteTask",
			&orchestrator.DeleteTaskRequest{TenantID: "t1", TaskID: "task1"},
			http.MethodDelete, "/api/v1/orchestrator/tasks/task1"},
		{"ListTasksProjectStatuses", "ListTasks",
			&orchestrator.ListTasksRequest{TenantID: "t1", ProjectID: "proj 1", Statuses: []string{"blocked", "pending"}},
			http.MethodGet, "/api/v1/orchestrator/tasks"},
		{"RegisterFleetNode", "RegisterFleetNode",
			&orchestrator.RegisterFleetNodeRequest{TenantID: "t1", Name: "n", Host: "h", NodeType: "worker"},
			http.MethodPost, "/api/v1/fleet"},
		{"GetFleetNode", "GetFleetNode",
			&orchestrator.GetFleetNodeRequest{TenantID: "t1", NodeID: "a1"},
			http.MethodGet, "/api/v1/fleet/a1"},
		{"ListFleetNodes", "ListFleetNodes",
			&orchestrator.ListFleetNodesRequest{TenantID: "t1", Host: "h", Status: "up", NodeType: "worker", Backend: "ollama"},
			http.MethodGet, "/api/v1/fleet"},
		{"RemoveFleetNode", "RemoveFleetNode",
			&orchestrator.RemoveFleetNodeRequest{TenantID: "t1", NodeID: "a1"},
			http.MethodDelete, "/api/v1/fleet/a1"},
		{"FleetNodeHeartbeat", "FleetNodeHeartbeat",
			&orchestrator.FleetNodeHeartbeatRequest{TenantID: "t1", NodeID: "a1"},
			http.MethodPost, "/api/v1/fleet/a1/heartbeat"},
		{"DecomposeTask", "DecomposeTask",
			&orchestrator.DecomposeTaskRequest{Backend: "ollama"},
			http.MethodPost, "/api/v1/orchestrator/tasks/decompose"},
		{"SpawnChildJob", "SpawnChildJob",
			&orchestrator.SpawnChildJobRequest{ParentJobID: "job1"},
			http.MethodPost, "/api/v1/orchestrator/jobs/job1/children"},
		{"StartJob", "StartJob",
			&orchestrator.StartJobRequest{TenantID: "t1", Description: "d"},
			http.MethodPost, "/api/v1/orchestrator/jobs"},
		{"GetJobStatus", "GetJobStatus",
			&orchestrator.GetJobStatusRequest{TenantID: "t1", JobID: "job1"},
			http.MethodGet, "/api/v1/orchestrator/jobs/job1"},
		{"ListJobs", "ListJobs",
			&orchestrator.ListJobsRequest{TenantID: "t1"},
			http.MethodGet, "/api/v1/orchestrator/jobs"},
		{"CancelJob", "CancelJob",
			&orchestrator.CancelJobRequest{TenantID: "t1", JobID: "job1"},
			http.MethodDelete, "/api/v1/orchestrator/jobs/job1"},
		{"DisableFleetNode", "DisableFleetNode",
			&orchestrator.DisableFleetNodeRequest{TenantID: "t1", NodeID: "a1"},
			http.MethodPost, "/api/v1/agents/a1/disable"},
		{"EnableFleetNode", "EnableFleetNode",
			&orchestrator.EnableFleetNodeRequest{TenantID: "t1", NodeID: "a1"},
			http.MethodPost, "/api/v1/agents/a1/enable"},
		{"SubmitTaskArtifacts", "SubmitTaskArtifacts",
			&orchestrator.SubmitTaskArtifactsRequest{TenantID: "t1", TaskID: "task1",
				Artifacts: []*orchestrator.TaskArtifact{{Name: "out"}}},
			http.MethodPost, "/api/v1/orchestrator/tasks/task1/artifacts"},
		{"GetTaskArtifact", "GetTaskArtifact",
			&orchestrator.GetTaskArtifactRequest{TenantID: "t1", TaskID: "task1", Name: "out"},
			http.MethodGet, "/api/v1/orchestrator/tasks/task1/artifacts/out"},
		{"ListTaskArtifacts", "ListTaskArtifacts",
			&orchestrator.ListTaskArtifactsRequest{TenantID: "t1", TaskID: "task1"},
			http.MethodGet, "/api/v1/orchestrator/tasks/task1/artifacts"},
		{"Inference", "Inference",
			&orchestrator.InferenceRequest{Backend: "ollama", Prompt: "hi"},
			http.MethodPost, "/api/v1/orchestrator/inference"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath = "", ""
			resp, err := tr.Request(ctx, "Orchestrator", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path: want %s, got %s", tc.wantPath, gotPath)
			}
		})
	}
}

// TestOrchestratorHandlerInvalidRequestType exercises the type-assertion error
// branch on every handler that requires a typed request.
func TestOrchestratorHandlerInvalidRequestType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server should not be reached for invalid request type, path %s", r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	methods := []string{
		"ListBackendModels", "SpawnSession", "PromptSession", "SessionStatus",
		"CloseSession", "ListSessions", "CreateTask", "GetTask", "ListTasks",
		"ClaimTask", "CompleteTask", "BlockTask", "ReadmitTask", "RegisterFleetNode", "GetFleetNode", "ListFleetNodes",
		"RemoveFleetNode", "FleetNodeHeartbeat", "DecomposeTask", "SpawnChildJob",
		"StartJob", "GetJobStatus", "ListJobs", "CancelJob", "DisableFleetNode",
		"EnableFleetNode", "SubmitTaskArtifacts", "GetTaskArtifact", "ListTaskArtifacts",
		"Inference",
	}

	for _, m := range methods {
		t.Run(m, func(t *testing.T) {
			// A bare string is never the expected typed pointer.
			_, err := tr.Request(ctx, "Orchestrator", m, "not-the-right-type")
			if err == nil {
				t.Fatalf("expected invalid-input error for method %s", m)
			}
		})
	}
}

// TestOrchestratorHandlerSubscribeJobEvents verifies the streaming method
// returns an unimplemented error over REST.
func TestOrchestratorHandlerSubscribeJobEvents(t *testing.T) {
	tr := newMappingTransport(t)
	_, err := tr.Request(context.Background(), "Orchestrator", "SubscribeJobEvents",
		&orchestrator.SubscribeJobEventsRequest{TenantID: "t1", JobID: "j1"})
	if err == nil {
		t.Fatal("expected unimplemented error for SubscribeJobEvents over REST")
	}
}

// TestOrchestratorHandlerUnknownMethodFallback verifies that an unrecognized
// method falls through to the generic retry path.
func TestOrchestratorHandlerUnknownMethodFallback(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Orchestrator", "CustomMethod", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/orchestrator/custommethod" {
		t.Errorf("unexpected fallback path: %s", gotPath)
	}
}

// TestOrchestratorHandlerServerError exercises the doJSONRequest error branch.
func TestOrchestratorHandlerServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Orchestrator", "Health", nil)
	if err == nil {
		t.Fatal("expected error from server 500")
	}
}
