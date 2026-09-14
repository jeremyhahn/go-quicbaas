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
)

// =============================================================================
// Execution guard
// =============================================================================
//
// A workflow's review criteria are TENANT DATA, and the command validator
// runs a criterion's config through the host shell. Without an operator gate
// a tenant administrator configures arbitrary execution on the daemon host by
// registering a workflow.
//
// The guard is the operator's gate, built from DAEMON configuration
// (orchestration.verification_commands — the system plane, never
// tenant-writable) and enforced at the point of consumption: the command
// validator re-checks every command against the guard before anything runs,
// so a workflow smuggled past registration — an older build, a direct store
// write — is refused where it would have executed.
//
// THE ZERO VALUE REFUSES EVERYTHING. A node whose operator has configured no
// allowlist runs no tenant-named command; enabling one is one config entry,
// visible in the deployment file.

// ExecutionGuard is the operator allowlist over the commands a workflow's
// review criteria may run on the daemon host.
type ExecutionGuard struct {
	commands map[string]struct{}
}

// NewExecutionGuard builds the guard from the operator's configuration.
//
// Commands are matched EXACTLY (after trimming surrounding whitespace) — no
// globbing, no prefix rules, no shell parsing. An exact string is the one
// grammar that cannot be smuggled past.
func NewExecutionGuard(commands []string) (ExecutionGuard, error) {
	g := ExecutionGuard{commands: make(map[string]struct{}, len(commands))}
	for _, c := range commands {
		c = strings.TrimSpace(c)
		if c == "" {
			return ExecutionGuard{}, &ExecutionGuardConfigError{
				Message: "an empty allowlist entry would allow the empty command",
			}
		}
		g.commands[c] = struct{}{}
	}
	return g, nil
}

// CheckCommand reports whether a criterion's command is operator-allowed.
//
// Exact match only. The command is tenant data bound for the host shell, so
// anything the operator did not spell out — one flag more, one space of
// difference — is refused with the config knob named, because the operator
// fixing this needs to know WHERE the allowlist lives, not just that one
// exists.
func (g ExecutionGuard) CheckCommand(name, command string) error {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return &CommandRefusedError{Name: name, Command: command}
	}
	if _, ok := g.commands[trimmed]; !ok {
		return &CommandRefusedError{Name: name, Command: trimmed}
	}
	return nil
}

// =============================================================================
// Errors
// =============================================================================

var (
	// ErrExecutionGuardConfig indicates the allowlist could not be built.
	ErrExecutionGuardConfig = errors.New("execution guard: invalid configuration")

	// ErrCommandRefused indicates a command the allowlist does not carry.
	ErrCommandRefused = errors.New("execution guard: command refused")
)

// ExecutionGuardConfigError reports an allowlist entry the guard refuses to
// be built from.
type ExecutionGuardConfigError struct {
	Message string
}

// Error implements the error interface.
func (e *ExecutionGuardConfigError) Error() string {
	return ErrExecutionGuardConfig.Error() + ": orchestration.verification_commands: " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *ExecutionGuardConfigError) Unwrap() error {
	return ErrExecutionGuardConfig
}

// CommandRefusedError reports a criterion command the operator's allowlist
// does not carry.
type CommandRefusedError struct {
	// Name is the criterion, and it is what Error() renders: it identifies
	// which configured command was refused, which is what makes the refusal
	// actionable.
	Name string

	// Command is the argv the criterion asked for.
	//
	// IT IS NEVER RENDERED BY Error(). This is tenant-supplied text bound
	// for a shell, and a criterion whose argv carries an inline token would
	// have that token republished by every log line and audit event this
	// error reaches. A caller that needs it reads the field.
	Command string
}

// Error implements the error interface.
func (e *CommandRefusedError) Error() string {
	return ErrCommandRefused.Error() + ": criterion " + e.Name +
		" is not in the operator allowlist (orchestration.verification_commands)"
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *CommandRefusedError) Unwrap() error {
	return ErrCommandRefused
}
