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
	def, err := loader.LoadFromFile(path)
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
	got, err := loader.Get("test-workflow")
	require.NoError(t, err)
	assert.Equal(t, def, got)
}

func TestWorkflowLoader_LoadFromFile_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")

	err := os.WriteFile(path, []byte("{{invalid yaml:::"), 0o600)
	require.NoError(t, err)

	loader := NewWorkflowLoader()
	_, err = loader.LoadFromFile(path)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowParse))
}

func TestWorkflowLoader_LoadFromFile_MissingFile(t *testing.T) {
	loader := NewWorkflowLoader()
	_, err := loader.LoadFromFile("/nonexistent/path/workflow.yaml")
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
	_, err = loader.LoadFromFile(path)
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
	err = loader.LoadFromDir(dir)
	require.NoError(t, err)

	names := loader.List()
	assert.Equal(t, []string{"alpha", "bravo"}, names)
}

func TestWorkflowLoader_LoadFromDir_NonexistentDir(t *testing.T) {
	loader := NewWorkflowLoader()
	err := loader.LoadFromDir("/nonexistent/directory")
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
	err = loader.LoadFromDir(dir)
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

	err := loader.Register(def)
	require.NoError(t, err)

	got, err := loader.Get("my-workflow")
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

	err := loader.Register(def)
	require.NoError(t, err)

	err = loader.Register(def)
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

	err := loader.Register(def)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrWorkflowValidation))
}

// =============================================================================
// WorkflowLoader.Get Error Tests
// =============================================================================

func TestWorkflowLoader_Get_NotFound(t *testing.T) {
	loader := NewWorkflowLoader()

	_, err := loader.Get("nonexistent")
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
	names := loader.List()
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
		err := loader.Register(def)
		require.NoError(t, err)
	}

	names := loader.List()
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
		err := loader.Register(def)
		require.NoError(t, err)
	}

	result := loader.ListDefinitions()
	require.Len(t, result, 3)
	assert.Equal(t, "alpha", result[0].Name)
	assert.Equal(t, "middle", result[1].Name)
	assert.Equal(t, "zebra", result[2].Name)
	assert.Equal(t, WorkflowModeConstrained, result[0].Mode)
	assert.Len(t, result[0].Agents, 2)
}

func TestWorkflowLoader_ListDefinitions_Empty(t *testing.T) {
	loader := NewWorkflowLoader()
	result := loader.ListDefinitions()
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
	err = loader.LoadFromDir(tmpDir)
	require.NoError(t, err)

	names := loader.List()
	assert.Contains(t, names, "test-workflow")

	wf, err := loader.Get("test-workflow")
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
	err := loader.LoadFromDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, loader.List())
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
