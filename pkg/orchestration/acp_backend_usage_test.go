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
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

// =============================================================================
// Stream-json usage helper subprocess
//
// The real agent CLI publishes token counts on its stream-json output. This
// helper reproduces the three shapes the parser has to handle — usage on the
// terminal result message, usage only on the assistant messages, and no usage
// at all — so the parsing is proved against the wire format rather than against
// a hand-built struct.
// =============================================================================

const (
	usageHelperEnv     = "STREAM_JSON_USAGE_HELPER"
	usageHelperModeEnv = "STREAM_JSON_USAGE_MODE"

	// usageModeResult puts the turn aggregate on the result message and the
	// per-message components on the assistant messages, exactly as the CLI
	// does. A parser that sums both bills the turn twice.
	usageModeResult = "result"

	// usageModeAssistant publishes counts only on the assistant messages, which
	// is what a turn cut short before its result aggregate looks like.
	usageModeAssistant = "assistant"

	// usageModeNone publishes no counts at all.
	usageModeNone = "none"
)

// TestStreamJSONUsageHelper is the subprocess half of the usage parsing tests.
// It is inert unless the parent process explicitly invokes it.
func TestStreamJSONUsageHelper(t *testing.T) {
	if os.Getenv(usageHelperEnv) != "1" {
		return
	}

	mode := os.Getenv(usageHelperModeEnv)
	const sessionID = "usage-test-session"
	const model = "claude-sonnet-4-5-20250514"

	writeMessage := func(msg streamJSONMessage) {
		encoded, err := json.Marshal(msg)
		if err != nil {
			os.Exit(1)
		}
		if _, writeErr := os.Stdout.Write(append(encoded, '\n')); writeErr != nil {
			os.Exit(1)
		}
	}

	writeMessage(streamJSONMessage{Type: "system", Subtype: "init", SessionID: sessionID, Model: model})

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var userMsg streamJSONUserMessage
		if err := json.Unmarshal(scanner.Bytes(), &userMsg); err != nil {
			continue
		}

		// Two assistant messages per turn, so the accumulation path is
		// exercised rather than a single-message shortcut.
		for i := 0; i < 2; i++ {
			assistant := &streamJSONAssistant{
				Role:    "assistant",
				Content: []streamJSONContent{{Type: "text", Text: "part"}},
				Model:   model,
			}
			if mode == usageModeResult || mode == usageModeAssistant {
				assistant.Usage = &TokenUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 1}
			}
			writeMessage(streamJSONMessage{Type: "assistant", Message: assistant})
		}

		result := streamJSONMessage{
			Type:      "result",
			Subtype:   "success",
			SessionID: sessionID,
			Result:    "done",
		}
		if mode == usageModeResult {
			// The CLI's aggregate: the sum of the two assistant messages.
			result.Usage = &TokenUsage{InputTokens: 20, OutputTokens: 10, CacheReadInputTokens: 2}
		}
		writeMessage(result)
	}
}

// newUsageReportingCLIBackend wires an ACPBackend to the usage helper
// subprocess in the given publication mode.
func newUsageReportingCLIBackend(t *testing.T, mode string) *ACPBackend {
	t.Helper()

	backend := NewACPBackend(ACPBackendConfig{
		Command:      "claude",
		Args:         []string{"--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose"},
		DefaultModel: "claude-sonnet-4-5-20250514",
		CLIMode:      true,
	})
	backend.processFactory = func(ctx context.Context, command string, args []string, workDir string, env []string) (*exec.Cmd, error) {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestStreamJSONUsageHelper")
		cmd.Env = append(os.Environ(), usageHelperEnv+"=1", usageHelperModeEnv+"="+mode)
		if workDir != "" {
			cmd.Dir = workDir
		}
		return cmd, nil
	}
	return backend
}

// promptUsageBackend spawns a persistent session, runs one turn, and returns
// the usage the backend reported for it.
func promptUsageBackend(t *testing.T, backend *ACPBackend) *TokenUsage {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	sess, err := backend.Spawn(ctx, &SpawnRequest{WorkDir: "/tmp", Persistent: true})
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}
	t.Cleanup(func() {
		if closeErr := backend.Close(context.Background(), sess.ID); closeErr != nil {
			t.Logf("Close() error = %v", closeErr)
		}
	})

	resp, err := backend.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "hello"})
	if err != nil {
		t.Fatalf("Prompt() error = %v", err)
	}
	return resp.Usage
}

// TestACPBackend_StreamJSON_ReportsResultAggregateUsage is the load-bearing
// case: the agent CLI publishes the turn's token counts, and the backend must
// surface exactly the CLI's own aggregate. Without this the cost ledger has no
// input at all and LeastUsageAccountStrategy has nothing to balance on.
func TestACPBackend_StreamJSON_ReportsResultAggregateUsage(t *testing.T) {
	usage := promptUsageBackend(t, newUsageReportingCLIBackend(t, usageModeResult))

	if usage == nil {
		t.Fatal("PromptResponse.Usage = nil for a turn whose CLI published token counts")
	}
	if usage.InputTokens != 20 || usage.OutputTokens != 10 || usage.CacheReadInputTokens != 2 {
		t.Fatalf("Usage = %+v, want the result message's aggregate (20/10/2), not the summed per-message counts", usage)
	}
	if got := usage.Total(); got != 32 {
		t.Fatalf("Usage.Total() = %d, want 32 — summing the assistant messages on top would bill the turn twice", got)
	}
}

// TestACPBackend_StreamJSON_FallsBackToAssistantUsage covers a turn that ends
// without the CLI's aggregate. The per-message counts are real measurements and
// must still reach the ledger.
func TestACPBackend_StreamJSON_FallsBackToAssistantUsage(t *testing.T) {
	usage := promptUsageBackend(t, newUsageReportingCLIBackend(t, usageModeAssistant))

	if usage == nil {
		t.Fatal("PromptResponse.Usage = nil although each assistant message carried token counts")
	}
	if got := usage.Total(); got != 32 {
		t.Fatalf("Usage.Total() = %d, want 32 (two assistant messages of 16)", got)
	}
}

// TestACPBackend_StreamJSON_NoUsageReportedStaysNil is the honesty case: a CLI
// that publishes no counts must leave Usage nil so the turn is absent from the
// ledger rather than represented by a fabricated figure.
func TestACPBackend_StreamJSON_NoUsageReportedStaysNil(t *testing.T) {
	usage := promptUsageBackend(t, newUsageReportingCLIBackend(t, usageModeNone))

	if usage != nil {
		t.Fatalf("PromptResponse.Usage = %+v for a turn that reported no token counts, want nil", usage)
	}
}
