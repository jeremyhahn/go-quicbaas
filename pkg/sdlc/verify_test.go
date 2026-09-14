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
// container_command
// =============================================================================

// verifyRig is a JobContext carrying an allowlist and a scripted container,
// which is the whole world the validator has.
type verifyRig struct {
	job     *orchestration.JobContext
	execs   []*orchestration.ExecSpec
	result  *orchestration.ExecResult
	execErr error
}

// newVerifyRig admits every command in allowed and binds a container whose
// checkout is the one path a command may name.
func newVerifyRig(t *testing.T, checkout string, allowed ...string) *verifyRig {
	t.Helper()
	guard, err := orchestration.NewExecutionGuard(allowed)
	if err != nil {
		t.Fatal(err)
	}
	rig := &verifyRig{result: &orchestration.ExecResult{}}
	container, err := orchestration.NewJobContainer("session-1", checkout,
		func(_ context.Context, spec *orchestration.ExecSpec) (*orchestration.ExecResult, error) {
			rig.execs = append(rig.execs, spec)
			if rig.execErr != nil {
				return nil, rig.execErr
			}
			return rig.result, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	rig.job = &orchestration.JobContext{TaskID: testTask, Guard: guard, Container: container}
	return rig
}

// TestValidateInContainer_RunsTheCommandInTheContainersCheckout: the command
// is handed to the container's exec seam, as a shell command line, in the
// directory the CONTAINER calls its checkout — never a daemon path.
func TestValidateInContainer_RunsTheCommandInTheContainersCheckout(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")
	rig.result = &orchestration.ExecResult{Stdout: "build ok\n"}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make build", Validator: ValidatorKindContainerCommand, Config: "make build"})
	if err != nil {
		t.Fatalf("ValidateInContainer: %v", err)
	}
	if !result.Passed || result.Message != "build ok" {
		t.Fatalf("result = %+v, want a pass carrying the command's output", result)
	}
	if len(rig.execs) != 1 {
		t.Fatalf("execs = %d, want exactly one command in the container", len(rig.execs))
	}
	spec := rig.execs[0]
	if spec.WorkDir != "/workspace/api" {
		t.Errorf("work dir = %q, want the container's checkout", spec.WorkDir)
	}
	if len(spec.Cmd) != 3 || spec.Cmd[0] != verificationShell || spec.Cmd[1] != "-c" || spec.Cmd[2] != "make build" {
		t.Errorf("cmd = %v, want the shell asked for by name with the command line", spec.Cmd)
	}
}

// TestValidateInContainer_AFailureCarriesBothStreamsAsEvidence: the exit
// status is the verdict, and what the command said on either stream is what
// the worker is sent back with. A build tool that reports on stdout must not
// send the worker an exit code and nothing else.
func TestValidateInContainer_AFailureCarriesBothStreamsAsEvidence(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make test")
	rig.result = &orchestration.ExecResult{
		ExitCode: 2,
		Stdout:   "--- FAIL: TestThing\n",
		Stderr:   "make: *** [test] Error 1\n",
	}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make test", Config: "make test"})
	if err != nil {
		t.Fatalf("a non-zero exit is a verdict, not an error: %v", err)
	}
	if result.Passed {
		t.Fatal("a command that exited non-zero passed")
	}
	if result.Message != "make: *** [test] Error 1" {
		t.Errorf("message = %q, want the command's stderr", result.Message)
	}
	if !strings.Contains(result.Details, "exit code: 2") || !strings.Contains(result.Details, "TestThing") {
		t.Errorf("details = %q, want the exit code and the stdout evidence", result.Details)
	}
}

// TestValidateInContainer_ACommandTheOperatorNeverAllowedIsRefused: the
// allowlist is checked on the DAEMON, before anything reaches a container,
// and the refusal is the guard's own typed error.
func TestValidateInContainer_ACommandTheOperatorNeverAllowedIsRefused(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "rm", Config: "rm -rf /"})
	var refused *orchestration.CommandRefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a *orchestration.CommandRefusedError", err)
	}
	if !errors.Is(err, orchestration.ErrCommandRefused) {
		t.Errorf("err = %v, does not unwrap to the guard's sentinel", err)
	}
	if refused.Command != "rm -rf /" {
		t.Errorf("the refused command was not carried on the error: %q", refused.Command)
	}
	if strings.Contains(refused.Error(), "rm -rf /") {
		t.Error("the refusal rendered tenant-supplied argv into its message")
	}
	if result.Passed {
		t.Error("a refused command passed")
	}
	if len(rig.execs) != 0 {
		t.Fatalf("a refused command reached the container: %v", rig.execs)
	}
}

// TestValidateInContainer_AJobWithNoContainerIsRefused: verification runs
// where the work is or not at all. There is no daemon path to fall back to,
// and the refusal is typed so a caller can tell it from a failing build.
func TestValidateInContainer_AJobWithNoContainerIsRefused(t *testing.T) {
	guard, err := orchestration.NewExecutionGuard([]string{"make build"})
	if err != nil {
		t.Fatal(err)
	}
	job := &orchestration.JobContext{TaskID: testTask, Guard: guard}

	result, err := ValidateInContainer(context.Background(), job,
		orchestration.ReviewCriterion{Name: "make build", Config: "make build"})
	var refused *VerificationNotContainerizedError
	if !errors.As(err, &refused) {
		t.Fatalf("err = %v, want a *VerificationNotContainerizedError", err)
	}
	if refused.TaskID != testTask || refused.Criterion != "make build" {
		t.Errorf("refusal = %+v, want the task and the criterion named", refused)
	}
	if !errors.Is(err, ErrRunner) {
		t.Errorf("err = %v, does not unwrap to the runner sentinel", err)
	}
	if result.Passed {
		t.Error("a job with no container passed a verification command")
	}
}

// TestValidateInContainer_RefusesAnEmptyCriterion: a criterion IS its
// command, so one carrying none is an input error rather than a silent pass.
func TestValidateInContainer_RefusesAnEmptyCriterion(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")

	for _, config := range []string{"", "   "} {
		result, err := ValidateInContainer(context.Background(), rig.job,
			orchestration.ReviewCriterion{Name: "blank", Config: config})
		var invalid *InvalidInputError
		if !errors.As(err, &invalid) {
			t.Fatalf("config %q: err = %v, want an *InvalidInputError", config, err)
		}
		if result.Passed {
			t.Errorf("config %q passed", config)
		}
	}
	if len(rig.execs) != 0 {
		t.Fatalf("an empty criterion reached the container: %v", rig.execs)
	}
}

// TestValidateInContainer_RefusesANilJob: the validator judges a running job
// and has nothing to judge without one.
func TestValidateInContainer_RefusesANilJob(t *testing.T) {
	result, err := ValidateInContainer(context.Background(), nil,
		orchestration.ReviewCriterion{Name: "make build", Config: "make build"})
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an *InvalidInputError", err)
	}
	if result.Passed {
		t.Error("a nil job passed")
	}
}

// TestValidateInContainer_AContainerThatCouldNotRunTheCommandIsAnError: a
// command that never ran is not a failing build. It is reported as an error
// so the pipeline stops rather than sending the worker a round of feedback
// about a container that is gone.
func TestValidateInContainer_AContainerThatCouldNotRunTheCommandIsAnError(t *testing.T) {
	rig := newVerifyRig(t, "/workspace/api", "make build")
	rig.execErr = &orchestration.ContainerNotFoundError{ContainerID: "container-1"}

	result, err := ValidateInContainer(context.Background(), rig.job,
		orchestration.ReviewCriterion{Name: "make build", Config: "make build"})
	var missing *orchestration.ContainerNotFoundError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, want the runtime's own error", err)
	}
	if result.Passed {
		t.Error("a command that never ran passed")
	}
}
