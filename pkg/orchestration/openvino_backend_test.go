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

// answerOf is the text block a response ends with, or "" when it has none:
// tool_use blocks precede it, so the answer is no longer Content[0].
func answerOf(resp *PromptResponse) string {
	for i := len(resp.Content) - 1; i >= 0; i-- {
		if resp.Content[i].Type == "text" {
			return resp.Content[i].Text
		}
	}
	return ""
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
	if !strings.Contains(answerOf(resp), "already on main") {
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

// TestOpenVINOBackend_ReportsEveryToolCallAsABlock proves a turn's audit can
// tell a judgement from a give-up: each command the loop ran is a tool_use
// block before the answer, and an answer forced on the final round carries
// StopReasonToolBudgetSpent rather than end_turn.
func TestOpenVINOBackend_ReportsEveryToolCallAsABlock(t *testing.T) {
	srv := &ovServer{t: t, responses: []string{
		toolCallJSON("call-1", "git log --oneline -1"),
		toolCallJSON("call-2", "cat go.mod"),
		answerJSON("Judged from two reads."),
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	runtime := &execRecorder{result: ExecResult{ExitCode: 0, Stdout: "ok"}}
	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "qwen3-8b"})
	b.SetContainerRuntime(runtime)
	b.mu.Lock()
	b.sessions["s1"] = &openVINOSession{id: "s1", model: "qwen3-8b", containerID: "c1", workDir: "/workspace"}
	b.mu.Unlock()

	resp, err := b.Prompt(context.Background(), &PromptRequest{SessionID: "s1", Prompt: "judge"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	var tools, texts int
	for _, block := range resp.Content {
		switch block.Type {
		case ContentBlockToolUse:
			tools++
			if !strings.Contains(block.Text, "run_command") {
				t.Fatalf("a tool_use block names the tool, got %q", block.Text)
			}
			if block.ToolName != "run_command" || block.ToolUseID == "" || block.Bytes == 0 {
				t.Fatalf("a tool_use block carries the name, id and argument size, got %+v", block)
			}
		case "text":
			texts++
		}
	}
	if tools != 2 || texts != 1 {
		t.Fatalf("want 2 tool_use blocks and 1 text block, got %+v", resp.Content)
	}
	if resp.ToolUse == nil || resp.ToolUse.Calls != 2 {
		t.Fatalf("ToolUse = %+v, want a census of 2: the loop ran every tool call itself", resp.ToolUse)
	}
	if resp.StopReason != StopReasonEndTurn {
		t.Fatalf("an answer given with rounds to spare is end_turn, got %q", resp.StopReason)
	}
}

// TestOpenVINOBackend_AForcedAnswerSaysSo proves the stop reason distinguishes
// the final-round answer: the model spent every round on tools and was then
// told to conclude.
func TestOpenVINOBackend_AForcedAnswerSaysSo(t *testing.T) {
	srv := &ovServer{t: t, responses: []string{
		toolCallJSON("call-1", "ls"),
		toolCallJSON("call-2", "ls"),
		answerJSON("SDLC-TRIAGE: not-actionable"),
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()
	runtime := &execRecorder{result: ExecResult{ExitCode: 0, Stdout: "ok"}}
	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "qwen3-8b", MaxToolRounds: 2})
	b.SetContainerRuntime(runtime)
	b.mu.Lock()
	b.sessions["s1"] = &openVINOSession{id: "s1", model: "qwen3-8b", containerID: "c1", workDir: "/workspace"}
	b.mu.Unlock()

	resp, err := b.Prompt(context.Background(), &PromptRequest{SessionID: "s1", Prompt: "judge"})
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if resp.StopReason != StopReasonToolBudgetSpent {
		t.Fatalf("an answer forced on the final round reports tool_budget_spent, got %q", resp.StopReason)
	}
	if len(resp.Content) != 3 {
		t.Fatalf("two tool_use blocks then the answer, got %+v", resp.Content)
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
	if len(resp.Content) == 0 || !strings.Contains(answerOf(resp), "does not exist") {
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

// TestOpenVINOBackend_SpawnReportsItsContainer is the regression that took the
// NPU off the fleet a second time.
//
// The container is created by THIS process's runtime, so the spawn response is
// the only place its identity can come from, and the control plane refuses a
// session that reports none — triage with "session ... is not containerized",
// a delivery with "runs no container". An empty field is therefore
// indistinguishable from having run no container at all, while one sits
// running on the host. Measured 2026-09-16: every openvino triage job created
// its container, reported no id, and was refused one second later.
func TestOpenVINOBackend_SpawnReportsItsContainer(t *testing.T) {
	runtime := &mockContainerRuntime{available: true, runID: "ctr-abc123"}
	backend := NewOpenVINOBackend(OpenVINOBackendConfig{
		BaseURL:      "http://gpu-host.invalid:9000",
		DefaultModel: "qwen3-8b",
	})
	backend.SetContainerRuntime(runtime)

	session, err := backend.Spawn(context.Background(), &SpawnRequest{
		Model:          "qwen3-8b",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "worker:test",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if session.ContainerID != "ctr-abc123" {
		t.Fatalf("session.ContainerID = %q, want the container the backend just started", session.ContainerID)
	}
	if len(runtime.runCalls) != 1 {
		t.Fatalf("container runs = %d, want exactly one", len(runtime.runCalls))
	}
}

// TestOpenVINOBackend_AnUnsandboxedSpawnReportsNoContainer keeps empty a REAL
// answer: a session that genuinely runs no container must not claim one, or
// the control plane would publish a delivery from a host process.
func TestOpenVINOBackend_AnUnsandboxedSpawnReportsNoContainer(t *testing.T) {
	runtime := &mockContainerRuntime{available: true, runID: "ctr-abc123"}
	backend := NewOpenVINOBackend(OpenVINOBackendConfig{
		BaseURL:      "http://gpu-host.invalid:9000",
		DefaultModel: "qwen3-8b",
	})
	backend.SetContainerRuntime(runtime)

	session, err := backend.Spawn(context.Background(), &SpawnRequest{Model: "qwen3-8b"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if session.ContainerID != "" {
		t.Fatalf("session.ContainerID = %q, want empty for a session with no container", session.ContainerID)
	}
	if len(runtime.runCalls) != 0 {
		t.Fatalf("container runs = %d, want none for an unsandboxed spawn", len(runtime.runCalls))
	}
}

// TestOpenVINOBackend_TheKeepAliveCommandSurvivesTheRuntimeMapping is the
// regression for a container that started, returned an id, and was gone a
// second later.
//
// The runtime maps only Entrypoint[0] onto `--entrypoint` and drops the rest,
// so {"tail","-f","/dev/null"} in Entrypoint launched a BARE `tail`. With no
// stdin it read EOF and exited immediately, and AutoRemove took the container
// away — while the spawn had already returned an id, so the session looked
// healthy until the first exec failed with "container: not found". Measured on
// both bound projects 2026-09-16.
func TestOpenVINOBackend_TheKeepAliveCommandSurvivesTheRuntimeMapping(t *testing.T) {
	runtime := &mockContainerRuntime{available: true, runID: "ctr-keepalive"}
	backend := NewOpenVINOBackend(OpenVINOBackendConfig{
		BaseURL:      "http://gpu-host.invalid:9000",
		DefaultModel: "qwen3-8b",
	})
	backend.SetContainerRuntime(runtime)

	if _, err := backend.Spawn(context.Background(), &SpawnRequest{
		Model:          "qwen3-8b",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "worker:test",
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if len(runtime.runCalls) != 1 {
		t.Fatalf("container runs = %d, want exactly one", len(runtime.runCalls))
	}
	spec := runtime.runCalls[0].Spec

	// Every argument of the keep-alive must survive into Command, because
	// that is the only field the runtime passes through whole.
	if got := strings.Join(spec.Command, " "); got != "tail -f /dev/null" {
		t.Fatalf("spec.Command = %q, want the whole keep-alive; a bare tail exits on EOF", got)
	}
	// A non-empty Entrypoint would put argv[0] behind --entrypoint and drop
	// the arguments again.
	if len(spec.Entrypoint) != 0 {
		t.Fatalf("spec.Entrypoint = %v, want cleared so Command is used verbatim", spec.Entrypoint)
	}
	if spec.User == "" {
		t.Fatal("spec.User is empty; the container must run as the uid that owns the bind-mounted checkout")
	}
}

// TestOpenVINOBackend_TheSpawnEnvironmentReachesTheContainer: the environment
// carries how the checkout authenticates and whom it trusts — the ssh agent
// socket and the project's pinned host key among them. A container started
// without it clones nothing, measured as "Host key verification failed" on
// `git fetch origin main`, on both bound projects 2026-09-16.
func TestOpenVINOBackend_TheSpawnEnvironmentReachesTheContainer(t *testing.T) {
	runtime := &mockContainerRuntime{available: true, runID: "ctr-env"}
	backend := NewOpenVINOBackend(OpenVINOBackendConfig{
		BaseURL:      "http://gpu-host.invalid:9000",
		DefaultModel: "qwen3-8b",
	})
	backend.SetContainerRuntime(runtime)

	if _, err := backend.Spawn(context.Background(), &SpawnRequest{
		Model:          "qwen3-8b",
		SandboxMode:    SandboxModeRequired,
		ContainerImage: "worker:test",
		Env:            map[string]string{"SSH_AUTH_SOCK": "/run/agent.sock"},
	}); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	spec := runtime.runCalls[0].Spec
	if spec.Env["SSH_AUTH_SOCK"] != "/run/agent.sock" {
		t.Fatalf("spec.Env = %v, want the spawn's environment carried through", spec.Env)
	}
}

// TestOpenVINOBackend_TheChatPathIsConfigurable is what lets one backend drive
// any OpenAI-compatible server.
//
// This backend is not about one vendor: it is an OpenAI-compatible HTTP client
// that runs its tool calls INSIDE a container, which is the shape a role that
// must read a repository needs — a plain model-server backend has no container
// and cannot be given a workspace at all. OVMS serves /v3/chat/completions;
// ollama, llama-server and vLLM serve /v1. Hardcoding either ties the
// capability to a vendor for no reason.
func TestOpenVINOBackend_TheChatPathIsConfigurable(t *testing.T) {
	cases := []struct {
		name string
		base string
		path string
		want string
	}{
		{"the OVMS default", "http://ovms:9000", "", "http://ovms:9000" + DefaultOVMSChatPath},
		{"an ollama endpoint", "http://gpu:11434", "/v1/chat/completions", "http://gpu:11434/v1/chat/completions"},
		{"a path missing its slash", "http://gpu:11434", "v1/chat/completions", "http://gpu:11434/v1/chat/completions"},
		{"a base with a trailing slash", "http://gpu:11434/", "/v1/chat/completions", "http://gpu:11434/v1/chat/completions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: c.base, ChatPath: c.path, DefaultModel: "m"})
			if got := b.chatURL(); got != c.want {
				t.Fatalf("chatURL() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestOpenVINOBackend_LastRoundIsAskedToAnswer proves the loop spends its
// final round asking for a conclusion rather than reporting a dead end.
//
// The case is a real one: a triage turn read the CI workflow, go.mod, the
// upstream module versions and the implementation — every call on point — and
// then failed with nothing to show because the budget ended mid-investigation.
func TestOpenVINOBackend_LastRoundIsAskedToAnswer(t *testing.T) {
	srv := &ovServer{t: t, responses: []string{
		toolCallJSON("call-1", "ls"),
		toolCallJSON("call-2", "cat go.mod"),
		answerJSON("VERDICT: ACTIONABLE"),
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "m", MaxToolRounds: 2})
	b.SetContainerRuntime(&execRecorder{result: ExecResult{Stdout: "."}})
	b.mu.Lock()
	b.sessions["s"] = &openVINOSession{id: "s", model: "m", containerID: "c"}
	b.mu.Unlock()

	resp, err := b.Prompt(context.Background(), &PromptRequest{SessionID: "s", Prompt: "go"})
	if err != nil {
		t.Fatalf("a spent budget must still produce the model's conclusion: %v", err)
	}
	if len(resp.Content) == 0 || !strings.Contains(answerOf(resp), "VERDICT: ACTIONABLE") {
		t.Fatalf("want the final answer, got %+v", resp.Content)
	}

	// The final request must WITHDRAW the tools, or the model can keep asking.
	final := srv.seen[len(srv.seen)-1]
	if len(final.Tools) != 0 {
		t.Fatalf("the final round must offer no tools, got %d", len(final.Tools))
	}
	last := final.Messages[len(final.Messages)-1]
	if last.Role != "user" || !strings.Contains(last.Content, "budget") {
		t.Fatalf("the final round must say the budget is spent, got %+v", last)
	}
	// Every earlier round still offered them.
	for i, seen := range srv.seen[:len(srv.seen)-1] {
		if len(seen.Tools) == 0 {
			t.Fatalf("round %d offered no tools; only the final round withdraws them", i)
		}
	}
}

// TestOpenVINOBackend_SilentOnLastRoundIsReported keeps the bound meaningful:
// asked point blank for a conclusion and producing nothing is the failure the
// bound exists to report, not an empty answer to hand back.
func TestOpenVINOBackend_SilentOnLastRoundIsReported(t *testing.T) {
	srv := &ovServer{t: t, responses: []string{
		toolCallJSON("call-1", "ls"),
		answerJSON("   "),
	}}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	b := NewOpenVINOBackend(OpenVINOBackendConfig{BaseURL: ts.URL, DefaultModel: "m", MaxToolRounds: 1})
	b.SetContainerRuntime(&execRecorder{result: ExecResult{Stdout: "."}})
	b.mu.Lock()
	b.sessions["s"] = &openVINOSession{id: "s", model: "m", containerID: "c"}
	b.mu.Unlock()

	_, err := b.Prompt(context.Background(), &PromptRequest{SessionID: "s", Prompt: "go"})
	var bound *OpenVINOToolRoundsExceededError
	if !errors.As(err, &bound) {
		t.Fatalf("want OpenVINOToolRoundsExceededError, got %T: %v", err, err)
	}
	if bound.Rounds != 1 {
		t.Fatalf("the refusal must name the bound it hit, got %d", bound.Rounds)
	}
}
