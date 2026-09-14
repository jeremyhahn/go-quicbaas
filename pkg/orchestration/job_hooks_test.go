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
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// Registry
// =============================================================================

func TestJobHooksRegistry_RegisterLookupAndDuplicate(t *testing.T) {
	t.Parallel()
	reg := NewJobHooksRegistry()
	if err := reg.Register("set", hooksTestFake{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := reg.Lookup("set"); !ok {
		t.Fatal("registered hooks must be found")
	}
	if _, ok := reg.Lookup("other"); ok {
		t.Fatal("unregistered name must miss")
	}
	var dup *JobHooksAlreadyRegisteredError
	if err := reg.Register("set", hooksTestFake{}); !errors.As(err, &dup) || dup.Name != "set" ||
		!errors.Is(err, ErrJobHooksAlreadyRegistered) || !strings.Contains(err.Error(), "set") {
		t.Fatalf("duplicate: got %v, want JobHooksAlreadyRegisteredError naming the set", err)
	}
	var regErr *JobHooksRegistrationError
	if err := reg.Register("", hooksTestFake{}); !errors.As(err, &regErr) || !errors.Is(err, ErrJobHooksRegistration) {
		t.Fatalf("empty name: %v", err)
	}
	if err := reg.Register("nil", nil); !errors.As(err, &regErr) || regErr.Name != "nil" {
		t.Fatalf("nil hooks: %v", err)
	}
}

func TestRegisterJobHooks_EngineRegistry(t *testing.T) {
	t.Parallel()
	const name = "job-hooks-test-engine"
	if err := ValidateJobHooksName(name); !errors.Is(err, ErrWorkflowValidation) {
		t.Fatalf("unregistered name must fail validation: %v", err)
	}
	registerJobHooksForTest(t, name, hooksTestFake{})
	if _, ok := LookupJobHooks(name); !ok {
		t.Fatal("LookupJobHooks must find the engine-registered set")
	}
	if err := ValidateJobHooksName(name); err != nil {
		t.Fatalf("registered name must validate: %v", err)
	}
	if err := ValidateJobHooksName(""); err != nil {
		t.Fatalf("empty name means no hooks and is valid: %v", err)
	}
}

// =============================================================================
// Stage timeouts
// =============================================================================

func TestStageTimeouts_WithDefaultsAndFor(t *testing.T) {
	t.Parallel()
	var zero StageTimeouts
	filled := zero.WithDefaults()
	if filled != DefaultStageTimeouts {
		t.Fatalf("WithDefaults on zero = %+v, want %+v", filled, DefaultStageTimeouts)
	}
	custom := StageTimeouts{Prepare: time.Second, Discard: -1}.WithDefaults()
	if custom.Prepare != time.Second || custom.Discard != DefaultStageTimeouts.Discard || custom.Blocked != DefaultStageTimeouts.Blocked {
		t.Fatalf("WithDefaults must keep set fields and fill the rest: %+v", custom)
	}

	bounds := StageTimeouts{Prepare: 1, Reviewable: 2, Complete: 3, Discard: 4, Blocked: 5}
	for stage, want := range map[JobStage]time.Duration{
		JobStagePrepare: 1, JobStageReviewable: 2, JobStageComplete: 3, JobStageDiscard: 4, JobStageBlocked: 5,
	} {
		if got := bounds.For(stage); got != want {
			t.Errorf("For(%s) = %v, want %v", stage, got, want)
		}
	}
	if got := bounds.For("unknown"); got != 4 {
		t.Fatalf("an unknown stage takes the discard bound, got %v", got)
	}
	if got := (StageTimeouts{}).For(JobStagePrepare); got != DefaultStageTimeouts.Prepare {
		t.Fatalf("a zero bound takes the default, got %v", got)
	}
}

// TestStageTimeouts_ConfigDefaultsMatchEngine pins the shipped
// orchestration.stage_timeouts to the engine's defaults layer so the two
// cannot drift.
func TestStageTimeouts_ConfigDefaultsMatchEngine(t *testing.T) {
	t.Parallel()
	shipped := config.DefaultConfig().Orchestration.StageTimeouts
	engine := DefaultStageTimeouts
	if shipped.Prepare != engine.Prepare || shipped.Reviewable != engine.Reviewable ||
		shipped.Complete != engine.Complete || shipped.Cleanup != engine.Discard || shipped.Blocked != engine.Blocked {
		t.Fatalf("config stage_timeouts %+v drifted from DefaultStageTimeouts %+v", shipped, engine)
	}
}

// =============================================================================
// Errors
// =============================================================================

func TestJobHooksErrors(t *testing.T) {
	t.Parallel()
	cause := errors.New("cause")
	hookErr := &JobHookError{Stage: JobStagePrepare, Hooks: "h", JobID: "j", Cause: cause}
	if !errors.Is(hookErr, ErrJobHook) || !errors.Is(hookErr, cause) {
		t.Fatal("JobHookError must unwrap to the sentinel and the cause")
	}
	for _, want := range []string{"stage=prepare", "hooks=h", "job=j", "cause"} {
		if !strings.Contains(hookErr.Error(), want) {
			t.Errorf("JobHookError missing %q: %v", want, hookErr)
		}
	}
	bare := &JobHookError{Stage: JobStageDiscard}
	if !errors.Is(bare, ErrJobHook) || strings.HasSuffix(bare.Error(), ": ") {
		t.Fatalf("JobHookError without cause: %v", bare)
	}
	regErr := &JobHooksRegistrationError{Message: "m"}
	if !strings.HasSuffix(regErr.Error(), ": m") {
		t.Fatalf("registration error without name: %v", regErr)
	}
	unknown := &UnknownJobHooksError{Name: "n"}
	if !errors.Is(unknown, ErrWorkflowValidation) || !strings.Contains(unknown.Error(), "n") {
		t.Fatalf("unknown hooks: %v", unknown)
	}
}
