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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"
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
		CreatedAt: sess.created.UnixNano(),
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
		found, ok := b.loadSession(req.SessionID)
		if !ok {
			return nil, &SessionNotFoundError{SessionID: req.SessionID}
		}
		sess = found
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

	// Build argv: <command> [global-args...] exec --json [-c model=...] --skip-git-repo-check [extra-args...] -
	// The trailing "-" makes codex read the prompt from stdin, which avoids
	// argv length limits and shell escaping pitfalls.
	// --skip-git-repo-check is required when the workspace is not inside a
	// git repository codex recognizes (e.g. mounted dev directories); without
	// it codex aborts with "Not inside a trusted directory".
	//
	// --json IS WHAT MAKES THE TURN MEASURABLE. The text mode prints its
	// token count as prose; the JSONL stream carries turn.completed.usage, the
	// same accounting the worker daemons read (parseExecEvents), so a turn on
	// this backend reaches the ledger with its tokens (go-quicbaas#1058).
	argv := append([]string{}, b.args...)
	argv = append(argv, "exec", "--json")
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
	// The session's variables over this daemon's scrubbed environment. Both
	// halves matter: appending to a nil cmd.Env, as this did, gave the codex
	// process the session's variables and NOTHING ELSE — no PATH, no HOME —
	// whenever a session carried any at all, and inheriting os.Environ()
	// untouched gave it the operator's Claude Code session (go-quicbaas#218).
	cmd.Env = agentProcessEnv(sess.env)
	cmd.Stdin = strings.NewReader(full)

	// THE MODEL THE TURN IS RECORDED UNDER. With no model named, codex runs
	// the one its config.toml pins — the same file, found through the same
	// environment, the process above reads — so that is the model the turn
	// ran. A config that pins none leaves codex on its built-in default,
	// which the CLI does not report; the turn is then recorded under
	// CodexCLIDefaultModel rather than refused by the ledger for having no
	// model at all.
	served := model
	if served == "" {
		served = codexConfiguredModel(cmd.Env)
	}
	if served == "" {
		served = CodexCLIDefaultModel
	}

	if req.EventCallback != nil {
		req.EventCallback(&SessionEvent{
			SessionID: req.SessionID,
			Kind:      KindSystemMessage,
			System: &SystemMessagePayload{
				Level:   "info",
				Code:    "codex_exec_start",
				Message: b.command + " exec",
			},
		})
	}

	output, runErr := cmd.CombinedOutput()
	stdout := string(output)

	turn, parseErr := parseExecEvents(stdout)
	if parseErr != nil {
		return nil, &PromptError{SessionID: req.SessionID, Cause: parseErr}
	}
	// THE STREAM IS THE VERDICT, NOT THE EXIT STATUS. A turn is a success
	// only when codex reported it COMPLETED and it produced an answer: an
	// assistant message followed by turn.failed is a failed turn, however
	// much text came before the failure (go-quicbaas#1058 review), and a
	// completed turn with no answer is a failure too.
	//
	// A failure still carries what codex measured and the model the turn
	// ran on, so the ledger records its tokens and names its model rather
	// than recording it unmeasured or refusing it for having no model.
	if turn.stopReason != StopReasonEndTurn || turn.text == "" {
		return nil, &PromptError{
			SessionID: req.SessionID,
			Cause: &OneShotExecutionError{
				Command: b.command + " " + strings.Join(argv, " "),
				Stderr:  stdout,
				Cause:   runErr,
				TurnFailureEvidence: TurnFailureEvidence{
					Usage: turn.usage,
					Model: served,
				},
			},
		}
	}

	if req.EventCallback != nil && turn.text != "" {
		req.EventCallback(&SessionEvent{
			SessionID: req.SessionID,
			Kind:      KindAssistantChunk,
			AssistantChunk: &AssistantChunkPayload{
				Text:  turn.text,
				Model: served,
			},
		})
	}

	return &PromptResponse{
		SessionID:  req.SessionID,
		Model:      served,
		StopReason: StopReasonEndTurn,
		Usage:      turn.usage,
		Content:    turn.content(),
		ToolUse:    turn.toolUse(),
	}, nil
}

// CodexCLIDefaultModel is the model name a codex exec turn is recorded under
// when neither the request, the session nor codex's config.toml names one:
// the CLI's own built-in default, which it does not report. The name is a
// label, never sent to codex; the price table may list it like any model.
const CodexCLIDefaultModel = "codex-cli-default"

// codexConfig is the part of codex's config.toml this backend reads.
type codexConfig struct {
	Model string `toml:"model"`
}

// codexConfiguredModel returns the model codex's config.toml pins for a
// process run with env, or "" when there is no readable config or it pins
// none. The file is found the way codex finds it: $CODEX_HOME/config.toml,
// else $HOME/.codex/config.toml.
func codexConfiguredModel(env []string) string {
	dir := envValue(env, "CODEX_HOME")
	if dir == "" {
		home := envValue(env, "HOME")
		if home == "" {
			return ""
		}
		dir = filepath.Join(home, ".codex")
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		return ""
	}
	var cfg codexConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.Model)
}

// envValue returns key's value in env as a process would see it: the last
// assignment wins.
func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):]
		}
	}
	return ""
}

// loadSession returns the registered session for id. A value of any other
// type is reported as absent rather than trusted.
func (b *CodexExecBackend) loadSession(id string) (*codexExecSession, bool) {
	v, ok := b.sessions.Load(id)
	if !ok {
		return nil, false
	}
	sess, ok := v.(*codexExecSession)
	return sess, ok
}

// Status returns the registered session's state. Always SessionStateIdle
// because codex exec is stateless.
func (b *CodexExecBackend) Status(_ context.Context, sessionID string) (*SessionStatus, error) {
	sess, ok := b.loadSession(sessionID)
	if !ok {
		return nil, &SessionNotFoundError{SessionID: sessionID}
	}
	return &SessionStatus{
		SessionID:    sess.id,
		State:        SessionStateIdle,
		LastActivity: time.Now().UnixNano(),
		Uptime:       int64(time.Since(sess.created).Seconds()),
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
