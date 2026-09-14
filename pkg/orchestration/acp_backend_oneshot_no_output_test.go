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
	"strings"
	"testing"
	"time"
)

// TestOneShotStdoutHelper is a helper subprocess whose entire stdout is
// whatever ONESHOT_STDOUT holds, so a test can present the one-shot reader with
// an exact byte sequence — including none at all — while still exiting 0.
func TestOneShotStdoutHelper(t *testing.T) {
	if os.Getenv("ONESHOT_STDOUT_HELPER") != "1" {
		return
	}
	_, _ = os.Stdout.Write([]byte(os.Getenv("ONESHOT_STDOUT")))
	os.Exit(0)
}

// promptOneShotWithStdout runs one full spawn + prompt against a CLI that
// writes stdout and exits 0, and returns what the caller sees.
func promptOneShotWithStdout(t *testing.T, stdout string, log *captureLogger) (*PromptResponse, error) {
	t.Helper()

	b := NewACPBackend(ACPBackendConfig{Command: "claude", CLIMode: true})
	if log != nil {
		b.SetLogger(log)
	}
	b.processFactory = func(ctx context.Context, _ string, _ []string, workDir string, _ []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestOneShotStdoutHelper")
		cmd.Env = append(os.Environ(), "ONESHOT_STDOUT_HELPER=1", "ONESHOT_STDOUT="+stdout)
		if workDir != "" {
			cmd.Dir = workDir
		}
		return cmd, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sess, err := b.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: false})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	return b.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "go"})
}

// TestACPBackend_OneShot_SilentExitIsNotAnEmptyAnswer is the regression test
// for #241.
//
// A one-shot CLI that exits 0 having written nothing used to reach the
// raw-stdout fallback — an empty buffer fails json.Unmarshal — and produced a
// completed turn whose single content block held the empty string. That is
// BYTE-FOR-BYTE what a CLI reporting {"result":""} produces, so a dead process
// and an empty answer were the same PromptResponse and no caller could tell
// them apart.
//
// The pair of subtests is the whole point: neither half means anything without
// the other.
func TestACPBackend_OneShot_SilentExitIsNotAnEmptyAnswer(t *testing.T) {
	t.Run("a CLI that produced nothing fails the turn", func(t *testing.T) {
		resp, err := promptOneShotWithStdout(t, "", nil)

		if err == nil {
			t.Fatalf("Prompt() succeeded with %+v; a CLI that wrote nothing did not take a turn", resp)
		}
		if !errors.Is(err, ErrOneShotNoOutput) {
			t.Errorf("errors.Is(ErrOneShotNoOutput) = false for %v; the diagnosis must name the silent CLI", err)
		}
		if resp != nil {
			t.Errorf("Prompt() response = %+v, want nil", resp)
		}
	})

	t.Run("whitespace alone is still nothing", func(t *testing.T) {
		// A CLI that emits a bare newline has written no more of an answer than
		// one that wrote nothing, and reading the two differently would put the
		// fail-open back one byte away.
		_, err := promptOneShotWithStdout(t, "   \n\t ", nil)

		if !errors.Is(err, ErrOneShotNoOutput) {
			t.Errorf("errors.Is(ErrOneShotNoOutput) = false for %v", err)
		}
	})

	t.Run("a CLI that reported an empty result still succeeds", func(t *testing.T) {
		// The contrast half. An empty answer the CLI actually CLAIMED is a
		// turn; refusing it too would trade one indistinguishable pair for
		// another, with a working deployment on the losing side.
		resp, err := promptOneShotWithStdout(t, `{"type":"result","result":"","is_error":false}`, nil)

		if err != nil {
			t.Fatalf("Prompt() error = %v; a reported empty result is a completed turn", err)
		}
		if len(resp.Content) != 1 || resp.Content[0].Text != "" {
			t.Errorf("Content = %+v, want one empty text block", resp.Content)
		}
		if resp.StopReason != StopReasonEndTurn {
			t.Errorf("StopReason = %q, want %q", resp.StopReason, StopReasonEndTurn)
		}
	})

	t.Run("an ordinary result is untouched", func(t *testing.T) {
		resp, err := promptOneShotWithStdout(t, `{"type":"result","result":"hello","is_error":false}`, nil)

		if err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}
		if len(resp.Content) != 1 || resp.Content[0].Text != "hello" {
			t.Errorf("Content = %+v, want one text block \"hello\"", resp.Content)
		}
	})
}

// TestACPBackend_OneShot_UnparseableStdoutIsAcceptedButNotSilent covers the
// half of #241 this change deliberately does NOT decide.
//
// Whether the one-shot path may accept unparseable stdout as a turn at all is
// the behaviour question the issue reserves for the owner: a backend whose CLI
// does not honour --output-format json works today because of that fallback.
// The turn's OUTCOME is therefore unchanged. What must no longer be true is
// that taking the fallback leaves no trace.
func TestACPBackend_OneShot_UnparseableStdoutIsAcceptedButNotSilent(t *testing.T) {
	fellBack := func(log *captureLogger) bool {
		return strings.Contains(log.joined(), "not parseable JSON")
	}

	t.Run("a truncated result object is accepted and reported", func(t *testing.T) {
		log := &captureLogger{}

		resp, err := promptOneShotWithStdout(t, `{"type":"result","result":"partial ans`, log)

		// Unchanged: still a turn. This is the owner's call, not this fix's.
		if err != nil {
			t.Fatalf("Prompt() error = %v; the fallback's outcome must not change here", err)
		}
		if resp == nil {
			t.Fatal("Prompt() response = nil")
		}
		if !fellBack(log) {
			t.Error("no diagnostic recorded; a truncated result object accepted as an answer must say the parse was given up")
		}
	})

	t.Run("a usage count outside int64 is reported too", func(t *testing.T) {
		// The whole unmarshal fails, so this arrives as raw text rather than as
		// an error — the case #241 calls out by name.
		log := &captureLogger{}

		if _, err := promptOneShotWithStdout(t,
			`{"type":"result","result":"hi","usage":{"input_tokens":999999999999999999999}}`, log); err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}

		if !fellBack(log) {
			t.Error("no diagnostic recorded for a result object carrying an unrepresentable number")
		}
	})

	t.Run("a parsed result records no fallback", func(t *testing.T) {
		// The contrast half: a diagnostic emitted for every turn would pass
		// every assertion above and tell an operator nothing.
		log := &captureLogger{}

		if _, err := promptOneShotWithStdout(t, `{"type":"result","result":"hello","is_error":false}`, log); err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}

		if fellBack(log) {
			t.Errorf("fallback reported for a well-formed result: %s", log.joined())
		}
	})

	t.Run("the diagnostic does not carry the stdout bytes", func(t *testing.T) {
		// Stdout is model output from a process holding credential material in
		// its environment. This package counts such buffers into diagnostics
		// rather than rendering them (see OneShotExecutionError.Stderr).
		log := &captureLogger{}
		const secret = "sk-do-not-log-this-value"

		if _, err := promptOneShotWithStdout(t, `{"result":"`+secret, log); err != nil {
			t.Fatalf("Prompt() error = %v", err)
		}

		if strings.Contains(log.joined(), secret) {
			t.Errorf("diagnostic rendered the CLI's stdout: %s", log.joined())
		}
		if !fellBack(log) {
			t.Fatal("no diagnostic recorded; the assertion above proves nothing without one")
		}
	})
}
