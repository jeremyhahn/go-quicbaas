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
	"testing"
)

// =============================================================================
// Enum validators
// =============================================================================

// enumCase is one input to an enum validator: wantField is empty when the
// input is accepted, otherwise the field the validation error must name.
type enumCase struct {
	input     string
	wantField string
}

// checkEnumValidator runs validate over cases and asserts each input is
// accepted or refused with a *WorkflowValidationError naming wantField.
func checkEnumValidator(t *testing.T, validate func(string) error, cases []enumCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run("input="+tc.input, func(t *testing.T) {
			err := validate(tc.input)
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("expected %q to be accepted, got: %v", tc.input, err)
				}
				return
			}
			if !errors.Is(err, ErrWorkflowValidation) {
				t.Fatalf("expected ErrWorkflowValidation for %q, got: %v", tc.input, err)
			}
			var valErr *WorkflowValidationError
			if !errors.As(err, &valErr) {
				t.Fatalf("expected *WorkflowValidationError, got %T", err)
			}
			if valErr.Field != tc.wantField {
				t.Errorf("expected field=%s, got %s", tc.wantField, valErr.Field)
			}
		})
	}
}

func TestValidateTopology(t *testing.T) {
	checkEnumValidator(t, func(s string) error { return ValidateTopology(CommunicationTopology(s)) }, []enumCase{
		{input: string(TopologyHubAndSpoke)},
		{input: string(TopologyHybrid)},
		{input: string(TopologyPeerToPeer)},
		{input: ""},
		{input: "mesh", wantField: "topology"},
	})
}

func TestValidateIsolation(t *testing.T) {
	checkEnumValidator(t, func(s string) error { return ValidateIsolation(WorkspaceIsolation(s)) }, []enumCase{
		{input: string(IsolationShared)},
		{input: string(IsolationWorktree)},
		{input: ""},
		{input: "container", wantField: "isolation"},
	})
}

func TestValidateOverflowAction(t *testing.T) {
	checkEnumValidator(t, ValidateOverflowAction, []enumCase{
		{input: "reject"},
		{input: "drop_oldest"},
		{input: ""},
		{input: "discard", wantField: "overflow_action"},
	})
}

func TestValidateBudgetAction(t *testing.T) {
	checkEnumValidator(t, ValidateBudgetAction, []enumCase{
		{input: "warn"},
		{input: "pause"},
		{input: "shutdown"},
		{input: ""},
		{input: "kill", wantField: "budget_action"},
	})
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
	if ws.NumCtx != 0 {
		t.Errorf("expected NumCtx=0 (omitted), got %d", ws.NumCtx)
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
		NumCtx: 65536,
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
	if ws.NumCtx != 65536 {
		t.Errorf("expected NumCtx preserved as 65536, got %d", ws.NumCtx)
	}
}

// =============================================================================
// WorkflowSettings.Validate
// =============================================================================

func TestWorkflowSettings_Validate(t *testing.T) {
	defaults := WorkflowSettings{}
	defaults.ApplyDefaults()
	tests := []struct {
		name      string
		settings  WorkflowSettings
		wantField string
	}{
		{name: "defaults", settings: defaults},
		{name: "invalid topology", settings: WorkflowSettings{Topology: "mesh"}, wantField: "topology"},
		{name: "invalid isolation", settings: WorkflowSettings{Topology: TopologyHubAndSpoke, Isolation: "container"}, wantField: "isolation"},
		{name: "invalid overflow action", settings: WorkflowSettings{PeerMessaging: PeerMessagingSettings{OverflowAction: "discard"}}, wantField: "overflow_action"},
		{name: "invalid budget action", settings: WorkflowSettings{CostControls: CostControlSettings{BudgetAction: "kill"}}, wantField: "budget_action"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.settings.Validate()
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("expected settings to validate, got: %v", err)
				}
				return
			}
			var valErr *WorkflowValidationError
			if !errors.Is(err, ErrWorkflowValidation) || !errors.As(err, &valErr) {
				t.Fatalf("expected *WorkflowValidationError, got: %v", err)
			}
			if valErr.Field != tc.wantField {
				t.Errorf("expected field=%s, got %s", tc.wantField, valErr.Field)
			}
		})
	}
}

// =============================================================================
// WorkflowDefinition.Validate (with settings validation)
// =============================================================================

func TestWorkflowDefinition_Validate(t *testing.T) {
	worker := AgentRoleSpec{Role: "worker", Backend: "claude", Model: "opus"}
	lead := AgentRoleSpec{Role: "lead", Backend: "claude", Model: "opus"}
	tests := []struct {
		name      string
		def       WorkflowDefinition
		wantField string
	}{
		{
			name: "autonomous with settings",
			def: WorkflowDefinition{
				Name: "test-workflow", Mode: WorkflowModeAutonomous, Agents: []AgentRoleSpec{worker},
				Settings: WorkflowSettings{Topology: TopologyHybrid, Isolation: IsolationWorktree},
			},
		},
		{
			name: "constrained with NumCtx",
			def: WorkflowDefinition{
				Name: "ollama-workflow", Mode: WorkflowModeConstrained,
				Agents: []AgentRoleSpec{
					{Role: "lead", Backend: "ollama", Model: "llama3.3:70b-32k", NumCtx: 32768},
					{Role: "worker", Backend: "ollama", Model: "qwen2.5-coder:32b-16k", NumCtx: 16384},
				},
				Settings: WorkflowSettings{NumCtx: 32768},
			},
		},
		{
			name:      "empty name",
			def:       WorkflowDefinition{Mode: WorkflowModeAutonomous, Agents: []AgentRoleSpec{worker}},
			wantField: "name",
		},
		{
			name:      "invalid mode",
			def:       WorkflowDefinition{Name: "test", Mode: "invalid", Agents: []AgentRoleSpec{worker}},
			wantField: "mode",
		},
		{
			name:      "no agents",
			def:       WorkflowDefinition{Name: "test", Mode: WorkflowModeAutonomous},
			wantField: "agents",
		},
		{
			name:      "canned is not a mode",
			def:       WorkflowDefinition{Name: "test", Mode: WorkflowMode("canned"), Agents: []AgentRoleSpec{lead, worker}},
			wantField: "mode",
		},
		{
			name: "invalid settings",
			def: WorkflowDefinition{
				Name: "test-workflow", Mode: WorkflowModeAutonomous, Agents: []AgentRoleSpec{worker},
				Settings: WorkflowSettings{Topology: "invalid_topo"},
			},
			wantField: "topology",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.def.Validate()
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("expected workflow to validate, got: %v", err)
				}
				return
			}
			var valErr *WorkflowValidationError
			if !errors.Is(err, ErrWorkflowValidation) || !errors.As(err, &valErr) {
				t.Fatalf("expected *WorkflowValidationError, got: %v", err)
			}
			if valErr.Field != tc.wantField {
				t.Errorf("expected field=%s, got %s", tc.wantField, valErr.Field)
			}
		})
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
