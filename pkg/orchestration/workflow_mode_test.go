package orchestration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #622: the "canned" workflow mode was declared and documented but
// never built — the lead decomposed the task in every mode. Ruling 189
// deleted it, so a canned workflow is refused at validation, the refusal
// names the modes that exist, and a workflow in a mode that does exist
// still runs with the lead decomposing the task.

// writeWorkflowYAML writes body to a workflow file in a fresh directory.
func writeWorkflowYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "workflow.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	return path
}

func TestWorkflowLoader_CannedModeIsRefusedNamingTheModes(t *testing.T) {
	t.Parallel()
	path := writeWorkflowYAML(t, `name: predefined
mode: canned
agents:
  - role: lead
    backend: ollama
`)
	loader := NewWorkflowLoader()
	def, err := loader.LoadFromFile(SystemTenantID, path)
	if err == nil {
		t.Fatalf("a canned workflow must be refused, loaded %+v", def)
	}
	var verr *WorkflowValidationError
	if !errors.As(err, &verr) || verr.Field != "mode" {
		t.Fatalf("err = %v, want a validation error on the mode", err)
	}
	for _, want := range []string{`"canned"`, string(WorkflowModeConstrained), string(WorkflowModeAutonomous)} {
		if !strings.Contains(verr.Message, want) {
			t.Fatalf("refusal %q must name %s", verr.Message, want)
		}
	}
	if got, err := loader.Get(SystemTenantID, "predefined"); err == nil && got != nil {
		t.Fatal("a refused workflow must not be registered")
	}
}

func TestControlLoop_ConstrainedWorkflowFromYAMLDecomposes(t *testing.T) {
	t.Parallel()
	path := writeWorkflowYAML(t, `name: constrained
mode: constrained
agents:
  - role: lead
    backend: ollama
`)
	def, err := NewWorkflowLoader().LoadFromFile(SystemTenantID, path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	svc := leadDrivenService(`{"approved": true, "feedback": "all good"}`)
	decompose := svc.decomposeFn
	decomposeCalls := 0
	svc.decomposeFn = func() (*DecomposeTaskResponse, error) {
		decomposeCalls++
		return decompose()
	}

	run, err := runLaunch(t, jobLaunch{Agents: svc, Workflow: def})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if status := run.Status(); status.State != JobStateCompleted {
		t.Fatalf("status = %+v, want completed", status)
	}
	if decomposeCalls == 0 {
		t.Fatal("a constrained workflow is lead-decomposed: DecomposeTask must be called")
	}
}

// TestWorkflowLoader_StoredCannedWorkflowRunsConstrained is the upgrade
// regression: a canned definition stored before the mode was deleted is read
// back by Rehydrate and RehydrateAll (the boot and the resume path) as
// constrained, so the control loop keeps its workflow-authoritative role and
// backend restriction instead of treating it as unconstrained.
func TestWorkflowLoader_StoredCannedWorkflowRunsConstrained(t *testing.T) {
	t.Parallel()
	stored := func() *WorkflowDefinition {
		return &WorkflowDefinition{
			Name:     "legacy",
			TenantID: "acme",
			Mode:     WorkflowMode("canned"),
			Agents: []AgentRoleSpec{
				{Role: "lead", Backend: "ollama"},
				{Role: "coder", Backend: "claude"},
			},
		}
	}
	rehydrators := map[string]func(*WorkflowLoader) error{
		"Rehydrate": func(wl *WorkflowLoader) error {
			_, err := wl.Rehydrate(context.Background(), "acme")
			return err
		},
		"RehydrateAll": func(wl *WorkflowLoader) error {
			_, err := wl.RehydrateAll(context.Background())
			return err
		},
	}
	for name, rehydrate := range rehydrators {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			persistor := newMemWorkflowPersistor()
			persistor.saved["legacy"] = stored()
			loader := NewWorkflowLoader()
			loader.SetPersistor(persistor)
			if err := rehydrate(loader); err != nil {
				t.Fatalf("rehydrate: %v", err)
			}
			got, err := loader.Get("acme", "legacy")
			if err != nil {
				t.Fatalf("get: %v", err)
			}
			if got.Mode != WorkflowModeConstrained {
				t.Fatalf("mode = %q, want %q", got.Mode, WorkflowModeConstrained)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("the migrated definition must validate: %v", err)
			}

			cl := &ControlLoop{workflow: got}
			if !cl.isConstrainedWorkflowMode() {
				t.Fatal("a stored canned workflow must stay constrained")
			}
			item := &WorkItem{RequiredRole: "coder", Backend: "llm-picked"}
			if backend := cl.resolveBackendForMode(item, nil); backend != "claude" {
				t.Fatalf("backend = %q, want the workflow's coder backend claude", backend)
			}
		})
	}
}
