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

package orchestration

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

// oneShotLimitFixture is the result object Claude Code prints on a turn the
// provider refused for a subscription limit: the measured shape of a failed
// result (subtype "success", is_error true, api_error, all-zero usage) with the
// HTTP status the CLI records for it.
const oneShotLimitFixture = `{"type":"result","subtype":"success","is_error":true,"num_turns":1,` +
	`"terminal_reason":"api_error","api_error_status":429,` +
	`"result":"You've hit your session limit · resets 9pm (America/New_York)",` +
	`"usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`

// TestOneShotExitHelper is a helper subprocess that writes ONESHOT_STDOUT and
// ONESHOT_STDERR and exits with ONESHOT_EXIT, the way the CLI ends a failed
// turn: its result object on stdout AND a non-zero status.
func TestOneShotExitHelper(t *testing.T) {
	if os.Getenv("ONESHOT_EXIT_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.Write([]byte(os.Getenv("ONESHOT_STDOUT")))
	_, _ = os.Stderr.Write([]byte(os.Getenv("ONESHOT_STDERR")))
	code, _ := strconv.Atoi(os.Getenv("ONESHOT_EXIT"))
	os.Exit(code)
}

func promptOneShotExiting(t *testing.T, stdout, stderr string, exit int) error {
	t.Helper()
	b := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})
	b.processFactory = func(ctx context.Context, _ string, _ []string, workDir string, _ []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestOneShotExitHelper")
		cmd.Env = append(os.Environ(), "ONESHOT_EXIT_HELPER=1", "ONESHOT_STDOUT="+stdout,
			"ONESHOT_STDERR="+stderr, "ONESHOT_EXIT="+strconv.Itoa(exit))
		cmd.Dir = workDir
		return cmd, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: t.TempDir(), Persistent: false})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	resp, err := b.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "go"})
	if resp != nil {
		t.Fatalf("Prompt succeeded with %+v, want the failure", resp)
	}
	return err
}

// TestACPBackend_OneShot_AStructured429WithExit1IsALimit is the round-3
// regression: the CLI exits 1 on the refused turn, and the limit it printed
// must still reach the classifier.
func TestACPBackend_OneShot_AStructured429WithExit1IsALimit(t *testing.T) {
	err := promptOneShotExiting(t, oneShotLimitFixture, "", 1)

	evidence, ok := EvidenceOf(err)
	if !ok {
		t.Fatalf("err = %v carries no turn evidence; the exit status hid the result object", err)
	}
	if evidence.ProviderLimit == nil || evidence.ProviderLimit.Source != ProviderLimitSourceHTTPStatus {
		t.Fatalf("ProviderLimit = %+v, want the 429 read from the result object", evidence.ProviderLimit)
	}
	if evidence.ProviderLimit.ResetsAt.IsZero() {
		t.Error("the stated reset was not parsed from the result text")
	}
	if _, limited := NewAccountLimitClassifier().Classify(AccountProviderClaude, err); !limited {
		t.Fatal("the classifier did not see a limit")
	}
}

// TestACPBackend_OneShot_AProcessErrorWithoutAResultStaysTheProcessError is
// the fallback half: a crash that printed no result object is still reported
// as the process failure it is.
func TestACPBackend_OneShot_AProcessErrorWithoutAResultStaysTheProcessError(t *testing.T) {
	err := promptOneShotExiting(t, "panic: something", "fatal: crashed", 2)

	var execErr *OneShotExecutionError
	if !errors.As(err, &execErr) {
		t.Fatalf("err = %v, want a OneShotExecutionError", err)
	}
	if execErr.Stderr != "fatal: crashed" {
		t.Errorf("Stderr = %q, want the process's own stderr", execErr.Stderr)
	}
	if execErr.ProviderLimit != nil || execErr.TerminalReason != "" {
		t.Errorf("evidence = %+v, want none: nothing structured was printed", execErr.TurnFailureEvidence)
	}
}
