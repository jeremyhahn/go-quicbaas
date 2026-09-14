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
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// ValidateTopology
// =============================================================================

func TestValidateTopology_ValidTopologies(t *testing.T) {
	valid := []CommunicationTopology{
		TopologyHubAndSpoke,
		TopologyHybrid,
		TopologyPeerToPeer,
	}
	for _, topo := range valid {
		t.Run(string(topo), func(t *testing.T) {
			if err := ValidateTopology(topo); err != nil {
				t.Errorf("expected %s to be valid, got: %v", topo, err)
			}
		})
	}
}

func TestValidateTopology_EmptyAccepted(t *testing.T) {
	if err := ValidateTopology(""); err != nil {
		t.Errorf("expected empty topology to be accepted, got: %v", err)
	}
}

func TestValidateTopology_InvalidRejected(t *testing.T) {
	err := ValidateTopology("mesh")
	if err == nil {
		t.Fatal("expected invalid topology to be rejected")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
	var valErr *WorkflowValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *WorkflowValidationError, got %T", err)
	}
	if valErr.Field != "topology" {
		t.Errorf("expected field=topology, got %s", valErr.Field)
	}
}

// =============================================================================
// ValidateIsolation
// =============================================================================

func TestValidateIsolation_ValidIsolations(t *testing.T) {
	valid := []WorkspaceIsolation{IsolationShared, IsolationWorktree}
	for _, iso := range valid {
		t.Run(string(iso), func(t *testing.T) {
			if err := ValidateIsolation(iso); err != nil {
				t.Errorf("expected %s to be valid, got: %v", iso, err)
			}
		})
	}
}

func TestValidateIsolation_EmptyAccepted(t *testing.T) {
	if err := ValidateIsolation(""); err != nil {
		t.Errorf("expected empty isolation to be accepted, got: %v", err)
	}
}

func TestValidateIsolation_InvalidRejected(t *testing.T) {
	err := ValidateIsolation("container")
	if err == nil {
		t.Fatal("expected invalid isolation to be rejected")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
	var valErr *WorkflowValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *WorkflowValidationError, got %T", err)
	}
	if valErr.Field != "isolation" {
		t.Errorf("expected field=isolation, got %s", valErr.Field)
	}
}

// =============================================================================
// ValidateOverflowAction
// =============================================================================

func TestValidateOverflowAction_ValidActions(t *testing.T) {
	valid := []string{"reject", "drop_oldest"}
	for _, action := range valid {
		t.Run(action, func(t *testing.T) {
			if err := ValidateOverflowAction(action); err != nil {
				t.Errorf("expected %s to be valid, got: %v", action, err)
			}
		})
	}
}

func TestValidateOverflowAction_EmptyAccepted(t *testing.T) {
	if err := ValidateOverflowAction(""); err != nil {
		t.Errorf("expected empty overflow action to be accepted, got: %v", err)
	}
}

func TestValidateOverflowAction_InvalidRejected(t *testing.T) {
	err := ValidateOverflowAction("discard")
	if err == nil {
		t.Fatal("expected invalid overflow action to be rejected")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
	var valErr *WorkflowValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *WorkflowValidationError, got %T", err)
	}
	if valErr.Field != "overflow_action" {
		t.Errorf("expected field=overflow_action, got %s", valErr.Field)
	}
}

// =============================================================================
// ValidateBudgetAction
// =============================================================================

func TestValidateBudgetAction_ValidActions(t *testing.T) {
	valid := []string{"warn", "pause", "shutdown"}
	for _, action := range valid {
		t.Run(action, func(t *testing.T) {
			if err := ValidateBudgetAction(action); err != nil {
				t.Errorf("expected %s to be valid, got: %v", action, err)
			}
		})
	}
}

func TestValidateBudgetAction_EmptyAccepted(t *testing.T) {
	if err := ValidateBudgetAction(""); err != nil {
		t.Errorf("expected empty budget action to be accepted, got: %v", err)
	}
}

func TestValidateBudgetAction_InvalidRejected(t *testing.T) {
	err := ValidateBudgetAction("kill")
	if err == nil {
		t.Fatal("expected invalid budget action to be rejected")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
	var valErr *WorkflowValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *WorkflowValidationError, got %T", err)
	}
	if valErr.Field != "budget_action" {
		t.Errorf("expected field=budget_action, got %s", valErr.Field)
	}
}

// =============================================================================
// WorkflowSettings.ApplyDefaults
// =============================================================================

func TestWorkflowSettings_ApplyDefaults_AllZeroValues(t *testing.T) {
	ws := &WorkflowSettings{}
	ws.ApplyDefaults()

	if ws.Topology != TopologyHubAndSpoke {
		t.Errorf("expected topology=%s, got %s", TopologyHubAndSpoke, ws.Topology)
	}
	if ws.Isolation != IsolationShared {
		t.Errorf("expected isolation=%s, got %s", IsolationShared, ws.Isolation)
	}
	if ws.PeerMessaging.MaxMessageSize != 10240 {
		t.Errorf("expected max_message_size=10240, got %d", ws.PeerMessaging.MaxMessageSize)
	}
	if ws.PeerMessaging.MaxInboxSize != 1000 {
		t.Errorf("expected max_inbox_size=1000, got %d", ws.PeerMessaging.MaxInboxSize)
	}
	if ws.PeerMessaging.OverflowAction != "reject" {
		t.Errorf("expected overflow_action=reject, got %s", ws.PeerMessaging.OverflowAction)
	}
	if ws.PeerMessaging.InboxTTL != "1h" {
		t.Errorf("expected inbox_ttl=1h, got %s", ws.PeerMessaging.InboxTTL)
	}
	if ws.FileLocking.LeaseTTL != "5m" {
		t.Errorf("expected lease_ttl=5m, got %s", ws.FileLocking.LeaseTTL)
	}
	if ws.FileLocking.HeartbeatInterval != "30s" {
		t.Errorf("expected heartbeat_interval=30s, got %s", ws.FileLocking.HeartbeatInterval)
	}
	if ws.FileLocking.MaxRenewals != 60 {
		t.Errorf("expected max_renewals=60, got %d", ws.FileLocking.MaxRenewals)
	}
	if ws.VectorDB.IndexType != "auto" {
		t.Errorf("expected index_type=auto, got %s", ws.VectorDB.IndexType)
	}
	if ws.Recovery.MaxRestarts != 3 {
		t.Errorf("expected max_restarts=3, got %d", ws.Recovery.MaxRestarts)
	}
	if ws.Recovery.RestartBackoff != "30s" {
		t.Errorf("expected restart_backoff=30s, got %s", ws.Recovery.RestartBackoff)
	}
}

func TestWorkflowSettings_ApplyDefaults_PreservesExistingValues(t *testing.T) {
	ws := &WorkflowSettings{
		Topology:  TopologyPeerToPeer,
		Isolation: IsolationWorktree,
		PeerMessaging: PeerMessagingSettings{
			MaxMessageSize: 5000,
			MaxInboxSize:   500,
			OverflowAction: "drop_oldest",
			InboxTTL:       "2h",
		},
		FileLocking: FileLockSettings{
			LeaseTTL:          "10m",
			HeartbeatInterval: "1m",
			MaxRenewals:       30,
		},
		VectorDB: VectorDBSettings{
			IndexType: "hnsw",
		},
		Recovery: RecoverySettings{
			MaxRestarts:    5,
			RestartBackoff: "1m",
		},
	}
	ws.ApplyDefaults()

	if ws.Topology != TopologyPeerToPeer {
		t.Errorf("expected topology preserved as peer_to_peer, got %s", ws.Topology)
	}
	if ws.Isolation != IsolationWorktree {
		t.Errorf("expected isolation preserved as worktree, got %s", ws.Isolation)
	}
	if ws.PeerMessaging.MaxMessageSize != 5000 {
		t.Errorf("expected max_message_size preserved as 5000, got %d", ws.PeerMessaging.MaxMessageSize)
	}
	if ws.PeerMessaging.MaxInboxSize != 500 {
		t.Errorf("expected max_inbox_size preserved as 500, got %d", ws.PeerMessaging.MaxInboxSize)
	}
	if ws.PeerMessaging.OverflowAction != "drop_oldest" {
		t.Errorf("expected overflow_action preserved as drop_oldest, got %s", ws.PeerMessaging.OverflowAction)
	}
	if ws.FileLocking.LeaseTTL != "10m" {
		t.Errorf("expected lease_ttl preserved as 10m, got %s", ws.FileLocking.LeaseTTL)
	}
	if ws.FileLocking.MaxRenewals != 30 {
		t.Errorf("expected max_renewals preserved as 30, got %d", ws.FileLocking.MaxRenewals)
	}
	if ws.VectorDB.IndexType != "hnsw" {
		t.Errorf("expected index_type preserved as hnsw, got %s", ws.VectorDB.IndexType)
	}
	if ws.Recovery.MaxRestarts != 5 {
		t.Errorf("expected max_restarts preserved as 5, got %d", ws.Recovery.MaxRestarts)
	}
}

// =============================================================================
// WorkflowSettings.Validate
// =============================================================================

func TestWorkflowSettings_Validate_ValidDefaults(t *testing.T) {
	ws := &WorkflowSettings{}
	ws.ApplyDefaults()
	if err := ws.Validate(); err != nil {
		t.Errorf("expected defaults to validate, got: %v", err)
	}
}

func TestWorkflowSettings_Validate_InvalidTopology(t *testing.T) {
	ws := &WorkflowSettings{Topology: "mesh"}
	err := ws.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid topology")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
}

func TestWorkflowSettings_Validate_InvalidIsolation(t *testing.T) {
	ws := &WorkflowSettings{
		Topology:  TopologyHubAndSpoke,
		Isolation: "container",
	}
	err := ws.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid isolation")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
}

func TestWorkflowSettings_Validate_InvalidOverflowAction(t *testing.T) {
	ws := &WorkflowSettings{
		PeerMessaging: PeerMessagingSettings{OverflowAction: "discard"},
	}
	err := ws.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid overflow action")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
}

func TestWorkflowSettings_Validate_InvalidBudgetAction(t *testing.T) {
	ws := &WorkflowSettings{
		CostControls: CostControlSettings{BudgetAction: "kill"},
	}
	err := ws.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid budget action")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
}

// =============================================================================
// WorkflowDefinition.Validate (with settings validation)
// =============================================================================

func TestWorkflowDefinition_Validate_ValidWithSettings(t *testing.T) {
	wd := &WorkflowDefinition{
		Name: "test-workflow",
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "worker", Backend: "claude", Model: "opus"},
		},
		Settings: WorkflowSettings{
			Topology:  TopologyHybrid,
			Isolation: IsolationWorktree,
		},
	}
	if err := wd.Validate(); err != nil {
		t.Errorf("expected valid workflow to pass, got: %v", err)
	}
}

func TestWorkflowDefinition_Validate_InvalidSettings(t *testing.T) {
	wd := &WorkflowDefinition{
		Name: "test-workflow",
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "worker", Backend: "claude", Model: "opus"},
		},
		Settings: WorkflowSettings{
			Topology: "invalid_topo",
		},
	}
	err := wd.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid settings")
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Errorf("expected ErrWorkflowValidation, got: %v", err)
	}
}

func TestWorkflowDefinition_Validate_EmptyName(t *testing.T) {
	wd := &WorkflowDefinition{
		Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{
			{Role: "worker", Backend: "claude", Model: "opus"},
		},
	}
	err := wd.Validate()
	if err == nil {
		t.Fatal("expected validation error for empty name")
	}
	var valErr *WorkflowValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *WorkflowValidationError, got %T", err)
	}
	if valErr.Field != "name" {
		t.Errorf("expected field=name, got %s", valErr.Field)
	}
}

func TestWorkflowDefinition_Validate_InvalidMode(t *testing.T) {
	wd := &WorkflowDefinition{
		Name: "test",
		Mode: "invalid",
		Agents: []AgentRoleSpec{
			{Role: "worker", Backend: "claude", Model: "opus"},
		},
	}
	err := wd.Validate()
	if err == nil {
		t.Fatal("expected validation error for invalid mode")
	}
	var valErr *WorkflowValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *WorkflowValidationError, got %T", err)
	}
	if valErr.Field != "mode" {
		t.Errorf("expected field=mode, got %s", valErr.Field)
	}
}

func TestWorkflowDefinition_Validate_NoAgents(t *testing.T) {
	wd := &WorkflowDefinition{
		Name: "test",
		Mode: WorkflowModeAutonomous,
	}
	err := wd.Validate()
	if err == nil {
		t.Fatal("expected validation error for no agents")
	}
	var valErr *WorkflowValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("expected *WorkflowValidationError, got %T", err)
	}
	if valErr.Field != "agents" {
		t.Errorf("expected field=agents, got %s", valErr.Field)
	}
}

func TestWorkflowDefinition_Validate_CannedRequiresLead(t *testing.T) {
	wd := &WorkflowDefinition{
		Name: "test",
		Mode: WorkflowModeCanned,
		Agents: []AgentRoleSpec{
			{Role: "worker", Backend: "claude", Model: "opus"},
		},
	}
	err := wd.Validate()
	if err == nil {
		t.Fatal("expected validation error for canned without lead")
	}
}

func TestWorkflowDefinition_Validate_CannedWithLead(t *testing.T) {
	wd := &WorkflowDefinition{
		Name: "test",
		Mode: WorkflowModeCanned,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: "claude", Model: "opus"},
			{Role: "worker", Backend: "claude", Model: "opus"},
		},
	}
	if err := wd.Validate(); err != nil {
		t.Errorf("expected canned with lead to pass, got: %v", err)
	}
}

// =============================================================================
// Struct Field Coverage
// =============================================================================

func TestPeerMessagingSettings_Fields(t *testing.T) {
	s := PeerMessagingSettings{
		MaxMessageSize: 2048,
		MaxInboxSize:   100,
		OverflowAction: "reject",
		InboxTTL:       "30m",
	}
	if s.MaxMessageSize != 2048 {
		t.Errorf("expected MaxMessageSize=2048, got %d", s.MaxMessageSize)
	}
	if s.MaxInboxSize != 100 {
		t.Errorf("expected MaxInboxSize=100, got %d", s.MaxInboxSize)
	}
	if s.OverflowAction != "reject" {
		t.Errorf("expected OverflowAction=reject, got %s", s.OverflowAction)
	}
	if s.InboxTTL != "30m" {
		t.Errorf("expected InboxTTL=30m, got %s", s.InboxTTL)
	}
}

func TestFileLockSettings_Fields(t *testing.T) {
	s := FileLockSettings{
		LeaseTTL:          "10m",
		HeartbeatInterval: "1m",
		MaxRenewals:       30,
	}
	if s.LeaseTTL != "10m" {
		t.Errorf("expected LeaseTTL=10m, got %s", s.LeaseTTL)
	}
	if s.HeartbeatInterval != "1m" {
		t.Errorf("expected HeartbeatInterval=1m, got %s", s.HeartbeatInterval)
	}
	if s.MaxRenewals != 30 {
		t.Errorf("expected MaxRenewals=30, got %d", s.MaxRenewals)
	}
}

func TestVectorDBSettings_Fields(t *testing.T) {
	s := VectorDBSettings{
		Enabled:        true,
		IndexType:      "hnsw",
		AutoIndex:      true,
		Triggers:       []string{"on_commit", "on_save"},
		GPUAccelerated: true,
	}
	if !s.Enabled {
		t.Error("expected Enabled=true")
	}
	if s.IndexType != "hnsw" {
		t.Errorf("expected IndexType=hnsw, got %s", s.IndexType)
	}
	if !s.AutoIndex {
		t.Error("expected AutoIndex=true")
	}
	if len(s.Triggers) != 2 {
		t.Errorf("expected 2 triggers, got %d", len(s.Triggers))
	}
	if !s.GPUAccelerated {
		t.Error("expected GPUAccelerated=true")
	}
}

func TestCostControlSettings_Fields(t *testing.T) {
	s := CostControlSettings{
		MaxTokensPerJob:   1000000,
		MaxTokensPerAgent: 100000,
		BudgetAction:      "pause",
	}
	if s.MaxTokensPerJob != 1000000 {
		t.Errorf("expected MaxTokensPerJob=1000000, got %d", s.MaxTokensPerJob)
	}
	if s.MaxTokensPerAgent != 100000 {
		t.Errorf("expected MaxTokensPerAgent=100000, got %d", s.MaxTokensPerAgent)
	}
	if s.BudgetAction != "pause" {
		t.Errorf("expected BudgetAction=pause, got %s", s.BudgetAction)
	}
}

func TestRecoverySettings_Fields(t *testing.T) {
	s := RecoverySettings{
		AutoRestart:    true,
		MaxRestarts:    5,
		RestartBackoff: "1m",
	}
	if !s.AutoRestart {
		t.Error("expected AutoRestart=true")
	}
	if s.MaxRestarts != 5 {
		t.Errorf("expected MaxRestarts=5, got %d", s.MaxRestarts)
	}
	if s.RestartBackoff != "1m" {
		t.Errorf("expected RestartBackoff=1m, got %s", s.RestartBackoff)
	}
}

// =============================================================================
// AgentRoleSpec NumCtx
// =============================================================================

func TestAgentRoleSpec_NumCtx_Set(t *testing.T) {
	spec := AgentRoleSpec{
		Role:    "worker",
		Backend: "ollama",
		Model:   "llama3.3:70b-32k",
		NumCtx:  32768,
	}
	if spec.NumCtx != 32768 {
		t.Errorf("expected NumCtx=32768, got %d", spec.NumCtx)
	}
}

func TestAgentRoleSpec_NumCtx_ZeroDefault(t *testing.T) {
	spec := AgentRoleSpec{
		Role:    "worker",
		Backend: "claude",
		Model:   "opus",
	}
	if spec.NumCtx != 0 {
		t.Errorf("expected NumCtx=0 (omitted), got %d", spec.NumCtx)
	}
}

// =============================================================================
// WorkflowSettings NumCtx
// =============================================================================

func TestWorkflowSettings_NumCtx_Set(t *testing.T) {
	ws := &WorkflowSettings{
		NumCtx: 65536,
	}
	ws.ApplyDefaults()
	if ws.NumCtx != 65536 {
		t.Errorf("expected NumCtx=65536, got %d", ws.NumCtx)
	}
}

func TestWorkflowSettings_NumCtx_ZeroDefault(t *testing.T) {
	ws := &WorkflowSettings{}
	ws.ApplyDefaults()
	if ws.NumCtx != 0 {
		t.Errorf("expected NumCtx=0 (omitted), got %d", ws.NumCtx)
	}
}

func TestWorkflowDefinition_Validate_WithNumCtx(t *testing.T) {
	wd := &WorkflowDefinition{
		Name: "ollama-workflow",
		Mode: WorkflowModeConstrained,
		Agents: []AgentRoleSpec{
			{
				Role:    "lead",
				Backend: "ollama",
				Model:   "llama3.3:70b-32k",
				NumCtx:  32768,
			},
			{
				Role:    "worker",
				Backend: "ollama",
				Model:   "qwen2.5-coder:32b-16k",
				NumCtx:  16384,
			},
		},
		Settings: WorkflowSettings{
			NumCtx: 32768,
		},
	}
	if err := wd.Validate(); err != nil {
		t.Errorf("expected workflow with NumCtx to validate, got: %v", err)
	}
}

// =============================================================================
// Lifecycle seams on the workflow: Hooks and criterion Phase
// =============================================================================

// hooksTestFake is the smallest JobLifecycleHooks for registry tests.
type hooksTestFake struct{}

func (hooksTestFake) Prepare(context.Context, *JobContext) (string, error)    { return "", nil }
func (hooksTestFake) Reviewable(context.Context, *JobContext) error           { return nil }
func (hooksTestFake) Complete(context.Context, *JobContext) error             { return nil }
func (hooksTestFake) Discard(context.Context, *JobContext, bool) error        { return nil }
func (hooksTestFake) Blocked(context.Context, *JobContext, BlockReason) error { return nil }

func seamsWorkflow(settings WorkflowSettings) *WorkflowDefinition {
	return &WorkflowDefinition{
		Name:     "seams",
		Mode:     WorkflowModeAutonomous,
		Agents:   []AgentRoleSpec{{Role: "worker", Backend: "claude"}},
		Settings: settings,
	}
}

func TestWorkflowSettings_Validate_UnknownHooksRefused(t *testing.T) {
	t.Parallel()
	err := seamsWorkflow(WorkflowSettings{Hooks: "nobody-registered-these"}).Validate()
	var unknown *UnknownJobHooksError
	if !errors.As(err, &unknown) || unknown.Name != "nobody-registered-these" {
		t.Fatalf("got %v, want UnknownJobHooksError", err)
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Fatal("an unknown hook set must be a workflow validation failure")
	}
}

func TestWorkflowSettings_Validate_RegisteredHooksAccepted(t *testing.T) {
	t.Parallel()
	const name = "workflow-types-test-hooks"
	registerJobHooksForTest(t, name, hooksTestFake{})
	if err := seamsWorkflow(WorkflowSettings{Hooks: name}).Validate(); err != nil {
		t.Fatalf("a registered hook set must validate: %v", err)
	}
	if err := seamsWorkflow(WorkflowSettings{}).Validate(); err != nil {
		t.Fatalf("no hooks is valid: %v", err)
	}
}

func TestReviewCriterion_Validate_PhaseAndKind(t *testing.T) {
	t.Parallel()
	good := ReviewCriterion{Name: "tests", Validator: ValidatorKindCommand, Phase: CriterionPhaseReviewing}
	if err := good.Validate(); err != nil {
		t.Fatalf("valid criterion refused: %v", err)
	}
	if good.EffectivePhase() != CriterionPhaseReviewing {
		t.Fatalf("EffectivePhase = %q", good.EffectivePhase())
	}
	if (ReviewCriterion{}).EffectivePhase() != CriterionPhaseValidating {
		t.Fatal("an unset phase must default to validating")
	}

	var wfErr *WorkflowValidationError
	if err := (ReviewCriterion{Validator: ValidatorKindPolicy}).Validate(); !errors.As(err, &wfErr) || wfErr.Field != "review_criteria" {
		t.Fatalf("nameless criterion: got %v, want WorkflowValidationError on review_criteria", err)
	}
	if err := (ReviewCriterion{Name: "x", Validator: ValidatorKindPolicy, Phase: "publishing"}).Validate(); !errors.As(err, &wfErr) {
		t.Fatalf("unknown phase: got %v, want WorkflowValidationError", err)
	}
	var unknown *UnknownValidatorError
	if err := (ReviewCriterion{Name: "x", Validator: "nope"}).Validate(); !errors.As(err, &unknown) {
		t.Fatalf("unknown kind: got %v, want UnknownValidatorError", err)
	}
	if err := seamsWorkflow(WorkflowSettings{ReviewCriteria: []ReviewCriterion{{Name: "x", Validator: "nope"}}}).Validate(); !errors.As(err, &unknown) {
		t.Fatalf("workflow with unknown kind: got %v, want UnknownValidatorError", err)
	}
}

func TestCriteriaForPhase_SplitsByEffectivePhase(t *testing.T) {
	t.Parallel()
	criteria := []ReviewCriterion{
		{Name: "a"},
		{Name: "b", Phase: CriterionPhaseReviewing},
		{Name: "c", Phase: CriterionPhaseValidating},
	}
	validating := criteriaForPhase(criteria, CriterionPhaseValidating)
	reviewing := criteriaForPhase(criteria, CriterionPhaseReviewing)
	if len(validating) != 2 || validating[0].Name != "a" || validating[1].Name != "c" {
		t.Fatalf("validating = %v", validating)
	}
	if len(reviewing) != 1 || reviewing[0].Name != "b" {
		t.Fatalf("reviewing = %v", reviewing)
	}
}

// TestAgentRoleSpec_SystemPromptRoundTrips pins the role's system prompt on
// both wire shapes: a module ships its charter here and the engine spawns
// every session of the role with it.
func TestAgentRoleSpec_SystemPromptRoundTrips(t *testing.T) {
	var def WorkflowDefinition
	if err := yaml.Unmarshal([]byte("name: w\nmode: autonomous\nagents:\n  - role: worker\n    backend: claude\n    system_prompt: |\n      be careful\n      be kind\n"), &def); err != nil {
		t.Fatal(err)
	}
	if def.Agents[0].SystemPrompt != "be careful\nbe kind\n" {
		t.Fatalf("yaml system_prompt = %q", def.Agents[0].SystemPrompt)
	}
	data, err := json.Marshal(def.Agents[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"system_prompt":"be careful\nbe kind\n"`) {
		t.Fatalf("json = %s", data)
	}
	var back AgentRoleSpec
	if err := json.Unmarshal(data, &back); err != nil || back.SystemPrompt != def.Agents[0].SystemPrompt {
		t.Fatalf("json round trip: %+v %v", back, err)
	}
	if empty, _ := json.Marshal(AgentRoleSpec{Role: "lead"}); strings.Contains(string(empty), "system_prompt") {
		t.Fatal("an empty prompt must be omitted")
	}
}
