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
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// WorkflowDefinition.Validate Tests
// =============================================================================

func TestWorkflowDefinition_Validate_Valid(t *testing.T) {
	tests := []struct {
		name string
		def  WorkflowDefinition
	}{
		{
			name: "constrained with lead and workers",
			def: WorkflowDefinition{
				Name: "test-workflow",
				Mode: WorkflowModeConstrained,
				Agents: []AgentRoleSpec{
					{Role: "lead", Backend: "claude", Model: "claude-sonnet"},
					{Role: "coder", Backend: "claude", Model: "claude-sonnet"},
				},
			},
		},
		{
			name: "canned with lead only",
			def: WorkflowDefinition{
				Name: "canned-workflow",
				Mode: WorkflowModeCanned,
				Agents: []AgentRoleSpec{
					{Role: "lead", Backend: "ollama", Model: "llama3"},
				},
			},
		},
		{
			name: "autonomous with lead only",
			def: WorkflowDefinition{
				Name: "autonomous-workflow",
				Mode: WorkflowModeAutonomous,
				Agents: []AgentRoleSpec{
					{Role: "lead", Backend: "claude", Model: "claude-sonnet"},
				},
			},
		},
		{
			name: "autonomous without lead role",
			def: WorkflowDefinition{
				Name: "autonomous-no-lead",
				Mode: WorkflowModeAutonomous,
				Agents: []AgentRoleSpec{
					{Role: "worker", Backend: "ollama", Model: "llama3"},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestWorkflowDefinition_Validate_Invalid(t *testing.T) {
	tests := []struct {
		name      string
		def       WorkflowDefinition
		wantField string
	}{
		{
			name:      "empty name",
			def:       WorkflowDefinition{Mode: WorkflowModeConstrained, Agents: []AgentRoleSpec{{Role: "lead"}}},
			wantField: "name",
		},
		{
			name:      "invalid mode",
			def:       WorkflowDefinition{Name: "test", Mode: "bogus", Agents: []AgentRoleSpec{{Role: "lead"}}},
			wantField: "mode",
		},
		{
			name:      "no agents",
			def:       WorkflowDefinition{Name: "test", Mode: WorkflowModeConstrained},
			wantField: "agents",
		},
		{
			name: "constrained missing lead",
			def: WorkflowDefinition{
				Name: "test",
				Mode: WorkflowModeConstrained,
				Agents: []AgentRoleSpec{
					{Role: "coder", Backend: "claude"},
				},
			},
			wantField: "agents",
		},
		{
			name: "canned missing lead",
			def: WorkflowDefinition{
				Name: "test",
				Mode: WorkflowModeCanned,
				Agents: []AgentRoleSpec{
					{Role: "worker", Backend: "ollama"},
				},
			},
			wantField: "agents",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.Validate()
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrWorkflowValidation))

			var valErr *WorkflowValidationError
			require.True(t, errors.As(err, &valErr))
			assert.Equal(t, tc.wantField, valErr.Field)
		})
	}
}

// =============================================================================
// WorkflowLoader.LoadFromFile Tests
// =============================================================================

func TestWorkflowLoader_LoadFromFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	content := `name: test-workflow
description: "A test workflow"
mode: constrained
agents:
  - role: lead
    backend: claude
    model: claude-sonnet-4-5-20250514
    capabilities: [persistent_sessions, tool_use]
    mcp_scopes: [orchestrator:read, orchestrator:write]
  - role: coder
    backend: claude
    model: claude-sonnet-4-5-20250514
    capabilities: [persistent_sessions, tool_use, file_access]
    mcp_scopes: [kms:read]
    count: 2
settings:
  max_iterations: 10
  max_parallelism: 3
  poll_interval: 5s
  timeout_per_item: 10m
  total_timeout: 30m
  review_criteria:
    - name: tests_pass
      validator: command
      config: "make test"
      required: true
`
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)

	loader := NewWorkflowLoader()
	def, err := loader.LoadFromFile(SystemTenantID, path)
	require.NoError(t, err)

	// Verify all fields parsed correctly.
	assert.Equal(t, "test-workflow", def.Name)
	assert.Equal(t, "A test workflow", def.Description)
	assert.Equal(t, WorkflowModeConstrained, def.Mode)

	require.Len(t, def.Agents, 2)

	lead := def.Agents[0]
	assert.Equal(t, "lead", lead.Role)
	assert.Equal(t, "claude", lead.Backend)
	assert.Equal(t, "claude-sonnet-4-5-20250514", lead.Model)
	assert.Equal(t, []string{"persistent_sessions", "tool_use"}, lead.Capabilities)
	assert.Equal(t, []string{"orchestrator:read", "orchestrator:write"}, lead.MCPScopes)

	coder := def.Agents[1]
	assert.Equal(t, "coder", coder.Role)
	assert.Equal(t, 2, coder.Count)
	assert.Equal(t, []string{"kms:read"}, coder.MCPScopes)

	assert.Equal(t, 10, def.Settings.MaxIterations)
	assert.Equal(t, 3, def.Settings.MaxParallelism)
	assert.Equal(t, "5s", def.Settings.PollInterval)
	assert.Equal(t, "10m", def.Settings.TimeoutPerItem)
	assert.Equal(t, "30m", def.Settings.TotalTimeout)

	require.Len(t, def.Settings.ReviewCriteria, 1)
	assert.Equal(t, "tests_pass", def.Settings.ReviewCriteria[0].Name)
	assert.Equal(t, "command", def.Settings.ReviewCriteria[0].Validator)
	assert.Equal(t, "make test", def.Settings.ReviewCriteria[0].Config)
	assert.True(t, def.Settings.ReviewCriteria[0].Required)

	// Verify it was stored in the loader.
	got, err := loader.Get(SystemTenantID, "test-workflow")
	require.NoError(t, err)
	assert.Equal(t, def, got)
}

func TestWorkflowLoader_LoadFromFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	err := os.WriteFile(path, []byte("{{invalid yaml:::"), 0o600)
	require.NoError(t, err)

	loader := NewWorkflowLoader()
	_, err = loader.LoadFromFile(SystemTenantID, path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowParse))
}

func TestWorkflowLoader_LoadFromFile_RefusesRetiredSystemPrompt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retired.yaml")
	doc := "name: retired\nmode: autonomous\nagents:\n" +
		"  - role: lead\n    backend: claude\n" +
		"  - role: worker\n    backend: claude\n    system_prompt: be careful\n"
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))

	loader := NewWorkflowLoader()
	_, err := loader.LoadFromFile(SystemTenantID, path)

	require.ErrorIs(t, err, ErrWorkflowRetiredField)
	var retired *RetiredFieldError
	require.ErrorAs(t, err, &retired)
	assert.Equal(t, path, retired.Path)
	assert.Equal(t, "agents[1].system_prompt", retired.Field)
	assert.Contains(t, err.Error(), "prompts store")
	_, getErr := loader.Get(SystemTenantID, "retired")
	assert.Error(t, getErr, "a refused definition must not be stored")
}

func TestWorkflowLoader_LoadFromFile_LoadsWithoutRetiredFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean.yaml")
	doc := "name: clean\nmode: autonomous\nagents:\n" +
		"  - role: worker\n    backend: claude\n    prompt_append: be careful\n"
	require.NoError(t, os.WriteFile(path, []byte(doc), 0o600))

	def, err := NewWorkflowLoader().LoadFromFile(SystemTenantID, path)

	require.NoError(t, err)
	assert.Equal(t, "be careful", def.Agents[0].PromptAppend)
}

func TestWorkflowLoader_LoadFromFile_MissingFile(t *testing.T) {
	loader := NewWorkflowLoader()
	_, err := loader.LoadFromFile(SystemTenantID, "/nonexistent/path/workflow.yaml")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowLoad))
}

func TestWorkflowLoader_LoadFromFile_ValidationFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "invalid.yaml")

	// Valid YAML but missing required name field.
	content := `mode: constrained
agents:
  - role: lead
    backend: claude
`
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)

	loader := NewWorkflowLoader()
	_, err = loader.LoadFromFile(SystemTenantID, path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowValidation))
}

// =============================================================================
// WorkflowLoader.LoadFromDir Tests
// =============================================================================

func TestWorkflowLoader_LoadFromDir_LoadsAllYAML(t *testing.T) {
	dir := t.TempDir()

	workflows := map[string]string{
		"a.yaml": `name: alpha
mode: autonomous
agents:
  - role: lead
    backend: claude
    model: claude-sonnet
`,
		"b.yml": `name: bravo
mode: constrained
agents:
  - role: lead
    backend: ollama
    model: llama3
`,
		"ignore.txt": "not a workflow",
	}

	for name, content := range workflows {
		err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
		require.NoError(t, err)
	}

	// Create a subdirectory that should be skipped.
	err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755)
	require.NoError(t, err)

	loader := NewWorkflowLoader()
	err = loader.LoadFromDir(SystemTenantID, dir)
	require.NoError(t, err)

	names := loader.List(SystemTenantID)
	assert.Equal(t, []string{"alpha", "bravo"}, names)
}

func TestWorkflowLoader_LoadFromDir_NonexistentDir(t *testing.T) {
	loader := NewWorkflowLoader()
	err := loader.LoadFromDir(SystemTenantID, "/nonexistent/directory")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowLoad))
}

func TestWorkflowLoader_LoadFromDir_InvalidFileInDir(t *testing.T) {
	dir := t.TempDir()

	// First file is valid.
	err := os.WriteFile(filepath.Join(dir, "good.yaml"), []byte(`name: good
mode: autonomous
agents:
  - role: lead
    backend: claude
`), 0o600)
	require.NoError(t, err)

	// Second file has invalid YAML.
	err = os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("{{broken"), 0o600)
	require.NoError(t, err)

	loader := NewWorkflowLoader()
	err = loader.LoadFromDir(SystemTenantID, dir)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowParse))
}

// =============================================================================
// WorkflowLoader.Register + Get Tests
// =============================================================================

func TestWorkflowLoader_Register_Get_RoundTrip(t *testing.T) {
	loader := NewWorkflowLoader()

	def := &WorkflowDefinition{
		Name: "my-workflow",
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: "claude", Model: "claude-sonnet"},
		},
		Settings: WorkflowSettings{
			MaxIterations: 5,
			PollInterval:  "10s",
		},
	}

	err := loader.Register(SystemTenantID, def)
	require.NoError(t, err)

	got, err := loader.Get(SystemTenantID, "my-workflow")
	require.NoError(t, err)
	assert.Equal(t, def, got)
}

func TestWorkflowLoader_Register_DuplicateName(t *testing.T) {
	loader := NewWorkflowLoader()

	def := &WorkflowDefinition{
		Name: "dup",
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: "claude", Model: "claude-sonnet"},
		},
	}

	err := loader.Register(SystemTenantID, def)
	require.NoError(t, err)

	err = loader.Register(SystemTenantID, def)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowAlreadyExists))
}

func TestWorkflowLoader_Register_InvalidDefinition(t *testing.T) {
	loader := NewWorkflowLoader()

	def := &WorkflowDefinition{
		Name: "", // missing name
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: "claude"},
		},
	}

	err := loader.Register(SystemTenantID, def)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowValidation))
}

// =============================================================================
// WorkflowLoader.Get Error Tests
// =============================================================================

func TestWorkflowLoader_Get_NotFound(t *testing.T) {
	loader := NewWorkflowLoader()

	_, err := loader.Get(SystemTenantID, "nonexistent")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowNotFound))

	var nfErr *WorkflowNotFoundError
	require.True(t, errors.As(err, &nfErr))
	assert.Equal(t, "nonexistent", nfErr.Name)
}

// =============================================================================
// WorkflowLoader.List Tests
// =============================================================================

func TestWorkflowLoader_List_Empty(t *testing.T) {
	loader := NewWorkflowLoader()
	names := loader.List(SystemTenantID)
	assert.Empty(t, names)
}

func TestWorkflowLoader_List_SortedAlphabetically(t *testing.T) {
	loader := NewWorkflowLoader()

	defs := []*WorkflowDefinition{
		{Name: "zebra", Mode: WorkflowModeAutonomous, Agents: []AgentRoleSpec{{Role: "lead"}}},
		{Name: "alpha", Mode: WorkflowModeAutonomous, Agents: []AgentRoleSpec{{Role: "lead"}}},
		{Name: "middle", Mode: WorkflowModeAutonomous, Agents: []AgentRoleSpec{{Role: "lead"}}},
	}

	for _, def := range defs {
		err := loader.Register(SystemTenantID, def)
		require.NoError(t, err)
	}

	names := loader.List(SystemTenantID)
	assert.Equal(t, []string{"alpha", "middle", "zebra"}, names)
}

func TestWorkflowLoader_ListDefinitions_SortedByName(t *testing.T) {
	loader := NewWorkflowLoader()

	defs := []*WorkflowDefinition{
		{Name: "zebra", Description: "Z workflow", Mode: WorkflowModeAutonomous, Agents: []AgentRoleSpec{{Role: "lead"}}},
		{Name: "alpha", Description: "A workflow", Mode: WorkflowModeConstrained, Agents: []AgentRoleSpec{{Role: "lead"}, {Role: "worker"}}},
		{Name: "middle", Description: "M workflow", Mode: WorkflowModeCanned, Agents: []AgentRoleSpec{{Role: "lead"}}},
	}

	for _, def := range defs {
		err := loader.Register(SystemTenantID, def)
		require.NoError(t, err)
	}

	result := loader.ListDefinitions(SystemTenantID)
	require.Len(t, result, 3)
	assert.Equal(t, "alpha", result[0].Name)
	assert.Equal(t, "middle", result[1].Name)
	assert.Equal(t, "zebra", result[2].Name)
	assert.Equal(t, WorkflowModeConstrained, result[0].Mode)
	assert.Len(t, result[0].Agents, 2)
}

func TestWorkflowLoader_ListDefinitions_Empty(t *testing.T) {
	loader := NewWorkflowLoader()
	result := loader.ListDefinitions(SystemTenantID)
	assert.Empty(t, result)
}

// =============================================================================
// Workflow Error Type Tests
// =============================================================================

func TestWorkflowErrors_Unwrap(t *testing.T) {
	t.Run("WorkflowValidationError", func(t *testing.T) {
		err := &WorkflowValidationError{Field: "name", Message: "required"}
		assert.True(t, errors.Is(err, ErrWorkflowValidation))
		assert.Contains(t, err.Error(), "name")
		assert.Contains(t, err.Error(), "required")
	})

	t.Run("WorkflowNotFoundError", func(t *testing.T) {
		err := &WorkflowNotFoundError{Name: "missing"}
		assert.True(t, errors.Is(err, ErrWorkflowNotFound))
		assert.Contains(t, err.Error(), "missing")
	})

	t.Run("WorkflowAlreadyExistsError", func(t *testing.T) {
		err := &WorkflowAlreadyExistsError{Name: "dup"}
		assert.True(t, errors.Is(err, ErrWorkflowAlreadyExists))
		assert.Contains(t, err.Error(), "dup")
	})

	t.Run("WorkflowLoadError", func(t *testing.T) {
		cause := errors.New("permission denied")
		err := &WorkflowLoadError{Path: "/tmp/bad.yaml", Cause: cause}
		assert.True(t, errors.Is(err, ErrWorkflowLoad))
		assert.True(t, errors.Is(err, cause))
		assert.Contains(t, err.Error(), "/tmp/bad.yaml")
	})

	t.Run("WorkflowParseError", func(t *testing.T) {
		cause := errors.New("unexpected token")
		err := &WorkflowParseError{Path: "/tmp/bad.yaml", Cause: cause}
		assert.True(t, errors.Is(err, ErrWorkflowParse))
		assert.True(t, errors.Is(err, cause))
		assert.Contains(t, err.Error(), "/tmp/bad.yaml")
	})
}

// =============================================================================
// WorkflowMode Validation Tests
// =============================================================================

func TestWorkflowMode_AllValidModes(t *testing.T) {
	modes := []WorkflowMode{
		WorkflowModeCanned,
		WorkflowModeConstrained,
		WorkflowModeAutonomous,
	}

	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			_, ok := validWorkflowModes[mode]
			assert.True(t, ok)
		})
	}
}

func TestWorkflowMode_InvalidMode(t *testing.T) {
	_, ok := validWorkflowModes["invalid"]
	assert.False(t, ok)
}

// =============================================================================
// Bundled Workflow YAML Tests
// =============================================================================

func TestWorkflowLoader_LoadFromDir(t *testing.T) {
	// Create a temp directory with test workflow files.
	tmpDir := t.TempDir()

	testYAML := `name: test-workflow
description: "Test workflow"
mode: constrained
agents:
  - role: lead
    backend: claude
    model: test-model
    capabilities: [persistent_sessions]
  - role: coder
    backend: opencode
    model: test-model
    capabilities: [tool_use]
settings:
  max_iterations: 5
  max_parallelism: 2
`
	err := os.WriteFile(filepath.Join(tmpDir, "test-workflow.yaml"), []byte(testYAML), 0644)
	require.NoError(t, err)

	loader := NewWorkflowLoader()
	err = loader.LoadFromDir(SystemTenantID, tmpDir)
	require.NoError(t, err)

	names := loader.List(SystemTenantID)
	assert.Contains(t, names, "test-workflow")

	wf, err := loader.Get(SystemTenantID, "test-workflow")
	require.NoError(t, err)
	assert.Equal(t, WorkflowModeConstrained, wf.Mode)
	assert.Len(t, wf.Agents, 2)
	assert.Equal(t, "lead", wf.Agents[0].Role)
	assert.Equal(t, "coder", wf.Agents[1].Role)
	assert.Equal(t, 5, wf.Settings.MaxIterations)
}

func TestWorkflowLoader_LoadFromDir_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	loader := NewWorkflowLoader()
	err := loader.LoadFromDir(SystemTenantID, tmpDir)
	require.NoError(t, err)
	assert.Empty(t, loader.List(SystemTenantID))
}

// findProjectRoot locates the repository root by looking for go.mod.
func findProjectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}
		dir = parent
	}
}

// =============================================================================
// Tenant scoping
// =============================================================================

// TestWorkflowLoader_TenantsDoNotCollide is the isolation proof: two tenants
// register the same workflow name, each sees only its own definition, and
// deleting one leaves the other in place.
func TestWorkflowLoader_TenantsDoNotCollide(t *testing.T) {
	loader := NewWorkflowLoader()

	alpha := newValidWorkflowDef("shared")
	alpha.Description = "alpha's"
	beta := newValidWorkflowDef("shared")
	beta.Description = "beta's"

	require.NoError(t, loader.Register("alpha", alpha))
	require.NoError(t, loader.Register("beta", beta))

	gotAlpha, err := loader.Get("alpha", "shared")
	require.NoError(t, err)
	assert.Equal(t, "alpha's", gotAlpha.Description)
	assert.Equal(t, "alpha", gotAlpha.TenantID, "the loader stamps the owning tenant")

	gotBeta, err := loader.Get("beta", "shared")
	require.NoError(t, err)
	assert.Equal(t, "beta's", gotBeta.Description)
	assert.Equal(t, "beta", gotBeta.TenantID)

	assert.Equal(t, []string{"shared"}, loader.List("alpha"))
	assert.Equal(t, []string{"shared"}, loader.List("beta"))
	assert.Empty(t, loader.List("gamma"), "a tenant with no workflows sees none of the others'")

	require.NoError(t, loader.Delete("alpha", "shared"))
	_, err = loader.Get("alpha", "shared")
	assert.True(t, errors.Is(err, ErrWorkflowNotFound))
	_, err = loader.Get("beta", "shared")
	require.NoError(t, err, "deleting alpha's workflow must not touch beta's")

	// Update is tenant-scoped too: beta's definition cannot be updated
	// through alpha.
	assert.True(t, errors.Is(loader.Update("alpha", beta), ErrWorkflowNotFound))
}

// TestWorkflowLoader_EmptyTenantIsRefused: every record key begins with its
// owning tenant, so an empty tenant would collapse the keyspace.
func TestWorkflowLoader_EmptyTenantIsRefused(t *testing.T) {
	loader := NewWorkflowLoader()
	def := newValidWorkflowDef("wf")

	assert.True(t, errors.Is(loader.Register("", def), ErrWorkflowValidation))
	assert.True(t, errors.Is(loader.Update("", def), ErrWorkflowValidation))
	assert.True(t, errors.Is(loader.Delete("", "wf"), ErrWorkflowValidation))
	_, err := loader.Get(" ", "wf")
	assert.True(t, errors.Is(err, ErrWorkflowValidation))
	_, err = loader.LoadFromFile("", "/nonexistent.yaml")
	assert.True(t, errors.Is(err, ErrWorkflowValidation))
	assert.True(t, errors.Is(loader.LoadFromDir("", t.TempDir()), ErrWorkflowValidation))
	_, err = loader.Rehydrate(context.Background(), "")
	assert.True(t, errors.Is(err, ErrWorkflowValidation))

	var verr *WorkflowValidationError
	require.True(t, errors.As(loader.Register("", def), &verr))
	assert.Equal(t, "tenant_id", verr.Field)
}

// TestWorkflowLoader_NotFoundNamesTheTenant pins the error shape a transport
// renders.
func TestWorkflowLoader_NotFoundNamesTheTenant(t *testing.T) {
	loader := NewWorkflowLoader()
	_, err := loader.Get("acme", "ghost")
	var nf *WorkflowNotFoundError
	require.True(t, errors.As(err, &nf))
	assert.Equal(t, "acme", nf.TenantID)
	assert.Equal(t, "ghost", nf.Name)
	assert.Contains(t, err.Error(), "acme")

	require.NoError(t, loader.Register("acme", newValidWorkflowDef("dup")))
	err = loader.Register("acme", newValidWorkflowDef("dup"))
	var exists *WorkflowAlreadyExistsError
	require.True(t, errors.As(err, &exists))
	assert.Equal(t, "acme", exists.TenantID)
	assert.Contains(t, err.Error(), "acme")
}

// TestWorkflowLoader_RehydrateIsPerTenant: rehydrating one tenant replaces
// only that tenant's cache; RehydrateAll files every stored definition under
// its own tenant.
func TestWorkflowLoader_RehydrateIsPerTenant(t *testing.T) {
	ctx := context.Background()
	persistor := newMemWorkflowPersistor()
	loader := NewWorkflowLoader()
	loader.SetPersistor(persistor)

	require.NoError(t, loader.Register("alpha", newValidWorkflowDef("a1")))
	require.NoError(t, loader.Register("beta", newValidWorkflowDef("b1")))

	// Something only in memory for beta, and something only in the
	// persistor for alpha.
	loader.store("beta", newValidWorkflowDef("b-memory-only"))
	stored := newValidWorkflowDef("a-stored-only")
	stored.TenantID = "alpha"
	persistor.saved["a-stored-only"] = stored

	n, err := loader.Rehydrate(ctx, "alpha")
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, []string{"a-stored-only", "a1"}, loader.List("alpha"))
	assert.Equal(t, []string{"b-memory-only", "b1"}, loader.List("beta"), "rehydrating alpha must not touch beta's cache")

	n, err = loader.RehydrateAll(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []string{"a-stored-only", "a1"}, loader.List("alpha"))
	assert.Equal(t, []string{"b1"}, loader.List("beta"), "RehydrateAll replaces every tenant's cache with storage")
}

// TestWorkflowLoader_RehydrateAll_SkipsUnaddressableEntries: a stored entry
// with no tenant or no name cannot be filed and must not abort the boot.
func TestWorkflowLoader_RehydrateAll_SkipsUnaddressableEntries(t *testing.T) {
	persistor := newMemWorkflowPersistor()
	good := newValidWorkflowDef("good")
	good.TenantID = "acme"
	persistor.saved["good"] = good
	persistor.saved["no-tenant"] = newValidWorkflowDef("no-tenant")
	persistor.saved["no-name"] = &WorkflowDefinition{TenantID: "acme"}

	loader := NewWorkflowLoader()
	loader.SetPersistor(persistor)
	n, err := loader.RehydrateAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, n, "the raw count is reported")
	assert.Equal(t, []string{"good"}, loader.List("acme"))
}

// TestWorkflowLoader_RehydrateAll_NoPersistorOrError covers the boundaries.
func TestWorkflowLoader_RehydrateAll_NoPersistorOrError(t *testing.T) {
	loader := NewWorkflowLoader()
	n, err := loader.RehydrateAll(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)

	persistor := newMemWorkflowPersistor()
	persistor.loadErr = errors.New("load failed")
	loader.SetPersistor(persistor)
	_, err = loader.RehydrateAll(context.Background())
	assert.True(t, errors.Is(err, persistor.loadErr))
}

// TestWorkflowLoader_LoadFromFile_FilesUnderTheTenant: a YAML file carries no
// tenant; the caller's tenant owns what it loads.
func TestWorkflowLoader_LoadFromFile_FilesUnderTheTenant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wf.yaml")
	require.NoError(t, os.WriteFile(path, []byte("name: filed\nmode: autonomous\nagents:\n  - role: lead\n    backend: mock\n"), 0o600))

	loader := NewWorkflowLoader()
	def, err := loader.LoadFromFile("acme", path)
	require.NoError(t, err)
	assert.Equal(t, "acme", def.TenantID)

	_, err = loader.Get("other", "filed")
	assert.True(t, errors.Is(err, ErrWorkflowNotFound))
	got, err := loader.Get("acme", "filed")
	require.NoError(t, err)
	assert.Same(t, def, got)
}
