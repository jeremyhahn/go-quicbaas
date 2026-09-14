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
// The engine's validator registry — the seam a module registers into
// =============================================================================

// TestValidatorRegistry_BuiltinsRegisteredAtInit: the three kinds a workflow
// could always name are present in the engine's registry before anything
// registers, so an existing workflow validates unchanged.
func TestValidatorRegistry_BuiltinsRegisteredAtInit(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{ValidatorKindAgent, ValidatorKindCommand, ValidatorKindPolicy} {
		if _, err := DefaultValidatorRegistry().Get(kind); err != nil {
			t.Errorf("built-in %q not registered: %v", kind, err)
		}
		if err := ValidateValidatorKind(kind); err != nil {
			t.Errorf("built-in %q refused at workflow validation: %v", kind, err)
		}
	}
}

// TestRegisterValidator_ModuleKindBecomesValid: a kind a module registers is
// what a workflow may name from then on, and a second registration under the
// same kind is refused typed.
func TestRegisterValidator_ModuleKindBecomesValid(t *testing.T) {
	t.Parallel()
	const kind = "test-module-verdict"
	if err := ValidateValidatorKind(kind); !errors.Is(err, ErrWorkflowValidation) {
		t.Fatalf("an unregistered kind must be refused at workflow validation, got %v", err)
	}
	registerValidatorForTest(t, kind, fixedVerdict(true, "module says yes"))
	if err := ValidateValidatorKind(kind); err != nil {
		t.Fatalf("a registered kind must validate: %v", err)
	}
	var dup *ValidatorAlreadyRegisteredError
	if err := RegisterValidator(kind, fixedVerdict(false, "")); !errors.As(err, &dup) || dup.Kind != kind {
		t.Fatalf("second registration: got %v, want ValidatorAlreadyRegisteredError", err)
	}

	results, allPassed, err := DefaultValidatorRegistry().RunPipeline(context.Background(), jobWithArtifacts(),
		[]ReviewCriterion{{Name: "m", Validator: kind, Required: true}})
	if err != nil || !allPassed || len(results) != 1 || results[0].Message != "module says yes" {
		t.Fatalf("results=%v allPassed=%v err=%v", results, allPassed, err)
	}
}

// TestValidateValidatorKind_UnknownRefusedAtWorkflowValidation: a workflow
// naming a kind nothing registered fails validation with a typed error that
// unwraps to ErrWorkflowValidation, so registration refuses it.
func TestValidateValidatorKind_UnknownRefusedAtWorkflowValidation(t *testing.T) {
	t.Parallel()
	err := ValidateValidatorKind("nobody-registered-this")
	var unknown *UnknownValidatorError
	if !errors.As(err, &unknown) || unknown.Kind != "nobody-registered-this" {
		t.Fatalf("got %v, want UnknownValidatorError", err)
	}
	if !errors.Is(err, ErrWorkflowValidation) {
		t.Fatal("an unknown validator kind must be a workflow validation failure")
	}

	wf := &WorkflowDefinition{
		Name: "w", Mode: WorkflowModeAutonomous,
		Agents: []AgentRoleSpec{{Role: "worker", Backend: "claude"}},
		Settings: WorkflowSettings{ReviewCriteria: []ReviewCriterion{
			{Name: "ok", Validator: ValidatorKindPolicy},
			{Name: "bad", Validator: "nobody-registered-this"},
		}},
	}
	if err := wf.Validate(); !errors.As(err, &unknown) {
		t.Fatalf("workflow validation: got %v, want UnknownValidatorError", err)
	}
}

// TestCommandKind_RefusesNonAllowlistedThroughTheRegistry: the registered
// "command" kind applies the operator's guard, so a workflow criterion
// naming a command the allowlist does not carry is refused typed rather than
// run, and the pipeline stops on it.
func TestCommandKind_RefusesNonAllowlistedThroughTheRegistry(t *testing.T) {
	t.Parallel()
	job := &JobContext{WorkDir: t.TempDir(), Guard: guardAllowing(t, "true")}
	criteria := []ReviewCriterion{
		{Name: "allowed", Validator: ValidatorKindCommand, Config: "true", Required: true},
		{Name: "smuggled", Validator: ValidatorKindCommand, Config: "true; echo pwned", Required: true},
	}
	results, allPassed, err := DefaultValidatorRegistry().RunPipeline(context.Background(), job, criteria)
	var refused *CommandRefusedError
	if !errors.As(err, &refused) || refused.Name != "smuggled" {
		t.Fatalf("got %v, want CommandRefusedError for the smuggled criterion", err)
	}
	if !errors.Is(err, ErrValidatorExecution) {
		t.Fatal("the refusal must surface as a validator execution error")
	}
	if allPassed || len(results) != 1 || !results[0].Passed {
		t.Fatalf("the allowed criterion runs, the smuggled one stops the pipeline: results=%v allPassed=%v", results, allPassed)
	}
}

// TestControlLoop_ValidatorsFallsBackToTheEngineRegistry: a loop with no
// injected registry judges through the engine's, so a module's kind reaches
// every job.
func TestControlLoop_ValidatorsFallsBackToTheEngineRegistry(t *testing.T) {
	t.Parallel()
	cl := NewControlLoop(&controlLoopMockService{}, &controlLoopMockLogger{})
	if cl.validators() != DefaultValidatorRegistry() {
		t.Fatal("expected the engine registry")
	}
	own := NewValidatorRegistry()
	cl.job.validatorRegistry = own
	if cl.validators() != own {
		t.Fatal("an injected registry must win")
	}
}
