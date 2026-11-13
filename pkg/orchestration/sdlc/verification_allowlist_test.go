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

package sdlc

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// X14 — a verification command outside the operator allowlist
// =============================================================================
//
// go-blockstore's binding named "make fmt-check vet golangci-lint guards"
// while the node allowed make build, make test and make lint. The binding was
// accepted, the guard refused the command only after each worker turn, and
// each refusal was charged: twelve refusals, two tasks exhausted.

const refusedCommand = "make fmt-check vet golangci-lint guards"

func testGuard(t *testing.T, commands ...string) orchestration.ExecutionGuard {
	t.Helper()
	guard, err := orchestration.NewExecutionGuard(commands)
	if err != nil {
		t.Fatal(err)
	}
	return guard
}

func TestProjectBinding_CheckVerificationAdmitsAllowlistedCommands(t *testing.T) {
	guard := testGuard(t, "make build", "make test", "make lint")
	for name, commands := range map[string][]string{
		"none":              nil,
		"every one allowed": {"make build", "make test", "make lint"},
		"surrounding space": {"  make lint "},
	} {
		t.Run(name, func(t *testing.T) {
			binding := testBinding()
			binding.Verification = commands
			if err := binding.CheckVerification(guard); err != nil {
				t.Fatalf("CheckVerification = %v", err)
			}
		})
	}
}

func TestProjectBinding_CheckVerificationNamesTheRefusedCommand(t *testing.T) {
	binding := testBinding()
	binding.Verification = []string{"make build", "make test", refusedCommand}
	err := binding.CheckVerification(testGuard(t, "make build", "make test", "make lint"))
	var refused *VerificationCommandRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want VerificationCommandRefusedError", err)
	}
	if refused.Index != 2 || refused.Command != refusedCommand {
		t.Fatalf("refused = %+v, want verification[2] %q", refused, refusedCommand)
	}
	if !errors.Is(err, ErrInvalidInput) || !errors.Is(err, orchestration.ErrCommandRefused) {
		t.Fatalf("err = %v, want both the invalid-input and the command-refused sentinel", err)
	}
	for _, want := range []string{"verification[2]", refusedCommand, "orchestration.verification_commands"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %q, want it to name %q", err.Error(), want)
		}
	}
}

func TestProjectBinding_CheckVerificationUnderAnEmptyAllowlistRefusesEverything(t *testing.T) {
	binding := testBinding()
	binding.Verification = []string{"make build"}
	if err := binding.CheckVerification(orchestration.ExecutionGuard{}); !errors.Is(err, orchestration.ErrCommandRefused) {
		t.Fatalf("err = %v, want the zero-value guard to refuse", err)
	}
}

// TestRunner_AVerificationCommandOffTheAllowlistStartsNoWorker is X14 at
// dispatch: the task is parked for an operator, with the config_error class
// so the attempt is refunded, and no worker is ever spawned.
func TestRunner_AVerificationCommandOffTheAllowlistStartsNoWorker(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true", refusedCommand)
	rig.workerWrote()
	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil && !errors.Is(err, ErrRunner) {
		t.Fatalf("Run: %v", err)
	}
	if rig.agents.sessionCount() != 0 {
		t.Fatal("a worker was spawned for work this node refuses to verify")
	}
	parked := job.Parked()
	if parked == nil {
		t.Fatal("the task was not parked")
	}
	if parked.FaultClass != orchestration.FaultConfigError {
		t.Fatalf("park fault class = %q, want %q; without it the attempt is charged", parked.FaultClass, orchestration.FaultConfigError)
	}
	if parked.Kind != orchestration.TaskBlockKindBlocked {
		t.Fatalf("park kind = %q, want blocked", parked.Kind)
	}
	if !strings.Contains(parked.Message, refusedCommand) {
		t.Fatalf("the park does not name the refused command: %q", parked.Message)
	}
	condition, err := ParseUnblockCondition(parked.Message)
	if err != nil || condition.Kind != UnblockOperatorRepair {
		t.Fatalf("condition = %+v, err = %v, want operator_repair", condition, err)
	}
}

// TestRunner_AnAllowlistedVerificationStartsTheWorker: a binding whose every
// command the node admits spawns its worker and verifies.
func TestRunner_AnAllowlistedVerificationStartsTheWorker(t *testing.T) {
	rig := newRunnerRig(t)
	rig.rebindVerification(t, "true")
	rig.workerWrote()
	rig.verdictOnPublish("APPROVED", "lgtm")
	job := rig.job(t)
	if err := rig.runner.Run(context.Background(), job); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rig.agents.sessionCount() == 0 {
		t.Fatal("no worker was spawned for an allowlisted binding")
	}
	if parked := job.Parked(); parked != nil {
		t.Fatalf("an allowlisted binding was parked: %+v", parked)
	}
}

// TestRunner_AGuardRefusalAtValidationIsAnOperatorFault: a refusal that
// surfaces from the validator (an allowlist narrowed while the job ran) parks
// with config_error; any other validator error is returned as it was.
func TestRunner_AGuardRefusalAtValidationIsAnOperatorFault(t *testing.T) {
	rig := newRunnerRig(t)
	run := &runnerRun{runner: rig.runner, job: rig.job(t)}
	state, err := run.refusedVerification(&orchestration.CommandRefusedError{Name: refusedCommand, Command: refusedCommand})
	if err != nil || state != orchestration.JobStateEscalating {
		t.Fatalf("state = %q, err = %v; want the park's escalating", state, err)
	}
	if parked := run.job.Parked(); parked == nil || parked.FaultClass != orchestration.FaultConfigError {
		t.Fatalf("parked = %+v, want config_error", parked)
	}

	other := errors.New("container runtime gone")
	fresh := &runnerRun{runner: rig.runner, job: rig.job(t)}
	if state, err := fresh.refusedVerification(other); state != "" || !errors.Is(err, other) {
		t.Fatalf("state = %q, err = %v; want the error returned unchanged", state, err)
	}
	if fresh.job.Parked() != nil {
		t.Fatal("an error that is not a guard refusal parked the task")
	}
}
