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
	"fmt"
	"strings"
	"testing"
)

// =============================================================================
// The stderr tail never reaches a log, through any path
// =============================================================================
//
// THE RULE LIVES ON THE TYPE, not at a log site, and this is the test that says
// why. The supervisor's own log line was fixed to withhold agent stderr; the
// SDLC stage observer then rendered the SAME error type through a different
// path and put it straight back in the daemon log (#187 hosted review). A
// redaction applied where an error is printed is a redaction the next printer
// does not have.
//
// So the assertion is about formatting verbs rather than about any one caller:
// whatever a future call site does with %v, %s, or a wrapped chain, the
// credential-shaped bytes must not come out.

func TestAgentErrors_NeverRenderTheStderrTail(t *testing.T) {
	const secret = "fatal: auth failed for token sk-live-DEADBEEF01234567"

	for _, tc := range []struct {
		name string
		err  error
	}{
		{"stream-json exit", &StreamJSONProcessExitedError{
			Command: "claude", ExitCode: 7, Stderr: secret,
		}},
		{"one-shot execution", &OneShotExecutionError{
			Command: "claude", Stderr: secret, Cause: errors.New("exit status 1"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Every rendering a logger could plausibly reach for, including a
			// wrapped chain — which is how the stage observer got it.
			renderings := []string{
				tc.err.Error(),
				fmt.Sprintf("%v", tc.err),
				fmt.Sprintf("%s", tc.err),
				fmt.Sprintf("%v", fmt.Errorf("stage failed: %w", tc.err)),
				fmt.Sprintf("%v", errors.Join(errors.New("outer"), tc.err)),
			}
			for i, got := range renderings {
				if strings.Contains(got, "sk-live-DEADBEEF01234567") || strings.Contains(got, secret) {
					t.Fatalf("rendering %d leaked the stderr tail: %s", i, got)
				}
				if !strings.Contains(got, "claude") {
					t.Errorf("rendering %d lost the command name: %s", i, got)
				}
				if !strings.Contains(got, "withheld") {
					t.Errorf("rendering %d does not say evidence was withheld: %s", i, got)
				}
			}
		})
	}
}

func TestAgentErrors_TheTailStaysAvailableToCallersThatAskByName(t *testing.T) {
	// Withholding it from the string must not destroy it: the account-limit
	// classifier reads the field to tell a provider limit from a crash, and an
	// operator surface behind authorization can show it deliberately.
	const tail = "Claude AI usage limit reached|1735689600"
	err := &StreamJSONProcessExitedError{Command: "claude", ExitCode: 1, Stderr: tail}

	var exited *StreamJSONProcessExitedError
	if !errors.As(error(err), &exited) {
		t.Fatal("the typed error is no longer reachable through errors.As")
	}
	if exited.Stderr != tail {
		t.Errorf("Stderr = %q, want the captured tail intact", exited.Stderr)
	}
	if got := limitFailureText(err); got != tail {
		t.Errorf("the account-limit classifier can no longer read the tail: %q", got)
	}
}
