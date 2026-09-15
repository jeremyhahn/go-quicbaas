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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ovServer is a stand-in OpenVINO Model Server. It answers a scripted sequence
// of completions so a test can drive the tool loop without a model.
type ovServer struct {
	t         *testing.T
	responses []string
	seen      []ovRequest
	calls     int
}

func (s *ovServer) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body ovRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Fatalf("decode request: %v", err)
		}
		s.seen = append(s.seen, body)
		i := s.calls
		s.calls++
		if i >= len(s.responses) {
			s.t.Fatalf("server asked for completion %d with only %d scripted", i+1, len(s.responses))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(s.responses[i]))
	}
}

func answerJSON(text string) string {
	return `{"choices":[{"finish_reason":"stop","index":0,"message":{"role":"assistant","content":` +
		mustJSON(text) + `,"tool_calls":[]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`
}

func toolCallJSON(id, command string) string {
	return `{"choices":[{"finish_reason":"tool_calls","index":0,"message":{"role":"assistant","content":"",` +
		`"tool_calls":[{"id":"` + id + `","type":"function","function":{"name":"run_command","arguments":` +
		mustJSON(`{"command":"`+command+`"}`) + `}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`
}

func mustJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// execRecorder is a container runtime that records what the tool loop ran.
type execRecorder struct {
	ContainerRuntime
	ran    []string
	result ExecResult
	err    error
}

func (e *execRecorder) Exec(_ context.Context, _ string, spec *ExecSpec) (*ExecResult, error) {
	e.ran = append(e.ran, strings.Join(spec.Cmd, " "))
	if e.err != nil {
		return nil, e.err
	}
	r := e.result
	return &r, nil
}

// TestOpenVINOBackend_ToolLoopFeedsResultsBack is the property that makes this
// a backend rather than a chat client: the model asks to run something, the
// backend runs it in the session's container, and the OUTPUT goes back so the
// next round can use it.
func TestOpenVINOBackend_ToolLoopFeedsResultsBack(t *testing.T) {
	srv := &ovServer{t: t, responses: []string{
		toolCallJSON("call-1", "git log --oneline -1"),
		answerJSON("The work is already on main."),
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	runtime := &execRecorder{result: ExecResult{ExitCode: 0, Stdout: "40efc14 remove the guards"}}
	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "qwen3-8b"})
	b.SetContainerRuntime(runtime)
	b.mu.Lock()
	b.sessions["s1"] = &openVINOSession{id: "s1", model: "qwen3-8b", containerID: "c1", workDir: "/workspace"}
	b.mu.Unlock()

	resp, err := b.Prompt(context.Background(), &PromptRequest{SessionID: "s1", Prompt: "Is #132 already done?"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if len(runtime.ran) != 1 || !strings.Contains(runtime.ran[0], "git log") {
		t.Fatalf("the tool call did not reach the container: %v", runtime.ran)
	}
	if len(resp.Content) == 0 || !strings.Contains(resp.Content[0].Text, "already on main") {
		t.Fatalf("the final answer did not come back: %+v", resp.Content)
	}
	// THE RESULT MUST BE IN THE SECOND REQUEST, matched to its call id — a
	// loop that runs the command and does not show the model the output is a
	// loop that judges without evidence.
	second := srv.seen[1]
	var sawResult bool
	for _, m := range second.Messages {
		if m.Role == "tool" && m.ToolCallID == "call-1" && strings.Contains(m.Content, "40efc14") {
			sawResult = true
		}
	}
	if !sawResult {
		t.Fatalf("the command output was not fed back: %+v", second.Messages)
	}
	// Usage accumulates across BOTH completions, not just the last.
	if resp.Usage == nil || resp.Usage.InputTokens != 20 {
		t.Fatalf("usage must accumulate across rounds, got %+v", resp.Usage)
	}
}

// TestOpenVINOBackend_OffersOnlyFunctionTools pins the constraint that decided
// this backend's existence.
//
// OVMS answers "Only function tools are supported" to anything else, which is
// why the vendor CLI cannot drive it — codex advertises its shell and patch
// tools as custom types and has no switch to change them.
func TestOpenVINOBackend_OffersOnlyFunctionTools(t *testing.T) {
	srv := &ovServer{t: t, responses: []string{answerJSON("done")}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "m"})
	if _, err := b.Prompt(context.Background(), &PromptRequest{Prompt: "hi"}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	tools := srv.seen[0].Tools
	if len(tools) == 0 {
		t.Fatal("no tools offered; the loop cannot read the repository")
	}
	for _, tool := range tools {
		if tool.Type != "function" {
			t.Fatalf("tool %q is type %q; OVMS accepts only \"function\"", tool.Function.Name, tool.Type)
		}
	}
}

// TestOpenVINOBackend_BoundsToolRounds proves a turn that never answers is
// reported rather than returned as though it had.
func TestOpenVINOBackend_BoundsToolRounds(t *testing.T) {
	loop := make([]string, 6)
	for i := range loop {
		loop[i] = toolCallJSON("call", "ls")
	}
	srv := &ovServer{t: t, responses: loop}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "m", MaxToolRounds: 3})
	b.SetContainerRuntime(&execRecorder{result: ExecResult{Stdout: "."}})
	b.mu.Lock()
	b.sessions["s"] = &openVINOSession{id: "s", model: "m", containerID: "c"}
	b.mu.Unlock()

	_, err := b.Prompt(context.Background(), &PromptRequest{SessionID: "s", Prompt: "go"})
	var bound *OpenVINOToolRoundsExceededError
	if !errors.As(err, &bound) {
		t.Fatalf("want OpenVINOToolRoundsExceededError, got %T: %v", err, err)
	}
	if bound.Rounds != 3 {
		t.Fatalf("the refusal must name the bound it hit, got %d", bound.Rounds)
	}
}

// TestOpenVINOBackend_ToolFailureIsEvidence keeps a failed command from ending
// the turn: the model asked to run something, and needs to see that it failed.
func TestOpenVINOBackend_ToolFailureIsEvidence(t *testing.T) {
	srv := &ovServer{t: t, responses: []string{
		toolCallJSON("c1", "cat missing.txt"),
		answerJSON("That file does not exist."),
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "m"})
	b.SetContainerRuntime(&execRecorder{result: ExecResult{ExitCode: 1, Stderr: "No such file"}})
	b.mu.Lock()
	b.sessions["s"] = &openVINOSession{id: "s", model: "m", containerID: "c"}
	b.mu.Unlock()

	resp, err := b.Prompt(context.Background(), &PromptRequest{SessionID: "s", Prompt: "read it"})
	if err != nil {
		t.Fatalf("a failed command must not fail the turn: %v", err)
	}
	if len(resp.Content) == 0 || !strings.Contains(resp.Content[0].Text, "does not exist") {
		t.Fatalf("the turn did not continue past the failure: %+v", resp.Content)
	}
	var sawFailure bool
	for _, m := range srv.seen[1].Messages {
		if m.Role == "tool" && strings.Contains(m.Content, "exit status 1") && strings.Contains(m.Content, "No such file") {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Fatal("the model was not shown the exit status and stderr")
	}
}

// TestStripThinking_KeepsTheAnswer covers a thinking model's output. Qwen3
// wraps its reasoning in <think>…</think> and the caller wants the answer; the
// tokens are already recorded in usage.
func TestStripThinking_KeepsTheAnswer(t *testing.T) {
	cases := map[string]string{
		"<think>weighing it up</think>ANSWER":             "ANSWER",
		"before<think>a</think>middle<think>b</think>end": "beforemiddleend",
		"no thinking at all":                              "no thinking at all",
		// An unterminated block is a turn cut off inside its reasoning.
		"partial<think>never closed": "partial",
	}
	for in, want := range cases {
		if got := stripThinking(in); got != want {
			t.Fatalf("stripThinking(%q) = %q, want %q", in, got, want)
		}
	}
}
