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

// Package workflowcrud is the shared fixture for the workflow CRUD
// round-trip tests: every transport must carry a workflow definition from
// the SDK client to the agent service and back with every field intact, and
// must report not-found, duplicate and invalid definitions as typed SDK
// errors. Each transport's test wires a Store behind its own server and runs
// Exercise through the SDK orchestrator client.
package workflowcrud

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// TenantID is the tenant every fixture request names. gRPC tests bind the
// same tenant on the server side.
const TenantID = "tenant-1"

// HooksName is the lifecycle hook set the fixture definition names. A
// definition naming an unregistered set fails validation, so Definition
// registers it once per process.
const HooksName = "workflowcrud-fixture"

var registerHooks sync.Once

// Definition returns a valid definition with every field set, so a field a
// transport drops shows up as a mismatch.
func Definition(t testing.TB) *orchestrator.WorkflowDefinition {
	t.Helper()
	registerHooks.Do(func() {
		if err := orchestration.RegisterJobHooks(HooksName, noHooks{}); err != nil {
			t.Fatalf("register fixture hooks: %v", err)
		}
	})
	return &orchestrator.WorkflowDefinition{
		Name:        "review-loop",
		Description: "lead plans, coder builds, reviewer gates",
		Mode:        orchestrator.WorkflowModeConstrained,
		Agents: []orchestrator.AgentRoleSpec{{
			Role:            "lead",
			Backend:         "claude",
			Model:           "opus",
			Capabilities:    []string{"plan", "review"},
			MCPScopes:       []string{"orchestrator", "kv"},
			Count:           1,
			Env:             map[string]string{"LOG_LEVEL": "debug"},
			WorkspacePath:   "/workspace/lead",
			PermissionMode:  "plan",
			SkipPermissions: true,
			Sandboxed:       true,
			ContainerImage:  "registry.example/agent:1",
			HostAffinity:    "gpu-node",
			NumCtx:          32768,
			PromptAppend:    "Keep commits small.",
		}, {
			Role:    "coder",
			Backend: "codex",
			Count:   2,
		}},
		Settings: orchestrator.WorkflowSettings{
			MaxIterations:  5,
			MaxParallelism: 3,
			PollInterval:   "5s",
			TimeoutPerItem: "10m",
			TotalTimeout:   "1h",
			ReviewCriteria: []orchestrator.ReviewCriterion{{
				Name:      "tests",
				Validator: "command",
				Config:    "make test",
				Required:  true,
				Phase:     "validating",
			}},
			Topology:  "hub_and_spoke",
			Isolation: "worktree",
			PeerMessaging: orchestrator.PeerMessagingSettings{
				MaxMessageSize: 4096,
				MaxInboxSize:   64,
				OverflowAction: "drop_oldest",
				InboxTTL:       "30m",
			},
			FileLocking: orchestrator.FileLockSettings{
				LeaseTTL:          "2m",
				HeartbeatInterval: "20s",
				MaxRenewals:       10,
			},
			VectorDB: orchestrator.VectorDBSettings{
				Enabled:        true,
				IndexType:      "hnsw",
				AutoIndex:      true,
				Triggers:       []string{"commit"},
				GPUAccelerated: true,
			},
			CostControls: orchestrator.CostControlSettings{
				MaxTokensPerJob:   1_000_000,
				MaxTokensPerAgent: 250_000,
				BudgetAction:      "pause",
			},
			Recovery: orchestrator.RecoverySettings{
				AutoRestart:    true,
				MaxRestarts:    3,
				RestartBackoff: "15s",
			},
			Verbosity: "verbose",
			NumCtx:    8192,
			Hooks:     HooksName,
		},
	}
}

// Invalid returns a definition the agent service rejects: it names no agents.
func Invalid() *orchestrator.WorkflowDefinition {
	return &orchestrator.WorkflowDefinition{Name: "no-agents", Mode: orchestrator.WorkflowModeAutonomous}
}

// noHooks satisfies JobLifecycleHooks for registration; the fixture never
// runs a job, so no moment is ever called.
type noHooks struct {
	orchestration.JobLifecycleHooks
}

// Store is a QuicBaaS whose agent service keeps workflows in a real
// orchestration.WorkflowLoader. Every other service behaves as the testutil
// mock does.
type Store struct {
	*testutil.MockQuicBaaS
	agent *loaderAgent
}

// NewStore returns a Store with no workflows registered.
func NewStore() *Store {
	return &Store{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		agent:        &loaderAgent{loader: orchestration.NewWorkflowLoader()},
	}
}

// Agent returns the loader-backed agent service.
func (s *Store) Agent() orchestration.AgentService {
	return s.agent
}

// loaderAgent implements only the workflow CRUD verbs; the embedded
// interface is nil, so any other call fails the test loudly.
type loaderAgent struct {
	orchestration.AgentService
	loader *orchestration.WorkflowLoader
}

func (a *loaderAgent) RegisterWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return a.loader.Register(orchestration.StorageTenant(ctx), def)
}

func (a *loaderAgent) GetWorkflow(ctx context.Context, name string) (*orchestration.WorkflowDefinition, error) {
	return a.loader.Get(orchestration.StorageTenant(ctx), name)
}

func (a *loaderAgent) UpdateWorkflow(ctx context.Context, def *orchestration.WorkflowDefinition) error {
	return a.loader.Update(orchestration.StorageTenant(ctx), def)
}

func (a *loaderAgent) DeleteWorkflow(ctx context.Context, name string) error {
	return a.loader.Delete(orchestration.StorageTenant(ctx), name)
}

const workflowsPath = "/api/v1/orchestrator/workflows"

// HTTPHandler serves the REST workflow routes over agent with the server's
// contract: POST creates from {tenant_id, name, definition} and answers 201,
// GET and PUT answer {"workflow": ...}, DELETE answers {"name", "deleted"},
// and a missing, duplicate or invalid workflow answers 404, 409 or 400.
func HTTPHandler(agent orchestration.AgentService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := orchestration.ContextWithTenant(r.Context(), r.URL.Query().Get("tenant_id"))
		name := strings.TrimPrefix(r.URL.Path, workflowsPath+"/")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == workflowsPath:
			var body struct {
				TenantID   string                            `json:"tenant_id"`
				Name       string                            `json:"name"`
				Definition *orchestration.WorkflowDefinition `json:"definition"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Definition == nil {
				writeError(w, http.StatusBadRequest, "invalid body")
				return
			}
			body.Definition.Name = body.Name
			ctx = orchestration.ContextWithTenant(r.Context(), body.TenantID)
			if err := agent.RegisterWorkflow(ctx, body.Definition); err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"workflow": body.Definition})
		case r.Method == http.MethodGet:
			def, err := agent.GetWorkflow(ctx, name)
			if err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"workflow": def})
		case r.Method == http.MethodPut:
			var def orchestration.WorkflowDefinition
			if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
				writeError(w, http.StatusBadRequest, "invalid body")
				return
			}
			def.Name = name
			if err := agent.UpdateWorkflow(ctx, &def); err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"workflow": &def})
		case r.Method == http.MethodDelete:
			if err := agent.DeleteWorkflow(ctx, name); err != nil {
				writeAgentError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"name": name, "deleted": true})
		default:
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	})
}

func writeAgentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, orchestration.ErrWorkflowNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, orchestration.ErrWorkflowAlreadyExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, orchestration.ErrWorkflowValidation):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Exercise runs every workflow CRUD verb through client and checks each
// definition arrives whole. isDuplicate recognises the transport's error for
// creating a workflow that exists: REST and QUIC answer Conflict, gRPC and
// embedded AlreadyExists.
func Exercise(t *testing.T, client orchestrator.Client, isDuplicate func(error) bool) {
	t.Helper()
	ctx := context.Background()
	want := Definition(t)

	created, err := client.CreateWorkflow(ctx, &orchestrator.CreateWorkflowRequest{TenantID: TenantID, Definition: want})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	requireEqual(t, "CreateWorkflow", created, want)

	got, err := client.GetWorkflow(ctx, &orchestrator.GetWorkflowRequest{TenantID: TenantID, Name: want.Name})
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	requireEqual(t, "GetWorkflow", got, want)

	changed := Definition(t)
	changed.Description = "reviewer now gates twice"
	changed.Settings.MaxIterations = 9
	updated, err := client.UpdateWorkflow(ctx, &orchestrator.UpdateWorkflowRequest{TenantID: TenantID, Name: want.Name, Definition: changed})
	if err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	requireEqual(t, "UpdateWorkflow", updated, changed)
	got, err = client.GetWorkflow(ctx, &orchestrator.GetWorkflowRequest{TenantID: TenantID, Name: want.Name})
	if err != nil {
		t.Fatalf("GetWorkflow after update: %v", err)
	}
	requireEqual(t, "GetWorkflow after update", got, changed)

	_, err = client.CreateWorkflow(ctx, &orchestrator.CreateWorkflowRequest{TenantID: TenantID, Definition: want})
	requireError(t, "duplicate CreateWorkflow", err, isDuplicate)
	_, err = client.CreateWorkflow(ctx, &orchestrator.CreateWorkflowRequest{TenantID: TenantID, Definition: Invalid()})
	requireError(t, "invalid CreateWorkflow", err, sdkerrors.IsInvalidInput)
	_, err = client.UpdateWorkflow(ctx, &orchestrator.UpdateWorkflowRequest{TenantID: TenantID, Name: want.Name, Definition: Invalid()})
	requireError(t, "invalid UpdateWorkflow", err, sdkerrors.IsInvalidInput)
	_, err = client.GetWorkflow(ctx, &orchestrator.GetWorkflowRequest{TenantID: TenantID})
	requireError(t, "nameless GetWorkflow", err, sdkerrors.IsInvalidInput)
	err = client.DeleteWorkflow(ctx, &orchestrator.DeleteWorkflowRequest{TenantID: TenantID})
	requireError(t, "nameless DeleteWorkflow", err, sdkerrors.IsInvalidInput)

	if err := client.DeleteWorkflow(ctx, &orchestrator.DeleteWorkflowRequest{TenantID: TenantID, Name: want.Name}); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}
	_, err = client.GetWorkflow(ctx, &orchestrator.GetWorkflowRequest{TenantID: TenantID, Name: want.Name})
	requireError(t, "GetWorkflow after delete", err, sdkerrors.IsNotFound)
	_, err = client.UpdateWorkflow(ctx, &orchestrator.UpdateWorkflowRequest{TenantID: TenantID, Name: want.Name, Definition: changed})
	requireError(t, "UpdateWorkflow of a missing workflow", err, sdkerrors.IsNotFound)
	err = client.DeleteWorkflow(ctx, &orchestrator.DeleteWorkflowRequest{TenantID: TenantID, Name: want.Name})
	requireError(t, "DeleteWorkflow of a missing workflow", err, sdkerrors.IsNotFound)
}

func requireEqual(t *testing.T, verb string, got, want *orchestrator.WorkflowDefinition) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: definition did not survive the transport:\n got  %#v\n want %#v", verb, got, want)
	}
}

func requireError(t *testing.T, verb string, err error, is func(error) bool) {
	t.Helper()
	if err == nil || !is(err) {
		t.Fatalf("%s: got error %v (code %q), want a different typed error", verb, err, sdkerrors.Code(err))
	}
}
