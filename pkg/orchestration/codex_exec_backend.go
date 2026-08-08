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
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// CodexExecBackend is an AgentBackend that wraps the OpenAI codex CLI in its
// non-interactive `codex exec` mode. Unlike Claude Code, the codex CLI does
// not implement ACP/JSON-RPC over stdio, so the standard ACPBackend cannot
// drive it. This backend instead invokes `codex exec [PROMPT]` once per
// Prompt call, captures stdout, and returns it as a single ContentBlock.
//
// Sessions are bookkeeping-only: Spawn returns a synthetic session ID and
// stores the work directory and CLI args, but no subprocess is started until
// Prompt is invoked. Each Prompt is a fresh codex run with no conversation
// memory between calls. This fits the reviewer-agent use case where every
// review is an independent evaluation of the current workspace state.
type CodexExecBackend struct {
	command      string
	args         []string
	defaultModel string
	sessions     sync.Map // map[string]*codexExecSession
}

// CodexExecBackendConfig configures the codex exec backend.
type CodexExecBackendConfig struct {
	// Command is the codex executable. Empty defaults to "codex".
	Command string

	// Args are extra global args passed before "exec". Typically empty.
	Args []string

	// DefaultModel is the model name used when SpawnRequest.Model is empty.
	// Codex consults its own config when empty.
	DefaultModel string
}

type codexExecSession struct {
	id      string
	workDir string
	model   string
	args    []string
	env     []string
	created time.Time
}

// NewCodexExecBackend constructs a backend ready to run `codex exec`.
func NewCodexExecBackend(cfg CodexExecBackendConfig) *CodexExecBackend {
	cmd := cfg.Command
	if cmd == "" {
		cmd = "codex"
	}
	return &CodexExecBackend{
		command:      cmd,
		args:         append([]string{}, cfg.Args...),
		defaultModel: cfg.DefaultModel,
	}
}

// Type returns the backend identifier used by the orchestrator.
func (b *CodexExecBackend) Type() string { return "codex" }

// ToolGuidance returns empty. codex exec runs the OpenAI codex CLI which
// has its own built-in tool schema; no orchestrator-side guidance is needed.
func (b *CodexExecBackend) ToolGuidance() string { return "" }

// Capabilities reports what the backend can do. Codex exec is stateless
// (no persistent sessions) and the orchestrator may schedule a fresh run
// per prompt.
func (b *CodexExecBackend) Capabilities() BackendCapabilities {
	return BackendCapabilities{
		Kind:               BackendKindModelServer,
		PersistentSessions: false,
		ToolUse:            true,
		FileAccess:         true,
		Streaming:          false,
		ACP:                false,
		TeamManagement:     false,
	}
}

// Spawn registers a session so the orchestrator's session bookkeeping works.
// No subprocess is started here — codex exec is launched per Prompt.
func (b *CodexExecBackend) Spawn(_ context.Context, req *SpawnRequest) (*Session, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.WorkDir == "" {
		return nil, &InputValidationError{Field: "work_dir", Message: "cannot be empty"}
	}

	id, err := newCodexExecSessionID()
	if err != nil {
		return nil, &CodexExecError{Op: "generate session id", Cause: err}
	}

	model := req.Model
	if model == "" {
		model = b.defaultModel
	}

	env := make([]string, 0, len(req.Env))
	for k, v := range req.Env {
		env = append(env, k+"="+v)
	}

	sess := &codexExecSession{
		id:      id,
		workDir: req.WorkDir,
		model:   model,
		args:    append([]string{}, req.ExtraArgs...),
		env:     env,
		created: time.Now(),
	}
	b.sessions.Store(id, sess)

	return &Session{
		ID:        id,
		ProjectID: req.ProjectID,
		Backend:   b.Type(),
		Model:     model,
		Status:    SessionStateIdle,
		WorkDir:   req.WorkDir,
		CreatedAt: sess.created.Unix(),
	}, nil
}

// Prompt invokes `codex exec [PROMPT]` in the session's work directory and
// returns stdout as a single text ContentBlock. If SessionID is empty an
// ephemeral session is created from the request fields. The PromptRequest's
// SystemPrompt, when present, is prepended to the user prompt with a clear
// boundary since codex exec has no separate system-prompt flag.
func (b *CodexExecBackend) Prompt(ctx context.Context, req *PromptRequest) (*PromptResponse, error) {
	if req == nil {
		return nil, &InputValidationError{Field: "request", Message: "cannot be nil"}
	}
	if req.Prompt == "" {
		return nil, &InputValidationError{Field: "prompt", Message: "cannot be empty"}
	}

	var sess *codexExecSession
	if req.SessionID != "" {
		v, ok := b.sessions.Load(req.SessionID)
		if !ok {
			return nil, &SessionNotFoundError{SessionID: req.SessionID}
		}
		sess = v.(*codexExecSession) //nolint:errcheck // we control the value type
	} else {
		// Ephemeral session: no working directory was registered, fall back
		// to the current process's cwd. Callers should prefer Spawn first.
		sess = &codexExecSession{id: "", model: b.defaultModel}
	}

	model := req.Model
	if model == "" {
		model = sess.model
	}

	// Compose the full prompt. codex exec has no -s/--system flag so we
	// inline the system prompt as a clearly-marked preamble.
	full := req.Prompt
	if req.SystemPrompt != "" {
		full = "[SYSTEM]\n" + req.SystemPrompt + "\n\n[USER]\n" + req.Prompt
	}

	// Build argv: <command> [global-args...] exec [-c model=...] --skip-git-repo-check [extra-args...] -
	// The trailing "-" makes codex read the prompt from stdin, which avoids
	// argv length limits and shell escaping pitfalls.
	// --skip-git-repo-check is required when the workspace is not inside a
	// git repository codex recognizes (e.g. mounted dev directories); without
	// it codex aborts with "Not inside a trusted directory".
	argv := append([]string{}, b.args...)
	argv = append(argv, "exec")
	if model != "" {
		argv = append(argv, "-c", "model=\""+model+"\"")
	}
	argv = append(argv, "--skip-git-repo-check")
	argv = append(argv, sess.args...)
	argv = append(argv, "-")

	cmd := exec.CommandContext(ctx, b.command, argv...)
	if sess.workDir != "" {
		cmd.Dir = sess.workDir
	}
	if len(sess.env) > 0 {
		cmd.Env = append(cmd.Env, sess.env...)
	}
	cmd.Stdin = strings.NewReader(full)

	if req.EventCallback != nil {
		req.EventCallback(req.SessionID, "agent:codex_exec_start", b.command+" exec")
	}

	output, runErr := cmd.CombinedOutput()
	stdout := string(output)

	if req.EventCallback != nil && stdout != "" {
		req.EventCallback(req.SessionID, "agent:oneshot_response", stdout)
	}

	if runErr != nil {
		return nil, &PromptError{
			SessionID: req.SessionID,
			Cause: &OneShotExecutionError{
				Command: b.command + " " + strings.Join(argv, " "),
				Stderr:  stdout,
				Cause:   runErr,
			},
		}
	}

	return &PromptResponse{
		SessionID:  req.SessionID,
		Model:      model,
		StopReason: StopReasonEndTurn,
		Content: []ContentBlock{
			{Type: "text", Text: extractCodexAssistantText(stdout)},
		},
	}, nil
}

// Status returns the registered session's state. Always SessionStateIdle
// because codex exec is stateless.
func (b *CodexExecBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	v, ok := b.sessions.Load(sessionID)
	if !ok {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	sess := v.(*codexExecSession) //nolint:errcheck
	return &SessionStatus{
		SessionID:    sess.id,
		State:        SessionStateIdle,
		LastActivity: time.Now().Unix(),
		Uptime:       time.Since(sess.created).Milliseconds(),
	}, nil
}

// Close removes the session bookkeeping. There is no subprocess to terminate.
func (b *CodexExecBackend) Close(_ context.Context, sessionID string) error {
	if _, ok := b.sessions.LoadAndDelete(sessionID); !ok {
		return &SessionNotFoundError{SessionID: sessionID}
	}
	return nil
}

// newCodexExecSessionID returns a 32-hex-char random session ID, prefixed so
// it is visually distinguishable from ACP/Claude session IDs.
func newCodexExecSessionID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", errors.New("codex exec backend: random source failed")
	}
	return "codex-" + hex.EncodeToString(buf[:]), nil
}

// extractCodexAssistantText reduces codex exec output to just the assistant
// turn. The codex CLI emits a structured stream where each top-level block
// starts with a marker on its own line: header (until "--------"), then
// alternating "user" / "exec ..." / "codex" / "tokens used" sections. The
// final assistant answer lives in the last "codex" block; everything inside
// it (until the next marker or EOF) is what we want to return to the caller.
//
// The function uses the LAST occurrence of "codex" so prompt echoes that
// happen earlier (which can include the system prompt's JSON example) are
// not mistaken for the assistant's response. Falls back to the raw output
// when the markers cannot be found so the parser at least has data to scan.
func extractCodexAssistantText(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")

	// markerLine returns the section name when the trimmed line is exactly a
	// known top-level marker, or "" otherwise. We deliberately match exact
	// strings rather than prefixes so prose lines that happen to start with
	// "user" or "codex" inside a section are not mistaken for markers.
	markerLine := func(line string) string {
		switch strings.TrimSpace(line) {
		case "codex":
			return "codex"
		case "user":
			return "user"
		case "tokens used":
			return "tokens used"
		case "thinking":
			return "thinking"
		}
		// "exec ..." sections start with "exec " then a shell command.
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "exec ") {
			return "exec"
		}
		return ""
	}

	// Find the last index where the line is the bare marker "codex".
	lastCodex := -1
	for i, line := range lines {
		if markerLine(line) == "codex" {
			lastCodex = i
		}
	}
	if lastCodex < 0 {
		return strings.TrimSpace(raw)
	}

	// Collect lines after the marker until the next marker or EOF.
	var collected []string
	for i := lastCodex + 1; i < len(lines); i++ {
		if markerLine(lines[i]) != "" {
			break
		}
		collected = append(collected, lines[i])
	}
	if len(collected) == 0 {
		return strings.TrimSpace(raw)
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

// CodexExecError wraps a codex exec backend operation failure with a
// contextual op string. It is the typed replacement for fmt.Errorf and unwraps
// to the underlying cause for errors.Is/As traversal.
type CodexExecError struct {
	// Op names the failing operation, e.g. "generate session id".
	Op string
	// Cause is the wrapped underlying error.
	Cause error
}

// Error implements the error interface.
func (e *CodexExecError) Error() string {
	return "codex exec backend: " + e.Op + ": " + e.Cause.Error()
}

// Unwrap returns the wrapped cause for errors.Is/As support.
func (e *CodexExecError) Unwrap() error { return e.Cause }
