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
	"io"
	"os"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/inference"
)

// =============================================================================
// Every backend's turn reaches the ledger with its tokens (go-quicbaas#1058)
// =============================================================================

// opencodePromptResult is the shape opencode 1.18's ACP agent returns for
// session/prompt, as read from its shipped build: the usage object sits beside
// stopReason, with reasoning and both cache counts reported apart from input
// and output.
const opencodePromptResult = `{"stopReason":"end_turn","usage":{"inputTokens":1200,"outputTokens":300,` +
	`"thoughtTokens":50,"cachedReadTokens":8000,"cachedWriteTokens":400,"totalTokens":9950},"_meta":{}}`

func TestACPPromptUsageMapsOpencodeAccounting(t *testing.T) {
	var result SessionPromptResult
	if err := json.Unmarshal([]byte(opencodePromptResult), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	usage := result.Usage.TokenUsage()
	if usage == nil {
		t.Fatal("usage = nil; opencode reported its accounting and it must be kept")
	}
	// Reasoning tokens are generated tokens and bill as output.
	if usage.InputTokens != 1200 || usage.OutputTokens != 350 ||
		usage.CacheReadInputTokens != 8000 || usage.CacheCreationInputTokens != 400 {
		t.Fatalf("usage = %+v, want input 1200, output 350, cache read 8000, cache write 400", usage)
	}
	if usage.Total() != 9950 {
		t.Fatalf("Total = %d, want opencode's own totalTokens 9950", usage.Total())
	}
}

// TestACPPromptUsageRepairsEachDimensionBeforeCombining is the round-2 review
// finding on #1063: output 50 plus reasoning -50 used to become a clean zero
// that dropped the valid output; the negative reasoning is repaired first and
// the record is marked malformed.
func TestACPPromptUsageRepairsEachDimensionBeforeCombining(t *testing.T) {
	t.Parallel()
	usage := (&ACPPromptUsage{InputTokens: 100, OutputTokens: 50, ThoughtTokens: -50, CachedReadTokens: -20, CachedWriteTokens: 3}).TokenUsage()
	if usage.OutputTokens != 50 {
		t.Fatalf("output = %d, want the valid 50 kept", usage.OutputTokens)
	}
	if usage.InputTokens != 100 || usage.CacheReadInputTokens != 0 || usage.CacheCreationInputTokens != 3 {
		t.Fatalf("usage = %+v", usage)
	}
	if !usage.Malformed() {
		t.Fatal("a record with negative dimensions must be marked malformed")
	}
	clean := (&ACPPromptUsage{InputTokens: 1, OutputTokens: 2, ThoughtTokens: 3}).TokenUsage()
	if clean.Malformed() || clean.OutputTokens != 5 {
		t.Fatalf("clean record = %+v malformed=%t", clean, clean.Malformed())
	}
}

func TestACPPromptUsageAbsentIsReportedNothing(t *testing.T) {
	var result SessionPromptResult
	if err := json.Unmarshal([]byte(`{"stopReason":"end_turn"}`), &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if usage := result.Usage.TokenUsage(); usage != nil {
		t.Fatalf("usage = %+v, want nil — no accounting is not zero accounting", usage)
	}
}

func TestACPPromptUsageRepairsNegativeCounts(t *testing.T) {
	usage := (&ACPPromptUsage{InputTokens: -5, OutputTokens: 3}).TokenUsage()
	if usage.InputTokens != 0 || !usage.Malformed() {
		t.Fatalf("usage = %+v malformed=%t, want the negative count clamped and marked", usage, usage.Malformed())
	}
}

func TestParsePromptResultCarriesUsage(t *testing.T) {
	b := NewACPBackend(ACPBackendConfig{Command: "/bin/false"})
	var raw any
	if err := json.Unmarshal([]byte(opencodePromptResult), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp, err := b.parsePromptResult(&JSONRPCMessage{Result: raw}, "s1")
	if err != nil {
		t.Fatalf("parsePromptResult: %v", err)
	}
	if resp.Usage == nil || resp.Usage.Total() != 9950 {
		t.Fatalf("Usage = %+v, want the result's accounting", resp.Usage)
	}
}

func TestDecodePromptResultRefusesAMalformedResult(t *testing.T) {
	if _, err := decodePromptResult(&JSONRPCMessage{Result: "not an object"}); err == nil {
		t.Fatal("a result that is not an object must fail to decode")
	}
}

func TestCountedUsage(t *testing.T) {
	tests := []struct {
		name              string
		prompt, generated int64
		wantNil           bool
	}{
		{"both reported", 120, 30, false},
		{"only generation reported", 0, 30, false},
		{"nothing reported", 0, 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage := countedUsage(tc.prompt, tc.generated)
			if tc.wantNil {
				if usage != nil {
					t.Fatalf("usage = %+v, want nil", usage)
				}
				return
			}
			if usage == nil || usage.InputTokens != tc.prompt || usage.OutputTokens != tc.generated {
				t.Fatalf("usage = %+v, want %d/%d", usage, tc.prompt, tc.generated)
			}
		})
	}
}

func TestAPIInferenceBackendPromptReportsProviderUsage(t *testing.T) {
	be, err := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{
			provider: "openai",
			resp: &inference.GenerateResponse{
				Content: "hi", Model: "gpt-5", FinishReason: inference.FinishReasonStop,
				Usage: inference.Usage{PromptTokens: 11, CompletionTokens: 4, TotalTokens: 15},
			},
		},
		DefaultModel: "gpt-5",
	})
	if err != nil {
		t.Fatalf("NewAPIInferenceBackend: %v", err)
	}
	resp, err := be.Prompt(context.Background(), &PromptRequest{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Usage == nil || resp.Usage.Total() != 15 {
		t.Fatalf("Usage = %+v, want 11 in / 4 out", resp.Usage)
	}
}

func TestAPIInferenceBackendPromptWithoutUsageReportsNothing(t *testing.T) {
	be, err := NewAPIInferenceBackend(APIInferenceBackendConfig{
		Wrapped: &stubInferenceBackend{
			provider: "openai",
			resp:     &inference.GenerateResponse{Content: "hi", Model: "gpt-5", FinishReason: inference.FinishReasonStop},
		},
		DefaultModel: "gpt-5",
	})
	if err != nil {
		t.Fatalf("NewAPIInferenceBackend: %v", err)
	}
	resp, err := be.Prompt(context.Background(), &PromptRequest{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.Usage != nil {
		t.Fatalf("Usage = %+v, want nil when the provider sent none", resp.Usage)
	}
}

// opencodeResponder plays opencode's side of an ACP JSON-RPC session: the
// answer arrives as session/update notifications and the session/prompt
// result carries only the stop reason and the turn's usage.
func opencodeResponder(r io.Reader, w io.WriteCloser, done <-chan struct{}) {
	defer w.Close()
	scanner := bufio.NewScanner(r)
	write := func(msg JSONRPCMessage) bool {
		data, err := json.Marshal(msg)
		if err != nil {
			return false
		}
		_, err = w.Write(append(data, '\n'))
		return err == nil
	}
	for scanner.Scan() {
		select {
		case <-done:
			return
		default:
		}
		var msg JSONRPCMessage
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil || msg.ID == nil {
			continue
		}
		resp := JSONRPCMessage{JSONRPC: JSONRPCVersion, ID: msg.ID}
		switch msg.Method {
		case "initialize":
			resp.Result = InitializeResult{AgentInfo: ServerInfo{Name: "opencode", Version: "1.18.29"}}
		case "session/new":
			resp.Result = SessionNewResult{SessionID: "oc-session"}
		case "session/prompt":
			if !write(JSONRPCMessage{JSONRPC: JSONRPCVersion, Method: "session/update", Params: map[string]any{
				"sessionId": "oc-session",
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"content":       map[string]any{"type": "text", "text": "LGTM"},
				},
			}}) {
				return
			}
			var raw any
			if err := json.Unmarshal([]byte(opencodePromptResult), &raw); err != nil {
				return
			}
			resp.Result = raw
		default:
			resp.Result = map[string]string{"status": "ok"}
		}
		if !write(resp) {
			return
		}
	}
}

// TestPromptJSONRPCKeepsTheUsageOfAStreamedTurn is the go-quicbaas#1058
// regression for opencode: its answer arrives as notifications, and the
// result object — which carries the turn's usage — used to be discarded
// whenever any content had streamed, so every opencode turn was recorded as
// having reported nothing.
func TestPromptJSONRPCKeepsTheUsageOfAStreamedTurn(t *testing.T) {
	backend := NewACPBackend(ACPBackendConfig{Command: "opencode", DefaultModel: "ollama/review-q8:latest"})
	done := make(chan struct{})
	defer close(done)
	backend.transportFactory = func(string, []string, string, []string) (*StdioTransport, error) {
		serverRead, clientWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		clientRead, serverWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		go opencodeResponder(serverRead, serverWrite, done)
		return newStdioTransportFromPipes(clientWrite, clientRead), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	sess, err := backend.Spawn(ctx, &SpawnRequest{ProjectID: "p1", WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() { _ = backend.Close(context.Background(), sess.ID) }()

	resp, err := backend.Prompt(ctx, &PromptRequest{SessionID: sess.ID, Prompt: "review"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "LGTM" {
		t.Fatalf("Content = %+v, want the streamed answer", resp.Content)
	}
	if resp.Usage == nil || resp.Usage.Total() != 9950 || resp.Usage.CacheReadInputTokens != 8000 {
		t.Fatalf("Usage = %+v, want opencode's reported accounting", resp.Usage)
	}
}
